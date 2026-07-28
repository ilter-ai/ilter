package semanticcache

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaEmbedder_Dim(t *testing.T) {
	e := NewOllamaEmbedder("http://localhost:11434")
	if d := e.Dim(); d != 768 {
		t.Errorf("expected dim 768, got %d", d)
	}
}

func TestOllamaEmbedder_Embed_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("expected /api/embeddings, got %s", r.URL.Path)
		}
		var req ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Model != "nomic-embed-text" {
			t.Errorf("expected model nomic-embed-text, got %s", req.Model)
		}
		if req.Prompt != "hello world" {
			t.Errorf("expected prompt 'hello world', got %s", req.Prompt)
		}

		// Return a 768-dimensional vector
		emb := make([]float32, 768)
		emb[0] = 0.1
		emb[1] = 0.2
		emb[767] = 0.3
		_ = json.NewEncoder(w).Encode(ollamaEmbedResponse{Embedding: emb})
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL)
	vec, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 768 {
		t.Fatalf("expected 768-dim vector, got %d", len(vec))
	}
	if vec[0] != 0.1 || vec[1] != 0.2 || vec[767] != 0.3 {
		t.Errorf("unexpected vector values: %v", vec[:3])
	}
}

func TestOllamaEmbedder_Embed_Error(t *testing.T) {
	// Server returns 500
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL)
	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOllamaEmbedder_Embed_Unreachable(t *testing.T) {
	e := NewOllamaEmbedder("http://127.0.0.1:1")
	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for unreachable server, got nil")
	}
}

func TestOllamaEmbedder_Embed_WrongDim(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(ollamaEmbedResponse{
			Embedding: []float32{0.1, 0.2}, // only 2-dim, not 768
		})
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL)
	vec, err := e.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 2 {
		t.Errorf("expected 2-dim vector, got %d", len(vec))
	}
}
