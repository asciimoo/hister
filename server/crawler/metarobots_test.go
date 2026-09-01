// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"testing"

	"github.com/asciimoo/hister/config"
)

// staticFetcher returns a fixed response for any URL.
type staticFetcher struct {
	body       string
	links      []Link
	metaRobots MetaRobots
	statusCode int
}

func (f *staticFetcher) fetchPage(_ context.Context, rawURL string, _ RequestHints) (string, []byte, []Link, FetchMeta, error) {
	return rawURL, []byte(f.body), f.links, FetchMeta{
		StatusCode: f.statusCode,
		MetaRobots: f.metaRobots,
	}, nil
}

func (f *staticFetcher) close() error { return nil }

func newMetaRobotsCrawler(respectMeta bool, f fetcher) *baseCrawler {
	cfg := &config.CrawlerConfig{
		Rate: config.CrawlerRate{
			GlobalRPS:          1000,
			PerHostRPS:         1000,
			GlobalConcurrency:  1,
			PerHostConcurrency: 1,
		},
		RespectMetaRobots: respectMeta,
	}
	return newBaseCrawler(f, cfg, nil, options{})
}

func TestMetaRobotsNoIndexSuppressesDoc(t *testing.T) {
	sf := &staticFetcher{
		body:       "<html>secret</html>",
		statusCode: 200,
		metaRobots: MetaRobots{NoIndex: true},
	}
	bc := newMetaRobotsCrawler(true, sf)

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

	if docs != 0 {
		t.Errorf("expected 0 docs with noindex, got %d", docs)
	}
}

func TestMetaRobotsNoIndexDisabledEmitsDoc(t *testing.T) {
	sf := &staticFetcher{
		body:       "<html>content</html>",
		statusCode: 200,
		metaRobots: MetaRobots{NoIndex: true},
	}
	// RespectMetaRobots is false - noindex should be ignored.
	bc := newMetaRobotsCrawler(false, sf)

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

	if docs != 1 {
		t.Errorf("expected 1 doc with RespectMetaRobots=false and noindex, got %d", docs)
	}
}
