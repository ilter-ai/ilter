package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ilter-ai/ilter/internal/auth"
	"github.com/ilter-ai/ilter/internal/db/sqlc"
	"github.com/ilter-ai/ilter/internal/platform/crypto"
)

// userFromSQLC converts a sqlc.User to an auth.User.
func userFromSQLC(u sqlc.User) (*auth.User, error) {
	user := &auth.User{
		ID:        int(u.ID),
		Name:      u.Name,
		Email:     u.Email,
		Status:    u.Status,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
	if u.PasswordHash != nil {
		user.PasswordHash = *u.PasswordHash
	}
	if u.Metadata != nil {
		user.Metadata = *u.Metadata
	}
	if u.Budget != nil {
		user.Budget = *u.Budget
	}
	if u.DailyLimit != nil {
		user.DailyLimit = *u.DailyLimit
	}
	return user, nil
}

// CreateUser inserts a new user and returns the created user with timestamps.
func (s *SQLiteStore) CreateUser(req auth.CreateUserRequest) (auth.User, error) {
	status := req.Status
	if status == "" {
		status = "active"
	}

	passwordHash := ""
	if req.Password != "" {
		hashHex, salt, err := crypto.HashToken(req.Password, "argon2")
		if err != nil {
			return auth.User{}, fmt.Errorf("failed to hash password: %w", err)
		}
		passwordHash = salt + ":" + hashHex
	}

	var pwdHash *string
	if passwordHash != "" {
		pwdHash = &passwordHash
	}
	budget := req.Budget

	res, err := s.queries.CreateUser(context.Background(), sqlc.CreateUserParams{
		Name:         req.Name,
		Email:        req.Email,
		Status:       status,
		Budget:       &budget,
		PasswordHash: pwdHash,
	})
	if err != nil {
		return auth.User{}, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return auth.User{}, err
	}

	sqlcUser, err := s.queries.GetUser(context.Background(), id)
	if err != nil {
		return auth.User{}, err
	}

	u, err := userFromSQLC(sqlcUser)
	if err != nil {
		return auth.User{}, err
	}
	return *u, nil
}

// GetUser retrieves a user by their primary key.
// Returns nil, sql.ErrNoRows if the user does not exist.
func (s *SQLiteStore) GetUser(id int) (*auth.User, error) {
	sqlcUser, err := s.queries.GetUser(context.Background(), int64(id))
	if err != nil {
		return nil, err
	}
	return userFromSQLC(sqlcUser)
}

// GetUserByEmail retrieves a user by their email address.
// Returns nil, sql.ErrNoRows if not found.
func (s *SQLiteStore) GetUserByEmail(email string) (*auth.User, error) {
	sqlcUser, err := s.queries.GetUserByEmail(context.Background(), email)
	if err != nil {
		return nil, err
	}
	return userFromSQLC(sqlcUser)
}

// ListUsers returns all users ordered by id ascending.
func (s *SQLiteStore) ListUsers() ([]auth.User, error) {
	sqlcUsers, err := s.queries.ListUsers(context.Background())
	if err != nil {
		return nil, err
	}
	users := make([]auth.User, len(sqlcUsers))
	for i, u := range sqlcUsers {
		converted, err := userFromSQLC(u)
		if err != nil {
			return nil, err
		}
		users[i] = *converted
	}
	return users, nil
}

// GetUserBudget retrieves just the budget and daily_limit for a user.
// Returns (0, 0, nil) if the user does not exist.
func (s *SQLiteStore) GetUserBudget(id int) (budget float64, dailyLimit float64, err error) {
	row, err := s.queries.GetUserBudget(context.Background(), int64(id))
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	if row.Budget != nil {
		budget = *row.Budget
	}
	if row.DailyLimit != nil {
		dailyLimit = *row.DailyLimit
	}
	return budget, dailyLimit, nil
}

// UpdateUser applies partial updates to a user. Only non-nil fields in the
// request are updated. Returns the updated user or sql.ErrNoRows if not found.
func (s *SQLiteStore) UpdateUser(id int, req auth.UpdateUserRequest) (*auth.User, error) {
	var sets []string
	var args []any

	if req.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *req.Name)
	}
	if req.Email != nil {
		sets = append(sets, "email = ?")
		args = append(args, *req.Email)
	}
	if req.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *req.Status)
	}
	if req.Budget != nil {
		sets = append(sets, "budget = ?")
		args = append(args, *req.Budget)
	}
	if req.DailyLimit != nil {
		sets = append(sets, "daily_limit = ?")
		args = append(args, *req.DailyLimit)
	}
	if req.Password != nil {
		passwordHash := ""
		if *req.Password != "" {
			hashHex, salt, err := crypto.HashToken(*req.Password, "argon2")
			if err != nil {
				return nil, fmt.Errorf("failed to hash password: %w", err)
			}
			passwordHash = salt + ":" + hashHex
		}
		sets = append(sets, "password_hash = ?")
		args = append(args, passwordHash)
	}

	if len(sets) == 0 {
		// Nothing to update — still fetch and return the current user.
		return s.GetUser(id)
	}

	sets = append(sets, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)

	query := fmt.Sprintf("UPDATE users SET %s WHERE id = ?", strings.Join(sets, ", "))
	res, err := s.DB.Exec(query, args...)
	if err != nil {
		return nil, err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, sql.ErrNoRows
	}

	return s.GetUser(id)
}

// DeleteUser deletes a user by their primary key.
// Returns sql.ErrNoRows if the user does not exist.
func (s *SQLiteStore) DeleteUser(id int) error {
	n, err := s.queries.DeleteUser(context.Background(), int64(id))
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// VerifyPassword compares a plaintext password against a stored salt:hash value.
// The storedHash should be in "salt:hashHex" format as produced by CreateUser/UpdateUser.
// Returns false if storedHash is empty or malformed.
func VerifyPassword(plaintext, storedHash string) bool {
	if storedHash == "" {
		return false
	}
	parts := strings.SplitN(storedHash, ":", 2)
	if len(parts) != 2 {
		return false
	}
	salt, hashHex := parts[0], parts[1]
	algo := "argon2"
	if salt == "sha256" {
		algo = "sha256"
	}
	computed, err := crypto.HashTokenWithSalt(plaintext, salt, algo)
	if err != nil {
		return false
	}
	return computed == hashHex
}
