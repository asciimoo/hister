// SPDX-FileContributor: Theophysics research fork
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Research job and source receipt records.
//
// These tables hold the authoritative research metadata for the Theophysics
// fork: who created a job, which source bytes were accepted into it, their
// SHA-256 identity and whether they were indexed. Hister's Bleve index stays
// the full-text projection; these rows are the record of what was accepted.

// ResearchJobStatus values.
const (
	ResearchJobOpen     = "open"
	ResearchJobArchived = "archived"
)

// Research source receipt states.
const (
	ResearchSourceAccepted = "accepted" // bytes hashed and receipt stored
	ResearchSourceIndexed  = "indexed"  // Hister acknowledged the index write
	ResearchSourceFailed   = "failed"   // extraction or indexing failed; receipt retained
)

// Duplicate kinds recorded on a source occurrence.
const (
	DuplicateNone     = ""
	DuplicateInJob    = "exact_in_job"    // same bytes already in this job
	DuplicateOtherJob = "exact_other_job" // same bytes already accepted in another job of this user
)

var (
	ErrResearchJobName     = errors.New("job name must be 1-200 characters")
	ErrResearchJobNotFound = errors.New("research job not found")
)

// ResearchJob is a named research container. It is distinct from a Hister
// CrawlJob, which is a crawler execution.
type ResearchJob struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	UserID      uint      `gorm:"index;not null" json:"user_id"`
	Name        string    `gorm:"size:200;not null" json:"name"`
	Description string    `gorm:"size:2000" json:"description"`
	Status      string    `gorm:"size:32;not null;default:open" json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ResearchArtifact is one unique byte sequence per user, identified by
// SHA-256 and size. Exact duplicates share an artifact; every upload still
// gets its own ResearchSource receipt.
type ResearchArtifact struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"uniqueIndex:idx_research_artifact_identity;not null" json:"user_id"`
	SHA256    string    `gorm:"size:64;uniqueIndex:idx_research_artifact_identity;not null" json:"sha256"`
	Size      int64     `gorm:"uniqueIndex:idx_research_artifact_identity;not null" json:"size"`
	IndexURL  string    `gorm:"size:2048;not null" json:"index_url"`
	Format    string    `gorm:"size:32" json:"format"`
	Title     string    `gorm:"size:1000" json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

// ResearchSource is the receipt for one accepted upload occurrence inside a
// job: original name, relative path, hash, size, and processing outcome.
type ResearchSource struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	JobID         uint       `gorm:"index;not null" json:"job_id"`
	UserID        uint       `gorm:"index;not null" json:"user_id"`
	ArtifactID    uint       `gorm:"index;not null" json:"artifact_id"`
	OriginalName  string     `gorm:"size:1000;not null" json:"original_name"`
	RelativePath  string     `gorm:"size:2000" json:"relative_path"`
	SHA256        string     `gorm:"size:64;index;not null" json:"sha256"`
	Size          int64      `gorm:"not null" json:"size"`
	MediaType     string     `gorm:"size:200" json:"media_type"`
	Status        string     `gorm:"size:32;not null" json:"status"`
	Error         string     `gorm:"size:2000" json:"error,omitempty"`
	DuplicateKind string     `gorm:"size:32" json:"duplicate_kind,omitempty"`
	DuplicateOfID *uint      `json:"duplicate_of_id,omitempty"`
	IndexedAt     *time.Time `json:"indexed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// ResearchJobSummary adds receipt counts to a job.
type ResearchJobSummary struct {
	ResearchJob
	Sources    int64 `json:"sources"`
	Indexed    int64 `json:"indexed"`
	Failed     int64 `json:"failed"`
	Duplicates int64 `json:"duplicates"`
}

// CreateResearchJob validates and stores a new job for the user.
func CreateResearchJob(userID uint, name, description string) (*ResearchJob, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return nil, ErrResearchJobName
	}
	description = strings.TrimSpace(description)
	if len(description) > 2000 {
		description = description[:2000]
	}
	job := &ResearchJob{UserID: userID, Name: name, Description: description, Status: ResearchJobOpen}
	if err := DB.Create(job).Error; err != nil {
		return nil, err
	}
	return job, nil
}

// GetResearchJob returns the user's job or ErrResearchJobNotFound. Jobs of
// other users are reported as not found.
func GetResearchJob(userID, jobID uint) (*ResearchJob, error) {
	var job ResearchJob
	err := DB.Where("id = ? AND user_id = ?", jobID, userID).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrResearchJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// ListResearchJobs returns the user's jobs, newest first, with counts.
func ListResearchJobs(userID uint) ([]*ResearchJobSummary, error) {
	var jobs []ResearchJob
	if err := DB.Where("user_id = ?", userID).Order("id DESC").Find(&jobs).Error; err != nil {
		return nil, err
	}
	out := make([]*ResearchJobSummary, 0, len(jobs))
	for _, j := range jobs {
		s := &ResearchJobSummary{ResearchJob: j}
		q := func() *gorm.DB { return DB.Model(&ResearchSource{}).Where("job_id = ?", j.ID) }
		if err := q().Count(&s.Sources).Error; err != nil {
			return nil, err
		}
		q().Where("status = ?", ResearchSourceIndexed).Count(&s.Indexed)
		q().Where("status = ?", ResearchSourceFailed).Count(&s.Failed)
		q().Where("duplicate_kind <> ''").Count(&s.Duplicates)
		out = append(out, s)
	}
	return out, nil
}

// ListResearchSources returns a job's receipts in upload order.
func ListResearchSources(jobID uint) ([]*ResearchSource, error) {
	var sources []*ResearchSource
	err := DB.Where("job_id = ?", jobID).Order("id ASC").Find(&sources).Error
	return sources, err
}

// FindOrCreateResearchArtifact returns the user's artifact for the given
// hash and size, creating it when absent. created reports a new artifact.
func FindOrCreateResearchArtifact(a *ResearchArtifact) (artifact *ResearchArtifact, created bool, err error) {
	var existing ResearchArtifact
	err = DB.Where("user_id = ? AND sha256 = ? AND size = ?", a.UserID, a.SHA256, a.Size).First(&existing).Error
	if err == nil {
		return &existing, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	if err = DB.Create(a).Error; err != nil {
		// A concurrent upload of the same bytes may have won the unique index.
		if again := DB.Where("user_id = ? AND sha256 = ? AND size = ?", a.UserID, a.SHA256, a.Size).First(&existing).Error; again == nil {
			return &existing, false, nil
		}
		return nil, false, err
	}
	return a, true, nil
}

// UpdateResearchArtifactMetadata stores extraction results on an artifact.
func UpdateResearchArtifactMetadata(id uint, format, title string) error {
	if len(title) > 1000 {
		title = title[:1000]
	}
	return DB.Model(&ResearchArtifact{}).Where("id = ?", id).Updates(map[string]any{"format": format, "title": title}).Error
}

// FirstResearchSourceForArtifact returns the earliest receipt of an
// artifact, optionally restricted to one job.
func FirstResearchSourceForArtifact(artifactID uint, jobID *uint) (*ResearchSource, error) {
	q := DB.Where("artifact_id = ?", artifactID)
	if jobID != nil {
		q = q.Where("job_id = ?", *jobID)
	}
	var s ResearchSource
	err := q.Order("id ASC").First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// CreateResearchSource stores an upload receipt.
func CreateResearchSource(s *ResearchSource) error {
	return DB.Create(s).Error
}

// MarkResearchSource records the processing outcome of a receipt.
func MarkResearchSource(id uint, status, errMsg string) error {
	if len(errMsg) > 2000 {
		errMsg = errMsg[:2000]
	}
	updates := map[string]any{"status": status, "error": errMsg}
	if status == ResearchSourceIndexed {
		now := time.Now()
		updates["indexed_at"] = &now
	}
	return DB.Model(&ResearchSource{}).Where("id = ?", id).Updates(updates).Error
}

// ResearchJobIndexURLs maps index URL to artifact for every indexed
// artifact in the job. Used to scope full-text results to one job.
func ResearchJobIndexURLs(jobID uint) (map[string]uint, error) {
	type row struct {
		IndexURL   string
		ArtifactID uint
	}
	var rows []row
	err := DB.Table("research_sources").
		Select("research_artifacts.index_url AS index_url, research_artifacts.id AS artifact_id").
		Joins("JOIN research_artifacts ON research_artifacts.id = research_sources.artifact_id").
		Where("research_sources.job_id = ? AND research_sources.status = ?", jobID, ResearchSourceIndexed).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]uint, len(rows))
	for _, r := range rows {
		out[r.IndexURL] = r.ArtifactID
	}
	return out, nil
}

// ArtifactIndexed reports whether any receipt of the artifact was indexed.
func ArtifactIndexed(artifactID uint) bool {
	var n int64
	DB.Model(&ResearchSource{}).Where("artifact_id = ? AND status = ?", artifactID, ResearchSourceIndexed).Count(&n)
	return n > 0
}
