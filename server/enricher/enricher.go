package enricher

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// Enricher defines the interface for async document enrichment.
// Unlike Extractors (which are sync HTML parsers), Enrichers run in the
// background and fetch metadata from external sources.
type Enricher interface {
	// Name returns the enricher's unique identifier (e.g., "yt-dlp").
	Name() string
	// Match returns true if this enricher can handle the given URL.
	Match(url, domain string) bool
	// Enrich fetches metadata for the URL and returns a Result.
	Enrich(ctx context.Context, url string) (*Result, error)
	// Rebuild reconstructs a Result from previously stored data,
	// without making any external requests.
	Rebuild(storedData string) (*Result, error)
}

// Result holds the output of an enrichment operation.
type Result struct {
	Title      string         `json:"title"`
	Text       string         `json:"text"`        // searchable text for the index
	Properties map[string]any `json:"properties"`  // display metadata (key-value pairs)
	StoredData string         `json:"stored_data"` // JSON blob stored in HTML field (survives reindex)
}

// ExistingDoc holds fields from an already-indexed document that should
// be preserved when re-indexing after enrichment.
type ExistingDoc struct {
	Domain   string
	Favicon  string
	Added    int64
	Language string
}

const enricherKey = "_enricher"

// IsStoredEnrichment checks whether the HTML field contains enrichment
// data rather than regular HTML content.
func IsStoredEnrichment(html string) bool {
	if !strings.HasPrefix(html, "{") {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(html), &m); err != nil {
		return false
	}
	_, ok := m[enricherKey]
	return ok
}

// ParseStoredEnricherName extracts the enricher name from stored enrichment JSON.
func ParseStoredEnricherName(html string) (string, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(html), &m); err != nil {
		return "", err
	}
	name, ok := m[enricherKey].(string)
	if !ok || name == "" {
		return "", errors.New("missing or invalid _enricher field")
	}
	return name, nil
}

// ParseStoredProperties extracts the properties from stored enrichment JSON.
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
