package cmd

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/asciimoo/hister/config"
)

func TestBrowserImportExplicitOverrideIncludesSkippedURLs(t *testing.T) {
	oldCfg := cfg
	t.Cleanup(func() { cfg = oldCfg })
	cfg = config.CreateDefaultConfig()
	cfg.Rules = &config.Rules{
		Allow: &config.Rule{ReStrs: []string{`^https://allowed\.example/`}},
		Skip:  &config.Rule{ReStrs: []string{`^https://blocked\.example/`}},
	}
	if err := cfg.Rules.Compile(); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		cmd := &cobra.Command{Use: "browser"}
		cmd.Flags().Bool("ignore-rules", enabled, "")
		isSkip := browserImportSkipChecker(cmd)
		if got := isSkip("https://blocked.example/article"); got == enabled {
			t.Errorf("ignore-rules=%v: blocked URL skipped=%v", enabled, got)
		}
		if isSkip("https://allowed.example/article") {
			t.Error("unmatched URL was skipped")
		}
		if got := isSkip("https://outside.example/article"); got == enabled {
			t.Errorf("ignore-rules=%v: URL outside allow list skipped=%v", enabled, got)
		}
	}
}

func TestPrepareBrowserImportsContinuesAfterUnusableDatabase(t *testing.T) {
	dir := t.TempDir()
	invalidPath := filepath.Join(dir, "Chrome", "Default", "History")
	emptyPath := filepath.Join(dir, "Brave", "Default", "History")
	validPath := filepath.Join(dir, "Vivaldi", "Default", "History")

	writeBrowserHistoryFile(t, invalidPath, false, nil)
	writeBrowserHistoryFile(t, emptyPath, true, nil)
	writeBrowserHistoryFile(t, validPath, true, []string{"https://example.com"})

	choices, issues := prepareBrowserImports([]DBToImport{
		{table: "urls", databaseFile: invalidPath},
		{table: "urls", databaseFile: emptyPath},
		{table: "urls", databaseFile: validPath},
	}, 0, nil, nil)
	if len(choices) != 1 {
		t.Fatalf("prepareBrowserImports() returned %d choices, want 1", len(choices))
	}
	defer func() {
		if err := choices[0].db.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	if choices[0].choice != validPath {
		t.Fatalf("prepareBrowserImports() choice = %q, want %q", choices[0].choice, validPath)
	}
	if choices[0].urls != 1 {
		t.Fatalf("prepareBrowserImports() URL count = %d, want 1", choices[0].urls)
	}

	if len(issues) != 2 {
		t.Fatalf("prepareBrowserImports() returned %d issues, want 2", len(issues))
	}
	if issues[0].databaseFile != invalidPath {
		t.Fatalf("first issue file = %q, want %q", issues[0].databaseFile, invalidPath)
	}
	if issues[1].databaseFile != emptyPath {
		t.Fatalf("second issue file = %q, want %q", issues[1].databaseFile, emptyPath)
	}
	if !errors.Is(issues[1].err, errNoBrowserURLs) {
		t.Fatalf("second issue = %v, want %v", issues[1].err, errNoBrowserURLs)
	}
}

func writeBrowserHistoryFile(t *testing.T, path string, createTable bool, urls []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	if !createTable {
		return
	}
	if _, err := db.Exec("CREATE TABLE urls (url TEXT, visit_count INTEGER)"); err != nil {
		t.Fatal(err)
	}
	for _, u := range urls {
		if _, err := db.Exec("INSERT INTO urls (url, visit_count) VALUES (?, 1)", u); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBrowserImportURLQueryFlatSchemasUnchanged(t *testing.T) {
	// A regression guard, not a feature test. Introducing a FROM expression for Safari must not
	// alter a single byte of the query every other browser has always produced.
	for _, table := range []string{"urls", "moz_places", "history", "some_unknown_table"} {
		got, err := browserImportURLQuery(table, 0, nil)
		if err != nil {
			t.Fatalf("browserImportURLQuery(%q) returned error: %v", table, err)
		}
		want := "SELECT DISTINCT url FROM " + table +
			" WHERE (url LIKE 'http://%' OR url LIKE 'https://%')"
		if got != want {
			t.Fatalf("browserImportURLQuery(%q) = %q, want %q", table, got, want)
		}
	}
}

func TestBrowserImportURLQuerySafariJoinsVisits(t *testing.T) {
	got, err := browserImportURLQuery("safari", 0, nil)
	if err != nil {
		t.Fatalf("browserImportURLQuery(safari) returned error: %v", err)
	}
	for _, want := range []string{"history_items", "history_visits", "MAX(history_visits.visit_time)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("browserImportURLQuery(safari) = %q, missing %q", got, want)
		}
	}
}

func TestBrowserImportURLQuerySafariUsesCoreDataEpoch(t *testing.T) {
	// Safari counts seconds from 2001-01-01, so a Unix timestamp is 978,307,200 seconds ahead of
	// the same moment in its terms. Hard-coding the expected value rather than recomputing it
	// means a sign flip in the offset fails here rather than silently importing the wrong decade.
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got, err := browserImportURLQuery("safari", 0, &start)
	if err != nil {
		t.Fatalf("browserImportURLQuery(safari) returned error: %v", err)
	}
	const want = "AND last_visit_time >= 788918400"
	if !strings.Contains(got, want) {
		t.Fatalf("browserImportURLQuery(safari) = %q, missing %q", got, want)
	}
}

func TestPrepareBrowserImportsReadsSafariHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Safari", "History.db")
	writeSafariHistoryFile(t, path, map[string]int64{
		// visit_time values are seconds since 2001-01-01.
		"https://example.com/kept":    788918400 + 86400, // a day after the cut-off below
		"https://example.com/too-old": 788918400 - 86400,
	})

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	choices, issues := prepareBrowserImports([]DBToImport{
		{table: "safari", databaseFile: path},
	}, 0, &start, nil)
	if len(issues) != 0 {
		t.Fatalf("prepareBrowserImports() returned issues: %v", issues)
	}
	if len(choices) != 1 {
		t.Fatalf("prepareBrowserImports() returned %d choices, want 1", len(choices))
	}
	defer func() {
		if err := choices[0].db.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	if choices[0].urls != 1 {
		t.Fatalf("prepareBrowserImports() URL count = %d, want 1 (the older visit should be filtered out)", choices[0].urls)
	}
}

func writeSafariHistoryFile(t *testing.T, path string, visits map[string]int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// The two tables Safari actually uses, with only the columns the import reads.
	if _, err := db.Exec(
		"CREATE TABLE history_items (id INTEGER PRIMARY KEY, url TEXT, visit_count INTEGER)",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		"CREATE TABLE history_visits (id INTEGER PRIMARY KEY, history_item INTEGER, visit_time REAL)",
	); err != nil {
		t.Fatal(err)
	}
	id := 0
	for url, visitTime := range visits {
		id++
		if _, err := db.Exec(
			"INSERT INTO history_items (id, url, visit_count) VALUES (?, ?, 1)", id, url,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(
			"INSERT INTO history_visits (history_item, visit_time) VALUES (?, ?)", id, visitTime,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDetectHistoryTableIdentifiesByContent(t *testing.T) {
	// Safari and Ladybird both call their database History.db, so a name cannot separate them.
	// This is the case ad3lre reported on #694: a path-only import of Safari's history was read
	// as Ladybird's and failed with "no such table: History".
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
		got, err := detectHistoryTable(tc.path)
		if err != nil {
			t.Fatalf("detectHistoryTable(%s) returned error: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("detectHistoryTable(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDetectHistoryTableRejectsUnknownSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "History.db")
	writeTables(t, path, "CREATE TABLE something_else (a TEXT)")
	if _, err := detectHistoryTable(path); err == nil {
		t.Fatal("detectHistoryTable() accepted a database with no history table")
	}
}

func writeTables(t *testing.T, path string, statements ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGetBrowserTypeRecognisesSafari(t *testing.T) {
	// Without this the selection screen labels Safari "unknown", and typing "safari" at the
	// exclusion prompt matches nothing.
	got := getBrowserType("/Users/someone/Library/Safari/History.db")
	if got != "safari" {
		t.Fatalf("getBrowserType() = %q, want %q", got, "safari")
	}
}

func TestIsSafariHistoryPath(t *testing.T) {
	// The Full Disk Access hint is only true for Safari's protected directory. Anywhere else, a
	// permission error is an ordinary permission error and the hint would send someone to change
	// a system setting that was never the problem.
	for path, want := range map[string]bool{
		"/Users/someone/Library/Safari/History.db":                         true,
		"/Users/someone/library/safari/History.db":                         true,
		"/Users/someone/Library/Application Support/Google/Chrome/History": false,
		"/tmp/History.db": false,
	} {
		if got := isSafariHistoryPath(path); got != want {
			t.Fatalf("isSafariHistoryPath(%q) = %v, want %v", path, got, want)
		}
	}
}
