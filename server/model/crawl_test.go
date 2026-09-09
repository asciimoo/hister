package model_test

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"
)

func TestCreateNamedCrawlJobWithURLs(t *testing.T) {
	testutil.InitModel(t)
	urls := []string{
		"https://example.com/one",
		"https://example.com/two",
		"https://example.com/one",
	}

	jobID, err := model.CreateNamedCrawlJobWithURLs(
		"urls.txt", urls[0], `{"NoDepth":true}`, "reference", urls,
	)
	if err != nil {
		t.Fatalf("CreateNamedCrawlJobWithURLs() error: %v", err)
	}
	if jobID != "urls.txt" {
		t.Fatalf("job ID = %q, want %q", jobID, "urls.txt")
	}

	job, err := model.GetCrawlJob(jobID)
	if err != nil {
		t.Fatalf("GetCrawlJob() error: %v", err)
	}
	if job == nil {
		t.Fatal("GetCrawlJob() returned nil")
	}
	if job.StartURL != urls[0] {
		t.Fatalf("start URL = %q, want %q", job.StartURL, urls[0])
	}
	if job.Label != "reference" {
		t.Fatalf("label = %q, want %q", job.Label, "reference")
	}

	var queued []string
	if err := model.ForEachCrawlURL(jobID, func(_ string, _ int, rawURL string) error {
		queued = append(queued, rawURL)
		return nil
	}); err != nil {
		t.Fatalf("ForEachCrawlURL() error: %v", err)
	}
	wantQueued := []string{urls[0], urls[1]}
	if !slices.Equal(queued, wantQueued) {
		t.Fatalf("queued URLs = %q, want %q", queued, wantQueued)
	}

	secondJobID, err := model.CreateNamedCrawlJobWithURLs(
		"urls.txt", urls[0], `{"NoDepth":true}`, "", urls[:1],
	)
	if err != nil {
		t.Fatalf("second CreateNamedCrawlJobWithURLs() error: %v", err)
	}
	if secondJobID != "urls.txt-2" {
		t.Fatalf("second job ID = %q, want %q", secondJobID, "urls.txt-2")
	}
}

func TestCreateNamedCrawlJobWithURLsRejectsEmptyQueue(t *testing.T) {
	testutil.InitModel(t)

	if _, err := model.CreateNamedCrawlJobWithURLs("urls.txt", "", `{}`, "", nil); err == nil {
		t.Fatal("CreateNamedCrawlJobWithURLs() expected an error")
	}
	jobs, err := model.ListCrawlJobs()
	if err != nil {
		t.Fatalf("ListCrawlJobs() error: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("job count = %d, want 0", len(jobs))
	}
}

// TestClaimNextPendingCrawlURLIsExclusive verifies that concurrent claimers
// each get a distinct row and that every pending row is handed out exactly
// once. A select followed by an unconditional update by ID does not guarantee
// this: two claimers can read the same row before either writes it.
func TestClaimNextPendingCrawlURLIsExclusive(t *testing.T) {
	testutil.InitModel(t)

	jobID := "claim-exclusivity"
	const urlCount = 50
	urls := make([]string, urlCount)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://example.com/%d", i)
	}
	if err := model.CreateCrawlJob(jobID, urls[0], "", ""); err != nil {
		t.Fatalf("CreateCrawlJob() error: %v", err)
	}
	if err := model.BulkInsertCrawlURLs(jobID, urls, 0); err != nil {
		t.Fatalf("BulkInsertCrawlURLs() error: %v", err)
	}

	const claimers = 8
	var (
		mu      sync.Mutex
		claims  = map[uint]int{}
		errs    []error
		wg      sync.WaitGroup
		started = make(chan struct{})
	)
	for i := 0; i < claimers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-started
			for {
				row, err := model.ClaimNextPendingCrawlURL(jobID)
				if err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
					return
				}
				if row == nil {
					return
				}
				mu.Lock()
				claims[row.ID]++
				mu.Unlock()
			}
		}()
	}
	close(started)
	wg.Wait()

	for _, err := range errs {
		t.Errorf("ClaimNextPendingCrawlURL() error: %v", err)
	}
	for id, count := range claims {
		if count > 1 {
			t.Errorf("row %d claimed %d times, want 1", id, count)
		}
	}
	if len(claims) != urlCount {
		t.Errorf("claimed %d distinct rows, want %d", len(claims), urlCount)
	}

	inProgress, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLInProgress)
	if err != nil {
		t.Fatalf("CountCrawlURLsByStatus() error: %v", err)
	}
	if inProgress != urlCount {
		t.Errorf("in_progress rows = %d, want %d", inProgress, urlCount)
	}
}

// TestCrawlJobLeaseExcludesOtherRuns pins the ownership rules: one run at a
// time, renewals only for the holder, and a release that only the holder can
// perform.
func TestCrawlJobLeaseExcludesOtherRuns(t *testing.T) {
	testutil.InitModel(t)

	jobID := "lease-exclusivity"
	if err := model.CreateCrawlJob(jobID, "https://example.com/", "", ""); err != nil {
		t.Fatalf("CreateCrawlJob() error: %v", err)
	}

	acquired, _, err := model.AcquireCrawlJobLease(jobID, "run-a", time.Minute)
	if err != nil {
		t.Fatalf("AcquireCrawlJobLease(run-a) error: %v", err)
	}
	if !acquired {
		t.Fatal("run-a could not acquire an unheld job")
	}

	acquired, holder, err := model.AcquireCrawlJobLease(jobID, "run-b", time.Minute)
	if err != nil {
		t.Fatalf("AcquireCrawlJobLease(run-b) error: %v", err)
	}
	if acquired {
		t.Fatal("run-b acquired a job already held by run-a")
	}
	if holder == nil || holder.LockedBy != "run-a" {
		t.Fatalf("holder = %+v, want lease held by run-a", holder)
	}

	renewed, err := model.RenewCrawlJobLease(jobID, "run-a", time.Minute)
	if err != nil {
		t.Fatalf("RenewCrawlJobLease(run-a) error: %v", err)
	}
	if !renewed {
		t.Error("the holder could not renew its own lease")
	}

	renewed, err = model.RenewCrawlJobLease(jobID, "run-b", time.Minute)
	if err != nil {
		t.Fatalf("RenewCrawlJobLease(run-b) error: %v", err)
	}
	if renewed {
		t.Error("run-b renewed a lease it does not hold")
	}

	// Releasing is scoped to the holder, so a stale run cannot free the job
	// out from under the run that owns it.
	if err := model.ReleaseCrawlJobLease(jobID, "run-b"); err != nil {
		t.Fatalf("ReleaseCrawlJobLease(run-b) error: %v", err)
	}
	acquired, _, err = model.AcquireCrawlJobLease(jobID, "run-b", time.Minute)
	if err != nil {
		t.Fatalf("AcquireCrawlJobLease(run-b) error: %v", err)
	}
	if acquired {
		t.Fatal("run-b freed and took a lease held by run-a")
	}

	if err := model.ReleaseCrawlJobLease(jobID, "run-a"); err != nil {
		t.Fatalf("ReleaseCrawlJobLease(run-a) error: %v", err)
	}
	acquired, _, err = model.AcquireCrawlJobLease(jobID, "run-b", time.Minute)
	if err != nil {
		t.Fatalf("AcquireCrawlJobLease(run-b) error: %v", err)
	}
	if !acquired {
		t.Error("run-b could not acquire the job after run-a released it")
	}
}

// TestCrawlJobLeaseExpiryAllowsTakeover covers the crash path: a run that dies
// without releasing must not lock the job forever.
func TestCrawlJobLeaseExpiryAllowsTakeover(t *testing.T) {
	testutil.InitModel(t)

	jobID := "lease-expiry"
	if err := model.CreateCrawlJob(jobID, "https://example.com/", "", ""); err != nil {
		t.Fatalf("CreateCrawlJob() error: %v", err)
	}

	if _, _, err := model.AcquireCrawlJobLease(jobID, "dead-run", -time.Second); err != nil {
		t.Fatalf("AcquireCrawlJobLease(dead-run) error: %v", err)
	}

	acquired, _, err := model.AcquireCrawlJobLease(jobID, "new-run", time.Minute)
	if err != nil {
		t.Fatalf("AcquireCrawlJobLease(new-run) error: %v", err)
	}
	if !acquired {
		t.Fatal("an expired lease still blocks the job")
	}

	renewed, err := model.RenewCrawlJobLease(jobID, "dead-run", time.Minute)
	if err != nil {
		t.Fatalf("RenewCrawlJobLease(dead-run) error: %v", err)
	}
	if renewed {
		t.Error("the timed-out run renewed a lease it had lost")
	}
}

// TestCrawlJobLeaseAcquiresUnsetLock covers jobs created before the lease
// columns existed, where both columns are NULL after the migration.
func TestCrawlJobLeaseAcquiresUnsetLock(t *testing.T) {
	testutil.InitModel(t)

	jobID := "lease-legacy-row"
	if err := model.CreateCrawlJob(jobID, "https://example.com/", "", ""); err != nil {
		t.Fatalf("CreateCrawlJob() error: %v", err)
	}
	if err := model.DB.Exec(
		"UPDATE crawl_jobs SET locked_by = NULL, lock_expires_at = NULL WHERE id = ?", jobID,
	).Error; err != nil {
		t.Fatalf("clearing lease columns: %v", err)
	}

	acquired, holder, err := model.AcquireCrawlJobLease(jobID, "run-a", time.Minute)
	if err != nil {
		t.Fatalf("AcquireCrawlJobLease() error: %v", err)
	}
	if !acquired {
		t.Fatalf("a job with no lease recorded could not be acquired (holder %+v)", holder)
	}
}
