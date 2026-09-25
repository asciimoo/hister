// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"errors"
	"math/rand/v2"
	"net"
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
			return false, 0, statusCode
		}
	}

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
	jitter := time.Duration(rand.Int64N(int64(exp) + 1))
	return jitter
}
