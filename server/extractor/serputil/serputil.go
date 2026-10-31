// Package serputil provides shared helpers for extractors that index search
// engine result pages.
package serputil

import (
	"errors"
	"fmt"
	stdhtml "html"
	"net/url"
	"slices"
	"strings"

	"github.com/asciimoo/hister/server/extractor/sdk"
	"github.com/asciimoo/hister/server/sanitizer"
)

const (
	// DefaultMaxResults bounds how many results a result page contributes to
	// the index when the extractor configuration does not say otherwise.
	DefaultMaxResults = 20

	maxSnippetLength = 480
)

// Result is one organic search result parsed from a result page.
type Result struct {
	Title   string
	URL     string
	Snippet string
	Site    string
}

// Collector accumulates results in document order and drops duplicates by URL.
type Collector struct {
	results []Result
	seen    map[string]struct{}
}

// Add records candidate unless it lacks a title or URL, or its URL was already
// collected. Text fields are normalised before they are stored.
func (c *Collector) Add(candidate Result) {
	candidate.Title = CleanText(candidate.Title)
	candidate.URL = strings.TrimSpace(candidate.URL)
	candidate.Snippet = ClipSnippet(candidate.Snippet)
	candidate.Site = CleanText(candidate.Site)
	if candidate.Title == "" || candidate.URL == "" {
		return
	}
	if c.seen == nil {
		c.seen = make(map[string]struct{})
	}
	if _, found := c.seen[candidate.URL]; found {
		return
	}
	c.seen[candidate.URL] = struct{}{}
	c.results = append(c.results, candidate)
}

// Len returns the number of collected results.
func (c *Collector) Len() int {
	return len(c.results)
}

// Results returns the collected results in document order.
func (c *Collector) Results() []Result {
	return c.results
}

// CleanText collapses every whitespace run into a single space and trims the
// result.
func CleanText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ClipSnippet collapses whitespace and truncates long text on a word boundary.
func ClipSnippet(s string) string {
	text := CleanText(s)
	runes := []rune(text)
	if len(runes) <= maxSnippetLength {
		return text
	}
	cut := string(runes[:maxSnippetLength])
	if idx := strings.LastIndexByte(cut, ' '); idx > len(cut)/2 {
		cut = cut[:idx]
	}
	return strings.TrimRight(cut, " ,.;:-") + "…"
}

// CanonicalURL returns raw as an absolute http(s) URL without a fragment and
// without UTM tracking parameters. ok is false when raw is not a usable web
// URL.
func CanonicalURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		u.Scheme = strings.ToLower(u.Scheme)
	default:
		return "", false
	}
	u.Fragment = ""
	q := u.Query()
	tracking := false
	for key := range q {
		if key == "utm" || strings.HasPrefix(key, "utm_") {
			tracking = true
			q.Del(key)
		}
	}
	if tracking {
		u.RawQuery = q.Encode()
	}
	return u.String(), true
}

// HostOf returns the lower case host name of rawURL, or an empty string when
// the URL cannot be parsed.
func HostOf(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// IsHost reports whether host equals domain or is one of its subdomains.
func IsHost(host, domain string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == domain || strings.HasSuffix(host, "."+domain)
}

// DisplayedHost reduces the URL text search engines show under a result, such
// as "en.wikipedia.org › wiki › Web_scraping", to its host name.
func DisplayedHost(raw string) string {
	text := CleanText(raw)
	if idx := strings.Index(text, "›"); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	if text == "" {
		return ""
	}
	if !strings.Contains(text, "://") {
		text = "https://" + strings.TrimPrefix(text, "//")
	}
	u, err := url.Parse(text)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// Query returns the search query carried by a result page URL.
func Query(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return CleanText(u.Query().Get("q"))
}

// Limit truncates results to max entries. Non-positive values fall back to
// DefaultMaxResults.
func Limit(results []Result, max int) []Result {
	if max < 1 {
		max = DefaultMaxResults
	}
	if len(results) > max {
		return results[:max]
	}
	return results
}

// Documents converts parsed results into extra documents owned by owner. The
// searchable text keeps the title and snippet, and the metadata records the
// source type, query, position, and site. Every document is marked as
// summary-only so later indexing cannot replace richer content already stored
// for the same URL.
func Documents(results []Result, owner uint, metadataType, query string, maxResults int) []*sdk.Document {
	results = Limit(results, maxResults)
	docs := make([]*sdk.Document, 0, len(results))
	for i, result := range results {
		text := result.Title
		if result.Snippet != "" {
			text += "\n" + result.Snippet
		}
		metadata := map[string]any{
			"type":     metadataType,
			"position": i + 1,
		}
		if query != "" {
			metadata["query"] = query
		}
		if result.Site != "" {
			metadata["site"] = result.Site
		}
		docs = append(docs, &sdk.Document{
			URL:         result.URL,
			Title:       result.Title,
			Text:        text,
			UserID:      owner,
			Metadata:    metadata,
			SummaryOnly: true,
		})
	}
	return docs
}

// Preview renders parsed search results as an ordered list. An answer, when
// present, is rendered above the list because it is the page's main content.
func Preview(label, query string, results []Result) sdk.PreviewResult {
	return PreviewWithAnswer(label, query, "", results)
}

// PreviewWithAnswer renders an optional answer above the parsed search results.
func PreviewWithAnswer(label, query, answer string, results []Result) sdk.PreviewResult {
	if len(results) == 0 && answer == "" {
		return sdk.PreviewFallback(errors.New("no search results"))
	}
	var b strings.Builder
	b.WriteString("<h2>")
	if query != "" {
		fmt.Fprintf(&b, "%s search: %s", stdhtml.EscapeString(label), stdhtml.EscapeString(query))
	} else {
		fmt.Fprintf(&b, "%s search results", stdhtml.EscapeString(label))
	}
	b.WriteString("</h2>")
	if answer != "" {
		b.WriteString("<p>")
		b.WriteString(stdhtml.EscapeString(answer))
		b.WriteString("</p>")
	}
	if len(results) == 0 {
		return sdk.Previewed(sdk.PreviewResponse{Content: sanitizer.SanitizeHTML(b.String())})
	}
	b.WriteString("<ol>")
	for _, result := range results {
		fmt.Fprintf(&b, `<li><p><a href="%s">%s</a></p>`,
			stdhtml.EscapeString(result.URL), stdhtml.EscapeString(result.Title))
		if result.Site != "" {
			fmt.Fprintf(&b, "<p>%s</p>", stdhtml.EscapeString(result.Site))
		}
		if result.Snippet != "" {
			fmt.Fprintf(&b, "<p>%s</p>", stdhtml.EscapeString(result.Snippet))
		}
		b.WriteString("</li>")
	}
	b.WriteString("</ol>")
	return sdk.Previewed(sdk.PreviewResponse{Content: sanitizer.SanitizeHTML(b.String())})
}

// MaxResults returns the max_results option value, defaulting to
// DefaultMaxResults when the option is absent.
func MaxResults(options map[string]any) (int, error) {
	value, ok := options["max_results"]
	if !ok {
		return DefaultMaxResults, nil
	}
	var maxResults int
	switch value := value.(type) {
	case int:
		maxResults = value
	case float64:
		maxResults = int(value)
		if float64(maxResults) != value {
			return 0, errors.New("max_results must be an integer")
		}
	default:
		return 0, errors.New("max_results must be an integer")
	}
	if maxResults < 1 {
		return 0, errors.New("max_results must be at least 1")
	}
	return maxResults, nil
}

// ValidateOptions rejects unknown result page extractor options and invalid
// values.
// ValidateOptions rejects option keys other than max_results and the optional
// keys the caller supports, then validates max_results.
func ValidateOptions(options map[string]any, optionalKeys ...string) error {
	for key := range options {
		if key != "max_results" && !slices.Contains(optionalKeys, key) {
			return fmt.Errorf("unknown option %q", key)
		}
	}
	_, err := MaxResults(options)
	return err
}
