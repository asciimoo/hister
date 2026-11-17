// Package reddit extracts posts and comments from Reddit post pages.
package reddit

import (
	"encoding/json"
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

type RedditExtractor struct {
	sdk.ConfigSupport
}

type redditPost struct {
	Title        string
	Author       string
	Subreddit    string
	Score        string
	Published    string
	Flair        string
	Link         string
	BodyText     string
	BodyHTML     string
	PostID       string
	CommentCount string
	Comments     []redditComment
}

type redditComment struct {
	ID        string
	Author    string
	Score     string
	Published string
	Text      string
	HTML      string
	Depth     int
}

func (e *RedditExtractor) Name() string {
	return "Reddit"
}

func (e *RedditExtractor) Description() string {
	return "Extracts a Reddit post and every comment already present on its post page."
}

func (e *RedditExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

func (e *RedditExtractor) Match(d *sdk.Document) bool {
	_, _, ok := redditPostURL(d.URL)
	return ok
}

func redditPostURL(rawURL string) (string, string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", "", false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	parts := pathParts(u.Path)

	if host == "redd.it" {
		if len(parts) == 1 && validRedditID(parts[0]) {
			return strings.ToLower(parts[0]), "", true
		}
		return "", "", false
	}
	if host != "reddit.com" && !strings.HasSuffix(host, ".reddit.com") {
		return "", "", false
	}

	trimmedPath := strings.ToLower(strings.TrimSuffix(u.Path, "/"))
	for _, suffix := range []string{".json", ".rss", ".xml"} {
		if strings.HasSuffix(trimmedPath, suffix) {
			return "", "", false
		}
	}

	if len(parts) >= 4 && strings.EqualFold(parts[0], "r") && strings.EqualFold(parts[2], "comments") && validRedditID(parts[3]) {
		return strings.ToLower(parts[3]), parts[1], true
	}
	if len(parts) >= 2 && strings.EqualFold(parts[0], "comments") && validRedditID(parts[1]) {
		return strings.ToLower(parts[1]), "", true
	}
	return "", "", false
}

func pathParts(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return nil
	}
	return parts
}

func validRedditID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

func (e *RedditExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	post, err := parseRedditPost(d)
	if err != nil {
		return sdk.ExtractFallback(err)
	}

	d.Title = post.Title
	d.Text = redditPostText(post)
	if d.Metadata == nil {
		d.Metadata = make(map[string]any)
	}
	d.Metadata["type"] = "reddit"
	setMetadata(d.Metadata, "author", post.Author)
	setMetadata(d.Metadata, "subreddit", post.Subreddit)
	setMetadata(d.Metadata, "published", post.Published)
	setMetadata(d.Metadata, "score", post.Score)
	setMetadata(d.Metadata, "flair", post.Flair)
	setMetadata(d.Metadata, "link", post.Link)
	setMetadata(d.Metadata, "post_id", post.PostID)
	d.Metadata["comments"] = len(post.Comments)

	return sdk.Extracted()
}

func setMetadata(metadata map[string]any, key, value string) {
	if value != "" {
		metadata[key] = value
	}
}

func (e *RedditExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	post, err := parseRedditPost(d)
	if err != nil {
		return sdk.PreviewFallback(err)
	}

	var b strings.Builder
	if post.Title != "" {
		fmt.Fprintf(&b, `<h2><a href="%s">%s</a></h2>`, stdhtml.EscapeString(d.URL), stdhtml.EscapeString(post.Title))
	}
	parts := make([]string, 0, 5)
	if post.Author != "" {
		parts = append(parts, "submitted by <strong>"+stdhtml.EscapeString(post.Author)+"</strong>")
	}
	if post.Subreddit != "" {
		parts = append(parts, "to "+stdhtml.EscapeString(post.Subreddit))
	}
	if post.Score != "" {
		parts = append(parts, stdhtml.EscapeString(post.Score)+" points")
	}
	if post.Published != "" {
		parts = append(parts, stdhtml.EscapeString(post.Published))
	}
	if post.Flair != "" {
		parts = append(parts, "flair: "+stdhtml.EscapeString(post.Flair))
	}
	if len(parts) > 0 {
		fmt.Fprintf(&b, "<p>%s</p>", strings.Join(parts, " &middot; "))
	}
	if post.BodyHTML != "" {
		b.WriteString(post.BodyHTML)
	}
	if post.Link != "" {
		fmt.Fprintf(&b, `<p><a href="%s">View linked content</a></p>`, stdhtml.EscapeString(post.Link))
	}
	if len(post.Comments) > 0 {
		b.WriteString("<hr><h2>Comments</h2>")
		baseDepth := post.Comments[0].Depth
		for _, comment := range post.Comments[1:] {
			if comment.Depth < baseDepth {
				baseDepth = comment.Depth
			}
		}
		for _, comment := range post.Comments {
			depth := min(max(comment.Depth-baseDepth, 0), 12)
			fmt.Fprintf(&b, `<div style="margin-left:%dem">`, depth*2)
			commentParts := make([]string, 0, 3)
			if comment.Author != "" {
				commentParts = append(commentParts, "<strong>"+stdhtml.EscapeString(comment.Author)+"</strong>")
			}
			if comment.Score != "" {
				commentParts = append(commentParts, stdhtml.EscapeString(comment.Score)+" points")
			}
			if comment.Published != "" {
				commentParts = append(commentParts, stdhtml.EscapeString(comment.Published))
			}
			if len(commentParts) > 0 {
				fmt.Fprintf(&b, "<p>%s</p>", strings.Join(commentParts, " &middot; "))
			}
			b.WriteString(comment.HTML)
			b.WriteString("</div>")
		}
	}

	return sdk.Previewed(sdk.PreviewResponse{Content: sanitizer.SanitizeHTML(b.String())})
}

func parseRedditPost(d *sdk.Document) (*redditPost, error) {
	postID, subreddit, ok := redditPostURL(d.URL)
	if !ok {
		return nil, fmt.Errorf("not a Reddit post page")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return nil, err
	}
	structured := structuredRedditPost(doc)
	root := findPost(doc, postID)
	if root == nil && structured == nil {
		return nil, fmt.Errorf("no Reddit post found")
	}

	post := &redditPost{PostID: postID, Subreddit: subreddit}
	base, _ := url.Parse(d.URL)
	if root != nil {
		parsePostElement(post, root, doc, base)
	}
	post.Comments = parseComments(doc, base)
	mergeStructuredPost(post, structured)

	post.Subreddit = normalizeSubreddit(post.Subreddit)
	post.Title = cleanRedditTitle(post.Title, post.Subreddit)
	post.Link = meaningfulPostLink(post.Link, d.URL, post.PostID)
	if post.Title == "" && post.BodyText == "" && post.Link == "" && len(post.Comments) == 0 {
		return nil, fmt.Errorf("reddit post has no content")
	}
	return post, nil
}

func parsePostElement(post *redditPost, root *goquery.Selection, doc *goquery.Document, base *url.URL) {
	post.Title = firstValue(
		firstAttr(root, "post-title", "data-title"),
		firstSelectionText(root, `h1[slot="title"]`, `[data-testid="post-title"]`, `[itemprop="headline"]`, `p.title a.title`, "h1"),
		firstPageMeta(doc, `meta[property="og:title"]`, `meta[name="twitter:title"]`),
		textutil.SelectionText(doc.Find("title").First()),
	)
	post.Author = firstValue(
		firstAttr(root, "author", "data-author"),
		firstSelectionText(root, `[slot="authorName"]`, `[noun="post_author"]`, `[data-testid="post_author_link"]`, `.tagline .author`, `[rel="author"]`),
	)
	post.Subreddit = firstValue(
		firstAttr(root, "subreddit-prefixed-name", "data-subreddit-prefixed-name", "subreddit"),
		post.Subreddit,
	)
	post.Score = firstValue(
		firstAttr(root, "score", "data-score"),
		firstSelectionText(root, `[data-testid="post-score"]`, `[data-click-id="score"]`, `.score.unvoted`, `[itemprop="upvoteCount"]`),
	)
	post.CommentCount = firstAttr(root, "comment-count", "data-comment-count", "num-comments")
	post.Published = firstValue(
		firstAttr(root, "created-timestamp", "date-created", "published"),
		firstSelectionAttr(root, "datetime", `time[datetime]`, `[itemprop="datePublished"]`),
	)
	post.Flair = firstValue(
		firstAttr(root, "post-flair", "post-flair-text", "data-flair"),
		firstSelectionText(root, `shreddit-post-flair`, `[slot="post-flair"]`, `.linkflairlabel`),
	)

	body := firstBodySelection(root)
	if body != nil {
		post.BodyText = textutil.SelectionText(body)
		urlutil.RewriteURLs(body, base)
		post.BodyHTML, _ = body.Html()
	}
	if post.BodyText == "" {
		post.BodyText = firstPageMeta(root, `meta[itemprop="articleBody"]`, `meta[itemprop="text"]`)
		if post.BodyText != "" {
			post.BodyHTML = paragraphHTML(post.BodyText)
		}
	}

	post.Link = firstValue(
		firstAttr(root, "content-href", "data-url", "url"),
		firstSelectionAttr(root, "href", `p.title a.title`, `a[data-click-id="body"]`, `[slot="outbound-link"]`),
	)
	post.Link = urlutil.ResolveURL(base, strings.TrimSpace(post.Link))
	if id := selectionPostID(root); id != "" {
		post.PostID = id
	}
}

func firstBodySelection(root *goquery.Selection) *goquery.Selection {
	selectors := []string{
		`[slot="text-body"]`,
		`shreddit-post-text-body`,
		`[data-testid="post-content"]`,
		`[data-click-id="text"]`,
		`.usertext-body .md`,
		`[itemprop="articleBody"]:not(meta)`,
		`[itemprop="text"]:not(meta)`,
	}
	for _, selector := range selectors {
		var found *goquery.Selection
		root.Find(selector).EachWithBreak(func(_ int, s *goquery.Selection) bool {
			if textutil.SelectionText(s) == "" && s.Find("img, video, audio").Length() == 0 {
				return true
			}
			found = s
			return false
		})
		if found != nil {
			return found
		}
	}
	return nil
}

func findPost(doc *goquery.Document, postID string) *goquery.Selection {
	selectors := []string{
		"shreddit-post",
		`[data-fullname^="t3_"]`,
		`[data-testid="post-container"]`,
		`[itemtype$="/DiscussionForumPosting"]`,
		`[itemtype$="/SocialMediaPosting"]`,
		"main article",
	}
	for _, selector := range selectors {
		candidates := doc.Find(selector)
		if candidates.Length() == 0 {
			continue
		}
		var contextCandidate *goquery.Selection
		var titleCandidate *goquery.Selection
		var exactCandidate *goquery.Selection
		candidates.EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
			if postID != "" && selectionPostID(candidate) == postID {
				exactCandidate = candidate
				return false
			}
			if strings.EqualFold(candidate.AttrOr("view-context", ""), "CommentsPage") {
				contextCandidate = candidate
			}
			if titleCandidate == nil && candidate.Find(`h1[slot="title"], [itemprop="headline"], p.title a.title`).Length() > 0 {
				titleCandidate = candidate
			}
			return true
		})
		if exactCandidate != nil {
			return exactCandidate
		}
		if contextCandidate != nil {
			return contextCandidate
		}
		if candidates.Length() == 1 {
			return candidates.First()
		}
		if titleCandidate != nil {
			return titleCandidate
		}
	}
	return nil
}

func selectionPostID(s *goquery.Selection) string {
	for _, attr := range []string{"post-id", "postid", "data-post-id"} {
		if id := normalizeThingID(s.AttrOr(attr, ""), "t3_"); id != "" {
			return id
		}
	}
	for _, attr := range []string{"id", "thingid", "thing-id", "data-fullname"} {
		value := strings.ToLower(strings.TrimSpace(s.AttrOr(attr, "")))
		if !strings.HasPrefix(value, "t3_") {
			continue
		}
		if id := normalizeThingID(value, "t3_"); id != "" {
			return id
		}
	}
	for _, attr := range []string{"permalink", "data-permalink"} {
		if raw := strings.TrimSpace(s.AttrOr(attr, "")); raw != "" {
			u, err := url.Parse(raw)
			if err == nil {
				if id, _, ok := redditPostURL("https://reddit.com" + u.Path); ok {
					return id
				}
			}
		}
	}
	return ""
}

func normalizeThingID(id, prefix string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if value, ok := strings.CutPrefix(id, prefix); ok {
		id = value
	} else if strings.HasPrefix(id, "t1_") || strings.HasPrefix(id, "t2_") || strings.HasPrefix(id, "t3_") {
		return ""
	}
	if !validRedditID(id) {
		return ""
	}
	return id
}

func parseComments(doc *goquery.Document, base *url.URL) []redditComment {
	scope := commentScope(doc)
	selectors := []string{
		"shreddit-comment",
		`[data-fullname^="t1_"]`,
		`[data-testid="comment"]`,
		`[itemtype$="/Comment"]`,
	}
	for _, selector := range selectors {
		roots := scope.Find(selector)
		if roots.Length() == 0 {
			continue
		}
		comments := make([]redditComment, 0, roots.Length())
		roots.Each(func(_ int, root *goquery.Selection) {
			if comment, ok := parseComment(root, selector, base); ok {
				comments = append(comments, comment)
			}
		})
		return comments
	}
	return nil
}

func commentScope(doc *goquery.Document) *goquery.Selection {
	for _, selector := range []string{"shreddit-comment-tree", "#comment-tree", `.commentarea`, `[data-testid="comment-tree"]`} {
		if scope := doc.Find(selector).First(); scope.Length() > 0 {
			return scope
		}
	}
	return doc.Selection
}

func parseComment(root *goquery.Selection, rootSelector string, base *url.URL) (redditComment, bool) {
	comment := redditComment{
		ID:        commentID(root),
		Author:    firstValue(firstAttr(root, "author", "data-author"), firstSelectionText(root, `[noun="comment_author"]`, `[data-testid="comment_author_link"]`, `.tagline .author`, `[rel="author"]`, `[slot="commentMeta"] a`)),
		Score:     firstValue(firstAttr(root, "score", "data-score"), firstSelectionText(root, `[data-testid="comment-score"]`, `.score.unvoted`, `[itemprop="upvoteCount"]`)),
		Published: firstValue(firstAttr(root, "created-timestamp", "date-created", "published"), firstSelectionAttr(root, "datetime", `time[datetime]`, `[itemprop="datePublished"]`)),
		Depth:     commentDepth(root, rootSelector),
	}
	body := commentBody(root)
	if body != nil {
		comment.Text = textutil.SelectionText(body)
		urlutil.RewriteURLs(body, base)
		comment.HTML, _ = body.Html()
	} else {
		comment.Text = fallbackCommentText(root, rootSelector)
		if comment.Text != "" {
			comment.HTML = paragraphHTML(comment.Text)
		}
	}
	if comment.Text == "" && comment.Author == "" && comment.ID == "" {
		return redditComment{}, false
	}
	return comment, true
}

func commentBody(root *goquery.Selection) *goquery.Selection {
	if direct := root.Children().Filter(`[slot="comment"]`).First(); direct.Length() > 0 {
		return direct
	}
	for _, selector := range []string{
		`[slot="comment"]`,
		`[data-testid="comment-body"]`,
		`[data-click-id="text"]`,
		`.entry .usertext-body .md`,
		`[id$="-comment-rtjson-content"]`,
		`[itemprop="text"]`,
		`[itemprop="commentText"]`,
	} {
		if body := root.Find(selector).First(); body.Length() > 0 {
			return body
		}
	}
	return nil
}

func fallbackCommentText(root *goquery.Selection, rootSelector string) string {
	clone := root.Clone()
	clone.Find(rootSelector).Remove()
	clone.Find("button, script, style, svg, shreddit-comment-action-row, [slot=actionRow], [slot=commentMeta], [slot=commentAvatar]").Remove()
	return textutil.SelectionText(clone)
}

func commentID(root *goquery.Selection) string {
	for _, attr := range []string{"comment-id"} {
		if id := normalizeThingID(root.AttrOr(attr, ""), "t1_"); id != "" {
			return id
		}
	}
	for _, attr := range []string{"thingid", "thing-id", "data-fullname", "id"} {
		value := strings.ToLower(strings.TrimSpace(root.AttrOr(attr, "")))
		if !strings.HasPrefix(value, "t1_") {
			continue
		}
		if id := normalizeThingID(value, "t1_"); id != "" {
			return id
		}
	}
	return ""
}

func commentDepth(root *goquery.Selection, selector string) int {
	for _, attr := range []string{"depth", "data-depth"} {
		if depth, err := strconv.Atoi(strings.TrimSpace(root.AttrOr(attr, ""))); err == nil && depth >= 0 {
			return depth
		}
	}
	depth := 0
	root.Parents().Each(func(_ int, parent *goquery.Selection) {
		if parent.Is(selector) {
			depth++
		}
	})
	return depth
}

func redditPostText(post *redditPost) string {
	var b strings.Builder
	b.WriteString(post.Title)
	if post.Author != "" {
		b.WriteString("\nsubmitted by ")
		b.WriteString(post.Author)
	}
	if post.Subreddit != "" {
		b.WriteString("\nsubreddit: ")
		b.WriteString(post.Subreddit)
	}
	if post.Score != "" {
		b.WriteString("\nscore: ")
		b.WriteString(post.Score)
	}
	if post.Flair != "" {
		b.WriteString("\nflair: ")
		b.WriteString(post.Flair)
	}
	if post.BodyText != "" {
		b.WriteString("\n\n")
		b.WriteString(post.BodyText)
	}
	if post.Link != "" {
		b.WriteString("\n\nlink: ")
		b.WriteString(post.Link)
	}
	if len(post.Comments) > 0 {
		b.WriteString("\n\nComments")
		baseDepth := post.Comments[0].Depth
		for _, comment := range post.Comments[1:] {
			if comment.Depth < baseDepth {
				baseDepth = comment.Depth
			}
		}
		for _, comment := range post.Comments {
			indent := strings.Repeat("  ", max(comment.Depth-baseDepth, 0))
			b.WriteString("\n\n")
			b.WriteString(indent)
			if comment.Author != "" {
				b.WriteString(comment.Author)
			}
			if comment.Score != "" {
				fmt.Fprintf(&b, " [%s points]", comment.Score)
			}
			if comment.Text != "" {
				for line := range strings.SplitSeq(comment.Text, "\n") {
					b.WriteString("\n")
					b.WriteString(indent)
					b.WriteString(line)
				}
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func meaningfulPostLink(rawLink, pageURL, postID string) string {
	rawLink = strings.TrimSpace(rawLink)
	if rawLink == "" {
		return ""
	}
	linkURL, err := url.Parse(rawLink)
	if err != nil || (linkURL.Scheme != "http" && linkURL.Scheme != "https") {
		return ""
	}
	if id, _, ok := redditPostURL(rawLink); ok && id == postID {
		return ""
	}
	page, err := url.Parse(pageURL)
	if err == nil && page.Scheme == linkURL.Scheme && strings.EqualFold(page.Host, linkURL.Host) && page.Path == linkURL.Path {
		return ""
	}
	return linkURL.String()
}

func cleanRedditTitle(title, subreddit string) string {
	title = sanitizer.SanitizeText(title)
	if subreddit != "" {
		for _, suffix := range []string{" : " + subreddit, " | " + subreddit} {
			title = strings.TrimSpace(strings.TrimSuffix(title, suffix))
		}
	}
	for _, suffix := range []string{" - Reddit", " : Reddit"} {
		title = strings.TrimSpace(strings.TrimSuffix(title, suffix))
	}
	return title
}

func normalizeSubreddit(subreddit string) string {
	subreddit = strings.Trim(strings.TrimSpace(subreddit), "/")
	if subreddit == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(subreddit), "r/") {
		return "r/" + subreddit[2:]
	}
	return "r/" + subreddit
}

func firstValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return sanitizer.SanitizeText(value)
		}
	}
	return ""
}

func firstAttr(s *goquery.Selection, attrs ...string) string {
	if s == nil {
		return ""
	}
	for _, attr := range attrs {
		if value := strings.TrimSpace(s.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	return ""
}

func firstSelectionText(s *goquery.Selection, selectors ...string) string {
	if s == nil {
		return ""
	}
	for _, selector := range selectors {
		if value := textutil.SelectionText(s.Find(selector).First()); value != "" {
			return value
		}
	}
	return ""
}

func firstSelectionAttr(s *goquery.Selection, attr string, selectors ...string) string {
	if s == nil {
		return ""
	}
	for _, selector := range selectors {
		if value := strings.TrimSpace(s.Find(selector).First().AttrOr(attr, "")); value != "" {
			return value
		}
	}
	return ""
}

type selectionFinder interface {
	Find(string) *goquery.Selection
}

func firstPageMeta(s selectionFinder, selectors ...string) string {
	for _, selector := range selectors {
		if value := strings.TrimSpace(s.Find(selector).First().AttrOr("content", "")); value != "" {
			return value
		}
	}
	return ""
}

func paragraphHTML(text string) string {
	escaped := stdhtml.EscapeString(strings.ReplaceAll(text, "\r\n", "\n"))
	return "<p>" + strings.ReplaceAll(escaped, "\n", "<br>") + "</p>"
}

func structuredRedditPost(doc *goquery.Document) *redditPost {
	var result *redditPost
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, script *goquery.Selection) bool {
		var payload any
		if err := json.Unmarshal([]byte(script.Text()), &payload); err != nil {
			return true
		}
		node := findStructuredPosting(payload)
		if node == nil {
			return true
		}
		result = postFromStructuredNode(node)
		return false
	})
	return result
}

func findStructuredPosting(value any) map[string]any {
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			if found := findStructuredPosting(item); found != nil {
				return found
			}
		}
	case map[string]any:
		if schemaTypeMatches(value["@type"], "DiscussionForumPosting") || schemaTypeMatches(value["@type"], "SocialMediaPosting") {
			return value
		}
		for _, key := range []string{"mainEntity", "@graph", "itemListElement"} {
			if found := findStructuredPosting(value[key]); found != nil {
				return found
			}
		}
	}
	return nil
}

func schemaTypeMatches(value any, want string) bool {
	switch value := value.(type) {
	case string:
		value = strings.TrimSuffix(value, "/")
		if i := strings.LastIndexAny(value, "/#"); i >= 0 {
			value = value[i+1:]
		}
		return strings.EqualFold(value, want)
	case []any:
		for _, item := range value {
			if schemaTypeMatches(item, want) {
				return true
			}
		}
	case map[string]any:
		return schemaTypeMatches(value["@type"], want)
	}
	return false
}

func postFromStructuredNode(node map[string]any) *redditPost {
	post := &redditPost{
		Title:        structuredString(node, "headline", "name"),
		Author:       structuredAuthor(node["author"]),
		Published:    structuredString(node, "datePublished", "dateCreated"),
		BodyText:     structuredString(node, "articleBody", "text"),
		Score:        structuredCount(node, "upvoteCount"),
		CommentCount: structuredCount(node, "commentCount"),
	}
	if post.BodyText != "" {
		post.BodyHTML = paragraphHTML(post.BodyText)
	}
	for _, key := range []string{"comment", "comments"} {
		post.Comments = append(post.Comments, structuredComments(node[key], 0)...)
	}
	return post
}

func structuredComments(value any, depth int) []redditComment {
	var comments []redditComment
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			comments = append(comments, structuredComments(item, depth)...)
		}
	case map[string]any:
		if !schemaTypeMatches(value["@type"], "Comment") {
			return nil
		}
		text := structuredString(value, "text", "commentText", "articleBody")
		comment := redditComment{
			ID:        structuredString(value, "identifier", "@id"),
			Author:    structuredAuthor(value["author"]),
			Published: structuredString(value, "datePublished", "dateCreated"),
			Score:     structuredCount(value, "upvoteCount"),
			Text:      text,
			Depth:     depth,
		}
		if text != "" {
			comment.HTML = paragraphHTML(text)
		}
		comments = append(comments, comment)
		for _, key := range []string{"comment", "comments"} {
			comments = append(comments, structuredComments(value[key], depth+1)...)
		}
	}
	return comments
}

func mergeStructuredPost(post, structured *redditPost) {
	if structured == nil {
		return
	}
	if post.Title == "" {
		post.Title = structured.Title
	}
	if post.Author == "" {
		post.Author = structured.Author
	}
	if post.Published == "" {
		post.Published = structured.Published
	}
	if post.Score == "" {
		post.Score = structured.Score
	}
	if post.CommentCount == "" {
		post.CommentCount = structured.CommentCount
	}
	if post.BodyText == "" {
		post.BodyText = structured.BodyText
		post.BodyHTML = structured.BodyHTML
	}
	if len(post.Comments) == 0 {
		post.Comments = structured.Comments
	}
}

func structuredString(node map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := node[key].(string); ok {
			if value = sanitizer.SanitizeText(value); value != "" {
				return value
			}
		}
	}
	return ""
}

func structuredAuthor(value any) string {
	switch value := value.(type) {
	case string:
		return sanitizer.SanitizeText(value)
	case map[string]any:
		return structuredString(value, "name", "alternateName")
	case []any:
		for _, item := range value {
			if author := structuredAuthor(item); author != "" {
				return author
			}
		}
	}
	return ""
}

func structuredCount(node map[string]any, key string) string {
	if value, ok := node[key]; ok {
		return formatStructuredNumber(value)
	}
	statistics, ok := node["interactionStatistic"]
	if !ok {
		return ""
	}
	var entries []any
	if list, ok := statistics.([]any); ok {
		entries = list
	} else {
		entries = []any{statistics}
	}
	for _, entry := range entries {
		stat, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if key == "commentCount" && !schemaTypeMatches(stat["interactionType"], "CommentAction") {
			continue
		}
		if key == "upvoteCount" && !schemaTypeMatches(stat["interactionType"], "LikeAction") {
			continue
		}
		if count := formatStructuredNumber(stat["userInteractionCount"]); count != "" {
			return count
		}
	}
	return ""
}

func formatStructuredNumber(value any) string {
	switch value := value.(type) {
	case string:
		return sanitizer.SanitizeText(value)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case json.Number:
		return value.String()
	}
	return ""
}
