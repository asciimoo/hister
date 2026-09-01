// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"errors"
	"fmt"
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
