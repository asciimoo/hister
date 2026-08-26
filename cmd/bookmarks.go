// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var importBookmarksCmd = &cobra.Command{
	Use:   "bookmarks",
	Short: "Import Firefox bookmarks",
	Long: `Import bookmarks from a Firefox-family places.sqlite database.

Usage:
  hister import browser bookmarks
  hister import browser bookmarks --browser firefox
  hister import browser bookmarks --db ~/.mozilla/firefox/example.default/places.sqlite
  hister import browser bookmarks --browser firefox --db /path/to/places.sqlite

--browser limits autodetection to firefox, zen, or waterfox.
--db imports a specific places.sqlite file.

Bookmarks live in the same Firefox places.sqlite file as browsing history, in
the moz_bookmarks table. This command reads only bookmark rows (type=1), not
visited pages that were never bookmarked.

The pages are then fetched and indexed through the same crawl job used by
browser history import. Skip rules apply the same way; there is no extra
denylist for a browser's shipped default bookmarks.

Use --label LABEL to replace the default bookmarks label. --start-date is not
supported: it would drop bookmarks that have never been visited.
`,
	Args: cobra.NoArgs,
	PreRun: func(_ *cobra.Command, _ []string) {
		initDB()
		initExtractor()
	},
	Run: importBookmarks,
}

func importBookmarks(cmd *cobra.Command, _ []string) {
	cfg.Crawler.UserAgent = UserAgent
	applyCrawlerBackendFlags(cmd)

	browser, err := cmd.Flags().GetString("browser")
	if err != nil {
		exit(1, err.Error())
	}
	dbPath, err := cmd.Flags().GetString("db")
	if err != nil {
		exit(1, err.Error())
	}
	databases, err := resolveBookmarkImports(browser, dbPath)
	if err != nil {
		exit(1, err.Error())
	}
	importDB(databases, cmd, nil, browserImportKindBookmarks)
}

func resolveBookmarkImports(browser, dbPath string) ([]DBToImport, error) {
	browser = strings.ToLower(strings.TrimSpace(browser))
	if browser != "" && !isFirefoxPlacesBrowser(browser) {
		return nil, fmt.Errorf("bookmark import currently supports --browser firefox, zen, or waterfox")
	}
	if dbPath != "" {
		if !strings.HasSuffix(dbPath, "places.sqlite") {
			return nil, fmt.Errorf("bookmark import --db expects a Firefox places.sqlite database")
		}
		return []DBToImport{{
			table:        firefoxBookmarkTable,
			databaseFile: dbPath,
		}}, nil
	}

	dbs := bookmarkDBPaths()
	if browser != "" {
		var filtered []browserDB
		for _, db := range dbs {
			if strings.HasPrefix(strings.ToLower(db.name), browser) {
				filtered = append(filtered, db)
			}
		}
		dbs = filtered
	}
	if len(dbs) == 0 {
		if browser != "" {
			return nil, fmt.Errorf("no bookmark database found for browser %s", browser)
		}
		return nil, fmt.Errorf("no Firefox bookmark databases found")
	}
	return bookmarkImportsFromDBs(dbs), nil
}

func bookmarkImportsFromDBs(dbs []browserDB) []DBToImport {
	var databases []DBToImport
	for _, db := range dbs {
		for _, path := range db.paths {
			databases = append(databases, DBToImport{
				table:        firefoxBookmarkTable,
				databaseFile: path,
			})
		}
	}
	return databases
}

func bookmarkDBPaths() []browserDB {
	var out []browserDB
	for _, db := range getDBPaths() {
		if db.table_name == "moz_places" {
			out = append(out, db)
		}
	}
	return out
}

func isFirefoxPlacesBrowser(name string) bool {
	switch strings.ToLower(name) {
	case "firefox", "zen", "waterfox":
		return true
	default:
		return false
	}
}
