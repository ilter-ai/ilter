package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db/sqlc"
)

// CreatePromptTemplate creates a new prompt template and its first version entry.
func (s *SQLiteStore) CreatePromptTemplate(tmpl config.PromptTemplate) (int, error) {
	labelsJSON, err := json.Marshal(tmpl.Labels)
	if err != nil {
		return 0, fmt.Errorf("marshal labels: %w", err)
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	version := tmpl.Version
	if version == "" {
		version = "0.1.0"
	}

	var descPtr *string
	if tmpl.Description != "" {
		descPtr = &tmpl.Description
	}

	labelsStr := string(labelsJSON)

	q := s.queries.WithTx(tx)
	res, err := q.CreatePrompt(context.Background(), sqlc.CreatePromptParams{
		Name:        tmpl.Name,
		Description: descPtr,
		Version:     version,
		Content:     tmpl.Content,
		IsActive:    int64(boolToInt(tmpl.IsActive)),
		Labels:      &labelsStr,
	})
	if err != nil {
		return 0, fmt.Errorf("insert prompt: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	changeSummary := "initial version"
	if err := q.CreatePromptVersion(context.Background(), sqlc.CreatePromptVersionParams{
		PromptID:      id,
		Version:       version,
		Content:       tmpl.Content,
		ChangeSummary: &changeSummary,
	}); err != nil {
		return 0, fmt.Errorf("insert initial version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}

	return int(id), nil
}

// GetPromptTemplate retrieves a prompt template by ID.
func (s *SQLiteStore) GetPromptTemplate(id int) (*config.PromptTemplate, error) {
	p, err := s.queries.GetPromptTemplate(context.Background(), int64(id))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query prompt %d: %w", id, err)
	}
	tmpl := promptFromSQLC(p)
	return &tmpl, nil
}

// GetPromptTemplateByName retrieves a prompt template by name.
func (s *SQLiteStore) GetPromptTemplateByName(name string) (*config.PromptTemplate, error) {
	p, err := s.queries.GetPromptTemplateByName(context.Background(), name)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query prompt by name %q: %w", name, err)
	}
	tmpl := promptFromSQLC(p)
	return &tmpl, nil
}

// ListPromptTemplates returns all prompt templates ordered by name.
func (s *SQLiteStore) ListPromptTemplates() ([]config.PromptTemplate, error) {
	ps, err := s.queries.ListPromptTemplates(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list prompts: %w", err)
	}
	templates := make([]config.PromptTemplate, len(ps))
	for i, p := range ps {
		templates[i] = promptFromSQLC(p)
	}
	return templates, nil
}

// UpdatePromptTemplate updates a prompt template, bumps the version, and records version history.
// The new version is auto-incremented as a patch bump unless specified in the input.
func (s *SQLiteStore) UpdatePromptTemplate(tmpl config.PromptTemplate) error {
	existing, err := s.GetPromptTemplate(tmpl.ID)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("prompt %d not found", tmpl.ID)
	}

	version := tmpl.Version
	if version == "" {
		version = bumpPatch(existing.Version)
	}

	labelsJSON, err := json.Marshal(tmpl.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var descPtr *string
	if tmpl.Description != "" {
		descPtr = &tmpl.Description
	}

	labelsStr := string(labelsJSON)

	q := s.queries.WithTx(tx)
	if err := q.UpdatePrompt(context.Background(), sqlc.UpdatePromptParams{
		Name:        tmpl.Name,
		Description: descPtr,
		Version:     version,
		Content:     tmpl.Content,
		IsActive:    int64(boolToInt(tmpl.IsActive)),
		Labels:      &labelsStr,
		ID:          int64(tmpl.ID),
	}); err != nil {
		return fmt.Errorf("update prompt %d: %w", tmpl.ID, err)
	}

	if err := q.CreatePromptVersion(context.Background(), sqlc.CreatePromptVersionParams{
		PromptID:      int64(tmpl.ID),
		Version:       version,
		Content:       tmpl.Content,
		ChangeSummary: &tmpl.Description,
	}); err != nil {
		return fmt.Errorf("insert version history: %w", err)
	}

	return tx.Commit()
}

// DeletePromptTemplate deletes a prompt template and its version history (CASCADE).
func (s *SQLiteStore) DeletePromptTemplate(id int) error {
	n, err := s.queries.DeletePromptTemplate(context.Background(), int64(id))
	if err != nil {
		return fmt.Errorf("delete prompt %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("prompt %d not found", id)
	}
	return nil
}

// GetPromptTemplateVersions returns all version history for a prompt, newest first.
func (s *SQLiteStore) GetPromptTemplateVersions(promptID int) ([]config.PromptTemplateVersion, error) {
	vs, err := s.queries.GetPromptTemplateVersions(context.Background(), int64(promptID))
	if err != nil {
		return nil, fmt.Errorf("query versions for prompt %d: %w", promptID, err)
	}
	versions := make([]config.PromptTemplateVersion, len(vs))
	for i, v := range vs {
		versions[i] = promptVersionFromSQLC(v)
	}
	return versions, nil
}

func promptFromSQLC(p sqlc.Prompt) config.PromptTemplate {
	var description string
	if p.Description != nil {
		description = *p.Description
	}

	var labels []string
	if p.Labels != nil {
		labels = parseLabels(*p.Labels)
	}

	return config.PromptTemplate{
		ID:          int(p.ID),
		Name:        p.Name,
		Description: description,
		Version:     p.Version,
		Content:     p.Content,
		IsActive:    p.IsActive != 0,
		Labels:      labels,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

func promptVersionFromSQLC(v sqlc.PromptVersion) config.PromptTemplateVersion {
	var changeSummary string
	if v.ChangeSummary != nil {
		changeSummary = *v.ChangeSummary
	}

	return config.PromptTemplateVersion{
		ID:            int(v.ID),
		PromptID:      int(v.PromptID),
		Version:       v.Version,
		Content:       v.Content,
		ChangeSummary: changeSummary,
		CreatedAt:     v.CreatedAt,
	}
}

func parseLabels(raw string) []string {
	var labels []string
	if err := json.Unmarshal([]byte(raw), &labels); err != nil {
		return nil
	}
	return labels
}

func bumpPatch(version string) string {
	var major, minor, patch int
	n, _ := fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch)
	if n < 3 {
		return "0.1.0"
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch+1)
}
