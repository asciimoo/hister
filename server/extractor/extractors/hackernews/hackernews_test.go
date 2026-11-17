package hackernews

import (
	"strings"
	"testing"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/extractor/sdk"
)

// itemHTML mirrors the structure of a news.ycombinator.com item page: the
// submission in a fatitem table, then one flat comment table whose rows carry
// their depth in the indent attribute of the leading td.ind cell.
const itemHTML = `<html><body><center><table id="hnmain"><tr><td>
<table class="fatitem"><tbody>
  <tr class="athing submission" id="1">
    <td class="title"><span class="titleline"><a href="http://example.com/post">Example Post</a><span class="sitebit comhead"> (<a href="from?site=example.com"><span class="sitestr">example.com</span></a>)</span></span></td>
  </tr>
  <tr><td class="subtext"><span class="subline">
    <span class="score" id="score_1">57 points</span> by <a href="user?id=alice" class="hnuser">alice</a>
    <span class="age" title="2026-01-02T03:04:05 1767322445"><a href="item?id=1">2 hours ago</a></span>
  </span></td></tr>
  <tr><td class="toptext">Body of the submission.</td></tr>
</tbody></table>
<table class="comment-tree"><tbody>
  <tr class="athing comtr" id="10"><td><table><tr>
    <td class="ind" indent="0"><img src="s.gif"></td>
    <td class="default"><div><span class="comhead"><a href="user?id=bob" class="hnuser">bob</a>
      <span class="age" title="2026-01-02T04:00:00 1767326400"><a href="item?id=10">1 hour ago</a></span></span></div>
      <div class="comment"><div class="commtext c00">Top level remark.<p>Second paragraph, see <a href="item?id=8863">this thread</a>.</p></div></div></td>
  </tr></table></td></tr>
  <tr class="athing comtr" id="11"><td><table><tr>
    <td class="ind" indent="1"><img src="s.gif"></td>
    <td class="default"><div><span class="comhead"><a href="user?id=carol" class="hnuser">carol</a>
      <span class="age" title="2026-01-02T05:00:00 1767330000"><a href="item?id=11">30 minutes ago</a></span></span></div>
      <div class="comment"><div class="commtext c00">Nested reply.</div></div></td>
  </tr></table></td></tr>
  <tr class="athing comtr" id="12"><td><table><tr>
    <td class="ind" indent="0"><img src="s.gif"></td>
    <td class="default"><div><span class="comhead"><a href="user?id=dan" class="hnuser">dan</a>
      <span class="age" title="2026-01-02T06:00:00 1767333600"><a href="item?id=12">10 minutes ago</a></span></span></div>
      <div class="comment"><div class="commtext c00">Back to top level.</div></div></td>
  </tr></table></td></tr>
</tbody></table>
</td></tr></table></center></body></html>`

func TestSetConfigRejectsUnknownOptions(t *testing.T) {
	err := (&HackerNewsExtractor{}).SetConfig(&config.Extractor{
		Enable:  true,
		Options: map[string]any{"unknown": true},
	})
	if err == nil {
		t.Fatal("SetConfig accepted an unknown option")
	}
}

func TestMatchOnlyAcceptsItemPages(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "item", url: "https://news.ycombinator.com/item?id=1", want: true},
		{name: "item with extra params", url: "https://news.ycombinator.com/item?id=1&p=2", want: true},
		{name: "trailing slash", url: "https://news.ycombinator.com/item/?id=9", want: true},
		{name: "plain http", url: "http://news.ycombinator.com/item?id=9", want: true},
		{name: "www host", url: "https://www.news.ycombinator.com/item?id=9", want: true},
		{name: "front page", url: "https://news.ycombinator.com/", want: false},
		{name: "newest listing", url: "https://news.ycombinator.com/newest", want: false},
		{name: "item without id", url: "https://news.ycombinator.com/item", want: false},
		{name: "user profile", url: "https://news.ycombinator.com/user?id=alice", want: false},
		{name: "lookalike host", url: "https://news.ycombinator.com.example/item?id=1", want: false},
		{name: "unrelated host", url: "https://example.com/item?id=1", want: false},
	}

	extractor := &HackerNewsExtractor{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := extractor.Match(&document.Document{URL: test.url}); got != test.want {
				t.Errorf("Match(%q) = %v, want %v", test.url, got, test.want)
			}
		})
	}
}

func TestExtractCollectsSubmissionAndComments(t *testing.T) {
	d := &sdk.Document{URL: "https://news.ycombinator.com/item?id=1", HTML: itemHTML}
	if res := (&HackerNewsExtractor{}).Extract(d); res.Err() != nil {
		t.Fatalf("Extract returned %v", res.Err())
	}

	if d.Title != "Example Post" {
		t.Errorf("Title = %q, want %q", d.Title, "Example Post")
	}
	for _, want := range []string{
		"example.com",
		"57 points",
		"by alice",
		"Body of the submission.",
		"Top level remark.",
		"Nested reply.",
		"Back to top level.",
		"bob",
		"carol",
	} {
		if !strings.Contains(d.Text, want) {
			t.Errorf("Text is missing %q\ngot:\n%s", want, d.Text)
		}
	}
}

func TestExtractIndentsRepliesByDepth(t *testing.T) {
	d := &sdk.Document{URL: "https://news.ycombinator.com/item?id=1", HTML: itemHTML}
	if res := (&HackerNewsExtractor{}).Extract(d); res.Err() != nil {
		t.Fatalf("Extract returned %v", res.Err())
	}

	// The reply sits one level deep, so it must carry more leading whitespace
	// than the top level comments around it.
	for line := range strings.SplitSeq(d.Text, "\n") {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(trimmed)
		switch trimmed {
		case "Top level remark.", "Back to top level.":
			if indent != 0 {
				t.Errorf("%q indented by %d, want 0", trimmed, indent)
			}
		case "Nested reply.":
			if indent == 0 {
				t.Errorf("%q was not indented", trimmed)
			}
		}
	}
}

func TestExtractKeepsParagraphBoundaries(t *testing.T) {
	d := &sdk.Document{URL: "https://news.ycombinator.com/item?id=1", HTML: itemHTML}
	if res := (&HackerNewsExtractor{}).Extract(d); res.Err() != nil {
		t.Fatalf("Extract returned %v", res.Err())
	}

	// bob's comment has two paragraphs; each must land on its own line rather
	// than running together the way goquery's Text() would leave them.
	if strings.Contains(d.Text, "remark.Second") {
		t.Errorf("paragraphs ran together:\n%s", d.Text)
	}
	if !strings.Contains(d.Text, "Top level remark.\n") {
		t.Errorf("first paragraph does not end its line:\n%s", d.Text)
	}
	if !strings.Contains(d.Text, "Second paragraph, see this thread.") {
		t.Errorf("second paragraph is missing:\n%s", d.Text)
	}
}

func TestExtractFallsBackWithoutContent(t *testing.T) {
	d := &sdk.Document{URL: "https://news.ycombinator.com/item?id=1", HTML: "<html><body></body></html>"}
	if res := (&HackerNewsExtractor{}).Extract(d); res.Err() == nil {
		t.Error("Extract accepted a page with no submission")
	}
}

func TestPreviewNestsCommentsAndBalancesTags(t *testing.T) {
	d := &sdk.Document{URL: "https://news.ycombinator.com/item?id=1", HTML: itemHTML}
	res := (&HackerNewsExtractor{}).Preview(d)
	if res.Err() != nil {
		t.Fatalf("Preview returned %v", res.Err())
	}
	html := res.Response().Content

	if open, closed := strings.Count(html, "<ul>"), strings.Count(html, "</ul>"); open != closed {
		t.Errorf("unbalanced ul: %d open, %d closed\n%s", open, closed, html)
	}
	if open, closed := strings.Count(html, "<li>"), strings.Count(html, "</li>"); open != closed {
		t.Errorf("unbalanced li: %d open, %d closed\n%s", open, closed, html)
	}
	// Two depths in the fixture must produce two levels of nesting.
	if got := strings.Count(html, "<ul>"); got != 2 {
		t.Errorf("got %d ul, want 2 (one per depth)\n%s", got, html)
	}
	if !strings.Contains(html, "Example Post") {
		t.Errorf("preview is missing the title\n%s", html)
	}
}

func TestPreviewResolvesRelativeLinks(t *testing.T) {
	d := &sdk.Document{URL: "https://news.ycombinator.com/item?id=1", HTML: itemHTML}
	res := (&HackerNewsExtractor{}).Preview(d)
	if res.Err() != nil {
		t.Fatalf("Preview returned %v", res.Err())
	}
	html := res.Response().Content

	// The relative item?id=8863 link in bob's comment must survive
	// sanitization, which drops URLs that are not absolute http(s).
	if !strings.Contains(html, `href="https://news.ycombinator.com/item?id=8863"`) {
		t.Errorf("relative comment link was not resolved\n%s", html)
	}
}

func TestPreviewResolvesRelativeTitleLink(t *testing.T) {
	// Ask HN and poll submissions link their own title to the relative
	// item?id=N URL.
	html := strings.Replace(itemHTML, `href="http://example.com/post"`, `href="item?id=1"`, 1)
	d := &sdk.Document{URL: "https://news.ycombinator.com/item?id=1", HTML: html}
	res := (&HackerNewsExtractor{}).Preview(d)
	if res.Err() != nil {
		t.Fatalf("Preview returned %v", res.Err())
	}
	if !strings.Contains(res.Response().Content, `href="https://news.ycombinator.com/item?id=1"`) {
		t.Errorf("relative title link was not resolved\n%s", res.Response().Content)
	}
}
