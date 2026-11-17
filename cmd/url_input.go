// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"errors"
	"fmt"

	"github.com/asciimoo/nogon/server/crawler"
	"github.com/asciimoo/nogon/server/model"

	"github.com/spf13/cobra"
)

func applyIndexCrawlerFlags(cmd *cobra.Command) {
	cfg.Crawler.UserAgent = UserAgent
	applyCrawlerBackendFlags(cmd)
	if ua, _ := cmd.Flags().GetString("user-agent"); ua != "" {
		UserAgent = ua
		cfg.Crawler.UserAgent = ua
	}
	if cmd.Flags().Changed("delay") {
		cfg.Crawler.Delay, _ = cmd.Flags().GetInt("delay")
	}
	if cmd.Flags().Changed("timeout") {
		cfg.Crawler.Timeout, _ = cmd.Flags().GetInt("timeout")
	}
}

func indexRobotsCache(cmd *cobra.Command) (*crawler.RobotsCache, error) {
	noRobots, _ := cmd.Flags().GetBool("no-robots")
	if noRobots || cfg.Crawler.NoRobots {
		return nil, nil
	}
	cache, err := crawler.NewRobotsCacheWithProxy(cfg.Crawler.UserAgent, cfg.Crawler.Proxy)
	if err != nil {
		return nil, fmt.Errorf("failed to configure robots.txt requests: %w", err)
	}
	return cache, nil
}

// runURLInputJob shares the persistent queue and result reporting for URL sources.
func runURLInputJob(cmd *cobra.Command, name string, urls []string, rules *crawler.ValidatorRules, robots *crawler.RobotsCache, report *failedURLReport) error {
	if len(urls) == 0 {
		return errors.New("no URLs found to import")
	}
	label, _ := cmd.Flags().GetString("label")
	force, _ := cmd.Flags().GetBool("force")
	global, _ := cmd.Flags().GetBool("global")
	clientOpts := targetUserIDClientOptions(cmd, global)
	clientOpts = append(clientOpts, documentSubmissionClientOptions(cmd)...)
	rulesJSON, err := crawler.MarshalValidatorRules(rules)
	if err != nil {
		return fmt.Errorf("failed to serialize validator rules: %w", err)
	}
	jobID, err := model.CreateNamedCrawlJobWithURLs(name, urls[0], rulesJSON, label, urls)
	if err != nil {
		return fmt.Errorf("failed to create URL input crawl job: %w", err)
	}
	cmd.PrintErrln("Starting crawl job:", jobID)
	err = runPersistentIndexJob(cmd.Context(), jobID, urls[0], rules, label, robots, force, clientOpts...)
	return finishPersistentIndex(cmd, jobID, report, err)
}

func addURLInputFlags(cmd *cobra.Command) {
	addOutputFormatFlag(cmd)
	cmd.Flags().String("failed-urls", "", "Write failed URLs to this file, one per line, replacing its contents")
	cmd.Flags().Bool("force", false, "Reindex URLs even if they are already in the index. Already indexed URLs are skipped otherwise")
	cmd.Flags().Bool("global", false, "Make indexed documents available for all users (only for admins in multiuser mode)")
	cmd.Flags().Uint("user-id", 0, "Index documents under the given user ID (only for admins in multiuser mode)")
	addCrawlerBackendFlags(cmd)
	cmd.Flags().Bool("no-robots", false, "Disable robots.txt compliance during crawling")
	cmd.Flags().Int("delay", 0, "Delay in seconds between requests (0 = no delay; overrides config)")
	cmd.Flags().Int("timeout", 0, "Request timeout in seconds (0 = 5s default; overrides config)")
	cmd.Flags().String("user-agent", "", "User agent string for requests (overrides config)")
	cmd.Flags().Bool("allow-sensitive", false, "Skip sensitive content checks, allowing matching documents to be indexed")
}
