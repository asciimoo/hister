// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/model"
)

// persistentCrawler wraps a fetcher with DB-backed BFS so crawl jobs can be
// interrupted and resumed.
type persistentCrawler struct {
	fetcher        fetcher
	cfg            *config.CrawlerConfig
	jobID          string
	robots         *RobotsCache // nil means robots.txt enforcement is disabled
	skipURLChecker SkipURLChecker
	err            error
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
	return &persistentCrawler{fetcher: f, cfg: cfg, jobID: jobID, robots: robots, skipURLChecker: o.skipURLChecker}, nil
}

// Crawl starts (or resumes) the persistent crawl job identified by jobID.
// startURL and v are only used when creating a new job; on resume the stored
// start URL and validator rules take precedence (the caller is responsible for
// passing the correct v with a pre-seeded visited counter).
func (c *persistentCrawler) Crawl(ctx context.Context, startURL string, v *Validator) (<-chan *document.Document, error) {
	ch := make(chan *document.Document)
	go func() {
		defer close(ch)
		c.err = c.persistentBFS(ctx, startURL, v, ch)
		if c.err != nil {
			log.Error().Err(c.err).Str("job_id", c.jobID).Msg("persistent crawl failed")
		}
	}()
	return ch, nil
}

// Err returns the background crawl error after the document channel closes.
func (c *persistentCrawler) Err() error {
	return c.err
}

// Close releases resources held by the underlying fetcher backend.
func (c *persistentCrawler) Close() error {
	return c.fetcher.close()
}

func (c *persistentCrawler) persistentBFS(ctx context.Context, startURL string, v *Validator, ch chan<- *document.Document) error {
	// Restore any URLs that were left in_progress from a previous run.
	if err := model.ResetInProgressCrawlURLs(c.jobID); err != nil {
		return fmt.Errorf("reset in_progress URLs: %w", err)
	}

	// Queue the start URL if this is a new job (nothing pending yet).
	if err := model.InsertCrawlURLIfNotExists(c.jobID, startURL, 0); err != nil {
		return fmt.Errorf("insert start URL: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return model.UpdateCrawlJobStatus(c.jobID, model.CrawlJobInterrupted)
		default:
		}

		cur, err := model.NextPendingCrawlURL(c.jobID)
		if err != nil {
			return fmt.Errorf("next pending URL: %w", err)
		}
		if cur == nil {
			// No more pending URLs; crawl is complete.
			break
		}

		// Mark as in_progress.
		if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLInProgress, ""); err != nil {
			return fmt.Errorf("mark in_progress: %w", err)
		}

		parsedURL, err := url.Parse(cur.URL)
		if err != nil {
			if err2 := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLFailed, err.Error()); err2 != nil {
				return fmt.Errorf("record invalid crawl URL: %w", err2)
			}
			continue
		}

		switch v.Validate(parsedURL, cur.Depth) {
		case URLStop:
			// Put the URL back so a resumed job can pick it up with higher limits.
			if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLPending, ""); err != nil {
				return fmt.Errorf("failed to revert URL to pending on URLStop: %w", err)
			}
			return model.UpdateCrawlJobStatus(c.jobID, model.CrawlJobInterrupted)
		case URLSkip:
			log.Info().Str("url", cur.URL).Int("depth", cur.Depth).Msg("crawler: skipping URL by crawler rules")
			if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLSkipped, ""); err != nil {
				return fmt.Errorf("failed to mark URL skipped: %w", err)
			}
			continue
		}

		if c.robots != nil && !c.robots.Allowed(ctx, cur.URL) {
			log.Info().Str("url", cur.URL).Msg("crawler: skipping URL disallowed by robots.txt")
			if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLSkipped, "robots.txt"); err != nil {
				return fmt.Errorf("failed to mark URL skipped by robots.txt: %w", err)
			}
			continue
		}

		if c.skipURLChecker != nil {
			skip, err := c.skipURLChecker(cur.URL)
			if err != nil {
				log.Warn().Err(err).Str("url", cur.URL).Msg("crawler: failed to check whether URL should be skipped")
			} else if skip {
				log.Info().Str("url", cur.URL).Msg("crawler: skipping URL by prefetch skip predicate")
				if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLSkipped, "prefetch skip"); err != nil {
					return fmt.Errorf("failed to mark URL skipped by prefetch predicate: %w", err)
				}
				continue
			}
		}

		if c.cfg.Delay > 0 {
			select {
			case <-time.After(time.Duration(c.cfg.Delay) * time.Second):
			case <-ctx.Done():
				if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLPending, ""); err != nil {
					return fmt.Errorf("failed to revert URL to pending on cancel: %w", err)
				}
				return model.UpdateCrawlJobStatus(c.jobID, model.CrawlJobInterrupted)
			}
		}

		finalURL, htmlContent, links, fetchErr := c.fetcher.fetchPage(ctx, cur.URL)
		if fetchErr != nil {
			log.Warn().Err(fetchErr).Str("url", cur.URL).Msg("crawler: failed to fetch page")
			if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLFailed, fetchErr.Error()); err != nil {
				return fmt.Errorf("failed to mark URL failed: %w", err)
			}
			continue
		}

		// Handle redirects: insert the final URL as done so it won't be fetched again.
		if finalURL != cur.URL {
			finalParsedURL, fErr := url.Parse(finalURL)
			if fErr == nil {
				finalParsedURL.Fragment = ""
				cleanFinal := finalParsedURL.String()
				if err := model.InsertCrawlURLDone(c.jobID, cleanFinal, cur.Depth); err != nil {
					return fmt.Errorf("record redirect target %s: %w", cleanFinal, err)
				}
			}
		}

		if v.Rules().NoDepth {
			if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLDone, ""); err != nil {
				return fmt.Errorf("failed to mark URL done: %w", err)
			}
		} else {
			// Resolve all discovered links first, then enqueue them together with
			// the mark-done update in a single transaction.
			finalParsed, err := url.Parse(finalURL)
			if err != nil {
				finalParsed = parsedURL
			}
			finalParsed.Fragment = ""

			resolved := make([]string, 0, len(links))
			for _, link := range links {
				abs, err := resolveURL(finalParsed, link)
				if err != nil || abs == "" {
					continue
				}
				resolved = append(resolved, abs)
			}

			if err := model.MarkDoneAndEnqueueLinks(cur.ID, c.jobID, resolved, cur.Depth+1); err != nil {
				return fmt.Errorf("failed to mark URL done and enqueue links: %w", err)
			}
		}

		doc := &document.Document{
			URL:  finalURL,
			HTML: htmlContent,
		}

		select {
		case ch <- doc:
		case <-ctx.Done():
			return model.UpdateCrawlJobStatus(c.jobID, model.CrawlJobInterrupted)
		}
	}

	return model.UpdateCrawlJobStatus(c.jobID, model.CrawlJobCompleted)
}
