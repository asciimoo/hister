package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/server/document"
)

func raindropTestClient(t *testing.T, batches *[][]*document.Document) *client.Client {
	t.Helper()
	httpClient := &http.Client{Transport: raindropTestTransport(t, batches)}
	return client.New("http://nogon.example", client.WithHTTPClient(httpClient), client.WithMaxBatchBodyBytes(40<<20))
}

func raindropTestTransport(t *testing.T, batches *[][]*document.Document) roundTripFunc {
	t.Helper()
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/api/batch" {
			return nil, fmt.Errorf("unexpected Nogon request: %s %s", req.Method, req.URL)
		}
		var body struct {
			Ops []*document.Document `json:"ops"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return nil, err
		}
		*batches = append(*batches, body.Ops)
		results := make([]map[string]int, len(body.Ops))
		for i := range results {
			results[i] = map[string]int{"status": http.StatusCreated}
		}
		var response bytes.Buffer
		if err := json.NewEncoder(&response).Encode(map[string]any{"results": results}); err != nil {
			return nil, err
		}
		return jsonHTTPResponse(req, http.StatusOK, response.String()), nil
	})
}

func TestImportRaindropCSVPreservesBookmarksAndDownloadsContent(t *testing.T) {
	input := `id,title,note,excerpt,url,folder,tags,created,cover,highlights,favorite
42,"Saved, ""quoted"" title","First note line
Second note line",Saved description,https://example.com/article?utm_source=raindrop&keep=1#part,Research/Go,"reading, go, reading, ",2024-01-02T03:04:05.123Z,https://example.com/cover.jpg,"First highlight
Second highlight",true
43,,,,https://example.com/untitled,,,1704164645,,,
44,Unavailable page,Keep this note,,https://example.com/unavailable,,,2024-01-02,,Keep this highlight,
`
	var fetchedURLs []string
	fetcher := serviceContentFetchFunc(func(_ context.Context, rawURL string) (*document.Document, error) {
		fetchedURLs = append(fetchedURLs, rawURL)
		if strings.HasSuffix(rawURL, "/unavailable") {
			return nil, errors.New("page is unavailable")
		}
		return &document.Document{
			URL:  rawURL,
			HTML: `<html><head><title>Downloaded title</title></head><body><main><p>Downloaded article text.</p></main></body></html>`,
		}, nil
	})
	var batches [][]*document.Document
	stats, err := importRaindropCSV(context.Background(), strings.NewReader(input), raindropTestClient(t, &batches), document.NewNullLanguageDetector(), fetcher, serviceImportOptions{
		BatchSize: 2,
		FaviconDownloader: func(d *document.Document) error {
			d.Favicon = "data:image/png;base64,aWNvbg=="
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := (serviceImportStats{Imported: 3, Errors: 1}); stats != want {
		t.Fatalf("stats = %+v, want %+v", stats, want)
	}
	if len(batches) != 2 || len(batches[0]) != 2 || len(batches[1]) != 1 {
		t.Fatalf("batches = %+v, want sizes 2 and 1", batches)
	}
	if want := []string{"https://example.com/article?utm_source=raindrop&keep=1#part", "https://example.com/untitled", "https://example.com/unavailable"}; !reflect.DeepEqual(fetchedURLs, want) {
		t.Fatalf("fetched URLs = %v, want %v", fetchedURLs, want)
	}
	article := batches[0][0]
	if article.URL != "https://example.com/article?keep=1" || article.Title != `Saved, "quoted" title` {
		t.Errorf("article URL = %q, title = %q", article.URL, article.Title)
	}
	for _, text := range []string{"Saved description", "First note line\nSecond note line", "First highlight\nSecond highlight", "Downloaded article text."} {
		if !strings.Contains(article.Text, text) {
			t.Errorf("article text %q does not contain %q", article.Text, text)
		}
	}
	if article.HTML == "" || article.Favicon != "data:image/png;base64,aWNvbg==" {
		t.Errorf("article content or favicon was not preserved: %+v", article)
	}
	if article.Added != mustUnixTime(t, "2024-01-02T03:04:05Z") || article.Updated != article.Added {
		t.Errorf("article dates: added = %d, updated = %d", article.Added, article.Updated)
	}
	for key, want := range map[string]any{
		"source": "raindrop", "raindrop_id": "42", "raindrop_folder": "Research/Go",
		"raindrop_favorite": "true", "raindrop_cover": "https://example.com/cover.jpg",
		"raindrop_note": "First note line\nSecond note line", "description": "Saved description",
		"raindrop_highlights": "First highlight\nSecond highlight", "raindrop_tags": []any{"reading", "go"},
	} {
		if !reflect.DeepEqual(article.Metadata[key], want) {
			t.Errorf("metadata[%q] = %#v, want %#v", key, article.Metadata[key], want)
		}
	}
	untitled := batches[0][1]
	if untitled.Title != "Downloaded title" || untitled.Added != article.Added {
		t.Errorf("untitled bookmark = %+v", untitled)
	}
	unavailable := batches[1][0]
	if unavailable.Title != "Unavailable page" || unavailable.Text != "Keep this note\n\nKeep this highlight" {
		t.Errorf("unavailable bookmark = %+v", unavailable)
	}
	for _, batch := range batches {
		for _, d := range batch {
			if d.Label != "raindrop" || !d.Processed || d.Type != document.Web {
				t.Errorf("document was not prepared for import: %+v", d)
			}
		}
	}
}

func TestImportRaindropCSVCSVVariantsAndErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		stats serviceImportStats
		err   string
	}{
		{name: "URL only", input: "url\nhttps://example.com\n", stats: serviceImportStats{Imported: 1}},
		{name: "header only", input: "url,title\n"},
		{name: "BOM and reordered quoted header", input: "\ufeff\" Title \",\" URL \",unknown\r\nTitle,https://example.com,ignored\r\n", stats: serviceImportStats{Imported: 1}},
		{name: "empty", err: "empty"},
		{name: "missing URL header", input: "title,note\nTitle,Note\n", err: "required url column"},
		{name: "duplicate header", input: "url,URL\n", err: "duplicate CSV column"},
		{name: "invalid header", input: "\"url\n", err: "read CSV header"},
		{name: "missing URL", input: "url,title\n,No URL\nhttps://example.com,Valid\n", stats: serviceImportStats{Imported: 1, Skipped: 1}},
		{name: "bad field counts", input: "url,title\nhttps://example.com/short\nhttps://example.com/long,title,extra\nhttps://example.com,Valid\n", stats: serviceImportStats{Imported: 1, Errors: 2}},
		{name: "invalid URLs", input: "url\nfile:///etc/passwd\njavascript:alert(1)\nrelative/path\nhttps:///missing-host\nhttps://example.com\n", stats: serviceImportStats{Imported: 1, Errors: 4}},
		{name: "invalid date", input: "url,created\nhttps://example.com/invalid,yesterday\nhttps://example.com,2024-01-02\n", stats: serviceImportStats{Imported: 1, Errors: 1}},
		{name: "parse error flushes pending documents", input: "url,title\nhttps://example.com,Valid\nhttps://example.com/broken,\"unterminated\n", stats: serviceImportStats{Imported: 1}, err: "read CSV record 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var batches [][]*document.Document
			stats, err := importRaindropCSV(context.Background(), strings.NewReader(tt.input), raindropTestClient(t, &batches), document.NewNullLanguageDetector(), nil, serviceImportOptions{BatchSize: 10})
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("error = %v, want %q", err, tt.err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if stats != tt.stats {
				t.Fatalf("stats = %+v, want %+v", stats, tt.stats)
			}
			if tt.name == "URL only" && batches[0][0].Title != "https://example.com" {
				t.Errorf("title = %q, want URL fallback", batches[0][0].Title)
			}
		})
	}
}

func TestImportRaindropCSVFiltersBeforeFetching(t *testing.T) {
	input := `url,title,created
https://example.com/old,Old,2023-12-31
https://example.com/new,New,2024-02-01
https://example.com/existing?utm_source=raindrop#part,Existing,2024-01-02
https://example.com/article,Article,2024-01-03
`
	var batches [][]*document.Document
	batchTransport := raindropTestTransport(t, &batches)
	var checkedURLs []string
	target := client.New("http://nogon.example", client.WithMaxBatchBodyBytes(40<<20), client.WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodHead && req.URL.Path == "/api/document" {
				rawURL := req.URL.Query().Get("url")
				checkedURLs = append(checkedURLs, rawURL)
				status := http.StatusNotFound
				if rawURL == "https://example.com/existing" {
					status = http.StatusOK
				}
				return jsonHTTPResponse(req, status, ""), nil
			}
			return batchTransport.RoundTrip(req)
		}),
	}))
	var fetchedURLs []string
	fetcher := serviceContentFetchFunc(func(_ context.Context, rawURL string) (*document.Document, error) {
		fetchedURLs = append(fetchedURLs, rawURL)
		return &document.Document{URL: rawURL, Text: "Downloaded text"}, nil
	})
	stats, err := importRaindropCSV(context.Background(), strings.NewReader(input), target, document.NewNullLanguageDetector(), fetcher, serviceImportOptions{
		BatchSize: 10, SkipExisting: true,
		StartDate: mustUnixTime(t, "2024-01-01T00:00:00Z"), EndDate: mustUnixTime(t, "2024-01-31T23:59:59Z"),
		Label: documentLabelOverride{set: true, value: "reading"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := (serviceImportStats{Imported: 1, Skipped: 3}); stats != want {
		t.Fatalf("stats = %+v, want %+v", stats, want)
	}
	if !reflect.DeepEqual(checkedURLs, []string{"https://example.com/existing", "https://example.com/article"}) {
		t.Errorf("existence checks = %v", checkedURLs)
	}
	if !reflect.DeepEqual(fetchedURLs, []string{"https://example.com/article"}) {
		t.Errorf("fetched URLs = %v", fetchedURLs)
	}
	if len(batches) != 1 || len(batches[0]) != 1 || batches[0][0].Label != "reading" {
		t.Errorf("imported documents = %+v", batches)
	}
}

func TestImportRaindropCSVCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var batches [][]*document.Document
	fetches := 0
	fetcher := serviceContentFetchFunc(func(_ context.Context, rawURL string) (*document.Document, error) {
		fetches++
		cancel()
		return nil, ctx.Err()
	})
	stats, err := importRaindropCSV(ctx, strings.NewReader("url\nhttps://example.com/first\nhttps://example.com/second\n"), raindropTestClient(t, &batches), document.NewNullLanguageDetector(), fetcher, serviceImportOptions{BatchSize: 10})
	if !errors.Is(err, context.Canceled) || fetches != 1 || stats.Imported != 1 || stats.Errors != 1 {
		t.Fatalf("error = %v, fetches = %d, stats = %+v", err, fetches, stats)
	}
	_, err = importRaindropCSV(ctx, strings.NewReader(""), nil, nil, nil, serviceImportOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("already canceled import error = %v", err)
	}
}
