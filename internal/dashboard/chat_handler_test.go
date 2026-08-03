package dashboard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
)

func setupChatTestStore(t *testing.T) *db.SQLiteStore {
	t.Helper()
	store, err := db.NewSQLiteStore(config.StorageConfig{Type: "sqlite", SqlitePath: ":memory:"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func chatTestRouter(h *ChatHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/threads", h.ListThreads)
	r.Post("/threads", h.CreateThread)
	r.Get("/threads/{id}", h.GetThread)
	r.Put("/threads/{id}", h.UpdateThread)
	r.Delete("/threads/{id}", h.DeleteThread)
	r.Post("/threads/{id}/messages", h.AddMessage)
	r.Get("/threads/{id}/messages", h.ListMessages)
	return r
}

func TestChatHandler_ThreadLifecycle(t *testing.T) {
	store := setupChatTestStore(t)
	h := NewChatHandler(store)
	r := chatTestRouter(h)

	// Create
	body, _ := json.Marshal(map[string]string{"title": "My Chat"})
	req := httptest.NewRequest(http.MethodPost, "/threads", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusCreated, rr.Code)

	var createResp struct {
		Conversation struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"conversation"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &createResp))
	id := createResp.Conversation.ID
	require.NotEmpty(t, id)
	assert.Equal(t, "My Chat", createResp.Conversation.Title)

	// List — must include the new thread
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/threads", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	var listResp struct {
		Conversations []conversationResponse `json:"conversations"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &listResp))
	require.Len(t, listResp.Conversations, 1)
	assert.Equal(t, id, listResp.Conversations[0].ID)
	assert.Equal(t, 0, listResp.Conversations[0].MessageCount)

	// Get — empty message list
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/threads/"+id, nil))
	require.Equal(t, http.StatusOK, rr.Code)
	var getResp struct {
		Conversation conversationResponse `json:"conversation"`
		Messages     []messageResponse    `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &getResp))
	assert.Equal(t, "My Chat", getResp.Conversation.Title)
	assert.Empty(t, getResp.Messages)

	// Update title
	body, _ = json.Marshal(map[string]string{"title": "Renamed"})
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/threads/"+id, bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, rr.Code)

	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/threads/"+id, nil))
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &getResp))
	assert.Equal(t, "Renamed", getResp.Conversation.Title)

	// Update on missing id -> 404
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/threads/no-such-id", bytes.NewReader(body)))
	assert.Equal(t, http.StatusNotFound, rr.Code)

	// Delete
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/threads/"+id, nil))
	assert.Equal(t, http.StatusOK, rr.Code)

	// Get after delete -> 404
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/threads/"+id, nil))
	assert.Equal(t, http.StatusNotFound, rr.Code)

	// Delete again -> 404
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/threads/"+id, nil))
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestChatHandler_AddMessage_AutoTitleAndFields(t *testing.T) {
	store := setupChatTestStore(t)
	h := NewChatHandler(store)
	r := chatTestRouter(h)

	body, _ := json.Marshal(map[string]string{}) // no title -> defaults to "New Chat"
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/threads", bytes.NewReader(body)))
	require.Equal(t, http.StatusCreated, rr.Code)
	var createResp struct {
		Conversation struct {
			ID string `json:"id"`
		} `json:"conversation"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &createResp))
	id := createResp.Conversation.ID

	// First user message auto-titles the conversation.
	longContent := "This is the first message and it is definitely longer than forty characters."
	tokenCount := 42
	cost := 0.0123
	msgBody, _ := json.Marshal(map[string]any{
		"role":        "user",
		"content":     longContent,
		"token_count": tokenCount,
		"cost":        cost,
	})
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/threads/"+id+"/messages", bytes.NewReader(msgBody)))
	require.Equal(t, http.StatusCreated, rr.Code)

	var addResp struct {
		Message messageResponse `json:"message"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &addResp))
	assert.Equal(t, longContent, addResp.Message.Content)
	require.NotNil(t, addResp.Message.TokenCount)
	assert.Equal(t, tokenCount, *addResp.Message.TokenCount)
	require.NotNil(t, addResp.Message.Cost)
	assert.InDelta(t, cost, *addResp.Message.Cost, 1e-9)
	assert.NotEmpty(t, addResp.Message.CreatedAt)

	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/threads/"+id, nil))
	var getResp struct {
		Conversation conversationResponse `json:"conversation"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &getResp))
	assert.Equal(t, truncate(longContent, 40), getResp.Conversation.Title)

	// A second (assistant) message must not re-trigger auto-title.
	msgBody2, _ := json.Marshal(map[string]string{"role": "assistant", "content": "a reply"})
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/threads/"+id+"/messages", bytes.NewReader(msgBody2)))
	require.Equal(t, http.StatusCreated, rr.Code)

	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/threads/"+id, nil))
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &getResp))
	assert.Equal(t, truncate(longContent, 40), getResp.Conversation.Title, "title must not change after the first user message")

	// AddMessage on missing conversation -> 404
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/threads/no-such-id/messages", bytes.NewReader(msgBody2)))
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestChatHandler_ListMessages_Pagination(t *testing.T) {
	store := setupChatTestStore(t)
	h := NewChatHandler(store)
	r := chatTestRouter(h)

	body, _ := json.Marshal(map[string]string{"title": "Paginated"})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/threads", bytes.NewReader(body)))
	var createResp struct {
		Conversation struct {
			ID string `json:"id"`
		} `json:"conversation"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &createResp))
	id := createResp.Conversation.ID

	const total = 5
	for i := range total {
		msgBody, _ := json.Marshal(map[string]string{
			"role":    "user",
			"content": fmt.Sprintf("message %d", i),
		})
		rr = httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/threads/"+id+"/messages", bytes.NewReader(msgBody)))
		require.Equal(t, http.StatusCreated, rr.Code)
	}

	// First page: limit=2, newest first, has_more=true.
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/threads/"+id+"/messages?limit=2", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	var page1 struct {
		Messages []messageResponse `json:"messages"`
		HasMore  bool              `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &page1))
	require.Len(t, page1.Messages, 2)
	assert.True(t, page1.HasMore)
	assert.Equal(t, "message 4", page1.Messages[0].Content)
	assert.Equal(t, "message 3", page1.Messages[1].Content)

	// Second page, using before_id from the last message of page 1 — must
	// return the next two messages, not repeat page 1 (regression check for
	// the id/TEXT comparison bug where before_id was silently ignored).
	beforeID := page1.Messages[len(page1.Messages)-1].ID
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/threads/%s/messages?limit=2&before_id=%d", id, beforeID), nil))
	require.Equal(t, http.StatusOK, rr.Code)
	var page2 struct {
		Messages []messageResponse `json:"messages"`
		HasMore  bool              `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &page2))
	require.Len(t, page2.Messages, 2)
	assert.True(t, page2.HasMore)
	assert.Equal(t, "message 2", page2.Messages[0].Content)
	assert.Equal(t, "message 1", page2.Messages[1].Content)
	for _, m := range page2.Messages {
		assert.Less(t, m.ID, beforeID, "every message on page 2 must have id < before_id")
	}

	// Third page — final message, has_more=false.
	beforeID2 := page2.Messages[len(page2.Messages)-1].ID
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/threads/%s/messages?limit=2&before_id=%d", id, beforeID2), nil))
	require.Equal(t, http.StatusOK, rr.Code)
	var page3 struct {
		Messages []messageResponse `json:"messages"`
		HasMore  bool              `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &page3))
	require.Len(t, page3.Messages, 1)
	assert.False(t, page3.HasMore)
	assert.Equal(t, "message 0", page3.Messages[0].Content)

	// ListMessages on missing conversation -> 404
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/threads/no-such-id/messages", nil))
	assert.Equal(t, http.StatusNotFound, rr.Code)
}
