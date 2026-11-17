package cmd

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/server/document"
)

// raindropCSVReader maps exported columns by name so both collection exports
// and full backups can supply their optional bookmark fields in any order.
type raindropCSVReader struct {
	reader  *csv.Reader
	columns []string
}

func newRaindropCSVReader(input io.Reader) (*raindropCSVReader, error) {
	buffered := bufio.NewReader(input)
	// Strip the UTF 8 BOM before CSV parsing, including when the header is quoted.
	if prefix, _ := buffered.Peek(3); string(prefix) == "\xef\xbb\xbf" {
		if _, err := buffered.Discard(3); err != nil {
			return nil, err
		}
	}
	reader := csv.NewReader(buffered)
	columns, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return nil, errors.New("raindrop CSV is empty; a header with a url column is required")
	}
	if err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}
	seen := make(map[string]bool, len(columns))
	for i, column := range columns {
		column = strings.ToLower(strings.TrimSpace(column))
		if column != "" && seen[column] {
			return nil, fmt.Errorf("duplicate CSV column %q", column)
		}
		seen[column] = true
		columns[i] = column
	}
	if !seen["url"] {
		return nil, errors.New("raindrop CSV is missing the required url column")
	}
	return &raindropCSVReader{reader: reader, columns: columns}, nil
}

func (r *raindropCSVReader) Read() (map[string]string, error) {
	values, err := r.reader.Read()
	if err != nil {
		return nil, err
	}
	bookmark := make(map[string]string, len(r.columns))
	for i, column := range r.columns {
		bookmark[column] = strings.TrimSpace(values[i])
	}
	return bookmark, nil
}

func importRaindropCSV(
	ctx context.Context,
	input io.Reader,
	target *client.Client,
	languageDetector document.LanguageDetector,
	contentFetcher serviceContentFetcher,
	options serviceImportOptions,
) (serviceImportStats, error) {
	if err := ctx.Err(); err != nil {
		return serviceImportStats{}, err
	}
	source, err := newRaindropCSVReader(input)
	if err != nil {
		return serviceImportStats{}, err
	}
	buffer, err := newServiceImportBuffer(raindropSourceMetadataValue, target, languageDetector, contentFetcher, options)
	if err != nil {
		return serviceImportStats{}, err
	}

	for record := 2; ; record++ {
		if err = ctx.Err(); err != nil {
			break
		}
		var bookmark map[string]string
		bookmark, err = source.Read()
		if errors.Is(err, io.EOF) {
			err = nil
			break
		}
		if errors.Is(err, csv.ErrFieldCount) {
			log.Warn().Err(err).Int("record", record).Msg("Skipping Raindrop CSV record with an incorrect number of fields")
			buffer.stats.Errors++
			continue
		}
		if err != nil {
			err = fmt.Errorf("read CSV record %d: %w", record, err)
			break
		}
		if bookmark["url"] == "" {
			log.Debug().Int("record", record).Msg("Skipping Raindrop bookmark without a URL")
			buffer.stats.Skipped++
			continue
		}
		d, request, conversionErr := raindropDocument(raindropCSVBookmark(bookmark), languageDetector)
		if conversionErr != nil {
			log.Warn().Err(conversionErr).Int("record", record).Msg("Failed to convert Raindrop bookmark, skipping")
			buffer.stats.Errors++
			continue
		}
		buffer.Add(ctx, d, request)
	}
	buffer.Flush()
	return buffer.stats, err
}

func raindropCSVBookmark(record map[string]string) raindropBookmark {
	metadata := make(map[string]any)
	for _, key := range []string{"id", "favorite"} {
		if value := record[key]; value != "" {
			metadata["raindrop_"+key] = value
		}
	}
	return raindropBookmark{
		URL: record["url"], Title: record["title"], Created: record["created"],
		Excerpt: record["excerpt"], Note: record["note"], Highlights: record["highlights"],
		Folder: record["folder"], Cover: record["cover"], Tags: strings.Split(record["tags"], ","),
		Metadata: metadata,
	}
}
