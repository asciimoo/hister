// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/asciimoo/hister/server/model"
	"github.com/spf13/cobra"
)

// partialFailure reports a completed operation with unsuccessful items.
type partialFailure struct {
	count int64
}

func (e *partialFailure) Error() string {
	return fmt.Sprintf("processing finished with %d error(s)", e.count)
}

// ExitCode maps command results to shell status codes: 0 for success, 1 for
// execution errors, and 2 for completed operations with item failures.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var partial *partialFailure
	if errors.As(err, &partial) {
		return 2
	}
	return 1
}

type indexResult struct {
	Indexed int64
	Skipped int64
	Failed  int64
	Pending int64
	JobID   string
}

func (r indexResult) finish(cmd *cobra.Command, runErr error) error {
	columns := []string{"indexed", "skipped", "failed"}
	record := map[string]any{"indexed": r.Indexed, "skipped": r.Skipped, "failed": r.Failed}
	if r.JobID != "" {
		columns = append(columns, "job_id", "pending")
		record["job_id"], record["pending"] = r.JobID, r.Pending
	}
	w, err := newRecordWriter(cmd.OutOrStdout(), commandOutputFormat(cmd), columns)
	if err != nil {
		return err
	}
	if err := w.Write(record, func(out io.Writer) error {
		text := fmt.Sprintf("Indexed %d URL(s); %d skipped; %d failed", r.Indexed, r.Skipped, r.Failed)
		if r.JobID != "" {
			text += fmt.Sprintf("; %d pending (job %s)", r.Pending, r.JobID)
		}
		_, err := fmt.Fprintln(out, text)
		return err
	}); err != nil {
		return fmt.Errorf("write indexing summary: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("write indexing summary: %w", err)
	}
	if runErr != nil {
		return runErr
	}
	if r.Failed > 0 {
		return &partialFailure{count: r.Failed}
	}
	return nil
}

// failedURLReport writes retry input as failures occur. Opening it before any
// indexing work ensures an unusable output path fails without submitting URLs.
type failedURLReport struct {
	file *os.File
}

func newFailedURLReport(path string) (*failedURLReport, error) {
	r := &failedURLReport{}
	if path == "" {
		return r, nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open failed URL report: %w", err)
	}
	r.file = f
	return r, nil
}

func (r *failedURLReport) Write(rawURL string) error {
	if r.file == nil {
		return nil
	}
	if strings.ContainsAny(rawURL, "\r\n") {
		return errors.New("cannot save a failed URL containing a newline")
	}
	_, err := fmt.Fprintln(r.file, rawURL)
	if err != nil {
		return fmt.Errorf("write failed URL report: %w", err)
	}
	return nil
}

func (r *failedURLReport) Close() error {
	if r.file == nil {
		return nil
	}
	if err := r.file.Close(); err != nil {
		return fmt.Errorf("close failed URL report: %w", err)
	}
	return nil
}

func finishPersistentIndex(cmd *cobra.Command, jobID string, report *failedURLReport, runErr error) error {
	stats, err := model.GetCrawlJobStats(jobID)
	if err != nil {
		return fmt.Errorf("read crawl result: %w", err)
	}
	if report.file != nil {
		if err := model.ForEachFailedCrawlURL(jobID, func(_ int, rawURL string) error {
			return report.Write(rawURL)
		}); err != nil {
			runErr = fmt.Errorf("save failed crawl URLs: %w", err)
		}
	}
	return (indexResult{
		JobID: jobID, Indexed: stats.Done, Skipped: stats.Skipped,
		Failed: stats.Failed, Pending: stats.Pending + stats.InProgress,
	}).finish(cmd, runErr)
}
