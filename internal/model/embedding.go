package model

// EmbeddingRequest mirrors OpenAI's POST /v1/embeddings request body.
// Input accepts a string, an array of strings, or arrays of token IDs —
// it is passed through to the upstream provider untouched.
type EmbeddingRequest struct {
	Model          string `json:"model"`
	Input          any    `json:"input"`
	EncodingFormat string `json:"encoding_format,omitempty"`
	Dimensions     *int   `json:"dimensions,omitempty"`
	User           string `json:"user,omitempty"`
}

type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  *Usage          `json:"usage,omitempty"`
}

type EmbeddingData struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

// RerankRequest mirrors the Cohere/TEI-style rerank request shape used by
// self-hosted rerankers (oMLX, Xinference, Infinity, text-embeddings-inference).
type RerankRequest struct {
	Model           string   `json:"model"`
	Query           string   `json:"query"`
	Documents       []string `json:"documents"`
	TopN            *int     `json:"top_n,omitempty"`
	ReturnDocuments bool     `json:"return_documents,omitempty"`
}

type RerankResponse struct {
	Model   string         `json:"model,omitempty"`
	Results []RerankResult `json:"results"`
	Usage   *Usage         `json:"usage,omitempty"`
}

type RerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
	Document       *string `json:"document,omitempty"`
}
