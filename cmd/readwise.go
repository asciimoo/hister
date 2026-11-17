package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/server/document"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

const (
	readwiseAPIURL              = "https://readwise.io"
	readwiseTokenEnv            = "NOGON_IMPORT_READWISE_TOKEN"
	readwiseSourceMetadataValue = "readwise"
)

var errReadwiseMissingURL = errors.New("readwise document has no URL")

type readwiseItem struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	SourceURL     string `json:"source_url"`
	Title         string `json:"title"`
	Author        string `json:"author"`
	Source        string `json:"source"`
	Category      string `json:"category"`
	Location      string `json:"location"`
	SiteName      string `json:"site_name"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	PublishedDate string `json:"published_date"`
	Summary       string `json:"summary"`
	Notes         string `json:"notes"`
	ImageURL      string `json:"image_url"`
	ParentID      string `json:"parent_id"`
	HTMLContent   string `json:"html_content"`
	Tags          map[string]struct {
		Name string `json:"name"`
	} `json:"tags"`
}

type readwiseDocumentsPage struct {
	NextPageCursor string         `json:"nextPageCursor"`
	Results        []readwiseItem `json:"results"`
}

type readwiseClient struct {
	*serviceAPIClient
	wait func(context.Context, time.Duration) error
}

var importReadwiseCmd = &cobra.Command{
	Use:   "readwise",
	Short: "Import documents from Readwise Reader",
	Long: `Import saved documents and their searchable content from Readwise Reader.

Set the Readwise access token with NOGON_IMPORT_READWISE_TOKEN or --api-token.
Get a token from https://readwise.io/access_token.

Nogon uses stored HTML when available and downloads missing content from the
original source URL with the configured crawler backend. Uploaded documents
without a source URL use their Reader URL and stored content.

Each run scans the library again. Use --skip-existing to keep existing documents.
Separate highlight and note records are skipped; document notes are preserved.

The legacy HISTER_IMPORT_READWISE_TOKEN variable is also supported. The Nogon
variable takes precedence, even when empty.

The global --token flag remains the access token for the destination Nogon server.`,
	Args: cobra.NoArgs,
	PreRun: func(_ *cobra.Command, _ []string) {
		initExtractor()
	},
	RunE: serviceImportRunE(
		"Readwise", readwiseTokenEnv, "API token",
		func(_ string, token string, httpClient *http.Client) (*readwiseClient, error) {
			return newReadwiseClient(token, httpClient)
		}, importReadwise,
		nil,
	),
}

func newReadwiseClient(token string, httpClient *http.Client) (*readwiseClient, error) {
	api, err := newServiceAPIClientWithAuthorization(
		readwiseSourceMetadataValue, readwiseAPIURL, "Token", func() string { return token },
		readwiseTokenEnv+" or --api-token", httpClient,
	)
	if err != nil {
		return nil, err
	}
	return &readwiseClient{serviceAPIClient: api, wait: waitForServiceImport}, nil
}

func (c *readwiseClient) documents(ctx context.Context, cursor string) (*readwiseDocumentsPage, error) {
	query := url.Values{"withHtmlContent": {"true"}}
	if cursor != "" {
		query.Set("pageCursor", cursor)
	}
	var page readwiseDocumentsPage
	if err := c.getJSONWithRetry(ctx, "/api/v3/list/", query, &page, c.wait); err != nil {
		return nil, err
	}
	if page.Results == nil {
		return nil, errors.New("readwise returned an invalid document list")
	}
	return &page, nil
}

func importReadwise(
	ctx context.Context,
	source *readwiseClient,
	target *client.Client,
	languageDetector document.LanguageDetector,
	contentFetcher serviceContentFetcher,
	options serviceImportOptions,
) (serviceImportStats, error) {
	buffer, err := newServiceImportBuffer(readwiseSourceMetadataValue, target, languageDetector, contentFetcher, options)
	if err != nil {
		return serviceImportStats{}, err
	}
	err = source.walkDocuments(ctx, func(item readwiseItem) {
		// Child records can share their parent's source URL. Importing them as
		// documents would replace the parent's full content with a short passage.
		if item.ParentID != "" || item.Category == "highlight" || item.Category == "note" {
			buffer.stats.Skipped++
			return
		}
		d, request, err := readwiseDocument(item, languageDetector)
		if errors.Is(err, errReadwiseMissingURL) {
			buffer.stats.Skipped++
			return
		}
		if err != nil {
			log.Warn().Err(err).Str("readwise_id", item.ID).Msg("Failed to convert Readwise document, skipping")
			buffer.stats.Errors++
			return
		}
		buffer.Add(ctx, d, request)
	})
	buffer.Flush()
	return buffer.stats, err
}

func (c *readwiseClient) walkDocuments(ctx context.Context, visit func(readwiseItem)) error {
	cursor := ""
	seenCursors := make(map[string]bool)
	seenIDs := make(map[string]bool)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := c.documents(ctx, cursor)
		if err != nil {
			return err
		}
		for _, item := range page.Results {
			if err := ctx.Err(); err != nil {
				return err
			}
			if item.ID != "" {
				if seenIDs[item.ID] {
					continue
				}
				seenIDs[item.ID] = true
			}
			visit(item)
		}
		cursor = page.NextPageCursor
		if cursor == "" {
			return ctx.Err()
		}
		if seenCursors[cursor] {
			return errors.New("readwise returned a repeated pagination cursor")
		}
		seenCursors[cursor] = true
	}
}

func readwiseDocument(item readwiseItem, languageDetector document.LanguageDetector) (*document.Document, *serviceContentRequest, error) {
	rawURL := firstImportValue(item.SourceURL, item.URL)
	if rawURL == "" {
		return nil, nil, errReadwiseMissingURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return nil, nil, errors.New("readwise document URL must be an http or https URL with a host and no embedded credentials")
	}
	title := strings.TrimSpace(item.Title)
	prefixText := combineImportText(item.Summary, item.Notes)
	d := &document.Document{
		URL: rawURL, Title: title, Text: prefixText, Added: parseServiceTime(item.CreatedAt), Metadata: readwiseMetadata(item),
	}
	if err := d.Process(languageDetector, nil); err != nil {
		return nil, nil, err
	}
	d.Updated = d.Added
	if updated := parseServiceTime(item.UpdatedAt); updated != 0 {
		d.Updated = updated
	}
	if title == "" {
		d.Title = d.URL
	}
	// Reader URLs require a login, so there is no public page to fetch when an
	// uploaded document has neither stored HTML nor an original source URL.
	if strings.TrimSpace(item.HTMLContent) == "" && strings.TrimSpace(item.SourceURL) == "" {
		return d, nil, nil
	}
	return d, &serviceContentRequest{
		URL: rawURL, HTML: serviceArticleDocument(title, item.HTMLContent), PrefixText: prefixText, SourceTitle: title,
		StoredOnly: strings.TrimSpace(item.SourceURL) == "",
	}, nil
}

func readwiseMetadata(item readwiseItem) map[string]any {
	metadata := map[string]any{"source": readwiseSourceMetadataValue, "readwise_id": item.ID}
	for key, value := range map[string]string{
		"readwise_url": item.URL, "readwise_source_url": item.SourceURL, "readwise_author": item.Author,
		"readwise_category": item.Category, "readwise_location": item.Location, "readwise_site_name": item.SiteName,
		"readwise_source": item.Source, "readwise_published_date": item.PublishedDate, "readwise_image_url": item.ImageURL,
		"description": item.Summary, "readwise_notes": item.Notes,
	} {
		if value = strings.TrimSpace(value); value != "" {
			metadata[key] = value
		}
	}
	var tags []string
	for key, tag := range item.Tags {
		tags = append(tags, firstImportValue(tag.Name, key))
	}
	tags = cleanImportStrings(tags)
	slices.Sort(tags)
	if len(tags) > 0 {
		metadata["readwise_tags"] = tags
	}
	return metadata
}
