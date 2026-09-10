// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/asciimoo/hister/config"
)

func newTestCoordinator(rate config.CrawlerRate) *Coordinator {
	cfg := &config.CrawlerConfig{
		Rate: rate,
		CircuitBreaker: config.CrawlerBreaker{
			ConsecutiveFailures: 5,
			Cooldown:            300, // 5 min in seconds
		},
	}
	return NewCoordinator(cfg)
}

func TestCoordinatorPerHostRateSpacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config.CrawlerRate{
			GlobalRPS:          100,
			PerHostRPS:         2,
			GlobalConcurrency:  10,
			PerHostConcurrency: 1,
			Jitter:             0,
		}
		coord := newTestCoordinator(cfg)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// First request should pass immediately (limiter starts full).
		done := make(chan error, 1)
		go func() {
			done <- coord.Wait(ctx, "example.com")
		}()

		synctest.Wait()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("first Wait error: %v", err)
			}
		default:
			t.Fatal("first Wait should have completed immediately")
		}

		coord.Release("example.com")
	})
}

func TestCoordinatorRetryAfterCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config.CrawlerRate{
			GlobalRPS:          100,
			PerHostRPS:         100,
			GlobalConcurrency:  10,
			PerHostConcurrency: 5,
			Jitter:             0,
		}
		coord := newTestCoordinator(cfg)

		// Install a 10-second cooldown.
		coord.Cooldown("slow.example.com", 10*time.Second)

		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan error, 1)
		go func() {
			done <- coord.Wait(ctx, "slow.example.com")
		}()

		// Wait for the goroutine to block on cooldown.
		synctest.Wait()

		select {
		case <-done:
			t.Fatal("Wait should have blocked during cooldown")
		default:
			// Still blocked - expected.
		}

		cancel() // unblock
		synctest.Wait()
		<-done
	})
}

func TestCoordinatorBreakerInteraction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := &config.CrawlerConfig{
			Rate: config.CrawlerRate{
				GlobalRPS:          100,
				PerHostRPS:         100,
				GlobalConcurrency:  10,
				PerHostConcurrency: 5,
				Jitter:             0,
			},
			CircuitBreaker: config.CrawlerBreaker{
				ConsecutiveFailures: 1,
				Cooldown:            300,
			},
		}
		coord := NewCoordinator(cfg)

		// Trip the breaker.
		coord.RecordFailure("tripped.example.com")
		if coord.HostBreakerState("tripped.example.com") != BreakerOpen {
			t.Fatal("breaker should be open after failure")
		}

		ctx := context.Background()
		err := coord.Wait(ctx, "tripped.example.com")
		if err == nil {
			t.Fatal("Wait should return error when breaker is open")
		}
		var breakerErr *errBreakerOpen
		if !errors.As(err, &breakerErr) {
			t.Errorf("expected errBreakerOpen, got %T: %v", err, err)
		}
	})
}

func TestCoordinatorLRUEviction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config.CrawlerRate{
			GlobalRPS:          1000,
			PerHostRPS:         1000,
			GlobalConcurrency:  1000,
			PerHostConcurrency: 1,
			Jitter:             0,
		}
		coord := newTestCoordinator(cfg)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Fill to the cap.
		firstHost := "host-0.com"
		for i := 0; i < hostLRUCap; i++ {
			host := fmt.Sprintf("host-%d.com", i)
			if i == 0 {
				firstHost = host
			}
			coord.getHostEntry(host)
		}

		coord.mu.Lock()
		lenAtCap := len(coord.hosts)
		coord.mu.Unlock()

		if lenAtCap != hostLRUCap {
			t.Fatalf("expected %d hosts at cap, got %d", hostLRUCap, lenAtCap)
		}

		// Adding one more should evict the oldest (firstHost).
		_ = coord.Wait(ctx, "newcomer.example.com") //nolint - error fine for test
		coord.Release("newcomer.example.com")

		synctest.Wait()

		coord.mu.Lock()
		lenAfter := len(coord.hosts)
		_, newcomerPresent := coord.hosts["newcomer.example.com"]
		_, firstPresent := coord.hosts[firstHost]
		coord.mu.Unlock()

		if lenAfter != hostLRUCap {
			t.Fatalf("expected map len %d after eviction, got %d", hostLRUCap, lenAfter)
		}
		if !newcomerPresent {
			t.Fatal("newcomer host should be present after eviction")
		}
		if firstPresent {
			t.Fatalf("oldest host %q should have been evicted", firstHost)
		}
	})
}

func TestCoordinatorHostOverride(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := &config.CrawlerConfig{
			Rate: config.CrawlerRate{
				GlobalRPS:          1000,
				PerHostRPS:         1000,
				GlobalConcurrency:  10,
				PerHostConcurrency: 1,
				Jitter:             0,
			},
			CircuitBreaker: config.CrawlerBreaker{
				ConsecutiveFailures: 5,
				Cooldown:            300,
			},
			Hosts: map[string]config.CrawlerHostOverride{
				"slow.example.com": {PerHostRPS: 0.001},
			},
		}
		coord := NewCoordinator(cfg)

		// First request passes (limiter starts full).
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		err := coord.Wait(ctx, "slow.example.com")
		if err != nil {
			t.Fatalf("first Wait on overridden host: %v", err)
		}
		coord.Release("slow.example.com")

		// Second request should block because rate is 0.001 rps.
		ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel2()

		done := make(chan error, 1)
		go func() {
			done <- coord.Wait(ctx2, "slow.example.com")
		}()
		synctest.Wait()

		select {
		case err2 := <-done:
			if err2 == nil {
				coord.Release("slow.example.com")
				t.Fatal("second Wait on overridden host should have timed out")
			}
		default:
			// Still blocked - advance time to trigger timeout.
			time.Sleep(100 * time.Millisecond)
			synctest.Wait()
			err2 := <-done
			if err2 == nil {
				coord.Release("slow.example.com")
				t.Fatal("second Wait on overridden host should have timed out due to low RPS")
			}
		}

		// Default host should not be throttled.
		ctx3, cancel3 := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel3()
		if err := coord.Wait(ctx3, "fast.example.com"); err != nil {
			t.Fatalf("Wait on non-overridden host: %v", err)
		}
		coord.Release("fast.example.com")
	})
}

// TestCoordinatorCooldownAppliesToWaitingWorkers covers a Retry-After that
// arrives while other workers are already queued for the same host. A worker
// that checked the cooldown before blocking for a rate token must not fetch on
// the strength of that stale check.
func TestCoordinatorCooldownAppliesToWaitingWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		coord := newTestCoordinator(config.CrawlerRate{
			GlobalRPS:          100,
			PerHostRPS:         1,
			GlobalConcurrency:  10,
			PerHostConcurrency: 2,
		})
		ctx := context.Background()

		// The first caller takes the only token, so the second one waits.
		if err := coord.Wait(ctx, "example.com"); err != nil {
			t.Fatalf("first Wait: %v", err)
		}

		start := time.Now()
		second := make(chan error, 1)
		go func() {
			second <- coord.Wait(ctx, "example.com")
		}()
		synctest.Wait()

		// The in-flight response carries Retry-After.
		coord.Cooldown("example.com", time.Minute)
		coord.Release("example.com")

		if err := <-second; err != nil {
			t.Fatalf("second Wait: %v", err)
		}
		if waited := time.Since(start); waited < time.Minute {
			t.Errorf("queued worker proceeded after %s, want to serve the full %s cooldown", waited, time.Minute)
		}
		coord.Release("example.com")
	})
}

// TestCoordinatorPacesFetchStarts checks that the rate limit paces the moment a
// request actually starts. Taking a token before queueing for a concurrency
// slot banks it: workers released together then fetch back to back regardless
// of the configured rate.
func TestCoordinatorPacesFetchStarts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		coord := newTestCoordinator(config.CrawlerRate{
			GlobalRPS:          100,
			PerHostRPS:         1,
			GlobalConcurrency:  10,
			PerHostConcurrency: 2,
		})
		ctx := context.Background()

		// Durations are handed out in acquisition order and chosen so the first
		// two requests finish together, freeing both slots at the same instant.
		fetchDurations := []time.Duration{
			30 * time.Second,
			29 * time.Second,
			5 * time.Second,
			5 * time.Second,
			5 * time.Second,
		}
		workers := len(fetchDurations)
		origin := time.Now()

		var mu sync.Mutex
		var starts []time.Duration

		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := coord.Wait(ctx, "example.com"); err != nil {
					t.Errorf("Wait: %v", err)
					return
				}
				mu.Lock()
				fetchDuration := fetchDurations[len(starts)]
				starts = append(starts, time.Since(origin))
				mu.Unlock()
				time.Sleep(fetchDuration)
				coord.Release("example.com")
			}()
		}
		wg.Wait()

		if len(starts) != workers {
			t.Fatalf("recorded %d starts, want %d", len(starts), workers)
		}
		slices.Sort(starts)
		for i := 1; i < len(starts); i++ {
			if gap := starts[i] - starts[i-1]; gap < time.Second {
				t.Errorf("request %d started %s after the previous one, want at least 1s at 1 rps", i, gap)
			}
		}
	})
}

// TestCoordinatorAbandonedProbeDoesNotStrandHost covers a worker admitted as
// the half-open probe that never gets to make its request. The breaker hands
// out one probe permit at a time, so a permit that is never returned refuses
// every later request to that host for the rest of the crawl.
func TestCoordinatorAbandonedProbeDoesNotStrandHost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		coord := newTestCoordinator(config.CrawlerRate{
			GlobalRPS:          100,
			PerHostRPS:         100,
			GlobalConcurrency:  10,
			PerHostConcurrency: 1,
		})
		const host = "flaky.example.com"

		// Trip the breaker, then let its cooldown run out so the next caller
		// is admitted as the probe.
		for i := 0; i < 5; i++ {
			coord.RecordFailure(host)
		}
		time.Sleep(6 * time.Minute)

		// The probe is admitted, then blocked by a Retry-After and given up on.
		coord.Cooldown(host, time.Hour)
		ctx, cancel := context.WithCancel(context.Background())
		probe := make(chan error, 1)
		go func() {
			probe <- coord.Wait(ctx, host)
		}()
		synctest.Wait()
		cancel()
		if err := <-probe; err == nil {
			t.Fatal("Wait should have failed once its context was cancelled")
		}

		// The host is reachable again: clear the Retry-After and try once more.
		coord.Cooldown(host, -time.Hour)
		if err := coord.Wait(context.Background(), host); err != nil {
			t.Fatalf("host is stranded after an abandoned probe: %v", err)
		}
		coord.Release(host)
	})
}
