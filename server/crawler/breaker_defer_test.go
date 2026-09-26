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

// failingFetcher fails every fetch and reports each attempt.
type failingFetcher struct {
	calls    atomic.Int64
	attempts chan struct{}
}

func (f *failingFetcher) fetchPage(_ context.Context, _ string) (string, []byte, []Link, FetchMeta, error) {
	f.calls.Add(1)
	select {
	case f.attempts <- struct{}{}:
	default:
	}
	return "", nil, nil, FetchMeta{}, errors.New("connection refused")
}

func (f *failingFetcher) close() error { return nil }

// TestOpenBreakerDefersRemainingURLs covers a host outage. Once the breaker
// opens, its rejection is not a verdict on the queued URLs: they were never
// attempted, so they must stay pending for a later run rather than draining
// into permanent failures.
func TestOpenBreakerDefersRemainingURLs(t *testing.T) {
	initTestDB(t)

	jobID := "breaker-outage"
	const (
		urlCount  = 6
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
		CircuitBreaker: config.CrawlerBreaker{ConsecutiveFailures: threshold, Cooldown: 60},
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

	// Let the host fail often enough to open the breaker.
	for i := 0; i < threshold; i++ {
		select {
		case <-f.attempts:
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d fetch attempts made, want %d", i, threshold)
		}
	}

	// With the breaker open the queue must stop shrinking. Give the crawl far
	// longer than it needs to burn through the rest before calling it stable.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		failed, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLFailed)
		if err != nil {
			t.Fatal(err)
		}
		if failed > threshold {
			t.Fatalf("%d URLs marked failed, want at most %d: an open breaker is recording untried URLs as failures", failed, threshold)
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	for range ch {
	}

	failed, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLFailed)
	if err != nil {
		t.Fatal(err)
	}
	if failed != threshold {
		t.Errorf("failed URLs = %d, want %d", failed, threshold)
	}

	pending, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLPending)
	if err != nil {
		t.Fatal(err)
	}
	if pending != urlCount-threshold {
		t.Errorf("pending URLs = %d, want %d (untried URLs must survive the outage)", pending, urlCount-threshold)
	}

	if calls := f.calls.Load(); calls != threshold {
		t.Errorf("fetcher called %d times, want %d (no fetch is attempted while the breaker is open)", calls, threshold)
	}
}
