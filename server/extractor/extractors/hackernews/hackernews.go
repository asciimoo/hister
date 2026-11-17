// Package hackernews provides an extractor for Hacker News item pages.
package hackernews

import (
	"fmt"
	stdhtml "html"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/asciimoo/nogon/server/extractor/sdk"
	"github.com/asciimoo/nogon/server/extractor/textutil"
	"github.com/asciimoo/nogon/server/extractor/urlutil"
	"github.com/asciimoo/nogon/server/sanitizer"
)

// HackerNewsExtractor turns a news.ycombinator.com item page into searchable
// text and a readable preview.
//
// Unlike the Lobsters markup, Hacker News does not nest its comments: every
// comment is a sibling row in one flat table and its depth is carried by the
// indent attribute on the leading td.ind cell. Reconstructing the tree
// therefore means tracking that number across the row sequence rather than
// recursing, which is what commentRows below does.
type HackerNewsExtractor struct {
	sdk.ConfigSupport
}

func (e *HackerNewsExtractor) Name() string {
	return "HackerNews"
}

func (e *HackerNewsExtractor) Description() string {
	return "Extracts the submission metadata, self text and full comment tree from Hacker News item pages."
}

func (e *HackerNewsExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

// Match accepts item pages on news.ycombinator.com. The URL is parsed rather
// than prefix-matched so that pagination and tracking parameters, which show
// up on long threads, do not stop a page from being recognised.
func (e *HackerNewsExtractor) Match(d *sdk.Document) bool {
	u, err := url.Parse(d.URL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "news.ycombinator.com" && host != "www.news.ycombinator.com" {
		return false
	}
	if strings.TrimSuffix(u.Path, "/") != "/item" {
		return false
	}
	return u.Query().Get("id") != ""
}

// story collects the header fields shared by Extract and Preview.
type story struct {
	title  string
	link   string
	site   string
	score  string
	author string
	age    string
	// selfText is the body of an Ask HN or text submission, absent for link
	// submissions.
	selfText *goquery.Selection
}

func parseStory(doc *goquery.Document) story {
	titleLink := doc.Find(".titleline > a").First()
	subline := doc.Find(".subline").First()
	return story{
		title:    strings.TrimSpace(titleLink.Text()),
		link:     strings.TrimSpace(titleLink.AttrOr("href", "")),
		site:     strings.TrimSpace(doc.Find(".titleline .sitestr").First().Text()),
		score:    strings.TrimSpace(subline.Find(".score").First().Text()),
		author:   strings.TrimSpace(subline.Find(".hnuser").First().Text()),
		age:      strings.TrimSpace(subline.Find(".age").First().AttrOr("title", "")),
		selfText: doc.Find(".fatitem .toptext").First(),
	}
}

// comment is one row of the flat comment table.
type comment struct {
	depth    int
	author   string
	age      string
	body     string
	bodyHTML string
}

func commentRows(doc *goquery.Document) []comment {
	comments := make([]comment, 0)
	doc.Find("tr.athing.comtr").Each(func(_ int, row *goquery.Selection) {
		depth, err := strconv.Atoi(row.Find("td.ind").First().AttrOr("indent", ""))
		if err != nil || depth < 0 {
			depth = 0
		}
		text := row.Find(".commtext").First()
		// Collapsed and flagged comments keep their row but carry no body.
		// They are still worth a line so the thread shape survives, but an
		// entirely empty one adds nothing to the index.
		body := textutil.SelectionText(text)
		author := strings.TrimSpace(row.Find(".comhead .hnuser").First().Text())
		if body == "" && author == "" {
			return
		}
		bodyHTML, err := text.Html()
		if err != nil {
			bodyHTML = ""
		}
		comments = append(comments, comment{
			depth:    depth,
			author:   author,
			age:      strings.TrimSpace(row.Find(".comhead .age").First().AttrOr("title", "")),
			body:     body,
			bodyHTML: bodyHTML,
		})
	})
	return comments
}

// Extract populates Title and Text with the submission header, any self text
// and the whole comment tree, so the discussion is searchable and not just the
// headline.
func (e *HackerNewsExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.ExtractFallback(err)
	}

	s := parseStory(doc)
	d.Title = s.title

	var b strings.Builder
	if s.title != "" {
		b.WriteString(s.title)
	}
	if s.site != "" {
		b.WriteString("\n")
		b.WriteString(s.site)
	}
	byline := make([]string, 0, 3)
	if s.score != "" {
		byline = append(byline, s.score)
	}
	if s.author != "" {
		byline = append(byline, "by "+s.author)
	}
	if s.age != "" {
		byline = append(byline, "on "+s.age)
	}
	if len(byline) > 0 {
		b.WriteString("\n")
		b.WriteString(strings.Join(byline, " "))
	}
	if t := textutil.SelectionText(s.selfText); t != "" {
		b.WriteString("\n\n")
		b.WriteString(t)
	}

	// Indent each comment by its depth so parent/child relationships survive
	// into the indexed text, matching how the Lobsters extractor renders them.
	for _, c := range commentRows(doc) {
		indent := strings.Repeat("  ", c.depth)
		b.WriteString("\n\n")
		b.WriteString(indent)
		if c.author != "" {
			b.WriteString(c.author)
		}
		if c.age != "" {
			fmt.Fprintf(&b, " [%s]", c.age)
		}
		for line := range strings.SplitSeq(c.body, "\n") {
			b.WriteString("\n")
			if line == "" {
				continue
			}
			b.WriteString(indent)
			b.WriteString(line)
		}
	}

	d.Text = strings.TrimSpace(b.String())
	if d.Text == "" && d.Title == "" {
		return sdk.ExtractFallback(fmt.Errorf("no content found"))
	}
	return sdk.Extracted()
}

// Preview renders the submission and its comment tree as sanitized HTML. The
// flat indent sequence is turned back into nested lists so the preview shows
// the reply structure rather than a wall of equal-weight comments.
func (e *HackerNewsExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.PreviewFallback(err)
	}

	// Hacker News links internally with relative URLs (item?id=123 on Ask HN
	// titles, polls and reply links) which the sanitizer strips because it
	// only keeps absolute http(s) URLs. Resolving everything against the page
	// URL up front keeps those links in the preview.
	base, _ := url.Parse(d.URL)
	urlutil.RewriteURLs(doc.Selection, base)

	s := parseStory(doc)

	var b strings.Builder
	if s.title != "" || s.link != "" {
		b.WriteString("<h2>")
		if s.link != "" {
			fmt.Fprintf(&b, `<a href="%s">%s</a>`, stdhtml.EscapeString(s.link), stdhtml.EscapeString(s.title))
		} else {
			b.WriteString(stdhtml.EscapeString(s.title))
		}
		b.WriteString("</h2>")
	}

	bylineParts := make([]string, 0, 4)
	if s.score != "" {
		bylineParts = append(bylineParts, stdhtml.EscapeString(s.score))
	}
	if s.author != "" {
		bylineParts = append(bylineParts, fmt.Sprintf("submitted by <strong>%s</strong>", stdhtml.EscapeString(s.author)))
	}
	if s.age != "" {
		bylineParts = append(bylineParts, "on "+stdhtml.EscapeString(s.age))
	}
	if s.site != "" {
		bylineParts = append(bylineParts, stdhtml.EscapeString(s.site))
	}
	if len(bylineParts) > 0 {
		fmt.Fprintf(&b, "<p>%s</p>", strings.Join(bylineParts, " &middot; "))
	}

	if s.selfText.Length() > 0 {
		if inner, err := s.selfText.Html(); err == nil && strings.TrimSpace(inner) != "" {
			b.WriteString(inner)
		}
	}

	comments := commentRows(doc)
	if len(comments) > 0 {
		b.WriteString("<h2>Comments</h2>")
		writeCommentTree(&b, comments)
	}

	return sdk.Previewed(sdk.PreviewResponse{Content: sanitizer.SanitizeHTML(b.String())})
}

// writeCommentTree converts the flat depth-tagged sequence into nested lists.
// Depth can jump by more than one only downward in malformed markup, so the
// close path loops while the open path steps once, which keeps the tag stack
// balanced whatever the input.
func writeCommentTree(b *strings.Builder, comments []comment) {
	depth := -1
	for _, c := range comments {
		for depth > c.depth {
			b.WriteString("</li></ul>")
			depth--
		}
		switch {
		case depth < c.depth:
			for depth < c.depth {
				b.WriteString("<ul>")
				depth++
			}
		default:
			b.WriteString("</li>")
		}
		b.WriteString("<li>")
		head := make([]string, 0, 2)
		if c.author != "" {
			head = append(head, fmt.Sprintf("<strong>%s</strong>", stdhtml.EscapeString(c.author)))
		}
		if c.age != "" {
			head = append(head, stdhtml.EscapeString(c.age))
		}
		if len(head) > 0 {
			fmt.Fprintf(b, "<p>%s</p>", strings.Join(head, " &middot; "))
		}
		if c.bodyHTML != "" {
			b.WriteString(c.bodyHTML)
		} else if c.body != "" {
			fmt.Fprintf(b, "<p>%s</p>", stdhtml.EscapeString(c.body))
		}
	}
	for depth >= 0 {
		b.WriteString("</li></ul>")
		depth--
	}
}
