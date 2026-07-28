package auth

import "time"

type Group struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Metadata    string    `json:"metadata,omitempty"`
	Budget      float64   `json:"budget,omitempty"`
	DailyLimit  float64   `json:"daily_limit,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateGroupRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Budget      float64 `json:"budget,omitempty"`
}

// Only non-nil fields will be updated.
type UpdateGroupRequest struct {
	Name        *string  `json:"name,omitempty"`
	Description *string  `json:"description,omitempty"`
	Budget      *float64 `json:"budget,omitempty"`
	DailyLimit  *float64 `json:"daily_limit,omitempty"`
}

type UserGroupMembership struct {
	UserID    int       `json:"user_id"`
	GroupID   int       `json:"group_id"`
	Role      string    `json:"role"` // member, admin
	CreatedAt time.Time `json:"created_at"`
	UserName  string    `json:"user_name,omitempty"`  // joined from users table
	GroupName string    `json:"group_name,omitempty"` // joined from groups table
}
