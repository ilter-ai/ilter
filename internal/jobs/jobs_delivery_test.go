package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// deliverViaWebhook — HTTP calls
// ---------------------------------------------------------------------------

func TestDeliverViaWebhook_Success(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var err error
		gotBody, err = io.ReadAll(r.Body)
		assert.NoError(t, err)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &DeliveryConfig{Type: "webhook", WebhookURL: srv.URL}
	err := deliverViaWebhook(context.Background(), cfg, "hello result", &JobRun{ID: "run-w-1", JobID: "job-w-1"})
	assert.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &payload))
	assert.Equal(t, "hello result", payload["result"])
	assert.Equal(t, "run-w-1", payload["run_id"])
	assert.Equal(t, "job-w-1", payload["job_id"])
	assert.NotEmpty(t, payload["timestamp"])
}

func TestDeliverViaWebhook_HTTPError(t *testing.T) {
	tests := []struct {
		name string
		code int
	}{
		{"400 Bad Request", http.StatusBadRequest},
		{"401 Unauthorized", http.StatusUnauthorized},
		{"500 Server Error", http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
			}))
			defer srv.Close()

			cfg := &DeliveryConfig{Type: "webhook", WebhookURL: srv.URL}
			err := deliverViaWebhook(context.Background(), cfg, "result", &JobRun{ID: "run-err", JobID: "job-err"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), fmt.Sprintf("status %d", tc.code))
		})
	}
}

func TestDeliverViaWebhook_TransportError(t *testing.T) {
	cfg := &DeliveryConfig{Type: "webhook", WebhookURL: "http://127.0.0.1:1/invalid"}
	err := deliverViaWebhook(context.Background(), cfg, "result", &JobRun{ID: "run-t", JobID: "job-t"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "webhook request failed")
}

func TestDeliverViaWebhook_MissingURL(t *testing.T) {
	cfg := &DeliveryConfig{Type: "webhook", WebhookURL: ""}
	err := deliverViaWebhook(context.Background(), cfg, "result", &JobRun{ID: "run-no-url", JobID: "job-no-url"})
	assert.ErrorContains(t, err, "URL required")
}

// ---------------------------------------------------------------------------
// Deliver — dispatch
// ---------------------------------------------------------------------------

func TestDeliver_EmptyConfig(t *testing.T) {
	err := Deliver(context.Background(), "", &JobRun{}, "result")
	assert.NoError(t, err, "empty config is a no-op")

	err = Deliver(context.Background(), "{}", &JobRun{}, "result")
	assert.NoError(t, err, "empty JSON config is a no-op")
}

func TestDeliver_UnsupportedType(t *testing.T) {
	err := Deliver(context.Background(), `{"type":"unsupported"}`, &JobRun{}, "result")
	assert.ErrorContains(t, err, "unknown delivery type")
}

func TestDeliver_InvalidJSON(t *testing.T) {
	err := Deliver(context.Background(), "not json", &JobRun{}, "result")
	assert.ErrorContains(t, err, "parse delivery_config")
}

func TestDeliver_MCP_MissingExecutor(t *testing.T) {
	err := Deliver(context.Background(), `{"type":"mcp","mcp_server":"srv","tool":"my-tool"}`, &JobRun{}, "result")
	assert.ErrorContains(t, err, "MCP executor not initialized")
}

// ---------------------------------------------------------------------------
// buildTemplateCtx
// ---------------------------------------------------------------------------

func TestBuildTemplateCtx(t *testing.T) {
	t.Run("basic structure", func(t *testing.T) {
		ctx := buildTemplateCtx(
			[]string{"first output", "second output"},
			[]json.RawMessage{json.RawMessage(`{"key":"val"}`), json.RawMessage(`42`)},
			map[string]any{"name": "world", "url": "https://example.com"},
		)
		// When raw[i] is valid JSON, the parsed value (not the text fallback) is used.
		assert.Equal(t, map[string]any{"key": "val"}, ctx["step0"])
		assert.Equal(t, float64(42), ctx["step1"], "numeric JSON becomes float64")
		assert.Equal(t, float64(42), ctx["prev"], "prev should be last step's parsed value")
		assert.Equal(t, "world", ctx["name"])
		assert.Equal(t, "https://example.com", ctx["url"])
	})

	t.Run("empty inputs", func(t *testing.T) {
		ctx := buildTemplateCtx(nil, nil, nil)
		assert.Empty(t, ctx)

		ctx = buildTemplateCtx([]string{}, []json.RawMessage{}, map[string]any{})
		assert.Empty(t, ctx)
	})

	t.Run("variable shadows step key", func(t *testing.T) {
		ctx := buildTemplateCtx(
			[]string{"real-step"},
			[]json.RawMessage{json.RawMessage(`{}`)},
			map[string]any{"step0": "shadow-step", "name": "alice"},
		)
		// Raw {} is valid JSON, parsed to empty map. Variable "step0" is shadowed.
		assert.Equal(t, map[string]any{}, ctx["step0"], "parsed JSON {} wins over variable")
		assert.Equal(t, "alice", ctx["name"], "non-shadowing variable should be present")
	})

	t.Run("prev absent with no steps", func(t *testing.T) {
		ctx := buildTemplateCtx(nil, nil, map[string]any{"key": "val"})
		_, hasPrev := ctx["prev"]
		assert.False(t, hasPrev, "prev should not exist with no steps")
	})
}

// ---------------------------------------------------------------------------
// renderTemplate
// ---------------------------------------------------------------------------

func TestRenderTemplate(t *testing.T) {
	t.Run("simple substitution", func(t *testing.T) {
		out, err := renderTemplate("Hello {{.name}}", map[string]any{"name": "World"})
		require.NoError(t, err)
		assert.Equal(t, "Hello World", out)
	})

	t.Run("json function", func(t *testing.T) {
		out, err := renderTemplate("data: {{json .obj}}", map[string]any{"obj": map[string]any{"a": 1}})
		require.NoError(t, err)
		assert.Contains(t, out, `"a":1`)
	})

	t.Run("v function", func(t *testing.T) {
		out, err := renderTemplate("{{v \"name\"}}", map[string]any{"name": "Alice"})
		require.NoError(t, err)
		assert.Equal(t, "Alice", out)
	})

	t.Run("v function missing key", func(t *testing.T) {
		_, err := renderTemplate("{{v \"missing\"}}", map[string]any{})
		assert.Error(t, err)
	})

	t.Run("missing key error", func(t *testing.T) {
		_, err := renderTemplate("Hello {{.missing}}", map[string]any{"name": "World"})
		assert.Error(t, err)
	})

	t.Run("invalid template syntax", func(t *testing.T) {
		_, err := renderTemplate("Hello {{.name", map[string]any{})
		assert.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// failRun
// ---------------------------------------------------------------------------

func TestFailRun(t *testing.T) {
	start := time.Now().Add(-time.Millisecond)
	run := &JobRun{Status: "running"}

	failRun(run, start)

	assert.Equal(t, "running", run.Status, "failRun should not change status")
	assert.True(t, run.FinishedAt.Valid)
	assert.GreaterOrEqual(t, run.DurationMs, 0)
	assert.NotZero(t, run.FinishedAt.Time)
}

func TestFailRunSetsStartedAtWhenMissing(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	run := &JobRun{}
	failRun(run, start)
	assert.True(t, run.StartedAt.Valid)
	assert.Equal(t, start.Unix(), run.StartedAt.Time.Unix())
}

func TestFailRunPreservesExistingStartedAt(t *testing.T) {
	existing := time.Now().Add(-time.Hour)
	start := time.Now()
	run := &JobRun{StartedAt: sqlNullTime(existing)}
	failRun(run, start)
	assert.Equal(t, existing.Unix(), run.StartedAt.Time.Unix())
}
