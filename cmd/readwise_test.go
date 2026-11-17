package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/server/document"
)

func TestReadwiseImportCommand(t *testing.T) {
	command, _, err := importCmd.Find([]string{"readwise"})
	if err != nil || command.Name() != "readwise" {
		t.Fatalf("Readwise importer is unavailable: command = %q, error = %v", command.Name(), err)
	}
	if err := command.Args(command, nil); err != nil {
		t.Fatalf("Readwise should not require an instance URL: %v", err)
	}
	if err := command.Args(command, []string{"https://example.com"}); err == nil {
		t.Fatal("Readwise accepted an unexpected instance URL")
	}
	for _, name := range []string{"api-token", "batch-size", "skip-existing", "start-date", "end-date", "backend", "proxy", "global", "user-id", "format"} {
		if command.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s", name)
		}
	}
	if command.Annotations[executionScopeAnnotation] != string(executionScopeRemote) {
		t.Error("Readwise import must use the remote execution scope")
	}
	t.Setenv("NOGON_IMPORT_READWISE_TOKEN", "")
	err = command.RunE(command, nil)
	if err == nil || !strings.Contains(err.Error(), "NOGON_IMPORT_READWISE_TOKEN") {
		t.Fatalf("missing credential error = %v", err)
	}
}

func readwiseTestSource(t *testing.T, handler roundTripFunc) *readwiseClient {
	t.Helper()
	source, err := newReadwiseClient("readwise-secret", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Host != "readwise.io" || req.URL.Path != "/api/v3/list/" {
			return nil, fmt.Errorf("unexpected Readwise request: %s %s", req.Method, req.URL)
		}
		if req.Header.Get("Authorization") != "Token readwise-secret" || req.Header.Get("Accept") != "application/json" {
			t.Errorf("incorrect API headers: %v", req.Header)
		}
		if req.URL.Query().Get("withHtmlContent") != "true" {
			t.Error("stored HTML was not requested")
		}
		return handler(req)
	})})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestImportReadwisePaginatesAndPreservesContent(t *testing.T) {
	var cursors, fetched []string
	source := readwiseTestSource(t, func(req *http.Request) (*http.Response, error) {
		cursor := req.URL.Query().Get("pageCursor")
		cursors = append(cursors, cursor)
		if cursor == "" {
			return jsonHTTPResponse(req, http.StatusOK, `{"nextPageCursor":"opaque+/= cursor","results":[
				{"id":"article","url":"https://read.readwise.io/read/article","source_url":"https://example.com/article?utm_source=reader#part",
				"title":"Saved title & more","author":"Author","summary":"Saved summary","notes":"Personal notes",
				"category":"article","location":"archive","site_name":"Example","source":"Reader extension",
				"created_at":"2020-01-02T03:04:05.123456+00:00","updated_at":"2024-03-01T01:02:03.456Z",
				"tags":{"go":{"name":"Go"},"reading":{"name":"Reading"}},"published_date":"2019-12-01",
				"html_content":"<p>Saved article body.</p>"},
				{"id":"pdf","url":"https://read.readwise.io/read/pdf","source_url":null,"category":"pdf","html_content":"<p>Stored PDF text.</p>"},
				{"id":"highlight","parent_id":"article","category":"highlight","source_url":"https://example.com/article","html_content":"A passage"}
			]}`), nil
		}
		if cursor != "opaque+/= cursor" {
			return nil, fmt.Errorf("unexpected cursor %q", cursor)
		}
		return jsonHTTPResponse(req, http.StatusOK, `{"nextPageCursor":null,"results":[
			{"id":"epub","url":"https://read.readwise.io/read/epub","category":"epub","html_content":"<div>Book chapter text.</div>"},
			{"id":"video","source_url":"https://example.com/video","category":"video","notes":"Watch this","created_at":"2024-01-02T00:00:00Z"},
			{"id":"article","source_url":"https://example.com/article","title":"Duplicate page entry"},
			{"id":"no-url","title":"No usable URL"}
		]}`), nil
	})
	fetcher := serviceContentFetchFunc(func(_ context.Context, rawURL string) (*document.Document, error) {
		fetched = append(fetched, rawURL)
		return &document.Document{URL: rawURL, HTML: "<html><head><title>Video title</title></head><body>Video transcript.</body></html>"}, nil
	})
	var batches [][]*document.Document
	stats, err := importReadwise(context.Background(), source, raindropTestClient(t, &batches), document.NewNullLanguageDetector(), fetcher, serviceImportOptions{BatchSize: 3})
	if err != nil || stats != (serviceImportStats{Imported: 4, Skipped: 2}) {
		t.Fatalf("stats = %+v, error = %v", stats, err)
	}
	if !reflect.DeepEqual(cursors, []string{"", "opaque+/= cursor"}) || !reflect.DeepEqual(fetched, []string{"https://example.com/video"}) {
		t.Fatalf("cursors = %v, fetched = %v", cursors, fetched)
	}
	if len(batches) != 2 || len(batches[0]) != 3 || len(batches[1]) != 1 {
		t.Fatalf("unexpected batches: %v", batches)
	}
	article := batches[0][0]
	if article.URL != "https://example.com/article" || article.Title != "Saved title & more" || article.Label != "readwise" {
		t.Errorf("article = %+v", article)
	}
	for _, text := range []string{"Saved summary", "Personal notes", "Saved article body."} {
		if !strings.Contains(article.Text, text) {
			t.Errorf("article text missing %q: %q", text, article.Text)
		}
	}
	if !strings.Contains(article.HTML, "<html") || !strings.Contains(article.HTML, "Saved article body.") {
		t.Errorf("stored HTML = %q", article.HTML)
	}
	if article.Added != mustUnixTime(t, "2020-01-02T03:04:05Z") || article.Updated != mustUnixTime(t, "2024-03-01T01:02:03Z") {
		t.Errorf("article dates = %d, %d", article.Added, article.Updated)
	}
	for key, value := range map[string]string{
		"source": "readwise", "readwise_id": "article", "readwise_url": "https://read.readwise.io/read/article",
		"readwise_category": "article", "readwise_location": "archive", "readwise_author": "Author",
		"readwise_notes": "Personal notes", "readwise_published_date": "2019-12-01",
	} {
		if article.Metadata[key] != value {
			t.Errorf("metadata[%s] = %v, want %s", key, article.Metadata[key], value)
		}
	}
	if !reflect.DeepEqual(article.Metadata["readwise_tags"], []any{"Go", "Reading"}) {
		t.Errorf("tags = %v", article.Metadata["readwise_tags"])
	}
	for i, category := range []string{"pdf", "epub"} {
		d := batches[0][i+1]
		if d.URL != "https://read.readwise.io/read/"+category || d.HTML == "" || d.Text == "" {
			t.Errorf("%s content missing: %+v", category, d)
		}
	}
	video := batches[1][0]
	if video.Title != "Video title" || !strings.Contains(video.Text, "Video transcript.") || video.Added != video.Updated {
		t.Errorf("video = %+v", video)
	}
}

func TestImportReadwiseFiltersBeforeFetching(t *testing.T) {
	source := readwiseTestSource(t, func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(req, http.StatusOK, `{"results":[
			{"id":"old","source_url":"https://example.com/old","created_at":"2023-12-31T00:00:00Z"},
			{"id":"future","source_url":"https://example.com/future","created_at":"2025-01-01T00:00:00Z"},
			{"id":"exists","source_url":"https://example.com/exists?utm_source=reader","created_at":"2024-01-02T00:00:00Z"},
			{"id":"new","source_url":"https://example.com/new","created_at":"2024-01-03T00:00:00Z"}
		]}`), nil
	})
	var batches [][]*document.Document
	var checked, fetched []string
	batchTransport := raindropTestTransport(t, &batches)
	target := client.New("http://hister.example", client.WithMaxBatchBodyBytes(40<<20), client.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodHead {
			rawURL := req.URL.Query().Get("url")
			checked = append(checked, rawURL)
			status := http.StatusNotFound
			if rawURL == "https://example.com/exists" {
				status = http.StatusOK
			}
			return jsonHTTPResponse(req, status, ""), nil
		}
		return batchTransport(req)
	})}))
	fetcher := serviceContentFetchFunc(func(_ context.Context, rawURL string) (*document.Document, error) {
		fetched = append(fetched, rawURL)
		return &document.Document{URL: rawURL, Text: "Fetched text"}, nil
	})
	stats, err := importReadwise(context.Background(), source, target, document.NewNullLanguageDetector(), fetcher, serviceImportOptions{
		BatchSize: 10, SkipExisting: true, StartDate: mustUnixTime(t, "2024-01-01T00:00:00Z"), EndDate: mustUnixTime(t, "2024-12-31T23:59:59Z"),
		Label: documentLabelOverride{set: true, value: "library"},
	})
	if err != nil || stats != (serviceImportStats{Imported: 1, Skipped: 3}) {
		t.Fatalf("stats = %+v, error = %v", stats, err)
	}
	if !reflect.DeepEqual(checked, []string{"https://example.com/exists", "https://example.com/new"}) || !reflect.DeepEqual(fetched, []string{"https://example.com/new"}) {
		t.Errorf("checked = %v, fetched = %v", checked, fetched)
	}
	if batches[0][0].Label != "library" {
		t.Errorf("label = %q", batches[0][0].Label)
	}
}

func TestImportReadwiseRejectsUnsafeURLs(t *testing.T) {
	for _, rawURL := range []string{"file:///etc/passwd", "javascript:alert(1)", "relative/path", "https:///no-host", "https://user:password@example.com"} {
		t.Run(rawURL, func(t *testing.T) {
			_, _, err := readwiseDocument(readwiseItem{SourceURL: rawURL}, document.NewNullLanguageDetector())
			if err == nil {
				t.Fatal("unsafe source URL accepted")
			}
			_, _, err = readwiseDocument(readwiseItem{URL: rawURL}, document.NewNullLanguageDetector())
			if err == nil {
				t.Fatal("unsafe fallback URL accepted")
			}
		})
	}
}

func TestImportReadwiseRetainsMetadataWhenContentIsUnavailable(t *testing.T) {
	source := readwiseTestSource(t, func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(req, http.StatusOK, `{"results":[
			{"id":"unavailable","source_url":"https://example.com/unavailable","title":"Saved title","notes":"Keep my note","html_content":"<p></p>"},
			{"id":"upload","url":"https://read.readwise.io/read/upload","title":"Uploaded book","category":"epub"},
			{"id":"empty-upload","url":"https://read.readwise.io/read/empty-upload","category":"pdf","html_content":"<p></p>"},
			{"id":"unsafe","source_url":"file:///etc/passwd"}
		]}`), nil
	})
	var fetched []string
	fetcher := serviceContentFetchFunc(func(_ context.Context, rawURL string) (*document.Document, error) {
		fetched = append(fetched, rawURL)
		return nil, errors.New("source unavailable")
	})
	var batches [][]*document.Document
	stats, err := importReadwise(context.Background(), source, raindropTestClient(t, &batches), document.NewNullLanguageDetector(), fetcher, serviceImportOptions{BatchSize: 10})
	if err != nil || stats != (serviceImportStats{Imported: 3, Errors: 3}) {
		t.Fatalf("stats = %+v, error = %v", stats, err)
	}
	if !reflect.DeepEqual(fetched, []string{"https://example.com/unavailable"}) || batches[0][0].Text != "Keep my note" {
		t.Errorf("fetched = %v, retained document = %+v", fetched, batches[0][0])
	}
}

func TestImportReadwisePaginationErrorsFlushPendingDocuments(t *testing.T) {
	for _, second := range []string{`{"results":[],"nextPageCursor":"again"}`, `{"detail":"not a document list"}`, `{invalid`} {
		t.Run(second, func(t *testing.T) {
			requests := 0
			source := readwiseTestSource(t, func(req *http.Request) (*http.Response, error) {
				requests++
				body := second
				if requests == 1 {
					body = `{"nextPageCursor":"again","results":[{"id":"first","source_url":"https://example.com/first"}]}`
				}
				return jsonHTTPResponse(req, http.StatusOK, body), nil
			})
			var batches [][]*document.Document
			stats, err := importReadwise(context.Background(), source, raindropTestClient(t, &batches), document.NewNullLanguageDetector(), nil, serviceImportOptions{BatchSize: 10})
			if err == nil || requests != 2 || stats.Imported != 1 || len(batches) != 1 {
				t.Fatalf("error = %v, requests = %d, stats = %+v", err, requests, stats)
			}
		})
	}
}

func TestReadwiseAuthenticationAndRateLimits(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			requests, waits := 0, 0
			source := readwiseTestSource(t, func(req *http.Request) (*http.Response, error) {
				requests++
				response := jsonHTTPResponse(req, status, `{"detail":"readwise-secret"}`)
				response.Header.Set("Retry-After", "2")
				return response, nil
			})
			source.wait = func(_ context.Context, delay time.Duration) error {
				waits++
				if delay != 2*time.Second {
					t.Errorf("delay = %v", delay)
				}
				return nil
			}
			_, err := source.documents(context.Background(), "")
			if err == nil {
				t.Fatal("API failure was ignored")
			}
			if status == http.StatusTooManyRequests {
				if requests != 4 || waits != 3 {
					t.Errorf("requests = %d, waits = %d", requests, waits)
				}
				source.wait = func(_ context.Context, _ time.Duration) error { return context.Canceled }
				_, err = source.documents(context.Background(), "")
				if !errors.Is(err, context.Canceled) {
					t.Errorf("canceled retry = %v", err)
				}
			} else if requests != 1 || strings.Contains(err.Error(), "readwise-secret") || !strings.Contains(err.Error(), "NOGON_IMPORT_READWISE_TOKEN") {
				t.Errorf("requests = %d, authentication error = %v", requests, err)
			}
		})
	}
}

func TestReadwiseRateLimitRecovery(t *testing.T) {
	requests := 0
	source := readwiseTestSource(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return jsonHTTPResponse(req, http.StatusTooManyRequests, ""), nil
		}
		return jsonHTTPResponse(req, http.StatusOK, `{"results":[],"nextPageCursor":""}`), nil
	})
	source.wait = func(_ context.Context, _ time.Duration) error { return nil }
	stats, err := importReadwise(context.Background(), source, nil, nil, nil, serviceImportOptions{BatchSize: 10})
	if err != nil || requests != 2 || stats != (serviceImportStats{}) {
		t.Fatalf("requests = %d, stats = %+v, error = %v", requests, stats, err)
	}
}

func TestReadwiseRefusesRedirectsToAnotherOrigin(t *testing.T) {
	source, err := newReadwiseClient("readwise-secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	source.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		response := jsonHTTPResponse(req, http.StatusFound, "")
		response.Header.Set("Location", "https://other.example/api/v3/list/")
		return response, nil
	})
	_, err = source.documents(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "different origin") || requests != 1 {
		t.Fatalf("redirect error = %v, requests = %d", err, requests)
	}
	if strings.Contains(err.Error(), "readwise-secret") {
		t.Fatal("redirect error exposed the API token")
	}
}

func TestImportReadwiseCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := readwiseTestSource(t, func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(req, http.StatusOK, `{"results":[{"id":"first","source_url":"https://example.com/first"},{"id":"second","source_url":"https://example.com/second"}]}`), nil
	})
	fetcher := serviceContentFetchFunc(func(_ context.Context, rawURL string) (*document.Document, error) {
		cancel()
		return &document.Document{URL: rawURL, Text: "Fetched content"}, nil
	})
	var batches [][]*document.Document
	stats, err := importReadwise(ctx, source, raindropTestClient(t, &batches), document.NewNullLanguageDetector(), fetcher, serviceImportOptions{BatchSize: 10})
	if !errors.Is(err, context.Canceled) || stats.Imported != 1 {
		t.Fatalf("stats = %+v, error = %v", stats, err)
	}
}
