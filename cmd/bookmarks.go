// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var importBookmarksCmd = &cobra.Command{
	Use:   "bookmarks [BROWSER_TYPE] [DB_PATH]",
	Short: "Import Firefox bookmarks",
	Long: `Import bookmarks from a Firefox-family places.sqlite database.

Usage:
  hister import bookmarks                        auto-detect Firefox, Zen, and Waterfox
  hister import bookmarks firefox                auto-detect the Firefox database path
  hister import bookmarks DB_PATH                import bookmarks from a places.sqlite file
  hister import bookmarks firefox DB_PATH        import bookmarks from a specific database

Bookmarks live in the same Firefox places.sqlite file as browsing history, in
the moz_bookmarks table. This command reads only bookmark rows (type=1), not
visited pages that were never bookmarked.

The pages are then fetched and indexed through the same crawl job used by
browser history import. Skip rules apply the same way; there is no extra
denylist for a browser's shipped default bookmarks.

Use --label LABEL to replace the default bookmarks label. --start-date is not
supported: it would drop bookmarks that have never been visited.
`,
	Args: cobra.RangeArgs(0, 2),
	PreRun: func(_ *cobra.Command, _ []string) {
		initDB()
		initExtractor()
	},
	Run: importBookmarks,
}

func importBookmarks(cmd *cobra.Command, args []string) {
	cfg.Crawler.UserAgent = UserAgent
	applyCrawlerBackendFlags(cmd)

	switch len(args) {
	case 0:
		dbs := bookmarkDBPaths()
		if len(dbs) == 0 {
			log.Fatal().Msg("no Firefox bookmark databases found")
		}
		importDB(bookmarkImportsFromDBs(dbs), cmd, nil, browserImportKindBookmarks)
	case 1:
		if _, err := os.Stat(args[0]); os.IsNotExist(err) {
			importBookmarksBrowser(strings.ToLower(args[0]), cmd)
			return
		}
		importBookmarksFile(args[0], cmd)
	case 2:
		browser := strings.ToLower(args[0])
		if !isFirefoxPlacesBrowser(browser) {
			log.Fatal().Str("browser", args[0]).Msg("bookmark import currently supports firefox, zen, and waterfox")
		}
		importBookmarksFile(args[1], cmd)
	}
}

func importBookmarksBrowser(browser string, cmd *cobra.Command) {
	if !isFirefoxPlacesBrowser(browser) {
		log.Fatal().Str("browser", browser).Msg("bookmark import currently supports firefox, zen, and waterfox")
	}
	var found bool
	for _, db := range bookmarkDBPaths() {
		if strings.HasPrefix(strings.ToLower(db.name), browser) {
			found = true
			importDB(bookmarkImportsFromDBs([]browserDB{db}), cmd, nil, browserImportKindBookmarks)
		}
	}
	if !found {
		log.Fatal().Str("browser", browser).Msg("no bookmark database found for browser")
	}
}

func importBookmarksFile(filePath string, cmd *cobra.Command) {
	if !strings.HasSuffix(filePath, "places.sqlite") {
		log.Fatal().Str("file", filePath).Msg("bookmark import expects a Firefox places.sqlite database")
	}
	importDB([]DBToImport{{
		table:        firefoxBookmarkTable,
		databaseFile: filePath,
	}}, cmd, nil, browserImportKindBookmarks)
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
