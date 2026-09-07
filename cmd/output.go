// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func addOutputFormatFlag(cmd *cobra.Command) {
	cmd.Flags().StringP("format", "f", "text", "Output format: text, json, jsonl, csv")
	validateArgs := cmd.Args
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if validateArgs != nil {
			if err := validateArgs(cmd, args); err != nil {
				return err
			}
		}
		return validateOutputFormat(commandOutputFormat(cmd))
	}
}

func commandOutputFormat(cmd *cobra.Command) string {
	format, _ := cmd.Flags().GetString("format")
	return format
}

func validateOutputFormat(format string) error {
	switch format {
	case "text", "json", "jsonl", "csv":
		return nil
	default:
		return fmt.Errorf("invalid --format %q: expected text, json, jsonl, or csv", format)
	}
}

// recordWriter streams records without buffering the complete result set.
// JSON uses an array even for a single record. Text rendering belongs to each
// command so existing human output can retain its layout and styling.
type recordWriter struct {
	out     io.Writer
	format  string
	columns []string
	csv     *csv.Writer
	started bool
}

func newRecordWriter(out io.Writer, format string, columns []string) (*recordWriter, error) {
	if err := validateOutputFormat(format); err != nil {
		return nil, err
	}
	return &recordWriter{out: out, format: format, columns: columns}, nil
}

func (w *recordWriter) Write(record map[string]any, renderText func(io.Writer) error) error {
	switch w.format {
	case "text":
		return renderText(w.out)
	case "json", "jsonl":
		data, err := json.Marshal(record)
		if err != nil {
			return err
		}
		prefix, suffix := "", "\n"
		if w.format == "json" {
			prefix, suffix = ",\n", ""
			if !w.started {
				prefix = "[\n"
			}
		}
		if _, err := fmt.Fprintf(w.out, "%s%s%s", prefix, data, suffix); err != nil {
			return err
		}
		w.started = true
		return nil
	case "csv":
		row := make([]string, len(w.columns))
		for i, field := range w.columns {
			value := record[field]
			if value == nil {
				continue
			}
			if s, ok := value.(string); ok {
				row[i] = s
			} else if s, ok := value.(fmt.Stringer); ok {
				row[i] = s.String()
			} else {
				data, err := json.Marshal(value)
				if err != nil {
					return err
				}
				row[i] = string(data)
			}
		}
		if err := w.startCSV(); err != nil {
			return err
		}
		if err := w.csv.Write(row); err != nil {
			return err
		}
		w.csv.Flush()
		return w.csv.Error()
	}
	return nil
}

func (w *recordWriter) startCSV() error {
	if w.csv == nil {
		w.csv = csv.NewWriter(w.out)
		return w.csv.Write(w.columns)
	}
	return nil
}

// Close finishes a successful result. Callers must return immediately on a
// fetch or write error, leaving partial JSON visibly incomplete.
func (w *recordWriter) Close() error {
	switch w.format {
	case "json":
		ending := "[]\n"
		if w.started {
			ending = "\n]\n"
		}
		_, err := io.WriteString(w.out, ending)
		return err
	case "csv":
		if err := w.startCSV(); err != nil {
			return err
		}
		w.csv.Flush()
		return w.csv.Error()
	}
	return nil
}
