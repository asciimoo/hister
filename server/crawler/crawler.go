// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
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

// errResponseTooLarge is returned when a response body exceeds the configured limit.
var errResponseTooLarge = fmt.Errorf("response body exceeds size limit")

// baseCrawler wraps a fetcher with BFS traversal logic.
type baseCrawler struct {
	fetcher        fetcher
	cfg            *config.CrawlerConfig
	robots         *RobotsCache // nil means robots.txt enforcement is disabled
	skipURLChecker SkipURLChecker
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
	return &baseCrawler{fetcher: f, cfg: cfg, robots: robots, skipURLChecker: o.skipURLChecker}, nil
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
	return c.fetcher.close()
}

type queueItem struct {
	rawURL string
	depth  int
}

func (c *baseCrawler) bfsCrawl(ctx context.Context, startURL string, v *Validator, ch chan<- *document.Document) {
	queue := []queueItem{{startURL, 0}}
	seen := map[string]struct{}{startURL: {}}

	for len(queue) > 0 {
		select {
		case <-ctx.Done():
			return
		default:
		}

		cur := queue[0]
		queue = queue[1:]

		parsedURL, err := url.Parse(cur.rawURL)
		if err != nil {
			continue
		}

		switch v.Validate(parsedURL, cur.depth) {
		case URLStop:
			return
		case URLSkip:
			log.Info().Str("url", cur.rawURL).Int("depth", cur.depth).Msg("crawler: skipping URL by crawler rules")
			continue
		}

		if c.robots != nil && !c.robots.Allowed(ctx, cur.rawURL) {
			log.Info().Str("url", cur.rawURL).Msg("crawler: skipping URL disallowed by robots.txt")
			continue
		}

		if c.skipURLChecker != nil {
			skip, err := c.skipURLChecker(cur.rawURL)
			if err != nil {
				log.Warn().Err(err).Str("url", cur.rawURL).Msg("crawler: failed to check whether URL should be skipped")
			} else if skip {
				log.Info().Str("url", cur.rawURL).Msg("crawler: skipping URL by prefetch skip predicate")
				continue
			}
		}

		if c.cfg.Delay > 0 {
			select {
			case <-time.After(time.Duration(c.cfg.Delay) * time.Second):
			case <-ctx.Done():
				return
			}
		}

		finalURL, body, links, _, err := c.fetcher.fetchPage(ctx, cur.rawURL, RequestHints{})
		if err != nil {
			log.Warn().Err(err).Str("url", cur.rawURL).Msg("crawler: failed to fetch page")
			continue
		}

		// If the server redirected to a different URL, mark it seen so it
		// won't be queued again (e.g. /path/ -> /path). Use the final URL
		// for the document and as the base for link resolution.
		if finalURL != cur.rawURL {
			seen[finalURL] = struct{}{}
		}
		finalParsed, err := url.Parse(finalURL)
		if err != nil {
			finalParsed = parsedURL
		}

		doc := &document.Document{
			URL:  finalURL,
			HTML: string(body),
		}

		select {
		case ch <- doc:
		case <-ctx.Done():
			return
		}

		for _, link := range links {
			abs, err := resolveURL(finalParsed, link.Href)
			if err != nil || abs == "" {
				continue
			}
			if _, exists := seen[abs]; !exists {
				seen[abs] = struct{}{}
				queue = append(queue, queueItem{abs, cur.depth + 1})
			}
		}
	}
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

// isNofollow returns true if the rel string contains "nofollow" as a token.
func isNofollow(rel string) bool {
	for _, token := range strings.Fields(rel) {
		if strings.EqualFold(token, "nofollow") {
			return true
		}
	}
	return false
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
