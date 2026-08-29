// SPDX-License-Identifier: AGPL-3.0-or-later

package bookmarks

import (
	"os"
	"path/filepath"
	"strings"
)

// Source is one on-disk bookmark format.
type Source interface {
	Names() []string
	Accepts(path string) bool
	Detect(browser string, find FindProfiles) []Store
	ListURLs(path string) ([]string, error)
}

type Store struct {
	Browser string
	Path    string
	Source  Source
}

type Profile struct {
	Name  string
	Paths []string
}

// FindProfiles returns profile dirs that have a history table of that name.
// table is moz_places / urls / History — same keys history import already uses.
type FindProfiles func(table, browserPrefix string) []Profile

func UniqueHTTPURLs(urls []string) []string {
	seen := make(map[string]struct{}, len(urls))
	var out []string
	for _, u := range urls {
		if !isHTTPURL(u) {
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

func FileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func SiblingFile(path, name string) string {
	return filepath.Join(filepath.Dir(path), name)
}

func isHTTPURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}
