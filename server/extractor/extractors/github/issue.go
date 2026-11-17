// SPDX-License-Identifier: AGPL-3.0-or-later

package github

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	readabilityrender "codeberg.org/readeck/go-readability/v2/render"
	"github.com/PuerkitoBio/goquery"

	"github.com/asciimoo/nogon/server/extractor/sdk"
	"github.com/asciimoo/nogon/server/sanitizer"
)

func extractIssue(d *sdk.Document) sdk.ExtractResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.ExtractFallback(err)
	}
	info := parseIssue(doc, d.URL)
	if info == nil {
		return sdk.ExtractFallback(nil)
	}
	d.Title = strings.TrimSpace(doc.Find("title").First().Text())
	if d.Title == "" {
		d.Title = info.Title
	}
	if d.Metadata == nil {
		d.Metadata = make(map[string]any)
	}
	d.Metadata["type"] = "Issue"
	d.Metadata["repo"] = info.Repository
	d.Metadata["title"] = info.Title
	if info.Body.CreatedAt != "" {
		d.Metadata["date"] = info.Body.CreatedAt
	}
	if info.Body.Author != "" {
		d.Metadata["author"] = info.Body.Author
	}
	if info.State != "" {
		d.Metadata["state"] = info.State
	}
	var b strings.Builder
	if info.Title != "" {
		fmt.Fprintf(&b, "title: %s\n\n", info.Title)
	}
	if body := issuePlainText(info.Body.BodyHTML); body != "" {
		fmt.Fprintf(&b, "body: %s\n\n", body)
	}
	var comments []string
	for _, comment := range info.Comments {
		if body := issuePlainText(comment.BodyHTML); body != "" {
			comments = append(comments, body)
		}
	}
	if len(comments) > 0 {
		fmt.Fprintf(&b, "comments: %s\n", strings.Join(comments, ", "))
	}
	d.Text = strings.TrimSpace(b.String())
	return sdk.Extracted()
}

func previewIssue(d *sdk.Document) sdk.PreviewResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.PreviewFallback(err)
	}
	info := parseIssue(doc, d.URL)
	if info == nil {
		return sdk.PreviewFallback(nil)
	}
	content, err := json.Marshal(info)
	if err != nil {
		return sdk.PreviewFallback(err)
	}
	return sdk.Previewed(sdk.PreviewResponse{Template: "issue", Content: string(content)})
}

func parseIssue(doc *goquery.Document, sourceURL string) *sdk.IssuePreview {
	match := issueRe.FindStringSubmatch(sourceURL)
	if match == nil {
		return nil
	}
	title := strings.TrimSpace(doc.Find(`[data-testid="issue-title"]`).First().Text())
	body := doc.Find(`#issue-body-viewer, [data-testid="issue-body-viewer"]`).First()
	if title == "" && body.Length() == 0 {
		return nil
	}
	base, err := url.Parse(githubURLPrefix + match[1] + "/" + match[2] + "/issues/" + match[3])
	if err != nil {
		return nil
	}
	info := &sdk.IssuePreview{
		Title: title, Number: match[3], Repository: match[1] + "/" + match[2], URL: base.String(),
		State:    strings.TrimSpace(doc.Find(`[data-testid="header-state"]`).First().Text()),
		Comments: []sdk.IssueComment{},
	}
	header := doc.Find(`[data-testid="issue-body"]`).First().Clone()
	header.Find(`#issue-body-viewer, [data-testid="issue-body-viewer"]`).Remove()
	// The viewer also contains reaction controls outside the rendered description.
	if markdown := body.Find(`[data-testid="markdown-body"], .markdown-body`).First(); markdown.Length() > 0 {
		body = markdown
	}
	info.Body = parseIssueComment(header, body, base)
	comments := doc.Find(`[data-testid="issue-viewer-comments-container"]`)
	comments.Find(`[data-testid="markdown-body"], .markdown-body`).Each(func(_ int, body *goquery.Selection) {
		// GitHub nests markdown-body classes and data attributes around the same content.
		if body.ParentsFiltered(`[data-testid="markdown-body"], .markdown-body`).Length() > 0 {
			return
		}
		container := body.Closest(`[data-testid^="comment-viewer-outer-box"], .timeline-comment`)
		header := container.Find(`[data-testid="comment-header"], .timeline-comment-header`).First()
		info.Comments = append(info.Comments, parseIssueComment(header, body, base))
	})
	doc.Find(`[data-testid="issue-labels"] a`).Each(func(_ int, s *goquery.Selection) {
		label := s.Clone()
		label.Find(`.sr-only, [role="tooltip"]`).Remove()
		if text := strings.TrimSpace(label.Text()); text != "" {
			info.Labels = append(info.Labels, text)
		}
	})
	return info
}

func parseIssueComment(header, body *goquery.Selection, base *url.URL) sdk.IssueComment {
	author := header.Find(`[data-testid="issue-body-header-author"], [data-testid="avatar-link"], .author`).First()
	date := header.Find(`relative-time, time`).First()
	createdAt := strings.TrimSpace(date.AttrOr("datetime", ""))
	if createdAt == "" {
		createdAt = strings.TrimSpace(date.Text())
	}
	link := header.Find(`[data-testid="issue-body-header-link"], a[href*="#issuecomment-"]`).First()
	if link.Length() == 0 {
		link = date.Closest("a")
	}
	return sdk.IssueComment{
		Author:    strings.TrimSpace(author.Text()),
		AuthorURL: issueLink(author.AttrOr("href", ""), base),
		CreatedAt: createdAt,
		URL:       issueLink(link.AttrOr("href", ""), base),
		BodyHTML:  issueBodyHTML(body, base),
	}
}

// Only HTTP links are used in template attributes. Body links pass through the
// standard sanitizer separately, which also permits mailto links.
func issueLink(raw string, base *url.URL) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	u = base.ResolveReference(u)
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return ""
	}
	return u.String()
}

func issueBodyHTML(body *goquery.Selection, base *url.URL) string {
	if body.Length() == 0 {
		return ""
	}
	content := body.Clone()
	content.Find("script, style, button, form").Remove()
	// Preserve task completion without allowing interactive form inputs.
	content.Find(`input[type="checkbox"]`).Each(func(_ int, s *goquery.Selection) {
		_, checked := s.Attr("checked")
		if checked || s.AttrOr("aria-checked", "") == "true" {
			s.ReplaceWithHtml(`<span title="Completed task">☑</span> `)
		} else {
			s.ReplaceWithHtml(`<span title="Incomplete task">☐</span> `)
		}
	})
	content.Find("[href], [src]").Each(func(_ int, s *goquery.Selection) {
		for _, attr := range []string{"href", "src"} {
			if raw, ok := s.Attr(attr); ok {
				if ref, err := url.Parse(strings.TrimSpace(raw)); err == nil {
					s.SetAttr(attr, base.ResolveReference(ref).String())
				}
			}
		}
	})
	// Use the resolved src instead of relative responsive image candidates.
	content.Find("[srcset]").RemoveAttr("srcset")
	raw, err := content.Html()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(sanitizer.SanitizeHTML(raw))
}

func issuePlainText(content string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(readabilityrender.InnerText(doc.Nodes[0]))
}
