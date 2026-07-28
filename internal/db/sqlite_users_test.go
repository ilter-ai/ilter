package db

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/auth"
)

// ─── CreateUser ────────────────────────────────────────────────────────────

func TestCreateUser_Basic(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	user, err := ts.store.CreateUser(auth.CreateUserRequest{
		Name:     "Alice",
		Email:    "alice@example.com",
		Password: "securepass",
		Status:   "active",
		Budget:   100.0,
	})
	require.NoError(t, err)
	assert.NotZero(t, user.ID)
	assert.Equal(t, "Alice", user.Name)
	assert.Equal(t, "alice@example.com", user.Email)
	assert.Equal(t, "active", user.Status)
	assert.Equal(t, 100.0, user.Budget)
	assert.NotEmpty(t, user.PasswordHash)
	assert.NotZero(t, user.CreatedAt)
	assert.NotZero(t, user.UpdatedAt)
	assert.True(t, VerifyPassword("securepass", user.PasswordHash))
}

func TestCreateUser_DefaultsStatus(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	user, err := ts.store.CreateUser(auth.CreateUserRequest{
		Name:  "Bob",
		Email: "bob@example.com",
	})
	require.NoError(t, err)
	assert.Equal(t, "active", user.Status)
}

func TestCreateUser_NoPassword(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	user, err := ts.store.CreateUser(auth.CreateUserRequest{
		Name:   "NoPass",
		Email:  "nopass@example.com",
		Status: "active",
	})
	require.NoError(t, err)
	assert.Empty(t, user.PasswordHash)
}

func TestCreateUser_ZeroBudget(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	user, err := ts.store.CreateUser(auth.CreateUserRequest{
		Name:   "Zero",
		Email:  "zero@example.com",
		Budget: 0,
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, user.Budget)
}

func TestCreateUser_DuplicateEmail(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	_, err := ts.store.CreateUser(auth.CreateUserRequest{
		Name:  "Alice",
		Email: "alice@example.com",
	})
	require.NoError(t, err)

	_, err = ts.store.CreateUser(auth.CreateUserRequest{
		Name:  "Alice2",
		Email: "alice@example.com",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UNIQUE")
}

// ─── GetUser ───────────────────────────────────────────────────────────────

func TestGetUser_Found(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateUser(auth.CreateUserRequest{
		Name:   "Alice",
		Email:  "alice@example.com",
		Budget: 50.0,
	})
	require.NoError(t, err)

	got, err := ts.store.GetUser(created.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "Alice", got.Name)
	assert.Equal(t, "alice@example.com", got.Email)
	assert.Equal(t, 50.0, got.Budget)
	assert.NotZero(t, got.CreatedAt)
	assert.NotZero(t, got.UpdatedAt)
}

func TestGetUser_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	got, err := ts.store.GetUser(99999)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

// ─── GetUserByEmail ────────────────────────────────────────────────────────

func TestGetUserByEmail_Found(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateUser(auth.CreateUserRequest{
		Name:  "Alice",
		Email: "alice@example.com",
	})
	require.NoError(t, err)

	got, err := ts.store.GetUserByEmail("alice@example.com")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "Alice", got.Name)
	assert.Equal(t, "alice@example.com", got.Email)
}

func TestGetUserByEmail_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	got, err := ts.store.GetUserByEmail("nonexistent@example.com")
	assert.Nil(t, got)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

// ─── ListUsers ─────────────────────────────────────────────────────────────

func TestListUsers_Empty(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	users, err := ts.store.ListUsers()
	require.NoError(t, err)
	assert.Empty(t, users)
}

func TestListUsers_Multiple(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	u1, err := ts.store.CreateUser(auth.CreateUserRequest{Name: "Alice", Email: "alice@example.com"})
	require.NoError(t, err)
	u2, err := ts.store.CreateUser(auth.CreateUserRequest{Name: "Bob", Email: "bob@example.com"})
	require.NoError(t, err)
	u3, err := ts.store.CreateUser(auth.CreateUserRequest{Name: "Charlie", Email: "charlie@example.com"})
	require.NoError(t, err)

	users, err := ts.store.ListUsers()
	require.NoError(t, err)
	require.Len(t, users, 3)
	assert.Equal(t, u1.ID, users[0].ID)
	assert.Equal(t, u2.ID, users[1].ID)
	assert.Equal(t, u3.ID, users[2].ID)
}

// ─── GetUserBudget ─────────────────────────────────────────────────────────

func TestGetUserBudget_Found(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateUser(auth.CreateUserRequest{
		Name:   "Alice",
		Email:  "alice@example.com",
		Budget: 250.50,
	})
	require.NoError(t, err)

	budget, dailyLimit, err := ts.store.GetUserBudget(created.ID)
	require.NoError(t, err)
	assert.Equal(t, 250.50, budget)
	assert.Equal(t, 0.0, dailyLimit)
}

func TestGetUserBudget_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	budget, dailyLimit, err := ts.store.GetUserBudget(99999)
	require.NoError(t, err)
	assert.Equal(t, 0.0, budget)
	assert.Equal(t, 0.0, dailyLimit)
}

func TestGetUserBudget_ZeroBudget(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateUser(auth.CreateUserRequest{
		Name:   "ZeroBudget",
		Email:  "zero@example.com",
		Budget: 0,
	})
	require.NoError(t, err)

	budget, dailyLimit, err := ts.store.GetUserBudget(created.ID)
	require.NoError(t, err)
	assert.Equal(t, 0.0, budget)
	assert.Equal(t, 0.0, dailyLimit)
}

// ─── UpdateUser ────────────────────────────────────────────────────────────

func TestUpdateUser_Partial(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateUser(auth.CreateUserRequest{
		Name:   "Alice",
		Email:  "alice@example.com",
		Budget: 100.0,
	})
	require.NoError(t, err)

	newName := "Alice Updated"
	newBudget := 200.0
	newDailyLimit := 50.0
	updated, err := ts.store.UpdateUser(created.ID, auth.UpdateUserRequest{
		Name:       &newName,
		Budget:     &newBudget,
		DailyLimit: &newDailyLimit,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "Alice Updated", updated.Name)
	assert.Equal(t, "alice@example.com", updated.Email)
	assert.Equal(t, 200.0, updated.Budget)
	assert.Equal(t, 50.0, updated.DailyLimit)
	assert.True(t, updated.UpdatedAt.Equal(created.UpdatedAt) || updated.UpdatedAt.After(created.UpdatedAt))
}

func TestUpdateUser_Password(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateUser(auth.CreateUserRequest{
		Name:     "Alice",
		Email:    "alice@example.com",
		Password: "oldpass",
	})
	require.NoError(t, err)
	assert.True(t, VerifyPassword("oldpass", created.PasswordHash))

	newPass := "newpass123"
	updated, err := ts.store.UpdateUser(created.ID, auth.UpdateUserRequest{
		Password: &newPass,
	})
	require.NoError(t, err)
	assert.True(t, VerifyPassword("newpass123", updated.PasswordHash))
	assert.False(t, VerifyPassword("oldpass", updated.PasswordHash))
}

func TestUpdateUser_NoChanges(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateUser(auth.CreateUserRequest{
		Name:  "Alice",
		Email: "alice@example.com",
	})
	require.NoError(t, err)

	updated, err := ts.store.UpdateUser(created.ID, auth.UpdateUserRequest{})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, created.ID, updated.ID)
	assert.Equal(t, "Alice", updated.Name)
}

func TestUpdateUser_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	newName := "Ghost"
	updated, err := ts.store.UpdateUser(99999, auth.UpdateUserRequest{
		Name: &newName,
	})
	assert.Nil(t, updated)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestUpdateUser_ClearPassword(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateUser(auth.CreateUserRequest{
		Name:     "Alice",
		Email:    "alice@example.com",
		Password: "oldpass",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, created.PasswordHash)

	empty := ""
	updated, err := ts.store.UpdateUser(created.ID, auth.UpdateUserRequest{
		Password: &empty,
	})
	require.NoError(t, err)
	assert.Empty(t, updated.PasswordHash)
}

// ─── DeleteUser ────────────────────────────────────────────────────────────

func TestDeleteUser_Exists(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateUser(auth.CreateUserRequest{
		Name:  "Alice",
		Email: "alice@example.com",
	})
	require.NoError(t, err)

	err = ts.store.DeleteUser(created.ID)
	require.NoError(t, err)

	got, err := ts.store.GetUser(created.ID)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestDeleteUser_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	err := ts.store.DeleteUser(99999)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}
