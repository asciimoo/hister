// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"container/list"
	"context"
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

// HostStats tracks per-host counters.
type HostStats struct {
	Pages  atomic.Int64
	Bytes  atomic.Int64
	Errors atomic.Int64
}

// CrawlerStatsSnapshot is a point-in-time copy of CrawlerStats.
type CrawlerStatsSnapshot struct {
	Pages        int64
	Bytes        int64
	Count2xx     int64
	Count3xx     int64
	Count4xx     int64
	Count5xx     int64
	Retries      int64
	BreakerTrips int64
	RobotsDenials int64
	BudgetStops  int64
}

// CrawlerStats holds atomic counters for a single crawl.
type CrawlerStats struct {
	Pages         atomic.Int64
	Bytes         atomic.Int64
	Count2xx      atomic.Int64
	Count3xx      atomic.Int64
	Count4xx      atomic.Int64
	Count5xx      atomic.Int64
	Retries       atomic.Int64
	BreakerTrips  atomic.Int64
	RobotsDenials atomic.Int64
	BudgetStops   atomic.Int64
	PerHost       sync.Map // host -> *HostStats
}

// Snapshot returns a point-in-time copy of the stats.
func (s *CrawlerStats) Snapshot() CrawlerStatsSnapshot {
	return CrawlerStatsSnapshot{
		Pages:         s.Pages.Load(),
		Bytes:         s.Bytes.Load(),
		Count2xx:      s.Count2xx.Load(),
		Count3xx:      s.Count3xx.Load(),
		Count4xx:      s.Count4xx.Load(),
		Count5xx:      s.Count5xx.Load(),
		Retries:       s.Retries.Load(),
		BreakerTrips:  s.BreakerTrips.Load(),
		RobotsDenials: s.RobotsDenials.Load(),
		BudgetStops:   s.BudgetStops.Load(),
	}
}

func (s *CrawlerStats) hostStats(host string) *HostStats {
	v, _ := s.PerHost.LoadOrStore(host, &HostStats{})
	return v.(*HostStats)
}

// baseCrawler wraps a fetcher with BFS traversal logic.
type baseCrawler struct {
	fetcher        fetcher
	cfg            *config.CrawlerConfig
	robots         *RobotsCache
	skipURLChecker SkipURLChecker
	scheduler      *Scheduler
	budget         *Budget
	breaker        *CircuitBreaker
	backoff        *Backoff
	stats          *CrawlerStats
	clock          Clock
}

// New creates a Crawler backed by the backend specified in cfg.Backend.
// Accepted values are "chromedp" and "http" (default).
// Pass a non-nil RobotsCache to enforce robots.txt rules during crawling;
// pass nil to disable robots.txt checks entirely.
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
	clock := RealClock{}
	cooldown := time.Duration(cfg.CircuitBreaker.Cooldown) * time.Second
	if cooldown == 0 {
		cooldown = 5 * time.Minute
	}
	breaker := NewCircuitBreaker(cfg.CircuitBreaker.ConsecutiveFailures, cooldown, clock)
	budget := NewBudget(cfg.Limits, cfg.Hosts, clock)
	scheduler := NewScheduler(cfg, breaker, robots, clock)
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
		scheduler:      scheduler,
		budget:         budget,
		breaker:        breaker,
		backoff:        NewBackoff(initial, maxB),
		stats:          &CrawlerStats{},
		clock:          clock,
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

// Crawl starts a BFS crawl from startURL. It returns a channel on which
// *document.Document values are sent (URL and HTML fields populated) for every
// successfully fetched page. The channel is closed when the crawl ends.
func (c *baseCrawler) Crawl(ctx context.Context, startURL string, v *Validator) (<-chan *document.Document, error) {
	if _, err := url.Parse(startURL); err != nil {
		return nil, fmt.Errorf("invalid start URL: %w", err)
	}
	ch := make(chan *document.Document)
	go func() {
		defer close(ch)
		c.bfsCrawl(ctx, startURL, v, ch)
	}()
	return ch, nil
}

// Close releases resources held by the underlying backend.
func (c *baseCrawler) Close() error {
	c.scheduler.Stop()
	return c.fetcher.close()
}

type queueItem struct {
	rawURL string
	depth  int
}

// seenSet deduplicates URLs using FNV-64 hashes to keep memory compact.
type seenSet struct {
	m map[uint64]struct{}
}

func newSeenSet() *seenSet {
	return &seenSet{m: make(map[uint64]struct{})}
}

func (s *seenSet) add(key string) {
	s.m[hashURL(key)] = struct{}{}
}

func (s *seenSet) contains(key string) bool {
	_, ok := s.m[hashURL(key)]
	return ok
}

func hashURL(key string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return h.Sum64()
}

func (c *baseCrawler) bfsCrawl(ctx context.Context, startURL string, v *Validator, ch chan<- *document.Document) {
	// Graceful shutdown: crawlCtx cancels when caller cancels. fetchCtx is
	// cancelled after shutdown grace period.
	grace := time.Duration(c.cfg.ShutdownGrace) * time.Second
	if grace == 0 {
		grace = 30 * time.Second
	}

	crawlCtx, crawlCancel := context.WithCancel(ctx)
	defer crawlCancel()

	fetchCtx, fetchCancel := context.WithCancel(context.Background())
	defer fetchCancel()

	// When crawlCtx is cancelled, start grace-period countdown then cancel fetchCtx.
	go func() {
		<-crawlCtx.Done()
		select {
		case <-c.clock.After(grace):
		case <-fetchCtx.Done():
		}
		fetchCancel()
	}()

	queue := list.New()
	seen := newSeenSet()

	normStart, err := normalizeRawURL(startURL)
	if err != nil {
		normStart = startURL
	}
	seen.add(normStart)
	queue.PushBack(queueItem{startURL, 0})

	concurrency := c.cfg.Rate.GlobalConcurrency
	if concurrency < 1 {
		concurrency = 1
	}

	type result struct {
		item      queueItem
		finalURL  string
		body      []byte
		links     []Link
		meta      FetchMeta
		err       error
		attempt   int
		durationMs int64
	}

	work := make(chan queueItem, concurrency)
	results := make(chan result, concurrency)
	var wg sync.WaitGroup

	// Worker goroutines.
	maxAttempts := c.cfg.Retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range work {
				parsedURL, err := url.Parse(item.rawURL)
				if err != nil {
					results <- result{item: item, err: err}
					continue
				}
				host := parsedURL.Hostname()

				var res result
				res.item = item

				for attempt := 0; attempt < maxAttempts; attempt++ {
					if attempt > 0 {
						c.stats.Retries.Add(1)
						wait := c.backoff.Duration(attempt)
						select {
						case <-fetchCtx.Done():
							res.err = fetchCtx.Err()
							goto done
						case <-c.clock.After(wait):
						}
					}

					if err := c.scheduler.Wait(crawlCtx, host); err != nil {
						res.err = err
						goto done
					}

					start := c.clock.Now()
					finalURL, body, links, meta, fetchErr := c.fetcher.fetchPage(fetchCtx, item.rawURL, RequestHints{})
					elapsed := c.clock.Now().Sub(start)

					c.scheduler.Release(host)

					res.attempt = attempt + 1
					res.durationMs = elapsed.Milliseconds()

					if fetchErr != nil {
						retryable, retryAfter, statusCode := ClassifyError(fetchErr)
						log.Warn().
							Err(fetchErr).
							Str("url", item.rawURL).
							Str("host", host).
							Int("status", statusCode).
							Int("attempt", attempt+1).
							Msg("crawler: fetch error")

						if retryAfter > 0 {
							c.scheduler.Cooldown(host, retryAfter)
						}

						if retryable && attempt < maxAttempts-1 {
							c.breaker.RecordFailure(host)
							continue
						}
						c.breaker.RecordFailure(host)
						res.err = fetchErr
						goto done
					}

					c.breaker.RecordSuccess(host)
					res.finalURL = finalURL
					res.body = body
					res.links = links
					res.meta = meta
					goto done
				}
			done:
				results <- res
			}
		}()
	}

	// Close results when all workers are done.
	go func() {
		wg.Wait()
		close(results)
	}()

	// Dispatcher: feed work to workers, collect results.
	inFlight := 0
	dispatch := func() {
		for inFlight < concurrency && queue.Len() > 0 {
			front := queue.Front()
			queue.Remove(front)
			item := front.Value.(queueItem)

			select {
			case <-crawlCtx.Done():
				return
			case work <- item:
				inFlight++
			}
		}
	}

	dispatch()

	for inFlight > 0 {
		res, ok := <-results
		if !ok {
			break
		}
		inFlight--

		if res.err != nil {
			hs := c.stats.hostStats("")
			if res.item.rawURL != "" {
				parsed, pErr := url.Parse(res.item.rawURL)
				if pErr == nil {
					hs = c.stats.hostStats(parsed.Hostname())
				}
			}
			hs.Errors.Add(1)
			dispatch()
			continue
		}

		// Re-validate the final URL (it may differ from the queued URL after redirects).
		finalParsed, err := url.Parse(res.finalURL)
		if err != nil {
			dispatch()
			continue
		}

		host := finalParsed.Hostname()
		hs := c.stats.hostStats(host)

		// Record stats.
		bodyLen := int64(len(res.body))
		c.stats.Pages.Add(1)
		c.stats.Bytes.Add(bodyLen)
		hs.Pages.Add(1)
		hs.Bytes.Add(bodyLen)
		c.budget.AddBytes(host, bodyLen)

		switch {
		case res.meta.StatusCode >= 200 && res.meta.StatusCode < 300:
			c.stats.Count2xx.Add(1)
		case res.meta.StatusCode >= 300 && res.meta.StatusCode < 400:
			c.stats.Count3xx.Add(1)
		case res.meta.StatusCode >= 400 && res.meta.StatusCode < 500:
			c.stats.Count4xx.Add(1)
		case res.meta.StatusCode >= 500:
			c.stats.Count5xx.Add(1)
		}

		log.Info().
			Str("url", res.finalURL).
			Str("host", host).
			Int("status", res.meta.StatusCode).
			Int64("bytes", bodyLen).
			Int64("duration_ms", res.durationMs).
			Int("attempt", res.attempt).
			Str("breaker_state", breakerStateName(c.breaker.State(host))).
			Msg("crawler: fetched page")

		// Mark final URL as seen if it differs from the queued URL.
		normFinal, err := normalizeRawURL(res.finalURL)
		if err != nil {
			normFinal = res.finalURL
		}
		seen.add(normFinal)

		doc := &document.Document{
			URL:          res.finalURL,
			HTML:         string(res.body),
			ETag:         res.meta.ETag,
			LastModified: res.meta.LastModified,
		}

		select {
		case ch <- doc:
		case <-crawlCtx.Done():
			return
		}

		if !v.Rules().NoDepth {
			for _, link := range res.links {
				// Skip nofollow links.
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
				if seen.contains(normAbs) {
					continue
				}
				seen.add(normAbs)

				absParsed, err := url.Parse(abs)
				if err != nil {
					continue
				}
				switch v.Validate(absParsed, res.item.depth+1) {
				case URLStop:
					c.stats.BudgetStops.Add(1)
					return
				case URLSkip:
					continue
				}
				queue.PushBack(queueItem{abs, res.item.depth + 1})
			}
		}

		dispatch()
	}

	close(work)
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

// normalizeRawURL parses and normalizes a raw URL string.
func normalizeRawURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return NormalizeURL(u), nil
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
// It captures the href and rel attributes. Multi-valued rel is preserved as-is
// (space-separated); callers can use isNofollow to check individual tokens.
func extractLinks(r io.Reader) []Link {
	var links []Link
	z := html.NewTokenizer(r)
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return links
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			if !hasAttr || string(name) != "a" {
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

// activeRegistry holds references to active crawlers for debug inspection.
var activeRegistry sync.Map // id -> *baseCrawler

// RegisterActive registers c under id for debug inspection.
func RegisterActive(id string, c *baseCrawler) {
	activeRegistry.Store(id, c)
}

// Unregister removes id from the active registry.
func Unregister(id string) {
	activeRegistry.Delete(id)
}

// errResponseTooLarge is returned when a response body exceeds the configured limit.
var errResponseTooLarge = fmt.Errorf("response body exceeds size limit")
