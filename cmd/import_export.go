// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/server/crawler"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/model"

	"charm.land/lipgloss/v2"
	"github.com/bodgit/sevenzip"
	"github.com/mmcdole/gofeed"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export OUTPUT_FILE [QUERY...]",
	Short: "Export indexed documents to a JSON file",
	Long: `Export all indexed documents, or only those matching a search query, to a JSON file.

Each document is written as a single JSON line. Lines not starting with '{' are
structural markers ('[', ']', ',') and can be safely skipped by parsers.

If OUTPUT_FILE has no extension, .json is appended automatically. Explicit
extensions are preserved; use .json so the file importer recognizes the export.

An existing export is replaced only after the new export has been written
successfully. Symbolic links and other nonregular destinations are rejected.

Use --start-date and --end-date (format: YYYY-MM-DD) to only export
documents updated within the given date range.

Use '-' as OUTPUT_FILE to write to stdout.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		outputFile := exportOutputPath(args[0])
		queryStr := strings.Join(args[1:], " ")
		if queryStr == "" {
			queryStr = "*"
		}

		dateRange, err := parseDateRangeFlags(cmd)
		if err != nil {
			return err
		}
		cmd.SilenceUsage = true

		c := newClient(client.WithTimeout(0))
		query := client.SearchQuery{
			Text: queryStr, IncludeHTML: true, IncludeText: true,
			DateFrom: dateRange.From, DateTo: dateRange.To,
		}
		if outputFile == "-" {
			_, err := writeExport(cmd.OutOrStdout(), c, query)
			return err
		}
		if !strings.EqualFold(filepath.Ext(outputFile), ".json") {
			log.Warn().Str("file", outputFile).Msg("Export format is JSON. Use a .json extension so hister import file recognizes this export")
		}
		count, err := writeExportFile(outputFile, c, query)
		if err != nil {
			return err
		}
		_, err = lipgloss.Fprintf(cmd.OutOrStdout(), "%s Exported %d document(s) to %s\n",
			cliSuccessStyle.Render("✓"), count, cliInfoStyle.Render(outputFile))
		return err
	},
}

func writeExportFile(path string, c *client.Client, query client.SearchQuery) (count int, resultErr error) {
	info, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("inspect export destination: %w", err)
	}
	if info != nil && !info.Mode().IsRegular() {
		return 0, errors.New("export destination must be a regular file; use '-' to stream to stdout")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".hister-export-*")
	if err != nil {
		return 0, fmt.Errorf("create temporary export: %w", err)
	}
	temporary := f.Name()
	defer func() {
		if f != nil {
			if err := f.Close(); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("close temporary export: %w", err))
			}
		}
		if temporary != "" {
			if err := os.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
				resultErr = errors.Join(resultErr, fmt.Errorf("remove temporary export: %w", err))
			}
		}
	}()

	count, err = writeExport(f, c, query)
	if err != nil {
		return count, err
	}
	if err := f.Sync(); err != nil {
		return count, fmt.Errorf("sync export: %w", err)
	}
	err = f.Close()
	f = nil
	if err != nil {
		return count, fmt.Errorf("close export: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return count, fmt.Errorf("replace export: %w", err)
	}
	temporary = ""
	return count, nil
}

func writeExport(out io.Writer, c *client.Client, query client.SearchQuery) (int, error) {
	bw := bufio.NewWriter(out)
	if _, err := fmt.Fprintln(bw, "["); err != nil {
		return 0, fmt.Errorf("write export: %w", err)
	}
	encoder := json.NewEncoder(bw)
	count := 0
	for {
		res, err := c.Search(&query)
		if err != nil {
			return count, fmt.Errorf("search failed: %w", err)
		}
		if res == nil {
			return count, errors.New("search returned an empty response")
		}
		if res.Error != "" {
			return count, fmt.Errorf("search failed: %s", res.Error)
		}
		for _, d := range res.Documents {
			if d == nil {
				return count, errors.New("search returned an empty document")
			}
			if count > 0 {
				if _, err := fmt.Fprintln(bw, ","); err != nil {
					return count, fmt.Errorf("write export: %w", err)
				}
			}
			if err := encoder.Encode(d); err != nil {
				return count, fmt.Errorf("write export document: %w", err)
			}
			count++
		}
		if res.PageKey == "" || len(res.Documents) == 0 {
			break
		}
		query.PageKey = res.PageKey
	}
	if _, err := fmt.Fprintln(bw, "]"); err != nil {
		return count, fmt.Errorf("write export: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return count, fmt.Errorf("flush export: %w", err)
	}
	return count, nil
}

func exportOutputPath(path string) string {
	if path == "" || path == "-" || os.IsPathSeparator(path[len(path)-1]) || filepath.Ext(path) != "" {
		return path
	}
	return path + ".json"
}

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import documents from files, feeds, sitemaps, browsers, or services",
	Long: `Import documents from files, feeds, sitemaps, browser history, browser bookmarks, or external services.

Use one of the available subcommands to select the import source.

Execution scopes describe access to Hister state. Remote commands use the
configured Hister HTTP server without opening the local Hister database or
search index. Hybrid commands access local files or Hister state and also use
the configured Hister HTTP server.

The file importer reads and prepares local files before submitting documents
to the server. The browser importers read local history or bookmarks, keep
resumable crawl state locally, fetch page contents, and submit the prepared
documents to the server. Use import browser history for visit history or
import browser bookmarks for bookmarks from Firefox based browsers, Chromium
based browsers, and Ladybird. Both subcommands accept --browser and --db.`,
}

var importRSSCmd = &cobra.Command{
	Use:   "rss FEED_URL",
	Short: "Import articles linked from an RSS or Atom feed",
	Long: `Fetch an RSS or Atom feed from an HTTP or HTTPS URL and index its linked
articles. Only articles listed in the feed are fetched; links on those pages
are not followed. Duplicate URLs are visited once. Existing documents are
skipped unless --force is specified. The default label is rss.

Crawler settings and robots rules apply. The feed is always fetched over HTTP;
--backend selects how articles are fetched. This command imports once and exits.

Articles are queued in a persistent crawl job in the local Hister database
and submitted to the configured server. Resume an interrupted article import
with hister index --job-id ID, using the job ID printed when indexing starts.

Examples:
  hister import rss https://example.com/feed.xml
  hister import rss https://example.com/atom.xml --label news`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) (resultErr error) {
		cmd.SilenceUsage = true
		applyIndexCrawlerFlags(cmd)
		robots, err := indexRobotsCache(cmd)
		if err != nil {
			return err
		}
		urls, err := fetchRSSURLs(cmd.Context(), args[0], robots)
		if err != nil {
			return fmt.Errorf("import RSS feed: %w", err)
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
		feedURL, _ := url.Parse(args[0])
		return runURLInputJob(cmd, "rss-"+feedURL.Hostname(), urls, &crawler.ValidatorRules{NoDepth: true}, robots, report)
	},
}

func fetchRSSURLs(ctx context.Context, source string, robots *crawler.RobotsCache) ([]string, error) {
	const maxFeedBytes = 16 << 20
	feedURL, err := url.Parse(source)
	if err != nil || (feedURL.Scheme != "http" && feedURL.Scheme != "https") || feedURL.Hostname() == "" || strings.ContainsFunc(source, unicode.IsSpace) {
		return nil, errors.New("feed requires an absolute HTTP or HTTPS URL")
	}
	if feedURL.User != nil {
		return nil, errors.New("feed URL must not contain embedded credentials; use crawler headers or cookies")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if robots != nil && !robots.Allowed(ctx, source) {
		return nil, errors.New("feed disallowed by robots.txt")
	}
	httpClient, err := crawler.NewHTTPClient(&cfg.Crawler)
	if err != nil {
		return nil, err
	}
	defer httpClient.CloseIdleConnections()
	resp, err := httpClient.Get(ctx, source)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read feed: %w", err)
	}
	if len(body) > maxFeedBytes {
		return nil, errors.New("feed exceeds the 16 MiB size limit")
	}
	feed, err := gofeed.NewParser().Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}
	var urls []string
	seen := make(map[string]bool)
	for i, item := range feed.Items {
		link := strings.TrimSpace(item.Link)
		if link == "" {
			continue
		}
		articleURL, err := url.Parse(link)
		if err != nil || strings.ContainsFunc(link, unicode.IsSpace) {
			log.Warn().Int("item", i+1).Msg("Skipping invalid feed article URL")
			continue
		}
		articleURL = resp.Request.URL.ResolveReference(articleURL)
		if (articleURL.Scheme != "http" && articleURL.Scheme != "https") || articleURL.Hostname() == "" || articleURL.User != nil {
			log.Warn().Int("item", i+1).Msg("Skipping feed article URL without HTTP or HTTPS or with embedded credentials")
			continue
		}
		articleURL.Fragment = ""
		articleURL.RawFragment = ""
		link = articleURL.String()
		if !seen[link] {
			seen[link] = true
			urls = append(urls, link)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(urls) == 0 {
		return nil, errors.New("feed contains no article URLs")
	}
	return urls, nil
}

var importFileCmd = &cobra.Command{
	Use:   "file [INPUT_FILE_OR_DIR...]",
	Short: "Import documents and local file snapshots",
	Long: `Import documents from export files, saved pages, or local files.

JSON files are read line by line; each line starting with '{' is parsed as a
document and submitted to the running server without reprocessing its stored
content. Array brackets and separating commas must be on their own lines.
Compact or indented export layouts are not supported. Each line must be smaller
than 64 MiB; indexer.max_file_size_mb does not change this export limit.
JSON files that do not have the Hister export array shape are imported as file
snapshots.

A Hister JSON export may be read directly or from a 7z compressed archive
(.7z) containing a single JSON file.

HTML files (.html or .htm) can also be imported: the URL is extracted from
the HTML (canonical link, OpenGraph/Twitter meta tags, etc.) and the document
is submitted to the running server for processing. HTML without URL metadata,
PDF, DOCX, Markdown, Org mode, and valid UTF 8 files are extracted locally and
submitted through the normal add endpoint as remote file snapshots.

Multiple files may be given; they are imported in order and the result is
reported as a combined total.

Directories may be given too and are imported recursively.

Files in indexer.directories do not need this command when the server can
access them. Starting or restarting the server automatically scans those
directories and watches them for later changes.

With no input, this command creates remote file snapshots from the configured
directories using their file type, pattern, exclusion, hidden path, and label
rules. This mode is intended for directories that the command line client can
access but the server cannot.

Use --watch to import remote file snapshots and keep updating created or changed
files until interrupted. Exports, 7z archives, and HTML with source URL metadata
are skipped in this mode. --skip-existing applies only to the initial scan.
Removals are never synchronized, even with delete_on_remove configured.
Restarting the command scans all inputs again. Temporary server failures are
retried while the command remains active. A combined summary is printed on exit.

Use --start-date and --end-date (format: YYYY-MM-DD) to only import
documents whose "added" timestamp falls within the given date range.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		skip, _ := cmd.Flags().GetBool("skip-existing")
		watch, _ := cmd.Flags().GetBool("watch")
		if watch && (cmd.Flags().Changed("start-date") || cmd.Flags().Changed("end-date")) {
			return fmt.Errorf("--watch cannot be combined with --start-date or --end-date")
		}
		global, _ := cmd.Flags().GetBool("global")
		source, _ := cmd.Flags().GetString("source")
		normalizedSource, err := normalizeRemoteFileSource(source)
		if err != nil {
			return err
		}
		batchSize, _ := cmd.Flags().GetInt("batch-size")
		labelOverride := newDocumentLabelOverride(cmd)
		if batchSize < 1 || batchSize > maxImportBatchSize {
			return fmt.Errorf("--batch-size must be between 1 and %d", maxImportBatchSize)
		}

		dateRange, err := parseDateRangeFlags(cmd)
		if err != nil {
			return err
		}

		clientOpts := append([]client.Option{client.WithTimeout(0)}, targetUserIDClientOptions(cmd, global)...)
		clientOpts = append(clientOpts, documentSubmissionClientOptions(cmd)...)
		c := newClient(clientOpts...)
		importer := newFileImporter(c, cfg.Indexer)
		imported := 0
		skipped := 0
		errCount := 0

		maxFileSize := cfg.Indexer.MaxFileSize << 20
		if maxFileSize <= 0 {
			maxFileSize = 1 << 20
		}
		if watch {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			stats, err := importer.watchImportFiles(ctx, args, cfg.Indexer.Directories, fileWatchOptions{
				Source: normalizedSource, MaxFileSize: maxFileSize, SkipExisting: skip, Label: labelOverride,
				RetryDelay: 5 * time.Second,
			})
			return finishImport(cmd, stats, err)
		}
		inputFiles, err := expandImportInputs(args, cfg.Indexer.Directories)
		if err != nil {
			return err
		}
		for _, input := range inputFiles {
			i, s, e := importer.importFile(cmd.Context(), input, fileImportOptions{
				Source: normalizedSource, MaxFileSize: maxFileSize, SkipExisting: skip,
				StartDate: dateRange.From, EndDate: dateRange.To, BatchSize: batchSize, Label: labelOverride,
			})
			imported += i
			skipped += s
			errCount += e
		}

		return finishImport(cmd, serviceImportStats{Imported: imported, Skipped: skipped, Errors: errCount}, nil)
	},
}

const (
	defaultImportBatchSize = 10
	maxImportBatchSize     = 100
)

type documentLabelOverride struct {
	value string
	set   bool
}

func newDocumentLabelOverride(cmd *cobra.Command) documentLabelOverride {
	value, _ := cmd.Flags().GetString("label")
	return documentLabelOverride{
		value: value,
		set:   cmd.Flags().Changed("label"),
	}
}

func (o documentLabelOverride) apply(d *document.Document, fallback string) {
	if o.set {
		d.Label = o.value
	} else if d.Label == "" {
		d.Label = fallback
	}
}

func (o documentLabelOverride) resolve(existing, fallback string) string {
	if o.set {
		return o.value
	}
	if existing == "" {
		return fallback
	}
	return existing
}

func addDocumentImportFlags(cmd *cobra.Command) {
	addCommonImportFlags(cmd)
	cmd.Flags().String("source", defaultRemoteFileSource(), "Stable source name used in remote file document URLs")
	cmd.Flags().Bool("allow-sensitive", false, "Skip sensitive content checks, allowing matching documents to be indexed")
}

func addCommonImportFlags(cmd *cobra.Command) {
	addOutputFormatFlag(cmd)
	cmd.Flags().String("start-date", "", "only import documents added on or after this date (YYYY-MM-DD)")
	cmd.Flags().String("end-date", "", "only import documents added on or before this date (YYYY-MM-DD)")
	cmd.Flags().Int("batch-size", defaultImportBatchSize, "number of documents submitted per bulk request (maximum 100)")
	cmd.Flags().Bool("skip-existing", false, "Do not overwrite documents that are already in the index")
	cmd.Flags().Bool("global", false, "Make imported documents available for all users (only for admins in multiuser mode)")
	cmd.Flags().Uint("user-id", 0, "Import documents under the given user ID (only for admins in multiuser mode)")
}

func finishImport(cmd *cobra.Command, stats serviceImportStats, runErr error) error {
	if err := writeImportSummary(cmd.OutOrStdout(), commandOutputFormat(cmd), stats.Imported, stats.Skipped, stats.Errors); err != nil {
		return fmt.Errorf("write import summary: %w", err)
	}
	if runErr != nil {
		return runErr
	}
	if stats.Errors > 0 {
		return &partialFailure{count: int64(stats.Errors)}
	}
	return nil
}

func writeImportSummary(out io.Writer, format string, imported, skipped, errCount int) error {
	w, err := newRecordWriter(out, format, []string{"imported", "skipped", "errors"})
	if err != nil {
		return err
	}
	record := map[string]any{"imported": imported, "skipped": skipped, "errors": errCount}
	if err := w.Write(record, func(out io.Writer) error {
		msg := fmt.Sprintf("%s Imported %d document(s)", cliSuccessStyle.Render("✓"), imported)
		if skipped > 0 {
			msg += fmt.Sprintf(" (%d skipped)", skipped)
		}
		if errCount > 0 {
			msg += fmt.Sprintf(" (%d errors)", errCount)
		}
		_, err := lipgloss.Fprintln(out, msg)
		return err
	}); err != nil {
		return err
	}
	return w.Close()
}

func isHisterJSONExport(inputFile string) (bool, error) {
	f, err := os.Open(inputFile)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	return isHisterJSONExportReader(f), nil
}

func isHisterJSONExportReader(reader io.Reader) bool {
	// Export documents already have a line size limit. Inspect at most one
	// such document plus its opening array delimiter and newline.
	decoder := json.NewDecoder(io.LimitReader(reader, maxJSONExportLineSize+2))
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '[' {
		return false
	}
	if !decoder.More() {
		return true
	}
	var first map[string]json.RawMessage
	if err := decoder.Decode(&first); err != nil {
		return false
	}
	var documentURL string
	if err := json.Unmarshal(first["url"], &documentURL); err != nil || documentURL == "" {
		return false
	}
	_, hasType := first["type"]
	_, hasProcessed := first["processed"]
	return hasType && hasProcessed
}

// importJSONFile imports documents from a JSON export file (optionally a
// 7z compressed archive) and submits them to the running server. It returns
// the number of documents imported, skipped and failed.
func importJSONFile(
	c *client.Client,
	inputFile string,
	skip bool,
	startDate int64,
	endDate int64,
	batchSize int,
	labelOverride documentLabelOverride,
) (imported, skipped, errCount int) {
	var reader io.Reader
	inputLog := log.With().Str("file", inputFile).Logger()

	if strings.HasSuffix(strings.ToLower(inputFile), ".7z") {
		sz, err := sevenzip.OpenReader(inputFile)
		if err != nil {
			log.Warn().Err(err).Str("file", inputFile).Msg("Failed to open 7z archive, skipping")
			return 0, 0, 1
		}
		defer func() {
			if err := sz.Close(); err != nil {
				log.Error().Err(err).Msg("Failed to close 7z archive")
			}
		}()

		var jsonEntry *sevenzip.File
		for _, entry := range sz.File {
			if strings.HasSuffix(strings.ToLower(entry.Name), ".json") {
				jsonEntry = entry
				break
			}
		}
		if jsonEntry == nil {
			log.Warn().Str("file", inputFile).Msg("No JSON file found inside 7z archive, skipping")
			return 0, 0, 1
		}
		inputLog = inputLog.With().Str("entry", jsonEntry.Name).Logger()
		rc, err := jsonEntry.Open()
		if err != nil {
			log.Warn().Err(err).Str("file", inputFile).Msg("Failed to open JSON entry in 7z archive, skipping")
			return 0, 0, 1
		}
		defer func() {
			if err := rc.Close(); err != nil {
				log.Error().Err(err).Msg("Failed to close 7z entry reader")
			}
		}()
		reader = rc
	} else {
		f, err := os.Open(inputFile)
		if err != nil {
			log.Warn().Err(err).Str("file", inputFile).Msg("Failed to open input file, skipping")
			return 0, 0, 1
		}
		defer func() {
			if err := f.Close(); err != nil {
				log.Error().Err(err).Msg("Failed to close input file")
			}
		}()
		reader = f
	}

	exportReader := newJSONExportReader(reader, maxJSONExportLineSize)
	docs := make([]*document.Document, 0, batchSize)
	flush := func() {
		if len(docs) == 0 {
			return
		}
		i, e := addDocumentBatch(c, docs)
		imported += i
		errCount += e
		docs = docs[:0]
	}

	for {
		line, err := exportReader.nextLine()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			inputLog.Warn().Err(err).Int("line", exportReader.line).Msg("Failed to read JSON export, stopping this file")
			errCount++
			break
		}
		var d document.Document
		if err := json.Unmarshal(line, &d); err != nil {
			inputLog.Warn().Err(err).Int("line", exportReader.line).Msg("Failed to parse document line, skipping; each document must be complete on one line")
			errCount++
			continue
		}
		d.Processed = true
		if (startDate != 0 && d.Added < startDate) || (endDate != 0 && d.Added > endDate) {
			log.Debug().Str("url", d.URL).Int64("added", d.Added).Msg("Skipping document outside of date range")
			skipped++
			continue
		}
		if skip {
			exists, err := c.DocumentExists(d.URL)
			if err != nil {
				log.Warn().Err(err).Str("url", d.URL).Msg("Failed to check if document exists, skipping")
				errCount++
				continue
			}
			if exists {
				log.Debug().Str("url", d.URL).Msg("Document already exists, skipping")
				skipped++
				continue
			}
		}
		labelOverride.apply(&d, "import")
		docs = append(docs, &d)
		if len(docs) == batchSize {
			flush()
		}
	}
	flush()

	return imported, skipped, errCount
}

func addDocumentBatch(c *client.Client, docs []*document.Document) (imported, errCount int) {
	payload := make([]*client.Document, len(docs))
	for i, doc := range docs {
		payload[i] = clientDocument(doc)
	}
	results, err := c.AddDocumentsJSON(payload)
	for i, result := range results {
		if result.Status >= 200 && result.Status < 300 {
			imported++
			continue
		}
		log.Warn().Int("status", result.Status).Str("error", result.Error).Str("url", docs[i].URL).Msg("Failed to add document")
		errCount++
	}
	if err != nil {
		remaining := len(docs) - len(results)
		log.Warn().Err(err).Int("documents", remaining).Msg("Failed to add document batch")
		errCount += remaining
	}
	return imported, errCount
}

// importHTMLFile reads a single HTML file, builds a document from it by
// extracting the URL from the HTML or using its file URL as a fallback, and
// submits it to the running server. It returns the number of documents
// imported, skipped and failed.
func (i *fileImporter) importHTMLFile(
	ctx context.Context,
	input importFileInput,
	source string,
	maxFileSize int64,
	skip bool,
	labelOverride documentLabelOverride,
) (imported, skipped, errCount int) {
	data, info, err := readImportFile(input.Path, maxFileSize)
	if err != nil {
		log.Warn().Err(err).Str("file", input.Path).Msg("Failed to read HTML file, skipping")
		return 0, 0, 1
	}

	d, err := document.FromHTML(string(data))
	if errors.Is(err, document.ErrNoURL) {
		return i.importRemoteFile(ctx, input, data, info, source, maxFileSize, skip, labelOverride)
	}
	if err != nil {
		log.Warn().Err(err).Str("file", input.Path).Msg("Failed to import HTML file, skipping")
		return 0, 0, 1
	}

	if skip {
		exists, err := i.client.DocumentExists(d.URL)
		if err != nil {
			log.Warn().Err(err).Str("url", d.URL).Msg("Failed to check if document exists, skipping")
			return 0, 0, 1
		}
		if exists {
			log.Debug().Str("url", d.URL).Msg("Document already exists, skipping")
			return 0, 1, 0
		}
	}

	labelOverride.apply(d, "import")
	imported, errCount = addDocumentBatch(i.client, []*document.Document{d})
	return imported, 0, errCount
}
