// SPDX-License-Identifier: AGPL-3.0-or-later

package chromium

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/asciimoo/hister/pkg/browser/bookmarks"
)

type Source struct{}

type bookmarkNode struct {
	Type     string         `json:"type"`
	Name     string         `json:"name"`
	URL      string         `json:"url"`
	Children []bookmarkNode `json:"children"`
}

type bookmarksFile struct {
	Roots map[string]bookmarkNode `json:"roots"`
}

func (Source) Names() []string {
	return []string{"chrome", "chromium", "brave", "edge", "vivaldi", "opera"}
}

func (Source) Accepts(path string) bool {
	return filepath.Base(path) == "Bookmarks"
}

func (s Source) Detect(browser string, find bookmarks.FindProfiles) []bookmarks.Store {
	var stores []bookmarks.Store
	for _, db := range find("urls", browser) {
		for _, path := range db.Paths {
			bookmarkPath := bookmarks.SiblingFile(path, "Bookmarks")
			if !bookmarks.FileExists(bookmarkPath) {
				continue
			}
			stores = append(stores, bookmarks.Store{
				Browser: strings.ToLower(db.Name),
				Path:    bookmarkPath,
				Source:  s,
			})
		}
	}
	return stores
}

func (Source) ListURLs(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read chromium bookmarks: %w", err)
	}
	var file bookmarksFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse chromium bookmarks: %w", err)
	}
	var urls []string
	for _, root := range file.Roots {
		urls = append(urls, collectURLs(root)...)
	}
	return bookmarks.UniqueHTTPURLs(urls), nil
}

func collectURLs(node bookmarkNode) []string {
	if strings.EqualFold(node.Type, "url") {
		return []string{node.URL}
	}
	var urls []string
	for _, child := range node.Children {
		urls = append(urls, collectURLs(child)...)
	}
	return urls
}
