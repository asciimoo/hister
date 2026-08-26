// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"errors"
	"fmt"
	"net/url"

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
	scheduler      *Scheduler
	breaker        *CircuitBreaker
	backoff        *Backoff
	budget         *Budget
	clock          Clock
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

	core := newCrawlerCore(cfg, robots)

	return &persistentCrawler{
		fetcher:        f,
		cfg:            cfg,
		jobID:          jobID,
		robots:         robots,
		skipURLChecker: o.skipURLChecker,
		scheduler:      core.scheduler,
		breaker:        core.breaker,
		backoff:        core.backoff,
		budget:         core.budget,
		clock:          core.clock,
	}, nil
}

// Crawl starts (or resumes) the persistent crawl job identified by jobID.
// startURL and v are only used when creating a new job; on resume the stored
// start URL and validator rules take precedence (the caller is responsible for
// passing the correct v with a pre-seeded visited counter).
func (c *persistentCrawler) Crawl(ctx context.Context, startURL string, v *Validator) (<-chan *document.Document, error) {
	ch := make(chan *document.Document)
	go func() {
		defer close(ch)
		if err := c.persistentBFS(ctx, startURL, v, ch); err != nil {
			log.Error().Err(err).Str("job_id", c.jobID).Msg("persistent crawl failed")
		}
	}()
	return ch, nil
}

// Close releases resources held by the underlying fetcher backend.
func (c *persistentCrawler) Close() error {
	c.scheduler.Stop()
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

	maxAttempts := c.cfg.Retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	for {
		select {
		case <-ctx.Done():
			return model.UpdateCrawlJobStatus(c.jobID, model.CrawlJobInterrupted)
		default:
		}

		if c.budget.Exhausted() {
			log.Info().Str("job_id", c.jobID).Msg("crawler: budget exhausted, stopping")
			return model.UpdateCrawlJobStatus(c.jobID, model.CrawlJobInterrupted)
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
				log.Warn().Err(err2).Msg("failed to update URL status")
			}
			continue
		}

		switch v.Validate(parsedURL, cur.Depth) {
		case URLStop:
			// Put the URL back so a resumed job can pick it up with higher limits.
			if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLPending, ""); err != nil {
				log.Warn().Err(err).Msg("failed to revert URL to pending on URLStop")
			}
			return model.UpdateCrawlJobStatus(c.jobID, model.CrawlJobInterrupted)
		case URLSkip:
			log.Info().Str("url", cur.URL).Int("depth", cur.Depth).Msg("crawler: skipping URL by crawler rules")
			if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLSkipped, ""); err != nil {
				log.Warn().Err(err).Msg("failed to mark URL skipped")
			}
			continue
		}

		if c.robots != nil && !c.robots.Allowed(ctx, cur.URL) {
			log.Info().Str("url", cur.URL).Msg("crawler: skipping URL disallowed by robots.txt")
			if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLSkipped, "robots.txt"); err != nil {
				log.Warn().Err(err).Msg("failed to mark URL skipped by robots.txt")
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
					log.Warn().Err(err).Msg("failed to mark URL skipped by prefetch predicate")
				}
				continue
			}
		}

		host := parsedURL.Hostname()

		if c.budget.HostExhausted(host) {
			log.Info().Str("url", cur.URL).Str("host", host).Msg("crawler: per-host budget reached, skipping")
			if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLSkipped, "budget"); err != nil {
				log.Warn().Err(err).Msg("failed to mark URL skipped by budget")
			}
			continue
		}
		var hints RequestHints
		if c.cfg.ConditionalGet {
			etag, lastMod, ok, lookupErr := model.GetLastFetchedURLMeta(cur.URL)
			if lookupErr != nil {
				log.Warn().Err(lookupErr).Str("url", cur.URL).Msg("crawler: failed to look up prior fetch meta")
			} else if ok {
				hints.IfNoneMatch = etag
				hints.IfModifiedSince = lastMod
			}
		}

		var finalURL string
		var body []byte
		var links []Link
		var meta FetchMeta
		var fetchErr error

		for attempt := 0; attempt < maxAttempts; attempt++ {
			if attempt > 0 {
				wait := c.backoff.Duration(attempt)
				select {
				case <-ctx.Done():
					if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLPending, ""); err != nil {
						log.Warn().Err(err).Msg("failed to revert URL to pending on cancel")
					}
					return model.UpdateCrawlJobStatus(c.jobID, model.CrawlJobInterrupted)
				case <-c.clock.After(wait):
				}
			}

			if err := c.scheduler.Wait(ctx, host); err != nil {
				if err2 := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLPending, ""); err2 != nil {
					log.Warn().Err(err2).Msg("failed to revert URL to pending on scheduler error")
				}
				return model.UpdateCrawlJobStatus(c.jobID, model.CrawlJobInterrupted)
			}

			if !c.budget.TryReservePage(host) {
				c.scheduler.Release(host)
				log.Info().Str("url", cur.URL).Str("host", host).Msg("crawler: page reservation failed (budget)")
				if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLSkipped, "budget"); err != nil {
					log.Warn().Err(err).Msg("failed to mark URL skipped by budget")
				}
				break
			}

			start := c.clock.Now()
			finalURL, body, links, meta, fetchErr = c.fetcher.fetchPage(ctx, cur.URL, hints)
			elapsed := c.clock.Now().Sub(start)

			c.scheduler.Release(host)

			if fetchErr != nil {
				retryable, retryAfter, statusCode := ClassifyError(fetchErr)
				// 304 Not Modified is not a fault; the outer handler treats it as success.
				logEvent := log.Warn()
				msg := "crawler: fetch error"
				var httpErr *HTTPStatusError
				if errors.As(fetchErr, &httpErr) && httpErr.Status == 304 {
					logEvent = log.Info()
					msg = "crawler: not modified"
				}
				logEvent.
					Err(fetchErr).
					Str("url", cur.URL).
					Str("host", host).
					Int("status", statusCode).
					Int("attempt", attempt+1).
					Int64("duration_ms", elapsed.Milliseconds()).
					Str("breaker_state", breakerStateName(c.breaker.State(host))).
					Msg(msg)

				if retryAfter > 0 {
					c.scheduler.Cooldown(host, retryAfter)
				}
				if httpErr != nil && httpErr.Status == 304 {
					// Success path — no retry, no breaker penalty.
					c.breaker.RecordSuccess(host)
					break
				}
				if retryable && attempt < maxAttempts-1 {
					c.breaker.RecordFailure(host)
					continue
				}
				c.breaker.RecordFailure(host)
				break
			}

			log.Info().
				Str("url", finalURL).
				Str("host", host).
				Int("status", meta.StatusCode).
				Int64("bytes", int64(len(body))).
				Int64("duration_ms", elapsed.Milliseconds()).
				Int("attempt", attempt+1).
				Str("breaker_state", breakerStateName(c.breaker.State(host))).
				Msg("crawler: fetched page")

			c.breaker.RecordSuccess(host)
			break
		}

		if fetchErr != nil {
			var httpErr *HTTPStatusError
			if errors.As(fetchErr, &httpErr) && httpErr.Status == 304 {
				// Not modified - mark done, no new doc, no re-enqueue.
				log.Info().Str("url", cur.URL).Msg("crawler: 304 not modified, skipping re-index")
				if err2 := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLDone, ""); err2 != nil {
					log.Warn().Err(err2).Msg("failed to mark URL done (304)")
				}
				continue
			}
			if err2 := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLFailed, fetchErr.Error()); err2 != nil {
				log.Warn().Err(err2).Msg("failed to mark URL failed")
			}
			continue
		}

		bodyLen := int64(len(body))
		c.budget.AddBytes(host, bodyLen)

		// Handle redirects: insert the final URL as done so it won't be fetched again.
		if finalURL != cur.URL {
			finalParsedURL, fErr := url.Parse(finalURL)
			if fErr == nil {
				finalParsedURL.Fragment = ""
				cleanFinal := finalParsedURL.String()
				if err := model.InsertCrawlURLDone(c.jobID, cleanFinal, cur.Depth); err != nil {
					log.Warn().Err(err).Str("url", cleanFinal).Msg("failed to insert redirect target as done")
				}
			}
		}

		effectiveMR := meta.MetaRobots
		if c.cfg.RespectMetaRobots {
			xr := parseXRobotsTag(meta.XRobotsTag)
			if xr.NoIndex {
				effectiveMR.NoIndex = true
			}
			if xr.NoFollow {
				effectiveMR.NoFollow = true
			}
		}

		noFollow := c.cfg.RespectMetaRobots && effectiveMR.NoFollow

		if v.Rules().NoDepth {
			if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLDone, ""); err != nil {
				log.Warn().Err(err).Msg("failed to mark URL done")
			}
		} else {
			// Resolve all discovered links first (unless nofollow), then enqueue
			// them together with the mark-done update in a single transaction.
			finalParsed, err := url.Parse(finalURL)
			if err != nil {
				finalParsed = parsedURL
			}
			finalParsed.Fragment = ""

			var resolved []string
			if !noFollow {
				resolved = make([]string, 0, len(links))
				for _, link := range links {
					// Skip nofollow links.
					if isNofollow(link.Rel) {
						continue
					}
					abs, err := resolveURL(finalParsed, link.Href)
					if err != nil || abs == "" {
						continue
					}
					resolved = append(resolved, abs)
				}
			}

			if err := model.MarkDoneAndEnqueueLinks(cur.ID, c.jobID, resolved, cur.Depth+1, meta.ETag, meta.LastModified); err != nil {
				log.Warn().Err(err).Msg("failed to mark URL done and enqueue links")
			}
		}

		if !c.cfg.RespectMetaRobots || !effectiveMR.NoIndex {
			doc := &document.Document{
				URL:          finalURL,
				HTML:         string(body),
				ETag:         meta.ETag,
				LastModified: meta.LastModified,
			}

			select {
			case ch <- doc:
			case <-ctx.Done():
				return model.UpdateCrawlJobStatus(c.jobID, model.CrawlJobInterrupted)
			}
		}
	}

	return model.UpdateCrawlJobStatus(c.jobID, model.CrawlJobCompleted)
}
