// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/model"
)

// persistentCrawler wraps baseCrawler with a DB-backed queue so crawl jobs can
// be interrupted and resumed.
type persistentCrawler struct {
	*baseCrawler
	jobID string
}

// NewPersistent creates a Crawler that persists its state to the database.
// jobID is used as the primary key for the crawl job.
// Pass a non-nil RobotsCache to enforce robots.txt rules; pass nil to disable.
func NewPersistent(cfg *config.CrawlerConfig, jobID string, robots *RobotsCache, opts ...Option) (Crawler, error) {
	o := applyOptions(opts...)
	var f fetcher
	var err error
	switch cfg.Backend {
	case "chromedp":
		f, err = newChromedpFetcher(cfg)
	case "bidi":
		f, err = newBidiFetcher(cfg)
	default:
		f, err = newHTTPFetcher(cfg)
	}
	if err != nil {
		return nil, fmt.Errorf("%s backend: %w", crawlerBackendName(cfg), err)
	}

	initial := time.Duration(cfg.Retry.InitialBackoff) * time.Second
	if initial == 0 {
		initial = time.Second
	}
	maxB := time.Duration(cfg.Retry.MaxBackoff) * time.Second
	if maxB == 0 {
		maxB = 30 * time.Second
	}

	bc := &baseCrawler{
		fetcher:        f,
		cfg:            cfg,
		robots:         robots,
		skipURLChecker: o.skipURLChecker,
		coord:          NewCoordinator(cfg),
		backoff:        NewBackoff(initial, maxB),
	}

	return &persistentCrawler{baseCrawler: bc, jobID: jobID}, nil
}

// Crawl starts (or resumes) the persistent crawl job identified by jobID.
func (c *persistentCrawler) Crawl(ctx context.Context, startURL string, v *Validator) (<-chan *document.Document, error) {
	// Restore any URLs left in_progress from a previous run.
	if err := model.ResetInProgressCrawlURLs(c.jobID); err != nil {
		return nil, fmt.Errorf("reset in_progress URLs: %w", err)
	}

	q := newSQLiteQueue(c.jobID)
	ch := make(chan *document.Document)
	go func() {
		defer close(ch)
		if err := c.run(ctx, q, startURL, v, ch); err != nil {
			log.Error().Err(err).Str("job_id", c.jobID).Msg("persistent crawl failed")
		}
	}()
	return ch, nil
}
