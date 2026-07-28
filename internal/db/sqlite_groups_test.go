package db

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/auth"
)

func TestCreateGroup(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	req := auth.CreateGroupRequest{
		Name:        "test-group",
		Description: "a test group",
		Budget:      100.0,
	}
	g, err := ts.store.CreateGroup(req)
	require.NoError(t, err)
	assert.NotZero(t, g.ID)
	assert.Equal(t, "test-group", g.Name)
	assert.Equal(t, "a test group", g.Description)
	assert.Equal(t, 100.0, g.Budget)
	assert.Equal(t, 0.0, g.DailyLimit)
	assert.NotZero(t, g.CreatedAt)
	assert.NotZero(t, g.UpdatedAt)
}

func TestCreateGroup_EmptyDescription(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	req := auth.CreateGroupRequest{
		Name:   "no-desc-group",
		Budget: 50.0,
	}
	g, err := ts.store.CreateGroup(req)
	require.NoError(t, err)
	assert.Equal(t, "no-desc-group", g.Name)
	assert.Empty(t, g.Description)
}

func TestCreateGroup_ZeroBudget(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	req := auth.CreateGroupRequest{
		Name: "zero-budget-group",
	}
	g, err := ts.store.CreateGroup(req)
	require.NoError(t, err)
	assert.Equal(t, 0.0, g.Budget)
}

func TestGetGroup_Found(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "find-me", Budget: 50})
	require.NoError(t, err)

	got, err := ts.store.GetGroup(created.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "find-me", got.Name)
	assert.Equal(t, 50.0, got.Budget)
}

func TestGetGroup_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	got, err := ts.store.GetGroup(99999)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestGetGroupByName_Found(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	_, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "by-name", Budget: 25})
	require.NoError(t, err)

	got, err := ts.store.GetGroupByName("by-name")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "by-name", got.Name)
	assert.Equal(t, 25.0, got.Budget)
}

func TestGetGroupByName_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	got, err := ts.store.GetGroupByName("nonexistent")
	assert.Nil(t, got)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestListGroups(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	// No groups initially.
	groups, err := ts.store.ListGroups()
	require.NoError(t, err)
	assert.Empty(t, groups)

	// Create three groups.
	names := []string{"alpha", "beta", "gamma"}
	for _, n := range names {
		_, createErr := ts.store.CreateGroup(auth.CreateGroupRequest{Name: n, Budget: 10})
		require.NoError(t, createErr)
	}

	groups, err = ts.store.ListGroups()
	require.NoError(t, err)
	require.Len(t, groups, 3)
	assert.Equal(t, "alpha", groups[0].Name)
	assert.Equal(t, "beta", groups[1].Name)
	assert.Equal(t, "gamma", groups[2].Name)
}

func TestUpdateGroup_Partial(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "before", Budget: 10})
	require.NoError(t, err)

	newName := "after"
	updated, err := ts.store.UpdateGroup(created.ID, auth.UpdateGroupRequest{Name: &newName})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "after", updated.Name)
	assert.Equal(t, 10.0, updated.Budget) // unchanged
	assert.Equal(t, created.Description, updated.Description)
}

func TestUpdateGroup_Full(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "original", Budget: 10})
	require.NoError(t, err)

	newName := "updated-name"
	newDesc := "updated desc"
	newBudget := 200.0
	newDailyLimit := 50.0

	updated, err := ts.store.UpdateGroup(created.ID, auth.UpdateGroupRequest{
		Name:        &newName,
		Description: &newDesc,
		Budget:      &newBudget,
		DailyLimit:  &newDailyLimit,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "updated-name", updated.Name)
	assert.Equal(t, "updated desc", updated.Description)
	assert.Equal(t, 200.0, updated.Budget)
	assert.Equal(t, 50.0, updated.DailyLimit)
}

func TestUpdateGroup_NoChanges(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "same", Budget: 10})
	require.NoError(t, err)

	// Empty request — should return current group unchanged.
	updated, err := ts.store.UpdateGroup(created.ID, auth.UpdateGroupRequest{})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "same", updated.Name)
}

func TestUpdateGroup_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	newName := "nope"
	updated, err := ts.store.UpdateGroup(99999, auth.UpdateGroupRequest{Name: &newName})
	assert.Nil(t, updated)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestDeleteGroup_Exists(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "delete-me", Budget: 5})
	require.NoError(t, err)

	err = ts.store.DeleteGroup(created.ID)
	require.NoError(t, err)

	// Verify it's gone.
	got, err := ts.store.GetGroup(created.ID)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestDeleteGroup_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	err := ts.store.DeleteGroup(99999)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestGetGroupBudget(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "budget-test", Budget: 75.5})
	require.NoError(t, err)

	budget, dailyLimit, err := ts.store.GetGroupBudget(created.ID)
	require.NoError(t, err)
	assert.Equal(t, 75.5, budget)
	assert.Equal(t, 0.0, dailyLimit)
}

func TestGetGroupBudget_AfterUpdate(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	created, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "budget-update", Budget: 10})
	require.NoError(t, err)

	newBudget := 500.0
	_, err = ts.store.UpdateGroup(created.ID, auth.UpdateGroupRequest{Budget: &newBudget})
	require.NoError(t, err)

	budget, _, err := ts.store.GetGroupBudget(created.ID)
	require.NoError(t, err)
	assert.Equal(t, 500.0, budget)
}

func TestGetGroupBudget_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	budget, dailyLimit, err := ts.store.GetGroupBudget(99999)
	require.NoError(t, err)
	assert.Equal(t, 0.0, budget)
	assert.Equal(t, 0.0, dailyLimit)
}

func TestAddUserToGroup(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	user, err := ts.store.CreateUser(auth.CreateUserRequest{Name: "member-user", Email: "member@test.com"})
	require.NoError(t, err)

	group, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "member-group", Budget: 10})
	require.NoError(t, err)

	err = ts.store.AddUserToGroup(user.ID, group.ID, "member")
	require.NoError(t, err)
}

func TestAddUserToGroup_DefaultRole(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	user, err := ts.store.CreateUser(auth.CreateUserRequest{Name: "default-role", Email: "default@test.com"})
	require.NoError(t, err)

	group, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "default-role-group", Budget: 10})
	require.NoError(t, err)

	err = ts.store.AddUserToGroup(user.ID, group.ID, "")
	require.NoError(t, err)
}

func TestRemoveUserFromGroup_Exists(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	user, err := ts.store.CreateUser(auth.CreateUserRequest{Name: "remove-user", Email: "remove@test.com"})
	require.NoError(t, err)

	group, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "remove-group", Budget: 10})
	require.NoError(t, err)

	err = ts.store.AddUserToGroup(user.ID, group.ID, "member")
	require.NoError(t, err)

	err = ts.store.RemoveUserFromGroup(user.ID, group.ID)
	require.NoError(t, err)
}

func TestRemoveUserFromGroup_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	err := ts.store.RemoveUserFromGroup(999, 999)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestGetGroupUsers(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	group, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "multi-user-group", Budget: 100})
	require.NoError(t, err)

	user1, err := ts.store.CreateUser(auth.CreateUserRequest{Name: "user1", Email: "u1@test.com"})
	require.NoError(t, err)

	user2, err := ts.store.CreateUser(auth.CreateUserRequest{Name: "user2", Email: "u2@test.com"})
	require.NoError(t, err)

	err = ts.store.AddUserToGroup(user1.ID, group.ID, "admin")
	require.NoError(t, err)

	err = ts.store.AddUserToGroup(user2.ID, group.ID, "member")
	require.NoError(t, err)

	users, err := ts.store.GetGroupUsers(group.ID)
	require.NoError(t, err)
	require.Len(t, users, 2)
	assert.Equal(t, user1.ID, users[0].ID)
	assert.Equal(t, "user1", users[0].Name)
	assert.Equal(t, "u1@test.com", users[0].Email)
	assert.Equal(t, user2.ID, users[1].ID)
}

func TestGetGroupUsers_NoUsers(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	group, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "empty-group", Budget: 10})
	require.NoError(t, err)

	users, err := ts.store.GetGroupUsers(group.ID)
	require.NoError(t, err)
	assert.Empty(t, users)
}

func TestGetGroupUsers_AfterRemoval(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	group, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "removal-test-group", Budget: 10})
	require.NoError(t, err)

	user, err := ts.store.CreateUser(auth.CreateUserRequest{Name: "removed-user", Email: "removed@test.com"})
	require.NoError(t, err)

	err = ts.store.AddUserToGroup(user.ID, group.ID, "member")
	require.NoError(t, err)

	err = ts.store.RemoveUserFromGroup(user.ID, group.ID)
	require.NoError(t, err)

	users, err := ts.store.GetGroupUsers(group.ID)
	require.NoError(t, err)
	assert.Empty(t, users)
}

func TestGetUserGroups(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	user, err := ts.store.CreateUser(auth.CreateUserRequest{Name: "multi-group-user", Email: "multi@test.com"})
	require.NoError(t, err)

	group1, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "group-a", Budget: 10})
	require.NoError(t, err)

	group2, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "group-b", Budget: 20})
	require.NoError(t, err)

	err = ts.store.AddUserToGroup(user.ID, group1.ID, "member")
	require.NoError(t, err)

	err = ts.store.AddUserToGroup(user.ID, group2.ID, "admin")
	require.NoError(t, err)

	groups, err := ts.store.GetUserGroups(user.ID)
	require.NoError(t, err)
	require.Len(t, groups, 2)
	assert.Equal(t, "group-a", groups[0].Name)
	assert.Equal(t, "group-b", groups[1].Name)
}

func TestGetUserGroups_NoGroups(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	user, err := ts.store.CreateUser(auth.CreateUserRequest{Name: "lonely-user", Email: "lonely@test.com"})
	require.NoError(t, err)

	groups, err := ts.store.GetUserGroups(user.ID)
	require.NoError(t, err)
	assert.Empty(t, groups)
}

func TestGroupMembership_CrossCheck(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	// Create 2 users and 2 groups, add users to different groups,
	// then cross-check GetGroupUsers and GetUserGroups.

	user1, err := ts.store.CreateUser(auth.CreateUserRequest{Name: "cross-a", Email: "cross-a@test.com"})
	require.NoError(t, err)

	user2, err := ts.store.CreateUser(auth.CreateUserRequest{Name: "cross-b", Email: "cross-b@test.com"})
	require.NoError(t, err)

	group1, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "cross-1", Budget: 10})
	require.NoError(t, err)

	group2, err := ts.store.CreateGroup(auth.CreateGroupRequest{Name: "cross-2", Budget: 20})
	require.NoError(t, err)

	// user1 → both groups, user2 → only group1
	_ = ts.store.AddUserToGroup(user1.ID, group1.ID, "member")
	_ = ts.store.AddUserToGroup(user1.ID, group2.ID, "member")
	_ = ts.store.AddUserToGroup(user2.ID, group1.ID, "admin")

	// GetGroupUsers: group1 should have both users
	usersInGroup1, err := ts.store.GetGroupUsers(group1.ID)
	require.NoError(t, err)
	require.Len(t, usersInGroup1, 2)

	// GetGroupUsers: group2 should have only user1
	usersInGroup2, err := ts.store.GetGroupUsers(group2.ID)
	require.NoError(t, err)
	require.Len(t, usersInGroup2, 1)
	assert.Equal(t, user1.ID, usersInGroup2[0].ID)

	// GetUserGroups: user1 should have both groups
	groupsForUser1, err := ts.store.GetUserGroups(user1.ID)
	require.NoError(t, err)
	require.Len(t, groupsForUser1, 2)

	// GetUserGroups: user2 should have only group1
	groupsForUser2, err := ts.store.GetUserGroups(user2.ID)
	require.NoError(t, err)
	require.Len(t, groupsForUser2, 1)
	assert.Equal(t, group1.ID, groupsForUser2[0].ID)
}
