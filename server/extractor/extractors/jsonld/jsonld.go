// Package jsonld provides an extractor that parses application/ld+json
// script tags and writes normalized metadata to the document.
package jsonld

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/rs/zerolog/log"
	"golang.org/x/net/html"

	"github.com/asciimoo/nogon/server/extractor/sdk"
	"github.com/asciimoo/nogon/server/sanitizer"
)

// scriptTypeMarker is the substring we look for to cheaply rule out pages
// that cannot contain JSON-LD before running the tokenizer.
const scriptTypeMarker = "application/ld+json"

// JSONLDExtractor extracts schema.org metadata from JSON-LD script tags.
type JSONLDExtractor struct {
	sdk.ConfigSupport
}

// Name returns the extractor's identifier.
func (e *JSONLDExtractor) Name() string {
	return "JSONLD"
}

// Description returns a short summary of what this extractor does.
func (e *JSONLDExtractor) Description() string {
	return "Parses application/ld+json script tags and stores normalized schema.org metadata on the document."
}

func (e *JSONLDExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Enrich: true}
}

// Match reports whether the document's HTML plausibly contains a JSON-LD
// script. strings.Contains is orders of magnitude cheaper than tokenizing
// the whole HTML, so pages without JSON-LD skip the extractor entirely.
func (e *JSONLDExtractor) Match(d *sdk.Document) bool {
	return len(d.HTML) > 0 && strings.Contains(d.HTML, scriptTypeMarker)
}

// preferredTypes lists @type values that make a node a good source for
// normalized metadata, in descending priority order.
var preferredTypes = []string{
	"Article", "NewsArticle", "BlogPosting",
	"WebPage", "Product", "Recipe",
	"VideoObject", "Event", "Person", "Organization",
}

// Extract parses every application/ld+json script block, flattens @graph/array
// wrappers and writes normalized fields to d.Metadata.
func (e *JSONLDExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	blobs := findJSONLDBlobs(d.HTML)
	if len(blobs) == 0 {
		return sdk.ExtractFallback(nil)
	}

	var nodes []map[string]any
	for _, blob := range blobs {
		var parsed any
		if err := json.Unmarshal([]byte(blob), &parsed); err != nil {
			log.Debug().Err(err).Str("URL", d.URL).Msg("Failed to parse JSON-LD blob")
			continue
		}
		nodes = append(nodes, flatten(parsed)...)
	}
	if len(nodes) == 0 {
		return sdk.ExtractFallback(nil)
	}

	sanitizeNodes(nodes)

	if d.Metadata == nil {
		d.Metadata = make(map[string]any)
	}
	// Stored as a JSON string so the nested structure survives bleve's
	// text-mapped metadata round-trip. Preview consumers unmarshal it.
	if b, err := json.Marshal(nodes); err == nil {
		d.Metadata["jsonld"] = string(b)
	}

	// Readability already extracts author/description/image/dates from
	// JSON-LD + OpenGraph + meta tags and runs after us, so writing those
	// here would just be overwritten. Keep the two fields readability does
	// not expose: schema.org @type for classification, and headline (which
	// is distinct from the HTML <title> readability uses).
	best := pickBest(nodes)
	setString(d.Metadata, "type", sanitizer.SanitizeText(typeString(best)))
	setString(d.Metadata, "headline", sanitizer.SanitizeText(firstString(best, "headline", "name")))

	return sdk.Extracted()
}

// Preview is not implemented; Readability/Default handle rendering.
func (e *JSONLDExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	return sdk.PreviewFallback(nil)
}

// findJSONLDBlobs returns the raw text content of every
// <script type="application/ld+json"> element in rawHTML.
func findJSONLDBlobs(rawHTML string) []string {
	z := html.NewTokenizer(strings.NewReader(rawHTML))
	var blobs []string
	var buf bytes.Buffer
	capturing := false
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) {
				return blobs
			}
			return blobs
		case html.StartTagToken:
			tn, hasAttr := z.TagName()
			if string(tn) != "script" || !hasAttr {
				continue
			}
			if isJSONLDScript(z) {
				capturing = true
				buf.Reset()
			}
		case html.TextToken:
			if capturing {
				buf.Write(z.Text())
			}
		case html.EndTagToken:
			if !capturing {
				continue
			}
			tn, _ := z.TagName()
			if string(tn) == "script" {
				s := strings.TrimSpace(buf.String())
				if s != "" {
					blobs = append(blobs, s)
				}
				capturing = false
				buf.Reset()
			}
		}
	}
}

func isJSONLDScript(z *html.Tokenizer) bool {
	for {
		key, val, more := z.TagAttr()
		if string(key) == "type" && strings.EqualFold(strings.TrimSpace(string(val)), "application/ld+json") {
			return true
		}
		if !more {
			return false
		}
	}
}

// flatten turns an arbitrary JSON-LD payload into a flat slice of object
// nodes, unwrapping @graph wrappers and top-level arrays.
func flatten(v any) []map[string]any {
	switch t := v.(type) {
	case map[string]any:
		if g, ok := t["@graph"]; ok {
			return flatten(g)
		}
		return []map[string]any{t}
	case []any:
		var out []map[string]any
		for _, item := range t {
			out = append(out, flatten(item)...)
		}
		return out
	}
	return nil
}

// pickBest returns the first node whose @type matches one of preferredTypes,
// falling back to the first node.
func pickBest(nodes []map[string]any) map[string]any {
	for _, want := range preferredTypes {
		for _, n := range nodes {
			if typeMatches(n["@type"], want) {
				return n
			}
		}
	}
	return nodes[0]
}

func typeMatches(v any, want string) bool {
	switch t := v.(type) {
	case string:
		return t == want
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

func typeString(n map[string]any) string {
	switch t := n["@type"].(type) {
	case string:
		return t
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func firstString(n map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := n[k].(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				return s
			}
		}
	}
	return ""
}

func setString(m map[string]any, key, value string) {
	if value == "" {
		return
	}
	m[key] = value
}

// sanitizeNodes walks the parsed JSON-LD tree in place and runs every
// string leaf through sanitizer.SanitizeText so the raw dump stored at
// d.Metadata["jsonld"] cannot carry untrusted HTML into downstream
// consumers. @-prefixed keys (@context, @type, @id) are left untouched
// because they are structural identifiers, not free-form text.
func sanitizeNodes(nodes []map[string]any) {
	for _, n := range nodes {
		sanitizeMap(n)
	}
}

func sanitizeMap(m map[string]any) {
	for k, v := range m {
		if strings.HasPrefix(k, "@") {
			continue
		}
		m[k] = sanitizeValue(v)
	}
}

func sanitizeValue(v any) any {
	switch t := v.(type) {
	case string:
		return sanitizer.SanitizeText(t)
	case map[string]any:
		sanitizeMap(t)
		return t
	case []any:
		for i, item := range t {
			t[i] = sanitizeValue(item)
		}
		return t
	}
	return v
}
