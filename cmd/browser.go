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
	Short: "Import browsing history or bookmarks from a supported browser",
	Long: `Import browsing history from a supported browser.

Use ` + "`hister import browser history`" + ` or ` + "`hister import browser bookmarks`" + ` for named-flag imports.
The bare ` + "`hister import browser`" + ` command still imports visit history.

Usage:
  hister import browser                        auto-detect all installed browsers
  hister import browser BROWSER_TYPE           auto-detect database path
  hister import browser DB_PATH                auto-detect browser type
  hister import browser BROWSER_TYPE DB_PATH   import a browser type with a specific database path

Browser types supported for automatic detection: firefox, chrome, chromium, brave, edge, vivaldi, opera, zen, waterfox, ladybird

The Firefox URL database is usually located at ~/.mozilla/firefox/*.default/places.sqlite
The Chrome/Chromium URL database is usually located at ~/.config/chromium/Default/History

Use --start-date (format: YYYY-MM-DD) to only import URLs whose most recent
recorded visit is on or after the given date.
`,
	Args: cobra.RangeArgs(0, 2),
	PreRun: func(_ *cobra.Command, _ []string) {
		initDB()
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

const (
	browserImportJobPrefix       = "browser-history-import-"
	bookmarkImportJobPrefix      = "browser-bookmark-import-"
	legacyBrowserImportJobPrefix = "browser-import-" // pre-#648 history jobs
	browserImportKindHistory     = "history"
	browserImportKindBookmarks   = "bookmarks"
)

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
		importDB(databases, cmd, startDate, browserImportKindHistory)

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
				startDate,
				browserImportKindHistory)
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
					startDate,
					browserImportKindHistory)
			}
		}
	}
	if !found {
		log.Fatal().Str("browser", browser).Msg("no database found for browser")
	}
}

func importHistoryFile(file_path string, cmd *cobra.Command, startDate *time.Time) {
	table, err := historyTableFromPath(file_path)
	if err != nil {
		log.Fatal().Str("file", file_path).Msg("Couldn't auto detect table")
	}

	importDB([]DBToImport{
		{
			table:        table,
			databaseFile: file_path,
		},
	},
		cmd,
		startDate,
		browserImportKindHistory)
}

func importDB(databases []DBToImport, cmd *cobra.Command, startDate *time.Time, kind string) {
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

	minVisit := 1
	// Every current caller registers min-visit; the nil check is so a future
	// import path without the flag still defaults to 1.
	if cmd.Flags().Lookup("min-visit") != nil {
		var err error
		minVisit, err = cmd.Flags().GetInt("min-visit")
		if err != nil {
			log.Error().Err(err).Msg("Failed to read minimum visit count")
			return
		}
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

	chosen := multipleChoiceImport(dbsToImport, importChoiceNoun(kind))

	job, err := beginBrowserImportJob(cmd, kind)
	if err != nil {
		log.Error().Err(err).Msg("Failed to select browser import crawl job")
		return
	}

	for _, database := range chosen {
		q := database.q
		count := database.count
		db := database.db

		q += browserImportOrderBy(kind)

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
			batch = append(batch, u)
			if len(batch) >= cap(batch) {
				if err := enqueueBrowserImportURLs(job, batch); err != nil {
					log.Error().Err(err).Msg("Failed to add browser URLs to crawl job")
					return
				}
				batch = batch[:0]
			}
		}
		if err := rows.Err(); err != nil {
			log.Error().Err(err).Msg("Failed to read browser URLs")
			return
		}
		if len(batch) > 0 {
			if err := enqueueBrowserImportURLs(job, batch); err != nil {
				log.Error().Err(err).Msg("Failed to add browser URLs to crawl job")
				return
			}
		}
		if skippedByRules != 0 {
			log.Info().Msgf("Skipped %d URLs by rules", skippedByRules)
		}
		log.Info().Str("job_id", job.id).Int("seen", i).Int("total", count).Msg("Browser URLs added to crawl job")
	}

	finishBrowserImportJob(cmd, job)
}

type browserHistoryTimestampSchema struct {
	column             string
	unitsPerSecond     int64
	epochOffsetSeconds int64
}

var browserHistoryTimestampSchemas = map[string]browserHistoryTimestampSchema{
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

func browserImportIdentity(kind string) (prefix, label string) {
	if kind == browserImportKindBookmarks {
		return bookmarkImportJobPrefix, "bookmarks"
	}
	return browserImportJobPrefix, "browser"
}

// browserImportMatchPrefixes is the set of job ID prefixes resume may offer.
// History still matches the released browser-import-* name so an upgrade can
// continue an in-flight import.
func browserImportMatchPrefixes(kind string) []string {
	if kind == browserImportKindBookmarks {
		return []string{bookmarkImportJobPrefix}
	}
	return []string{browserImportJobPrefix, legacyBrowserImportJobPrefix}
}

func importChoiceNoun(kind string) string {
	if kind == browserImportKindBookmarks {
		return "Bookmarks"
	}
	return "Histories"
}

func browserImportOrderBy(kind string) string {
	if kind == browserImportKindBookmarks {
		return " ORDER BY p.visit_count DESC"
	}
	return " ORDER BY visit_count DESC"
}

func browserImportURLQuery(table string, minVisit int, startDate *time.Time) (string, error) {
	q := fmt.Sprintf("SELECT DISTINCT url FROM %s WHERE (url LIKE 'http://%%' OR url LIKE 'https://%%')", table)
	if minVisit > 1 {
		q += fmt.Sprintf(" AND visit_count >= %d", minVisit)
	}
	if startDate == nil {
		return q, nil
	}

	schema, ok := browserHistoryTimestampSchemas[strings.ToLower(table)]
	if !ok {
		return "", fmt.Errorf("start date filtering is not supported for browser history table %q", table)
	}
	startTimestamp := (startDate.Unix() + schema.epochOffsetSeconds) * schema.unitsPerSecond
	q += fmt.Sprintf(" AND %s >= %d", schema.column, startTimestamp)
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

func beginBrowserImportJob(cmd *cobra.Command, kind string) (*browserImportJob, error) {
	jobPrefix, defaultLabel := browserImportIdentity(kind)
	defaultJobID := jobPrefix + time.Now().Format("2006-01-02")
	jobID, resumeExisting, err := chooseBrowserImportJobID(defaultJobID, browserImportMatchPrefixes(kind)...)
	if err != nil {
		return nil, err
	}
	job := &browserImportJob{
		id:            jobID,
		labelOverride: newDocumentLabelOverride(cmd),
	}
	job.label = job.labelOverride.resolve("", defaultLabel)
	if resumeExisting {
		if err := ensureBrowserImportJob(job, ""); err != nil {
			return nil, err
		}
	}
	return job, nil
}

func enqueueBrowserImportURLs(job *browserImportJob, urls []string) error {
	batch := make([]string, 0, 500)
	for _, u := range urls {
		if err := ensureBrowserImportJob(job, u); err != nil {
			return err
		}
		batch = append(batch, u)
		if len(batch) >= cap(batch) {
			if err := model.BulkInsertCrawlURLs(job.id, batch, 0); err != nil {
				return err
			}
			job.enqueued += len(batch)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := model.BulkInsertCrawlURLs(job.id, batch, 0); err != nil {
			return err
		}
		job.enqueued += len(batch)
	}
	return nil
}

func finishBrowserImportJob(cmd *cobra.Command, job *browserImportJob) {
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
	job.label = job.labelOverride.resolve(existing.Label, job.label)
	if err := model.UpdateCrawlJobStatus(job.id, model.CrawlJobRunning); err != nil {
		return fmt.Errorf("update crawl job status: %w", err)
	}
	job.startURL = existing.StartURL
	job.created = true
	return nil
}

func chooseBrowserImportJobID(defaultID string, prefixes ...string) (string, bool, error) {
	jobs, err := model.ListCrawlJobs()
	if err != nil {
		return "", false, fmt.Errorf("list crawl jobs: %w", err)
	}
	browserJobs := browserImportJobs(jobs, prefixes...)
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

func browserImportJobs(jobs []*model.CrawlJob, prefixes ...string) []*model.CrawlJob {
	var browserJobs []*model.CrawlJob
	for _, job := range jobs {
		if !hasAnyPrefix(job.ID, prefixes) {
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

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
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

	switch runtime.GOOS {
	default:
		log.Fatal().Msgf("Failed to detect os")
	case "darwin":
		candidates = []browserDBCandidates{
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
					filepath.Join(home, "Library", "Application Support", "Ladybird", "Profiles", "*", "History.db"),
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
					filepath.Join(home, ".local", "share", "Ladybird", "Profiles", "*", "History.db"),
					filepath.Join(home, ".var", "app", "org.ladybird.Ladybird", "data", "Ladybird", "History.db"),
					filepath.Join(home, ".var", "app", "org.ladybird.Ladybird", "data", "Ladybird", "Profiles", "*", "History.db"),
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
	}
	return ""
}

func multipleChoiceImport(choices []importHistoryMultipleChoicePrompt, noun string) []DBToImport {
	r := bufio.NewReader(os.Stdin)
	var s string
	var returnDBs []DBToImport
	println("----Available " + noun + "----")
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
	println("==> " + noun + " to exclude: (eg: \"1 2 3\", browser name or leave empty to to import all)")
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
	} else {
		return "unknown"
	}
}
