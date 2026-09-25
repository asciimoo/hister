// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/model"
)

// graphFetcher serves a fixed link graph and records every URL it fetched.
type graphFetcher struct {
	graph map[string][]string

	mu      sync.Mutex
	fetched []string
}

func (f *graphFetcher) fetchPage(_ context.Context, rawURL string) (string, []byte, []Link, FetchMeta, error) {
	f.mu.Lock()
	f.fetched = append(f.fetched, rawURL)
	f.mu.Unlock()

	links := make([]Link, 0, len(f.graph[rawURL]))
	for _, href := range f.graph[rawURL] {
		links = append(links, Link{Href: href})
	}
	return rawURL, []byte("<html></html>"), links, FetchMeta{StatusCode: 200}, nil
}

func (f *graphFetcher) close() error { return nil }

func (f *graphFetcher) fetchedURLs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.fetched...)
	sort.Strings(out)
	return out
}

func testCrawlerConfig(concurrency int) *config.CrawlerConfig {
	return &config.CrawlerConfig{
		Rate: config.CrawlerRate{
			GlobalRPS:          1000,
			PerHostRPS:         1000,
			GlobalConcurrency:  concurrency,
			PerHostConcurrency: concurrency,
		},
		ShutdownGrace: 1,
	}
}

func newGraphCrawler(concurrency int, graph map[string][]string) (*baseCrawler, *graphFetcher) {
	f := &graphFetcher{graph: graph}
	return newBaseCrawler(f, testCrawlerConfig(concurrency), nil, options{}), f
}

func drain(t *testing.T, ch <-chan *document.Document) int {
	t.Helper()
	var docs int
	for range ch {
		docs++
	}
	return docs
}

// TestMaxLinksOneVisitsOnlyStartURL pins the meaning of MaxLinks: it counts
// pages fetched, including the start URL. It used to be charged at link
// discovery time while the start URL escaped the check entirely, so MaxLinks: 1
// fetched the start page plus one discovered link.
func TestMaxLinksOneVisitsOnlyStartURL(t *testing.T) {
	start := "http://example.com/"
	bc, f := newGraphCrawler(1, map[string][]string{
		start: {
			"http://example.com/1",
			"http://example.com/2",
			"http://example.com/3",
			"http://example.com/4",
			"http://example.com/5",
		},
	})

	v, err := NewValidator(&ValidatorRules{MaxLinks: 1})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := bc.Crawl(context.Background(), start, v)
	if err != nil {
		t.Fatal(err)
	}
	if docs := drain(t, ch); docs != 1 {
		t.Errorf("emitted %d documents with MaxLinks=1, want 1", docs)
	}

	got := f.fetchedURLs()
	if len(got) != 1 || got[0] != start {
		t.Errorf("fetched %v with MaxLinks=1, want only %q", got, start)
	}
}

// TestMaxLinksNotConsumedByDuplicateLinks covers the second half of the same
// bug: the budget was charged per discovered href, so a URL linked from two
// pages consumed two units even though the queue deduplicated it to one fetch.
func TestMaxLinksNotConsumedByDuplicateLinks(t *testing.T) {
	start := "http://example.com/"
	a := "http://example.com/a"
	b := "http://example.com/b"
	c := "http://example.com/c"

	// /a links to /b three times: under per-href accounting those duplicates
	// exhausted the budget and /c was never enqueued.
	bc, f := newGraphCrawler(1, map[string][]string{
		start: {a, b},
		a:     {b, b, b, c},
	})

	v, err := NewValidator(&ValidatorRules{MaxLinks: 4})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := bc.Crawl(context.Background(), start, v)
	if err != nil {
		t.Fatal(err)
	}
	if docs := drain(t, ch); docs != 4 {
		t.Errorf("emitted %d documents with MaxLinks=4, want 4", docs)
	}

	got := f.fetchedURLs()
	want := []string{start, a, b, c}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("fetched %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fetched %v, want %v", got, want)
		}
	}
}

// TestMaxLinksWithConcurrentWorkers verifies the budget holds when several
// workers claim URLs at once.
func TestMaxLinksWithConcurrentWorkers(t *testing.T) {
	start := "http://example.com/"
	graph := map[string][]string{}
	var startLinks []string
	for i := 0; i < 20; i++ {
		u := "http://example.com/" + string(rune('a'+i))
		startLinks = append(startLinks, u)
		graph[u] = startLinks
	}
	graph[start] = startLinks

	bc, f := newGraphCrawler(4, graph)

	v, err := NewValidator(&ValidatorRules{MaxLinks: 5})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := bc.Crawl(context.Background(), start, v)
	if err != nil {
		t.Fatal(err)
	}
	docs := drain(t, ch)

	if got := len(f.fetchedURLs()); got > 5 {
		t.Errorf("fetched %d pages with MaxLinks=5 and 4 workers, want at most 5", got)
	}
	if docs > 5 {
		t.Errorf("emitted %d documents with MaxLinks=5, want at most 5", docs)
	}
}

// TestCrawlUnblocksWorkersOnCancel covers callers that take a single document
// and stop reading: cancelling the context must let the parked worker finish so
// the channel closes and the crawl goroutine exits.
func TestCrawlUnblocksWorkersOnCancel(t *testing.T) {
	start := "http://example.com/"
	bc, _ := newGraphCrawler(1, map[string][]string{
		start:                  {"http://example.com/a", "http://example.com/b"},
		"http://example.com/a": {"http://example.com/c"},
		"http://example.com/b": {"http://example.com/d"},
	})

	v, err := NewValidator(&ValidatorRules{})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := bc.Crawl(ctx, start, v)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := <-ch; !ok {
		t.Fatal("crawl produced no documents")
	}
	cancel()

	closed := make(chan struct{})
	go func() {
		for range ch {
		}
		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("document channel never closed; a worker is parked on an unread send")
	}
}

func TestStartURLRejectedByRules(t *testing.T) {
	bc, f := newGraphCrawler(1, nil)

	v, err := NewValidator(&ValidatorRules{ExcludeDomains: []string{"example.com"}})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := bc.Crawl(context.Background(), "http://example.com/", v); err == nil {
		t.Fatal("Crawl accepted a start URL excluded by its own rules")
	}
	if got := len(f.fetchedURLs()); got != 0 {
		t.Errorf("fetched %d pages for a rejected start URL, want 0", got)
	}
}

// TestPersistentMaxLinksLeavesRemainderPending checks that stopping on the
// budget does not burn the queue: unfetched rows stay pending so a later run
// with a larger budget can pick them up.
func TestPersistentMaxLinksLeavesRemainderPending(t *testing.T) {
	initTestDB(t)

	jobID := "max-links-stop-test"
	start := "http://example.com/"
	if err := model.CreateCrawlJob(jobID, start, "", "test"); err != nil {
		t.Fatalf("CreateCrawlJob: %v", err)
	}

	f := &graphFetcher{graph: map[string][]string{
		start: {"http://example.com/a", "http://example.com/b"},
	}}
	cfg := testCrawlerConfig(1)
	pc := &persistentCrawler{
		baseCrawler: &baseCrawler{
			fetcher: f,
			cfg:     cfg,
			coord:   NewCoordinator(cfg),
			backoff: NewBackoff(time.Second, 30*time.Second),
		},
		jobID: jobID,
	}

	v, err := NewValidator(&ValidatorRules{MaxLinks: 1})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := pc.Crawl(context.Background(), start, v)
	if err != nil {
		t.Fatal(err)
	}
	if docs := drain(t, ch); docs != 1 {
		t.Errorf("emitted %d documents with MaxLinks=1, want 1", docs)
	}
	if err := pc.Err(); err != nil {
		t.Fatalf("crawl error: %v", err)
	}

	done, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLDone)
	if err != nil {
		t.Fatal(err)
	}
	if done != 1 {
		t.Errorf("done URLs = %d, want 1", done)
	}

	pending, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLPending)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 2 {
		t.Errorf("pending URLs = %d, want 2 (discovered links must survive the stop)", pending)
	}

	failed, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLFailed)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 0 {
		t.Errorf("failed URLs = %d, want 0", failed)
	}

	// A job stopped by its budget still has work queued, so it must not be
	// reported as completed: the CLI refuses to resume completed jobs.
	job, err := model.GetCrawlJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != model.CrawlJobInterrupted {
		t.Errorf("job status = %q, want %q", job.Status, model.CrawlJobInterrupted)
	}

	// Resuming with a larger budget picks the remaining URLs up.
	resumeFetcher := &graphFetcher{graph: map[string][]string{}}
	resume := &persistentCrawler{
		baseCrawler: &baseCrawler{
			fetcher: resumeFetcher,
			cfg:     cfg,
			coord:   NewCoordinator(cfg),
			backoff: NewBackoff(time.Second, 30*time.Second),
		},
		jobID: jobID,
	}
	resumeValidator, err := NewValidator(&ValidatorRules{MaxLinks: 3})
	if err != nil {
		t.Fatal(err)
	}
	resumeValidator.SetVisited(int(done))

	ch2, err := resume.Crawl(context.Background(), start, resumeValidator)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if docs := drain(t, ch2); docs != 2 {
		t.Errorf("resume emitted %d documents, want 2", docs)
	}

	job, err = model.GetCrawlJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != model.CrawlJobCompleted {
		t.Errorf("after resume: job status = %q, want %q", job.Status, model.CrawlJobCompleted)
	}
}
