package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
)

func TestServiceImportBufferDownloadsMissingFavicon(t *testing.T) {
	var received *document.Document
	faviconDownloads := 0
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
	target := client.New("http://hister.example", client.WithHTTPClient(targetHTTPClient), client.WithMaxBatchBodyBytes(40<<20))
	buffer, err := newServiceImportBuffer(
		"test",
		target,
		document.NewNullLanguageDetector(),
		nil,
		serviceImportOptions{
			BatchSize: 1,
			FaviconDownloader: func(d *document.Document) error {
				faviconDownloads++
				d.Favicon = "data:image/png;base64,ZGVmYXVsdCBpY29u"
				return nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	d := &document.Document{
		URL:       "https://example.com/article",
		Title:     "Article",
		Text:      "Contents",
		Processed: true,
	}
	buffer.Add(context.Background(), d, nil)

	if received == nil {
		t.Fatal("no document was submitted")
	}
	if received.Favicon != "data:image/png;base64,ZGVmYXVsdCBpY29u" {
		t.Errorf("favicon = %q, want downloaded default icon", received.Favicon)
	}
	if faviconDownloads != 1 {
		t.Errorf("favicon downloads = %d, want one", faviconDownloads)
	}
	if buffer.stats.Imported != 1 || buffer.stats.Errors != 0 {
		t.Errorf("stats = %+v, want one import without errors", buffer.stats)
	}
}

func TestApplyServiceContentPreservesFetchedFavicon(t *testing.T) {
	d := &document.Document{URL: "https://example.com/article"}
	fetched := &document.Document{
		URL:     "https://example.com/article",
		HTML:    `<html><head><title>Article</title></head><body><main><p>Downloaded contents.</p></main></body></html>`,
		Favicon: "data:image/png;base64,bGlua2VkIGljb24=",
	}
	if err := applyServiceContent(context.Background(), d, fetched, "", "", document.NewNullLanguageDetector()); err != nil {
		t.Fatal(err)
	}
	if d.Favicon != fetched.Favicon {
		t.Errorf("favicon = %q, want fetched favicon %q", d.Favicon, fetched.Favicon)
	}
}

// TestCrawlerContentFetcherFetchesOnlyRequestedURL pins the single-document
// contract of the service content fetcher: one Fetch means one page, and the
// crawl behind it must be finished by the time Fetch returns so the cached
// crawler is reusable for the next URL.
func TestCrawlerContentFetcherFetchesOnlyRequestedURL(t *testing.T) {
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><a href="/a">a</a><a href="/b">b</a></body></html>`)
	}))
	defer srv.Close()

	f := newCrawlerServiceContentFetcher(&config.CrawlerConfig{
		Rate: config.CrawlerRate{
			GlobalRPS:          1000,
			PerHostRPS:         1000,
			GlobalConcurrency:  1,
			PerHostConcurrency: 1,
		},
		ShutdownGrace: 1,
	})
	defer func() {
		if err := f.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	doc, err := f.Fetch(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if doc == nil {
		t.Fatal("Fetch returned no document")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("first Fetch made %d requests, want 1 (linked pages must not be crawled)", got)
	}

	if _, err := f.Fetch(context.Background(), srv.URL+"/a"); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("after two Fetches: %d requests, want 2", got)
	}
}
