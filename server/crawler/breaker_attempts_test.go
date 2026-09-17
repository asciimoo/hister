// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/model"
)

// retryableFailingFetcher fails every fetch with a retryable status, so the
// crawler exhausts its retry budget rather than giving up on the first error.
type retryableFailingFetcher struct{ calls atomic.Int64 }

func (f *retryableFailingFetcher) fetchPage(_ context.Context, _ string) (string, []byte, []Link, FetchMeta, error) {
	f.calls.Add(1)
	return "", nil, nil, FetchMeta{}, &HTTPStatusError{Status: 503}
}

func (f *retryableFailingFetcher) close() error { return nil }

// TestDeferralSpendsTheRetryBudgetOnce covers a host that stays down. Deferral
// puts the URL back as pending, and the retry budget used to restart on every
// pass: a half-open breaker granted a fresh probe each time round, so one URL
// was fetched far more than max_attempts times and the crawl never converged.
func TestDeferralSpendsTheRetryBudgetOnce(t *testing.T) {
	initTestDB(t)

	const (
		jobID       = "deferral-retry-budget"
		maxAttempts = 3
		threshold   = 1
	)
	url := "http://down.example.com/only"
	if err := model.CreateCrawlJob(jobID, url, "", "test"); err != nil {
		t.Fatalf("CreateCrawlJob: %v", err)
	}
	if err := model.BulkInsertCrawlURLs(jobID, []string{url}, 0); err != nil {
		t.Fatalf("BulkInsertCrawlURLs: %v", err)
	}

	cfg := &config.CrawlerConfig{
		Rate: config.CrawlerRate{
			GlobalRPS:          1000,
			PerHostRPS:         1000,
			GlobalConcurrency:  1,
			PerHostConcurrency: 1,
		},
		Retry: config.CrawlerRetry{MaxAttempts: maxAttempts},
		// A threshold of one opens the breaker on the first failure, so every
		// pass runs one probe and is then turned away, which is the defer loop
		// under test.
		CircuitBreaker: config.CrawlerBreaker{ConsecutiveFailures: threshold},
		ShutdownGrace:  1,
	}
	// The cooldown has to be non-zero for the breaker to refuse anyone (a zero
	// one re-probes immediately and the URL is never deferred at all), but short
	// enough that the next pass finds it ready. The constructor coerces zero to
	// five minutes, so set it past the constructor.
	coord := NewCoordinator(cfg)
	coord.cooldown = 50 * time.Millisecond

	f := &retryableFailingFetcher{}
	bc := &baseCrawler{
		fetcher: f,
		cfg:     cfg,
		coord:   coord,
		backoff: NewBackoff(time.Millisecond, time.Millisecond),
	}

	v, err := NewValidator(&ValidatorRules{NoDepth: true})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- runQueue(t, bc, newSQLiteQueue(jobID), url, v)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatalf("crawl did not converge: %d fetches so far, want it to give up after %d",
			f.calls.Load(), maxAttempts)
	}

	if calls := f.calls.Load(); calls != maxAttempts {
		t.Errorf("fetcher called %d times for one URL, want exactly %d: "+
			"the retry budget must be spent across deferrals, not restarted by each one", calls, maxAttempts)
	}

	failed, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLFailed)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 1 {
		t.Errorf("failed URLs = %d, want 1: a URL that exhausts its attempts must be retired", failed)
	}

	pending, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLPending)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Errorf("pending URLs = %d, want 0", pending)
	}
}

// TestDeferralDoesNotChargeUntriedURLs is the other half of the budget: a URL
// turned away before it ever reached the network has spent nothing, so an
// outage must not retire it. Bounding deferrals by count rather than by fetches
// actually made would drain a down host's queue into permanent failures.
func TestDeferralDoesNotChargeUntriedURLs(t *testing.T) {
	initTestDB(t)

	const (
		jobID     = "deferral-untried"
		urlCount  = 5
		threshold = 2
	)
	urls := make([]string, urlCount)
	for i := range urls {
		urls[i] = "http://down.example.com/" + string(rune('a'+i))
	}
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
			GlobalConcurrency:  1,
			PerHostConcurrency: 1,
		},
		Retry:          config.CrawlerRetry{MaxAttempts: 1},
		CircuitBreaker: config.CrawlerBreaker{ConsecutiveFailures: threshold, Cooldown: 3600},
		ShutdownGrace:  1,
	}
	f := &failingFetcher{attempts: make(chan struct{}, urlCount)}
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

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := pc.Crawl(ctx, urls[0], v)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	for i := 0; i < threshold; i++ {
		select {
		case <-f.attempts:
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d fetch attempts made, want %d", i, threshold)
		}
	}

	// With the breaker open and a long cooldown, nothing else may be fetched or
	// retired no matter how many times the deferred URLs cycle through.
	time.Sleep(500 * time.Millisecond)
	cancel()
	for range ch {
	}

	if calls := f.calls.Load(); calls != threshold {
		t.Errorf("fetcher called %d times, want %d: no fetch is attempted while the breaker is open", calls, threshold)
	}

	failed, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLFailed)
	if err != nil {
		t.Fatal(err)
	}
	if failed != threshold {
		t.Errorf("failed URLs = %d, want %d: URLs never tried during an outage must survive it", failed, threshold)
	}

	pending, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLPending)
	if err != nil {
		t.Fatal(err)
	}
	if pending != urlCount-threshold {
		t.Errorf("pending URLs = %d, want %d", pending, urlCount-threshold)
	}
}

// TestWaitReturnsProbePermitWhenCancelledWaitingForSlot covers the half-open
// probe permit against cancellation. breakerAllow hands the permit out before
// the in-flight semaphore is taken, so a caller cancelled while queueing for a
// slot has to give it back. Holding it pins the breaker half-open with a probe
// nobody is running, and every later request to that host is refused for the
// rest of the crawl.
func TestWaitReturnsProbePermitWhenCancelledWaitingForSlot(t *testing.T) {
	const host = "probe.example.com"
	coord := newTestCoordinator(config.CrawlerRate{
		GlobalRPS:          1000,
		PerHostRPS:         1000,
		GlobalConcurrency:  4,
		PerHostConcurrency: 1,
	})

	// Occupy the host's only in-flight slot while the breaker is still closed,
	// so this caller holds the slot without holding the probe permit.
	if err := coord.Wait(context.Background(), host); err != nil {
		t.Fatalf("occupying Wait: %v", err)
	}

	// Now open the breaker and make it immediately ready to probe, so the next
	// caller is handed the permit and then blocks on the occupied semaphore.
	for i := 0; i < 5; i++ {
		coord.RecordFailure(host)
	}
	if got := coord.HostBreakerState(host); got != BreakerOpen {
		t.Fatalf("breaker state = %v, want %v", got, BreakerOpen)
	}
	coord.cooldown = 0

	ctx, cancel := context.WithCancel(context.Background())
	blocked := make(chan error, 1)
	go func() { blocked <- coord.Wait(ctx, host) }()

	// Give the goroutine time to take the probe permit and reach the semaphore,
	// then give up on it.
	time.Sleep(100 * time.Millisecond)
	if got := coord.HostBreakerState(host); got != BreakerHalfOpen {
		t.Fatalf("breaker state = %v, want %v: the blocked caller should hold the probe permit", got, BreakerHalfOpen)
	}
	cancel()
	if err := <-blocked; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Wait error = %v, want %v", err, context.Canceled)
	}

	// Free the slot. The host must now be reachable again: the cancelled caller
	// made no request, so its probe permit belongs back in the breaker.
	coord.Release(host)

	admitted := make(chan error, 1)
	go func() { admitted <- coord.Wait(context.Background(), host) }()
	select {
	case err := <-admitted:
		if err != nil {
			t.Fatalf("Wait after cancellation = %v, want admitted: the probe permit was not returned", err)
		}
		coord.Release(host)
	case <-time.After(5 * time.Second):
		t.Fatal("Wait after cancellation never returned: the probe permit was not returned")
	}
}
