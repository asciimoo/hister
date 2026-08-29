// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"fmt"
	"strings"

	"github.com/asciimoo/hister/pkg/browser/bookmarks"
	"github.com/asciimoo/hister/pkg/browser/bookmarks/chromium"
	"github.com/asciimoo/hister/pkg/browser/bookmarks/firefox"
	"github.com/asciimoo/hister/pkg/browser/bookmarks/ladybird"
)

func bookmarkSources() []bookmarks.Source {
	return []bookmarks.Source{
		firefox.Source{},
		chromium.Source{},
		ladybird.Source{},
	}
}

func findBookmarkProfiles(table, browserPrefix string) []bookmarks.Profile {
	var out []bookmarks.Profile
	for _, db := range historyDBs(table, browserPrefix) {
		out = append(out, bookmarks.Profile{Name: db.name, Paths: db.paths})
	}
	return out
}

func resolveBookmarkStores(browser, dbPath string) ([]bookmarks.Store, error) {
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
		return []bookmarks.Store{{Browser: name, Path: dbPath, Source: src}}, nil
	}

	var stores []bookmarks.Store
	for _, src := range bookmarkSources() {
		if browser != "" && !bookmarkSourceHasName(src, browser) {
			continue
		}
		stores = append(stores, src.Detect(browser, findBookmarkProfiles)...)
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

func bookmarkSourceAccepting(path string) bookmarks.Source {
	for _, src := range bookmarkSources() {
		if src.Accepts(path) {
			return src
		}
	}
	return nil
}

func bookmarkSourceByName(name string) bookmarks.Source {
	for _, src := range bookmarkSources() {
		if bookmarkSourceHasName(src, name) {
			return src
		}
	}
	return nil
}

func bookmarkSourceHasName(src bookmarks.Source, name string) bool {
	name = strings.ToLower(name)
	for _, n := range src.Names() {
		if n == name || strings.HasPrefix(n, name) {
			return true
		}
	}
	return false
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
