// SPDX-License-Identifier: AGPL-3.0-or-later

// Package bluesky extracts individual posts from Bluesky pages.
package bluesky

import (
	"encoding/json"
	"fmt"
	stdhtml "html"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/asciimoo/nogon/server/extractor/sdk"
	"github.com/asciimoo/nogon/server/extractor/urlutil"
	"github.com/asciimoo/nogon/server/sanitizer"
)

const postType = "bluesky"

var blueskyHosts = map[string]struct{}{
	"bsky.app":       {},
	"www.bsky.app":   {},
	"embed.bsky.app": {},
}

// BlueskyExtractor extracts Bluesky posts from profiles, feeds, and threads.
type BlueskyExtractor struct {
	sdk.ConfigSupport
}

func (e *BlueskyExtractor) Name() string {
	return "Bluesky"
}

func (e *BlueskyExtractor) Description() string {
	return "Extracts Bluesky posts as individual documents from profiles, feeds, and post pages."
}

func (e *BlueskyExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

func (e *BlueskyExtractor) Match(d *sdk.Document) bool {
	if metadataType(d) == postType {
		return true
	}
	u, err := url.Parse(d.URL)
	if err != nil {
		return false
	}
	return isBlueskyHost(u.Hostname())
}

func metadataType(d *sdk.Document) string {
	if d.Metadata == nil {
		return ""
	}
	v, _ := d.Metadata["type"].(string)
	return v
}

func isBlueskyHost(host string) bool {
	_, ok := blueskyHosts[strings.ToLower(host)]
	return ok
}

func (e *BlueskyExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	if metadataType(d) == postType {
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

	posts := make([]*sdk.Document, 0)
	byURL := make(map[string]int)
	for _, node := range schemaPostNodes(doc) {
		post := schemaPostDocument(node, base, d.UserID)
		appendPost(&posts, byURL, post)
	}
	for _, selection := range renderedPostSelections(doc) {
		post := renderedPostDocument(selection, base, d.UserID)
		appendPost(&posts, byURL, post)
	}
	if len(posts) == 0 {
		appendPost(&posts, byURL, pagePostDocument(doc, base, d.UserID))
	}

	d.ExtraDocuments = append(d.ExtraDocuments, posts...)
	return sdk.Extracted()
}

func appendPost(posts *[]*sdk.Document, byURL map[string]int, candidate *sdk.Document) {
	if candidate == nil {
		return
	}
	if index, ok := byURL[candidate.URL]; ok {
		mergePost((*posts)[index], candidate)
		return
	}
	byURL[candidate.URL] = len(*posts)
	*posts = append(*posts, candidate)
}

func mergePost(existing, candidate *sdk.Document) {
	if candidate.Text != "" {
		existing.Text = candidate.Text
	}
	if candidate.HTML != "" {
		existing.HTML = candidate.HTML
	}
	if existing.Title == "Bluesky post" && candidate.Title != "" {
		existing.Title = candidate.Title
	}
	if existing.Metadata == nil {
		existing.Metadata = map[string]any{"type": postType}
	}
	for key, value := range candidate.Metadata {
		if current, ok := existing.Metadata[key]; !ok || current == "" {
			existing.Metadata[key] = value
		}
	}
}

func schemaPostNodes(doc *goquery.Document) []map[string]any {
	var posts []map[string]any
	doc.Find("script").Each(func(_ int, script *goquery.Selection) {
		if !strings.EqualFold(strings.TrimSpace(script.AttrOr("type", "")), "application/ld+json") {
			return
		}
		var value any
		if err := json.Unmarshal([]byte(script.Text()), &value); err != nil {
			return
		}
		collectSchemaPosts(value, &posts, 0)
	})
	return posts
}

func collectSchemaPosts(value any, posts *[]map[string]any, depth int) {
	if depth > 20 {
		return
	}
	switch node := value.(type) {
	case []any:
		for _, item := range node {
			collectSchemaPosts(item, posts, depth+1)
		}
	case map[string]any:
		if isSchemaPost(node) {
			*posts = append(*posts, node)
			collectSchemaPosts(node["comment"], posts, depth+1)
			return
		}
		for _, key := range []string{
			"@graph", "mainEntity", "hasPart", "itemListElement", "item", "comment",
		} {
			collectSchemaPosts(node[key], posts, depth+1)
		}
	}
}

func isSchemaPost(node map[string]any) bool {
	for _, typ := range schemaTypes(node["@type"]) {
		typ = schemaTypeName(typ)
		if typ == "DiscussionForumPosting" || typ == "SocialMediaPosting" || typ == "Comment" {
			return true
		}
	}
	return false
}

func schemaTypes(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if value, ok := item.(string); ok {
				values = append(values, value)
			}
		}
		return values
	default:
		return nil
	}
}

func schemaTypeName(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.LastIndexAny(value, "/#"); index >= 0 {
		return value[index+1:]
	}
	return value
}

func schemaPostDocument(node map[string]any, base *url.URL, userID uint) *sdk.Document {
	postURL, _, _, ok := canonicalPostURL(firstSchemaString(node, "url", "mainEntityOfPage"), base)
	if !ok {
		return nil
	}
	text := sanitizer.SanitizeText(firstSchemaString(node, "text", "articleBody", "description"))
	images := schemaImages(node, base)
	if text == "" && len(images) == 0 {
		return nil
	}

	name, handle, did := schemaAuthor(node["author"])
	if handle == "" {
		_, actor, _, _ := canonicalPostURL(postURL, base)
		if !strings.HasPrefix(strings.ToLower(actor), "did:") {
			handle = actor
		}
	}
	author := formatAuthor(name, handle)
	metadata := postMetadata(author, handle, firstSchemaString(node, "datePublished", "dateCreated"))
	if did != "" {
		metadata["did"] = did
	}
	if identifier := firstSchemaString(node, "identifier"); strings.HasPrefix(identifier, "at://") {
		metadata["at_uri"] = sanitizer.SanitizeText(identifier)
	}
	if language := firstSchemaString(node, "inLanguage"); language != "" {
		metadata["language"] = sanitizer.SanitizeText(language)
	}
	if len(images) > 0 {
		metadata["image"] = images[0]
	}

	html := schemaPostHTML(text, images)
	if quoted := schemaURL(node["isBasedOn"], base); quoted != "" {
		metadata["quoted_post"] = quoted
		html += fmt.Sprintf(
			`<blockquote><p><a href="%s">Quoted Bluesky post</a></p></blockquote>`,
			stdhtml.EscapeString(quoted),
		)
	}
	if cardHTML, cardURL := schemaCardHTML(node["sharedContent"], base); cardHTML != "" {
		html += cardHTML
		metadata["external_url"] = cardURL
	}

	return &sdk.Document{
		URL:      postURL,
		Title:    postTitle(author),
		Text:     text,
		HTML:     html,
		UserID:   userID,
		Metadata: metadata,
	}
}

func firstSchemaString(node map[string]any, keys ...string) string {
	for _, key := range keys {
		switch value := node[key].(type) {
		case string:
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		case map[string]any:
			if value := firstSchemaString(value, "url", "@id"); value != "" {
				return value
			}
		}
	}
	return ""
}

func schemaAuthor(value any) (string, string, string) {
	var author map[string]any
	switch typed := value.(type) {
	case map[string]any:
		author = typed
	case []any:
		for _, item := range typed {
			if candidate, ok := item.(map[string]any); ok {
				author = candidate
				break
			}
		}
	}
	if author == nil {
		return "", "", ""
	}
	name := sanitizer.SanitizeText(firstSchemaString(author, "name"))
	handle := strings.TrimPrefix(sanitizer.SanitizeText(firstSchemaString(author, "alternateName")), "@")
	if !validHandle(handle) {
		handle = ""
	}
	did := sanitizer.SanitizeText(firstSchemaString(author, "identifier"))
	if !strings.HasPrefix(strings.ToLower(did), "did:") {
		did = ""
	}
	return name, handle, did
}

func schemaImages(node map[string]any, base *url.URL) []string {
	images := make([]string, 0)
	seen := make(map[string]struct{})
	for _, key := range []string{"image", "thumbnailUrl"} {
		collectSchemaImages(node[key], base, &images, seen)
	}
	return images
}

func collectSchemaImages(value any, base *url.URL, images *[]string, seen map[string]struct{}) {
	switch typed := value.(type) {
	case string:
		appendImage(images, seen, typed, base)
	case []any:
		for _, item := range typed {
			collectSchemaImages(item, base, images, seen)
		}
	case map[string]any:
		appendImage(images, seen, firstSchemaString(typed, "contentUrl", "url", "thumbnailUrl"), base)
	}
}

func appendImage(images *[]string, seen map[string]struct{}, raw string, base *url.URL) {
	image := safeHTTPURL(raw, base)
	if image == "" {
		return
	}
	if _, ok := seen[image]; ok {
		return
	}
	seen[image] = struct{}{}
	*images = append(*images, image)
}

func schemaPostHTML(text string, images []string) string {
	var builder strings.Builder
	if text != "" {
		builder.WriteString(paragraphHTML(text))
	}
	if len(images) > 0 {
		builder.WriteString("<figure>")
		for _, image := range images {
			fmt.Fprintf(&builder, `<img src="%s" alt="">`, stdhtml.EscapeString(image))
		}
		builder.WriteString("</figure>")
	}
	return builder.String()
}

func schemaCardHTML(value any, base *url.URL) (string, string) {
	card, ok := value.(map[string]any)
	if !ok {
		return "", ""
	}
	cardURL := safeHTTPURL(firstSchemaString(card, "url"), base)
	if cardURL == "" {
		return "", ""
	}
	title := sanitizer.SanitizeText(firstSchemaString(card, "headline", "name"))
	description := sanitizer.SanitizeText(firstSchemaString(card, "description"))
	if title == "" {
		title = cardURL
	}

	var builder strings.Builder
	builder.WriteString("<aside><p>")
	fmt.Fprintf(
		&builder,
		`<a href="%s">%s</a>`,
		stdhtml.EscapeString(cardURL),
		stdhtml.EscapeString(title),
	)
	builder.WriteString("</p>")
	if description != "" {
		builder.WriteString(paragraphHTML(description))
	}
	images := schemaImages(card, base)
	if len(images) > 0 {
		fmt.Fprintf(&builder, `<img src="%s" alt="">`, stdhtml.EscapeString(images[0]))
	}
	builder.WriteString("</aside>")
	return builder.String(), cardURL
}

func schemaURL(value any, base *url.URL) string {
	raw := ""
	switch typed := value.(type) {
	case string:
		raw = typed
	case map[string]any:
		raw = firstSchemaString(typed, "url", "@id")
	}
	postURL, _, _, ok := canonicalPostURL(raw, base)
	if !ok {
		return ""
	}
	return postURL
}

func renderedPostSelections(doc *goquery.Document) []*goquery.Selection {
	posts := make([]*goquery.Selection, 0)
	seenNodes := make(map[any]struct{})
	appendSelection := func(selection *goquery.Selection) {
		if selection == nil || selection.Length() == 0 || len(selection.Nodes) == 0 {
			return
		}
		node := selection.Nodes[0]
		if _, ok := seenNodes[node]; ok {
			return
		}
		seenNodes[node] = struct{}{}
		posts = append(posts, selection.First())
	}

	doc.Find(strings.Join([]string{
		`[data-testid^="feedItem-by-"]`,
		`[data-testid^="postThreadItem-by-"]`,
		`article`,
		`[role="article"]`,
	}, ", ")).Each(func(_ int, post *goquery.Selection) {
		appendSelection(post)
	})

	doc.Find(`a[href*="/profile/"][href*="/post/"]`).Each(func(_ int, anchor *goquery.Selection) {
		if anchor.Find("time").Length() == 0 &&
			anchor.AttrOr("data-tooltip", "") == "" &&
			anchor.AttrOr("aria-label", "") == "" {
			return
		}
		if container := semanticPostContainer(anchor); container != nil {
			appendSelection(container)
		}
	})
	return posts
}

func semanticPostContainer(anchor *goquery.Selection) *goquery.Selection {
	current := anchor
	for range 14 {
		current = current.Parent()
		if current.Length() == 0 {
			return nil
		}
		if current.Is(`article, [role="article"], [data-testid^="feedItem-by-"], [data-testid^="postThreadItem-by-"]`) {
			return current
		}
		if current.Find(`[data-testid="postText"]`).Length() > 0 {
			return current
		}
	}
	return nil
}

func renderedPostDocument(post *goquery.Selection, base *url.URL, userID uint) *sdk.Document {
	postURL, handle, ok := renderedPostURL(post, base)
	if !ok {
		return nil
	}
	name, authorHandle := renderedAuthor(post, handle)
	if authorHandle != "" {
		handle = authorHandle
	}
	author := formatAuthor(name, handle)

	content := firstSelection(post,
		`[data-testid="contentHider-post"]`,
		`[itemprop="articleBody"]:not(meta)`,
		`[data-testid="postText"]`,
	)
	textSelection := firstSelection(post,
		`[data-testid="postText"]`,
		`[itemprop="articleBody"]:not(meta)`,
	)
	text := ""
	if textSelection != nil {
		text = sanitizer.SanitizeText(textSelection.Text())
	} else if content != nil {
		text = sanitizer.SanitizeText(content.Text())
	}
	if text == "" && (content == nil || content.Find("img, video").Length() == 0) {
		return nil
	}

	html := ""
	if content != nil {
		urlutil.RewriteURLs(content, base)
		html, _ = content.Html()
	}
	if strings.TrimSpace(html) == "" && text != "" {
		html = paragraphHTML(text)
	}

	metadata := postMetadata(author, handle, renderedPublished(post, postURL, base))
	return &sdk.Document{
		URL:      postURL,
		Title:    postTitle(author),
		Text:     text,
		HTML:     html,
		UserID:   userID,
		Metadata: metadata,
	}
}

func firstSelection(root *goquery.Selection, selectors ...string) *goquery.Selection {
	for _, selector := range selectors {
		selection := root.Find(selector).First()
		if selection.Length() > 0 {
			return selection
		}
	}
	return nil
}

func renderedPostURL(post *goquery.Selection, base *url.URL) (string, string, bool) {
	type candidate struct {
		raw   string
		score int
	}
	candidates := make([]candidate, 0)
	expectedHandle := handleFromTestID(post.AttrOr("data-testid", ""))

	post.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		raw := strings.TrimSpace(anchor.AttrOr("href", ""))
		postURL, actor, _, ok := canonicalPostURL(raw, base)
		if !ok {
			return
		}
		score := 1
		if anchor.Find("time").Length() > 0 || anchor.Is("time") {
			score += 5
		}
		if anchor.AttrOr("data-tooltip", "") != "" || anchor.AttrOr("aria-label", "") != "" {
			score += 4
		}
		if expectedHandle != "" && strings.EqualFold(actor, expectedHandle) {
			score += 3
		}
		candidates = append(candidates, candidate{raw: postURL, score: score})
	})
	post.Find(`meta[itemprop="url"]`).Each(func(_ int, item *goquery.Selection) {
		candidates = append(candidates, candidate{raw: item.AttrOr("content", ""), score: 8})
	})
	post.Find(`link[itemprop="url"]`).Each(func(_ int, item *goquery.Selection) {
		candidates = append(candidates, candidate{raw: item.AttrOr("href", ""), score: 8})
	})
	for _, attr := range []string{"href", "itemid"} {
		if raw := post.AttrOr(attr, ""); raw != "" {
			candidates = append(candidates, candidate{raw: raw, score: 2})
		}
	}

	bestScore := -1
	bestURL := ""
	bestActor := ""
	for _, candidate := range candidates {
		postURL, actor, _, ok := canonicalPostURL(candidate.raw, base)
		if ok && candidate.score > bestScore {
			bestScore = candidate.score
			bestURL = postURL
			bestActor = actor
		}
	}
	return bestURL, bestActor, bestURL != ""
}

func handleFromTestID(testID string) string {
	for _, marker := range []string{"feedItem-by-", "postThreadItem-by-"} {
		if _, handle, ok := strings.Cut(testID, marker); ok && validHandle(handle) {
			return handle
		}
	}
	return ""
}

func renderedAuthor(post *goquery.Selection, urlActor string) (string, string) {
	handle := handleFromTestID(post.AttrOr("data-testid", ""))
	if handle == "" && !strings.HasPrefix(strings.ToLower(urlActor), "did:") && validHandle(urlActor) {
		handle = urlActor
	}

	name := ""
	for _, selector := range []string{
		`[data-testid*="DisplayName"]`,
		`[data-testid*="displayName"]`,
		`[itemprop="author"] [itemprop="name"]`,
	} {
		candidate := strings.TrimSpace(post.Find(selector).First().Text())
		if candidate != "" {
			name = candidate
			break
		}
	}
	if name == "" {
		post.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
			actor, ok := profileActor(anchor.AttrOr("href", ""))
			if !ok || (handle != "" && !strings.EqualFold(actor, handle)) {
				return true
			}
			candidate := strings.TrimSpace(anchor.Text())
			if candidate == "" || strings.HasPrefix(candidate, "@") || strings.EqualFold(candidate, actor) {
				return true
			}
			name = candidate
			if handle == "" && validHandle(actor) {
				handle = actor
			}
			return false
		})
	}
	if name == "" {
		post.Find(`[aria-label$="'s avatar"]`).EachWithBreak(func(_ int, avatar *goquery.Selection) bool {
			candidate := strings.TrimSuffix(strings.TrimSpace(avatar.AttrOr("aria-label", "")), "'s avatar")
			if candidate != "" {
				name = candidate
				return false
			}
			return true
		})
	}
	return sanitizer.SanitizeText(name), sanitizer.SanitizeText(handle)
}

func profileActor(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.IsAbs() && !isBlueskyHost(u.Hostname())) {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	if len(parts) != 2 || parts[0] != "profile" {
		return "", false
	}
	actor, err := url.PathUnescape(parts[1])
	return actor, err == nil && actor != ""
}

func renderedPublished(post *goquery.Selection, postURL string, base *url.URL) string {
	if published := strings.TrimSpace(post.Find("time[datetime]").First().AttrOr("datetime", "")); published != "" {
		return sanitizer.SanitizeText(published)
	}
	value := ""
	post.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		candidateURL, _, _, ok := canonicalPostURL(anchor.AttrOr("href", ""), base)
		if !ok || candidateURL != postURL {
			return true
		}
		value = strings.TrimSpace(anchor.AttrOr("data-tooltip", ""))
		if value == "" {
			value = strings.TrimSpace(anchor.AttrOr("aria-label", ""))
		}
		return value == ""
	})
	return sanitizer.SanitizeText(value)
}

func pagePostDocument(doc *goquery.Document, base *url.URL, userID uint) *sdk.Document {
	if base == nil {
		return nil
	}
	postURL, actor, _, ok := canonicalPostURL(base.String(), base)
	if !ok {
		return nil
	}
	text := firstPageMeta(doc,
		`meta[property="og:description"]`,
		`meta[name="twitter:description"]`,
		`meta[name="description"]`,
	)
	text = sanitizer.SanitizeText(text)
	if text == "" {
		return nil
	}

	name, titleHandle := authorFromPageTitle(firstPageMeta(doc,
		`meta[property="og:title"]`,
		`meta[name="twitter:title"]`,
		`meta[name="title"]`,
	))
	handle := titleHandle
	if handle == "" && !strings.HasPrefix(strings.ToLower(actor), "did:") {
		handle = actor
	}
	author := formatAuthor(name, handle)
	image := safeHTTPURL(firstPageMeta(doc,
		`meta[property="og:image"]`,
		`meta[name="twitter:image"]`,
	), base)
	metadata := postMetadata(author, handle, firstPageMeta(doc, `meta[property="article:published_time"]`))
	if image != "" {
		metadata["image"] = image
	}

	return &sdk.Document{
		URL:      postURL,
		Title:    postTitle(author),
		Text:     text,
		HTML:     schemaPostHTML(text, compactStrings(image)),
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
	title = sanitizer.SanitizeText(title)
	for _, suffix := range []string{" on Bluesky", " | Bluesky", " • Bluesky"} {
		title = strings.TrimSuffix(title, suffix)
	}
	open := strings.LastIndex(title, " (@")
	if open >= 0 && strings.HasSuffix(title, ")") {
		name := strings.TrimSpace(title[:open])
		handle := strings.TrimSuffix(title[open+3:], ")")
		if validHandle(handle) {
			return name, handle
		}
	}
	return title, ""
}

func postMetadata(author, handle, published string) map[string]any {
	metadata := map[string]any{"type": postType}
	if author != "" {
		metadata["author"] = author
	}
	if handle != "" {
		metadata["handle"] = "@" + strings.TrimPrefix(handle, "@")
	}
	if published = sanitizer.SanitizeText(published); published != "" {
		metadata["published"] = published
	}
	return metadata
}

func postTitle(author string) string {
	if author == "" {
		return "Bluesky post"
	}
	return "Bluesky post: " + author
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

func canonicalPostURL(raw string, base *url.URL) (string, string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", false
	}
	if base != nil {
		raw = urlutil.ResolveURL(base, raw)
	}
	u, err := url.Parse(raw)
	if err != nil || !isBlueskyHost(u.Hostname()) {
		return "", "", "", false
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	if len(parts) != 4 || parts[0] != "profile" || parts[2] != "post" {
		return "", "", "", false
	}
	actor, err := url.PathUnescape(parts[1])
	if err != nil || !validActor(actor) {
		return "", "", "", false
	}
	postID, err := url.PathUnescape(parts[3])
	if err != nil || !validPostID(postID) {
		return "", "", "", false
	}
	canonical := (&url.URL{
		Scheme: "https",
		Host:   "bsky.app",
		Path:   "/profile/" + actor + "/post/" + postID,
	}).String()
	return canonical, actor, postID, true
}

func validActor(actor string) bool {
	if strings.HasPrefix(strings.ToLower(actor), "did:") {
		return !strings.ContainsAny(actor, "/?# ")
	}
	return validHandle(actor)
}

func validHandle(handle string) bool {
	handle = strings.TrimSpace(handle)
	if handle == "" || strings.HasPrefix(handle, ".") || strings.HasSuffix(handle, ".") {
		return false
	}
	for _, char := range handle {
		if (char < 'a' || char > 'z') &&
			(char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') &&
			char != '-' && char != '.' {
			return false
		}
	}
	return true
}

func validPostID(postID string) bool {
	if postID == "" {
		return false
	}
	for _, char := range postID {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func safeHTTPURL(raw string, base *url.URL) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if base != nil {
		raw = urlutil.ResolveURL(base, raw)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.String()
}

func compactStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func paragraphHTML(text string) string {
	escaped := stdhtml.EscapeString(strings.ReplaceAll(text, "\r\n", "\n"))
	return "<p>" + strings.ReplaceAll(escaped, "\n", "<br>") + "</p>"
}

func (e *BlueskyExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	var builder strings.Builder
	if title := strings.TrimSpace(d.Title); title != "" {
		fmt.Fprintf(&builder, "<h2>%s</h2>", stdhtml.EscapeString(title))
	}
	if strings.TrimSpace(d.HTML) != "" {
		builder.WriteString(d.HTML)
	} else if strings.TrimSpace(d.Text) != "" {
		builder.WriteString(paragraphHTML(d.Text))
	}
	return sdk.Previewed(sdk.PreviewResponse{
		Content: sanitizer.SanitizeHTML(builder.String()),
	})
}
