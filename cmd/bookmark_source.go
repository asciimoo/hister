// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// bookmarkSource is one on-disk bookmark format. Add a new browser by
// implementing this and appending it to bookmarkSources.
type bookmarkSource interface {
	Names() []string
	Accepts(path string) bool
	Detect(browser string) []bookmarkStore
	ListURLs(path string) ([]string, error)
}

type bookmarkStore struct {
	browser string
	path    string
	source  bookmarkSource
}

func bookmarkSources() []bookmarkSource {
	return []bookmarkSource{
		firefoxBookmarkSource{},
		chromiumBookmarkSource{},
		ladybirdBookmarkSource{},
	}
}

func resolveBookmarkStores(browser, dbPath string) ([]bookmarkStore, error) {
	browser = strings.ToLower(strings.TrimSpace(browser))
	if dbPath != "" {
		src := bookmarkSourceAccepting(dbPath)
		if src == nil {
			return nil, fmt.Errorf("unsupported bookmark --db %s", dbPath)
		}
		if browser != "" && !bookmarkSourceHasName(src, browser) {
			return nil, fmt.Errorf("--browser %s does not match --db %s", browser, dbPath)
		}
		name := browser
		if name == "" {
			name = src.Names()[0]
		}
		return []bookmarkStore{{browser: name, path: dbPath, source: src}}, nil
	}

	var stores []bookmarkStore
	for _, src := range bookmarkSources() {
		if browser != "" && !bookmarkSourceHasName(src, browser) {
			continue
		}
		stores = append(stores, src.Detect(browser)...)
	}
	if browser != "" && bookmarkSourceByName(browser) == nil {
		return nil, fmt.Errorf("unknown --browser %s", browser)
	}
	if len(stores) == 0 {
		if browser != "" {
			return nil, fmt.Errorf("no bookmark store found for browser %s", browser)
		}
		return nil, fmt.Errorf("no bookmark stores found")
	}
	return stores, nil
}

func bookmarkSourceAccepting(path string) bookmarkSource {
	for _, src := range bookmarkSources() {
		if src.Accepts(path) {
			return src
		}
	}
	return nil
}

func bookmarkSourceByName(name string) bookmarkSource {
	for _, src := range bookmarkSources() {
		if bookmarkSourceHasName(src, name) {
			return src
		}
	}
	return nil
}

func bookmarkSourceHasName(src bookmarkSource, name string) bool {
	name = strings.ToLower(name)
	for _, n := range src.Names() {
		if n == name || strings.HasPrefix(name, n) || strings.HasPrefix(n, name) {
			return true
		}
	}
	return false
}

func isHTTPBookmarkURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

func uniqueHTTPBookmarkURLs(urls []string) []string {
	seen := make(map[string]struct{}, len(urls))
	var out []string
	for _, u := range urls {
		if !isHTTPBookmarkURL(u) {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func siblingFile(path, name string) string {
	return filepath.Join(filepath.Dir(path), name)
}

func historyDBs(table, browserPrefix string) []browserDB {
	var out []browserDB
	for _, db := range getDBPaths() {
		if db.table_name != table {
			continue
		}
		if browserPrefix != "" && !strings.HasPrefix(strings.ToLower(db.name), browserPrefix) {
			continue
		}
		out = append(out, db)
	}
	return out
}
