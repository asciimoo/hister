// SPDX-License-Identifier: AGPL-3.0-or-later

package serputil

import (
	"strings"
	"testing"

	"github.com/asciimoo/hister/server/extractor/sdk"
)

func TestCanonicalURL(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		want   string
		wantOK bool
	}{
		{
			name:   "keeps plain URL",
			raw:    "https://example.org/page",
			want:   "https://example.org/page",
			wantOK: true,
		},
		{
			name:   "drops fragment",
			raw:    "https://example.org/page#section",
			want:   "https://example.org/page",
			wantOK: true,
		},
		{
			name:   "drops tracking parameters",
			raw:    "https://example.org/page?id=1&utm_source=serp&utm_medium=organic",
			want:   "https://example.org/page?id=1",
			wantOK: true,
		},
		{
			name: "rejects relative URL",
			raw:  "/search?q=test",
		},
		{
			name: "rejects non-web scheme",
			raw:  "ftp://example.org/page",
		},
		{
			name: "rejects empty host",
			raw:  "https:///page",
		},
		{
			name: "rejects userinfo",
			raw:  "https://user:pass@example.org/page",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := CanonicalURL(test.raw)
			if ok != test.wantOK {
				t.Fatalf("CanonicalURL(%q) ok = %v, want %v", test.raw, ok, test.wantOK)
			}
			if got != test.want {
				t.Fatalf("CanonicalURL(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

func TestDisplayedHost(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "https://github.com › asciimoo › hister", want: "github.com"},
		{raw: "en.wikipedia.org › wiki › Web_scraping", want: "en.wikipedia.org"},
		{raw: "https://www.example.org/page", want: "example.org"},
		{raw: "", want: ""},
	}
	for _, test := range tests {
		if got := DisplayedHost(test.raw); got != test.want {
			t.Errorf("DisplayedHost(%q) = %q, want %q", test.raw, got, test.want)
		}
	}
}

func TestClipSnippet(t *testing.T) {
	if got := ClipSnippet("  one\n\t two  "); got != "one two" {
		t.Errorf("ClipSnippet collapsed to %q, want %q", got, "one two")
	}
	long := strings.Repeat("word ", 200)
	got := ClipSnippet(long)
	if len([]rune(got)) > maxSnippetLength+1 {
		t.Errorf("clipped snippet has %d runes, want at most %d", len([]rune(got)), maxSnippetLength+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("clipped snippet %q misses the ellipsis", got)
	}
	if strings.HasSuffix(got, " …") {
		t.Errorf("clipped snippet %q keeps a trailing space", got)
	}
}

func TestCollectorDropsDuplicatesAndEmptyResults(t *testing.T) {
	var c Collector
	c.Add(Result{Title: "First", URL: "https://example.org/a"})
	c.Add(Result{Title: "Duplicate", URL: "https://example.org/a"})
	c.Add(Result{URL: "https://example.org/b"})
	c.Add(Result{Title: "No URL"})
	if c.Len() != 1 {
		t.Fatalf("collected %d results, want 1", c.Len())
	}
	if got := c.Results()[0].Title; got != "First" {
		t.Fatalf("kept title %q, want the first entry", got)
	}
}

func TestDocuments(t *testing.T) {
	results := []Result{
		{Title: "First", URL: "https://example.org/a", Snippet: "First snippet.", Site: "example.org"},
		{Title: "Second", URL: "https://example.org/b"},
	}
	docs := Documents(results, 42, "test_result", "query text", 5)
	if len(docs) != 2 {
		t.Fatalf("documents = %d, want 2", len(docs))
	}
	first := docs[0]
	if first.URL != "https://example.org/a" || first.Title != "First" {
		t.Errorf("unexpected first document %+v", first)
	}
	if first.Text != "First\nFirst snippet." {
		t.Errorf("first text = %q", first.Text)
	}
	if first.UserID != 42 {
		t.Errorf("owner = %d, want 42", first.UserID)
	}
	if !first.SummaryOnly {
		t.Error("document is not marked as a summary")
	}
	if first.Metadata["type"] != "test_result" {
		t.Errorf("metadata type = %v", first.Metadata["type"])
	}
	if first.Metadata["query"] != "query text" {
		t.Errorf("metadata query = %v", first.Metadata["query"])
	}
	if first.Metadata["position"] != 1 {
		t.Errorf("metadata position = %v", first.Metadata["position"])
	}
	if first.Metadata["site"] != "example.org" {
		t.Errorf("metadata site = %v", first.Metadata["site"])
	}
	if docs[1].Text != "Second" {
		t.Errorf("text without snippet = %q", docs[1].Text)
	}
	if _, found := docs[1].Metadata["site"]; found {
		t.Error("empty site is stored in the metadata")
	}

	limited := Documents(results, 42, "test_result", "", 1)
	if len(limited) != 1 {
		t.Errorf("limited documents = %d, want 1", len(limited))
	}
	if _, found := limited[0].Metadata["query"]; found {
		t.Error("empty query is stored in the metadata")
	}
}

func TestPreviewEscapesContent(t *testing.T) {
	results := []Result{{
		Title:   `<script>alert(1)</script>`,
		URL:     "https://example.org/?a=1&b=2",
		Snippet: "A <b>bold</b> claim.",
		Site:    "example.org",
	}}
	response, decision, err := Preview("Test", "query", results).Unpack()
	if decision != sdk.ExtractorSuccess {
		t.Fatalf("Preview decision = %v (%v), want success", decision, err)
	}
	content := response.Content
	if strings.Contains(content, "<script") {
		t.Errorf("preview keeps a script tag: %q", content)
	}
	if !strings.Contains(content, "Test search: query") {
		t.Errorf("preview misses the heading: %q", content)
	}
	if !strings.Contains(content, `href="https://example.org/?a=1&amp;b=2"`) {
		t.Errorf("preview misses the escaped link: %q", content)
	}
	if !strings.Contains(content, "<ol>") || !strings.Contains(content, "<li>") {
		t.Errorf("preview is not rendered as a list: %q", content)
	}
}

func TestMaxResultsAndValidateOptions(t *testing.T) {
	if got, err := MaxResults(nil); err != nil || got != DefaultMaxResults {
		t.Fatalf("MaxResults(nil) = %d, %v", got, err)
	}
	if got, err := MaxResults(map[string]any{"max_results": 5}); err != nil || got != 5 {
		t.Fatalf("MaxResults(int) = %d, %v", got, err)
	}
	if got, err := MaxResults(map[string]any{"max_results": float64(7)}); err != nil || got != 7 {
		t.Fatalf("MaxResults(float64) = %d, %v", got, err)
	}
	for _, value := range []any{0, -1, 1.5, "5", true} {
		if _, err := MaxResults(map[string]any{"max_results": value}); err == nil {
			t.Errorf("MaxResults accepted %v", value)
		}
	}
	if err := ValidateOptions(map[string]any{"max_results": 5}); err != nil {
		t.Errorf("ValidateOptions rejected max_results: %v", err)
	}
	for _, options := range []map[string]any{
		{"other": 1},
		{"max_results": 5, "other": 1},
		{"max_results": 0},
	} {
		if err := ValidateOptions(options); err == nil {
			t.Errorf("ValidateOptions accepted %v", options)
		}
	}
}
