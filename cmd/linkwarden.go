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
	linkwardenTokenEnv            = "HISTER_IMPORT_LINKWARDEN_TOKEN"
	linkwardenSourceMetadataValue = "linkwarden"
)

var errLinkwardenMissingURL = errors.New("linkwarden record has no URL")

type linkwardenTag struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type linkwardenCollection struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type linkwardenLink struct {
	ID          int64                 `json:"id"`
	Name        string                `json:"name"`
	Type        string                `json:"type"`
	Description string                `json:"description"`
	URL         string                `json:"url"`
	TextContent string                `json:"textContent"`
	ImportDate  string                `json:"importDate"`
	CreatedAt   string                `json:"createdAt"`
	UpdatedAt   string                `json:"updatedAt"`
	Tags        []linkwardenTag       `json:"tags"`
	Collection  *linkwardenCollection `json:"collection"`
}

type linkwardenSearchData struct {
	NextCursor *int             `json:"nextCursor"`
	Links      []linkwardenLink `json:"links"`
}

type linkwardenSearchResponse struct {
	Data *linkwardenSearchData `json:"data"`
}

type linkwardenClient struct {
	*serviceAPIClient
	updatedAfter int64
}

type (
	linkwardenImportOptions  = serviceImportOptions
	linkwardenImportStats    = serviceImportStats
	linkwardenContentFetcher = serviceContentFetcher
)

var importLinkwardenCmd = &cobra.Command{
	Use:   "linkwarden INSTANCE_URL",
	Short: "Import bookmarks from Linkwarden",
	Long: `Import bookmarks and their searchable content from a Linkwarden instance.

Set the Linkwarden API token with HISTER_IMPORT_LINKWARDEN_TOKEN or --api-token.

When a URL record has no extracted text, Hister downloads it using the configured
crawler backend. Override the backend with --backend and --backend-option.

The global --token flag remains the access token for the destination Hister server.`,
	Args: cobra.ExactArgs(1),
	PreRun: func(_ *cobra.Command, _ []string) {
		initExtractor()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		token := linkwardenAPIToken(cmd)
		if token == "" {
			return fmt.Errorf("missing Linkwarden API token; set %s or use --api-token", linkwardenTokenEnv)
		}

		runtime, err := newServiceImportRuntime(cmd)
		if err != nil {
			return err
		}
		defer func() {
			if err := runtime.Close(); err != nil {
				log.Warn().Err(err).Msg("Linkwarden content crawler close error")
			}
		}()
		source, err := newLinkwardenClient(args[0], token, nil)
		if err != nil {
			return err
		}

		updatedAfter, err := latestLinkwardenUpdated(runtime.target)
		if err != nil {
			return fmt.Errorf("failed to find the latest Linkwarden import: %w", err)
		}
		source.updatedAfter = updatedAfter

		stats, err := importLinkwarden(
			cmd.Context(),
			source,
			runtime.target,
			runtime.languageDetector,
			runtime.contentFetcher,
			runtime.options,
		)
		if err != nil {
			err = fmt.Errorf("import from Linkwarden failed: %w", err)
		}
		return finishImport(cmd, stats, err)
	},
}

func linkwardenAPIToken(cmd *cobra.Command) string {
	return serviceAPIToken(cmd, linkwardenTokenEnv)
}

func newLinkwardenClient(instanceURL, token string, httpClient *http.Client) (*linkwardenClient, error) {
	apiClient, err := newServiceAPIClient(
		linkwardenSourceMetadataValue,
		instanceURL,
		token,
		linkwardenTokenEnv+" or --api-token",
		httpClient,
	)
	if err != nil {
		return nil, err
	}
	return &linkwardenClient{serviceAPIClient: apiClient}, nil
}

func (c *linkwardenClient) search(ctx context.Context, cursor *int) (*linkwardenSearchData, error) {
	query := make(url.Values)
	if c.updatedAfter != 0 {
		query.Set("searchQueryString", "after:"+time.Unix(c.updatedAfter, 0).UTC().Format("2006-01-02"))
	}
	if cursor != nil {
		query.Set("cursor", strconv.Itoa(*cursor))
	}
	var response linkwardenSearchResponse
	if err := c.getJSON(ctx, "/api/v1/search", query, &response); err != nil {
		return nil, err
	}
	if response.Data == nil {
		return nil, errors.New("linkwarden response is missing data")
	}
	return response.Data, nil
}

func latestLinkwardenUpdated(target *client.Client) (int64, error) {
	return latestServiceUpdated(target, linkwardenSourceMetadataValue)
}

func importLinkwarden(
	ctx context.Context,
	source *linkwardenClient,
	target *client.Client,
	languageDetector document.LanguageDetector,
	contentFetcher linkwardenContentFetcher,
	options linkwardenImportOptions,
) (linkwardenImportStats, error) {
	buffer, err := newServiceImportBuffer(
		linkwardenSourceMetadataValue,
		target,
		languageDetector,
		contentFetcher,
		options,
	)
	if err != nil {
		return linkwardenImportStats{}, err
	}
	var cursor *int
	seenCursors := make(map[int]struct{})

	for {
		page, err := source.search(ctx, cursor)
		if err != nil {
			buffer.Flush()
			return buffer.stats, err
		}

		for _, link := range page.Links {
			d, err := linkwardenDocument(link, languageDetector)
			if errors.Is(err, errLinkwardenMissingURL) {
				log.Debug().Int64("linkwarden_id", link.ID).Msg("Skipping Linkwarden record without a URL")
				buffer.stats.Skipped++
				continue
			}
			if err != nil {
				log.Warn().Err(err).Int64("linkwarden_id", link.ID).Msg("Failed to convert Linkwarden record, skipping")
				buffer.stats.Errors++
				continue
			}
			var contentRequest *serviceContentRequest
			if linkwardenNeedsContentDownload(link) {
				contentRequest = &serviceContentRequest{
					URL:         strings.TrimSpace(link.URL),
					PrefixText:  link.Description,
					SourceTitle: strings.TrimSpace(link.Name),
				}
			}
			buffer.Add(ctx, d, contentRequest)
		}

		if page.NextCursor == nil {
			buffer.Flush()
			return buffer.stats, nil
		}
		if _, seen := seenCursors[*page.NextCursor]; seen {
			buffer.Flush()
			return buffer.stats, fmt.Errorf("linkwarden returned the repeated pagination cursor %d", *page.NextCursor)
		}
		seenCursors[*page.NextCursor] = struct{}{}
		cursor = page.NextCursor
	}
}

func linkwardenNeedsContentDownload(link linkwardenLink) bool {
	return strings.TrimSpace(link.TextContent) == "" && (link.Type == "" || strings.EqualFold(link.Type, "url"))
}

func linkwardenDocument(link linkwardenLink, languageDetector document.LanguageDetector) (*document.Document, error) {
	linkURL := strings.TrimSpace(link.URL)
	if linkURL == "" {
		return nil, errLinkwardenMissingURL
	}

	metadata := map[string]any{
		"source":          linkwardenSourceMetadataValue,
		"linkwarden_id":   link.ID,
		"linkwarden_type": link.Type,
	}
	if description := strings.TrimSpace(link.Description); description != "" {
		metadata["description"] = description
	}
	if link.Collection != nil {
		metadata["linkwarden_collection"] = link.Collection.Name
		metadata["linkwarden_collection_id"] = link.Collection.ID
	}
	if len(link.Tags) > 0 {
		tags := make([]string, 0, len(link.Tags))
		for _, tag := range link.Tags {
			if name := strings.TrimSpace(tag.Name); name != "" {
				tags = append(tags, name)
			}
		}
		if len(tags) > 0 {
			metadata["linkwarden_tags"] = tags
		}
	}

	added := parseServiceTime(link.ImportDate)
	if added == 0 {
		added = parseServiceTime(link.CreatedAt)
	}
	d := &document.Document{
		URL:      linkURL,
		Title:    strings.TrimSpace(link.Name),
		Text:     combineLinkwardenText(link.Description, link.TextContent),
		Added:    added,
		Updated:  parseServiceTime(link.UpdatedAt),
		Metadata: metadata,
	}
	sourceUpdated := d.Updated
	if err := d.Process(languageDetector, nil); err != nil {
		return nil, err
	}
	if d.Title == "" {
		d.Title = d.URL
	}
	if sourceUpdated != 0 {
		d.Updated = sourceUpdated
	}
	return d, nil
}

func combineLinkwardenText(description, textContent string) string {
	return combineImportText(description, textContent)
}
