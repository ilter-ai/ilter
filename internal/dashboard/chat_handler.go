package dashboard

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/model"
)

// ChatHandler serves chat conversation CRUD endpoints.
type ChatHandler struct {
	store *db.SQLiteStore
}

func NewChatHandler(store *db.SQLiteStore) *ChatHandler {
	return &ChatHandler{store: store}
}

// --- response types ---

type conversationResponse struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	LastMessagePreview string `json:"last_message_preview,omitempty"`
	MessageCount       int    `json:"message_count"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

type messageResponse struct {
	ID               int      `json:"id"`
	ConversationID   string   `json:"conversation_id"`
	Role             string   `json:"role"`
	Content          string   `json:"content"`
	Model            *string  `json:"model,omitempty"`
	TokenCount       *int     `json:"token_count,omitempty"`
	Cost             *float64 `json:"cost,omitempty"`
	ReasoningContent *string  `json:"reasoning_content,omitempty"`
	ToolCalls        *string  `json:"tool_calls,omitempty"`
	UsageCost        *float64 `json:"usage_cost,omitempty"`
	BillingKey       *string  `json:"billing_key,omitempty"`
	CreatedAt        string   `json:"created_at"`
}

func messageResponseFromRow(r db.MessageRow) messageResponse {
	return messageResponse{
		ID:               r.ID,
		ConversationID:   r.ConversationID,
		Role:             r.Role,
		Content:          r.Content,
		Model:            r.Model,
		TokenCount:       r.TokenCount,
		Cost:             r.Cost,
		ReasoningContent: r.ReasoningContent,
		ToolCalls:        r.ToolCalls,
		UsageCost:        r.UsageCost,
		BillingKey:       r.BillingKey,
		CreatedAt:        r.CreatedAt,
	}
}

// --- helpers ---

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ListThreads returns all conversations ordered by most recently updated.
func (h *ChatHandler) ListThreads(w http.ResponseWriter, _ *http.Request) {
	rows, err := h.store.ListConversations()
	if err != nil {
		slog.Error("failed to list threads", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	conversations := make([]conversationResponse, 0, len(rows))
	for _, r := range rows {
		conversations = append(conversations, conversationResponse{
			ID:                 r.ID,
			Title:              r.Title,
			LastMessagePreview: truncate(r.LastMessage, 100),
			MessageCount:       r.MessageCount,
			CreatedAt:          r.CreatedAt,
			UpdatedAt:          r.UpdatedAt,
		})
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"conversations": conversations,
	})
}

// CreateThread creates a new conversation thread.
func (h *ChatHandler) CreateThread(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if body.Title == "" {
		body.Title = "New Chat"
	}

	id := uuid.New().String()

	if err := h.store.CreateConversation(id, body.Title); err != nil {
		slog.Error("failed to create thread", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	model.WriteJSON(w, http.StatusCreated, map[string]any{
		"conversation": map[string]any{
			"id":         id,
			"title":      body.Title,
			"created_at": "now",
			"updated_at": "now",
		},
	})
}

// GetThread returns a conversation with all its messages.
func (h *ChatHandler) GetThread(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	conv, err := h.store.GetConversation(id)
	if errors.Is(err, sql.ErrNoRows) {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "conversation not found")
		return
	}
	if err != nil {
		slog.Error("failed to get thread", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	msgRows, err := h.store.ListMessagesByConversation(id)
	if err != nil {
		slog.Error("failed to query messages", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	messages := make([]messageResponse, 0, len(msgRows))
	for _, r := range msgRows {
		messages = append(messages, messageResponseFromRow(r))
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"conversation": conversationResponse{
			ID:        conv.ID,
			Title:     conv.Title,
			CreatedAt: conv.CreatedAt,
			UpdatedAt: conv.UpdatedAt,
		},
		"messages": messages,
	})
}

// UpdateThread updates a conversation's title.
func (h *ChatHandler) UpdateThread(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if body.Title == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request", "title is required")
		return
	}

	found, err := h.store.UpdateConversationTitle(id, body.Title)
	if err != nil {
		slog.Error("failed to update thread", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if !found {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "conversation not found")
		return
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"conversation": map[string]any{
			"id":    id,
			"title": body.Title,
		},
	})
}

// DeleteThread deletes a conversation and its messages (CASCADE).
func (h *ChatHandler) DeleteThread(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	found, err := h.store.DeleteConversation(id)
	if err != nil {
		slog.Error("failed to delete thread", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if !found {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "conversation not found")
		return
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// AddMessage creates a new message in a conversation.
func (h *ChatHandler) AddMessage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var body struct {
		Role             string   `json:"role"`
		Content          string   `json:"content"`
		Model            *string  `json:"model,omitempty"`
		TokenCount       *int     `json:"token_count,omitempty"`
		Cost             *float64 `json:"cost,omitempty"`
		ReasoningContent *string  `json:"reasoning_content,omitempty"`
		ToolCalls        *string  `json:"tool_calls,omitempty"`
		UsageCost        *float64 `json:"usage_cost,omitempty"`
		BillingKey       *string  `json:"billing_key,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if body.Role == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request", "role is required")
		return
	}

	// Verify conversation exists
	conv, err := h.store.GetConversation(id)
	if errors.Is(err, sql.ErrNoRows) {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "conversation not found")
		return
	}
	if err != nil {
		slog.Error("failed to verify conversation", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	msgID, err := h.store.InsertMessage(db.NewMessageParams{
		ConversationID:   id,
		Role:             body.Role,
		Content:          body.Content,
		Model:            body.Model,
		TokenCount:       body.TokenCount,
		Cost:             body.Cost,
		ReasoningContent: body.ReasoningContent,
		ToolCalls:        body.ToolCalls,
		UsageCost:        body.UsageCost,
		BillingKey:       body.BillingKey,
	})
	if err != nil {
		slog.Error("failed to add message", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	// Update conversation timestamp
	if tErr := h.store.TouchConversation(id); tErr != nil {
		slog.Error("failed to touch conversation", "id", id, "error", tErr)
	}

	// Auto-title: set title to first 40 chars of first user message
	if body.Role == "user" && conv.Title == "New Chat" {
		autoTitle := truncate(body.Content, 40)
		if sErr := h.store.SetConversationTitle(id, autoTitle); sErr != nil {
			slog.Error("failed to set conversation title", "id", id, "error", sErr)
		}
	}

	createdAt, err := h.store.GetMessageCreatedAt(msgID)
	if err != nil {
		slog.Error("failed to get message created_at", "id", msgID, "error", err)
	}

	model.WriteJSON(w, http.StatusCreated, map[string]any{
		"message": messageResponse{
			ID:               msgID,
			ConversationID:   id,
			Role:             body.Role,
			Content:          body.Content,
			Model:            body.Model,
			TokenCount:       body.TokenCount,
			Cost:             body.Cost,
			ReasoningContent: body.ReasoningContent,
			ToolCalls:        body.ToolCalls,
			UsageCost:        body.UsageCost,
			BillingKey:       body.BillingKey,
			CreatedAt:        createdAt,
		},
	})
}

// ListMessages returns paginated messages for a conversation.
func (h *ChatHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}

	// Verify conversation exists
	exists, err := h.store.ConversationExists(id)
	if err != nil || !exists {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "conversation not found")
		return
	}

	var beforeID *int
	if bStr := r.URL.Query().Get("before_id"); bStr != "" {
		b, atoiErr := strconv.Atoi(bStr)
		if atoiErr == nil {
			beforeID = &b
		}
	}

	// fetch +1 to detect has_more
	rows, err := h.store.ListMessagesPaginated(id, beforeID, limit+1)
	if err != nil {
		slog.Error("failed to list messages", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	messages := make([]messageResponse, 0, len(rows))
	for _, r := range rows {
		messages = append(messages, messageResponseFromRow(r))
	}

	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"messages": messages,
		"has_more": hasMore,
	})
}
