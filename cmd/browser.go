// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/server/crawler"
	"github.com/asciimoo/hister/server/model"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var importBrowserCmd = &cobra.Command{
	Use:   "browser [BROWSER_TYPE] [DB_PATH]",
	Short: "Import Chrome, Firefox or auto-detect browsing history",
	Long: `Import browsing history from a supported browser.

Usage:
  hister import browser                        auto-detect all installed browsers
  hister import browser BROWSER_TYPE           auto-detect database path
  hister import browser DB_PATH                auto-detect browser type
  hister import browser BROWSER_TYPE DB_PATH   import a browser type with a specific database path

Browser types supported for automatic detection: firefox, chrome, chromium, brave, edge, vivaldi, opera, zen, waterfox, ladybird, safari

The Firefox URL database is usually located at ~/.mozilla/firefox/*.default/places.sqlite
The Chrome/Chromium URL database is usually located at ~/.config/chromium/Default/History
The Safari URL database is located at ~/Library/Safari/History.db (macOS only), and reading it
requires Full Disk Access for the terminal or application running hister

Use --start-date (format: YYYY-MM-DD) to only import URLs whose most recent
recorded visit is on or after the given date.
`,
	Args: cobra.RangeArgs(0, 2),
	PreRun: func(_ *cobra.Command, _ []string) {
		initDB(model.ReadWrite)
		initExtractor()
	},
	Run: importHistory,
}

type browserDBCandidates struct {
	name             string
	table_name       string
	paths_candidates []string
}

type browserDB struct {
	name       string
	table_name string
	paths      []string
}

type importHistoryMultipleChoicePrompt struct {
	choice  string
	urls    int
	skipped int
	db      *sql.DB
	q       string
	c       *client.Client
}

type browserImportPreparationIssue struct {
	databaseFile string
	query        string
	err          error
}

type DBToImport struct {
	name         string
	table        string
	databaseFile string
	browserType  string
	db           *sql.DB
	q            string
	c            *client.Client
	count        int
}

type browserImportJob struct {
	id            string
	startURL      string
	label         string
	labelOverride documentLabelOverride
	created       bool
	enqueued      int
}

const browserImportJobPrefix = "browser-import-"

var errNoBrowserURLs = errors.New("no URLs found to import")

func importHistory(cmd *cobra.Command, args []string) {
	// TODO: get skip rules from server
	cfg.Crawler.UserAgent = UserAgent
	applyCrawlerBackendFlags(cmd)

	startDate, err := browserImportStartDate(cmd)
	if err != nil {
		exit(1, err.Error())
	}

	switch len(args) {
	case 0:
		// Auto-detect all installed browsers.
		dbs := getDBPaths()
		if len(dbs) == 0 {
			log.Fatal().Msg("no browser databases found")
		}
		var databases []DBToImport
		for _, db := range dbs {
			for _, path := range db.paths {
				databases = append(databases, DBToImport{
					table:        db.table_name,
					databaseFile: path,
				})
			}
		}
		importDB(databases, cmd, startDate)

	case 1, 2:
		if len(args) == 1 {
			// check if args[0] is a file or not and call the correct function
			if _, err := os.Stat(args[0]); os.IsNotExist(err) {
				importBrowser(strings.ToLower(args[0]), cmd, startDate)
			} else {
				importHistoryFile(args[0], cmd, startDate)
			}
		} else {
			browser := args[0]
			table_name := browserTableName(browser)
			if table_name == "" {
				log.Warn().Msg(fmt.Sprintf("Unknown browser, couldn't auto detect table name using %s as table name", browser))
				table_name = browser
			}
			importDB([]DBToImport{
				{
					table:        table_name,
					databaseFile: args[1],
				},
			},
				cmd,
				startDate)
		}

	default:
		log.Fatal().Msg(cmd.Long)
	}
}

func importBrowser(browser string, cmd *cobra.Command, startDate *time.Time) {
	var found bool

	for _, db := range getDBPaths() {
		if strings.HasPrefix(strings.ToLower(db.name), browser) {
			found = true
			for _, path := range db.paths {
				importDB([]DBToImport{
					{
						table:        db.table_name,
						databaseFile: path,
					},
				},
					cmd,
					startDate)
			}
		}
	}
	if !found {
		log.Fatal().Str("browser", browser).Msg("no database found for browser")
	}
}

func importHistoryFile(file_path string, cmd *cobra.Command, startDate *time.Time) {
	table, err := detectHistoryTable(file_path)
	if err != nil {
		log.Fatal().Err(err).Str("file", file_path).Msg("Couldn't auto detect table")
	}

	importDB([]DBToImport{
		{
			table:        table,
			databaseFile: file_path,
		},
	},
		cmd,
		startDate)
}

// detectHistoryTable identifies a browser history database by the tables it contains.
//
// This used to guess from the filename, which cannot work: Safari and Ladybird both call their
// database History.db, so a Safari history was read as Ladybird's and failed with "no such table:
// History". Chrome's is called History with no extension, which the same check would have to
// distinguish by the absence of a suffix.
//
// What a database IS cannot be settled by what it is called, and the answer is inside it. Safari is
// checked first because it is the only one identified by a pair of tables, so a match is
// unambiguous.
func detectHistoryTable(path string) (_ string, err error) {
	// Before opening it, not after: a path-only import reaches here first, and without this the
	// driver's "unable to open database file" would arrive ahead of any explanation.
	if err := browserHistoryReadable(path); err != nil {
		return "", err
	}

	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?immutable=1&mode=ro", path))
	if err != nil {
		return "", fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type = 'table'")
	if err != nil {
		return "", fmt.Errorf("read schema: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	tables := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return "", err
		}
		// SQLite is case-insensitive about table names; the schema records whatever case created
		// them, so compare in one case and return the name this code expects to query.
		tables[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	switch {
	case tables["history_items"] && tables["history_visits"]:
		return "safari", nil
	case tables["moz_places"]:
		return "moz_places", nil
	case tables["urls"]:
		return "urls", nil
	case tables["history"]:
		return "History", nil
	}
	return "", errors.New("no recognised browser history table found")
}

func importDB(databases []DBToImport, cmd *cobra.Command, startDate *time.Time) {
	// Fetch skip rules from the server.
	c := newClient()
	resp, err := c.FetchRules()
	if err != nil {
		log.Error().Err(err).Msg("Unable to obtain skip rules from server; using local ones instead")
	} else {
		// TODO: let the user know that their local rules are being overwritten?
		cfg.Rules.Skip.ReStrs = resp.Skip
		if err := cfg.Rules.Skip.Compile(); err != nil {
			log.Error().Err(err).Msg("Unable to compile skip rules from server")
			return
		}
	}

	minVisit, err := cmd.Flags().GetInt("min-visit")
	if err != nil {
		log.Error().Err(err).Msg("Failed to read minimum visit count")
		return
	}
	dbsToImport, issues := prepareBrowserImports(databases, minVisit, startDate, func(u string) bool {
		return !cfg.App.UserHandling && cfg.Rules.IsSkip(u)
	})
	for _, issue := range issues {
		event := log.Warn().Str("file", issue.databaseFile)
		if issue.query != "" {
			event.Str("query", issue.query)
		}
		if errors.Is(issue.err, errNoBrowserURLs) {
			event.Msg("Skipping browser database with no URLs to import")
		} else {
			event.Err(issue.err).Msg("Skipping browser database")
		}
	}
	if len(dbsToImport) == 0 {
		exit(1, "No URLs found to import")
	}
	for i := range dbsToImport {
		dbsToImport[i].c = c
		db := dbsToImport[i].db
		defer func() {
			if err := db.Close(); err != nil {
				log.Warn().Err(err).Msg("failed to close database")
			}
		}()
	}

	chosen := multipleChoiceImport(dbsToImport)

	defaultJobID := browserImportJobPrefix + time.Now().Format("2006-01-02")
	jobID, resumeExisting, err := chooseBrowserImportJobID(defaultJobID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to select browser import crawl job")
		return
	}
	job := &browserImportJob{
		id:            jobID,
		labelOverride: newDocumentLabelOverride(cmd),
	}
	job.label = job.labelOverride.resolve("", "browser")
	if resumeExisting {
		if err := ensureBrowserImportJob(job, ""); err != nil {
			log.Error().Err(err).Msg("Failed to resume browser import crawl job")
			return
		}
	}

	for _, database := range chosen {
		q := database.q
		count := database.count
		db := database.db

		q += " ORDER BY visit_count DESC"

		rows, err := db.Query(q)
		if err != nil {
			log.Error().Err(err).Msg("Failed to execute database query")
			return
		}
		defer func() {
			if err := rows.Close(); err != nil {
				log.Warn().Err(err).Msg("failed to close database rows")
			}
		}()
		i := 0
		skippedByRules := 0
		batch := make([]string, 0, 500)
		for rows.Next() {
			i += 1
			var u string
			err = rows.Scan(&u)
			if err != nil {
				log.Error().Err(err).Msg("Failed to scan database row")
				return
			}
			// skip URLs only in single user environments
			if !cfg.App.UserHandling && cfg.Rules.IsSkip(u) {
				log.Debug().Str("URL", u).Msg("skip importing URL by rule")
				skippedByRules += 1
				continue
			}
			if err := ensureBrowserImportJob(job, u); err != nil {
				log.Error().Err(err).Msg("Failed to create browser import crawl job")
				return
			}
			batch = append(batch, u)
			if len(batch) >= cap(batch) {
				if err := model.BulkInsertCrawlURLs(job.id, batch, 0); err != nil {
					log.Error().Err(err).Msg("Failed to add browser URLs to crawl job")
					return
				}
				job.enqueued += len(batch)
				batch = batch[:0]
			}
		}
		if err := rows.Err(); err != nil {
			log.Error().Err(err).Msg("Failed to read browser URLs")
			return
		}
		if len(batch) > 0 {
			if err := model.BulkInsertCrawlURLs(job.id, batch, 0); err != nil {
				log.Error().Err(err).Msg("Failed to add browser URLs to crawl job")
				return
			}
			job.enqueued += len(batch)
		}
		if skippedByRules != 0 {
			log.Info().Msgf("Skipped %d URLs by rules", skippedByRules)
		}
		log.Info().Str("job_id", job.id).Int("seen", i).Int("total", count).Msg("Browser URLs added to crawl job")
	}

	if !job.created {
		exit(1, "No URLs found to import")
	}
	storedJob, err := model.GetCrawlJob(job.id)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load browser import crawl job")
		return
	}
	if storedJob == nil {
		log.Error().Str("job_id", job.id).Msg("Browser import crawl job not found")
		return
	}
	hasURLs, err := crawlJobHasURLsToCrawl(storedJob)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load browser import crawl job queue")
		return
	}
	if !hasURLs {
		fmt.Println("No URLs to crawl for job:", job.id)
		return
	}

	cliPrintln(cliBoldStyle.Render("IMPORTING"))
	fmt.Println("Starting crawl job:", job.id)

	cfg.Crawler.UserAgent = UserAgent
	cr, err := crawler.NewPersistent(&cfg.Crawler, job.id, nil, crawlerSkipOptions(false)...)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize persistent crawler")
	}
	defer func() {
		if err := cr.Close(); err != nil {
			log.Warn().Err(err).Msg("crawler close error")
		}
	}()

	validatorRules := &crawler.ValidatorRules{NoDepth: true}
	validator, err := crawler.NewValidator(validatorRules)
	if err != nil {
		log.Fatal().Err(err).Msg("Invalid browser import crawler rules")
	}
	done, err := model.CountCrawlURLsByStatus(job.id, model.CrawlURLDone)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to count done browser import URLs")
	}
	failed, err := model.CountCrawlURLsByStatus(job.id, model.CrawlURLFailed)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to count failed browser import URLs")
	}
	validator.SetVisited(int(done + failed))

	if err := crawlAndIndex(cmd.Context(), job.id, job.startURL, cr, validator, job.label); err != nil {
		log.Fatal().Err(err).Msg("Browser import crawl failed")
	}
}

// browserHistorySource describes where a browser keeps the two things an import needs: the URL,
// and when that URL was last visited.
//
// Most browsers keep both in one flat table, so from is empty and the table name is used as the
// FROM clause directly. Safari does not: it holds URLs in history_items and one row per visit in
// history_visits, so it needs a join. Expressing that as a derived table keeps every other
// browser's query byte for byte what it was.
type browserHistorySource struct {
	// from is the FROM clause. Empty means the table name itself.
	from string
	// column is the last-visit timestamp as exposed by from.
	column string
	// unitsPerSecond and epochOffsetSeconds convert a Unix timestamp into the browser's own
	// representation: (unix + epochOffsetSeconds) * unitsPerSecond.
	unitsPerSecond     int64
	epochOffsetSeconds int64
}

// fromExpr returns the FROM clause for a source reached under table.
func (s browserHistorySource) fromExpr(table string) string {
	if s.from != "" {
		return s.from
	}
	return table
}

var browserHistorySources = map[string]browserHistorySource{
	"history": {
		column:         "last_visited_time",
		unitsPerSecond: 1_000,
	},
	"moz_places": {
		column:         "last_visit_date",
		unitsPerSecond: 1_000_000,
	},
	"urls": {
		column:             "last_visit_time",
		unitsPerSecond:     1_000_000,
		epochOffsetSeconds: 11_644_473_600,
	},
	// Safari, and the reason from exists.
	//
	// The visits are reduced to the most recent one per URL so the derived table has the same
	// shape as every other browser's: one row per URL, carrying visit_count so --min-visit
	// keeps working.
	//
	// Its epoch offset is NEGATIVE because Safari counts from 2001-01-01, the Core Data epoch,
	// rather than from 1970 — a Unix timestamp is 978,307,200 seconds further along than the
	// same moment expressed in Safari's terms.
	"safari": {
		from: "(SELECT history_items.url AS url, " +
			"history_items.visit_count AS visit_count, " +
			"MAX(history_visits.visit_time) AS last_visit_time " +
			"FROM history_items " +
			"JOIN history_visits ON history_visits.history_item = history_items.id " +
			"GROUP BY history_items.id) AS safari_history",
		column:             "last_visit_time",
		unitsPerSecond:     1,
		epochOffsetSeconds: -978_307_200,
	},
}

func prepareBrowserImports(
	databases []DBToImport,
	minVisit int,
	startDate *time.Time,
	isSkip func(string) bool,
) ([]importHistoryMultipleChoicePrompt, []browserImportPreparationIssue) {
	choices := make([]importHistoryMultipleChoicePrompt, 0, len(databases))
	var issues []browserImportPreparationIssue
	for _, database := range databases {
		q, err := browserImportURLQuery(database.table, minVisit, startDate)
		if err != nil {
			issues = append(issues, browserImportPreparationIssue{
				databaseFile: database.databaseFile,
				err:          fmt.Errorf("create browser history query: %w", err),
			})
			continue
		}

		if err := browserHistoryReadable(database.databaseFile); err != nil {
			issues = append(issues, browserImportPreparationIssue{
				databaseFile: database.databaseFile,
				query:        q,
				err:          fmt.Errorf("open database: %w", err),
			})
			continue
		}

		db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?immutable=1&mode=ro", database.databaseFile))
		if err != nil {
			issues = append(issues, browserImportPreparationIssue{
				databaseFile: database.databaseFile,
				query:        q,
				err:          fmt.Errorf("open database: %w", err),
			})
			continue
		}

		count, skipped, err := countBrowserImportURLs(db, q, isSkip)
		if err != nil {
			_ = db.Close()
			issues = append(issues, browserImportPreparationIssue{
				databaseFile: database.databaseFile,
				query:        q,
				err:          fmt.Errorf("execute counting query: %w", err),
			})
			continue
		}
		if count < 1 {
			_ = db.Close()
			issues = append(issues, browserImportPreparationIssue{
				databaseFile: database.databaseFile,
				query:        q,
				err:          errNoBrowserURLs,
			})
			continue
		}

		choices = append(choices, importHistoryMultipleChoicePrompt{
			choice:  database.databaseFile,
			urls:    count,
			skipped: skipped,
			db:      db,
			q:       q,
		})
	}
	return choices, issues
}

func browserImportStartDate(cmd *cobra.Command) (*time.Time, error) {
	value, err := cmd.Flags().GetString("start-date")
	if err != nil || value == "" {
		return nil, err
	}
	startDate, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, fmt.Errorf("invalid --start-date: %w", err)
	}
	return &startDate, nil
}

// browserHistoryReadable reports why a history database cannot be read, or nil if it can.
//
// sql.Open is lazy, so a file the process may not read fails much later and much less clearly —
// the driver reports "unable to open database file", which sends people looking at file
// permissions. On Safari's history that is the wrong place to look: the directory is protected by
// macOS privacy controls and no chmod will help. Opening the file here turns that into an answer.
//
// The hint is limited to Safari deliberately. A permission error on a Chrome profile is an ordinary
// permission error, and telling somebody to open the Full Disk Access pane would send them off to
// change a system setting that was never the problem.
func browserHistoryReadable(path string) error {
	f, err := os.Open(path)
	if err == nil {
		return f.Close()
	}
	if runtime.GOOS == "darwin" && errors.Is(err, os.ErrPermission) && isSafariHistoryPath(path) {
		return fmt.Errorf(
			"%w: reading Safari's history requires Full Disk Access for the terminal or "+
				"application running hister (System Settings > Privacy & Security > Full Disk Access)",
			err,
		)
	}
	return err
}

// isSafariHistoryPath reports whether a path is inside Safari's protected data directory.
func isSafariHistoryPath(path string) bool {
	return strings.Contains(filepath.ToSlash(strings.ToLower(path)), "/library/safari/")
}

func browserImportURLQuery(table string, minVisit int, startDate *time.Time) (string, error) {
	// An unknown table is still usable: it is passed through as the FROM clause, which is what
	// lets a caller name a table this code has never heard of. Only date filtering needs to know
	// the schema, so only date filtering fails on one.
	source, known := browserHistorySources[strings.ToLower(table)]
	q := fmt.Sprintf(
		"SELECT DISTINCT url FROM %s WHERE (url LIKE 'http://%%' OR url LIKE 'https://%%')",
		source.fromExpr(table),
	)
	if minVisit > 1 {
		q += fmt.Sprintf(" AND visit_count >= %d", minVisit)
	}
	if startDate == nil {
		return q, nil
	}

	if !known {
		return "", fmt.Errorf("start date filtering is not supported for browser history table %q", table)
	}
	startTimestamp := (startDate.Unix() + source.epochOffsetSeconds) * source.unitsPerSecond
	q += fmt.Sprintf(" AND %s >= %d", source.column, startTimestamp)
	return q, nil
}

func countBrowserImportURLs(db *sql.DB, query string, isSkip func(string) bool) (count, skipped int, err error) {
	rows, err := db.Query(query)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return 0, 0, err
		}
		if isSkip != nil && isSkip(u) {
			skipped++
			continue
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	return count, skipped, nil
}

func ensureBrowserImportJob(job *browserImportJob, startURL string) error {
	if job.created {
		return nil
	}
	rules := &crawler.ValidatorRules{NoDepth: true}
	rulesJSON, err := crawler.MarshalValidatorRules(rules)
	if err != nil {
		return fmt.Errorf("serialize browser import crawler rules: %w", err)
	}
	existing, err := model.GetCrawlJob(job.id)
	if err != nil {
		return fmt.Errorf("load crawl job: %w", err)
	}
	if existing == nil {
		if err := model.CreateCrawlJob(job.id, startURL, rulesJSON, job.label); err != nil {
			return fmt.Errorf("create crawl job: %w", err)
		}
		job.startURL = startURL
		job.created = true
		return nil
	}
	existingRules, err := crawler.UnmarshalValidatorRules(existing.ValidatorRules)
	if err != nil {
		return fmt.Errorf("restore crawl job rules: %w", err)
	}
	if !existingRules.NoDepth {
		return fmt.Errorf("crawl job %q already exists and is not a browser import job", job.id)
	}
	job.label = job.labelOverride.resolve(existing.Label, "browser")
	if err := model.UpdateCrawlJobStatus(job.id, model.CrawlJobRunning); err != nil {
		return fmt.Errorf("update crawl job status: %w", err)
	}
	job.startURL = existing.StartURL
	job.created = true
	return nil
}

func chooseBrowserImportJobID(defaultID string) (string, bool, error) {
	jobs, err := model.ListCrawlJobs()
	if err != nil {
		return "", false, fmt.Errorf("list crawl jobs: %w", err)
	}
	browserJobs := browserImportJobs(jobs)
	if len(browserJobs) == 0 {
		id, err := nextBrowserImportJobID(defaultID)
		return id, false, err
	}
	if selected := promptBrowserImportJob(browserJobs, defaultID); selected != "" {
		return selected, true, nil
	}
	id, err := nextBrowserImportJobID(defaultID)
	return id, false, err
}

func browserImportJobs(jobs []*model.CrawlJob) []*model.CrawlJob {
	var browserJobs []*model.CrawlJob
	for _, job := range jobs {
		if !strings.HasPrefix(job.ID, browserImportJobPrefix) {
			continue
		}
		rules, err := crawler.UnmarshalValidatorRules(job.ValidatorRules)
		if err != nil {
			log.Warn().Err(err).Str("job_id", job.ID).Msg("failed to restore crawl job rules")
			continue
		}
		if !rules.NoDepth {
			continue
		}
		browserJobs = append(browserJobs, job)
	}
	return browserJobs
}

func promptBrowserImportJob(jobs []*model.CrawlJob, defaultID string) string {
	r := bufio.NewReader(os.Stdin)
	if len(jobs) == 1 {
		job := jobs[0]
		fmt.Println("Existing browser import job found:")
		printBrowserImportJob(1, job)
		if yesNoPrompt(fmt.Sprintf("Continue this job instead of creating %s?", defaultID), true) {
			return job.ID
		}
		return ""
	}

	fmt.Println("Existing browser import jobs found:")
	for i, job := range jobs {
		printBrowserImportJob(i+1, job)
	}
	fmt.Printf("Choose job number to continue, or press enter to create %s: ", defaultID)
	answer, _ := r.ReadString('\n')
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return ""
	}
	selected, err := strconv.Atoi(answer)
	if err != nil || selected < 1 || selected > len(jobs) {
		fmt.Println("Invalid selection, creating a new browser import job.")
		return ""
	}
	return jobs[selected-1].ID
}

func printBrowserImportJob(idx int, job *model.CrawlJob) {
	stats, err := model.GetCrawlJobStats(job.ID)
	if err != nil {
		log.Warn().Err(err).Str("job_id", job.ID).Msg("failed to get job stats")
	}
	fmt.Printf("%d  %s  %s\n", idx, job.ID, crawlJobStatusLabel(job.Status))
	fmt.Printf(
		"   pending: %d  done: %d  failed: %d  skipped: %d  created: %s\n",
		stats.Pending, stats.Done, stats.Failed, stats.Skipped,
		job.CreatedAt.Format("2006-01-02 15:04:05"),
	)
}

func nextBrowserImportJobID(baseID string) (string, error) {
	job, err := model.GetCrawlJob(baseID)
	if err != nil {
		return "", fmt.Errorf("load crawl job: %w", err)
	}
	if job == nil {
		return baseID, nil
	}
	for i := 2; ; i++ {
		id := fmt.Sprintf("%s-%d", baseID, i)
		job, err := model.GetCrawlJob(id)
		if err != nil {
			return "", fmt.Errorf("load crawl job: %w", err)
		}
		if job == nil {
			return id, nil
		}
	}
}

func getDBPaths() []browserDB {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var candidates []browserDBCandidates

	chromium_table := "urls"
	firefox_table := "moz_places"
	ladybird_table := "History"
	safari_table := "safari"

	switch runtime.GOOS {
	default:
		log.Fatal().Msgf("Failed to detect os")
	case "darwin":
		candidates = []browserDBCandidates{
			// safari
			//
			// One fixed location, with no profile directories to glob: Safari keeps a single
			// history database per user. Reading it requires Full Disk Access — see
			// browserHistoryReadable.
			{
				"Safari",
				safari_table,
				[]string{
					filepath.Join(home, "Library", "Safari", "History.db"),
				},
			},
			// firefox
			{
				"Firefox",
				firefox_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles", "*.default*", "places.sqlite"),
					filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles", "*.default-release*", "places.sqlite"),
				},
			},
			{
				"Firefox Developer Edition",
				firefox_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles", "*.dev-edition-default*", "places.sqlite"),
				},
			},
			{
				"Zen",
				firefox_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "zen", "Profiles", "*Default*", "places.sqlite"),
				},
			},
			{
				"Waterfox",
				firefox_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "Waterfox", "Profiles", "*.default*", "places.sqlite"),
				},
			},
			{
				"Ladybird",
				ladybird_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "Ladybird", "History.db"),
				},
			},
			{
				"Chrome",
				chromium_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default", "History"),
					filepath.Join(home, "Library", "Application Support", "Google", "Chrome Beta", "Default", "History"),
					filepath.Join(home, "Library", "Application Support", "Google", "Chrome Canary", "Default", "History"),
				},
			},
			{
				"Chromium",
				chromium_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "Chromium", "Default", "History"),
				},
			},
			{
				"Brave",
				chromium_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "BraveSoftware", "Brave-Browser", "Default", "History"),
					filepath.Join(home, "Library", "Application Support", "BraveSoftware", "Brave-Browser-Beta", "Default", "History"),
				},
			},
			{
				"Edge",
				chromium_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "Microsoft Edge", "Default", "History"),
					filepath.Join(home, "Library", "Application Support", "Microsoft Edge Beta", "Default", "History"),
				},
			},
			{
				"Vivaldi",
				chromium_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "Vivaldi", "Default", "History"),
				},
			},
			{
				"Opera",
				chromium_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "com.operasoftware.Opera", "Default", "History"),
				},
			},
		}
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		appData := os.Getenv("APPDATA")
		if localAppData != "" {
			candidates = []browserDBCandidates{
				{
					"firefox",
					firefox_table,
					[]string{
						filepath.Join(appData, "Mozilla", "Firefox", "Profiles", "*.default*", "places.sqlite"),
						filepath.Join(appData, "Mozilla", "Firefox", "Profiles", "*.default-release*", "places.sqlite"),
					},
				},
				{
					"Zen",
					firefox_table,
					[]string{
						filepath.Join(appData, "zen", "Profiles", "*.Default*", "places.sqlite"),
					},
				},
				{
					"Waterfox",
					firefox_table,
					[]string{
						filepath.Join(appData, "Waterfox", "Profiles", "*.default*", "places.sqlite"),
					},
				},
				{
					"Chrome",
					chromium_table,
					[]string{
						filepath.Join(localAppData, "Google", "Chrome", "User Data", "Default", "History"),
						filepath.Join(localAppData, "Google", "Chrome Beta", "User Data", "Default", "History"),
					},
				},
				{
					"Chromium",
					chromium_table,
					[]string{
						filepath.Join(localAppData, "Chromium", "User Data", "Default", "History"),
					},
				},
				{
					"Brave",
					chromium_table,
					[]string{
						filepath.Join(localAppData, "BraveSoftware", "Brave-Browser", "User Data", "Default", "History"),
					},
				},
				{
					"Edge",
					chromium_table,
					[]string{
						filepath.Join(localAppData, "Microsoft", "Edge", "User Data", "Default", "History"),
					},
				},
				{
					"Vivaldi",
					chromium_table,
					[]string{
						filepath.Join(localAppData, "Vivaldi", "User Data", "Default", "History"),
					},
				},
				{
					"Opera",
					chromium_table,
					[]string{
						filepath.Join(appData, "Opera Software", "Opera Stable", "History"),
					},
				},
			}
		}
	case "linux":
		candidates = []browserDBCandidates{
			{
				"firefox",
				firefox_table,
				[]string{
					filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox", "*.default*", "places.sqlite"),
					filepath.Join(home, ".mozilla", "firefox", "*.default*", "places.sqlite"),
				},
			},
			{
				"Firefox Developer Edition",
				firefox_table,
				[]string{
					filepath.Join(home, ".mozilla", "firefox", "*.dev-edition-default*", "places.sqlite"),
				},
			},
			{
				"Zen",
				firefox_table,
				[]string{
					filepath.Join(home, ".zen", "*.Default*", "places.sqlite"),
					filepath.Join(home, ".config", "zen", "*.Default*", "places.sqlite"),
				},
			},
			{
				"Waterfox",
				firefox_table,
				[]string{
					filepath.Join(home, ".waterfox", "Profiles", "*.default*", "places.sqlite"),
				},
			},
			{
				"Ladybird",
				ladybird_table,
				[]string{
					filepath.Join(home, ".local", "share", "Ladybird", "History.db"),
				},
			},
			{
				"Chrome",
				chromium_table,
				[]string{
					filepath.Join(home, ".config", "google-chrome", "Default", "History"),
					filepath.Join(home, ".config", "google-chrome-beta", "Default", "History"),
				},
			},
			{
				"Chromium",
				chromium_table,
				[]string{
					filepath.Join(home, ".config", "chromium", "Default", "History"),
					filepath.Join(home, "snap", "chromium", "common", "chromium", "Default", "History"),
				},
			},
			{
				"Brave",
				chromium_table,
				[]string{
					filepath.Join(home, ".config", "BraveSoftware", "Brave-Browser", "Default", "History"),
				},
			},
			{
				"Edge",
				chromium_table,
				[]string{
					filepath.Join(home, ".config", "microsoft-edge", "Default", "History"),
					filepath.Join(home, ".config", "microsoft-edge-beta", "Default", "History"),
				},
			},
			{
				"Vivaldi",
				chromium_table,
				[]string{
					filepath.Join(home, ".config", "vivaldi", "Default", "History"),
				},
			},
			{
				"Opera",
				chromium_table,
				[]string{
					filepath.Join(home, ".config", "opera", "Default", "History"),
				},
			},
		}
	}

	var dbFiles []browserDB
	var paths []string

	for _, candidate := range candidates {
		for _, globs := range candidate.paths_candidates {
			matches, _ := filepath.Glob(globs)
			for _, p := range matches {
				if _, err := os.Stat(p); err == nil {
					paths = append(paths, p)
				}
			}
		}

		if len(paths) != 0 {
			dbFiles = append(dbFiles, browserDB{candidate.name, candidate.table_name, paths})
		}
		paths = []string{}
	}
	return dbFiles
}

func browserTableName(browser string) string {
	switch strings.ToLower(browser) {
	case "firefox", "zen", "waterfox":
		return "moz_places"
	case "chrome", "chromium", "brave", "edge", "vivaldi", "opera":
		return "urls"
	case "ladybird":
		return "History"
	case "safari":
		return "safari"
	}
	return ""
}

func multipleChoiceImport(choices []importHistoryMultipleChoicePrompt) []DBToImport {
	r := bufio.NewReader(os.Stdin)
	var s string
	var returnDBs []DBToImport
	println("----Available Histories----")
	for i, choiceData := range choices {
		prefix := getBrowserType(choiceData.choice)
		choice := fmt.Sprint(strconv.Itoa(i), "  |  ", prefix, "  ", choiceData.choice, "  urls: ", choiceData.urls)
		if choiceData.skipped > 0 {
			choice += fmt.Sprintf("  skipped by rules: %d", choiceData.skipped)
		}
		println(choice)
		returnDBs = append(returnDBs, DBToImport{
			name:        prefix,
			browserType: prefix,
			count:       choiceData.urls,
			db:          choiceData.db,
			q:           choiceData.q,
			c:           choiceData.c,
		})
	}
	println("==> Histories to exclude: (eg: \"1 2 3\", browser name or leave empty to to import all)")
	print("==> ")

	s, _ = r.ReadString('\n')

	blacklists := strings.Split(strings.Trim(s, "\n"), " ")

	// Handle blacklisted imports
	var selected []DBToImport
	var unselected bool
	for i, data := range returnDBs {
		for _, blacklist := range blacklists {
			if strconv.Itoa(i) == blacklist || data.name == blacklist {
				unselected = true
				break
			}
		}
		if !unselected {
			selected = append(selected, data)
		}
		unselected = false
	}
	return selected
}

func getBrowserType(path string) string {
	path = strings.ToLower(path)
	if strings.Contains(path, "firefox") {
		return "firefox"
	} else if strings.Contains(path, "zen") {
		return "zen"
	} else if strings.Contains(path, "waterfox") {
		return "waterfox"
	} else if strings.Contains(path, "chrome") {
		return "chrome"
	} else if strings.Contains(path, "chromium") {
		return "chromium"
	} else if strings.Contains(path, "brave") {
		return "brave"
	} else if strings.Contains(path, "edge") {
		return "edge"
	} else if strings.Contains(path, "vivaldi") {
		return "vivaldi"
	} else if strings.Contains(path, "opera") {
		return "opera"
	} else if strings.Contains(path, "ladybird") {
		return "ladybird"
	} else if strings.Contains(path, "safari") {
		return "safari"
	} else {
		return "unknown"
	}
}
