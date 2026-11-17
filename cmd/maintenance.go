package cmd

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/server/types"

	"github.com/spf13/cobra"
)

var reindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Rebuild the search index",
	Long:  `Rebuild the search index from all stored documents using the running server.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		skipSensitive := false
		if b, err := cmd.Flags().GetBool("exclude-sensitive"); err == nil {
			skipSensitive = b
		}
		c := newClient(client.WithTimeout(0))
		return runMaintenance(cmd, "Reindex", "Reindexing", func() error {
			return c.Reindex(skipSensitive, cfg.Indexer.DetectLanguages)
		})
	},
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove stale local documents and orphaned data",
	Long:  `Remove indexed local documents that no longer match configured directories, and remove HTML and favicon files that are no longer referenced by any document in the index`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		c := newClient(client.WithTimeout(0))
		var result client.CleanupResult
		if err := runMaintenance(cmd, "Cleanup", "Cleaning up", func() error {
			var err error
			result, err = c.Cleanup()
			return err
		}); err != nil {
			return err
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Checked %d indexed local document(s)\nSkipped %d indexed local document(s)\nRemoved %d stale local document(s)\nRemoved %d orphaned HTML file(s)\nRemoved %d orphaned favicon file(s)\n",
			result.LocalDocumentsChecked, result.LocalDocumentsSkipped, result.LocalDocumentsRemoved, result.HTMLRemoved, result.FaviconRemoved)
		return err
	},
}

var purgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Permanently remove selected retained data",
	Long: `Purge stored versions, unpinned opened result history, or completed crawl records before an explicit cutoff.

Select at least one of --versions, --history, or --crawl-jobs, and supply --before.
Current indexed documents, pinned history, and resumable crawls are preserved.
This applies across all users and requires administrator access in multi user mode.
Counts are previewed before confirmation. Use --dry to preview without deleting.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		before, _ := cmd.Flags().GetString("before")
		cutoff, err := time.Parse(time.RFC3339, before)
		if err != nil {
			cutoff, err = time.Parse(time.DateOnly, before)
		}
		if err != nil {
			return fmt.Errorf("--before requires a date (YYYY-MM-DD) or RFC3339 timestamp")
		}
		request := types.PurgeRequest{Before: cutoff, DryRun: true}
		request.Versions, _ = cmd.Flags().GetBool("versions")
		request.History, _ = cmd.Flags().GetBool("history")
		request.CrawlJobs, _ = cmd.Flags().GetBool("crawl-jobs")
		if err := request.Validate(); err != nil {
			return err
		}
		cmd.SilenceUsage = true
		c := newClient(client.WithTimeout(0))
		result, err := c.Purge(cmd.Context(), request)
		if err != nil {
			return fmt.Errorf("failed to preview purge: %w", err)
		}
		if err := printPurgeCounts(cmd.OutOrStdout(), "Would purge", result); err != nil {
			return err
		}
		dry, _ := cmd.Flags().GetBool("dry")
		if dry {
			return nil
		}
		if result.Versions == 0 && result.HistoryEntries == 0 && result.HistoryQueries == 0 && result.HistoryURLs == 0 && result.CrawlJobs == 0 && result.CrawlURLs == 0 {
			return nil
		}
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Permanently purge these records across all users before %s? [y/N] ", cutoff.UTC().Format(time.RFC3339)); err != nil {
				return err
			}
			answer, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if err != nil && err != io.EOF {
				return err
			}
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer != "y" && answer != "yes" {
				cmd.Println("Purge cancelled")
				return nil
			}
		}
		request.DryRun = false
		result, err = c.Purge(cmd.Context(), request)
		if err != nil {
			return fmt.Errorf("failed to purge retained data: %w", err)
		}
		return printPurgeCounts(cmd.OutOrStdout(), "Purged", result)
	},
}

func printPurgeCounts(out io.Writer, prefix string, result *types.PurgeResult) error {
	_, err := fmt.Fprintf(out, "%s %d version(s), %d history entry(s), %d history query record(s), %d history URL record(s), %d crawl job(s), and %d crawl URL record(s)\n", prefix, result.Versions, result.HistoryEntries, result.HistoryQueries, result.HistoryURLs, result.CrawlJobs, result.CrawlURLs)
	return err
}

func runMaintenance(cmd *cobra.Command, name, activity string, operation func() error) error {
	cmd.SilenceUsage = true
	out := cmd.ErrOrStderr()
	stop := startMaintenanceProgress(out, activity, isTerminalWriter(out))
	err := operation()
	elapsed := stop().Round(time.Second)
	if err != nil {
		wrapped := fmt.Errorf("%s error: %w", name, err)
		if isConnectionError(err) {
			return fmt.Errorf("%w\n  Make sure the Nogon server is running before executing %s", wrapped, cmd.Name())
		}
		return wrapped
	}
	_, err = fmt.Fprintf(out, "%s completed in %s.\n", name, elapsed)
	return err
}

// startMaintenanceProgress returns a function that stops and joins the timer
// before callers write their final status or results.
func startMaintenanceProgress(out io.Writer, activity string, interactive bool) func() time.Duration {
	started := time.Now()
	if !interactive {
		_, _ = fmt.Fprintf(out, "%s...\n", activity)
		return func() time.Duration { return time.Since(started) }
	}
	_, _ = fmt.Fprintf(out, "%s... 0s elapsed", activity)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, _ = fmt.Fprintf(out, "\r%s... %s elapsed", activity, time.Since(started).Truncate(time.Second))
			case <-stop:
				_, _ = fmt.Fprintln(out)
				return
			}
		}
	}()
	return func() time.Duration {
		close(stop)
		<-done
		return time.Since(started)
	}
}
