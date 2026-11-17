// SPDX-License-Identifier: AGPL-3.0-or-later

// Package markdown provides an extractor for locally indexed Markdown files.
package markdown

import (
	"strings"

	"github.com/asciimoo/nogon/server/extractor/sdk"
	"github.com/asciimoo/nogon/server/sanitizer"
)

// MarkdownExtractor serves previews for locally indexed Markdown files.
// File extraction renders the source to HTML and stores it in doc.HTML, so
// Preview only needs to sanitize that HTML.
type MarkdownExtractor struct {
	sdk.ConfigSupport
}

func (e *MarkdownExtractor) Name() string { return "Markdown" }

func (e *MarkdownExtractor) Description() string {
	return "Renders locally indexed Markdown files (.md, .markdown) as HTML for preview."
}

func (e *MarkdownExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Preview: true}
}

// Match returns true for file:// URLs with a .md or .markdown extension.
func (e *MarkdownExtractor) Match(d *sdk.Document) bool {
	if !strings.HasPrefix(d.URL, "file://") {
		return false
	}
	lower := strings.ToLower(d.URL)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown")
}

// Extract is a no-op: source parsing is handled by files/extract.
func (e *MarkdownExtractor) Extract(_ *sdk.Document) sdk.ExtractResult {
	return sdk.ExtractFallback(nil)
}

// Preview sanitizes the rendered HTML stored in doc.HTML.
func (e *MarkdownExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	if d.HTML == "" {
		return sdk.PreviewFallback(nil)
	}
	return sdk.Previewed(sdk.PreviewResponse{Content: sanitizer.SanitizeHTML(d.HTML)})
}
