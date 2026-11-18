package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/server/document"
)

func TestImportLinkdingPaginatesAndMapsBookmarks(t *testing.T) {
	var sourcePages []string
	sourceHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			return nil, fmt.Errorf("unexpected Linkding method %s", req.Method)
		}
		if got := req.Header.Get("Authorization"); got != "Token linkding-secret" {
			t.Errorf("Authorization = %q, want Token linkding-secret", got)
		}
		if got := req.URL.Query().Get("limit"); got != "2" {
			t.Errorf("limit = %q, want 2", got)
		}
		if got := req.URL.Query().Get("modified_since"); got != "" {
			t.Errorf("unexpected modified_since = %q", got)
		}

		page := req.URL.Path + ":" + req.URL.Query().Get("offset")
		sourcePages = append(sourcePages, page)
		var response string
		switch page {
		case "/base/api/bookmarks/:0":
			response = `{
				"count": 3,
				"next": "https://linkding.example/base/api/bookmarks/?limit=2&offset=2",
				"previous": null,
				"results": [
					{
						"id": 1,
						"url": "https://example.com/article?utm_source=linkding&keep=1#section",
						"title": "Saved article",
						"description": "Linkding description",
						"notes": "Personal notes",
						"web_archive_snapshot_url": "https://web.archive.org/web/20200102030405/https://example.com/article",
						"favicon_url": "/static/favicon.png",
						"preview_image_url": "/static/preview.jpg",
						"is_archived": false,
						"unread": true,
						"shared": true,
						"tag_names": ["reading", "go", "reading", "  "],
						"date_added": "2020-01-02T03:04:05.123456Z",
						"date_modified": "2022-01-02T03:04:05.654321Z"
					},
					{"id": 2, "title": "Missing URL"}
				]
			}`
		case "/base/api/bookmarks/:2":
			response = `{
				"count": 3,
				"next": null,
				"previous": "https://linkding.example/base/api/bookmarks/?limit=2&offset=0",
				"results": [{
					"id": 3,
					"url": "https://second.example/page",
					"notes": "Second bookmark notes",
					"date_added": "2023-03-04T05:06:07Z"
				}]
			}`
		case "/base/api/bookmarks/archived/:0":
			response = `{
				"count": 1,
				"next": null,
				"previous": null,
				"results": [{
					"id": 4,
					"url": "https://archived.example/page",
					"title": "Archived bookmark",
					"description": "Archived description",
					"is_archived": false,
					"date_added": "2024-04-05T06:07:08Z",
					"date_modified": "2024-05-06T07:08:09Z"
				}]
			}`
		default:
			return nil, fmt.Errorf("unexpected Linkding page %q", page)
		}
		return jsonHTTPResponse(req, http.StatusOK, response), nil
	})}
	source, err := newLinkdingClient("https://linkding.example/base/", "linkding-secret", sourceHTTPClient)
	if err != nil {
		t.Fatal(err)
	}
	source.pageSize = 2

	var fetchedURLs []string
	contentFetcher := serviceContentFetchFunc(func(_ context.Context, rawURL string) (*document.Document, error) {
		fetchedURLs = append(fetchedURLs, rawURL)
		return &document.Document{
			URL:  rawURL,
			HTML: `<html><head><title>Downloaded title</title></head><body><main><p>Downloaded Linkding content.</p></main></body></html>`,
			Text: "Downloaded Linkding content.",
		}, nil
	})

	var receivedDocs []*document.Document
	targetHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/api/batch" {
			return nil, fmt.Errorf("unexpected Nogon request %s %s", req.Method, req.URL.Path)
		}
		var body struct {
			Ops []struct {
				*document.Document
			} `json:"ops"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return nil, err
		}
		results := make([]map[string]any, len(body.Ops))
		for i, op := range body.Ops {
			receivedDocs = append(receivedDocs, op.Document)
			results[i] = map[string]any{"status": http.StatusCreated}
		}
		var response bytes.Buffer
		if err := json.NewEncoder(&response).Encode(map[string]any{"results": results}); err != nil {
			return nil, err
		}
		return jsonHTTPResponse(req, http.StatusOK, response.String()), nil
	})}
	target := client.New("http://nogon.example", client.WithHTTPClient(targetHTTPClient), client.WithMaxBatchBodyBytes(40<<20))

	stats, err := importLinkding(
		context.Background(),
		source,
		target,
		document.NewNullLanguageDetector(),
		contentFetcher,
		serviceImportOptions{
			BatchSize: 10,
			FaviconDownloader: func(d *document.Document) error {
				d.Favicon = "data:image/png;base64,bGlua2Rpbmc="
				return nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if stats != (serviceImportStats{Imported: 3, Skipped: 1}) {
		t.Fatalf("stats = %+v, want 3 imported and 1 skipped", stats)
	}
	if !reflect.DeepEqual(sourcePages, []string{
		"/base/api/bookmarks/:0",
		"/base/api/bookmarks/:2",
		"/base/api/bookmarks/archived/:0",
	}) {
		t.Fatalf("source pages = %v", sourcePages)
	}
	if !reflect.DeepEqual(fetchedURLs, []string{
		"https://example.com/article?utm_source=linkding&keep=1#section",
		"https://second.example/page",
		"https://archived.example/page",
	}) {
		t.Fatalf("fetched URLs = %v", fetchedURLs)
	}
	if len(receivedDocs) != 3 {
		t.Fatalf("received %d documents, want 3", len(receivedDocs))
	}

	article := receivedDocs[0]
	if article.URL != "https://example.com/article?keep=1" {
		t.Errorf("article URL = %q, want normalized URL", article.URL)
	}
	if article.Title != "Saved article" || article.Label != linkdingSourceMetadataValue {
		t.Errorf("article title = %q, label = %q", article.Title, article.Label)
	}
	for _, content := range []string{"Linkding description", "Personal notes", "Downloaded Linkding content."} {
		if !strings.Contains(article.Text, content) {
			t.Errorf("article text does not contain %q: %q", content, article.Text)
		}
	}
	if article.HTML == "" {
		t.Error("downloaded article HTML was not preserved")
	}
	if article.Added != mustUnixTime(t, "2020-01-02T03:04:05Z") {
		t.Errorf("article added = %d", article.Added)
	}
	if article.Updated != mustUnixTime(t, "2022-01-02T03:04:05Z") {
		t.Errorf("article updated = %d", article.Updated)
	}
	if article.Favicon != "data:image/png;base64,bGlua2Rpbmc=" {
		t.Errorf("article favicon = %q", article.Favicon)
	}
	if article.Metadata["source"] != linkdingSourceMetadataValue || article.Metadata["linkding_id"] != float64(1) {
		t.Errorf("article metadata = %#v", article.Metadata)
	}
	if article.Metadata["linkding_unread"] != true || article.Metadata["linkding_shared"] != true {
		t.Errorf("article status metadata = %#v", article.Metadata)
	}
	if !reflect.DeepEqual(article.Metadata["linkding_tags"], []any{"reading", "go"}) {
		t.Errorf("article tags = %#v", article.Metadata["linkding_tags"])
	}
	if article.Metadata["linkding_favicon_url"] != "https://linkding.example/static/favicon.png" {
		t.Errorf("favicon metadata = %#v", article.Metadata["linkding_favicon_url"])
	}
	if article.Metadata["linkding_preview_image_url"] != "https://linkding.example/static/preview.jpg" {
		t.Errorf("preview metadata = %#v", article.Metadata["linkding_preview_image_url"])
	}

	second := receivedDocs[1]
	if second.Title != "Downloaded title" || second.Updated != second.Added {
		t.Errorf("second bookmark = %+v", second)
	}
	archived := receivedDocs[2]
	if archived.Metadata["linkding_archived"] != true {
		t.Errorf("archived metadata = %#v", archived.Metadata)
	}
}

func TestLinkdingClientUsesIncrementalModifiedSince(t *testing.T) {
	const checkpoint = int64(1_704_164_645)
	var paths []string
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if got := req.Header.Get("Authorization"); got != "Token source-token" {
			t.Errorf("Authorization = %q, want Token source-token", got)
		}
		if got := req.URL.Query().Get("modified_since"); got != time.Unix(checkpoint, 0).UTC().Format(time.RFC3339) {
			t.Errorf("modified_since = %q", got)
		}
		return jsonHTTPResponse(req, http.StatusOK, `{"count":0,"next":null,"previous":null,"results":[]}`), nil
	})}
	source, err := newLinkdingClient("https://linkding.example", "source-token", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	source.updatedAfter = checkpoint
	if err := source.walkBookmarks(context.Background(), func(linkdingBookmark) {}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths, []string{"/api/bookmarks/", "/api/bookmarks/archived/"}) {
		t.Fatalf("paths = %v", paths)
	}
}

func TestLinkdingRejectsAuthenticationFailureWithoutExposingToken(t *testing.T) {
	const token = "private-linkding-token"
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(req, http.StatusUnauthorized, `{"detail":"Invalid token."}`), nil
	})}
	source, err := newLinkdingClient("https://linkding.example", token, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	err = source.walkBookmarks(context.Background(), func(linkdingBookmark) {})
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("bookmarks error = %v, want authentication failure", err)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("bookmarks error exposes the Linkding token: %v", err)
	}
	if !strings.Contains(err.Error(), linkdingTokenEnv) || !strings.Contains(err.Error(), "--api-token") {
		t.Fatalf("bookmarks error = %v, want credential guidance", err)
	}
}

func TestLinkdingNextOffsetRejectsNonAdvancingPagination(t *testing.T) {
	next := "https://linkding.example/api/bookmarks/?limit=100&offset=100"
	if _, _, err := linkdingNextOffset(&next, 100); err == nil {
		t.Fatal("nonadvancing pagination was accepted")
	}
}
