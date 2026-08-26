// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CrawlJobStatus values.
const (
	CrawlJobRunning     = "running"
	CrawlJobCompleted   = "completed"
	CrawlJobInterrupted = "interrupted"
)

// CrawlURLStatus values.
const (
	CrawlURLPending    = "pending"
	CrawlURLInProgress = "in_progress"
	CrawlURLDone       = "done"
	CrawlURLFailed     = "failed"
	CrawlURLSkipped    = "skipped"
)

// CrawlJob stores the configuration and status of a persistent crawl job.
type CrawlJob struct {
	ID             string    `gorm:"primaryKey" json:"id"`
	StartURL       string    `json:"start_url"`
	ValidatorRules string    `gorm:"type:text" json:"validator_rules"` // JSON-encoded ValidatorRules
	Label          string    `json:"label"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// CrawlURL tracks every URL discovered during a crawl job.
type CrawlURL struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	JobID        string    `gorm:"uniqueIndex:idx_crawl_job_url;not null" json:"job_id"`
	URL          string    `gorm:"uniqueIndex:idx_crawl_job_url;index;not null" json:"url"`
	Depth        int       `json:"depth"`
	Status       string    `gorm:"not null;default:pending" json:"status"`
	Error        string    `json:"error"`
	ErrorCode    int       `json:"error_code"`
	ETag         string    `gorm:"column:etag" json:"etag"`                   // ETag from last successful fetch. Explicit column name so GORM does not derive "e_tag" from the field name.
	LastModified string    `gorm:"column:last_modified" json:"last_modified"` // Last-Modified from last successful fetch
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// GenerateCrawlJobID returns a random 8-character hex string suitable as a job ID.
func GenerateCrawlJobID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CreateCrawlJob inserts a new CrawlJob record.
func CreateCrawlJob(id, startURL, validatorRules, label string) error {
	return DB.Create(&CrawlJob{
		ID:             id,
		StartURL:       startURL,
		ValidatorRules: validatorRules,
		Label:          label,
		Status:         CrawlJobRunning,
	}).Error
}

// CreateNamedCrawlJobWithURLs atomically creates a crawl job and its initial
// URL queue. The base ID is used when available, followed by -2, -3, and so on
// when another job already uses that ID.
func CreateNamedCrawlJobWithURLs(baseID, startURL, validatorRules, label string, urls []string) (string, error) {
	if len(urls) == 0 {
		return "", errors.New("crawl job requires at least one URL")
	}

	var jobID string
	err := DB.Transaction(func(tx *gorm.DB) error {
		for suffix := 1; ; suffix++ {
			id := baseID
			if suffix > 1 {
				id = fmt.Sprintf("%s-%d", baseID, suffix)
			}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&CrawlJob{
				ID:             id,
				StartURL:       startURL,
				ValidatorRules: validatorRules,
				Label:          label,
				Status:         CrawlJobRunning,
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				continue
			}
			if err := insertCrawlURLs(tx, id, urls, 0); err != nil {
				return err
			}
			jobID = id
			return nil
		}
	})
	if err != nil {
		return "", err
	}
	return jobID, nil
}

// GetCrawlJob returns the job with the given ID, or (nil, nil) when not found.
func GetCrawlJob(id string) (*CrawlJob, error) {
	var job CrawlJob
	err := DB.Where("id = ?", id).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// UpdateCrawlJobStatus updates the status field of a job.
func UpdateCrawlJobStatus(id, status string) error {
	return DB.Model(&CrawlJob{}).Where("id = ?", id).Update("status", status).Error
}

// IsUniqueConstraintError reports whether err is a SQLite unique constraint
// violation, meaning the row already exists in the database.
func IsUniqueConstraintError(err error) bool {
	var sqliteErr sqlite3.Error
	return errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique
}

// InsertCrawlURLIfNotExists adds a URL to the job's queue only when it has not
// been seen before (the unique index on job_id+url enforces this).
func InsertCrawlURLIfNotExists(jobID, rawURL string, depth int) error {
	cu := CrawlURL{
		JobID:  jobID,
		URL:    rawURL,
		Depth:  depth,
		Status: CrawlURLPending,
	}
	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&cu).Error
}

// BulkInsertCrawlURLs inserts all URLs for the given job in a single
// transaction, silently skipping any that are already present.
func BulkInsertCrawlURLs(jobID string, urls []string, depth int) error {
	return insertCrawlURLs(DB, jobID, urls, depth)
}

func insertCrawlURLs(db *gorm.DB, jobID string, urls []string, depth int) error {
	if len(urls) == 0 {
		return nil
	}
	records := make([]CrawlURL, 0, len(urls))
	for _, u := range urls {
		records = append(records, CrawlURL{
			JobID:  jobID,
			URL:    u,
			Depth:  depth,
			Status: CrawlURLPending,
		})
	}
	// CreateInBatches wraps each batch in its own transaction and generates a
	// single multi-row INSERT per batch, keeping the statement size bounded.
	return db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(records, 500).Error
}

// GetLastFetchedURLMeta returns the ETag and Last-Modified from the most-recent
// successful fetch of url across any job, or ok=false if none exists.
func GetLastFetchedURLMeta(rawURL string) (etag, lastModified string, ok bool, err error) {
	var cu CrawlURL
	err = DB.Select("etag, last_modified").
		Where("url = ? AND status = ?", rawURL, CrawlURLDone).
		Order("updated_at DESC").
		Limit(1).
		First(&cu).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	if cu.ETag == "" && cu.LastModified == "" {
		return "", "", false, nil
	}
	return cu.ETag, cu.LastModified, true, nil
}

// MarkDoneAndEnqueueLinks marks a crawl URL as done (with conditional-GET
// metadata) and inserts all discovered child URLs in a single transaction,
// minimising the number of SQLite commits.
func MarkDoneAndEnqueueLinks(id uint, jobID string, links []string, depth int, etag, lastModified string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&CrawlURL{}).Where("id = ?", id).
			Updates(map[string]any{
				"status":        CrawlURLDone,
				"etag":          etag,
				"last_modified": lastModified,
				"error":         "",
				"error_code":    0,
			}).Error; err != nil {
			return err
		}
		if len(links) == 0 {
			return nil
		}
		records := make([]CrawlURL, 0, len(links))
		for _, u := range links {
			records = append(records, CrawlURL{
				JobID:  jobID,
				URL:    u,
				Depth:  depth,
				Status: CrawlURLPending,
			})
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(records, 500).Error
	})
}

// pending state (used for redirect targets that have been fetched indirectly).
func InsertCrawlURLDone(jobID, rawURL string, depth int) error {
	// Update if already present, otherwise insert as done.
	result := DB.Model(&CrawlURL{}).
		Where("job_id = ? AND url = ?", jobID, rawURL).
		Updates(map[string]any{"status": CrawlURLDone, "error": "", "error_code": 0})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return DB.Create(&CrawlURL{
			JobID:  jobID,
			URL:    rawURL,
			Depth:  depth,
			Status: CrawlURLDone,
		}).Error
	}
	return nil
}

// NextPendingCrawlURL returns the oldest pending URL for the job.
// Returns (nil, nil) when no pending URLs remain.
func NextPendingCrawlURL(jobID string) (*CrawlURL, error) {
	var cu CrawlURL
	err := DB.Where("job_id = ? AND status = ?", jobID, CrawlURLPending).
		Order("id ASC").
		First(&cu).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cu, nil
}

// UpdateCrawlURLStatus sets the status and optional error message on a URL row.
func UpdateCrawlURLStatus(id uint, status, errMsg string) error {
	return DB.Model(&CrawlURL{}).Where("id = ?", id).
		Updates(map[string]any{"status": status, "error": errMsg, "error_code": 0}).Error
}

func MarkCrawlURLFailed(jobID, rawURL string, errCode int, errMsg string) error {
	return DB.Model(&CrawlURL{}).Where("job_id = ? AND url = ?", jobID, rawURL).
		Updates(map[string]any{"status": CrawlURLFailed, "error": errMsg, "error_code": errCode}).Error
}

// ResetInProgressCrawlURLs moves all in_progress URLs back to pending so they
// are retried after a crash or interruption.
func ResetInProgressCrawlURLs(jobID string) error {
	return DB.Model(&CrawlURL{}).
		Where("job_id = ? AND status = ?", jobID, CrawlURLInProgress).
		Update("status", CrawlURLPending).Error
}

// CountCrawlURLsByStatus returns the number of URLs with the given status for a job.
func CountCrawlURLsByStatus(jobID, status string) (int64, error) {
	var count int64
	err := DB.Model(&CrawlURL{}).
		Where("job_id = ? AND status = ?", jobID, status).
		Count(&count).Error
	return count, err
}

// CountCrawlURLs returns the number of URL rows tracked for a job.
func CountCrawlURLs(jobID string) (int64, error) {
	var count int64
	err := DB.Model(&CrawlURL{}).
		Where("job_id = ?", jobID).
		Count(&count).Error
	return count, err
}

// ForEachFailedCrawlURL streams failed crawl URL error codes and URLs for a job.
func ForEachFailedCrawlURL(jobID string, fn func(errorCode int, rawURL string) error) error {
	return ForEachFailedCrawlURLWithMessage(jobID, func(errorCode int, rawURL, _ string) error {
		return fn(errorCode, rawURL)
	})
}

// ForEachFailedCrawlURLWithMessage streams failed crawl URL error codes, URLs,
// and stored error messages for a job.
func ForEachFailedCrawlURLWithMessage(jobID string, fn func(errorCode int, rawURL, errMsg string) error) (err error) {
	rows, err := DB.Model(&CrawlURL{}).
		Select("error_code, url, error").
		Where("job_id = ? AND status = ?", jobID, CrawlURLFailed).
		Order("id ASC").
		Rows()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	for rows.Next() {
		var errorCode int
		var rawURL, errMsg string
		if err := rows.Scan(&errorCode, &rawURL, &errMsg); err != nil {
			return err
		}
		if err := fn(errorCode, rawURL, errMsg); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ForEachCrawlURL streams crawl URL status, depth, and URL rows for a job.
func ForEachCrawlURL(jobID string, fn func(status string, depth int, rawURL string) error) (err error) {
	return ForEachCrawlURLByStatus(jobID, "", fn)
}

// ForEachCrawlURLByStatus streams crawl URL status, depth, and URL rows for a
// job. An empty status includes every URL.
func ForEachCrawlURLByStatus(jobID, status string, fn func(status string, depth int, rawURL string) error) (err error) {
	query := DB.Model(&CrawlURL{}).
		Select("status, depth, url").
		Where("job_id = ?", jobID)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	rows, err := query.Order("id ASC").Rows()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	for rows.Next() {
		var status string
		var depth int
		var rawURL string
		if err := rows.Scan(&status, &depth, &rawURL); err != nil {
			return err
		}
		if err := fn(status, depth, rawURL); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ListCrawlJobs returns all crawl jobs ordered by creation time descending.
func ListCrawlJobs() ([]*CrawlJob, error) {
	var jobs []*CrawlJob
	err := DB.Order("created_at DESC").Find(&jobs).Error
	return jobs, err
}

// DeleteCrawlJob removes a job and all its associated URL rows.
func DeleteCrawlJob(id string) error {
	if err := DB.Where("job_id = ?", id).Delete(&CrawlURL{}).Error; err != nil {
		return err
	}
	return DB.Where("id = ?", id).Delete(&CrawlJob{}).Error
}

// CrawlJobStats contains aggregate counts for a job's URLs.
type CrawlJobStats struct {
	Pending    int64
	InProgress int64
	Done       int64
	Failed     int64
	Skipped    int64
}

// GetCrawlJobStats returns URL counts per status for the given job.
func GetCrawlJobStats(jobID string) (CrawlJobStats, error) {
	type row struct {
		Status string
		Count  int64
	}
	var rows []row
	err := DB.Model(&CrawlURL{}).
		Select("status, count(*) as count").
		Where("job_id = ?", jobID).
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return CrawlJobStats{}, err
	}
	var s CrawlJobStats
	for _, r := range rows {
		switch r.Status {
		case CrawlURLPending:
			s.Pending = r.Count
		case CrawlURLInProgress:
			s.InProgress = r.Count
		case CrawlURLDone:
			s.Done = r.Count
		case CrawlURLFailed:
			s.Failed = r.Count
		case CrawlURLSkipped:
			s.Skipped = r.Count
		}
	}
	return s, nil
}
