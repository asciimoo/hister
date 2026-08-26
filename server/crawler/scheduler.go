// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"container/list"
	"context"
	"math"
	randv2 "math/rand/v2"
	"sync"
	"time"

	"github.com/asciimoo/hister/config"
)

const hostLRUCap = 10000

// lazyBucket is a goroutine-free token bucket. Tokens are computed lazily on
// each Wait call based on elapsed time since the last refill.
type lazyBucket struct {
	mu         sync.Mutex
	rate       float64 // tokens per second
	tokens     float64
	burst      float64
	lastRefill time.Time
}

func newLazyBucket(rps float64, burst int) *lazyBucket {
	if rps <= 0 {
		rps = 10
	}
	b := 1.0
	if burst > 1 {
		b = float64(burst)
	}
	return &lazyBucket{
		rate:       rps,
		tokens:     b, // pre-filled
		burst:      b,
		lastRefill: time.Now(),
	}
}

func (lb *lazyBucket) Wait(ctx context.Context) error {
	for {
		lb.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(lb.lastRefill).Seconds()
		lb.tokens = math.Min(lb.burst, lb.tokens+elapsed*lb.rate)
		lb.lastRefill = now

		if lb.tokens >= 1 {
			lb.tokens--
			lb.mu.Unlock()
			return nil
		}

		// Compute wait until we have one token.
		wait := time.Duration((1.0-lb.tokens)/lb.rate*1e9) * time.Nanosecond
		lb.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// hostState holds per-host scheduler state.
type hostState struct {
	bucket    *lazyBucket
	inflight  chan struct{} // semaphore
	coolUntil time.Time
	lruElem   *list.Element
}

// Scheduler controls when fetches are dispatched based on rate limits,
// per-host concurrency, circuit breaker state, and crawl budget.
type Scheduler struct {
	cfg           config.CrawlerRate
	respectDelay  bool
	hostOverrides map[string]config.CrawlerHostOverride
	global        *lazyBucket
	breaker       *CircuitBreaker
	robots        *RobotsCache
	clock         Clock

	mu      sync.Mutex
	hosts   map[string]*hostState
	lru     *list.List // front = most recently used; Value = host string
}

// NewScheduler creates a Scheduler. cfg is the full crawler configuration.
func NewScheduler(cfg *config.CrawlerConfig, breaker *CircuitBreaker, robots *RobotsCache, clock Clock) *Scheduler {
	if clock == nil {
		clock = RealClock{}
	}
	return &Scheduler{
		cfg:           cfg.Rate,
		respectDelay:  cfg.Robots.RespectCrawlDelay,
		hostOverrides: cfg.Hosts,
		global:        newLazyBucket(cfg.Rate.GlobalRPS, cfg.Rate.GlobalConcurrency),
		breaker:       breaker,
		robots:        robots,
		clock:         clock,
		hosts:         make(map[string]*hostState),
		lru:           list.New(),
	}
}

func (s *Scheduler) effectiveRPS(host string) float64 {
	rps := s.cfg.PerHostRPS
	if rps <= 0 {
		rps = 1
	}

	// Apply per-host override from config.
	if ov, ok := s.hostOverrides[host]; ok && ov.PerHostRPS > 0 {
		rps = ov.PerHostRPS
	}

	// Apply robots.txt Crawl-delay if configured.
	if s.respectDelay && s.robots != nil {
		delay := s.robots.CrawlDelayForHost(host)
		if delay > 0 {
			robotsRPS := 1.0 / delay.Seconds()
			if robotsRPS < rps {
				rps = robotsRPS
			}
		}
	}

	return rps
}

func (s *Scheduler) getHostState(host string) *hostState {
	s.mu.Lock()
	defer s.mu.Unlock()

	if hs, ok := s.hosts[host]; ok {
		s.lru.MoveToFront(hs.lruElem)
		return hs
	}

	rps := s.effectiveRPS(host)
	concurrency := s.cfg.PerHostConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	hs := &hostState{
		bucket:   newLazyBucket(rps, 1),
		inflight: make(chan struct{}, concurrency),
	}
	for i := 0; i < concurrency; i++ {
		hs.inflight <- struct{}{}
	}

	// Evict LRU entry if at cap.
	if s.lru.Len() >= hostLRUCap {
		back := s.lru.Back()
		if back != nil {
			s.lru.Remove(back)
			delete(s.hosts, back.Value.(string))
		}
	}

	elem := s.lru.PushFront(host)
	hs.lruElem = elem
	s.hosts[host] = hs
	return hs
}

// Wait blocks until it is safe to fetch from host. It respects:
// (1) circuit breaker state, (2) global token bucket, (3) per-host token
// bucket, (4) per-host cooldown (Retry-After), (5) per-host in-flight limit.
// Applies jitter to the per-host wait.
func (s *Scheduler) Wait(ctx context.Context, host string) error {
	// Check circuit breaker.
	if s.breaker != nil && !s.breaker.Allow(host) {
		return &errBreakerOpen{host: host}
	}

	// Global rate limit.
	if err := s.global.Wait(ctx); err != nil {
		return err
	}

	hs := s.getHostState(host)

	// Per-host cooldown (Retry-After).
	s.mu.Lock()
	coolUntil := hs.coolUntil
	s.mu.Unlock()

	if !coolUntil.IsZero() {
		remaining := coolUntil.Sub(s.clock.Now())
		if remaining > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-s.clock.After(remaining):
			}
		}
	}

	// Per-host token (rate).
	if err := hs.bucket.Wait(ctx); err != nil {
		return err
	}

	// Jitter.
	if s.cfg.Jitter > 0 {
		jitterMax := s.cfg.Jitter / s.cfg.PerHostRPS
		if s.cfg.PerHostRPS <= 0 {
			jitterMax = s.cfg.Jitter
		}
		jitter := time.Duration(randv2.Float64() * jitterMax * float64(time.Second))
		if jitter > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-s.clock.After(jitter):
			}
		}
	}

	// Per-host in-flight semaphore.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-hs.inflight:
	}

	return nil
}

// Release returns the in-flight slot for host after a fetch completes.
func (s *Scheduler) Release(host string) {
	hs := s.getHostState(host)
	hs.inflight <- struct{}{}
}

// Cooldown marks host as cooling down until now+d, based on a Retry-After header.
func (s *Scheduler) Cooldown(host string, d time.Duration) {
	hs := s.getHostState(host)
	s.mu.Lock()
	hs.coolUntil = s.clock.Now().Add(d)
	s.mu.Unlock()
}

// Stop is a no-op. The lazy token buckets have no goroutines to stop.
func (s *Scheduler) Stop() {}

type errBreakerOpen struct{ host string }

func (e *errBreakerOpen) Error() string { return "circuit breaker open for host: " + e.host }
