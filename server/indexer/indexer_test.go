package indexer

import (
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/testutil"
)

func TestSearchSortsByMostVisited(t *testing.T) {
	idxCfg := testutil.Config(t)
	if err := Init(idxCfg); err != nil {
		t.Fatalf("failed to init indexer: %v", err)
	}
	defer i.Close()

	lessVisitedURL := "https://example.com/less-visited"
	mostVisitedURL := "https://example.com/most-visited"
	docs := []string{
		lessVisitedURL,
		mostVisitedURL,
		mostVisitedURL,
		mostVisitedURL,
	}
	for _, url := range docs {
		if err := Add(&document.Document{
			URL:   url,
			Title: "Visited sort",
			Text:  "Visited sort document text",
		}); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	res, err := Search(idxCfg, &Query{Text: "*", Sort: "visits"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(res.Documents) < 2 {
		t.Fatalf("Search returned %d documents, want at least 2", len(res.Documents))
	}
	if res.Documents[0].URL != mostVisitedURL {
		t.Fatalf("first result URL = %q, want %q", res.Documents[0].URL, mostVisitedURL)
	}
	if res.Documents[0].AddCount != 3 {
		t.Fatalf("first result AddCount = %d, want 3", res.Documents[0].AddCount)
	}
	if res.Documents[1].URL != lessVisitedURL {
		t.Fatalf("second result URL = %q, want %q", res.Documents[1].URL, lessVisitedURL)
	}
}

func TestSearchFiltersMetadataSourceByLatestUpdate(t *testing.T) {
	idxCfg := testutil.Config(t)
	if err := Init(idxCfg); err != nil {
		t.Fatalf("failed to init indexer: %v", err)
	}
	defer i.Close()

	docs := []*document.Document{
		{
			URL:       "https://example.com/older-linkwarden",
			Title:     "Older Linkwarden document",
			Updated:   100,
			Metadata:  map[string]any{"source": "linkwarden"},
			Processed: true,
		},
		{
			URL:       "https://example.com/newer-linkwarden",
			Title:     "Newer Linkwarden document",
			Updated:   200,
			Metadata:  map[string]any{"source": "linkwarden"},
			Processed: true,
		},
		{
			URL:       "https://example.com/unrelated",
			Title:     "Unrelated document",
			Updated:   300,
			Metadata:  map[string]any{"source": "other"},
			Processed: true,
		},
	}
	for _, doc := range docs {
		if err := Add(doc); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	res, err := Search(idxCfg, &Query{Text: "metadata.source:linkwarden", Sort: "date", Limit: 1})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(res.Documents) != 1 {
		t.Fatalf("Search returned %d documents, want 1", len(res.Documents))
	}
	if res.Documents[0].URL != docs[1].URL || res.Documents[0].Updated != 200 {
		t.Fatalf("latest Linkwarden document = %#v, want %#v", res.Documents[0], docs[1])
	}
}

func TestSearchFiltersByVisitCount(t *testing.T) {
	idxCfg := testutil.Config(t)
	if err := Init(idxCfg); err != nil {
		t.Fatalf("failed to init indexer: %v", err)
	}
	defer i.Close()

	lessVisitedURL := "https://example.com/visit-filter-less"
	mostVisitedURL := "https://example.com/visit-filter-most"
	docs := []string{
		lessVisitedURL,
		mostVisitedURL,
		mostVisitedURL,
		mostVisitedURL,
	}
	for _, url := range docs {
		if err := Add(&document.Document{
			URL:   url,
			Title: "Visited filter",
			Text:  "Visited filter document text",
		}); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	res, err := Search(idxCfg, &Query{Text: "Visited filter visits:2..4"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(res.Documents) != 1 {
		t.Fatalf("Search returned %d documents, want 1", len(res.Documents))
	}
	if res.Documents[0].URL != mostVisitedURL {
		t.Fatalf("result URL = %q, want %q", res.Documents[0].URL, mostVisitedURL)
	}
}

func TestSearchAndDeleteFilterByRelativeTime(t *testing.T) {
	idxCfg := testutil.Config(t)
	if err := Init(idxCfg); err != nil {
		t.Fatalf("failed to init indexer: %v", err)
	}
	defer i.Close()

	now := time.Now()
	oldDocument := &document.Document{
		URL:       "https://example.com/time-filter-old",
		Title:     "Time filter old",
		Text:      "Time filter document text",
		Added:     now.Add(-100 * 24 * time.Hour).Unix(),
		Updated:   now.Add(-100 * 24 * time.Hour).Unix(),
		Processed: true,
	}
	revisitedDocument := &document.Document{
		URL:       "https://example.com/time-filter-revisited",
		Title:     "Time filter revisited",
		Text:      "Time filter document text",
		Added:     now.Add(-100 * 24 * time.Hour).Unix(),
		Updated:   now.Add(-10 * 24 * time.Hour).Unix(),
		Processed: true,
	}
	recentDocument := &document.Document{
		URL:       "https://example.com/time-filter-recent",
		Title:     "Time filter recent",
		Text:      "Time filter document text",
		Added:     now.Add(-10 * 24 * time.Hour).Unix(),
		Updated:   now.Add(-10 * 24 * time.Hour).Unix(),
		Processed: true,
	}
	for _, doc := range []*document.Document{oldDocument, revisitedDocument, recentDocument} {
		if err := Add(doc); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	res, err := Search(idxCfg, &Query{Text: "Time filter added:>90d updated:<90d"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(res.Documents) != 1 || res.Documents[0].URL != revisitedDocument.URL {
		t.Fatalf("combined relative time search returned %#v, want only %q", res.Documents, revisitedDocument.URL)
	}

	deleted, err := DeleteByQuery("updated:>90d", nil, nil)
	if err != nil {
		t.Fatalf("DeleteByQuery failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("DeleteByQuery deleted %d documents, want 1", deleted)
	}
	if GetByURLAndUser(oldDocument.URL, 0) != nil {
		t.Fatal("old document still exists after relative time deletion")
	}
	if GetByURLAndUser(revisitedDocument.URL, 0) == nil {
		t.Fatal("recently updated document was removed by relative time deletion")
	}
	if GetByURLAndUser(recentDocument.URL, 0) == nil {
		t.Fatal("recent document was removed by relative time deletion")
	}
}

func TestSearchFiltersByAbsoluteDate(t *testing.T) {
	idxCfg := testutil.Config(t)
	if err := Init(idxCfg); err != nil {
		t.Fatalf("failed to init indexer: %v", err)
	}
	defer i.Close()

	beforeCutoff := time.Date(2025, time.December, 31, 23, 59, 59, 0, time.UTC).Unix()
	atCutoff := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()
	afterCutoff := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC).Unix()
	documents := []*document.Document{
		{
			URL:       "https://example.com/absolute-date-before",
			Title:     "Absolute date before",
			Text:      "Absolute date filter text",
			Added:     beforeCutoff,
			Updated:   beforeCutoff,
			Processed: true,
		},
		{
			URL:       "https://example.com/absolute-date-boundary",
			Title:     "Absolute date boundary",
			Text:      "Absolute date filter text",
			Added:     atCutoff,
			Updated:   atCutoff,
			Processed: true,
		},
		{
			URL:       "https://example.com/absolute-date-after",
			Title:     "Absolute date after",
			Text:      "Absolute date filter text",
			Added:     afterCutoff,
			Updated:   afterCutoff,
			Processed: true,
		},
	}
	for _, doc := range documents {
		if err := Add(doc); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	res, err := Search(idxCfg, &Query{Text: "Absolute date added:<2026-01-01"})
	if err != nil {
		t.Fatalf("Search before absolute date failed: %v", err)
	}
	if len(res.Documents) != 1 || res.Documents[0].URL != documents[0].URL {
		t.Fatalf("absolute date search returned %#v, want only %q", res.Documents, documents[0].URL)
	}

	res, err = Search(idxCfg, &Query{Text: "Absolute date updated:>=2026-01-01"})
	if err != nil {
		t.Fatalf("Search from absolute date failed: %v", err)
	}
	gotURLs := make(map[string]bool, len(res.Documents))
	for _, doc := range res.Documents {
		gotURLs[doc.URL] = true
	}
	if len(gotURLs) != 2 || !gotURLs[documents[1].URL] || !gotURLs[documents[2].URL] {
		t.Fatalf("absolute date search returned %#v, want boundary and after documents", res.Documents)
	}
}

func TestSearchVisitCountFacets(t *testing.T) {
	idxCfg := testutil.Config(t)
	if err := Init(idxCfg); err != nil {
		t.Fatalf("failed to init indexer: %v", err)
	}
	defer i.Close()

	lessVisitedURL := "https://example.com/visit-facet-less"
	mostVisitedURL := "https://example.com/visit-facet-most"
	docs := []string{
		lessVisitedURL,
		mostVisitedURL,
		mostVisitedURL,
		mostVisitedURL,
	}
	for _, url := range docs {
		if err := Add(&document.Document{
			URL:   url,
			Title: "Visited facet",
			Text:  "Visited facet document text",
		}); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	res, err := Search(idxCfg, &Query{Text: "Visited facet", Facets: true, FacetsOnly: true})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if res.Facets == nil {
		t.Fatal("Facets is nil")
	}
	visits := res.Facets.Terms["visits"].Terms
	counts := make(map[string]int, len(visits))
	labels := make(map[string]string, len(visits))
	for _, bucket := range visits {
		counts[bucket.Term] = bucket.Count
		labels[bucket.Term] = bucket.Label
	}
	if counts["1"] != 1 {
		t.Fatalf("visit bucket 1 = %d, want 1", counts["1"])
	}
	if counts["2..4"] != 1 {
		t.Fatalf("visit bucket 2..4 = %d, want 1", counts["2..4"])
	}
	if labels["2..4"] != "2 to 4" {
		t.Fatalf("visit bucket label 2..4 = %q, want %q", labels["2..4"], "2 to 4")
	}
}

func TestSearchReturnsFaviconKeyWithoutFaviconData(t *testing.T) {
	idxCfg := testutil.Config(t)
	if err := Init(idxCfg); err != nil {
		t.Fatalf("failed to init indexer: %v", err)
	}
	defer i.Close()

	const faviconData = "data:image/png;base64,ZmF2aWNvbg=="
	if err := Add(&document.Document{
		URL:     "https://example.com/favicon-key",
		Title:   "Favicon key",
		Text:    "Favicon key document text",
		Favicon: faviconData,
	}); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	res, err := Search(idxCfg, &Query{Text: "Favicon key"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(res.Documents) != 1 {
		t.Fatalf("Search returned %d documents, want 1", len(res.Documents))
	}
	doc := res.Documents[0]
	if doc.Favicon != "" {
		t.Fatalf("Favicon data was included in search result: %.32q", doc.Favicon)
	}
	if doc.FaviconKey == "" {
		t.Fatal("FaviconKey is empty")
	}
	if strings.Contains(doc.FaviconKey, "data:") {
		t.Fatalf("FaviconKey contains inline data: %q", doc.FaviconKey)
	}

	data, err := ReadFavicon(doc.FaviconKey)
	if err != nil {
		t.Fatalf("ReadFavicon failed: %v", err)
	}
	if string(data) != faviconData {
		t.Fatalf("ReadFavicon = %q, want %q", string(data), faviconData)
	}
}
