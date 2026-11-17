// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/asciimoo/nogon/server/model"
)

var importBrowserHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Import browsing history",
	Long: `Import browsing history from a supported browser.

Usage:
  nogon import browser history
  nogon import browser history --browser firefox
  nogon import browser history --db ~/.mozilla/firefox/example.default/places.sqlite
  nogon import browser history --browser chrome --db ~/.config/google-chrome/Default/History

--browser limits autodetection to a supported browser name.
--db imports a specific history database (places.sqlite, History, or History.db).

Use --min-visit N to import only URLs visited at least N times.
Use --start-date YYYY-MM-DD to import only URLs whose most recent recorded
visit is on or after that date.
`,
	Args: cobra.NoArgs,
	PreRun: func(_ *cobra.Command, _ []string) {
		initDB(model.ReadWrite)
		initExtractor()
	},
	Run: importBrowserHistory,
}

func importBrowserHistory(cmd *cobra.Command, _ []string) {
	cfg.Crawler.UserAgent = UserAgent
	applyCrawlerBackendFlags(cmd)

	startDate, err := browserImportStartDate(cmd)
	if err != nil {
		exit(1, err.Error())
	}
	browser, err := cmd.Flags().GetString("browser")
	if err != nil {
		exit(1, err.Error())
	}
	dbPath, err := cmd.Flags().GetString("db")
	if err != nil {
		exit(1, err.Error())
	}
	databases, err := resolveHistoryImports(browser, dbPath)
	if err != nil {
		exit(1, err.Error())
	}
	importDB(databases, cmd, startDate, browserImportKindHistory)
}

func resolveHistoryImports(browser, dbPath string) ([]DBToImport, error) {
	browser = strings.ToLower(strings.TrimSpace(browser))
	if browser != "" && browserTableName(browser) == "" {
		return nil, fmt.Errorf("unknown --browser %s", browser)
	}
	if dbPath != "" {
		table := browserTableName(browser)
		if table == "" {
			var err error
			table, err = historyTableFromPath(dbPath)
			if err != nil {
				return nil, err
			}
		}
		return []DBToImport{{
			table:        table,
			databaseFile: dbPath,
		}}, nil
	}

	dbs := getDBPaths()
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
			return nil, fmt.Errorf("no history database found for browser %s", browser)
		}
		return nil, fmt.Errorf("no browser databases found")
	}
	var out []DBToImport
	for _, db := range dbs {
		for _, path := range db.paths {
			out = append(out, DBToImport{
				table:        db.table_name,
				databaseFile: path,
			})
		}
	}
	return out, nil
}

func historyTableFromPath(filePath string) (string, error) {
	// Filename suffixes cannot separate Safari from Ladybird (both History.db).
	// Reuse schema detection from the legacy import browser path.
	return detectHistoryTable(filePath)
}
