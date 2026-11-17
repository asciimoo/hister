package discourse

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/extractor/sdk"
)

func TestSetConfigRejectsUnknownOptions(t *testing.T) {
	err := (&DiscourseExtractor{}).SetConfig(&config.Extractor{
		Enable:  true,
		Options: map[string]any{"unknown": true},
	})
	if err == nil {
		t.Fatal("SetConfig accepted an unknown option")
	}
}

func TestMatchOnlyAcceptsDiscourseTopics(t *testing.T) {
	discourseHTML := `<meta name="generator" content="Discourse 2026.8.0">`
	tests := []struct {
		name string
		url  string
		html string
		want bool
	}{
		{name: "topic", url: "https://forum.example.com/t/topic-title/123", html: discourseHTML, want: true},
		{name: "topic post", url: "https://forum.example.com/t/topic-title/123/4?u=alice", html: discourseHTML, want: true},
		{name: "topic without slug", url: "https://forum.example.com/t/123", html: `<meta id="data-discourse-setup">`, want: true},
		{name: "subpath installation", url: "https://example.com/forum/t/topic-title/123", html: `<meta name="discourse/config/environment" content="{}">`, want: true},
		{name: "generator attribute order", url: "https://forum.example.com/t/topic-title/123", html: `<meta content='Discourse 3.3' name='generator'>`, want: true},
		{name: "category", url: "https://forum.example.com/c/support/6", html: discourseHTML},
		{name: "tag", url: "https://forum.example.com/tag/solved", html: discourseHTML},
		{name: "user", url: "https://forum.example.com/u/alice", html: discourseHTML},
		{name: "JSON response", url: "https://forum.example.com/t/topic-title/123.json", html: discourseHTML},
		{name: "RSS response", url: "https://forum.example.com/t/topic-title/123.rss", html: discourseHTML},
		{name: "invalid post number", url: "https://forum.example.com/t/topic-title/123/latest", html: discourseHTML},
		{name: "generic preloaded data", url: "https://example.com/t/topic-title/123", html: `<script id="data-preloaded"></script>`},
		{name: "unrelated generator", url: "https://example.com/t/topic-title/123", html: `<meta name="generator" content="Another App">`},
		{name: "unrelated application", url: "https://example.com/t/topic-title/123", html: `<main>not Discourse</main>`},
	}

	extractor := &DiscourseExtractor{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := extractor.Match(&document.Document{URL: test.url, HTML: test.html})
			if got != test.want {
				t.Fatalf("Match() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestExtractPreloadedAndRenderedPosts(t *testing.T) {
	preloaded := discoursePreloadedHTML(t, 42, map[string]any{
		"title": "How should this work?",
		"tags":  []string{"support", "solved"},
		"post_stream": map[string]any{
			"posts": []map[string]any{
				{
					"id":              1001,
					"name":            "Alice Example",
					"username":        "alice",
					"created_at":      "2026-08-14T08:00:00Z",
					"cooked":          `<p>Opening question with <a href="/docs/start">the docs</a>.</p><pre><code>go test ./...</code></pre>`,
					"post_number":     1,
					"post_type":       1,
					"hidden":          false,
					"accepted_answer": false,
					"actions_summary": []map[string]any{{"id": 2, "count": 2}},
					"reactions": []map[string]any{
						{"id": "heart", "count": 2},
						{"id": "laughing", "count": 1},
					},
				},
				{
					"id":                   1002,
					"username":             "bob",
					"created_at":           "2026-08-14T09:00:00Z",
					"cooked":               `<p>Preloaded answer.</p>`,
					"post_number":          2,
					"post_type":            1,
					"reply_to_post_number": 1,
					"accepted_answer":      false,
				},
				{
					"id":          1003,
					"username":    "hidden_user",
					"cooked":      `<p>Hidden post body.</p>`,
					"post_number": 3,
					"post_type":   1,
					"hidden":      true,
				},
				{
					"id":          1004,
					"username":    "system",
					"cooked":      `<p>Topic automatically closed.</p>`,
					"post_number": 4,
					"post_type":   3,
				},
			},
		},
	})

	d := &document.Document{
		URL: "https://forum.example.com/t/how-should-this-work/42",
		HTML: fmt.Sprintf(`<html><head>
			<meta name="generator" content="Discourse 2026.8.0">
			<meta property="og:title" content="How should this work?">
			%s
		</head><body>
			<div id="topic-title">
				<h1><a href="/t/how-should-this-work/42">How should this work?</a></h1>
				<div class="topic-category"><span class="badge-category__name">Support</span></div>
			</div>
			<div class="post-stream">
				<div class="topic-post" data-post-number="2">
					<article id="post_2" data-post-id="1002">
						<div class="names">
							<span class="first full-name"><a data-user-card="bob">Bob Example</a></span>
							<span class="second username"><a data-user-card="bob">bob</a></span>
						</div>
						<span class="relative-date" title="Aug 14, 2026 9:00 am" data-time="1786698000000"></span>
						<div class="cooked"><p>Rendered <strong>accepted answer</strong>.</p><script>alert("bad")</script></div>
						<span class="accepted-text">Solution</span>
					</article>
				</div>
				<div class="topic-post" data-post-number="5">
					<article id="post_5" data-post-id="1005" data-reply-to-post-number="2">
						<div class="names"><span class="username"><a data-user-card="carol">carol</a></span></div>
						<time datetime="2026-08-14T10:00:00Z"></time>
						<div class="cooked"><p>A later rendered reply.</p></div>
					</article>
				</div>
				<div class="topic-post post-hidden" data-post-number="6">
					<article id="post_6" data-post-id="1006">
						<div class="cooked"><p>Rendered hidden post.</p></div>
					</article>
				</div>
			</div>
		</body></html>`, preloaded),
	}

	state, err := (&DiscourseExtractor{}).Extract(d).Unpack()
	if err != nil {
		t.Fatalf("Extract returned an error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("Extract state = %v, want %v", state, sdk.ExtractorSuccess)
	}
	if got, want := d.Title, "How should this work?"; got != want {
		t.Fatalf("Title = %q, want %q", got, want)
	}
	for _, want := range []string{
		"Alice Example (@alice)",
		"Opening question with the docs.",
		"go test ./...",
		"Bob Example (@bob)",
		"Rendered accepted answer.",
		"A later rendered reply.",
		"[Accepted Solution]",
		"[reply to #2]",
		"[2 likes]",
		"[1 reactions]",
	} {
		if !strings.Contains(d.Text, want) {
			t.Errorf("indexed text is missing %q:\n%s", want, d.Text)
		}
	}
	for _, unwanted := range []string{"Preloaded answer", "Hidden post body", "Rendered hidden post", "automatically closed", "alert("} {
		if strings.Contains(d.Text, unwanted) {
			t.Errorf("indexed text contains %q:\n%s", unwanted, d.Text)
		}
	}
	for key, want := range map[string]any{
		"type":            "discourse",
		"topic_id":        "42",
		"author":          "Alice Example (@alice)",
		"published":       "2026-08-14T08:00:00Z",
		"category":        "Support",
		"tags":            "support, solved",
		"posts":           3,
		"replies":         2,
		"accepted_answer": 2,
	} {
		if got := d.Metadata[key]; got != want {
			t.Errorf("Metadata[%q] = %#v, want %#v", key, got, want)
		}
	}

	preview, previewState, err := (&DiscourseExtractor{}).Preview(d).Unpack()
	if err != nil {
		t.Fatalf("Preview returned an error: %v", err)
	}
	if previewState != sdk.ExtractorSuccess {
		t.Fatalf("Preview state = %v, want %v", previewState, sdk.ExtractorSuccess)
	}
	for _, want := range []string{
		"Original post",
		"Reply #2 (accepted solution)",
		"Reply #5",
		`href="https://forum.example.com/docs/start"`,
		"<code>go test ./...</code>",
		"Bob Example (@bob)",
		"2026-08-14T09:00:00Z",
		"Rendered <strong>accepted answer</strong>.",
	} {
		if !strings.Contains(preview.Content, want) {
			t.Errorf("preview is missing %q:\n%s", want, preview.Content)
		}
	}
	for _, unwanted := range []string{"<script", "alert(", "Hidden post body"} {
		if strings.Contains(preview.Content, unwanted) {
			t.Errorf("preview contains %q:\n%s", unwanted, preview.Content)
		}
	}
}

func TestExtractCrawlerMarkup(t *testing.T) {
	d := &document.Document{
		URL: "https://forum.example.com/t/crawler-topic/77",
		HTML: `<html><head><meta name="generator" content="Discourse 3.3"></head><body>
			<div id="topic-title">
				<h1><a href="/t/crawler-topic/77">Crawler topic</a></h1>
				<div class="topic-category"><span class="category-name">Help</span></div>
				<div class="topic-category"><div class="discourse-tags"><a class="discourse-tag">howto</a></div></div>
			</div>
			<div itemscope itemtype="https://schema.org/QAPage">
				<div id="post_1" itemprop="mainEntity" class="topic-body crawler-post">
					<div class="crawler-post-meta"><span class="creator"><span itemprop="name">questioner</span></span><time itemprop="datePublished" datetime="2026-08-01T10:00:00Z"></time></div>
					<div class="post" itemprop="text"><p>Crawler question.</p></div>
				</div>
				<div id="post_2" itemprop="acceptedAnswer" class="topic-body crawler-post">
					<div class="crawler-post-meta"><span class="creator"><span itemprop="name">helper</span></span><time itemprop="datePublished" datetime="2026-08-01T11:00:00Z"></time></div>
					<div class="post" itemprop="text"><p>Crawler accepted answer.</p></div>
				</div>
			</div>
		</body></html>`,
	}

	state, err := (&DiscourseExtractor{}).Extract(d).Unpack()
	if err != nil || state != sdk.ExtractorSuccess {
		t.Fatalf("Extract returned state %v and error %v", state, err)
	}
	for _, want := range []string{"Crawler question.", "Crawler accepted answer.", "[Accepted Solution]"} {
		if !strings.Contains(d.Text, want) {
			t.Errorf("indexed text is missing %q:\n%s", want, d.Text)
		}
	}
	if got := d.Metadata["accepted_answer"]; got != 2 {
		t.Fatalf("accepted answer = %#v, want 2", got)
	}
}

func TestSchemaFallback(t *testing.T) {
	d := &document.Document{
		URL: "https://forum.example.com/t/schema-topic/88",
		HTML: `<html><head>
			<meta name="generator" content="Discourse 3.3">
			<script type="application/ld+json">{
				"@context": "https://schema.org",
				"@type": "QAPage",
				"name": "Schema topic",
				"mainEntity": {
					"@type": "Question",
					"name": "Schema topic",
					"text": "<p>Schema question.</p>",
					"author": {"name": "schema_author"},
					"datePublished": "2026-08-02T10:00:00Z",
					"acceptedAnswer": [{
						"@type": "Answer",
						"text": "<p>Schema <strong>solution</strong>.</p>",
						"author": {"name": "solution_author"},
						"url": "https://forum.example.com/t/schema-topic/88/3",
						"upvoteCount": 4
					}],
					"suggestedAnswer": {
						"@type": "Answer",
						"text": "<p>Another answer.</p>",
						"author": "another_author",
						"url": "https://forum.example.com/t/schema-topic/88/2"
					}
				}
			}</script>
		</head><body></body></html>`,
	}

	state, err := (&DiscourseExtractor{}).Extract(d).Unpack()
	if err != nil || state != sdk.ExtractorSuccess {
		t.Fatalf("Extract returned state %v and error %v", state, err)
	}
	for _, want := range []string{"Schema question.", "Another answer.", "Schema solution.", "[4 likes]"} {
		if !strings.Contains(d.Text, want) {
			t.Errorf("indexed text is missing %q:\n%s", want, d.Text)
		}
	}
	if got := d.Metadata["accepted_answer"]; got != 3 {
		t.Fatalf("accepted answer = %#v, want 3", got)
	}
}

func TestExtractRejectsNonTopicPage(t *testing.T) {
	d := &document.Document{
		URL:  "https://forum.example.com/c/support/6",
		HTML: `<meta name="generator" content="Discourse 3.3"><article data-post-id="100"><div class="cooked">Feed post</div></article>`,
	}
	state, err := (&DiscourseExtractor{}).Extract(d).Unpack()
	if err == nil {
		t.Fatal("Extract returned no error for a category page")
	}
	if state != sdk.ExtractorFallback {
		t.Fatalf("Extract state = %v, want %v", state, sdk.ExtractorFallback)
	}
	if d.Title != "" || d.Text != "" {
		t.Fatalf("category page was modified: %#v", d)
	}
}

func discoursePreloadedHTML(t *testing.T, topicID int, topic any) string {
	t.Helper()
	inner, err := json.Marshal(topic)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := json.Marshal(map[string]string{
		fmt.Sprintf("topic_%d", topicID): string(inner),
	})
	if err != nil {
		t.Fatal(err)
	}
	return `<script type="application/json" id="data-preloaded">` + string(outer) + `</script>`
}
