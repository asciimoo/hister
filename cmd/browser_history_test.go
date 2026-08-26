// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"testing"

	"github.com/asciimoo/hister/server/crawler"
	"github.com/asciimoo/hister/server/model"
)

func TestHistoryTableFromPath(t *testing.T) {
	tests := map[string]string{
		"/tmp/places.sqlite":       "moz_places",
		"/tmp/Default/History":     "urls",
		"/tmp/Ladybird/History.db": "History",
	}
	for path, want := range tests {
		got, err := historyTableFromPath(path)
		if err != nil {
			t.Fatalf("historyTableFromPath(%q) err = %v", path, err)
		}
		if got != want {
			t.Fatalf("historyTableFromPath(%q) = %q, want %q", path, got, want)
		}
	}
	if _, err := historyTableFromPath("/tmp/Bookmarks"); err == nil {
		t.Fatal("expected Bookmarks path to be rejected as history")
	}
}

func TestResolveHistoryImportsNamedDB(t *testing.T) {
	got, err := resolveHistoryImports("", "/tmp/profile/places.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].table != "moz_places" || got[0].databaseFile != "/tmp/profile/places.sqlite" {
		t.Fatalf("resolveHistoryImports() = %#v", got)
	}

	got, err = resolveHistoryImports("chrome", "/tmp/Default/History")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].table != "urls" {
		t.Fatalf("chrome History table = %#v", got)
	}
}

func TestResolveHistoryImportsRejectsUnknownBrowser(t *testing.T) {
	if _, err := resolveHistoryImports("safari", ""); err == nil {
		t.Fatal("expected safari to be rejected")
	}
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
	if !contains(ids, "browser-history-import-2026-08-26") || !contains(ids, "browser-import-2026-08-25") {
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

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func jobIDs(jobs []*model.CrawlJob) []string {
	out := make([]string, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, job.ID)
	}
	return out
}
