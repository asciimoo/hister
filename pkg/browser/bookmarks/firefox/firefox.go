// SPDX-License-Identifier: AGPL-3.0-or-later

package firefox

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/asciimoo/hister/pkg/browser/bookmarks"

	_ "github.com/mattn/go-sqlite3"
)

const listURLsQuery = "SELECT DISTINCT p.url FROM moz_bookmarks b JOIN moz_places p ON p.id = b.fk WHERE b.type = 1 AND (p.url LIKE 'http://%' OR p.url LIKE 'https://%')"

type Source struct{}

func (Source) Names() []string {
	return []string{"firefox", "zen", "waterfox"}
}

func (Source) Accepts(path string) bool {
	return strings.HasSuffix(path, "places.sqlite")
}

func (s Source) Detect(browser string, find bookmarks.FindProfiles) []bookmarks.Store {
	var stores []bookmarks.Store
	for _, db := range find("moz_places", browser) {
		for _, path := range db.Paths {
			stores = append(stores, bookmarks.Store{
				Browser: strings.ToLower(db.Name),
				Path:    path,
				Source:  s,
			})
		}
	}
	return stores
}

func (Source) ListURLs(path string) ([]string, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?immutable=1&mode=ro", path))
	if err != nil {
		return nil, fmt.Errorf("open firefox places: %w", err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(listURLsQuery)
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
	return bookmarks.UniqueHTTPURLs(urls), nil
}
