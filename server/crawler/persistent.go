// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/model"
)

// jobLeaseTTL is how long a crawl job stays owned by a run without a renewal.
// A run that dies without releasing its lease blocks a resume for this long.
// jobLeaseRenewInterval must stay well below it so a slow database write does
// not cost a live run its lease.
const (
	jobLeaseTTL           = time.Minute
	jobLeaseRenewInterval = 20 * time.Second
)

// errLeaseLost is reported by Err when another run took the job over.
var errLeaseLost = errors.New("crawl job lease lost to another run")

// persistentCrawler wraps baseCrawler with a DB-backed queue so crawl jobs can
// be interrupted and resumed.
type persistentCrawler struct {
	*baseCrawler
	jobID     string
	owner     string
	leaseLost atomic.Bool
	err       error
}

// newLeaseOwner builds a human-readable owner ID so a blocked resume can say
// which process holds the job.
func newLeaseOwner(jobID string) string {
	suffix, err := model.GenerateCrawlJobID()
	if err != nil {
		// The suffix only disambiguates two runs of the same pid on the same
		// host, which cannot overlap; the pid alone is still unique enough.
		log.Debug().Err(err).Msg("crawler: falling back to pid-only lease owner")
		suffix = jobID
	}
	host, err := os.Hostname()
	if err != nil {
		log.Debug().Err(err).Msg("crawler: hostname unavailable for lease owner")
		host = "unknown"
	}
	return fmt.Sprintf("%s/%d/%s", host, os.Getpid(), suffix)
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

	return &persistentCrawler{baseCrawler: bc, jobID: jobID, owner: newLeaseOwner(jobID)}, nil
}

// Crawl starts (or resumes) the persistent crawl job identified by jobID.
// Only one run may own a job at a time: Crawl takes the job lease and fails
// when another live run holds it.
func (c *persistentCrawler) Crawl(ctx context.Context, startURL string, v *Validator) (<-chan *document.Document, error) {
	if err := checkStartURL(startURL, v); err != nil {
		return nil, err
	}

	if c.owner == "" {
		c.owner = newLeaseOwner(c.jobID)
	}
	acquired, holder, err := model.AcquireCrawlJobLease(c.jobID, c.owner, jobLeaseTTL)
	if err != nil {
		return nil, fmt.Errorf("acquire crawl job lease: %w", err)
	}
	if !acquired {
		return nil, fmt.Errorf("crawl job %s is already running as %s (lease expires %s)",
			c.jobID, holder.LockedBy, holder.LockExpiresAt.Format(time.RFC3339))
	}

	// Restore any URLs left in_progress from a previous run. The lease makes
	// this safe: no other run can be fetching them.
	if err := model.ResetInProgressCrawlURLs(c.jobID); err != nil {
		c.releaseLease()
		return nil, fmt.Errorf("reset in_progress URLs: %w", err)
	}

	runCtx, cancelRun := context.WithCancel(ctx)

	q := newSQLiteQueue(c.jobID)
	ch := make(chan *document.Document)
	go func() {
		defer close(ch)
		defer c.releaseLease()
		defer cancelRun()

		heartbeatDone := make(chan struct{})
		go c.renewLease(runCtx, cancelRun, heartbeatDone)

		c.err = c.run(runCtx, q, startURL, v, ch)
		cancelRun()
		<-heartbeatDone

		if c.leaseLost.Load() {
			c.err = errors.Join(c.err, errLeaseLost)
		}
		if c.err != nil {
			log.Error().Err(c.err).Str("job_id", c.jobID).Msg("persistent crawl failed")
		}
	}()
	return ch, nil
}

// renewLease keeps the job lease alive while the crawl runs. Losing it means
// another run has taken the job over, so the crawl stops rather than compete
// for the same URLs.
func (c *persistentCrawler) renewLease(ctx context.Context, onLost func(), done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(jobLeaseRenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ok, err := model.RenewCrawlJobLease(c.jobID, c.owner, jobLeaseTTL)
			if err != nil {
				// Transient database trouble: keep trying until the lease runs
				// out, at which point a renewal reports it as lost.
				log.Warn().Err(err).Str("job_id", c.jobID).Msg("crawler: crawl job lease renewal failed")
				continue
			}
			if !ok {
				log.Error().Str("job_id", c.jobID).Str("owner", c.owner).Msg("crawler: crawl job lease lost, stopping run")
				c.leaseLost.Store(true)
				onLost()
				return
			}
		}
	}
}

func (c *persistentCrawler) releaseLease() {
	if err := model.ReleaseCrawlJobLease(c.jobID, c.owner); err != nil {
		log.Warn().Err(err).Str("job_id", c.jobID).Msg("crawler: releasing crawl job lease failed")
	}
}

// Err returns the background crawl error after the document channel closes.
func (c *persistentCrawler) Err() error {
	return c.err
}
