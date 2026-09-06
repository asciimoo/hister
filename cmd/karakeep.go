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
	karakeepTokenEnv            = "HISTER_IMPORT_KARAKEEP_TOKEN"
	karakeepSourceMetadataValue = "karakeep"
	karakeepPageSize            = 100
)

var errKarakeepMissingURL = errors.New("karakeep bookmark has no URL")

type karakeepTag struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AttachedBy string `json:"attachedBy"`
}

type karakeepAsset struct {
	ID        string `json:"id"`
	AssetType string `json:"assetType"`
	FileName  string `json:"fileName"`
}

type karakeepContent struct {
	Type                     string `json:"type"`
	URL                      string `json:"url"`
	Title                    string `json:"title"`
	Description              string `json:"description"`
	ImageURL                 string `json:"imageUrl"`
	ImageAssetID             string `json:"imageAssetId"`
	ScreenshotAssetID        string `json:"screenshotAssetId"`
	PDFAssetID               string `json:"pdfAssetId"`
	FullPageArchiveAssetID   string `json:"fullPageArchiveAssetId"`
	PrecrawledArchiveAssetID string `json:"precrawledArchiveAssetId"`
	VideoAssetID             string `json:"videoAssetId"`
	Favicon                  string `json:"favicon"`
	HTMLContent              string `json:"htmlContent"`
	ContentAssetID           string `json:"contentAssetId"`
	CrawledAt                string `json:"crawledAt"`
	CrawlStatus              string `json:"crawlStatus"`
	Author                   string `json:"author"`
	Publisher                string `json:"publisher"`
	DatePublished            string `json:"datePublished"`
	DateModified             string `json:"dateModified"`
	Text                     string `json:"text"`
	SourceURL                string `json:"sourceUrl"`
	AssetType                string `json:"assetType"`
	AssetID                  string `json:"assetId"`
	FileName                 string `json:"fileName"`
	Size                     int64  `json:"size"`
	Content                  string `json:"content"`
}

type karakeepBookmark struct {
	ID                  string          `json:"id"`
	CreatedAt           string          `json:"createdAt"`
	ModifiedAt          string          `json:"modifiedAt"`
	Title               string          `json:"title"`
	Archived            bool            `json:"archived"`
	Favourited          bool            `json:"favourited"`
	TaggingStatus       string          `json:"taggingStatus"`
	SummarizationStatus string          `json:"summarizationStatus"`
	Note                string          `json:"note"`
	Summary             string          `json:"summary"`
	Source              string          `json:"source"`
	UserID              string          `json:"userId"`
	Tags                []karakeepTag   `json:"tags"`
	Content             karakeepContent `json:"content"`
	Assets              []karakeepAsset `json:"assets"`
}

type karakeepBookmarksPage struct {
	Bookmarks  []karakeepBookmark `json:"bookmarks"`
	NextCursor *string            `json:"nextCursor"`
}

type karakeepClient struct {
	*serviceAPIClient
	updatedAfter int64
}

var importKarakeepCmd = &cobra.Command{
	Use:   "karakeep INSTANCE_URL",
	Short: "Import bookmarks from Karakeep",
	Long: `Import bookmarks and their searchable content from a Karakeep instance.

Karakeep was previously named Hoarder. Set its API token with
HISTER_IMPORT_KARAKEEP_TOKEN or --api-token.

Stored HTML is used when available. When a link bookmark has no stored HTML,
Hister downloads it using the configured crawler backend. Override the backend
with --backend and --backend-option.

The global --token flag remains the access token for the destination Hister server.`,
	Args: cobra.ExactArgs(1),
	PreRun: func(_ *cobra.Command, _ []string) {
		initExtractor()
	},
	Run: func(cmd *cobra.Command, args []string) {
		token := serviceAPIToken(cmd, karakeepTokenEnv)
		if token == "" {
			exit(1, "Karakeep API token is required; set "+karakeepTokenEnv+" or use --api-token")
		}
		runtime, err := newServiceImportRuntime(cmd)
		if err != nil {
			exit(1, err.Error())
		}
		defer func() {
			if err := runtime.Close(); err != nil {
				log.Warn().Err(err).Msg("Karakeep content crawler close error")
			}
		}()
		source, err := newKarakeepClient(args[0], token, nil)
		if err != nil {
			exit(1, err.Error())
		}
		updatedAfter, err := latestServiceUpdated(runtime.target, karakeepSourceMetadataValue)
		if err != nil {
			exit(1, "Failed to find the latest Karakeep import: "+err.Error())
		}
		source.updatedAfter = updatedAfter

		stats, err := importKarakeep(
			cmd.Context(),
			source,
			runtime.target,
			runtime.languageDetector,
			runtime.contentFetcher,
			runtime.options,
		)
		if err != nil {
			exit(1, "Karakeep import failed: "+err.Error())
		}
		printImportSummary(cmd, stats.Imported, stats.Skipped, stats.Errors)
	},
}

func newKarakeepClient(instanceURL, token string, httpClient *http.Client) (*karakeepClient, error) {
	apiClient, err := newServiceAPIClient(
		karakeepSourceMetadataValue,
		instanceURL,
		token,
		karakeepTokenEnv+" or --api-token",
		httpClient,
	)
	if err != nil {
		return nil, err
	}
	return &karakeepClient{serviceAPIClient: apiClient}, nil
}

func (c *karakeepClient) bookmarks(ctx context.Context, cursor *string) (*karakeepBookmarksPage, error) {
	query := make(url.Values)
	query.Set("includeContent", "true")
	query.Set("limit", strconv.Itoa(karakeepPageSize))
	query.Set("sortOrder", "asc")
	endpoint := "/api/v1/bookmarks"
	if c.updatedAfter != 0 {
		endpoint += "/search"
		query.Set("q", "after:"+time.Unix(c.updatedAfter, 0).UTC().Format("2006-01-02"))
	}
	if cursor != nil {
		query.Set("cursor", *cursor)
	}

	var page karakeepBookmarksPage
	if err := c.getJSON(ctx, endpoint, query, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func importKarakeep(
	ctx context.Context,
	source *karakeepClient,
	target *client.Client,
	languageDetector document.LanguageDetector,
	contentFetcher serviceContentFetcher,
	options serviceImportOptions,
) (serviceImportStats, error) {
	buffer, err := newServiceImportBuffer(
		karakeepSourceMetadataValue,
		target,
		languageDetector,
		contentFetcher,
		options,
	)
	if err != nil {
		return serviceImportStats{}, err
	}
	var cursor *string
	seenCursors := make(map[string]struct{})

	for {
		page, err := source.bookmarks(ctx, cursor)
		if err != nil {
			buffer.Flush()
			return buffer.stats, err
		}
		for _, bookmark := range page.Bookmarks {
			d, contentRequest, err := karakeepDocument(bookmark, languageDetector)
			if errors.Is(err, errKarakeepMissingURL) {
				log.Debug().Str("karakeep_id", bookmark.ID).Str("type", bookmark.Content.Type).Msg("Skipping Karakeep bookmark without a URL")
				buffer.stats.Skipped++
				continue
			}
			if err != nil {
				log.Warn().Err(err).Str("karakeep_id", bookmark.ID).Msg("Failed to convert Karakeep bookmark, skipping")
				buffer.stats.Errors++
				continue
			}
			buffer.Add(ctx, d, contentRequest)
		}

		if page.NextCursor == nil {
			buffer.Flush()
			return buffer.stats, nil
		}
		if _, seen := seenCursors[*page.NextCursor]; seen {
			buffer.Flush()
			return buffer.stats, fmt.Errorf("karakeep returned the repeated pagination cursor %q", *page.NextCursor)
		}
		seenCursors[*page.NextCursor] = struct{}{}
		cursor = page.NextCursor
	}
}

func karakeepDocument(
	bookmark karakeepBookmark,
	languageDetector document.LanguageDetector,
) (*document.Document, *serviceContentRequest, error) {
	contentType := strings.ToLower(strings.TrimSpace(bookmark.Content.Type))
	rawURL := strings.TrimSpace(bookmark.Content.URL)
	if rawURL == "" {
		rawURL = strings.TrimSpace(bookmark.Content.SourceURL)
	}
	if rawURL == "" {
		return nil, nil, errKarakeepMissingURL
	}

	title := firstImportValue(bookmark.Title, bookmark.Content.Title, bookmark.Content.FileName)
	metadata := karakeepMetadata(bookmark)
	prefixText := combineImportText(bookmark.Note, bookmark.Summary, bookmark.Content.Description)
	text := prefixText
	switch contentType {
	case "text":
		text = combineImportText(prefixText, bookmark.Content.Text)
	case "asset":
		text = combineImportText(prefixText, bookmark.Content.Content)
	}

	added := parseServiceTime(bookmark.CreatedAt)
	updated := parseServiceTime(bookmark.ModifiedAt)
	if updated == 0 {
		updated = added
	}
	d := &document.Document{
		URL:      rawURL,
		Title:    title,
		Text:     text,
		Added:    added,
		Updated:  updated,
		Metadata: metadata,
	}
	setServiceFavicon(d, bookmark.Content.Favicon)
	if err := d.Process(languageDetector, nil); err != nil {
		return nil, nil, err
	}
	if title == "" {
		d.Title = d.URL
	}
	if updated != 0 {
		d.Updated = updated
	}

	var contentRequest *serviceContentRequest
	if contentType == "link" {
		contentRequest = &serviceContentRequest{
			URL:         rawURL,
			HTML:        bookmark.Content.HTMLContent,
			PrefixText:  prefixText,
			SourceTitle: title,
		}
	}
	return d, contentRequest, nil
}

func karakeepMetadata(bookmark karakeepBookmark) map[string]any {
	metadata := map[string]any{
		"source":                karakeepSourceMetadataValue,
		"karakeep_id":           bookmark.ID,
		"karakeep_type":         bookmark.Content.Type,
		"karakeep_archived":     bookmark.Archived,
		"karakeep_favourited":   bookmark.Favourited,
		"karakeep_source":       bookmark.Source,
		"karakeep_crawl_status": bookmark.Content.CrawlStatus,
	}
	optional := map[string]string{
		"description":                          bookmark.Content.Description,
		"karakeep_author":                      bookmark.Content.Author,
		"karakeep_publisher":                   bookmark.Content.Publisher,
		"karakeep_date_published":              bookmark.Content.DatePublished,
		"karakeep_date_modified":               bookmark.Content.DateModified,
		"karakeep_asset_type":                  bookmark.Content.AssetType,
		"karakeep_asset_id":                    bookmark.Content.AssetID,
		"karakeep_image_asset_id":              bookmark.Content.ImageAssetID,
		"karakeep_screenshot_asset_id":         bookmark.Content.ScreenshotAssetID,
		"karakeep_pdf_asset_id":                bookmark.Content.PDFAssetID,
		"karakeep_full_page_archive_asset_id":  bookmark.Content.FullPageArchiveAssetID,
		"karakeep_precrawled_archive_asset_id": bookmark.Content.PrecrawledArchiveAssetID,
		"karakeep_video_asset_id":              bookmark.Content.VideoAssetID,
		"karakeep_content_asset_id":            bookmark.Content.ContentAssetID,
	}
	for key, value := range optional {
		if value = strings.TrimSpace(value); value != "" {
			metadata[key] = value
		}
	}
	if bookmark.Content.Size > 0 {
		metadata["karakeep_asset_size"] = bookmark.Content.Size
	}
	if len(bookmark.Tags) > 0 {
		tags := make([]string, 0, len(bookmark.Tags))
		for _, tag := range bookmark.Tags {
			if name := strings.TrimSpace(tag.Name); name != "" {
				tags = append(tags, name)
			}
		}
		if len(tags) > 0 {
			metadata["karakeep_tags"] = tags
		}
	}
	if len(bookmark.Assets) > 0 {
		assetTypes := make([]string, 0, len(bookmark.Assets))
		for _, asset := range bookmark.Assets {
			if assetType := strings.TrimSpace(asset.AssetType); assetType != "" {
				assetTypes = append(assetTypes, assetType)
			}
		}
		if len(assetTypes) > 0 {
			metadata["karakeep_asset_types"] = assetTypes
		}
	}
	return metadata
}

func firstImportValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
