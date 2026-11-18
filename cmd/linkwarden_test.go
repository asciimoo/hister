package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/indexer"

	"github.com/spf13/cobra"
)

type serviceContentFetchFunc func(context.Context, string) (*document.Document, error)

func (f serviceContentFetchFunc) Fetch(ctx context.Context, rawURL string) (*document.Document, error) {
	return f(ctx, rawURL)
}

func TestImportLinkwardenPaginatesAndMapsDocuments(t *testing.T) {
	var sourceCursors []string
	sourceHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Path != "/base/api/v1/search" {
			t.Errorf("Linkwarden request = %s %s, want GET /base/api/v1/search", req.Method, req.URL.Path)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer source-secret" {
			t.Errorf("Authorization = %q, want Bearer source-secret", got)
		}
		if got := req.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		if got := req.Header.Get("User-Agent"); got == "" {
			t.Error("User-Agent is empty")
		}

		cursor := req.URL.Query().Get("cursor")
		if filter := req.URL.Query().Get("searchQueryString"); filter != "" {
			t.Errorf("unexpected incremental filter %q", filter)
		}
		sourceCursors = append(sourceCursors, cursor)
		var response string
		switch cursor {
		case "":
			response = `{
				"data": {
					"nextCursor": 42,
					"links": [
						{
							"id": 10,
							"name": "Article",
							"type": "url",
							"description": "A useful description",
							"url": "https://example.com/article?utm_source=newsletter&keep=1#section",
							"textContent": "Full searchable content",
							"importDate": "2020-01-02T03:04:05Z",
							"createdAt": "2021-01-02T03:04:05Z",
							"updatedAt": "2022-01-02T03:04:05Z",
							"tags": [{"id": 1, "name": "reading"}, {"id": 2, "name": "go"}],
							"collection": {"id": 3, "name": "References"}
						},
						{"id": 11, "name": "Uploaded image", "type": "image", "url": null}
					]
				}
			}`
		case "42":
			response = `{
				"data": {
					"nextCursor": null,
					"links": [{
						"id": 12,
						"name": "Manual",
						"type": "pdf",
						"url": "https://example.com/manual.pdf",
						"createdAt": "2023-04-05T06:07:08Z",
						"updatedAt": "2023-05-06T07:08:09Z"
					}]
				}
			}`
		default:
			return nil, fmt.Errorf("unexpected cursor %q", cursor)
		}
		return jsonHTTPResponse(req, http.StatusOK, response), nil
	})}
	source, err := newLinkwardenClient("https://links.example/base/", "source-secret", sourceHTTPClient)
	if err != nil {
		t.Fatal(err)
	}

	var receivedDocs []*document.Document
	var batchSizes []int
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
			if op.Op != "add" {
				t.Errorf("batch operation = %q, want add", op.Op)
			}
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

	stats, err := importLinkwarden(context.Background(), source, target, document.NewNullLanguageDetector(), nil, serviceImportOptions{BatchSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if stats != (serviceImportStats{Imported: 2, Skipped: 1}) {
		t.Fatalf("stats = %+v, want 2 imported and 1 skipped", stats)
	}
	if !reflect.DeepEqual(sourceCursors, []string{"", "42"}) {
		t.Fatalf("source cursors = %v, want [empty 42]", sourceCursors)
	}
	if !reflect.DeepEqual(batchSizes, []int{2}) {
		t.Fatalf("batch sizes = %v, want [2]", batchSizes)
	}
	if len(receivedDocs) != 2 {
		t.Fatalf("received %d documents, want 2", len(receivedDocs))
	}

	article := receivedDocs[0]
	if article.Label != "linkwarden" {
		t.Errorf("article label = %q, want linkwarden", article.Label)
	}
	if article.URL != "https://example.com/article?keep=1" {
		t.Errorf("article URL = %q, want normalized URL", article.URL)
	}
	if article.Domain != "example.com" || article.Title != "Article" {
		t.Errorf("article identity = domain %q, title %q", article.Domain, article.Title)
	}
	if article.Text != "A useful description\n\nFull searchable content" {
		t.Errorf("article text = %q", article.Text)
	}
	if article.Added != mustUnixTime(t, "2020-01-02T03:04:05Z") {
		t.Errorf("article added = %d", article.Added)
	}
	if article.Updated != mustUnixTime(t, "2022-01-02T03:04:05Z") {
		t.Errorf("article updated = %d", article.Updated)
	}
	if !article.Processed || article.Language != document.UnknownLanguage {
		t.Errorf("article processing state = processed %v, language %q", article.Processed, article.Language)
	}
	if article.Metadata["source"] != "linkwarden" || article.Metadata["description"] != "A useful description" {
		t.Errorf("article metadata = %#v", article.Metadata)
	}
	if article.Metadata["linkwarden_collection"] != "References" {
		t.Errorf("collection metadata = %#v", article.Metadata["linkwarden_collection"])
	}
	wantTags := []any{"reading", "go"}
	if !reflect.DeepEqual(article.Metadata["linkwarden_tags"], wantTags) {
		t.Errorf("tag metadata = %#v, want %#v", article.Metadata["linkwarden_tags"], wantTags)
	}
	if receivedDocs[1].Metadata["linkwarden_type"] != "pdf" {
		t.Errorf("PDF source type = %#v", receivedDocs[1].Metadata["linkwarden_type"])
	}
}

func TestImportLinkwardenDownloadsMissingURLContent(t *testing.T) {
	sourceHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(req, http.StatusOK, `{
			"data": {
				"nextCursor": null,
				"links": [{
					"id": 15,
					"name": "Saved title",
					"type": "url",
					"description": "Saved description",
					"url": "https://example.com/article?utm_source=linkwarden#content",
					"createdAt": "2024-01-02T03:04:05Z",
					"updatedAt": "2024-02-03T04:05:06Z"
				}]
			}
		}`), nil
	})}
	source, err := newLinkwardenClient("https://links.example", "token", sourceHTTPClient)
	if err != nil {
		t.Fatal(err)
	}

	fetchCalls := 0
	const downloadedHTML = `<html><head><title>Downloaded title</title></head><body><main><p>Downloaded body text.</p></main></body></html>`
	contentFetcher := serviceContentFetchFunc(func(_ context.Context, rawURL string) (*document.Document, error) {
		fetchCalls++
		if rawURL != "https://example.com/article?utm_source=linkwarden#content" {
			t.Errorf("download URL = %q, want original bookmark URL", rawURL)
		}
		return &document.Document{URL: rawURL, HTML: downloadedHTML}, nil
	})

	var received *document.Document
	targetHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Ops []struct {
				*document.Document
			} `json:"ops"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return nil, err
		}
		if len(body.Ops) != 1 {
			return nil, fmt.Errorf("received %d batch operations, want one", len(body.Ops))
		}
		received = body.Ops[0].Document
		return jsonHTTPResponse(req, http.StatusOK, `{"results":[{"status":201}]}`), nil
	})}
	target := client.New("http://nogon.example", client.WithHTTPClient(targetHTTPClient), client.WithMaxBatchBodyBytes(40<<20))

	stats, err := importLinkwarden(
		context.Background(),
		source,
		target,
		document.NewNullLanguageDetector(),
		contentFetcher,
		serviceImportOptions{BatchSize: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	if stats != (serviceImportStats{Imported: 1}) {
		t.Fatalf("stats = %+v, want one imported document", stats)
	}
	if fetchCalls != 1 {
		t.Fatalf("content fetch calls = %d, want one", fetchCalls)
	}
	if received == nil {
		t.Fatal("no document was submitted")
	}
	if received.HTML != downloadedHTML {
		t.Errorf("downloaded HTML was not preserved")
	}
	if !strings.Contains(received.Text, "Saved description") || !strings.Contains(received.Text, "Downloaded body text.") {
		t.Errorf("document text = %q, want description and downloaded content", received.Text)
	}
	if received.Title != "Saved title" {
		t.Errorf("document title = %q, want Linkwarden title", received.Title)
	}
	if received.Updated != mustUnixTime(t, "2024-02-03T04:05:06Z") {
		t.Errorf("document updated = %d, want Linkwarden timestamp", received.Updated)
	}
}

func TestLinkwardenSearchRejectsAuthenticationFailure(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(req, http.StatusUnauthorized, `{"error":"unauthorized"}`), nil
	})}
	source, err := newLinkwardenClient("https://links.example", "bad-token", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.search(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("search error = %v, want authentication failure", err)
	}
	if strings.Contains(err.Error(), "bad-token") {
		t.Fatalf("search error exposes the source token: %v", err)
	}
}

func TestLatestLinkwardenUpdatedConfiguresSourceFilter(t *testing.T) {
	const latestUpdated = int64(1641092645)
	targetHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Path != "/search" {
			return nil, fmt.Errorf("unexpected Nogon request %s %s", req.Method, req.URL.Path)
		}
		var query indexer.Query
		if err := json.Unmarshal([]byte(req.URL.Query().Get("query")), &query); err != nil {
			return nil, fmt.Errorf("decode Nogon query: %w", err)
		}
		if query.Text != "metadata.source:linkwarden" || query.Sort != "date" || query.Limit != 1 {
			t.Errorf("Nogon query = %+v", query)
		}
		return jsonHTTPResponse(req, http.StatusOK, fmt.Sprintf(`{"documents":[{"updated":%d}]}`, latestUpdated)), nil
	})}
	updatedAfter, err := latestServiceUpdated(client.New("http://nogon.example", client.WithHTTPClient(targetHTTPClient)), linkwardenSourceMetadataValue)
	if err != nil {
		t.Fatal(err)
	}
	if updatedAfter != latestUpdated {
		t.Fatalf("latest update = %d, want %d", updatedAfter, latestUpdated)
	}

	sourceHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.Query().Get("searchQueryString"); got != "after:2022-01-02" {
			t.Errorf("Linkwarden search query = %q, want after:2022-01-02", got)
		}
		return jsonHTTPResponse(req, http.StatusOK, `{"data":{"nextCursor":null,"links":[]}}`), nil
	})}
	source, err := newLinkwardenClient("https://links.example", "token", sourceHTTPClient)
	if err != nil {
		t.Fatal(err)
	}
	source.updatedAfter = updatedAfter
	if _, err := source.search(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestImportLinkwardenSkipsExistingNormalizedURL(t *testing.T) {
	sourceHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(req, http.StatusOK, `{
			"data": {
				"nextCursor": null,
				"links": [{"id": 1, "url": "https://example.com/page?utm_source=test#section"}]
			}
		}`), nil
	})}
	source, err := newLinkwardenClient("https://links.example", "token", sourceHTTPClient)
	if err != nil {
		t.Fatal(err)
	}
	targetHTTPClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodHead || req.URL.Path != "/api/document" {
			return nil, fmt.Errorf("unexpected Nogon request %s %s", req.Method, req.URL.Path)
		}
		if got := req.URL.Query().Get("url"); got != "https://example.com/page" {
			t.Errorf("existence check URL = %q, want normalized URL", got)
		}
		return jsonHTTPResponse(req, http.StatusOK, ""), nil
	})}
	target := client.New("http://nogon.example", client.WithHTTPClient(targetHTTPClient), client.WithMaxBatchBodyBytes(40<<20))

	stats, err := importLinkwarden(context.Background(), source, target, document.NewNullLanguageDetector(), nil, serviceImportOptions{
		BatchSize:    10,
		SkipExisting: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats != (serviceImportStats{Skipped: 1}) {
		t.Fatalf("stats = %+v, want one skipped document", stats)
	}
}

func TestImportLinkwardenRejectsRepeatedCursor(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(req, http.StatusOK, `{"data":{"nextCursor":7,"links":[]}}`), nil
	})}
	source, err := newLinkwardenClient("https://links.example", "token", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	target := client.New("http://nogon.example")
	_, err = importLinkwarden(context.Background(), source, target, document.NewNullLanguageDetector(), nil, serviceImportOptions{BatchSize: 10})
	if err == nil || !strings.Contains(err.Error(), "repeated pagination cursor 7") {
		t.Fatalf("import error = %v, want repeated cursor error", err)
	}
}

func TestLinkwardenAPITokenFlagOverridesEnvironment(t *testing.T) {
	t.Setenv(linkwardenTokenEnv, "environment-token")
	cmd := &cobra.Command{}
	cmd.Flags().String("api-token", "", "")
	if got := serviceAPIToken(cmd, linkwardenTokenEnv); got != "environment-token" {
		t.Fatalf("API token = %q, want environment token", got)
	}
	if err := cmd.Flags().Set("api-token", "flag-token"); err != nil {
		t.Fatal(err)
	}
	if got := serviceAPIToken(cmd, linkwardenTokenEnv); got != "flag-token" {
		t.Fatalf("API token = %q, want flag token", got)
	}
}

func TestNewLinkwardenClientValidatesInstanceURL(t *testing.T) {
	for _, instanceURL := range []string{"links.example", "ftp://links.example", "https://user:secret@links.example", "https://links.example?token=secret"} {
		if _, err := newLinkwardenClient(instanceURL, "token", nil); err == nil {
			t.Errorf("newLinkwardenClient(%q) unexpectedly succeeded", instanceURL)
		}
	}
}

func TestLinkwardenClientRefusesCrossOriginRedirect(t *testing.T) {
	source, err := newLinkwardenClient("https://links.example", "token", nil)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := http.NewRequest(http.MethodGet, "https://links.example/api/v1/search", nil)
	if err != nil {
		t.Fatal(err)
	}
	redirect, err := http.NewRequest(http.MethodGet, "https://attacker.example/collect", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.httpClient.CheckRedirect(redirect, []*http.Request{origin}); err == nil {
		t.Fatal("cross origin redirect was accepted")
	}
}

func jsonHTTPResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func mustUnixTime(t *testing.T, value string) int64 {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Unix()
}
