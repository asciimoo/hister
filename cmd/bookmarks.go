// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/asciimoo/hister/server/crawler"
	"github.com/asciimoo/hister/server/model"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var importBookmarksCmd = &cobra.Command{
	Use:   "bookmarks",
	Short: "Import browser bookmarks",
	Long: `Import bookmarks from a supported browser.

Usage:
  hister import browser bookmarks
  hister import browser bookmarks --browser firefox
  hister import browser bookmarks --browser chrome
  hister import browser bookmarks --db ~/.mozilla/firefox/example.default/places.sqlite
  hister import browser bookmarks --db ~/.config/google-chrome/Default/Bookmarks
  hister import browser bookmarks --db ~/.config/Ladybird/Bookmarks.json

--browser limits autodetection to a supported browser.
--db imports a specific bookmark store.

Supported stores:
  firefox, zen, waterfox   places.sqlite (moz_bookmarks)
  chrome, chromium, brave, edge, vivaldi, opera   Bookmarks JSON
  ladybird                 Bookmarks.json

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

type urlImportGroup struct {
	name    string
	path    string
	urls    []string
	skipped int
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
	stores, err := resolveBookmarkStores(browser, dbPath)
	if err != nil {
		exit(1, err.Error())
	}

	loadBrowserImportSkipRules()
	isSkip := func(u string) bool {
		return !cfg.App.UserHandling && cfg.Rules.IsSkip(u)
	}

	var groups []urlImportGroup
	for _, store := range stores {
		urls, err := store.source.ListURLs(store.path)
		if err != nil {
			log.Warn().Err(err).Str("file", store.path).Msg("Skipping bookmark store")
			continue
		}
		group := urlImportGroup{name: store.browser, path: store.path}
		for _, u := range urls {
			if isSkip(u) {
				group.skipped++
				continue
			}
			group.urls = append(group.urls, u)
		}
		if len(group.urls) == 0 {
			log.Warn().Str("file", store.path).Msg("Skipping bookmark store with no URLs to import")
			continue
		}
		groups = append(groups, group)
	}
	if len(groups) == 0 {
		exit(1, "No URLs found to import")
	}
	importURLGroups(cmd, groups, browserImportKindBookmarks)
}

func loadBrowserImportSkipRules() {
	c := newClient()
	resp, err := c.FetchRules()
	if err != nil {
		log.Error().Err(err).Msg("Unable to obtain skip rules from server; using local ones instead")
		return
	}
	cfg.Rules.Skip.ReStrs = resp.Skip
	if err := cfg.Rules.Skip.Compile(); err != nil {
		log.Error().Err(err).Msg("Unable to compile skip rules from server")
	}
}

func importURLGroups(cmd *cobra.Command, groups []urlImportGroup, kind string) {
	chosen := multipleChoiceURLGroups(groups, importChoiceNoun(kind))

	jobPrefix, defaultLabel := browserImportIdentity(kind)
	defaultJobID := jobPrefix + time.Now().Format("2006-01-02")
	jobID, resumeExisting, err := chooseBrowserImportJobID(defaultJobID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to select browser import crawl job")
		return
	}
	job := &browserImportJob{
		id:            jobID,
		labelOverride: newDocumentLabelOverride(cmd),
	}
	job.label = job.labelOverride.resolve("", defaultLabel)
	if resumeExisting {
		if err := ensureBrowserImportJob(job, ""); err != nil {
			log.Error().Err(err).Msg("Failed to resume browser import crawl job")
			return
		}
	}

	for _, group := range chosen {
		batch := make([]string, 0, 500)
		for _, u := range group.urls {
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
		if len(batch) > 0 {
			if err := model.BulkInsertCrawlURLs(job.id, batch, 0); err != nil {
				log.Error().Err(err).Msg("Failed to add browser URLs to crawl job")
				return
			}
			job.enqueued += len(batch)
		}
		if group.skipped != 0 {
			log.Info().Msgf("Skipped %d URLs by rules", group.skipped)
		}
		log.Info().Str("job_id", job.id).Int("seen", len(group.urls)+group.skipped).Int("total", len(group.urls)).Msg("Browser URLs added to crawl job")
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

func multipleChoiceURLGroups(groups []urlImportGroup, noun string) []urlImportGroup {
	r := bufio.NewReader(os.Stdin)
	println("----Available " + noun + "----")
	for i, group := range groups {
		choice := fmt.Sprint(strconv.Itoa(i), "  |  ", group.name, "  ", group.path, "  urls: ", len(group.urls))
		if group.skipped > 0 {
			choice += fmt.Sprintf("  skipped by rules: %d", group.skipped)
		}
		println(choice)
	}
	println("==> " + noun + " to exclude: (eg: \"1 2 3\", browser name or leave empty to to import all)")
	print("==> ")

	s, _ := r.ReadString('\n')
	blacklists := strings.Split(strings.Trim(s, "\n"), " ")

	var selected []urlImportGroup
	for i, group := range groups {
		skip := false
		for _, blacklist := range blacklists {
			if strconv.Itoa(i) == blacklist || group.name == blacklist {
				skip = true
				break
			}
		}
		if !skip {
			selected = append(selected, group)
		}
	}
	return selected
}
