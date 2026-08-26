// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/asciimoo/hister/config"
)

// countingFetcher is a minimal fetcher that tracks how many times it is called.
type countingFetcher struct {
	calls   atomic.Int64
	bodyLen int // body size to return per call
	links   []Link
}

func (f *countingFetcher) fetchPage(_ context.Context, rawURL string, _ RequestHints) (string, []byte, []Link, FetchMeta, error) {
	f.calls.Add(1)
	body := make([]byte, f.bodyLen)
	return rawURL, body, f.links, FetchMeta{StatusCode: 200}, nil
}

func (f *countingFetcher) close() error { return nil }

func newBudgetCrawler(limits config.CrawlerLimits, f fetcher, links []Link) *baseCrawler {
	cfg := &config.CrawlerConfig{
		Rate: config.CrawlerRate{
			GlobalRPS:          1000,
			PerHostRPS:         1000,
			GlobalConcurrency:  1,
			PerHostConcurrency: 1,
		},
		Limits: limits,
	}
	o := options{}
	bc := newBaseCrawler(f, cfg, nil, o)
	return bc
}

func TestBudgetMaxPages(t *testing.T) {
	// Set MaxPages=2; fetcher returns 5 links back to itself.
	links := []Link{
		{Href: "http://example.com/1"},
		{Href: "http://example.com/2"},
		{Href: "http://example.com/3"},
		{Href: "http://example.com/4"},
		{Href: "http://example.com/5"},
	}
	cf := &countingFetcher{bodyLen: 10, links: links}
	bc := newBudgetCrawler(config.CrawlerLimits{MaxPages: 2}, cf, links)

	v, err := NewValidator(&ValidatorRules{})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := bc.Crawl(context.Background(), "http://example.com/", v)
	if err != nil {
		t.Fatal(err)
	}

	var docs int
	for range ch {
		docs++
	}

	calls := cf.calls.Load()
	// MaxPages=2 means at most 2 TryReservePage successes; fetcher called at most 2 times.
	// (The start URL counts as 1 reservation.)
	if calls > 2 {
		t.Errorf("expected at most 2 fetches with MaxPages=2, got %d", calls)
	}
}

func TestBudgetMaxPagesPerHost(t *testing.T) {
	// Set MaxPagesPerHost=1; seed with 3 URLs from same host.
	cf := &countingFetcher{bodyLen: 10, links: nil}
	bc := newBudgetCrawler(config.CrawlerLimits{MaxPagesPerHost: 1}, cf, nil)

	v, err := NewValidator(&ValidatorRules{})
	if err != nil {
		t.Fatal(err)
	}

	// We crawl a start URL that returns 2 more links on the same host.
	cf.links = []Link{
		{Href: "http://example.com/a"},
		{Href: "http://example.com/b"},
	}

	ch, err := bc.Crawl(context.Background(), "http://example.com/", v)
	if err != nil {
		t.Fatal(err)
	}

	for range ch {
	}

	calls := cf.calls.Load()
	// The start URL uses the 1 allowed page for example.com; /a and /b are skipped.
	if calls != 1 {
		t.Errorf("expected exactly 1 fetch with MaxPagesPerHost=1, got %d", calls)
	}
}

func TestBudgetMaxBytesPerHost(t *testing.T) {
	// Set MaxBytesPerHost=100; fetcher returns 60-byte body.
	// First fetch (60 bytes) succeeds; second URL from same host is skipped because
	// after AddBytes the next TryReservePage call for that host would proceed but
	// HostExhausted returns true once 100 bytes exceeded on second fetch.
	// Actually: budget checks happen before fetch, bytes added after.
	// So both could pass budget check; after first fetch bytes=60 < 100.
	// After second fetch bytes would be 120 > 100 but we already fetched.
	// HostExhausted checks bytes BEFORE fetch, so: first fetch: 0 < 100 ok, bytes->60.
	// Second fetch: 60 < 100 ok, bytes->120. Third fetch: 120 >= 100 skip.
	// Provide 3 links so we can observe the 3rd being skipped.
	cf := &countingFetcher{bodyLen: 60, links: nil}
	bc := newBudgetCrawler(config.CrawlerLimits{MaxBytesPerHost: 100}, cf, nil)

	v, err := NewValidator(&ValidatorRules{})
	if err != nil {
		t.Fatal(err)
	}

	cf.links = []Link{
		{Href: "http://example.com/a"},
		{Href: "http://example.com/b"},
	}

	ch, err := bc.Crawl(context.Background(), "http://example.com/", v)
	if err != nil {
		t.Fatal(err)
	}

	for range ch {
	}

	calls := cf.calls.Load()
	// start URL + /a fetched (2 fetches, 120 bytes total after). /b should be skipped.
	if calls > 2 {
		t.Errorf("expected at most 2 fetches with MaxBytesPerHost=100 and 60-byte body, got %d", calls)
	}
}
