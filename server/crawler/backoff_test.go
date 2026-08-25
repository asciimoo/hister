// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantRetryable bool
		wantStatus    int
	}{
		{name: "nil error", err: nil, wantRetryable: false},
		{name: "429", err: &HTTPStatusError{Status: 429}, wantRetryable: true, wantStatus: 429},
		{name: "503", err: &HTTPStatusError{Status: 503}, wantRetryable: true, wantStatus: 503},
		{name: "500", err: &HTTPStatusError{Status: 500}, wantRetryable: true, wantStatus: 500},
		{name: "502", err: &HTTPStatusError{Status: 502}, wantRetryable: true, wantStatus: 502},
		{name: "504", err: &HTTPStatusError{Status: 504}, wantRetryable: true, wantStatus: 504},
		{name: "408", err: &HTTPStatusError{Status: 408}, wantRetryable: true, wantStatus: 408},
		{name: "404", err: &HTTPStatusError{Status: 404}, wantRetryable: false, wantStatus: 404},
		{name: "403", err: &HTTPStatusError{Status: 403}, wantRetryable: false, wantStatus: 403},
		{name: "501", err: &HTTPStatusError{Status: 501}, wantRetryable: false, wantStatus: 501},
		{name: "400", err: &HTTPStatusError{Status: 400}, wantRetryable: false, wantStatus: 400},
		{
			name: "network timeout",
			err: &net.OpError{
				Op:  "dial",
				Err: fmt.Errorf("connection refused"),
			},
			wantRetryable: true,
			wantStatus:    0,
		},
		{
			name:          "wrapped non-retryable",
			err:           fmt.Errorf("wrap: %w", &HTTPStatusError{Status: 403}),
			wantRetryable: false,
			wantStatus:    403,
		},
		{
			name:          "wrapped retryable",
			err:           fmt.Errorf("wrap: %w", &HTTPStatusError{Status: 503}),
			wantRetryable: true,
			wantStatus:    503,
		},
		{
			name:          "generic error",
			err:           errors.New("something went wrong"),
			wantRetryable: false,
			wantStatus:    0,
		},
		{
			name:          "retry-after propagated",
			err:           &HTTPStatusError{Status: 429, RetryAfter: 10 * time.Second},
			wantRetryable: true,
			wantStatus:    429,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retryable, _, statusCode := ClassifyError(tt.err)
			if retryable != tt.wantRetryable {
				t.Errorf("ClassifyError(%v) retryable = %v, want %v", tt.err, retryable, tt.wantRetryable)
			}
			if statusCode != tt.wantStatus {
				t.Errorf("ClassifyError(%v) statusCode = %d, want %d", tt.err, statusCode, tt.wantStatus)
			}
		})
	}
}

func TestRetryAfterPropagated(t *testing.T) {
	expected := 42 * time.Second
	err := &HTTPStatusError{Status: 429, RetryAfter: expected}
	_, retryAfter, _ := ClassifyError(err)
	if retryAfter != expected {
		t.Errorf("ClassifyError RetryAfter = %v, want %v", retryAfter, expected)
	}
}

func TestBackoffBounds(t *testing.T) {
	initial := 100 * time.Millisecond
	max := 1 * time.Second
	b := NewBackoff(initial, max)

	for attempt := 0; attempt < 10; attempt++ {
		d := b.Duration(attempt)
		if d < 0 {
			t.Errorf("attempt %d: Duration = %v, must be >= 0", attempt, d)
		}
		if d > max {
			t.Errorf("attempt %d: Duration = %v exceeds max %v", attempt, d, max)
		}
	}
}

func TestBackoffIncreasesWithAttempt(t *testing.T) {
	// With full jitter the result is random, but the cap should grow with attempt.
	// Run many samples and verify the max seen grows.
	initial := 10 * time.Millisecond
	max := 1 * time.Second
	b := NewBackoff(initial, max)

	samples := 200
	maxAttempt0 := time.Duration(0)
	maxAttempt5 := time.Duration(0)
	for i := 0; i < samples; i++ {
		d0 := b.Duration(0)
		d5 := b.Duration(5)
		if d0 > maxAttempt0 {
			maxAttempt0 = d0
		}
		if d5 > maxAttempt5 {
			maxAttempt5 = d5
		}
	}
	if maxAttempt5 <= maxAttempt0 {
		t.Errorf("expected max sample for attempt 5 (%v) to exceed attempt 0 (%v)", maxAttempt5, maxAttempt0)
	}
}
