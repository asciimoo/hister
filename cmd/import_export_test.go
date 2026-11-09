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
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func TestImportRSS(t *testing.T) {
	for _, tc := range []struct {
		name  string
		feed  string
		flags []string
		label string
		count int
	}{
		{
			name:  "RSS",
			feed:  `<rss version="2.0"><channel><title>News</title><item><link>%[1]s/article</link></item><item><link>%[1]s/article</link></item><item><link>%[1]s/existing</link></item><item><link>%[1]s/blocked</link></item></channel></rss>`,
			label: "rss", count: 1,
		},
		{
			name: "Atom with relative links and force",
			feed: `<feed xmlns="http://www.w3.org/2005/Atom"><title>News</title>
<entry><link rel="self" href="%[1]s/metadata"/><link rel="alternate" href="../article"/></entry>
<entry><link href="%[1]s/article#section"/></entry>
<entry><link href="%[1]s/existing"/></entry>
<entry><link href="%[1]s/blocked"/></entry>
<entry><title>No link</title></entry>
<entry><link href="file:///nonexistent-hister-feed-entry"/></entry>
<entry><link href="javascript:alert(1)"/></entry>
<entry><link href="http://user:secret@127.0.0.1:1/private"/></entry></feed>`,
			flags: []string{"--force", "--label=news", "--ignore-rules"}, label: "news", count: 2,
		},
		{
			name: "Atom with explicit base URL",
			feed: `<feed xmlns="http://www.w3.org/2005/Atom" xml:base="%[1]s/posts/">
<entry xml:base="../"><link href="article"/></entry>
<entry><link href="%[1]s/existing"/></entry>
<entry><link href="%[1]s/blocked"/></entry></feed>`,
			label: "rss", count: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registered, _, err := rootCmd.Find([]string{"import", "rss"})
			if err != nil || registered.Name() != "rss" {
				t.Fatalf("RSS import command unavailable: command=%s, error=%v", registered.Name(), err)
			}
			oldCfg, oldDB := cfg, model.DB
			t.Cleanup(func() { cfg, model.DB = oldCfg, oldDB })
			cfg = testutil.Config(t)
			cfg.Crawler.Backend = "http"
			cfg.Crawler.Delay = 0
			cfg.Crawler.Timeout = 2
			cfg.Crawler.Headers = map[string]string{"X-Feed-Test": "configured"}
			cfg.App.AccessToken = "hister-only"
			var submitted []document.Document
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("Authorization") != "" {
					t.Error("Hister token sent to feed or article")
				}
				switch r.URL.Path {
				case "/feed.xml":
					if r.URL.Query().Get("format") != "xml" {
						t.Error("feed URL query was lost")
					}
					http.Redirect(w, r, "/feeds/latest.xml", http.StatusFound)
				case "/feeds/latest.xml":
					if r.Header.Get("X-Feed-Test") != "configured" {
						t.Error("configured header missing from feed request")
					}
					_, _ = fmt.Fprintf(w, tc.feed, server.URL)
				case "/robots.txt":
					_, _ = io.WriteString(w, "User-agent: *\nDisallow: /blocked\n")
				case "/favicon.ico":
					w.WriteHeader(http.StatusNotFound)
				case "/api/document":
					if !strings.HasSuffix(r.URL.Query().Get("url"), "/existing") {
						w.WriteHeader(http.StatusNotFound)
					}
				case "/article", "/existing":
					if r.URL.Path == "/existing" && tc.count == 1 {
						t.Error("existing article fetched without --force")
					}
					w.Header().Set("Content-Type", "text/html")
					_, _ = fmt.Fprint(w, `<html><head><title>Feed article</title></head><body><p>Full article content.</p><a href="/unlisted">Do not follow</a></body></html>`)
				case "/api/add":
					var doc document.Document
					if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
						t.Error(err)
					}
					submitted = append(submitted, doc)
				default:
					t.Errorf("unexpected request: %s", r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			if err := cfg.UpdateBaseURL(server.URL); err != nil {
				t.Fatal(err)
			}
			command := &cobra.Command{Use: registered.Use, Args: registered.Args, RunE: registered.RunE}
			addURLInputFlags(command)
			command.Flags().String("label", registered.Flags().Lookup("label").DefValue, "")
			command.Flags().Bool("ignore-rules", false, "")
			command.SetArgs(append([]string{server.URL + "/feed.xml?format=xml"}, tc.flags...))
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&output)
			if err := command.ExecuteContext(t.Context()); err != nil {
				t.Fatal(err)
			}
			db, err := model.DB.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if len(submitted) != tc.count {
				t.Fatalf("submitted %d documents, want %d", len(submitted), tc.count)
			}
			doc := submitted[0]
			if doc.URL != server.URL+"/article" || doc.Title != "Feed article" || doc.Label != tc.label || !strings.Contains(doc.Text, "Full article content.") || doc.IgnoreSkipRules() != (len(tc.flags) > 0) {
				t.Errorf("unexpected imported document: %+v", doc)
			}
			if !strings.Contains(output.String(), "Starting crawl job:") || (tc.count == 1 && !strings.Contains(output.String(), "2 skipped")) {
				t.Errorf("missing job or skip summary: %s", &output)
			}
		})
	}
}

func TestImportRSSRejectsInvalidFeeds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		body   string
		status int
		cancel bool
		robots bool
		want   string
	}{
		{name: "relative source", source: "feed.xml", want: "absolute HTTP or HTTPS URL"},
		{name: "file source", source: "file:///feed.xml", want: "absolute HTTP or HTTPS URL"},
		{name: "missing host", source: "https:///feed.xml", want: "absolute HTTP or HTTPS URL"},
		{name: "credentials", source: "https://user:secret@127.0.0.1:1/feed", want: "embedded credentials"},
		{name: "HTTP failure", status: http.StatusNotFound, want: "HTTP status 404"},
		{name: "invalid content", body: `<html><body>Not a feed</body></html>`, want: "parse feed"},
		{name: "malformed XML", body: `<rss version="2.0"><channel><item><link>`, want: "parse feed"},
		{name: "empty feed", body: `<rss version="2.0"><channel><title>Empty</title></channel></rss>`, want: "no article URLs"},
		{name: "oversized feed", body: `<rss version="2.0"><channel/></rss>` + strings.Repeat(" ", 16<<20), want: "16 MiB"},
		{name: "canceled", cancel: true, want: "context canceled"},
		{name: "robots disallow feed", robots: true, want: "disallowed by robots.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldCfg, oldDB := cfg, model.DB
			t.Cleanup(func() { cfg, model.DB = oldCfg, oldDB })
			cfg = testutil.Config(t)
			cfg.Crawler.NoRobots = !tc.robots
			model.DB = nil
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.robots {
					if r.URL.Path != "/robots.txt" {
						t.Errorf("robots disallowed feed fetched: %s", r.URL)
					}
					_, _ = io.WriteString(w, "User-agent: *\nDisallow: /feed\n")
					return
				}
				if r.URL.Path != "/feed" {
					t.Errorf("unexpected request: %s", r.URL)
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			source := tc.source
			if source == "" {
				source = server.URL + "/feed"
			}
			command := &cobra.Command{Use: importRSSCmd.Use, Args: importRSSCmd.Args, RunE: importRSSCmd.RunE}
			addURLInputFlags(command)
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			command.SetArgs([]string{source})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			err := command.ExecuteContext(ctx)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want %q", err, tc.want)
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Errorf("cancellation not preserved: %v", err)
			}
			if model.DB != nil {
				t.Error("invalid feed opened the Hister database")
				db, dbErr := model.DB.DB()
				if dbErr != nil {
					t.Fatal(dbErr)
				}
				_ = db.Close()
			}
		})
	}
}

func TestImportZIMCommandRegistered(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"import", "zim"})
	if err != nil || command.Name() != "zim" {
		t.Fatalf("ZIM import command unavailable: command=%s, error=%v", command.Name(), err)
	}
	for _, name := range []string{"base-url", "skip-existing", "allow-sensitive"} {
		if command.Flags().Lookup(name) == nil {
			t.Errorf("ZIM import is missing --%s", name)
		}
	}
	for _, name := range []string{"url-map", "collection", "python"} {
		if command.Flags().Lookup(name) != nil {
			t.Errorf("obsolete --%s flag is still registered", name)
		}
	}
}

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestPDFImportParserProcess(t *testing.T) {
	if !slices.Contains(os.Args, "--pdf-import-parser-test") {
		return
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil || string(data) != "%PDF-1.4 client snapshot" {
		os.Exit(2)
	}
	if _, err := fmt.Fprint(os.Stdout, "Text extracted on the import client"); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestImportFileUsesConfiguredPDFParser(t *testing.T) {
	var received document.Document
	newExportTestCommand(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/add" {
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			return nil, err
		}
		return jsonHTTPResponse(r, http.StatusCreated, ""), nil
	})
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Allow for the race detector's one second shutdown delay in the helper process.
	cfg.Indexer.PDFParser = config.PDFParser{
		Command: executable,
		Args:    []string{"-test.run=^TestPDFImportParserProcess$", "--", "--pdf-import-parser-test"},
		Timeout: 10,
	}
	path := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4 client snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := &cobra.Command{Use: importFileCmd.Use, Args: importFileCmd.Args, RunE: importFileCmd.RunE}
	command.Flags().Bool("skip-existing", false, "")
	command.Flags().Bool("watch", false, "")
	command.Flags().Bool("global", false, "")
	command.Flags().String("source", "laptop", "")
	command.Flags().String("format", "text", "")
	command.Flags().Int("batch-size", 10, "")
	command.Flags().String("start-date", "", "")
	command.Flags().String("end-date", "", "")
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{path})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if received.Text != "Text extracted on the import client" || received.Type != document.RemoteFile || received.Metadata["type"] != "pdf" {
		t.Fatalf("PDF was not submitted as a prepared snapshot: %#v", received)
	}
}

func newExportTestCommand(t *testing.T, transport roundTripFunc) *cobra.Command {
	t.Helper()
	previousConfig, previousTransport := cfg, http.DefaultTransport
	t.Cleanup(func() { cfg, http.DefaultTransport = previousConfig, previousTransport })
	cfg = config.CreateDefaultConfig()
	if err := cfg.UpdateBaseURL("https://hister.test"); err != nil {
		t.Fatal(err)
	}
	http.DefaultTransport = transport
	command := &cobra.Command{Use: exportCmd.Use, Args: exportCmd.Args, Run: exportCmd.Run, RunE: exportCmd.RunE}
	command.Flags().String("start-date", "", "")
	command.Flags().String("end-date", "", "")
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	return command
}

func TestExportPublishesOnlyAfterSuccess(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing=%t", existing), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "backup.json")
			const original = "previous backup\n"
			if existing {
				if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			documents := []*client.Document{
				{URL: "https://example.com/first", Text: strings.Repeat("stored text ", 600), HTML: "<p>Stored HTML</p>", Processed: true},
				{URL: "https://example.com/last", Title: "Last document", Label: "reading", Metadata: map[string]any{"source": "test"}},
			}
			calls := 0
			command := newExportTestCommand(t, func(r *http.Request) (*http.Response, error) {
				data, err := os.ReadFile(path)
				if existing {
					if err != nil || string(data) != original {
						t.Errorf("destination changed before export completed: length = %d, error = %v", len(data), err)
					}
				} else if !os.IsNotExist(err) {
					t.Errorf("destination published before export completed: error = %v", err)
				}
				var query client.SearchQuery
				if err := json.Unmarshal([]byte(r.URL.Query().Get("query")), &query); err != nil {
					t.Fatal(err)
				}
				pageKey := ""
				if calls > 0 {
					pageKey = "next"
				}
				if query.Text != "label:reading" || !query.IncludeText || !query.IncludeHTML || query.PageKey != pageKey {
					t.Errorf("unexpected export query: %+v", query)
				}
				if calls >= len(documents) {
					t.Fatal("requested an unexpected export page")
				}
				result := client.SearchResults{Documents: documents[calls : calls+1]}
				calls++
				if calls < len(documents) {
					result.PageKey = "next"
				}
				return jsonHTTPResponse(r, http.StatusOK, string(contractJSON(t, result))), nil
			})
			command.SetArgs([]string{path, "label:reading"})
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			assertContractJSON(t, data, contractJSON(t, documents))
			reader := newJSONExportReader(bytes.NewReader(data), maxJSONExportLineSize)
			for range documents {
				if _, err := reader.nextLine(); err != nil {
					t.Fatalf("export cannot be imported: %v", err)
				}
			}
			if _, err := reader.nextLine(); err != io.EOF {
				t.Errorf("export ending = %v, want EOF", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != "backup.json" {
				t.Errorf("unexpected files after export: %v, error = %v", entries, err)
			}
		})
	}
}

func TestExportReportsFinalFlushFailure(t *testing.T) {
	out, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("requires a device that rejects writes: %v", err)
	}
	t.Cleanup(func() {
		if err := out.Close(); err != nil {
			t.Error(err)
		}
	})
	command := newExportTestCommand(t, func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(r, http.StatusOK, `{"documents":[]}`), nil
	})
	command.SetOut(out)
	command.SetArgs([]string{"-"})
	if err := command.Execute(); !errors.Is(err, syscall.ENOSPC) || ExitCode(err) != 1 {
		t.Errorf("export returned %v after the final write failed", err)
	}
}

func TestExportFailuresPreserveDestination(t *testing.T) {
	for _, failure := range []string{"first request", "later request", "invalid response", "server error", "empty document"} {
		for _, existing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing=%t", failure, existing), func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "backup.json")
				const original = "previous backup\n"
				if existing {
					if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				calls := 0
				command := newExportTestCommand(t, func(r *http.Request) (*http.Response, error) {
					calls++
					if failure == "invalid response" {
						return jsonHTTPResponse(r, http.StatusOK, `null`), nil
					}
					if failure == "server error" {
						return jsonHTTPResponse(r, http.StatusOK, `{"error":"search failed"}`), nil
					}
					if failure == "empty document" {
						return jsonHTTPResponse(r, http.StatusOK, `{"documents":[null]}`), nil
					}
					if failure == "later request" && calls == 1 {
						result := client.SearchResults{Documents: []*client.Document{{URL: "https://example.com", Text: strings.Repeat("content ", 1000)}}, PageKey: "next"}
						return jsonHTTPResponse(r, http.StatusOK, string(contractJSON(t, result))), nil
					}
					return jsonHTTPResponse(r, http.StatusServiceUnavailable, "server unavailable"), nil
				})
				var summary bytes.Buffer
				command.SetOut(&summary)
				command.SetArgs([]string{path})
				if err := command.Execute(); err == nil || ExitCode(err) != 1 {
					t.Errorf("failed export returned %v", err)
				}
				if summary.Len() != 0 {
					t.Errorf("failed export printed success output: %q", summary.String())
				}
				entries, err := os.ReadDir(dir)
				if err != nil {
					t.Fatal(err)
				}
				if existing {
					data, err := os.ReadFile(path)
					if err != nil || string(data) != original || len(entries) != 1 {
						t.Errorf("failed export did not preserve the destination: content = %q, files = %v, error = %v", data, entries, err)
					}
				} else if len(entries) != 0 {
					t.Errorf("failed export left files behind: %v", entries)
				}
			})
		}
	}
}

func TestExportStdout(t *testing.T) {
	for _, documents := range [][]*client.Document{{}, {{URL: "https://example.com", Text: "stored content"}}} {
		t.Run(fmt.Sprintf("documents=%d", len(documents)), func(t *testing.T) {
			command := newExportTestCommand(t, func(r *http.Request) (*http.Response, error) {
				return jsonHTTPResponse(r, http.StatusOK, string(contractJSON(t, client.SearchResults{Documents: documents}))), nil
			})
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetArgs([]string{"-"})
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			assertContractJSON(t, output.Bytes(), contractJSON(t, documents))
		})
	}
}

func TestExportRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.json")
	command := newExportTestCommand(t, func(r *http.Request) (*http.Response, error) {
		// Simulate another process creating a directory at the destination after
		// the export has started writing its temporary file.
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		return jsonHTTPResponse(r, http.StatusOK, `{"documents":[]}`), nil
	})
	var summary bytes.Buffer
	command.SetOut(&summary)
	command.SetArgs([]string{path})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "replace export") {
		t.Errorf("rename failure returned %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "backup.json" || !entries[0].IsDir() {
		t.Errorf("rename failure left unexpected files: %v, error = %v", entries, err)
	}
	if summary.Len() != 0 {
		t.Errorf("rename failure printed success output: %q", summary.String())
	}
}

func TestExportRejectsNonRegularDestinations(t *testing.T) {
	for _, kind := range []string{"directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "backup.json")
			if kind == "directory" {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				target := filepath.Join(dir, "target.json")
				if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			command := newExportTestCommand(t, func(*http.Request) (*http.Response, error) {
				t.Error("contacted server for an unsupported destination")
				return nil, errors.New("unexpected request")
			})
			command.SetArgs([]string{path})
			if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "regular file") {
				t.Errorf("unsupported destination returned %v", err)
			}
			if kind == "symlink" {
				if _, err := os.Readlink(path); err != nil {
					t.Errorf("destination symlink replaced: %v", err)
				}
				if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
					t.Errorf("symlink target changed: content = %q, error = %v", data, err)
				}
			}
		})
	}
}

func TestExpandImportInputsExpandsDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "input")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"b.html",
		"a.json",
		"c.7z",
		"d.htm",
		"notes.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subdir", "nested.html"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}

	first := filepath.Join(root, "first.json")
	last := filepath.Join(root, "last")
	if err := os.WriteFile(first, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(last, []byte("last"), 0o600); err != nil {
		t.Fatal(err)
	}

	inputs, err := expandImportInputs([]string{first, dir, last}, nil)
	if err != nil {
		t.Fatal(err)
	}

	want := []importFileInput{
		{Path: first},
		{Path: filepath.Join(dir, "a.json")},
		{Path: filepath.Join(dir, "b.html")},
		{Path: filepath.Join(dir, "c.7z")},
		{Path: filepath.Join(dir, "d.htm")},
		{Path: filepath.Join(dir, "notes.txt")},
		{Path: filepath.Join(dir, "subdir", "nested.html")},
		{Path: last},
	}
	if !reflect.DeepEqual(inputs, want) {
		t.Fatalf("expandImportInputs() = %#v, want %#v", inputs, want)
	}
}

func TestExpandImportInputsSkipsBrokenSymlink(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.txt")
	last := filepath.Join(dir, "z.txt")
	for _, path := range []string{first, last} {
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "missing.txt"), filepath.Join(dir, "b.txt")); err != nil {
		t.Skipf("cannot create symbolic link: %v", err)
	}

	inputs, err := expandImportInputs([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}

	want := []importFileInput{{Path: first}, {Path: last}}
	if !reflect.DeepEqual(inputs, want) {
		t.Fatalf("expandImportInputs() = %#v, want %#v", inputs, want)
	}
}

func TestRemoteFileURL(t *testing.T) {
	inputFile := filepath.Join(t.TempDir(), "My note.md")
	got, err := remoteFileURL("Alice's Laptop", inputFile)
	if err != nil {
		t.Fatalf("remoteFileURL() unexpected error: %v", err)
	}
	want := "remote-file://alice-s-laptop/" + strings.TrimPrefix(filepath.ToSlash(filepath.Dir(inputFile)), "/") + "/My%20note.md"
	if got != want {
		t.Fatalf("remoteFileURL() = %q, want %q", got, want)
	}
}

func TestNormalizeRemoteFileSourceRejectsEmptyName(t *testing.T) {
	if _, err := normalizeRemoteFileSource("___"); err == nil {
		t.Fatal("normalizeRemoteFileSource() accepted a source with no usable characters")
	}
}

func TestImportHTMLFileWithoutSourceURLUsesAddEndpoint(t *testing.T) {
	var received document.Document
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/add" {
			t.Errorf("request path = %q, want /api/add", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			return nil, fmt.Errorf("decode request: %w", err)
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	})}

	inputFile := filepath.Join(t.TempDir(), "Saved page.html")
	const content = `<html><head><title>Saved page</title><style>style noise</style></head><body><p>Saved page content</p><script>script noise</script></body></html>`
	if err := os.WriteFile(inputFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	importer := newFileImporter(
		client.New("http://hister.test", client.WithHTTPClient(httpClient), client.WithMaxBatchBodyBytes(40<<20)),
		config.Indexer{},
	)
	imported, skipped, errCount := importer.importHTMLFile(
		context.Background(),
		importFileInput{Path: inputFile},
		"laptop",
		1<<20,
		false,
		documentLabelOverride{},
	)
	if imported != 1 || skipped != 0 || errCount != 0 {
		t.Fatalf("importHTMLFile() = (%d, %d, %d), want (1, 0, 0)", imported, skipped, errCount)
	}
	wantURL, err := remoteFileURL("laptop", inputFile)
	if err != nil {
		t.Fatal(err)
	}
	if received.URL != wantURL || received.Type != document.RemoteFile {
		t.Fatalf("imported document = %#v, want remote snapshot %q", received, wantURL)
	}
	if strings.TrimSpace(received.Text) != "Saved page content" || received.Title != "Saved page" || received.HTML != content {
		t.Fatalf("unexpected extracted HTML snapshot: %#v", received)
	}
}

func TestImportFileSizeLimit(t *testing.T) {
	for _, tc := range []struct {
		name, content string
	}{
		{name: "note.txt", content: "plain text"},
		{name: "page.html", content: `<html><head><link rel="canonical" href="https://example.com/page"></head><body>Saved page</body></html>`},
		{name: "snapshot.html", content: `<html><title>Notes</title><body>Saved notes</body></html>`},
	} {
		for _, oversized := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/oversized=%t", tc.name, oversized), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), tc.name)
				if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
				requests := 0
				c := client.New("http://hister.test", client.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					requests++
					if r.URL.Path == "/api/batch" {
						return jsonHTTPResponse(r, http.StatusOK, `{"results":[{"status":201}]}`), nil
					}
					return jsonHTTPResponse(r, http.StatusCreated, ""), nil
				})}), client.WithMaxBatchBodyBytes(40<<20))
				limit := int64(len(tc.content))
				if oversized {
					limit--
				}
				imported, skipped, failed := newFileImporter(c, config.Indexer{}).importFile(context.Background(), importFileInput{Path: path}, fileImportOptions{
					Source: "laptop", MaxFileSize: limit,
				})
				if oversized {
					if imported != 0 || skipped != 0 || failed != 1 || requests != 0 {
						t.Fatalf("oversized file: imported=%d skipped=%d failed=%d requests=%d", imported, skipped, failed, requests)
					}
				} else if imported != 1 || skipped != 0 || failed != 0 || requests != 1 {
					t.Fatalf("file at size limit: imported=%d skipped=%d failed=%d requests=%d", imported, skipped, failed, requests)
				}
			})
		}
	}
}

func TestIsHisterJSONExportBoundsInspection(t *testing.T) {
	const maxProbe = maxJSONExportLineSize + 2
	for _, prefix := range []string{"", `[{"text":"`} {
		t.Run(fmt.Sprintf("prefix=%q", prefix), func(t *testing.T) {
			reader := io.MultiReader(strings.NewReader(prefix), io.LimitReader(exportProbePadding{}, maxProbe+4096))
			counted := &countingExportProbe{Reader: reader}
			if isHisterJSONExportReader(counted) {
				t.Fatal("incomplete oversized input was recognized as an export")
			}
			if counted.bytes > maxProbe {
				t.Fatalf("inspection read %d bytes, want at most %d", counted.bytes, maxProbe)
			}
		})
	}
}

type exportProbePadding struct{}

func (exportProbePadding) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

type countingExportProbe struct {
	io.Reader
	bytes int
}

func (r *countingExportProbe) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.bytes += n
	return n, err
}

func TestExpandImportInputsUsesWatchedDirectoryRules(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"keep.md":    "keep",
		"skip.txt":   "skip",
		"draft.md":   "draft",
		".hidden.md": "hidden",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	directory := &config.Directory{Path: root, Label: "watched", Filetypes: []string{"md"}, Excludes: []string{"draft*"}}

	inputs, err := expandImportInputs(nil, []*config.Directory{directory})
	if err != nil {
		t.Fatal(err)
	}
	want := []importFileInput{{Path: filepath.Join(root, "keep.md"), Label: "watched"}}
	if !reflect.DeepEqual(inputs, want) {
		t.Fatalf("expandImportInputs() = %#v, want %#v", inputs, want)
	}
}

func TestIsHisterJSONExport(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "export", content: "[\n{\"url\":\"https://example.com\",\"title\":\"Example\",\"type\":0,\"processed\":true}\n]\n", want: true},
		{name: "empty export", content: "[]", want: true},
		{name: "ordinary object", content: "{\"name\":\"settings\"}", want: false},
		{name: "ordinary array", content: "[1,2,3]", want: false},
		{name: "ordinary URL array", content: "[{\"url\":\"https://example.com\"}]", want: false},
		{name: "plain text", content: "Notes for tomorrow", want: false},
		{name: "markdown link", content: "[Hister](https://hister.org)", want: false},
		{name: "binary", content: "\xff\x00", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(tt.name, " ", "_")+".json")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := isHisterJSONExport(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("isHisterJSONExport() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestImportJSONFileUsesConfiguredBatchSize(t *testing.T) {
	var batchSizes []int
	var receivedLabels []string
	var receivedMetadata map[string]any
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/batch" {
			t.Errorf("request path = %q, want /api/batch", r.URL.Path)
		}
		var req struct {
			Ops []struct {
				Op        string         `json:"op"`
				Label     string         `json:"label"`
				Metadata  map[string]any `json:"metadata"`
				Processed bool           `json:"processed"`
			} `json:"ops"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, fmt.Errorf("decode request: %w", err)
		}
		batchSizes = append(batchSizes, len(req.Ops))
		for _, op := range req.Ops {
			receivedLabels = append(receivedLabels, op.Label)
		}
		if len(batchSizes) == 1 && len(req.Ops) > 0 {
			receivedMetadata = req.Ops[0].Metadata
		}
		for _, op := range req.Ops {
			if !op.Processed {
				t.Error("imported JSON document was not marked as processed")
			}
		}
		results := make([]map[string]any, len(req.Ops))
		for i, op := range req.Ops {
			if op.Op != "add" {
				t.Errorf("operation = %q, want add", op.Op)
			}
			results[i] = map[string]any{"status": http.StatusCreated}
		}
		var response bytes.Buffer
		if err := json.NewEncoder(&response).Encode(map[string]any{"results": results}); err != nil {
			return nil, fmt.Errorf("encode response: %w", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(response.Bytes())),
			Request:    r,
		}, nil
	})}

	var input strings.Builder
	for i := range 23 {
		doc := &document.Document{
			URL:   fmt.Sprintf("https://example.com/%d", i),
			Title: fmt.Sprintf("Document %d", i),
		}
		if i == 0 {
			doc.Label = "reference"
			doc.Metadata = map[string]any{"source": "export"}
		}
		line, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		input.Write(line)
		input.WriteByte('\n')
	}
	inputFile := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(inputFile, []byte(input.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	imported, skipped, errCount := importJSONFile(
		client.New("http://hister.test", client.WithHTTPClient(httpClient), client.WithMaxBatchBodyBytes(40<<20)),
		inputFile,
		false,
		0,
		0,
		10,
		documentLabelOverride{},
	)
	if imported != 23 || skipped != 0 || errCount != 0 {
		t.Fatalf("importJSONFile() = (%d, %d, %d), want (23, 0, 0)", imported, skipped, errCount)
	}
	if want := []int{10, 10, 3}; !reflect.DeepEqual(batchSizes, want) {
		t.Fatalf("batch sizes = %v, want %v", batchSizes, want)
	}
	if len(receivedLabels) != 23 {
		t.Fatalf("received %d labels, want 23", len(receivedLabels))
	}
	if receivedLabels[0] != "reference" {
		t.Fatalf("stored label = %q, want reference", receivedLabels[0])
	}
	for i, label := range receivedLabels[1:] {
		if label != "import" {
			t.Fatalf("label %d = %q, want import", i+1, label)
		}
	}
	if receivedMetadata["source"] != "export" {
		t.Fatalf("metadata source = %v, want export", receivedMetadata["source"])
	}
}

func TestImportJSONFileReportsInvalidInput(t *testing.T) {
	const doc = `{"url":"https://example.com","type":0,"processed":true,"added":1}`
	for _, tc := range []struct {
		name, input       string
		startDate         int64
		imported, skipped int
		errors, errorLine int
	}{
		{name: "compact export", input: "[" + doc + "]", errors: 1, errorLine: 1},
		{name: "indented export", input: "[\n  " + doc + "\n]", errors: 1, errorLine: 2},
		{name: "malformed document between valid documents", input: "[\n" + doc + "\n,\n{\"url\":}\n,\n" + doc + "\n]", imported: 2, errors: 1, errorLine: 4},
		{name: "truncated export", input: "[\n" + doc + "\n", imported: 1, errors: 1, errorLine: 2},
		{name: "empty export", input: "[\n]\n"},
		{name: "all documents filtered", input: "[\n" + doc + "\n]", startDate: 2, skipped: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			oldLogger := log.Logger
			log.Logger = zerolog.New(&logs)
			t.Cleanup(func() { log.Logger = oldLogger })
			inputFile := filepath.Join(t.TempDir(), "backup.json")
			if err := os.WriteFile(inputFile, []byte(tc.input), 0o600); err != nil {
				t.Fatal(err)
			}
			submitted := 0
			c := client.New("http://hister.test", client.WithMaxBatchBodyBytes(40<<20), client.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/api/batch" {
					t.Fatalf("unexpected request to %s", r.URL.Path)
				}
				var req struct {
					Ops []json.RawMessage `json:"ops"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					return nil, err
				}
				submitted += len(req.Ops)
				results := make([]map[string]int, len(req.Ops))
				for i := range results {
					results[i] = map[string]int{"status": http.StatusCreated}
				}
				body, err := json.Marshal(map[string]any{"results": results})
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
			})}))
			imported, skipped, errCount := importJSONFile(c, inputFile, false, tc.startDate, 0, 10, documentLabelOverride{})
			if imported != tc.imported || skipped != tc.skipped || errCount != tc.errors || submitted != tc.imported {
				t.Fatalf("result = (%d, %d, %d), submitted %d, want (%d, %d, %d)", imported, skipped, errCount, submitted, tc.imported, tc.skipped, tc.errors)
			}
			if tc.errors > 0 {
				var entry struct {
					File  string `json:"file"`
					Line  int    `json:"line"`
					Error string `json:"error"`
				}
				if err := json.NewDecoder(&logs).Decode(&entry); err != nil {
					t.Fatal(err)
				}
				if entry.File != inputFile || entry.Line != tc.errorLine || entry.Error == "" {
					t.Fatalf("error lacks file, line or cause: %#v", entry)
				}
			}
		})
	}
}

func TestImportBatchSizeDefault(t *testing.T) {
	batchSize, err := importFileCmd.Flags().GetInt("batch-size")
	if err != nil {
		t.Fatal(err)
	}
	if batchSize != 10 {
		t.Fatalf("batch size default = %d, want 10", batchSize)
	}
}

func TestDocumentLabelOverride(t *testing.T) {
	d := &document.Document{Label: "exported"}
	documentLabelOverride{}.apply(d, "import")
	if d.Label != "exported" {
		t.Fatalf("unset override changed label to %q", d.Label)
	}

	d.Label = ""
	documentLabelOverride{}.apply(d, "import")
	if d.Label != "import" {
		t.Fatalf("fallback label = %q, want import", d.Label)
	}

	override := documentLabelOverride{value: "imported", set: true}
	override.apply(d, "import")
	if d.Label != "imported" {
		t.Fatalf("label = %q, want imported", d.Label)
	}
	if got := (documentLabelOverride{}).resolve("stored", "browser"); got != "stored" {
		t.Fatalf("resolved stored label = %q, want stored", got)
	}
	if got := (documentLabelOverride{}).resolve("", "browser"); got != "browser" {
		t.Fatalf("resolved fallback label = %q, want browser", got)
	}
	if got := override.resolve("stored", "browser"); got != "imported" {
		t.Fatalf("resolved override label = %q, want imported", got)
	}
}

func TestDocumentLabelOverrideReadsInheritedFlag(t *testing.T) {
	parent := &cobra.Command{Use: "import"}
	child := &cobra.Command{Use: "source"}
	parent.AddCommand(child)
	parent.PersistentFlags().String("label", "", "")
	if err := child.ParseFlags([]string{"--label", "reading"}); err != nil {
		t.Fatal(err)
	}

	override := newDocumentLabelOverride(child)
	if !override.set || override.value != "reading" {
		t.Fatalf("label override = %+v, want explicitly set reading label", override)
	}
}

func TestSubmissionOverrideInheritedByImports(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			parent := &cobra.Command{Use: "import"}
			parent.PersistentFlags().Bool("ignore-rules", false, ignoreRulesFlagUsage)
			child := &cobra.Command{Use: "source"}
			parent.AddCommand(child)
			if err := child.ParseFlags([]string{"--ignore-rules=" + strconv.FormatBool(enabled)}); err != nil {
				t.Fatal(err)
			}
			var received document.Document
			opts := documentSubmissionClientOptions(child)
			opts = append(opts, client.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					return nil, err
				}
				return jsonHTTPResponse(r, http.StatusCreated, ""), nil
			})}))
			c := client.New("http://hister.test", opts...)
			if err := c.AddDocumentJSON(&client.Document{URL: "https://example.com"}); err != nil {
				t.Fatal(err)
			}
			if received.IgnoreSkipRules() != enabled {
				t.Errorf("saved override = %v, want %v", received.IgnoreSkipRules(), enabled)
			}
			if received.SkipSensitiveCheck {
				t.Error("URL rule override unexpectedly disabled sensitive content checks")
			}
		})
	}
}

func TestImportCommandHierarchy(t *testing.T) {
	tests := map[string]*cobra.Command{
		"file":       importFileCmd,
		"sitemap":    importSitemapCmd,
		"rss":        importRSSCmd,
		"browser":    importBrowserCmd,
		"linkding":   importLinkdingCmd,
		"linkwarden": importLinkwardenCmd,
		"karakeep":   importKarakeepCmd,
		"raindrop":   importRaindropCmd,
		"readeck":    importReadeckCmd,
		"readwise":   importReadwiseCmd,
		"shaarli":    importShaarliCmd,
		"wallabag":   importWallabagCmd,
	}
	for name, want := range tests {
		got, _, err := importCmd.Find([]string{name})
		if err != nil {
			t.Fatalf("import %s command lookup failed: %v", name, err)
		}
		if got != want {
			t.Fatalf("import %s command = %q, want %q", name, got.Name(), want.Name())
		}
	}
	nested := map[string]*cobra.Command{
		"bookmarks": importBookmarksCmd,
		"history":   importBrowserHistoryCmd,
	}
	for name, want := range nested {
		got, _, err := importCmd.Find([]string{"browser", name})
		if err != nil {
			t.Fatalf("import browser %s lookup failed: %v", name, err)
		}
		if got != want {
			t.Fatalf("import browser %s = %q, want %q", name, got.Name(), want.Name())
		}
	}
	for _, cmd := range importCmd.Commands() {
		if cmd == importBookmarksCmd || cmd == importBrowserHistoryCmd {
			t.Fatal("browser import subcommands should be nested under import browser")
		}
	}
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "import-browser" {
			t.Fatal("legacy import-browser command remains registered at the root")
		}
	}
}

func TestImportSubcommandFlagOwnership(t *testing.T) {
	if indexCmd.Flags().Lookup("ignore-rules") == nil {
		t.Error("index is missing --ignore-rules")
	}
	if indexCmd.Flags().Lookup("proxy") == nil {
		t.Error("index is missing --proxy")
	}
	if importCmd.PersistentFlags().Lookup("label") == nil {
		t.Fatal("import is missing --label")
	}
	for _, importCommand := range []*cobra.Command{importFileCmd, importBrowserCmd, importBrowserHistoryCmd, importBookmarksCmd, importLinkdingCmd, importLinkwardenCmd, importKarakeepCmd, importRaindropCmd, importReadeckCmd, importReadwiseCmd, importShaarliCmd, importWallabagCmd} {
		if importCommand.InheritedFlags().Lookup("ignore-rules") == nil {
			t.Errorf("import %s does not inherit --ignore-rules", importCommand.Name())
		}
		if importCommand.InheritedFlags().Lookup("label") == nil {
			t.Errorf("import %s does not inherit --label", importCommand.Name())
		}
	}
	for _, name := range []string{"batch-size", "start-date", "end-date", "skip-existing", "global", "user-id"} {
		for _, importCommand := range []*cobra.Command{importFileCmd, importLinkdingCmd, importLinkwardenCmd, importKarakeepCmd, importRaindropCmd, importReadeckCmd, importReadwiseCmd, importShaarliCmd, importWallabagCmd} {
			if importCommand.Flags().Lookup(name) == nil {
				t.Errorf("import %s is missing --%s", importCommand.Name(), name)
			}
		}
		if name != "start-date" && importBrowserCmd.Flags().Lookup(name) != nil {
			t.Errorf("import browser unexpectedly has --%s", name)
		}
		if importBookmarksCmd.Flags().Lookup(name) != nil {
			t.Errorf("import bookmarks unexpectedly has --%s", name)
		}
		if name != "start-date" && importBrowserHistoryCmd.Flags().Lookup(name) != nil {
			t.Errorf("import browser history unexpectedly has --%s", name)
		}
	}
	if importBrowserCmd.Flags().Lookup("start-date") == nil {
		t.Error("import browser is missing --start-date")
	}
	if importBrowserCmd.Flags().Lookup("min-visit") == nil {
		t.Error("import browser is missing --min-visit")
	}
	if importBookmarksCmd.Flags().Lookup("start-date") != nil {
		t.Error("import bookmarks unexpectedly has --start-date")
	}
	if importBookmarksCmd.Flags().Lookup("min-visit") != nil {
		t.Error("import bookmarks unexpectedly has --min-visit")
	}
	if importBookmarksCmd.Flags().Lookup("browser") == nil {
		t.Error("import bookmarks is missing --browser")
	}
	if importBookmarksCmd.Flags().Lookup("db") == nil {
		t.Error("import bookmarks is missing --db")
	}
	if importBrowserHistoryCmd.Flags().Lookup("start-date") == nil {
		t.Error("import browser history is missing --start-date")
	}
	if importBrowserHistoryCmd.Flags().Lookup("min-visit") == nil {
		t.Error("import browser history is missing --min-visit")
	}
	if importBrowserHistoryCmd.Flags().Lookup("browser") == nil {
		t.Error("import browser history is missing --browser")
	}
	if importBrowserHistoryCmd.Flags().Lookup("db") == nil {
		t.Error("import browser history is missing --db")
	}
	for _, name := range []string{"source", "skip-existing", "global", "user-id", "allow-sensitive", "watch"} {
		if importFileCmd.Flags().Lookup(name) == nil {
			t.Errorf("import file is missing --%s", name)
		}
	}
	for _, name := range []string{"backend", "backend-option", "proxy", "header", "cookie"} {
		for _, importCommand := range []*cobra.Command{importBrowserCmd, importBrowserHistoryCmd, importBookmarksCmd, importLinkdingCmd, importLinkwardenCmd, importKarakeepCmd, importRaindropCmd, importReadeckCmd, importReadwiseCmd, importShaarliCmd} {
			if importCommand.Flags().Lookup(name) == nil {
				t.Errorf("import %s is missing --%s", importCommand.Name(), name)
			}
		}
		if importFileCmd.Flags().Lookup(name) != nil {
			t.Errorf("import file unexpectedly has --%s", name)
		}
	}
	for _, importCommand := range []*cobra.Command{importLinkdingCmd, importLinkwardenCmd, importKarakeepCmd, importRaindropCmd, importReadeckCmd, importReadwiseCmd, importShaarliCmd} {
		if importCommand.Flags().Lookup("api-token") == nil {
			t.Errorf("import %s is missing --api-token", importCommand.Name())
		}
		if importCommand.Flags().Lookup("source-token") != nil {
			t.Errorf("import %s has the old --source-token flag", importCommand.Name())
		}
	}
	for _, importCommand := range []*cobra.Command{importFileCmd, importBrowserCmd} {
		if importCommand.Flags().Lookup("api-token") != nil {
			t.Errorf("import %s unexpectedly has --api-token", importCommand.Name())
		}
	}
}
