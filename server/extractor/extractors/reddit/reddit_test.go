package reddit

import (
	"strings"
	"testing"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/extractor/sdk"
)

func TestSetConfigRejectsUnknownOptions(t *testing.T) {
	err := (&RedditExtractor{}).SetConfig(&config.Extractor{
		Enable:  true,
		Options: map[string]any{"unknown": true},
	})
	if err == nil {
		t.Fatal("SetConfig accepted an unknown option")
	}
}

func TestMatchOnlyAcceptsPostPages(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "current post", url: "https://www.reddit.com/r/golang/comments/18ujt6g/new_at_go_start_here/", want: true},
		{name: "legacy post", url: "https://old.reddit.com/r/golang/comments/18ujt6g/", want: true},
		{name: "post comment permalink", url: "https://reddit.com/r/golang/comments/18ujt6g/title/kf123ab/?context=3", want: true},
		{name: "global comments path", url: "https://www.reddit.com/comments/18ujt6g/title/", want: true},
		{name: "short post link", url: "https://redd.it/18ujt6g", want: true},
		{name: "subreddit", url: "https://www.reddit.com/r/golang/", want: false},
		{name: "user profile", url: "https://www.reddit.com/user/alice/", want: false},
		{name: "search", url: "https://www.reddit.com/search/?q=golang", want: false},
		{name: "comments listing", url: "https://www.reddit.com/r/golang/comments/", want: false},
		{name: "JSON response", url: "https://www.reddit.com/r/golang/comments/18ujt6g/title.json", want: false},
		{name: "lookalike host", url: "https://reddit.com.example/r/golang/comments/18ujt6g/title", want: false},
		{name: "unrelated host", url: "https://example.com/r/golang/comments/18ujt6g/title", want: false},
	}

	extractor := &RedditExtractor{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := extractor.Match(&document.Document{URL: test.url})
			if got != test.want {
				t.Fatalf("Match() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestExtractShredditPostAndVisibleComments(t *testing.T) {
	d := &document.Document{
		URL: "https://www.reddit.com/r/golang/comments/abc123/a_durable_parser/",
		HTML: `<html><body>
			<aside>
				<shreddit-post id="t3_other" post-title="A recommendation" permalink="/r/other/comments/other/a_recommendation/"></shreddit-post>
			</aside>
			<main>
				<article>
					<shreddit-post id="t3_abc123" post-title="A durable parser" author="post_author"
						subreddit-prefixed-name="r/golang" score="42" comment-count="99"
						created-timestamp="2026-08-12T10:00:00Z" post-flair-text="Discussion"
						content-href="https://example.com/articles/parser">
						<div slot="text-body">
							<p>Post <strong>body</strong>.</p>
							<p>Read the <a href="/r/golang/wiki/rules">rules</a>.</p>
							<script>alert("post")</script>
						</div>
					</shreddit-post>
				</article>
				<shreddit-comment-tree>
					<shreddit-comment thingid="t1_first" author="alice" score="10" depth="0" created-timestamp="2026-08-12T11:00:00Z">
						<div slot="comment"><p>First <em>comment</em>.</p></div>
						<shreddit-comment thingid="t1_reply" author="bob" score="3" depth="1">
							<div slot="comment"><p>A nested reply with <a href="/user/bob">a profile link</a>.</p></div>
						</shreddit-comment>
					</shreddit-comment>
					<shreddit-comment thingid="t1_last" author="carol" score="1" depth="0">
						<div slot="comment"><p>Last visible comment.</p><script>alert("comment")</script></div>
						<faceplate-partial><button>Load hidden comment text</button></faceplate-partial>
					</shreddit-comment>
				</shreddit-comment-tree>
			</main>
		</body></html>`,
	}

	state, err := (&RedditExtractor{}).Extract(d).Unpack()
	if err != nil {
		t.Fatalf("Extract returned an error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("Extract state = %v, want %v", state, sdk.ExtractorSuccess)
	}
	if got, want := d.Title, "A durable parser"; got != want {
		t.Fatalf("Title = %q, want %q", got, want)
	}
	for _, want := range []string{
		"Post body.",
		"First comment.",
		"A nested reply with a profile link.",
		"Last visible comment.",
		"https://example.com/articles/parser",
	} {
		if !strings.Contains(d.Text, want) {
			t.Errorf("indexed text is missing %q:\n%s", want, d.Text)
		}
	}
	for _, unwanted := range []string{"A recommendation", "Load hidden comment text", "alert("} {
		if strings.Contains(d.Text, unwanted) {
			t.Errorf("indexed text contains %q:\n%s", unwanted, d.Text)
		}
	}
	if strings.Count(d.Text, "A nested reply") != 1 {
		t.Fatalf("nested reply was not extracted exactly once:\n%s", d.Text)
	}
	for key, want := range map[string]any{
		"type":      "reddit",
		"author":    "post_author",
		"subreddit": "r/golang",
		"published": "2026-08-12T10:00:00Z",
		"score":     "42",
		"flair":     "Discussion",
		"link":      "https://example.com/articles/parser",
		"post_id":   "abc123",
		"comments":  3,
	} {
		if got := d.Metadata[key]; got != want {
			t.Errorf("Metadata[%q] = %#v, want %#v", key, got, want)
		}
	}

	preview, previewState, err := (&RedditExtractor{}).Preview(d).Unpack()
	if err != nil {
		t.Fatalf("Preview returned an error: %v", err)
	}
	if previewState != sdk.ExtractorSuccess {
		t.Fatalf("Preview state = %v, want %v", previewState, sdk.ExtractorSuccess)
	}
	for _, want := range []string{
		"A durable parser",
		"First <em>comment</em>.",
		"A nested reply",
		`href="https://www.reddit.com/user/bob"`,
		`href="https://www.reddit.com/r/golang/wiki/rules"`,
		`style="margin-left: 2em"`,
	} {
		if !strings.Contains(preview.Content, want) {
			t.Errorf("preview is missing %q:\n%s", want, preview.Content)
		}
	}
	for _, unwanted := range []string{"<script", "alert(", "Load hidden comment text"} {
		if strings.Contains(preview.Content, unwanted) {
			t.Errorf("preview contains %q:\n%s", unwanted, preview.Content)
		}
	}
}

func TestExtractLegacyRedditPost(t *testing.T) {
	d := &document.Document{
		URL: "https://old.reddit.com/r/golang/comments/abc123/legacy_post/",
		HTML: `<html><body>
			<div id="siteTable">
				<div class="thing link self" data-fullname="t3_abc123" data-author="legacy_author" data-score="17" data-url="/r/golang/comments/abc123/legacy_post/">
					<div class="entry">
						<p class="title"><a class="title" href="/r/golang/comments/abc123/legacy_post/">Legacy post title</a></p>
						<p class="tagline"><a class="author">legacy_author</a><time datetime="2020-01-02T03:04:05Z"></time></p>
						<div class="usertext-body"><div class="md"><p>Legacy post body.</p></div></div>
					</div>
				</div>
			</div>
			<div class="commentarea">
				<div class="thing comment" data-fullname="t1_parent" data-author="parent_author" data-score="8">
					<div class="entry"><p class="tagline"><a class="author">parent_author</a></p><div class="usertext-body"><div class="md"><p>Legacy parent comment.</p></div></div></div>
					<div class="child"><div class="sitetable nestedlisting">
						<div class="thing comment" data-fullname="t1_child" data-author="child_author" data-score="2">
							<div class="entry"><p class="tagline"><a class="author">child_author</a></p><div class="usertext-body"><div class="md"><p>Legacy child comment.</p></div></div></div>
						</div>
					</div></div>
				</div>
			</div>
		</body></html>`,
	}

	state, err := (&RedditExtractor{}).Extract(d).Unpack()
	if err != nil || state != sdk.ExtractorSuccess {
		t.Fatalf("Extract returned state %v and error %v", state, err)
	}
	if got, want := d.Title, "Legacy post title"; got != want {
		t.Fatalf("Title = %q, want %q", got, want)
	}
	for _, want := range []string{"Legacy post body.", "Legacy parent comment.", "Legacy child comment."} {
		if !strings.Contains(d.Text, want) {
			t.Errorf("indexed text is missing %q:\n%s", want, d.Text)
		}
	}
	if strings.Count(d.Text, "Legacy child comment.") != 1 {
		t.Fatalf("child comment was not extracted exactly once:\n%s", d.Text)
	}
	if got, want := d.Metadata["subreddit"], "r/golang"; got != want {
		t.Fatalf("subreddit = %#v, want %#v", got, want)
	}
	if got := d.Metadata["comments"]; got != 2 {
		t.Fatalf("comment count = %#v, want 2", got)
	}
}

func TestStructuredDataFallback(t *testing.T) {
	d := &document.Document{
		URL: "https://www.reddit.com/r/golang/comments/schema1/schema_post/",
		HTML: `<html><head><script type="application/ld+json">{
			"@context": "https://schema.org",
			"@type": "DiscussionForumPosting",
			"headline": "Schema post",
			"text": "Schema post body.",
			"author": {"@type": "Person", "name": "schema_author"},
			"datePublished": "2026-08-10T09:00:00Z",
			"upvoteCount": 12,
			"comment": [{
				"@type": "Comment",
				"identifier": "first",
				"text": "Schema comment.",
				"author": {"name": "comment_author"},
				"comment": [{"@type": "Comment", "identifier": "reply", "text": "Schema reply.", "author": "reply_author"}]
			}]
		}</script></head><body></body></html>`,
	}

	state, err := (&RedditExtractor{}).Extract(d).Unpack()
	if err != nil || state != sdk.ExtractorSuccess {
		t.Fatalf("Extract returned state %v and error %v", state, err)
	}
	if got, want := d.Title, "Schema post"; got != want {
		t.Fatalf("Title = %q, want %q", got, want)
	}
	for _, want := range []string{"Schema post body.", "Schema comment.", "Schema reply."} {
		if !strings.Contains(d.Text, want) {
			t.Errorf("indexed text is missing %q:\n%s", want, d.Text)
		}
	}
	if got := d.Metadata["comments"]; got != 2 {
		t.Fatalf("comment count = %#v, want 2", got)
	}
}

func TestExtractContinuesWhenPostMarkupIsMissing(t *testing.T) {
	d := &document.Document{
		URL:  "https://www.reddit.com/r/golang/comments/abc123/title/",
		HTML: `<html><head><title>Reddit block page</title></head><body>blocked</body></html>`,
	}
	state, err := (&RedditExtractor{}).Extract(d).Unpack()
	if err == nil {
		t.Fatal("Extract returned no error for missing post markup")
	}
	if state != sdk.ExtractorFallback {
		t.Fatalf("Extract state = %v, want %v", state, sdk.ExtractorFallback)
	}
}

func TestExtractRejectsNonPostRedditPage(t *testing.T) {
	d := &document.Document{
		URL:  "https://www.reddit.com/r/golang/",
		HTML: `<main><shreddit-post id="t3_abc123" post-title="Feed post"></shreddit-post></main>`,
	}
	state, err := (&RedditExtractor{}).Extract(d).Unpack()
	if err == nil {
		t.Fatal("Extract returned no error for a subreddit listing")
	}
	if state != sdk.ExtractorFallback {
		t.Fatalf("Extract state = %v, want %v", state, sdk.ExtractorFallback)
	}
	if d.Title != "" || d.Text != "" {
		t.Fatalf("non post page was modified: %#v", d)
	}
}
