// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/asciimoo/hister/server/crawler"
	"github.com/asciimoo/hister/server/model"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var crawlCmd = &cobra.Command{
	Use:   "crawl",
	Short: "Manage persistent crawl jobs",
	Long:  "Manage persistent crawl jobs",
}

var crawlListCmd = &cobra.Command{
	Use:   "list",
	Short: "List persistent crawl jobs",
	Long:  "Display all persistent crawl jobs with their status and URL counts",
	Args:  cobra.NoArgs,
	PreRun: func(_ *cobra.Command, _ []string) {
		initDB(model.ReadOnly)
	},
	Run: func(cmd *cobra.Command, args []string) {
		jobs, err := model.ListCrawlJobs()
		if err != nil {
			exit(1, "Failed to list crawl jobs: "+err.Error())
		}
		if len(jobs) == 0 {
			fmt.Println("No crawl jobs found.")
			return
		}
		for _, j := range jobs {
			stats, err := model.GetCrawlJobStats(j.ID)
			if err != nil {
				log.Warn().Err(err).Str("job_id", j.ID).Msg("failed to get job stats")
			}
			cliPrintf("%s  %-12s  %s\n",
				cliInfoStyle.Render(j.ID),
				crawlJobStatusLabel(j.Status),
				j.StartURL,
			)
			fmt.Printf("  pending: %d  done: %d  failed: %d  skipped: %d  created: %s\n",
				stats.Pending, stats.Done, stats.Failed, stats.Skipped,
				j.CreatedAt.Format("2006-01-02 15:04:05"),
			)
		}
	},
}

var crawlShowCmd = &cobra.Command{
	Use:   "show JOB_ID",
	Short: "Show detailed persistent crawl job state",
	Long:  "Display detailed information about a persistent crawl job and its queued URL state",
	Args:  cobra.ExactArgs(1),
	PreRun: func(_ *cobra.Command, _ []string) {
		initDB(model.ReadOnly)
	},
	Run: func(cmd *cobra.Command, args []string) {
		showCrawlJob(args[0])
	},
}

var crawlErrorsCmd = &cobra.Command{
	Use:   "errors JOB_ID",
	Short: "List failed crawl URLs",
	Long:  "List failed crawl URL error codes and URLs for a persistent crawl job",
	Args:  cobra.ExactArgs(1),
	PreRun: func(_ *cobra.Command, _ []string) {
		initDB(model.ReadOnly)
	},
	Run: func(cmd *cobra.Command, args []string) {
		showCrawlJobErrors(args[0])
	},
}

var crawlQueueCmd = &cobra.Command{
	Use:   "queue JOB_ID",
	Short: "List crawl queue URLs",
	Long:  "List crawl URL status, depth, and URL rows for a persistent crawl job",
	Args:  cobra.ExactArgs(1),
	PreRun: func(_ *cobra.Command, _ []string) {
		initDB(model.ReadOnly)
	},
	Run: func(cmd *cobra.Command, args []string) {
		countOnly, _ := cmd.Flags().GetBool("count")
		showCrawlJobQueue(args[0], countOnly)
	},
}

var crawlURLsCmd = &cobra.Command{
	Use:   "urls JOB_ID",
	Short: "List crawl job URLs",
	Long:  "List crawl job URL status, depth, and URL rows, optionally filtered by status",
	Args:  validateCrawlURLsArgs,
	PreRun: func(_ *cobra.Command, _ []string) {
		initDB(model.ReadOnly)
	},
	Run: func(cmd *cobra.Command, args []string) {
		status, _ := cmd.Flags().GetString("status")
		countOnly, _ := cmd.Flags().GetBool("count")
		showCrawlJobURLs(args[0], status, countOnly)
	},
}

var crawlDeleteCmd = &cobra.Command{
	Use:   "delete JOB_ID",
	Short: "Delete a persistent crawl job",
	Long:  "Delete a crawl job and all its associated URL tracking data",
	Args:  cobra.ExactArgs(1),
	PreRun: func(_ *cobra.Command, _ []string) {
		initDB(model.ReadWrite)
	},
	Run: func(cmd *cobra.Command, args []string) {
		jobID := args[0]
		if err := model.DeleteCrawlJob(jobID); err != nil {
			exit(1, "Failed to delete crawl job: "+err.Error())
		}
		cliPrintln(cliSuccessStyle.Render("✓") + " Crawl job deleted: " + cliInfoStyle.Render(jobID))
	},
}

func showCrawlJob(jobID string) {
	job := loadCrawlJob(jobID)

	stats, err := model.GetCrawlJobStats(job.ID)
	if err != nil {
		exit(1, "Failed to load crawl job stats: "+err.Error())
	}

	cliPrintln(cliBoldStyle.Render("CRAWL JOB"))
	cliPrintf("id: %s\n", cliInfoStyle.Render(job.ID))
	fmt.Printf("status: %s\n", crawlJobStatusLabel(job.Status))
	fmt.Printf("start_url: %s\n", job.StartURL)
	fmt.Printf("label: %s\n", job.Label)
	fmt.Printf("created: %s\n", job.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("updated: %s\n", job.UpdatedAt.Format("2006-01-02 15:04:05"))
	fmt.Println()

	cliPrintln(cliBoldStyle.Render("STATE"))
	fmt.Printf("pending: %d\n", stats.Pending)
	fmt.Printf("in_progress: %d\n", stats.InProgress)
	fmt.Printf("done: %d\n", stats.Done)
	fmt.Printf("failed: %d\n", stats.Failed)
	fmt.Printf("skipped: %d\n", stats.Skipped)
	fmt.Println()

	cliPrintln(cliBoldStyle.Render("RULES"))
	rules, err := crawler.UnmarshalValidatorRules(job.ValidatorRules)
	if err != nil {
		fmt.Println(job.ValidatorRules)
		log.Warn().Err(err).Str("job_id", job.ID).Msg("failed to restore crawl job rules")
	} else {
		rulesJSON, err := json.MarshalIndent(rules, "", "  ")
		if err != nil {
			exit(1, "Failed to format crawl job rules: "+err.Error())
		}
		fmt.Println(string(rulesJSON))
	}
}

func crawlJobStatusLabel(status string) string {
	if status == model.CrawlJobRunning {
		return "unfinished"
	}
	return status
}

func showCrawlJobQueue(jobID string, countOnly bool) {
	showCrawlJobURLs(jobID, "", countOnly)
}

func showCrawlJobURLs(jobID, status string, countOnly bool) {
	job := loadCrawlJob(jobID)
	if err := writeCrawlJobURLs(os.Stdout, job.ID, status, countOnly); err != nil {
		exit(1, "Failed to load crawl job URLs: "+err.Error())
	}
}

func writeCrawlJobURLs(out io.Writer, jobID, status string, countOnly bool) error {
	if countOnly {
		var (
			count int64
			err   error
		)
		if status == "" {
			count, err = model.CountCrawlURLs(jobID)
		} else {
			count, err = model.CountCrawlURLsByStatus(jobID, status)
		}
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, count)
		return err
	}
	return model.ForEachCrawlURLByStatus(jobID, status, func(status string, depth int, rawURL string) error {
		_, err := fmt.Fprintf(out, "%s\t%d\t%s\n", status, depth, rawURL)
		return err
	})
}

func validateCrawlURLStatus(status string) error {
	switch status {
	case "", model.CrawlURLPending, model.CrawlURLFailed, model.CrawlURLDone, model.CrawlURLSkipped:
		return nil
	default:
		return fmt.Errorf("invalid --status %q: expected pending, failed, done, or skipped", status)
	}
}

func validateCrawlURLsArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.ExactArgs(1)(cmd, args); err != nil {
		return err
	}
	status, err := cmd.Flags().GetString("status")
	if err != nil {
		return err
	}
	return validateCrawlURLStatus(status)
}

func showCrawlJobErrors(jobID string) {
	job := loadCrawlJob(jobID)
	if err := writeCrawlJobErrors(os.Stdout, job.ID); err != nil {
		exit(1, "Failed to load crawl job errors: "+err.Error())
	}
}

func writeCrawlJobErrors(out io.Writer, jobID string) error {
	return model.ForEachFailedCrawlURLWithMessage(jobID, func(errorCode int, rawURL, errMsg string) error {
		errMsg = strings.NewReplacer("\t", " ", "\r", " ", "\n", " ").Replace(errMsg)
		_, err := fmt.Fprintf(out, "%d\t%s\t%s\n", errorCode, rawURL, errMsg)
		return err
	})
}

func loadCrawlJob(jobID string) *model.CrawlJob {
	job, err := model.GetCrawlJob(jobID)
	if err != nil {
		exit(1, "Failed to load crawl job: "+err.Error())
	}
	if job == nil {
		exit(1, "Crawl job not found: "+jobID)
	}
	return job
}
