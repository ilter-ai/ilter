package mcp

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/ilter-ai/ilter/internal/db"
)

type AuditEntry struct {
	APIKeyID   string
	Tool       string
	ServerID   string
	Method     string
	Params     string
	DurationMs float64
	StatusCode int
	Success    bool
	ErrorMsg   string
	ClientIP   string
}

type AuditLogger struct {
	store *db.SQLiteStore
	ch    chan AuditEntry
	wg    sync.WaitGroup
	done  chan struct{}
}

func NewAuditLogger(store *db.SQLiteStore) *AuditLogger {
	l := &AuditLogger{
		store: store,
		ch:    make(chan AuditEntry, 1000),
		done:  make(chan struct{}),
	}
	l.wg.Go(func() {
		l.worker()
	})
	return l
}

func (l *AuditLogger) worker() {
	for {
		select {
		case entry, ok := <-l.ch:
			if !ok {
				return
			}
			query := `INSERT INTO mcp_audit_log
			(key_id, tool, server_id, method, params, duration_ms, status_code, success, error_msg, client_ip)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

			paramsJSON := "{}"
			if entry.Params != "" {
				var m map[string]any
				if err := json.Unmarshal([]byte(entry.Params), &m); err == nil {
					sanitized := make(map[string]any)
					for k, v := range m {
						if isSensitiveParam(k) {
							sanitized[k] = "***"
						} else {
							sanitized[k] = v
						}
					}
					if b, err := json.Marshal(sanitized); err == nil {
						paramsJSON = string(b)
					}
				}
			}

			_, err := l.store.DB.Exec(
				query,
				nullIfEmpty(entry.APIKeyID),
				entry.Tool,
				nullIfEmpty(entry.ServerID),
				entry.Method,
				paramsJSON,
				entry.DurationMs,
				entry.StatusCode,
				boolToInt(entry.Success),
				nullIfEmpty(entry.ErrorMsg),
				nullIfEmpty(entry.ClientIP),
			)
			if err != nil {
				mcpLog.Error("failed to write audit log", "error", err)
			}
		case <-l.done:
			return
		}
	}
}

func (l *AuditLogger) LogAsync(entry AuditEntry) {
	select {
	case l.ch <- entry:
	default:
		mcpLog.Warn("audit log channel full, dropping entry")
	}
}

func (l *AuditLogger) Close() {
	close(l.done)
	l.wg.Wait()
}

func isSensitiveParam(key string) bool {
	sensitive := map[string]bool{
		"api_key":       true,
		"apiKey":        true,
		"password":      true,
		"secret":        true,
		"token":         true,
		"authorization": true,
	}
	return sensitive[key]
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type AuditFilter struct {
	Tool     string
	ServerID string
	Method   string
	Success  *bool
	From     string
	To       string
	Limit    int
	Offset   int
	// Source narrows results by call origin using the server_id sentinel the
	// OpenAPI bridge logs under: "mcp" excludes server_id='openapi' rows,
	// "openapi" includes only them. Empty means no filtering by source.
	Source string
}

type AuditLogEntry struct {
	ID         int     `json:"id"`
	APIKeyID   *string `json:"key_id,omitempty"`
	Tool       string  `json:"tool"`
	ServerID   string  `json:"server_id"`
	Method     string  `json:"method"`
	Params     string  `json:"params,omitempty"`
	DurationMs float64 `json:"duration_ms"`
	StatusCode int     `json:"status_code"`
	Success    bool    `json:"success"`
	ErrorMsg   *string `json:"error_msg,omitempty"`
	ClientIP   *string `json:"client_ip,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

func (l *AuditLogger) Query(filter AuditFilter) ([]AuditLogEntry, int, error) {
	args := []any{}
	conds := []string{}

	if filter.Tool != "" {
		conds = append(conds, "tool = ?")
		args = append(args, filter.Tool)
	}
	if filter.ServerID != "" {
		conds = append(conds, "server_id = ?")
		args = append(args, filter.ServerID)
	}
	if filter.Method != "" {
		conds = append(conds, "method = ?")
		args = append(args, filter.Method)
	}
	switch filter.Source {
	case "openapi":
		conds = append(conds, "server_id = 'openapi'")
	case "mcp":
		conds = append(conds, "server_id != 'openapi'")
	}
	if filter.Success != nil {
		if *filter.Success {
			conds = append(conds, "success = 1")
		} else {
			conds = append(conds, "success = 0")
		}
	}
	// datetime(...) on both sides: created_at is a bare "YYYY-MM-DD HH:MM:SS"
	// string, while the frontend sends a JS Date.toISOString() value
	// ("...T....000Z") — already a precise timestamp, not a bare date, so no
	// end-of-day suffix is needed (appending " 23:59:59" to a full timestamp
	// only produced a doubly-malformed string). A raw string comparison
	// between the two formats is lexicographically wrong (space < 'T') and
	// silently drops same-day rows; datetime() normalizes both before comparing.
	if filter.From != "" {
		conds = append(conds, "datetime(created_at) >= datetime(?)")
		args = append(args, filter.From)
	}
	if filter.To != "" {
		conds = append(conds, "datetime(created_at) <= datetime(?)")
		args = append(args, filter.To)
	}

	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	countSQL := "SELECT COUNT(*) FROM mcp_audit_log" + where
	if err := l.store.DB.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit logs: %w", err)
	}

	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset := filter.Offset

	dataSQL := `SELECT id, key_id, tool, server_id, method, params,
		duration_ms, status_code, success, error_msg, client_ip, created_at
		FROM mcp_audit_log` + where + ` ORDER BY id DESC LIMIT ? OFFSET ?`
	dataArgs := append(args, limit, offset)

	rows, err := l.store.DB.Query(dataSQL, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query audit logs: %w", err)
	}
	defer rows.Close()

	var entries []AuditLogEntry
	for rows.Next() {
		var e AuditLogEntry
		var keyID, serverID, params, errorMsg, clientIP, createdAt sql.NullString
		var statusCode, successInt sql.NullInt64
		var durationMs sql.NullFloat64

		if err := rows.Scan(&e.ID, &keyID, &e.Tool, &serverID,
			&e.Method, &params, &durationMs, &statusCode, &successInt, &errorMsg, &clientIP, &createdAt); err != nil {
			return nil, 0, fmt.Errorf("scan audit log: %w", err)
		}

		if keyID.Valid {
			e.APIKeyID = &keyID.String
		}
		if serverID.Valid {
			e.ServerID = serverID.String
		}
		if params.Valid {
			e.Params = params.String
		}
		if durationMs.Valid {
			e.DurationMs = durationMs.Float64
		}
		if statusCode.Valid {
			e.StatusCode = int(statusCode.Int64)
		}
		if successInt.Valid {
			e.Success = successInt.Int64 == 1
		}
		if errorMsg.Valid {
			e.ErrorMsg = &errorMsg.String
		}
		if clientIP.Valid {
			e.ClientIP = &clientIP.String
		}
		if createdAt.Valid {
			e.CreatedAt = db.FormatSQLiteTimestamp(createdAt.String)
		}

		entries = append(entries, e)
	}

	return entries, total, nil
}
