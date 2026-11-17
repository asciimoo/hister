// Package discourse extracts topics and posts from Discourse forums.
package discourse

import (
	"cmp"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"github.com/asciimoo/nogon/server/extractor/sdk"
	"github.com/asciimoo/nogon/server/extractor/textutil"
	"github.com/asciimoo/nogon/server/extractor/urlutil"
	"github.com/asciimoo/nogon/server/sanitizer"
)

type DiscourseExtractor struct {
	sdk.ConfigSupport
}

type discourseTopic struct {
	ID       int64
	Title    string
	Category string
	Tags     []string
	Posts    []discoursePost
}

type discoursePost struct {
	ID         int64
	Number     int
	ReplyTo    int
	Author     string
	Published  string
	Text       string
	HTML       string
	Likes      int
	Reactions  int
	Accepted   bool
	SourceRank int
}

type preloadedTopic struct {
	Title      string          `json:"title"`
	FancyTitle string          `json:"fancy_title"`
	Tags       json.RawMessage `json:"tags"`
	PostStream struct {
		Posts []preloadedPost `json:"posts"`
	} `json:"post_stream"`
}

type preloadedPost struct {
	ID                int64           `json:"id"`
	Name              string          `json:"name"`
	Username          string          `json:"username"`
	DisplayUsername   string          `json:"display_username"`
	CreatedAt         string          `json:"created_at"`
	Cooked            string          `json:"cooked"`
	PostNumber        int             `json:"post_number"`
	PostType          int             `json:"post_type"`
	ReplyToPostNumber *int            `json:"reply_to_post_number"`
	AcceptedAnswer    bool            `json:"accepted_answer"`
	Hidden            bool            `json:"hidden"`
	DeletedAt         json.RawMessage `json:"deleted_at"`
	ActionsSummary    []struct {
		ID    int `json:"id"`
		Count int `json:"count"`
	} `json:"actions_summary"`
	Reactions []struct {
		ID    string `json:"id"`
		Count int    `json:"count"`
	} `json:"reactions"`
}

func (e *DiscourseExtractor) Name() string {
	return "Discourse"
}

func (e *DiscourseExtractor) Description() string {
	return "Extracts a Discourse topic and every post already present in the page."
}

func (e *DiscourseExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

func (e *DiscourseExtractor) Match(d *sdk.Document) bool {
	_, ok := discourseTopicURL(d.URL)
	return ok && isDiscourseHTML(d.HTML)
}

func (e *DiscourseExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	topic, err := parseDiscourseTopic(d)
	if err != nil {
		return sdk.ExtractFallback(err)
	}

	d.Title = topic.Title
	d.Text = topicText(topic)
	if d.Metadata == nil {
		d.Metadata = make(map[string]any)
	}
	d.Metadata["type"] = "discourse"
	d.Metadata["topic_id"] = strconv.FormatInt(topic.ID, 10)
	d.Metadata["posts"] = len(topic.Posts)
	d.Metadata["replies"] = len(topic.Posts)
	for _, post := range topic.Posts {
		if post.Number == 1 {
			d.Metadata["replies"] = len(topic.Posts) - 1
			setMetadata(d.Metadata, "author", post.Author)
			setMetadata(d.Metadata, "published", post.Published)
			break
		}
	}
	setMetadata(d.Metadata, "category", topic.Category)
	if len(topic.Tags) > 0 {
		d.Metadata["tags"] = strings.Join(topic.Tags, ", ")
	}
	for _, post := range topic.Posts {
		if post.Accepted && post.Number > 0 {
			d.Metadata["accepted_answer"] = post.Number
			break
		}
	}

	return sdk.Extracted()
}

func (e *DiscourseExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	topic, err := parseDiscourseTopic(d)
	if err != nil {
		return sdk.PreviewFallback(err)
	}

	var b strings.Builder
	if topic.Title != "" {
		fmt.Fprintf(&b, `<h2><a href="%s">%s</a></h2>`, stdhtml.EscapeString(d.URL), stdhtml.EscapeString(topic.Title))
	}
	topicParts := make([]string, 0, 2)
	if topic.Category != "" {
		topicParts = append(topicParts, "category: "+stdhtml.EscapeString(topic.Category))
	}
	if len(topic.Tags) > 0 {
		escaped := make([]string, 0, len(topic.Tags))
		for _, tag := range topic.Tags {
			escaped = append(escaped, stdhtml.EscapeString(tag))
		}
		topicParts = append(topicParts, "tags: "+strings.Join(escaped, ", "))
	}
	if len(topicParts) > 0 {
		fmt.Fprintf(&b, "<p>%s</p>", strings.Join(topicParts, " &middot; "))
	}

	for index, post := range topic.Posts {
		if index > 0 {
			b.WriteString("<hr>")
		}
		heading := "Post"
		if post.Number == 1 {
			heading = "Original post"
		} else if post.Number > 1 {
			heading = fmt.Sprintf("Reply #%d", post.Number)
		}
		if post.Accepted {
			heading += " (accepted solution)"
		}
		fmt.Fprintf(&b, "<h3>%s</h3>", stdhtml.EscapeString(heading))
		parts := postMetadataParts(post)
		if len(parts) > 0 {
			fmt.Fprintf(&b, "<p>%s</p>", strings.Join(parts, " &middot; "))
		}
		b.WriteString(post.HTML)
	}

	return sdk.Previewed(sdk.PreviewResponse{Content: sanitizer.SanitizeHTML(b.String())})
}

func parseDiscourseTopic(d *sdk.Document) (*discourseTopic, error) {
	topicID, ok := discourseTopicURL(d.URL)
	if !ok {
		return nil, fmt.Errorf("not a Discourse topic page")
	}
	if !isDiscourseHTML(d.HTML) {
		return nil, fmt.Errorf("page is not generated by Discourse")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(d.HTML))
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(d.URL)

	topic := parsePreloadedTopic(doc, topicID, base)
	if topic == nil {
		topic = &discourseTopic{ID: topicID}
	}
	topic.ID = topicID
	readTopicHeader(topic, doc)
	mergePosts(topic, parseRenderedPosts(doc, base))
	mergeSchemaTopic(topic, parseSchemaTopic(doc, base))
	finalizeTopic(topic)

	if topic.Title == "" || len(topic.Posts) == 0 {
		return nil, fmt.Errorf("discourse topic has no posts")
	}
	return topic, nil
}

func discourseTopicURL(rawURL string) (int64, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return 0, false
	}
	parts := pathParts(u.Path)
	for index, part := range parts {
		if part != "t" || index+1 >= len(parts) {
			continue
		}
		idIndex := index + 1
		if !isPositiveInteger(parts[idIndex]) {
			idIndex++
		}
		if idIndex >= len(parts) || !isPositiveInteger(parts[idIndex]) {
			return 0, false
		}
		if len(parts) > idIndex+2 || len(parts) == idIndex+2 && !isPositiveInteger(parts[idIndex+1]) {
			return 0, false
		}
		id, err := strconv.ParseInt(parts[idIndex], 10, 64)
		return id, err == nil && id > 0
	}
	return 0, false
}

func pathParts(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return nil
	}
	return parts
}

func isPositiveInteger(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return value != "0"
}

func isDiscourseHTML(rawHTML string) bool {
	tokenizer := html.NewTokenizer(strings.NewReader(rawHTML))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return false
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if !strings.EqualFold(token.Data, "meta") {
				continue
			}
			var id, name, content string
			for _, attribute := range token.Attr {
				switch strings.ToLower(attribute.Key) {
				case "id":
					id = attribute.Val
				case "name":
					name = attribute.Val
				case "content":
					content = attribute.Val
				}
			}
			if strings.EqualFold(id, "data-discourse-setup") ||
				strings.EqualFold(name, "discourse/config/environment") {
				return true
			}
			generator := strings.ToLower(strings.TrimSpace(content))
			if strings.EqualFold(name, "generator") &&
				(generator == "discourse" || strings.HasPrefix(generator, "discourse ")) {
				return true
			}
		}
	}
}

func parsePreloadedTopic(doc *goquery.Document, topicID int64, base *url.URL) *discourseTopic {
	raw := strings.TrimSpace(doc.Find("#data-preloaded").First().Text())
	if raw == "" {
		return nil
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil
	}
	entry, ok := entries["topic_"+strconv.FormatInt(topicID, 10)]
	if !ok {
		return nil
	}

	var encoded string
	if json.Unmarshal(entry, &encoded) == nil {
		entry = json.RawMessage(encoded)
	}
	var source preloadedTopic
	if err := json.Unmarshal(entry, &source); err != nil {
		return nil
	}

	topic := &discourseTopic{
		ID:    topicID,
		Title: firstNonempty(source.Title, sanitizer.SanitizeText(source.FancyTitle)),
		Tags:  parsePreloadedTags(source.Tags),
		Posts: make([]discoursePost, 0, len(source.PostStream.Posts)),
	}
	for _, sourcePost := range source.PostStream.Posts {
		if sourcePost.PostType > 1 || sourcePost.Hidden || hasJSONValue(sourcePost.DeletedAt) {
			continue
		}
		bodyText, bodyHTML := extractContentHTML(sourcePost.Cooked, base)
		if bodyText == "" {
			continue
		}
		post := discoursePost{
			ID:         sourcePost.ID,
			Number:     sourcePost.PostNumber,
			Author:     formatAuthor(sourcePost.Name, sourcePost.DisplayUsername, sourcePost.Username),
			Published:  sourcePost.CreatedAt,
			Text:       bodyText,
			HTML:       bodyHTML,
			Accepted:   sourcePost.AcceptedAnswer,
			SourceRank: 2,
		}
		if sourcePost.ReplyToPostNumber != nil {
			post.ReplyTo = *sourcePost.ReplyToPostNumber
		}
		for _, action := range sourcePost.ActionsSummary {
			if action.ID == 2 {
				post.Likes = action.Count
			}
		}
		for _, reaction := range sourcePost.Reactions {
			if post.Likes > 0 && (reaction.ID == "heart" || reaction.ID == "like") {
				continue
			}
			post.Reactions += reaction.Count
		}
		topic.Posts = append(topic.Posts, post)
	}
	return topic
}

func hasJSONValue(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	return value != "" && value != "null"
}

func parsePreloadedTags(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var stringsList []string
	if json.Unmarshal(raw, &stringsList) == nil {
		return cleanStrings(stringsList)
	}
	var objects []map[string]any
	if json.Unmarshal(raw, &objects) != nil {
		return nil
	}
	tags := make([]string, 0, len(objects))
	for _, object := range objects {
		for _, key := range []string{"name", "id"} {
			if value, ok := object[key].(string); ok && value != "" {
				tags = append(tags, value)
				break
			}
		}
	}
	return cleanStrings(tags)
}

func readTopicHeader(topic *discourseTopic, doc *goquery.Document) {
	topic.Title = firstNonempty(
		textutil.SelectionText(doc.Find(`#topic-title h1 a`).First()),
		textutil.SelectionText(doc.Find(`h1[data-topic-id]`).First()),
		metaContent(doc, `meta[property="og:title"]`, `meta[name="twitter:title"]`),
		topic.Title,
	)
	if topic.Category == "" {
		topic.Category = firstNonempty(
			textutil.SelectionText(doc.Find(`#topic-title .badge-category__name`).First()),
			textutil.SelectionText(doc.Find(`#topic-title .category-name`).First()),
			metaContent(doc, `meta[property="og:article:section"]`),
		)
	}
	if len(topic.Tags) == 0 {
		doc.Find(`#topic-title .discourse-tag`).Each(func(_ int, tag *goquery.Selection) {
			if value := textutil.SelectionText(tag); value != "" {
				topic.Tags = append(topic.Tags, value)
			}
		})
	}
}

func parseRenderedPosts(doc *goquery.Document, base *url.URL) []discoursePost {
	roots := doc.Find(`.post-stream article[data-post-id], article[data-post-id], .crawler-post`)
	posts := make([]discoursePost, 0, roots.Length())
	roots.Each(func(_ int, root *goquery.Selection) {
		container := root.Closest(".topic-post")
		if root.Is("[hidden], .post-hidden, .deleted") || container.Is("[hidden], .post-hidden, .deleted") {
			return
		}
		body := firstBody(root)
		if body == nil {
			return
		}
		body = body.Clone()
		body.Find(`script, style, button, .cooked-selection-barrier, .post-menu-area`).Remove()
		urlutil.RewriteURLs(body, base)
		bodyHTML, _ := body.Html()
		bodyText := textutil.SelectionText(body)
		if bodyText == "" {
			return
		}

		post := discoursePost{
			ID:         parseInt64(root.AttrOr("data-post-id", "")),
			Number:     renderedPostNumber(root),
			ReplyTo:    parseInt(root.AttrOr("data-reply-to-post-number", "")),
			Author:     renderedAuthor(root),
			Published:  renderedPublished(root),
			Text:       bodyText,
			HTML:       bodyHTML,
			Accepted:   renderedPostAccepted(root),
			SourceRank: 3,
		}
		post.Likes = firstSelectionInt(root, `[data-action-id="2"]`, `.like-count`, `.post-action-menu__like-count`)
		posts = append(posts, post)
	})
	return posts
}

func renderedAuthor(root *goquery.Selection) string {
	displayName := firstSelectionText(
		root,
		`.names .full-name a`,
		`.names .full-name`,
		`.creator [itemprop="name"]`,
		`[itemprop="author"] [itemprop="name"]`,
	)
	username := firstSelectionText(root, `.names .username a`, `.names .username`)
	if username == "" {
		username = firstSelectionAttr(root, "data-user-card", `.names [data-user-card]`)
	}
	return formatAuthor(displayName, "", username)
}

func renderedPublished(root *goquery.Selection) string {
	if published := firstSelectionAttr(root, "datetime", `time[datetime]`, `[itemprop="datePublished"][datetime]`); published != "" {
		return published
	}
	if value := firstSelectionAttr(root, "data-time", `.relative-date[data-time]`); value != "" {
		if timestamp, err := strconv.ParseInt(value, 10, 64); err == nil {
			if timestamp < 1_000_000_000_000 {
				return time.Unix(timestamp, 0).UTC().Format(time.RFC3339Nano)
			}
			return time.UnixMilli(timestamp).UTC().Format(time.RFC3339Nano)
		}
	}
	return firstSelectionAttr(root, "title", `.relative-date[title]`)
}

func renderedPostAccepted(root *goquery.Selection) bool {
	const selectors = `[itemprop="acceptedAnswer"], .accepted-answer, .accepted-text`
	return root.Is(selectors) || root.Find(selectors).Length() > 0
}

func firstBody(root *goquery.Selection) *goquery.Selection {
	for _, selector := range []string{`.cooked`, `.post[itemprop="text"]`, `[itemprop="text"]`} {
		if body := root.Find(selector).First(); body.Length() > 0 {
			return body
		}
	}
	return nil
}

func renderedPostNumber(root *goquery.Selection) int {
	if number := parseInt(root.AttrOr("data-post-number", "")); number > 0 {
		return number
	}
	if number := parseInt(root.Closest(`[data-post-number]`).AttrOr("data-post-number", "")); number > 0 {
		return number
	}
	id := root.AttrOr("id", "")
	if value, ok := strings.CutPrefix(id, "post_"); ok {
		return parseInt(value)
	}
	return 0
}

func parseSchemaTopic(doc *goquery.Document, base *url.URL) *discourseTopic {
	var topic *discourseTopic
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, script *goquery.Selection) bool {
		var value any
		if json.Unmarshal([]byte(script.Text()), &value) != nil {
			return true
		}
		page := findSchemaType(value, "QAPage")
		if page == nil {
			return true
		}
		topic = schemaTopic(page, base)
		return false
	})
	return topic
}

func findSchemaType(value any, wanted string) map[string]any {
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			if found := findSchemaType(item, wanted); found != nil {
				return found
			}
		}
	case map[string]any:
		if schemaTypeMatches(value["@type"], wanted) {
			return value
		}
		for _, key := range []string{"@graph", "mainEntity"} {
			if found := findSchemaType(value[key], wanted); found != nil {
				return found
			}
		}
	}
	return nil
}

func schemaTopic(page map[string]any, base *url.URL) *discourseTopic {
	topic := &discourseTopic{Title: schemaString(page, "name", "headline")}
	question, _ := page["mainEntity"].(map[string]any)
	if question == nil || !schemaTypeMatches(question["@type"], "Question") {
		return topic
	}
	topic.Posts = append(topic.Posts, schemaPost(question, 1, false, base))
	for _, value := range schemaObjects(question["acceptedAnswer"]) {
		topic.Posts = append(topic.Posts, schemaPost(value, 0, true, base))
	}
	for _, value := range schemaObjects(question["suggestedAnswer"]) {
		topic.Posts = append(topic.Posts, schemaPost(value, 0, false, base))
	}
	return topic
}

func schemaPost(value map[string]any, fallbackNumber int, accepted bool, base *url.URL) discoursePost {
	rawHTML := schemaString(value, "text", "articleBody")
	bodyText, bodyHTML := extractContentHTML(rawHTML, base)
	post := discoursePost{
		Number:     schemaPostNumber(schemaString(value, "url", "@id")),
		Author:     schemaAuthor(value["author"]),
		Published:  schemaString(value, "datePublished", "dateCreated"),
		Text:       bodyText,
		HTML:       bodyHTML,
		Likes:      schemaInt(value["upvoteCount"]),
		Accepted:   accepted,
		SourceRank: 1,
	}
	if post.Number == 0 {
		post.Number = fallbackNumber
	}
	return post
}

func schemaPostNumber(rawURL string) int {
	u, err := url.Parse(rawURL)
	if err != nil {
		return 0
	}
	parts := pathParts(u.Path)
	if len(parts) == 0 {
		return 0
	}
	return parseInt(parts[len(parts)-1])
}

func schemaObjects(value any) []map[string]any {
	switch value := value.(type) {
	case map[string]any:
		return []map[string]any{value}
	case []any:
		objects := make([]map[string]any, 0, len(value))
		for _, item := range value {
			if object, ok := item.(map[string]any); ok {
				objects = append(objects, object)
			}
		}
		return objects
	}
	return nil
}

func schemaTypeMatches(value any, wanted string) bool {
	switch value := value.(type) {
	case string:
		value = strings.TrimSuffix(value, "/")
		if index := strings.LastIndexAny(value, "/#"); index >= 0 {
			value = value[index+1:]
		}
		return strings.EqualFold(value, wanted)
	case []any:
		for _, item := range value {
			if schemaTypeMatches(item, wanted) {
				return true
			}
		}
	case map[string]any:
		return schemaTypeMatches(value["@type"], wanted)
	}
	return false
}

func schemaString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if stringValue, ok := value[key].(string); ok {
			if stringValue = strings.TrimSpace(stringValue); stringValue != "" {
				return stringValue
			}
		}
	}
	return ""
}

func schemaAuthor(value any) string {
	switch value := value.(type) {
	case string:
		return sanitizer.SanitizeText(value)
	case map[string]any:
		return sanitizer.SanitizeText(schemaString(value, "name", "alternateName"))
	case []any:
		for _, item := range value {
			if author := schemaAuthor(item); author != "" {
				return author
			}
		}
	}
	return ""
}

func schemaInt(value any) int {
	switch value := value.(type) {
	case float64:
		return int(value)
	case string:
		return parseInt(value)
	}
	return 0
}

func mergeSchemaTopic(topic, schema *discourseTopic) {
	if schema == nil {
		return
	}
	if topic.Title == "" {
		topic.Title = schema.Title
	}
	mergePosts(topic, schema.Posts)
}

func mergePosts(topic *discourseTopic, incoming []discoursePost) {
	for _, candidate := range incoming {
		if candidate.Text == "" {
			continue
		}
		matched := -1
		for index, existing := range topic.Posts {
			if (candidate.ID > 0 && existing.ID == candidate.ID) ||
				(candidate.Number > 0 && existing.Number == candidate.Number) {
				matched = index
				break
			}
		}
		if matched < 0 {
			topic.Posts = append(topic.Posts, candidate)
			continue
		}
		existing := &topic.Posts[matched]
		existing.Accepted = existing.Accepted || candidate.Accepted
		if candidate.SourceRank >= existing.SourceRank {
			mergePostContent(existing, candidate)
		} else {
			fillPostMetadata(existing, candidate)
		}
	}
}

func mergePostContent(destination *discoursePost, source discoursePost) {
	accepted := destination.Accepted
	if source.ID == 0 {
		source.ID = destination.ID
	}
	if source.Number == 0 {
		source.Number = destination.Number
	}
	if source.ReplyTo == 0 {
		source.ReplyTo = destination.ReplyTo
	}
	if source.Author == "" {
		source.Author = destination.Author
	}
	if source.Published == "" {
		source.Published = destination.Published
	}
	if source.Likes == 0 {
		source.Likes = destination.Likes
	}
	if source.Reactions == 0 {
		source.Reactions = destination.Reactions
	}
	*destination = source
	destination.Accepted = accepted || source.Accepted
}

func fillPostMetadata(destination *discoursePost, source discoursePost) {
	if destination.Author == "" {
		destination.Author = source.Author
	}
	if destination.Published == "" {
		destination.Published = source.Published
	}
	if destination.ReplyTo == 0 {
		destination.ReplyTo = source.ReplyTo
	}
	if destination.Likes == 0 {
		destination.Likes = source.Likes
	}
	if destination.Reactions == 0 {
		destination.Reactions = source.Reactions
	}
}

func finalizeTopic(topic *discourseTopic) {
	topic.Title = sanitizer.SanitizeText(topic.Title)
	topic.Category = sanitizer.SanitizeText(topic.Category)
	topic.Tags = cleanStrings(topic.Tags)
	slices.SortStableFunc(topic.Posts, func(left, right discoursePost) int {
		if left.Number == 0 && right.Number == 0 {
			return 0
		}
		if left.Number == 0 {
			return 1
		}
		if right.Number == 0 {
			return -1
		}
		return cmp.Compare(left.Number, right.Number)
	})
}

func topicText(topic *discourseTopic) string {
	var b strings.Builder
	b.WriteString(topic.Title)
	if topic.Category != "" {
		b.WriteString("\ncategory: ")
		b.WriteString(topic.Category)
	}
	if len(topic.Tags) > 0 {
		b.WriteString("\ntags: ")
		b.WriteString(strings.Join(topic.Tags, ", "))
	}
	for _, post := range topic.Posts {
		b.WriteString("\n\n")
		if post.Number > 0 {
			fmt.Fprintf(&b, "#%d ", post.Number)
		}
		if post.Author != "" {
			b.WriteString(post.Author)
		}
		if post.Accepted {
			b.WriteString(" [Accepted Solution]")
		}
		if post.ReplyTo > 0 {
			fmt.Fprintf(&b, " [reply to #%d]", post.ReplyTo)
		}
		if post.Likes > 0 {
			fmt.Fprintf(&b, " [%d likes]", post.Likes)
		}
		if post.Reactions > 0 {
			fmt.Fprintf(&b, " [%d reactions]", post.Reactions)
		}
		b.WriteString("\n")
		b.WriteString(post.Text)
	}
	return strings.TrimSpace(b.String())
}

func postMetadataParts(post discoursePost) []string {
	parts := make([]string, 0, 5)
	add := func(value string) {
		parts = append(parts, stdhtml.EscapeString(value))
	}
	if post.Author != "" {
		add(post.Author)
	}
	if post.Published != "" {
		add(post.Published)
	}
	if post.ReplyTo > 0 {
		add(fmt.Sprintf("reply to #%d", post.ReplyTo))
	}
	if post.Likes > 0 {
		add(fmt.Sprintf("%d likes", post.Likes))
	}
	if post.Reactions > 0 {
		add(fmt.Sprintf("%d reactions", post.Reactions))
	}
	return parts
}

func formatAuthor(name, displayName, username string) string {
	name = firstNonempty(displayName, name)
	name = sanitizer.SanitizeText(name)
	username = sanitizer.SanitizeText(username)
	if name == "" {
		return username
	}
	if username == "" || strings.EqualFold(name, username) {
		return name
	}
	return name + " (@" + username + ")"
}

func extractContentHTML(rawHTML string, base *url.URL) (string, string) {
	if strings.TrimSpace(rawHTML) == "" {
		return "", ""
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div>" + rawHTML + "</div>"))
	if err != nil {
		return sanitizer.SanitizeText(rawHTML), ""
	}
	body := doc.Find("body > div").First()
	body.Find("script, style, button, .cooked-selection-barrier, .post-menu-area").Remove()
	urlutil.RewriteURLs(body, base)
	content, _ := body.Html()
	return textutil.SelectionText(body), content
}

func cleanStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = sanitizer.SanitizeText(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func setMetadata(metadata map[string]any, key, value string) {
	if value != "" {
		metadata[key] = value
	}
}

func metaContent(doc *goquery.Document, selectors ...string) string {
	for _, selector := range selectors {
		if value := strings.TrimSpace(doc.Find(selector).First().AttrOr("content", "")); value != "" {
			return value
		}
	}
	return ""
}

func firstSelectionText(root *goquery.Selection, selectors ...string) string {
	for _, selector := range selectors {
		if value := textutil.SelectionText(root.Find(selector).First()); value != "" {
			return value
		}
	}
	return ""
}

func firstSelectionAttr(root *goquery.Selection, attr string, selectors ...string) string {
	for _, selector := range selectors {
		if value := strings.TrimSpace(root.Find(selector).First().AttrOr(attr, "")); value != "" {
			return value
		}
	}
	return ""
}

func firstSelectionInt(root *goquery.Selection, selectors ...string) int {
	for _, selector := range selectors {
		value := textutil.SelectionText(root.Find(selector).First())
		for field := range strings.FieldsSeq(value) {
			if number := parseInt(strings.Trim(field, "()[]{}.,")); number > 0 {
				return number
			}
		}
	}
	return 0
}

func parseInt(value string) int {
	number, _ := strconv.Atoi(strings.TrimSpace(value))
	return number
}

func parseInt64(value string) int64 {
	number, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return number
}
