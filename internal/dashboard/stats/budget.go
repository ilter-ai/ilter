package stats

// KeyInfo holds identifying information about an API key used across event types.
type KeyInfo struct {
	ID        string `json:"id"`
	KeyName   string `json:"key_name"`
	OwnerType string `json:"owner_type,omitempty"`
	OwnerID   int    `json:"owner_id,omitempty"`
	OwnerName string `json:"owner_name,omitempty"`
}
