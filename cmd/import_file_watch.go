package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/server/document"
)

type fileWatchOptions struct {
	Source       string
	MaxFileSize  int64
	SkipExisting bool
	Label        documentLabelOverride
	RetryDelay   time.Duration
}

type fileWatchInputs struct {
	dirs  []*config.Directory
	paths map[string]importFileInput
}

func newFileWatchInputs(args []string, directories []*config.Directory) (*fileWatchInputs, error) {
	inputs := &fileWatchInputs{paths: make(map[string]importFileInput)}
	normalized := make([]*config.Directory, 0, len(directories))
	for _, dir := range directories {
		if dir == nil {
			continue
		}
		copy := *dir
		var err error
		copy.Path, err = filepath.Abs(files.ExpandHome(dir.Path))
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, &copy)
	}
	if len(args) == 0 {
		if len(normalized) == 0 {
			return nil, fmt.Errorf("no watched directories are configured")
		}
		inputs.dirs = normalized
		return inputs, nil
	}
	for _, arg := range args {
		path, err := filepath.Abs(files.ExpandHome(arg))
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect input %s: %w", arg, err)
		}
		dir := files.FindMatchingDir(normalized, path)
		if info.IsDir() {
			copy := config.Directory{Path: path}
			if dir != nil {
				copy = *dir
				copy.Path = path
			}
			inputs.dirs = append(inputs.dirs, &copy)
		} else if info.Mode().IsRegular() {
			input := importFileInput{Path: path}
			if files.DirectoryMatchesPath(dir, path) {
				input.Label = dir.Label
			}
			inputs.paths[path] = input
		} else {
			return nil, fmt.Errorf("input is not a regular file: %s", arg)
		}
	}
	return inputs, nil
}

func (inputs *fileWatchInputs) input(path string) (importFileInput, bool) {
	if input, ok := inputs.paths[path]; ok {
		return input, true
	}
	for _, dir := range inputs.dirs {
		if files.DirectoryMatchesPath(dir, path) {
			return importFileInput{Path: path, Label: dir.Label}, true
		}
	}
	return importFileInput{}, false
}

func watchImportFiles(ctx context.Context, c *client.Client, args []string, dirs []*config.Directory, opts fileWatchOptions) (serviceImportStats, error) {
	inputs, err := newFileWatchInputs(args, dirs)
	if err != nil {
		return serviceImportStats{}, err
	}
	paths := make([]string, 0, len(inputs.paths))
	for path := range inputs.paths {
		paths = append(paths, path)
	}
	watcher, err := files.NewWatcher(inputs.dirs, paths)
	if err != nil {
		return serviceImportStats{}, err
	}
	defer func() { _ = watcher.Close() }()

	queue := newFileImportQueue()
	// Watches are already active while the initial scan is collected. All later
	// notifications override skip_existing, including changes during this scan.
	if len(inputs.dirs) > 0 {
		initial, err := expandImportInputs(nil, inputs.dirs)
		if err != nil {
			return serviceImportStats{}, err
		}
		for _, input := range initial {
			queue.enqueue(input.Path, opts.SkipExisting, time.Now(), false)
		}
	}
	for path := range inputs.paths {
		queue.enqueue(path, opts.SkipExisting, time.Now(), false)
	}

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchDone := make(chan error, 1)
	go func() {
		err := watcher.Run(watchCtx, func(path string) {
			queue.enqueue(path, false, time.Now(), false)
		}, nil)
		watchDone <- err
		cancel()
	}()
	log.Info().Msg("Importing and watching file snapshots; interrupt to stop")
	stats := queue.run(watchCtx, func(ctx context.Context, path string, skip bool) (bool, error) {
		input, ok := inputs.input(path)
		if !ok {
			return true, nil
		}
		return importWatchedFile(ctx, c, input, opts, skip)
	}, opts.RetryDelay)
	cancel()
	err = <-watchDone
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		err = nil
	}
	return stats, err
}

// importWatchedFile reports skipped inputs separately from failures. It reads
// only regular files within the size limit and preserves snapshot identities.
func importWatchedFile(ctx context.Context, c *client.Client, input importFileInput, opts fileWatchOptions, skip bool) (bool, error) {
	ext := strings.ToLower(filepath.Ext(input.Path))
	if ext == ".7z" {
		log.Warn().Str("file", input.Path).Msg("Watch mode skips export archives")
		return true, nil
	}
	info, err := os.Stat(input.Path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return true, nil
	}
	if info.Size() > opts.MaxFileSize {
		return false, fileSnapshotSizeError(info.Size(), opts.MaxFileSize)
	}
	f, err := os.Open(input.Path)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	content, err := io.ReadAll(io.LimitReader(f, opts.MaxFileSize+1))
	if err != nil {
		return false, err
	}
	if int64(len(content)) > opts.MaxFileSize {
		return false, fileSnapshotSizeError(max(info.Size(), int64(len(content))), opts.MaxFileSize)
	}
	if ext == ".json" && isHisterJSONExportReader(bytes.NewReader(content)) {
		log.Warn().Str("file", input.Path).Msg("Watch mode skips Hister exports")
		return true, nil
	}
	if ext == ".html" || ext == ".htm" {
		if _, err := document.FromHTML(string(content)); !errors.Is(err, document.ErrNoURL) {
			if err != nil {
				return false, err
			}
			log.Warn().Str("file", input.Path).Msg("Watch mode skips saved pages with source URLs")
			return true, nil
		}
	}

	requestCtx, cancel := context.WithTimeout(ctx, serviceImportRequestTimeout)
	defer cancel()
	if skip {
		url, err := remoteFileURL(opts.Source, input.Path)
		if err != nil {
			return false, err
		}
		exists, err := c.DocumentExistsContext(requestCtx, url)
		if err != nil || exists {
			return exists, err
		}
	}
	d, err := prepareRemoteFile(input, content, info, opts.Source, opts.MaxFileSize, opts.Label)
	if err != nil {
		return false, err
	}
	return false, c.AddDocumentJSONContext(requestCtx, d)
}

type fileImportWork struct {
	path string
	skip bool
	due  time.Time
}

// fileImportQueue keeps at most one pending operation per path and serializes
// submissions so an older request cannot overwrite a newer snapshot.
type fileImportQueue struct {
	mu      sync.Mutex
	pending map[string]fileImportWork
	wake    chan struct{}
}

func newFileImportQueue() *fileImportQueue {
	return &fileImportQueue{pending: make(map[string]fileImportWork), wake: make(chan struct{}, 1)}
}

func (q *fileImportQueue) enqueue(path string, skip bool, due time.Time, retry bool) {
	q.mu.Lock()
	previous, exists := q.pending[path]
	if !retry || !exists {
		if exists {
			skip = skip && previous.skip
		}
		q.pending[path] = fileImportWork{path: path, skip: skip, due: due}
	}
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *fileImportQueue) next() (fileImportWork, time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var next fileImportWork
	for _, work := range q.pending {
		if next.path == "" || work.due.Before(next.due) {
			next = work
		}
	}
	if next.path == "" {
		return next, time.Hour
	}
	delay := time.Until(next.due)
	if delay <= 0 {
		delete(q.pending, next.path)
	}
	return next, delay
}

func (q *fileImportQueue) run(ctx context.Context, process func(context.Context, string, bool) (bool, error), retryDelay time.Duration) serviceImportStats {
	stats := serviceImportStats{}
	failed := make(map[string]bool)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for ctx.Err() == nil {
		work, delay := q.next()
		if delay > 0 {
			timer.Reset(delay)
			select {
			case <-ctx.Done():
			case <-q.wake:
			case <-timer.C:
			}
			continue
		}
		skipped, err := process(ctx, work.path, work.skip)
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			failed[work.path] = true
			retry := retryableFileImportError(err)
			log.Warn().Err(err).Str("file", work.path).Bool("retry", retry).Msg("Failed to import watched file")
			if retry {
				q.enqueue(work.path, work.skip, time.Now().Add(retryDelay), true)
			}
			continue
		}
		delete(failed, work.path)
		if skipped {
			stats.Skipped++
		} else {
			stats.Imported++
			log.Info().Str("file", work.path).Msg("Imported file snapshot")
		}
	}
	stats.Errors = len(failed)
	return stats
}

func retryableFileImportError(err error) bool {
	if httpErr, ok := errors.AsType[*client.HTTPError](err); ok {
		return httpErr.StatusCode == http.StatusRequestTimeout || httpErr.StatusCode == http.StatusTooManyRequests || httpErr.StatusCode >= 500
	}
	var netErr net.Error
	return errors.As(err, &netErr) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}
