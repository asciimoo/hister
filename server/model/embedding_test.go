// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package model_test

import (
	"testing"
	"time"

	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"
)

func getEmbeddingJob(t *testing.T, docID string) *model.EmbeddingJob {
	t.Helper()
	var job model.EmbeddingJob
	if err := model.DB.Where("doc_id = ?", docID).First(&job).Error; err != nil {
		t.Fatalf("failed to load embedding job: %v", err)
	}
	return &job
}

func embeddingJobCount(t *testing.T) int64 {
	t.Helper()
	var count int64
	if err := model.DB.Model(&model.EmbeddingJob{}).Count(&count).Error; err != nil {
		t.Fatalf("failed to count embedding jobs: %v", err)
	}
	return count
}

func TestEmbeddingJobDeduplicatesPendingAndActiveWork(t *testing.T) {
	testutil.InitModel(t)
	const docID = "https://example.com/document"

	if err := model.EnqueueEmbeddingJob(docID); err != nil {
		t.Fatalf("first EnqueueEmbeddingJob() error: %v", err)
	}
	if err := model.EnqueueEmbeddingJob(docID); err != nil {
		t.Fatalf("second EnqueueEmbeddingJob() error: %v", err)
	}
	if count := embeddingJobCount(t); count != 1 {
		t.Fatalf("embedding job count = %d, want 1", count)
	}

	job, err := model.ClaimNextEmbeddingJob()
	if err != nil {
		t.Fatalf("ClaimNextEmbeddingJob() error: %v", err)
	}
	if job == nil || job.DocID != docID {
		t.Fatalf("claimed job = %#v, want document %q", job, docID)
	}
	if job.Attempts != 1 {
		t.Fatalf("claimed attempts = %d, want 1", job.Attempts)
	}

	if err := model.EnqueueEmbeddingJob(docID); err != nil {
		t.Fatalf("active EnqueueEmbeddingJob() error: %v", err)
	}
	if dirty := getEmbeddingJob(t, docID).Dirty; !dirty {
		t.Fatal("active embedding job was not marked dirty")
	}

	retry, err := model.CompleteEmbeddingJob(docID)
	if err != nil {
		t.Fatalf("dirty CompleteEmbeddingJob() error: %v", err)
	}
	if !retry {
		t.Fatal("dirty CompleteEmbeddingJob() did not request a retry")
	}
	if status := getEmbeddingJob(t, docID).Status; status != model.EmbeddingJobPending {
		t.Fatalf("dirty completed job status = %q, want %q", status, model.EmbeddingJobPending)
	}

	job, err = model.ClaimNextEmbeddingJob()
	if err != nil {
		t.Fatalf("second ClaimNextEmbeddingJob() error: %v", err)
	}
	if job == nil || job.DocID != docID {
		t.Fatalf("second claimed job = %#v, want document %q", job, docID)
	}
	retry, err = model.CompleteEmbeddingJob(docID)
	if err != nil {
		t.Fatalf("CompleteEmbeddingJob() error: %v", err)
	}
	if retry {
		t.Fatal("clean CompleteEmbeddingJob() requested a retry")
	}
	if count := embeddingJobCount(t); count != 0 {
		t.Fatalf("completed embedding job count = %d, want 0", count)
	}
}

func TestEmbeddingJobRetryAndRecovery(t *testing.T) {
	testutil.InitModel(t)
	const docID = "https://example.com/retry"

	if err := model.EnqueueEmbeddingJob(docID); err != nil {
		t.Fatalf("EnqueueEmbeddingJob() error: %v", err)
	}
	job, err := model.ClaimNextEmbeddingJob()
	if err != nil || job == nil {
		t.Fatalf("ClaimNextEmbeddingJob() = %#v, %v", job, err)
	}

	retryAt := time.Now().Add(time.Hour)
	if err := model.RetryEmbeddingJob(docID, retryAt, "endpoint unavailable"); err != nil {
		t.Fatalf("RetryEmbeddingJob() error: %v", err)
	}
	if job, err = model.ClaimNextEmbeddingJob(); err != nil || job != nil {
		t.Fatalf("delayed ClaimNextEmbeddingJob() = %#v, %v, want nil, nil", job, err)
	}

	if err := model.ResetInProgressEmbeddingJobs(); err != nil {
		t.Fatalf("ResetInProgressEmbeddingJobs() error: %v", err)
	}
	stored := getEmbeddingJob(t, docID)
	stored.Status = model.EmbeddingJobInProgress
	if err := model.DB.Save(stored).Error; err != nil {
		t.Fatalf("failed to prepare interrupted job: %v", err)
	}
	if err := model.ResetInProgressEmbeddingJobs(); err != nil {
		t.Fatalf("second ResetInProgressEmbeddingJobs() error: %v", err)
	}
	stored = getEmbeddingJob(t, docID)
	if stored.Status != model.EmbeddingJobPending {
		t.Fatalf("recovered job status = %q, want %q", stored.Status, model.EmbeddingJobPending)
	}
	if stored.AvailableAt.After(time.Now()) {
		t.Fatalf("recovered job remains delayed until %v", stored.AvailableAt)
	}
}

func TestEmbeddingJobClaimsByAvailabilityBeforeCreation(t *testing.T) {
	testutil.InitModel(t)
	const (
		oldDoc = "https://example.com/old-retry"
		newDoc = "https://example.com/new-work"
	)

	if err := model.EnqueueEmbeddingJob(oldDoc); err != nil {
		t.Fatalf("enqueue old job: %v", err)
	}
	oldJob, err := model.ClaimNextEmbeddingJob()
	if err != nil || oldJob == nil {
		t.Fatalf("claim old job = %#v, %v", oldJob, err)
	}
	if err := model.RetryEmbeddingJob(oldDoc, time.Now(), "retry later"); err != nil {
		t.Fatalf("retry old job: %v", err)
	}
	if err := model.EnqueueEmbeddingJob(newDoc); err != nil {
		t.Fatalf("enqueue new job: %v", err)
	}

	now := time.Now().UTC()
	if err := model.DB.Model(&model.EmbeddingJob{}).
		Where("doc_id = ?", oldDoc).
		Update("available_at", now.Add(-time.Minute)).Error; err != nil {
		t.Fatalf("set old job availability: %v", err)
	}
	if err := model.DB.Model(&model.EmbeddingJob{}).
		Where("doc_id = ?", newDoc).
		Update("available_at", now.Add(-2*time.Minute)).Error; err != nil {
		t.Fatalf("set new job availability: %v", err)
	}

	job, err := model.ClaimNextEmbeddingJob()
	if err != nil {
		t.Fatalf("claim next job: %v", err)
	}
	if job == nil || job.DocID != newDoc {
		t.Fatalf("claimed job = %#v, want %q", job, newDoc)
	}
}

func TestEmbeddingJobFailureAndReenqueue(t *testing.T) {
	testutil.InitModel(t)
	const docID = "https://example.com/failed"

	if err := model.EnqueueEmbeddingJob(docID); err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	job, err := model.ClaimNextEmbeddingJob()
	if err != nil || job == nil {
		t.Fatalf("claim job = %#v, %v", job, err)
	}
	retry, err := model.FailEmbeddingJob(docID, "document timed out")
	if err != nil {
		t.Fatalf("fail job: %v", err)
	}
	if retry {
		t.Fatal("clean failed job requested a retry")
	}
	stored := getEmbeddingJob(t, docID)
	if stored.Status != model.EmbeddingJobFailed {
		t.Fatalf("failed job status = %q, want %q", stored.Status, model.EmbeddingJobFailed)
	}
	if stored.LastError != "document timed out" {
		t.Fatalf("failed job error = %q", stored.LastError)
	}
	if job, err = model.ClaimNextEmbeddingJob(); err != nil || job != nil {
		t.Fatalf("claim with failed job = %#v, %v, want nil, nil", job, err)
	}

	if err := model.EnqueueEmbeddingJob(docID); err != nil {
		t.Fatalf("reenqueue failed job: %v", err)
	}
	stored = getEmbeddingJob(t, docID)
	if stored.Status != model.EmbeddingJobPending {
		t.Fatalf("reenqueued job status = %q, want %q", stored.Status, model.EmbeddingJobPending)
	}
	if stored.Attempts != 0 {
		t.Fatalf("reenqueued job attempts = %d, want 0", stored.Attempts)
	}
	if stored.LastError != "" {
		t.Fatalf("reenqueued job retained error %q", stored.LastError)
	}
}

func TestDirtyEmbeddingJobRetriesInsteadOfFailing(t *testing.T) {
	testutil.InitModel(t)
	const docID = "https://example.com/dirty-failure"

	if err := model.EnqueueEmbeddingJob(docID); err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	job, err := model.ClaimNextEmbeddingJob()
	if err != nil || job == nil {
		t.Fatalf("claim job = %#v, %v", job, err)
	}
	if err := model.EnqueueEmbeddingJob(docID); err != nil {
		t.Fatalf("mark active job dirty: %v", err)
	}
	retry, err := model.FailEmbeddingJob(docID, "old contents timed out")
	if err != nil {
		t.Fatalf("fail dirty job: %v", err)
	}
	if !retry {
		t.Fatal("dirty failed job did not request a retry")
	}
	stored := getEmbeddingJob(t, docID)
	if stored.Status != model.EmbeddingJobPending {
		t.Fatalf("dirty job status = %q, want %q", stored.Status, model.EmbeddingJobPending)
	}
	if stored.Attempts != 0 {
		t.Fatalf("dirty job attempts = %d, want 0", stored.Attempts)
	}
	if stored.LastError != "" {
		t.Fatalf("dirty job retained error %q", stored.LastError)
	}
}

func TestDirtyEmbeddingJobRetriesImmediatelyAfterFailure(t *testing.T) {
	testutil.InitModel(t)
	const docID = "https://example.com/dirty-retry"

	if err := model.EnqueueEmbeddingJob(docID); err != nil {
		t.Fatalf("EnqueueEmbeddingJob() error: %v", err)
	}
	job, err := model.ClaimNextEmbeddingJob()
	if err != nil || job == nil {
		t.Fatalf("ClaimNextEmbeddingJob() = %#v, %v", job, err)
	}
	if err := model.EnqueueEmbeddingJob(docID); err != nil {
		t.Fatalf("active EnqueueEmbeddingJob() error: %v", err)
	}
	if err := model.RetryEmbeddingJob(docID, time.Now().Add(time.Hour), "old contents failed"); err != nil {
		t.Fatalf("RetryEmbeddingJob() error: %v", err)
	}

	job, err = model.ClaimNextEmbeddingJob()
	if err != nil {
		t.Fatalf("second ClaimNextEmbeddingJob() error: %v", err)
	}
	if job == nil || job.DocID != docID {
		t.Fatalf("dirty retry job = %#v, want document %q", job, docID)
	}
	if job.Attempts != 1 {
		t.Fatalf("dirty retry attempts = %d, want 1", job.Attempts)
	}
	if job.LastError != "" {
		t.Fatalf("dirty retry retained error %q", job.LastError)
	}
}
