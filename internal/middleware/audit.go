package middleware

import (
	"log/slog"
	"sync"

	"github.com/ilter-ai/ilter/internal/db"
)

type AuditLogEntry struct {
	KeyID            string
	Model            string
	Provider         string
	PromptTokens     int
	CompletionTokens int
	TotalCost        float64
	LatencyMs        int
	StatusCode       int
	CacheHit         bool
	PromptPreview    string
	RequestBody      string
	ResponseBody     string
	ComplexityScore  float64
	IPAddress        string
}

type AuditLoggerMiddleware struct {
	store *db.SQLiteStore
	ch    chan AuditLogEntry
	wg    sync.WaitGroup
	done  chan struct{}
}

func NewAuditLoggerMiddleware(store *db.SQLiteStore) *AuditLoggerMiddleware {
	l := &AuditLoggerMiddleware{
		store: store,
		ch:    make(chan AuditLogEntry, 1000),
		done:  make(chan struct{}),
	}
	l.wg.Go(func() {
		l.worker()
	})
	return l
}

func (l *AuditLoggerMiddleware) worker() {
	for {
		select {
		case entry := <-l.ch:
			_, err := l.store.DB.Exec(
				`INSERT INTO audit_log
					(key_id, model, provider, prompt_tokens, completion_tokens, total_cost,
					 latency_ms, status_code, cache_hit, prompt_preview, request_body, response_body, complexity_score, client_ip)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				nullIfEmpty(entry.KeyID),
				entry.Model,
				entry.Provider,
				entry.PromptTokens,
				entry.CompletionTokens,
				entry.TotalCost,
				entry.LatencyMs,
				entry.StatusCode,
				boolToInt(entry.CacheHit),
				nullIfEmpty(entry.PromptPreview),
				nullIfEmpty(entry.RequestBody),
				nullIfEmpty(entry.ResponseBody),
				entry.ComplexityScore,
				nullIfEmpty(entry.IPAddress),
			)
			if err != nil {
				slog.Error("Failed to write audit log", "error", err)
			}
		case <-l.done:
			return
		}
	}
}

func (l *AuditLoggerMiddleware) LogAsync(entry AuditLogEntry) {
	select {
	case l.ch <- entry:
	default:
		slog.Warn("audit log channel full, dropping entry")
	}
}

func (l *AuditLoggerMiddleware) Close() {
	close(l.done)
	l.wg.Wait()
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
