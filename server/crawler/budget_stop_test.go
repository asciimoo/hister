// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/model"
)

// gatedFetcher lets the first passThrough fetches complete immediately and
// parks every later one until the test releases them. That holds several
// workers mid-fetch while another runs the budget out, which is the race the
// stop has to survive: their pages are already reserved and paid for, so their
// documents must still be delivered.
type gatedFetcher struct {
	passThrough int
	release     chan struct{}
	started     chan string

	mu      sync.Mutex
	fetched []string
	links   []Link
}

func (f *gatedFetcher) fetchPage(_ context.Context, rawURL string) (string, []byte, []Link, FetchMeta, error) {
	f.mu.Lock()
	f.fetched = append(f.fetched, rawURL)
	n := len(f.fetched)
	links := f.links
	f.mu.Unlock()

	if n > f.passThrough {
		select {
		case f.started <- rawURL:
		default:
		}
		<-f.release
	}
	return rawURL, []byte("<html></html>"), links, FetchMeta{StatusCode: 200}, nil
}

func (f *gatedFetcher) close() error { return nil }

func (f *gatedFetcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.fetched)
}

// TestMaxLinksAllowsReservedWorkToFinish pins the difference between stopping
// intake and cancelling the crawl. Workers that already reserved a unit of the
// MaxLinks budget and are only waiting on a rate token must still fetch: the
// stop used to cancel the crawl context out from under them, so a run with four
// workers and MaxLinks=2 delivered only the seed page.
func TestMaxLinksAllowsReservedWorkToFinish(t *testing.T) {
	const (
		workers  = 4
		maxLinks = 2
	)
	start := "http://example.com/"
	graph := map[string][]string{
		start: {
			"http://example.com/1",
			"http://example.com/2",
			"http://example.com/3",
			"http://example.com/4",
			"http://example.com/5",
		},
	}
	bc, f := newGraphCrawler(workers, graph)

	v, err := NewValidator(&ValidatorRules{MaxLinks: maxLinks})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := bc.Crawl(context.Background(), start, v)
	if err != nil {
		t.Fatal(err)
	}
	docs := drain(t, ch)

	if docs != maxLinks {
		t.Errorf("emitted %d documents with MaxLinks=%d and %d workers, want exactly %d: "+
			"a spent budget must not cancel work that is already reserved", docs, maxLinks, workers, maxLinks)
	}
	if got := f.fetchedURLs(); len(got) != maxLinks {
		t.Errorf("fetched %v, want exactly %d URLs", got, maxLinks)
	}
	if err := bc.Err(); err != nil {
		t.Errorf("crawl error = %v, want nil", err)
	}
}

// TestMaxPagesAllowsReservedWorkToFinish is the same guarantee for the
// crawl-wide page budget, which travels a different code path: the stop comes
// from TryReservePage rather than from TryVisit. The workers parked mid-fetch
// have already reserved and paid for their pages, so cancelling the crawl out
// from under them throws away pages the budget was charged for - their sends
// lose the race with the cancelled context and the documents are dropped.
func TestMaxPagesAllowsReservedWorkToFinish(t *testing.T) {
	const (
		workers  = 4
		maxPages = 4
	)
	start := "http://example.com/"
	links := []Link{
		{Href: "http://example.com/1"},
		{Href: "http://example.com/2"},
		{Href: "http://example.com/3"},
		{Href: "http://example.com/4"},
	}
	// Only the seed passes straight through; the three workers that reserve the
	// remaining pages park until the fourth has run the budget out.
	f := &gatedFetcher{
		passThrough: 1,
		release:     make(chan struct{}),
		started:     make(chan string, workers),
		links:       links,
	}
	cfg := testCrawlerConfig(workers)
	cfg.Limits = config.CrawlerLimits{MaxPages: maxPages}
	bc := newBaseCrawler(f, cfg, nil, options{})

	v, err := NewValidator(&ValidatorRules{})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := bc.Crawl(context.Background(), start, v)
	if err != nil {
		t.Fatal(err)
	}
	docs := make(chan int, 1)
	go func() {
		docs <- drain(t, ch)
	}()

	// Wait until the budget is fully reserved, then give the fourth worker time
	// to be turned away and stop the crawl, before letting the parked fetches
	// finish.
	select {
	case <-f.started:
	case <-time.After(10 * time.Second):
		t.Fatal("no worker parked in fetch")
	}
	deadline := time.Now().Add(10 * time.Second)
	for bc.coord.globalPages.Load() < maxPages && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := bc.coord.globalPages.Load(); got != maxPages {
		t.Fatalf("reserved pages = %d, want %d", got, maxPages)
	}
	time.Sleep(200 * time.Millisecond)
	close(f.release)

	if n := <-docs; n != maxPages {
		t.Errorf("emitted %d documents with max_pages=%d and %d workers, want exactly %d: "+
			"pages the budget already paid for must still be delivered", n, maxPages, workers, maxPages)
	}
	if n := f.count(); n != maxPages {
		t.Errorf("fetched %d pages, want exactly %d", n, maxPages)
	}
}

// TestBudgetStopReleasesVisitReservation covers the reservation leak behind the
// stop. The crawl-wide page budget is checked after MaxLinks was already
// charged, so a URL turned away there has to hand its MaxLinks unit back -
// otherwise the two budgets interfere and a resumed run starts short.
func TestBudgetStopReleasesVisitReservation(t *testing.T) {
	start := "http://example.com/"
	f := &graphFetcher{graph: map[string][]string{
		start: {"http://example.com/1", "http://example.com/2"},
	}}
	cfg := testCrawlerConfig(1)
	cfg.Limits = config.CrawlerLimits{MaxPages: 1}
	bc := newBaseCrawler(f, cfg, nil, options{})

	v, err := NewValidator(&ValidatorRules{MaxLinks: 10})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := bc.Crawl(context.Background(), start, v)
	if err != nil {
		t.Fatal(err)
	}
	drain(t, ch)

	// One page was fetched; the URL rejected by the page budget must have
	// returned the MaxLinks unit it had already taken.
	v.mu.Lock()
	visited := v.visited
	v.mu.Unlock()
	if visited != 1 {
		t.Errorf("visited counter = %d, want 1: a URL stopped by the page budget must release its MaxLinks reservation", visited)
	}
}

// flakyFetcher fails the first failures fetches of each URL, then succeeds.
type flakyFetcher struct {
	failures int32
	calls    atomic.Int32
}

func (f *flakyFetcher) fetchPage(_ context.Context, rawURL string) (string, []byte, []Link, FetchMeta, error) {
	if f.calls.Add(1) <= f.failures {
		return "", nil, nil, FetchMeta{}, &HTTPStatusError{Status: 503}
	}
	return rawURL, []byte("<html></html>"), nil, FetchMeta{StatusCode: 200}, nil
}

func (f *flakyFetcher) close() error { return nil }

// TestRetriesChargeThePageBudgetOnce covers retries against the page budget.
// The reservation used to be taken inside the retry loop and never rolled back,
// so a single page that needed two retries spent three units of max_pages and a
// max_pages=1 crawl returned nothing at all.
func TestRetriesChargeThePageBudgetOnce(t *testing.T) {
	f := &flakyFetcher{failures: 2}
	cfg := testCrawlerConfig(1)
	cfg.Limits = config.CrawlerLimits{MaxPages: 1}
	cfg.Retry = config.CrawlerRetry{MaxAttempts: 3}
	bc := newBaseCrawler(f, cfg, nil, options{})
	bc.backoff = NewBackoff(time.Millisecond, time.Millisecond)

	v, err := NewValidator(&ValidatorRules{NoDepth: true})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := bc.Crawl(context.Background(), "http://example.com/", v)
	if err != nil {
		t.Fatal(err)
	}
	if docs := drain(t, ch); docs != 1 {
		t.Errorf("emitted %d documents, want 1: retries of one page must not each spend a unit of max_pages", docs)
	}
	if calls := f.calls.Load(); calls != 3 {
		t.Errorf("fetcher called %d times, want 3 (two failures then a success)", calls)
	}
	if got := bc.coord.globalPages.Load(); got != 1 {
		t.Errorf("page counter = %d, want 1", got)
	}
}

// TestMaxDurationStopsDuringCooldown covers the duration limit while every
// worker is parked. The limit used to be consulted only between items, so a
// Retry-After cooldown longer than max_duration ran straight past it.
func TestMaxDurationStopsDuringCooldown(t *testing.T) {
	const (
		maxDuration = 2 * time.Second
		cooldown    = time.Hour
	)
	f := &gatedFetcher{release: make(chan struct{}), started: make(chan string, 4)}
	close(f.release) // never block; the cooldown is what parks the workers

	cfg := testCrawlerConfig(1)
	cfg.Limits = config.CrawlerLimits{MaxDuration: int(maxDuration / time.Second)}
	cfg.ShutdownGrace = 1
	bc := newBaseCrawler(f, cfg, nil, options{})

	// Park the host before the crawl starts: StartRun restamps the clock, so
	// the deadline is measured from the run rather than from construction.
	bc.coord.Cooldown("example.com", cooldown)

	v, err := NewValidator(&ValidatorRules{NoDepth: true})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	ch, err := bc.Crawl(context.Background(), "http://example.com/", v)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		drain(t, ch)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(maxDuration + 10*time.Second):
		// Without the limit the crawl sits out the full hour-long cooldown.
		t.Fatalf("crawl still running after %s with max_duration=%s: the limit must interrupt a cooldown, not wait it out",
			time.Since(start), maxDuration)
	}
	elapsed := time.Since(start)

	if elapsed < maxDuration {
		t.Errorf("crawl ended after %s, want at least max_duration=%s", elapsed, maxDuration)
	}
	if n := f.count(); n != 0 {
		t.Errorf("fetched %d pages, want 0: the host was in cooldown for the whole run", n)
	}
	// Reaching a configured limit is a stopping condition, not a failure.
	if err := bc.Err(); err != nil {
		t.Errorf("crawl error = %v, want nil: max_duration is a configured limit, not a failure", err)
	}
}

// TestMaxDurationRecordsJobInterrupted is the persistent half of the limit: the
// run stops with URLs still pending, so the job has to stay resumable rather
// than be recorded as completed.
func TestMaxDurationRecordsJobInterrupted(t *testing.T) {
	jobID := "max-duration-stop"
	urls := []string{"http://example.com/a", "http://example.com/b"}
	bc, f := newFailureTestCrawler(t, jobID, urls)
	bc.cfg.Limits = config.CrawlerLimits{MaxDuration: 1}
	bc.coord = NewCoordinator(bc.cfg)
	bc.coord.Cooldown("example.com", time.Hour)

	v, err := NewValidator(&ValidatorRules{NoDepth: true})
	if err != nil {
		t.Fatal(err)
	}

	runDone := make(chan error, 1)
	go func() { runDone <- runQueue(t, bc, newSQLiteQueue(jobID), urls[0], v) }()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("run did not stop at max_duration: it is sitting out the cooldown instead")
	}
	if calls := f.calls.Load(); calls != 0 {
		t.Errorf("fetcher called %d times, want 0", calls)
	}

	job, err := model.GetCrawlJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != model.CrawlJobInterrupted {
		t.Errorf("job status = %q, want %q: a run cut short by max_duration must stay resumable", job.Status, model.CrawlJobInterrupted)
	}

	pending, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLPending)
	if err != nil {
		t.Fatal(err)
	}
	if pending != int64(len(urls)) {
		t.Errorf("pending URLs = %d, want %d", pending, len(urls))
	}
}
