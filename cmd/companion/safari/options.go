package safari

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// defaultPollInterval is short because the point of a companion is that a page is searchable
	// while you are still thinking about it. The cost of a poll that finds nothing is three
	// stat() calls — see the modification-time check in Run.
	defaultPollInterval = 10 * time.Second
	// defaultBatchSize bounds how many URLs are handed over at once, so a burst of restored tabs
	// becomes several crawls rather than one very long one.
	defaultBatchSize = 25
)

// URLIndexer crawls and indexes URLs.
//
// Deliberately narrow, and deliberately not the client. The qutebrowser companion submits finished
// documents because DevTools hands it the rendered page; Safari's history gives us only addresses,
// so something has to fetch them. Keeping that behind an interface leaves this package free of the
// crawler and testable without a network — the concrete implementation is wired in cmd, where the
// crawl machinery already lives.
type URLIndexer interface {
	IndexURLs(ctx context.Context, urls []string) error
}

// Options configures Safari history monitoring.
type Options struct {
	// HistoryPath is Safari's history database. Empty means the standard location.
	HistoryPath string
	// StatePath records the most recent visit already seen, so a restart does not re-index.
	// Empty means the standard location.
	StatePath string
	// PollInterval is how often the database is checked for changes.
	PollInterval time.Duration
	// BatchSize is the largest number of URLs handed to the indexer at once.
	BatchSize int
	// CatchUp indexes everything in the history on first run rather than only new visits.
	// Off by default: an existing history belongs to `hister import browser safari`, and
	// replaying months of it one batch at a time is not what "watch for new pages" means.
	CatchUp bool
	// Label is applied to indexed documents.
	Label string
}

type normalizedOptions struct {
	historyPath  string
	statePath    string
	pollInterval time.Duration
	batchSize    int
	catchUp      bool
	label        string
}

// DefaultOptions returns the default Safari companion settings.
func DefaultOptions() Options {
	return Options{
		HistoryPath:  DefaultHistoryPath(),
		StatePath:    DefaultStatePath(),
		PollInterval: defaultPollInterval,
		BatchSize:    defaultBatchSize,
		Label:        "safari",
	}
}

// DefaultHistoryPath is where Safari keeps its history. One fixed location per user, with no
// profile directories to search.
func DefaultHistoryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Safari", "History.db")
}

// DefaultStatePath is where the companion remembers how far it has read.
func DefaultStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "hister", "safari-companion.json")
}

func normalizeOptions(input Options) (normalizedOptions, error) {
	normalized := normalizedOptions{
		historyPath:  input.HistoryPath,
		statePath:    input.StatePath,
		pollInterval: input.PollInterval,
		batchSize:    input.BatchSize,
		catchUp:      input.CatchUp,
		label:        input.Label,
	}

	if normalized.historyPath == "" {
		normalized.historyPath = DefaultHistoryPath()
	}
	if normalized.historyPath == "" {
		return normalized, errors.New("safari history path is not set and cannot be derived")
	}
	if normalized.statePath == "" {
		normalized.statePath = DefaultStatePath()
	}
	if normalized.statePath == "" {
		return normalized, errors.New("state path is not set and cannot be derived")
	}
	if normalized.pollInterval <= 0 {
		normalized.pollInterval = defaultPollInterval
	}
	if normalized.batchSize <= 0 {
		normalized.batchSize = defaultBatchSize
	}

	// Checked up front rather than on the first poll, so a wrong path or a missing Full Disk
	// Access grant is reported when the command starts instead of once a second forever.
	if err := historyReadable(normalized.historyPath); err != nil {
		return normalized, err
	}
	return normalized, nil
}

// historyReadable reports why Safari's history cannot be read, or nil if it can.
//
// ~/Library/Safari is protected on macOS, and the resulting failure is easy to misread: without
// the grant the SQLite driver reports "unable to open database file", which looks like a file
// permission problem and is not one — no chmod will fix it. Opening the file here turns that into
// an error naming the setting.
func historyReadable(path string) error {
	f, err := os.Open(path)
	if err == nil {
		return f.Close()
	}
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf(
			"%w: reading Safari's history requires Full Disk Access for the terminal or "+
				"application running hister (System Settings > Privacy & Security > Full Disk Access)",
			err,
		)
	}
	return err
}
