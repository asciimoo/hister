// SPDX-License-Identifier: AGPL-3.0-or-later

package github

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/server/extractor/sdk"
)

const richIssuePage = `<title>Fix &lt;img src=x onerror=alert(1)&gt; · Issue #305</title>
<bdi data-testid="issue-title">Fix &lt;img src=x onerror=alert(1)&gt;</bdi>
<span data-testid="header-state" data-status="issueClosed">Closed</span>
<div data-testid="issue-body">
  <a data-testid="issue-body-header-author" href="/alice">alice</a>
  <a data-testid="issue-body-header-link" href="#issue-1"><relative-time datetime="2026-04-09T07:47:32Z">on Apr 9, 2026</relative-time></a>
  <div id="issue-body-viewer"><div data-testid="markdown-body"><div class="markdown-body">
    <p>First paragraph</p><p>Second <strong>paragraph</strong></p>
    <pre><code>go test ./...</code></pre><blockquote>Quoted text</blockquote>
    <ul><li><input type="checkbox" checked disabled>Done</li><li><input type="checkbox" disabled>Todo</li></ul>
    <a href='/owner/repo/issues/42'>Related issue</a><a href='#issuecomment-2'>Reply</a>
    <img src='/images/example.png' onerror="alert(1)" alt="Example">
    <script>alert(1)</script><a href="javascript:alert(1)">Unsafe link</a>
  </div></div><div>Reactions are currently unavailable</div></div>
</div>
<div data-testid="issue-viewer-comments-container">
  <div data-testid="comment-viewer-outer-box-1">
    <div data-testid="comment-header" id="issuecomment-2">
      <a data-testid="avatar-link" href="/bob">bob</a>
      <a href="#issuecomment-2"><relative-time datetime="2026-04-10T07:47:32Z">on Apr 10, 2026</relative-time></a>
      <button>Comment actions</button>
    </div>
    <div data-testid="markdown-body"><div class="markdown-body"><p>Try <code>--verbose</code></p></div></div>
  </div>
  <div data-testid="comment-viewer-outer-box-2">
    <div data-testid="comment-header"><a data-testid="avatar-link" href="javascript:alert(1)">eve</a>
      <a href="javascript:alert(1)"><relative-time>on Apr 11, 2026</relative-time></a></div>
    <div data-testid="markdown-body"><p>Second reply</p></div>
  </div>
</div>
<div data-testid="issue-labels"><a><span data-component="Text">bug</span><span class="sr-only">Something is broken</span></a></div>`

type previewCommentData struct {
	Author    string `json:"author"`
	AuthorURL string `json:"authorURL"`
	CreatedAt string `json:"createdAt"`
	URL       string `json:"url"`
	BodyHTML  string `json:"bodyHTML"`
}

func TestIssuePreviewStructuredContent(t *testing.T) {
	d := &sdk.Document{URL: "https://github.com/owner/repo/issues/305", HTML: richIssuePage}
	response, state, err := (&GitHubExtractor{}).Preview(d).Unpack()
	if err != nil || state != sdk.ExtractorSuccess || response.Template != "issue" {
		t.Fatalf("preview = template %q, state %v, error %v", response.Template, state, err)
	}
	var data struct {
		Title, Number, Repository, State, URL string
		Labels                                []string
		Body                                  previewCommentData
		Comments                              []previewCommentData
	}
	if err := json.Unmarshal([]byte(response.Content), &data); err != nil {
		t.Fatal(err)
	}
	if data.Title != `Fix <img src=x onerror=alert(1)>` || data.Number != "305" || data.Repository != "owner/repo" || data.State != "Closed" || data.URL != d.URL {
		t.Fatalf("unexpected issue header: %+v", data)
	}
	if len(data.Labels) != 1 || data.Labels[0] != "bug" {
		t.Errorf("labels = %v", data.Labels)
	}
	if data.Body.Author != "alice" || data.Body.AuthorURL != "https://github.com/alice" || data.Body.CreatedAt != "2026-04-09T07:47:32Z" || data.Body.URL != d.URL+"#issue-1" {
		t.Errorf("body attribution = %+v", data.Body)
	}
	for _, want := range []string{"<p>First paragraph</p>", "<strong>paragraph</strong>", "<pre><code>go test ./...</code></pre>", "Quoted text", "☑", "☐", `href="https://github.com/owner/repo/issues/42"`, `href="` + d.URL + `#issuecomment-2"`, `src="https://github.com/images/example.png"`} {
		if !strings.Contains(data.Body.BodyHTML, want) {
			t.Errorf("body missing %q: %s", want, data.Body.BodyHTML)
		}
	}
	for _, unsafe := range []string{"<script", "onerror", "javascript:", "<input"} {
		if strings.Contains(data.Body.BodyHTML, unsafe) {
			t.Errorf("unsafe content %q survived: %s", unsafe, data.Body.BodyHTML)
		}
	}
	if len(data.Comments) != 2 {
		t.Fatalf("comments = %+v", data.Comments)
	}
	if data.Comments[0].Author != "bob" || data.Comments[0].URL != d.URL+"#issuecomment-2" || !strings.Contains(data.Comments[0].BodyHTML, "<code>--verbose</code>") {
		t.Errorf("first comment = %+v", data.Comments[0])
	}
	if data.Comments[1].AuthorURL != "" || data.Comments[1].URL != "" || data.Comments[1].CreatedAt != "on Apr 11, 2026" {
		t.Errorf("second comment = %+v", data.Comments[1])
	}
	if strings.Contains(response.Content, "Comment actions") {
		t.Error("preview contains page controls")
	}
	if strings.Contains(response.Content, "Reactions are currently unavailable") {
		t.Error("preview contains reaction controls outside the issue description")
	}
}

func TestIssuePreviewFallbackAndEmptyBody(t *testing.T) {
	for _, tc := range []struct {
		name, html string
		want       sdk.Decision
	}{
		{"empty", "", sdk.ExtractorFallback},
		{"login", "<title>Sign in to GitHub</title><p>Sign in</p>", sdk.ExtractorFallback},
		{"unrelated markdown", `<div data-testid="markdown-body">Navigation</div>`, sdk.ExtractorFallback},
		{"no description", `<bdi data-testid="issue-title">Empty issue</bdi><div id="issue-body-viewer"></div>`, sdk.ExtractorSuccess},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &sdk.Document{URL: "https://github.com/owner/repo/issues/305", HTML: tc.html}
			_, state, err := (&GitHubExtractor{}).Preview(d).Unpack()
			if state != tc.want || err != nil {
				t.Fatalf("preview state = %v, err = %v", state, err)
			}
			if result := (&GitHubExtractor{}).Extract(d); result.Decision() != tc.want {
				t.Errorf("extract state = %v, want %v", result.Decision(), tc.want)
			}
		})
	}
}

func TestIssueURLMatching(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://github.com/owner/repo/issues/305#issuecomment-123", true},
		{"https://github.com/owner/repo/issues/305/?notification_referrer_id=1#issue-123", true},
		{"https://githubXcom/owner/repo/issues/305", false},
		{"https://github.com/owner/repo/issues/305/edit", false},
	} {
		if got := (&GitHubExtractor{}).Match(&sdk.Document{URL: tc.url}); got != tc.want {
			t.Errorf("Match(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestIssueExtractionPreservesParagraphs(t *testing.T) {
	d := &sdk.Document{URL: "https://github.com/owner/repo/issues/305", HTML: richIssuePage}
	if result := (&GitHubExtractor{}).Extract(d); result.Decision() != sdk.ExtractorSuccess {
		t.Fatal(result.Err())
	}
	if !strings.Contains(d.Text, "First paragraph\n") || strings.Contains(d.Text, "alert(1)Unsafe") || strings.Contains(d.Text, "<code>") {
		t.Errorf("unreadable extracted text: %s", d.Text)
	}
}
