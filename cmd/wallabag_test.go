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

func TestImportWallabagAuthenticatesPaginatesAndMapsContent(t *testing.T) {
	updatedAfter := mustUnixTime(t, "2024-01-02T03:04:05Z")
	var sourcePages []string
	sourceHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/base/api/entries.json":
			if req.Method != http.MethodGet {
				t.Errorf("entries request method = %s, want GET", req.Method)
			}
			if got := req.Header.Get("Authorization"); got != "Bearer wallabag-access" {
				t.Errorf("entries Authorization = %q", got)
			}
			query := req.URL.Query()
			for key, want := range map[string]string{
				"detail":  "full",
				"order":   "asc",
				"perPage": "100",
				"since":   fmt.Sprintf("%d", updatedAfter),
				"sort":    "updated",
			} {
				if got := query.Get(key); got != want {
					t.Errorf("entries query %s = %q, want %q", key, got, want)
				}
			}
			page := query.Get("page")
			sourcePages = append(sourcePages, page)
			switch page {
			case "1":
				return jsonHTTPResponse(req, http.StatusOK, `{
					"_embedded":{"items":[{
						"id":11,
						"url":"https://example.com/article?utm_source=wallabag#section",
						"given_url":"https://given.example/article",
						"origin_url":"https://origin.example/list",
						"title":"Saved article",
						"content":"<html><head><title>Stored title</title></head><body><main><p>Stored wallabag content.</p></main></body></html>",
						"created_at":"2024-02-03T04:05:06+0000",
						"updated_at":"2024-02-04T05:06:07Z",
						"published_at":"2024-02-01T00:00:00Z",
						"domain_name":"example.com",
						"mimetype":"text/html",
						"language":"en",
						"preview_picture":"https://example.com/preview.jpg",
						"http_status":"200",
						"reading_time":4,
						"is_archived":1,
						"is_starred":false,
						"is_public":true,
						"tags":["reading",{"label":"go"}],
						"published_by":["Ada Lovelace"]
					}]},
					"page":1,"pages":2,"limit":100,"total":3
				}`), nil
			case "2":
				return jsonHTTPResponse(req, http.StatusOK, `{
					"_embedded":{"items":[
						{
							"id":12,
							"url":"https://fallback.example/article",
							"title":"Fallback article",
							"content":"",
							"created_at":"2024-03-04T05:06:07Z",
							"updated_at":"2024-03-05T06:07:08Z",
							"is_archived":"0",
							"is_starred":"1",
							"is_public":false,
							"tags":[]
						},
						{"id":13,"url":"","title":"Missing URL"}
					]},
					"page":2,"pages":2,"limit":100,"total":3
				}`), nil
			default:
				return nil, fmt.Errorf("unexpected wallabag page %q", page)
			}
		default:
			return nil, fmt.Errorf("unexpected wallabag request %s %s", req.Method, req.URL.Path)
		}
	})}

	source, err := newWallabagClient(
		"https://wallabag.example/base/",
		"wallabag-access",
		sourceHTTPClient,
	)
	if err != nil {
		t.Fatal(err)
	}
	source.updatedAfter = updatedAfter

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
			return nil, fmt.Errorf("unexpected Hister request %s %s", req.Method, req.URL.Path)
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
	target := client.New("http://hister.example", client.WithHTTPClient(targetHTTPClient), client.WithMaxBatchBodyBytes(40<<20))

	stats, err := importWallabag(
		context.Background(),
		source,
		target,
		document.NewNullLanguageDetector(),
		contentFetcher,
		serviceImportOptions{
			BatchSize: 2,
			FaviconDownloader: func(d *document.Document) error {
				d.Favicon = "data:image/png;base64,d2FsbGFiYWc="
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
	if !reflect.DeepEqual(sourcePages, []string{"1", "2"}) {
		t.Fatalf("source pages = %v, want [1 2]", sourcePages)
	}
	if !reflect.DeepEqual(fetchedURLs, []string{"https://fallback.example/article"}) {
		t.Fatalf("fetched URLs = %v, want only the entry without stored content", fetchedURLs)
	}
	if len(receivedDocs) != 2 {
		t.Fatalf("received %d documents, want 2", len(receivedDocs))
	}

	stored := receivedDocs[0]
	if stored.Label != "wallabag" {
		t.Errorf("stored label = %q, want wallabag", stored.Label)
	}
	if stored.URL != "https://example.com/article" {
		t.Errorf("stored URL = %q, want normalized URL", stored.URL)
	}
	if stored.Title != "Saved article" {
		t.Errorf("stored title = %q, want source title", stored.Title)
	}
	if !strings.Contains(stored.Text, "Stored wallabag content.") || stored.HTML == "" {
		t.Errorf("stored content was not extracted and preserved: text %q", stored.Text)
	}
	if stored.Added != mustUnixTime(t, "2024-02-03T04:05:06Z") {
		t.Errorf("stored added = %d", stored.Added)
	}
	if stored.Updated != mustUnixTime(t, "2024-02-04T05:06:07Z") {
		t.Errorf("stored updated = %d", stored.Updated)
	}
	if stored.Metadata["source"] != "wallabag" || stored.Metadata["wallabag_id"] != float64(11) {
		t.Errorf("stored metadata = %#v", stored.Metadata)
	}
	if stored.Metadata["wallabag_archived"] != true || stored.Metadata["wallabag_starred"] != false {
		t.Errorf("stored status metadata = %#v", stored.Metadata)
	}
	if !reflect.DeepEqual(stored.Metadata["wallabag_tags"], []any{"reading", "go"}) {
		t.Errorf("stored tags = %#v", stored.Metadata["wallabag_tags"])
	}
	if !reflect.DeepEqual(stored.Metadata["wallabag_authors"], []any{"Ada Lovelace"}) {
		t.Errorf("stored authors = %#v", stored.Metadata["wallabag_authors"])
	}

	fallback := receivedDocs[1]
	if !strings.Contains(fallback.Text, "Downloaded fallback content.") || fallback.HTML == "" {
		t.Errorf("fallback content was not downloaded: text %q", fallback.Text)
	}
	if fallback.Metadata["wallabag_starred"] != true {
		t.Errorf("fallback starred metadata = %#v", fallback.Metadata["wallabag_starred"])
	}
}

func TestWallabagClientUsesAPIToken(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/entries.json" {
			return nil, fmt.Errorf("unexpected request path %q", req.URL.Path)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer direct-token" {
			t.Errorf("Authorization = %q, want Bearer direct-token", got)
		}
		return jsonHTTPResponse(req, http.StatusOK, `{"_embedded":{"items":[]},"page":1,"pages":1,"limit":100,"total":0}`), nil
	})}
	source, err := newWallabagClient("https://wallabag.example", "direct-token", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.entries(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
}

func TestWallabagAPITokenFlagOverridesEnvironment(t *testing.T) {
	t.Setenv(wallabagTokenEnv, "environment-token")
	cmd := &cobra.Command{}
	cmd.Flags().String("api-token", "", "")
	if got := serviceAPIToken(cmd, wallabagTokenEnv); got != "environment-token" {
		t.Fatalf("API token = %q, want environment token", got)
	}
	if err := cmd.Flags().Set("api-token", "flag-token"); err != nil {
		t.Fatal(err)
	}
	if got := serviceAPIToken(cmd, wallabagTokenEnv); got != "flag-token" {
		t.Fatalf("API token = %q, want flag token", got)
	}
}

func TestWallabagCommandOnlyExposesTokenAuthentication(t *testing.T) {
	if importWallabagCmd.Flags().Lookup("api-token") == nil {
		t.Fatal("wallabag import is missing --api-token")
	}
	for _, name := range []string{"client-id", "client-secret", "username", "password"} {
		if importWallabagCmd.Flags().Lookup(name) != nil {
			t.Errorf("wallabag import unexpectedly exposes --%s", name)
		}
	}
}
