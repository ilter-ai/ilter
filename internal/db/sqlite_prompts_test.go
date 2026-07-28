package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
)

func setupPromptsStore(t *testing.T) testStore {
	t.Helper()
	ts := setupTestStore(t)

	// Verify V9 migration applied prompts tables
	var exists bool
	err := ts.store.DB.QueryRow(
		"SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name='prompts'",
	).Scan(&exists)
	require.NoError(t, err)
	if !exists {
		t.Fatal("prompts table not found — V9 migration may not have run")
	}

	return ts
}

func samplePrompt(name string) config.PromptTemplate {
	return config.PromptTemplate{
		Name:        name,
		Description: "test prompt",
		Version:     "1.0.0",
		Content:     "Hello {{.Name}}, welcome to {{.Service}}!",
		IsActive:    true,
		Labels:      []string{"production", "staging"},
	}
}

func TestCreatePromptTemplate(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	id, err := ts.store.CreatePromptTemplate(samplePrompt("create-test"))
	require.NoError(t, err)
	assert.Greater(t, id, 0)
}

func TestCreatePromptTemplate_DefaultsVersion(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	tmpl := config.PromptTemplate{
		Name:    "default-version",
		Content: "test content",
	}

	id, err := ts.store.CreatePromptTemplate(tmpl)
	require.NoError(t, err)
	assert.Greater(t, id, 0)

	saved, err := ts.store.GetPromptTemplate(id)
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, "0.1.0", saved.Version)
}

func TestCreatePromptTemplate_DuplicateName(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	_, err := ts.store.CreatePromptTemplate(samplePrompt("dup"))
	require.NoError(t, err)

	_, err = ts.store.CreatePromptTemplate(samplePrompt("dup"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UNIQUE")
}

func TestGetPromptTemplate(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	id, err := ts.store.CreatePromptTemplate(samplePrompt("get-test"))
	require.NoError(t, err)

	tmpl, err := ts.store.GetPromptTemplate(id)
	require.NoError(t, err)
	require.NotNil(t, tmpl)

	assert.Equal(t, id, tmpl.ID)
	assert.Equal(t, "get-test", tmpl.Name)
	assert.Equal(t, "test prompt", tmpl.Description)
	assert.Equal(t, "1.0.0", tmpl.Version)
	assert.Equal(t, "Hello {{.Name}}, welcome to {{.Service}}!", tmpl.Content)
	assert.True(t, tmpl.IsActive)
	assert.Equal(t, []string{"production", "staging"}, tmpl.Labels)
	assert.NotEmpty(t, tmpl.CreatedAt, "created_at should be set by DB")
	assert.NotEmpty(t, tmpl.UpdatedAt, "updated_at should be set by DB")
}

func TestGetPromptTemplate_ByName(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	_, err := ts.store.CreatePromptTemplate(samplePrompt("by-name"))
	require.NoError(t, err)

	tmpl, err := ts.store.GetPromptTemplateByName("by-name")
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	assert.Equal(t, "by-name", tmpl.Name)
}

func TestGetPromptTemplate_NotFound(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	tmpl, err := ts.store.GetPromptTemplate(99999)
	require.NoError(t, err)
	assert.Nil(t, tmpl)
}

func TestGetPromptTemplateByName_NotFound(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	tmpl, err := ts.store.GetPromptTemplateByName("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, tmpl)
}

func TestListPromptTemplates_Empty(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	templates, err := ts.store.ListPromptTemplates()
	require.NoError(t, err)
	assert.Empty(t, templates)
}

func TestListPromptTemplates_Multiple(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	names := []string{"alpha", "beta", "gamma"}
	for _, name := range names {
		_, err := ts.store.CreatePromptTemplate(samplePrompt(name))
		require.NoError(t, err)
	}

	templates, err := ts.store.ListPromptTemplates()
	require.NoError(t, err)
	assert.Len(t, templates, 3)

	// Verify alphabetical order
	assert.Equal(t, "alpha", templates[0].Name)
	assert.Equal(t, "beta", templates[1].Name)
	assert.Equal(t, "gamma", templates[2].Name)
}

func TestUpdatePromptTemplate(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	id, err := ts.store.CreatePromptTemplate(samplePrompt("update-test"))
	require.NoError(t, err)

	updated := config.PromptTemplate{
		ID:          id,
		Name:        "update-test",
		Description: "updated description",
		Version:     "2.0.0",
		Content:     "Updated {{.Name}} content!",
		IsActive:    false,
		Labels:      []string{"canary"},
	}

	err = ts.store.UpdatePromptTemplate(updated)
	require.NoError(t, err)

	saved, err := ts.store.GetPromptTemplate(id)
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, "updated description", saved.Description)
	assert.Equal(t, "2.0.0", saved.Version)
	assert.Equal(t, "Updated {{.Name}} content!", saved.Content)
	assert.False(t, saved.IsActive)
	assert.Equal(t, []string{"canary"}, saved.Labels)
}

func TestUpdatePromptTemplate_AutoVersion(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	id, err := ts.store.CreatePromptTemplate(samplePrompt("auto-version"))
	require.NoError(t, err)

	updated := config.PromptTemplate{
		ID:      id,
		Name:    "auto-version",
		Content: "New content",
	}

	err = ts.store.UpdatePromptTemplate(updated)
	require.NoError(t, err)

	saved, err := ts.store.GetPromptTemplate(id)
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, "1.0.1", saved.Version, "should auto-bump patch")
}

func TestUpdatePromptTemplate_NotFound(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	err := ts.store.UpdatePromptTemplate(config.PromptTemplate{ID: 99999})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDeletePromptTemplate(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	id, err := ts.store.CreatePromptTemplate(samplePrompt("delete-test"))
	require.NoError(t, err)

	err = ts.store.DeletePromptTemplate(id)
	require.NoError(t, err)

	tmpl, err := ts.store.GetPromptTemplate(id)
	require.NoError(t, err)
	assert.Nil(t, tmpl)
}

func TestDeletePromptTemplate_NotFound(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	err := ts.store.DeletePromptTemplate(99999)
	require.Error(t, err)
}

func TestDeletePromptTemplate_CascadesVersions(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	id, err := ts.store.CreatePromptTemplate(samplePrompt("cascade-test"))
	require.NoError(t, err)

	// Update a few times to create version history
	for i := 0; i < 3; i++ {
		err = ts.store.UpdatePromptTemplate(config.PromptTemplate{
			ID:      id,
			Name:    "cascade-test",
			Content: "version",
		})
		require.NoError(t, err)
	}

	// Verify versions exist
	versions, err := ts.store.GetPromptTemplateVersions(id)
	require.NoError(t, err)
	assert.Len(t, versions, 4) // 1 initial + 3 updates

	// Delete prompt
	err = ts.store.DeletePromptTemplate(id)
	require.NoError(t, err)

	// Verify versions are gone (CASCADE)
	versions, err = ts.store.GetPromptTemplateVersions(id)
	require.NoError(t, err)
	assert.Empty(t, versions)
}

func TestGetPromptTemplateVersions(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	id, err := ts.store.CreatePromptTemplate(samplePrompt("versions-test"))
	require.NoError(t, err)

	// Initial version
	versions, err := ts.store.GetPromptTemplateVersions(id)
	require.NoError(t, err)
	require.Len(t, versions, 1)
	assert.Equal(t, "1.0.0", versions[0].Version)
	assert.Equal(t, "initial version", versions[0].ChangeSummary)

	// Update
	err = ts.store.UpdatePromptTemplate(config.PromptTemplate{
		ID:          id,
		Name:        "versions-test",
		Content:     "v2 content",
		Description: "second version",
	})
	require.NoError(t, err)

	versions, err = ts.store.GetPromptTemplateVersions(id)
	require.NoError(t, err)
	assert.Len(t, versions, 2)
}

func TestGetPromptTemplateVersions_OrderedByNewest(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	id, err := ts.store.CreatePromptTemplate(config.PromptTemplate{
		Name:    "order-test",
		Content: "v1",
	})
	require.NoError(t, err)

	err = ts.store.UpdatePromptTemplate(config.PromptTemplate{
		ID:      id,
		Name:    "order-test",
		Content: "v2",
	})
	require.NoError(t, err)

	err = ts.store.UpdatePromptTemplate(config.PromptTemplate{
		ID:      id,
		Name:    "order-test",
		Content: "v3",
	})
	require.NoError(t, err)

	versions, err := ts.store.GetPromptTemplateVersions(id)
	require.NoError(t, err)
	require.Len(t, versions, 3)

	assert.Equal(t, "v3", versions[0].Content)
	assert.Equal(t, "v2", versions[1].Content)
}

func TestPromptTemplate_EmptyLabels(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	tmpl := config.PromptTemplate{
		Name:    "no-labels",
		Content: "test",
	}

	id, err := ts.store.CreatePromptTemplate(tmpl)
	require.NoError(t, err)

	saved, err := ts.store.GetPromptTemplate(id)
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Empty(t, saved.Labels)
}

func TestFullPromptLifecycle(t *testing.T) {
	ts := setupPromptsStore(t)
	defer ts.close()

	// Create
	id, err := ts.store.CreatePromptTemplate(samplePrompt("lifecycle"))
	require.NoError(t, err)
	assert.Greater(t, id, 0)

	// Read
	tmpl, err := ts.store.GetPromptTemplate(id)
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	assert.Equal(t, "lifecycle", tmpl.Name)

	// Update
	tmpl.Content = "Updated content for {{.User}}"
	tmpl.Description = "lifecycle updated"
	err = ts.store.UpdatePromptTemplate(*tmpl)
	require.NoError(t, err)

	saved, err := ts.store.GetPromptTemplate(id)
	require.NoError(t, err)
	assert.Equal(t, "Updated content for {{.User}}", saved.Content)

	// List
	templates, err := ts.store.ListPromptTemplates()
	require.NoError(t, err)
	assert.Len(t, templates, 1)

	// Delete
	err = ts.store.DeletePromptTemplate(id)
	require.NoError(t, err)

	templates, err = ts.store.ListPromptTemplates()
	require.NoError(t, err)
	assert.Empty(t, templates)
}
