package extractor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

// ExistingDoc holds fields from an already-indexed document that should
// be preserved when re-indexing after extraction.
type ExistingDoc struct {
	Domain   string
	Favicon  string
	Added    int64
	Language string
}

// ErrRebuildNotSupported is returned by extractors that do not support
// rebuilding from stored data.
var ErrRebuildNotSupported = errors.New("rebuild not supported")

const extractorKey = "_extractor"

// IsStoredExtraction checks whether the HTML field contains extraction
// data rather than regular HTML content.
func IsStoredExtraction(html string) bool {
	if !strings.HasPrefix(html, "{") {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(html), &m); err != nil {
		return false
	}
	_, ok := m[extractorKey]
	return ok
}

// ParseStoredExtractorName extracts the extractor name from stored extraction JSON.
func ParseStoredExtractorName(html string) (string, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(html), &m); err != nil {
		return "", err
	}
	name, ok := m[extractorKey].(string)
	if !ok || name == "" {
		return "", errors.New("missing or invalid _extractor field")
	}
	return name, nil
}

// ParseStoredProperties extracts the properties from stored extraction JSON.
func ParseStoredProperties(html string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(html), &m); err != nil {
		return nil, err
	}
	props, ok := m["properties"].(map[string]any)
	if !ok {
		return nil, errors.New("missing or invalid properties field")
	}
	return props, nil
}
