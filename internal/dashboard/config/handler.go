package dashconfig

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/audit"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// ConfigAPIHandler serves REST CRUD endpoints for runtime configuration
// sections. It bridges the dashboard API to the DB-backed stores and
// triggers a hot-apply cache refresh after every write mutation.
type ConfigAPIHandler struct {
	stores      *config.RuntimeStores
	configCache *config.Cache
	auditor     *audit.SQLiteConfigAuditor // nil if audit disabled
	db          *sql.DB                    // for optimistic concurrency version checks
}

// NewConfigAPIHandler creates a ConfigAPIHandler.
//
// Parameters:
//   - stores:   runtime store access (may have nil entries for unavailable stores)
//   - cache:    config cache to refresh after writes
//   - auditor:  audit logger (nil if audit disabled)
//   - db:       database handle (for version checks; may be nil)
func NewConfigAPIHandler(stores *config.RuntimeStores, cache *config.Cache, auditor *audit.SQLiteConfigAuditor, db *sql.DB) *ConfigAPIHandler {
	return &ConfigAPIHandler{
		stores:      stores,
		configCache: cache,
		auditor:     auditor,
		db:          db,
	}
}

func (h *ConfigAPIHandler) rcStore() *db.SQLiteStore {
	if h.stores.RuntimeConfig == nil {
		return nil
	}
	store, _ := h.stores.RuntimeConfig.(*db.SQLiteStore)
	return store
}

// ─────────────────────────────────────────────────────────────────────
// meta carries section metadata in every response.
// ─────────────────────────────────────────────────────────────────────

type configMeta struct {
	Section string `json:"section"`
	Count   int    `json:"count,omitempty"`
	Key     string `json:"key,omitempty"`
}

type configListResponse struct {
	Data any        `json:"data"`
	Meta configMeta `json:"meta"`
}

// ─────────────────────────────────────────────────────────────────────
// ListSection handles  GET /api/config/{section}
// ─────────────────────────────────────────────────────────────────────

func (h *ConfigAPIHandler) ListSection(w http.ResponseWriter, r *http.Request) {
	section := chi.URLParam(r, "section")
	if section == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "section parameter is required")
		return
	}

	switch section {
	case "feature_flags":
		h.listFeatureFlags(w, r)
	case "runtime_config":
		h.listRuntimeConfig(w, r)
	default:
		h.listRuntimeConfig(w, r)
	}
}

// ─────────────────────────────────────────────────────────────────────
// GetItem handles  GET /api/config/{section}/{key}
// ─────────────────────────────────────────────────────────────────────

func (h *ConfigAPIHandler) GetItem(w http.ResponseWriter, r *http.Request) {
	section := chi.URLParam(r, "section")
	key := chi.URLParam(r, "key")
	if section == "" || key == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "section and key parameters are required")
		return
	}

	switch section {
	case "feature_flags":
		h.getFeatureFlag(w, r, key)
	case "runtime_config":
		h.getRuntimeConfig(w, r, key)
	default:
		h.getRuntimeConfig(w, r, key)
	}
}

// ─────────────────────────────────────────────────────────────────────
// Section: feature_flags
// ─────────────────────────────────────────────────────────────────────

// featureFlagEntry is the JSON shape for a single flag in list responses.
type featureFlagEntry struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func (h *ConfigAPIHandler) listFeatureFlags(w http.ResponseWriter, _ *http.Request) {
	if h.rcStore() == nil {
		writeConfigList(w, "feature_flags", []any{})
		return
	}

	entries, err := h.rcStore().GetBySection("feature_flag")
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	flagEntries := make([]featureFlagEntry, 0, len(entries))
	for name, raw := range entries {
		var enabled bool
		if err := json.Unmarshal([]byte(raw), &enabled); err != nil {
			enabled = raw == "true" || raw == "1"
		}
		flagEntries = append(flagEntries, featureFlagEntry{Name: name, Enabled: enabled})
	}
	writeConfigList(w, "feature_flags", flagEntries)
}

func (h *ConfigAPIHandler) getFeatureFlag(w http.ResponseWriter, _ *http.Request, name string) {
	if h.rcStore() == nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "feature flag store not available")
		return
	}

	entry, err := h.rcStore().GetRuntimeConfigEntry("feature_flag", name)
	if err != nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	var enabled bool
	if err := json.Unmarshal([]byte(entry.Value), &enabled); err != nil {
		enabled = entry.Value == "true" || entry.Value == "1"
	}
	writeConfigItem(w, "feature_flags", name, featureFlagEntry{Name: name, Enabled: enabled})
}

// ─────────────────────────────────────────────────────────────────────
// Section: runtime_config (generic key-value entries)
// ─────────────────────────────────────────────────────────────────────

type runtimeConfigEntry struct {
	Section string `json:"section"`
	Key     string `json:"key"`
	Value   string `json:"value"`
}

func (h *ConfigAPIHandler) listRuntimeConfig(w http.ResponseWriter, r *http.Request) {
	if h.rcStore() == nil {
		writeConfigList(w, "runtime_config", []any{})
		return
	}

	// Support ?section= filter query parameter.
	sectionFilter := r.URL.Query().Get("section")

	var all map[string]string
	var err error
	if sectionFilter != "" {
		entries, e := h.rcStore().GetBySection(sectionFilter)
		if e != nil {
			model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", e.Error())
			return
		}
		all = make(map[string]string, len(entries))
		for k, v := range entries {
			all[sectionFilter+":"+k] = v
		}
	} else {
		all, err = h.rcStore().GetAll()
		if err != nil {
			model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	entries := make([]runtimeConfigEntry, 0, len(all))
	for compositeKey, value := range all {
		// compositeKey is "section:key"
		section, key := splitCompositeKey(compositeKey)
		entries = append(entries, runtimeConfigEntry{
			Section: section,
			Key:     key,
			Value:   value,
		})
	}
	writeConfigList(w, "runtime_config", entries)
}

func (h *ConfigAPIHandler) getRuntimeConfig(w http.ResponseWriter, _ *http.Request, key string) {
	if h.rcStore() == nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "runtime_config store not available")
		return
	}

	// key may be "section:key" (composite) or just "section" (list by section).
	section, subKey := splitCompositeKey(key)
	if subKey == "" {
		// Treat as section filter — list entries for this section.
		entries, err := h.rcStore().GetBySection(section)
		if err != nil {
			model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		items := make([]runtimeConfigEntry, 0, len(entries))
		for k, v := range entries {
			items = append(items, runtimeConfigEntry{Section: section, Key: k, Value: v})
		}
		writeConfigList(w, "runtime_config", items)
		return
	}

	entry, err := h.rcStore().GetRuntimeConfigEntry(section, subKey)
	if err != nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeConfigItem(w, "runtime_config", compositeKey(section, subKey), runtimeConfigEntry{
		Section: entry.Section,
		Key:     entry.Key,
		Value:   entry.Value,
	})
}

func (h *ConfigAPIHandler) createRuntimeConfig(raw []byte) (string, error) {
	if h.rcStore() == nil {
		return "", fmt.Errorf("runtime_config store not available")
	}
	var entry runtimeConfigEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return "", fmt.Errorf("unmarshal runtime_config: %w", err)
	}
	if entry.Section == "" {
		return "", fmt.Errorf("runtime_config: section is required")
	}
	if entry.Key == "" {
		return "", fmt.Errorf("runtime_config: key is required")
	}

	// Schema validation.
	if err := config.ValidateConfig(entry.Section, entry.Key, entry.Value); err != nil {
		return "", err
	}

	if err := h.rcStore().UpsertRuntimeConfig(entry.Section, entry.Key, entry.Value, "admin-api"); err != nil {
		return "", err
	}
	return compositeKey(entry.Section, entry.Key), nil
}

func (h *ConfigAPIHandler) updateRuntimeConfig(composite string, raw []byte) error {
	if h.rcStore() == nil {
		return fmt.Errorf("runtime_config store not available")
	}
	section, key := splitCompositeKey(composite)
	if section == "" || key == "" {
		return fmt.Errorf("runtime_config: invalid key %q, expected section:key", composite)
	}

	var body struct {
		Value   string `json:"value"`
		Version int    `json:"version,omitempty"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("unmarshal runtime_config update: %w", err)
	}

	// Schema validation.
	if err := config.ValidateConfig(section, key, body.Value); err != nil {
		return err
	}

	if err := h.rcStore().UpsertRuntimeConfig(section, key, body.Value, "admin-api"); err != nil {
		return err
	}
	return nil
}

func (h *ConfigAPIHandler) deleteRuntimeConfig(composite string) error {
	if h.rcStore() == nil {
		return fmt.Errorf("runtime_config store not available")
	}
	section, key := splitCompositeKey(composite)
	if section == "" || key == "" {
		return fmt.Errorf("runtime_config: invalid key %q, expected section:key", composite)
	}
	return h.rcStore().DeleteRuntimeConfig(section, key)
}

func (h *ConfigAPIHandler) getRuntimeConfigItem(composite string) (map[string]any, error) {
	if h.rcStore() == nil {
		return nil, sql.ErrNoRows
	}
	section, key := splitCompositeKey(composite)
	if section == "" || key == "" {
		return nil, fmt.Errorf("invalid runtime_config key %q", composite)
	}
	entry, err := h.rcStore().GetRuntimeConfigEntry(section, key)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"section": entry.Section,
		"key":     entry.Key,
		"value":   entry.Value,
		"version": entry.Version,
	}, nil
}

// splitCompositeKey splits "section:key" into (section, key).
// If there is no colon, key is returned empty.
func splitCompositeKey(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

// compositeKey joins section and key with a colon.
func compositeKey(section, key string) string {
	return section + ":" + key
}

// ─────────────────────────────────────────────────────────────────────
// CRUD: POST /api/config/{section} — create entry
// ─────────────────────────────────────────────────────────────────────

// Create handles POST /api/config/{section}. It validates the request body,
// writes to the appropriate store, records an audit entry, and triggers a
// config cache hot-reload.
func (h *ConfigAPIHandler) Create(w http.ResponseWriter, r *http.Request) {
	section := chi.URLParam(r, "section")
	if section == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "section parameter is required")
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "cannot read request body")
		return
	}
	if len(rawBody) == 0 {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "empty request body")
		return
	}

	storeSection := urlToStoreSection(section)
	if err = validateWritePayload(storeSection, rawBody, w); err != nil {
		return
	}

	var bodyMap map[string]any
	if err = json.Unmarshal(rawBody, &bodyMap); err != nil {
		bodyMap = map[string]any{"raw": string(rawBody)}
	}

	performedBy := reqmeta.GetKeyID(r.Context())

	key, err := h.createInStore(section, rawBody)
	if err != nil {
		if isDuplicateError(err) {
			model.WriteJSONError(w, http.StatusConflict, "already_exists", err.Error())
			return
		}
		slog.Error("config create failed", "section", section, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if h.auditor != nil {
		entityType := storeSection
		if entityType == "" {
			entityType = section
		}
		if err := h.auditor.LogCreate(entityType, key, bodyMap, performedBy); err != nil {
			slog.Warn("audit log create failed", "section", entityType, "key", key, "error", err)
		}
	}

	if h.configCache != nil {
		if err := h.configCache.Refresh(r.Context(), h.stores); err != nil {
			slog.Warn("config cache refresh after create failed", "error", err)
		}
	} else {
		slog.Warn("configCache nil after create — runtime config change not applied until restart", "section", section, "key", key)
	}

	model.WriteJSON(w, http.StatusCreated, map[string]any{
		"status": "created",
		"key":    key,
	})
}

// ─────────────────────────────────────────────────────────────────────
// CRUD: PUT /api/config/{section}/{key} — update entry
// ─────────────────────────────────────────────────────────────────────

// Update handles PUT /api/config/{section}/{key}. It validates the request
// body, checks optimistic concurrency (version field), writes to the store,
// records an audit entry, and triggers a config cache hot-reload.
func (h *ConfigAPIHandler) Update(w http.ResponseWriter, r *http.Request) {
	section := chi.URLParam(r, "section")
	key := chi.URLParam(r, "key")
	if section == "" || key == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "section and key parameters are required")
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "cannot read request body")
		return
	}
	if len(rawBody) == 0 {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "empty request body")
		return
	}

	storeSection := urlToStoreSection(section)

	if err := validateWritePayload(storeSection, rawBody, w); err != nil {
		return
	}

	if h.db != nil {
		reqVersion := extractVersion(rawBody)
		if reqVersion > 0 {
			if err := h.checkVersion(storeSection, key, reqVersion); err != nil {
				if strings.Contains(err.Error(), "version conflict") {
					model.WriteJSONError(w, http.StatusConflict, "version_conflict", err.Error())
					return
				}
				slog.Error("version check failed", "section", section, "key", key, "error", err)
				model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
		}
	}

	var bodyMap map[string]any
	if err := json.Unmarshal(rawBody, &bodyMap); err != nil {
		bodyMap = map[string]any{"raw": string(rawBody)}
	}

	performedBy := reqmeta.GetKeyID(r.Context())

	var oldValues map[string]any
	if h.auditor != nil {
		oldEntry, err := h.getItem(section, key)
		if err == nil {
			oldValues = oldEntry
		}
	}

	if err := h.updateInStore(section, key, rawBody); err != nil {
		if isNotFound(err) {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", fmt.Sprintf("%s/%s not found", section, key))
			return
		}
		slog.Error("config update failed", "section", section, "key", key, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if h.auditor != nil {
		entityType := storeSection
		if entityType == "" {
			entityType = section
		}
		if err := h.auditor.LogUpdate(entityType, key, oldValues, bodyMap, performedBy); err != nil {
			slog.Warn("audit log update failed", "section", entityType, "key", key, "error", err)
		}
	}

	if h.configCache != nil {
		if err := h.configCache.Refresh(r.Context(), h.stores); err != nil {
			slog.Warn("config cache refresh after update failed", "error", err)
		}
	} else {
		slog.Warn("configCache nil after update — runtime config change not applied until restart", "section", section, "key", key)
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "updated",
		"key":    key,
	})
}

// ─────────────────────────────────────────────────────────────────────
// CRUD: DELETE /api/config/{section}/{key} — delete entry
// ─────────────────────────────────────────────────────────────────────

// Delete handles DELETE /api/config/{section}/{key}. It records an audit
// entry, removes the record, and triggers a config cache hot-reload.
func (h *ConfigAPIHandler) Delete(w http.ResponseWriter, r *http.Request) {
	section := chi.URLParam(r, "section")
	key := chi.URLParam(r, "key")
	if section == "" || key == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "section and key parameters are required")
		return
	}

	storeSection := urlToStoreSection(section)
	performedBy := reqmeta.GetKeyID(r.Context())

	var oldValues map[string]any
	if h.auditor != nil {
		oldEntry, err := h.getItem(section, key)
		if err == nil {
			oldValues = oldEntry
		}
	}

	if err := h.deleteFromStore(section, key); err != nil {
		if isNotFound(err) {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", fmt.Sprintf("%s/%s not found", section, key))
			return
		}
		slog.Error("config delete failed", "section", section, "key", key, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if h.auditor != nil {
		entityType := storeSection
		if entityType == "" {
			entityType = section
		}
		if err := h.auditor.LogDelete(entityType, key, oldValues, performedBy); err != nil {
			slog.Warn("audit log delete failed", "section", entityType, "key", key, "error", err)
		}
	}

	if h.configCache != nil {
		if err := h.configCache.Refresh(r.Context(), h.stores); err != nil {
			slog.Warn("config cache refresh after delete failed", "error", err)
		}
	} else {
		slog.Warn("configCache nil after delete — runtime config change not applied until restart", "section", section, "key", key)
	}

	w.WriteHeader(http.StatusNoContent)
}

// ─────────────────────────────────────────────────────────────────────
// Internal CRUD dispatchers
// ─────────────────────────────────────────────────────────────────────

// createInStore dispatches creation to the appropriate store by URL section name.
func (h *ConfigAPIHandler) createInStore(section string, rawBody []byte) (string, error) {
	switch section {
	case "runtime_config":
		return h.createRuntimeConfig(rawBody)
	default:
		return h.createRuntimeConfig(rawBody)
	}
}

// updateInStore dispatches updates to the appropriate store.
func (h *ConfigAPIHandler) updateInStore(section, key string, rawBody []byte) error {
	switch section {
	case "runtime_config":
		return h.updateRuntimeConfig(key, rawBody)
	default:
		return h.updateRuntimeConfig(key, rawBody)
	}
}

// deleteFromStore dispatches deletion to the appropriate store.
func (h *ConfigAPIHandler) deleteFromStore(section, key string) error {
	switch section {
	case "runtime_config":
		return h.deleteRuntimeConfig(key)
	default:
		return h.deleteRuntimeConfig(key)
	}
}

// getItem returns a single entry as a map for a given section and key.
// Used by Update and Delete to capture old values for audit logging.
func (h *ConfigAPIHandler) getItem(section, key string) (map[string]any, error) {
	switch section {
	case "runtime_config":
		return h.getRuntimeConfigItem(key)
	default:
		return h.getRuntimeConfigItem(key)
	}
}

// ─────────────────────────────────────────────────────────────────────
// Optimistic concurrency
// ─────────────────────────────────────────────────────────────────────

// checkVersion reads the current version of a runtime_config entry and
// returns an error if it does not match expectedVersion.
func (h *ConfigAPIHandler) checkVersion(section, key string, expectedVersion int) error {
	if h.db == nil {
		return nil
	}
	var currentVersion int
	err := h.db.QueryRow(
		"SELECT version FROM runtime_config WHERE section = ? AND key = ?",
		section, key,
	).Scan(&currentVersion)
	if err == sql.ErrNoRows {
		return fmt.Errorf("version conflict: %s/%s does not exist", section, key)
	}
	if err != nil {
		return fmt.Errorf("version check %s/%s: %w", section, key, err)
	}
	if currentVersion != expectedVersion {
		return fmt.Errorf("version conflict: expected version %d, current version %d",
			expectedVersion, currentVersion)
	}
	return nil
}

// extractVersion attempts to read the "version" field from a raw JSON body.
// Returns 0 if not present or not a number.
func extractVersion(raw []byte) int {
	var v struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0
	}
	return v.Version
}

// ─────────────────────────────────────────────────────────────────────
// Validation helpers
// ─────────────────────────────────────────────────────────────────────

// urlToStoreSection maps a URL section name to a runtime_config store section.
// Returns the URL name as-is if no mapping exists.
func urlToStoreSection(urlSection string) string {
	m := map[string]string{
		"providers":       "provider",
		"mcp_servers":     "mcp_server",
		"guardrail_rules": "guardrail_rule",
		"openapi_tools":   "openapi_tool",
		"routing":         "routing_strategy",
		"runtime_config":  "runtime_config",
	}
	if v, ok := m[urlSection]; ok {
		return v
	}
	return urlSection
}

// validateWritePayload runs ValidateRuntimeConfig on the raw body.
// It writes an error response and returns an error on failure.
func validateWritePayload(storeSection string, rawBody []byte, w http.ResponseWriter) error {
	if storeSection == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "unknown config section")
		return fmt.Errorf("empty store section")
	}

	vResult, err := config.ValidateRuntimeConfig(storeSection, rawBody)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("validation error: %v", err))
		return err
	}
	if !vResult.Valid {
		msgs := make([]string, 0, len(vResult.Errors))
		for _, e := range vResult.Errors {
			msgs = append(msgs, fmt.Sprintf("%s: %s", e.Field, e.Message))
		}
		model.WriteJSONError(w, http.StatusUnprocessableEntity, "validation_error",
			strings.Join(msgs, "; "))
		return fmt.Errorf("validation failed: %s", strings.Join(msgs, "; "))
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────
// Error detection helpers
// ─────────────────────────────────────────────────────────────────────

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if err == sql.ErrNoRows {
		return true
	}
	return strings.Contains(err.Error(), "not found")
}

func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "PRIMARY KEY")
}

// ─────────────────────────────────────────────────────────────────────
// Utility
// ─────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────
// Response helpers
// ─────────────────────────────────────────────────────────────────────

func writeConfigList(w http.ResponseWriter, section string, data any) {
	count := 0
	if v, ok := data.([]any); ok {
		count = len(v)
	} else if v, ok := data.([]featureFlagEntry); ok {
		count = len(v)
	}

	model.WriteJSON(w, http.StatusOK, configListResponse{
		Data: data,
		Meta: configMeta{Section: section, Count: count},
	})
}

func writeConfigItem(w http.ResponseWriter, section, key string, data any) {
	model.WriteJSON(w, http.StatusOK, configListResponse{
		Data: data,
		Meta: configMeta{Section: section, Key: key},
	})
}
