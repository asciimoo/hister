package github

import (
	"strings"
	"testing"

	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/types"
)

func TestMatchGitHubURLs(t *testing.T) {
	e := &GitHubExtractor{}
	cases := []struct {
		url  string
		want bool
	}{
		// only top-level is supported
		{"https://github.com/asciimoo", false},
		{"https://github.com/asciimoo/hister", true},
		{"https://github.com/asciimoo/hister/", true},
		{"https://github.com/asciimoo/hister?tab=readme-ov-file", true},
		{"https://github.com/asciimoo/hister#community", true},
		{"https://github.com/asciimoo/hister/issues", true},
		{"https://github.com/asciimoo/hister/issues/305", true},
		{"https://github.com/asciimoo/hister/issues/305/", true},
		{"https://github.com/asciimoo/hister/pulls", false},
		{"https://github.com/asciimoo/hister/pull/495", true},
		{"https://github.com/asciimoo/hister/pull/495#issuecomment-4873615716", true},
		{"https://github.com/asciimoo/hister/settings", false},
		{"https://github.com/topics/react-native", false},
		{"https://de.wikipedia.org/wiki/Berlin", false},
		{"https://stackoverflow.com/questions/1234", false},
		{"https://example.com/wiki/Foo", false},
	}
	for _, tc := range cases {
		d := &document.Document{URL: tc.url}
		if got := e.Match(d); got != tc.want {
			t.Errorf("Match(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

// --- Repositories --------------------------------------------------------
const minimalRepoPage = `<html>
<head><title>asciimoo/hister: Your own search engine</title></head>
<body>
<div><p class="f4">Your own search engine</p></div>
<div>
<form class="js-social-form BtnGroup-parent flex-auto js-deferred-toggler-target" data-turbo="false" action="/asciimoo/hister/unstar" accept-charset="UTF-8" method="post">
<button>
<path d="star"></path>
</svg><span data-view-component="true" class="d-inline">Starred</span>
<span id="repo-stars-counter-unstar" aria-label="1255 users starred this repository" title="1,255">1.3k</span>
</button></form>

<h3 class="sr-only">Topics</h3>
<div class="tmp-my-3">
<div class="f6">
<a href="/topics/search" title="Topic: search" data-view-component="true" class="topic-tag topic-tag-link">
search
</a>
<a href="/topics/go" title="Topic: go" data-view-component="true" class="topic-tag topic-tag-link">
go
</a>
<a href="/topics/golang" title="Topic: golang" data-view-component="true" class="topic-tag topic-tag-link">
golang
</a>
<a href="/topics/search-engine" title="Topic: search-engine" data-view-component="true" class="topic-tag topic-tag-link">
search-engine
</a>
<a href="/topics/privacy" title="Topic: privacy" data-view-component="true" class="topic-tag topic-tag-link">
privacy
</a>
<a href="/topics/web" title="Topic: web" data-view-component="true" class="topic-tag topic-tag-link">
web
</a>
<a href="/topics/mcp" title="Topic: mcp" data-view-component="true" class="topic-tag topic-tag-link">
mcp
</a>
<a href="/topics/history" title="Topic: history" data-view-component="true" class="topic-tag topic-tag-link">
history
</a>
<a href="/topics/index" title="Topic: index" data-view-component="true" class="topic-tag topic-tag-link">
index
</a>
<a href="/topics/semantic-search" title="Topic: semantic-search" data-view-component="true" class="topic-tag topic-tag-link">
semantic-search
</a>
<a href="/topics/browser-history" title="Topic: browser-history" data-view-component="true" class="topic-tag topic-tag-link">
browser-history
</a>
<a href="/topics/personal-search" title="Topic: personal-search" data-view-component="true" class="topic-tag topic-tag-link">
personal-search
</a>
<a href="/topics/personal-search-engine" title="Topic: personal-search-engine" data-view-component="true" class="topic-tag topic-tag-link">
personal-search-engine
</a>
<a href="/topics/mcp-server" title="Topic: mcp-server" data-view-component="true" class="topic-tag topic-tag-link">
mcp-server
</a>
</div>
</div>

<ul class="list-style-none">
<li class="d-inline">
<a class="d-inline-flex flex-items-center flex-nowrap Link--secondary no-underline text-small tmp-mr-3" href="/asciimoo/hister/search?l=go" data-ga-click="Repository, language stats search click, location:repo overview">
<svg style="color:#00ADD8;" aria-hidden="true" data-component="Octicon" height="16" viewBox="0 0 16 16" version="1.1" width="16" data-view-component="true" class="octicon octicon-dot-fill mr-2 tmp-mr-2">
<path d="M8 4a4 4 0 1 1 0 8 4 4 0 0 1 0-8Z"></path>
</svg>
<span class="color-fg-default text-bold mr-1">Go</span>
<span>60.0%</span>
</a>
</li>
<li class="d-inline">
<a class="d-inline-flex flex-items-center flex-nowrap Link--secondary no-underline text-small tmp-mr-3" href="/asciimoo/hister/search?l=svelte" data-ga-click="Repository, language stats search click, location:repo overview">
<svg style="color:#ff3e00;" aria-hidden="true" data-component="Octicon" height="16" viewBox="0 0 16 16" version="1.1" width="16" data-view-component="true" class="octicon octicon-dot-fill mr-2 tmp-mr-2">
<path d="M8 4a4 4 0 1 1 0 8 4 4 0 0 1 0-8Z"></path>
</svg>
<span class="color-fg-default text-bold mr-1">Svelte</span>
<span>30.9%</span>
</a>
</li>
<li class="d-inline">
<a class="d-inline-flex flex-items-center flex-nowrap Link--secondary no-underline text-small tmp-mr-3" href="/asciimoo/hister/search?l=typescript" data-ga-click="Repository, language stats search click, location:repo overview">
<svg style="color:#3178c6;" aria-hidden="true" data-component="Octicon" height="16" viewBox="0 0 16 16" version="1.1" width="16" data-view-component="true" class="octicon octicon-dot-fill mr-2 tmp-mr-2">
<path d="M8 4a4 4 0 1 1 0 8 4 4 0 0 1 0-8Z"></path>
</svg>
<span class="color-fg-default text-bold mr-1">TypeScript</span>
<span>5.4%</span>
</a>
</li>
<li class="d-inline">
<a class="d-inline-flex flex-items-center flex-nowrap Link--secondary no-underline text-small tmp-mr-3" href="/asciimoo/hister/search?l=shell" data-ga-click="Repository, language stats search click, location:repo overview">
<svg style="color:#89e051;" aria-hidden="true" data-component="Octicon" height="16" viewBox="0 0 16 16" version="1.1" width="16" data-view-component="true" class="octicon octicon-dot-fill mr-2 tmp-mr-2">
<path d="M8 4a4 4 0 1 1 0 8 4 4 0 0 1 0-8Z"></path>
</svg>
<span class="color-fg-default text-bold mr-1">Shell</span>
<span>1.1%</span>
</a>
</li>
<li class="d-inline">
<a class="d-inline-flex flex-items-center flex-nowrap Link--secondary no-underline text-small tmp-mr-3" href="/asciimoo/hister/search?l=css" data-ga-click="Repository, language stats search click, location:repo overview">
<svg style="color:#663399;" aria-hidden="true" data-component="Octicon" height="16" viewBox="0 0 16 16" version="1.1" width="16" data-view-component="true" class="octicon octicon-dot-fill mr-2 tmp-mr-2">
<path d="M8 4a4 4 0 1 1 0 8 4 4 0 0 1 0-8Z"></path>
</svg>
<span class="color-fg-default text-bold mr-1">CSS</span>
<span>1.1%</span>
</a>
</li>
<li class="d-inline">
<a class="d-inline-flex flex-items-center flex-nowrap Link--secondary no-underline text-small tmp-mr-3" href="/asciimoo/hister/search?l=nix" data-ga-click="Repository, language stats search click, location:repo overview">
<svg style="color:#7e7eff;" aria-hidden="true" data-component="Octicon" height="16" viewBox="0 0 16 16" version="1.1" width="16" data-view-component="true" class="octicon octicon-dot-fill mr-2 tmp-mr-2">
<path d="M8 4a4 4 0 1 1 0 8 4 4 0 0 1 0-8Z"></path>
</svg>
<span class="color-fg-default text-bold mr-1">Nix</span>
<span>1.0%</span>
</a>
</li>
<li class="d-inline">
<span class="d-inline-flex flex-items-center flex-nowrap text-small tmp-mr-3">
<svg style="color:#ededed;" aria-hidden="true" data-component="Octicon" height="16" viewBox="0 0 16 16" version="1.1" width="16" data-view-component="true" class="octicon octicon-dot-fill mr-2 tmp-mr-2">
<path d="M8 4a4 4 0 1 1 0 8 4 4 0 0 1 0-8Z"></path>
</svg>
<span class="color-fg-default text-bold mr-1">Other</span>
<span>0.5%</span>
</span>
</li>
</ul>
<react-app app-name="code-view" initial-path="/asciimoo/hister">
  <script type="application/json" data-target="react-app.embeddedData">{"payload":{"codeViewRepoRoute":{"path":"/","refInfo":{"name":"master","listCacheKey":"v0:1782301410.0","canEdit":true,"refType":"branch","currentOid":"6a2fdecb5b7d20f1b214fdfa8bc9bf1dcebab86e"},"tree":{"items":[{"name":".forgejo/workflows","path":".forgejo/workflows","contentType":"directory","hasSimplifiedPath":true}],"totalCount":37,"templateDirectorySuggestionUrl":null,"readme":null,"showBranchInfobar":false},"overview":{"overviewFiles":[{"displayName":"README.md","repoName":"hister","refName":"master","path":"README.md","preferredFileType":"readme","tabName":"README","richText":"<article class=\"markdown-body entry-content container-lg\" itemprop=\"text\"><div class=\"markdown-heading\" dir=\"auto\"><h1 tabindex=\"-1\" class=\"heading-element\" dir=\"auto\">Hister</h1><a id=\"user-content-hister\" class=\"anchor\" aria-label=\"Permalink: Hister\" href=\"#hister\"><svg data-component=\"Octicon\" class=\"octicon octicon-link\" viewBox=\"0 0 16 16\" version=\"1.1\" width=\"16\" height=\"16\" aria-hidden=\"true\"><path d=\"m7.775 3.275 1.25-1.25a3.5 3.5 0 1 1 4.95 4.95l-2.5 2.5a3.5 3.5 0 0 1-4.95 0 .751.751 0 0 1 .018-1.042.751.751 0 0 1 1.042-.018 1.998 1.998 0 0 0 2.83 0l2.5-2.5a2.002 2.002 0 0 0-2.83-2.83l-1.25 1.25a.751.751 0 0 1-1.042-.018.751.751 0 0 1-.018-1.042Zm-4.69 9.64a1.998 1.998 0 0 0 2.83 0l1.25-1.25a.751.751 0 0 1 1.042.018.751.751 0 0 1 .018 1.042l-1.25 1.25a3.5 3.5 0 1 1-4.95-4.95l2.5-2.5a3.5 3.5 0 0 1 4.95 0 .751.751 0 0 1-.018 1.042.751.751 0 0 1-1.042.018 1.998 1.998 0 0 0-2.83 0l-2.5 2.5a1.998 1.998 0 0 0 0 2.83Z\"></path></svg></a></div>\n<p dir=\"auto\"><strong>Your own search engine</strong></p>\n<p dir=\"auto\">Hister is a general purpose web search engine providing automatic full-text indexing for visited websites.</p>\n<div class=\"markdown-heading\" dir=\"auto\"><h2 tabindex=\"-1\" class=\"heading-element\" dir=\"auto\">Features</h2><a id=\"user-content-features\" class=\"anchor\" aria-label=\"Permalink: Features\" href=\"#features\"><svg data-component=\"Octicon\" class=\"octicon octicon-link\" viewBox=\"0 0 16 16\" version=\"1.1\" width=\"16\" height=\"16\" aria-hidden=\"true\"><path d=\"m7.775 3.275 1.25-1.25a3.5 3.5 0 1 1 4.95 4.95l-2.5 2.5a3.5 3.5 0 0 1-4.95 0 .751.751 0 0 1 .018-1.042.751.751 0 0 1 1.042-.018 1.998 1.998 0 0 0 2.83 0l2.5-2.5a2.002 2.002 0 0 0-2.83-2.83l-1.25 1.25a.751.751 0 0 1-1.042-.018.751.751 0 0 1-.018-1.042Zm-4.69 9.64a1.998 1.998 0 0 0 2.83 0l1.25-1.25a.751.751 0 0 1 1.042.018.751.751 0 0 1 .018 1.042l-1.25 1.25a3.5 3.5 0 1 1-4.95-4.95l2.5-2.5a3.5 3.5 0 0 1 4.95 0 .751.751 0 0 1-.018 1.042.751.751 0 0 1-1.042.018 1.998 1.998 0 0 0-2.83 0l-2.5 2.5a1.998 1.998 0 0 0 0 2.83Z\"></path></svg></a></div>\n<ul dir=\"auto\">\n<li><strong>Privacy-focused</strong>: Keep your browsing history indexed locally - don't use remote search engines if it isn't necessary</li>\n<li><strong>Full-text indexing</strong>: Search through the actual content of web pages you've visited</li>\n<li><strong>Advanced search capabilities</strong>: Utilize a powerful <a href=\"https://hister.org/docs/query-language\" rel=\"nofollow\">query language</a> for precise results</li>\n<li><strong>Local file indexing</strong>: Index your local knowledge base</li>\n<li><strong>Crawler</strong>: Use a (headless) browser or a traditional crawler to extend your index fast</li>\n<li><strong>Multi-user support</li></ul></article>"}]}}}}</script>
</body>`

func TestExtractRepo(t *testing.T) {
	d := &document.Document{
		URL:  "https://github.com/asciimoo/hister",
		HTML: minimalRepoPage,
	}
	e := &GitHubExtractor{}
	state, err := e.Extract(d)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if state != types.ExtractorStop {
		t.Fatalf("state = %v, want Stop", state)
	}
	if d.Title != "asciimoo/hister: Your own search engine" {
		t.Errorf("Title = %q, want %q", d.Title, "asciimoo/hister: Your own search engine")
	}
	// Text checks.
	if !strings.Contains(d.Text, "description: Your own search engine") {
		t.Error("Text should contain description")
	}
	if !strings.Contains(d.Text, "topics: search, go, golang") {
		t.Error("Text should contain topics")
	}
	if !strings.Contains(d.Text, "languages: Go, Svelte, TypeScript") {
		t.Error("Text should contain languages")
	}
	if !strings.Contains(d.Text, "Multi-user support") {
		t.Error("Text should contain README")
	}
	if !strings.Contains(d.Text, "stars: 1255") {
		t.Error("Text should contain stars")
	}

	// Metadata checks.
	if d.Metadata["repo"] != "asciimoo/hister" {
		t.Errorf("Metadata[repo] = %v, want asciimoo/hister", d.Metadata["repo"])
	}
	if d.Metadata["description"] != "Your own search engine" {
		t.Errorf("Metadata[description] = %v, want Your own search engine", d.Metadata["description"])
	}
	if d.Metadata["topics"] != "search, go, golang, search-engine, privacy, web, mcp, history, index, semantic-search, browser-history, personal-search, personal-search-engine, mcp-server" {
		t.Errorf("Metadata[topics] = %q", d.Metadata["topics"])
	}
	if d.Metadata["languages"] != "Go, Svelte, TypeScript, Shell, CSS, Nix" {
		t.Errorf("Metadata[languages] = %q", d.Metadata["languages"])
	}
}

func TestPreviewRepo(t *testing.T) {
	d := &document.Document{
		URL:  "https://github.com/asciimoo/hister",
		HTML: minimalRepoPage,
	}
	e := &GitHubExtractor{}
	response, state, err := e.Preview(d)
	c := response.Content
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if state != types.ExtractorStop {
		t.Fatalf("state = %v, want Stop", state)
	}
	if c == "" {
		t.Fatalf("content is empty")
	}
	if !strings.Contains(c, `<p class="gh-description">Your own search engine</p>`) {
		t.Error("Preview should contain the description")
	}
	if !strings.Contains(c, "<h1>Hister</h1>") {
		t.Error("Preview should contain the README, properly formatted")
	}
	if !strings.Contains(c, "Hister is a general purpose web search engine providing automatic full-text indexing for visited websites") {
		t.Error("Preview should contain the README body")
	}
	if !strings.Contains(c, `<p class="gh-stats">`) {
		t.Errorf("Preview should show the stats nicely")
	}
}

// --- Issue ---------------------------------------------------------------
const issuePage = `<html>
<head><title>Extractors wanted! · Issue #305 · asciimoo/hister</title></head>
<body>
<div>
<div data-component="TitleArea" data-size-variant="medium">
  <h1 data-component="PH_Title" data-hidden="false">
  <bdi data-testid="issue-title">Extractors wanted!</bdi>
  </h1>
</div>
<div>
  <div data-testid="issue-viewer-issue-container">
	<div data-testid="issue-body">
	  <div>
		<h2>Description</h2>
		<a data-component="Link" href="https://github.com/asciimoo/hister/issues/305#issue-4230456940" data-testid="issue-body-header-link">
		<relative-time class="IssueBodyHeader-module__RelativeTime__xv0lw" datetime="2026-04-09T07:47:32.000Z" title="Apr 9, 2026, 09:47 GMT+2">on Apr 9, 2026</relative-time>
		</a>
	  </div>
      <div id="issue-body-viewer">
		<div data-testid="markdown-body">
		  <h1 dir="auto">This is a meta issue raising awareness to contribute to existing extractors or add new ones</h1>
		  <p dir="auto">Extractors are modules that provide custom, content-specific document parsing or rendering functions to enhance the data quality of Hister. [...]</p>
		  <ul><li><div><div><div><div><div data-testid="tasklist-item-0-0"><div><div></div><div id="checkbox-item-0"><input disabled="" aria-required="false" aria-invalid="false" aria-label="Video information extractor using yt-dlp (@FlameFlag) checklist item" data-component="Checkbox" type="checkbox" checked="" aria-checked="true"><div> Video information extractor using<code>yt-dlp</code> (@FlameFlag)</div></div></div><div></div></div></div></div><div><div><div data-testid="tasklist-item-0-1"><div><div></div><div id="checkbox-item-1"><input disabled="" aria-required="false" aria-invalid="false" aria-label="GitHub project information extractor checklist item" data-component="Checkbox" type="checkbox" aria-checked="false"><div> GitHub project information extractor</div></div></div><div></div></div></div></div><div><div><div data-testid="tasklist-item-0-2"><div><div></div><div id="checkbox-item-2"><input disabled="" aria-required="false" aria-invalid="false" aria-label="General StackExchange question/answer extractor checklist item" data-component="Checkbox" type="checkbox" checked="" aria-checked="true"><div> General StackExchange question/answer extractor</div></div></div><div></div></div></div></div><div><div><div data-testid="tasklist-item-0-3"><div><div></div><div id="checkbox-item-3"><input disabled="" aria-required="false" aria-invalid="false" aria-label="Reddit post extractor (@dinzz005) checklist item" data-component="Checkbox" type="checkbox" aria-checked="false"><div> Reddit post extractor (<a data-hovercard-type="user" data-hovercard-url="/users/dinzz005/hovercard" data-octo-click="hovercard-link-click" data-octo-dimensions="link_type:self" href="https://github.com/dinzz005" aria-keyshortcuts="Alt+ArrowUp">@dinzz005</a>)</div></div></div><div></div></div></div></div></div></div></li></ul>
		</div>
      </div>
	</div>
  </div>
  <div data-testid="issue-viewer-comments-container">
	<div data-testid="markdown-body">
      <p dir="auto">hey bruhh i like to work on reddit post extractor...</p>
	</div>
	<div data-testid="markdown-body">
	  <p dir="auto">Thanks bro, is there any deadline for this ???</p>
	</div>
	<div data-testid="markdown-body"
	  <p dir="auto"><a class="user-mention notranslate" data-hovercard-type="user" data-hovercard-url="/users/dinzz005/hovercard" data-octo-click="hovercard-link-click" data-octo-dimensions="link_type:self" href="https://github.com/dinzz005" aria-keyshortcuts="Alt+ArrowUp">@dinzz005</a> No deadlines</p>
	</div>
  </div>
</div>
</body>
</html>`

func TestExtractIssuePage(t *testing.T) {
	d := &document.Document{
		URL:  "https://github.com/asciimoo/hister/issues/305",
		HTML: issuePage,
	}
	e := &GitHubExtractor{}
	state, err := e.Extract(d)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	if state != types.ExtractorStop {
		t.Fatalf("state = %v, want Stop", state)
	}
	if d.Title != "Extractors wanted! · Issue #305 · asciimoo/hister" {
		t.Errorf("Title = %q, want %q", d.Title, "Extractors wanted! · Issue #305 · asciimoo/hister")
	}

	// Text checks.
	if !strings.Contains(d.Text, "comments: hey bruhh i like to work on reddit post extractor..., Thanks bro, is there any deadline for this ???") {
		t.Error("Text should contain comments")
	}
	if !strings.Contains(d.Text, "This is a meta issue raising awareness to contribute to existing extractors or add new ones") || !strings.Contains(d.Text, "Extractors are modules that provide custom") {
		t.Error("Text should contain the issue body")
	}

	// Metadata checks.
	if d.Metadata["type"] != "Issue" {
		t.Errorf("Metadata[type] = %v, want Issue", d.Metadata["type"])
	}
	if d.Metadata["repo"] != "asciimoo/hister" {
		t.Errorf("Metadata[repo] = %v, want asciimoo/hister", d.Metadata["repo"])
	}
	if d.Metadata["title"] != "Extractors wanted!" {
		t.Errorf("Metadata[title] = %v, want Extractors wanted!", d.Metadata["title"])
	}
	if d.Metadata["date"] != "2026-04-09T07:47:32.000Z" {
		t.Errorf("Metadata[date] = %v, want 2026-04-09T07:47:32.000Z", d.Metadata["date"])
	}
}

func TestPreviewIssue(t *testing.T) {
	d := &document.Document{
		URL:  "https://github.com/asciimoo/hister/issues/305",
		HTML: issuePage,
	}
	e := &GitHubExtractor{}
	response, state, err := e.Preview(d)
	c := response.Content
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if state != types.ExtractorStop {
		t.Fatalf("state = %v, want Stop", state)
	}
	if c == "" {
		t.Fatalf("content is empty")
	}
	if !strings.Contains(c, `<h1>Extractors wanted!</h1>`) {
		t.Error("Preview should contain the issue title")
	}
	if !strings.Contains(c, `This is a meta issue raising awareness to contribute to existing extractors or add new ones`) {
		t.Error("Preview should contain the issue body, if it exists")
	}
	if !strings.Contains(c, `General StackExchange question/answer extractor`) {
		t.Error("Preview should contain the issue body, if it exists")
	}
	if !strings.Contains(c, `Thanks bro, is there any deadline for this ???`) {
		t.Error("Preview should contain the issue comments")
	}
}

const issuesPage = `<html>
<head><title>Issues · asciimoo/hister</title></head>
<body>
<div>
  <div>
	<ul aria-label="Drag and drop pinned issues list.">
	  <li><div>
        <div><a href="https://github.com/asciimoo/hister/issues/305"><span>Extractors wanted!</span></a></div>
        <div><span>#305 · <a href="https://github.com/asciimoo">asciimoo</a><span>opened</span><relative-time data-component="RelativeTime" datetime="2026-04-09T07:47:32.000Z" title="Apr 9, 2026, 9:47 AM GMT+2">on Apr 9, 2026</relative-time></span></div>
      </div></li>
	  <li><div>
        <div><a href="https://github.com/asciimoo/hister/issues/416"><span>Project logo, mascot design</span></a></div>
        <div><span>#416 · <a href="https://github.com/asciimoo">asciimoo</a><span>opened</span><relative-time data-component="RelativeTime" datetime="2026-05-18T10:01:27.000Z" title="May 18, 2026, 12:01 PM GMT+2">on May 18, 2026</relative-time></span></div>
      </div></li>
	</ul>
  </div>
  <div>
	<h2 data-component="Heading" id="_r_d_-list-view-container-title">Search results</h2>
    <ul aria-labelledby="_r_d_-list-view-container-title" data-listview-component="items-list">
	  <li>
		<div><h3><a href="/asciimoo/hister/issues/500"><span>Feature: Import bookmarks from Linkding, Linkwarden, and Archive.org</span></a></h3></div>
		<div><div><span><span>#500</span> <span class="sr-only">In asciimoo/hister;</span></span><div><span>· </span><span></span><a href="/asciimoo/hister/issues?q=is%3Aissue%20state%3Aopen%20author%3Aavinashkanaujiya">avinashkanaujiya</a> <span> opened </span><relative-time data-component="RelativeTime" datetime="2026-06-29T00:48:52.000Z" title="Jun 29, 2026, 2:48 AM GMT+2">on Jun 29, 2026</relative-time></div></div></div>
      </li>
	  <li>
		<div><h3><a href="/asciimoo/hister/issues/493"><span>Limit parallel jobs</span></a></h3></div>
		<div><div><span><span>#493</span> <span class="sr-only">In asciimoo/hister;</span></span><div><span>· </span><span></span><a href="/asciimoo/hister/issues?q=is%3Aissue%20state%3Aopen%20author%3Anadir-ishiguro">nadir-ishiguro</a> <span> opened </span><relative-time data-component="RelativeTime" datetime="2026-06-25T11:13:35.000Z" title="Jun 25, 2026, 1:13 PM GMT+2">on Jun 25, 2026</relative-time></div></div></div>
      </li>
    </ul>
  </div>
</div>
</body>
</html>`

func TestExtractIssuesPage(t *testing.T) {
	d := &document.Document{
		URL:  "https://github.com/asciimoo/hister/issues",
		HTML: issuesPage,
	}
	e := &GitHubExtractor{}
	state, err := e.Extract(d)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	if state != types.ExtractorStop {
		t.Fatalf("state = %v, want Stop", state)
	}
	if d.Title != "Issues · asciimoo/hister" {
		t.Errorf("Title = %q, want %q", d.Title, "Extractors wanted! · Issues · asciimoo/hister")
	}

	// Text checks.
	if !strings.Contains(d.Text, "Extractors wanted") || !strings.Contains(d.Text, "Project logo, mascot design") {
		t.Error("Text should contain pinned issues")
	}
	if !strings.Contains(d.Text, "#305") || !strings.Contains(d.Text, "asciimoo") {
		t.Error("Text should contain the issue description of pinned issues")
	}
	if !strings.Contains(d.Text, "Feature: Import bookmarks from Linkding, Linkwarden, and Archive.org") || !strings.Contains(d.Text, "Limit parallel jobs") {
		t.Error("Text should contain the list of issues")
	}
	if !strings.Contains(d.Text, "#500") || !strings.Contains(d.Text, "avinashkanaujiya") {
		t.Error("Text should contain the issue description of listed issues")
	}

	// Metadata checks.
	if d.Metadata["type"] != "Issues" {
		t.Errorf("Metadata[type] = %v, want Issues", d.Metadata["type"])
	}
	if d.Metadata["repo"] != "asciimoo/hister" {
		t.Errorf("Metadata[repo] = %v, want asciimoo/hister", d.Metadata["repo"])
	}
}

// --- Pull Requests -------------------------------------------------------
const prPage = `<html>
<head><title>Improve GitHub extractor by RensOliemans · Pull Request #495 · asciimoo/hister · GitHub</title></head>
<body>
<div id="repo-content-pjax-container">
<react-app app-name="pull-requests">
<div data-target="react-app.reactRoot">

<header>
  <div data-component="TitleArea">
    <h1 data-component="PH_Title" data-hidden="false">
      <span class="markdown-title">Improve GitHub extractor</span>
      <span><span>#495</span></span>
    </h1>
  </div>
  <div data-component="PageHeader.Description">
    <span data-size="medium" data-status="pullOpened"><svg aria-label="Pull request"></svg>Open</span>
    <div>
      <a data-component="Link" data-muted="true" data-inline="true" href="/RensOliemans">RensOliemans</a> wants to merge 9 commits into
      <div>
        <a href="/asciimoo/hister/tree/master" data-component="BranchName">asciimoo:master</a>
        <span>from </span>
        <a href="/RensOliemans/hister/tree/github-extractor" data-component="BranchName">RensOliemans:github-extractor</a>
      </div>
    </div>
  </div>
</header>

<div>
  <rails-partial data-partial-name="pullRequestsConversationsRoute.Body">
    <div class="TimelineItem TimelineItem--condensed pt-0 js-comment-container js-command-palette-pull-body" data-gid="PR_kwDOQzaUZ87qtsb6">
      <div id="pullrequest-3937847034">
        <div>
          <h3>
            <strong><a href="/RensOliemans">RensOliemans</a></strong>
            commented
            <a href="#issue-4746857605" id="issue-4746857605-permalink"><relative-time datetime="2026-06-25T19:35:45Z">Jun 25, 2026</relative-time></a>
          </h3>
        </div>
        <div>
          <task-lists disabled sortable>
            <div class="comment-body markdown-body js-comment-body">
              <p dir="auto">From <a href="https://github.com/asciimoo/hister/issues/305">#305</a> , I started with a stab at improving the GitHub extractor. The PR is still very much WIP, and I thought I'd create it now because I wanted to ask this question: do you want me to split <code>github.go</code> into separate files? Like <code>issue.go</code> for parsing the <code>/owner/repo/issues/:id</code> url, etc.</p>
              <p dir="auto">I actually have two other questions about the existing code (I'm quite new to Go):</p>
              <ol dir="auto">
                <li>What is the purpose of <code>Preview</code>? Should this be unique for each type of URL as well?</li>
                <li>What was the purpose of the <code>b string.Builder</code>? Should I keep it, or use only metadata?</li>
              </ol>
            </div>
          </task-lists>
        </div>
      </div>
    </div>
  </rails-partial>

  <rails-partial data-partial-name="pullRequestsConversationsRoute.Timeline">
    <div class="TimelineItem js-comment-container" data-gid="IC_kwDOQzaUZ88AAAABHlW0fQ">
      <div class="timeline-comment-group" id="issuecomment-4803900541">
        <div class="timeline-comment-header">
          <h3><strong><a href="/asciimoo">asciimoo</a></strong> commented
            <a href="#issuecomment-4803900541" id="issuecomment-4803900541-permalink"><relative-time datetime="2026-06-25T20:32:09Z">Jun 25, 2026</relative-time></a>
          </h3>
          <div><span>Owner</span></div>
        </div>
        <div><task-lists disabled sortable>
          <table><tbody><tr>
            <td class="d-block comment-body markdown-body  js-comment-body">
              <p dir="auto">Nice!</p>
              <blockquote><p dir="auto">do you want me to split github.go into separate files?</p></blockquote>
              <p dir="auto">It's up to you. Having long files is idiomatic in Go if there is no real logical separation that would make sense (e.g. our <code>server.go</code>). Perhaps in this case it makes sense to put different handlers to different files, but it isn't necessary.</p>
            </td>
          </tr></tbody></table>
        </task-lists></div>
      </div>
    </div>

    <div class="TimelineItem js-comment-container" data-gid="IC_kwDOQzaUZ88AAAABHpbNCg">
      <div class="timeline-comment-group" id="issuecomment-4808166666">
        <div class="timeline-comment-header">
          <h3><strong><a href="/asciimoo">asciimoo</a></strong> commented
            <a href="#issuecomment-4808166666" id="issuecomment-4808166666-permalink"><relative-time datetime="2026-06-26T09:12:01Z">Jun 26, 2026</relative-time></a>
          </h3>
          <div><span>Owner</span></div>
        </div>
        <div><task-lists disabled sortable>
          <table><tbody><tr>
            <td class="d-block comment-body markdown-body  js-comment-body">
              <p dir="auto">Let me know if I can start the review.</p>
            </td>
          </tr></tbody></table>
        </task-lists></div>
      </div>
    </div>

    <div class="TimelineItem js-comment-container" data-gid="IC_kwDOQzaUZ88AAAABHqy1Aw">
      <div class="timeline-comment-group" id="issuecomment-4809602307">
        <div class="timeline-comment-header">
          <h3><strong><a href="/RensOliemans">RensOliemans</a></strong> commented
            <a href="#issuecomment-4809602307" id="issuecomment-4809602307-permalink"><relative-time datetime="2026-06-26T12:33:28Z">Jun 26, 2026</relative-time></a>
          </h3>
          <div><span>Author</span></div>
        </div>
        <div><task-lists disabled sortable>
          <table><tbody><tr>
            <td class="d-block comment-body markdown-body  js-comment-body">
              <p dir="auto">Thanks for the info. I'll let you know when I'm happy with it!</p>
            </td>
          </tr></tbody></table>
        </task-lists></div>
      </div>
    </div>
  </rails-partial>
</div>

</div>
</react-app>
</div>
</body>
</html>`

func TestExtractPullRequestPage(t *testing.T) {
	d := &document.Document{
		URL:  "https://github.com/asciimoo/hister/pull/495",
		HTML: prPage,
	}
	e := &GitHubExtractor{}
	state, err := e.Extract(d)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	if state != types.ExtractorStop {
		t.Fatalf("state = %v, want Stop", state)
	}
	wantTitle := "Improve GitHub extractor by RensOliemans · Pull Request #495 · asciimoo/hister · GitHub"
	if d.Title != wantTitle {
		t.Errorf("Title = %q, want %q", d.Title, wantTitle)
	}

	// Text checks.
	if !strings.Contains(d.Text, "From #305") || !strings.Contains(d.Text, "do you want me to split") {
		t.Error("Text should contain the PR body")
	}
	for _, c := range []string{
		"Nice!",
		"Let me know if I can start the review.",
		"Thanks for the info. I'll let you know when I'm happy with it!",
	} {
		if !strings.Contains(d.Text, c) {
			t.Errorf("Text should contain comment %q", c)
		}
	}

	// Metadata checks.
	if d.Metadata["type"] != "PullRequest" {
		t.Errorf("Metadata[type] = %v, want PullRequest", d.Metadata["type"])
	}
	if d.Metadata["repo"] != "asciimoo/hister" {
		t.Errorf("Metadata[repo] = %v, want asciimoo/hister", d.Metadata["repo"])
	}
	if d.Metadata["title"] != "Improve GitHub extractor" {
		t.Errorf("Metadata[title] = %v, want Improve GitHub extractor", d.Metadata["title"])
	}
	if d.Metadata["state"] != "Open" {
		t.Errorf("Metadata[state] = %v, want Open", d.Metadata["state"])
	}
	if d.Metadata["date"] != "2026-06-25T19:35:45Z" {
		t.Errorf("Metadata[date] = %v, want 2026-06-25T19:35:45Z", d.Metadata["date"])
	}
}
