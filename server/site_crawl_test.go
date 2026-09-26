package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/crawler"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/testutil"
)

func TestSiteCrawlAPIAuthAndValidation(t *testing.T) {
	_, handler := newPublicTokenTestServer(t)
	headers := map[string]string{"X-Access-Token": "secret", "Origin": chromeExtensionOrigin, "Content-Type": "application/json"}
	for _, path := range []string{"/api/crawl", "/api/crawl/stop?id=missing"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			if method == http.MethodGet && strings.Contains(path, "stop") {
				continue
			}
			res := testutil.ServeHTTP(t, handler, method, path, strings.NewReader(`{}`), nil)
			if res.Code != http.StatusForbidden {
				t.Errorf("unauthenticated %s %s: %d", method, path, res.Code)
			}
		}
	}
	res := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/crawl", strings.NewReader(`{"url":"https://example.com"}`), map[string]string{"X-Access-Token": "secret", "Origin": "https://evil.example"})
	if res.Code != http.StatusForbidden {
		t.Fatalf("cross-site start: %d", res.Code)
	}
	for _, body := range []string{`{`, `{"url":"file:///etc/passwd"}`, `{"url":"https://example.com:8443"}`, `{"url":"https://example.com","max_pages":1001}`, `{"url":"https://example.com","max_pages":-1}`} {
		res = testutil.ServeHTTP(t, handler, http.MethodPost, "/api/crawl", strings.NewReader(body), headers)
		if res.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", body, res.Code, res.Body.String())
		}
	}
	res = testutil.ServeHTTP(t, handler, http.MethodGet, "/api/crawl", nil, headers)
	if res.Code != http.StatusOK || strings.TrimSpace(res.Body.String()) != "null" {
		t.Fatalf("initial status: %d %s", res.Code, res.Body.String())
	}
}

func TestSiteCrawlOwnershipAndStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &siteCrawls{job: &siteCrawl{ID: "owned", userID: 42, URL: "https://example.com/", State: "running", cancel: cancel}}
	c := &webContext{crawls: s, UserID: 7, Request: httptest.NewRequest(http.MethodPost, "/api/crawl/stop?id=owned", nil), Response: httptest.NewRecorder()}
	serveSiteCrawl(c)
	if strings.TrimSpace(c.Response.(*httptest.ResponseRecorder).Body.String()) != "null" {
		t.Fatal("exposed another user's crawl")
	}
	c.Response = httptest.NewRecorder()
	stopSiteCrawl(c)
	if c.Response.(*httptest.ResponseRecorder).Code != http.StatusNotFound || ctx.Err() != nil {
		t.Fatal("another user stopped crawl")
	}
	c.UserID = 42
	c.Request = httptest.NewRequest(http.MethodPost, "/api/crawl/stop?id=stale", nil)
	c.Response = httptest.NewRecorder()
	stopSiteCrawl(c)
	if ctx.Err() != nil {
		t.Fatal("stale popup stopped crawl")
	}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/crawl/stop?id=owned", nil)
	c.Response = httptest.NewRecorder()
	stopSiteCrawl(c)
	if ctx.Err() == nil || c.Response.(*httptest.ResponseRecorder).Code != http.StatusNoContent {
		t.Fatal("owner could not stop crawl")
	}
}

type siteTestCrawler struct {
	docs   []*document.Document
	wait   bool
	closed bool
}

func (f *siteTestCrawler) Crawl(ctx context.Context, _ string, _ *crawler.Validator) (<-chan *document.Document, error) {
	ch := make(chan *document.Document)
	go func() {
		defer close(ch)
		for _, d := range f.docs {
			select {
			case ch <- d:
			case <-ctx.Done():
				return
			}
		}
		if f.wait {
			<-ctx.Done()
		}
	}()
	return ch, nil
}
func (f *siteTestCrawler) Close() error { f.closed = true; return nil }

func TestSiteCrawlIndexesForOwnerAndKeepsExisting(t *testing.T) {
	cfg := testutil.Config(t)
	idx := newServerTestIndexer(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	job := &siteCrawl{State: "running", userID: 42, cancel: cancel}
	s := &siteCrawls{job: job}
	skip := &config.Rule{ReStrs: []string{`/skip$`}}
	if err := skip.Compile(); err != nil {
		t.Fatal(err)
	}
	f := &siteTestCrawler{docs: []*document.Document{
		{URL: "https://example.com/", HTML: "<html><title>Original</title><body>Some useful public text to index.</body></html>"},
		{URL: "https://example.com/", HTML: "<html><title>Replacement</title><body>Do not replace an existing document.</body></html>"},
		{URL: "https://example.com/skip", HTML: "<html><body>Skipped redirect destination.</body></html>"},
	}}
	s.wg.Add(1)
	s.run(ctx, job, f, nil, idx, skip)
	if job.Indexed != 1 || job.Skipped != 2 || job.State != "finished" || job.Error != "" {
		t.Fatalf("status: %+v", job)
	}
	d := idx.GetByURLAndUser("https://example.com/", 42)
	if d == nil || d.Title != "Original" {
		t.Fatalf("indexed document: %+v", d)
	}
	if idx.GetByURLAndUser("https://example.com/", 7) != nil {
		t.Fatal("document visible under another owner")
	}
	if !f.closed {
		t.Fatal("crawler not closed")
	}
}

func TestSiteCrawlShutdown(t *testing.T) {
	cfg := testutil.Config(t)
	idx := newServerTestIndexer(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	job := &siteCrawl{State: "running", cancel: cancel}
	s := &siteCrawls{job: job}
	f := &siteTestCrawler{wait: true}
	s.wg.Add(1)
	go s.run(ctx, job, f, nil, idx, &config.Rule{})
	s.close()
	if !f.closed || job.State != "stopped" {
		t.Fatalf("shutdown did not stop worker: %+v", job)
	}
}

func TestSiteCrawlStartAndConflict(t *testing.T) {
	cfg, handler := newPublicTokenTestServer(t)
	h := handler.(*crawlHandler)
	t.Cleanup(h.crawls.close)
	cfg.Rules.Skip = &config.Rule{ReStrs: []string{".*"}} // No network in this test.
	headers := map[string]string{"X-Access-Token": "secret", "Origin": chromeExtensionOrigin, "Content-Type": "application/json"}
	h.crawls.job = &siteCrawl{State: "running", cancel: func() {}}
	res := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/crawl", strings.NewReader(`{"url":"http://127.0.0.1/path?query=1"}`), headers)
	if res.Code != http.StatusConflict {
		t.Fatalf("concurrent start: %d", res.Code)
	}
	h.crawls.job = nil
	res = testutil.ServeHTTP(t, handler, http.MethodPost, "/api/crawl", strings.NewReader(`{"url":"http://127.0.0.1/path?query=1"}`), headers)
	if res.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", res.Code, res.Body.String())
	}
	h.crawls.wg.Wait()
	res = testutil.ServeHTTP(t, handler, http.MethodGet, "/api/crawl", nil, headers)
	var job siteCrawl
	if err := json.Unmarshal(res.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.URL != "http://127.0.0.1/" || job.MaxPages != 100 || job.State != "finished" {
		t.Fatalf("status: %+v", job)
	}
}
