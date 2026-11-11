// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/asciimoo/hister/server/model"

	"github.com/rs/zerolog/log"
)

const (
	embeddingQueuePollInterval = time.Second
	embeddingRetryMaxDelay     = time.Minute
	embeddingMaxJobAttempts    = 5
	defaultEmbeddingWorkers    = 2
)

type activeEmbedding struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// embeddingQueue runs a fixed number of workers over a durable SQL work set.
// The jobs channel is intentionally unbuffered so only active documents are
// loaded from the index and retained in memory.
type embeddingQueue struct {
	idx       *Indexer
	ctx       context.Context
	cancel    context.CancelFunc
	jobs      chan *model.EmbeddingJob
	wake      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
	activeMu  sync.Mutex
	active    map[string]*activeEmbedding
}

func normalizeEmbeddingWorkerCount(configured int) int {
	if configured > 0 {
		return configured
	}
	return defaultEmbeddingWorkers
}

func newEmbeddingQueue(idx *Indexer, workers int) (*embeddingQueue, error) {
	if err := model.ResetInProgressEmbeddingJobs(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(idx.embedCtx)
	q := &embeddingQueue{
		idx:    idx,
		ctx:    ctx,
		cancel: cancel,
		jobs:   make(chan *model.EmbeddingJob),
		wake:   make(chan struct{}, 1),
		active: make(map[string]*activeEmbedding),
	}
	q.wg.Go(q.dispatch)
	for range workers {
		q.wg.Go(q.work)
	}
	q.notify()
	return q, nil
}

func (i *Indexer) startEmbeddingQueue(workers int) error {
	if i.embeddingQueue != nil {
		return nil
	}
	workers = normalizeEmbeddingWorkerCount(workers)
	q, err := newEmbeddingQueue(i, workers)
	if err != nil {
		return err
	}
	i.embeddingWorkers = workers
	i.embeddingQueue = q
	return nil
}

func (i *Indexer) stopEmbeddingQueue() {
	if i.embeddingQueue == nil {
		return
	}
	i.embeddingQueue.Close()
	i.embeddingQueue = nil
}

func (i *Indexer) enqueueEmbedding(docID string) error {
	if i.embeddingQueue != nil {
		return i.embeddingQueue.Enqueue(docID)
	}
	return model.EnqueueEmbeddingJob(docID)
}

func (i *Indexer) cancelEmbedding(docID string) error {
	if i.embeddingQueue != nil {
		return i.embeddingQueue.Cancel(docID)
	}
	if model.DB == nil {
		return nil
	}
	return model.DeleteEmbeddingJob(docID)
}

func (q *embeddingQueue) notify() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *embeddingQueue) waitForWork(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-q.ctx.Done():
		return false
	case <-q.wake:
		return true
	case <-timer.C:
		return true
	}
}

func (q *embeddingQueue) dispatch() {
	defer close(q.jobs)
	for {
		job, err := model.ClaimNextEmbeddingJob()
		if err != nil {
			log.Error().Err(err).Msg("failed to claim embedding job")
			if !q.waitForWork(embeddingQueuePollInterval) {
				return
			}
			continue
		}
		if job == nil {
			if !q.waitForWork(embeddingQueuePollInterval) {
				return
			}
			continue
		}
		select {
		case q.jobs <- job:
		case <-q.ctx.Done():
			if err := model.ReleaseEmbeddingJob(job.DocID); err != nil {
				log.Warn().Err(err).Str("id", job.DocID).Msg("failed to release embedding job")
			}
			return
		}
	}
}

func (q *embeddingQueue) work() {
	for job := range q.jobs {
		q.process(job)
	}
}

func (q *embeddingQueue) begin(docID string) (context.Context, *activeEmbedding) {
	ctx, cancel := context.WithCancel(q.ctx)
	active := &activeEmbedding{cancel: cancel, done: make(chan struct{})}
	q.activeMu.Lock()
	q.active[docID] = active
	q.activeMu.Unlock()
	return ctx, active
}

func (q *embeddingQueue) finish(docID string, active *activeEmbedding) {
	active.cancel()
	q.activeMu.Lock()
	if q.active[docID] == active {
		delete(q.active, docID)
	}
	close(active.done)
	q.activeMu.Unlock()
}

func (q *embeddingQueue) process(job *model.EmbeddingJob) {
	ctx, active := q.begin(job.DocID)
	defer q.finish(job.DocID, active)

	owned, err := model.EmbeddingJobInProgressExists(job.DocID)
	if err != nil {
		log.Warn().Err(err).Str("id", job.DocID).Msg("failed to verify embedding job")
		q.retry(job, err)
		return
	}
	if !owned {
		return
	}

	d := q.idx.getByDocID(job.DocID, resultIncludeText)
	if d == nil {
		retry, err := model.CompleteEmbeddingJob(job.DocID)
		if err != nil {
			log.Warn().Err(err).Str("id", job.DocID).Msg("failed to discard missing embedding document")
		} else if retry {
			q.notify()
		}
		return
	}

	err = embedDocumentChunks(ctx, q.idx, d, false)
	if err == nil {
		retry, completeErr := model.CompleteEmbeddingJob(job.DocID)
		if completeErr != nil {
			log.Warn().Err(completeErr).Str("id", job.DocID).Msg("failed to complete embedding job")
			q.retry(job, completeErr)
			return
		}
		if retry {
			q.notify()
		}
		return
	}
	if errors.Is(err, context.Canceled) {
		if releaseErr := model.ReleaseEmbeddingJob(job.DocID); releaseErr != nil {
			log.Warn().Err(releaseErr).Str("id", job.DocID).Msg("failed to release canceled embedding job")
		}
		q.notify()
		return
	}
	q.retry(job, err)
}

func embeddingRetryDelay(attempt uint) time.Duration {
	if attempt == 0 {
		attempt = 1
	}
	delay := time.Second
	for range attempt - 1 {
		if delay >= embeddingRetryMaxDelay/2 {
			return embeddingRetryMaxDelay
		}
		delay *= 2
	}
	return min(delay, embeddingRetryMaxDelay)
}

func (q *embeddingQueue) retry(job *model.EmbeddingJob, jobErr error) {
	if job.Attempts >= embeddingMaxJobAttempts {
		retry, err := model.FailEmbeddingJob(job.DocID, jobErr.Error())
		if err != nil {
			log.Warn().Err(err).Str("id", job.DocID).Msg("failed to quarantine embedding job")
			return
		}
		if retry {
			q.notify()
			return
		}
		log.Error().Err(jobErr).Str("id", job.DocID).Uint("attempts", job.Attempts).Msg("embedding job quarantined")
		return
	}
	retryAt := time.Now().Add(embeddingRetryDelay(job.Attempts))
	if err := model.RetryEmbeddingJob(job.DocID, retryAt, jobErr.Error()); err != nil {
		log.Warn().Err(err).Str("id", job.DocID).Msg("failed to retry embedding job")
		return
	}
	q.notify()
}

func (q *embeddingQueue) Enqueue(docID string) error {
	if err := model.EnqueueEmbeddingJob(docID); err != nil {
		return err
	}
	q.notify()
	return nil
}

// Cancel removes queued work, interrupts an active request, and waits until the
// active worker can no longer write vectors for the document.
func (q *embeddingQueue) Cancel(docID string) error {
	err := model.DeleteEmbeddingJob(docID)
	q.activeMu.Lock()
	active := q.active[docID]
	if active != nil {
		active.cancel()
	}
	q.activeMu.Unlock()
	if active != nil {
		<-active.done
	}
	return err
}

func (q *embeddingQueue) Close() {
	q.closeOnce.Do(func() {
		q.cancel()
		q.wg.Wait()
	})
}
