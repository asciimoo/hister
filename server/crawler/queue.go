// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/server/model"
)

type pendingItem struct {
	id     any    // opaque: crawl_urls.ID for sqlite, nil for memory
	rawURL string
	depth  int
}

type completion struct {
	finalURL      string
	resolvedLinks []string
	err           error
	skipped       bool
	skipReason    string
	etag          string
	lastModified  string
	// interrupted is set when the fetch was aborted by context cancellation
	// (graceful shutdown, grace-period expiry). Persistent queues must reset
	// the row to pending so a resumed run can retry it, rather than marking
	// it failed and silently dropping it.
	interrupted bool
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
	if !c.skipped && c.err == nil {
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

func (q *memoryQueue) Close() error              { return nil }
func (q *memoryQueue) OnStop(_ context.Context) error { return nil }
func (q *memoryQueue) OnDone(_ context.Context) error { return nil }

// sqliteQueue is a DB-backed queue that survives process restarts.
type sqliteQueue struct {
	jobID    string
	mu       sync.Mutex
	cond     *sync.Cond
	inflight int
}

func newSQLiteQueue(jobID string) *sqliteQueue {
	q := &sqliteQueue{jobID: jobID}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *sqliteQueue) Seed(_ context.Context, startURL string) error {
	return model.InsertCrawlURLIfNotExists(q.jobID, startURL, 0)
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

		cur, err := model.ClaimNextPendingCrawlURL(q.jobID)
		if err != nil {
			return nil, false, err
		}
		if cur != nil {
			q.mu.Lock()
			q.inflight++
			q.mu.Unlock()
			return &pendingItem{id: cur.ID, rawURL: cur.URL, depth: cur.Depth}, true, nil
		}

		// No pending rows - if no workers are in flight the crawl is done;
		// otherwise wait for a Complete to signal (either a new row was
		// enqueued, or the last worker finished so we can exit).
		q.mu.Lock()
		if q.inflight == 0 {
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
	case c.interrupted:
		// Revert to pending so a resumed run picks it up. Do NOT mark failed:
		// this URL was never actually attempted-to-completion.
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
		if err := model.MarkDoneAndEnqueueLinks(id, q.jobID, c.resolvedLinks, item.depth+1, c.etag, c.lastModified); err != nil {
			log.Warn().Err(err).Msg("sqliteQueue: MarkDoneAndEnqueueLinks failed")
		}
		// Handle redirect: mark the final URL as done if it differs.
		if c.finalURL != "" && c.finalURL != item.rawURL {
			if err := model.InsertCrawlURLDone(q.jobID, c.finalURL, item.depth); err != nil {
				log.Warn().Err(err).Str("url", c.finalURL).Msg("sqliteQueue: InsertCrawlURLDone failed")
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

