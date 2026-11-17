// SPDX-License-Identifier: AGPL-3.0-or-later

package bluesky

import (
	"net/url"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/extractor/sdk"
)

func TestSetConfigRejectsUnknownOptions(t *testing.T) {
	extractor := &BlueskyExtractor{}
	err := extractor.SetConfig(&config.Extractor{
		Enable:  true,
		Options: map[string]any{"unknown": true},
	})
	if err == nil {
		t.Fatal("SetConfig accepted an unknown option")
	}
}

func TestMatch(t *testing.T) {
	extractor := &BlueskyExtractor{}
	tests := []struct {
		name string
		doc  *document.Document
		want bool
	}{
		{
			name: "profile",
			doc:  &document.Document{URL: "https://bsky.app/profile/bsky.app"},
			want: true,
		},
		{
			name: "custom feed",
			doc:  &document.Document{URL: "https://www.bsky.app/profile/emily.space/feed/astro"},
			want: true,
		},
		{
			name: "embed host",
			doc:  &document.Document{URL: "https://embed.bsky.app/profile/alice.test/post/abc"},
			want: true,
		},
		{
			name: "extracted post",
			doc: &document.Document{
				URL:      "https://example.com/imported",
				Metadata: map[string]any{"type": postType},
			},
			want: true,
		},
		{
			name: "unrelated subdomain",
			doc:  &document.Document{URL: "https://api.bsky.app/profile/alice.test"},
		},
		{
			name: "lookalike host",
			doc:  &document.Document{URL: "https://notbsky.app/profile/alice.test/post/abc"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := extractor.Match(test.doc); got != test.want {
				t.Fatalf("Match() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCanonicalPostURL(t *testing.T) {
	base, err := url.Parse("https://bsky.app/profile/alice.test/feed/news")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		raw        string
		wantURL    string
		wantActor  string
		wantPostID string
		wantOK     bool
	}{
		{
			name:       "absolute URL",
			raw:        "https://www.bsky.app/profile/Alice.Test/post/3abc123?ref=feed#fragment",
			wantURL:    "https://bsky.app/profile/Alice.Test/post/3abc123",
			wantActor:  "Alice.Test",
			wantPostID: "3abc123",
			wantOK:     true,
		},
		{
			name:       "relative DID URL",
			raw:        "/profile/did:plc:abcdef/post/3xyz",
			wantURL:    "https://bsky.app/profile/did:plc:abcdef/post/3xyz",
			wantActor:  "did:plc:abcdef",
			wantPostID: "3xyz",
			wantOK:     true,
		},
		{
			name: "feed URL",
			raw:  "https://bsky.app/profile/alice.test/feed/news",
		},
		{
			name: "unrelated host",
			raw:  "https://example.com/profile/alice.test/post/3abc",
		},
		{
			name: "invalid post identifier",
			raw:  "https://bsky.app/profile/alice.test/post/not_valid",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotURL, gotActor, gotPostID, gotOK := canonicalPostURL(test.raw, base)
			if gotURL != test.wantURL || gotActor != test.wantActor || gotPostID != test.wantPostID || gotOK != test.wantOK {
				t.Fatalf(
					"canonicalPostURL(%q) = (%q, %q, %q, %v), want (%q, %q, %q, %v)",
					test.raw, gotURL, gotActor, gotPostID, gotOK,
					test.wantURL, test.wantActor, test.wantPostID, test.wantOK,
				)
			}
		})
	}
}

func TestExtractProfileSchemaPosts(t *testing.T) {
	doc := &document.Document{
		URL:    "https://bsky.app/profile/alice.test",
		UserID: 42,
		HTML: `<html><head>
			<script type="application/ld+json">{
				"@context": "https://schema.org",
				"@type": "ProfilePage",
				"hasPart": [
					{
						"@type": "DiscussionForumPosting",
						"url": "https://bsky.app/profile/alice.test/post/3first?ref=profile",
						"identifier": "at://did:plc:alice/app.bsky.feed.post/3first",
						"author": {
							"@type": "Person",
							"name": "Alice Example",
							"alternateName": "@alice.test",
							"identifier": "did:plc:alice"
						},
						"text": "First post with <unsafe> markup.",
						"image": ["https://cdn.bsky.app/first.jpg"],
						"datePublished": "2026-08-18T08:00:00.000Z",
						"isBasedOn": "https://bsky.app/profile/bob.test/post/3quoted",
						"sharedContent": {
							"@type": "WebPage",
							"url": "https://example.com/article",
							"name": "External article",
							"description": "Article summary"
						}
					},
					{
						"@type": "https://schema.org/DiscussionForumPosting",
						"url": "https://bsky.app/profile/alice.test/post/3second",
						"author": {"name": "Alice Example", "alternateName": "@alice.test"},
						"articleBody": "Second post"
					}
				]
			}</script>
		</head><body></body></html>`,
	}

	state, err := (&BlueskyExtractor{}).Extract(doc).Unpack()
	if err != nil {
		t.Fatalf("Extract returned an error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("Extract state = %v, want %v", state, sdk.ExtractorSuccess)
	}
	if !doc.SkipIndexing {
		t.Fatal("source profile was not marked to skip indexing")
	}
	if len(doc.ExtraDocuments) != 2 {
		t.Fatalf("ExtraDocuments length = %d, want 2", len(doc.ExtraDocuments))
	}

	first := doc.ExtraDocuments[0]
	if got, want := first.URL, "https://bsky.app/profile/alice.test/post/3first"; got != want {
		t.Fatalf("post URL = %q, want %q", got, want)
	}
	if got, want := first.Title, "Bluesky post: Alice Example (@alice.test)"; got != want {
		t.Fatalf("post title = %q, want %q", got, want)
	}
	if got, want := first.Text, "First post with  markup."; got != want {
		t.Fatalf("post text = %q, want %q", got, want)
	}
	if first.UserID != doc.UserID {
		t.Fatalf("post user ID = %d, want %d", first.UserID, doc.UserID)
	}
	for key, want := range map[string]any{
		"type":         postType,
		"author":       "Alice Example (@alice.test)",
		"handle":       "@alice.test",
		"did":          "did:plc:alice",
		"at_uri":       "at://did:plc:alice/app.bsky.feed.post/3first",
		"published":    "2026-08-18T08:00:00.000Z",
		"image":        "https://cdn.bsky.app/first.jpg",
		"quoted_post":  "https://bsky.app/profile/bob.test/post/3quoted",
		"external_url": "https://example.com/article",
	} {
		if got := first.Metadata[key]; got != want {
			t.Errorf("post metadata %q = %#v, want %#v", key, got, want)
		}
	}
	for _, expected := range []string{
		`src="https://cdn.bsky.app/first.jpg"`,
		`href="https://bsky.app/profile/bob.test/post/3quoted"`,
		`href="https://example.com/article"`,
		"External article",
	} {
		if !strings.Contains(first.HTML, expected) {
			t.Errorf("post HTML is missing %q: %s", expected, first.HTML)
		}
	}
	if got, want := doc.ExtraDocuments[1].Text, "Second post"; got != want {
		t.Fatalf("second post text = %q, want %q", got, want)
	}
}

func TestExtractSchemaThreadIncludesComments(t *testing.T) {
	doc := &document.Document{
		URL: "https://bsky.app/profile/alice.test/post/3root",
		HTML: `<script type="application/ld+json">{
			"@type": "WebPage",
			"mainEntity": {
				"@type": "DiscussionForumPosting",
				"url": "https://bsky.app/profile/alice.test/post/3root",
				"author": {"name": "Alice", "alternateName": "@alice.test"},
				"text": "Root post",
				"comment": [{
					"@type": "Comment",
					"url": "https://bsky.app/profile/bob.test/post/3reply",
					"author": {"name": "Bob", "alternateName": "@bob.test"},
					"text": "A reply"
				}]
			}
		}</script>`,
	}

	state, err := (&BlueskyExtractor{}).Extract(doc).Unpack()
	if err != nil {
		t.Fatal(err)
	}
	if state != sdk.ExtractorSuccess || len(doc.ExtraDocuments) != 2 {
		t.Fatalf("Extract returned state %v and %d documents", state, len(doc.ExtraDocuments))
	}
	if got, want := doc.ExtraDocuments[0].Text, "Root post"; got != want {
		t.Fatalf("root text = %q, want %q", got, want)
	}
	if got, want := doc.ExtraDocuments[1].URL, "https://bsky.app/profile/bob.test/post/3reply"; got != want {
		t.Fatalf("reply URL = %q, want %q", got, want)
	}
	if got, want := doc.ExtraDocuments[1].Metadata["author"], "Bob (@bob.test)"; got != want {
		t.Fatalf("reply author = %#v, want %#v", got, want)
	}
}

func TestExtractRenderedFeed(t *testing.T) {
	doc := &document.Document{
		URL:    "https://bsky.app/profile/feeds.test/feed/news",
		UserID: 9,
		HTML: `<main>
			<div role="link" data-testid="feedItem-by-alice.test">
				<a href="/profile/alice.test" aria-label="Alice Example's avatar"><img src="/avatars/alice.jpg"></a>
				<a href="/profile/alice.test">Alice Example</a>
				<a href="/profile/alice.test/post/3outer" aria-label="August 18, 2026 at 9:30 AM">2m</a>
				<div data-testid="contentHider-post">
					<div data-testid="postText">Rendered post with <a href="/hashtag/testing">#testing</a></div>
					<figure><img src="/media/photo.jpg" alt="A photo"></figure>
					<blockquote>
						<a href="/profile/bob.test/post/3quote">Quoted post</a>
						<div>Quoted content</div>
					</blockquote>
				</div>
			</div>
			<article role="article">
				<a href="/profile/bob.test">Bob Example</a>
				<a href="/profile/bob.test/post/3second"><time datetime="2026-08-18T09:00:00Z">now</time></a>
				<div data-testid="postText">Second rendered post</div>
			</article>
		</main>`,
	}

	state, err := (&BlueskyExtractor{}).Extract(doc).Unpack()
	if err != nil {
		t.Fatal(err)
	}
	if state != sdk.ExtractorSuccess || len(doc.ExtraDocuments) != 2 {
		t.Fatalf("Extract returned state %v and %d documents", state, len(doc.ExtraDocuments))
	}

	first := doc.ExtraDocuments[0]
	if got, want := first.URL, "https://bsky.app/profile/alice.test/post/3outer"; got != want {
		t.Fatalf("first post URL = %q, want %q", got, want)
	}
	if got, want := first.Title, "Bluesky post: Alice Example (@alice.test)"; got != want {
		t.Fatalf("first post title = %q, want %q", got, want)
	}
	if got, want := first.Metadata["published"], "August 18, 2026 at 9:30 AM"; got != want {
		t.Fatalf("publication time = %#v, want %#v", got, want)
	}
	if first.UserID != doc.UserID {
		t.Fatalf("post user ID = %d, want %d", first.UserID, doc.UserID)
	}
	for _, expected := range []string{
		`href="https://bsky.app/hashtag/testing"`,
		`src="https://bsky.app/media/photo.jpg"`,
		`href="https://bsky.app/profile/bob.test/post/3quote"`,
		"Quoted content",
	} {
		if !strings.Contains(first.HTML, expected) {
			t.Errorf("rendered post HTML is missing %q: %s", expected, first.HTML)
		}
	}
	if got, want := doc.ExtraDocuments[1].Metadata["published"], "2026-08-18T09:00:00Z"; got != want {
		t.Fatalf("second publication time = %#v, want %#v", got, want)
	}
}

func TestRenderedMarkupOverridesSchemaContentAndDeduplicates(t *testing.T) {
	doc := &document.Document{
		URL: "https://bsky.app/profile/alice.test",
		HTML: `<script type="application/ld+json">{
			"@type":"ProfilePage",
			"hasPart":[{
				"@type":"DiscussionForumPosting",
				"url":"https://bsky.app/profile/alice.test/post/3same",
				"author":{"name":"Alice","alternateName":"@alice.test"},
				"text":"Semantic plain text",
				"datePublished":"2026-08-18T10:00:00Z"
			}]
		}</script>
		<div data-testid="feedItem-by-alice.test">
			<a href="/profile/alice.test/post/3same" aria-label="August 18, 2026">now</a>
			<div data-testid="contentHider-post">
				<div data-testid="postText">Rendered <a href="https://example.com">rich text</a></div>
			</div>
		</div>`,
	}

	state, err := (&BlueskyExtractor{}).Extract(doc).Unpack()
	if err != nil {
		t.Fatal(err)
	}
	if state != sdk.ExtractorSuccess || len(doc.ExtraDocuments) != 1 {
		t.Fatalf("Extract returned state %v and %d documents", state, len(doc.ExtraDocuments))
	}
	post := doc.ExtraDocuments[0]
	if got, want := post.Text, "Rendered rich text"; got != want {
		t.Fatalf("post text = %q, want %q", got, want)
	}
	if !strings.Contains(post.HTML, `href="https://example.com"`) {
		t.Fatalf("rendered HTML did not replace schema HTML: %s", post.HTML)
	}
	if got, want := post.Metadata["published"], "2026-08-18T10:00:00Z"; got != want {
		t.Fatalf("semantic publication time = %#v, want %#v", got, want)
	}
}

func TestGenericPostAnchorFallback(t *testing.T) {
	doc := &document.Document{
		URL: "https://bsky.app/profile/alice.test/feed/future",
		HTML: `<div role="link" data-component="future-post-card">
			<a href="/profile/alice.test">Alice</a>
			<div data-testid="postText">Future markup post</div>
			<a href="/profile/alice.test/post/3future" aria-label="August 18, 2026">now</a>
		</div>`,
	}

	state, err := (&BlueskyExtractor{}).Extract(doc).Unpack()
	if err != nil {
		t.Fatal(err)
	}
	if state != sdk.ExtractorSuccess || len(doc.ExtraDocuments) != 1 {
		t.Fatalf("Extract returned state %v and %d documents", state, len(doc.ExtraDocuments))
	}
	if got, want := doc.ExtraDocuments[0].Text, "Future markup post"; got != want {
		t.Fatalf("post text = %q, want %q", got, want)
	}
}

func TestExtractDirectPostMetadataFallback(t *testing.T) {
	doc := &document.Document{
		URL: "https://bsky.app/profile/alice.test/post/3fallback?ref=share",
		HTML: `<html><head>
			<meta property="og:title" content="Alice Example (@alice.test) on Bluesky">
			<meta property="og:description" content="A post available without rendered markup.">
			<meta property="article:published_time" content="2026-08-18T09:45:00.000Z">
			<meta property="og:image" content="https://cdn.bsky.app/fallback.jpg">
		</head><body><div id="application"></div></body></html>`,
	}

	state, err := (&BlueskyExtractor{}).Extract(doc).Unpack()
	if err != nil {
		t.Fatal(err)
	}
	if state != sdk.ExtractorSuccess || len(doc.ExtraDocuments) != 1 {
		t.Fatalf("Extract returned state %v and %d documents", state, len(doc.ExtraDocuments))
	}
	post := doc.ExtraDocuments[0]
	if got, want := post.URL, "https://bsky.app/profile/alice.test/post/3fallback"; got != want {
		t.Fatalf("post URL = %q, want %q", got, want)
	}
	if got, want := post.Title, "Bluesky post: Alice Example (@alice.test)"; got != want {
		t.Fatalf("post title = %q, want %q", got, want)
	}
	if got, want := post.Text, "A post available without rendered markup."; got != want {
		t.Fatalf("post text = %q, want %q", got, want)
	}
	if got, want := post.Metadata["image"], "https://cdn.bsky.app/fallback.jpg"; got != want {
		t.Fatalf("post image = %#v, want %#v", got, want)
	}
	if !strings.Contains(post.HTML, `src="https://cdn.bsky.app/fallback.jpg"`) {
		t.Fatalf("post HTML is missing the fallback image: %s", post.HTML)
	}
}

func TestExtractSkipsPageWhenNoPostsFound(t *testing.T) {
	doc := &document.Document{
		URL:  "https://bsky.app/",
		HTML: `<html><body><div id="application"></div></body></html>`,
	}

	state, err := (&BlueskyExtractor{}).Extract(doc).Unpack()
	if err != nil {
		t.Fatal(err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("Extract state = %v, want %v", state, sdk.ExtractorSuccess)
	}
	if !doc.SkipIndexing {
		t.Fatal("source page was not marked to skip indexing")
	}
	if len(doc.ExtraDocuments) != 0 {
		t.Fatalf("ExtraDocuments length = %d, want 0", len(doc.ExtraDocuments))
	}
}

func TestExtractStopsForExtractedPost(t *testing.T) {
	doc := &document.Document{
		URL:      "https://bsky.app/profile/alice.test/post/3abc",
		HTML:     `<div data-testid="feedItem-by-bob.test"></div>`,
		Metadata: map[string]any{"type": postType},
	}

	state, err := (&BlueskyExtractor{}).Extract(doc).Unpack()
	if err != nil {
		t.Fatal(err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("Extract state = %v, want %v", state, sdk.ExtractorSuccess)
	}
	if doc.SkipIndexing {
		t.Fatal("extracted post was marked to skip indexing")
	}
	if len(doc.ExtraDocuments) != 0 {
		t.Fatalf("ExtraDocuments length = %d, want 0", len(doc.ExtraDocuments))
	}
}

func TestPreviewSanitizesPostHTML(t *testing.T) {
	doc := &document.Document{
		Title: `Post <script>alert("title")</script>`,
		HTML:  `<p onclick="alert(1)">Safe text<script>alert(2)</script><a href="javascript:alert(3)">bad link</a></p>`,
	}

	response, state, err := (&BlueskyExtractor{}).Preview(doc).Unpack()
	if err != nil {
		t.Fatal(err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("Preview state = %v, want %v", state, sdk.ExtractorSuccess)
	}
	for _, disallowed := range []string{"<script", "onclick", "javascript:"} {
		if strings.Contains(strings.ToLower(response.Content), disallowed) {
			t.Fatalf("preview contains %q: %s", disallowed, response.Content)
		}
	}
	if !strings.Contains(response.Content, "Safe text") {
		t.Fatalf("preview is missing post text: %s", response.Content)
	}
}
