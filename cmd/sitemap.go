// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/server/crawler"
	"github.com/asciimoo/hister/server/model"

	"github.com/spf13/cobra"
)

var importSitemapCmd = &cobra.Command{
	Use:   "sitemap FILE_OR_URL",
	Short: "Import pages listed in an XML sitemap",
	Long: `Read an XML sitemap from a local file, an HTTP or HTTPS URL, or standard
input with -. Plain XML and gzip compressed sitemaps are supported.

Sitemap indexes are expanded by fetching their referenced sitemaps. All loc
values must be absolute HTTP or HTTPS URLs, even in local sitemap indexes.
Duplicate page URLs and repeated sitemap references are visited once.

Only pages listed in the sitemaps are indexed; links on those pages are not
followed. Existing documents are skipped unless --force is specified. Robots
rules and crawler settings apply. Sitemap XML is always fetched over HTTP;
--backend selects how the listed pages are fetched.

The pages are queued in a persistent crawl job in the local Nogon database
and submitted to the configured server. Resume an interrupted page import with
nogon index --job-id ID, using the job ID printed when indexing starts.
Sitemap discovery finishes before the job is created.

Examples:
  nogon import sitemap https://example.com/sitemap.xml
  nogon import sitemap sitemap.xml.gz --label reference
  nogon import sitemap - < sitemap.xml`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) (resultErr error) {
		cmd.SilenceUsage = true
		applyIndexCrawlerFlags(cmd)
		robots, err := indexRobotsCache(cmd)
		if err != nil {
			return err
		}
		reader, err := newSitemapReader(&cfg.Crawler, robots)
		if err != nil {
			return err
		}
		defer reader.Close()
		urls, err := readSitemapInput(cmd, reader, args[0])
		if err != nil {
			return fmt.Errorf("import sitemap %q: %w", args[0], err)
		}
		reportPath, _ := cmd.Flags().GetString("failed-urls")
		report, err := newFailedURLReport(reportPath)
		if err != nil {
			return err
		}
		defer func() {
			if err := report.Close(); err != nil {
				resultErr = err
			}
		}()
		if err := model.Init(cfg); err != nil {
			return err
		}
		initExtractor()
		return runURLInputJob(cmd, sitemapJobName(args[0]), urls, &crawler.ValidatorRules{NoDepth: true}, robots, report)
	},
}

func readSitemapInput(cmd *cobra.Command, reader *sitemapReader, source string) (_ []string, err error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return reader.Fetch(cmd.Context(), source)
	}
	input := cmd.InOrStdin()
	if source != "-" {
		file, openErr := os.Open(files.ExpandHome(source))
		if openErr != nil {
			return nil, openErr
		}
		defer func() {
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
		}()
		input = file
	}
	return reader.Read(cmd.Context(), input)
}

func sitemapJobName(source string) string {
	if u, err := url.Parse(source); err == nil && u.Host != "" {
		return "sitemap-" + u.Hostname() + "-" + path.Base(u.Path)
	}
	return "sitemap-" + indexInputJobName(source)
}

func init() {
	addURLInputFlags(importSitemapCmd)
	importSitemapCmd.Flags().String("label", "sitemap", "Label to attach to imported pages")
}
