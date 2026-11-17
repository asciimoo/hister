// SPDX-License-Identifier: AGPL-3.0-or-later

package duckduckgo

import (
	"strings"
	"testing"

	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/extractor/sdk"
	"github.com/asciimoo/hister/server/extractor/serputil"
)

const htmlSearchURL = "https://html.duckduckgo.com/html/?q=hister+search"

const htmlResultPage = `<!doctype html>
<html>
<head><title>hister search at DuckDuckGo</title></head>
<body>
  <form><input name="q" value="hister search"></form>
  <div id="links" class="results">
    <div class="result results_links web-result">
      <div class="result__body">
        <h2 class="result__title">
          <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgithub.com%2Fasciimoo%2Fhister&amp;rut=abc">GitHub - asciimoo/hister</a>
        </h2>
        <a class="result__snippet">A private search engine for your own web history.</a>
        <a class="result__url">github.com/asciimoo/hister</a>
      </div>
    </div>
    <div class="result results_links web-result">
      <div class="result__body">
        <h2 class="result__title">
          <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgithub.com%2Fasciimoo%2Fhister&amp;rut=def">GitHub - asciimoo/hister</a>
        </h2>
        <a class="result__snippet">Duplicate result.</a>
      </div>
    </div>
    <div class="result results_links web-result result--ad">
      <div class="result__body">
        <span class="result__badge">Ad</span>
        <h2 class="result__title"><a class="result__a" href="https://duckduckgo.com/y.js?ad_domain=ads.example">Sponsored thing</a></h2>
      </div>
    </div>
    <div class="result results_links web-result">
      <div class="result__body">
        <h2 class="result__title"><a class="result__a" href="https://duckduckgo.com/y.js?ad_domain=other.example">Unmarked promotion</a></h2>
      </div>
    </div>
  </div>
</body>
</html>`

const modernResultPage = `<!doctype html>
<html>
<head><title>web scraping at DuckDuckGo</title></head>
<body>
  <input name="q" value="web scraping">
  <ol>
    <li>
      <article data-testid="result">
        <h2><a data-testid="result-title-a" href="https://en.wikipedia.org/wiki/Web_scraping">Web scraping - Wikipedia</a></h2>
        <div data-result="snippet">Web scraping is the collection of data from the web.</div>
        <a data-testid="result-extras-url-link" href="https://en.wikipedia.org/wiki/Web_scraping">en.wikipedia.org › wiki › Web_scraping</a>
      </article>
    </li>
    <li>
      <article data-testid="result">
        <span data-testid="result-badge">Ad</span>
        <h2><a data-testid="result-title-a" href="https://ads.example.com/x">Buy things</a></h2>
      </article>
    </li>
  </ol>
</body>
</html>`

// reactResultPage mirrors the markup of the JavaScript front end as rendered by
// a browser: the results end up in an ordered list of articles, the title anchor
// holds the target URL directly, and DuckDuckGo's own "search this site" link
// sits next to the displayed URL.
const reactResultPage = `<!doctype html>
<html class="js full-urls breadcrumb-urls react">
<head><title>hister at DuckDuckGo</title></head>
<body>
  <div data-testid="mainline" data-area="mainline">
    <ol class="react-results--main">
      <li data-layout="organic">
        <article id="r1-0" data-testid="result" data-nrn="result">
          <div>
            <a href="/?q=hister+site:hister.org" data-testid="result-extras-site-search-link">Search domain hister.org</a>
            <a href="https://hister.org/" data-testid="result-extras-url-link"><p><span>https://hister.org</span></p></a>
          </div>
          <div>
            <h2><a href="https://hister.org/" rel="noopener" target="_self" data-testid="result-title-a"><span>Hister | Your Own Search Engine</span></a></h2>
          </div>
          <div>
            <div data-result="snippet"><div><span><span><b>Hister</b> is a private, self hosted search engine for the pages you visit and the files you keep.</span></span></div></div>
          </div>
        </article>
      </li>
      <li data-layout="organic">
        <article id="r1-1" data-testid="result" data-nrn="result">
          <div>
            <a href="https://en.wikipedia.org/wiki/Hister_(beetle)" data-testid="result-extras-url-link"><p><span>https://en.wikipedia.org &#8250; wiki &#8250; Hister_(beetle)</span></p></a>
          </div>
          <h2><a href="https://en.wikipedia.org/wiki/Hister_(beetle)" data-testid="result-title-a"><span>Hister (beetle) - Wikipedia</span></a></h2>
          <div data-result="snippet"><span>Hister is a genus of clown beetles in the family Histeridae.</span></div>
        </article>
      </li>
      <li data-layout="organic">
        <article id="r1-2" data-testid="result" data-nrn="result">
          <h2><a href="https://hister.org/" data-testid="result-title-a"><span>Hister | Your Own Search Engine</span></a></h2>
          <div data-result="snippet"><span>Duplicate of the first result.</span></div>
        </article>
      </li>
    </ol>
  </div>
</body>
</html>`

// answerPage mirrors a result page after Search Assist generated an answer. The
// answer container holds the answer paragraphs and, next to them, the source
// chips, whose labels are link text rather than answer text. The chips are
// spans, so reading the paragraphs alone keeps them out.
const answerPage = `<!doctype html>
<html class="js full-urls breadcrumb-urls react">
<head><title>what could be the best thing to happen to humanity at DuckDuckGo</title></head>
<body>
  <div data-testid="mainline" data-area="mainline">
    <ol class="react-results--main">
      <li data-layout="ai">
        <div>
          <div data-testid="duckassist-answer-content" class="dyuE6BMumAPpqiH0dQVX">
            <div>
              <div class="qcramu8merKO3n5WPVV0">
                <div class="space-y-4 whitespace-normal">
                  <p>The best thing that could happen to humanity would be a collective realization that we are <span class="font-semibold" data-streamdown="strong">caretakers of the Earth</span>, leading to efforts to preserve life and promote biodiversity.</p>
                </div>
                <span class="r9A9FbwL91fnnJKgCCsi">
                  <span class="Vfx795Sid9w2hY240SOs">
                    <span><span tabindex="0">
                      <a href="https://scienceforums.net/topic/135854/" rel="noopener" target="_self"><img src="//external-content.duckduckgo.com/ip3/scienceforums.net.ico"> <span><span><span>Science Forums</span></span></span></a>
                    </span></span>
                  </span>
                </span>
              </div>
            </div>
          </div>
          <div>
            <button data-testid="duckassist-stream-more-pill"><span>More</span></button>
          </div>
        </div>
      </li>
      <li data-layout="organic">
        <article id="r1-0" data-testid="result" data-nrn="result">
          <div>
            <a href="https://hister.org/" data-testid="result-extras-url-link"><p><span>https://hister.org</span></p></a>
          </div>
          <div>
            <h2><a href="https://hister.org/" rel="noopener" target="_self" data-testid="result-title-a"><span>Hister | Your Own Search Engine</span></a></h2>
          </div>
          <div>
            <div data-result="snippet"><div><span><span><b>Hister</b> is a private, self hosted search engine.</span></span></div></div>
          </div>
        </article>
      </li>
    </ol>
  </div>
</body>
</html>`

const answerPageURL = "https://duckduckgo.com/?q=what+could+be+the+best+thing+to+happen+to+humanity&ia=web"

const answerTextExpected = "The best thing that could happen to humanity would be a collective realization that we are caretakers of the Earth, leading to efforts to preserve life and promote biodiversity."

// shellPage mirrors the HTML DuckDuckGo serves to a client that does not run
// JavaScript: the body holds bootstrap scripts and a noscript redirect to the
// non-JavaScript site, but no result. Readability and Basic both "succeed" on
// this markup with a ten character document, which is why the result page has to
// be dropped instead of being handed to a generic extractor. The redirect target
// is disallowed by the robots.txt of this host, so a crawler cannot recover the
// results from it either.
const shellPage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>what could be the best thing to happen to humanity? at DuckDuckGo</title>
  <script src="/dist/wpm.duckassist-ia.d72a90dc5e54b2aeffa7.js"></script>
</head>
<body>
  <noscript>
    <meta http-equiv="refresh" content="0;URL=/html?q=what%20could%20be%20the%20best%20thing%20to%20happen%20to%20humanity%3F">
    <div class="msg msg--noscript">
      <p class="msg-title--noscript">You are being redirected to the non-JavaScript site.</p>
      Click <a href="/html/?q=what%20could%20be%20the%20best%20thing%20to%20happen%20to%20humanity%3F">here</a> if it doesn't happen automatically.
    </div>
  </noscript>
  <script>window.__DDG = {};</script>
</body>
</html>`

func TestMatch(t *testing.T) {
	e := &DuckDuckGoExtractor{}
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "javascript page", url: "https://duckduckgo.com/?q=hister&ia=web", want: true},
		{name: "www page", url: "https://www.duckduckgo.com/?q=hister", want: true},
		{name: "html endpoint", url: "https://html.duckduckgo.com/html/?q=hister", want: true},
		{name: "lite endpoint", url: "https://lite.duckduckgo.com/lite/?q=hister", want: true},
		{name: "noai page", url: "https://noai.duckduckgo.com/?q=hister", want: true},
		{name: "start page", url: "https://start.duckduckgo.com/?q=hister", want: true},
		{name: "settings", url: "https://duckduckgo.com/settings", want: false},
		{name: "homepage", url: "https://duckduckgo.com/", want: false},
		{name: "other path", url: "https://duckduckgo.com/about?q=hister", want: false},
		{name: "outbound redirect", url: "https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com", want: false},
		{name: "lookalike host", url: "https://notduckduckgo.com/?q=hister", want: false},
		{name: "suffix host", url: "https://duckduckgo.com.example.org/?q=hister", want: false},
		{name: "other scheme", url: "ftp://duckduckgo.com/?q=hister", want: false},
		{name: "userinfo", url: "https://user@duckduckgo.com/?q=hister", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := e.Match(&document.Document{URL: test.url}); got != test.want {
				t.Fatalf("Match(%q) = %v, want %v", test.url, got, test.want)
			}
		})
	}
}

func TestExtractHTMLEndpoint(t *testing.T) {
	e := &DuckDuckGoExtractor{}
	doc := &document.Document{URL: htmlSearchURL, HTML: htmlResultPage, UserID: 3}

	result := e.Extract(doc)
	if decision, err := result.Unpack(); decision != sdk.ExtractorSuccess {
		t.Fatalf("Extract decision = %v (%v), want success", decision, err)
	}
	if !doc.SkipIndexing {
		t.Fatal("result page is indexed, want SkipIndexing")
	}
	if len(doc.ExtraDocuments) != 1 {
		t.Fatalf("extra documents = %d, want 1", len(doc.ExtraDocuments))
	}
	extra := doc.ExtraDocuments[0]
	if extra.URL != "https://github.com/asciimoo/hister" {
		t.Errorf("URL = %q, want unwrapped redirect target", extra.URL)
	}
	if extra.Title != "GitHub - asciimoo/hister" {
		t.Errorf("title = %q", extra.Title)
	}
	if !strings.Contains(extra.Text, "A private search engine") {
		t.Errorf("text = %q, want snippet", extra.Text)
	}
	if extra.UserID != 3 {
		t.Errorf("owner = %d, want 3", extra.UserID)
	}
	if !extra.SummaryOnly {
		t.Error("result document is not marked as a summary")
	}
	if got := extra.Metadata["type"]; got != resultMetadataType {
		t.Errorf("metadata type = %v, want %q", got, resultMetadataType)
	}
	if got := extra.Metadata["query"]; got != "hister search" {
		t.Errorf("metadata query = %v", got)
	}
	if got := extra.Metadata["site"]; got != "github.com" {
		t.Errorf("metadata site = %v, want github.com", got)
	}
	if got := extra.Metadata["position"]; got != 1 {
		t.Errorf("metadata position = %v, want 1", got)
	}
}

func TestExtractModernPage(t *testing.T) {
	e := &DuckDuckGoExtractor{}
	doc := &document.Document{URL: "https://duckduckgo.com/?q=web+scraping&ia=web", HTML: modernResultPage}

	result := e.Extract(doc)
	if decision, err := result.Unpack(); decision != sdk.ExtractorSuccess {
		t.Fatalf("Extract decision = %v (%v), want success", decision, err)
	}
	if len(doc.ExtraDocuments) != 1 {
		t.Fatalf("extra documents = %d, want 1 (the ad is skipped)", len(doc.ExtraDocuments))
	}
	extra := doc.ExtraDocuments[0]
	if extra.URL != "https://en.wikipedia.org/wiki/Web_scraping" {
		t.Errorf("URL = %q", extra.URL)
	}
	if !strings.Contains(extra.Text, "collection of data from the web") {
		t.Errorf("text = %q, want snippet", extra.Text)
	}
	if got := extra.Metadata["site"]; got != "en.wikipedia.org" {
		t.Errorf("metadata site = %v, want en.wikipedia.org", got)
	}
}

func TestExtractJavaScriptFrontEnd(t *testing.T) {
	e := &DuckDuckGoExtractor{}
	doc := &document.Document{URL: "https://duckduckgo.com/?q=hister&ia=web", HTML: reactResultPage}

	result := e.Extract(doc)
	if decision, err := result.Unpack(); decision != sdk.ExtractorSuccess {
		t.Fatalf("Extract decision = %v (%v), want success", decision, err)
	}
	if !doc.SkipIndexing {
		t.Error("result page is indexed, want SkipIndexing")
	}
	if len(doc.ExtraDocuments) != 2 {
		t.Fatalf("extra documents = %d, want 2 (duplicate collapsed)", len(doc.ExtraDocuments))
	}
	first := doc.ExtraDocuments[0]
	if first.URL != "https://hister.org/" {
		t.Errorf("URL = %q, want the title link target", first.URL)
	}
	if first.Title != "Hister | Your Own Search Engine" {
		t.Errorf("title = %q", first.Title)
	}
	if !strings.Contains(first.Text, "private, self hosted search engine") {
		t.Errorf("text = %q, want snippet", first.Text)
	}
	if strings.Contains(first.Text, "Search domain") {
		t.Errorf("text = %q, contains the displayed site link", first.Text)
	}
	if got := first.Metadata["site"]; got != "hister.org" {
		t.Errorf("metadata site = %v, want hister.org", got)
	}
	if got := first.Metadata["query"]; got != "hister" {
		t.Errorf("metadata query = %v, want hister", got)
	}
	if got := first.Metadata["position"]; got != 1 {
		t.Errorf("metadata position = %v, want 1", got)
	}
	// The displayed address is a breadcrumb; it must not leak into the snippet.
	second := doc.ExtraDocuments[1]
	if second.URL != "https://en.wikipedia.org/wiki/Hister_(beetle)" {
		t.Errorf("URL = %q", second.URL)
	}
	if !strings.Contains(second.Text, "clown beetles") {
		t.Errorf("text = %q, want snippet", second.Text)
	}
	if strings.Contains(second.Text, "wiki") {
		t.Errorf("text = %q, contains the displayed breadcrumb", second.Text)
	}
	// DuckDuckGo's own "search this site" links must never become documents.
	for _, extra := range doc.ExtraDocuments {
		if strings.Contains(extra.URL, "duckduckgo.com") {
			t.Errorf("internal link indexed as a result: %q", extra.URL)
		}
	}
	if response, decision, err := e.Preview(&document.Document{URL: doc.URL, HTML: reactResultPage}).Unpack(); decision != sdk.ExtractorSuccess {
		t.Fatalf("Preview decision = %v (%v), want success", decision, err)
	} else if !strings.Contains(response.Content, `<a href="https://hister.org/">Hister | Your Own Search Engine</a>`) {
		t.Errorf("preview misses the linked title: %q", response.Content)
	}
}

func TestExtractWithoutContainersFallsBackToTitleLinks(t *testing.T) {
	e := &DuckDuckGoExtractor{}
	doc := &document.Document{
		URL: "https://html.duckduckgo.com/html/?q=example",
		HTML: `<html><body><div>
			<a class="result-link" href="https://example.org/page">Example page</a>
		</div></body></html>`,
	}
	result := e.Extract(doc)
	if decision, err := result.Unpack(); decision != sdk.ExtractorSuccess {
		t.Fatalf("Extract decision = %v (%v), want success", decision, err)
	}
	if len(doc.ExtraDocuments) != 1 {
		t.Fatalf("extra documents = %d, want 1", len(doc.ExtraDocuments))
	}
	if got := doc.ExtraDocuments[0].URL; got != "https://example.org/page" {
		t.Fatalf("URL = %q", got)
	}
}

func TestExtractWithoutResultsFallsBack(t *testing.T) {
	e := &DuckDuckGoExtractor{}
	doc := &document.Document{
		URL:  "https://duckduckgo.com/?q=hister",
		HTML: `<html><head><title>hister at DuckDuckGo</title></head><body><div id="links"></div></body></html>`,
	}
	result := e.Extract(doc)
	if decision, _ := result.Unpack(); decision != sdk.ExtractorFallback {
		t.Fatalf("Extract decision = %v, want fallback", decision)
	}
	if !doc.SkipIndexing {
		t.Error("SkipIndexing is not set, the result page would be indexed as boilerplate")
	}
	if len(doc.ExtraDocuments) != 0 {
		t.Errorf("extra documents = %d, want 0", len(doc.ExtraDocuments))
	}
	if _, decision, _ := e.Preview(doc).Unpack(); decision != sdk.ExtractorFallback {
		t.Fatalf("Preview decision = %v, want fallback", decision)
	}
}

func TestExtractJavaScriptShellIsSkipped(t *testing.T) {
	e := &DuckDuckGoExtractor{}
	doc := &document.Document{
		URL:  "https://duckduckgo.com/?q=what+could+be+the+best+thing+to+happen+to+humanity",
		HTML: shellPage,
	}
	if !e.Match(doc) {
		t.Fatal("shell page is not matched, a generic extractor would store it")
	}
	if decision, _ := e.Extract(doc).Unpack(); decision != sdk.ExtractorFallback {
		t.Fatalf("Extract decision = %v, want fallback", decision)
	}
	if !doc.SkipIndexing {
		t.Error("SkipIndexing is not set, the shell would be stored as an empty document")
	}
	if len(doc.ExtraDocuments) != 0 {
		t.Errorf("extra documents = %d, want 0", len(doc.ExtraDocuments))
	}
	if doc.Text != "" {
		t.Errorf("Text = %q, want the shell left untouched", doc.Text)
	}
	if _, decision, _ := e.Preview(doc).Unpack(); decision != sdk.ExtractorFallback {
		t.Fatalf("Preview decision = %v, want fallback", decision)
	}
}

func TestPreviewRendersResults(t *testing.T) {
	e := &DuckDuckGoExtractor{}
	doc := &document.Document{URL: htmlSearchURL, HTML: htmlResultPage}
	response, decision, err := e.Preview(doc).Unpack()
	if decision != sdk.ExtractorSuccess {
		t.Fatalf("Preview decision = %v (%v), want success", decision, err)
	}
	content := response.Content
	if !strings.Contains(content, "DuckDuckGo search: hister search") {
		t.Errorf("preview misses the query: %q", content)
	}
	if !strings.Contains(content, `<a href="https://github.com/asciimoo/hister">GitHub - asciimoo/hister</a>`) {
		t.Errorf("preview misses the linked title: %q", content)
	}
	if strings.Contains(content, "Sponsored thing") || strings.Contains(content, "Unmarked promotion") {
		t.Errorf("preview contains a promotion: %q", content)
	}
}

func TestExtractDropsAnswerByDefault(t *testing.T) {
	e := &DuckDuckGoExtractor{}
	doc := &document.Document{URL: answerPageURL, HTML: answerPage}
	if decision, err := e.Extract(doc).Unpack(); decision != sdk.ExtractorSuccess {
		t.Fatalf("Extract decision = %v (%v), want success", decision, err)
	}
	if !doc.SkipIndexing {
		t.Error("SkipIndexing is not set, the result page would be indexed")
	}
	if doc.Text != "" {
		t.Errorf("Text = %q, want the answer dropped", doc.Text)
	}
	if got := doc.Metadata["type"]; got != nil {
		t.Errorf("metadata type = %v, want no answer metadata", got)
	}
	if len(doc.ExtraDocuments) != 1 {
		t.Fatalf("extra documents = %d, want 1", len(doc.ExtraDocuments))
	}
}

func TestExtractKeepsAnswer(t *testing.T) {
	e := &DuckDuckGoExtractor{}
	if err := e.SetConfig(&sdk.Config{Enable: true, Options: map[string]any{keepAnswerOption: true}}); err != nil {
		t.Fatalf("SetConfig rejected keep_answer: %v", err)
	}
	doc := &document.Document{URL: answerPageURL, HTML: answerPage}
	if decision, err := e.Extract(doc).Unpack(); decision != sdk.ExtractorSuccess {
		t.Fatalf("Extract decision = %v (%v), want success", decision, err)
	}
	if doc.SkipIndexing {
		t.Error("SkipIndexing is set, the answer would be dropped")
	}
	if doc.Text != answerTextExpected {
		t.Errorf("Text = %q, want the answer", doc.Text)
	}
	// The source chips sit next to the answer paragraphs; their labels are link
	// text, not answer text.
	if strings.Contains(doc.Text, "Science Forums") || strings.Contains(doc.Text, "More") {
		t.Errorf("Text = %q, contains interface text", doc.Text)
	}
	if got := doc.Metadata["type"]; got != answerMetadataType {
		t.Errorf("metadata type = %v, want %s", got, answerMetadataType)
	}
	if got := doc.Metadata["query"]; got != "what could be the best thing to happen to humanity" {
		t.Errorf("metadata query = %v", got)
	}
	// The results are indexed either way.
	if len(doc.ExtraDocuments) != 1 {
		t.Fatalf("extra documents = %d, want 1", len(doc.ExtraDocuments))
	}
	if got := doc.ExtraDocuments[0].URL; got != "https://hister.org/" {
		t.Errorf("extra URL = %q", got)
	}
}

func TestPreviewRendersAnswer(t *testing.T) {
	e := &DuckDuckGoExtractor{}
	if err := e.SetConfig(&sdk.Config{Enable: true, Options: map[string]any{keepAnswerOption: true}}); err != nil {
		t.Fatalf("SetConfig rejected keep_answer: %v", err)
	}
	response, decision, err := e.Preview(&document.Document{URL: answerPageURL, HTML: answerPage}).Unpack()
	if decision != sdk.ExtractorSuccess {
		t.Fatalf("Preview decision = %v (%v), want success", decision, err)
	}
	content := response.Content
	if !strings.Contains(content, "caretakers of the Earth") {
		t.Errorf("preview misses the answer: %q", content)
	}
	if !strings.Contains(content, `<a href="https://hister.org/">Hister | Your Own Search Engine</a>`) {
		t.Errorf("preview misses the linked title: %q", content)
	}
	if strings.Contains(content, "Science Forums") {
		t.Errorf("preview contains a source chip: %q", content)
	}
	if strings.Index(content, "caretakers of the Earth") > strings.Index(content, "<ol>") {
		t.Errorf("preview renders the answer below the results: %q", content)
	}
}

func TestExtractKeepsAnswerWithoutResults(t *testing.T) {
	e := &DuckDuckGoExtractor{}
	if err := e.SetConfig(&sdk.Config{Enable: true, Options: map[string]any{keepAnswerOption: true}}); err != nil {
		t.Fatalf("SetConfig rejected keep_answer: %v", err)
	}
	// An answer page can carry no organic result; the answer is then the only
	// content worth storing.
	doc := &document.Document{
		URL:  answerPageURL,
		HTML: `<html><body><div data-testid="duckassist-answer-content"><p>An answer on its own.</p></div></body></html>`,
	}
	if decision, err := e.Extract(doc).Unpack(); decision != sdk.ExtractorSuccess {
		t.Fatalf("Extract decision = %v (%v), want success", decision, err)
	}
	if doc.SkipIndexing {
		t.Error("SkipIndexing is set, the answer would be dropped")
	}
	if doc.Text != "An answer on its own." {
		t.Errorf("Text = %q", doc.Text)
	}
	if len(doc.ExtraDocuments) != 0 {
		t.Errorf("extra documents = %d, want 0", len(doc.ExtraDocuments))
	}
}

func TestExtractSkipsPageWithoutAnswerOrResults(t *testing.T) {
	e := &DuckDuckGoExtractor{}
	if err := e.SetConfig(&sdk.Config{Enable: true, Options: map[string]any{keepAnswerOption: true}}); err != nil {
		t.Fatalf("SetConfig rejected keep_answer: %v", err)
	}
	// keep_answer must not keep the interface text of a page that has neither
	// an answer nor a result.
	doc := &document.Document{
		URL:  "https://duckduckgo.com/?q=hister",
		HTML: `<html><head><title>hister at DuckDuckGo</title></head><body><div>Search Settings</div></body></html>`,
	}
	if decision, _ := e.Extract(doc).Unpack(); decision != sdk.ExtractorFallback {
		t.Fatalf("Extract decision = %v, want fallback", decision)
	}
	if !doc.SkipIndexing {
		t.Error("SkipIndexing is not set, the interface text would be indexed")
	}
	if doc.Text != "" {
		t.Errorf("Text = %q, want the page left untouched", doc.Text)
	}
}

func TestSetConfig(t *testing.T) {
	e := &DuckDuckGoExtractor{}
	if got := e.GetConfig().Options["max_results"]; got != serputil.DefaultMaxResults {
		t.Errorf("default max_results = %v, want %d", got, serputil.DefaultMaxResults)
	}
	if got := e.GetConfig().Options[keepAnswerOption]; got != false {
		t.Errorf("default %s = %v, want false", keepAnswerOption, got)
	}
	if err := e.SetConfig(&sdk.Config{Enable: true, Options: map[string]any{"unknown": true}}); err == nil {
		t.Fatal("SetConfig accepted an unknown option")
	}
	if err := e.SetConfig(&sdk.Config{Enable: true, Options: map[string]any{keepAnswerOption: "yes"}}); err == nil {
		t.Fatal("SetConfig accepted a non-boolean keep_answer")
	}
	if err := e.SetConfig(&sdk.Config{Enable: true, Options: map[string]any{"max_results": 0}}); err == nil {
		t.Fatal("SetConfig accepted max_results=0")
	}
	if err := e.SetConfig(&sdk.Config{Enable: true, Options: map[string]any{"max_results": 1}}); err != nil {
		t.Fatalf("SetConfig rejected max_results: %v", err)
	}
	doc := &document.Document{URL: "https://duckduckgo.com/?q=web+scraping", HTML: modernResultPage}
	if decision, err := e.Extract(doc).Unpack(); decision != sdk.ExtractorSuccess {
		t.Fatalf("Extract decision = %v (%v)", decision, err)
	}
	if len(doc.ExtraDocuments) != 1 {
		t.Fatalf("extra documents = %d, want 1", len(doc.ExtraDocuments))
	}
}
