package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ilter-ai/ilter/internal/auth"
	"github.com/ilter-ai/ilter/internal/db/sqlc"
)

// CreateGroup inserts a new group and returns the created group with timestamps.
func (s *SQLiteStore) CreateGroup(req auth.CreateGroupRequest) (auth.Group, error) {
	ctx := context.Background()
	desc := req.Description
	budget := req.Budget

	res, err := s.queries.CreateGroup(ctx, sqlc.CreateGroupParams{
		Name:        req.Name,
		Description: &desc,
		Budget:      &budget,
	})
	if err != nil {
		return auth.Group{}, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return auth.Group{}, err
	}

	g, err := s.queries.GetGroup(ctx, id)
	if err != nil {
		return auth.Group{}, err
	}
	return groupFromSQLC(g)
}

// GetGroup retrieves a group by its primary key.
// Returns nil, sql.ErrNoRows if the group does not exist.
func (s *SQLiteStore) GetGroup(id int) (*auth.Group, error) {
	ctx := context.Background()
	g, err := s.queries.GetGroup(ctx, int64(id))
	if err != nil {
		return nil, err
	}
	group, err := groupFromSQLC(g)
	if err != nil {
		return nil, err
	}
	return &group, nil
}

// GetGroupByName retrieves a group by its name.
// Returns nil, sql.ErrNoRows if not found.
func (s *SQLiteStore) GetGroupByName(name string) (*auth.Group, error) {
	ctx := context.Background()
	g, err := s.queries.GetGroupByName(ctx, name)
	if err != nil {
		return nil, err
	}
	group, err := groupFromSQLC(g)
	if err != nil {
		return nil, err
	}
	return &group, nil
}

// ListGroups returns all groups ordered by id ascending.
func (s *SQLiteStore) ListGroups() ([]auth.Group, error) {
	ctx := context.Background()
	groups, err := s.queries.ListGroups(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]auth.Group, len(groups))
	for i, g := range groups {
		result[i], err = groupFromSQLC(g)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// UpdateGroup applies partial updates to a group. Only non-nil fields in the
// request are updated. Returns the updated group or sql.ErrNoRows if not found.
// (kept as hand-written dynamic fmt.Sprintf SET builder)
func (s *SQLiteStore) UpdateGroup(id int, req auth.UpdateGroupRequest) (*auth.Group, error) {
	var sets []string
	var args []any

	if req.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *req.Name)
	}
	if req.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *req.Description)
	}
	if req.Budget != nil {
		sets = append(sets, "budget = ?")
		args = append(args, *req.Budget)
	}
	if req.DailyLimit != nil {
		sets = append(sets, "daily_limit = ?")
		args = append(args, *req.DailyLimit)
	}

	if len(sets) == 0 {
		return s.GetGroup(id)
	}

	sets = append(sets, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)

	query := fmt.Sprintf("UPDATE groups SET %s WHERE id = ?", strings.Join(sets, ", "))
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

	return s.GetGroup(id)
}

// GetGroupBudget retrieves just the budget and daily_limit for a group.
// Returns (0, 0, nil) if the group does not exist.
func (s *SQLiteStore) GetGroupBudget(id int) (budget float64, dailyLimit float64, err error) {
	ctx := context.Background()
	row, err := s.queries.GetGroupBudget(ctx, int64(id))
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

// DeleteGroup deletes a group by its primary key.
// Returns sql.ErrNoRows if the group does not exist.
func (s *SQLiteStore) DeleteGroup(id int) error {
	ctx := context.Background()
	n, err := s.queries.DeleteGroup(ctx, int64(id))
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AddUserToGroup adds a user to a group with an optional role.
func (s *SQLiteStore) AddUserToGroup(userID, groupID int, role string) error {
	if role == "" {
		role = "member"
	}
	ctx := context.Background()
	return s.queries.AddUserToGroup(ctx, sqlc.AddUserToGroupParams{
		UserID:  int64(userID),
		GroupID: int64(groupID),
		Role:    role,
	})
}

// RemoveUserFromGroup removes a user from a group.
// Returns sql.ErrNoRows if the membership doesn't exist.
func (s *SQLiteStore) RemoveUserFromGroup(userID, groupID int) error {
	ctx := context.Background()
	n, err := s.queries.RemoveUserFromGroup(ctx, sqlc.RemoveUserFromGroupParams{
		UserID:  int64(userID),
		GroupID: int64(groupID),
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetGroupUsers returns all users in a group.
func (s *SQLiteStore) GetGroupUsers(groupID int) ([]auth.User, error) {
	ctx := context.Background()
	users, err := s.queries.GetGroupUsers(ctx, int64(groupID))
	if err != nil {
		return nil, err
	}
	result := make([]auth.User, len(users))
	for i, u := range users {
		au, err := userFromSQLC(u)
		if err != nil {
			return nil, err
		}
		result[i] = *au
	}
	return result, nil
}

// GetUserGroups returns all groups a user belongs to.
func (s *SQLiteStore) GetUserGroups(userID int) ([]auth.Group, error) {
	ctx := context.Background()
	groups, err := s.queries.GetUserGroups(ctx, int64(userID))
	if err != nil {
		return nil, err
	}
	result := make([]auth.Group, len(groups))
	for i, g := range groups {
		result[i], err = groupFromSQLC(g)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// groupFromSQLC converts a sqlc.Group to an auth.Group.
func groupFromSQLC(g sqlc.Group) (auth.Group, error) {
	var description, metadata string
	if g.Description != nil {
		description = *g.Description
	}
	if g.Metadata != nil {
		metadata = *g.Metadata
	}
	var budget, dailyLimit float64
	if g.Budget != nil {
		budget = *g.Budget
	}
	if g.DailyLimit != nil {
		dailyLimit = *g.DailyLimit
	}
	return auth.Group{
		ID:          int(g.ID),
		Name:        g.Name,
		Description: description,
		Metadata:    metadata,
		Budget:      budget,
		DailyLimit:  dailyLimit,
		CreatedAt:   g.CreatedAt,
		UpdatedAt:   g.UpdatedAt,
	}, nil
}
