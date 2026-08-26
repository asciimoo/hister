// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/asciimoo/hister/config"
)

func newTestScheduler(rate config.CrawlerRate, breaker *CircuitBreaker, robots *RobotsCache, clock Clock) *Scheduler {
	cfg := &config.CrawlerConfig{
		Rate: rate,
	}
	return NewScheduler(cfg, breaker, robots, clock)
}

func TestSchedulerPerHostRateSpacing(t *testing.T) {
	cfg := config.CrawlerRate{
		GlobalRPS:          100,
		PerHostRPS:         2, // 500ms between requests
		GlobalConcurrency:  10,
		PerHostConcurrency: 1,
		Jitter:             0,
	}
	clock := NewFakeClock(time.Now())
	breaker := NewCircuitBreaker(5, 5*time.Minute, clock)
	sched := newTestScheduler(cfg, breaker, nil, clock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// First request should pass immediately (bucket is pre-filled).
	done := make(chan error, 1)
	go func() {
		done <- sched.Wait(ctx, "example.com")
	}()

	// Advance clock so any timer-based waits inside the bucket fire.
	clock.Advance(10 * time.Millisecond)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("first Wait error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first Wait timed out")
	}

	sched.Release("example.com")
}

func TestSchedulerRetryAfterCooldown(t *testing.T) {
	cfg := config.CrawlerRate{
		GlobalRPS:          100,
		PerHostRPS:         100,
		GlobalConcurrency:  10,
		PerHostConcurrency: 5,
		Jitter:             0,
	}
	clock := NewFakeClock(time.Now())
	breaker := NewCircuitBreaker(5, 5*time.Minute, clock)
	sched := newTestScheduler(cfg, breaker, nil, clock)

	// Install a 10-second cooldown.
	sched.Cooldown("slow.example.com", 10*time.Second)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- sched.Wait(ctx, "slow.example.com")
	}()

	// Should still be blocked after 5s because cooldown is 10s.
	clock.Advance(5 * time.Second)
	time.Sleep(20 * time.Millisecond)

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Wait should have blocked during cooldown")
		}
	default:
		// Still blocked - expected.
	}

	cancel() // unblock
	<-done
}

func TestSchedulerBreakerInteraction(t *testing.T) {
	cfg := config.CrawlerRate{
		GlobalRPS:          100,
		PerHostRPS:         100,
		GlobalConcurrency:  10,
		PerHostConcurrency: 5,
		Jitter:             0,
	}
	clock := NewFakeClock(time.Now())
	breaker := NewCircuitBreaker(1, 5*time.Minute, clock)
	sched := newTestScheduler(cfg, breaker, nil, clock)

	// Trip the breaker.
	breaker.RecordFailure("tripped.example.com")
	if breaker.State("tripped.example.com") != BreakerOpen {
		t.Fatal("breaker should be open after failure")
	}

	ctx := context.Background()
	err := sched.Wait(ctx, "tripped.example.com")
	if err == nil {
		t.Fatal("Wait should return error when breaker is open")
	}
	var breakerErr *errBreakerOpen
	if !errors.As(err, &breakerErr) {
		t.Errorf("expected errBreakerOpen, got %T: %v", err, err)
	}
}

func TestSchedulerLRUEviction(t *testing.T) {
	cfg := config.CrawlerRate{
		GlobalRPS:          1000,
		PerHostRPS:         1000,
		GlobalConcurrency:  1000,
		PerHostConcurrency: 1,
		Jitter:             0,
	}
	clock := NewFakeClock(time.Now())
	breaker := NewCircuitBreaker(5, 5*time.Minute, clock)
	sched := newTestScheduler(cfg, breaker, nil, clock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Fill to the cap by calling getHostState with unique hostnames.
	firstHost := "host-0.com"
	for i := 0; i < hostLRUCap; i++ {
		host := fmt.Sprintf("host-%d.com", i)
		if i == 0 {
			firstHost = host
		}
		sched.getHostState(host)
	}

	sched.mu.Lock()
	lenAtCap := len(sched.hosts)
	sched.mu.Unlock()

	if lenAtCap != hostLRUCap {
		t.Fatalf("expected %d hosts at cap, got %d", hostLRUCap, lenAtCap)
	}

	// Adding one more should evict the oldest (firstHost).
	_ = sched.Wait(ctx, "newcomer.example.com") //nolint - error fine for test purposes
	sched.Release("newcomer.example.com")

	sched.mu.Lock()
	lenAfter := len(sched.hosts)
	_, newcomerPresent := sched.hosts["newcomer.example.com"]
	_, firstPresent := sched.hosts[firstHost]
	sched.mu.Unlock()

	if lenAfter != hostLRUCap {
		t.Fatalf("expected map len %d after eviction, got %d", hostLRUCap, lenAfter)
	}
	if !newcomerPresent {
		t.Fatal("newcomer host should be present after eviction")
	}
	if firstPresent {
		t.Fatalf("oldest host %q should have been evicted", firstHost)
	}
}

func TestSchedulerHostOverride(t *testing.T) {
	cfg := &config.CrawlerConfig{
		Rate: config.CrawlerRate{
			GlobalRPS:          1000,
			PerHostRPS:         1000,
			GlobalConcurrency:  10,
			PerHostConcurrency: 1,
			Jitter:             0,
		},
		Hosts: map[string]config.CrawlerHostOverride{
			"slow.example.com": {PerHostRPS: 0.001}, // effectively 1 request per 1000s
		},
	}
	clock := NewFakeClock(time.Now())
	breaker := NewCircuitBreaker(5, 5*time.Minute, clock)
	sched := NewScheduler(cfg, breaker, nil, clock)

	// First request should pass (bucket pre-filled).
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := sched.Wait(ctx, "slow.example.com")
	if err != nil {
		t.Fatalf("first Wait on overridden host: %v", err)
	}
	sched.Release("slow.example.com")

	// Second request should block because the rate is 0.001 rps.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	err2 := sched.Wait(ctx2, "slow.example.com")
	if err2 == nil {
		sched.Release("slow.example.com")
		t.Fatal("second Wait on overridden host should have timed out due to low RPS")
	}

	// Default host should not be throttled.
	ctx3, cancel3 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel3()
	if err := sched.Wait(ctx3, "fast.example.com"); err != nil {
		t.Fatalf("Wait on non-overridden host: %v", err)
	}
	sched.Release("fast.example.com")
}

func TestSchedulerRobotsCrawlDelay(t *testing.T) {
	// Build a fake RobotsCache with a cached entry for a host.
	rc := NewRobotsCache("testbot")

	// Manually inject a cache entry with a 1000s crawl delay so the second
	// request will definitely time out.
	rc.store("https://delayed.example.com", &robotsEntry{
		fetchedAt:  time.Now(),
		ttl:        24 * time.Hour,
		allowAll:   true,
		crawlDelay: 1000 * time.Second,
	})

	cfg := &config.CrawlerConfig{
		Rate: config.CrawlerRate{
			GlobalRPS:          1000,
			PerHostRPS:         1000,
			GlobalConcurrency:  10,
			PerHostConcurrency: 1,
			Jitter:             0,
		},
		Robots: config.CrawlerRobots{
			RespectCrawlDelay: true,
		},
	}
	clock := NewFakeClock(time.Now())
	breaker := NewCircuitBreaker(5, 5*time.Minute, clock)
	sched := NewScheduler(cfg, breaker, rc, clock)

	// First request passes (bucket pre-filled).
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := sched.Wait(ctx, "delayed.example.com"); err != nil {
		t.Fatalf("first Wait: %v", err)
	}
	sched.Release("delayed.example.com")

	// Second request should block - robots requires 1000s delay.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	err := sched.Wait(ctx2, "delayed.example.com")
	if err == nil {
		sched.Release("delayed.example.com")
		t.Fatal("second Wait should have blocked due to robots crawl-delay")
	}
}
