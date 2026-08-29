// Package safari watches Safari's browsing history and indexes newly visited pages.
//
// Safari has no Hister extension, so a Safari user's index is frozen at whatever the last
// `hister import browser safari` captured. It does, however, write every visit to a SQLite
// database of its own, which makes that file a live feed. This follows it.
//
// It is a companion in the same sense qutebrowser's is — a local process that keeps the index in
// step with a browser Hister cannot otherwise reach — but it works from the other end. qutebrowser
// hands over rendered pages through DevTools; Safari hands over addresses, so the pages have to be
// fetched. That is why this takes a URLIndexer rather than a DocumentSubmitter, and it means a
// page behind a login indexes as whatever an anonymous client sees.
package safari

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	_ "github.com/mattn/go-sqlite3"
)

// coreDataEpoch is 2001-01-01 in Unix seconds. Safari records visit times as seconds from there,
// the Core Data reference date, rather than from 1970.
const coreDataEpoch = 978_307_200

type state struct {
	LastVisitTime float64 `json:"last_visit_time"`
	LastRun       string  `json:"last_run"`
}

// Run follows Safari's history until ctx is cancelled.
func Run(ctx context.Context, input Options, indexer URLIndexer) error {
	opts, err := normalizeOptions(input)
	if err != nil {
		return err
	}

	log.Info().
		Str("history", opts.historyPath).
		Dur("interval", opts.pollInterval).
		Msg("Watching Safari history")

	ticker := time.NewTicker(opts.pollInterval)
	defer ticker.Stop()

	var lastFingerprint string
	catchUp := opts.catchUp
	for {
		// The database is only read when something has written to it. An idle machine therefore
		// costs three stat() calls per interval rather than copying a database that has not
		// changed, which matters when the interval is seconds and the file is tens of megabytes.
		if fingerprint := sourceFingerprint(opts.historyPath); fingerprint != lastFingerprint {
			lastFingerprint = fingerprint
			if err := pass(ctx, opts, indexer, catchUp); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				// One bad pass must not end the companion: it is meant to still be running
				// tomorrow, and the next poll may well succeed.
				log.Warn().Err(err).Msg("Safari history pass failed")
			}
			catchUp = false
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// sourceFingerprint summarises the modification times of the history database and its
// write-ahead log.
//
// The -wal file is the one that moves. SQLite in WAL mode appends there and folds the changes into
// the main database only at a checkpoint, so watching History.db alone would miss visits for
// minutes and then deliver them in a lump.
func sourceFingerprint(path string) string {
	var fingerprint strings.Builder
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if info, err := os.Stat(path + suffix); err == nil {
			fmt.Fprintf(&fingerprint, "%d:", info.ModTime().UnixNano())
			continue
		}
		fingerprint.WriteString("0:")
	}
	return fingerprint.String()
}

func pass(ctx context.Context, opts normalizedOptions, indexer URLIndexer, catchUp bool) error {
	current := loadState(opts.statePath)
	if current.LastVisitTime == 0 && !catchUp {
		// Start from now rather than from the beginning of time. An existing history is what
		// `hister import browser safari` is for; replaying it here, a batch at a time, is not what
		// "watch for new pages" means and would crawl thousands of pages unasked.
		current.LastVisitTime = float64(time.Now().Unix() - coreDataEpoch)
		log.Info().Msg("No previous state; indexing visits from now on")
	}

	urls, newest, err := visitsSince(opts.historyPath, current.LastVisitTime)
	if err != nil {
		return err
	}

	for start := 0; start < len(urls); start += opts.batchSize {
		end := min(start+opts.batchSize, len(urls))
		if err := ctx.Err(); err != nil {
			return err
		}
		batch := urls[start:end]
		if err := indexer.IndexURLs(ctx, batch); err != nil {
			// The state is not advanced past a batch that failed, so the next pass retries it.
			return fmt.Errorf("index %d url(s): %w", len(batch), err)
		}
		log.Info().Int("count", len(batch)).Msg("Indexed newly visited pages")
	}

	current.LastVisitTime = newest
	return saveState(opts.statePath, current)
}

// visitsSince returns URLs visited after the given Safari timestamp, and the newest visit seen.
//
// Read from a copy. Safari holds the database open and its write-ahead log may carry visits not
// yet folded into the main file, so opening the original as immutable — which is what the browser
// import does, correctly, for a one-off read — would silently miss exactly the recent visits this
// exists to catch. Copying all three parts takes a consistent snapshot without asking anybody to
// quit their browser.
func visitsSince(historyPath string, after float64) (_ []string, _ float64, err error) {
	dir, err := os.MkdirTemp("", "hister-safari-companion-")
	if err != nil {
		return nil, after, err
	}
	defer func() {
		if removeErr := os.RemoveAll(dir); removeErr != nil && err == nil {
			err = removeErr
		}
	}()

	work := filepath.Join(dir, "History.db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if copyErr := copyFile(historyPath+suffix, work+suffix); copyErr != nil {
			if suffix == "" {
				return nil, after, fmt.Errorf("copy safari history: %w", copyErr)
			}
			// A missing -wal or -shm only means SQLite has checkpointed. Not a problem.
		}
	}

	db, err := sql.Open("sqlite3", "file:"+work+"?mode=ro")
	if err != nil {
		return nil, after, err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	rows, err := db.Query(
		"SELECT history_items.url, history_visits.visit_time "+
			"FROM history_visits "+
			"JOIN history_items ON history_items.id = history_visits.history_item "+
			"WHERE history_visits.visit_time > ? "+
			"AND (history_items.url LIKE 'http://%' OR history_items.url LIKE 'https://%') "+
			"ORDER BY history_visits.visit_time",
		after,
	)
	if err != nil {
		return nil, after, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	newest := after
	seen := make(map[string]bool)
	var urls []string
	for rows.Next() {
		var rawURL string
		var visitTime float64
		if err := rows.Scan(&rawURL, &visitTime); err != nil {
			return nil, newest, err
		}
		if visitTime > newest {
			newest = visitTime
		}
		// Reloading a page records several visits to one URL; only the first is news. Anything
		// beyond this is left to the indexer, which already skips what it holds.
		if seen[rawURL] {
			continue
		}
		seen[rawURL] = true
		urls = append(urls, rawURL)
	}
	return urls, newest, rows.Err()
}

func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := in.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := out.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	_, err = io.Copy(out, in)
	return err
}

func loadState(path string) state {
	var current state
	data, err := os.ReadFile(path)
	if err != nil {
		return current
	}
	if err := json.Unmarshal(data, &current); err != nil {
		log.Warn().Err(err).Str("path", path).Msg("Ignoring unreadable companion state")
		return state{}
	}
	return current
}

func saveState(path string, current state) error {
	current.LastRun = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Written to a temporary file and renamed, so an interrupted write cannot leave a truncated
	// state behind and send the next run back to the beginning.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
