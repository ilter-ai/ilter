package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/config/openapi"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/audit"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

var adminOpenapiLog = slog.With("component", "openapi")

type OpenAPIHandler struct {
	store    *db.SQLiteStore
	provider *openapi.ToolProvider
	auditor  *audit.SQLiteConfigAuditor
}

func NewOpenAPIHandler(store *db.SQLiteStore, auditor *audit.SQLiteConfigAuditor) *OpenAPIHandler {
	return &OpenAPIHandler{store: store, auditor: auditor}
}

// LoadEnabledOpenAPISpecs reads enabled OpenAPI specs from the database.
func LoadEnabledOpenAPISpecs(store *db.SQLiteStore) ([]config.OpenAPISpecConfig, error) {
	rows, err := store.DB.Query(`SELECT name, description, spec_url, operations, auth_type, auth_value, auth_key, timeout_ms FROM openapi_specs WHERE enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var specs []config.OpenAPISpecConfig
	for rows.Next() {
		var name, description, specURL, operations, authType, authValue, authKey sql.NullString
		var timeoutMs sql.NullInt64
		if err := rows.Scan(&name, &description, &specURL, &operations, &authType, &authValue, &authKey, &timeoutMs); err != nil {
			adminOpenapiLog.Warn("skipping spec row", "error", err)
			continue
		}
		var ops []string
		if opsStr := nullToEmpty(operations); opsStr != "" {
			if err := json.Unmarshal([]byte(opsStr), &ops); err != nil {
				ops = []string{}
			}
		}
		specs = append(specs, config.OpenAPISpecConfig{
			Name:        nullToEmpty(name),
			Description: nullToEmpty(description),
			SpecURL:     nullToEmpty(specURL),
			Operations:  ops,
			Auth: config.OpenAPIAuthConfig{
				Type:  nullToEmpty(authType),
				Value: nullToEmpty(authValue),
				Key:   nullToEmpty(authKey),
			},
			Timeout: time.Duration(timeoutMs.Int64) * time.Millisecond,
		})
	}
	return specs, rows.Err()
}

func (h *OpenAPIHandler) SetProvider(p *openapi.ToolProvider) {
	h.provider = p
}

func (h *OpenAPIHandler) reloadProvider() {
	if h.provider == nil {
		return
	}
	specs, err := LoadEnabledOpenAPISpecs(h.store)
	if err != nil {
		adminOpenapiLog.Warn("failed to load specs for reload", "error", err)
		return
	}
	if err := h.provider.Reload(specs); err != nil {
		adminOpenapiLog.Warn("reload failed", "error", err)
	}
}

func (h *OpenAPIHandler) ListSpecs(w http.ResponseWriter, _ *http.Request) {
	rows, err := h.store.DB.Query(`SELECT id, name, description, spec_url, operations, auth_type, auth_value, auth_key, timeout_ms, enabled, created_at, updated_at FROM openapi_specs ORDER BY name`)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list OpenAPI specs")
		return
	}
	defer rows.Close()

	specs := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, description, specURL, operations, authType, authValue, authKey, createdAt, updatedAt sql.NullString
		var timeoutMs, enabled sql.NullInt64

		if err := rows.Scan(&id, &name, &description, &specURL, &operations, &authType, &authValue, &authKey, &timeoutMs, &enabled, &createdAt, &updatedAt); err != nil {
			continue
		}

		specs = append(specs, map[string]any{
			"id":          nullToEmpty(id),
			"name":        nullToEmpty(name),
			"description": nullToEmpty(description),
			"spec_url":    nullToEmpty(specURL),
			"operations":  nullToEmpty(operations),
			"auth_type":   nullToEmpty(authType),
			"auth_value":  nullToEmpty(authValue),
			"auth_key":    nullToEmpty(authKey),
			"timeout_ms":  timeoutMs.Int64,
			"enabled":     enabled.Int64 == 1,
			"created_at":  nullToEmpty(createdAt),
			"updated_at":  nullToEmpty(updatedAt),
		})
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{"specs": specs})
}

type operationsOrRaw string

func (o *operationsOrRaw) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		*o = "[]"
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*o = operationsOrRaw(s)
		return nil
	}
	if b[0] == '[' {
		*o = operationsOrRaw(string(b))
		return nil
	}
	return apiErr("operations must be a JSON string or array")
}

type apiErr string

func (e apiErr) Error() string { return string(e) }

func (h *OpenAPIHandler) CreateSpec(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		SpecURL     string          `json:"spec_url"`
		Ops         operationsOrRaw `json:"operations"`
		AuthType    string          `json:"auth_type"`
		AuthValue   string          `json:"auth_value"`
		AuthKey     string          `json:"auth_key"`
		TimeoutMs   int             `json:"timeout_ms"`
		Enabled     bool            `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	if req.Name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "name is required")
		return
	}

	id := uuid.New().String()

	if req.TimeoutMs == 0 {
		req.TimeoutMs = 30000
	}
	ops := string(req.Ops)
	if ops == "" {
		ops = "[]"
	}
	if req.AuthType == "" {
		req.AuthType = "none"
	}

	_, err := h.store.DB.Exec(`INSERT INTO openapi_specs (id, name, description, spec_url, operations, auth_type, auth_value, auth_key, timeout_ms, enabled) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, req.Description, req.SpecURL, ops, req.AuthType, req.AuthValue, req.AuthKey, req.TimeoutMs, boolToInt(req.Enabled))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			model.WriteJSONError(w, http.StatusConflict, "duplicate_spec", "Spec name already exists")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create spec: "+err.Error())
		return
	}

	h.reloadProvider()

	if h.auditor != nil {
		vals := map[string]any{
			"name":        req.Name,
			"description": req.Description,
			"spec_url":    req.SpecURL,
			"ops":         string(req.Ops),
			"auth_type":   req.AuthType,
			"enabled":     req.Enabled,
		}
		if req.AuthValue != "" {
			vals["auth_value"] = "***"
		}
		if req.AuthKey != "" {
			vals["auth_key"] = "***"
		}
		if err := h.auditor.LogCreate("openapi_spec", id, vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit create openapi_spec", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusCreated, map[string]any{"status": "ok", "id": id})
}

func (h *OpenAPIHandler) UpdateSpec(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Spec ID is required")
		return
	}

	defer r.Body.Close()
	var req struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		SpecURL     string          `json:"spec_url"`
		Ops         operationsOrRaw `json:"operations"`
		AuthType    string          `json:"auth_type"`
		AuthValue   string          `json:"auth_value"`
		AuthKey     string          `json:"auth_key"`
		TimeoutMs   *int            `json:"timeout_ms,omitempty"`
		Enabled     *bool           `json:"enabled,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	sets := []string{}
	args := []any{}

	if req.Name != "" {
		sets = append(sets, "name = ?")
		args = append(args, req.Name)
	}
	if req.Description != "" {
		sets = append(sets, "description = ?")
		args = append(args, req.Description)
	}

	if req.SpecURL != "" {
		sets = append(sets, "spec_url = ?")
		args = append(args, req.SpecURL)
	}
	if ops := string(req.Ops); ops != "" {
		sets = append(sets, "operations = ?")
		args = append(args, ops)
	}
	if req.AuthType != "" {
		sets = append(sets, "auth_type = ?")
		args = append(args, req.AuthType)
	}
	if req.AuthValue != "" {
		sets = append(sets, "auth_value = ?")
		args = append(args, req.AuthValue)
	}
	if req.AuthKey != "" {
		sets = append(sets, "auth_key = ?")
		args = append(args, req.AuthKey)
	}
	if req.TimeoutMs != nil {
		sets = append(sets, "timeout_ms = ?")
		args = append(args, *req.TimeoutMs)
	}
	if req.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, boolToInt(*req.Enabled))
	}

	// Capture old values for audit
	var oldName, oldSpecURL, oldOps, oldAuthType, oldAuthValue, oldAuthKey sql.NullString
	var oldTimeoutMs, oldEnabled sql.NullInt64
	if err := h.store.DB.QueryRow(
		`SELECT name, spec_url, operations, auth_type, auth_value, auth_key, timeout_ms, enabled
		 FROM openapi_specs WHERE id = ?`, id,
	).Scan(&oldName, &oldSpecURL, &oldOps, &oldAuthType, &oldAuthValue, &oldAuthKey, &oldTimeoutMs, &oldEnabled); err != nil {
		slog.Warn("Failed to read old spec values for audit", "spec_id", id, "error", err)
	}

	sets = append(sets, "updated_at = datetime('now')")
	args = append(args, id)

	q := "UPDATE openapi_specs SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	res, err := h.store.DB.Exec(q, args...)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			model.WriteJSONError(w, http.StatusConflict, "duplicate_spec", "Spec name already exists")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update spec: "+err.Error())
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Spec not found")
		return
	}

	if h.auditor != nil {
		oldVals := openapiSpecVals(nullToEmpty(oldName), nullToEmpty(oldSpecURL), nullToEmpty(oldOps), nullToEmpty(oldAuthType), nullToEmpty(oldAuthValue), nullToEmpty(oldAuthKey), int(oldTimeoutMs.Int64), oldEnabled.Int64 == 1)
		newVals := openapiSpecVals(
			ifStr(req.Name, nullToEmpty(oldName)), ifStr(req.SpecURL, nullToEmpty(oldSpecURL)), string(req.Ops),
			ifStr(req.AuthType, nullToEmpty(oldAuthType)), req.AuthValue, req.AuthKey,
			ifInt(req.TimeoutMs, int(oldTimeoutMs.Int64)), ifBool(req.Enabled, oldEnabled.Int64 == 1),
		)
		if err := h.auditor.LogUpdate("openapi_spec", id, oldVals, newVals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit update openapi_spec", "error", err)
		}
	}

	h.reloadProvider()
	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *OpenAPIHandler) ToggleSpec(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Spec ID is required")
		return
	}

	res, err := h.store.DB.Exec(
		"UPDATE openapi_specs SET enabled = CASE WHEN enabled = 1 THEN 0 ELSE 1 END, updated_at = datetime('now') WHERE id = ?",
		id,
	)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to toggle spec")
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Spec not found")
		return
	}

	var enabledInt int64
	err = h.store.DB.QueryRow("SELECT enabled FROM openapi_specs WHERE id = ?", id).Scan(&enabledInt)
	if err != nil {
		model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
		return
	}
	enabled := enabledInt != 0

	h.reloadProvider()
	model.WriteJSON(w, http.StatusOK, map[string]any{"enabled": enabled})
}

func (h *OpenAPIHandler) DeleteSpec(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Spec ID is required")
		return
	}

	var oldName, oldSpecURL, oldOps, oldAuthType, oldAuthValue, oldAuthKey sql.NullString
	var oldTimeoutMs, oldEnabled sql.NullInt64
	fetchErr := h.store.DB.QueryRow(
		`SELECT name, spec_url, operations, auth_type, auth_value, auth_key, timeout_ms, enabled
		 FROM openapi_specs WHERE id = ?`, id,
	).Scan(&oldName, &oldSpecURL, &oldOps, &oldAuthType, &oldAuthValue, &oldAuthKey, &oldTimeoutMs, &oldEnabled)

	res, err := h.store.DB.Exec("DELETE FROM openapi_specs WHERE id = ?", id)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to delete spec")
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Spec not found")
		return
	}

	if h.auditor != nil && fetchErr == nil {
		vals := openapiSpecVals(nullToEmpty(oldName), nullToEmpty(oldSpecURL), nullToEmpty(oldOps), nullToEmpty(oldAuthType), nullToEmpty(oldAuthValue), nullToEmpty(oldAuthKey), int(oldTimeoutMs.Int64), oldEnabled.Int64 == 1)
		if err := h.auditor.LogDelete("openapi_spec", id, vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit delete openapi_spec", "error", err)
		}
	}

	h.reloadProvider()
	w.WriteHeader(http.StatusNoContent)
}

func (h *OpenAPIHandler) SyncOperationsFromProvider(ctx context.Context) {
	log := slog.With("component", "openapi-sync")

	if h.provider == nil {
		log.Info("no OpenAPI provider, skipping operation sync")
		return
	}

	rows, err := h.store.DB.QueryContext(ctx, "SELECT id, name, COALESCE(operations, '') FROM openapi_specs WHERE enabled = 1")
	if err != nil {
		log.Error("failed to query enabled OpenAPI specs", "error", err)
		return
	}
	defer rows.Close()

	opsByAPI := h.provider.OperationsByAPI()
	updatedAny := false
	var ids []string
	for rows.Next() {
		var id, name, existingOps string
		if err := rows.Scan(&id, &name, &existingOps); err != nil {
			log.Warn("skipping spec row", "error", err)
			continue
		}
		ids = append(ids, id)

		discoveredPaths, ok := opsByAPI[name]
		if !ok {
			continue
		}

		newOpsJSON, err := json.Marshal(discoveredPaths)
		if err != nil {
			log.Warn("failed to marshal discovered operations", "spec_id", id, "error", err)
			continue
		}

		if existingOps == string(newOpsJSON) {
			continue
		}

		if _, err := h.store.DB.ExecContext(ctx, `UPDATE openapi_specs SET operations = ?, updated_at = datetime('now') WHERE id = ?`, string(newOpsJSON), id); err != nil {
			log.Warn("failed to update openapi operations", "spec_id", id, "error", err)
			continue
		}
		updatedAny = true
	}

	log.Info("synced OpenAPI operations from provider", "specs", len(ids), "updated", updatedAny)
}

func (h *OpenAPIHandler) validateSpecByID(_ context.Context, id string, forceUpdate bool) (int, bool, error) {
	var name, specURL, existingOps string
	err := h.store.DB.QueryRow(`SELECT name, spec_url, COALESCE(operations, '') FROM openapi_specs WHERE id = ?`, id).Scan(&name, &specURL, &existingOps)
	if err != nil {
		return 0, false, fmt.Errorf("spec %q not found", id)
	}

	cfg := &config.OpenAPISpecConfig{Name: name, SpecURL: specURL}
	doc, err := openapi.LoadSpec(cfg)
	if err != nil {
		return 0, false, fmt.Errorf("Failed to load spec: %w", err)
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	if err := doc.Validate(loader.Context); err != nil {
		return 0, false, fmt.Errorf("Spec validation failed: %w", err)
	}

	discoveredPaths := []string{}
	for path, pathItem := range doc.Paths.Map() {
		// Skip .well-known paths (metadata, discovery endpoints)
		if strings.HasPrefix(path, "/.well-known/") {
			continue
		}
		if pathItem.Get != nil {
			discoveredPaths = append(discoveredPaths, "GET "+path)
		}
		if pathItem.Post != nil {
			discoveredPaths = append(discoveredPaths, "POST "+path)
		}
		if pathItem.Put != nil {
			discoveredPaths = append(discoveredPaths, "PUT "+path)
		}
		if pathItem.Delete != nil {
			discoveredPaths = append(discoveredPaths, "DELETE "+path)
		}
		if pathItem.Patch != nil {
			discoveredPaths = append(discoveredPaths, "PATCH "+path)
		}
		if pathItem.Head != nil {
			discoveredPaths = append(discoveredPaths, "HEAD "+path)
		}
		if pathItem.Options != nil {
			discoveredPaths = append(discoveredPaths, "OPTIONS "+path)
		}
	}

	opsCount := len(discoveredPaths)
	opsUpdated := false

	// Update operations column if forced (e.g. Sync) or if empty/unconfigured
	if forceUpdate || existingOps == "" || existingOps == "[]" || existingOps == "null" {
		if opsJSON, err := json.Marshal(discoveredPaths); err == nil {
			if _, err := h.store.DB.Exec(`UPDATE openapi_specs SET operations = ?, updated_at = datetime('now') WHERE id = ?`, string(opsJSON), id); err != nil {
				slog.Error("failed to update openapi operations", "id", id, "error", err)
			}
			opsUpdated = true
		}
	}

	return opsCount, opsUpdated, nil
}

func (h *OpenAPIHandler) ValidateSpec(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Spec ID is required")
		return
	}

	opsCount, _, err := h.validateSpecByID(r.Context(), id, true)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", errMsg)
		} else if strings.Contains(errMsg, "Failed to load") {
			model.WriteJSON(w, http.StatusOK, map[string]any{"status": "error", "error": errMsg})
		} else if strings.Contains(errMsg, "Spec validation failed") {
			model.WriteJSON(w, http.StatusOK, map[string]any{"status": "error", "error": errMsg})
		} else {
			model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", errMsg)
		}
		return
	}

	h.reloadProvider()
	model.WriteJSON(w, http.StatusOK, map[string]any{
		"status":           "success",
		"operations_count": opsCount,
	})
}

func ifStr(s string, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

func ifBool(p *bool, fallback bool) bool {
	if p != nil {
		return *p
	}
	return fallback
}

func ifInt(p *int, fallback int) int {
	if p != nil {
		return *p
	}
	return fallback
}

func openapiSpecVals(name, specURL, ops, authType, authValue, authKey string, timeoutMs int, enabled bool) map[string]any {
	v := map[string]any{
		"name":       name,
		"spec_url":   specURL,
		"operations": ops,
		"auth_type":  authType,
		"timeout_ms": timeoutMs,
		"enabled":    enabled,
	}
	if authValue != "" {
		v["auth_value"] = "***"
	}
	if authKey != "" {
		v["auth_key"] = "***"
	}
	return v
}

func nullToEmpty(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
