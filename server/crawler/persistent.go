// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/model"
)

// PersistentCrawler persists traversal and document processing outcomes so
// interrupted jobs can resume without skipping unacknowledged documents.
type PersistentCrawler struct {
	fetcher        fetcher
	cfg            *config.CrawlerConfig
	jobID          string
	robots         *RobotsCache // nil means robots.txt enforcement is disabled
	skipURLChecker SkipURLChecker
}

// NewPersistent creates a crawler that persists its state to the database.
// jobID is used as the primary key for the crawl job.
// Pass a non-nil RobotsCache to enforce robots.txt rules; pass nil to disable.
func NewPersistent(cfg *config.CrawlerConfig, jobID string, robots *RobotsCache, opts ...Option) (*PersistentCrawler, error) {
	o := applyOptions(opts...)
	f, err := newFetcher(*cfg)
	if err != nil {
		return nil, fmt.Errorf("%s backend: %w", crawlerBackendName(cfg), err)
	}
	return &PersistentCrawler{fetcher: f, cfg: cfg, jobID: jobID, robots: robots, skipURLChecker: o.skipURLChecker}, nil
}

// Crawl starts (or resumes) the persistent crawl job identified by jobID.
// It calls process synchronously for each fetched document and marks its source
// URL done only after process succeeds. Processing failures are recorded against
// the source URL and crawling continues. Cancellation leaves unfinished work
// available for resume. A process crash can cause a successful submission to be
// retried if its completion was not yet recorded.
// startURL and v are only used when creating a new job; on resume the stored
// start URL and validator rules take precedence (the caller is responsible for
// passing the correct v with a pre-seeded visited counter).
func (c *PersistentCrawler) Crawl(ctx context.Context, startURL string, v *Validator, process func(*document.Document) error) error {
	if process == nil {
		return errors.New("persistent crawl requires a document processor")
	}
	return c.persistentBFS(ctx, startURL, v, process)
}

// Close releases resources held by the underlying fetcher backend.
func (c *PersistentCrawler) Close() error {
	return c.fetcher.close()
}

func (c *PersistentCrawler) persistentBFS(ctx context.Context, startURL string, v *Validator, process func(*document.Document) error) error {
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
			if ctx.Err() != nil {
				if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLPending, ""); err != nil {
					return fmt.Errorf("failed to requeue interrupted URL: %w", err)
				}
				return model.UpdateCrawlJobStatus(c.jobID, model.CrawlJobInterrupted)
			}
			log.Warn().Err(fetchErr).Str("url", cur.URL).Msg("crawler: failed to fetch page")
			if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLFailed, fetchErr.Error()); err != nil {
				return fmt.Errorf("failed to mark URL failed: %w", err)
			}
			continue
		}

		finalParsed, err := url.Parse(finalURL)
		if err != nil {
			finalParsed = parsedURL
		}
		finalParsed.Fragment = ""
		var resolved []string
		if !v.Rules().NoDepth {
			for _, link := range links {
				abs, err := resolveURL(finalParsed, link)
				if err == nil && abs != "" {
					resolved = append(resolved, abs)
				}
			}
		}
		// Save discovered links before submission. If the server accepts this
		// page but completion cannot be recorded, resume may skip it as already
		// indexed; its children must still be available in the queue.
		if err := model.BulkInsertCrawlURLs(c.jobID, resolved, cur.Depth+1); err != nil {
			return fmt.Errorf("enqueue discovered links: %w", err)
		}

		processErr := process(&document.Document{URL: finalURL, HTML: htmlContent})
		if processErr != nil {
			if ctx.Err() != nil {
				if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLPending, ""); err != nil {
					return fmt.Errorf("requeue interrupted submission: %w", err)
				}
				return model.UpdateCrawlJobStatus(c.jobID, model.CrawlJobInterrupted)
			}
			errCode := 0
			if httpErr, ok := errors.AsType[interface {
				error
				HTTPStatusCode() int
			}](processErr); ok {
				errCode = httpErr.HTTPStatusCode()
			}
			if err := model.MarkCrawlURLFailed(c.jobID, cur.URL, errCode, processErr.Error()); err != nil {
				return fmt.Errorf("record document processing failure: %w", err)
			}
			continue
		}

		// A redirect target is complete only after its document is accepted.
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

		if err := model.UpdateCrawlURLStatus(cur.ID, model.CrawlURLDone, ""); err != nil {
			return fmt.Errorf("failed to mark URL done: %w", err)
		}
	}

	return model.UpdateCrawlJobStatus(c.jobID, model.CrawlJobCompleted)
}
