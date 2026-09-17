// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	EmbeddingJobPending    = "pending"
	EmbeddingJobInProgress = "in_progress"
	EmbeddingJobFailed     = "failed"
)

// EmbeddingJob is a durable, deduplicated request to embed the latest indexed
// version of a document. Completed jobs are deleted. Failed jobs are retained
// with their last error until new document contents enqueue them again. Dirty
// records indicate that the document changed while a worker was processing it.
type EmbeddingJob struct {
	DocID       string    `gorm:"primaryKey;type:text"`
	Status      string    `gorm:"not null;index"`
	Dirty       bool      `gorm:"not null;default:false"`
	Attempts    uint      `gorm:"not null;default:0"`
	AvailableAt time.Time `gorm:"not null;index"`
	LastError   string    `gorm:"type:text"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func embeddingDB() (*gorm.DB, error) {
	if DB == nil {
		return nil, errors.New("database is not initialized")
	}
	return DB, nil
}

// EnqueueEmbeddingJob adds a document to the embedding work set. Pending jobs
// already represent the latest document stored in the index. An update to an
// active job marks it dirty so it is processed again after the active attempt.
// Enqueuing a failed job resets its failure state and attempt count.
func EnqueueEmbeddingJob(docID string) error {
	if docID == "" {
		return errors.New("embedding job document ID must not be empty")
	}
	db, err := embeddingDB()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	job := EmbeddingJob{
		DocID:       docID,
		Status:      EmbeddingJobPending,
		AvailableAt: now,
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "doc_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"status": gorm.Expr(
				"CASE WHEN embedding_jobs.status = ? THEN embedding_jobs.status ELSE ? END",
				EmbeddingJobInProgress, EmbeddingJobPending,
			),
			"dirty": gorm.Expr(
				"CASE WHEN embedding_jobs.status = ? THEN ? ELSE embedding_jobs.dirty END",
				EmbeddingJobInProgress, true,
			),
			"attempts": gorm.Expr(
				"CASE WHEN embedding_jobs.status = ? THEN embedding_jobs.attempts ELSE 0 END",
				EmbeddingJobInProgress,
			),
			"available_at": gorm.Expr(
				"CASE WHEN embedding_jobs.status = ? THEN embedding_jobs.available_at ELSE ? END",
				EmbeddingJobInProgress, now,
			),
			"last_error": gorm.Expr(
				"CASE WHEN embedding_jobs.status = ? THEN embedding_jobs.last_error ELSE '' END",
				EmbeddingJobInProgress,
			),
			"updated_at": now,
		}),
	}).Create(&job).Error
}

// ClaimNextEmbeddingJob atomically claims the oldest available pending job.
// It returns nil when no job is currently available.
func ClaimNextEmbeddingJob() (*EmbeddingJob, error) {
	db, err := embeddingDB()
	if err != nil {
		return nil, err
	}
	for {
		var job EmbeddingJob
		now := time.Now().UTC()
		err := db.Where("status = ? AND available_at <= ?", EmbeddingJobPending, now).
			Order("available_at ASC").
			Order("created_at ASC").
			First(&job).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		result := db.Model(&EmbeddingJob{}).
			Where("doc_id = ? AND status = ?", job.DocID, EmbeddingJobPending).
			Updates(map[string]any{
				"status":     EmbeddingJobInProgress,
				"dirty":      false,
				"attempts":   gorm.Expr("attempts + 1"),
				"updated_at": now,
			})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 1 {
			job.Status = EmbeddingJobInProgress
			job.Dirty = false
			job.Attempts++
			job.UpdatedAt = now
			return &job, nil
		}
	}
}

// CompleteEmbeddingJob deletes a completed job unless it became dirty while
// active. Dirty jobs return to pending and retry is reported as true.
func CompleteEmbeddingJob(docID string) (retry bool, err error) {
	db, err := embeddingDB()
	if err != nil {
		return false, err
	}
	result := db.Where(
		"doc_id = ? AND status = ? AND dirty = ?",
		docID, EmbeddingJobInProgress, false,
	).Delete(&EmbeddingJob{})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		return false, nil
	}
	now := time.Now().UTC()
	result = db.Model(&EmbeddingJob{}).
		Where("doc_id = ? AND status = ? AND dirty = ?", docID, EmbeddingJobInProgress, true).
		Updates(map[string]any{
			"status":       EmbeddingJobPending,
			"dirty":        false,
			"attempts":     0,
			"available_at": now,
			"last_error":   "",
			"updated_at":   now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// RetryEmbeddingJob returns an active job to pending. A dirty job retries
// immediately because its latest indexed contents have not been attempted yet.
func RetryEmbeddingJob(docID string, retryAt time.Time, lastError string) error {
	db, err := embeddingDB()
	if err != nil {
		return err
	}
	retryAt = retryAt.UTC()
	now := time.Now().UTC()
	return db.Model(&EmbeddingJob{}).
		Where("doc_id = ? AND status = ?", docID, EmbeddingJobInProgress).
		Updates(map[string]any{
			"status": EmbeddingJobPending,
			"dirty":  false,
			"attempts": gorm.Expr(
				"CASE WHEN dirty = ? THEN 0 ELSE attempts END", true,
			),
			"available_at": gorm.Expr(
				"CASE WHEN dirty = ? THEN ? ELSE ? END", true, now, retryAt,
			),
			"last_error": gorm.Expr(
				"CASE WHEN dirty = ? THEN '' ELSE ? END", true, lastError,
			),
			"updated_at": now,
		}).Error
}

// FailEmbeddingJob moves an active job to the failed state. If the document
// changed while the job was active, its latest contents return to pending
// instead and the attempt count is reset.
func FailEmbeddingJob(docID string, lastError string) (retry bool, err error) {
	db, err := embeddingDB()
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	result := db.Model(&EmbeddingJob{}).
		Where("doc_id = ? AND status = ? AND dirty = ?", docID, EmbeddingJobInProgress, false).
		Updates(map[string]any{
			"status":     EmbeddingJobFailed,
			"last_error": lastError,
			"updated_at": now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		return false, nil
	}
	result = db.Model(&EmbeddingJob{}).
		Where("doc_id = ? AND status = ? AND dirty = ?", docID, EmbeddingJobInProgress, true).
		Updates(map[string]any{
			"status":       EmbeddingJobPending,
			"dirty":        false,
			"attempts":     0,
			"available_at": now,
			"last_error":   "",
			"updated_at":   now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// ReleaseEmbeddingJob returns a claimed job to pending without counting the
// interrupted attempt. It is used during queue shutdown and cancellation.
func ReleaseEmbeddingJob(docID string) error {
	db, err := embeddingDB()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return db.Model(&EmbeddingJob{}).
		Where("doc_id = ? AND status = ?", docID, EmbeddingJobInProgress).
		Updates(map[string]any{
			"status":       EmbeddingJobPending,
			"dirty":        false,
			"attempts":     gorm.Expr("CASE WHEN attempts > 0 THEN attempts - 1 ELSE 0 END"),
			"available_at": now,
			"updated_at":   now,
		}).Error
}

// EmbeddingJobInProgressExists reports whether a worker still owns docID.
func EmbeddingJobInProgressExists(docID string) (bool, error) {
	db, err := embeddingDB()
	if err != nil {
		return false, err
	}
	var count int64
	if err := db.Model(&EmbeddingJob{}).
		Where("doc_id = ? AND status = ?", docID, EmbeddingJobInProgress).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count == 1, nil
}

// DeleteEmbeddingJob removes pending or active work for a deleted document.
func DeleteEmbeddingJob(docID string) error {
	db, err := embeddingDB()
	if err != nil {
		return err
	}
	return db.Where("doc_id = ?", docID).Delete(&EmbeddingJob{}).Error
}

// ResetInProgressEmbeddingJobs recovers jobs interrupted by process shutdown.
func ResetInProgressEmbeddingJobs() error {
	db, err := embeddingDB()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return db.Model(&EmbeddingJob{}).
		Where("status = ?", EmbeddingJobInProgress).
		Updates(map[string]any{
			"status":       EmbeddingJobPending,
			"dirty":        false,
			"available_at": now,
			"updated_at":   now,
		}).Error
}
