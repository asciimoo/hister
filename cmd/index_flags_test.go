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
	"reflect"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/server/crawler"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/model"
	"github.com/asciimoo/nogon/server/testutil"
)

func TestIndexSubmissionFlags(t *testing.T) {
	for _, tc := range []struct {
		name, inputFlag, owner             string
		flags                              []string
		force, noRobots, overrides, resume bool
	}{
		{name: "positional defaults"},
		{name: "stdin overrides", inputFlag: "input", owner: "0", flags: []string{"--global", "--force", "--no-robots", "--allow-sensitive", "--ignore-rules"}, force: true, noRobots: true, overrides: true},
		{name: "file user", inputFlag: "input", owner: "42", flags: []string{"--user-id=42", "--force=false", "--allow-sensitive=false", "--ignore-rules=false"}},
		{name: "legacy global", inputFlag: "url-list", owner: "0", flags: []string{"--user-id=0", "--no-robots"}, noRobots: true},
		{name: "resume user", owner: "42", flags: []string{"--user-id=42", "--force", "--allow-sensitive", "--ignore-rules"}, force: true, overrides: true, resume: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldCfg, oldDB, oldUserAgent := cfg, model.DB, UserAgent
			t.Cleanup(func() { cfg, model.DB, UserAgent = oldCfg, oldDB, oldUserAgent })
			cfg = testutil.InitModel(t)
			cfg.App.Public = false
			cfg.Crawler.Backend = "http"
			cfg.Crawler.Delay = 0
			cfg.Crawler.Timeout = 2
			var submitted []document.Document
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/document", "/api/add":
					if got := r.Header.Get("X-Nogon-Target-User-ID"); got != tc.owner {
						t.Errorf("target user = %q, want %q", got, tc.owner)
					}
					if r.URL.Path == "/api/document" {
						if tc.force {
							t.Error("--force should bypass the existence check")
						}
						if !strings.HasSuffix(r.URL.Query().Get("url"), "/existing") {
							w.WriteHeader(http.StatusNotFound)
						}
						return
					}
					var doc document.Document
					if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
						t.Error(err)
					}
					submitted = append(submitted, doc)
				case "/robots.txt":
					if tc.noRobots {
						t.Error("--no-robots should bypass robots requests")
					}
					if _, err := fmt.Fprint(w, "User-agent: *\nDisallow: /blocked\n"); err != nil {
						t.Error(err)
					}
				case "/page", "/existing", "/blocked":
					if r.URL.Path == "/existing" && !tc.force || r.URL.Path == "/blocked" && !tc.noRobots {
						t.Errorf("fetched a page that should be skipped: %s", r.URL.Path)
					}
					if r.UserAgent() != "flag agent" || r.Header.Get("X-Flag-Test") != "value" {
						t.Error("crawler request flags were not applied")
					}
					cookie, err := r.Cookie("session")
					if err != nil || cookie.Value != "abc" {
						t.Errorf("crawler cookie = %v, %v", cookie, err)
					}
					w.Header().Set("Content-Type", "text/html")
					if _, err := fmt.Fprint(w, `<html><head><title>Flag test</title></head><body><p>Page content.</p><a href="/unlisted">Unlisted page</a></body></html>`); err != nil {
						t.Error(err)
					}
				case "/missing", "/favicon.ico":
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
			cmd := newIndexTestCommand()
			cmd.SetContext(t.Context())
			cmd.SetErr(io.Discard)
			var output bytes.Buffer
			cmd.SetOut(io.Discard)
			reportPath := filepath.Join(t.TempDir(), "failed.txt")
			if err := os.WriteFile(reportPath, []byte("previous report\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{
				"-f=json", "--failed-urls=" + reportPath,
				"--backend=http", "--user-agent=flag agent", "--header=X-Flag-Test=value",
				"--cookie=session=abc; Domain=127.0.0.1", "--delay=0", "--timeout=2",
			}
			args = append(args, tc.flags...)
			urls := []string{server.URL + "/page", server.URL + "/existing", server.URL + "/blocked", server.URL + "/missing"}
			if !tc.resume {
				args = append(args, "--label=reading")
			}
			if tc.resume {
				rulesJSON, err := crawler.MarshalValidatorRules(&crawler.ValidatorRules{NoDepth: true})
				if err != nil {
					t.Fatal(err)
				}
				jobID, err := model.CreateNamedCrawlJobWithURLs("flag-resume", urls[0], rulesJSON, "reading", urls)
				if err != nil {
					t.Fatal(err)
				}
				args = append(args, "--job-id="+jobID)
			} else if tc.inputFlag != "" {
				input := strings.Join(urls, "\n")
				path := "-"
				if tc.name == "stdin overrides" {
					cmd.SetIn(strings.NewReader(input))
				} else {
					path = filepath.Join(t.TempDir(), "urls.txt")
					if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				args = append(args, "--"+tc.inputFlag+"="+path, server.URL+"/ignored-positional")
			} else {
				args = append(args, urls...)
			}
			if err := cmd.ParseFlags(args); err != nil {
				t.Fatal(err)
			}
			if err := cmd.ValidateArgs(cmd.Flags().Args()); err != nil {
				t.Fatal(err)
			}
			// Capture results after parsing so Cobra's legacy flag warning stays separate.
			cmd.SetOut(&output)
			if err := cmd.RunE(cmd, cmd.Flags().Args()); ExitCode(err) != 2 {
				t.Fatalf("expected partial failure for /missing, got %v", err)
			}
			wantIndexed := 1
			if tc.force {
				wantIndexed++
			}
			if tc.noRobots {
				wantIndexed++
			}
			if len(submitted) != wantIndexed {
				t.Fatalf("submitted %d documents, want %d", len(submitted), wantIndexed)
			}
			for _, doc := range submitted {
				if doc.Label != "reading" || doc.SkipSensitiveCheck != tc.overrides || doc.IgnoreSkipRules() != tc.overrides {
					t.Errorf("incorrect submission options: %#v", doc)
				}
			}
			var summary []struct {
				Indexed, Skipped, Failed, Pending int
				JobID                             string `json:"job_id"`
			}
			if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
				t.Fatalf("--format=json output: %v; %s", err, &output)
			}
			if len(summary) != 1 || summary[0].Indexed != wantIndexed || summary[0].Skipped != 3-wantIndexed || summary[0].Failed != 1 || summary[0].Pending != 0 {
				t.Fatalf("unexpected summary: %s", &output)
			}
			if tc.inputFlag != "" || tc.resume {
				job, err := model.GetCrawlJob(summary[0].JobID)
				if err != nil || job == nil || job.Label != "reading" {
					t.Fatalf("missing labeled persistent job: %v, %v", job, err)
				}
				rules, err := crawler.UnmarshalValidatorRules(job.ValidatorRules)
				if err != nil || !rules.NoDepth {
					t.Fatalf("URL input should disable link traversal: %v, %v", rules, err)
				}
			}
			failed, err := os.ReadFile(reportPath)
			if err != nil || string(failed) != server.URL+"/missing\n" {
				t.Errorf("failed URL report = %q, %v", failed, err)
			}
		})
	}
}

func TestIndexRecursiveInputFlags(t *testing.T) {
	oldCfg, oldDB, oldUserAgent := cfg, model.DB, UserAgent
	t.Cleanup(func() { cfg, model.DB, UserAgent = oldCfg, oldDB, oldUserAgent })
	cfg = testutil.InitModel(t)
	cfg.Crawler.Backend = "http"
	for _, input := range []bool{false, true} {
		t.Run(fmt.Sprint("input=", input), func(t *testing.T) {
			cmd := newIndexTestCommand()
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			cmd.SetContext(ctx)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			args := []string{
				"-r", "--max-depth=2", "--max-links=10", "--label=recursive", "--no-robots",
				"--allowed-domain=example.com", "--allowed-domain=example.org",
				"--exclude-domain=private.example.com", "--exclude-domain=private.example.org",
				"--allowed-pattern=/docs/", "--allowed-pattern=/news/",
				"--exclude-pattern=/login", "--exclude-pattern=/logout",
			}
			jobID := "recursive-flags"
			if input {
				jobID = "stdin"
				cmd.SetIn(strings.NewReader("https://example.com/docs/\n"))
				args = append(args, "--input=-")
			} else {
				args = append(args, "--job-id="+jobID, "https://example.com/docs/")
			}
			if err := cmd.ParseFlags(args); err != nil {
				t.Fatal(err)
			}
			if err := cmd.ValidateArgs(cmd.Flags().Args()); err != nil {
				t.Fatal(err)
			}
			if err := cmd.RunE(cmd, cmd.Flags().Args()); !errors.Is(err, context.Canceled) {
				t.Fatalf("expected interrupted crawl: %v", err)
			}
			job, err := model.GetCrawlJob(jobID)
			if err != nil || job == nil || job.Label != "recursive" {
				t.Fatalf("missing labeled job: %v, %v", job, err)
			}
			rules, err := crawler.UnmarshalValidatorRules(job.ValidatorRules)
			if err != nil {
				t.Fatal(err)
			}
			want := &crawler.ValidatorRules{
				MaxDepth: 2, MaxLinks: 10,
				AllowedDomains:  []string{"example.com", "example.org"},
				ExcludeDomains:  []string{"private.example.com", "private.example.org"},
				AllowedPatterns: []string{"/docs/", "/news/"},
				ExcludePatterns: []string{"/login", "/logout"},
			}
			if !reflect.DeepEqual(rules, want) {
				t.Errorf("persisted rules = %#v, want %#v", rules, want)
			}
		})
	}
}
