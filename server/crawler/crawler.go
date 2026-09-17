// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/html"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
)

// Crawler is the public interface for scraping backends.
// Crawl performs a BFS traversal starting from startURL, sending discovered
// documents to the returned channel. The channel is closed when crawling
// finishes or ctx is cancelled. Callers must read the channel until it closes,
// or cancel ctx: a caller that abandons it blocks a crawl worker on its send.
// Close must be called when the Crawler is no longer needed to release backend
// resources.
type Crawler interface {
	Crawl(ctx context.Context, startURL string, v *Validator) (<-chan *document.Document, error)
	Close() error
}

// ErrorReporter exposes failures from a background crawl. Err must be read
// after its document channel closes and before starting another crawl.
type ErrorReporter interface {
	Err() error
}

// SkipURLChecker decides whether rawURL should be skipped before delay and fetch.
type SkipURLChecker func(rawURL string) (bool, error)

type options struct {
	skipURLChecker SkipURLChecker
}

// Option customizes crawler traversal behavior.
type Option func(*options)

// WithSkipURLChecker installs a prefetch skip predicate. It runs after validator and
// robots checks, but before configured crawl delay and network fetch.
func WithSkipURLChecker(skipURLChecker SkipURLChecker) Option {
	return func(opts *options) {
		opts.skipURLChecker = skipURLChecker
	}
}

// fetcher is the internal interface implemented by each scraping backend.
type fetcher interface {
	fetchPage(ctx context.Context, rawURL string) (finalURL string, body []byte, links []Link, meta FetchMeta, err error)
	close() error
}

// baseCrawler wraps a fetcher with BFS traversal logic.
type baseCrawler struct {
	fetcher        fetcher
	cfg            *config.CrawlerConfig
	robots         *RobotsCache
	skipURLChecker SkipURLChecker
	coord          *Coordinator
	backoff        *Backoff
	// err holds the failure from the most recent background crawl. It is
	// written before the document channel closes, which is the happens-before
	// the ErrorReporter contract relies on.
	err error
}

// New creates a Crawler backed by the backend specified in cfg.Backend.
// Accepted values are "chromedp", "bidi", and "http" (default).
// Pass a non-nil RobotsCache to enforce robots.txt rules; pass nil to disable.
func New(cfg *config.CrawlerConfig, robots *RobotsCache, opts ...Option) (Crawler, error) {
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
	return newBaseCrawler(f, cfg, robots, o), nil
}

func newBaseCrawler(f fetcher, cfg *config.CrawlerConfig, robots *RobotsCache, o options) *baseCrawler {
	initial := time.Duration(cfg.Retry.InitialBackoff) * time.Second
	if initial == 0 {
		initial = time.Second
	}
	maxB := time.Duration(cfg.Retry.MaxBackoff) * time.Second
	if maxB == 0 {
		maxB = 30 * time.Second
	}
	return &baseCrawler{
		fetcher:        f,
		cfg:            cfg,
		robots:         robots,
		skipURLChecker: o.skipURLChecker,
		coord:          NewCoordinator(cfg),
		backoff:        NewBackoff(initial, maxB),
	}
}

func crawlerBackendName(cfg *config.CrawlerConfig) string {
	if cfg.Backend == "" {
		return "http"
	}
	return cfg.Backend
}

func parseCaptureDelay(value any) (time.Duration, error) {
	var delay time.Duration
	switch typed := value.(type) {
	case float64:
		delay = time.Duration(typed * float64(time.Second))
	case int:
		delay = time.Duration(typed) * time.Second
	case string:
		parsed, err := time.ParseDuration(typed)
		if err != nil {
			return 0, fmt.Errorf("invalid capture_delay %q: %w", typed, err)
		}
		delay = parsed
	default:
		return 0, fmt.Errorf("capture_delay must be a number in seconds or a duration string")
	}
	if delay < 0 {
		return 0, fmt.Errorf("capture_delay cannot be negative")
	}
	return delay, nil
}

func applyOptions(opts ...Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// Crawl starts a BFS crawl from startURL using an in-memory queue.
func (c *baseCrawler) Crawl(ctx context.Context, startURL string, v *Validator) (<-chan *document.Document, error) {
	if err := checkStartURL(startURL, v); err != nil {
		return nil, err
	}
	ch := make(chan *document.Document)
	q := newMemoryQueue()
	go func() {
		defer close(ch)
		c.err = c.run(ctx, q, startURL, v, ch)
		if c.err != nil {
			log.Error().Err(c.err).Msg("crawler: crawl failed")
		}
	}()
	return ch, nil
}

// Err returns the background crawl error after the document channel closes.
func (c *baseCrawler) Err() error {
	return c.err
}

// Close releases resources held by the underlying backend.
func (c *baseCrawler) Close() error {
	return c.fetcher.close()
}

// checkStartURL parses startURL and runs it through the traversal filters. The
// start URL is subject to the same domain and pattern rules as discovered
// links, and a rejected one is an error rather than an empty crawl.
func checkStartURL(startURL string, v *Validator) error {
	parsed, err := url.Parse(startURL)
	if err != nil {
		return fmt.Errorf("invalid start URL: %w", err)
	}
	if v.Validate(parsed, 0) != URLAllow {
		return fmt.Errorf("start URL rejected by crawl rules: %s", startURL)
	}
	return nil
}

// run is the unified crawl driver shared by both in-memory and sqlite backends.
func (c *baseCrawler) run(ctx context.Context, q CrawlQueue, startURL string, v *Validator, ch chan<- *document.Document) error {
	grace := time.Duration(c.cfg.ShutdownGrace) * time.Second
	if grace == 0 {
		grace = 30 * time.Second
	}

	// MaxDuration has to bound the whole run, not just the gaps between items.
	// Checking it only after a fetch lets a Retry-After cooldown, a rate-limiter
	// wait or a retry backoff run straight past the limit, so it is enforced as
	// a deadline on the context every one of those waits selects on.
	c.coord.StartRun()
	crawlCtx, crawlCancel := context.WithCancel(ctx)
	if deadline, ok := c.coord.Deadline(); ok {
		crawlCtx, crawlCancel = context.WithDeadline(ctx, deadline)
	}
	defer crawlCancel()

	fetchCtx, fetchCancel := context.WithCancel(context.Background())
	defer fetchCancel()

	go func() {
		<-crawlCtx.Done()
		select {
		case <-time.After(grace):
		case <-fetchCtx.Done():
		}
		fetchCancel()
	}()

	if err := q.Seed(crawlCtx, startURL); err != nil {
		return fmt.Errorf("seed queue: %w", err)
	}

	concurrency := c.cfg.Rate.GlobalConcurrency
	if concurrency < 1 {
		concurrency = 1
	}

	// A queue write is the crawl's durable state, not a log line. Failures are
	// collected here so the run reports them and, critically, never reaches
	// OnDone: a job marked completed with unrecorded results cannot be resumed.
	var (
		errMu   sync.Mutex
		runErr  error
		stopped atomic.Bool
	)
	recordErr := func(err error) {
		errMu.Lock()
		runErr = errors.Join(runErr, err)
		errMu.Unlock()
	}

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				item, ok, err := q.Pop(crawlCtx)
				if err != nil {
					// A cancelled crawl is the caller's doing, not a fault.
					if crawlCtx.Err() == nil {
						recordErr(fmt.Errorf("read the crawl queue: %w", err))
						stopped.Store(true)
					}
					return
				}
				if !ok {
					return
				}
				comp := c.fetchOne(fetchCtx, crawlCtx, item, v, ch)
				// Complete must run to completion even if the crawl context has
				// been cancelled: persistent queues need the DB write to reset
				// the row to pending (interrupted) or record the outcome.
				if cerr := q.Complete(context.Background(), item, comp); cerr != nil {
					log.Error().Err(cerr).Str("url", item.rawURL).Msg("crawler: recording the crawl result failed")
					recordErr(cerr)
				}
				if comp.stop || c.coord.Exhausted() {
					// Stop intake without cancelling the crawl. Cancelling here
					// kills the workers that already hold a reservation and are
					// only waiting on a rate token, which cuts the crawl short
					// of the very budget that triggered the stop.
					stopped.Store(true)
					q.Stop()
					continue
				}
				if comp.deferred {
					// BreakerRetryIn can race to zero, and sleeping for zero
					// turns an outage into a hot requeue loop.
					wait := max(comp.deferFor, breakerProbeWait)
					select {
					case <-crawlCtx.Done():
						return
					case <-time.After(wait):
					}
				}
			}
		}()
	}
	wg.Wait()

	// Reaching max_duration is a configured stopping condition, not a failure:
	// it must not make the command exit non-zero. The signal callers act on is
	// the job being recorded as stopped below, which is what makes it resumable.
	if ctx.Err() == nil && crawlCtx.Err() != nil {
		log.Info().
			Int("max_duration", c.cfg.Limits.MaxDuration).
			Msg("crawler: max crawl duration reached, stopping crawl")
		stopped.Store(true)
	}

	// Anything but a queue drained to completion leaves URLs pending, so the
	// queue must record the run as stopped: reporting it as done marks the job
	// completed and the CLI then refuses to resume it. That covers caller
	// cancellation, the MaxDuration deadline, a budget stopping intake, and a
	// queue write that failed - the last one especially, since its row is still
	// in_progress with its discovered links unrecorded.
	if ctx.Err() != nil || stopped.Load() || runErr != nil {
		return errors.Join(runErr, q.OnStop(context.Background()))
	}
	return q.OnDone(context.Background())
}

// fetchOne performs all pre-fetch checks, the actual fetch with retry/backoff,
// link resolution, and document emission. It returns a completion for the queue.
func (c *baseCrawler) fetchOne(fetchCtx, crawlCtx context.Context, item *pendingItem, v *Validator, ch chan<- *document.Document) (comp completion) {
	parsedURL, err := url.Parse(item.rawURL)
	if err != nil {
		return completion{err: err}
	}
	host := parsedURL.Hostname()

	// Pre-fetch: robots check.
	if c.robots != nil && !c.robots.Allowed(crawlCtx, item.rawURL) {
		log.Info().Str("url", item.rawURL).Msg("crawler: skipping URL disallowed by robots.txt")
		return completion{skipped: true, skipReason: "robots.txt"}
	}

	// Pre-fetch: skipURLChecker.
	if c.skipURLChecker != nil {
		skip, err := c.skipURLChecker(item.rawURL)
		if err != nil {
			log.Warn().Err(err).Str("url", item.rawURL).Msg("crawler: skipURL checker error")
		} else if skip {
			log.Info().Str("url", item.rawURL).Msg("crawler: skipping URL by prefetch skip predicate")
			return completion{skipped: true, skipReason: "prefetch skip"}
		}
	}

	// Pre-fetch: per-host budget.
	if c.coord.HostExhausted(host) {
		log.Info().Str("url", item.rawURL).Str("host", host).Msg("crawler: per-host budget reached, skipping")
		return completion{skipped: true, skipReason: "budget"}
	}

	// MaxLinks is charged here rather than at link discovery so that the queue
	// has already deduplicated, and so that the start URL counts too.
	if !v.TryVisit() {
		log.Info().Str("url", item.rawURL).Msg("crawler: max links reached, stopping crawl")
		return completion{stop: true}
	}
	// A URL that ends without a document must hand back everything it reserved.
	// stop is in the list because the crawl-budget stop below is returned after
	// TryVisit already succeeded; the MaxLinks stop above returns before this
	// defer is installed, so it cannot double-release.
	reservedPage := false
	defer func() {
		if !comp.interrupted && !comp.skipped && !comp.stop && !comp.deferred {
			return
		}
		v.ReleaseVisit()
		if reservedPage {
			c.coord.ReleasePage(host)
		}
	}()

	maxAttempts := c.cfg.Retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	// Attempts spent on this URL by earlier passes. A URL handed back by an open
	// breaker keeps its count, so the retry budget is spent across passes rather
	// than restarting on each one - otherwise a half-open breaker grants a fresh
	// probe every pass and one URL is fetched without bound during an outage.
	remaining := maxAttempts - item.attempts
	if remaining < 1 {
		return completion{err: fmt.Errorf(
			"giving up on %s after %d attempts", item.rawURL, item.attempts)}
	}
	// Fetches this pass actually reached the network. Only these are charged
	// back to the item: a pass turned away before it fetched costs it nothing,
	// so URLs never tried during an outage stay queued indefinitely.
	fetches := 0

	var finalURL string
	var body []byte
	var links []Link
	var meta FetchMeta
	var fetchErr error

	for attempt := 0; attempt < remaining; attempt++ {
		if attempt > 0 {
			wait := c.backoff.Duration(attempt)
			select {
			case <-fetchCtx.Done():
				return completion{interrupted: true, err: fetchCtx.Err()}
			case <-time.After(wait):
			}
		}

		if err := c.coord.Wait(crawlCtx, host); err != nil {
			if crawlCtx.Err() != nil {
				return completion{interrupted: true, err: err}
			}
			var breakerOpen *errBreakerOpen
			if errors.As(err, &breakerOpen) {
				// The host is down as a whole and this URL was never attempted.
				// Recording it as failed would drain the rest of the host's
				// queue into permanent failures during an outage, so leave it
				// queued and hold off until the breaker probes again.
				//
				// Deferring is not free: each pass hands the URL back with a
				// fresh retry loop, and a half-open breaker grants a fresh probe
				// with it. The attempt count carried on the item is what bounds
				// that, so an outage that outlasts the retry budget retires the
				// URL instead of serving it forever.
				wait := c.coord.BreakerRetryIn(host)
				log.Info().
					Str("url", item.rawURL).
					Str("host", host).
					Int("attempts", item.attempts+fetches).
					Dur("retry_in", wait).
					Msg("crawler: circuit breaker open, deferring URL")
				return completion{deferred: true, deferFor: wait, attemptsUsed: fetches}
			}
			return completion{err: err}
		}

		if !reservedPage && !c.coord.TryReservePage(host) {
			c.coord.Release(host)
			c.coord.AbandonProbe(host)
			if c.coord.Exhausted() {
				// The crawl-wide budget is spent rather than this host's. The
				// URL was never fetched, so leave it queued: the driver stops
				// the run and a resume with budget left picks it up. Recording
				// it as skipped would retire it for good.
				log.Info().Str("url", item.rawURL).Msg("crawler: crawl budget reached, stopping crawl")
				return completion{stop: true}
			}
			log.Info().Str("url", item.rawURL).Str("host", host).Msg("crawler: per-host page budget reached, skipping")
			return completion{skipped: true, skipReason: "budget"}
		}
		// Retries of the same URL reuse the reservation. Charging each attempt
		// spends one unit of MaxPages per retry for a single page.
		reservedPage = true

		start := time.Now()
		fetches++
		finalURL, body, links, meta, fetchErr = c.fetcher.fetchPage(fetchCtx, item.rawURL)
		elapsed := time.Since(start)

		c.coord.Release(host)

		if fetchErr != nil {
			// A context cancellation surfacing as fetch error is an interruption,
			// not a permanent failure; the persistent queue should reset the row.
			if fetchCtx.Err() != nil {
				return completion{interrupted: true, err: fetchErr}
			}
			retryable, retryAfter, statusCode := ClassifyError(fetchErr)
			log.Warn().
				Err(fetchErr).
				Str("url", item.rawURL).
				Str("host", host).
				Int("status", statusCode).
				Int("attempt", item.attempts+fetches).
				Int64("duration_ms", elapsed.Milliseconds()).
				Str("breaker_state", breakerStateName(c.coord.HostBreakerState(host))).
				Msg("crawler: fetch error")

			if retryAfter > 0 {
				c.coord.Cooldown(host, retryAfter)
			}
			if retryable && attempt < remaining-1 {
				c.coord.RecordFailure(host)
				continue
			}
			c.coord.RecordFailure(host)
			return completion{err: fetchErr}
		}

		log.Info().
			Str("url", finalURL).
			Str("host", host).
			Int("status", meta.StatusCode).
			Int64("bytes", int64(len(body))).
			Int64("duration_ms", elapsed.Milliseconds()).
			Int("attempt", item.attempts+fetches).
			Str("breaker_state", breakerStateName(c.coord.HostBreakerState(host))).
			Msg("crawler: fetched page")

		c.coord.RecordSuccess(host)
		break
	}

	if fetchErr != nil {
		return completion{err: fetchErr}
	}

	finalParsed, err := url.Parse(finalURL)
	if err != nil {
		finalParsed = parsedURL
	}
	finalParsed.Fragment = ""
	bodyLen := int64(len(body))
	c.coord.AddBytes(host, bodyLen)

	doc := &document.Document{
		URL:  finalURL,
		HTML: string(body),
	}
	select {
	case ch <- doc:
	case <-crawlCtx.Done():
		// Doc was fetched but never delivered downstream; treat as interrupted
		// so a resumed run refetches and re-emits.
		return completion{interrupted: true, finalURL: finalURL}
	}

	// Resolve discovered links. Validator filters here so queue doesn't re-filter.
	var resolvedLinks []string
	if !v.Rules().NoDepth {
		for _, link := range links {
			if isNofollow(link.Rel) {
				continue
			}
			abs, err := resolveURL(finalParsed, link.Href)
			if err != nil || abs == "" {
				continue
			}
			absParsed, err := url.Parse(abs)
			if err != nil {
				continue
			}
			if v.Validate(absParsed, item.depth+1) == URLSkip {
				continue
			}
			resolvedLinks = append(resolvedLinks, abs)
		}
	}

	return completion{finalURL: finalURL, resolvedLinks: resolvedLinks}
}

func hashURL(key string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return h.Sum64()
}

func breakerStateName(s BreakerState) string {
	switch s {
	case BreakerClosed:
		return "closed"
	case BreakerOpen:
		return "open"
	case BreakerHalfOpen:
		return "half_open"
	}
	return "unknown"
}

// isNofollow returns true if the rel string contains "nofollow" as a token.
func isNofollow(rel string) bool {
	for _, token := range strings.Fields(rel) {
		if strings.EqualFold(token, "nofollow") {
			return true
		}
	}
	return false
}

// resolveURL turns a potentially relative href into an absolute http(s) URL
// using base as the reference. Returns "" for non-http(s) schemes.
func resolveURL(base *url.URL, href string) (string, error) {
	u, err := url.Parse(href)
	if err != nil {
		return "", err
	}
	abs := base.ResolveReference(u)
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return "", nil
	}
	abs.Fragment = ""
	return abs.String(), nil
}

// extractLinks parses HTML from r and returns the links found in <a> elements.
func extractLinks(r io.Reader) ([]Link, error) {
	var links []Link
	z := html.NewTokenizer(r)
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return links, nil
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tagName := string(name)
			if !hasAttr || tagName != "a" {
				continue
			}
			var href, rel string
			for {
				key, val, more := z.TagAttr()
				switch string(key) {
				case "href":
					href = string(val)
				case "rel":
					rel = string(val)
				}
				if !more {
					break
				}
			}
			if href != "" {
				links = append(links, Link{Href: href, Rel: rel})
			}
		}
	}
}

// errResponseTooLarge is returned when a response body exceeds the configured limit.
var errResponseTooLarge = fmt.Errorf("response body exceeds size limit")
