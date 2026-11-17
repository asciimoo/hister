package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/server/document"
)

func raindropTestSource(t *testing.T, handler roundTripFunc) *raindropClient {
	t.Helper()
	source, err := newRaindropClient("raindrop-secret", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Host != "api.raindrop.io" {
			return nil, fmt.Errorf("unexpected Raindrop request: %s %s", req.Method, req.URL)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer raindrop-secret" {
			t.Errorf("Authorization = %q", got)
		}
		if req.Header.Get("Accept") != "application/json" || req.Header.Get("User-Agent") == "" {
			t.Errorf("missing API headers: %v", req.Header)
		}
		if req.URL.Path == "/rest/v1/collections" {
			return jsonHTTPResponse(req, http.StatusOK, `{"result":true,"items":[{"_id":1,"title":"Research"}]}`), nil
		}
		if req.URL.Path == "/rest/v1/collections/childrens" {
			return jsonHTTPResponse(req, http.StatusOK, `{"result":true,"items":[{"_id":2,"title":"Go","parent":{"$id":1}}]}`), nil
		}
		return handler(req)
	})})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestImportRaindropAPIOrdersUpdatesAndPreservesMetadata(t *testing.T) {
	var pages []string
	source := raindropTestSource(t, func(req *http.Request) (*http.Response, error) {
		query := req.URL.Query()
		if req.URL.Path != "/rest/v1/raindrops/0" || query.Get("perpage") != "2" || query.Get("sort") != "created" || query.Get("search") != "" {
			return nil, fmt.Errorf("unexpected bookmarks request: %s", req.URL)
		}
		pages = append(pages, query.Get("page"))
		var body string
		switch query.Get("page") {
		case "0":
			body = `{"result":true,"items":[
				{"_id":42,"link":"https://example.com/edited?utm_source=raindrop#part","title":"Saved title",
				"created":"2020-01-02T03:04:05Z","lastUpdate":"2024-03-01T01:02:03.456Z",
				"excerpt":"Description","note":"Bookmark note","cover":"https://example.com/cover.jpg",
				"collection":{"$id":2},"important":true,"broken":true,"type":"article",
				"tags":["go","tag, with comma","go"," "],
				"highlights":[{"_id":"highlight-id","text":"Quoted passage","note":"Highlight note","color":"green","created":"2024-02-01T00:00:00Z"}]},
				{"_id":43,"link":"https://example.com/earlier","title":"Earlier update","created":"2023-01-02T00:00:00Z","lastUpdate":"2024-01-02T00:00:00Z","collection":{"$id":-1}}
			]}`
		case "1":
			body = `{"result":true,"items":[
				{"_id":44,"link":"https://example.com/middle","created":"2024-01-03T00:00:00Z","lastUpdate":"2024-02-01T00:00:00Z"},
				{"_id":45,"title":"No URL","created":"2024-01-04T00:00:00Z","lastUpdate":"2024-02-02T00:00:00Z"}
			]}`
		case "2":
			body = `{"result":true,"items":[]}`
		default:
			return nil, fmt.Errorf("unexpected page %q", query.Get("page"))
		}
		return jsonHTTPResponse(req, http.StatusOK, body), nil
	})
	source.pageSize = 2
	var fetchedURLs []string
	fetcher := serviceContentFetchFunc(func(_ context.Context, rawURL string) (*document.Document, error) {
		fetchedURLs = append(fetchedURLs, rawURL)
		return &document.Document{URL: rawURL, HTML: `<html><head><title>Fetched title</title></head><body><p>Downloaded content.</p></body></html>`}, nil
	})
	var batches [][]*document.Document
	stats, err := importRaindrop(context.Background(), source, raindropTestClient(t, &batches), document.NewNullLanguageDetector(), fetcher, serviceImportOptions{BatchSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if want := (serviceImportStats{Imported: 3, Skipped: 1}); stats != want {
		t.Fatalf("stats = %+v, want %+v", stats, want)
	}
	if !reflect.DeepEqual(pages, []string{"0", "1", "2"}) {
		t.Errorf("pages = %v", pages)
	}
	if want := []string{"https://example.com/earlier", "https://example.com/middle", "https://example.com/edited?utm_source=raindrop#part"}; !reflect.DeepEqual(fetchedURLs, want) {
		t.Fatalf("fetch order = %v, want %v", fetchedURLs, want)
	}
	if len(batches) != 2 || len(batches[0]) != 2 || len(batches[1]) != 1 {
		t.Fatalf("batches = %+v", batches)
	}
	edited := batches[1][0]
	if edited.Title != "Saved title" || edited.URL != "https://example.com/edited" || edited.Label != "raindrop" {
		t.Errorf("edited document = %+v", edited)
	}
	if edited.Added != mustUnixTime(t, "2020-01-02T03:04:05Z") || edited.Updated != mustUnixTime(t, "2024-03-01T01:02:03Z") {
		t.Errorf("edited dates: added = %d, updated = %d", edited.Added, edited.Updated)
	}
	for _, want := range []string{"Description", "Bookmark note", "Quoted passage", "Highlight note", "Downloaded content."} {
		if !strings.Contains(edited.Text, want) {
			t.Errorf("text %q does not contain %q", edited.Text, want)
		}
	}
	for key, want := range map[string]any{
		"source": "raindrop", "raindrop_import": "api", "raindrop_id": float64(42), "raindrop_collection_id": float64(2),
		"raindrop_folder": "Research/Go", "raindrop_type": "article", "raindrop_favorite": true, "raindrop_broken": true,
		"raindrop_cover": "https://example.com/cover.jpg", "raindrop_tags": []any{"go", "tag, with comma"},
	} {
		if !reflect.DeepEqual(edited.Metadata[key], want) {
			t.Errorf("metadata[%q] = %#v, want %#v", key, edited.Metadata[key], want)
		}
	}
	highlights, ok := edited.Metadata["raindrop_highlight_details"].([]any)
	if !ok || len(highlights) != 1 {
		t.Fatalf("highlight details = %#v", edited.Metadata["raindrop_highlight_details"])
	}
	if highlight := highlights[0].(map[string]any); highlight["_id"] != "highlight-id" || highlight["color"] != "green" {
		t.Errorf("highlight details = %#v", highlight)
	}
	if batches[0][0].Metadata["raindrop_folder"] != "Unsorted" || batches[0][1].Title != "Fetched title" {
		t.Errorf("earlier documents = %+v", batches[0])
	}
}

func TestImportRaindropAPIRetriesOlderFailedBookmarks(t *testing.T) {
	source := raindropTestSource(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("search") != "" {
			t.Errorf("unexpected API filter: %s", req.URL)
		}
		return jsonHTTPResponse(req, http.StatusOK, `{"result":true,"items":[
			{"_id":1,"link":"https://example.com/old","created":"2020-01-01T00:00:00Z","lastUpdate":"2020-01-01T00:00:00Z"},
			{"_id":2,"link":"https://example.com/new","created":"2024-01-01T00:00:00Z","lastUpdate":"2024-01-01T00:00:00Z"}
		]}`), nil
	})
	var batches [][]*document.Document
	transport := raindropTestTransport(t, &batches)
	fail := true
	target := client.New("http://hister.example", client.WithMaxBatchBodyBytes(40<<20), client.WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			response, err := transport.RoundTrip(req)
			if err != nil || !fail {
				return response, err
			}
			_ = response.Body.Close()
			return jsonHTTPResponse(req, http.StatusOK, `{"results":[{"status":503},{"status":201}]}`), nil
		}),
	}))
	for attempt := range 2 {
		stats, err := importRaindrop(context.Background(), source, target, document.NewNullLanguageDetector(), nil, serviceImportOptions{BatchSize: 10})
		if err != nil {
			t.Fatal(err)
		}
		want := serviceImportStats{Imported: 2}
		if fail {
			want = serviceImportStats{Imported: 1, Errors: 1}
		}
		if stats != want {
			t.Fatalf("attempt %d stats = %+v, want %+v", attempt, stats, want)
		}
		fail = false
	}
	if len(batches) != 2 || len(batches[1]) != 2 || batches[1][0].URL != "https://example.com/old" {
		t.Fatalf("older failed bookmark was not retried: %+v", batches)
	}
}

func TestRaindropAPIPaginationErrorsDoNotSubmitDocuments(t *testing.T) {
	for _, response := range []string{
		`{"result":false,"items":[]}`,
		`{"result":true}`,
		`invalid JSON`,
		`{"result":true,"items":[{"_id":1,"link":"https://example.com"}]}`,
	} {
		t.Run(response, func(t *testing.T) {
			requests := 0
			source := raindropTestSource(t, func(req *http.Request) (*http.Response, error) {
				requests++
				if requests == 1 {
					return jsonHTTPResponse(req, http.StatusOK, `{"result":true,"items":[{"_id":1,"link":"https://example.com"}]}`), nil
				}
				return jsonHTTPResponse(req, http.StatusOK, response), nil
			})
			source.pageSize = 1
			var batches [][]*document.Document
			stats, err := importRaindrop(context.Background(), source, raindropTestClient(t, &batches), document.NewNullLanguageDetector(), nil, serviceImportOptions{BatchSize: 1})
			if err == nil || requests != 2 || stats.Imported != 0 || len(batches) != 0 {
				t.Fatalf("error = %v, requests = %d, stats = %+v, batches = %v", err, requests, stats, batches)
			}
		})
	}
}

func TestRaindropAPIAuthenticationFailure(t *testing.T) {
	source := raindropTestSource(t, func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(req, http.StatusUnauthorized, `{"error":"raindrop-secret"}`), nil
	})
	_, err := source.bookmarks(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "authentication failed") || !strings.Contains(err.Error(), raindropTokenEnv) || strings.Contains(err.Error(), "raindrop-secret") {
		t.Fatalf("authentication error = %v", err)
	}
}

func TestRaindropAPIRetriesRateLimits(t *testing.T) {
	requests, waits := 0, 0
	source := raindropTestSource(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			resp := jsonHTTPResponse(req, http.StatusTooManyRequests, `{"error":"rate limit"}`)
			resp.Header.Set("Retry-After", "2")
			return resp, nil
		}
		return jsonHTTPResponse(req, http.StatusOK, `{"result":true,"items":[]}`), nil
	})
	source.wait = func(_ context.Context, delay time.Duration) error {
		waits++
		if delay != 2*time.Second {
			t.Errorf("retry delay = %v", delay)
		}
		return nil
	}
	if _, err := source.bookmarks(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || waits != 1 {
		t.Errorf("requests = %d, waits = %d", requests, waits)
	}
}

func TestRaindropAPIRateLimitRetryBoundAndCancellation(t *testing.T) {
	requests, waits := 0, 0
	source := raindropTestSource(t, func(req *http.Request) (*http.Response, error) {
		requests++
		return jsonHTTPResponse(req, http.StatusTooManyRequests, ""), nil
	})
	source.wait = func(_ context.Context, _ time.Duration) error { waits++; return nil }
	_, err := source.bookmarks(context.Background(), 0)
	if err == nil || requests != 4 || waits != 3 {
		t.Fatalf("error = %v, requests = %d, waits = %d", err, requests, waits)
	}
	source.wait = func(_ context.Context, _ time.Duration) error { return context.Canceled }
	_, err = source.bookmarks(context.Background(), 0)
	if !errors.Is(err, context.Canceled) || requests != 5 {
		t.Fatalf("canceled retry error = %v, requests = %d", err, requests)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForServiceImport(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error = %v", err)
	}
}

func TestRaindropRetryDelay(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	for _, tt := range []struct {
		header http.Header
		want   time.Duration
	}{
		{header: http.Header{"Retry-After": {"2"}}, want: 2 * time.Second},
		{header: http.Header{"Retry-After": {now.Add(3 * time.Second).Format(http.TimeFormat)}}, want: 3 * time.Second},
		{header: http.Header{"X-Ratelimit-Reset": {strconv.FormatInt(now.Unix()+4, 10)}}, want: 5 * time.Second},
		{header: http.Header{"Retry-After": {"9223372036854775807"}}, want: 5 * time.Minute},
		{header: http.Header{"Retry-After": {"invalid"}}, want: time.Minute},
	} {
		if got := serviceImportRetryDelay(tt.header, now); got != tt.want {
			t.Errorf("delay for %v = %v, want %v", tt.header, got, tt.want)
		}
	}
}
