// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

type firefoxBookmarkSource struct{}

func (firefoxBookmarkSource) Names() []string {
	return []string{"firefox", "zen", "waterfox"}
}

func (firefoxBookmarkSource) Accepts(path string) bool {
	return strings.HasSuffix(path, "places.sqlite")
}

func (s firefoxBookmarkSource) Detect(browser string) []bookmarkStore {
	var stores []bookmarkStore
	for _, db := range historyDBs("moz_places", browser) {
		for _, path := range db.paths {
			stores = append(stores, bookmarkStore{
				browser: strings.ToLower(db.name),
				path:    path,
				source:  s,
			})
		}
	}
	return stores
}

func (firefoxBookmarkSource) ListURLs(path string) ([]string, error) {
	q, err := browserImportURLQuery(firefoxBookmarkTable, 1, nil)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?immutable=1&mode=ro", path))
	if err != nil {
		return nil, fmt.Errorf("open firefox places: %w", err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("query firefox bookmarks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var urls []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		urls = append(urls, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return uniqueHTTPBookmarkURLs(urls), nil
}
