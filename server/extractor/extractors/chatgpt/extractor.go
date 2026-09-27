// SPDX-License-Identifier: AGPL-3.0-or-later

// Package chatgpt extracts rendered ChatGPT conversations as single documents.
package chatgpt

import (
	"errors"
	"fmt"
	stdhtml "html"
	"net/url"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"github.com/asciimoo/hister/server/extractor/sdk"
	"github.com/asciimoo/hister/server/extractor/urlutil"
	"github.com/asciimoo/hister/server/sanitizer"
)

const conversationType = "chatgpt"

const conversationRoleSelector = `[data-message-author-role], [data-testid^="conversation-turn-"][data-turn], ` +
	`[data-chatgpt-search-unit-key], [data-content-search-unit-key]`

const noConversationTurns = "no visible user or assistant turns found; capture the loaded conversation with the browser extension or a browser crawler (chromedp or bidi); private conversations require a signed in browser"

// ChatGPTExtractor extracts one visible ChatGPT conversation into one Hister
// document. It intentionally works only with the rendered HTML already on the
// document and never fetches conversation data itself.
type ChatGPTExtractor struct {
	sdk.ConfigSupport
}

var _ sdk.Extractor = (*ChatGPTExtractor)(nil)

func (e *ChatGPTExtractor) Name() string {
	return "ChatGPT"
}

func (e *ChatGPTExtractor) Description() string {
	return "Extracts the visible user and assistant turns from rendered ChatGPT conversations as one searchable document. Requires HTML captured by the browser extension or a browser crawler (chromedp or bidi)."
}

func (e *ChatGPTExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

// Match accepts authenticated, custom GPT, and public shared conversation URLs
// on chatgpt.com. Other ChatGPT pages are left to later extractors.
func (e *ChatGPTExtractor) Match(d *sdk.Document) bool {
	if d == nil {
		return false
	}
	u, err := url.Parse(d.URL)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.User != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host != "chatgpt.com" && host != "www.chatgpt.com" {
		return false
	}

	path := u.EscapedPath()
	if !strings.HasPrefix(path, "/") {
		return false
	}
	path = strings.TrimPrefix(path, "/")
	path, _ = strings.CutSuffix(path, "/")
	parts := strings.Split(path, "/")
	var idParts []string
	switch {
	case len(parts) == 2 && (parts[0] == "c" || parts[0] == "share"):
		idParts = []string{parts[1]}
	case len(parts) == 4 && parts[0] == "g" && parts[2] == "c":
		idParts = []string{parts[1], parts[3]}
	default:
		return false
	}
	for _, rawID := range idParts {
		if !validConversationPathSegment(rawID) {
			return false
		}
	}
	return true
}

func validConversationPathSegment(rawID string) bool {
	if rawID == "" {
		return false
	}
	id, err := url.PathUnescape(rawID)
	if err != nil || id == "." || id == ".." || strings.Contains(id, "/") {
		return false
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

type conversationTurn struct {
	role    string
	content *goquery.Selection
}

func (e *ChatGPTExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	if !e.Match(d) {
		return sdk.ExtractFallback(fmt.Errorf("not a ChatGPT conversation URL"))
	}
	doc, turns, err := parseConversation(d)
	if err != nil {
		return sdk.ExtractFallback(err)
	}
	if len(turns) == 0 {
		return sdk.AbortExtraction(errors.New(noConversationTurns))
	}

	title := documentTitle(d, doc)
	if title != "" {
		d.Title = title
	}
	d.Text = conversationText(turns)
	if d.Metadata == nil {
		d.Metadata = make(map[string]any)
	}
	d.Metadata["type"] = conversationType
	return sdk.Extracted()
}

func (e *ChatGPTExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	if !e.Match(d) {
		return sdk.PreviewFallback(fmt.Errorf("not a ChatGPT conversation URL"))
	}
	doc, turns, err := parseConversation(d)
	if err != nil {
		return sdk.PreviewFallback(err)
	}
	if len(turns) == 0 {
		return sdk.PreviewFallback(errors.New(noConversationTurns))
	}

	base, err := url.Parse(d.URL)
	if err != nil {
		return sdk.PreviewFallback(err)
	}

	var b strings.Builder
	if title := documentTitle(d, doc); title != "" {
		fmt.Fprintf(&b, "<h1>%s</h1>", stdhtml.EscapeString(title))
	}
	for _, turn := range turns {
		fmt.Fprintf(&b, "<div><h2>%s</h2>", stdhtml.EscapeString(roleLabel(turn.role)))
		content := turn.content.Clone()
		cleanConversationContent(content, turn.role)
		urlutil.RewriteURLs(content, base)
		htmlContent, err := content.Html()
		if err != nil {
			return sdk.PreviewFallback(err)
		}
		b.WriteString(htmlContent)
		b.WriteString("</div>")
	}

	content := sanitizer.SanitizeHTML(b.String())
	if strings.TrimSpace(content) == "" {
		return sdk.PreviewFallback(fmt.Errorf("no preview content"))
	}
	return sdk.Previewed(sdk.PreviewResponse{Content: content})
}

func parseConversation(d *sdk.Document) (*goquery.Document, []conversationTurn, error) {
	if d == nil {
		return nil, nil, fmt.Errorf("nil document")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return nil, nil, err
	}
	return doc, findConversationTurns(doc), nil
}

func findConversationTurns(doc *goquery.Document) []conversationTurn {
	if doc == nil {
		return nil
	}
	turns := make([]conversationTurn, 0)
	// Visit all marker forms together so mixed markup retains document order.
	doc.Find(conversationRoleSelector).Each(func(_ int, roleNode *goquery.Selection) {
		role := conversationRole(roleNode)
		if role == "" || hasRoleAncestor(roleNode) || isHiddenElement(roleNode) {
			return
		}
		if _, explicit := roleNode.Attr("data-message-author-role"); !explicit && roleNode.Find(`[data-message-author-role]`).Length() > 0 {
			// Prefer explicit messages, even when they are hidden or unsupported.
			// Falling back to their wrapper would index controls or internal text.
			return
		}

		content := roleNode.Clone()
		cleanConversationContent(content, role)
		if strings.TrimSpace(conversationSelectionText(content)) == "" {
			return
		}
		turns = append(turns, conversationTurn{role: role, content: content})
	})
	return turns
}

func conversationRole(selection *goquery.Selection) string {
	if rawRole, ok := selection.Attr("data-message-author-role"); ok {
		return normalizeRole(rawRole)
	}
	if rawRole, ok := selection.Attr("data-turn"); ok {
		return normalizeRole(rawRole)
	}
	// Search units encode the role as the final component, for example
	// "fallback-turn-0:2:assistant". Both attributes may wrap the same message.
	for _, attribute := range []string{"data-chatgpt-search-unit-key", "data-content-search-unit-key"} {
		if key, ok := selection.Attr(attribute); ok {
			if separator := strings.LastIndexByte(key, ':'); separator > 0 {
				return normalizeRole(key[separator+1:])
			}
			return ""
		}
	}
	return ""
}

func normalizeRole(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "user":
		return "user"
	case "assistant":
		return "assistant"
	default:
		return ""
	}
}

func hasRoleAncestor(selection *goquery.Selection) bool {
	found := false
	selection.ParentsFiltered(conversationRoleSelector).EachWithBreak(func(_ int, ancestor *goquery.Selection) bool {
		_, explicit := ancestor.Attr("data-message-author-role")
		// A wrapper with explicit messages is not itself a turn. Other role
		// ancestors either already contain this text or mark internal content.
		found = explicit || conversationRole(ancestor) == "" || ancestor.Find(`[data-message-author-role]`).Length() == 0
		return !found
	})
	return found
}

func cleanConversationContent(content *goquery.Selection, role string) {
	if content == nil {
		return
	}
	content.Find(conversationRoleSelector).Each(func(_ int, nested *goquery.Selection) {
		if conversationRole(nested) != role {
			nested.Remove()
		}
	})
	if _, explicit := content.Attr("data-message-author-role"); !explicit {
		// Turn wrappers include an accessibility heading repeating the speaker.
		content.ChildrenFiltered(".sr-only").Remove()
	}
	content.Find(`[data-markdown-copy="exclude"], .turn-action-controls`).Remove()
	content.Find(`script, style, noscript, template, button, svg, img, picture, video, audio, iframe, embed, object, canvas, source, form, input, textarea, select, option`).Remove()
	content.Find(`[hidden], [aria-hidden]`).Each(func(_ int, nested *goquery.Selection) {
		if isHiddenElement(nested) {
			nested.Remove()
		}
	})
}

func isHiddenElement(selection *goquery.Selection) bool {
	if selection == nil || selection.Length() == 0 {
		return false
	}
	if hasHiddenMarker(selection) {
		return true
	}
	found := false
	selection.Parents().EachWithBreak(func(_ int, ancestor *goquery.Selection) bool {
		if hasHiddenMarker(ancestor) {
			found = true
			return false
		}
		return true
	})
	return found
}

func hasHiddenMarker(selection *goquery.Selection) bool {
	if _, ok := selection.Attr("hidden"); ok {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(selection.AttrOr("aria-hidden", "")), "true")
}

func documentTitle(d *sdk.Document, doc *goquery.Document) string {
	if d != nil {
		if title := sanitizer.SanitizeText(d.Title); title != "" {
			return title
		}
	}
	if doc != nil {
		return sanitizer.SanitizeText(doc.Find("title").First().Text())
	}
	return ""
}

func roleLabel(role string) string {
	if role == "assistant" {
		return "Assistant"
	}
	return "User"
}

func conversationText(turns []conversationTurn) string {
	var b strings.Builder
	for _, turn := range turns {
		text := conversationSelectionText(turn.content)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(roleLabel(turn.role))
		b.WriteString(":\n")
		b.WriteString(text)
	}
	return strings.TrimSpace(b.String())
}

var conversationBlockElements = map[string]bool{
	"address": true, "article": true, "blockquote": true, "dd": true, "div": true,
	"dl": true, "dt": true, "figcaption": true, "figure": true, "footer": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"header": true, "main": true, "p": true, "section": true,
}

type conversationTextWriter struct {
	b        strings.Builder
	rowCells []int
}

func conversationSelectionText(selection *goquery.Selection) string {
	if selection == nil || selection.Length() == 0 {
		return ""
	}
	writer := &conversationTextWriter{}
	for _, node := range selection.Nodes {
		writer.writeNode(node)
	}
	return normalizeConversationText(writer.b.String())
}

func (w *conversationTextWriter) writeNode(node *html.Node) {
	if node == nil {
		return
	}
	if node.Type == html.TextNode {
		w.b.WriteString(node.Data)
		return
	}
	if node.Type != html.ElementNode {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			w.writeNode(child)
		}
		return
	}

	name := strings.ToLower(node.Data)
	switch name {
	case "script", "style", "noscript", "template", "button", "svg", "img", "picture", "video", "audio", "iframe", "embed", "object", "canvas", "source", "form", "input", "textarea", "select", "option":
		return
	case "br":
		w.writeBreak()
		return
	case "pre":
		w.writeBreak()
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			w.writeNode(child)
		}
		w.writeBreak()
		return
	case "li":
		w.writeBreak()
		w.b.WriteString("- ")
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			w.writeNode(child)
		}
		w.writeBreak()
		return
	case "ul", "ol":
		w.writeBreak()
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			w.writeNode(child)
		}
		w.writeBreak()
		return
	case "table":
		w.writeBreak()
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			w.writeNode(child)
		}
		w.writeBreak()
		return
	case "tr":
		w.writeBreak()
		w.rowCells = append(w.rowCells, 0)
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			w.writeNode(child)
		}
		w.rowCells = w.rowCells[:len(w.rowCells)-1]
		w.writeBreak()
		return
	case "td", "th":
		if len(w.rowCells) > 0 {
			last := len(w.rowCells) - 1
			if w.rowCells[last] > 0 {
				w.b.WriteString(" | ")
			}
			w.rowCells[last]++
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			w.writeNode(child)
		}
		return
	}

	if conversationBlockElements[name] {
		w.writeBreak()
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		w.writeNode(child)
	}
	if conversationBlockElements[name] {
		w.writeBreak()
	}
}

func (w *conversationTextWriter) writeBreak() {
	if w.b.Len() > 0 && !strings.HasSuffix(w.b.String(), "\n") {
		w.b.WriteByte('\n')
	}
}

func normalizeConversationText(text string) string {
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			if len(cleaned) > 0 && !blank {
				cleaned = append(cleaned, "")
				blank = true
			}
			continue
		}
		cleaned = append(cleaned, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}
