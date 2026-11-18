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

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/server/document"

	"github.com/spf13/cobra"
)

func TestImportKarakeepPaginatesAndMapsContent(t *testing.T) {
	var sourceCursors []string
	sourceHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Path != "/base/api/v1/bookmarks" {
			t.Errorf("Karakeep request = %s %s, want GET /base/api/v1/bookmarks", req.Method, req.URL.Path)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer karakeep-secret" {
			t.Errorf("Authorization = %q, want Bearer karakeep-secret", got)
		}
		if got := req.URL.Query().Get("includeContent"); got != "true" {
			t.Errorf("includeContent = %q, want true", got)
		}
		if got := req.URL.Query().Get("limit"); got != "100" {
			t.Errorf("limit = %q, want 100", got)
		}
		if got := req.URL.Query().Get("sortOrder"); got != "asc" {
			t.Errorf("sortOrder = %q, want asc", got)
		}

		cursor := req.URL.Query().Get("cursor")
		sourceCursors = append(sourceCursors, cursor)
		var response string
		switch cursor {
		case "":
			response = `{
				"bookmarks": [
					{
						"id": "bookmark-1",
						"createdAt": "2024-01-02T03:04:05Z",
						"modifiedAt": "2024-02-03T04:05:06Z",
						"title": "Saved article",
						"archived": true,
						"favourited": true,
						"note": "Personal note",
						"summary": "Short summary",
						"source": "extension",
						"tags": [{"id": "tag-1", "name": "reading", "attachedBy": "human"}],
						"content": {
							"type": "link",
							"url": "https://example.com/article?utm_source=karakeep#section",
							"title": "Crawled title",
							"description": "Article description",
							"favicon": "data:image/png;base64,a2FyYWtlZXA=",
							"htmlContent": "<html><head><title>Stored title</title></head><body><main><p>Stored Karakeep content.</p></main></body></html>",
							"crawlStatus": "success",
							"fullPageArchiveAssetId": "archive-1"
						},
						"assets": [{"id": "archive-1", "assetType": "fullPageArchive", "fileName": "page.html"}]
					},
					{
						"id": "bookmark-2",
						"createdAt": "2024-03-04T05:06:07Z",
						"modifiedAt": null,
						"title": "Text note",
						"note": "Note metadata",
						"content": {
							"type": "text",
							"text": "Complete text bookmark",
							"sourceUrl": "https://notes.example/note/2"
						}
					},
					{
						"id": "bookmark-3",
						"createdAt": "2024-04-05T06:07:08Z",
						"content": {"type": "text", "text": "No source URL"}
					}
				],
				"nextCursor": "cursor-2"
			}`
		case "cursor-2":
			response = `{
				"bookmarks": [{
					"id": "bookmark-4",
					"createdAt": "2024-05-06T07:08:09Z",
					"modifiedAt": "2024-06-07T08:09:10Z",
					"title": "Fallback article",
					"content": {
						"type": "link",
						"url": "https://fallback.example/article",
						"description": "Fallback description",
						"htmlContent": null,
						"crawlStatus": "failure"
					}
				}],
				"nextCursor": null
			}`
		default:
			return nil, fmt.Errorf("unexpected cursor %q", cursor)
		}
		return jsonHTTPResponse(req, http.StatusOK, response), nil
	})}
	source, err := newKarakeepClient("https://karakeep.example/base/", "karakeep-secret", sourceHTTPClient)
	if err != nil {
		t.Fatal(err)
	}

	var fetchedURLs []string
	contentFetcher := serviceContentFetchFunc(func(_ context.Context, rawURL string) (*document.Document, error) {
		fetchedURLs = append(fetchedURLs, rawURL)
		return &document.Document{
			URL:  rawURL,
			HTML: `<html><head><title>Downloaded title</title></head><body><main><p>Downloaded fallback content.</p></main></body></html>`,
		}, nil
	})

	var receivedDocs []*document.Document
	var batchSizes []int
	var faviconDownloads []string
	targetHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/api/batch" {
			return nil, fmt.Errorf("unexpected Nogon request %s %s", req.Method, req.URL.Path)
		}
		var body struct {
			Ops []struct {
				Op string `json:"op"`
				*document.Document
			} `json:"ops"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return nil, err
		}
		batchSizes = append(batchSizes, len(body.Ops))
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

	stats, err := importKarakeep(
		context.Background(),
		source,
		target,
		document.NewNullLanguageDetector(),
		contentFetcher,
		serviceImportOptions{
			BatchSize: 10,
			FaviconDownloader: func(d *document.Document) error {
				faviconDownloads = append(faviconDownloads, d.URL)
				d.Favicon = "data:image/png;base64,ZmV0Y2hlZA=="
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
	if !reflect.DeepEqual(sourceCursors, []string{"", "cursor-2"}) {
		t.Fatalf("source cursors = %v, want [empty cursor-2]", sourceCursors)
	}
	if !reflect.DeepEqual(batchSizes, []int{3}) {
		t.Fatalf("batch sizes = %v, want [3]", batchSizes)
	}
	if !reflect.DeepEqual(fetchedURLs, []string{"https://fallback.example/article"}) {
		t.Fatalf("fetched URLs = %v, want only the bookmark missing stored content", fetchedURLs)
	}
	if !reflect.DeepEqual(faviconDownloads, []string{"https://notes.example/note/2", "https://fallback.example/article"}) {
		t.Fatalf("favicon downloads = %v, want only records without a stored favicon", faviconDownloads)
	}

	stored := receivedDocs[0]
	if stored.Label != "karakeep" {
		t.Errorf("stored label = %q, want karakeep", stored.Label)
	}
	if stored.URL != "https://example.com/article" {
		t.Errorf("stored URL = %q, want normalized URL", stored.URL)
	}
	if stored.Title != "Saved article" {
		t.Errorf("stored title = %q, want source title", stored.Title)
	}
	for _, text := range []string{"Personal note", "Short summary", "Article description", "Stored Karakeep content."} {
		if !strings.Contains(stored.Text, text) {
			t.Errorf("stored text %q does not contain %q", stored.Text, text)
		}
	}
	if stored.HTML == "" {
		t.Error("stored Karakeep HTML was not preserved")
	}
	if stored.Favicon != "data:image/png;base64,a2FyYWtlZXA=" {
		t.Errorf("stored favicon = %q, want Karakeep favicon", stored.Favicon)
	}
	if stored.Updated != mustUnixTime(t, "2024-02-03T04:05:06Z") {
		t.Errorf("stored updated = %d", stored.Updated)
	}
	if stored.Metadata["source"] != "karakeep" || stored.Metadata["karakeep_id"] != "bookmark-1" {
		t.Errorf("stored metadata = %#v", stored.Metadata)
	}
	if !reflect.DeepEqual(stored.Metadata["karakeep_tags"], []any{"reading"}) {
		t.Errorf("stored tags = %#v", stored.Metadata["karakeep_tags"])
	}

	textNote := receivedDocs[1]
	if textNote.Text != "Note metadata\n\nComplete text bookmark" {
		t.Errorf("text bookmark content = %q", textNote.Text)
	}
	if textNote.Updated != textNote.Added {
		t.Errorf("text bookmark timestamps = added %d updated %d", textNote.Added, textNote.Updated)
	}

	fallback := receivedDocs[2]
	if !strings.Contains(fallback.Text, "Fallback description") || !strings.Contains(fallback.Text, "Downloaded fallback content.") {
		t.Errorf("fallback text = %q", fallback.Text)
	}
	if fallback.HTML == "" {
		t.Error("downloaded fallback HTML was not preserved")
	}
}

func TestKarakeepClientUsesIncrementalSearch(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v1/bookmarks/search" {
			t.Errorf("request path = %q, want incremental search endpoint", req.URL.Path)
		}
		if got := req.URL.Query().Get("q"); got != "after:2024-01-02" {
			t.Errorf("incremental query = %q, want after:2024-01-02", got)
		}
		if got := req.URL.Query().Get("includeContent"); got != "true" {
			t.Errorf("includeContent = %q, want true", got)
		}
		return jsonHTTPResponse(req, http.StatusOK, `{"bookmarks":[],"nextCursor":null}`), nil
	})}
	source, err := newKarakeepClient("https://karakeep.example", "token", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	source.updatedAfter = mustUnixTime(t, "2024-01-02T03:04:05Z")
	if _, err := source.bookmarks(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestKarakeepAPITokenFlagOverridesEnvironment(t *testing.T) {
	t.Setenv(karakeepTokenEnv, "environment-token")
	cmd := &cobra.Command{}
	cmd.Flags().String("api-token", "", "")
	if got := serviceAPIToken(cmd, karakeepTokenEnv); got != "environment-token" {
		t.Fatalf("API token = %q, want environment token", got)
	}
	if err := cmd.Flags().Set("api-token", "flag-token"); err != nil {
		t.Fatal(err)
	}
	if got := serviceAPIToken(cmd, karakeepTokenEnv); got != "flag-token" {
		t.Fatalf("API token = %q, want flag token", got)
	}
}
