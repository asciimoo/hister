// Package lobsters provides an extractor for lobste.rs story pages.
package lobsters

import (
	"fmt"
	stdhtml "html"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/asciimoo/hister/server/extractor/sdk"
	"github.com/asciimoo/hister/server/sanitizer"
)

const (
	matchURLPrefix           = "https://lobste.rs/s/"
	topLevelCommentsSelector = "#story_comments > ol.comments > li.comments_subtree"
)

type LobstersExtractor struct {
	sdk.ConfigSupport
}

func (e *LobstersExtractor) Name() string {
	return "Lobsters"
}

func (e *LobstersExtractor) Description() string {
	return "Extracts the submission metadata, story body and full nested comment tree from lobste.rs story pages."
}

func (e *LobstersExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

func (e *LobstersExtractor) Match(d *sdk.Document) bool {
	return strings.HasPrefix(d.URL, matchURLPrefix) && len(d.URL) > len(matchURLPrefix)
}

// Extract populates the document's Title and Text with the story metadata and
// the full comment tree so they are searchable from the index.
func (e *LobstersExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.ExtractFallback(err)
	}

	story := doc.Find("li.story").First()
	d.Title = strings.TrimSpace(story.Find(".link .u-url").First().Text())

	var b strings.Builder
	writeStoryText(&b, story)
	if body := strings.TrimSpace(doc.Find(".story_content").Text()); body != "" {
		b.WriteString("\n\n")
		b.WriteString(body)
	}
	doc.Find(topLevelCommentsSelector).Each(func(_ int, s *goquery.Selection) {
		writeCommentText(&b, s, 0)
	})

	d.Text = strings.TrimSpace(b.String())
	if d.Text == "" && d.Title == "" {
		return sdk.ExtractFallback(fmt.Errorf("no content found"))
	}
	return sdk.Extracted()
}

// Preview renders the story header, body and nested comment tree as sanitized
// HTML suitable for the preview pane.
func (e *LobstersExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.PreviewFallback(err)
	}

	story := doc.Find("li.story").First()
	storyLink := story.Find(".link .u-url").First()
	title := strings.TrimSpace(storyLink.Text())
	link, _ := storyLink.Attr("href")
	author := strings.TrimSpace(story.Find(".byline .u-author").First().Text())
	submitted := strings.TrimSpace(story.Find(".byline time").First().AttrOr("title", ""))
	tags := make([]string, 0)
	story.Find(".tags .tag").Each(func(_ int, s *goquery.Selection) {
		tags = append(tags, strings.TrimSpace(s.Text()))
	})

	var b strings.Builder
	if title != "" || link != "" {
		b.WriteString("<h2>")
		if link != "" {
			fmt.Fprintf(&b, `<a href="%s">%s</a>`, stdhtml.EscapeString(link), stdhtml.EscapeString(title))
		} else {
			b.WriteString(stdhtml.EscapeString(title))
		}
		b.WriteString("</h2>")
	}

	bylineParts := make([]string, 0, 3)
	if author != "" {
		bylineParts = append(bylineParts, fmt.Sprintf("submitted by <strong>%s</strong>", stdhtml.EscapeString(author)))
	}
	if submitted != "" {
		bylineParts = append(bylineParts, "on "+stdhtml.EscapeString(submitted))
	}
	if len(tags) > 0 {
		escaped := make([]string, len(tags))
		for i, t := range tags {
			escaped[i] = stdhtml.EscapeString(t)
		}
		bylineParts = append(bylineParts, "tags: "+strings.Join(escaped, ", "))
	}
	if len(bylineParts) > 0 {
		fmt.Fprintf(&b, "<p>%s</p>", strings.Join(bylineParts, " &middot; "))
	}

	if body, err := doc.Find(".story_content").Html(); err == nil && strings.TrimSpace(body) != "" {
		b.WriteString(body)
	}

	comments := doc.Find(topLevelCommentsSelector)
	if comments.Length() > 0 {
		b.WriteString("<h2>Comments</h2>")
		b.WriteString(`<ol class="comments">`)
		comments.Each(func(_ int, s *goquery.Selection) {
			writeCommentHTML(&b, s)
		})
		b.WriteString("</ol>")
	}

	return sdk.Previewed(sdk.PreviewResponse{Content: sanitizer.SanitizeHTML(b.String())})
}

// commentAuthor returns the commenter's username. The byline contains two
// anchors to /~user an aria-hidden avatar anchor (empty text) and the real
// author anchor so pick the first one that carries visible text.
func commentAuthor(comment *goquery.Selection) string {
	var name string
	comment.Find(".byline a[href^='/~']").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if t := strings.TrimSpace(s.Text()); t != "" {
			name = t
			return false
		}
		return true
	})
	return name
}

// writeStoryText writes a short, searchable summary of the submission header.
func writeStoryText(b *strings.Builder, story *goquery.Selection) {
	title := strings.TrimSpace(story.Find(".link .u-url").First().Text())
	if title != "" {
		b.WriteString(title)
	}
	if author := strings.TrimSpace(story.Find(".byline .u-author").First().Text()); author != "" {
		b.WriteString("\nsubmitted by ")
		b.WriteString(author)
	}
	tags := make([]string, 0)
	story.Find(".tags .tag").Each(func(_ int, s *goquery.Selection) {
		tags = append(tags, strings.TrimSpace(s.Text()))
	})
	if len(tags) > 0 {
		b.WriteString("\ntags: ")
		b.WriteString(strings.Join(tags, ", "))
	}
}

// writeCommentText walks the nested comment subtree and writes each comment as
// an indented block of plain text so that parent/child relationships are
// preserved in the indexed Text.
func writeCommentText(b *strings.Builder, subtree *goquery.Selection, depth int) {
	comment := subtree.Children().Filter("div.comment").First()
	if comment.Length() > 0 && comment.AttrOr("data-shortid", "") != "" {
		indent := strings.Repeat("  ", depth)
		author := commentAuthor(comment)
		score := strings.TrimSpace(comment.Find(".voters .upvoter").First().Text())
		body := strings.TrimSpace(comment.Find(".comment_text").First().Text())

		b.WriteString("\n\n")
		b.WriteString(indent)
		if author != "" {
			b.WriteString(author)
		}
		if score != "" {
			fmt.Fprintf(b, " [%s]", score)
		}
		if body != "" {
			for line := range strings.SplitSeq(body, "\n") {
				b.WriteString("\n")
				b.WriteString(indent)
				b.WriteString(line)
			}
		}
	}
	subtree.Children().Filter("ol.comments").Children().Filter("li.comments_subtree").Each(func(_ int, s *goquery.Selection) {
		writeCommentText(b, s, depth+1)
	})
}

// writeCommentHTML renders a single comment subtree as nested <li>/<ol>
// preserving the original reply hierarchy.
func writeCommentHTML(b *strings.Builder, subtree *goquery.Selection) {
	comment := subtree.Children().Filter("div.comment").First()
	if comment.Length() == 0 || comment.AttrOr("data-shortid", "") == "" {
		return
	}
	author := commentAuthor(comment)
	score := strings.TrimSpace(comment.Find(".voters .upvoter").First().Text())
	when := strings.TrimSpace(comment.Find(".byline time").First().AttrOr("title", ""))
	body, _ := comment.Find(".comment_text").First().Html()

	b.WriteString("<li>")
	b.WriteString("<p>")
	if author != "" {
		fmt.Fprintf(b, "<strong>%s</strong>", author)
	}
	if score != "" {
		fmt.Fprintf(b, " [%s]", score)
	}
	if when != "" {
		fmt.Fprintf(b, " &middot; %s", when)
	}
	b.WriteString("</p>")
	b.WriteString(body)

	children := subtree.Children().Filter("ol.comments").Children().Filter("li.comments_subtree")
	if children.Length() > 0 {
		b.WriteString(`<ol class="comments">`)
		children.Each(func(_ int, s *goquery.Selection) {
			writeCommentHTML(b, s)
		})
		b.WriteString("</ol>")
	}
	b.WriteString("</li>")
}
