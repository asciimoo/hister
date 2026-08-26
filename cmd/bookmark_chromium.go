// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type chromiumBookmarkSource struct{}

type chromiumBookmarkNode struct {
	Type     string                 `json:"type"`
	Name     string                 `json:"name"`
	URL      string                 `json:"url"`
	Children []chromiumBookmarkNode `json:"children"`
}

type chromiumBookmarksFile struct {
	Roots map[string]chromiumBookmarkNode `json:"roots"`
}

func (chromiumBookmarkSource) Names() []string {
	return []string{"chrome", "chromium", "brave", "edge", "vivaldi", "opera"}
}

func (chromiumBookmarkSource) Accepts(path string) bool {
	return filepath.Base(path) == "Bookmarks"
}

func (s chromiumBookmarkSource) Detect(browser string) []bookmarkStore {
	var stores []bookmarkStore
	for _, db := range historyDBs("urls", browser) {
		for _, path := range db.paths {
			bookmarkPath := siblingFile(path, "Bookmarks")
			if !fileExists(bookmarkPath) {
				continue
			}
			stores = append(stores, bookmarkStore{
				browser: strings.ToLower(db.name),
				path:    bookmarkPath,
				source:  s,
			})
		}
	}
	return stores
}

func (chromiumBookmarkSource) ListURLs(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read chromium bookmarks: %w", err)
	}
	var file chromiumBookmarksFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse chromium bookmarks: %w", err)
	}
	var urls []string
	for _, root := range file.Roots {
		urls = append(urls, collectChromiumBookmarkURLs(root)...)
	}
	return uniqueHTTPBookmarkURLs(urls), nil
}

func collectChromiumBookmarkURLs(node chromiumBookmarkNode) []string {
	if strings.EqualFold(node.Type, "url") {
		return []string{node.URL}
	}
	var urls []string
	for _, child := range node.Children {
		urls = append(urls, collectChromiumBookmarkURLs(child)...)
	}
	return urls
}
