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

// hangingFetcher blocks on ctx.Done and returns whatever ctx.Err() says.
// It counts the number of calls so tests can assert that a resumed job
// re-invokes the fetch rather than silently dropping the URL.
type hangingFetcher struct {
	calls   atomic.Int64
	started chan struct{} // signals the moment the first fetch begins
}

func (f *hangingFetcher) fetchPage(ctx context.Context, rawURL string, _ RequestHints) (string, []byte, []Link, FetchMeta, error) {
	f.calls.Add(1)
	select {
	case f.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return "", nil, nil, FetchMeta{}, ctx.Err()
}

func (f *hangingFetcher) close() error { return nil }

// succeedingFetcher returns immediately with a small HTML body and no links.
type succeedingFetcher struct{ calls atomic.Int64 }

func (f *succeedingFetcher) fetchPage(_ context.Context, rawURL string, _ RequestHints) (string, []byte, []Link, FetchMeta, error) {
	f.calls.Add(1)
	return rawURL, []byte("<html></html>"), nil, FetchMeta{StatusCode: 200}, nil
}

func (f *succeedingFetcher) close() error { return nil }

// TestPersistentCrawlInterruptResumesInsteadOfDropping is a regression test for
// H1 (silently dropped URLs on graceful-shutdown cancellation).
//
// Old behaviour: fetchCtx cancellation returned completion{err: ctx.Err()},
// which sqliteQueue.Complete mapped to CrawlURLFailed. On resume, the URL
// was NOT retried (already terminal) and was silently dropped.
//
// New behaviour: cancellation returns completion{interrupted: true, ...},
// which sqliteQueue.Complete maps to CrawlURLPending. On resume, the URL is
// picked up again by ResetInProgressCrawlURLs + the next Pop.
func TestPersistentCrawlInterruptResumesInsteadOfDropping(t *testing.T) {
	initTestDB(t)

	jobID := "interrupt-resume-test"
	startURL := "http://example.com/interrupt"
	if err := model.CreateCrawlJob(jobID, startURL, "", "test"); err != nil {
		t.Fatalf("CreateCrawlJob: %v", err)
	}

	baseCfg := func() *config.CrawlerConfig {
		return &config.CrawlerConfig{
			Rate: config.CrawlerRate{
				GlobalRPS:          1000,
				PerHostRPS:         1000,
				GlobalConcurrency:  1,
				PerHostConcurrency: 1,
			},
			ShutdownGrace: 1, // seconds; keep the test snappy
		}
	}

	// Interrupted run: use a fetcher that blocks until ctx cancels.
	hang := &hangingFetcher{started: make(chan struct{}, 1)}
	pc1 := &persistentCrawler{
		baseCrawler: &baseCrawler{
			fetcher: hang,
			cfg:     baseCfg(),
			coord:   NewCoordinator(baseCfg()),
			backoff: NewBackoff(time.Second, 30*time.Second),
		},
		jobID: jobID,
	}

	v, err := NewValidator(&ValidatorRules{})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := pc1.Crawl(ctx, startURL, v)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	// Wait until the fetcher is actually in-flight before cancelling, so the
	// interruption path (not the pre-fetch path) is what fires.
	select {
	case <-hang.started:
	case <-time.After(5 * time.Second):
		t.Fatal("fetcher never started; wiring must be broken")
	}

	cancel()

	// Drain the doc channel until the crawl goroutine exits.
	for range ch {
	}

	// The row must be pending, not failed. Failed would silently drop it.
	got, err := findRowByURL(jobID, startURL)
	if err != nil {
		t.Fatalf("findRowByURL after interrupt: %v", err)
	}
	if got.Status != model.CrawlURLPending {
		t.Fatalf("after interrupt: status = %q, want %q (row would be silently dropped on resume)", got.Status, model.CrawlURLPending)
	}
	if hang.calls.Load() != 1 {
		t.Fatalf("interrupted run: fetcher called %d times, want 1", hang.calls.Load())
	}

	// Resume with a normal fetcher and verify the URL is re-attempted.
	ok := &succeedingFetcher{}
	pc2 := &persistentCrawler{
		baseCrawler: &baseCrawler{
			fetcher: ok,
			cfg:     baseCfg(),
			coord:   NewCoordinator(baseCfg()),
			backoff: NewBackoff(time.Second, 30*time.Second),
		},
		jobID: jobID,
	}

	ch2, err := pc2.Crawl(context.Background(), startURL, v)
	if err != nil {
		t.Fatalf("resume Crawl: %v", err)
	}
	for range ch2 {
	}

	if ok.calls.Load() != 1 {
		t.Fatalf("resume: fetcher called %d times, want 1 (URL was silently dropped)", ok.calls.Load())
	}

	got2, err := findRowByURL(jobID, startURL)
	if err != nil {
		t.Fatalf("findRowByURL after resume: %v", err)
	}
	if got2.Status != model.CrawlURLDone {
		t.Fatalf("after resume: status = %q, want %q", got2.Status, model.CrawlURLDone)
	}
}

func findRowByURL(jobID, rawURL string) (*model.CrawlURL, error) {
	var row model.CrawlURL
	if err := model.DB.Where("job_id = ? AND url = ?", jobID, rawURL).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}
