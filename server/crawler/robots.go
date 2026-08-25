// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"container/list"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/temoto/robotstxt"
)

const (
	defaultRobotsCacheTTL  = 24 * time.Hour
	shortRobotsCacheTTL    = 5 * time.Minute
	robotsCacheLRUCapacity = 1000
)

type robotsEntry struct {
	data        *robotstxt.RobotsData
	fetchedAt   time.Time
	ttl         time.Duration
	crawlDelay  time.Duration
	allowAll    bool
	denyAll     bool
}

// RobotsCache fetches and caches robots.txt files. It is safe for concurrent use.
type RobotsCache struct {
	mu        sync.Mutex
	cache     map[string]*list.Element // key -> LRU element
	lru       *list.List               // front = most recently used
	hostLocks sync.Map                 // host -> *sync.Mutex
	userAgent string
	client    *http.Client
	ttl       time.Duration
}

type lruEntry struct {
	key  string
	data *robotsEntry
}

// NewRobotsCache creates a RobotsCache that will identify itself using the
// given userAgent when fetching robots.txt files.
func NewRobotsCache(userAgent string) *RobotsCache {
	cache, _ := NewRobotsCacheWithProxy(userAgent, "")
	return cache
}

// NewRobotsCacheWithProxy creates a RobotsCache that uses proxy for robots.txt
// requests. The proxy accepts the same URL formats as crawler backends.
func NewRobotsCacheWithProxy(userAgent, proxy string) (*RobotsCache, error) {
	return NewRobotsCacheWithProxyAndTTL(userAgent, proxy, 0)
}

// NewRobotsCacheWithProxyAndTTL creates a RobotsCache with a custom cache TTL.
// A zero ttl uses the default (24 hours).
func NewRobotsCacheWithProxyAndTTL(userAgent, proxy string, ttl time.Duration) (*RobotsCache, error) {
	proxyURL, err := parseProxyURL(proxy)
	if err != nil {
		return nil, err
	}
	if ttl == 0 {
		ttl = defaultRobotsCacheTTL
	}
	return &RobotsCache{
		cache:     make(map[string]*list.Element),
		lru:       list.New(),
		userAgent: userAgent,
		client: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transportWithProxy(proxyURL),
		},
		ttl: ttl,
	}, nil
}

// hostMu returns (creating if necessary) the per-host mutex for serialising
// robots.txt fetches to the same origin.
func (r *RobotsCache) hostMu(host string) *sync.Mutex {
	v, _ := r.hostLocks.LoadOrStore(host, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// Allowed reports whether the given URL is allowed to be fetched according
// to the site's robots.txt.
func (r *RobotsCache) Allowed(ctx context.Context, rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	key := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)

	entry := r.getOrFetch(ctx, key, parsed.Host)
	if entry == nil || entry.allowAll {
		return true
	}
	if entry.denyAll {
		return false
	}

	agent := r.userAgent
	if agent == "" {
		agent = "*"
	}
	return entry.data.TestAgent(parsed.RequestURI(), agent)
}

// CrawlDelay returns the crawl delay specified in robots.txt for the host of rawURL.
func (r *RobotsCache) CrawlDelay(rawURL string) time.Duration {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return 0
	}
	key := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)

	r.mu.Lock()
	elem, ok := r.cache[key]
	r.mu.Unlock()

	if !ok {
		return 0
	}
	entry := elem.Value.(*lruEntry).data
	return entry.crawlDelay
}

// CrawlDelayForHost returns the crawl delay for a bare hostname by checking
// the https:// origin first, then http://, returning the longer of the two.
// Returns 0 if neither origin has been cached yet.
func (r *RobotsCache) CrawlDelayForHost(host string) time.Duration {
	https := r.CrawlDelay("https://" + host)
	http := r.CrawlDelay("http://" + host)
	if https > http {
		return https
	}
	return http
}

func (r *RobotsCache) getOrFetch(ctx context.Context, key, host string) *robotsEntry {
	// Check cache first (fast path, no per-host lock needed).
	r.mu.Lock()
	if elem, ok := r.cache[key]; ok {
		entry := elem.Value.(*lruEntry).data
		if time.Since(entry.fetchedAt) < entry.ttl {
			r.lru.MoveToFront(elem)
			r.mu.Unlock()
			return entry
		}
		// Expired - remove from cache so we re-fetch.
		r.lru.Remove(elem)
		delete(r.cache, key)
	}
	r.mu.Unlock()

	// Serialise fetches per host to avoid thundering herd.
	mu := r.hostMu(host)
	mu.Lock()
	defer mu.Unlock()

	// Double-check after acquiring the host lock.
	r.mu.Lock()
	if elem, ok := r.cache[key]; ok {
		entry := elem.Value.(*lruEntry).data
		if time.Since(entry.fetchedAt) < entry.ttl {
			r.lru.MoveToFront(elem)
			r.mu.Unlock()
			return entry
		}
	}
	r.mu.Unlock()

	entry := r.fetch(ctx, key)
	r.store(key, entry)
	return entry
}

func (r *RobotsCache) store(key string, entry *robotsEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if elem, ok := r.cache[key]; ok {
		r.lru.Remove(elem)
	}
	// Evict LRU entries if over capacity.
	for r.lru.Len() >= robotsCacheLRUCapacity {
		back := r.lru.Back()
		if back == nil {
			break
		}
		r.lru.Remove(back)
		delete(r.cache, back.Value.(*lruEntry).key)
	}
	elem := r.lru.PushFront(&lruEntry{key: key, data: entry})
	r.cache[key] = elem
}

// fetch retrieves and parses robots.txt for the given origin (scheme://host).
func (r *RobotsCache) fetch(ctx context.Context, origin string) *robotsEntry {
	now := time.Now()
	robotsURL := origin + "/robots.txt"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		return &robotsEntry{fetchedAt: now, ttl: shortRobotsCacheTTL, allowAll: true}
	}
	if r.userAgent != "" {
		req.Header.Set("User-Agent", r.userAgent)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		log.Debug().Err(err).Str("url", robotsURL).Msg("robots: failed to fetch")
		// Network error - allow for now but retry sooner.
		return &robotsEntry{fetchedAt: now, ttl: shortRobotsCacheTTL, allowAll: true}
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warn().Err(err).Msg("robots: failed to close response body")
		}
	}()

	if resp.StatusCode == http.StatusNotFound {
		// No robots.txt - allow everything.
		return &robotsEntry{fetchedAt: now, ttl: r.ttl, allowAll: true}
	}

	if resp.StatusCode >= 500 {
		// Server error - per RFC 9309: treat as deny-all.
		log.Debug().Int("status", resp.StatusCode).Str("url", robotsURL).Msg("robots: server error, treating as deny-all")
		return &robotsEntry{fetchedAt: now, ttl: r.ttl, denyAll: true}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Debug().Err(err).Str("url", robotsURL).Msg("robots: failed to read body")
		return &robotsEntry{fetchedAt: now, ttl: shortRobotsCacheTTL, allowAll: true}
	}

	data, err := robotstxt.FromBytes(body)
	if err != nil {
		log.Debug().Err(err).Str("url", robotsURL).Msg("robots: failed to parse")
		return &robotsEntry{fetchedAt: now, ttl: r.ttl, allowAll: true}
	}

	agent := r.userAgent
	if agent == "" {
		agent = "*"
	}
	group := data.FindGroup(agent)
	var crawlDelay time.Duration
	if group != nil && group.CrawlDelay > 0 {
		crawlDelay = group.CrawlDelay
	}

	log.Debug().Str("origin", origin).Msg("robots: cached robots.txt")
	return &robotsEntry{
		data:       data,
		fetchedAt:  now,
		ttl:        r.ttl,
		crawlDelay: crawlDelay,
	}
}
