package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/asciimoo/hister/cmd/tui"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/indexer"

	"github.com/spf13/cobra"
)

func searchDocToMap(d *document.Document) map[string]any {
	return map[string]any{
		"id":          d.ID(),
		"url":         d.URL,
		"title":       d.Title,
		"domain":      d.Domain,
		"score":       d.Score,
		"added":       d.Added,
		"updated":     d.Updated,
		"language":    d.Language,
		"type":        d.Type,
		"text":        d.Text,
		"favicon":     d.Favicon,
		"favicon_key": d.FaviconKey,
		"user_id":     d.UserID,
		"html":        d.HTML,
	}
}

// searchFilterMap returns only the requested keys; returns the full map when fields is empty.
func searchFilterMap(m map[string]any, fields []string) map[string]any {
	if len(fields) == 0 {
		return m
	}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		out[f] = m[f]
	}
	return out
}

var searchCmd = &cobra.Command{
	Use:   "search [search terms]",
	Short: "Search indexed documents",
	Long:  "Search indexed documents using the running server.\nRun without search terms to open the terminal interface, or provide terms to print results to standard output.",
	Args:  cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return tui.SearchTUI(cfg)
		}
		qs := strings.Join(args, " ")
		format, _ := cmd.Flags().GetString("format")
		limit, _ := cmd.Flags().GetInt("limit")
		sortMode, _ := cmd.Flags().GetString("sort")
		switch sortMode {
		case "relevance":
			sortMode = ""
		case "date", "domain", "visits":
		default:
			return fmt.Errorf("unknown sort order: %s (valid values: relevance, date, domain, visits)", sortMode)
		}

		// Parse and validate --fields.
		var fields []string
		includeHTML := false
		if fieldsRaw, _ := cmd.Flags().GetString("fields"); fieldsRaw != "" {
			validFields := map[string]bool{
				"id": true, "url": true, "title": true, "domain": true, "score": true,
				"added": true, "updated": true, "language": true, "type": true, "text": true,
				"favicon": true, "favicon_key": true, "user_id": true, "html": true,
			}
			for f := range strings.SplitSeq(fieldsRaw, ",") {
				f = strings.TrimSpace(f)
				if f == "" {
					continue
				}
				if !validFields[f] {
					return fmt.Errorf("unknown field: %s (valid fields: id, url, title, domain, score, added, updated, language, type, text, favicon, favicon_key, user_id, html)", f)
				}
				fields = append(fields, f)
				if f == "html" {
					includeHTML = true
				}
			}
		}

		c := newClient()
		return writeSearchResults(cmd.OutOrStdout(), format, fields, limit,
			indexer.Query{Text: qs, IncludeHTML: includeHTML, Sort: sortMode}, c.Search)
	},
}

func writeSearchResults(out io.Writer, format string, fields []string, limit int, query indexer.Query, search func(*indexer.Query) (*indexer.Results, error)) error {
	csvFields := fields
	if len(csvFields) == 0 {
		csvFields = []string{"title", "url", "domain", "score", "added", "updated", "language", "text"}
	}
	w, err := newRecordWriter(out, format, csvFields)
	if err != nil {
		return err
	}
	total := 0
	for {
		res, err := search(&query)
		if err != nil {
			return fmt.Errorf("search failed: %w", err)
		}
		for _, d := range res.Documents {
			record := searchFilterMap(searchDocToMap(d), fields)
			if err := w.Write(record, func(out io.Writer) error {
				if len(fields) == 0 {
					_, err := fmt.Fprintf(out, "%s\n%s\n\n", d.Title, d.URL)
					return err
				}
				parts := make([]string, 0, len(fields))
				for _, field := range fields {
					parts = append(parts, fmt.Sprint(record[field]))
				}
				text := strings.Join(parts, "\n") + "\n"
				if len(fields) > 1 {
					text += "\n"
				}
				_, err := io.WriteString(out, text)
				return err
			}); err != nil {
				return fmt.Errorf("write search result: %w", err)
			}
			total++
			if limit > 0 && total >= limit {
				return w.Close()
			}
		}
		if res.PageKey == "" || len(res.Documents) == 0 {
			return w.Close()
		}
		query.PageKey = res.PageKey
	}
}
