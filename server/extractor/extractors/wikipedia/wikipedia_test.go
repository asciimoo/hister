package wikipedia

import (
	"os"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/extractor/sdk"
)

func TestMatchWikipediaURLs(t *testing.T) {
	e := &WikipediaExtractor{}
	cases := []struct {
		url  string
		want bool
	}{
		{"https://en.wikipedia.org/wiki/Go_(programming_language)", true},
		{"https://de.wikipedia.org/wiki/Berlin", true},
		{"https://ja.wikipedia.org/wiki/東京", true},
		{"https://en.wikipedia.org/wiki/", false}, // bare /wiki/
		{"https://en.wikipedia.org/", false},      // homepage
		{"https://en.wikipedia.org/wiki/Special:Search", false},
		{"https://en.wikipedia.org/wiki/Wikipedia:About", true},
		{"https://en.wikipedia.org/wiki/Wikipedia:Unusual_articles", true},
		{"https://en.wikipedia.org/wiki/Talk:Go", false},
		{"https://en.wikipedia.org/wiki/User:Example", false},
		{"https://en.wikipedia.org/wiki/Category:Programming", false},
		{"https://en.wikipedia.org/wiki/File:Example.jpg", false},
		{"https://en.wikipedia.org/wiki/Template:Infobox", false},
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

const minimalArticle = `<html>
<head><title>Test - Wikipedia</title></head>
<body>
<h1 id="firstHeading"><span class="mw-page-title-main">Test Article</span></h1>
<div id="mw-content-text" class="mw-body-content">
<div class="mw-parser-output">
<div class="shortdescription">A test article for extraction</div>
<p>This is the lead paragraph of the test article. It contains important information.</p>
<h2 id="History">History</h2>
<p>The history section describes past events.</p>
<h3 id="Early_history">Early history</h3>
<p>Early history details go here.</p>
<table class="wikitable">
<caption>Test data table</caption>
<tr><th>Name</th><th>Value</th></tr>
<tr><td>Alpha</td><td>100</td></tr>
<tr><td>Beta</td><td>200</td></tr>
</table>
</div>
</div>
<div id="catlinks"><div class="mw-normal-catlinks">
<ul><li><a title="Category:Test">Test</a></li><li><a title="Category:Articles">Articles</a></li></ul>
</div></div>
</body></html>`

func TestExtractMinimalArticle(t *testing.T) {
	d := &document.Document{
		URL:  "https://en.wikipedia.org/wiki/Test_Article",
		HTML: minimalArticle,
	}
	e := &WikipediaExtractor{}
	state, err := e.Extract(d).Unpack()
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("state = %v, want Stop", state)
	}
	if d.Title != "Test Article" {
		t.Errorf("Title = %q, want %q", d.Title, "Test Article")
	}
	if !strings.Contains(d.Text, "lead paragraph") {
		t.Error("Text should contain lead paragraph")
	}
	if !strings.Contains(d.Text, "History") {
		t.Error("Text should contain section heading")
	}
	if !strings.Contains(d.Text, "Early history") {
		t.Error("Text should contain subsection heading")
	}
	// Table content should be present.
	if !strings.Contains(d.Text, "Alpha") || !strings.Contains(d.Text, "100") {
		t.Error("Text should contain wikitable data")
	}
	if !strings.Contains(d.Text, "Test data table") {
		t.Error("Text should contain table caption")
	}
	// Metadata checks.
	if d.Metadata["type"] != "Article" {
		t.Errorf("Metadata[type] = %v, want Article", d.Metadata["type"])
	}
	if d.Metadata["description"] != "A test article for extraction" {
		t.Errorf("Metadata[description] = %q", d.Metadata["description"])
	}
	if cats, _ := d.Metadata["categories"].(string); !strings.Contains(cats, "Test") {
		t.Errorf("Metadata[categories] = %q, want to contain Test", cats)
	}
}

const articleWithInfobox = `<html>
<body>
<h1 id="firstHeading"><span class="mw-page-title-main">Test Country</span></h1>
<div class="mw-parser-output">
<table class="infobox">
<tr><th colspan="2">Test Country</th></tr>
<tr><th>Capital</th><td>Testville</td></tr>
<tr><th>Population</th><td>1,000,000</td></tr>
<tr><th>Area</th><td>50,000 km²</td></tr>
</table>
<p>Test Country is a nation in the world.</p>
</div>
</body></html>`

func TestExtractInfobox(t *testing.T) {
	d := &document.Document{
		URL:  "https://en.wikipedia.org/wiki/Test_Country",
		HTML: articleWithInfobox,
	}
	e := &WikipediaExtractor{}
	state, err := e.Extract(d).Unpack()
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("state = %v, want Stop", state)
	}
	if !strings.Contains(d.Text, "Capital: Testville") {
		t.Errorf("Text should contain infobox key-value pair, got:\n%s", d.Text)
	}
	if !strings.Contains(d.Text, "Population: 1,000,000") {
		t.Error("Text should contain population")
	}
}

const articleWithNoise = `<html>
<body>
<h1 id="firstHeading"><span class="mw-page-title-main">Noisy Article</span></h1>
<div class="mw-parser-output">
<div class="shortdescription">Short desc</div>
<div class="hatnote">For other uses, see Foo.</div>
<p>Real content here.</p>
<div class="navbox">Navigation box content that should be removed.</div>
<div class="toc">Table of contents that should be removed.</div>
<div class="sidebar">Sidebar content to remove.</div>
<ol class="references"><li>Reference 1</li><li>Reference 2</li></ol>
<p>More real content.<sup class="reference">[1]</sup></p>
</div>
</body></html>`

func TestNoiseRemoval(t *testing.T) {
	d := &document.Document{
		URL:  "https://en.wikipedia.org/wiki/Noisy_Article",
		HTML: articleWithNoise,
	}
	e := &WikipediaExtractor{}
	state, err := e.Extract(d).Unpack()
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("state = %v, want Stop", state)
	}
	if !strings.Contains(d.Text, "Real content here") {
		t.Error("Text should contain real content")
	}
	if strings.Contains(d.Text, "Navigation box") {
		t.Error("Text should not contain navbox content")
	}
	if strings.Contains(d.Text, "Reference 1") {
		t.Error("Text should not contain reference list")
	}
	if strings.Contains(d.Text, "[1]") {
		t.Error("Text should not contain inline citation markers")
	}
}

func TestPreviewMinimalArticle(t *testing.T) {
	d := &document.Document{
		URL:  "https://en.wikipedia.org/wiki/Test_Article",
		HTML: minimalArticle,
	}
	e := &WikipediaExtractor{}
	resp, state, err := e.Preview(d).Unpack()
	if err != nil {
		t.Fatalf("Preview error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("state = %v, want Stop", state)
	}
	if !strings.Contains(resp.Content, "lead paragraph") {
		t.Error("Preview should contain article text")
	}
	if !strings.Contains(resp.Content, "<table") {
		t.Error("Preview should preserve tables")
	}
	// Wikitable should be styled with borders.
	if !strings.Contains(resp.Content, "border-collapse: collapse") {
		t.Error("Preview should style wikitables with border-collapse")
	}
	if !strings.Contains(resp.Content, "border: 1px solid") {
		t.Error("Preview should add borders to wikitable cells")
	}
	// Headings should be styled.
	if !strings.Contains(resp.Content, "border-bottom: 1px solid") {
		t.Error("Preview should add bottom border to h2 headings")
	}
	// Navbox/toc should be gone even in preview.
	if strings.Contains(resp.Content, "navbox") {
		t.Error("Preview should not contain navbox")
	}
}

func TestPreviewInfoboxStyling(t *testing.T) {
	d := &document.Document{
		URL:  "https://en.wikipedia.org/wiki/Test_Country",
		HTML: articleWithInfobox,
	}
	e := &WikipediaExtractor{}
	resp, state, err := e.Preview(d).Unpack()
	if err != nil {
		t.Fatalf("Preview error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("state = %v, want Stop", state)
	}
	// Infobox should be floated right with background.
	if !strings.Contains(resp.Content, "float: right") {
		t.Error("Preview should float infobox right")
	}
	if !strings.Contains(resp.Content, "background-color: rgba(128,128,128,0.06)") {
		t.Error("Preview should add background color to infobox")
	}
	// Header cell should have highlighted background.
	if !strings.Contains(resp.Content, "background-color: rgba(100,150,220,0.18)") {
		t.Error("Preview should highlight infobox header with accent background")
	}
	// Should still contain the data.
	if !strings.Contains(resp.Content, "Testville") {
		t.Error("Preview should contain infobox data")
	}
}

func TestPreviewURLRewriting(t *testing.T) {
	const html = `<html><body>
<h1 id="firstHeading"><span class="mw-page-title-main">Links</span></h1>
<div class="mw-parser-output">
<p><a href="/wiki/Go_(programming_language)">Go</a></p>
<img src="//upload.wikimedia.org/image.png" />
</div>
</body></html>`

	d := &document.Document{
		URL:  "https://en.wikipedia.org/wiki/Links",
		HTML: html,
	}
	e := &WikipediaExtractor{}
	resp, state, err := e.Preview(d).Unpack()
	if err != nil {
		t.Fatalf("Preview error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("state = %v, want Stop", state)
	}
	if !strings.Contains(resp.Content, "https://en.wikipedia.org/wiki/Go_(programming_language)") {
		t.Error("Preview should rewrite relative wiki links to absolute")
	}
	if !strings.Contains(resp.Content, "https://upload.wikimedia.org/image.png") {
		t.Error("Preview should resolve protocol-relative URLs")
	}
}

func TestNoContentReturnsContinue(t *testing.T) {
	d := &document.Document{
		URL:  "https://en.wikipedia.org/wiki/Empty",
		HTML: "<html><body><h1 id='firstHeading'></h1></body></html>",
	}
	e := &WikipediaExtractor{}
	state, _ := e.Extract(d).Unpack()
	if state != sdk.ExtractorFallback {
		t.Errorf("state = %v, want Continue for empty page", state)
	}
}

func TestPreviewReferencesPreserved(t *testing.T) {
	const html = `<html><body>
<h1 id="firstHeading"><span class="mw-page-title-main">Refs</span></h1>
<div class="mw-parser-output">
<p>Some text with a citation.<sup class="reference"><a href="#cite_note-1">[1]</a></sup></p>
<h2 id="Notes">Notes</h2>
<div class="mw-references-wrap"><ol class="references" data-mw-group="lower-alpha">
<li id="cite_note-alpha"><span class="reference-text">This is an explanatory note.</span></li>
</ol></div>
<h2 id="References">References</h2>
<div class="mw-references-wrap"><ol class="references">
<li id="cite_note-1"><span class="reference-text">Smith, J. (2024). A Book.</span></li>
</ol></div>
</div>
</body></html>`

	d := &document.Document{URL: "https://en.wikipedia.org/wiki/Refs", HTML: html}
	e := &WikipediaExtractor{}
	resp, state, err := e.Preview(d).Unpack()
	if err != nil {
		t.Fatalf("Preview error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("state = %v, want Stop", state)
	}
	// References should be in the preview.
	if !strings.Contains(resp.Content, "Smith, J.") {
		t.Error("Preview should preserve reference text")
	}
	// Explanatory notes should be in the preview.
	if !strings.Contains(resp.Content, "explanatory note") {
		t.Error("Preview should preserve explanatory notes")
	}
	// Inline citation markers should be preserved too.
	if !strings.Contains(resp.Content, "[1]") {
		t.Error("Preview should preserve inline citation markers")
	}
	// References should be styled.
	if !strings.Contains(resp.Content, "font-size: 0.8em") {
		t.Error("Preview should style reference sections with smaller font")
	}
}

func TestPreviewGallery(t *testing.T) {
	const html = `<html><body>
<h1 id="firstHeading"><span class="mw-page-title-main">Gallery</span></h1>
<div class="mw-parser-output">
<ul class="gallery mw-gallery-traditional">
<li class="gallerybox" style="width: 155px">
<div class="thumb" style="width: 150px; height: 150px;"><a href="/wiki/File:Test.jpg"><img src="//upload.wikimedia.org/test.jpg" width="120" height="80" /></a></div>
<div class="gallerytext">A test image</div>
</li>
<li class="gallerybox" style="width: 155px">
<div class="thumb" style="width: 150px; height: 150px;"><a href="/wiki/File:Test2.jpg"><img src="//upload.wikimedia.org/test2.jpg" width="120" height="80" /></a></div>
<div class="gallerytext">Another test</div>
</li>
</ul>
</div>
</body></html>`

	d := &document.Document{URL: "https://en.wikipedia.org/wiki/Gallery", HTML: html}
	e := &WikipediaExtractor{}
	resp, state, err := e.Preview(d).Unpack()
	if err != nil {
		t.Fatalf("Preview error: %v", err)
	}
	if state != sdk.ExtractorSuccess {
		t.Fatalf("state = %v, want Stop", state)
	}
	if !strings.Contains(resp.Content, "display: flex") {
		t.Error("Preview should style gallery as a flex container")
	}
	if !strings.Contains(resp.Content, "A test image") {
		t.Error("Preview should preserve gallery text")
	}
	if !strings.Contains(resp.Content, "https://upload.wikimedia.org/test.jpg") {
		t.Error("Preview should resolve gallery image URLs")
	}
}

// TestRealArticles tests against real Wikipedia articles if available on disk.
// Skipped when the files are absent (CI). To run locally:
//
//	curl -sL "https://en.wikipedia.org/wiki/Periodic_table" -o /tmp/wiki_periodic_table.html
//	curl -sL "https://en.wikipedia.org/wiki/World_War_II" -o /tmp/wiki_ww2.html
//	curl -sL "https://en.wikipedia.org/wiki/List_of_countries_by_GDP_(nominal)" -o /tmp/wiki_gdp.html
func TestRealArticles(t *testing.T) {
	cases := []struct {
		name  string
		file  string
		url   string
		title string
		terms []string
	}{
		{
			name:  "PeriodicTable",
			file:  "/tmp/wiki_periodic_table.html",
			url:   "https://en.wikipedia.org/wiki/Periodic_table",
			title: "Periodic table",
			terms: []string{"hydrogen", "element", "atomic"},
		},
		{
			name:  "WorldWarII",
			file:  "/tmp/wiki_ww2.html",
			url:   "https://en.wikipedia.org/wiki/World_War_II",
			title: "World War II",
			terms: []string{"nazi", "allies", "1939", "1945"},
		},
		{
			name:  "GDP",
			file:  "/tmp/wiki_gdp.html",
			url:   "https://en.wikipedia.org/wiki/List_of_countries_by_GDP_(nominal)",
			title: "List of countries by GDP (nominal)",
			terms: []string{"united states", "china", "imf"},
		},
		{
			name:  "WorldPopulation",
			file:  "/tmp/wiki_world_pop.html",
			url:   "https://en.wikipedia.org/wiki/World_population",
			title: "World population",
			terms: []string{"billion", "india", "china", "fertility"},
		},
		{
			// Wikipedia:Unusual_articles lives in the Wikipedia: namespace
			// which Match() excludes, but Extract/Preview handle the HTML fine.
			// We use a fake article URL so the extractor processes it.
			name:  "UnusualArticles",
			file:  "/tmp/wiki_unusual.html",
			url:   "https://en.wikipedia.org/wiki/Unusual_articles",
			title: "Unusual articles",
			terms: []string{"unusual"},
		},
	}

	e := &WikipediaExtractor{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html, err := os.ReadFile(tc.file)
			if err != nil {
				t.Skipf("file %s not available; skipping", tc.file)
			}

			d := &document.Document{URL: tc.url, HTML: string(html)}

			t.Run("Extract", func(t *testing.T) {
				state, err := e.Extract(d).Unpack()
				if err != nil {
					t.Fatalf("Extract error: %v", err)
				}
				if state != sdk.ExtractorSuccess {
					t.Fatalf("state = %v, want Stop", state)
				}
				if d.Title != tc.title {
					t.Errorf("Title = %q, want %q", d.Title, tc.title)
				}
				if len(d.Text) < 1000 {
					t.Errorf("Text too short (%d chars), expected substantial content", len(d.Text))
				}
				for _, want := range tc.terms {
					if !strings.Contains(strings.ToLower(d.Text), want) {
						t.Errorf("Text should contain %q", want)
					}
				}
				t.Logf("Extracted %d chars of text, title=%q", len(d.Text), d.Title)
			})

			t.Run("Preview", func(t *testing.T) {
				resp, state, err := e.Preview(d).Unpack()
				if err != nil {
					t.Fatalf("Preview error: %v", err)
				}
				if state != sdk.ExtractorSuccess {
					t.Fatalf("state = %v, want Stop", state)
				}
				if len(resp.Content) < 1000 {
					t.Errorf("Preview too short (%d chars)", len(resp.Content))
				}
				if strings.Contains(resp.Content, `href="/wiki/`) {
					t.Error("Preview still contains relative /wiki/ links")
				}
				t.Logf("Preview: %d chars of HTML", len(resp.Content))
			})
		})
	}
}
