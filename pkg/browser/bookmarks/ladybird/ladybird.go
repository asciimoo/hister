// SPDX-License-Identifier: AGPL-3.0-or-later

package ladybird

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/asciimoo/hister/pkg/browser/bookmarks"
)

type Source struct{}

type bookmarkItem struct {
	Type     string         `json:"type"`
	URL      string         `json:"url"`
	Title    string         `json:"title"`
	Children []bookmarkItem `json:"children"`
}

type bookmarksFile struct {
	Items []bookmarkItem `json:"items"`
}

func (Source) Names() []string {
	return []string{"ladybird"}
}

func (Source) Accepts(path string) bool {
	return strings.HasSuffix(path, "Bookmarks.json")
}

func (s Source) Detect(browser string, find bookmarks.FindProfiles) []bookmarks.Store {
	var stores []bookmarks.Store
	seen := map[string]struct{}{}
	add := func(path string) {
		if !bookmarks.FileExists(path) {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		stores = append(stores, bookmarks.Store{Browser: "ladybird", Path: path, Source: s})
	}

	for _, db := range find("History", browser) {
		for _, path := range db.Paths {
			add(bookmarks.SiblingFile(path, "Bookmarks.json"))
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return stores
	}
	for _, cand := range []string{
		filepath.Join(home, ".config", "Ladybird", "Bookmarks.json"),
		filepath.Join(home, ".local", "share", "Ladybird", "Bookmarks.json"),
		filepath.Join(home, "Library", "Application Support", "Ladybird", "Bookmarks.json"),
		filepath.Join(home, ".var", "app", "org.ladybird.Ladybird", "config", "Ladybird", "Bookmarks.json"),
	} {
		add(cand)
	}
	for _, pattern := range []string{
		filepath.Join(home, ".config", "Ladybird", "Profiles", "*", "Bookmarks.json"),
		filepath.Join(home, ".local", "share", "Ladybird", "Profiles", "*", "Bookmarks.json"),
		filepath.Join(home, "Library", "Application Support", "Ladybird", "Profiles", "*", "Bookmarks.json"),
		filepath.Join(home, ".var", "app", "org.ladybird.Ladybird", "config", "Ladybird", "Profiles", "*", "Bookmarks.json"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range matches {
			add(path)
		}
	}
	return stores
}

func (Source) ListURLs(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ladybird bookmarks: %w", err)
	}
	var file bookmarksFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse ladybird bookmarks: %w", err)
	}
	return bookmarks.UniqueHTTPURLs(collectURLs(file.Items)), nil
}

func collectURLs(items []bookmarkItem) []string {
	var urls []string
	for _, item := range items {
		if strings.EqualFold(item.Type, "bookmark") {
			urls = append(urls, item.URL)
			continue
		}
		urls = append(urls, collectURLs(item.Children)...)
	}
	return urls
}
