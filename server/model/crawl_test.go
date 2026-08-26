package model_test

import (
	"slices"
	"testing"

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

