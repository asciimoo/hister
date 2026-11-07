// SPDX-FileContributor: slowerloris <taylor@teukka.tech>
//
// SPDX-License-Identifier: AGPL-3.0-or-later
package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/model"
)

var (
	ErrEmptyFile    = errors.New("empty file")
	ErrFileTooLarge = errors.New("file too large")
)

// ReadAllBounded reads at most limit bytes plus one byte to detect overflow.
// It returns no partial content when the limit is exceeded or a read fails.
func ReadAllBounded(reader io.Reader, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, errors.New("read limit must not be negative")
	}
	limited := &io.LimitedReader{R: reader, N: limit}
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if limited.N == 0 {
		var extra [1]byte
		n, err := io.ReadFull(reader, extra[:])
		if n > 0 {
			return nil, fmt.Errorf("%w: limit is %d bytes", ErrFileTooLarge, limit)
		}
		if err != io.EOF {
			return nil, err
		}
	}
	return data, nil
}

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

// ValidateDirectorySettings checks web selections against the server filesystem
// and user database without installing watchers or indexing files.
func ValidateDirectorySettings(c *config.Config) []config.FieldError {
	var result []config.FieldError
	var roots []string
	for i, dir := range c.Indexer.Directories {
		reject := func(message string) {
			result = append(result, config.FieldError{Path: "indexer.directories", Code: "invalid_value", Message: fmt.Sprintf("Directory %d: %s", i+1, message)})
		}
		if dir == nil {
			reject("Supply a directory row.")
			continue
		}
		root, err := filepath.EvalSymlinks(ExpandHome(dir.Path))
		if err != nil {
			reject("Choose an existing directory accessible to the server.")
			continue
		}
		root, err = filepath.Abs(root)
		if err != nil {
			reject("Cannot resolve the server directory path.")
			continue
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			reject("Choose an existing directory accessible to the server.")
			continue
		}
		folder, err := os.Open(root)
		if err == nil {
			_, err = folder.Readdirnames(1)
			_ = folder.Close()
		}
		if err != nil && err != io.EOF {
			reject("Choose a directory the server can read.")
			continue
		}
		for _, previous := range roots {
			if HasPathPrefix(root, previous) || HasPathPrefix(previous, root) {
				reject("Directory paths must not overlap, including symbolic links. Use one row for each tree.")
				break
			}
		}
		roots = append(roots, root)
		if dir.User != "" {
			if !c.App.UserHandling {
				reject("An owner requires multi user mode. Leave the owner empty for global files.")
			} else if _, err := model.GetUser(dir.User); err != nil {
				reject("The owner could not be resolved. Choose an existing username.")
			}
		}
	}
	return result
}

// HasPathPrefix reports whether filePath equals dirPath or is contained within it,
// using the platform's path separator.
func HasPathPrefix(filePath, dirPath string) bool {
	if filePath == dirPath {
		return true
	}
	separator := string(filepath.Separator)
	if !strings.HasSuffix(dirPath, separator) {
		dirPath += separator
	}
	return strings.HasPrefix(filePath, dirPath)
}

// PathToFileURL converts an absolute filesystem path into a file:// URL.
// On Windows, paths like C:\foo\bar become file:///C:/foo/bar.
func PathToFileURL(absPath string) string {
	p := filepath.ToSlash(absPath)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}

// FileURLToPath extracts an OS-native filesystem path from a file:// URL.
// On Windows, it strips the leading slash that precedes a drive letter
// (e.g. file:///C:/foo → C:\foo). A non-file URL is returned unchanged.
func FileURLToPath(fileURL string) string {
	p, ok := strings.CutPrefix(fileURL, "file://")
	if !ok {
		return fileURL
	}
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

// FindMatchingDir returns the Directory config whose expanded path contains filePath, or nil.
func FindMatchingDir(dirs []*config.Directory, filePath string) *config.Directory {
	for i := range dirs {
		if dirs[i] == nil {
			continue
		}
		dirPath, err := filepath.Abs(ExpandHome(dirs[i].Path))
		if err != nil {
			continue
		}
		if HasPathPrefix(filePath, dirPath) {
			return dirs[i]
		}
	}
	return nil
}

// DirectoryMatchesPath reports whether filePath would be discovered while
// walking dir with its current filters. In addition to the file name filters,
// it checks every parent directory below the configured root because excluded
// and hidden directories are pruned during directory walks.
func DirectoryMatchesPath(dir *config.Directory, filePath string) bool {
	if dir == nil {
		return false
	}
	root, err := filepath.Abs(ExpandHome(dir.Path))
	if err != nil {
		return false
	}
	filePath = filepath.Clean(filePath)
	if !HasPathPrefix(filePath, root) || filePath == root {
		return false
	}
	rel, err := filepath.Rel(root, filePath)
	if err != nil {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		if shouldSkipDir(part, dir.Excludes, dir.IncludeHidden) {
			return false
		}
	}
	return dir.IsMatching(filePath)
}

// FindDirUser finds the directory config matching a file path and resolves its user to a user ID.
// Returns 0 for global directories (no user set). Returns an error if the username can't be resolved.
func FindDirUser(dirs []*config.Directory, filePath string) (uint, error) {
	dir := FindMatchingDir(dirs, filePath)
	if dir == nil {
		return 0, nil
	}
	if dir.User == "" {
		return 0, nil
	}
	u, err := model.GetUser(dir.User)
	if err != nil {
		return 0, fmt.Errorf("user %q not found", dir.User)
	}
	return u.ID, nil
}

// skipDirs lists directory names that are skipped by default during watching.
// These are well-known dependency/cache directories whose names are unambiguous
// and can contain tens of thousands of entries, easily exhausting OS watch limits.
// Hidden directories (starting with ".") are always skipped separately.
// Users can exclude additional directories via the per-directory excludes config.
var skipDirs = map[string]struct{}{
	"node_modules":     {},
	"bower_components": {},
	"jspm_packages":    {},
	"__pycache__":      {},
	"__pypackages__":   {},
}

// shouldSkipDir reports whether a directory should be excluded from watching.
// It skips hidden directories, well-known dependency/cache directories, and
// directories matching any exclude pattern from the config.
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

// ShouldSkipDir is the exported form of shouldSkipDir for use by the indexer.
func ShouldSkipDir(name string, excludes []string, includeHidden bool) bool {
	return shouldSkipDir(name, excludes, includeHidden)
}

func WatchDirectories(ctx context.Context, dirs []*config.Directory, callback func(string), onRemove func(string)) error {
	watcher, err := NewDirectoryWatcher(dirs)
	if err != nil {
		return err
	}
	defer func() {
		if err := watcher.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close file watcher")
		}
	}()
	return watcher.Run(ctx, callback, onRemove)
}
