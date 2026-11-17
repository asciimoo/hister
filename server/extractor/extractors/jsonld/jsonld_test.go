package jsonld

import (
	"encoding/json"
	"testing"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/extractor/sdk"
)

func extract(t *testing.T, html string) *document.Document {
	t.Helper()
	d := &document.Document{URL: "https://example.com/", HTML: html}
	e := &JSONLDExtractor{}
	state, err := e.Extract(d).Unpack()
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("Extract decision = %v, want Success", state)
	}
	return d
}

func jsonldNodes(t *testing.T, d *document.Document) []map[string]any {
	t.Helper()
	raw, _ := d.Metadata["jsonld"].(string)
	if raw == "" {
		return nil
	}
	var nodes []map[string]any
	if err := json.Unmarshal([]byte(raw), &nodes); err != nil {
		t.Fatalf("jsonld metadata is not valid JSON: %v", err)
	}
	return nodes
}

func TestWikipediaArticle(t *testing.T) {
	const h = `<html><head><script type="application/ld+json">{
		"@context": "https://schema.org",
		"@type": "Article",
		"name": "Kristi Noem",
		"headline": "Kristi Noem",
		"author": {"@type": "Organization", "name": "Contributors to Wikimedia projects"},
		"datePublished": "2010-01-01T00:00:00Z",
		"dateModified": "2026-04-01T00:00:00Z",
		"image": "https://upload.wikimedia.org/noem.jpg"
	}</script></head><body></body></html>`

	d := extract(t, h)
	checks := map[string]string{
		"type":     "Article",
		"headline": "Kristi Noem",
	}
	for k, want := range checks {
		if got, _ := d.Metadata[k].(string); got != want {
			t.Errorf("Metadata[%q] = %q, want %q", k, got, want)
		}
	}
	// Overlap fields (author/description/image/dates) are readability's
	// responsibility now; jsonld should not be writing them.
	for _, k := range []string{"author", "description", "image", "published", "modified"} {
		if _, ok := d.Metadata[k]; ok {
			t.Errorf("Metadata[%q] should not be set by jsonld", k)
		}
	}
	if nodes := jsonldNodes(t, d); len(nodes) != 1 {
		t.Errorf("expected 1 flattened node, got %d", len(nodes))
	}
}

func TestYoastGraph(t *testing.T) {
	const h = `<html><head><script type="application/ld+json">{
		"@context": "https://schema.org",
		"@graph": [
			{"@type": "WebPage", "name": "About Us", "description": "The about page"},
			{"@type": "BreadcrumbList", "itemListElement": []},
			{"@type": "Organization", "name": "ACME"}
		]
	}</script></head></html>`

	d := extract(t, h)
	nodes := jsonldNodes(t, d)
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	if got, _ := d.Metadata["type"].(string); got != "WebPage" {
		t.Errorf("type = %q, want WebPage", got)
	}
	if got, _ := d.Metadata["headline"].(string); got != "About Us" {
		t.Errorf("headline = %q, want About Us", got)
	}
}

func TestMultipleScriptTags(t *testing.T) {
	const h = `<html><head>
		<script type="application/ld+json">{"@type": "Organization", "name": "ACME"}</script>
		<script type="application/ld+json">{"@type": "Article", "headline": "Hello"}</script>
	</head></html>`

	d := extract(t, h)
	nodes := jsonldNodes(t, d)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	// Article is preferred over Organization.
	if got, _ := d.Metadata["type"].(string); got != "Article" {
		t.Errorf("type = %q, want Article", got)
	}
}

func TestArrayForm(t *testing.T) {
	const h = `<html><head><script type="application/ld+json">[
		{"@type": "Person", "name": "Alice"},
		{"@type": "Person", "name": "Bob"}
	]</script></head></html>`

	d := extract(t, h)
	nodes := jsonldNodes(t, d)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestMalformedJSONContinues(t *testing.T) {
	const h = `<html><head>
		<script type="application/ld+json">{not valid json</script>
		<script type="application/ld+json">{"@type": "Article", "headline": "Survives"}</script>
	</head></html>`

	d := extract(t, h)
	if got, _ := d.Metadata["headline"].(string); got != "Survives" {
		t.Errorf("headline = %q, want Survives", got)
	}
	nodes := jsonldNodes(t, d)
	if len(nodes) != 1 {
		t.Errorf("expected 1 node (malformed skipped), got %d", len(nodes))
	}
}

func TestNoJSONLD(t *testing.T) {
	d := &document.Document{URL: "https://example.com/", HTML: "<html><body><p>hi</p></body></html>"}
	e := &JSONLDExtractor{}
	state, err := e.Extract(d).Unpack()
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if state != sdk.ExtractorFallback {
		t.Fatalf("decision = %v, want Fallback", state)
	}
	if _, ok := d.Metadata["jsonld"]; ok {
		t.Errorf("Metadata[jsonld] should not be set, got %v", d.Metadata)
	}
}

func TestMatchSkipsPagesWithoutJSONLD(t *testing.T) {
	e := &JSONLDExtractor{}
	if e.Match(&document.Document{HTML: ""}) {
		t.Error("Match should be false for empty HTML")
	}
	if e.Match(&document.Document{HTML: "<html><body><p>hi</p></body></html>"}) {
		t.Error("Match should be false for HTML without the ld+json marker")
	}
	if !e.Match(&document.Document{HTML: `<script type="application/ld+json">{}</script>`}) {
		t.Error("Match should be true when the marker is present")
	}
}

func TestSanitizeHeadlineStripsTagsAndDecodesEntities(t *testing.T) {
	// A real HTML document must escape </script> inside the blob; browsers
	// and golang.org/x/net/html both end the enclosing <script> at </script>
	// regardless of JSON context.
	const h = `<script type="application/ld+json">{
		"@type": "Article",
		"headline": "Smith &amp; Jones: <i>an unlikely<\/i> story",
		"description": "<script>alert(1)<\/script>plain",
		"author": {"@type": "Person", "name": "John O&#39;Brien"}
	}</script>`

	d := extract(t, h)
	if got, _ := d.Metadata["headline"].(string); got != "Smith & Jones: an unlikely story" {
		t.Errorf("headline = %q", got)
	}
}

func TestRawJSONLDDumpIsDeepSanitized(t *testing.T) {
	const h = `<script type="application/ld+json">{
		"@type": "Article",
		"headline": "Smith &amp; Jones",
		"author": {"@type": "Person", "name": "<b>Jane<\/b>"},
		"keywords": ["<i>go<\/i>", "hister"]
	}</script>`

	d := extract(t, h)
	nodes := jsonldNodes(t, d)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if got, _ := n["headline"].(string); got != "Smith & Jones" {
		t.Errorf("nodes[0].headline = %q", got)
	}
	author, _ := n["author"].(map[string]any)
	if got, _ := author["name"].(string); got != "Jane" {
		t.Errorf("nodes[0].author.name = %q", got)
	}
	keywords, _ := n["keywords"].([]any)
	if len(keywords) != 2 || keywords[0] != "go" || keywords[1] != "hister" {
		t.Errorf("nodes[0].keywords = %v", keywords)
	}
	// @-prefixed structural keys are preserved verbatim.
	if got, _ := n["@type"].(string); got != "Article" {
		t.Errorf("nodes[0].@type = %q", got)
	}
}
