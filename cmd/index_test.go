package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/crawler"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/extractor"
	"github.com/asciimoo/nogon/server/model"
	"github.com/asciimoo/nogon/server/testutil"

	"github.com/spf13/cobra"
)

func TestPersistentIndexWaitsForSubmission(t *testing.T) {
	oldCfg, oldDB := cfg, model.DB
	t.Cleanup(func() { cfg, model.DB = oldCfg, oldDB })
	cfg = testutil.InitModel(t)
	cfg.Crawler.Backend = "http"
	cfg.Crawler.Delay = 0
	const jobID = "submission-ack"
	var submitted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			if _, err := fmt.Fprint(w, `<html><head><title>Pending submission</title></head><body><p>This document must be accepted before the crawl is done.</p></body></html>`); err != nil {
				t.Error(err)
			}
		case "/api/add":
			submitted.Store(true)
			stats, err := model.GetCrawlJobStats(jobID)
			if err != nil || stats.Done != 0 || stats.InProgress != 1 {
				t.Errorf("crawl state before submission succeeds: %+v, %v; want one in progress and none done", stats, err)
			}
			job, err := model.GetCrawlJob(jobID)
			if err != nil || job == nil || job.Status != model.CrawlJobRunning {
				t.Errorf("job before submission succeeds: %+v, %v; want running", job, err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if err := cfg.UpdateBaseURL(server.URL); err != nil {
		t.Fatal(err)
	}
	startURL := server.URL + "/page"
	rules := &crawler.ValidatorRules{NoDepth: true}
	if err := model.CreateCrawlJob(jobID, startURL, `{"NoDepth":true}`, ""); err != nil {
		t.Fatal(err)
	}
	if err := runPersistentIndexJob(t.Context(), jobID, startURL, rules, "", nil, true); err != nil {
		t.Fatal(err)
	}
	if !submitted.Load() {
		t.Fatal("document was not submitted")
	}
	stats, err := model.GetCrawlJobStats(jobID)
	if err != nil || stats.Done != 1 || stats.InProgress != 0 {
		t.Fatalf("crawl state after submission succeeds: %+v, %v", stats, err)
	}
}

func TestPersistentIndexResumesInterruptedSubmission(t *testing.T) {
	oldCfg, oldDB := cfg, model.DB
	t.Cleanup(func() { cfg, model.DB = oldCfg, oldDB })
	cfg = testutil.InitModel(t)
	cfg.Crawler.Backend = "http"
	cfg.Crawler.Delay = 0
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/page", http.StatusFound)
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			if _, err := fmt.Fprint(w, `<html><head><title>Resumable page</title></head><body><p>Retry this document after an interrupted submission.</p></body></html>`); err != nil {
				t.Error(err)
			}
		case "/api/add":
			if attempts.Add(1) == 1 {
				cancel()
				http.Error(w, "submission interrupted", http.StatusServiceUnavailable)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if err := cfg.UpdateBaseURL(server.URL); err != nil {
		t.Fatal(err)
	}
	const jobID = "interrupted-submission"
	startURL := server.URL + "/start"
	rules := &crawler.ValidatorRules{NoDepth: true}
	if err := model.CreateCrawlJob(jobID, startURL, `{"NoDepth":true}`, ""); err != nil {
		t.Fatal(err)
	}
	if err := runPersistentIndexJob(ctx, jobID, startURL, rules, "", nil, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted submission error = %v, want cancellation", err)
	}
	stats, err := model.GetCrawlJobStats(jobID)
	if err != nil || stats.Pending != 1 || stats.InProgress != 0 || stats.Done != 0 || stats.Failed != 0 {
		t.Fatalf("interrupted crawl must remain resumable without completed redirect targets: %+v, %v", stats, err)
	}
	job, err := model.GetCrawlJob(jobID)
	if err != nil || job == nil || job.Status != model.CrawlJobInterrupted {
		t.Fatalf("interrupted job: %+v, %v", job, err)
	}
	if err := runPersistentIndexJob(t.Context(), jobID, startURL, rules, "", nil, true); err != nil {
		t.Fatal(err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("submission attempts = %d, want interrupted submission retried", got)
	}
	stats, err = model.GetCrawlJobStats(jobID)
	if err != nil || stats.Done != 2 || stats.Pending != 0 || stats.InProgress != 0 || stats.Failed != 0 {
		t.Fatalf("resumed crawl should complete source and redirect target: %+v, %v", stats, err)
	}
	job, err = model.GetCrawlJob(jobID)
	if err != nil || job == nil || job.Status != model.CrawlJobCompleted {
		t.Fatalf("resumed job: %+v, %v", job, err)
	}
}

func TestPersistentIndexRecordsRedirectFailureAndContinues(t *testing.T) {
	oldCfg, oldDB := cfg, model.DB
	t.Cleanup(func() { cfg, model.DB = oldCfg, oldDB })
	cfg = testutil.InitModel(t)
	cfg.Crawler.Backend = "http"
	cfg.Crawler.Delay = 0
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/page?utm_source=test", http.StatusFound)
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			if _, err := fmt.Fprint(w, `<html><head><title>Rejected page</title></head><body><p>Its child must still be crawled.</p><a href="/child#section">Child</a></body></html>`); err != nil {
				t.Error(err)
			}
		case "/child":
			w.Header().Set("Content-Type", "text/html")
			if _, err := fmt.Fprint(w, `<html><head><title>Accepted child</title></head><body><p>This child should still be indexed after its parent fails.</p></body></html>`); err != nil {
				t.Error(err)
			}
		case "/api/add":
			attempts.Add(1)
			var doc document.Document
			if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
				t.Error(err)
			}
			if strings.HasSuffix(doc.URL, "/page") {
				http.Error(w, "index unavailable", http.StatusServiceUnavailable)
			} else if !strings.HasSuffix(doc.URL, "/child") {
				t.Errorf("unexpected submitted URL %q", doc.URL)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if err := cfg.UpdateBaseURL(server.URL); err != nil {
		t.Fatal(err)
	}
	const jobID = "rejected-submission"
	startURL := server.URL + "/start"
	if err := model.CreateCrawlJob(jobID, startURL, `{}`, ""); err != nil {
		t.Fatal(err)
	}
	err := runPersistentIndexJob(t.Context(), jobID, startURL, &crawler.ValidatorRules{}, "", nil, true)
	if failure, ok := errors.AsType[*partialFailure](err); !ok || failure.count != 1 {
		t.Fatalf("crawl error = %v, want one failed submission", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("submission attempts = %d, want parent and child", attempts.Load())
	}
	stats, err := model.GetCrawlJobStats(jobID)
	if err != nil || stats.Done != 1 || stats.Failed != 1 || stats.InProgress != 0 || stats.Pending != 0 {
		t.Fatalf("crawl should complete child and fail original source: %+v, %v", stats, err)
	}
	failedURLs := 0
	if err := model.ForEachFailedCrawlURLWithMessage(jobID, func(code int, rawURL, message string) error {
		failedURLs++
		if rawURL != startURL || code != http.StatusServiceUnavailable || !strings.Contains(message, "index unavailable") {
			t.Errorf("failure record = (%q, %d, %q), want original URL and server failure", rawURL, code, message)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if failedURLs != 1 {
		t.Fatalf("failed URL records = %d, want 1", failedURLs)
	}
}

func TestPersistentIndexRetainsSourceWhenLinkQueueFails(t *testing.T) {
	oldCfg, oldDB := cfg, model.DB
	t.Cleanup(func() { cfg, model.DB = oldCfg, oldDB })
	cfg = testutil.InitModel(t)
	cfg.Crawler.Backend = "http"
	cfg.Crawler.Delay = 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page", "/child":
			w.Header().Set("Content-Type", "text/html")
			if _, err := fmt.Fprint(w, `<html><head><title>Rejected document</title></head><body><p>Preserve its discovered links.</p><a href="/child">Child</a></body></html>`); err != nil {
				t.Error(err)
			}
		case "/api/add":
			http.Error(w, "index unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if err := cfg.UpdateBaseURL(server.URL); err != nil {
		t.Fatal(err)
	}
	const jobID = "queue-write-failure"
	startURL := server.URL + "/page"
	if err := model.CreateCrawlJob(jobID, startURL, `{}`, ""); err != nil {
		t.Fatal(err)
	}
	if err := model.DB.Exec(`CREATE TRIGGER reject_child_queue BEFORE INSERT ON crawl_urls
WHEN NEW.depth > 0 BEGIN SELECT RAISE(ABORT, 'injected queue failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	err := runPersistentIndexJob(t.Context(), jobID, startURL, &crawler.ValidatorRules{}, "", nil, true)
	if err == nil || !strings.Contains(err.Error(), "injected queue failure") {
		t.Fatalf("queue failure was not returned: %v", err)
	}
	stats, err := model.GetCrawlJobStats(jobID)
	if err != nil || stats.InProgress != 1 || stats.Done != 0 || stats.Failed != 0 {
		t.Fatalf("source must remain retryable until its links are saved: %+v, %v", stats, err)
	}
	if err := model.DB.Exec("DROP TRIGGER reject_child_queue").Error; err != nil {
		t.Fatal(err)
	}
	err = runPersistentIndexJob(t.Context(), jobID, startURL, &crawler.ValidatorRules{}, "", nil, true)
	if failure, ok := errors.AsType[*partialFailure](err); !ok || failure.count != 2 {
		t.Fatalf("resume should submit the source and child: %v", err)
	}
	stats, err = model.GetCrawlJobStats(jobID)
	if err != nil || stats.Failed != 2 || stats.InProgress != 0 || stats.Pending != 0 {
		t.Fatalf("resumed queue lost documents: %+v, %v", stats, err)
	}
}

func TestPersistentIndexResumesAfterAcceptedSubmission(t *testing.T) {
	oldCfg, oldDB := cfg, model.DB
	t.Cleanup(func() { cfg, model.DB = oldCfg, oldDB })
	cfg = testutil.InitModel(t)
	cfg.Crawler.Backend = "http"
	cfg.Crawler.Delay = 0
	var accepted atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			if _, err := fmt.Fprint(w, `<html><head><title>Accepted parent</title></head><body><p>Preserve this page's links after submission.</p><a href="/child">Child</a></body></html>`); err != nil {
				t.Error(err)
			}
		case "/child":
			w.Header().Set("Content-Type", "text/html")
			if _, err := fmt.Fprint(w, `<html><head><title>Child</title></head><body><p>The child must be discovered on resume.</p></body></html>`); err != nil {
				t.Error(err)
			}
		case "/api/document":
			if accepted.Load() == 0 || !strings.HasSuffix(r.URL.Query().Get("url"), "/page") {
				w.WriteHeader(http.StatusNotFound)
			}
		case "/api/add":
			accepted.Add(1)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if err := cfg.UpdateBaseURL(server.URL); err != nil {
		t.Fatal(err)
	}
	const jobID = "accepted-submission"
	startURL := server.URL + "/page"
	if err := model.CreateCrawlJob(jobID, startURL, `{}`, ""); err != nil {
		t.Fatal(err)
	}
	if err := model.DB.Exec(`CREATE TRIGGER reject_completion BEFORE UPDATE ON crawl_urls
WHEN NEW.status = 'done' BEGIN SELECT RAISE(ABORT, 'injected completion failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	err := runPersistentIndexJob(t.Context(), jobID, startURL, &crawler.ValidatorRules{}, "", nil, false)
	if err == nil || !strings.Contains(err.Error(), "injected completion failure") || accepted.Load() != 1 {
		t.Fatalf("expected completion failure after accepted submission: %v, accepted=%d", err, accepted.Load())
	}
	stats, err := model.GetCrawlJobStats(jobID)
	if err != nil || stats.InProgress != 1 || stats.Pending != 1 || stats.Done != 0 {
		t.Fatalf("accepted page must retain its discovered links before completion: %+v, %v", stats, err)
	}
	if err := model.DB.Exec("DROP TRIGGER reject_completion").Error; err != nil {
		t.Fatal(err)
	}
	if err := runPersistentIndexJob(t.Context(), jobID, startURL, &crawler.ValidatorRules{}, "", nil, false); err != nil {
		t.Fatal(err)
	}
	stats, err = model.GetCrawlJobStats(jobID)
	if err != nil || stats.Done != 1 || stats.Skipped != 1 || stats.Pending != 0 || stats.InProgress != 0 || accepted.Load() != 2 {
		t.Fatalf("resume must crawl child after skipping the indexed parent: %+v, %v, accepted=%d", stats, err, accepted.Load())
	}
}

func TestIndexInitializesExtractors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake yt-dlp executable requires a POSIX shell")
	}
	oldCfg := cfg
	t.Cleanup(func() { cfg = oldCfg })
	for _, e := range extractor.DefaultRegistry().Extractors() {
		original := e.GetConfig()
		t.Cleanup(func() {
			if err := e.SetConfig(original); err != nil {
				t.Error(err)
			}
		})
	}

	binary := filepath.Join(t.TempDir(), "yt-dlp")
	script := `#!/bin/sh
printf '%s\n' '{"title":"Video title","description":"Video description"}'
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name      string
		enabled   bool
		wantTitle string
		wantText  string
	}{
		{"enabled", true, "Video title", "Video description"},
		{"disabled", false, "Page title", "Page contents"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg = config.CreateDefaultConfig()
			cfg.Extractors = map[string]*config.Extractor{
				"ytdlp": {
					Enable: tc.enabled,
					Options: map[string]any{
						"binary": binary,
					},
				},
			}
			cmd := newIndexTestCommand()
			indexCmd.PreRun(cmd, nil)

			d := &document.Document{
				URL:  "https://www.youtube.com/watch?v=extractor-init-" + tc.name,
				HTML: "<html><head><title>Page title</title></head><body><p>Page contents</p></body></html>",
			}
			if err := d.ProcessContext(t.Context(), nil, extractor.ExtractContext); err != nil {
				t.Fatal(err)
			}
			if d.Title != tc.wantTitle || !strings.Contains(d.Text, tc.wantText) {
				t.Fatalf("extracted title = %q, text = %q; want title %q and text containing %q", d.Title, d.Text, tc.wantTitle, tc.wantText)
			}
		})
	}
}

func newIndexTestCommand() *cobra.Command {
	cmd := &cobra.Command{Use: indexCmd.Use, Args: validateIndexArgs, RunE: indexCmd.RunE}
	addIndexFlags(cmd)
	return cmd
}

func TestValidateIndexArgs(t *testing.T) {
	tests := []struct {
		name        string
		jobID       string
		input       string
		legacyInput string
		args        []string
		wantErr     bool
		wantErrText string
	}{
		{name: "URL", args: []string{"https://example.com"}},
		{name: "job ID without URL", jobID: "docs-crawl"},
		{name: "input without URL", input: "urls.txt"},
		{name: "legacy input without URL", legacyInput: "urls.txt"},
		{name: "neither job ID, input, nor URL", wantErr: true},
		{name: "job ID and input", jobID: "docs-crawl", input: "urls.txt", wantErr: true, wantErrText: "--job-id and --input"},
		{name: "input and legacy input", input: "urls.txt", legacyInput: "old.txt", wantErr: true, wantErrText: "--input and --url-list"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newIndexTestCommand()
			if err := cmd.Flags().Set("job-id", tt.jobID); err != nil {
				t.Fatalf("set job-id flag: %v", err)
			}
			if err := cmd.Flags().Set("input", tt.input); err != nil {
				t.Fatalf("set input flag: %v", err)
			}
			if err := cmd.Flags().Set("url-list", tt.legacyInput); err != nil {
				t.Fatalf("set url-list flag: %v", err)
			}

			err := validateIndexArgs(cmd, tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateIndexArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.wantErrText != "" && !strings.Contains(err.Error(), tt.wantErrText) {
				t.Fatalf("validateIndexArgs() error = %q, want text %q", err, tt.wantErrText)
			}
		})
	}
}

func TestResolveIndexURLsPrefersInputFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "urls.txt")
	if err := os.WriteFile(path, []byte(" https://example.com \r\n\nhttps://example.org\n"), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	cmd := newIndexTestCommand()
	if err := cmd.Flags().Set("input", path); err != nil {
		t.Fatalf("set input flag: %v", err)
	}
	want := []string{"https://example.com", "https://example.org"}

	got, err := resolveIndexURLs(cmd, []string{"https://ignored.example"})
	if err != nil {
		t.Fatalf("resolveIndexURLs() error: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("resolveIndexURLs() = %q, want %q", got, want)
	}
}

func TestResolveIndexURLsReadsStandardInput(t *testing.T) {
	cmd := newIndexTestCommand()
	if err := cmd.Flags().Set("input", "-"); err != nil {
		t.Fatalf("set input flag: %v", err)
	}
	cmd.SetIn(strings.NewReader("https://example.com\n\n https://example.org \n"))

	got, err := resolveIndexURLs(cmd, nil)
	if err != nil {
		t.Fatalf("resolveIndexURLs() error: %v", err)
	}
	want := []string{"https://example.com", "https://example.org"}
	if !slices.Equal(got, want) {
		t.Fatalf("resolveIndexURLs() = %q, want %q", got, want)
	}
}

func TestResolveIndexURLsSupportsLegacyFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "urls.txt")
	if err := os.WriteFile(path, []byte("https://example.com\n"), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	cmd := newIndexTestCommand()
	if err := cmd.Flags().Set("url-list", path); err != nil {
		t.Fatalf("set url-list flag: %v", err)
	}

	got, err := resolveIndexURLs(cmd, nil)
	if err != nil {
		t.Fatalf("resolveIndexURLs() error: %v", err)
	}
	if want := []string{"https://example.com"}; !slices.Equal(got, want) {
		t.Fatalf("resolveIndexURLs() = %q, want %q", got, want)
	}
}

func TestResolveIndexURLsRejectsEmptyInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "urls.txt")
	if err := os.WriteFile(path, []byte("\n \t\n"), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	cmd := newIndexTestCommand()
	if err := cmd.Flags().Set("input", path); err != nil {
		t.Fatalf("set input flag: %v", err)
	}

	if _, err := resolveIndexURLs(cmd, nil); err == nil {
		t.Fatal("resolveIndexURLs() expected an error for empty input")
	}
}

func TestResolveIndexURLsRejectsEmptyStandardInput(t *testing.T) {
	cmd := newIndexTestCommand()
	if err := cmd.Flags().Set("input", "-"); err != nil {
		t.Fatalf("set input flag: %v", err)
	}
	cmd.SetIn(strings.NewReader("\n \t\n"))

	if _, err := resolveIndexURLs(cmd, nil); err == nil || err.Error() != "standard input contains no URLs" {
		t.Fatalf("resolveIndexURLs() error = %v, want empty standard input error", err)
	}
}

func TestIndexInputJobName(t *testing.T) {
	if got, want := indexInputJobName(filepath.Join("tmp", "documentation-urls.txt")), "documentation-urls.txt"; got != want {
		t.Fatalf("indexInputJobName(file) = %q, want %q", got, want)
	}
	if got, want := indexInputJobName("-"), "stdin"; got != want {
		t.Fatalf("indexInputJobName(stdin) = %q, want %q", got, want)
	}
}

func TestIndexInputFlags(t *testing.T) {
	if flag := indexCmd.Flags().Lookup("input"); flag == nil || flag.Hidden {
		t.Fatal("--input should be visible")
	}
	legacy := indexCmd.Flags().Lookup("url-list")
	if legacy == nil || !legacy.Hidden || legacy.Deprecated == "" {
		t.Fatal("--url-list should remain as a hidden deprecated alias")
	}
}
