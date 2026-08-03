package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/auth"
	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/dbtest"
)

func setupBudgetTestStore(t *testing.T) *db.SQLiteStore {
	t.Helper()
	return dbtest.NewFile(t)
}

func createTestUser(t *testing.T, store *db.SQLiteStore) auth.User {
	user, err := store.CreateUser(auth.CreateUserRequest{
		Name:   "Test User",
		Email:  "test@example.com",
		Budget: 100.0,
	})
	require.NoError(t, err)
	return user
}

func createTestGroup(t *testing.T, store *db.SQLiteStore) auth.Group {
	group, err := store.CreateGroup(auth.CreateGroupRequest{
		Name:   "Test Group",
		Budget: 200.0,
	})
	require.NoError(t, err)
	return group
}

func TestBudgetHandlers_UserBudget(t *testing.T) {
	store := setupBudgetTestStore(t)

	user := createTestUser(t, store)
	s := &Server{store: store, cfg: &config.Config{}}

	r := chi.NewRouter()
	r.Get("/budget/user/{id}", s.handleUserBudget)

	req := httptest.NewRequest("GET", "/budget/user/1", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]any
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(user.ID), resp["user_id"])
	assert.Equal(t, user.Name, resp["user_name"])
	assert.Equal(t, 100.0, resp["monthly_budget"])
}

func TestBudgetHandlers_GroupBudget(t *testing.T) {
	store := setupBudgetTestStore(t)

	group := createTestGroup(t, store)
	s := &Server{store: store, cfg: &config.Config{}}

	r := chi.NewRouter()
	r.Get("/budget/group/{id}", s.handleGroupBudget)

	req := httptest.NewRequest("GET", "/budget/group/1", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]any
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(group.ID), resp["group_id"])
	assert.Equal(t, group.Name, resp["group_name"])
	assert.Equal(t, 200.0, resp["monthly_budget"])
}

func TestBudgetHandlers_SetUserBudget(t *testing.T) {
	store := setupBudgetTestStore(t)

	createTestUser(t, store)
	s := &Server{store: store, cfg: &config.Config{}}

	r := chi.NewRouter()
	r.Post("/budget/user/{id}", s.handleSetUserBudget)
	r.Get("/budget/user/{id}", s.handleUserBudget)

	body := bytes.NewBufferString(`{"monthly_budget": 150.00}`)
	req := httptest.NewRequest("POST", "/budget/user/1", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	// Verify by GET
	req2 := httptest.NewRequest("GET", "/budget/user/1", nil)
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)

	var resp map[string]any
	err := json.Unmarshal(rr2.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 150.0, resp["monthly_budget"])
}

func TestBudgetHandlers_SetGroupBudget(t *testing.T) {
	store := setupBudgetTestStore(t)

	createTestGroup(t, store)
	s := &Server{store: store, cfg: &config.Config{}}

	r := chi.NewRouter()
	r.Post("/budget/group/{id}", s.handleSetGroupBudget)
	r.Get("/budget/group/{id}", s.handleGroupBudget)

	body := bytes.NewBufferString(`{"monthly_budget": 300.00}`)
	req := httptest.NewRequest("POST", "/budget/group/1", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	// Verify by GET
	req2 := httptest.NewRequest("GET", "/budget/group/1", nil)
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)

	var resp map[string]any
	err := json.Unmarshal(rr2.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 300.0, resp["monthly_budget"])
}

func TestBudgetHandlers_UserNotFound(t *testing.T) {
	store := setupBudgetTestStore(t)

	s := &Server{store: store, cfg: &config.Config{}}

	r := chi.NewRouter()
	r.Get("/budget/user/{id}", s.handleUserBudget)

	req := httptest.NewRequest("GET", "/budget/user/999", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestBudgetHandlers_GroupNotFound(t *testing.T) {
	store := setupBudgetTestStore(t)

	s := &Server{store: store, cfg: &config.Config{}}

	r := chi.NewRouter()
	r.Get("/budget/group/{id}", s.handleGroupBudget)

	req := httptest.NewRequest("GET", "/budget/group/999", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}
