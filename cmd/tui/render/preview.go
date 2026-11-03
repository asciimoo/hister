// SPDX-License-Identifier: AGPL-3.0-or-later

package render

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	readabilityrender "codeberg.org/readeck/go-readability/v2/render"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/net/html"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/cmd/tui/model"
)

func ResultDetailsContent(m *model.Model) string {
	url := m.DetailsURL
	if url == "" {
		url = m.GetSelectedURL()
	}
	title := m.DetailsHintTitle
	if m.DetailsPreview != nil && m.DetailsPreview.Title != "" {
		title = m.DetailsPreview.Title
	}
	if title == "" {
		title = url
	}
	title = sanitizeTerminalText(title)
	url = sanitizeTerminalText(url)
	lines := []string{m.Styles.Title.Render(title), m.Styles.URL.Render(url)}

	doc := detailsDocument(m, url)
	meta := map[string]any(nil)
	if m.DetailsPreview != nil {
		meta = m.DetailsPreview.Meta
	}
	facts := previewFacts(m, doc, meta)
	if len(facts) > 0 {
		lines = append(lines, "", m.Styles.Gray.Render(sanitizeTerminalText(strings.Join(facts, "  ·  "))))
	}
	if doc != nil && doc.Label != "" {
		lines = append(lines, m.Styles.SuggTerm.Render(sanitizeTerminalText("label: "+doc.Label)))
	}
	if description := metaString(meta, "description"); description != "" {
		lines = append(lines, "", m.Styles.SecText.UnsetItalic().Render(sanitizeTerminalText(description)))
	}

	lines = append(lines, "", m.Styles.HelpHeader.Render("Content"), "")
	content := ""
	switch {
	case m.DetailsLoading:
		content = "Loading readable preview…"
	case m.DetailsErr != nil:
		content = "Readable preview unavailable: " + m.DetailsErr.Error()
		if doc != nil && strings.TrimSpace(doc.Text) != "" {
			content += "\n\n" + strings.TrimSpace(doc.Text)
		}
	case m.DetailsPreview != nil:
		content = previewPlainText(m.DetailsPreview.Template, m.DetailsPreview.Content)
	case doc != nil:
		content = strings.TrimSpace(doc.Text)
	}
	if content == "" {
		content = "No readable content is available for this result."
	}
	content = sanitizeTerminalText(content)
	lines = append(lines, m.Styles.SecText.UnsetItalic().UnsetFaint().Render(content))
	return wrapDetailsText(strings.Join(lines, "\n"), max(1, DetailsPaneWidth(m)-2))
}

func wrapDetailsText(content string, width int) string {
	width = max(1, width)
	return ansi.Hardwrap(ansi.Wordwrap(content, width, "/"), width, false)
}

func detailsDocument(m *model.Model, url string) *client.Document {
	for _, doc := range m.VisibleDocuments() {
		if doc != nil && doc.URL == url {
			return doc
		}
	}
	return nil
}

func previewFacts(m *model.Model, doc *client.Document, meta map[string]any) []string {
	var facts []string
	for _, key := range []string{"author", "published", "type", "site_name"} {
		if value := metaString(meta, key); value != "" {
			if key == "published" {
				value = formatPreviewDate(value)
			}
			facts = append(facts, value)
		}
	}
	if len(facts) == 0 && doc != nil {
		if doc.Domain != "" {
			facts = append(facts, doc.Domain)
		}
		if doc.Language != "" {
			facts = append(facts, "language "+doc.Language)
		}
		facts = append(facts, fmt.Sprintf("type %v", doc.Type))
	}
	added := int64(0)
	versionCount := 0
	if m.DetailsPreview != nil {
		added = m.DetailsPreview.Added
		versionCount = m.DetailsPreview.VersionCount
	} else if doc != nil {
		added = doc.Added
	}
	if age := relativeTime(added); age != "" {
		facts = append(facts, "indexed "+age+" ago")
	}
	if versionCount > 0 {
		facts = append(facts, fmt.Sprintf("%d previous version(s)", versionCount))
	}
	return facts
}

func metaString(meta map[string]any, key string) string {
	value, _ := meta[key].(string)
	return strings.TrimSpace(value)
}

func formatPreviewDate(raw string) string {
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.Format("2 Jan 2006")
	}
	return raw
}

func previewPlainText(template, content string) string {
	if template == "code" {
		var code struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(content), &code); err != nil {
			return "Code preview unavailable."
		}
		return code.Text
	}
	if template == "issue" {
		return issuePreviewText(content)
	}
	if template == "video" {
		return videoPreviewText(content)
	}
	if !strings.Contains(content, "<") {
		return strings.TrimSpace(content)
	}
	return htmlPreviewText(content)
}

func issuePreviewText(content string) string {
	type comment struct {
		Author    string `json:"author"`
		CreatedAt string `json:"createdAt"`
		BodyHTML  string `json:"bodyHTML"`
	}
	var issue struct {
		Title      string    `json:"title"`
		Number     string    `json:"number"`
		Repository string    `json:"repository"`
		State      string    `json:"state"`
		Labels     []string  `json:"labels"`
		Body       comment   `json:"body"`
		Comments   []comment `json:"comments"`
	}
	if err := json.Unmarshal([]byte(content), &issue); err != nil {
		return "Issue preview unavailable."
	}
	var sections []string
	facts := []string{issue.Repository}
	if issue.Number != "" {
		facts = append(facts, "#"+issue.Number)
	}
	if issue.State != "" {
		facts = append(facts, issue.State)
	}
	sections = append(sections, strings.TrimSpace(strings.Join(facts, " ")), issue.Title)
	if len(issue.Labels) > 0 {
		sections = append(sections, "Labels: "+strings.Join(issue.Labels, ", "))
	}
	appendComment := func(c comment) {
		author := c.Author
		if author == "" {
			author = "Unknown author"
		}
		if c.CreatedAt != "" {
			author += " · " + formatPreviewDate(c.CreatedAt)
		}
		sections = append(sections, author+"\n"+htmlPreviewText(c.BodyHTML))
	}
	appendComment(issue.Body)
	sections = append(sections, fmt.Sprintf("Comments (%d) from the saved page", len(issue.Comments)))
	for _, c := range issue.Comments {
		appendComment(c)
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func videoPreviewText(content string) string {
	var video struct {
		Uploader          string `json:"uploader"`
		DurationFormatted string `json:"durationFormatted"`
		UploadDate        string `json:"uploadDate"`
		ViewCount         int64  `json:"viewCount"`
		Description       string `json:"description"`
		Transcript        string `json:"transcript"`
		Chapters          []struct {
			Title     string `json:"title"`
			StartTime string `json:"startTime"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal([]byte(content), &video); err != nil {
		return strings.TrimSpace(content)
	}
	var sections []string
	var facts []string
	for _, value := range []string{video.Uploader, video.UploadDate, video.DurationFormatted} {
		if value != "" {
			facts = append(facts, value)
		}
	}
	if video.ViewCount > 0 {
		facts = append(facts, fmt.Sprintf("%d views", video.ViewCount))
	}
	if len(facts) > 0 {
		sections = append(sections, strings.Join(facts, " · "))
	}
	if video.Description != "" {
		sections = append(sections, video.Description)
	}
	if len(video.Chapters) > 0 {
		chapters := []string{"Chapters"}
		for _, chapter := range video.Chapters {
			chapters = append(chapters, chapter.StartTime+"  "+chapter.Title)
		}
		sections = append(sections, strings.Join(chapters, "\n"))
	}
	if video.Transcript != "" {
		sections = append(sections, "Transcript\n"+video.Transcript)
	}
	return strings.Join(sections, "\n\n")
}

func htmlPreviewText(content string) string {
	doc, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(readabilityrender.InnerText(doc))
}
