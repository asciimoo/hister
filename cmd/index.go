package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/server/crawler"
	"github.com/asciimoo/hister/server/extractor"
	"github.com/asciimoo/hister/server/model"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var indexCmd = &cobra.Command{
	Use:   "index [URL...]",
	Short: "Index URLs or resume a persistent crawl job",
	Long:  "Index one or more URLs. Use --recursive to crawl linked pages, --input to create a persistent job from a file or standard input, or --job-id to resume a persistent job.",
	Args:  validateIndexArgs,
	PreRun: func(cmd *cobra.Command, args []string) {
		recursive, _ := cmd.Flags().GetBool("recursive")
		jobID, _ := cmd.Flags().GetString("job-id")
		input, _ := indexInput(cmd)
		if recursive || jobID != "" || input != "" {
			initDB()
		}
	},
	Run: func(cmd *cobra.Command, args []string) {
		resolvedArgs, err := resolveIndexURLs(cmd, args)
		if err != nil {
			exit(1, err.Error())
			return
		}
		args = resolvedArgs
		input, _ := indexInput(cmd)

		global, _ := cmd.Flags().GetBool("global")
		clientOpts := targetUserIDClientOptions(cmd, global)
		if allowSensitive, _ := cmd.Flags().GetBool("allow-sensitive"); allowSensitive {
			clientOpts = append(clientOpts, client.WithAllowSensitive())
		}

		force, _ := cmd.Flags().GetBool("force")
		recursive, _ := cmd.Flags().GetBool("recursive")
		jobID, _ := cmd.Flags().GetString("job-id")
		label, _ := cmd.Flags().GetString("label")
		noRobots, _ := cmd.Flags().GetBool("no-robots")
		cfg.Crawler.UserAgent = UserAgent
		applyCrawlerBackendFlags(cmd)
		if ua, _ := cmd.Flags().GetString("user-agent"); ua != "" {
			UserAgent = ua
			cfg.Crawler.UserAgent = ua
		}
		if cmd.Flags().Changed("delay") {
			d, _ := cmd.Flags().GetInt("delay")
			cfg.Crawler.Delay = d
		}
		if cmd.Flags().Changed("timeout") {
			t, _ := cmd.Flags().GetInt("timeout")
			cfg.Crawler.Timeout = t
		}

		var robotsCache *crawler.RobotsCache
		if !noRobots && !cfg.Crawler.NoRobots {
			cacheTTL := time.Duration(cfg.Crawler.Robots.CacheTTL) * time.Second
			robotsCache, err = crawler.NewRobotsCacheWithProxyAndTTL(cfg.Crawler.UserAgent, cfg.Crawler.Proxy, cacheTTL)
			if err != nil {
				exit(1, "Failed to configure robots.txt requests: "+err.Error())
				return
			}
		}

		if input != "" {
			validatorRules := &crawler.ValidatorRules{NoDepth: true}
			if recursive {
				validatorRules = crawlValidatorRules(cmd)
			}
			rulesJSON, err := crawler.MarshalValidatorRules(validatorRules)
			if err != nil {
				exit(1, "Failed to serialize validator rules: "+err.Error())
				return
			}
			jobID, err = model.CreateNamedCrawlJobWithURLs(
				indexInputJobName(input), args[0], rulesJSON, label, args,
			)
			if err != nil {
				exit(1, "Failed to create URL input crawl job: "+err.Error())
				return
			}
			fmt.Println("Starting crawl job:", jobID)
			if err := runPersistentIndexJob(cmd.Context(), jobID, args[0], validatorRules, label, robotsCache, force, clientOpts...); err != nil {
				exit(1, "Crawl failed: "+err.Error())
			}
			return
		}

		if recursive {
			// Persistent crawl mode (always).

			var (
				startURL       string
				validatorRules *crawler.ValidatorRules
			)

			// Generate a random job ID when none was given.
			if jobID == "" {
				var err error
				jobID, err = model.GenerateCrawlJobID()
				if err != nil {
					exit(1, "Failed to generate crawl job ID: "+err.Error())
					return
				}
			}

			existingJob, err := model.GetCrawlJob(jobID)
			if err != nil {
				exit(1, "Failed to load crawl job: "+err.Error())
				return
			}

			if existingJob == nil {
				// New job: require at least one URL.
				if len(args) == 0 {
					exit(1, "at least one URL is required to start a new crawl job")
					return
				}
				startURL = args[0]

				validatorRules = crawlValidatorRules(cmd)

				rulesJSON, err := crawler.MarshalValidatorRules(validatorRules)
				if err != nil {
					exit(1, "Failed to serialize validator rules: "+err.Error())
					return
				}
				if err := model.CreateCrawlJob(jobID, startURL, rulesJSON, label); err != nil {
					exit(1, "Failed to create crawl job: "+err.Error())
					return
				}
				fmt.Println("Starting crawl job:", jobID)
			} else {
				// Resume existing job.
				hasURLs, err := crawlJobHasURLsToCrawl(existingJob)
				if err != nil {
					exit(1, "Failed to load crawl job queue: "+err.Error())
					return
				}
				if !hasURLs {
					fmt.Println("No URLs to crawl for job:", jobID)
					return
				}
				startURL = existingJob.StartURL
				validatorRules, err = crawler.UnmarshalValidatorRules(existingJob.ValidatorRules)
				if err != nil {
					exit(1, "Failed to restore validator rules: "+err.Error())
					return
				}
				// Use stored label unless --label was explicitly overridden.
				if !cmd.Flags().Changed("label") {
					label = existingJob.Label
				}
				fmt.Println("Resuming crawl job:", jobID)
			}

			if err := runPersistentIndexJob(cmd.Context(), jobID, startURL, validatorRules, label, robotsCache, force, clientOpts...); err != nil {
				exit(1, "Crawl failed: "+err.Error())
			}
			return
		}

		// Resume an existing job by ID without --recursive.
		if jobID != "" {
			existingJob, err := model.GetCrawlJob(jobID)
			if err != nil {
				exit(1, "Failed to load crawl job: "+err.Error())
				return
			}
			if existingJob == nil {
				exit(1, "Crawl job not found: "+jobID+". Use --recursive to start a new job.")
				return
			}
			hasURLs, err := crawlJobHasURLsToCrawl(existingJob)
			if err != nil {
				exit(1, "Failed to load crawl job queue: "+err.Error())
				return
			}
			if !hasURLs {
				fmt.Println("No URLs to crawl for job:", jobID)
				return
			}

			validatorRules, err := crawler.UnmarshalValidatorRules(existingJob.ValidatorRules)
			if err != nil {
				exit(1, "Failed to restore validator rules: "+err.Error())
				return
			}
			// Use stored label unless --label was explicitly overridden.
			if !cmd.Flags().Changed("label") {
				label = existingJob.Label
			}
			fmt.Println("Resuming crawl job:", jobID)

			if err := runPersistentIndexJob(cmd.Context(), jobID, existingJob.StartURL, validatorRules, label, robotsCache, force, clientOpts...); err != nil {
				exit(1, "Crawl failed: "+err.Error())
			}
			return
		}

		// Plain index mode (no crawling).
		if len(args) == 0 {
			exit(1, "at least one URL is required")
			return
		}

		// Create the crawler once so the bidi backend reuses its
		// WebSocket connection and session across all URLs.
		cr, err := crawler.New(&cfg.Crawler, robotsCache)
		if err != nil {
			exit(1, "Failed to create crawler: "+err.Error())
			return
		}
		defer func() {
			if err := cr.Close(); err != nil {
				log.Warn().Err(err).Msg("crawler close error")
			}
		}()

		c := newClient(clientOpts...)
		for _, u := range args {
			if !force {
				exists, err := c.DocumentExists(u)
				if err != nil {
					log.Warn().Err(err).Str("URL", u).Msg("Failed to check if URL is already indexed")
				} else if exists {
					log.Info().Str("URL", u).Msg("URL already indexed, skipping (use --force to reindex)")
					continue
				}
			}
			if err := indexURL(cmd.Context(), cr, u, label, clientOpts...); err != nil {
				log.Warn().Err(err).Str("URL", u).Msg("Failed to index URL")
			}
		}
	},
}

func crawlJobHasURLsToCrawl(job *model.CrawlJob) (bool, error) {
	if job.Status == model.CrawlJobCompleted {
		return false, nil
	}
	stats, err := model.GetCrawlJobStats(job.ID)
	if err != nil {
		return false, err
	}
	return stats.Pending+stats.InProgress > 0, nil
}

func validateIndexArgs(cmd *cobra.Command, args []string) error {
	jobID, err := cmd.Flags().GetString("job-id")
	if err != nil {
		return err
	}
	input, err := indexInput(cmd)
	if err != nil {
		return err
	}
	if jobID != "" && input != "" {
		return errors.New("--job-id and --input cannot be used together")
	}
	if len(args) > 0 || jobID != "" || input != "" {
		return nil
	}
	return cobra.MinimumNArgs(1)(cmd, args)
}

func resolveIndexURLs(cmd *cobra.Command, args []string) ([]string, error) {
	input, err := indexInput(cmd)
	if err != nil {
		return nil, err
	}
	if input == "" {
		return args, nil
	}

	var contents []byte
	if input == "-" {
		contents, err = io.ReadAll(cmd.InOrStdin())
	} else {
		contents, err = os.ReadFile(input)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read input %q: %w", input, err)
	}
	urls := parseURLInput(string(contents))
	if len(urls) == 0 {
		if input == "-" {
			return nil, errors.New("standard input contains no URLs")
		}
		return nil, fmt.Errorf("input %q contains no URLs", input)
	}
	return urls, nil
}

func indexInput(cmd *cobra.Command) (string, error) {
	input, err := cmd.Flags().GetString("input")
	if err != nil {
		return "", err
	}
	legacy, err := cmd.Flags().GetString("url-list")
	if err != nil {
		return "", err
	}
	if input != "" && legacy != "" {
		return "", errors.New("--input and --url-list cannot be used together")
	}
	if input != "" {
		return input, nil
	}
	return legacy, nil
}

func indexInputJobName(input string) string {
	if input == "-" {
		return "stdin"
	}
	return filepath.Base(input)
}

func parseURLInput(contents string) []string {
	lines := strings.Split(contents, "\n")
	urls := make([]string, 0, len(lines))
	for _, line := range lines {
		if u := strings.TrimSpace(line); u != "" {
			urls = append(urls, u)
		}
	}
	return urls
}

func crawlValidatorRules(cmd *cobra.Command) *crawler.ValidatorRules {
	maxDepth, _ := cmd.Flags().GetInt("max-depth")
	maxLinks, _ := cmd.Flags().GetInt("max-links")
	allowedDomains, _ := cmd.Flags().GetStringArray("allowed-domain")
	excludeDomains, _ := cmd.Flags().GetStringArray("exclude-domain")
	allowedPatterns, _ := cmd.Flags().GetStringArray("allowed-pattern")
	excludePatterns, _ := cmd.Flags().GetStringArray("exclude-pattern")
	return &crawler.ValidatorRules{
		MaxDepth:        maxDepth,
		MaxLinks:        maxLinks,
		AllowedDomains:  allowedDomains,
		ExcludeDomains:  excludeDomains,
		AllowedPatterns: allowedPatterns,
		ExcludePatterns: excludePatterns,
	}
}

func runPersistentIndexJob(
	ctx context.Context,
	jobID string,
	startURL string,
	validatorRules *crawler.ValidatorRules,
	label string,
	robotsCache *crawler.RobotsCache,
	force bool,
	clientOpts ...client.Option,
) error {
	validator, err := crawler.NewValidator(validatorRules)
	if err != nil {
		return fmt.Errorf("invalid crawler rules: %w", err)
	}

	done, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLDone)
	if err != nil {
		return fmt.Errorf("count done URLs: %w", err)
	}
	failed, err := model.CountCrawlURLsByStatus(jobID, model.CrawlURLFailed)
	if err != nil {
		return fmt.Errorf("count failed URLs: %w", err)
	}
	validator.SetVisited(int(done + failed))

	cr, err := crawler.NewPersistent(&cfg.Crawler, jobID, robotsCache, crawlerSkipOptions(force, clientOpts...)...)
	if err != nil {
		return fmt.Errorf("initialize persistent crawler: %w", err)
	}
	defer func() {
		if err := cr.Close(); err != nil {
			log.Warn().Err(err).Msg("crawler close error")
		}
	}()

	return crawlAndIndex(ctx, jobID, startURL, cr, validator, label, clientOpts...)
}

func init() {
	indexCmd.Flags().String("label", "", "Label to attach to all indexed documents")
	indexCmd.Flags().Bool("force", false, "Reindex URLs even if they are already in the index. Already indexed URLs are skipped otherwise")
	indexCmd.Flags().BoolP("recursive", "r", false, "Recursively crawl linked pages")
	indexCmd.Flags().Int("max-depth", 0, "Maximum crawl depth (0 = unlimited)")
	indexCmd.Flags().Int("max-links", 0, "Maximum number of pages to visit (0 = unlimited)")
	indexCmd.Flags().StringArray("allowed-domain", nil, "Domain to allow during crawl (repeatable; empty = all)")
	indexCmd.Flags().StringArray("exclude-domain", nil, "Domain to exclude during crawl (repeatable)")
	indexCmd.Flags().StringArray("allowed-pattern", nil, "Regexp pattern URLs must match to be followed (repeatable; empty = all)")
	indexCmd.Flags().StringArray("exclude-pattern", nil, "Regexp pattern; matching URLs are skipped (repeatable)")
	indexCmd.Flags().Bool("global", false, "Make indexed documents available for all users (only for admins in multiuser mode)")
	indexCmd.Flags().Uint("user-id", 0, "Index documents under the given user ID (only for admins in multiuser mode)")
	indexCmd.Flags().String("input", "", "Read one URL per line from a file, or from standard input with -; creates a persistent crawl job and replaces positional URLs")
	indexCmd.Flags().String("url-list", "", "Deprecated alias for --input")
	if err := indexCmd.Flags().MarkDeprecated("url-list", "use --input instead"); err != nil {
		panic(err)
	}
	indexCmd.Flags().String("job-id", "", "Persistent crawl job ID; use with --recursive to start a new job or alone to resume an existing one")
	addCrawlerBackendFlags(indexCmd)
	indexCmd.Flags().Bool("no-robots", false, "Disable robots.txt compliance during crawling")
	indexCmd.Flags().Int("delay", 0, "Delay in seconds between requests (0 = no delay; overrides config)")
	indexCmd.Flags().Int("timeout", 0, "Request timeout in seconds (0 = 5s default; overrides config)")
	indexCmd.Flags().String("user-agent", "", "User-agent string for requests (overrides config)")
	indexCmd.Flags().Bool("allow-sensitive", false, "Skip sensitive content checks, allowing matching documents to be indexed")
}

func indexURL(ctx context.Context, cr crawler.Crawler, u string, label string, clientOpts ...client.Option) error {
	if u == "" {
		log.Warn().Msg("URL must not be empty")
		return nil
	}
	v, err := crawler.NewValidator(&crawler.ValidatorRules{MaxLinks: 1})
	if err != nil {
		return fmt.Errorf("failed to create validator: %w", err)
	}
	ch, err := cr.Crawl(ctx, u, v)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", u, err)
	}
	d, ok := <-ch
	if !ok {
		return fmt.Errorf("failed to fetch %s: no response", u)
	}
	if err := d.ProcessContext(ctx, nil, extractor.ExtractContext); err != nil {
		return fmt.Errorf("failed to process document: %w", err)
	}
	if d.Favicon == "" {
		if err := d.DownloadFavicon(UserAgent); err != nil {
			log.Debug().Err(err).Str("URL", d.URL).Msg("failed to download favicon")
		}
	}
	d.Label = label
	c := newClient(clientOpts...)
	if err := c.AddDocumentJSON(d); err != nil {
		return fmt.Errorf("failed to send page to hister: %w", err)
	}
	return nil
}

func crawlAndIndex(ctx context.Context, jobID string, startURL string, cr crawler.Crawler, v *crawler.Validator, label string, clientOpts ...client.Option) error {
	ch, err := cr.Crawl(ctx, startURL, v)
	if err != nil {
		return err
	}
	c := newClient(clientOpts...)
	for doc := range ch {
		if err := doc.ProcessContext(ctx, nil, extractor.ExtractContext); err != nil {
			log.Warn().Err(err).Str("url", doc.URL).Msg("failed to process crawled document")
			markPersistentIndexFailure(jobID, doc.URL, err)
			continue
		}
		if doc.Favicon == "" {
			if err := doc.DownloadFavicon(UserAgent); err != nil {
				log.Debug().Err(err).Str("url", doc.URL).Msg("failed to download favicon")
			}
		}
		doc.Label = label
		if err := c.AddDocumentJSON(doc); err != nil {
			log.Warn().Err(err).Str("url", doc.URL).Msg("failed to index crawled document")
			markPersistentIndexFailure(jobID, doc.URL, err)
		}
	}
	return nil
}

func markPersistentIndexFailure(jobID, rawURL string, err error) {
	if jobID == "" || rawURL == "" || err == nil {
		return
	}
	errCode := 0
	if httpErr, ok := errors.AsType[*client.HTTPError](err); ok {
		errCode = httpErr.StatusCode
	}
	if dbErr := model.MarkCrawlURLFailed(jobID, rawURL, errCode, err.Error()); dbErr != nil {
		log.Warn().Err(dbErr).Str("url", rawURL).Msg("failed to record persistent crawl indexing error")
	}
}

func crawlerSkipOptions(force bool, clientOpts ...client.Option) []crawler.Option {
	if force {
		return nil
	}
	c := newClient(clientOpts...)
	return []crawler.Option{
		crawler.WithSkipURLChecker(func(rawURL string) (bool, error) {
			exists, err := c.DocumentExists(rawURL)
			if err != nil || !exists {
				return exists, err
			}
			return true, nil
		}),
	}
}
