package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"mime"
	"mime/multipart"
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
	readeckTokenEnv            = "HISTER_IMPORT_READECK_TOKEN"
	readeckSourceMetadataValue = "readeck"
	readeckSyncEndpoint        = "/api/bookmarks/sync"
)

var errReadeckMissingURL = errors.New("readeck bookmark has no URL")

type readeckSyncChange struct {
	ID   string `json:"id"`
	Time string `json:"time"`
	Type string `json:"type"`
}

type readeckResource struct {
	Src    string `json:"src"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type readeckBookmark struct {
	ID            string                     `json:"id"`
	Href          string                     `json:"href"`
	Created       string                     `json:"created"`
	Updated       string                     `json:"updated"`
	State         int                        `json:"state"`
	Loaded        bool                       `json:"loaded"`
	URL           string                     `json:"url"`
	Title         string                     `json:"title"`
	SiteName      string                     `json:"site_name"`
	Site          string                     `json:"site"`
	Published     string                     `json:"published"`
	Authors       []string                   `json:"authors"`
	Lang          string                     `json:"lang"`
	TextDirection string                     `json:"text_direction"`
	DocumentType  string                     `json:"document_type"`
	Type          string                     `json:"type"`
	HasArticle    bool                       `json:"has_article"`
	Description   string                     `json:"description"`
	IsDeleted     bool                       `json:"is_deleted"`
	IsMarked      bool                       `json:"is_marked"`
	IsArchived    bool                       `json:"is_archived"`
	Labels        []string                   `json:"labels"`
	ReadProgress  int                        `json:"read_progress"`
	ReadAnchor    string                     `json:"read_anchor"`
	Resources     map[string]readeckResource `json:"resources"`
	Embed         string                     `json:"embed"`
	EmbedDomain   string                     `json:"embed_domain"`
	Errors        []string                   `json:"errors"`
	WordCount     int                        `json:"word_count"`
	ReadingTime   int                        `json:"reading_time"`
}

type readeckSyncRequest struct {
	IDs            []string `json:"id"`
	WithJSON       bool     `json:"with_json"`
	WithHTML       bool     `json:"with_html"`
	ResourcePrefix string   `json:"resource_prefix"`
}

type readeckSyncedBookmark struct {
	Bookmark readeckBookmark
	HTML     string
	hasJSON  bool
	hasHTML  bool
}

type readeckClient struct {
	*serviceAPIClient
	updatedAfter int64
}

var importReadeckCmd = &cobra.Command{
	Use:   "readeck INSTANCE_URL",
	Short: "Import bookmarks from Readeck",
	Long: `Import bookmarks and their stored article content from a Readeck instance.

Set the Readeck API token with HISTER_IMPORT_READECK_TOKEN or --api-token.

Hister uses the article HTML stored by Readeck. If a bookmark has no usable
stored article, Hister downloads it using the configured crawler backend.

The global --token flag remains the access token for the destination Hister server.`,
	Args: cobra.ExactArgs(1),
	PreRun: func(_ *cobra.Command, _ []string) {
		initExtractor()
	},
	Run: func(cmd *cobra.Command, args []string) {
		token := serviceAPIToken(cmd, readeckTokenEnv)
		if token == "" {
			exit(1, "Readeck API token is required; set "+readeckTokenEnv+" or use --api-token")
		}

		runtime, err := newServiceImportRuntime(cmd)
		if err != nil {
			exit(1, err.Error())
		}
		defer func() {
			if err := runtime.Close(); err != nil {
				log.Warn().Err(err).Msg("Readeck content crawler close error")
			}
		}()

		source, err := newReadeckClient(args[0], token, nil)
		if err != nil {
			exit(1, err.Error())
		}
		updatedAfter, err := latestServiceUpdated(runtime.target, readeckSourceMetadataValue)
		if err != nil {
			exit(1, "Failed to find the latest Readeck import: "+err.Error())
		}
		source.updatedAfter = updatedAfter

		stats, err := importReadeck(
			cmd.Context(),
			source,
			runtime.target,
			runtime.languageDetector,
			runtime.contentFetcher,
			runtime.options,
		)
		if err != nil {
			exit(1, "Readeck import failed: "+err.Error())
		}
		printImportSummary(cmd, stats.Imported, stats.Skipped, stats.Errors)
	},
}

func newReadeckClient(instanceURL, token string, httpClient *http.Client) (*readeckClient, error) {
	apiClient, err := newServiceAPIClient(
		readeckSourceMetadataValue,
		instanceURL,
		token,
		readeckTokenEnv+" or --api-token",
		httpClient,
	)
	if err != nil {
		return nil, err
	}
	return &readeckClient{serviceAPIClient: apiClient}, nil
}

func (c *readeckClient) changes(ctx context.Context) ([]readeckSyncChange, error) {
	query := make(url.Values)
	if c.updatedAfter != 0 {
		query.Set("since", time.Unix(c.updatedAfter, 0).UTC().Format(time.RFC3339))
	}
	var changes []readeckSyncChange
	if err := c.getJSON(ctx, readeckSyncEndpoint, query, &changes); err != nil {
		return nil, err
	}
	return changes, nil
}

func (c *readeckClient) changedBookmarkIDs(ctx context.Context) ([]string, error) {
	changes, err := c.changes(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(changes))
	seen := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		id := strings.TrimSpace(change.ID)
		if id == "" {
			return nil, errors.New("readeck sync change has no bookmark ID")
		}
		switch strings.ToLower(strings.TrimSpace(change.Type)) {
		case "delete":
			continue
		case "update":
		default:
			return nil, fmt.Errorf("readeck sync change %q has unknown type %q", id, change.Type)
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

func (c *readeckClient) syncBookmarks(
	ctx context.Context,
	ids []string,
) ([]readeckSyncedBookmark, int, error) {
	if len(ids) == 0 {
		return nil, 0, nil
	}
	requestBody, err := json.Marshal(readeckSyncRequest{
		IDs:            ids,
		WithJSON:       true,
		WithHTML:       true,
		ResourcePrefix: "",
	})
	if err != nil {
		return nil, 0, fmt.Errorf("encode readeck sync request: %w", err)
	}
	resp, err := c.doRequest(
		ctx,
		http.MethodPost,
		readeckSyncEndpoint,
		nil,
		"multipart/mixed",
		"application/json",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("Failed to close Readeck sync response body")
		}
	}()

	mediaType, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, 0, fmt.Errorf("parse readeck sync content type: %w", err)
	}
	if !strings.EqualFold(mediaType, "multipart/mixed") {
		return nil, 0, fmt.Errorf("readeck sync returned content type %q, want multipart/mixed", mediaType)
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return nil, 0, errors.New("readeck sync response is missing its multipart boundary")
	}

	requested := make(map[string]*readeckSyncedBookmark, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, 0, errors.New("readeck sync request has an empty bookmark ID")
		}
		if _, exists := requested[id]; exists {
			return nil, 0, fmt.Errorf("readeck sync request repeats bookmark ID %q", id)
		}
		requested[id] = &readeckSyncedBookmark{}
	}

	reader := multipart.NewReader(resp.Body, boundary)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("read readeck sync part: %w", err)
		}
		partID := strings.TrimSpace(part.Header.Get("Bookmark-Id"))
		partType := strings.ToLower(strings.TrimSpace(part.Header.Get("Type")))
		item, exists := requested[partID]
		if partID == "" || !exists {
			_ = part.Close()
			return nil, 0, fmt.Errorf("readeck sync returned an unexpected bookmark ID %q", partID)
		}
		if partType != "json" && partType != "html" {
			_ = part.Close()
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(part, maxServiceImportResponseSize+1))
		closeErr := part.Close()
		if readErr != nil {
			return nil, 0, fmt.Errorf("read readeck %s part for %q: %w", partType, partID, readErr)
		}
		if closeErr != nil {
			return nil, 0, fmt.Errorf("close readeck %s part for %q: %w", partType, partID, closeErr)
		}
		if len(content) > maxServiceImportResponseSize {
			return nil, 0, fmt.Errorf(
				"readeck %s part for %q exceeds the %d MiB limit",
				partType,
				partID,
				maxServiceImportResponseSize>>20,
			)
		}

		switch partType {
		case "json":
			if item.hasJSON {
				return nil, 0, fmt.Errorf("readeck sync repeated the JSON part for %q", partID)
			}
			if err := json.Unmarshal(content, &item.Bookmark); err != nil {
				return nil, 0, fmt.Errorf("decode readeck bookmark %q: %w", partID, err)
			}
			if item.Bookmark.ID == "" {
				item.Bookmark.ID = partID
			} else if item.Bookmark.ID != partID {
				return nil, 0, fmt.Errorf(
					"readeck sync JSON ID %q does not match part ID %q",
					item.Bookmark.ID,
					partID,
				)
			}
			item.hasJSON = true
		case "html":
			if item.hasHTML {
				return nil, 0, fmt.Errorf("readeck sync repeated the HTML part for %q", partID)
			}
			item.HTML = string(content)
			item.hasHTML = true
		}
	}

	bookmarks := make([]readeckSyncedBookmark, 0, len(ids))
	missing := 0
	for _, id := range ids {
		item := requested[id]
		if !item.hasJSON {
			missing++
			continue
		}
		bookmarks = append(bookmarks, *item)
	}
	return bookmarks, missing, nil
}

func importReadeck(
	ctx context.Context,
	source *readeckClient,
	target *client.Client,
	languageDetector document.LanguageDetector,
	contentFetcher serviceContentFetcher,
	options serviceImportOptions,
) (serviceImportStats, error) {
	buffer, err := newServiceImportBuffer(
		readeckSourceMetadataValue,
		target,
		languageDetector,
		contentFetcher,
		options,
	)
	if err != nil {
		return serviceImportStats{}, err
	}

	ids, err := source.changedBookmarkIDs(ctx)
	if err != nil {
		return buffer.stats, err
	}
	for start := 0; start < len(ids); start += options.BatchSize {
		end := min(start+options.BatchSize, len(ids))
		bookmarks, missing, err := source.syncBookmarks(ctx, ids[start:end])
		if err != nil {
			buffer.Flush()
			return buffer.stats, err
		}
		buffer.stats.Skipped += missing
		for _, synced := range bookmarks {
			if synced.Bookmark.IsDeleted {
				buffer.stats.Skipped++
				continue
			}
			d, contentRequest, err := source.document(synced, languageDetector)
			if errors.Is(err, errReadeckMissingURL) {
				log.Debug().Str("readeck_id", synced.Bookmark.ID).Msg("Skipping Readeck bookmark without a URL")
				buffer.stats.Skipped++
				continue
			}
			if err != nil {
				log.Warn().Err(err).Str("readeck_id", synced.Bookmark.ID).Msg("Failed to convert Readeck bookmark, skipping")
				buffer.stats.Errors++
				continue
			}
			buffer.Add(ctx, d, contentRequest)
		}
	}
	buffer.Flush()
	return buffer.stats, nil
}

func (c *readeckClient) document(
	synced readeckSyncedBookmark,
	languageDetector document.LanguageDetector,
) (*document.Document, *serviceContentRequest, error) {
	bookmark := synced.Bookmark
	rawURL := strings.TrimSpace(bookmark.URL)
	if rawURL == "" {
		return nil, nil, errReadeckMissingURL
	}
	added := parseServiceTime(bookmark.Created)
	updated := parseServiceTime(bookmark.Updated)
	if updated == 0 {
		updated = added
	}
	title := strings.TrimSpace(bookmark.Title)
	prefixText := combineImportText(bookmark.Description, strings.Join(cleanImportStrings(bookmark.Authors), ", "))
	d := &document.Document{
		URL:      rawURL,
		Title:    title,
		Text:     prefixText,
		Added:    added,
		Updated:  updated,
		Metadata: c.metadata(bookmark),
	}
	if icon, exists := bookmark.Resources["icon"]; exists {
		setServiceFavicon(d, c.absoluteURL(icon.Src))
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
		HTML:        readeckArticleDocument(title, synced.HTML),
		PrefixText:  prefixText,
		SourceTitle: title,
	}, nil
}

func (c *readeckClient) metadata(bookmark readeckBookmark) map[string]any {
	metadata := map[string]any{
		"source":                readeckSourceMetadataValue,
		"readeck_id":            bookmark.ID,
		"readeck_state":         bookmark.State,
		"readeck_loaded":        bookmark.Loaded,
		"readeck_has_article":   bookmark.HasArticle,
		"readeck_marked":        bookmark.IsMarked,
		"readeck_archived":      bookmark.IsArchived,
		"readeck_read_progress": bookmark.ReadProgress,
		"readeck_word_count":    bookmark.WordCount,
		"readeck_reading_time":  bookmark.ReadingTime,
	}
	optional := map[string]string{
		"description":                bookmark.Description,
		"readeck_href":               c.absoluteURL(bookmark.Href),
		"readeck_site_name":          bookmark.SiteName,
		"readeck_site":               bookmark.Site,
		"readeck_published":          bookmark.Published,
		"readeck_language":           bookmark.Lang,
		"readeck_text_direction":     bookmark.TextDirection,
		"readeck_document_type":      bookmark.DocumentType,
		"readeck_type":               bookmark.Type,
		"readeck_read_anchor":        bookmark.ReadAnchor,
		"readeck_embed":              bookmark.Embed,
		"readeck_embed_domain":       bookmark.EmbedDomain,
		"readeck_image_url":          c.resourceURL(bookmark, "image"),
		"readeck_thumbnail_url":      c.resourceURL(bookmark, "thumbnail"),
		"readeck_article_source_url": c.resourceURL(bookmark, "article"),
	}
	for key, value := range optional {
		if value = strings.TrimSpace(value); value != "" {
			metadata[key] = value
		}
	}
	if authors := cleanImportStrings(bookmark.Authors); len(authors) > 0 {
		metadata["readeck_authors"] = authors
	}
	if labels := cleanImportStrings(bookmark.Labels); len(labels) > 0 {
		metadata["readeck_labels"] = labels
	}
	if syncErrors := cleanImportStrings(bookmark.Errors); len(syncErrors) > 0 {
		metadata["readeck_errors"] = syncErrors
	}
	return metadata
}

func (c *readeckClient) resourceURL(bookmark readeckBookmark, name string) string {
	resource, exists := bookmark.Resources[name]
	if !exists {
		return ""
	}
	return c.absoluteURL(resource.Src)
}

func (c *readeckClient) absoluteURL(value string) string {
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

func cleanImportStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || slices.Contains(result, value) {
			continue
		}
		result = append(result, value)
	}
	return result
}

func readeckArticleDocument(title, article string) string {
	article = strings.TrimSpace(article)
	if article == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(article), "<html") {
		return article
	}
	return "<!doctype html><html><head><title>" + stdhtml.EscapeString(title) +
		"</title></head><body>" + article + "</body></html>"
}
