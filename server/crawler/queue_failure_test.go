// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/model"
)

// faultyQueue wraps a real queue and injects a failure into one operation, so a
// test can see what the driver does when the crawl's durable state cannot be
// written.
type faultyQueue struct {
	CrawlQueue
	popErr      error
	completeErr error
}

func (q *faultyQueue) Pop(ctx context.Context) (*pendingItem, bool, error) {
	if q.popErr != nil {
		return nil, false, q.popErr
	}
	return q.CrawlQueue.Pop(ctx)
}

func (q *faultyQueue) Complete(ctx context.Context, item *pendingItem, c completion) error {
	if err := q.CrawlQueue.Complete(ctx, item, c); err != nil {
		return err
	}
	return q.completeErr
}

func newFailureTestCrawler(t *testing.T, jobID string, urls []string) (*baseCrawler, *countingFetcher) {
	t.Helper()
	initTestDB(t)
	if err := model.CreateCrawlJob(jobID, urls[0], "", "test"); err != nil {
		t.Fatalf("CreateCrawlJob: %v", err)
	}
	if err := model.BulkInsertCrawlURLs(jobID, urls, 0); err != nil {
		t.Fatalf("BulkInsertCrawlURLs: %v", err)
	}
	cfg := &config.CrawlerConfig{
		Rate: config.CrawlerRate{
			GlobalRPS:          1000,
			PerHostRPS:         1000,
			GlobalConcurrency:  1,
			PerHostConcurrency: 1,
		},
		Retry:         config.CrawlerRetry{MaxAttempts: 1},
		ShutdownGrace: 1,
	}
	f := &countingFetcher{bodyLen: 10}
	return &baseCrawler{
		fetcher: f,
		cfg:     cfg,
		coord:   NewCoordinator(cfg),
		backoff: NewBackoff(time.Millisecond, time.Millisecond),
	}, f
}

// runQueue drives a crawl to completion against q, draining documents so no
// worker blocks on its send.
func runQueue(t *testing.T, bc *baseCrawler, q CrawlQueue, startURL string, v *Validator) error {
	t.Helper()
	ch := make(chan *document.Document)
	go func() {
		for range ch {
		}
	}()
	err := bc.run(context.Background(), q, startURL, v, ch)
	close(ch)
	return err
}

// TestCompleteFailureDoesNotCompleteJob covers a result write that fails. The
// row is left in_progress with its discovered links unrecorded, so reporting
// the run as done marks the job completed and the CLI then refuses to resume
// the very URLs that were lost.
func TestCompleteFailureDoesNotCompleteJob(t *testing.T) {
	jobID := "complete-write-fails"
	bc, f := newFailureTestCrawler(t, jobID, []string{"http://example.com/a"})

	writeErr := errors.New("disk full")
	q := &faultyQueue{CrawlQueue: newSQLiteQueue(jobID), completeErr: writeErr}

	v, err := NewValidator(&ValidatorRules{NoDepth: true})
	if err != nil {
		t.Fatal(err)
	}

	runErr := runQueue(t, bc, q, "http://example.com/a", v)
	if !errors.Is(runErr, writeErr) {
		t.Errorf("run error = %v, want it to report %v: a failed result write must not be swallowed", runErr, writeErr)
	}
	if calls := f.calls.Load(); calls != 1 {
		t.Errorf("fetcher called %d times, want 1", calls)
	}

	job, err := model.GetCrawlJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status == model.CrawlJobCompleted {
		t.Error("job status = completed, want interrupted: results failed to save, so the job must stay resumable")
	}
	if job.Status != model.CrawlJobInterrupted {
		t.Errorf("job status = %q, want %q", job.Status, model.CrawlJobInterrupted)
	}
}

// TestPopFailureDoesNotCompleteJob covers the queue read failing. Every worker
// exits without having drained anything, which used to look exactly like a
// finished crawl.
func TestPopFailureDoesNotCompleteJob(t *testing.T) {
	jobID := "pop-read-fails"
	bc, f := newFailureTestCrawler(t, jobID, []string{"http://example.com/a"})

	readErr := errors.New("database is locked")
	q := &faultyQueue{CrawlQueue: newSQLiteQueue(jobID), popErr: readErr}

	v, err := NewValidator(&ValidatorRules{NoDepth: true})
	if err != nil {
		t.Fatal(err)
	}

	runErr := runQueue(t, bc, q, "http://example.com/a", v)
	if !errors.Is(runErr, readErr) {
		t.Errorf("run error = %v, want it to report %v", runErr, readErr)
	}
	if calls := f.calls.Load(); calls != 0 {
		t.Errorf("fetcher called %d times, want 0", calls)
	}

	job, err := model.GetCrawlJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != model.CrawlJobInterrupted {
		t.Errorf("job status = %q, want %q: an unreadable queue is not a finished crawl", job.Status, model.CrawlJobInterrupted)
	}

	pending, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLPending)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Errorf("pending URLs = %d, want 1", pending)
	}
}

// TestCleanRunStillCompletesJob is the control for the two above: with no
// injected failure the same driver must still reach OnDone, otherwise the
// error plumbing would have made every crawl look interrupted.
func TestCleanRunStillCompletesJob(t *testing.T) {
	jobID := "clean-run"
	bc, f := newFailureTestCrawler(t, jobID, []string{"http://example.com/a"})

	v, err := NewValidator(&ValidatorRules{NoDepth: true})
	if err != nil {
		t.Fatal(err)
	}

	if err := runQueue(t, bc, newSQLiteQueue(jobID), "http://example.com/a", v); err != nil {
		t.Fatalf("run: %v", err)
	}
	if calls := f.calls.Load(); calls != 1 {
		t.Errorf("fetcher called %d times, want 1", calls)
	}

	job, err := model.GetCrawlJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != model.CrawlJobCompleted {
		t.Errorf("job status = %q, want %q", job.Status, model.CrawlJobCompleted)
	}
}
