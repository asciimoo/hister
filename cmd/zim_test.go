// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/nogon/server/document"

	"github.com/PuerkitoBio/goquery"
	"github.com/spf13/cobra"
)

func zimTestCommand(t *testing.T, flags ...string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer, *[]document.Document) {
	t.Helper()
	var received []document.Document
	newExportTestCommand(t, func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/config":
			return jsonHTTPResponse(r, http.StatusOK, `{}`), nil
		case "/api/batch":
			var batch struct {
				Operations []document.Document `json:"ops"`
			}
			if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
				t.Fatal(err)
			}
			received = append(received, batch.Operations...)
			results := make([]map[string]any, len(batch.Operations))
			for i := range results {
				results[i] = map[string]any{"status": 201}
			}
			data, _ := json.Marshal(map[string]any{"results": results})
			return jsonHTTPResponse(r, http.StatusOK, string(data)), nil
		default:
			t.Errorf("unexpected request: %s", r.URL)
			return jsonHTTPResponse(r, http.StatusNotFound, ""), nil
		}
	})
	cfg.Indexer.DetectLanguages = false
	command := &cobra.Command{Use: importZIMCmd.Use, RunE: importZIMCmd.RunE, Args: importZIMCmd.Args}
	command.SetContext(t.Context())
	addZIMImportFlags(command)
	command.Flags().String("label", "", "")
	command.Flags().Bool("ignore-rules", false, "")
	if err := command.ParseFlags(flags); err != nil {
		t.Fatal(err)
	}
	var output, warnings bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&warnings)
	return command, &output, &warnings, &received
}

func TestZIMImportSkipsMissingURLs(t *testing.T) {
	filename := filepath.Join("testdata", "zim", "canonical-snapshot.zim")
	command, output, warnings, received := zimTestCommand(t, "--label=kiwix-test", "--format=json")
	if err := command.RunE(command, []string{filename}); err != nil {
		t.Fatal(err)
	}
	if len(*received) != 1 {
		t.Fatalf("submitted %d documents, want only the identified article", len(*received))
	}
	d := (*received)[0]
	if d.URL != "https://example.com/article" || !strings.Contains(d.HTML, "Archived article") || d.Label != "kiwix-test" || d.Processed || d.Type == document.RemoteFile {
		t.Errorf("unexpected document: %+v", d)
	}
	if len(d.Metadata) != 2 || d.Metadata["source"] != "kiwix" || d.Metadata["zim_collection"] != "example_collection" {
		t.Errorf("import metadata should contain only source and collection: %+v", d.Metadata)
	}
	if d.Added != time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Unix() {
		t.Errorf("archive publication date was not retained in Added: %d", d.Added)
	}
	for _, want := range []string{"missing.html", "original URL", "Skipping"} {
		if !strings.Contains(warnings.String(), want) {
			t.Errorf("missing warning %q: %s", want, warnings)
		}
	}
	var summaries []map[string]int
	if err := json.Unmarshal(output.Bytes(), &summaries); err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0]["imported"] != 1 || summaries[0]["skipped"] == 0 || summaries[0]["errors"] != 0 {
		t.Errorf("unexpected summary: %v", summaries)
	}
}

func TestZIMImportResolvesPageAndArticleURLs(t *testing.T) {
	command, _, warnings, received := zimTestCommand(t)
	if err := command.RunE(command, []string{filepath.Join("testdata", "zim", "relative-urls.zim")}); err != nil {
		t.Fatal(err)
	}
	byURL := make(map[string]document.Document)
	for _, d := range *received {
		byURL[d.URL] = d
	}
	want := []string{"https://example.org/articles/start", "https://docs.example.net/guides/target?edition=2", "https://example.org/reference/root", "https://example.org/social", "https://original.example/plain.txt"}
	if len(byURL) != len(want) {
		t.Errorf("imported URLs = %v, want %v", keysForZIMTest(byURL), want)
	}
	for _, u := range want {
		if _, ok := byURL[u]; !ok {
			t.Errorf("missing document %s", u)
		}
	}
	page, err := goquery.NewDocumentFromReader(strings.NewReader(byURL[want[0]].HTML))
	if err != nil {
		t.Fatal(err)
	}
	for id, href := range map[string]string{
		"forward": "https://docs.example.net/guides/target?edition=2#details",
		"alias":   "https://docs.example.net/guides/target?edition=2",
		"outside": "https://outside.example/path", "local": "#section", "network": "https://other.example/path",
	} {
		if got := page.Find("#"+id).AttrOr("href", ""); got != href {
			t.Errorf("link %s = %q, want %q", id, got, href)
		}
	}
	if _, ok := page.Find("#missing").Attr("href"); ok {
		t.Error("unidentified archive link must not become a broken link into Nogon")
	}
	for _, path := range []string{"missing.html", "ambiguous.html"} {
		if !strings.Contains(warnings.String(), path) {
			t.Errorf("missing warning for %s: %s", path, warnings)
		}
	}
}

func keysForZIMTest(documents map[string]document.Document) []string {
	var urls []string
	for u := range documents {
		urls = append(urls, u)
	}
	return urls
}

func TestZIMImportBaseURLFallback(t *testing.T) {
	command, _, _, received := zimTestCommand(t, "--base-url=https://manual.example/docs/")
	if err := command.RunE(command, []string{filepath.Join("testdata", "zim", "url-overrides.zim")}); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range *received {
		got = append(got, d.URL)
	}
	want := []string{"https://wrong.example/old", "https://manual.example/docs/reference.txt"}
	if !slices.Equal(got, want) {
		t.Errorf("URLs = %v, want page metadata before manual base: %v", got, want)
	}
}

func TestZIMImportDoesNotInferURLsFromDatasetNames(t *testing.T) {
	command, _, warnings, received := zimTestCommand(t)
	if err := command.RunE(command, []string{filepath.Join("testdata", "zim", "wikipedia.zim")}); err != nil {
		t.Fatal(err)
	}
	if len(*received) != 0 {
		t.Fatalf("dataset name must not invent an original URL: %+v", *received)
	}
	if !strings.Contains(warnings.String(), "Black_hole") || !strings.Contains(warnings.String(), "Skipping") {
		t.Errorf("missing skip warning: %s", warnings)
	}
}

func TestZIMURLInputValidation(t *testing.T) {
	for _, base := range []string{"relative/path", "file:///etc/passwd", "https://user:secret@example.com/", "https://example.com/?query=1", "https://example.com/#fragment"} {
		t.Run(base, func(t *testing.T) {
			command, _, _, _ := zimTestCommand(t, "--base-url="+base)
			if err := command.RunE(command, []string{"nonexistent.zim"}); err == nil || !strings.Contains(err.Error(), "--base-url") {
				t.Fatalf("invalid base accepted: %v", err)
			}
		})
	}
}

func TestZIMPageURLResolution(t *testing.T) {
	for _, tc := range []struct{ name, base, path, html, want string }{
		{"canonical", "", "entry", `<link rel="canonical" href="https://example.org/page">`, "https://example.org/page"},
		{"relative canonical", "", "entry", `<base href="https://example.org/docs/"><link rel="canonical" href="../guide?p=1#section">`, "https://example.org/guide?p=1#section"},
		{"relative with other metadata", "", "entry", `<link rel="canonical" href="/canonical"><meta property="og:url" content="https://example.org/old">`, "https://example.org/canonical"},
		{"relative base", "", "entry", `<base href="../docs/"><link rel="canonical" href="page"><meta property="og:url" content="https://example.org/old/index">`, "https://example.org/docs/page"},
		{"network relative", "", "entry", `<base href="http://example.org/"><link rel="canonical" href="//cdn.example.org/page">`, "http://cdn.example.org/page"},
		{"encoded path", "", "entry", `<base href="https://example.org/a%2Fb/"><link rel="canonical" href="page%20one">`, "https://example.org/a%2Fb/page%20one"},
		{"canonical rel tokens", "", "entry", `<link rel="alternate CANONICAL" href="https://example.org/page">`, "https://example.org/page"},
		{"invalid canonical", "", "entry", `<link rel="canonical" href="javascript:alert(1)"><meta name="twitter:url" content="https://example.org/page">`, "https://example.org/page"},
		{"missing origin", "", "entry", `<link rel="canonical" href="/page">`, ""},
		{"base alone", "", "entry", `<base href="https://example.org/assets/">`, ""},
		{"credentials", "", "entry", `<link rel="canonical" href="https://user:secret@example.org/page">`, ""},
		{"invalid base", "", "entry", `<base href="file:///tmp/"><link rel="canonical" href="page">`, ""},
		{"invalid base credentials", "", "entry", `<base href="https://user:secret@example.org/"><link rel="canonical" href="page">`, ""},
		{"external link is not identity", "", "entry", `<a href="https://example.org/page">Source?</a>`, ""},
		{"manual relative", "https://example.org/docs/", "index.html", `<link rel="canonical" href="../page">`, "https://example.org/page"},
		{"manual escaped path", "https://example.org/a%2Fb/", "Why?", `<p>Text</p>`, "https://example.org/a%2Fb/Why%3F"},
		{"unsafe manual path", "https://example.org/docs/", "../outside", `<p>Text</p>`, ""},
		{"absolute entry path", "", "https://original.example/page", `<p>Text</p>`, "https://original.example/page"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command, _, _, _ := zimTestCommand(t, "--base-url="+tc.base)
			resolver, err := newZIMURLResolver(command)
			if err != nil {
				t.Fatal(err)
			}
			got := resolver.resolve(zimRecord{Path: tc.path, MIME: "text/html", Content: tc.html})
			if got != tc.want {
				t.Errorf("URL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestZIMArticleLinkResolution(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, base, href, want string
	}{
		{"escaped path", "", "", "../space%20and%3F.html#part", "https://example.org/reference?id=7#part"},
		{"root relative", "", "", "/space%20and%3F.html", "https://example.org/reference?id=7"},
		{"legacy namespace", "A/", "", "../../A/space%20and%3F.html", "https://example.org/reference?id=7"},
		{"legacy asset namespace", "A/", "", "../../I/space%20and%3F.html", ""},
		{"query identifies entry", "", "", "../query?version=2#part", "https://example.org/v2#part"},
		{"redirect chain", "", "", "../alias#part", "https://example.org/reference?id=7#part"},
		{"redirect cycle", "", "", "../cycle", ""},
		{"unknown entry", "", "", "../unknown", ""},
		{"absolute HTML base", "", `<base href="https://example.net/guide/">`, "page?q=1#part", "https://example.net/guide/page?q=1#part"},
		{"network relative with HTML base", "", `<base href="http://example.net/guide/">`, "//cdn.example.net/page", "http://cdn.example.net/page"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &zimURLResolver{
				prefix: tc.prefix,
				pages: map[string]string{
					"space and?.html": "https://example.org/reference?id=7",
					"query?version=2": "https://example.org/v2",
				},
				redirects: map[string]string{"alias": "second", "second": "space and?.html", "cycle": "cycle"},
			}
			content, err := r.rewriteLinks(zimRecord{Path: "docs/start.html", Content: tc.base + `<a href="` + tc.href + `">Link text</a>`}, "https://example.org/start")
			if err != nil {
				t.Fatal(err)
			}
			html, err := goquery.NewDocumentFromReader(strings.NewReader(content))
			if err != nil {
				t.Fatal(err)
			}
			if got := html.Find("a").AttrOr("href", ""); got != tc.want {
				t.Errorf("link = %q, want %q", got, tc.want)
			}
			if html.Find("a").Text() != "Link text" {
				t.Error("link text was lost")
			}
		})
	}
}

func TestZIMImportedHTMLPreservesPageURL(t *testing.T) {
	command, _, _, _ := zimTestCommand(t)
	r, err := newZIMURLResolver(command)
	if err != nil {
		t.Fatal(err)
	}
	entry := zimRecord{Path: "entry.html", MIME: "text/html", Content: `<base href="https://example.org/docs/"><link rel="canonical" href="chapters/intro"><meta property="og:url" content="chapters/intro"><p>Article.</p>`}
	original := r.resolve(entry)
	content, err := r.rewriteLinks(entry, original)
	if err != nil {
		t.Fatal(err)
	}
	html, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if html.Find("base").Length() != 0 {
		t.Error("archive base must not affect the Nogon preview")
	}
	if got := html.Find(`link[rel="canonical"]`).AttrOr("href", ""); got != "https://example.org/docs/chapters/intro" {
		t.Errorf("saved canonical = %q", got)
	}
	if got := html.Find(`meta[property="og:url"]`).AttrOr("content", ""); got != original {
		t.Errorf("saved Open Graph URL = %q", got)
	}
}

func TestZIMImportLimitsAndErrors(t *testing.T) {
	filename := filepath.Join("testdata", "zim", "size-limit.zim")
	command, _, warnings, received := zimTestCommand(t, "--base-url=https://example.org/")
	cfg.Indexer.MaxFileSize = 1
	if err := command.RunE(command, []string{filename}); err == nil {
		t.Fatal("oversized entry should report an incomplete import")
	}
	if len(*received) != 1 || (*received)[0].Text != "small text" {
		t.Errorf("valid entry was not imported: %+v", *received)
	}
	if !strings.Contains(warnings.String(), "large.txt") || !strings.Contains(warnings.String(), "max_file_size_mb") {
		t.Errorf("missing size warning: %s", warnings)
	}

	command, _, _, _ = zimTestCommand(t, "--base-url=https://example.org/")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	command.SetContext(ctx)
	if err := command.RunE(command, []string{filename}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation lost: %v", err)
	}
}

func TestZIMLegacyArchive(t *testing.T) {
	// Optional fixture from the upstream suite: data/withns/small.zim.
	// https://github.com/openzim/zim-testing-suite
	filename, set := os.LookupEnv("NOGON_ZIM_LEGACY_FIXTURE")
	if !set {
		filename = os.Getenv("HISTER_ZIM_LEGACY_FIXTURE")
	}
	if filename == "" {
		t.Skip("set NOGON_ZIM_LEGACY_FIXTURE to the upstream withns/small.zim fixture")
	}
	command, _, _, received := zimTestCommand(t, "--base-url=https://example.org/")
	if err := command.RunE(command, []string{filename}); err != nil {
		t.Fatal(err)
	}
	if len(*received) != 1 || (*received)[0].URL != "https://example.org/main.html" {
		t.Fatalf("legacy archive must import only the article with its namespace removed: %+v", *received)
	}
}

func TestZIMImportContinuesAfterEmptyArticle(t *testing.T) {
	filename := filepath.Join("testdata", "zim", "empty-article.zim")
	command, _, warnings, received := zimTestCommand(t, "--base-url=https://example.org/")
	if err := command.RunE(command, []string{filename}); err == nil {
		t.Fatal("failed extraction was not reported")
	}
	if len(*received) != 1 || (*received)[0].Text != "Searchable text." {
		t.Errorf("valid article lost after extraction error: %+v", *received)
	}
	if !strings.Contains(warnings.String(), "empty.html") {
		t.Errorf("failed entry not identified: %s", warnings)
	}
}

func TestZIMImportSkipExistingAndSubmissionPolicy(t *testing.T) {
	filename := filepath.Join("testdata", "zim", "skip-existing.zim")
	command, _, warnings, received := zimTestCommand(t, "--base-url=https://example.org/", "--skip-existing", "--user-id=42", "--allow-sensitive", "--ignore-rules")
	cfg.App.AccessToken = "destination-token"
	base := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-Access-Token") != "destination-token" || r.Header.Get("X-Nogon-Target-User-ID") != "42" {
			t.Errorf("destination policy missing: %v", r.Header)
		}
		if r.URL.Path == "/api/document" {
			status := http.StatusNotFound
			if strings.HasSuffix(r.URL.Query().Get("url"), "/existing.txt") {
				status = http.StatusOK
			}
			return jsonHTTPResponse(r, status, ""), nil
		}
		return base.RoundTrip(r)
	})
	if err := command.RunE(command, []string{filename}); err != nil {
		t.Fatal(err)
	}
	if len(*received) != 1 || (*received)[0].Text != "New." || !(*received)[0].SkipSensitiveCheck || !(*received)[0].IgnoreSkipRules() {
		t.Errorf("submission options or skip-existing failed: %+v", *received)
	}
	if warnings.Len() != 0 {
		t.Errorf("supplied source URLs should not warn: %s", warnings)
	}
}

func TestZIMImportRejectsMalformedArchive(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "invalid.zim")
	if err := os.WriteFile(filename, []byte("not a ZIM"), 0o600); err != nil {
		t.Fatal(err)
	}
	command, _, _, received := zimTestCommand(t)
	if err := command.RunE(command, []string{filename}); err == nil || !strings.Contains(err.Error(), "ZIM") {
		t.Errorf("missing archive error: %v", err)
	}
	if len(*received) != 0 {
		t.Error("malformed archive produced documents")
	}
}

func TestZIMImportWithoutExternalReader(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	command, _, _, received := zimTestCommand(t)
	filename := filepath.Join("testdata", "zim", "canonical-snapshot.zim")
	if err := command.RunE(command, []string{filename}); err != nil {
		t.Fatalf("import must work without Python or an external reader: %v", err)
	}
	if len(*received) != 1 {
		t.Fatalf("imported %d articles, want 1", len(*received))
	}
}

func TestZIMImportLegacyXZ(t *testing.T) {
	command, _, warnings, received := zimTestCommand(t)
	filename := filepath.Join("testdata", "zim", "legacy-xz.zim")
	if err := command.RunE(command, []string{filename}); err != nil {
		t.Fatal(err)
	}
	if len(*received) != 1 {
		t.Fatalf("imported %d articles, want 1", len(*received))
	}
	if d := (*received)[0]; d.URL != "https://example.com/article" || !strings.Contains(d.HTML, "Archived article") {
		t.Errorf("legacy namespace or XZ content was not decoded: %+v", d)
	}
	if !strings.Contains(warnings.String(), "missing.html") {
		t.Error("missing source URL was not reported")
	}
}

func TestZIMImportRejectsInvalidOffsets(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "zim", "canonical-snapshot.zim"))
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	urlTable := le.Uint64(fixture[32:40])
	clusterTable := le.Uint64(fixture[48:56])
	for _, tc := range []struct {
		name          string
		offset, value uint64
	}{
		{"MIME list outside file", 56, uint64(len(fixture) + 1)},
		{"MIME list overflow", 56, ^uint64(0)},
		{"directory table overflow", 32, ^uint64(0) - 1},
		{"directory entry outside file", urlTable, uint64(len(fixture) + 1)},
		{"directory entry overflow", urlTable, ^uint64(0)},
		{"cluster table outside file", 48, uint64(len(fixture))},
		{"cluster outside file", clusterTable, uint64(len(fixture) + 1)},
		{"unordered clusters", clusterTable + 8, le.Uint64(fixture[clusterTable : clusterTable+8])},
		{"title table outside file", 40, uint64(len(fixture))},
		{"checksum outside file", 72, uint64(len(fixture))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := bytes.Clone(fixture)
			le.PutUint64(data[tc.offset:tc.offset+8], tc.value)
			filename := filepath.Join(t.TempDir(), "invalid.zim")
			if err := os.WriteFile(filename, data, 0o600); err != nil {
				t.Fatal(err)
			}
			command, _, _, received := zimTestCommand(t)
			if err := command.RunE(command, []string{filename}); err == nil {
				t.Error("corrupt archive was accepted")
			}
			if len(*received) != 0 {
				t.Error("corrupt archive produced documents")
			}
		})
	}
}

func TestZIMImportPreservesClientTimeout(t *testing.T) {
	for _, tc := range []struct {
		name, flag string
		want       time.Duration
	}{{"default", "", 10 * time.Second}, {"configured", "17", 17 * time.Second}, {"disabled", "0", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			flag := rootCmd.PersistentFlags().Lookup("client-timeout")
			oldValue, oldChanged := flag.Value.String(), flag.Changed
			t.Cleanup(func() {
				_ = flag.Value.Set(oldValue)
				flag.Changed = oldChanged
			})
			flag.Changed = tc.flag != ""
			if flag.Changed {
				if err := flag.Value.Set(tc.flag); err != nil {
					t.Fatal(err)
				}
			}
			command, _, _, _ := zimTestCommand(t)
			base := http.DefaultTransport
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				deadline, ok := r.Context().Deadline()
				if tc.want == 0 {
					if ok {
						t.Error("timeout was not disabled")
					}
				} else if !ok || time.Until(deadline) > tc.want || time.Until(deadline) < tc.want-time.Second {
					t.Errorf("request deadline = %v (present: %v), want a %v timeout", deadline, ok, tc.want)
				}
				return base.RoundTrip(r)
			})
			if err := command.RunE(command, []string{filepath.Join("testdata", "zim", "canonical-snapshot.zim")}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
