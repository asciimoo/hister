package extractor

import (
	"context"
)

// Input holds the data available for extraction.
type Input struct {
	URL      string
	Domain   string
	HTML     string
	Title    string
	Text     string
	Type     int
	Language string
	UserID   uint
}

// Result holds the output of an extraction operation.
type Result struct {
	Title      string         `json:"title"`
	Text       string         `json:"text"` // searchable text for the index
	FaviconURL string         `json:"favicon_url,omitempty"`
	Properties map[string]any `json:"properties,omitempty"` // display metadata (key-value pairs)
}

// Extractor defines the interface for content extraction.
type Extractor interface {
	// Name returns the extractor's unique identifier (e.g., "Readability", "yt-dlp").
	Name() string
	// Initialize is called once at startup with extractor-specific configuration.
	Initialize(config map[string]any) error
	// Match returns true if this extractor can handle the given URL.
	Match(url, domain string) bool
	// Extract processes the input and returns a Result.
	Extract(ctx context.Context, input *Input) (*Result, error)
}
