// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/asciimoo/hister/server/crawler"
	"github.com/asciimoo/hister/server/model"
)

func TestHistoryTableFromPath(t *testing.T) {
	dir := t.TempDir()

	safari := filepath.Join(dir, "safari", "History.db")
	writeSafariHistoryFile(t, safari, map[string]int64{"https://example.com": 1})

	ladybird := filepath.Join(dir, "ladybird", "History.db")
	writeTables(t, ladybird, "CREATE TABLE History (url TEXT, last_visited_time INTEGER)")

	chrome := filepath.Join(dir, "chrome", "History")
	writeTables(t, chrome, "CREATE TABLE urls (url TEXT, visit_count INTEGER)")

	firefox := filepath.Join(dir, "firefox", "places.sqlite")
	writeTables(t, firefox, "CREATE TABLE moz_places (url TEXT, last_visit_date INTEGER)")

	for _, tc := range []struct{ name, path, want string }{
		{"safari", safari, "safari"},
		{"ladybird", ladybird, "History"},
		{"chrome", chrome, "urls"},
		{"firefox", firefox, "moz_places"},
	} {
		got, err := historyTableFromPath(tc.path)
		if err != nil {
			t.Fatalf("historyTableFromPath(%s) err = %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("historyTableFromPath(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}

	unknown := filepath.Join(dir, "other", "Bookmarks")
	writeTables(t, unknown, "CREATE TABLE something_else (a TEXT)")
	if _, err := historyTableFromPath(unknown); err == nil {
		t.Fatal("expected unknown schema to be rejected")
	}
}

func TestResolveHistoryImportsNamedDB(t *testing.T) {
	dir := t.TempDir()
	firefox := filepath.Join(dir, "places.sqlite")
	writeTables(t, firefox, "CREATE TABLE moz_places (url TEXT, last_visit_date INTEGER)")

	got, err := resolveHistoryImports("", firefox)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].table != "moz_places" || got[0].databaseFile != firefox {
		t.Fatalf("resolveHistoryImports() = %#v", got)
	}

	chrome := filepath.Join(dir, "Default", "History")
	writeTables(t, chrome, "CREATE TABLE urls (url TEXT, visit_count INTEGER)")
	got, err = resolveHistoryImports("chrome", chrome)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].table != "urls" {
		t.Fatalf("chrome History table = %#v", got)
	}

	safari := filepath.Join(dir, "Safari", "History.db")
	writeSafariHistoryFile(t, safari, map[string]int64{"https://example.com": 1})
	for _, browser := range []string{"", "safari"} {
		got, err = resolveHistoryImports(browser, safari)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].table != "safari" || got[0].databaseFile != safari {
			t.Fatalf("resolveHistoryImports(%q, %q) = %#v", browser, safari, got)
		}
	}
}

func TestResolveHistoryImportsRejectsUnknownBrowser(t *testing.T) {
	isolateBrowserHistoryHome(t)

	const browser = "not-a-real-browser"
	_, err := resolveHistoryImports(browser, "")
	if want := "unknown --browser " + browser; err == nil || err.Error() != want {
		t.Fatalf("resolveHistoryImports(%q, %q) error = %v, want %q", browser, "", err, want)
	}
}

func TestResolveHistoryImportsSafariDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name     string
		createDB bool
	}{
		{name: "missing"},
		{name: "present", createDB: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := isolateBrowserHistoryHome(t)
			safari := filepath.Join(home, "Library", "Safari", "History.db")
			if tc.createDB {
				writeSafariHistoryFile(t, safari, map[string]int64{"https://example.com": 1})
			}

			got, err := resolveHistoryImports("safari", "")
			if !tc.createDB || runtime.GOOS != "darwin" {
				const want = "no history database found for browser safari"
				if err == nil || err.Error() != want {
					t.Fatalf("resolveHistoryImports(safari) error = %v, want %q", err, want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].table != "safari" || got[0].databaseFile != safari {
				t.Fatalf("resolveHistoryImports(safari) = %#v, want Safari database %q", got, safari)
			}
		})
	}
}

func isolateBrowserHistoryHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	return home
}

func TestBrowserImportJobsFiltersByPrefix(t *testing.T) {
	rules, err := crawler.MarshalValidatorRules(&crawler.ValidatorRules{NoDepth: true})
	if err != nil {
		t.Fatal(err)
	}
	deep, err := crawler.MarshalValidatorRules(&crawler.ValidatorRules{NoDepth: false})
	if err != nil {
		t.Fatal(err)
	}
	jobs := []*model.CrawlJob{
		{ID: "browser-history-import-2026-08-26", ValidatorRules: rules},
		{ID: "browser-bookmark-import-2026-08-26", ValidatorRules: rules},
		{ID: "browser-import-2026-08-25", ValidatorRules: rules},
		{ID: "browser-history-import-other", ValidatorRules: deep},
	}
	got := browserImportJobs(jobs, bookmarkImportJobPrefix)
	if len(got) != 1 || got[0].ID != "browser-bookmark-import-2026-08-26" {
		t.Fatalf("bookmark prefix = %#v", jobIDs(got))
	}
	got = browserImportJobs(jobs, browserImportMatchPrefixes(browserImportKindHistory)...)
	if len(got) != 2 {
		t.Fatalf("history prefixes = %#v", jobIDs(got))
	}
	ids := jobIDs(got)
	if !slices.Contains(ids, "browser-history-import-2026-08-26") || !slices.Contains(ids, "browser-import-2026-08-25") {
		t.Fatalf("history prefixes missing legacy or new job: %#v", ids)
	}
}

func TestImportBrowserCLICompat(t *testing.T) {
	cmd, args, err := rootCmd.Find([]string{"import", "browser", "firefox"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != importBrowserCmd {
		t.Fatalf("import browser firefox -> %q", cmd.Use)
	}
	if len(args) != 1 || args[0] != "firefox" {
		t.Fatalf("firefox args = %#v", args)
	}

	cmd, args, err = rootCmd.Find([]string{"import", "browser", "/tmp/places.sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != importBrowserCmd {
		t.Fatalf("import browser path -> %q", cmd.Use)
	}
	if len(args) != 1 || args[0] != "/tmp/places.sqlite" {
		t.Fatalf("path args = %#v", args)
	}

	cmd, args, err = rootCmd.Find([]string{"import", "browser", "history"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != importBrowserHistoryCmd {
		t.Fatalf("import browser history -> %q", cmd.Use)
	}
	if len(args) != 0 {
		t.Fatalf("history args = %#v", args)
	}

	cmd, args, err = rootCmd.Find([]string{"import", "browser", "bookmarks"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != importBookmarksCmd {
		t.Fatalf("import browser bookmarks -> %q", cmd.Use)
	}
	if len(args) != 0 {
		t.Fatalf("bookmarks args = %#v", args)
	}
}

func jobIDs(jobs []*model.CrawlJob) []string {
	out := make([]string, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, job.ID)
	}
	return out
}
