package middleware

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/auth"
	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"

	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

type AuthMiddleware struct {
	cfg         config.AuthConfig
	store       *db.SQLiteStore
	mcpEndpoint string
}

func NewAuthMiddleware(cfg config.AuthConfig, store *db.SQLiteStore) *AuthMiddleware {
	return &AuthMiddleware{
		cfg:   cfg,
		store: store,
	}
}

// WithMCPEndpoint sets the MCP endpoint path for WWW-Authenticate header behavior.
// Must be called before use if MCP endpoints require OAuth discovery headers.
func (am *AuthMiddleware) WithMCPEndpoint(endpoint string) *AuthMiddleware {
	am.mcpEndpoint = endpoint
	return am
}

func (am *AuthMiddleware) loadKeyContext(ctx context.Context, vk *auth.APIKey) context.Context {
	ctx = context.WithValue(ctx, reqmeta.APIKeyBudgetContextKey, vk.MonthlyBudgetUSD)
	ctx = context.WithValue(ctx, reqmeta.APIKeyRateLimitContextKey, vk.RateLimitRPM)

	if vk.GroupID != nil {
		ctx = context.WithValue(ctx, reqmeta.GroupIDsContextKey, []int{*vk.GroupID})
		if groupBudget, _, gbErr := am.store.GetGroupBudget(*vk.GroupID); gbErr == nil && groupBudget > 0 {
			ctx = context.WithValue(ctx, reqmeta.GroupBudgetsContextKey, map[int]float64{*vk.GroupID: groupBudget})
		}
	}
	if vk.UserID != nil {
		ctx = context.WithValue(ctx, reqmeta.UserIDContextKey, *vk.UserID)
		if userBudget, _, ubErr := am.store.GetUserBudget(*vk.UserID); ubErr == nil && userBudget > 0 {
			ctx = context.WithValue(ctx, reqmeta.UserBudgetContextKey, userBudget)
		}
		groups, gErr := am.store.GetUserGroups(*vk.UserID)
		if gErr == nil && len(groups) > 0 {
			groupIDs := make([]int, len(groups))
			groupBudgets := make(map[int]float64, len(groups))
			for i, g := range groups {
				groupIDs[i] = g.ID
				if g.Budget > 0 {
					groupBudgets[g.ID] = g.Budget
				}
			}
			ctx = context.WithValue(ctx, reqmeta.GroupIDsContextKey, groupIDs)
			if len(groupBudgets) > 0 {
				ctx = context.WithValue(ctx, reqmeta.GroupBudgetsContextKey, groupBudgets)
			}
		}
	}
	return ctx
}

// resolveBillingKey replaces the synthetic dev/admin identity with the real API key
// from X-Ilter-Billing-Key-ID so internal callers are billed to a specific key.
func (am *AuthMiddleware) resolveBillingKey(ctx context.Context, r *http.Request) (context.Context, error) {
	keyID := r.Header.Get("X-Ilter-Billing-Key-ID")
	if keyID == "" {
		return ctx, nil
	}

	vk, err := am.store.GetAPIKey(keyID)
	if err != nil {
		return ctx, fmt.Errorf("invalid billing key %q: %w", keyID, err)
	}
	if !vk.Enabled {
		return ctx, fmt.Errorf("billing key %q is disabled", keyID)
	}

	slog.Debug(
		"billing key override applied",
		"original_key", ctx.Value(reqmeta.KeyIDContextKey),
		"billing_key", keyID,
	)
	ctx = context.WithValue(ctx, reqmeta.KeyIDContextKey, keyID)
	ctx = am.loadKeyContext(ctx, vk)
	return ctx, nil
}

func (am *AuthMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := extractBearerToken(r)
		if !ok {
			am.authError(w, r, http.StatusUnauthorized, "authentication_error", "Missing or invalid Authorization header")
			return
		}

		if am.isAdminKey(token) {
			ctx := context.WithValue(r.Context(), reqmeta.KeyIDContextKey, "admin")
			var billErr error
			ctx, billErr = am.resolveBillingKey(ctx, r)
			if billErr != nil {
				am.authError(w, r, http.StatusForbidden, "invalid_billing_key", billErr.Error())
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		vk, err := am.store.GetActiveKeyByHash(token)
		if err != nil {
			am.authError(w, r, http.StatusUnauthorized, "authentication_error", "Invalid API key")
			return
		}

		if !vk.Enabled {
			am.authError(w, r, http.StatusForbidden, "key_disabled", "Key is disabled")
			return
		}

		ctx := context.WithValue(r.Context(), reqmeta.KeyIDContextKey, vk.ID)
		ctx = am.loadKeyContext(ctx, vk)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// authError writes a JSON error response and adds WWW-Authenticate header
// for MCP endpoints to trigger OAuth discovery (MCP 2025-03-26 spec).
func (am *AuthMiddleware) authError(w http.ResponseWriter, r *http.Request, status int, errType, msg string) {
	if am.mcpEndpoint != "" && strings.HasPrefix(r.URL.Path, am.mcpEndpoint) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="ilter"`)
	}
	model.WriteJSONError(w, status, errType, msg)
}

// Falls back to x-api-key header for Anthropic SDK compatibility.
// Per RFC 7235 the auth-scheme is case-insensitive; accepts both "Bearer" and "bearer".
func extractBearerToken(r *http.Request) (string, bool) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		authHeader = r.Header.Get("x-api-key")
		if authHeader != "" {
			authHeader = "Bearer " + authHeader
		}
	}
	// RFC 7235 §2.1: auth-scheme is case-insensitive.
	prefix := strings.TrimSpace(authHeader)
	if len(prefix) < 7 {
		return "", false
	}
	if !strings.EqualFold(prefix[:7], "Bearer ") {
		return "", false
	}
	token := strings.TrimSpace(prefix[7:])
	if token == "" {
		return "", false
	}
	return token, true
}

func (am *AuthMiddleware) isAdminKey(token string) bool {
	return am.cfg.AdminKey != "" && subtle.ConstantTimeCompare([]byte(token), []byte(am.cfg.AdminKey)) == 1
}
