// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"container/list"
	"context"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/asciimoo/hister/config"
)

const hostLRUCap = 10000

// BreakerState represents the state of a circuit breaker for a single host.
type BreakerState int

const (
	BreakerClosed   BreakerState = iota // normal operation
	BreakerOpen                         // requests blocked
	BreakerHalfOpen                     // one probe allowed
)

type hostEntry struct {
	limiter    *rate.Limiter
	inflightCh chan struct{} // semaphore, cap = per-host concurrency
	coolUntil  time.Time
	// breaker
	breakerState BreakerState
	failures     int
	lastFailure  time.Time
	probing      bool
	// budget
	pages atomic.Int64
	bytes atomic.Int64
	// lru
	lruElem *list.Element
}

// Coordinator is the single source of truth for per-host and global crawl
// resource management. It merges the former Scheduler, CircuitBreaker, and
// Budget types into one structure with one lock.
type Coordinator struct {
	cfg           *config.CrawlerConfig
	globalLimiter *rate.Limiter
	globalPages   atomic.Int64
	globalBytes   atomic.Int64
	startTime     time.Time
	threshold     int           // breaker threshold
	cooldown      time.Duration // breaker cooldown
	mu            sync.Mutex
	hosts         map[string]*hostEntry
	lru           *list.List
}

// NewCoordinator creates a Coordinator from the given config.
func NewCoordinator(cfg *config.CrawlerConfig) *Coordinator {
	threshold := cfg.CircuitBreaker.ConsecutiveFailures
	if threshold <= 0 {
		threshold = 5
	}
	cooldown := time.Duration(cfg.CircuitBreaker.Cooldown) * time.Second
	if cooldown <= 0 {
		cooldown = 5 * time.Minute
	}

	var globalLimiter *rate.Limiter
	if cfg.Rate.GlobalRPS <= 0 {
		globalLimiter = rate.NewLimiter(rate.Inf, 0)
	} else {
		burst := cfg.Rate.GlobalConcurrency
		if burst < 1 {
			burst = 1
		}
		globalLimiter = rate.NewLimiter(rate.Limit(cfg.Rate.GlobalRPS), burst)
	}

	return &Coordinator{
		cfg:           cfg,
		globalLimiter: globalLimiter,
		startTime:     time.Now(),
		threshold:     threshold,
		cooldown:      cooldown,
		hosts:         make(map[string]*hostEntry),
		lru:           list.New(),
	}
}

func (c *Coordinator) effectiveRPS(host string) float64 {
	rps := c.cfg.Rate.PerHostRPS
	if rps <= 0 {
		rps = 1
	}
	if ov, ok := c.cfg.Hosts[host]; ok && ov.PerHostRPS > 0 {
		rps = ov.PerHostRPS
	}
	return rps
}

func (c *Coordinator) getHostEntry(host string) *hostEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getHostEntryLocked(host)
}

// Wait blocks until it is safe to fetch from host. Checks circuit breaker,
// global limiter, per-host limiter, cooldown, jitter, then acquires the
// in-flight semaphore.
func (c *Coordinator) Wait(ctx context.Context, host string) error {
	// Circuit breaker check.
	if !c.breakerAllow(host) {
		return &errBreakerOpen{host: host}
	}

	// Global rate limit.
	if err := c.globalLimiter.Wait(ctx); err != nil {
		return err
	}

	he := c.getHostEntry(host)
	effectiveRate := c.effectiveRPS(host)
	he.limiter.SetLimit(rate.Limit(effectiveRate))

	// Per-host cooldown (Retry-After).
	c.mu.Lock()
	coolUntil := he.coolUntil
	c.mu.Unlock()

	if !coolUntil.IsZero() {
		remaining := time.Until(coolUntil)
		if remaining > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(remaining):
			}
		}
	}

	// Per-host token bucket.
	if err := he.limiter.Wait(ctx); err != nil {
		return err
	}

	// Jitter.
	if c.cfg.Rate.Jitter > 0 {
		const maxJitter = 30 * time.Second
		r := effectiveRate
		if r <= 0 {
			r = 1
		}
		jitterMax := c.cfg.Rate.Jitter / r
		jitter := time.Duration(rand.Float64() * jitterMax * float64(time.Second))
		if jitter > maxJitter {
			jitter = maxJitter
		}
		if jitter > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(jitter):
			}
		}
	}

	// Per-host in-flight semaphore.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-he.inflightCh:
	}

	return nil
}

// Release returns the in-flight slot for host after a fetch completes.
func (c *Coordinator) Release(host string) {
	he := c.getHostEntry(host)
	he.inflightCh <- struct{}{}
}

// Cooldown marks host as cooling down until now+d, based on a Retry-After header.
func (c *Coordinator) Cooldown(host string, d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	he := c.getHostEntryLocked(host)
	he.coolUntil = time.Now().Add(d)
}

// breakerAllow returns true if a request to host is permitted by the circuit breaker.
func (c *Coordinator) breakerAllow(host string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	he := c.getHostEntryLocked(host)
	switch he.breakerState {
	case BreakerClosed:
		return true
	case BreakerOpen:
		if time.Since(he.lastFailure) >= c.cooldown {
			he.breakerState = BreakerHalfOpen
			he.probing = true
			return true
		}
		return false
	case BreakerHalfOpen:
		if !he.probing {
			he.probing = true
			return true
		}
		return false
	}
	return true
}

// getHostEntryLocked returns the hostEntry for host, creating it (with LRU
// tracking and eviction) if absent. Must be called with c.mu held.
func (c *Coordinator) getHostEntryLocked(host string) *hostEntry {
	if he, ok := c.hosts[host]; ok {
		c.lru.MoveToFront(he.lruElem)
		return he
	}

	rps := c.effectiveRPS(host)
	concurrency := c.cfg.Rate.PerHostConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	he := &hostEntry{
		limiter:    rate.NewLimiter(rate.Limit(rps), 1),
		inflightCh: make(chan struct{}, concurrency),
	}
	for i := 0; i < concurrency; i++ {
		he.inflightCh <- struct{}{}
	}

	// Evict LRU entry if at cap, skipping hosts with in-flight requests to
	// avoid corrupting concurrency accounting.
	if c.lru.Len() >= hostLRUCap {
		const maxEvictAttempts = 4
		candidate := c.lru.Back()
		for i := 0; i < maxEvictAttempts && candidate != nil; i++ {
			victimHost := candidate.Value.(string)
			victimEntry := c.hosts[victimHost]
			if victimEntry == nil || len(victimEntry.inflightCh) == cap(victimEntry.inflightCh) {
				c.lru.Remove(candidate)
				delete(c.hosts, victimHost)
				break
			}
			candidate = candidate.Prev()
		}
	}

	elem := c.lru.PushFront(host)
	he.lruElem = elem
	c.hosts[host] = he
	return he
}

// RecordSuccess resets the failure count and closes the circuit breaker for host.
func (c *Coordinator) RecordSuccess(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	he := c.getHostEntryLocked(host)
	he.failures = 0
	he.probing = false
	he.breakerState = BreakerClosed
}

// RecordFailure increments the failure count and may open the circuit breaker.
func (c *Coordinator) RecordFailure(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	he := c.getHostEntryLocked(host)
	he.failures++
	he.probing = false
	he.lastFailure = time.Now()
	if he.failures >= c.threshold {
		he.breakerState = BreakerOpen
	}
}

// HostBreakerState returns the current circuit breaker state for host.
func (c *Coordinator) HostBreakerState(host string) BreakerState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getHostEntryLocked(host).breakerState
}

// TryReservePage atomically increments the per-host and global page counters
// if both are under their limits. Add-then-check-then-compensate: reserves
// speculatively, then rolls back on overshoot. Under concurrency N workers
// may each briefly push a counter to limit+k before compensation restores
// it, but the observable effect is that only exactly-limit reservations
// return true. Reads via Load (e.g. Exhausted) never observe > limit + N-1
// transiently, which is acceptable for our termination heuristic.
func (c *Coordinator) TryReservePage(host string) bool {
	he := c.getHostEntry(host)

	hostMaxPages := c.cfg.Limits.MaxPagesPerHost
	if ov, ok := c.cfg.Hosts[host]; ok && ov.MaxPages > 0 {
		hostMaxPages = ov.MaxPages
	}

	if hostMaxPages > 0 {
		if he.pages.Add(1) > int64(hostMaxPages) {
			he.pages.Add(-1)
			return false
		}
	} else {
		he.pages.Add(1)
	}

	if c.cfg.Limits.MaxPages > 0 {
		if c.globalPages.Add(1) > int64(c.cfg.Limits.MaxPages) {
			c.globalPages.Add(-1)
			he.pages.Add(-1)
			return false
		}
	} else {
		c.globalPages.Add(1)
	}

	return true
}

// AddBytes records n bytes fetched for the given host.
func (c *Coordinator) AddBytes(host string, n int64) {
	c.globalBytes.Add(n)
	c.getHostEntry(host).bytes.Add(n)
}

// Exhausted returns true if any global budget limit has been reached.
func (c *Coordinator) Exhausted() bool {
	if c.cfg.Limits.MaxPages > 0 && c.globalPages.Load() >= int64(c.cfg.Limits.MaxPages) {
		return true
	}
	if c.cfg.Limits.MaxDuration > 0 {
		if time.Since(c.startTime) >= time.Duration(c.cfg.Limits.MaxDuration)*time.Second {
			return true
		}
	}
	return false
}

// HostExhausted returns true if the per-host page or byte limit has been reached.
func (c *Coordinator) HostExhausted(host string) bool {
	he := c.getHostEntry(host)

	hostMaxPages := c.cfg.Limits.MaxPagesPerHost
	if ov, ok := c.cfg.Hosts[host]; ok && ov.MaxPages > 0 {
		hostMaxPages = ov.MaxPages
	}
	if hostMaxPages > 0 && he.pages.Load() >= int64(hostMaxPages) {
		return true
	}
	if c.cfg.Limits.MaxBytesPerHost > 0 && he.bytes.Load() >= c.cfg.Limits.MaxBytesPerHost {
		return true
	}
	return false
}

type errBreakerOpen struct{ host string }

func (e *errBreakerOpen) Error() string { return "circuit breaker open for host: " + e.host }
