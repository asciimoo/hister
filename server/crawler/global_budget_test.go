// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/model"
)

// blockingFetcher holds its first fetch open until the test releases it, so a
// second worker is guaranteed to reach the page reservation while the crawl
// budget is already spent.
type blockingFetcher struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int64
}

func (f *blockingFetcher) fetchPage(_ context.Context, rawURL string) (string, []byte, []Link, FetchMeta, error) {
	if f.calls.Add(1) == 1 {
		close(f.started)
		<-f.release
	}
	return rawURL, []byte("<html></html>"), nil, FetchMeta{StatusCode: 200}, nil
}

func (f *blockingFetcher) close() error { return nil }

// TestGlobalBudgetLeavesRemainingURLsQueued covers the crawl-wide page budget
// running out while another worker is mid-flight. That worker's URL was never
// fetched, so it has to stay queued: recording it as skipped retires it for
// good even though a resume would have budget to spend on it.
func TestGlobalBudgetLeavesRemainingURLsQueued(t *testing.T) {
	initTestDB(t)

	jobID := "global-budget-stop"
	urls := []string{"http://example.com/a", "http://example.com/b"}
	if err := model.CreateCrawlJob(jobID, urls[0], "", "test"); err != nil {
		t.Fatalf("CreateCrawlJob: %v", err)
	}
	if err := model.BulkInsertCrawlURLs(jobID, urls, 0); err != nil {
		t.Fatalf("BulkInsertCrawlURLs: %v", err)
	}

	cfg := &config.CrawlerConfig{
		Rate: config.CrawlerRate{
			GlobalRPS:          1000,
			PerHostRPS:         1000,
			GlobalConcurrency:  2,
			PerHostConcurrency: 2,
		},
		Retry:         config.CrawlerRetry{MaxAttempts: 1},
		Limits:        config.CrawlerLimits{MaxPages: 1},
		ShutdownGrace: 1,
	}
	f := &blockingFetcher{started: make(chan struct{}), release: make(chan struct{})}
	pc := &persistentCrawler{
		baseCrawler: &baseCrawler{
			fetcher: f,
			cfg:     cfg,
			coord:   NewCoordinator(cfg),
			backoff: NewBackoff(time.Millisecond, time.Millisecond),
		},
		jobID: jobID,
	}

	v, err := NewValidator(&ValidatorRules{NoDepth: true})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := pc.Crawl(context.Background(), urls[0], v)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	docs := make(chan int, 1)
	go func() {
		var n int
		for range ch {
			n++
		}
		docs <- n
	}()

	// Hold the first fetch until the other worker has spent its attempt against
	// the exhausted budget.
	<-f.started
	time.Sleep(100 * time.Millisecond)
	close(f.release)

	// The in-flight page may or may not be delivered before the stop cancels
	// the crawl, but the budget must hold either way.
	if n := <-docs; n > 1 {
		t.Errorf("emitted %d documents with max_pages=1, want at most 1", n)
	}

	skipped, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLSkipped)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 {
		t.Errorf("skipped URLs = %d, want 0: the crawl budget must not retire a URL it never fetched", skipped)
	}

	pending, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLPending)
	if err != nil {
		t.Fatal(err)
	}
	if pending < 1 {
		t.Errorf("pending URLs = %d, want at least 1: the unfetched URL must survive for a resume", pending)
	}
}
