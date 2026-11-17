// SPDX-License-Identifier: AGPL-3.0-or-later

// Package notion provides an extractor for Notion pages on notion.so and notion.site domains.
package notion

import (
	"fmt"
	"html"
	"net/url"
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/asciimoo/nogon/server/extractor/sdk"
	"github.com/asciimoo/nogon/server/extractor/urlutil"
	"github.com/asciimoo/nogon/server/sanitizer"
)

// Notion serves an empty SPA shell over plain HTTP and only renders the page
// content client-side. The extractor therefore only produces output when the
// document was fetched with a JavaScript-rendering crawler backend (chromedp
// or bidi). When the rendered block tree is not present it returns
// AbortExtraction to prevent the fallback chain from indexing the SPA shell as
// a low-quality "Notion"-titled document, which would later duplicate against
// a properly rendered crawl in a different language index.
type NotionExtractor struct {
	sdk.ConfigSupport
}

func (e *NotionExtractor) Name() string {
	return "Notion"
}

func (e *NotionExtractor) Description() string {
	return "Extracts the title and block content of Notion pages on notion.so and *.notion.site. Requires a JavaScript-rendering crawler backend (chromedp or bidi) because Notion renders content client-side."
}

func (e *NotionExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

// Match accepts URLs on notion.so / www.notion.so and any *.notion.site
// subdomain (used for publicly shared pages). The path must have at least one
// non-empty segment so the workspace root and the login page are skipped.
func (e *NotionExtractor) Match(d *sdk.Document) bool {
	u, err := url.Parse(d.URL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "notion.so" && host != "www.notion.so" && !strings.HasSuffix(host, ".notion.site") {
		return false
	}
	return strings.Trim(u.Path, "/") != ""
}

// pageContent returns the selection that wraps a single Notion page's body.
// Notion renders the body inside a div with class notion-page-content (and
// sometimes notion-page-content-inner on shared pages). Returns an empty
// selection when the rendered structure is not present.
func pageContent(doc *goquery.Document) *goquery.Selection {
	if s := doc.Find(".notion-page-content").First(); s.Length() > 0 {
		return s
	}
	return doc.Find(".notion-page-content-inner").First()
}

// pageTitle returns the page's h1 title. On rendered Notion pages the title
// lives inside the first .notion-page-block above the content (and also
// inside .notion-page-content as the first child on shared pages).
func pageTitle(doc *goquery.Document, content *goquery.Selection) string {
	if t := strings.TrimSpace(doc.Find(".notion-page-block h1").First().Text()); t != "" {
		return t
	}
	if content != nil && content.Length() > 0 {
		if t := strings.TrimSpace(content.Find("h1").First().Text()); t != "" {
			return t
		}
	}
	if t := strings.TrimSpace(doc.Find("h1").First().Text()); t != "" {
		return t
	}
	return ""
}

func (e *NotionExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.ExtractFallback(err)
	}

	content := pageContent(doc)
	if content.Length() == 0 {
		return sdk.AbortExtraction(fmt.Errorf("notion page content not rendered"))
	}

	d.Title = pageTitle(doc, content)

	var b strings.Builder
	writeBlocksText(&b, content)
	d.Text = strings.TrimSpace(b.String())

	if d.Title == "" && d.Text == "" {
		return sdk.ExtractFallback(fmt.Errorf("no content found"))
	}
	return sdk.Extracted()
}

func (e *NotionExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.PreviewFallback(err)
	}

	content := pageContent(doc)
	if content.Length() == 0 {
		return sdk.AbortPreview(fmt.Errorf("notion page content not rendered"))
	}

	pu, err := url.Parse(d.URL)
	if err != nil {
		return sdk.PreviewFallback(err)
	}
	urlutil.RewriteURLs(content, pu)

	var b strings.Builder
	if title := pageTitle(doc, content); title != "" {
		fmt.Fprintf(&b, "<h1>%s</h1>\n", html.EscapeString(title))
	}
	writeBlocksHTML(&b, content)

	if b.Len() == 0 {
		return sdk.PreviewFallback(fmt.Errorf("no preview content"))
	}
	return sdk.Previewed(sdk.PreviewResponse{Content: sanitizer.SanitizeHTML(b.String())})
}

// writeBlocksText walks the immediate Notion block children of content and
// writes a plain-text representation, preserving heading prominence and list
// bullets so the indexed text reads naturally.
func writeBlocksText(b *strings.Builder, content *goquery.Selection) {
	content.Find("[class*='notion-'][class*='-block']").Each(func(_ int, s *goquery.Selection) {
		// Skip the page title block — pageTitle already captured it.
		if hasBlockClass(s, "notion-page-block") {
			return
		}
		// Skip nested blocks; the parent walk handles their text.
		if s.ParentsFiltered("[class*='notion-'][class*='-block']").Length() > 0 {
			return
		}
		text := strings.TrimSpace(s.Text())
		if text == "" {
			return
		}
		switch {
		case hasBlockClass(s, "notion-header-block"),
			hasBlockClass(s, "notion-sub_header-block"),
			hasBlockClass(s, "notion-sub_sub_header-block"):
			fmt.Fprintf(b, "\n\n%s\n", text)
		case hasBlockClass(s, "notion-bulleted_list-block"),
			hasBlockClass(s, "notion-to_do-block"):
			fmt.Fprintf(b, "\n* %s", text)
		case hasBlockClass(s, "notion-numbered_list-block"):
			fmt.Fprintf(b, "\n- %s", text)
		case hasBlockClass(s, "notion-quote-block"):
			fmt.Fprintf(b, "\n\n> %s", text)
		default:
			fmt.Fprintf(b, "\n\n%s", text)
		}
	})
}

// writeBlocksHTML renders the Notion block tree as semantic HTML for the
// preview pane. It collapses Notion's deeply nested presentational divs into
// plain headings, paragraphs, lists, blockquotes and code blocks.
func writeBlocksHTML(b *strings.Builder, content *goquery.Selection) {
	content.Find("[class*='notion-'][class*='-block']").Each(func(_ int, s *goquery.Selection) {
		if hasBlockClass(s, "notion-page-block") {
			return
		}
		if s.ParentsFiltered("[class*='notion-'][class*='-block']").Length() > 0 {
			return
		}
		switch {
		case hasBlockClass(s, "notion-header-block"):
			writeTag(b, "h2", s)
		case hasBlockClass(s, "notion-sub_header-block"):
			writeTag(b, "h3", s)
		case hasBlockClass(s, "notion-sub_sub_header-block"):
			writeTag(b, "h4", s)
		case hasBlockClass(s, "notion-bulleted_list-block"),
			hasBlockClass(s, "notion-to_do-block"):
			fmt.Fprintf(b, "<ul><li>%s</li></ul>", html.EscapeString(strings.TrimSpace(s.Text())))
		case hasBlockClass(s, "notion-numbered_list-block"):
			fmt.Fprintf(b, "<ol><li>%s</li></ol>", html.EscapeString(strings.TrimSpace(s.Text())))
		case hasBlockClass(s, "notion-quote-block"):
			writeTag(b, "blockquote", s)
		case hasBlockClass(s, "notion-code-block"):
			fmt.Fprintf(b, "<pre><code>%s</code></pre>", html.EscapeString(s.Text()))
		case hasBlockClass(s, "notion-divider-block"):
			b.WriteString("<hr>")
		case hasBlockClass(s, "notion-image-block"):
			if src, ok := s.Find("img").First().Attr("src"); ok {
				fmt.Fprintf(b, `<p><img src="%s" alt=""></p>`, html.EscapeString(src))
			}
		default:
			writeTag(b, "p", s)
		}
	})
}

// writeTag writes a paragraph-style block whose visible text is rendered
// inside tag. Empty blocks (e.g. placeholder dividers) are dropped.
func writeTag(b *strings.Builder, tag string, s *goquery.Selection) {
	text := strings.TrimSpace(s.Text())
	if text == "" {
		return
	}
	fmt.Fprintf(b, "<%s>%s</%s>", tag, html.EscapeString(text), tag)
}

// hasBlockClass reports whether s carries the given Notion block class
func hasBlockClass(s *goquery.Selection, class string) bool {
	c, _ := s.Attr("class")
	return slices.Contains(strings.Fields(c), class)
}
