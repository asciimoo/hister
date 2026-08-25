// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/asciimoo/hister/config"
)

type hostBudget struct {
	pages atomic.Int64
	bytes atomic.Int64
}

// Budget tracks global and per-host resource consumption against configured limits.
// All methods are safe for concurrent use.
type Budget struct {
	limits        config.CrawlerLimits
	startTime     time.Time
	clock         Clock
	pages         atomic.Int64
	bytes         atomic.Int64
	perHost       sync.Map // host -> *hostBudget
	hostOverrides map[string]config.CrawlerHostOverride
}

// NewBudget creates a Budget with the given limits and overrides.
func NewBudget(limits config.CrawlerLimits, overrides map[string]config.CrawlerHostOverride, clock Clock) *Budget {
	if clock == nil {
		clock = RealClock{}
	}
	return &Budget{
		limits:        limits,
		startTime:     clock.Now(),
		clock:         clock,
		hostOverrides: overrides,
	}
}

func (b *Budget) hostBudget(host string) *hostBudget {
	v, _ := b.perHost.LoadOrStore(host, &hostBudget{})
	return v.(*hostBudget)
}

// TryReservePage atomically checks and increments the page counter for both
// global and per-host limits. Returns false if either limit would be exceeded.
func (b *Budget) TryReservePage(host string) bool {
	hb := b.hostBudget(host)

	// Check per-host page limit (from override or global config).
	hostMaxPages := b.limits.MaxPagesPerHost
	if ov, ok := b.hostOverrides[host]; ok && ov.MaxPages > 0 {
		hostMaxPages = ov.MaxPages
	}
	if hostMaxPages > 0 {
		cur := hb.pages.Load()
		if cur >= int64(hostMaxPages) {
			return false
		}
	}

	// Check global page limit.
	if b.limits.MaxPages > 0 {
		cur := b.pages.Load()
		if cur >= int64(b.limits.MaxPages) {
			return false
		}
	}

	b.pages.Add(1)
	hb.pages.Add(1)
	return true
}

// AddBytes records n bytes fetched for the given host.
func (b *Budget) AddBytes(host string, n int64) {
	b.bytes.Add(n)
	b.hostBudget(host).bytes.Add(n)
}

// Exhausted returns true if any global limit has been reached.
func (b *Budget) Exhausted() bool {
	if b.limits.MaxPages > 0 && b.pages.Load() >= int64(b.limits.MaxPages) {
		return true
	}
	if b.limits.MaxDuration > 0 {
		elapsed := b.clock.Now().Sub(b.startTime)
		if elapsed >= time.Duration(b.limits.MaxDuration)*time.Second {
			return true
		}
	}
	return false
}

// HostExhausted returns true if the per-host page or byte limit has been reached.
func (b *Budget) HostExhausted(host string) bool {
	hb := b.hostBudget(host)

	hostMaxPages := b.limits.MaxPagesPerHost
	if ov, ok := b.hostOverrides[host]; ok && ov.MaxPages > 0 {
		hostMaxPages = ov.MaxPages
	}
	if hostMaxPages > 0 && hb.pages.Load() >= int64(hostMaxPages) {
		return true
	}
	if b.limits.MaxBytesPerHost > 0 && hb.bytes.Load() >= b.limits.MaxBytesPerHost {
		return true
	}
	return false
}
