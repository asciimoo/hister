// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/model"
)

// Crawler is the public interface for scraping backends.
// Crawl performs a BFS traversal starting from startURL, sending discovered
// documents to the returned channel. The channel is closed when crawling
// finishes or ctx is cancelled. Close must be called when the Crawler is no
// longer needed to release backend resources.
type Crawler interface {
	Crawl(ctx context.Context, startURL string, v *Validator) (<-chan *document.Document, error)
	Close() error
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
// fetchPage downloads rawURL and returns the final URL after any redirects,
// the raw response body, the links found on the page, fetch metadata, and any error.
type fetcher interface {
	fetchPage(ctx context.Context, rawURL string, hints RequestHints) (finalURL string, body []byte, links []Link, meta FetchMeta, err error)
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
	if _, err := url.Parse(startURL); err != nil {
		return nil, fmt.Errorf("invalid start URL: %w", err)
	}
	ch := make(chan *document.Document)
	q := newMemoryQueue()
	go func() {
		defer close(ch)
		if err := c.run(ctx, q, startURL, v, ch); err != nil {
			log.Error().Err(err).Msg("crawler: crawl failed")
		}
	}()
	return ch, nil
}

// Close releases resources held by the underlying backend.
func (c *baseCrawler) Close() error {
	return c.fetcher.close()
}

// run is the unified crawl driver shared by both in-memory and sqlite backends.
func (c *baseCrawler) run(ctx context.Context, q CrawlQueue, startURL string, v *Validator, ch chan<- *document.Document) error {
	grace := time.Duration(c.cfg.ShutdownGrace) * time.Second
	if grace == 0 {
		grace = 30 * time.Second
	}

	crawlCtx, crawlCancel := context.WithCancel(ctx)
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

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				item, ok, err := q.Pop(crawlCtx)
				if err != nil {
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
					log.Warn().Err(cerr).Msg("crawler: queue Complete failed")
				}
				if c.coord.Exhausted() {
					crawlCancel()
					return
				}
			}
		}()
	}
	wg.Wait()

	if ctx.Err() != nil {
		return q.OnStop(context.Background())
	}
	return q.OnDone(context.Background())
}

// fetchOne performs all pre-fetch checks, the actual fetch with retry/backoff,
// link resolution, and document emission. It returns a completion for the queue.
func (c *baseCrawler) fetchOne(fetchCtx, crawlCtx context.Context, item *pendingItem, v *Validator, ch chan<- *document.Document) completion {
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

	// Refresh per-host rate from robots crawl-delay before waiting.
	if c.robots != nil && c.cfg.Robots.RespectCrawlDelay {
		if delay := c.robots.CrawlDelayForHost(host); delay > 0 {
			c.coord.SetHostRate(host, 1.0/delay.Seconds())
		}
	}

	// Build conditional-GET hints from the last successful fetch of this URL.
	var hints RequestHints
	if c.cfg.ConditionalGet {
		etag, lastMod, ok, lookupErr := model.GetLastFetchedURLMeta(item.rawURL)
		if lookupErr != nil {
			log.Warn().Err(lookupErr).Str("url", item.rawURL).Msg("crawler: failed to look up prior fetch meta")
		} else if ok {
			hints.IfNoneMatch = etag
			hints.IfModifiedSince = lastMod
		}
	}

	maxAttempts := c.cfg.Retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
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
			case <-fetchCtx.Done():
				return completion{interrupted: true, err: fetchCtx.Err()}
			case <-time.After(wait):
			}
		}

		if err := c.coord.Wait(crawlCtx, host); err != nil {
			// crawlCtx cancellation and breaker-open both surface here; the
			// former is an interruption the persistent queue should retry.
			if crawlCtx.Err() != nil {
				return completion{interrupted: true, err: err}
			}
			return completion{err: err}
		}

		if !c.coord.TryReservePage(host) {
			c.coord.Release(host)
			log.Info().Str("url", item.rawURL).Str("host", host).Msg("crawler: page reservation failed (budget)")
			return completion{skipped: true, skipReason: "budget"}
		}

		start := time.Now()
		finalURL, body, links, meta, fetchErr = c.fetcher.fetchPage(fetchCtx, item.rawURL, hints)
		elapsed := time.Since(start)

		c.coord.Release(host)

		if fetchErr != nil {
			// A context cancellation surfacing as fetch error is an interruption,
			// not a permanent failure; the persistent queue should reset the row.
			if fetchCtx.Err() != nil {
				return completion{interrupted: true, err: fetchErr}
			}

			// 304 Not Modified: resource unchanged - mark done, skip re-index.
			var httpErr *HTTPStatusError
			if errors.As(fetchErr, &httpErr) && httpErr.Status == http.StatusNotModified {
				log.Info().Str("url", item.rawURL).Msg("crawler: not modified (304), skipping re-index")
				c.coord.RecordSuccess(host)
				return completion{finalURL: item.rawURL, etag: meta.ETag, lastModified: meta.LastModified}
			}

			retryable, retryAfter, statusCode := ClassifyError(fetchErr)
			log.Warn().
				Err(fetchErr).
				Str("url", item.rawURL).
				Str("host", host).
				Int("status", statusCode).
				Int("attempt", attempt+1).
				Int64("duration_ms", elapsed.Milliseconds()).
				Str("breaker_state", breakerStateName(c.coord.HostBreakerState(host))).
				Msg("crawler: fetch error")

			if retryAfter > 0 {
				c.coord.Cooldown(host, retryAfter)
			}
			if retryable && attempt < maxAttempts-1 {
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
			Int("attempt", attempt+1).
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

	// Determine effective meta-robots directives.
	effective := meta.MetaRobots
	if c.cfg.RespectMetaRobots {
		xr := parseXRobotsTag(meta.XRobotsTag)
		if xr.NoIndex {
			effective.NoIndex = true
		}
		if xr.NoFollow {
			effective.NoFollow = true
		}
	}

	if !c.cfg.RespectMetaRobots || !effective.NoIndex {
		doc := &document.Document{
			URL:          finalURL,
			HTML:         string(body),
			ETag:         meta.ETag,
			LastModified: meta.LastModified,
		}
		select {
		case ch <- doc:
		case <-crawlCtx.Done():
			// Doc was fetched but never delivered downstream; treat as interrupted
			// so a resumed run refetches and re-emits.
			return completion{interrupted: true, finalURL: finalURL}
		}
	}

	// Resolve discovered links. Validator filters here so queue doesn't re-filter.
	var resolvedLinks []string
	if !v.Rules().NoDepth && !(c.cfg.RespectMetaRobots && effective.NoFollow) {
		for _, link := range links {
			if isNofollow(link.Rel) {
				continue
			}
			abs, err := resolveURL(finalParsed, link.Href)
			if err != nil || abs == "" {
				continue
			}
			normAbs, nErr := normalizeRawURL(abs)
			if nErr != nil {
				normAbs = abs
			}
			absParsed, err := url.Parse(normAbs)
			if err != nil {
				continue
			}
			switch v.Validate(absParsed, item.depth+1) {
			case URLStop:
				// Signal termination by cancelling crawlCtx via exhaustion logic.
				// Return immediately with what we have so far.
				return completion{finalURL: finalURL, resolvedLinks: resolvedLinks, etag: meta.ETag, lastModified: meta.LastModified}
			case URLSkip:
				continue
			}
			resolvedLinks = append(resolvedLinks, normAbs)
		}
	}

	return completion{finalURL: finalURL, resolvedLinks: resolvedLinks, etag: meta.ETag, lastModified: meta.LastModified}
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

// extractLinks parses HTML from r and returns the links found in <a> elements
// and the MetaRobots directives from <meta name="robots"> tags.
// Multi-valued rel is preserved as-is (space-separated); callers can use
// isNofollow to check individual tokens.
func extractLinks(r io.Reader) ([]Link, MetaRobots) {
	var links []Link
	var meta MetaRobots
	z := html.NewTokenizer(r)
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return links, meta
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tagName := string(name)
			if !hasAttr {
				continue
			}
			switch tagName {
			case "a":
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
			case "meta":
				var metaName, metaContent string
				for {
					key, val, more := z.TagAttr()
					switch strings.ToLower(string(key)) {
					case "name":
						metaName = strings.ToLower(string(val))
					case "content":
						metaContent = string(val)
					}
					if !more {
						break
					}
				}
				if metaName == "robots" {
					mr := parseRobotsContent(metaContent)
					if mr.NoIndex {
						meta.NoIndex = true
					}
					if mr.NoFollow {
						meta.NoFollow = true
					}
				}
			}
		}
	}
}

// parseRobotsContent parses a comma-separated robots directive string
// (from <meta name="robots"> or X-Robots-Tag) and returns a MetaRobots.
func parseRobotsContent(content string) MetaRobots {
	var mr MetaRobots
	for _, token := range strings.Split(content, ",") {
		switch strings.TrimSpace(strings.ToLower(token)) {
		case "noindex":
			mr.NoIndex = true
		case "nofollow":
			mr.NoFollow = true
		}
	}
	return mr
}

// parseXRobotsTag parses the X-Robots-Tag header value.
func parseXRobotsTag(header string) MetaRobots {
	return parseRobotsContent(header)
}

// normalizeRawURL parses and normalizes a raw URL string.
func normalizeRawURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return NormalizeURL(u), nil
}

// errResponseTooLarge is returned when a response body exceeds the configured limit.
var errResponseTooLarge = fmt.Errorf("response body exceeds size limit")

