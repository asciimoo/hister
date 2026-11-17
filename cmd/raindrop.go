package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/asciimoo/hister/server/document"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

const (
	raindropSourceMetadataValue = "raindrop"
	raindropTokenEnv            = "HISTER_IMPORT_RAINDROP_TOKEN"
)

type raindropBookmark struct {
	URL        string
	Title      string
	Created    string
	Updated    string
	Excerpt    string
	Note       string
	Highlights string
	Folder     string
	Cover      string
	Tags       []string
	Metadata   map[string]any
}

var importRaindropCmd = &cobra.Command{
	Use:   "raindrop",
	Short: "Import bookmarks from Raindrop.io",
	Long: `Import bookmarks, notes, highlights, and searchable page content from a
Raindrop.io account through its API.

Set the Raindrop API token with HISTER_IMPORT_RAINDROP_TOKEN or --api-token.
Each run reads all bookmarks. Use --skip-existing to keep documents already
in the destination Nogon index.

To import a CSV export instead, use --input FILE.csv, or --input - for stdin.
CSV input requires a url column and does not need a Raindrop API token.

Nogon downloads linked pages using the configured crawler backend. Override
the backend with --backend and --backend-option. If a page cannot be downloaded,
its bookmark details are still imported and the failure is reported.

The global --token flag remains the access token for the destination Nogon server.`,
	Args: cobra.NoArgs,
	PreRun: func(_ *cobra.Command, _ []string) {
		initExtractor()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		inputPath, _ := cmd.Flags().GetString("input")
		if cmd.Flags().Changed("input") && strings.TrimSpace(inputPath) == "" {
			return errors.New("--input requires a CSV file path or - for stdin")
		}
		token := serviceAPIToken(cmd, raindropTokenEnv)
		if inputPath == "" && token == "" {
			return fmt.Errorf("missing Raindrop API token; set %s or use --api-token", raindropTokenEnv)
		}

		runtime, err := newServiceImportRuntime(cmd)
		if err != nil {
			return err
		}
		defer func() {
			if err := runtime.Close(); err != nil {
				log.Warn().Err(err).Msg("Raindrop content crawler close error")
			}
		}()

		if inputPath != "" {
			return runRaindropCSVImport(cmd, runtime, inputPath)
		}
		source, err := newRaindropClient(token, nil)
		if err != nil {
			return err
		}
		stats, err := importRaindrop(cmd.Context(), source, runtime.target, runtime.languageDetector, runtime.contentFetcher, runtime.options)
		if err != nil {
			err = fmt.Errorf("import from Raindrop failed: %w", err)
		}
		return finishImport(cmd, stats, err)
	},
}

func runRaindropCSVImport(cmd *cobra.Command, runtime *serviceImportRuntime, inputPath string) error {
	input := cmd.InOrStdin()
	if inputPath != "-" {
		f, err := os.Open(inputPath)
		if err != nil {
			return fmt.Errorf("open Raindrop CSV: %w", err)
		}
		defer func() {
			if err := f.Close(); err != nil {
				log.Warn().Err(err).Msg("Raindrop CSV close error")
			}
		}()
		input = f
	}
	stats, err := importRaindropCSV(cmd.Context(), input, runtime.target, runtime.languageDetector, runtime.contentFetcher, runtime.options)
	if err != nil {
		err = fmt.Errorf("import from Raindrop CSV failed: %w", err)
	}
	return finishImport(cmd, stats, err)
}

func raindropDocument(bookmark raindropBookmark, languageDetector document.LanguageDetector) (*document.Document, *serviceContentRequest, error) {
	rawURL := strings.TrimSpace(bookmark.URL)
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return nil, nil, errors.New("bookmark URL must be an http or https URL with a host")
	}
	added := parseServiceTime(bookmark.Created)
	if created := bookmark.Created; created != "" && added == 0 {
		added, err = strconv.ParseInt(created, 10, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid bookmark creation date %q: expected an ISO 8601 date or Unix timestamp", created)
		}
	}
	metadata := bookmark.Metadata
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["source"] = raindropSourceMetadataValue
	for key, value := range map[string]string{
		"raindrop_folder": bookmark.Folder, "raindrop_note": bookmark.Note,
		"raindrop_highlights": bookmark.Highlights, "raindrop_cover": bookmark.Cover,
		"description": bookmark.Excerpt,
	} {
		if value = strings.TrimSpace(value); value != "" {
			metadata[key] = value
		}
	}
	var tags []string
	seenTags := make(map[string]bool)
	for _, tag := range bookmark.Tags {
		tag = strings.TrimSpace(tag)
		if tag != "" && !seenTags[tag] {
			tags = append(tags, tag)
			seenTags[tag] = true
		}
	}
	if len(tags) > 0 {
		metadata["raindrop_tags"] = tags
	}
	title := strings.TrimSpace(bookmark.Title)
	prefixText := combineImportText(bookmark.Excerpt, bookmark.Note, bookmark.Highlights)
	d := &document.Document{
		URL: rawURL, Title: title, Text: prefixText, Added: added, Metadata: metadata,
	}
	if err := d.Process(languageDetector, nil); err != nil {
		return nil, nil, err
	}
	d.Updated = d.Added
	if updated := parseServiceTime(bookmark.Updated); updated != 0 {
		d.Updated = updated
	}
	if title == "" {
		d.Title = d.URL
	}
	return d, &serviceContentRequest{URL: rawURL, PrefixText: prefixText, SourceTitle: title}, nil
}
