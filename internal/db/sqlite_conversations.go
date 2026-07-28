package db

import (
	"context"

	"github.com/ilter-ai/ilter/internal/db/sqlc"
)

// ConversationRow is a conversations row for the dashboard chat UI.
// CreatedAt/UpdatedAt use the raw SQLite timestamp layout (see timefmt.go),
// matching what the dashboard API has always returned.
type ConversationRow struct {
	ID        string
	Title     string
	CreatedAt string
	UpdatedAt string
}

// ConversationSummary is a ConversationRow plus its last-message preview and
// message count, for the thread list view.
type ConversationSummary struct {
	ConversationRow
	LastMessage  string
	MessageCount int
}

// ListConversations returns all conversations ordered by most recently updated.
func (s *SQLiteStore) ListConversations() ([]ConversationSummary, error) {
	rows, err := s.queries.ListConversations(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]ConversationSummary, 0, len(rows))
	for _, r := range rows {
		result = append(result, ConversationSummary{
			ConversationRow: ConversationRow{
				ID:        r.ID,
				Title:     r.Title,
				CreatedAt: r.CreatedAt.UTC().Format(sqliteTimestampLayout),
				UpdatedAt: r.UpdatedAt.UTC().Format(sqliteTimestampLayout),
			},
			LastMessage:  r.LastMessage,
			MessageCount: int(r.MessageCount),
		})
	}
	return result, nil
}

// CreateConversation inserts a new conversation with the given id and title.
func (s *SQLiteStore) CreateConversation(id, title string) error {
	return s.queries.CreateConversation(context.Background(), sqlc.CreateConversationParams{ID: id, Title: title})
}

// GetConversation returns a single conversation by id.
func (s *SQLiteStore) GetConversation(id string) (*ConversationRow, error) {
	r, err := s.queries.GetConversation(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return &ConversationRow{
		ID:        r.ID,
		Title:     r.Title,
		CreatedAt: r.CreatedAt.UTC().Format(sqliteTimestampLayout),
		UpdatedAt: r.UpdatedAt.UTC().Format(sqliteTimestampLayout),
	}, nil
}

// ConversationExists reports whether a conversation with the given id exists.
func (s *SQLiteStore) ConversationExists(id string) (bool, error) {
	n, err := s.queries.ConversationExists(context.Background(), id)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// UpdateConversationTitle sets a conversation's title and bumps updated_at.
// Returns false if no conversation matched id.
func (s *SQLiteStore) UpdateConversationTitle(id, title string) (bool, error) {
	n, err := s.queries.UpdateConversationTitle(context.Background(), sqlc.UpdateConversationTitleParams{Title: title, ID: id})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetConversationTitle sets a conversation's title without bumping updated_at
// (used by the auto-title-from-first-message heuristic).
func (s *SQLiteStore) SetConversationTitle(id, title string) error {
	return s.queries.SetConversationTitle(context.Background(), sqlc.SetConversationTitleParams{Title: title, ID: id})
}

// TouchConversation bumps a conversation's updated_at to now.
func (s *SQLiteStore) TouchConversation(id string) error {
	return s.queries.TouchConversation(context.Background(), id)
}

// DeleteConversation removes a conversation and its messages (CASCADE).
// Returns false if no conversation matched id.
func (s *SQLiteStore) DeleteConversation(id string) (bool, error) {
	n, err := s.queries.DeleteConversation(context.Background(), id)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// MessageRow is a messages row for the dashboard chat UI.
type MessageRow struct {
	ID               int
	ConversationID   string
	Role             string
	Content          string
	Model            *string
	TokenCount       *int
	Cost             *float64
	ReasoningContent *string
	ToolCalls        *string
	UsageCost        *float64
	BillingKey       *string
	CreatedAt        string
}

func messageRowFromSQLC(m sqlc.Message) MessageRow {
	return MessageRow{
		ID:               int(m.ID),
		ConversationID:   m.ConversationID,
		Role:             m.Role,
		Content:          m.Content,
		Model:            m.Model,
		TokenCount:       int64PtrToIntPtr(m.TokenCount),
		Cost:             m.Cost,
		ReasoningContent: m.ReasoningContent,
		ToolCalls:        m.ToolCalls,
		UsageCost:        m.UsageCost,
		BillingKey:       m.BillingKey,
		CreatedAt:        m.CreatedAt.UTC().Format(sqliteTimestampLayout),
	}
}

// ListMessagesByConversation returns every message for a conversation, oldest first.
func (s *SQLiteStore) ListMessagesByConversation(conversationID string) ([]MessageRow, error) {
	rows, err := s.queries.ListMessagesByConversation(context.Background(), conversationID)
	if err != nil {
		return nil, err
	}
	result := make([]MessageRow, 0, len(rows))
	for _, r := range rows {
		result = append(result, messageRowFromSQLC(r))
	}
	return result, nil
}

// NewMessageParams holds the fields needed to insert a new message.
type NewMessageParams struct {
	ConversationID   string
	Role             string
	Content          string
	Model            *string
	TokenCount       *int
	Cost             *float64
	ReasoningContent *string
	ToolCalls        *string
	UsageCost        *float64
	BillingKey       *string
}

// InsertMessage inserts a new message and returns its assigned id.
func (s *SQLiteStore) InsertMessage(p NewMessageParams) (int, error) {
	id, err := s.queries.InsertMessage(context.Background(), sqlc.InsertMessageParams{
		ConversationID:   p.ConversationID,
		Role:             p.Role,
		Content:          p.Content,
		Model:            p.Model,
		TokenCount:       intToInt64Ptr(p.TokenCount),
		Cost:             p.Cost,
		ReasoningContent: p.ReasoningContent,
		ToolCalls:        p.ToolCalls,
		UsageCost:        p.UsageCost,
		BillingKey:       p.BillingKey,
	})
	if err != nil {
		return 0, err
	}
	return int(id), nil
}

// GetMessageCreatedAt returns the created_at timestamp for a message, in the
// same raw layout as other timestamp fields in this package.
func (s *SQLiteStore) GetMessageCreatedAt(id int) (string, error) {
	t, err := s.queries.GetMessageCreatedAt(context.Background(), int64(id))
	if err != nil {
		return "", err
	}
	return t.UTC().Format(sqliteTimestampLayout), nil
}

// ListMessagesPaginated returns up to limit messages for a conversation,
// newest first, optionally starting before beforeID (nil for the first page).
func (s *SQLiteStore) ListMessagesPaginated(conversationID string, beforeID *int, limit int) ([]MessageRow, error) {
	rows, err := s.queries.ListMessagesPaginated(context.Background(), sqlc.ListMessagesPaginatedParams{
		ConversationID: conversationID,
		BeforeID:       intToInt64Ptr(beforeID),
		Limit:          int64(limit),
	})
	if err != nil {
		return nil, err
	}
	result := make([]MessageRow, 0, len(rows))
	for _, r := range rows {
		result = append(result, messageRowFromSQLC(r))
	}
	return result, nil
}
