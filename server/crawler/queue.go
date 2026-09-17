// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/asciimoo/hister/server/model"
)

type pendingItem struct {
	id     any // opaque: crawl_urls.ID for sqlite, nil for memory
	rawURL string
	depth  int
	// attempts is how many times this URL has been handed to a worker and
	// handed back without a verdict. It is carried across requeues so that a
	// host that stays down eventually retires the URL instead of serving it
	// back forever.
	attempts int
}

type completion struct {
	finalURL      string
	resolvedLinks []string
	err           error
	skipped       bool
	skipReason    string
	// interrupted is set when the fetch was aborted by context cancellation
	// (graceful shutdown, grace-period expiry). Persistent queues must reset
	// the row to pending so a resumed run can retry it, rather than marking
	// it failed and silently dropping it.
	interrupted bool
	// stop is set when the item was not fetched because a crawl-wide budget
	// (MaxLinks, MaxPages) is exhausted. Like interrupted, the item stays
	// pending so a later run with a larger budget can pick it up; the driver
	// stops the queue taking new work but lets reserved fetches finish.
	stop bool
	// deferred is set when the host is not accepting requests right now - its
	// circuit breaker is open. The URL was never attempted, so it stays queued
	// instead of being recorded as a failure, and the worker holds off for
	// deferFor before taking more work.
	deferred bool
	deferFor time.Duration
	// attemptsUsed is how many fetches this pass actually made. A deferred URL
	// carries it back into the queue so the retry budget is spent across passes
	// rather than restarting on each one; a pass that never reached the network
	// reports zero and costs the URL nothing.
	attemptsUsed int
}

// CrawlQueue is the interface that both in-memory and sqlite crawl queues implement.
type CrawlQueue interface {
	// Seed adds the initial URL before workers are spawned.
	Seed(ctx context.Context, startURL string) error
	// Pop blocks until an item is available. Returns ok=false when the crawl is
	// done (no pending items and no in-flight workers).
	Pop(ctx context.Context) (item *pendingItem, ok bool, err error)
	// Complete marks an item done and enqueues its discovered links.
	Complete(ctx context.Context, item *pendingItem, c completion) error
	// Stop makes blocked and subsequent Pop calls return ok=false without an
	// error, so workers finish the item they already hold and then exit. It is
	// how a spent budget stops intake without cancelling reserved work.
	Stop()
	// Close is called at end of the driver run.
	Close() error
	// OnStop is called when the driver's ctx is cancelled.
	OnStop(ctx context.Context) error
	// OnDone is called when the crawl completes normally.
	OnDone(ctx context.Context) error
}

// memoryQueue is an in-memory FIFO with dedup and in-flight tracking.
type memoryQueue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	items    []pendingItem
	seen     map[uint64]struct{}
	inFlight int
	stopped  bool
}

func newMemoryQueue() *memoryQueue {
	q := &memoryQueue{
		seen: make(map[uint64]struct{}),
	}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *memoryQueue) Seed(_ context.Context, startURL string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	h := hashURL(startURL)
	if _, exists := q.seen[h]; exists {
		return nil
	}
	q.seen[h] = struct{}{}
	q.items = append(q.items, pendingItem{rawURL: startURL, depth: 0})
	q.cond.Signal()
	return nil
}

func (q *memoryQueue) Stop() {
	q.mu.Lock()
	q.stopped = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

func (q *memoryQueue) Pop(ctx context.Context) (*pendingItem, bool, error) {
	// Wake the cond when ctx cancels so this call unblocks. Signalling is
	// local to this call; nothing is mutated on the queue itself.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			q.mu.Lock()
			q.cond.Broadcast()
			q.mu.Unlock()
		case <-stop:
		}
	}()

	q.mu.Lock()
	defer q.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		if q.stopped {
			return nil, false, nil
		}
		if len(q.items) > 0 {
			item := q.items[0]
			q.items = q.items[1:]
			q.inFlight++
			return &item, true, nil
		}
		if q.inFlight == 0 {
			return nil, false, nil
		}
		q.cond.Wait()
	}
}

func (q *memoryQueue) Complete(_ context.Context, item *pendingItem, c completion) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.inFlight--
	if c.deferred {
		// Requeue behind the work already waiting, so one unreachable host does
		// not keep the workers busy re-reading the same URL. Fetches made this
		// pass are charged here: without that the retry budget restarts every
		// pass and a host that stays down is fetched without bound.
		requeued := *item
		requeued.attempts += c.attemptsUsed
		q.items = append(q.items, requeued)
		q.cond.Broadcast()
		return nil
	}
	if !c.skipped && !c.stop && !c.interrupted && c.err == nil {
		if c.finalURL != "" {
			q.seen[hashURL(c.finalURL)] = struct{}{}
		}
		for _, link := range c.resolvedLinks {
			h := hashURL(link)
			if _, exists := q.seen[h]; exists {
				continue
			}
			q.seen[h] = struct{}{}
			q.items = append(q.items, pendingItem{rawURL: link, depth: item.depth + 1})
		}
	}
	q.cond.Broadcast()
	return nil
}

func (q *memoryQueue) Close() error                   { return nil }
func (q *memoryQueue) OnStop(_ context.Context) error { return nil }
func (q *memoryQueue) OnDone(_ context.Context) error { return nil }

// sqliteQueue is a DB-backed queue that survives process restarts.
type sqliteQueue struct {
	jobID    string
	mu       sync.Mutex
	cond     *sync.Cond
	inflight int
	stopped  bool
}

func newSQLiteQueue(jobID string) *sqliteQueue {
	q := &sqliteQueue{jobID: jobID}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *sqliteQueue) Seed(_ context.Context, startURL string) error {
	return model.InsertCrawlURLIfNotExists(q.jobID, startURL, 0)
}

func (q *sqliteQueue) Stop() {
	q.mu.Lock()
	q.stopped = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

func (q *sqliteQueue) Pop(ctx context.Context) (*pendingItem, bool, error) {
	// Wake any goroutine blocked on cond.Wait when ctx cancels. Local to this
	// call; no persistent state is mutated on the queue.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			q.mu.Lock()
			q.cond.Broadcast()
			q.mu.Unlock()
		case <-stop:
		}
	}()

	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		q.mu.Lock()
		stopped := q.stopped
		q.mu.Unlock()
		if stopped {
			return nil, false, nil
		}

		cur, err := model.ClaimNextPendingCrawlURL(q.jobID)
		if err != nil {
			return nil, false, err
		}
		if cur != nil {
			q.mu.Lock()
			q.inflight++
			q.mu.Unlock()
			return &pendingItem{id: cur.ID, rawURL: cur.URL, depth: cur.Depth, attempts: cur.Attempts}, true, nil
		}

		// No pending rows - if no workers are in flight the crawl is done;
		// otherwise wait for a Complete to signal (either a new row was
		// enqueued, or the last worker finished so we can exit).
		q.mu.Lock()
		if q.inflight == 0 || q.stopped {
			q.mu.Unlock()
			return nil, false, nil
		}
		q.cond.Wait()
		q.mu.Unlock()
	}
}

func (q *sqliteQueue) Complete(ctx context.Context, item *pendingItem, c completion) error {
	id := item.id.(uint)

	var completeErr error
	switch {
	case c.deferred:
		// The host refused the request as a whole, so the URL keeps its place in
		// the queue - but any fetch this pass did make is charged, otherwise a
		// host that stays down is fetched without bound.
		completeErr = model.DeferCrawlURL(id, c.attemptsUsed)
	case c.interrupted, c.stop:
		// Revert to pending so a resumed run picks it up. Do NOT mark failed:
		// this URL was never actually attempted-to-completion. No attempt is
		// charged: the run stopping is not this URL's fault.
		completeErr = model.UpdateCrawlURLStatus(id, model.CrawlURLPending, "")
	case c.skipped:
		reason := c.skipReason
		if reason == "" {
			reason = "skipped"
		}
		completeErr = model.UpdateCrawlURLStatus(id, model.CrawlURLSkipped, reason)
	case c.err != nil:
		completeErr = model.UpdateCrawlURLStatus(id, model.CrawlURLFailed, c.err.Error())
	default:
		// A failure here leaves the row in_progress with its links unrecorded
		// while the run reports the page as crawled. Report it so the driver
		// logs it and the resume path is the one that repairs the row, rather
		// than a warning nobody acts on.
		if err := model.MarkDoneAndEnqueueLinks(id, q.jobID, c.resolvedLinks, item.depth+1); err != nil {
			completeErr = fmt.Errorf("record %s as done: %w", item.rawURL, err)
		}
		// Handle redirect: mark the final URL as done if it differs.
		if c.finalURL != "" && c.finalURL != item.rawURL {
			if err := model.InsertCrawlURLDone(q.jobID, c.finalURL, item.depth); err != nil {
				completeErr = errors.Join(completeErr, fmt.Errorf("record redirect target %s as done: %w", c.finalURL, err))
			}
		}
	}

	q.mu.Lock()
	q.inflight--
	q.cond.Broadcast()
	q.mu.Unlock()

	return completeErr
}

func (q *sqliteQueue) Close() error { return nil }

func (q *sqliteQueue) OnStop(_ context.Context) error {
	return model.UpdateCrawlJobStatus(q.jobID, model.CrawlJobInterrupted)
}

func (q *sqliteQueue) OnDone(_ context.Context) error {
	return model.UpdateCrawlJobStatus(q.jobID, model.CrawlJobCompleted)
}
