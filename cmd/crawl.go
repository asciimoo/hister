// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/asciimoo/hister/server/crawler"
	"github.com/asciimoo/hister/server/model"

	"charm.land/lipgloss/v2"
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
	RunE: func(cmd *cobra.Command, args []string) error {
		jobs, err := model.ListCrawlJobs()
		if err != nil {
			return fmt.Errorf("list crawl jobs: %w", err)
		}
		return writeCrawlJobs(cmd.OutOrStdout(), commandOutputFormat(cmd), jobs, false)
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
	RunE: func(cmd *cobra.Command, args []string) error {
		job, err := loadCrawlJob(args[0])
		if err != nil {
			return err
		}
		return writeCrawlJobs(cmd.OutOrStdout(), commandOutputFormat(cmd), []*model.CrawlJob{job}, true)
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
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := loadCrawlJob(args[0]); err != nil {
			return err
		}
		return writeCrawlJobErrors(cmd.OutOrStdout(), commandOutputFormat(cmd), args[0])
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
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCrawlURLs(cmd, args[0], "")
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
	RunE: func(cmd *cobra.Command, args []string) error {
		status, _ := cmd.Flags().GetString("status")
		return runCrawlURLs(cmd, args[0], status)
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

func writeCrawlJobs(out io.Writer, format string, jobs []*model.CrawlJob, detail bool) error {
	columns := []string{"id", "status", "start_url", "label", "created_at", "updated_at", "pending", "in_progress", "done", "failed", "skipped"}
	if detail {
		columns = append(columns, "rules")
	}
	w, err := newRecordWriter(out, format, columns)
	if err != nil {
		return err
	}
	if len(jobs) == 0 && format == "text" {
		_, err := fmt.Fprintln(out, "No crawl jobs found.")
		return err
	}
	for _, job := range jobs {
		stats, err := model.GetCrawlJobStats(job.ID)
		if err != nil {
			return fmt.Errorf("load crawl job %s stats: %w", job.ID, err)
		}
		record := map[string]any{
			"id": job.ID, "status": crawlJobStatusLabel(job.Status),
			"start_url": job.StartURL, "label": job.Label,
			"created_at": job.CreatedAt.Format(time.RFC3339Nano),
			"updated_at": job.UpdatedAt.Format(time.RFC3339Nano),
			"pending":    stats.Pending, "in_progress": stats.InProgress,
			"done": stats.Done, "failed": stats.Failed, "skipped": stats.Skipped,
		}
		var rulesText string
		if detail {
			rules, err := crawler.UnmarshalValidatorRules(job.ValidatorRules)
			if err != nil {
				record["rules"] = job.ValidatorRules
				rulesText = job.ValidatorRules
				log.Warn().Err(err).Str("job_id", job.ID).Msg("failed to restore crawl job rules")
			} else {
				record["rules"] = rules
				rulesJSON, err := json.MarshalIndent(rules, "", "  ")
				if err != nil {
					return fmt.Errorf("format crawl job rules: %w", err)
				}
				rulesText = string(rulesJSON)
			}
		}
		if err := w.Write(record, func(out io.Writer) error {
			var text strings.Builder
			if !detail {
				fmt.Fprintf(&text, "%s  %-12s  %s\n", cliInfoStyle.Render(job.ID), crawlJobStatusLabel(job.Status), job.StartURL)
				fmt.Fprintf(&text, "  pending: %d  done: %d  failed: %d  skipped: %d  created: %s\n",
					stats.Pending, stats.Done, stats.Failed, stats.Skipped, job.CreatedAt.Format("2006-01-02 15:04:05"))
			} else {
				fmt.Fprintln(&text, cliBoldStyle.Render("CRAWL JOB"))
				fmt.Fprintf(&text, "id: %s\nstatus: %s\nstart_url: %s\nlabel: %s\ncreated: %s\nupdated: %s\n\n",
					cliInfoStyle.Render(job.ID), crawlJobStatusLabel(job.Status), job.StartURL, job.Label,
					job.CreatedAt.Format("2006-01-02 15:04:05"), job.UpdatedAt.Format("2006-01-02 15:04:05"))
				fmt.Fprintln(&text, cliBoldStyle.Render("STATE"))
				fmt.Fprintf(&text, "pending: %d\nin_progress: %d\ndone: %d\nfailed: %d\nskipped: %d\n\n",
					stats.Pending, stats.InProgress, stats.Done, stats.Failed, stats.Skipped)
				fmt.Fprintln(&text, cliBoldStyle.Render("RULES"))
				fmt.Fprintln(&text, rulesText)
			}
			_, err := lipgloss.Fprint(out, text.String())
			return err
		}); err != nil {
			return err
		}
	}
	return w.Close()
}

func crawlJobStatusLabel(status string) string {
	if status == model.CrawlJobRunning {
		return "unfinished"
	}
	return status
}

func runCrawlURLs(cmd *cobra.Command, jobID, status string) error {
	if _, err := loadCrawlJob(jobID); err != nil {
		return err
	}
	countOnly, _ := cmd.Flags().GetBool("count")
	return writeCrawlJobURLs(cmd.OutOrStdout(), commandOutputFormat(cmd), jobID, status, countOnly)
}

func writeCrawlJobURLs(out io.Writer, format, jobID, status string, countOnly bool) error {
	columns := []string{"status", "depth", "url"}
	if countOnly {
		columns = []string{"count"}
	}
	w, err := newRecordWriter(out, format, columns)
	if err != nil {
		return err
	}
	if countOnly {
		var count int64
		if status == "" {
			count, err = model.CountCrawlURLs(jobID)
		} else {
			count, err = model.CountCrawlURLsByStatus(jobID, status)
		}
		if err != nil {
			return err
		}
		err = w.Write(map[string]any{"count": count}, func(out io.Writer) error {
			_, err := fmt.Fprintln(out, count)
			return err
		})
	} else {
		err = model.ForEachCrawlURLByStatus(jobID, status, func(status string, depth int, rawURL string) error {
			return w.Write(map[string]any{"status": status, "depth": depth, "url": rawURL}, func(out io.Writer) error {
				_, err := fmt.Fprintf(out, "%s\t%d\t%s\n", status, depth, rawURL)
				return err
			})
		})
	}
	if err != nil {
		return err
	}
	return w.Close()
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

func writeCrawlJobErrors(out io.Writer, format, jobID string) error {
	w, err := newRecordWriter(out, format, []string{"error_code", "url", "error"})
	if err != nil {
		return err
	}
	if err := model.ForEachFailedCrawlURLWithMessage(jobID, func(errorCode int, rawURL, errMsg string) error {
		return w.Write(map[string]any{"error_code": errorCode, "url": rawURL, "error": errMsg}, func(out io.Writer) error {
			text := strings.NewReplacer("\t", " ", "\r", " ", "\n", " ").Replace(errMsg)
			_, err := fmt.Fprintf(out, "%d\t%s\t%s\n", errorCode, rawURL, text)
			return err
		})
	}); err != nil {
		return err
	}
	return w.Close()
}

func loadCrawlJob(jobID string) (*model.CrawlJob, error) {
	job, err := model.GetCrawlJob(jobID)
	if err != nil {
		return nil, fmt.Errorf("load crawl job: %w", err)
	}
	if job == nil {
		return nil, fmt.Errorf("crawl job not found: %s", jobID)
	}
	return job, nil
}
