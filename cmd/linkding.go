package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/server/document"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

const (
	linkdingTokenEnv            = "HISTER_IMPORT_LINKDING_TOKEN"
	linkdingSourceMetadataValue = "linkding"
	linkdingPageSize            = 100
)

var errLinkdingMissingURL = errors.New("linkding bookmark has no URL")

type linkdingBookmark struct {
	ID                    int64    `json:"id"`
	URL                   string   `json:"url"`
	Title                 string   `json:"title"`
	Description           string   `json:"description"`
	Notes                 string   `json:"notes"`
	WebArchiveSnapshotURL string   `json:"web_archive_snapshot_url"`
	FaviconURL            string   `json:"favicon_url"`
	PreviewImageURL       string   `json:"preview_image_url"`
	IsArchived            bool     `json:"is_archived"`
	Unread                bool     `json:"unread"`
	Shared                bool     `json:"shared"`
	TagNames              []string `json:"tag_names"`
	DateAdded             string   `json:"date_added"`
	DateModified          string   `json:"date_modified"`
}

type linkdingBookmarksPage struct {
	Count    int                `json:"count"`
	Next     *string            `json:"next"`
	Previous *string            `json:"previous"`
	Results  []linkdingBookmark `json:"results"`
}

type linkdingClient struct {
	*serviceAPIClient
	updatedAfter int64
	pageSize     int
}

var importLinkdingCmd = &cobra.Command{
	Use:   "linkding INSTANCE_URL",
	Short: "Import bookmarks from Linkding",
	Long: `Import bookmarks, notes, and searchable page content from a Linkding instance.

Set the Linkding API token with HISTER_IMPORT_LINKDING_TOKEN or --api-token.

Linkding stores bookmark metadata rather than complete page content. Hister
downloads linked pages using the configured crawler backend. Override the
backend with --backend and --backend-option.

The global --token flag remains the access token for the destination Hister server.`,
	Args: cobra.ExactArgs(1),
	PreRun: func(_ *cobra.Command, _ []string) {
		initExtractor()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		token := serviceAPIToken(cmd, linkdingTokenEnv)
		if token == "" {
			return fmt.Errorf("missing Linkding API token; set %s or use --api-token", linkdingTokenEnv)
		}

		runtime, err := newServiceImportRuntime(cmd)
		if err != nil {
			return err
		}
		defer func() {
			if err := runtime.Close(); err != nil {
				log.Warn().Err(err).Msg("Linkding content crawler close error")
			}
		}()

		source, err := newLinkdingClient(args[0], token, nil)
		if err != nil {
			return err
		}
		updatedAfter, err := latestServiceUpdated(runtime.target, linkdingSourceMetadataValue)
		if err != nil {
			return fmt.Errorf("failed to find the latest Linkding import: %w", err)
		}
		source.updatedAfter = updatedAfter

		stats, err := importLinkding(
			cmd.Context(),
			source,
			runtime.target,
			runtime.languageDetector,
			runtime.contentFetcher,
			runtime.options,
		)
		if err != nil {
			err = fmt.Errorf("import from Linkding failed: %w", err)
		}
		return finishImport(cmd, stats, err)
	},
}

func newLinkdingClient(instanceURL, token string, httpClient *http.Client) (*linkdingClient, error) {
	apiClient, err := newServiceAPIClientWithAuthorization(
		linkdingSourceMetadataValue,
		instanceURL,
		"Token",
		func() string { return token },
		linkdingTokenEnv+" or --api-token",
		httpClient,
	)
	if err != nil {
		return nil, err
	}
	return &linkdingClient{
		serviceAPIClient: apiClient,
		pageSize:         linkdingPageSize,
	}, nil
}

func (c *linkdingClient) bookmarks(ctx context.Context, endpoint string, offset int) (*linkdingBookmarksPage, error) {
	if c.pageSize < 1 {
		return nil, errors.New("linkding page size must be positive")
	}
	if offset < 0 {
		return nil, errors.New("linkding pagination offset must not be negative")
	}

	query := url.Values{
		"limit":  {strconv.Itoa(c.pageSize)},
		"offset": {strconv.Itoa(offset)},
	}
	if c.updatedAfter != 0 {
		query.Set("modified_since", time.Unix(c.updatedAfter, 0).UTC().Format(time.RFC3339))
	}
	var page linkdingBookmarksPage
	if err := c.getJSON(ctx, endpoint, query, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func linkdingNextOffset(next *string, current int) (int, bool, error) {
	if next == nil {
		return 0, false, nil
	}
	nextURL, err := url.Parse(strings.TrimSpace(*next))
	if err != nil {
		return 0, false, fmt.Errorf("parse next page URL: %w", err)
	}
	rawOffset := nextURL.Query().Get("offset")
	if rawOffset == "" {
		return 0, false, errors.New("next page URL is missing its offset")
	}
	nextOffset, err := strconv.Atoi(rawOffset)
	if err != nil {
		return 0, false, fmt.Errorf("parse next page offset: %w", err)
	}
	if nextOffset <= current {
		return 0, false, fmt.Errorf("pagination offset did not advance from %d to %d", current, nextOffset)
	}
	return nextOffset, true, nil
}

func (c *linkdingClient) walkBookmarks(ctx context.Context, visit func(linkdingBookmark)) error {
	seen := make(map[int64]struct{})
	endpoints := []struct {
		path     string
		archived bool
	}{
		{path: "/api/bookmarks/"},
		{path: "/api/bookmarks/archived/", archived: true},
	}

	for _, endpoint := range endpoints {
		for offset := 0; ; {
			page, err := c.bookmarks(ctx, endpoint.path, offset)
			if err != nil {
				return err
			}
			for _, bookmark := range page.Results {
				bookmark.IsArchived = bookmark.IsArchived || endpoint.archived
				if bookmark.ID != 0 {
					if _, exists := seen[bookmark.ID]; exists {
						continue
					}
					seen[bookmark.ID] = struct{}{}
				}
				visit(bookmark)
			}

			nextOffset, more, err := linkdingNextOffset(page.Next, offset)
			if err != nil {
				return fmt.Errorf("linkding %s: %w", endpoint.path, err)
			}
			if !more {
				break
			}
			offset = nextOffset
		}
	}
	return nil
}

func importLinkding(
	ctx context.Context,
	source *linkdingClient,
	target *client.Client,
	languageDetector document.LanguageDetector,
	contentFetcher serviceContentFetcher,
	options serviceImportOptions,
) (serviceImportStats, error) {
	buffer, err := newServiceImportBuffer(
		linkdingSourceMetadataValue,
		target,
		languageDetector,
		contentFetcher,
		options,
	)
	if err != nil {
		return serviceImportStats{}, err
	}

	err = source.walkBookmarks(ctx, func(bookmark linkdingBookmark) {
		d, contentRequest, err := source.document(bookmark, languageDetector)
		if errors.Is(err, errLinkdingMissingURL) {
			log.Debug().Int64("linkding_id", bookmark.ID).Msg("Skipping Linkding bookmark without a URL")
			buffer.stats.Skipped++
			return
		}
		if err != nil {
			log.Warn().Err(err).Int64("linkding_id", bookmark.ID).Msg("Failed to convert Linkding bookmark, skipping")
			buffer.stats.Errors++
			return
		}
		buffer.Add(ctx, d, contentRequest)
	})
	buffer.Flush()
	return buffer.stats, err
}

func (c *linkdingClient) document(
	bookmark linkdingBookmark,
	languageDetector document.LanguageDetector,
) (*document.Document, *serviceContentRequest, error) {
	rawURL := strings.TrimSpace(bookmark.URL)
	if rawURL == "" {
		return nil, nil, errLinkdingMissingURL
	}
	added := parseServiceTime(bookmark.DateAdded)
	updated := parseServiceTime(bookmark.DateModified)
	if updated == 0 {
		updated = added
	}
	title := strings.TrimSpace(bookmark.Title)
	prefixText := combineImportText(bookmark.Description, bookmark.Notes)
	d := &document.Document{
		URL:      rawURL,
		Title:    title,
		Text:     prefixText,
		Added:    added,
		Updated:  updated,
		Metadata: c.metadata(bookmark),
	}
	setServiceFavicon(d, c.absoluteURL(bookmark.FaviconURL))
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
		PrefixText:  prefixText,
		SourceTitle: title,
	}, nil
}

func (c *linkdingClient) metadata(bookmark linkdingBookmark) map[string]any {
	metadata := map[string]any{
		"source":            linkdingSourceMetadataValue,
		"linkding_id":       bookmark.ID,
		"linkding_archived": bookmark.IsArchived,
		"linkding_unread":   bookmark.Unread,
		"linkding_shared":   bookmark.Shared,
	}
	optional := map[string]string{
		"description":                       bookmark.Description,
		"linkding_notes":                    bookmark.Notes,
		"linkding_web_archive_snapshot_url": c.absoluteURL(bookmark.WebArchiveSnapshotURL),
		"linkding_favicon_url":              c.absoluteURL(bookmark.FaviconURL),
		"linkding_preview_image_url":        c.absoluteURL(bookmark.PreviewImageURL),
	}
	for key, value := range optional {
		if value = strings.TrimSpace(value); value != "" {
			metadata[key] = value
		}
	}
	if len(bookmark.TagNames) > 0 {
		tags := make([]string, 0, len(bookmark.TagNames))
		seen := make(map[string]struct{}, len(bookmark.TagNames))
		for _, tag := range bookmark.TagNames {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			if _, exists := seen[tag]; exists {
				continue
			}
			seen[tag] = struct{}{}
			tags = append(tags, tag)
		}
		if len(tags) > 0 {
			metadata["linkding_tags"] = tags
		}
	}
	return metadata
}

func (c *linkdingClient) absoluteURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	reference, err := url.Parse(value)
	if err != nil || reference.IsAbs() {
		return value
	}
	base, err := url.Parse(c.baseURL + "/")
	if err != nil {
		return value
	}
	return base.ResolveReference(reference).String()
}
