package middleware

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ilter-ai/ilter/internal/db/dbtest"
)

func TestInitTracer(t *testing.T) {
	ctx := context.Background()

	// Test empty endpoint (should return nil TracerProvider without error)
	tp, err := InitTracer(ctx, "", 1.0)
	assert.NoError(t, err)
	assert.Nil(t, tp)

	// Test non-empty endpoint
	// Note: We use WithInsecure, so it doesn't try to dial TLS.
	tp, err = InitTracer(ctx, "localhost:4318", 0.5)
	assert.NoError(t, err)
	assert.NotNil(t, tp)

	// Clean up global tracer provider
	err = tp.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestAuditLogger_Logging(t *testing.T) {
	store := dbtest.NewFile(t)

	al := NewAuditLoggerMiddleware(store)
	defer al.Close()

	entry1 := AuditLogEntry{
		KeyID:            "1",
		Model:            "gpt-4o",
		Provider:         "openai",
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalCost:        0.00015,
		LatencyMs:        120,
		StatusCode:       200,
		CacheHit:         false,
		PromptPreview:    "Hello world prompt",
	}

	al.ch <- entry1

	time.Sleep(100 * time.Millisecond)

	var (
		id            int
		model         string
		promptPreview sql.NullString
		timestamp     string
	)
	err := store.DB.QueryRow("SELECT id, model, prompt_preview, timestamp FROM audit_log ORDER BY id DESC LIMIT 1").Scan(&id, &model, &promptPreview, &timestamp)
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4o", model)
	assert.True(t, promptPreview.Valid)
	assert.Equal(t, "Hello world prompt", promptPreview.String)
}
