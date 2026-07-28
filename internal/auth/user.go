package auth

import "time"

type User struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	Status       string    `json:"status"` // active, suspended, disabled
	PasswordHash string    `json:"-"`
	Metadata     string    `json:"metadata,omitempty"`
	Budget       float64   `json:"budget,omitempty"`
	DailyLimit   float64   `json:"daily_limit,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CreateUserRequest struct {
	Name     string  `json:"name"`
	Email    string  `json:"email"`
	Password string  `json:"password,omitempty"` // plaintext password; storage hashes with Argon2id
	Status   string  `json:"status,omitempty"`
	Budget   float64 `json:"budget,omitempty"`
}

// Only non-nil fields will be updated.
type UpdateUserRequest struct {
	Name       *string  `json:"name,omitempty"`
	Email      *string  `json:"email,omitempty"`
	Password   *string  `json:"password,omitempty"` // optional new password; empty string clears it
	Status     *string  `json:"status,omitempty"`
	Budget     *float64 `json:"budget,omitempty"`
	DailyLimit *float64 `json:"daily_limit,omitempty"`
}
