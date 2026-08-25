// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"sync"
	"time"
)

// Clock abstracts time operations so they can be controlled in tests.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
	After(d time.Duration) <-chan time.Time
	NewTimer(d time.Duration) Timer
}

// Timer abstracts a time.Timer.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

// RealClock is a Clock backed by the real system clock.
type RealClock struct{}

func (RealClock) Now() time.Time                         { return time.Now() }
func (RealClock) Sleep(d time.Duration)                  { time.Sleep(d) }
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (RealClock) NewTimer(d time.Duration) Timer         { return &realTimer{t: time.NewTimer(d)} }

type realTimer struct{ t *time.Timer }

func (r *realTimer) C() <-chan time.Time     { return r.t.C }
func (r *realTimer) Stop() bool              { return r.t.Stop() }
func (r *realTimer) Reset(d time.Duration) bool {
	r.t.Reset(d)
	return true
}

// FakeClock is a Clock for tests that allows manual time advancement.
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	timers  []*fakeTimer
}

// NewFakeClock creates a FakeClock starting at t.
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{now: t}
}

func (f *FakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *FakeClock) Sleep(d time.Duration) {
	ch := f.After(d)
	<-ch
}

func (f *FakeClock) After(d time.Duration) <-chan time.Time {
	t := f.NewTimer(d)
	return t.C()
}

func (f *FakeClock) NewTimer(d time.Duration) Timer {
	f.mu.Lock()
	defer f.mu.Unlock()
	ft := &fakeTimer{
		ch:       make(chan time.Time, 1),
		deadline: f.now.Add(d),
		clock:    f,
	}
	f.timers = append(f.timers, ft)
	return ft
}

// Advance moves the fake clock forward by d, firing any expired timers.
func (f *FakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	now := f.now
	// Collect fired timers before releasing the lock.
	var fired []*fakeTimer
	remaining := f.timers[:0]
	for _, t := range f.timers {
		if !t.stopped && !t.deadline.After(now) {
			fired = append(fired, t)
		} else {
			remaining = append(remaining, t)
		}
	}
	f.timers = remaining
	f.mu.Unlock()

	for _, t := range fired {
		select {
		case t.ch <- now:
		default:
		}
	}
}

type fakeTimer struct {
	ch       chan time.Time
	deadline time.Time
	stopped  bool
	clock    *FakeClock
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if t.stopped {
		return false
	}
	t.stopped = true
	return true
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	wasActive := !t.stopped
	t.stopped = false
	t.deadline = t.clock.now.Add(d)
	t.clock.timers = append(t.clock.timers, t)
	return wasActive
}
