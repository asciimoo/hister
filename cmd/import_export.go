// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/indexer"

	"github.com/bodgit/sevenzip"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export OUTPUT_FILE [QUERY...]",
	Short: "Export indexed documents to a JSON file",
	Long: `Export all indexed documents, or only those matching a search query, to a JSON file.

Each document is written as a single JSON line. Lines not starting with '{' are
structural markers ('[', ']', ',') and can be safely skipped by parsers.

Use --start-date and --end-date (format: YYYY-MM-DD) to only export
documents updated within the given date range.

Use '-' as OUTPUT_FILE to write to stdout.`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		outputFile := args[0]
		queryStr := strings.Join(args[1:], " ")
		if queryStr == "" {
			queryStr = "*"
		}

		dateRange, err := parseDateRangeFlags(cmd)
		if err != nil {
			exit(1, err.Error())
		}

		var out *os.File
		if outputFile == "-" {
			out = os.Stdout
		} else {
			f, err := os.Create(outputFile)
			if err != nil {
				exit(1, "Failed to create output file: "+err.Error())
			}
			defer func() {
				if err := f.Close(); err != nil {
					log.Error().Err(err).Msg("Failed to close output file")
				}
			}()
			out = f
		}

		bw := bufio.NewWriter(out)
		defer func() {
			if err := bw.Flush(); err != nil {
				log.Error().Err(err).Msg("Failed to flush output")
			}
		}()

		if _, err := fmt.Fprintln(bw, "["); err != nil {
			exit(1, "Write error: "+err.Error())
		}

		c := newClient(client.WithTimeout(0))
		first := true
		count := 0
		pageKey := ""
		for {
			res, err := c.Search(&indexer.Query{
				Text:        queryStr,
				PageKey:     pageKey,
				IncludeHTML: true,
				IncludeText: true,
				DateFrom:    dateRange.From,
				DateTo:      dateRange.To,
			})
			if err != nil {
				exit(1, "Search failed: "+err.Error())
			}
			for _, d := range res.Documents {
				b, merr := json.Marshal(d)
				if merr != nil {
					log.Warn().Err(merr).Str("url", d.URL).Msg("Failed to serialize document, skipping")
					continue
				}
				if !first {
					if _, werr := fmt.Fprintln(bw, ","); werr != nil {
						exit(1, "Write error: "+werr.Error())
					}
				}
				first = false
				if _, werr := bw.Write(b); werr != nil {
					exit(1, "Write error: "+werr.Error())
				}
				if _, werr := fmt.Fprintln(bw); werr != nil {
					exit(1, "Write error: "+werr.Error())
				}
				count++
			}
			if res.PageKey == "" || len(res.Documents) == 0 {
				break
			}
			pageKey = res.PageKey
		}

		if _, err := fmt.Fprintln(bw, "]"); err != nil {
			exit(1, "Write error: "+err.Error())
		}

		if outputFile != "-" {
			cliPrintf("%s Exported %d document(s) to %s\n",
				cliSuccessStyle.Render("✓"), count, cliInfoStyle.Render(outputFile))
		}
	},
}

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import documents from files, browsers, or services",
	Long: `Import documents from files, browser history, browser bookmarks, or external services.

Use one of the available subcommands to select the import source.

Execution scopes describe access to Hister state. Remote commands use the
configured Hister HTTP server without opening the local Hister database or
search index. Hybrid commands access local files or Hister state and also use
the configured Hister HTTP server.

The file importer reads and prepares local files before submitting documents
to the server. The browser importer reads local browser history, keeps its
resumable crawl state locally, fetches page contents, and submits the prepared
documents to the server. Bookmark import reads Firefox bookmarks from the same
places.sqlite file via --browser and --db, and uses the same crawl path.`,
}

var importFileCmd = &cobra.Command{
	Use:   "file [INPUT_FILE_OR_DIR...]",
	Short: "Import documents and local file snapshots",
	Long: `Import documents from export files, saved pages, or local files.

JSON files are read line by line; each line starting with '{' is parsed as a
document and submitted to the running server without reprocessing its stored
content. JSON files that do not have the Hister export array shape are imported
as file snapshots.

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
access but the server cannot. Remote file snapshots are not watched for later
changes.

Use --start-date and --end-date (format: YYYY-MM-DD) to only import
documents whose "added" timestamp falls within the given date range.`,
	Args: cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		skip, _ := cmd.Flags().GetBool("skip-existing")
		global, _ := cmd.Flags().GetBool("global")
		source, _ := cmd.Flags().GetString("source")
		normalizedSource, err := normalizeRemoteFileSource(source)
		if err != nil {
			exit(1, err.Error())
		}
		batchSize, _ := cmd.Flags().GetInt("batch-size")
		labelOverride := newDocumentLabelOverride(cmd)
		if batchSize < 1 || batchSize > maxImportBatchSize {
			exit(1, fmt.Sprintf("--batch-size must be between 1 and %d", maxImportBatchSize))
		}

		dateRange, err := parseDateRangeFlags(cmd)
		if err != nil {
			exit(1, err.Error())
		}

		clientOpts := append([]client.Option{client.WithTimeout(0)}, targetUserIDClientOptions(cmd, global)...)
		if allowSensitive, _ := cmd.Flags().GetBool("allow-sensitive"); allowSensitive {
			clientOpts = append(clientOpts, client.WithAllowSensitive())
		}
		c := newClient(clientOpts...)
		imported := 0
		skipped := 0
		errCount := 0

		inputFiles, err := expandImportInputs(args, cfg.Indexer.Directories)
		if err != nil {
			exit(1, err.Error())
		}

		maxFileSize := cfg.Indexer.MaxFileSize << 20
		if maxFileSize <= 0 {
			maxFileSize = 1 << 20
		}
		for _, input := range inputFiles {
			var i, s, e int
			switch ext := strings.ToLower(filepath.Ext(input.Path)); ext {
			case ".7z":
				i, s, e = importJSONFile(c, input.Path, skip, dateRange.From, dateRange.To, batchSize, labelOverride)
			case ".json":
				isExport, detectErr := isHisterJSONExport(input.Path)
				if detectErr != nil {
					log.Warn().Err(detectErr).Str("file", input.Path).Msg("Failed to inspect JSON file")
					e = 1
				} else if isExport {
					i, s, e = importJSONFile(c, input.Path, skip, dateRange.From, dateRange.To, batchSize, labelOverride)
				} else {
					i, s, e = importRemoteFilePath(c, input, normalizedSource, maxFileSize, skip, labelOverride)
				}
			case ".html", ".htm":
				i, s, e = importHTMLFile(c, input, normalizedSource, maxFileSize, skip, labelOverride)
			default:
				i, s, e = importRemoteFilePath(c, input, normalizedSource, maxFileSize, skip, labelOverride)
			}
			imported += i
			skipped += s
			errCount += e
		}

		printImportSummary(imported, skipped, errCount)
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
	cmd.Flags().String("start-date", "", "only import documents added on or after this date (YYYY-MM-DD)")
	cmd.Flags().String("end-date", "", "only import documents added on or before this date (YYYY-MM-DD)")
	cmd.Flags().Int("batch-size", defaultImportBatchSize, "number of documents submitted per bulk request (maximum 100)")
	cmd.Flags().Bool("skip-existing", false, "Do not overwrite documents that are already in the index")
	cmd.Flags().Bool("global", false, "Make imported documents available for all users (only for admins in multiuser mode)")
	cmd.Flags().Uint("user-id", 0, "Import documents under the given user ID (only for admins in multiuser mode)")
	cmd.Flags().String("source", defaultRemoteFileSource(), "Stable source name used in remote file document URLs")
	cmd.Flags().Bool("allow-sensitive", false, "Skip sensitive content checks, allowing matching documents to be indexed")
}

func printImportSummary(imported, skipped, errCount int) {
	msg := fmt.Sprintf("%s Imported %d document(s)", cliSuccessStyle.Render("✓"), imported)
	if skipped > 0 {
		msg += fmt.Sprintf(" (%d skipped)", skipped)
	}
	if errCount > 0 {
		msg += fmt.Sprintf(" (%d errors)", errCount)
	}
	cliPrintln(msg)
}

func isHisterJSONExport(inputFile string) (bool, error) {
	f, err := os.Open(inputFile)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	decoder := json.NewDecoder(f)
	token, err := decoder.Token()
	if err != nil {
		return false, nil
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '[' {
		return false, nil
	}
	if !decoder.More() {
		return true, nil
	}
	var first map[string]json.RawMessage
	if err := decoder.Decode(&first); err != nil {
		return false, nil
	}
	var documentURL string
	if err := json.Unmarshal(first["url"], &documentURL); err != nil || documentURL == "" {
		return false, nil
	}
	_, hasType := first["type"]
	_, hasProcessed := first["processed"]
	return hasType && hasProcessed, nil
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

	const maxLineSize = 64 * 1024 * 1024 // 64 MB covers large HTML+favicon lines
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxLineSize)
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

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var d document.Document
		if err := json.Unmarshal(line, &d); err != nil {
			log.Warn().Err(err).Msg("Failed to parse document line, skipping")
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

	if err := scanner.Err(); err != nil {
		log.Warn().Err(err).Str("file", inputFile).Msg("Failed to read input file")
		errCount++
	}

	return imported, skipped, errCount
}

func addDocumentBatch(c *client.Client, docs []*document.Document) (imported, errCount int) {
	results, err := c.AddDocumentsJSON(docs)
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
func importHTMLFile(
	c *client.Client,
	input importFileInput,
	source string,
	maxFileSize int64,
	skip bool,
	labelOverride documentLabelOverride,
) (imported, skipped, errCount int) {
	data, err := os.ReadFile(input.Path)
	if err != nil {
		log.Warn().Err(err).Str("file", input.Path).Msg("Failed to read HTML file, skipping")
		return 0, 0, 1
	}

	d, err := document.FromHTML(string(data))
	if errors.Is(err, document.ErrNoURL) {
		info, statErr := os.Stat(input.Path)
		if statErr != nil {
			log.Warn().Err(statErr).Str("file", input.Path).Msg("Failed to inspect HTML file")
			return 0, 0, 1
		}
		return importRemoteFile(c, input, data, info, source, maxFileSize, skip, labelOverride)
	}
	if err != nil {
		log.Warn().Err(err).Str("file", input.Path).Msg("Failed to import HTML file, skipping")
		return 0, 0, 1
	}

	if skip {
		exists, err := c.DocumentExists(d.URL)
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
	imported, errCount = addDocumentBatch(c, []*document.Document{d})
	return imported, 0, errCount
}
