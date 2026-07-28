package seed

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/ilter-ai/ilter/internal/auth"
	dbpkg "github.com/ilter-ai/ilter/internal/db"
)

// EnsureAdminAccount creates the "admin" group, an admin user, and an admin
// API key on first run. It is idempotent: if the admin group already exists
// it does nothing and returns created=false so re-running `ilter init` never
// rotates credentials or duplicates the account.
func EnsureAdminAccount(store *dbpkg.SQLiteStore) (email, password, apiKeyToken string, created bool, err error) {
	if _, err := store.GetGroupByName("admin"); err == nil {
		return "", "", "", false, nil
	} else if err != sql.ErrNoRows {
		return "", "", "", false, fmt.Errorf("check admin group: %w", err)
	}

	group, err := store.CreateGroup(auth.CreateGroupRequest{
		Name:        "admin",
		Description: "Administrators",
	})
	if err != nil {
		return "", "", "", false, fmt.Errorf("create admin group: %w", err)
	}

	password, err = randomHex(24)
	if err != nil {
		return "", "", "", false, fmt.Errorf("generate admin password: %w", err)
	}

	email = "admin@localhost"
	user, err := store.CreateUser(auth.CreateUserRequest{
		Name:     "admin",
		Email:    email,
		Password: password,
		Status:   "active",
	})
	if err != nil {
		return "", "", "", false, fmt.Errorf("create admin user: %w", err)
	}

	if err := store.AddUserToGroup(user.ID, group.ID, "admin"); err != nil {
		return "", "", "", false, fmt.Errorf("add admin user to group: %w", err)
	}

	groupID, userID := group.ID, user.ID
	_, apiKeyToken, err = store.CreateAPIKey(
		"admin", &groupID, &userID,
		0, 0, // unlimited budget
		0, 0, // unlimited rate limit
		nil, nil, nil,
	)
	if err != nil {
		return "", "", "", false, fmt.Errorf("create admin api key: %w", err)
	}

	slog.Info("admin account created", "email", email, "group", group.Name)

	return email, password, apiKeyToken, true, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
