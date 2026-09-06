package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/server/document"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

const (
	wallabagTokenEnv            = "HISTER_IMPORT_WALLABAG_TOKEN"
	wallabagSourceMetadataValue = "wallabag"
	wallabagPageSize            = 100
)

var errWallabagMissingURL = errors.New("wallabag entry has no URL")

type wallabagBool bool

func (b *wallabagBool) UnmarshalJSON(data []byte) error {
	switch strings.ToLower(strings.TrimSpace(string(data))) {
	case "true", "1", `"true"`, `"1"`:
		*b = true
	case "false", "0", "null", `"false"`, `"0"`, `""`:
		*b = false
	default:
		return fmt.Errorf("invalid wallabag boolean %q", data)
	}
	return nil
}

type wallabagTags []string

func (tags *wallabagTags) UnmarshalJSON(data []byte) error {
	var values []json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		var label string
		if err := json.Unmarshal(value, &label); err != nil {
			var tag struct {
				Label string `json:"label"`
				Name  string `json:"name"`
			}
			if objectErr := json.Unmarshal(value, &tag); objectErr != nil {
				return fmt.Errorf("decode wallabag tag: %w", err)
			}
			label = firstImportValue(tag.Label, tag.Name)
		}
		if label = strings.TrimSpace(label); label != "" {
			result = append(result, label)
		}
	}
	*tags = result
	return nil
}

type wallabagEntry struct {
	ID             int64        `json:"id"`
	URL            string       `json:"url"`
	GivenURL       string       `json:"given_url"`
	OriginURL      string       `json:"origin_url"`
	Title          string       `json:"title"`
	Content        string       `json:"content"`
	CreatedAt      string       `json:"created_at"`
	UpdatedAt      string       `json:"updated_at"`
	PublishedAt    string       `json:"published_at"`
	DomainName     string       `json:"domain_name"`
	Mimetype       string       `json:"mimetype"`
	Language       string       `json:"language"`
	PreviewPicture string       `json:"preview_picture"`
	UID            string       `json:"uid"`
	HTTPStatus     string       `json:"http_status"`
	ReadingTime    int          `json:"reading_time"`
	IsArchived     wallabagBool `json:"is_archived"`
	IsStarred      wallabagBool `json:"is_starred"`
	IsPublic       wallabagBool `json:"is_public"`
	Tags           wallabagTags `json:"tags"`
	PublishedBy    []string     `json:"published_by"`
}

type wallabagEntries struct {
	Items []wallabagEntry `json:"items"`
}

type wallabagEntriesPage struct {
	Embedded *wallabagEntries `json:"_embedded"`
	Page     int              `json:"page"`
	Pages    int              `json:"pages"`
	Limit    int              `json:"limit"`
	Total    int              `json:"total"`
}

type wallabagClient struct {
	*serviceAPIClient
	updatedAfter int64
	pageSize     int
}

var importWallabagCmd = &cobra.Command{
	Use:   "wallabag INSTANCE_URL",
	Short: "Import saved entries from wallabag",
	Long: `Import saved entries and their searchable content from a wallabag instance.

Set the wallabag API access token with HISTER_IMPORT_WALLABAG_TOKEN or
--api-token.

Hister extracts the content stored by wallabag. If an entry has no usable
stored content, Hister downloads it using the configured crawler backend.

The global --token flag remains the access token for the destination Hister server.`,
	Args: cobra.ExactArgs(1),
	PreRun: func(_ *cobra.Command, _ []string) {
		initExtractor()
	},
	Run: func(cmd *cobra.Command, args []string) {
		token := serviceAPIToken(cmd, wallabagTokenEnv)
		if token == "" {
			exit(1, "wallabag API access token is required; set "+wallabagTokenEnv+" or use --api-token")
		}
		source, err := newWallabagClient(args[0], token, nil)
		if err != nil {
			exit(1, err.Error())
		}

		runtime, err := newServiceImportRuntime(cmd)
		if err != nil {
			exit(1, err.Error())
		}
		defer func() {
			if err := runtime.Close(); err != nil {
				log.Warn().Err(err).Msg("wallabag content crawler close error")
			}
		}()

		updatedAfter, err := latestServiceUpdated(runtime.target, wallabagSourceMetadataValue)
		if err != nil {
			exit(1, "Failed to find the latest wallabag import: "+err.Error())
		}
		source.updatedAfter = updatedAfter

		stats, err := importWallabag(
			cmd.Context(),
			source,
			runtime.target,
			runtime.languageDetector,
			runtime.contentFetcher,
			runtime.options,
		)
		if err != nil {
			exit(1, "wallabag import failed: "+err.Error())
		}
		printImportSummary(cmd, stats.Imported, stats.Skipped, stats.Errors)
	},
}

func newWallabagClient(
	instanceURL string,
	accessToken string,
	httpClient *http.Client,
) (*wallabagClient, error) {
	apiClient, err := newServiceAPIClient(
		wallabagSourceMetadataValue,
		instanceURL,
		accessToken,
		wallabagTokenEnv+" or --api-token",
		httpClient,
	)
	if err != nil {
		return nil, err
	}
	return &wallabagClient{
		serviceAPIClient: apiClient,
		pageSize:         wallabagPageSize,
	}, nil
}

func (c *wallabagClient) entries(ctx context.Context, page int) (*wallabagEntriesPage, error) {
	if c.pageSize < 1 {
		return nil, errors.New("wallabag page size must be positive")
	}
	if page < 1 {
		return nil, errors.New("wallabag page must be positive")
	}

	query := url.Values{
		"detail":  {"full"},
		"order":   {"asc"},
		"page":    {strconv.Itoa(page)},
		"perPage": {strconv.Itoa(c.pageSize)},
		"sort":    {"updated"},
	}
	if c.updatedAfter != 0 {
		query.Set("since", strconv.FormatInt(c.updatedAfter, 10))
	}

	var response wallabagEntriesPage
	if err := c.getJSON(ctx, "/api/entries.json", query, &response); err != nil {
		return nil, err
	}
	if response.Embedded == nil {
		return nil, errors.New("wallabag response is missing _embedded")
	}
	if response.Page == 0 {
		response.Page = page
	}
	if response.Page != page {
		return nil, fmt.Errorf("wallabag returned page %d while page %d was requested", response.Page, page)
	}
	if response.Pages > 0 && response.Pages < response.Page {
		return nil, fmt.Errorf("wallabag returned invalid page count %d for page %d", response.Pages, response.Page)
	}
	return &response, nil
}

func importWallabag(
	ctx context.Context,
	source *wallabagClient,
	target *client.Client,
	languageDetector document.LanguageDetector,
	contentFetcher serviceContentFetcher,
	options serviceImportOptions,
) (serviceImportStats, error) {
	buffer, err := newServiceImportBuffer(
		wallabagSourceMetadataValue,
		target,
		languageDetector,
		contentFetcher,
		options,
	)
	if err != nil {
		return serviceImportStats{}, err
	}

	for pageNumber := 1; ; pageNumber++ {
		page, err := source.entries(ctx, pageNumber)
		if err != nil {
			buffer.Flush()
			return buffer.stats, err
		}
		for _, entry := range page.Embedded.Items {
			d, contentRequest, err := wallabagDocument(entry, languageDetector)
			if errors.Is(err, errWallabagMissingURL) {
				log.Debug().Int64("wallabag_id", entry.ID).Msg("Skipping wallabag entry without a URL")
				buffer.stats.Skipped++
				continue
			}
			if err != nil {
				log.Warn().Err(err).Int64("wallabag_id", entry.ID).Msg("Failed to convert wallabag entry, skipping")
				buffer.stats.Errors++
				continue
			}
			buffer.Add(ctx, d, contentRequest)
		}

		if (page.Pages > 0 && page.Page >= page.Pages) || (page.Pages == 0 && len(page.Embedded.Items) < source.pageSize) {
			buffer.Flush()
			return buffer.stats, nil
		}
		if pageNumber == int(^uint(0)>>1) {
			buffer.Flush()
			return buffer.stats, errors.New("wallabag pagination did not advance")
		}
	}
}

func wallabagDocument(
	entry wallabagEntry,
	languageDetector document.LanguageDetector,
) (*document.Document, *serviceContentRequest, error) {
	rawURL := strings.TrimSpace(entry.URL)
	if rawURL == "" {
		return nil, nil, errWallabagMissingURL
	}
	added := parseServiceTime(entry.CreatedAt)
	updated := parseServiceTime(entry.UpdatedAt)
	if updated == 0 {
		updated = added
	}
	title := strings.TrimSpace(entry.Title)
	d := &document.Document{
		URL:      rawURL,
		Title:    title,
		Added:    added,
		Updated:  updated,
		Metadata: wallabagMetadata(entry),
	}
	if err := d.Process(languageDetector, nil); err != nil {
		return nil, nil, err
	}
	if title == "" {
		d.Title = d.URL
	}
	if updated != 0 {
		d.Updated = updated
	}
	return d, &serviceContentRequest{
		URL:         rawURL,
		HTML:        entry.Content,
		SourceTitle: title,
	}, nil
}

func wallabagMetadata(entry wallabagEntry) map[string]any {
	metadata := map[string]any{
		"source":                wallabagSourceMetadataValue,
		"wallabag_id":           entry.ID,
		"wallabag_archived":     bool(entry.IsArchived),
		"wallabag_starred":      bool(entry.IsStarred),
		"wallabag_public":       bool(entry.IsPublic),
		"wallabag_reading_time": entry.ReadingTime,
	}
	optional := map[string]string{
		"wallabag_domain_name":     entry.DomainName,
		"wallabag_given_url":       entry.GivenURL,
		"wallabag_http_status":     entry.HTTPStatus,
		"wallabag_language":        entry.Language,
		"wallabag_mimetype":        entry.Mimetype,
		"wallabag_origin_url":      entry.OriginURL,
		"wallabag_preview_picture": entry.PreviewPicture,
		"wallabag_published_at":    entry.PublishedAt,
		"wallabag_uid":             entry.UID,
	}
	for key, value := range optional {
		if value = strings.TrimSpace(value); value != "" {
			metadata[key] = value
		}
	}
	if len(entry.Tags) > 0 {
		metadata["wallabag_tags"] = []string(entry.Tags)
	}
	if len(entry.PublishedBy) > 0 {
		authors := make([]string, 0, len(entry.PublishedBy))
		for _, author := range entry.PublishedBy {
			if author = strings.TrimSpace(author); author != "" {
				authors = append(authors, author)
			}
		}
		if len(authors) > 0 {
			metadata["wallabag_authors"] = authors
		}
	}
	return metadata
}
