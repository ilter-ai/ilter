package prompts

import (
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/audit"
)

type PromptHandler struct {
	store   *db.SQLiteStore
	auditor *audit.SQLiteConfigAuditor
}

func NewPromptHandler(store *db.SQLiteStore, auditor *audit.SQLiteConfigAuditor) *PromptHandler {
	return &PromptHandler{store: store, auditor: auditor}
}
