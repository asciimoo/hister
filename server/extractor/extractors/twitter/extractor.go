// SPDX-License-Identifier: AGPL-3.0-or-later

// Package twitter extracts individual tweets from Twitter and X pages.
package twitter

import (
	"fmt"
	stdhtml "html"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/asciimoo/nogon/server/extractor/sdk"
	"github.com/asciimoo/nogon/server/extractor/urlutil"
	"github.com/asciimoo/nogon/server/sanitizer"
)

const tweetType = "tweet"

var twitterHosts = map[string]struct{}{
	"twitter.com":        {},
	"www.twitter.com":    {},
	"mobile.twitter.com": {},
	"m.twitter.com":      {},
	"x.com":              {},
	"www.x.com":          {},
	"mobile.x.com":       {},
}

type TwitterExtractor struct {
	sdk.ConfigSupport
}

func (e *TwitterExtractor) Name() string {
	return "Twitter"
}

func (e *TwitterExtractor) Description() string {
	return "Extracts tweets as individual documents from Twitter and X feeds, profiles, and tweet pages."
}

func (e *TwitterExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

func (e *TwitterExtractor) Match(d *sdk.Document) bool {
	if metadataType(d) == tweetType {
		return true
	}
	u, err := url.Parse(d.URL)
	if err != nil {
		return false
	}
	return isTwitterHost(u.Hostname())
}

func metadataType(d *sdk.Document) string {
	if d.Metadata == nil {
		return ""
	}
	v, _ := d.Metadata["type"].(string)
	return v
}

func isTwitterHost(host string) bool {
	_, ok := twitterHosts[strings.ToLower(host)]
	return ok
}

func (e *TwitterExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	if metadataType(d) == tweetType {
		return sdk.Extracted()
	}

	d.SkipIndexing = true
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return sdk.ExtractFallback(err)
	}
	base, err := url.Parse(d.URL)
	if err != nil {
		return sdk.ExtractFallback(err)
	}

	seen := make(map[string]struct{})
	found := false
	posts := findTweetSelections(doc)
	posts.Each(func(_ int, post *goquery.Selection) {
		td := tweetDocument(post, base, d.UserID)
		if td == nil {
			return
		}
		if _, ok := seen[td.URL]; ok {
			return
		}
		seen[td.URL] = struct{}{}
		found = true
		d.ExtraDocuments = append(d.ExtraDocuments, td)
	})

	if !found {
		if td := tweetDocumentFromPage(doc, base, d.UserID); td != nil {
			d.ExtraDocuments = append(d.ExtraDocuments, td)
		}
	}

	return sdk.Extracted()
}

func findTweetSelections(doc *goquery.Document) *goquery.Selection {
	return doc.Find(strings.Join([]string{
		`[itemtype$="/SocialMediaPosting"]`,
		`article[data-tweet-id]`,
		`article[data-testid="tweet"]`,
		`article[role="article"]`,
		`[data-testid="tweet"]`,
	}, ", "))
}

func tweetDocument(post *goquery.Selection, base *url.URL, userID uint) *sdk.Document {
	name, handle := tweetAuthor(post)
	tweetURL, urlHandle, ok := tweetStatusURL(post, base, handle)
	if !ok {
		return nil
	}
	if urlHandle != "" {
		handle = urlHandle
	}

	text, content := tweetText(post)
	text = replaceTweetLinkURLs(text, rewriteTweetLinks(content, base))
	urlutil.RewriteURLs(post, base)
	h := tweetContentHTML(post, content, text)
	author := formatAuthor(name, handle)

	metadata := map[string]any{"type": tweetType}
	if author != "" {
		metadata["author"] = author
	}
	if handle != "" {
		metadata["handle"] = "@" + handle
	}
	if published := tweetPublished(post); published != "" {
		metadata["published"] = published
	}

	title := "Twitter tweet"
	if author != "" {
		title += ": " + author
	}
	return &sdk.Document{
		URL:      tweetURL,
		Title:    title,
		Text:     text,
		HTML:     h,
		UserID:   userID,
		Metadata: metadata,
	}
}

func tweetText(post *goquery.Selection) (string, *goquery.Selection) {
	var semanticText string
	for _, selector := range []string{
		`meta[itemprop="articleBody"]`,
		`meta[itemprop="text"]`,
	} {
		post.Find(selector).EachWithBreak(func(_ int, s *goquery.Selection) bool {
			if value := strings.TrimSpace(s.AttrOr("content", "")); value != "" {
				semanticText = value
				return false
			}
			return true
		})
		if semanticText != "" {
			break
		}
	}

	var content *goquery.Selection
	for _, selector := range []string{
		`[data-testid="tweetText"]`,
		`[itemprop="articleBody"]:not(meta)`,
		`[itemprop="text"]:not(meta)`,
		`[data-contents="true"]`,
		`[lang][dir="auto"]`,
		`[dir="auto"]`,
	} {
		post.Find(selector).EachWithBreak(func(_ int, s *goquery.Selection) bool {
			candidate := strings.TrimSpace(s.Text())
			if candidate == "" || !relatedTweetText(semanticText, candidate) {
				return true
			}
			content = s
			return false
		})
		if content != nil {
			break
		}
	}

	if content != nil {
		return strings.TrimSpace(content.Text()), content
	}
	if semanticText != "" {
		return semanticText, nil
	}
	return "", nil
}

func relatedTweetText(semanticText, candidate string) bool {
	if semanticText == "" {
		return true
	}
	semanticText = normalizeTweetTextLinks(semanticText)
	candidate = normalizeTweetTextLinks(candidate)
	return strings.Contains(semanticText, candidate) || strings.Contains(candidate, semanticText)
}

func normalizeTweetTextLinks(text string) string {
	fields := strings.Fields(text)
	for i, field := range fields {
		candidate := strings.Trim(field, `.,!?;:()[]{}<>"'`)
		if isTCOURL(candidate) || parseOriginalTweetLinkURL(candidate, true) != "" {
			fields[i] = strings.Replace(field, candidate, "{url}", 1)
		}
	}
	return strings.Join(fields, " ")
}

func tweetContentHTML(post, content *goquery.Selection, text string) string {
	var b strings.Builder
	if content != nil {
		if h, err := content.Html(); err == nil && strings.TrimSpace(h) != "" {
			b.WriteString(h)
		}
	}
	if b.Len() == 0 && text != "" {
		b.WriteString(paragraphHTML(text))
	}

	media := post.Find(strings.Join([]string{
		`[data-testid="tweetPhoto"] img`,
		`img[src*="pbs.twimg.com/media/"]`,
		`video[poster]`,
	}, ", "))
	if media.Length() > 0 {
		b.WriteString("<figure>")
		media.Each(func(_ int, s *goquery.Selection) {
			if h, err := goquery.OuterHtml(s); err == nil {
				b.WriteString(h)
			}
		})
		b.WriteString("</figure>")
	}
	return b.String()
}

type tweetLink struct {
	shortURL    string
	originalURL string
}

func rewriteTweetLinks(content *goquery.Selection, base *url.URL) []tweetLink {
	if content == nil {
		return nil
	}
	links := make([]tweetLink, 0)
	content.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		shortURL := urlutil.ResolveURL(base, strings.TrimSpace(anchor.AttrOr("href", "")))
		if !isTCOURL(shortURL) {
			return
		}
		originalURL := originalTweetLinkURL(anchor)
		if originalURL == "" {
			anchor.RemoveAttr("href")
			return
		}

		anchor.SetAttr("href", originalURL)
		if strings.TrimSpace(anchor.Text()) == shortURL {
			anchor.SetText(originalURL)
		}
		links = append(links, tweetLink{shortURL: shortURL, originalURL: originalURL})
	})
	return links
}

func originalTweetLinkURL(anchor *goquery.Selection) string {
	for _, attr := range []string{"data-expanded-url", "title"} {
		raw := strings.TrimSpace(anchor.AttrOr(attr, ""))
		if originalURL := parseOriginalTweetLinkURL(raw, false); originalURL != "" {
			return originalURL
		}
	}
	return parseOriginalTweetLinkURL(strings.TrimSpace(anchor.Text()), true)
}

func parseOriginalTweetLinkURL(raw string, allowMissingScheme bool) string {
	if raw == "" || strings.ContainsAny(raw, " \t\r\n…") || strings.Contains(raw, "...") {
		return ""
	}
	if allowMissingScheme && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		!strings.Contains(u.Hostname(), ".") ||
		isTCOURL(u.String()) {
		return ""
	}
	return u.String()
}

func replaceTweetLinkURLs(text string, links []tweetLink) string {
	for _, link := range links {
		if link.shortURL != "" {
			text = strings.ReplaceAll(text, link.shortURL, link.originalURL)
		}
	}
	return text
}

func isTCOURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "t.co" || host == "www.t.co"
}

func tweetAuthor(post *goquery.Selection) (string, string) {
	author := post.Find(`[itemprop="author"]`).First()
	if author.Length() > 0 {
		name := firstMetaContent(author, `meta[itemprop="name"]`)
		handle := strings.TrimPrefix(firstMetaContent(author, `meta[itemprop="alternateName"]`), "@")
		if name != "" || handle != "" {
			return cleanAuthor(name, handle)
		}
	}

	var name, handle string
	userName := post.Find(`[data-testid="User-Name"]`).First()
	userName.Find("a").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		text := strings.TrimSpace(s.Text())
		if textHandle, ok := strings.CutPrefix(text, "@"); ok {
			if handle == "" {
				handle = textHandle
			}
		} else if text != "" && name == "" {
			name = text
		}
		return name == "" || handle == ""
	})

	if name == "" || handle == "" {
		post.Find("a[href]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			text := strings.TrimSpace(s.Text())
			profileHandle, ok := twitterProfileHandle(s.AttrOr("href", ""))
			if !ok || text == "" {
				return true
			}
			if textHandle, isHandle := strings.CutPrefix(text, "@"); isHandle {
				if handle == "" {
					handle = textHandle
				}
			} else if name == "" && (handle == "" || strings.EqualFold(handle, profileHandle)) {
				name = text
			}
			if handle == "" {
				handle = profileHandle
			}
			return name == "" || handle == ""
		})
	}
	return cleanAuthor(name, handle)
}

func twitterProfileHandle(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	if u.IsAbs() && !isTwitterHost(u.Hostname()) {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	if len(parts) != 1 {
		return "", false
	}
	handle, err := url.PathUnescape(parts[0])
	return handle, err == nil && validHandle(handle)
}

func cleanAuthor(name, handle string) (string, string) {
	name = sanitizer.SanitizeText(name)
	handle = sanitizer.SanitizeText(handle)
	if !validHandle(handle) {
		handle = ""
	}
	return name, handle
}

func tweetPublished(post *goquery.Selection) string {
	if value := firstMetaContent(post, `meta[itemprop="datePublished"]`); value != "" {
		return value
	}
	if value := firstMetaContent(post, `meta[itemprop="dateCreated"]`); value != "" {
		return value
	}
	return strings.TrimSpace(post.Find("time[datetime]").First().AttrOr("datetime", ""))
}

func firstMetaContent(s *goquery.Selection, selector string) string {
	return strings.TrimSpace(s.Find(selector).First().AttrOr("content", ""))
}

func tweetStatusURL(post *goquery.Selection, base *url.URL, handle string) (string, string, bool) {
	candidates := make([]string, 0)
	post.Find(`meta[itemprop="url"]`).Each(func(_ int, s *goquery.Selection) {
		candidates = append(candidates, s.AttrOr("content", ""))
	})
	post.Find(`link[itemprop="url"]`).Each(func(_ int, s *goquery.Selection) {
		candidates = append(candidates, s.AttrOr("href", ""))
	})
	if itemID, ok := post.Attr("itemid"); ok {
		candidates = append(candidates, itemID)
	}
	post.Find("time").Each(func(_ int, s *goquery.Selection) {
		if href, ok := s.Closest("a[href]").Attr("href"); ok {
			candidates = append(candidates, href)
		}
	})
	post.Find(`a[href*="/status/"]`).Each(func(_ int, s *goquery.Selection) {
		candidates = append(candidates, s.AttrOr("href", ""))
	})

	for _, candidate := range candidates {
		if statusURL, urlHandle, ok := canonicalStatusURL(candidate, base); ok {
			if urlHandle == "" && validHandle(handle) {
				statusURL = statusURLFor(handle, statusID(statusURL))
				urlHandle = handle
			}
			return statusURL, urlHandle, true
		}
	}

	if id := post.AttrOr("data-tweet-id", ""); validStatusID(id) {
		if validHandle(handle) {
			return statusURLFor(handle, id), handle, true
		}
		return statusURLFor("", id), "", true
	}
	if base != nil {
		return canonicalStatusURL(base.String(), base)
	}
	return "", "", false
}

func canonicalStatusURL(raw string, base *url.URL) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	if base != nil {
		raw = urlutil.ResolveURL(base, raw)
	}
	u, err := url.Parse(raw)
	if err != nil || !isTwitterHost(u.Hostname()) {
		return "", "", false
	}

	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	for i, part := range parts {
		if part != "status" || i+1 >= len(parts) {
			continue
		}
		id, err := url.PathUnescape(parts[i+1])
		if err != nil || !validStatusID(id) {
			return "", "", false
		}
		var handle string
		if i == 1 {
			handle, err = url.PathUnescape(parts[0])
			if err != nil || !validHandle(handle) {
				handle = ""
			}
		}
		return statusURLFor(handle, id), handle, true
	}
	return "", "", false
}

func validStatusID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validHandle(handle string) bool {
	if handle == "" || strings.EqualFold(handle, "i") || strings.EqualFold(handle, "web") {
		return false
	}
	for _, r := range handle {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func statusURLFor(handle, id string) string {
	if handle == "" {
		return "https://x.com/i/status/" + id
	}
	return "https://x.com/" + handle + "/status/" + id
}

func statusID(statusURL string) string {
	parts := strings.Split(strings.Trim(statusURL, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func formatAuthor(name, handle string) string {
	name = strings.TrimSpace(name)
	handle = strings.TrimPrefix(strings.TrimSpace(handle), "@")
	if name != "" && handle != "" {
		return name + " (@" + handle + ")"
	}
	if name != "" {
		return name
	}
	if handle != "" {
		return "@" + handle
	}
	return ""
}

func tweetDocumentFromPage(doc *goquery.Document, base *url.URL, userID uint) *sdk.Document {
	if base == nil {
		return nil
	}
	statusURL, handle, ok := canonicalStatusURL(base.String(), base)
	if !ok {
		return nil
	}

	text := firstPageMeta(doc,
		`meta[property="og:description"]`,
		`meta[name="twitter:description"]`,
		`meta[name="description"]`,
	)
	if text == "" {
		return nil
	}

	name, titleHandle := authorFromPageTitle(firstPageMeta(doc,
		`meta[property="og:title"]`,
		`meta[name="twitter:title"]`,
		`meta[name="title"]`,
	))
	if titleHandle != "" {
		handle = titleHandle
		statusURL = statusURLFor(handle, statusID(statusURL))
	}
	if handle == "" {
		if authorURL := firstPageMeta(doc, `meta[property="article:author"]`); authorURL != "" {
			if u, err := url.Parse(authorURL); err == nil && isTwitterHost(u.Hostname()) {
				candidate := strings.Trim(u.Path, "/")
				if validHandle(candidate) {
					handle = candidate
					statusURL = statusURLFor(handle, statusID(statusURL))
				}
			}
		}
	}
	author := formatAuthor(name, handle)

	metadata := map[string]any{"type": tweetType}
	if author != "" {
		metadata["author"] = author
	}
	if handle != "" {
		metadata["handle"] = "@" + handle
	}
	if published := firstPageMeta(doc, `meta[property="article:published_time"]`); published != "" {
		metadata["published"] = published
	}
	image := firstPageMeta(doc,
		`meta[property="og:image"]`,
		`meta[name="twitter:image"]`,
	)
	if image != "" {
		metadata["image"] = image
	}

	title := "Twitter tweet"
	if author != "" {
		title += ": " + author
	}
	return &sdk.Document{
		URL:      statusURL,
		Title:    title,
		Text:     text,
		HTML:     pageTweetHTML(text, image, base),
		UserID:   userID,
		Metadata: metadata,
	}
}

func firstPageMeta(doc *goquery.Document, selectors ...string) string {
	for _, selector := range selectors {
		if value := strings.TrimSpace(doc.Find(selector).First().AttrOr("content", "")); value != "" {
			return value
		}
	}
	return ""
}

func authorFromPageTitle(title string) (string, string) {
	title = strings.TrimSpace(title)
	for _, marker := range []string{" on X:", " on Twitter:"} {
		if before, _, ok := strings.Cut(title, marker); ok {
			title = before
			break
		}
	}
	for _, suffix := range []string{" on X", " on Twitter"} {
		title = strings.TrimSuffix(title, suffix)
	}
	open := strings.LastIndex(title, " (@")
	if open >= 0 && strings.HasSuffix(title, ")") {
		name := strings.TrimSpace(title[:open])
		handle := strings.TrimSuffix(title[open+3:], ")")
		if validHandle(handle) {
			return sanitizer.SanitizeText(name), sanitizer.SanitizeText(handle)
		}
	}
	return sanitizer.SanitizeText(title), ""
}

func paragraphHTML(text string) string {
	escaped := stdhtml.EscapeString(strings.ReplaceAll(text, "\r\n", "\n"))
	return "<p>" + strings.ReplaceAll(escaped, "\n", "<br>") + "</p>"
}

func pageTweetHTML(text, image string, base *url.URL) string {
	var b strings.Builder
	b.WriteString(paragraphHTML(text))
	image = urlutil.ResolveURL(base, strings.TrimSpace(image))
	u, err := url.Parse(image)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		fmt.Fprintf(&b, `<figure><img src="%s" alt=""></figure>`, stdhtml.EscapeString(image))
	}
	return b.String()
}

func (e *TwitterExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	var b strings.Builder
	if title := strings.TrimSpace(d.Title); title != "" {
		fmt.Fprintf(&b, "<h2>%s</h2>", stdhtml.EscapeString(title))
	}
	if strings.TrimSpace(d.HTML) != "" {
		b.WriteString(d.HTML)
	} else if strings.TrimSpace(d.Text) != "" {
		b.WriteString(paragraphHTML(d.Text))
	}
	return sdk.Previewed(sdk.PreviewResponse{
		Content: sanitizer.SanitizeHTML(b.String()),
	})
}
