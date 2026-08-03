package dashboard

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/db"
)

// RequestsHandler serves request-related admin endpoints.
type RequestsHandler struct {
	store *db.SQLiteStore
}

func NewRequestsHandler(store *db.SQLiteStore) *RequestsHandler {
	return &RequestsHandler{store: store}
}

// Helper function to decompress bytes inline
func decompressBytesInline(data []byte) *string {
	if len(data) == 0 {
		return nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		// Not actually compressed or corrupt — return as-is
		s := string(data)
		return &s
	}
	defer reader.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		s := string(data)
		return &s
	}
	s := buf.String()
	return &s
}

func (h *RequestsHandler) HandleRequestDetail(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_id", "invalid request id")
		return
	}

	sqldb := h.store.DB

	var detail RequestDetail
	var keyID *string
	var clientIP *string
	var traceID *string
	var promptPreview *string
	var reqBody []byte
	var respBody []byte
	var compressed *int

	err = sqldb.QueryRow(`
		SELECT
			al.id, al.timestamp, al.key_id, al.model, al.provider,
			al.prompt_tokens, al.completion_tokens, al.total_cost, al.latency_ms,
			al.status_code, al.cache_hit, al.client_ip, al.trace_id,
			al.prompt_preview,
			EXISTS(SELECT 1 FROM audit_body WHERE audit_log_id = al.id) AS has_body,
			ab.request_body, ab.response_body, ab.compressed,
			al.guardrail_latency_ms, al.llm_latency_ms, al.queued_latency_ms
		FROM audit_log al
		LEFT JOIN audit_body ab ON ab.audit_log_id = al.id
		WHERE al.id = ?
	`, id).Scan(
		&detail.ID, &detail.Timestamp, &keyID, &detail.Model, &detail.Provider,
		&detail.PromptTokens, &detail.CompletionTokens, &detail.TotalCost, &detail.LatencyMs,
		&detail.StatusCode, &detail.CacheHit, &clientIP, &traceID,
		&promptPreview, &detail.HasBody,
		&reqBody, &respBody, &compressed,
		&detail.PhaseLatencies.GuardrailLatencyMs,
		&detail.PhaseLatencies.LLMLatencyMs,
		&detail.PhaseLatencies.QueuedLatencyMs,
	)

	if err == sql.ErrNoRows {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "request not found")
		return
	}
	if err != nil {
		slog.Error("Failed to query request detail", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if keyID != nil {
		detail.KeyID = *keyID
	}
	if clientIP != nil {
		detail.ClientIP = *clientIP
	}
	if traceID != nil {
		detail.TraceID = traceID
	}
	if promptPreview != nil {
		detail.PromptPreview = *promptPreview
	}
	detail.Timestamp = db.FormatSQLiteTimestamp(detail.Timestamp)

	if compressed != nil && *compressed == 1 {
		detail.RequestBody = decompressBytesInline(reqBody)
		detail.ResponseBody = decompressBytesInline(respBody)
	} else {
		detail.RequestBody = new(string(reqBody))
		detail.ResponseBody = new(string(respBody))
	}

	model.WriteJSON(w, http.StatusOK, detail)
}

func (h *RequestsHandler) HandleListRequests(w http.ResponseWriter, r *http.Request) {
	// Parse pagination parameters
	page := 1
	if pStr := r.URL.Query().Get("page"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
			page = p
		}
	}

	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}
	offset := (page - 1) * limit

	// Parse filters
	status := r.URL.Query().Get("status")
	modelQuery := r.URL.Query().Get("model")
	provider := r.URL.Query().Get("provider")
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")

	// Build query
	query := `SELECT
		al.id, al.timestamp, al.key_id, al.model, al.provider,
		al.prompt_tokens, al.completion_tokens, al.total_cost, al.latency_ms,
		al.status_code, al.cache_hit, al.client_ip, al.trace_id, al.prompt_preview,
		EXISTS(SELECT 1 FROM audit_body WHERE audit_log_id = al.id) AS has_body
	FROM audit_log al WHERE 1=1`

	countQuery := `SELECT COUNT(*) FROM audit_log al WHERE 1=1`

	var args []any
	var countArgs []any

	if status != "" {
		if status == "success" {
			query += " AND al.status_code < 400"
			countQuery += " AND al.status_code < 400"
		} else if status == "error" {
			query += " AND al.status_code >= 400"
			countQuery += " AND al.status_code >= 400"
		}
	}

	if modelQuery != "" {
		filterPattern := "%" + modelQuery + "%"
		query += " AND (al.model LIKE ? OR al.provider LIKE ? OR al.client_ip LIKE ? OR al.key_id LIKE ?)"
		countQuery += " AND (al.model LIKE ? OR al.provider LIKE ? OR al.client_ip LIKE ? OR al.key_id LIKE ?)"
		args = append(args, filterPattern, filterPattern, filterPattern, filterPattern)
		countArgs = append(countArgs, filterPattern, filterPattern, filterPattern, filterPattern)
	}

	if provider != "" {
		query += " AND al.provider = ?"
		countQuery += " AND al.provider = ?"
		args = append(args, provider)
		countArgs = append(countArgs, provider)
	}

	// datetime(...) on both sides: al.timestamp is a bare "YYYY-MM-DD HH:MM:SS"
	// string, while the frontend sends a JS Date.toISOString() value
	// ("...T....000Z"). A raw string comparison between those two formats is
	// lexicographically wrong (space < 'T') and silently drops same-day rows;
	// wrapping both in datetime() normalizes them before comparing.
	if start != "" {
		query += " AND datetime(al.timestamp) >= datetime(?)"
		countQuery += " AND datetime(al.timestamp) >= datetime(?)"
		args = append(args, start)
		countArgs = append(countArgs, start)
	}

	if end != "" {
		query += " AND datetime(al.timestamp) <= datetime(?)"
		countQuery += " AND datetime(al.timestamp) <= datetime(?)"
		args = append(args, end)
		countArgs = append(countArgs, end)
	}

	// Get total count
	var total int
	sqldb := h.store.DB
	err := sqldb.QueryRow(countQuery, countArgs...).Scan(&total)
	if err != nil {
		slog.Error("Failed to query requests count", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	// Add ordering and pagination
	query += " ORDER BY al.id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := sqldb.Query(query, args...)
	if err != nil {
		slog.Error("Failed to query requests list", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer rows.Close()

	items := make([]RequestSummary, 0)
	for rows.Next() {
		var item RequestSummary
		var keyID *string
		var clientIP *string
		var traceID *string
		var promptPreview *string

		err := rows.Scan(
			&item.ID, &item.Timestamp, &keyID, &item.Model, &item.Provider,
			&item.PromptTokens, &item.CompletionTokens, &item.TotalCost, &item.LatencyMs,
			&item.StatusCode, &item.CacheHit, &clientIP, &traceID, &promptPreview,
			&item.HasBody,
		)
		if err != nil {
			slog.Error("Failed to scan request summary row", "error", err)
			model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		if keyID != nil {
			item.KeyID = *keyID
		}
		if clientIP != nil {
			item.ClientIP = *clientIP
		}
		if traceID != nil {
			item.TraceID = traceID
		}
		if promptPreview != nil {
			item.PromptPreview = *promptPreview
		}
		item.Timestamp = db.FormatSQLiteTimestamp(item.Timestamp)

		items = append(items, item)
	}

	resp := Page[RequestSummary]{
		Items: items,
		Total: total,
		Page:  page,
		Limit: limit,
	}

	model.WriteJSON(w, http.StatusOK, resp)
}
