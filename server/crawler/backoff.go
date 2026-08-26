// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"errors"
	randv2 "math/rand/v2"
	"net"
	"sync"
	"time"
)

// ClassifyError inspects err and returns whether it is retryable, any
// server-specified retry-after duration, and the HTTP status code (0 for
// network errors).
func ClassifyError(err error) (retryable bool, retryAfter time.Duration, statusCode int) {
	if err == nil {
		return false, 0, 0
	}

	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		statusCode = httpErr.Status
		retryAfter = httpErr.RetryAfter
		switch statusCode {
		case 408, 429, 500, 502, 503, 504:
			return true, retryAfter, statusCode
		default:
			// All other 4xx and 5xx are not retried (including 501).
			return false, 0, statusCode
		}
	}

	// Network-level errors are retryable.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true, 0, 0
	}

	return false, 0, 0
}

// Backoff computes exponential backoff durations with jitter.
type Backoff struct {
	initial time.Duration
	max     time.Duration
}

// NewBackoff creates a Backoff. initial and max must be positive.
func NewBackoff(initial, max time.Duration) *Backoff {
	return &Backoff{
		initial: initial,
		max:     max,
	}
}

// Duration returns the backoff duration for the given attempt number (0-based).
// It applies full jitter: the result is uniformly random in [0, cap].
func (b *Backoff) Duration(attempt int) time.Duration {
	exp := b.initial
	for i := 0; i < attempt; i++ {
		exp *= 2
		if exp > b.max {
			exp = b.max
			break
		}
	}
	if exp > b.max {
		exp = b.max
	}
	jitter := time.Duration(randv2.Int64N(int64(exp) + 1))
	return jitter
}

// BreakerState represents the state of a circuit breaker for a single host.
type BreakerState int

const (
	BreakerClosed   BreakerState = iota // normal operation
	BreakerOpen                         // requests blocked
	BreakerHalfOpen                     // one probe allowed
)

type breakerState struct {
	state       BreakerState
	failures    int
	lastFailure time.Time
	probing     bool // a half-open probe is in flight
}

// CircuitBreaker is a per-host circuit breaker.
type CircuitBreaker struct {
	threshold int
	cooldown  time.Duration
	clock     Clock
	mu        sync.Mutex
	hosts     map[string]*breakerState
}

// NewCircuitBreaker creates a CircuitBreaker. threshold is the number of
// consecutive failures before opening; cooldown is the duration to wait before
// entering half-open state.
func NewCircuitBreaker(threshold int, cooldown time.Duration, clock Clock) *CircuitBreaker {
	if clock == nil {
		clock = RealClock{}
	}
	return &CircuitBreaker{
		threshold: threshold,
		cooldown:  cooldown,
		clock:     clock,
		hosts:     make(map[string]*breakerState),
	}
}

func (cb *CircuitBreaker) hostState(host string) *breakerState {
	s, ok := cb.hosts[host]
	if !ok {
		s = &breakerState{}
		cb.hosts[host] = s
	}
	return s
}

// Allow returns true if a request to host is permitted.
func (cb *CircuitBreaker) Allow(host string) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	s := cb.hostState(host)
	switch s.state {
	case BreakerClosed:
		return true
	case BreakerOpen:
		if cb.clock.Now().Sub(s.lastFailure) >= cb.cooldown {
			s.state = BreakerHalfOpen
			s.probing = true
			return true
		}
		return false
	case BreakerHalfOpen:
		if !s.probing {
			s.probing = true
			return true
		}
		return false
	}
	return true
}

// RecordSuccess resets the failure count and closes the breaker.
func (cb *CircuitBreaker) RecordSuccess(host string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	s := cb.hostState(host)
	s.failures = 0
	s.probing = false
	s.state = BreakerClosed
}

// RecordFailure increments the failure count and may open the breaker.
func (cb *CircuitBreaker) RecordFailure(host string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	s := cb.hostState(host)
	s.failures++
	s.probing = false
	s.lastFailure = cb.clock.Now()
	if s.failures >= cb.threshold {
		s.state = BreakerOpen
	}
}

// State returns the current state of the breaker for host.
func (cb *CircuitBreaker) State(host string) BreakerState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.hostState(host).state
}
