// SPDX-FileContributor: slowerloris <taylor@teukka.tech>
//
// SPDX-License-Identifier: AGPL-3.0-or-later
package files

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/xsync"
)

func ExpandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// Debounce so we don't spam the index as write events can fire multiple times before closing a file after editing
const debounceTime = 200 * time.Millisecond

// findMatchingDir returns the Directory config whose expanded path contains filePath, or nil.
func findMatchingDir(dirs []*config.Directory, filePath string) *config.Directory {
	for i := range dirs {
		dirPath := filepath.Clean(ExpandHome(dirs[i].Path))
		if strings.HasPrefix(filePath, dirPath+"/") || filePath == dirPath {
			return dirs[i]
		}
	}
	return nil
}

var skipDirs = map[string]struct{}{
	"node_modules":     {},
	"bower_components": {},
	"jspm_packages":    {},
	"__pycache__":      {},
	"__pypackages__":   {},
}

// shouldSkipDir reports whether a directory should be excluded from watching.
func shouldSkipDir(name string, excludes []string, includeHidden bool) bool {
	if !includeHidden {
		if strings.HasPrefix(name, ".") {
			return true
		}
		if _, ok := skipDirs[name]; ok {
			return true
		}
	}
	for _, pattern := range excludes {
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
	}
	return false
}

// ShouldSkipDir is the exported form of shouldSkipDir.
func ShouldSkipDir(name string, excludes []string, includeHidden bool) bool {
	return shouldSkipDir(name, excludes, includeHidden)
}

// walkAndWatch registers subdirectories with the fsnotify watcher, up to maxWatchDirs total.
func walkAndWatch(watcher *fsnotify.Watcher, dirs []*config.Directory, maxWatchDirs int, watched *xsync.Set[string]) {
	for _, dir := range dirs {
		expanded := ExpandHome(dir.Path)
		if watched.Len() >= maxWatchDirs {
			log.Warn().
				Int("limit", maxWatchDirs).
				Str("path", expanded).
				Msg("Watch directory limit reached; remaining directories will not be watched for live changes")
			return
		}
		if err := watcher.Add(expanded); err != nil {
			log.Error().Err(err).Str("path", expanded).Msg("Failed to add path to file watcher")
		} else {
			watched.Add(expanded)
		}
		excludes := dir.Excludes
		_ = filepath.WalkDir(expanded, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				log.Warn().Err(err).Str("path", path).Msg("Error walking directory")
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if path == expanded {
				return nil
			}
			if shouldSkipDir(d.Name(), excludes, dir.IncludeHidden) {
				return filepath.SkipDir
			}
			if watched.Len() >= maxWatchDirs {
				log.Warn().
					Int("limit", maxWatchDirs).
					Str("path", path).
					Msg("Watch directory limit reached; skipping remaining subdirectories")
				return filepath.SkipAll
			}
			if err := watcher.Add(path); err != nil {
				log.Warn().Err(err).Str("path", path).Msg("Failed to watch subdirectory")
			} else {
				watched.Add(path)
			}
			return nil
		})
	}
	log.Debug().Int("directories", watched.Len()).Msg("File watcher registered directories")
}

func handleWrite(ctx context.Context, event fsnotify.Event, dirs []*config.Directory, mu *sync.Mutex, debounced map[string]*time.Timer, work chan<- string) {
	dir := findMatchingDir(dirs, event.Name)
	if dir == nil || !dir.IsMatching(event.Name) {
		return
	}
	name := event.Name
	mu.Lock()
	if t, ok := debounced[name]; ok {
		t.Reset(debounceTime)
	} else {
		debounced[name] = time.AfterFunc(debounceTime, func() {
			mu.Lock()
			delete(debounced, name)
			mu.Unlock()
			select {
			case work <- name:
			case <-ctx.Done():
			default:
				log.Debug().Str("path", name).Msg("Index worker queue full; dropping file event")
			}
		})
	}
	mu.Unlock()
}

func handleCreate(ctx context.Context, event fsnotify.Event, dirs []*config.Directory, watcher *fsnotify.Watcher, maxWatchDirs int, watched *xsync.Set[string], work chan<- string) {
	st, err := os.Stat(event.Name)
	if err != nil {
		return
	}
	if st.IsDir() {
		dir := findMatchingDir(dirs, event.Name)
		if dir == nil || shouldSkipDir(filepath.Base(event.Name), dir.Excludes, dir.IncludeHidden) {
			return
		}
		if !watched.Has(event.Name) {
			if watched.Len() >= maxWatchDirs {
				log.Warn().
					Int("limit", maxWatchDirs).
					Str("path", event.Name).
					Msg("Watch directory limit reached; new directory will not be watched")
				return
			}
			if err := watcher.Add(event.Name); err != nil {
				log.Warn().Err(err).Str("path", event.Name).Msg("Failed to watch new directory")
			} else {
				watched.Add(event.Name)
			}
		}
		return
	}
	dir := findMatchingDir(dirs, event.Name)
	if dir == nil || !dir.IsMatching(event.Name) {
		return
	}
	select {
	case work <- event.Name:
	case <-ctx.Done():
	default:
		log.Debug().Str("path", event.Name).Msg("Index worker queue full; dropping file event")
	}
}

// WatchDirectories watches configured directories for file changes and calls
// callback for each changed file.
func WatchDirectories(ctx context.Context, dirs []*config.Directory, workers int, maxWatchDirs int, callback func(string)) error {
	if workers < 1 {
		workers = 1
	}
	if maxWatchDirs < 1 {
		maxWatchDirs = 1
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	defer func() {
		if err := watcher.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close file watcher")
		}
	}()

	// innerCtx is cancelled and pending timers are stopped before work is closed,
	// ensuring timer closures never send on a closed channel.
	innerCtx, cancelInner := context.WithCancel(ctx)
	defer cancelInner()

	work := make(chan string, workers*4)

	var mu sync.Mutex
	debounced := make(map[string]*time.Timer)

	var closeOnce sync.Once
	closeWork := func() {
		closeOnce.Do(func() {
			cancelInner()
			mu.Lock()
			for name, t := range debounced {
				t.Stop()
				delete(debounced, name)
			}
			mu.Unlock()
			close(work)
		})
	}

	// Start the worker pool.
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range work {
				callback(path)
			}
		}()
	}

	var watched xsync.Set[string]

	log.Debug().Msg("Starting file watcher")
	walkAndWatch(watcher, dirs, maxWatchDirs, &watched)

	for {
		select {
		case <-ctx.Done():
			closeWork()
			wg.Wait()
			return ctx.Err()
		case event, ok := <-watcher.Events:
			if !ok {
				closeWork()
				wg.Wait()
				return nil
			}
			switch {
			case event.Has(fsnotify.Write):
				handleWrite(innerCtx, event, dirs, &mu, debounced, work)
			case event.Has(fsnotify.Create):
				handleCreate(innerCtx, event, dirs, watcher, maxWatchDirs, &watched, work)
			case event.Has(fsnotify.Remove), event.Has(fsnotify.Rename):
				watched.Delete(event.Name)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				closeWork()
				wg.Wait()
				return nil
			}
			log.Error().Err(err).Msg("Watcher failed to process event")
		}
	}
}
