package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/server/document"
)

type readeckTestSyncItem struct {
	ID       string
	Bookmark string
	HTML     *string
}

func TestImportReadeckUsesIncrementalSyncAndMapsStoredContent(t *testing.T) {
	checkpoint := mustUnixTime(t, "2024-01-02T03:04:05Z")
	var syncRequests [][]string
	sourceHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Authorization"); got != "Bearer readeck-secret" {
			t.Errorf("Authorization = %q, want Bearer readeck-secret", got)
		}
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/base/api/bookmarks/sync":
			if got := req.URL.Query().Get("since"); got != time.Unix(checkpoint, 0).UTC().Format(time.RFC3339) {
				t.Errorf("since = %q, want checkpoint", got)
			}
			return jsonHTTPResponse(req, http.StatusOK, `[
				{"id":"deleted","time":"2024-02-01T00:00:00Z","type":"delete"},
				{"id":"article-1","time":"2024-02-02T00:00:00Z","type":"update"},
				{"id":"article-1","time":"2024-02-02T00:00:01Z","type":"update"},
				{"id":"article-2","time":"2024-02-03T00:00:00Z","type":"update"},
				{"id":"missing-url","time":"2024-02-04T00:00:00Z","type":"update"}
			]`), nil
		case req.Method == http.MethodPost && req.URL.Path == "/base/api/bookmarks/sync":
			if got := req.Header.Get("Accept"); got != "multipart/mixed" {
				t.Errorf("Accept = %q, want multipart/mixed", got)
			}
			if got := req.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(body, &raw); err != nil {
				return nil, err
			}
			if _, exists := raw["resource_prefix"]; !exists {
				t.Error("sync request is missing resource_prefix")
			}
			var payload readeckSyncRequest
			if err := json.Unmarshal(body, &payload); err != nil {
				return nil, err
			}
			if !payload.WithJSON || !payload.WithHTML || payload.ResourcePrefix != "" {
				t.Errorf("sync payload = %+v", payload)
			}
			syncRequests = append(syncRequests, append([]string(nil), payload.IDs...))

			storedHTML := `<article><h1>Stored heading</h1><p>Stored Readeck article content.</p></article>`
			items := make([]readeckTestSyncItem, 0, len(payload.IDs))
			for _, id := range payload.IDs {
				switch id {
				case "article-1":
					items = append(items, readeckTestSyncItem{
						ID: id,
						Bookmark: `{
							"id":"article-1",
							"href":"/api/bookmarks/article-1",
							"created":"2024-02-02T03:04:05.123456Z",
							"updated":"2024-02-03T04:05:06.654321Z",
							"state":0,
							"loaded":true,
							"url":"https://example.com/article?utm_source=readeck#section",
							"title":"Saved Readeck article",
							"site_name":"Example News",
							"site":"example.com",
							"published":"2024-02-01T00:00:00Z",
							"authors":["Ada Lovelace","Ada Lovelace","  "],
							"lang":"en",
							"text_direction":"ltr",
							"document_type":"article",
							"type":"article",
							"has_article":true,
							"description":"Stored description",
							"is_marked":true,
							"is_archived":false,
							"labels":["reading","go","reading"],
							"read_progress":42,
							"read_anchor":"#paragraph-4",
							"word_count":1200,
							"reading_time":6,
							"resources":{
								"icon":{"src":"/bm/article-1/icon.png","width":32,"height":32},
								"image":{"src":"/bm/article-1/image.jpg","width":800,"height":600},
								"thumbnail":{"src":"/bm/article-1/thumbnail.jpg"},
								"article":{"src":"/api/bookmarks/article-1/article"}
							}
						}`,
						HTML: &storedHTML,
					})
				case "article-2":
					items = append(items, readeckTestSyncItem{
						ID: id,
						Bookmark: `{
							"id":"article-2",
							"created":"2024-03-02T03:04:05Z",
							"updated":"2024-03-03T04:05:06Z",
							"state":1,
							"loaded":true,
							"url":"https://fallback.example/article",
							"title":"Fallback article",
							"type":"article",
							"has_article":false,
							"errors":["source fetch failed"]
						}`,
					})
				case "missing-url":
					items = append(items, readeckTestSyncItem{
						ID:       id,
						Bookmark: `{"id":"missing-url","title":"Missing URL","loaded":true}`,
					})
				default:
					return nil, fmt.Errorf("unexpected Readeck sync ID %q", id)
				}
			}
			return readeckMultipartHTTPResponse(req, items)
		default:
			return nil, fmt.Errorf("unexpected Readeck request %s %s", req.Method, req.URL.Path)
		}
	})}

	source, err := newReadeckClient("https://readeck.example/base/", "readeck-secret", sourceHTTPClient)
	if err != nil {
		t.Fatal(err)
	}
	source.updatedAfter = checkpoint

	var fetchedURLs []string
	contentFetcher := serviceContentFetchFunc(func(_ context.Context, rawURL string) (*document.Document, error) {
		fetchedURLs = append(fetchedURLs, rawURL)
		return &document.Document{
			URL:  rawURL,
			HTML: `<html><head><title>Downloaded title</title></head><body><main><p>Downloaded fallback content.</p></main></body></html>`,
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

	stats, err := importReadeck(
		context.Background(),
		source,
		target,
		document.NewNullLanguageDetector(),
		contentFetcher,
		serviceImportOptions{
			BatchSize: 2,
			FaviconDownloader: func(d *document.Document) error {
				d.Favicon = "data:image/png;base64,cmVhZGVjaw=="
				return nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if stats != (serviceImportStats{Imported: 2, Skipped: 1}) {
		t.Fatalf("stats = %+v, want 2 imported and 1 skipped", stats)
	}
	if !reflect.DeepEqual(syncRequests, [][]string{{"article-1", "article-2"}, {"missing-url"}}) {
		t.Fatalf("sync requests = %v", syncRequests)
	}
	if !reflect.DeepEqual(fetchedURLs, []string{"https://fallback.example/article"}) {
		t.Fatalf("fetched URLs = %v, want only the bookmark without stored HTML", fetchedURLs)
	}
	if len(receivedDocs) != 2 {
		t.Fatalf("received %d documents, want 2", len(receivedDocs))
	}

	stored := receivedDocs[0]
	if stored.URL != "https://example.com/article" {
		t.Errorf("stored URL = %q, want normalized URL", stored.URL)
	}
	if stored.Title != "Saved Readeck article" || stored.Label != readeckSourceMetadataValue {
		t.Errorf("stored title = %q, label = %q", stored.Title, stored.Label)
	}
	for _, content := range []string{"Stored description", "Ada Lovelace", "Stored Readeck article content."} {
		if !strings.Contains(stored.Text, content) {
			t.Errorf("stored text does not contain %q: %q", content, stored.Text)
		}
	}
	if !strings.Contains(stored.HTML, "<body>") || !strings.Contains(stored.HTML, "Stored heading") {
		t.Errorf("stored HTML was not wrapped and preserved: %q", stored.HTML)
	}
	if stored.Added != mustUnixTime(t, "2024-02-02T03:04:05Z") {
		t.Errorf("stored added = %d", stored.Added)
	}
	if stored.Updated != mustUnixTime(t, "2024-02-03T04:05:06Z") {
		t.Errorf("stored updated = %d", stored.Updated)
	}
	if stored.Favicon != "data:image/png;base64,cmVhZGVjaw==" {
		t.Errorf("stored favicon = %q", stored.Favicon)
	}
	if stored.Metadata["source"] != readeckSourceMetadataValue || stored.Metadata["readeck_id"] != "article-1" {
		t.Errorf("stored metadata = %#v", stored.Metadata)
	}
	if stored.Metadata["readeck_marked"] != true || stored.Metadata["readeck_archived"] != false {
		t.Errorf("stored status metadata = %#v", stored.Metadata)
	}
	if stored.Metadata["readeck_read_progress"] != float64(42) {
		t.Errorf("stored read progress = %#v", stored.Metadata["readeck_read_progress"])
	}
	if !reflect.DeepEqual(stored.Metadata["readeck_labels"], []any{"reading", "go"}) {
		t.Errorf("stored labels = %#v", stored.Metadata["readeck_labels"])
	}
	if !reflect.DeepEqual(stored.Metadata["readeck_authors"], []any{"Ada Lovelace"}) {
		t.Errorf("stored authors = %#v", stored.Metadata["readeck_authors"])
	}
	if stored.Metadata["readeck_href"] != "https://readeck.example/api/bookmarks/article-1" {
		t.Errorf("stored href = %#v", stored.Metadata["readeck_href"])
	}
	if stored.Metadata["readeck_image_url"] != "https://readeck.example/bm/article-1/image.jpg" {
		t.Errorf("stored image URL = %#v", stored.Metadata["readeck_image_url"])
	}

	fallback := receivedDocs[1]
	if !strings.Contains(fallback.Text, "Downloaded fallback content.") || fallback.HTML == "" {
		t.Errorf("fallback content was not downloaded: text %q", fallback.Text)
	}
	if !reflect.DeepEqual(fallback.Metadata["readeck_errors"], []any{"source fetch failed"}) {
		t.Errorf("fallback errors = %#v", fallback.Metadata["readeck_errors"])
	}
}

func TestReadeckRejectsAuthenticationFailureWithoutExposingToken(t *testing.T) {
	const token = "private-readeck-token"
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(req, http.StatusUnauthorized, `{"message":"invalid token"}`), nil
	})}
	source, err := newReadeckClient("https://readeck.example", token, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.changedBookmarkIDs(context.Background())
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("sync error = %v, want authentication failure", err)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("sync error exposes the Readeck token: %v", err)
	}
	if !strings.Contains(err.Error(), readeckTokenEnv) || !strings.Contains(err.Error(), "--api-token") {
		t.Fatalf("sync error = %v, want credential guidance", err)
	}
}

func TestReadeckSyncRejectsUnexpectedBookmarkID(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return readeckMultipartHTTPResponse(req, []readeckTestSyncItem{{
			ID:       "unexpected",
			Bookmark: `{"id":"unexpected","url":"https://example.com"}`,
		}})
	})}
	source, err := newReadeckClient("https://readeck.example", "token", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := source.syncBookmarks(context.Background(), []string{"requested"}); err == nil {
		t.Fatal("sync response with an unexpected bookmark ID was accepted")
	}
}

func readeckMultipartHTTPResponse(
	req *http.Request,
	items []readeckTestSyncItem,
) (*http.Response, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, item := range items {
		if err := writeReadeckTestPart(writer, item.ID, "json", "application/json", item.Bookmark); err != nil {
			return nil, err
		}
		if item.HTML != nil {
			if err := writeReadeckTestPart(writer, item.ID, "html", "text/html", *item.HTML); err != nil {
				return nil, err
			}
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": {"multipart/mixed; boundary=" + writer.Boundary()},
		},
		Body:    io.NopCloser(bytes.NewReader(body.Bytes())),
		Request: req,
	}, nil
}

func writeReadeckTestPart(
	writer *multipart.Writer,
	id string,
	partType string,
	contentType string,
	content string,
) error {
	header := make(textproto.MIMEHeader)
	header.Set("Bookmark-Id", id)
	header.Set("Content-Type", contentType)
	header.Set("Type", partType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = io.WriteString(part, content)
	return err
}
