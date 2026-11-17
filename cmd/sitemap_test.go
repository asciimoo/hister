// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/crawler"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/model"
	"github.com/asciimoo/nogon/server/testutil"

	"github.com/spf13/cobra"
)

func sitemapTestCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "sitemap", Args: cobra.ExactArgs(1)}
	cmd.SetContext(context.Background())
	addURLInputFlags(cmd)
	cmd.Flags().String("label", "sitemap", "")
	cmd.Flags().Bool("ignore-rules", false, "")
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd
}

func TestReadSitemapInput(t *testing.T) {
	reader, err := newSitemapReader(&config.CrawlerConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	content := `<urlset><url><loc>https://example.com/one</loc></url></urlset>`
	file := filepath.Join(t.TempDir(), "sitemap.xml")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"-", file} {
		cmd := sitemapTestCommand()
		cmd.SetIn(strings.NewReader(content))
		got, err := readSitemapInput(cmd, reader, source)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, []string{"https://example.com/one"}) {
			t.Errorf("source %s: %v", source, got)
		}
	}
}

func TestImportSitemapQueuesOnlyListedPages(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprint("force=", force), func(t *testing.T) {
			oldCfg, oldDB := cfg, model.DB
			t.Cleanup(func() { cfg, model.DB = oldCfg, oldDB })
			cfg = testutil.Config(t)
			cfg.Crawler.Backend = "http"
			cfg.Crawler.Delay = 0
			cfg.Crawler.Timeout = 2
			cfg.Crawler.Headers = map[string]string{"X-Sitemap-Test": "configured"}
			var submitted []document.Document
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/sitemap.xml":
					if _, err := fmt.Fprintf(w, `<urlset><url><loc>%s/page</loc></url><url><loc>%s/page</loc></url><url><loc>%s/existing</loc></url><url><loc>%s/blocked</loc></url></urlset>`, server.URL, server.URL, server.URL, server.URL); err != nil {
						t.Error(err)
					}
				case "/robots.txt":
					if _, err := fmt.Fprint(w, "User-agent: *\nDisallow: /blocked\n"); err != nil {
						t.Error(err)
					}
				case "/api/document":
					if !strings.HasSuffix(r.URL.Query().Get("url"), "/existing") {
						w.WriteHeader(http.StatusNotFound)
					}
				case "/page", "/existing":
					if r.Header.Get("X-Sitemap-Test") != "configured" {
						t.Error("crawler header missing")
					}
					if r.URL.Path == "/existing" && !force {
						t.Error("existing page fetched without --force")
					}
					w.Header().Set("Content-Type", "text/html")
					if _, err := fmt.Fprint(w, `<html><head><title>Sitemap page</title></head><body><p>Content of the listed page.</p><a href="/unlisted">Do not crawl this page</a></body></html>`); err != nil {
						t.Error(err)
					}
				case "/api/add":
					var doc document.Document
					if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
						t.Error(err)
					}
					submitted = append(submitted, doc)
				case "/favicon.ico":
					w.WriteHeader(http.StatusNotFound)
				default:
					t.Errorf("unexpected request: %s", r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			if err := cfg.UpdateBaseURL(server.URL); err != nil {
				t.Fatal(err)
			}
			cmd := sitemapTestCommand()
			if err := cmd.ParseFlags([]string{"--label=reference", "--ignore-rules", fmt.Sprintf("--force=%v", force)}); err != nil {
				t.Fatal(err)
			}
			var summary bytes.Buffer
			cmd.SetOut(&summary)
			if err := importSitemapCmd.RunE(cmd, []string{server.URL + "/sitemap.xml"}); err != nil {
				t.Fatal(err)
			}
			db, err := model.DB.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			wantSubmitted := 1
			if force {
				wantSubmitted = 2
			}
			if len(submitted) != wantSubmitted {
				t.Fatalf("submitted %d pages, want %d", len(submitted), wantSubmitted)
			}
			for _, doc := range submitted {
				if doc.Label != "reference" || !doc.IgnoreSkipRules() || doc.Title != "Sitemap page" {
					t.Errorf("unexpected document: %#v", doc)
				}
			}
			jobID := sitemapJobName(server.URL + "/sitemap.xml")
			job, err := model.GetCrawlJob(jobID)
			if err != nil || job == nil {
				t.Fatalf("missing job: %v", err)
			}
			rules, err := crawler.UnmarshalValidatorRules(job.ValidatorRules)
			if err != nil || !rules.NoDepth {
				t.Fatalf("job should persist NoDepth: %v", err)
			}
			stats, err := model.GetCrawlJobStats(jobID)
			if err != nil || stats.Done != int64(wantSubmitted) || stats.Skipped != int64(3-wantSubmitted) {
				t.Fatalf("unexpected job stats: %+v, %v", stats, err)
			}
			if !strings.Contains(summary.String(), jobID) {
				t.Errorf("summary does not contain job ID: %s", &summary)
			}
		})
	}
}

func TestSitemapJobCanResume(t *testing.T) {
	oldCfg, oldDB := cfg, model.DB
	t.Cleanup(func() { cfg, model.DB = oldCfg, oldDB })
	cfg = testutil.InitModel(t)
	cfg.Crawler.Backend = "http"
	cfg.Crawler.Delay = 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Existing pages allow resume to finish without fetching page contents.
		if r.Method != http.MethodHead || r.URL.Path != "/api/document" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	}))
	defer server.Close()
	if err := cfg.UpdateBaseURL(server.URL); err != nil {
		t.Fatal(err)
	}
	cmd := sitemapTestCommand()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cmd.SetContext(ctx)
	rules := &crawler.ValidatorRules{NoDepth: true}
	urls := []string{"https://example.com/one", "https://example.com/two"}
	err := runURLInputJob(cmd, "sitemap-resume", urls, rules, nil, &failedURLReport{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled job: %v", err)
	}
	job, err := model.GetCrawlJob("sitemap-resume")
	if err != nil || job == nil {
		t.Fatalf("missing job: %v", err)
	}
	if pending, err := crawlJobHasURLsToCrawl(job); err != nil || !pending {
		t.Fatalf("expected resumable job: %v", err)
	}
	storedRules, err := crawler.UnmarshalValidatorRules(job.ValidatorRules)
	if err != nil {
		t.Fatal(err)
	}
	if err := runPersistentIndexJob(t.Context(), job.ID, job.StartURL, storedRules, job.Label, nil, false); err != nil {
		t.Fatal(err)
	}
	stats, err := model.GetCrawlJobStats(job.ID)
	if err != nil || stats.Pending != 0 || stats.InProgress != 0 || stats.Skipped != 2 {
		t.Fatalf("unexpected resumed stats: %+v, %v", stats, err)
	}
}
