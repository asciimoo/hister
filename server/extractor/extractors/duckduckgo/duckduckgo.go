// Package duckduckgo provides an extractor for DuckDuckGo search result pages.
package duckduckgo

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/asciimoo/hister/server/extractor/sdk"
	"github.com/asciimoo/hister/server/extractor/serputil"
	"github.com/asciimoo/hister/server/extractor/urlutil"
)

const (
	resultMetadataType = "duckduckgo_result"
	answerMetadataType = "duckduckgo_answer"

	// keepAnswerOption makes the extractor store the Search Assist answer as
	// the search page's own document instead of dropping it with the page.
	keepAnswerOption = "keep_answer"

	// answerSelector matches the container DuckDuckGo fills with the generated
	// Search Assist answer.
	answerSelector = "[data-testid='duckassist-answer-content']"

	// answerBlockSelector matches the elements that hold answer text. The
	// source chips of the answer are plain spans, so they stay out of the text.
	answerBlockSelector = "p, h2, h3, h4"

	// noiseSelector matches elements whose text is markup or styling rather
	// than content.
	noiseSelector = "script, style, noscript, template"

	// resultSelector matches the containers DuckDuckGo uses for search results
	// across the JavaScript page and the HTML and Lite endpoints.
	resultSelector = "article[data-testid='result'], div.result, div.web-result"

	// titleSelector matches the title link of a result in the same front ends.
	titleSelector = "a[data-testid='result-title-a'], a.result__a, a.result-link"

	// snippetSelector lists the description containers in order of preference.
	snippetSelector = "[data-result='snippet'], .result__snippet, .result-snippet"

	// siteSelector matches the displayed URL under a result.
	siteSelector = "[data-testid='result-extras-url-link'], .result__url"

	// adSelector matches promoted results and their badges.
	adSelector = ".result--ad, .result--ad--small, [data-testid='ad'], [data-testid='ad-badge'], .badge--ad"
)

// DuckDuckGoExtractor indexes the organic results of a DuckDuckGo search page
// as one document per result.
type DuckDuckGoExtractor struct {
	cfg *sdk.Config
}

var _ sdk.Extractor = (*DuckDuckGoExtractor)(nil)

func (e *DuckDuckGoExtractor) Name() string {
	return "DuckDuckGo"
}

func (e *DuckDuckGoExtractor) Description() string {
	return "Indexes the organic results of DuckDuckGo search pages as individual documents with their title, snippet, and displayed site."
}

func (e *DuckDuckGoExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

func (e *DuckDuckGoExtractor) Match(d *sdk.Document) bool {
	if d == nil {
		return false
	}
	u, err := url.Parse(d.URL)
	if err != nil || u.User != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if !isDuckDuckGoHost(strings.ToLower(u.Hostname())) {
		return false
	}
	switch strings.TrimSuffix(u.Path, "/") {
	case "", "/html", "/lite", "/search":
	default:
		return false
	}
	return serputil.Query(d.URL) != ""
}

// Extract records every organic result as an extra document and leaves the
// result page itself out of the index. A page that yields no result, such as a
// JavaScript shell, a consent screen, or an empty search, is skipped as well:
// none of it is worth indexing as boilerplate.
//
// With the keep_answer option the generated Search Assist answer is stored as
// the search page's own document, because it is the one part of the page whose
// content is not available anywhere else.
func (e *DuckDuckGoExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.ExtractFallback(err)
	}
	query := searchQuery(doc, d.URL)
	results := parseResults(doc, d.URL)
	answer := ""
	if e.keepAnswer() {
		answer = answerText(doc)
	}
	if answer == "" {
		// The result page is never a document of its own, so the flag covers
		// both the parsed and the empty case.
		d.SkipIndexing = true
	} else {
		d.SkipIndexing = false
		d.Text = answer
		if serputil.CleanText(d.Title) == "" {
			d.Title = query
		}
		d.AddMetadata("type", answerMetadataType)
		if query != "" {
			d.AddMetadata("query", query)
		}
	}
	if len(results) == 0 {
		if answer != "" {
			return sdk.Extracted()
		}
		return sdk.ExtractFallback(errors.New("no duckduckgo search results found"))
	}
	d.ExtraDocuments = append(d.ExtraDocuments,
		serputil.Documents(results, d.UserID, resultMetadataType, query, e.maxResults())...)
	return sdk.Extracted()
}

// answerText returns the text of the Search Assist answer on a result page.
// The answer paragraphs are read on their own because the container also holds
// the source chips, whose labels are not part of the answer.
func answerText(doc *goquery.Document) string {
	container := doc.Find(answerSelector).First()
	if container.Length() == 0 {
		return ""
	}
	clone := container.Clone()
	clone.Find(noiseSelector).Remove()
	var parts []string
	clone.Find(answerBlockSelector).Each(func(_ int, block *goquery.Selection) {
		if text := serputil.CleanText(block.Text()); text != "" {
			parts = append(parts, text)
		}
	})
	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}
	// Markup variants without paragraphs keep the whole answer container.
	return serputil.CleanText(clone.Text())
}

// Preview renders the parsed results instead of the raw result page.
func (e *DuckDuckGoExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.PreviewFallback(err)
	}
	answer := ""
	if e.keepAnswer() {
		answer = answerText(doc)
	}
	results := serputil.Limit(parseResults(doc, d.URL), e.maxResults())
	return serputil.PreviewWithAnswer("DuckDuckGo", searchQuery(doc, d.URL), answer, results)
}

func (e *DuckDuckGoExtractor) GetConfig() *sdk.Config {
	if e.cfg != nil {
		return e.cfg
	}
	return &sdk.Config{Enable: true, Options: map[string]any{
		"max_results":    serputil.DefaultMaxResults,
		keepAnswerOption: false,
	}}
}

func (e *DuckDuckGoExtractor) SetConfig(config *sdk.Config) error {
	if err := serputil.ValidateOptions(config.Options, keepAnswerOption); err != nil {
		return err
	}
	if _, err := keepAnswerValue(config.Options); err != nil {
		return err
	}
	e.cfg = config
	return nil
}

func (e *DuckDuckGoExtractor) keepAnswer() bool {
	keep, err := keepAnswerValue(e.GetConfig().Options)
	if err != nil {
		return false
	}
	return keep
}

// keepAnswerValue returns the keep_answer option, false when it is absent.
func keepAnswerValue(options map[string]any) (bool, error) {
	value, ok := options[keepAnswerOption]
	if !ok {
		return false, nil
	}
	keep, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", keepAnswerOption)
	}
	return keep, nil
}

func (e *DuckDuckGoExtractor) maxResults() int {
	maxResults, err := serputil.MaxResults(e.GetConfig().Options)
	if err != nil {
		return serputil.DefaultMaxResults
	}
	return maxResults
}

// parseResults collects the organic results of a result page in display order.
func parseResults(doc *goquery.Document, pageURL string) []serputil.Result {
	base, _ := url.Parse(pageURL)
	var results serputil.Collector
	doc.Find(resultSelector).Each(func(_ int, block *goquery.Selection) {
		if isAd(block) {
			return
		}
		addResult(&results, base, block, block.Find(titleSelector).First())
	})
	if results.Len() > 0 {
		return results.Results()
	}
	// Markup variants without the known result containers keep the title links.
	doc.Find(titleSelector).Each(func(_ int, anchor *goquery.Selection) {
		if anchor.Closest(adSelector).Length() > 0 {
			return
		}
		addResult(&results, base, resultBlock(anchor), anchor)
	})
	return results.Results()
}

// isAd reports whether a result block is a promotion, including the badge
// labelled variant used by the JavaScript page.
func isAd(block *goquery.Selection) bool {
	if block.Closest(adSelector).Length() > 0 || block.Find(adSelector).Length() > 0 {
		return true
	}
	badge := serputil.CleanText(block.Find("[data-testid='result-badge'], .result__badge").First().Text())
	return strings.EqualFold(badge, "ad") || strings.EqualFold(badge, "sponsored")
}

// addResult appends one result when its link points outside DuckDuckGo.
func addResult(results *serputil.Collector, base *url.URL, block, anchor *goquery.Selection) {
	if anchor.Length() == 0 {
		return
	}
	raw, ok := anchor.Attr("href")
	if !ok {
		return
	}
	target, ok := resultURL(base, raw)
	if !ok || isDuckDuckGoHost(serputil.HostOf(target)) {
		return
	}
	title := serputil.CleanText(anchor.Text())
	if title == "" {
		return
	}
	results.Add(serputil.Result{
		Title:   title,
		URL:     target,
		Snippet: snippetText(block, title),
		Site:    siteName(block, target),
	})
}

// resultBlock returns the closest ancestor that scopes a single result.
func resultBlock(anchor *goquery.Selection) *goquery.Selection {
	if block := anchor.Closest("article, div.result, div.web-result, li"); block.Length() > 0 {
		return block.First()
	}
	return anchor.Parent()
}

// resultURL resolves a title link and unwraps DuckDuckGo's outbound redirect.
func resultURL(base *url.URL, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return "", false
	}
	resolved := raw
	if base != nil {
		resolved = urlutil.ResolveURL(base, raw)
	}
	u, err := url.Parse(resolved)
	if err != nil {
		return "", false
	}
	// DuckDuckGo wraps outbound links as /l/?uddg=<target>&rut=<token>.
	if isDuckDuckGoHost(serputil.HostOf(resolved)) && strings.HasSuffix(strings.TrimSuffix(u.Path, "/"), "/l") {
		target := u.Query().Get("uddg")
		if target == "" {
			return "", false
		}
		return serputil.CanonicalURL(target)
	}
	return serputil.CanonicalURL(resolved)
}

// isDuckDuckGoHost reports whether host is one of DuckDuckGo's search front
// ends.
func isDuckDuckGoHost(host string) bool {
	switch strings.ToLower(strings.TrimSuffix(host, ".")) {
	case "duckduckgo.com", "www.duckduckgo.com", "html.duckduckgo.com",
		"lite.duckduckgo.com", "noai.duckduckgo.com", "start.duckduckgo.com":
		return true
	}
	return false
}

// snippetText returns the description of one result, falling back to the text
// of the result block without its title and displayed URL.
func snippetText(block *goquery.Selection, title string) string {
	if block.Length() > 0 {
		for _, selector := range strings.Split(snippetSelector, ",") {
			selector = strings.TrimSpace(selector)
			if candidate := block.Find(selector).First(); candidate.Length() > 0 {
				if text := serputil.CleanText(candidate.Text()); text != "" {
					return serputil.ClipSnippet(text)
				}
			}
		}
	}
	text := serputil.CleanText(block.Text())
	text = strings.Replace(text, title, "", 1)
	if site := serputil.CleanText(block.Find(siteSelector).First().Text()); site != "" {
		text = strings.Replace(text, site, "", 1)
	}
	return serputil.ClipSnippet(text)
}

// siteName prefers the URL DuckDuckGo displays under the result.
func siteName(block *goquery.Selection, target string) string {
	if block.Length() > 0 {
		if displayed := serputil.CleanText(block.Find(siteSelector).First().Text()); displayed != "" {
			if site := serputil.DisplayedHost(displayed); site != "" {
				return site
			}
		}
	}
	return serputil.DisplayedHost(target)
}

// searchQuery returns the query of the result page from its URL, its search
// field, or its title.
func searchQuery(doc *goquery.Document, pageURL string) string {
	if query := serputil.Query(pageURL); query != "" {
		return query
	}
	for _, selector := range []string{"input[name='q']", "textarea[name='q']"} {
		field := doc.Find(selector).First()
		if field.Length() == 0 {
			continue
		}
		if value, ok := field.Attr("value"); ok {
			if text := serputil.CleanText(value); text != "" {
				return text
			}
		}
		if text := serputil.CleanText(field.Text()); text != "" {
			return text
		}
	}
	title := serputil.CleanText(doc.Find("title").First().Text())
	for _, suffix := range []string{" at DuckDuckGo", " - DuckDuckGo"} {
		if trimmed := strings.TrimSuffix(title, suffix); trimmed != title {
			return trimmed
		}
	}
	return ""
}
