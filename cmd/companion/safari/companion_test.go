package safari

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVisitsSinceReturnsOnlyNewerVisits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "History.db")
	writeSafariHistory(t, path, []visit{
		{url: "https://example.com/old", at: 1000},
		{url: "https://example.com/new", at: 3000},
	})

	urls, newest, err := visitsSince(path, 2000)
	if err != nil {
		t.Fatalf("visitsSince() returned error: %v", err)
	}
	if len(urls) != 1 || urls[0] != "https://example.com/new" {
		t.Fatalf("visitsSince() = %v, want only the newer URL", urls)
	}
	// The high-water mark must move to the newest visit SEEN, not to the newest returned, or a
	// pass that filters everything out would replay the same rows forever.
	if newest != 3000 {
		t.Fatalf("visitsSince() newest = %v, want 3000", newest)
	}
}

func TestVisitsSinceDeduplicatesRepeatVisits(t *testing.T) {
	// Reloading a page records a visit each time. The companion should offer the URL once.
	path := filepath.Join(t.TempDir(), "History.db")
	writeSafariHistory(t, path, []visit{
		{url: "https://example.com/page", at: 1100},
		{url: "https://example.com/page", at: 1200},
		{url: "https://example.com/page", at: 1300},
	})

	urls, _, err := visitsSince(path, 1000)
	if err != nil {
		t.Fatalf("visitsSince() returned error: %v", err)
	}
	if len(urls) != 1 {
		t.Fatalf("visitsSince() returned %d URLs, want 1", len(urls))
	}
}

func TestVisitsSinceIgnoresNonHTTPSchemes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "History.db")
	writeSafariHistory(t, path, []visit{
		{url: "https://example.com/page", at: 1100},
		{url: "file:///Users/someone/notes.txt", at: 1200},
		{url: "about:blank", at: 1300},
	})

	urls, _, err := visitsSince(path, 1000)
	if err != nil {
		t.Fatalf("visitsSince() returned error: %v", err)
	}
	if len(urls) != 1 || urls[0] != "https://example.com/page" {
		t.Fatalf("visitsSince() = %v, want only the http(s) URL", urls)
	}
}

func TestVisitsSinceReadsWhileTheDatabaseIsOpen(t *testing.T) {
	// Safari holds its history open, so the companion must not need exclusive access. Keeping a
	// writable handle open across the call reproduces that.
	path := filepath.Join(t.TempDir(), "History.db")
	writeSafariHistory(t, path, []visit{{url: "https://example.com/page", at: 1100}})

	held, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := held.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	if err := held.Ping(); err != nil {
		t.Fatal(err)
	}

	urls, _, err := visitsSince(path, 1000)
	if err != nil {
		t.Fatalf("visitsSince() returned error while the database was open: %v", err)
	}
	if len(urls) != 1 {
		t.Fatalf("visitsSince() returned %d URLs, want 1", len(urls))
	}
}

func TestNormalizeOptionsRejectsMissingHistory(t *testing.T) {
	// Reported when the command starts rather than once per poll forever.
	_, err := normalizeOptions(Options{
		HistoryPath: filepath.Join(t.TempDir(), "absent", "History.db"),
		StatePath:   filepath.Join(t.TempDir(), "state.json"),
	})
	if err == nil {
		t.Fatal("normalizeOptions() accepted a history path that does not exist")
	}
}

func TestNormalizeOptionsFillsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "History.db")
	writeSafariHistory(t, path, nil)

	opts, err := normalizeOptions(Options{
		HistoryPath: path,
		StatePath:   filepath.Join(t.TempDir(), "state.json"),
	})
	if err != nil {
		t.Fatalf("normalizeOptions() returned error: %v", err)
	}
	if opts.pollInterval != defaultPollInterval {
		t.Fatalf("pollInterval = %v, want %v", opts.pollInterval, defaultPollInterval)
	}
	if opts.batchSize != defaultBatchSize {
		t.Fatalf("batchSize = %d, want %d", opts.batchSize, defaultBatchSize)
	}
}

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	if err := saveState(path, state{LastVisitTime: 1234.5}); err != nil {
		t.Fatalf("saveState() returned error: %v", err)
	}
	if got := loadState(path).LastVisitTime; got != 1234.5 {
		t.Fatalf("loadState() = %v, want 1234.5", got)
	}
}

func TestLoadStateToleratesRubbish(t *testing.T) {
	// A truncated or hand-edited state file must not be fatal: the companion falls back to
	// starting from now, which is the same thing it does on a first run.
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadState(path).LastVisitTime; got != 0 {
		t.Fatalf("loadState() = %v, want the zero value", got)
	}
}

func TestSourceFingerprintTracksTheWriteAheadLog(t *testing.T) {
	// The -wal file is where SQLite appends between checkpoints, so a fingerprint that ignored it
	// would leave the companion asleep through a burst of browsing.
	dir := t.TempDir()
	path := filepath.Join(dir, "History.db")
	writeSafariHistory(t, path, nil)

	before := sourceFingerprint(path)
	if err := os.WriteFile(path+"-wal", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if after := sourceFingerprint(path); after == before {
		t.Fatal("sourceFingerprint() did not change when the -wal file appeared")
	}
}

type visit struct {
	url string
	at  float64
}

func writeSafariHistory(t *testing.T, path string, visits []visit) {
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
	// The two tables Safari uses, with only the columns the companion reads. visit_time is REAL,
	// as Safari stores it.
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

	items := map[string]int{}
	for _, v := range visits {
		id, ok := items[v.url]
		if !ok {
			id = len(items) + 1
			items[v.url] = id
			if _, err := db.Exec(
				"INSERT INTO history_items (id, url, visit_count) VALUES (?, ?, 1)", id, v.url,
			); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.Exec(
			"INSERT INTO history_visits (history_item, visit_time) VALUES (?, ?)", id, v.at,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCoreDataEpochMatchesTheReferenceDate(t *testing.T) {
	// 2001-01-01 UTC in Unix seconds. Hard-coded so a sign flip or a typo fails here rather than
	// quietly indexing the wrong decade.
	want := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	if coreDataEpoch != want {
		t.Fatalf("coreDataEpoch = %d, want %d", coreDataEpoch, want)
	}
}
