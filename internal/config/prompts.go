package config

import (
	"fmt"
	"strings"
	"text/template"
	"time"
)

// PromptTemplate represents a prompt template with versioning support.
type PromptTemplate struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     string    `json:"version"` // semantic versioning
	Content     string    `json:"content"` // Go template syntax
	IsActive    bool      `json:"is_active"`
	Labels      []string  `json:"labels"` // for organization/team tagging
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PromptTemplateVersion represents a version in the history.
type PromptTemplateVersion struct {
	ID            int       `json:"id"`
	PromptID      int       `json:"prompt_id"`
	Version       string    `json:"version"`
	Content       string    `json:"content"`
	ChangeSummary string    `json:"change_summary"`
	CreatedAt     time.Time `json:"created_at"`
}

func RenderPrompt(tmpl *PromptTemplate, vars map[string]any) (string, error) {
	t, err := template.New(tmpl.Name).Parse(tmpl.Content)
	if err != nil {
		return "", fmt.Errorf("parsing prompt template %q: %w", tmpl.Name, err)
	}

	var buf strings.Builder
	err = t.Execute(&buf, vars)
	if err != nil {
		return "", fmt.Errorf("rendering prompt template %q: %w", tmpl.Name, err)
	}

	return buf.String(), nil
}

func ValidateTemplate(content string) error {
	_, err := template.New("validation").Parse(content)
	if err != nil {
		return fmt.Errorf("validating template: %w", err)
	}
	return nil
}
