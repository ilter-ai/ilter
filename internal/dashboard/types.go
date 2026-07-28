package dashboard

// KeyInfo holds identifying information about an API key used across event types.
type KeyInfo struct {
	ID        string `json:"id"`
	KeyName   string `json:"key_name"`
	OwnerType string `json:"owner_type,omitempty"`
	OwnerID   int    `json:"owner_id,omitempty"`
	OwnerName string `json:"owner_name,omitempty"`
}

type Page[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

type RequestSummary struct {
	ID               int     `json:"id"`
	Timestamp        string  `json:"timestamp"`
	KeyID            string  `json:"key_id"`
	Model            string  `json:"model"`
	Provider         string  `json:"provider"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalCost        float64 `json:"total_cost"`
	LatencyMs        int     `json:"latency_ms"`
	StatusCode       int     `json:"status_code"`
	CacheHit         bool    `json:"cache_hit"`
	ClientIP         string  `json:"client_ip,omitempty"`
	HasBody          bool    `json:"has_body"`
	TraceID          *string `json:"trace_id,omitempty"`
	PromptPreview    string  `json:"prompt_preview,omitempty"`
}

type RequestDetail struct {
	RequestSummary
	RequestBody    *string             `json:"request_body,omitempty"`
	ResponseBody   *string             `json:"response_body,omitempty"`
	PhaseLatencies PhaseLatencySummary `json:"phase_latencies"`
}

type PhaseLatencySummary struct {
	GuardrailLatencyMs float64 `json:"guardrail_latency_ms"`
	LLMLatencyMs       float64 `json:"llm_latency_ms"`
	QueuedLatencyMs    float64 `json:"queued_latency_ms"`
}
