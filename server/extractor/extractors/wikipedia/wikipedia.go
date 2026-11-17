// Package wikipedia provides an extractor for Wikipedia article pages.
package wikipedia

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/asciimoo/nogon/server/extractor/sdk"
	"github.com/asciimoo/nogon/server/extractor/urlutil"
	"github.com/asciimoo/nogon/server/sanitizer"
)

// WikipediaExtractor extracts content from Wikipedia article pages.
type WikipediaExtractor struct {
	sdk.ConfigSupport
}

func (e *WikipediaExtractor) Name() string {
	return "Wikipedia"
}

func (e *WikipediaExtractor) Description() string {
	return "Extracts article content, infoboxes, tables, and metadata from Wikipedia pages."
}

func (e *WikipediaExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

func (e *WikipediaExtractor) Match(d *sdk.Document) bool {
	return isWikipediaURL(d.URL)
}

// Extract populates the document's Title, Text, and Metadata from Wikipedia
// article HTML so that article content, infoboxes, and tables are searchable.
func (e *WikipediaExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.ExtractFallback(err)
	}

	d.Title = articleTitle(doc)

	content := articleContent(doc)
	if content.Length() == 0 {
		return sdk.ExtractFallback(fmt.Errorf("no mw-parser-output found"))
	}

	if d.Metadata == nil {
		d.Metadata = make(map[string]any)
	}
	d.Metadata["type"] = "Article"
	if desc := shortDescription(content); desc != "" {
		d.Metadata["description"] = desc
	}
	if cats := categories(doc); len(cats) > 0 {
		d.Metadata["categories"] = strings.Join(cats, ", ")
	}

	// Clone content so removals don't affect the original parse tree.
	clone := cloneSelection(content)
	removeNoise(clone)

	var b strings.Builder
	if d.Title != "" {
		b.WriteString(d.Title)
		b.WriteByte('\n')
	}
	writeInfoboxText(&b, content)
	writeArticleText(&b, clone)

	d.Text = strings.TrimSpace(b.String())
	if d.Text == "" && d.Title == "" {
		return sdk.ExtractFallback(fmt.Errorf("no content found"))
	}
	return sdk.Extracted()
}

// Preview returns sanitized HTML of the article body with navigation and
// reference noise stripped, inline styles applied for rich rendering, and
// relative URLs rewritten to absolute.
func (e *WikipediaExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.PreviewFallback(err)
	}

	content := articleContent(doc)
	if content.Length() == 0 {
		return sdk.PreviewFallback(fmt.Errorf("no mw-parser-output found"))
	}

	base, _ := url.Parse(d.URL)

	removePreviewNoise(content)
	replaceVideos(content)
	styleContent(content)
	urlutil.RewriteURLs(content, base)

	html, err := content.Html()
	if err != nil {
		return sdk.PreviewFallback(err)
	}
	return sdk.Previewed(sdk.PreviewResponse{Content: sanitizer.SanitizeTrustedHTML(html)})
}

// Non-content Wikipedia namespaces to exclude from extraction.
var excludedNamespaces = []string{
	"Special:", "Talk:", "User:", "User_talk:",
	"File:", "MediaWiki:", "Template:", "Help:", "Category:",
	"Portal:", "Draft:", "Module:", "Wikipedia_talk:",
	"Template_talk:", "Help_talk:", "Category_talk:",
}

// isWikipediaURL returns true for any *.wikipedia.org/wiki/ article URL,
// excluding non-content pages (Special:, Talk:, User:, etc.).
// The Wikipedia: namespace is allowed because it contains content pages
// like Wikipedia:Unusual_articles that benefit from rich extraction.
func isWikipediaURL(u string) bool {
	_, rest, found := strings.Cut(u, "wikipedia.org/wiki/")
	if !found || rest == "" {
		return false
	}
	return !slices.ContainsFunc(excludedNamespaces, func(ns string) bool {
		return strings.HasPrefix(rest, ns)
	})
}

// articleContent finds the main article content div. Wikipedia pages may have
// multiple .mw-parser-output divs (for indicators, etc.); the article body
// lives inside #mw-content-text.
func articleContent(doc *goquery.Document) *goquery.Selection {
	if s := doc.Find("#mw-content-text .mw-parser-output").First(); s.Length() > 0 {
		return s
	}
	return doc.Find(".mw-parser-output").First()
}

// articleTitle extracts the article title from the firstHeading element.
func articleTitle(doc *goquery.Document) string {
	title := doc.Find("#firstHeading .mw-page-title-main").First().Text()
	if title == "" {
		title = doc.Find("#firstHeading").First().Text()
	}
	return strings.TrimSpace(title)
}

// shortDescription extracts the hidden short description used by Wikipedia.
func shortDescription(content *goquery.Selection) string {
	return strings.TrimSpace(content.Find(".shortdescription").First().Text())
}

// categories extracts the list of categories from the catlinks section.
func categories(doc *goquery.Document) []string {
	var cats []string
	doc.Find("#catlinks .mw-normal-catlinks li a").Each(func(_ int, s *goquery.Selection) {
		if t := strings.TrimSpace(s.Text()); t != "" {
			cats = append(cats, t)
		}
	})
	return cats
}

// Noise selectors shared by both indexing and preview removal.
var noiseBase = []string{
	".navbox", ".navbar", ".toc", ".mw-editsection", ".noprint",
	"style", "script", ".mw-empty-elt", ".portal-bar",
	".sistersitebox", ".authority-control", ".shortdescription",
}

// Extra selectors stripped only during indexing (references, hatnotes, etc.).
var noiseIndexOnly = []string{
	".mw-references-wrap", "ol.references", ".reflist", ".sidebar",
	".external.text", "#catlinks", ".hatnote", "sup.reference", ".reference-text",
}

// Extra selectors stripped only during preview (admin notice boxes).
var noisePreviewOnly = []string{
	"table.ombox", "table.tmbox", "table.ambox",
	"table.cmbox", "table.fmbox", "table.imbox",
}

// removeSelectors removes all elements matching the given CSS selectors.
func removeSelectors(s *goquery.Selection, groups ...[]string) {
	for _, group := range groups {
		for _, sel := range group {
			s.Find(sel).Remove()
		}
	}
}

// removeNoise strips elements that should not be indexed.
func removeNoise(s *goquery.Selection) {
	removeSelectors(s, noiseBase, noiseIndexOnly)
}

// removePreviewNoise strips elements that should not appear in the preview
// but keeps references, notes, hatnotes, sidebars, and charts.
func removePreviewNoise(s *goquery.Selection) {
	removeSelectors(s, noiseBase, noisePreviewOnly)
}

// replaceVideos replaces <video> elements with their poster image. Video
// elements lose most attributes (controls, poster, dimensions) during
// sanitization, rendering as unconstrained blobs. Replacing with the
// poster thumbnail preserves visual context.
func replaceVideos(s *goquery.Selection) {
	s.Find("video").Each(func(_ int, v *goquery.Selection) {
		poster, hasPoster := v.Attr("poster")
		if hasPoster && poster != "" {
			// Replace the video with an <img> using the poster frame.
			v.ReplaceWithHtml(`<img src="` + poster + `" style="max-width: 100%; height: auto; display: block"/>`)
		} else {
			// No poster - remove the video entirely; the caption stays.
			v.Remove()
		}
	})
}

// cloneSelection re-parses the selection's HTML to produce an independent
// document that can be mutated without affecting the original.
func cloneSelection(s *goquery.Selection) *goquery.Selection {
	h, err := s.Html()
	if err != nil {
		return s
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div>" + h + "</div>"))
	if err != nil {
		return s
	}
	return doc.Find("div").First()
}
