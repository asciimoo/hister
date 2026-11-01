package indexer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/indexer/searchschema"
	servermetrics "github.com/asciimoo/hister/server/metrics"
	"github.com/asciimoo/hister/server/testutil"
	"github.com/asciimoo/hister/server/vectorstore"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

func newTestIndexer(t *testing.T, cfg *config.Config) *Indexer {
	t.Helper()
	idx, err := New(cfg)
	if err != nil {
		t.Fatalf("New indexer: %v", err)
	}
	return idx
}

func TestNewRejectsInvalidSensitivePatterns(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.SensitiveContentPatterns = map[string]string{"broken": "[private_credential"}
	idx, err := New(cfg)
	if idx != nil {
		idx.Close()
		t.Fatal("created an indexer with an invalid sensitive pattern")
	}
	if err == nil {
		t.Fatal("missing invalid sensitive pattern error")
	}
	if strings.Contains(err.Error(), "private_credential") {
		t.Fatal("indexer error exposed a sensitive pattern")
	}
}

func TestSensitivePatternFlagsAreIndependent(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.SensitiveContentPatterns = map[string]string{
		"a_folded": "(?i)^token$",
		"z_exact":  "^SECRET$",
	}
	idx := newTestIndexer(t, cfg)
	defer idx.Close()
	for _, tc := range []struct {
		text      string
		sensitive bool
	}{
		{"token", true},
		{"TOKEN", true},
		{"SECRET", true},
		{"secret", false},
		{"ordinary text", false},
	} {
		doc := &document.Document{URL: "remote-file://client/" + tc.text + ".txt", Text: tc.text, Type: document.RemoteFile}
		err := idx.Add(doc)
		if tc.sensitive {
			if !errors.Is(err, document.ErrSensitiveContent) {
				t.Errorf("Add(%q) error = %v, want sensitive content rejection", tc.text, err)
			}
		} else if err != nil {
			t.Errorf("Add(%q) error = %v, want success", tc.text, err)
		}
	}
}

func TestAddContextRejectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	idx := &Indexer{}

	err := idx.AddContext(ctx, &document.Document{URL: "https://example.com", Processed: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AddContext error = %v, want context.Canceled", err)
	}
}

func TestReindexErrorIdentifiesDocument(t *testing.T) {
	cfg := testutil.Config(t)
	idx := newTestIndexer(t, cfg)
	defer idx.Close()

	// Represent a stored document that no longer passes processing checks.
	const url = "https:/missing-host"
	if err := idx.Add(&document.Document{URL: url, Text: "Stored content", Processed: true}); err != nil {
		t.Fatal(err)
	}
	err := idx.Reindex(&config.Rules{}, false, cfg.Indexer.DetectLanguages, cfg.Indexer.KeepStopwords, cfg.Indexer.Directories)
	if err == nil {
		t.Fatal("reindex accepted an invalid document URL")
	}
	if !strings.Contains(err.Error(), "invalid URL: missing scheme/host") {
		t.Fatalf("reindex error lost the cause: %v", err)
	}
	if !strings.Contains(err.Error(), url) {
		t.Fatalf("reindex error does not identify the document: %v", err)
	}
	if got := idx.GetByURLAndUser(url, 0); got == nil || got.Text != "Stored content" {
		t.Fatal("failed reindex changed the original document")
	}
}

func TestIndexerInstancesAreIndependent(t *testing.T) {
	first := newTestIndexer(t, testutil.Config(t))
	defer first.Close()
	second := newTestIndexer(t, testutil.Config(t))
	defer second.Close()

	doc := &document.Document{
		URL:       "https://example.com/first-indexer",
		Title:     "First indexer",
		Text:      "Stored only in the first indexer",
		Processed: true,
	}
	if err := first.Add(doc); err != nil {
		t.Fatalf("add to first indexer: %v", err)
	}
	if first.GetByURLAndUser(doc.URL, 0) == nil {
		t.Fatal("first indexer did not store its document")
	}
	if second.GetByURLAndUser(doc.URL, 0) != nil {
		t.Fatal("second indexer contains a document from the first indexer")
	}
}

func TestMultiBatchCountsDocumentsOnlyAfterSave(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()

	m := servermetrics.New(context.Background(), idx)
	idx.SetMetrics(m)
	defer m.Stop()

	batch := idx.NewMultiBatch()
	doc := &document.Document{
		URL:       "https://example.com/batched",
		Text:      "A staged document",
		Type:      document.Web,
		Processed: true,
	}
	// Materialize the labeled counter so that its zero value is exposed.
	m.DocumentsIndexedTotal.WithLabelValues(doc.Type.String())
	if err := batch.Add(doc); err != nil {
		t.Fatalf("stage document: %v", err)
	}
	metric := "hister_documents_indexed_total{type=\"web\"} 0"
	if body := metricsResponse(t, m); !strings.Contains(body, metric) {
		t.Fatalf("documents indexed after Add missing %q:\n%s", metric, body)
	}
	if body := metricsResponse(t, m); !strings.Contains(body, "hister_indexing_duration_seconds_count 0") {
		t.Fatalf("indexing duration recorded before Save:\n%s", body)
	}
	if err := batch.Save(); err != nil {
		t.Fatalf("save batch: %v", err)
	}
	metric = "hister_documents_indexed_total{type=\"web\"} 1"
	if body := metricsResponse(t, m); !strings.Contains(body, metric) {
		t.Fatalf("documents indexed after Save missing %q:\n%s", metric, body)
	}
	if body := metricsResponse(t, m); !strings.Contains(body, "hister_indexing_duration_seconds_count 1") {
		t.Fatalf("indexing duration not recorded after Save:\n%s", body)
	}
}

func TestMultiBatchDurationIncludesBatchCommitDelay(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()

	m := servermetrics.New(context.Background(), idx)
	idx.SetMetrics(m)
	defer m.Stop()

	const batchDelay = 200 * time.Millisecond
	idx.indexesMu.Lock()
	for name, index := range idx.indexers {
		idx.indexers[name] = delayedBatchIndex{bleveIndex: index, delay: batchDelay}
	}
	idx.indexesMu.Unlock()

	batch := idx.NewMultiBatch()
	doc := &document.Document{
		URL:       "https://example.com/batched-delay",
		Text:      "A staged document with a delayed commit",
		Type:      document.Web,
		Processed: true,
	}
	if err := batch.Add(doc); err != nil {
		t.Fatalf("stage document: %v", err)
	}
	if err := batch.Save(); err != nil {
		t.Fatalf("save batch: %v", err)
	}

	sum := metricValue(t, metricsResponse(t, m), "hister_indexing_duration_seconds_sum")
	if sum < batchDelay.Seconds() {
		t.Fatalf("indexing duration sum = %.3fs, want at least %.3fs", sum, batchDelay.Seconds())
	}
	t.Logf("indexing duration sum includes delayed batch commit: %.3fs", sum)
}

type bleveIndex interface {
	bleve.Index
}

type delayedBatchIndex struct {
	bleveIndex
	delay time.Duration
}

func (i delayedBatchIndex) Batch(batch *bleve.Batch) error {
	time.Sleep(i.delay)
	return i.bleveIndex.Batch(batch)
}

func TestMetricsAttachmentIsSafeDuringIndexing(t *testing.T) {
	idx := &Indexer{}
	m := servermetrics.New(context.Background(), nil)
	defer m.Stop()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 1_000 {
			idx.SetMetrics(m)
		}
	}()
	go func() {
		defer wg.Done()
		for range 1_000 {
			idx.recordIndexingMetric("web", time.Now().Add(-time.Millisecond))
		}
	}()
	wg.Wait()
}

func metricsResponse(t *testing.T, m *servermetrics.Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec.Body.String()
}

func metricValue(t *testing.T, body, name string) float64 {
	t.Helper()
	for line := range strings.SplitSeq(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == name {
			var value float64
			if _, err := fmt.Sscanf(fields[1], "%f", &value); err != nil {
				t.Fatalf("parse metric %q value %q: %v", name, fields[1], err)
			}
			return value
		}
	}
	t.Fatalf("metric %q missing:\n%s", name, body)
	return 0
}

func TestSearchURLRegexpUsesGoMatchSemantics(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()

	documents := []*document.Document{
		{URL: "https://example.com/private/one", Title: "Private", Processed: true},
		{URL: "https://example.com/public", Title: "Public", Processed: true},
		{URL: "https://example.org/private/two", Title: "Other domain", Processed: true},
	}
	for _, d := range documents {
		if err := idx.Add(d); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		query string
		want  string
	}{
		{query: `url_re:example\.com/private`, want: documents[0].URL},
		{query: `url_re:^https://example\.com/private/.+$`, want: documents[0].URL},
		{query: `url_re:"example\.org/private"`, want: documents[2].URL},
	}
	for _, test := range tests {
		result, err := idx.Search(&Query{Text: test.query})
		if err != nil {
			t.Fatalf("Search(%q): %v", test.query, err)
		}
		if len(result.Documents) != 1 || result.Documents[0].URL != test.want {
			t.Fatalf("Search(%q) returned %#v, want %q", test.query, result.Documents, test.want)
		}
	}
}

func TestSearchOnlyNegatedTerms(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()

	documents := []*document.Document{
		{URL: "https://go.dev/doc", Title: "Golang docs", Text: "golang language", Processed: true},
		{URL: "https://rust-lang.org/", Title: "Rust", Text: "rust language", Processed: true},
		{URL: "https://python.org/", Title: "Python", Text: "python language", Processed: true},
	}
	for _, d := range documents {
		if err := idx.Add(d); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		query string
		want  []string
	}{
		// a negated field filter already returned every other document
		{query: "-title:golang", want: []string{documents[1].URL, documents[2].URL}},
		{query: "-golang", want: []string{documents[1].URL, documents[2].URL}},
		{query: `-"golang language"`, want: []string{documents[1].URL, documents[2].URL}},
		{query: "-golang -python", want: []string{documents[1].URL}},
		{query: "language -golang", want: []string{documents[1].URL, documents[2].URL}},
	}
	for _, test := range tests {
		result, err := idx.Search(&Query{Text: test.query})
		if err != nil {
			t.Fatalf("Search(%q): %v", test.query, err)
		}
		got := make([]string, 0, len(result.Documents))
		for _, d := range result.Documents {
			got = append(got, d.URL)
		}
		slices.Sort(got)
		want := slices.Clone(test.want)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("Search(%q) returned %q, want %q", test.query, got, want)
		}
	}
}

func TestConcurrentLanguageIndexCreation(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()

	languages := []string{"de", "en", "fr", "hu"}
	const writeCount = 24
	const readerCount = 4
	start := make(chan struct{})
	errs := make(chan error, writeCount+readerCount)
	var wg sync.WaitGroup

	for n := range writeCount {
		wg.Go(func() {
			<-start
			language := languages[n%len(languages)]
			err := idx.Add(&document.Document{
				URL:       fmt.Sprintf("https://example.com/concurrent-language/%d", n),
				Title:     "Concurrent language index",
				Text:      "Document stored while language indexes are created",
				Language:  language,
				Processed: true,
			})
			if err != nil {
				errs <- fmt.Errorf("add document %d: %w", n, err)
			}
		})
	}
	for range readerCount {
		wg.Go(func() {
			<-start
			for range writeCount {
				if _, _, err := idx.GetMetadata(); err != nil {
					errs <- fmt.Errorf("read metadata: %w", err)
					return
				}
				if _, err := idx.Search(&Query{MatchAll: true, Limit: 1}); err != nil {
					errs <- fmt.Errorf("search indexes: %w", err)
					return
				}
			}
		})
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		return
	}

	indexes := idx.indexes()
	if len(indexes) != len(languages)+1 {
		t.Fatalf("index count = %d, want %d", len(indexes), len(languages)+1)
	}
	for _, language := range languages {
		if _, exists := indexes[indexNameForLanguage(language)]; !exists {
			t.Errorf("language index %q was not created", language)
		}
	}
	if got := idx.Total(); got != writeCount {
		t.Fatalf("document count = %d, want %d", got, writeCount)
	}
}

func TestSearchSortsByMostVisited(t *testing.T) {
	idxCfg := testutil.Config(t)
	idx := newTestIndexer(t, idxCfg)
	defer idx.Close()

	lessVisitedURL := "https://example.com/less-visited"
	mostVisitedURL := "https://example.com/most-visited"
	docs := []string{
		lessVisitedURL,
		mostVisitedURL,
		mostVisitedURL,
		mostVisitedURL,
	}
	for _, url := range docs {
		if err := idx.Add(&document.Document{
			URL:   url,
			Title: "Visited sort",
			Text:  "Visited sort document text",
		}); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	res, err := idx.Search(&Query{Text: "*", Sort: "visits"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(res.Documents) < 2 {
		t.Fatalf("Search returned %d documents, want at least 2", len(res.Documents))
	}
	if res.Documents[0].URL != mostVisitedURL {
		t.Fatalf("first result URL = %q, want %q", res.Documents[0].URL, mostVisitedURL)
	}
	if res.Documents[0].DocumentID != res.Documents[0].ID() {
		t.Fatalf("first result document ID = %q, want %q", res.Documents[0].DocumentID, res.Documents[0].ID())
	}
	if res.Documents[0].AddCount != 3 {
		t.Fatalf("first result AddCount = %d, want 3", res.Documents[0].AddCount)
	}
	if res.Documents[1].URL != lessVisitedURL {
		t.Fatalf("second result URL = %q, want %q", res.Documents[1].URL, lessVisitedURL)
	}

	res, err = idx.Search(&Query{Text: "* sort:-visits"})
	if err != nil {
		t.Fatalf("reverse visit search failed: %v", err)
	}
	if len(res.Documents) < 2 || res.Documents[0].URL != lessVisitedURL {
		t.Fatalf("reverse visit search returned %#v, want %q first", res.Documents, lessVisitedURL)
	}
}

func TestSearchSortDirective(t *testing.T) {
	idxCfg := testutil.Config(t)
	idx := newTestIndexer(t, idxCfg)
	defer idx.Close()

	older := &document.Document{
		URL:       "https://a.example.com/sort-directive-older",
		Domain:    "a.example.com",
		Title:     "Sort directive document",
		Text:      "Sort directive document text",
		Updated:   100,
		Processed: true,
	}
	newer := &document.Document{
		URL:       "https://z.example.com/sort-directive-newer",
		Domain:    "z.example.com",
		Title:     "Sort directive document",
		Text:      "Sort directive document text",
		Updated:   200,
		Processed: true,
	}
	for _, doc := range []*document.Document{older, newer} {
		if err := idx.Add(doc); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	q := &Query{Text: "directive document sort:date", Sort: "domain", Limit: 1}
	res, err := idx.Search(q)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(res.Documents) != 1 || res.Documents[0].URL != newer.URL {
		t.Fatalf("date sorted search returned %#v, want %q first", res.Documents, newer.URL)
	}
	if q.Sort != "date" {
		t.Fatalf("effective sort = %q, want date", q.Sort)
	}

	q = &Query{Text: "directive document sort:-date", Limit: 1}
	res, err = idx.Search(q)
	if err != nil {
		t.Fatalf("reverse date search failed: %v", err)
	}
	if len(res.Documents) != 1 || res.Documents[0].URL != older.URL {
		t.Fatalf("reverse date search returned %#v, want %q first", res.Documents, older.URL)
	}
	if q.Sort != "-date" {
		t.Fatalf("effective sort = %q, want -date", q.Sort)
	}

	q = &Query{Text: "directive document sort:-domain", Limit: 1}
	res, err = idx.Search(q)
	if err != nil {
		t.Fatalf("reverse domain search failed: %v", err)
	}
	if len(res.Documents) != 1 || res.Documents[0].URL != newer.URL {
		t.Fatalf("reverse domain search returned %#v, want %q first", res.Documents, newer.URL)
	}

	q = &Query{Text: "sort:date", Limit: 1}
	res, err = idx.Search(q)
	if err != nil {
		t.Fatalf("directive only search failed: %v", err)
	}
	if len(res.Documents) != 1 || res.Documents[0].URL != newer.URL {
		t.Fatalf("directive only search returned %#v, want %q first", res.Documents, newer.URL)
	}

	q = &Query{Text: "directive sort:relevance", Sort: "date"}
	if _, err := idx.Search(q); err != nil {
		t.Fatalf("relevance directive search failed: %v", err)
	}
	if q.Sort != "" {
		t.Fatalf("effective sort = %q, want relevance", q.Sort)
	}

	q = &Query{Text: "directive sort:-relevance", Limit: 1}
	firstPage, err := idx.Search(q)
	if err != nil {
		t.Fatalf("reverse relevance search failed: %v", err)
	}
	if q.Sort != "-relevance" {
		t.Fatalf("effective sort = %q, want -relevance", q.Sort)
	}
	if len(firstPage.Documents) != 1 || firstPage.PageKey == "" {
		t.Fatalf("reverse relevance first page = %#v, want one document and a page key", firstPage)
	}
	secondPage, err := idx.Search(q)
	if err != nil {
		t.Fatalf("reverse relevance second page failed: %v", err)
	}
	if len(secondPage.Documents) != 1 || secondPage.Documents[0].URL == firstPage.Documents[0].URL {
		t.Fatalf("reverse relevance second page = %#v, want the other document", secondPage.Documents)
	}
}

func TestSearchFiltersMetadataSourceByLatestUpdate(t *testing.T) {
	idxCfg := testutil.Config(t)
	idx := newTestIndexer(t, idxCfg)
	defer idx.Close()

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
		if err := idx.Add(doc); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	res, err := idx.Search(&Query{Text: "metadata.source:linkwarden", Sort: "date", Limit: 1})
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

func TestSearchFiltersMetadataAlternation(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()

	docs := []*document.Document{
		{URL: "https://example.com/toot", Metadata: map[string]any{"type": "toot"}, Processed: true},
		{URL: "https://example.com/tweet", Metadata: map[string]any{"type": "tweet"}, Processed: true},
		{URL: "https://example.com/discourse", Metadata: map[string]any{"type": "discourse"}, Processed: true},
	}
	for _, doc := range docs {
		if err := idx.Add(doc); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	res, err := idx.Search(&Query{Text: "metadata.type:(toot|tweet)"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(res.Documents) != 2 {
		t.Fatalf("Search returned %d documents, want 2", len(res.Documents))
	}
	urls := make(map[string]bool, len(res.Documents))
	for _, doc := range res.Documents {
		urls[doc.URL] = true
	}
	for _, want := range docs[:2] {
		if !urls[want.URL] {
			t.Errorf("Search did not return %q", want.URL)
		}
	}
}

func TestSearchFileTypeIncludesLocalAndRemoteFiles(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()

	docs := []*document.Document{
		{URL: "https://example.com/page", Title: "Web", Type: document.Web, Processed: true},
		{URL: "file:///tmp/local.txt", Title: "Local", Type: document.Local, Processed: true},
		{URL: "remote-file://laptop/tmp/README.md", Title: "Remote", Type: document.RemoteFile, Processed: true},
	}
	for _, doc := range docs {
		if err := idx.AddDocument(doc); err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
	}

	res, err := idx.Search(&Query{Text: "type:file"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(res.Documents) != 2 {
		t.Fatalf("type:file returned %d documents, want 2", len(res.Documents))
	}
	types := map[document.DocType]bool{}
	for _, doc := range res.Documents {
		types[doc.Type] = true
	}
	if !types[document.Local] || !types[document.RemoteFile] || types[document.Web] {
		t.Fatalf("type:file returned types %#v", types)
	}

	for _, queryText := range []string{
		"type:remote url:*readme*",
		"type:remote url:*README*",
	} {
		res, err := idx.Search(&Query{Text: queryText})
		if err != nil {
			t.Fatalf("Search(%q) failed: %v", queryText, err)
		}
		if len(res.Documents) != 1 || res.Documents[0].Type != document.RemoteFile {
			t.Fatalf("Search(%q) returned %#v, want the remote README", queryText, res.Documents)
		}
	}
}

func TestTotalsIncludeGlobalDocumentsVisibleToUsers(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()

	docs := []*document.Document{
		{URL: "https://example.com/page", Type: document.Web, Processed: true},
		{URL: "file:///tmp/global.txt", Type: document.Local, Processed: true},
		{URL: "file:///tmp/local.txt", Type: document.Local, UserID: 1, Processed: true},
		{URL: "remote-file://laptop/tmp/README.md", Type: document.RemoteFile, UserID: 2, Processed: true},
	}
	for _, doc := range docs {
		if err := idx.AddDocument(doc); err != nil {
			t.Fatalf("AddDocument failed: %v", err)
		}
	}

	if got := idx.Total(); got != 4 {
		t.Fatalf("document count = %d, want 4", got)
	}
	if got := idx.TotalFiles(); got != 3 {
		t.Fatalf("file count = %d, want 3", got)
	}
	if got := idx.TotalByUser(0); got != 2 {
		t.Fatalf("global document count = %d, want 2", got)
	}
	if got := idx.TotalByUser(1); got != 3 {
		t.Fatalf("user 1 document count = %d, want 3", got)
	}
	if got := idx.TotalByUser(2); got != 3 {
		t.Fatalf("user 2 document count = %d, want 3", got)
	}
	if got := idx.TotalFilesByUser(0); got != 1 {
		t.Fatalf("global file count = %d, want 1", got)
	}
	if got := idx.TotalFilesByUser(1); got != 2 {
		t.Fatalf("user 1 file count = %d, want 2", got)
	}
	if got := idx.TotalFilesByUser(2); got != 2 {
		t.Fatalf("user 2 file count = %d, want 2", got)
	}
	if got := idx.TotalFilesByUser(3); got != 1 {
		t.Fatalf("user 3 file count = %d, want 1", got)
	}
}

func TestSearchFiltersByVisitCount(t *testing.T) {
	idxCfg := testutil.Config(t)
	idx := newTestIndexer(t, idxCfg)
	defer idx.Close()

	lessVisitedURL := "https://example.com/visit-filter-less"
	mostVisitedURL := "https://example.com/visit-filter-most"
	docs := []string{
		lessVisitedURL,
		mostVisitedURL,
		mostVisitedURL,
		mostVisitedURL,
	}
	for _, url := range docs {
		if err := idx.Add(&document.Document{
			URL:   url,
			Title: "Visited filter",
			Text:  "Visited filter document text",
		}); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	res, err := idx.Search(&Query{Text: "Visited filter visits:2..4"})
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
	idx := newTestIndexer(t, idxCfg)
	defer idx.Close()

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
		if err := idx.Add(doc); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	res, err := idx.Search(&Query{Text: "Time filter added:>90d updated:<90d"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(res.Documents) != 1 || res.Documents[0].URL != revisitedDocument.URL {
		t.Fatalf("combined relative time search returned %#v, want only %q", res.Documents, revisitedDocument.URL)
	}

	deleted, err := idx.DeleteByQuery("updated:>90d", nil, nil)
	if err != nil {
		t.Fatalf("DeleteByQuery failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("DeleteByQuery deleted %d documents, want 1", deleted)
	}
	if idx.GetByURLAndUser(oldDocument.URL, 0) != nil {
		t.Fatal("old document still exists after relative time deletion")
	}
	if idx.GetByURLAndUser(revisitedDocument.URL, 0) == nil {
		t.Fatal("recently updated document was removed by relative time deletion")
	}
	if idx.GetByURLAndUser(recentDocument.URL, 0) == nil {
		t.Fatal("recent document was removed by relative time deletion")
	}
}

func TestSearchFiltersByAbsoluteDate(t *testing.T) {
	idxCfg := testutil.Config(t)
	idx := newTestIndexer(t, idxCfg)
	defer idx.Close()

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
		if err := idx.Add(doc); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	res, err := idx.Search(&Query{Text: "Absolute date added:<2026-01-01"})
	if err != nil {
		t.Fatalf("Search before absolute date failed: %v", err)
	}
	if len(res.Documents) != 1 || res.Documents[0].URL != documents[0].URL {
		t.Fatalf("absolute date search returned %#v, want only %q", res.Documents, documents[0].URL)
	}

	res, err = idx.Search(&Query{Text: "Absolute date updated:>=2026-01-01"})
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
	idx := newTestIndexer(t, idxCfg)
	defer idx.Close()

	lessVisitedURL := "https://example.com/visit-facet-less"
	mostVisitedURL := "https://example.com/visit-facet-most"
	docs := []string{
		lessVisitedURL,
		mostVisitedURL,
		mostVisitedURL,
		mostVisitedURL,
	}
	for _, url := range docs {
		if err := idx.Add(&document.Document{
			URL:   url,
			Title: "Visited facet",
			Text:  "Visited facet document text",
		}); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	res, err := idx.Search(&Query{Text: "Visited facet", Facets: true, FacetsOnly: true})
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

func TestSearchDateFacetCountsMatchPresetFilters(t *testing.T) {
	idxCfg := testutil.Config(t)
	idx := newTestIndexer(t, idxCfg)
	defer idx.Close()

	now := time.Now()
	for index, age := range []time.Duration{
		time.Hour,
		3 * 24 * time.Hour,
		10 * 24 * time.Hour,
		400 * 24 * time.Hour,
	} {
		updated := now.Add(-age).Unix()
		if err := idx.Add(&document.Document{
			URL:       fmt.Sprintf("https://example.com/date-facet-%d", index),
			Title:     "Date facet",
			Text:      "Date facet document text",
			Added:     updated,
			Updated:   updated,
			Processed: true,
		}); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	res, err := idx.Search(&Query{Text: "Date facet", Facets: true, FacetsOnly: true})
	if err != nil {
		t.Fatalf("facet search failed: %v", err)
	}
	counts := make(map[string]int)
	for _, bucket := range res.Facets.DateHistogram {
		counts[bucket.Name] = bucket.Count
	}

	for _, test := range []struct {
		bucket string
		query  string
	}{
		{bucket: "last_24h", query: "Date facet updated:<24h"},
		{bucket: "last_7d", query: "Date facet updated:<7d"},
		{bucket: "last_30d", query: "Date facet updated:<30d"},
		{bucket: "last_year", query: "Date facet updated:<365d"},
		{bucket: "older", query: "Date facet updated:>365d"},
	} {
		filtered, err := idx.Search(&Query{Text: test.query})
		if err != nil {
			t.Fatalf("search %q failed: %v", test.query, err)
		}
		if counts[test.bucket] != len(filtered.Documents) {
			t.Fatalf("bucket %q count = %d, query returned %d", test.bucket, counts[test.bucket], len(filtered.Documents))
		}
	}
}

func TestSearchReturnsFaviconKeyWithoutFaviconData(t *testing.T) {
	idxCfg := testutil.Config(t)
	idx := newTestIndexer(t, idxCfg)
	defer idx.Close()

	const faviconData = "data:image/png;base64,ZmF2aWNvbg=="
	if err := idx.Add(&document.Document{
		URL:     "https://example.com/favicon-key",
		Title:   "Favicon key",
		Text:    "Favicon key document text",
		Favicon: faviconData,
	}); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	res, err := idx.Search(&Query{Text: "Favicon key"})
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

	data, err := idx.ReadFavicon(doc.FaviconKey)
	if err != nil {
		t.Fatalf("ReadFavicon failed: %v", err)
	}
	if string(data) != faviconData {
		t.Fatalf("ReadFavicon = %q, want %q", string(data), faviconData)
	}
}

func TestNestedDocumentsRespectRulesAfterManualIndexing(t *testing.T) {
	for _, batch := range []bool{false, true} {
		name := "single"
		if batch {
			name = "batch"
		}
		t.Run(name, func(t *testing.T) {
			idx := newTestIndexer(t, testutil.Config(t))
			t.Cleanup(idx.Close)
			rules := &config.Rules{
				Allow: &config.Rule{ReStrs: []string{`^https://allowed\.example/`}},
				Skip:  &config.Rule{ReStrs: []string{`/private$`}},
			}
			if err := rules.Compile(); err != nil {
				t.Fatal(err)
			}
			for _, submission := range []struct {
				text   string
				manual bool
			}{
				{"initial automatic submission", false},
				{"manual submission", true},
				{"later automatic submission", false},
			} {
				doc := &document.Document{
					URL:  "https://allowed.example/root",
					Text: submission.text,
					ExtraDocuments: []*document.Document{{
						URL:  "https://allowed.example/child",
						Text: submission.text,
						ExtraDocuments: []*document.Document{
							{URL: "https://outside.example/reply", Text: submission.text},
							{URL: "https://allowed.example/private", Text: submission.text},
						},
					}},
				}
				if submission.manual {
					doc.SetIgnoreSkipRules(true)
				}
				if batch {
					b := idx.NewMultiBatch()
					if err := b.AddContext(context.Background(), doc, WithRules(rules)); err != nil {
						t.Fatal(err)
					}
					if err := b.Save(); err != nil {
						t.Fatal(err)
					}
				} else if err := idx.AddContext(context.Background(), doc, WithRules(rules)); err != nil {
					t.Fatal(err)
				}
				for _, rawURL := range []string{doc.URL, doc.ExtraDocuments[0].URL} {
					stored := idx.GetByURLAndUser(rawURL, 0)
					if stored == nil || stored.Text != submission.text {
						t.Fatalf("allowed document %s was not updated", rawURL)
					}
				}
				for _, extra := range doc.ExtraDocuments[0].ExtraDocuments {
					stored := idx.GetByURLAndUser(extra.URL, 0)
					if submission.text == "initial automatic submission" {
						if stored != nil {
							t.Fatalf("excluded document %s was indexed automatically", extra.URL)
						}
						continue
					}
					if stored == nil || stored.Text != "manual submission" || !stored.IgnoreSkipRules() {
						t.Fatalf("excluded document %s must retain only its manual submission", extra.URL)
					}
				}
			}
		})
	}
}

func TestSearchReturnsKeywordResultsAfterQueryEmbeddingTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)
	cfg := testutil.Config(t)
	idx := newTestIndexer(t, cfg)
	defer idx.Close()
	doc := &document.Document{URL: "https://example.com/fallback", Title: "Fallback document", Text: "fallback", Processed: true}
	if err := idx.Add(doc); err != nil {
		t.Fatal(err)
	}
	idx.embedder = vectorstore.NewEmbedder(&config.SemanticSearch{
		EmbeddingEndpoint: server.URL, EmbeddingModel: "test", Dimensions: 3, QueryEmbeddingTimeout: 1,
	})
	idx.vectorStore = &metadataVectorStore{}
	result, err := idx.Search(&Query{Text: "fallback", SemanticEnabled: true})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(result.Documents) != 1 || result.Documents[0].URL != doc.URL {
		t.Fatalf("keyword results = %#v, want %s", result.Documents, doc.URL)
	}
	if len(result.SemanticHits) != 0 {
		t.Fatalf("semantic hits = %#v, want none", result.SemanticHits)
	}
	if !result.SemanticEnabled {
		t.Fatal("search did not attempt semantic embedding")
	}
}

func TestSemanticDateFilters(t *testing.T) {
	var requests atomic.Int64
	server := embeddingTestServer(t, &requests)
	defer server.Close()
	idx := newTestIndexer(t, snippetConfig(t, 80))
	defer idx.Close()
	store := &snippetVectorStore{}
	for _, updated := range []int64{100, 200, 300} {
		for _, title := range []string{"keyword", "semantic"} {
			doc := &document.Document{
				URL: fmt.Sprintf("https://example.com/%s/%d", title, updated), Title: title,
				Text: "Document content", Added: 50, Updated: updated, Processed: true,
			}
			if err := idx.Add(doc); err != nil {
				t.Fatal(err)
			}
			store.results = append(store.results, vectorstore.Result{DocID: doc.ID(), ChunkText: "content", Similarity: 1})
		}
	}
	idx.embedder = vectorstore.NewEmbedder(&config.SemanticSearch{
		EmbeddingEndpoint: server.URL, EmbeddingModel: "test", Dimensions: 2,
	})
	idx.vectorStore = store
	for _, tc := range []struct {
		name     string
		from, to int64
		want     []int64
	}{
		{"unfiltered", 0, 0, []int64{100, 200, 300}},
		{"from inclusive", 200, 0, []int64{200, 300}},
		{"to inclusive", 0, 200, []int64{100, 200}},
		{"range", 150, 250, []int64{200}},
		{"exact timestamp", 200, 200, []int64{200}},
		{"future from only", 1893456000, 0, nil},
		{"future", 1893456000, 1893542400, nil},
		{"before documents", 0, 99, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := idx.Search(&Query{Text: "keyword", SemanticEnabled: true, DateFrom: tc.from, DateTo: tc.to})
			if err != nil {
				t.Fatal(err)
			}
			var keywordDates, semanticDates []int64
			for _, doc := range res.Documents {
				keywordDates = append(keywordDates, doc.Updated)
			}
			for _, hit := range res.SemanticHits {
				if hit.Document != nil {
					semanticDates = append(semanticDates, hit.Document.Updated)
				}
			}
			slices.Sort(keywordDates)
			slices.Sort(semanticDates)
			if !slices.Equal(keywordDates, tc.want) {
				t.Errorf("keyword dates = %v, want %v", keywordDates, tc.want)
			}
			if !slices.Equal(semanticDates, tc.want) {
				t.Errorf("semantic dates = %v, want %v", semanticDates, tc.want)
			}
			if want := 2 * len(tc.want); len(res.SemanticHits) != want {
				t.Errorf("semantic hit count = %d, want %d", len(res.SemanticHits), want)
			}
			if want := uint64(2 * len(tc.want)); res.Total != want {
				t.Errorf("total = %d, want %d", res.Total, want)
			}
		})
	}
}

func TestSearchPriorityRulesUseGoRegexpSemantics(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()

	const ordinaryURL = "https://example.com/guide"
	const priorityURL = "https://wiki.example.com/guide"
	for _, d := range []*document.Document{
		{URL: ordinaryURL, Title: "Granite guide", Text: "Granite and quartz", Added: 200, Processed: true},
		{URL: priorityURL, Title: "Granite guide", Text: "Granite and quartz", Added: 100, Language: "en", Processed: true},
	} {
		if err := idx.Add(d); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range []struct {
		name     string
		patterns []string
		firstURL string
	}{
		{"full URL", []string{`https://wiki\.example\.com/.*`}, priorityURL},
		{"substring", []string{`wiki\.example\.com`}, priorityURL},
		{"prefix anchor", []string{`^https://wiki\.example\.com/`}, priorityURL},
		{"suffix anchor", []string{`wiki\.example\.com/guide$`}, priorityURL},
		{"word boundary", []string{`\bwiki\b`}, priorityURL},
		{"lazy quantifier", []string{`wiki.*?guide`}, priorityURL},
		{"empty alongside matching", []string{"", `\bwiki\b`}, priorityURL},
		{"no matches", []string{`missing\.example$`}, ""},
		{"empty", []string{""}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := &config.Rule{ReStrs: tc.patterns}
			if err := rule.Compile(); err != nil {
				t.Fatalf("valid rule rejected: %v", err)
			}
			for _, text := range []string{"*", "granite"} {
				t.Run(text, func(t *testing.T) {
					result, err := idx.Search(&Query{Text: text, PriorityPatterns: tc.patterns})
					if err != nil {
						t.Fatal(err)
					}
					if result.Total != 2 || len(result.Documents) != 2 {
						t.Fatalf("got %d total and %d documents, want both matches", result.Total, len(result.Documents))
					}
					if tc.firstURL != "" && result.Documents[0].URL != tc.firstURL {
						t.Errorf("first URL = %q, want priority URL %q", result.Documents[0].URL, tc.firstURL)
					}
				})
			}
		})
	}

	first, err := idx.Search(&Query{Text: "granite", PriorityPatterns: []string{`\bwiki\b`}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Documents) != 1 || first.Documents[0].URL != priorityURL || first.PageKey == "" {
		t.Fatalf("first page = %+v, want priority document and continuation", first)
	}
	second, err := idx.Search(&Query{Text: "granite", PriorityPatterns: []string{`\bwiki\b`}, Limit: 1, PageKey: first.PageKey})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Documents) != 1 || second.Documents[0].URL != ordinaryURL {
		t.Fatalf("second page = %+v, want ordinary document", second)
	}
}

func TestSearchPriorityRulesRespectFiltersAndSort(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()

	const ordinaryURL = "https://example.com/guide"
	const priorityURL = "https://wiki.example.com/guide"
	for _, d := range []*document.Document{
		{URL: ordinaryURL, Text: "Granite", Added: 200, UserID: 1, Processed: true},
		{URL: priorityURL, Text: "Granite", Added: 100, UserID: 0, Language: "en", Processed: true},
		{URL: "https://wiki.example.com/private", Text: "Granite", Added: 300, UserID: 2, Processed: true},
		{URL: "https://wiki.example.com/unrelated", Text: "Basalt", Added: 400, UserID: 1, Processed: true},
	} {
		if err := idx.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		text, sort, firstURL string
		total                uint64
	}{
		{"granite", "", priorityURL, 2},
		{"granite", "date", ordinaryURL, 2},
		{`granite -url:"https://wiki.example.com/guide"`, "", ordinaryURL, 1},
	} {
		t.Run(tc.text+"/"+tc.sort, func(t *testing.T) {
			result, err := idx.Search(&Query{Text: tc.text, Sort: tc.sort, UserID: 1, PriorityPatterns: []string{`\bwiki\b`}})
			if err != nil {
				t.Fatal(err)
			}
			if result.Total != tc.total || len(result.Documents) != int(tc.total) {
				t.Fatalf("got %d total and %d documents, want %d", result.Total, len(result.Documents), tc.total)
			}
			if result.Documents[0].URL != tc.firstURL {
				t.Errorf("first URL = %q, want %q", result.Documents[0].URL, tc.firstURL)
			}
		})
	}
}

func TestSearchRejectsInvalidPriorityRegexp(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	if _, err := idx.Search(&Query{Text: "*", PriorityPatterns: []string{"["}}); err == nil {
		t.Fatal("invalid priority regexp did not return an error")
	}
}

type failingSearchIndex struct {
	bleveIndex
	err error
}

func (i failingSearchIndex) SearchInContext(context.Context, *bleve.SearchRequest) (*bleve.SearchResult, error) {
	return nil, i.err
}

func TestSearchReturnsIndexFailures(t *testing.T) {
	for _, allFail := range []bool{false, true} {
		name := "partial failure"
		if allFail {
			name = "all indexes fail"
		}
		t.Run(name, func(t *testing.T) {
			idx := newTestIndexer(t, testutil.Config(t))
			defer idx.Close()
			for _, language := range []string{"", "en"} {
				if err := idx.Add(&document.Document{
					URL: "https://example.com/" + language, Text: "Granite", Language: language, Processed: true,
				}); err != nil {
					t.Fatal(err)
				}
			}
			failure := errors.New("test search failure")
			for name, index := range idx.indexes() {
				if allFail || name == indexNameForLanguage("en") {
					idx.idx.Remove(index)
					idx.idx.Add(failingSearchIndex{bleveIndex: index, err: failure})
				}
			}
			result, err := idx.Search(&Query{Text: "*"})
			if !errors.Is(err, failure) {
				t.Fatalf("Search error = %v, want underlying index failure", err)
			}
			if !strings.Contains(err.Error(), indexNameForLanguage("en")) {
				t.Errorf("Search error does not identify the failing index: %v", err)
			}
			if result != nil {
				t.Errorf("failed search returned apparently successful results: %+v", result)
			}
		})
	}
}

// BenchmarkPrioritySearch compares the old regexp query implementation with
// scoring candidate URLs. Patterns use syntax supported by both engines and
// match the same URLs, so failures are never mistaken for fast searches.
func BenchmarkPrioritySearch(b *testing.B) {
	for _, count := range []int{10_000, 40_000} {
		b.Run(fmt.Sprintf("documents=%d", count), func(b *testing.B) {
			idx := priorityBenchmarkIndexer(b, count)
			for _, tc := range []struct {
				name, text string
				total      uint64
			}{
				{"selective", "text:needle", uint64((count-1)/101 + 1)},
				{"common", "text:granite", uint64(count)},
				{"all", "*", uint64(count)},
			} {
				b.Run(tc.name, func(b *testing.B) {
					for _, ruleCount := range []int{0, 1, 10, 50} {
						patterns := make([]string, ruleCount)
						for n := range patterns {
							patterns[n] = fmt.Sprintf(`https://site%03d\.example/.*`, n)
						}
						implementations := []string{"old", "new"}
						if ruleCount == 0 {
							implementations = []string{"none"}
						}
						for _, implementation := range implementations {
							b.Run(fmt.Sprintf("rules=%d/%s", ruleCount, implementation), func(b *testing.B) {
								b.ReportAllocs()
								for b.Loop() {
									base, err := (&Query{Text: tc.text}).create(tc.text)
									if err != nil {
										b.Fatal(err)
									}
									switch implementation {
									case "old":
										boosted := query.NewBooleanQuery([]query.Query{base}, nil, nil)
										for _, pattern := range patterns {
											rq := bleve.NewRegexpQuery(pattern)
											rq.SetField("url")
											rq.SetBoost(100)
											boosted.AddShould(rq)
										}
										base = boosted
									case "new":
										base, err = boostPriorityURLs(base, patterns)
										if err != nil {
											b.Fatal(err)
										}
									}
									req := bleve.NewSearchRequest(base)
									req.Size = 100
									req.Fields = allFields
									req.SortBy(searchschema.Sort("").Fields)
									req.Highlight = bleve.NewHighlight()
									req.Highlight.Fields = []string{"text"}
									result, err := idx.searchIndexes(req)
									if err != nil {
										b.Fatal(err)
									}
									if result.Total != tc.total {
										b.Fatalf("got %d matches, want %d", result.Total, tc.total)
									}
								}
							})
						}
					}
				})
			}
		})
	}
}

func priorityBenchmarkIndexer(b *testing.B, count int) *Indexer {
	b.Helper()
	dir := b.TempDir()
	idx, err := initializeIndexer(dir, indexOptions{DetectLanguages: true, KeepStopwords: false}, "")
	if err != nil {
		b.Fatal(err)
	}
	defer idx.Close()
	for shard, language := range []string{"", "en"} {
		index := idx.getOrCreate(language)
		batch := index.NewBatch()
		for n := shard; n < count; n += 2 {
			text := "Granite quartz mineral geology mountain stone crystal research guide laboratory notes"
			if n%101 == 0 {
				text += " needle"
			}
			doc := &document.Document{
				URL:      fmt.Sprintf("https://site%03d.example/articles/%08d", n%100, n),
				Domain:   fmt.Sprintf("site%03d.example", n%100),
				Title:    "Geology reference",
				Text:     text,
				Added:    int64(1_700_000_000 + n),
				Updated:  int64(1_700_000_000 + n),
				Language: language,
			}
			if err := batch.Index(doc.ID(), doc); err != nil {
				b.Fatal(err)
			}
		}
		if err := index.Batch(batch); err != nil {
			b.Fatal(err)
		}
	}
	idx.Close()
	// Reopen persisted indexes so background writes do not skew query timings.
	idx, err = initializeIndexer(dir, indexOptions{DetectLanguages: true, KeepStopwords: false}, "")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(idx.Close)
	return idx
}
