// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Real CREATE TABLE text from Firefox ESR 140 (places.sqlite).
const firefox140MozPlacesSQL = `CREATE TABLE moz_places (
  id INTEGER PRIMARY KEY,
  url LONGVARCHAR,
  title LONGVARCHAR,
  rev_host LONGVARCHAR,
  visit_count INTEGER DEFAULT 0,
  hidden INTEGER DEFAULT 0 NOT NULL,
  typed INTEGER DEFAULT 0 NOT NULL,
  frecency INTEGER DEFAULT -1 NOT NULL,
  last_visit_date INTEGER,
  guid TEXT,
  foreign_count INTEGER DEFAULT 0 NOT NULL,
  url_hash INTEGER DEFAULT 0 NOT NULL,
  description TEXT,
  preview_image_url TEXT,
  site_name TEXT,
  origin_id INTEGER,
  recalc_frecency INTEGER NOT NULL DEFAULT 0,
  alt_frecency INTEGER,
  recalc_alt_frecency INTEGER NOT NULL DEFAULT 0
)`

const firefox140MozBookmarksSQL = `CREATE TABLE moz_bookmarks (
  id INTEGER PRIMARY KEY,
  type INTEGER,
  fk INTEGER DEFAULT NULL,
  parent INTEGER,
  position INTEGER,
  title LONGVARCHAR,
  keyword_id INTEGER,
  folder_type TEXT,
  dateAdded INTEGER,
  lastModified INTEGER,
  guid TEXT,
  syncStatus INTEGER NOT NULL DEFAULT 0,
  syncChangeCounter INTEGER NOT NULL DEFAULT 1
)`

func TestFirefoxBookmarkURLQuerySeparatesBookmarksFromHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "places.sqlite")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, stmt := range []string{firefox140MozPlacesSQL, firefox140MozBookmarksSQL} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	// Rows match what Firefox 140 actually wrote in the spike: toolbar
	// bookmarks via Ctrl+D, one history-only visit, plus stock Mozilla
	// defaults and a couple of non-http items history import would mishandle.
	mustExec(t, db, `INSERT INTO moz_places (id, url, title, visit_count, last_visit_date, guid) VALUES
		(1, 'https://go.dev/blog/', 'The Go Blog - The Go Programming Language', 1, 1787709279314000, 'goblog'),
		(2, 'https://pkg.go.dev/', 'Go Packages - Go Packages', 2, 1787709397912885, 'gopkg'),
		(3, 'https://pkg.go.dev/search?q=', NULL, 1, 1787709397741448, 'gopkgsearch'),
		(4, 'https://github.com/asciimoo/hister', 'GitHub - asciimoo/hister: Your own search engine · GitHub', 1, 1787709425337327, 'hister'),
		(5, 'https://news.ycombinator.com/', 'Hacker News', 1, 1787709482758865, 'hn'),
		(6, 'https://support.mozilla.org/products/firefox', 'Get Help', 0, NULL, 'mozhelp'),
		(7, 'about:config', 'about config', 1, 1787709500000000, 'aboutcfg'),
		(8, 'javascript:void(0)', 'bookmarklet', 0, NULL, 'jsvoid')`)
	mustExec(t, db, `INSERT INTO moz_bookmarks (id, type, fk, parent, position, title, guid) VALUES
		(1, 2, NULL, 0, 0, 'root', 'root'),
		(2, 2, NULL, 1, 0, 'menu', 'menu'),
		(3, 2, NULL, 1, 1, 'toolbar', 'toolbar'),
		(4, 2, NULL, 2, 0, 'Mozilla Firefox', 'mozfolder'),
		(12, 1, 1, 3, 0, 'The Go Blog - The Go Programming Language', 'b-goblog'),
		(13, 1, 2, 3, 1, 'Go Packages - Go Packages', 'b-gopkg'),
		(14, 1, 4, 3, 2, 'GitHub - asciimoo/hister: Your own search engine · GitHub', 'b-hister'),
		(15, 1, 6, 4, 0, 'Get Help', 'b-help'),
		(16, 1, 7, 3, 3, 'about config', 'b-about'),
		(17, 1, 8, 3, 4, 'bookmarklet', 'b-js'),
		(18, 3, NULL, 3, 5, NULL, 'sep')`)

	q, err := browserImportURLQuery(firefoxBookmarkTable, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := queryStrings(t, db, q)
	want := []string{
		"https://github.com/asciimoo/hister",
		"https://go.dev/blog/",
		"https://pkg.go.dev/",
		"https://support.mozilla.org/products/firefox",
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("bookmark query = %#v, want %#v", got, want)
	}

	hist, err := browserImportURLQuery("moz_places", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	histGot := queryStrings(t, db, hist)
	if !slices.Contains(histGot, "https://news.ycombinator.com/") {
		t.Fatalf("history query missing HN: %#v", histGot)
	}
	if slices.Contains(got, "https://news.ycombinator.com/") {
		t.Fatal("bookmark query included history-only HN")
	}
	if slices.Contains(got, "https://pkg.go.dev/search?q=") {
		t.Fatal("bookmark query included history-only search URL")
	}
}

func TestBrowserImportURLQueryBookmarksRejectStartDate(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	_, err := browserImportURLQuery(firefoxBookmarkTable, 1, &start)
	if err == nil {
		t.Fatal("expected start-date to be rejected for bookmarks")
	}
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatal(err)
	}
}

func queryStrings(t *testing.T, db *sql.DB, q string) []string {
	t.Helper()
	rows, err := db.Query(q)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			t.Fatal(err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestResolveBookmarkImportsUsesNamedDB(t *testing.T) {
	got, err := resolveBookmarkImports("", "/tmp/profile/places.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].databaseFile != "/tmp/profile/places.sqlite" || got[0].table != firefoxBookmarkTable {
		t.Fatalf("resolveBookmarkImports() = %#v", got)
	}
}

func TestResolveBookmarkImportsRejectsUnknownBrowser(t *testing.T) {
	if _, err := resolveBookmarkImports("chrome", ""); err == nil {
		t.Fatal("expected chrome to be rejected")
	}
	if _, err := resolveBookmarkImports("", "/tmp/History"); err == nil {
		t.Fatal("expected a non-places path to be rejected")
	}
}
