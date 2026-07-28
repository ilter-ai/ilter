package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
)

type PromptInjectionMiddleware struct {
	store *db.SQLiteStore
}

func NewPromptInjectionMiddleware(store *db.SQLiteStore) *PromptInjectionMiddleware {
	return &PromptInjectionMiddleware{store: store}
}

func (m *PromptInjectionMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		promptName := r.Header.Get("X-Prompt-Name")

		if promptName == "" {
			next.ServeHTTP(w, r)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		r.Body.Close()

		var requestMap map[string]any
		if err = json.Unmarshal(bodyBytes, &requestMap); err != nil {
			next.ServeHTTP(w, r)
			return
		}

		tmpl, err := m.store.GetPromptTemplateByName(promptName)
		if err != nil || tmpl == nil {
			promptErrors.Add(r.Context(), 1)
			next.ServeHTTP(w, r)
			return
		}

		var vars map[string]any
		if varsHeader := r.Header.Get("X-Prompt-Vars"); varsHeader != "" {
			if err = json.Unmarshal([]byte(varsHeader), &vars); err != nil {
				slog.Warn("Failed to parse X-Prompt-Vars header", "error", err)
			}
		}

		rendered, err := config.RenderPrompt(tmpl, vars)
		if err != nil {
			promptErrors.Add(r.Context(), 1)
			next.ServeHTTP(w, r)
			return
		}

		messages, _ := requestMap["messages"].([]any)
		systemMsg := map[string]any{
			"role":    "system",
			"content": rendered,
		}
		requestMap["messages"] = append([]any{systemMsg}, messages...)

		modifiedBytes, err := json.Marshal(requestMap)
		if err != nil {
			promptErrors.Add(r.Context(), 1)
			next.ServeHTTP(w, r)
			return
		}

		promptRequests.Add(r.Context(), 1)
		r.Body = io.NopCloser(bytes.NewReader(modifiedBytes))
		r.ContentLength = int64(len(modifiedBytes))
		r.Header.Set("Content-Length", fmt.Sprintf("%d", len(modifiedBytes)))
		next.ServeHTTP(w, r)
	})
}
