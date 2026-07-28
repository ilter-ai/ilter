package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
)

// Handler handles authentication-related endpoints.
type Handler struct {
	store *db.SQLiteStore
	cfg   *config.Config
}

func NewAuthHandler(store *db.SQLiteStore, cfg *config.Config) *Handler {
	return &Handler{store: store, cfg: cfg}
}

// isSecure reports whether the request uses TLS or a trusted proxy header.
func isSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

type loginRequest struct {
	Token string `json:"token"`
}

type loginResponse struct {
	Status string `json:"status"`
}

func (h *Handler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid JSON body")
		return
	}

	if req.Token == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "token is required")
		return
	}

	dashToken := h.cfg.Dashboard.AuthToken
	adminKey := h.cfg.Auth.AdminKey
	valid := (dashToken != "" && req.Token == dashToken) ||
		(adminKey != "" && req.Token == adminKey) ||
		(h.store != nil && h.store.IsAdminAPIKey(req.Token))

	if !valid {
		model.WriteJSONError(w, http.StatusUnauthorized, "authentication_error", "Invalid token")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    req.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 30,
	})
	model.WriteJSON(w, http.StatusOK, loginResponse{Status: "ok"})
}

type userLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userClaims struct {
	UserID int    `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	jwt.RegisteredClaims
}

func (h *Handler) HandleUserLogin(w http.ResponseWriter, r *http.Request) {
	var req userLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid JSON body")
		return
	}

	if req.Email == "" || req.Password == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Email and password are required")
		return
	}

	user, err := h.store.GetUserByEmail(req.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			model.WriteJSONError(w, http.StatusUnauthorized, "authentication_error", "Invalid email or password")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to lookup user")
		return
	}

	// Reject inactive users without revealing user existence.
	if user.Status != "" && user.Status != "active" {
		model.WriteJSONError(w, http.StatusUnauthorized, "authentication_error", "Invalid email or password")
		return
	}

	if !db.VerifyPassword(req.Password, user.PasswordHash) {
		model.WriteJSONError(w, http.StatusUnauthorized, "authentication_error", "Invalid email or password")
		return
	}

	jwtSecret := h.cfg.Dashboard.UserAuthJWTSecret
	if jwtSecret == "" {
		jwtSecret = "user-auth-dev-secret"
	}

	now := time.Now()
	claims := userClaims{
		UserID: user.ID,
		Email:  user.Email,
		Name:   user.Name,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "ilter-dashboard",
			Subject:   "user",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to generate token")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "user_token",
		Value:    tokenString,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400, // 24 hours
	})

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"token": tokenString,
		"user": map[string]any{
			"id":    user.ID,
			"email": user.Email,
			"name":  user.Name,
		},
	})
}
