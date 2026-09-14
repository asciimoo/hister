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
	bodyLen int
	links   []Link
}

func (f *countingFetcher) fetchPage(_ context.Context, rawURL string) (string, []byte, []Link, FetchMeta, error) {
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
	if calls > 2 {
		t.Errorf("expected at most 2 fetches with MaxPages=2, got %d", calls)
	}
}

func TestBudgetMaxPagesPerHost(t *testing.T) {
	cf := &countingFetcher{bodyLen: 10, links: nil}
	bc := newBudgetCrawler(config.CrawlerLimits{MaxPagesPerHost: 1}, cf, nil)

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
	if calls != 1 {
		t.Errorf("expected exactly 1 fetch with MaxPagesPerHost=1, got %d", calls)
	}
}

func TestBudgetNoOvercountOnSchedulerError(t *testing.T) {
	limits := config.CrawlerLimits{MaxPages: 2}
	cfg := &config.CrawlerConfig{Limits: limits, Rate: config.CrawlerRate{PerHostRPS: 1000, GlobalRPS: 1000, GlobalConcurrency: 1, PerHostConcurrency: 1}}
	coord := NewCoordinator(cfg)

	if !coord.TryReservePage("example.com") {
		t.Fatal("first TryReservePage should succeed")
	}
	if !coord.TryReservePage("example.com") {
		t.Fatal("second TryReservePage should succeed")
	}

	if coord.TryReservePage("example.com") {
		t.Error("third TryReservePage should fail when MaxPages=2")
	}

	if got := coord.globalPages.Load(); got > int64(limits.MaxPages) {
		t.Errorf("budget pages counter = %d, want <= %d", got, limits.MaxPages)
	}
}

func TestBudgetMaxBytesPerHost(t *testing.T) {
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
	if calls > 2 {
		t.Errorf("expected at most 2 fetches with MaxBytesPerHost=100 and 60-byte body, got %d", calls)
	}
}
