package extractor

import (
	"context"
	"errors"
)

// Input holds the data available for extraction.
type Input struct {
	URL    string
	Domain string
	HTML   string
}

// Result holds the output of an extraction operation.
type Result struct {
	Title      string         `json:"title"`
	Text       string         `json:"text"` // searchable text for the index
	FaviconURL string         `json:"favicon_url,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`  // display metadata (key-value pairs)
	StoredData string         `json:"stored_data,omitempty"` // JSON blob stored in HTML field (survives reindex)
}

// Extractor defines the interface for content extraction.
// Implementations may operate synchronously (e.g., HTML parsing) or
// asynchronously (e.g., fetching metadata from external tools).
// Async extractors can use the Manager as a reusable worker pool helper.
type Extractor interface {
	// Name returns the extractor's unique identifier (e.g., "Readability", "yt-dlp").
	Name() string
	// Match returns true if this extractor can handle the given URL.
	Match(url, domain string) bool
	// Extract processes the input and returns a Result.
	Extract(ctx context.Context, input *Input) (*Result, error)
	// Rebuild reconstructs a Result from previously stored data,
	// without making any external requests.
	// Return ErrRebuildNotSupported if this extractor doesn't support rebuilding.
	Rebuild(storedData string) (*Result, error)
}

// ErrRebuildNotSupported is returned by extractors that do not support
// rebuilding from stored data.
var ErrRebuildNotSupported = errors.New("rebuild not supported")
