package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateTemplate_Valid(t *testing.T) {
	err := ValidateTemplate("Hello {{.Name}}")
	assert.NoError(t, err)
}

func TestValidateTemplate_Invalid(t *testing.T) {
	err := ValidateTemplate("Hello {{.Name")
	assert.Error(t, err)
}

func TestValidateTemplate_Empty(t *testing.T) {
	err := ValidateTemplate("")
	assert.NoError(t, err)
}

func TestRenderPrompt_Basic(t *testing.T) {
	tmpl := &PromptTemplate{
		Name:    "greeting",
		Content: "Hello, {{.Name}}!",
	}
	result, err := RenderPrompt(tmpl, map[string]interface{}{"Name": "World"})
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", result)
}

func TestRenderPrompt_MultipleVars(t *testing.T) {
	tmpl := &PromptTemplate{
		Name:    "multi",
		Content: "{{.Greeting}}, {{.Name}}! You are {{.Age}} years old.",
	}
	result, err := RenderPrompt(tmpl, map[string]interface{}{
		"Greeting": "Hi",
		"Name":     "Alice",
		"Age":      30,
	})
	require.NoError(t, err)
	assert.Equal(t, "Hi, Alice! You are 30 years old.", result)
}

func TestRenderPrompt_NoVars(t *testing.T) {
	tmpl := &PromptTemplate{
		Name:    "static",
		Content: "Static text without variables",
	}
	result, err := RenderPrompt(tmpl, map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "Static text without variables", result)
}

func TestRenderPrompt_MissingVar(t *testing.T) {
	tmpl := &PromptTemplate{
		Name:    "missing",
		Content: "Hello, {{.Name}}!",
	}
	// text/template renders missing keys as "<no value>" (intentional – uncovers template bugs)
	result, err := RenderPrompt(tmpl, map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "Hello, <no value>!", result)
}

func TestRenderPrompt_InvalidTemplate(t *testing.T) {
	tmpl := &PromptTemplate{
		Name:    "bad",
		Content: "Hello {{.Name",
	}
	_, err := RenderPrompt(tmpl, map[string]interface{}{"Name": "World"})
	assert.Error(t, err)
}

func TestRenderPrompt_NilVars(t *testing.T) {
	tmpl := &PromptTemplate{
		Name:    "nilvars",
		Content: "Hello, {{.Name}}!",
	}
	result, err := RenderPrompt(tmpl, nil)
	require.NoError(t, err)
	assert.Equal(t, "Hello, <no value>!", result)
}

// Full struct coverage test to ensure all fields are properly set
func TestPromptTemplateStruct(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	tmpl := PromptTemplate{
		ID:          1,
		Name:        "test",
		Description: "Test template",
		Version:     "1.0.0",
		Content:     "content",
		IsActive:    true,
		Labels:      []string{"prod", "stable"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	assert.Equal(t, 1, tmpl.ID)
	assert.Equal(t, "test", tmpl.Name)
	assert.Equal(t, "Test template", tmpl.Description)
	assert.Equal(t, "1.0.0", tmpl.Version)
	assert.Equal(t, "content", tmpl.Content)
	assert.True(t, tmpl.IsActive)
	assert.Equal(t, []string{"prod", "stable"}, tmpl.Labels)
	assert.Equal(t, now, tmpl.CreatedAt)
	assert.Equal(t, now, tmpl.UpdatedAt)
}

func TestPromptTemplateVersionStruct(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	v := PromptTemplateVersion{
		ID:            1,
		PromptID:      2,
		Version:       "1.0.0",
		Content:       "content",
		ChangeSummary: "Initial version",
		CreatedAt:     now,
	}
	assert.Equal(t, 1, v.ID)
	assert.Equal(t, 2, v.PromptID)
	assert.Equal(t, "1.0.0", v.Version)
	assert.Equal(t, "content", v.Content)
	assert.Equal(t, "Initial version", v.ChangeSummary)
	assert.Equal(t, now, v.CreatedAt)
}
