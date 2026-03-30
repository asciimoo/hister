package extractor

import (
	"context"

	"github.com/asciimoo/hister/server/indexer/types"
)

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
	// Extract processes the document and returns a Result.
	Extract(ctx context.Context, doc *types.Document) (*Result, error)
}
