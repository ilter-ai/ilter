package dashboard

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	iltauth "github.com/ilter-ai/ilter/internal/auth"
	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

// ---------------------------------------------------------------------------
// Budget spent helpers
// ---------------------------------------------------------------------------

func (s *Server) keyMonthlySpent(keyID string) (float64, error) {
	now := time.Now().UTC()
	firstDay := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var total sql.NullFloat64
	err := s.store.DB.QueryRow(
		`SELECT COALESCE(SUM(ku.cost_usd), 0)
		 FROM key_usage ku
		 WHERE ku.key_id = ? AND ku.date >= ? AND ku.date <= ?`,
		keyID, firstDay.Format("2006-01-02"), now.Format("2006-01-02"),
	).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Float64, nil
}

func (s *Server) keyDailySpent(keyID string) (float64, error) {
	today := time.Now().UTC().Format("2006-01-02")
	var total sql.NullFloat64
	err := s.store.DB.QueryRow(
		`SELECT COALESCE(SUM(ku.cost_usd), 0)
		 FROM key_usage ku
		 WHERE ku.key_id = ? AND ku.date = ?`,
		keyID, today,
	).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Float64, nil
}

func (s *Server) userMonthlySpent(userID int) (float64, error) {
	now := time.Now().UTC()
	firstDay := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var total sql.NullFloat64
	err := s.store.DB.QueryRow(
		`SELECT COALESCE(SUM(ku.cost_usd), 0)
		 FROM key_usage ku
		 JOIN api_keys ak ON ku.key_id = ak.id
		 WHERE ak.user_id = ? AND ku.date >= ? AND ku.date <= ?`,
		userID, firstDay.Format("2006-01-02"), now.Format("2006-01-02"),
	).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Float64, nil
}

func (s *Server) groupMonthlySpent(groupID int) (float64, error) {
	now := time.Now().UTC()
	firstDay := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var total sql.NullFloat64
	err := s.store.DB.QueryRow(
		`SELECT COALESCE(SUM(ku.cost_usd), 0)
		 FROM key_usage ku
		 JOIN api_keys ak ON ku.key_id = ak.id
		 WHERE ak.group_id = ? AND ku.date >= ? AND ku.date <= ?`,
		groupID, firstDay.Format("2006-01-02"), now.Format("2006-01-02"),
	).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Float64, nil
}

func (s *Server) userDailySpent(userID int) (float64, error) {
	today := time.Now().UTC().Format("2006-01-02")
	var total sql.NullFloat64
	err := s.store.DB.QueryRow(
		`SELECT COALESCE(SUM(ku.cost_usd), 0)
		 FROM key_usage ku
		 JOIN api_keys ak ON ku.key_id = ak.id
		 WHERE ak.user_id = ? AND ku.date = ?`,
		userID, today,
	).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Float64, nil
}

func (s *Server) groupDailySpent(groupID int) (float64, error) {
	today := time.Now().UTC().Format("2006-01-02")
	var total sql.NullFloat64
	err := s.store.DB.QueryRow(
		`SELECT COALESCE(SUM(ku.cost_usd), 0)
		 FROM key_usage ku
		 JOIN api_keys ak ON ku.key_id = ak.id
		 WHERE ak.group_id = ? AND ku.date = ?`,
		groupID, today,
	).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Float64, nil
}

// ---------------------------------------------------------------------------
// Budget handler endpoints
// ---------------------------------------------------------------------------

func (s *Server) handleBudgetSummary(w http.ResponseWriter, _ *http.Request) {
	model.WriteJSON(w, http.StatusOK, map[string]any{
		"enabled":               config.IsEnabled(s.configCache, "budget"),
		"default_monthly_limit": 0,
		"default_daily_limit":   0,
		"alert_threshold":       0,
		"total_budget":          0,
		"total_spent":           0,
		"keys":                  []any{},
		"chart":                 []any{},
	})
}

func (s *Server) handleKeyBudget(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_id", "key id is required")
		return
	}

	key, err := s.store.GetAPIKey(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "API key not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	monthlySpent, _ := s.keyMonthlySpent(key.ID)
	dailySpent, _ := s.keyDailySpent(key.ID)

	status := "ok"
	if key.MonthlyBudgetUSD > 0 && monthlySpent >= key.MonthlyBudgetUSD {
		status = "depleted"
	} else if key.MonthlyBudgetUSD > 0 && monthlySpent >= key.MonthlyBudgetUSD*0.95 {
		status = "critical"
	} else if key.MonthlyBudgetUSD > 0 && monthlySpent >= key.MonthlyBudgetUSD*0.9 {
		status = "warning"
	}

	var prefix string
	if len(key.ID) >= 12 {
		prefix = key.ID[:12]
	} else {
		prefix = key.ID
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"id":                    key.ID,
		"key_prefix":            prefix,
		"key_name":              key.Name,
		"monthly_budget_usd":    key.MonthlyBudgetUSD,
		"monthly_budget_tokens": key.MonthlyBudgetTokens,
		"monthly_spent":         monthlySpent,
		"daily_spent":           dailySpent,
		"daily_limit":           0,
		"status":                status,
	})
}

func (s *Server) handleSetKeyBudget(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_id", "key id is required")
		return
	}

	var req struct {
		MonthlyBudgetUSD    *float64 `json:"monthly_budget_usd"`
		MonthlyBudgetTokens *int64   `json:"monthly_budget_tokens"`
	}
	if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return
	}

	if req.MonthlyBudgetUSD != nil && *req.MonthlyBudgetUSD < 0 {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_value", "monthly_budget_usd must be non-negative")
		return
	}
	if req.MonthlyBudgetTokens != nil && *req.MonthlyBudgetTokens < 0 {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_value", "monthly_budget_tokens must be non-negative")
		return
	}

	if err := s.store.SetKeyBudget(id, req.MonthlyBudgetUSD, req.MonthlyBudgetTokens); err != nil {
		if strings.Contains(err.Error(), "not found") {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	key, err := s.store.GetAPIKey(id)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	monthlySpent, _ := s.keyMonthlySpent(key.ID)
	dailySpent, _ := s.keyDailySpent(key.ID)

	var prefix string
	if len(key.ID) >= 12 {
		prefix = key.ID[:12]
	} else {
		prefix = key.ID
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"id":                    key.ID,
		"key_prefix":            prefix,
		"key_name":              key.Name,
		"monthly_budget_usd":    key.MonthlyBudgetUSD,
		"monthly_budget_tokens": key.MonthlyBudgetTokens,
		"monthly_spent":         monthlySpent,
		"daily_spent":           dailySpent,
		"daily_limit":           0,
		"status":                "ok",
	})
}

func (s *Server) handleUserBudget(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_id", "user id must be an integer")
		return
	}

	user, err := s.store.GetUser(id)
	if err != nil {
		if err == sql.ErrNoRows {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	monthlySpent, _ := s.userMonthlySpent(user.ID)
	dailySpent, _ := s.userDailySpent(user.ID)

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"user_id":        user.ID,
		"user_name":      user.Name,
		"monthly_budget": user.Budget,
		"monthly_spent":  monthlySpent,
		"daily_limit":    user.DailyLimit,
		"daily_spent":    dailySpent,
		"status":         "ok",
	})
}

func (s *Server) handleSetUserBudget(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_id", "user id must be an integer")
		return
	}

	var req struct {
		MonthlyBudget *float64 `json:"monthly_budget"`
		DailyLimit    *float64 `json:"daily_limit"`
	}
	if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return
	}

	updateReq := iltauth.UpdateUserRequest{
		Budget:     req.MonthlyBudget,
		DailyLimit: req.DailyLimit,
	}
	user, err := s.store.UpdateUser(id, updateReq)
	if err != nil {
		if err == sql.ErrNoRows {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	monthlySpent, _ := s.userMonthlySpent(user.ID)
	dailySpent, _ := s.userDailySpent(user.ID)

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"user_id":        user.ID,
		"user_name":      user.Name,
		"monthly_budget": user.Budget,
		"monthly_spent":  monthlySpent,
		"daily_limit":    user.DailyLimit,
		"daily_spent":    dailySpent,
		"status":         "ok",
	})
}

func (s *Server) handleGroupBudget(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_id", "group id must be an integer")
		return
	}

	group, err := s.store.GetGroup(id)
	if err != nil {
		if err == sql.ErrNoRows {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "group not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	monthlySpent, _ := s.groupMonthlySpent(group.ID)
	dailySpent, _ := s.groupDailySpent(group.ID)

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"group_id":       group.ID,
		"group_name":     group.Name,
		"monthly_budget": group.Budget,
		"monthly_spent":  monthlySpent,
		"daily_limit":    group.DailyLimit,
		"daily_spent":    dailySpent,
		"status":         "ok",
	})
}

func (s *Server) handleSetGroupBudget(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_id", "group id must be an integer")
		return
	}

	var req struct {
		MonthlyBudget *float64 `json:"monthly_budget"`
		DailyLimit    *float64 `json:"daily_limit"`
	}
	if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return
	}

	updateReq := iltauth.UpdateGroupRequest{
		Budget:     req.MonthlyBudget,
		DailyLimit: req.DailyLimit,
	}
	group, err := s.store.UpdateGroup(id, updateReq)
	if err != nil {
		if err == sql.ErrNoRows {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "group not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	monthlySpent, _ := s.groupMonthlySpent(group.ID)
	dailySpent, _ := s.groupDailySpent(group.ID)

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"group_id":       group.ID,
		"group_name":     group.Name,
		"monthly_budget": group.Budget,
		"monthly_spent":  monthlySpent,
		"daily_limit":    group.DailyLimit,
		"daily_spent":    dailySpent,
		"status":         "ok",
	})
}
