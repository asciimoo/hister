// SPDX-License-Identifier: AGPL-3.0-or-later

// Package extractor_template is a starting point for implementing a new Nogon
// extractor. To use it:
//
//  1. Copy this directory to server/extractor/extractors/<myname>/.
//  2. Rename the directory (remove the leading "_" so Go picks it up).
//  3. Change the package declaration below to match the new directory name.
//  4. Rename TemplateExtractor to something descriptive.
//  5. Update matchURLPrefix (and the Match function) for your target site.
//  6. Implement Extract and Preview.
//  7. Register the extractor in DefaultExtractors in registry.go.
//
// The directory name starts with "_" so the Go toolchain ignores it during
// normal builds. That means this file is never compiled as-is, but it is valid
// Go so editors and linters can still analyse it.
package extractor_template

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/asciimoo/nogon/server/extractor/sdk"
	"github.com/asciimoo/nogon/server/sanitizer"
)

// matchURLPrefix is the URL prefix this extractor targets.
// Adjust to match your site, e.g. "https://example.com/articles/".
const matchURLPrefix = "https://example.com/"

// TemplateExtractor extracts content from example.com pages.
// Rename this type to reflect the site or content type you are targeting.
type TemplateExtractor struct {
	// The zero value is enabled and rejects every option key. Initialize this
	// field with sdk.NewConfigSupport when the extractor has custom defaults.
	sdk.ConfigSupport
}

// Keep this assertion so missing or mismatched SDK methods fail compilation.
var _ sdk.Extractor = (*TemplateExtractor)(nil)

// Name returns a short human-readable identifier used in log messages and as
// the YAML config key (lowercased). "MyExtractor" → yaml key "myextractor".
func (e *TemplateExtractor) Name() string {
	return "Template"
}

// Description returns a short summary of what this extractor does.
// It is surfaced by the /api/config endpoint.
func (e *TemplateExtractor) Description() string {
	return "Template extractor. Replace this with a description of what your extractor does."
}

// Capabilities declares which phases this extractor participates in. Set
// Enrich when it only annotates documents and should never select their body.
func (e *TemplateExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

// Match reports whether this extractor should handle the given document.
// It is called for every document before Extract or Preview; keep it fast.
//
// Common patterns:
//
//	URL prefix:  strings.HasPrefix(d.URL, matchURLPrefix)
//	Exact domain: d.Domain == "example.com"
//	Path suffix:  strings.HasSuffix(d.URL, ".rss")
func (e *TemplateExtractor) Match(d *sdk.Document) bool {
	return strings.HasPrefix(d.URL, matchURLPrefix) && len(d.URL) > len(matchURLPrefix)
}

// Extract rewrites the document before it is added to the search index.
// Populate d.Title and d.Text with the best searchable representation of the
// content. Both fields are stored in Bleve and searched by default.
//
// Return Extracted on success, ExtractFallback when the next content
// extractor should try, or AbortExtraction for a fatal error.
//
// Optional features:
//   - Set d.Metadata["author"], d.Metadata["published"], etc. to surface
//     structured fields in the preview panel header.
//   - Call d.SetFaviconURL(url) to override the default favicon discovery.
//   - Append to d.ExtraDocuments to queue additional URLs for indexing
//     (e.g. paginated content, linked sub-pages).
//   - Set d.SkipIndexing = true when the current document should not be
//     indexed itself and only its ExtraDocuments matter.
func (e *TemplateExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		// Return ExtractFallback so the next extractor can try. Use
		// AbortExtraction only when further processing would be unsafe.
		return sdk.ExtractFallback(err)
	}

	// --- Title -----------------------------------------------------------
	d.Title = strings.TrimSpace(doc.Find("h1").First().Text())

	// --- Body text -------------------------------------------------------
	var b strings.Builder
	doc.Find("article").Each(func(_ int, s *goquery.Selection) {
		if text := strings.TrimSpace(s.Text()); text != "" {
			b.WriteString(text)
			b.WriteString("\n\n")
		}
	})
	d.Text = strings.TrimSpace(b.String())

	if d.Title == "" && d.Text == "" {
		return sdk.ExtractFallback(fmt.Errorf("no content found"))
	}

	// --- Metadata (optional) --------------------------------------------
	// Fields written here appear in the preview panel header. Recognised keys:
	// "author", "published", "modified", "description", "image", "site_name".
	if d.Metadata == nil {
		d.Metadata = make(map[string]any)
	}
	if author := strings.TrimSpace(doc.Find(".author").First().Text()); author != "" {
		d.Metadata["author"] = author
	}

	// --- Extra documents (optional) -------------------------------------
	// Queue related URLs for indexing. Each extra document is processed and
	// indexed independently after the current document.
	//
	//   doc.Find("a.related-link").Each(func(_ int, s *goquery.Selection) {
	//       if href, ok := s.Attr("href"); ok {
	//           d.ExtraDocuments = append(d.ExtraDocuments, &sdk.Document{
	//               URL:    resolveAbsoluteURL(d.URL, href),
	//               UserID: d.UserID,
	//           })
	//       }
	//   })

	return sdk.Extracted()
}

// Preview returns a rendered representation of the document for the preview
// panel. Content is typically sanitized HTML. The optional Template field
// selects a custom Svelte front-end template; leave it empty for the default.
//
// If you do not need a custom preview, return PreviewFallback and the generic
// readability or basic extractor will provide one automatically.
func (e *TemplateExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.PreviewFallback(err)
	}

	var b strings.Builder

	// --- Title -----------------------------------------------------------
	if title := strings.TrimSpace(doc.Find("h1").First().Text()); title != "" {
		fmt.Fprintf(&b, "<h2>%s</h2>\n", title)
	}

	// --- Body ------------------------------------------------------------
	if body, err := doc.Find("article").First().Html(); err == nil && strings.TrimSpace(body) != "" {
		b.WriteString(body)
	}

	if b.Len() == 0 {
		return sdk.PreviewFallback(fmt.Errorf("no preview content"))
	}

	// Always sanitize HTML before returning it to strip scripts, event
	// handlers, and other potentially unsafe markup.
	return sdk.Previewed(sdk.PreviewResponse{
		Content: sanitizer.SanitizeHTML(b.String()),
		// Template: "my_template", // leave empty to use the default layout
	})
}
