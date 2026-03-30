package types

// Document holds the core data fields for an indexed document.
// It lives in the types package so that both the indexer and extractor
// packages can reference it without circular imports.
type Document struct {
	URL        string         `json:"url"`
	Domain     string         `json:"domain"`
	HTML       string         `json:"html"`
	Title      string         `json:"title"`
	Text       string         `json:"text"`
	Favicon    string         `json:"favicon"`
	Score      float64        `json:"score"`
	Added      int64          `json:"added"`
	Type       DocType        `json:"type"`
	Language   string         `json:"language"`
	UserID     uint           `json:"user_id"`
	Properties map[string]any `json:"properties,omitempty"`
}
