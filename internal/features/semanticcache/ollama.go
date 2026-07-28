package semanticcache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const ollamaEmbedDim = 768

// OllamaEmbedder generates vector embeddings via the Ollama /api/embeddings HTTP API.
// It uses nomic-embed-text (768D) as the embedding model.
// When the Ollama endpoint is unreachable, Embed returns an error so the cache
// degrades to exact-match mode.
type OllamaEmbedder struct {
	url    string
	client *http.Client
	dim    int
}

// NewOllamaEmbedder creates an embedder that calls the Ollama embeddings API.
// url should point to a running Ollama instance, e.g. "http://localhost:11434".
// The model "nomic-embed-text" will be used; it must be pulled into Ollama first.
func NewOllamaEmbedder(url string) *OllamaEmbedder {
	return &OllamaEmbedder{
		url:    url,
		client: &http.Client{Timeout: 5 * time.Second},
		dim:    ollamaEmbedDim,
	}
}

func (o *OllamaEmbedder) Dim() int { return o.dim }

// Embed sends the text to the Ollama embeddings API and returns the vector.
// On any failure (network error, non-200, bad response), it returns an error
// so the caller can degrade to exact-match fallback.
func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body := ollamaEmbedRequest{
		Model:  "nomic-embed-text",
		Prompt: text,
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("ollama marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.url+"/api/embeddings", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama: %s: %s", resp.Status, string(bodyBytes))
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama decode: %w", err)
	}

	if len(result.Embedding) != o.dim {
		slog.Warn("Ollama embedding dimension mismatch", "expected", o.dim, "got", len(result.Embedding))
	}

	return result.Embedding, nil
}

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}
