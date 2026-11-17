package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/server/document"

	"github.com/rs/zerolog/log"
)

const (
	raindropAPIURL   = "https://api.raindrop.io/rest/v1"
	raindropPageSize = 50
)

type raindropReference struct {
	ID int64 `json:"$id"`
}

type raindropHighlight struct {
	ID      string `json:"_id"`
	Text    string `json:"text"`
	Note    string `json:"note"`
	Color   string `json:"color"`
	Created string `json:"created"`
}

type raindropItem struct {
	ID         int64               `json:"_id"`
	URL        string              `json:"link"`
	Title      string              `json:"title"`
	Excerpt    string              `json:"excerpt"`
	Note       string              `json:"note"`
	Created    string              `json:"created"`
	Updated    string              `json:"lastUpdate"`
	Tags       []string            `json:"tags"`
	Cover      string              `json:"cover"`
	Type       string              `json:"type"`
	Important  bool                `json:"important"`
	Broken     bool                `json:"broken"`
	Collection raindropReference   `json:"collection"`
	Highlights []raindropHighlight `json:"highlights"`
}

type raindropCollection struct {
	ID     int64             `json:"_id"`
	Title  string            `json:"title"`
	Parent raindropReference `json:"parent"`
}

type raindropClient struct {
	*serviceAPIClient
	pageSize int
	wait     func(context.Context, time.Duration) error
}

func newRaindropClient(token string, httpClient *http.Client) (*raindropClient, error) {
	api, err := newServiceAPIClient(raindropSourceMetadataValue, raindropAPIURL, token, raindropTokenEnv+" or --api-token", httpClient)
	if err != nil {
		return nil, err
	}
	return &raindropClient{serviceAPIClient: api, pageSize: raindropPageSize, wait: waitForServiceImport}, nil
}

func (c *raindropClient) bookmarks(ctx context.Context, pageNumber int) ([]raindropItem, error) {
	if c.pageSize < 1 || c.pageSize > raindropPageSize || pageNumber < 0 {
		return nil, errors.New("invalid Raindrop pagination parameters")
	}
	query := url.Values{
		"page": {strconv.Itoa(pageNumber)}, "perpage": {strconv.Itoa(c.pageSize)}, "sort": {"created"},
	}
	var response struct {
		Result bool           `json:"result"`
		Items  []raindropItem `json:"items"`
	}
	if err := c.getJSON(ctx, "/raindrops/0", query, &response); err != nil {
		return nil, err
	}
	if !response.Result || response.Items == nil {
		return nil, errors.New("raindrop returned an unsuccessful or invalid bookmarks response")
	}
	return response.Items, nil
}

func (c *raindropClient) collections(ctx context.Context) (map[int64]raindropCollection, error) {
	collections := make(map[int64]raindropCollection)
	for _, endpoint := range []string{"/collections", "/collections/childrens"} {
		var response struct {
			Result bool                 `json:"result"`
			Items  []raindropCollection `json:"items"`
		}
		if err := c.getJSON(ctx, endpoint, nil, &response); err != nil {
			return nil, err
		}
		if !response.Result || response.Items == nil {
			return nil, errors.New("raindrop returned an unsuccessful or invalid collections response")
		}
		for _, collection := range response.Items {
			collections[collection.ID] = collection
		}
	}
	return collections, nil
}

func raindropFolder(id int64, collections map[int64]raindropCollection) string {
	if id == -1 {
		return "Unsorted"
	}
	var parts []string
	seen := make(map[int64]bool)
	for id > 0 && !seen[id] {
		collection, ok := collections[id]
		if !ok {
			break
		}
		seen[id] = true
		if title := strings.TrimSpace(collection.Title); title != "" {
			parts = append(parts, title)
		}
		id = collection.Parent.ID
	}
	slices.Reverse(parts)
	return strings.Join(parts, "/")
}

func (item raindropItem) updatedTime() int64 {
	if updated := parseServiceTime(item.Updated); updated != 0 {
		return updated
	}
	return parseServiceTime(item.Created)
}

func (item raindropItem) bookmark(collections map[int64]raindropCollection) raindropBookmark {
	metadata := map[string]any{
		"raindrop_import": "api", "raindrop_id": item.ID, "raindrop_collection_id": item.Collection.ID,
		"raindrop_favorite": item.Important, "raindrop_broken": item.Broken, "raindrop_type": item.Type,
	}
	var highlights []string
	for _, highlight := range item.Highlights {
		highlights = append(highlights, highlight.Text, highlight.Note)
	}
	if len(item.Highlights) > 0 {
		metadata["raindrop_highlight_details"] = item.Highlights
	}
	return raindropBookmark{
		URL: item.URL, Title: item.Title, Created: item.Created, Updated: item.Updated,
		Excerpt: item.Excerpt, Note: item.Note, Cover: item.Cover, Tags: item.Tags,
		Highlights: combineImportText(highlights...), Folder: raindropFolder(item.Collection.ID, collections), Metadata: metadata,
	}
}

func importRaindrop(
	ctx context.Context,
	source *raindropClient,
	target *client.Client,
	languageDetector document.LanguageDetector,
	contentFetcher serviceContentFetcher,
	options serviceImportOptions,
) (serviceImportStats, error) {
	buffer, err := newServiceImportBuffer(raindropSourceMetadataValue, target, languageDetector, contentFetcher, options)
	if err != nil {
		return serviceImportStats{}, err
	}
	collections, err := source.collections(ctx)
	if err != nil {
		return buffer.stats, err
	}
	type pendingItem struct {
		raindropItem
		updated int64
	}
	var items []pendingItem
	seen := make(map[int64]bool)
	for pageNumber := 0; ; pageNumber++ {
		page, err := source.bookmarks(ctx, pageNumber)
		if err != nil {
			return buffer.stats, err
		}
		newItems := 0
		for _, item := range page {
			if item.ID <= 0 {
				return buffer.stats, errors.New("raindrop returned a bookmark without a valid ID")
			}
			if seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			newItems++
			updated := item.updatedTime()
			items = append(items, pendingItem{raindropItem: item, updated: updated})
		}
		if len(page) > 0 && newItems == 0 {
			return buffer.stats, fmt.Errorf("raindrop pagination did not advance on page %d", pageNumber)
		}
		if len(page) < source.pageSize {
			break
		}
	}
	// Apply newer bookmarks last when several records normalize to the same URL.
	// Parse update timestamps once per record instead of inside the comparator.
	slices.SortStableFunc(items, func(a, b pendingItem) int {
		return cmp.Compare(a.updated, b.updated)
	})
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			buffer.Flush()
			return buffer.stats, err
		}
		if strings.TrimSpace(item.URL) == "" {
			buffer.stats.Skipped++
			continue
		}
		d, request, err := raindropDocument(item.bookmark(collections), languageDetector)
		if err != nil {
			log.Warn().Err(err).Int64("raindrop_id", item.ID).Msg("Failed to convert Raindrop bookmark, skipping")
			buffer.stats.Errors++
			continue
		}
		buffer.Add(ctx, d, request)
	}
	buffer.Flush()
	return buffer.stats, ctx.Err()
}
