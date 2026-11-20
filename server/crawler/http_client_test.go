// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/nogon/config"
)

type httpTestTransport func(*http.Request) (*http.Response, error)

func (f httpTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type trackedResponseBody struct {
	io.Reader
	read   int
	closed bool
}

func (b *trackedResponseBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += n
	return n, err
}

func (b *trackedResponseBody) Close() error {
	b.closed = true
	return nil
}

func TestCrawlerStartsWithSavedOptionsForOtherBackends(t *testing.T) {
	for _, backend := range []string{"http", "chromedp"} {
		cfg := &config.CrawlerConfig{Backend: backend, BackendOptions: map[string]any{
			"port": 9223, "capture_delay": 0.25, "socket": "ws://localhost:9223/session",
		}}
		cr, err := New(cfg, nil)
		if err != nil {
			t.Fatalf("start %s crawler: %v", backend, err)
		}
		_ = cr.Close()
		persistent, err := NewPersistent(cfg, "saved-options", nil)
		if err != nil {
			t.Fatalf("start persistent %s crawler: %v", backend, err)
		}
		_ = persistent.Close()
		if len(cfg.BackendOptions) != 3 {
			t.Fatalf("starting %s discarded saved options", backend)
		}
		cfg.BackendOptions["typo"] = true
		if _, err := New(cfg, nil); err == nil {
			t.Fatalf("%s accepted an unknown backend option", backend)
		}
	}
}

func TestHTTPFetcherBoundsBodyReads(t *testing.T) {
	const limit = 32 << 20
	const lastLink = `<a href="/last">Last link</a>`
	html := strings.Repeat(" ", limit-len(lastLink)) + lastLink
	for _, tc := range []struct {
		name          string
		body          string
		contentLength int64
		wantRead      int
		wantErr       bool
	}{
		{name: "exact limit", body: html, contentLength: limit, wantRead: limit},
		{name: "streamed overflow", body: html + strings.Repeat("x", 8192), contentLength: -1, wantRead: limit + 1, wantErr: true},
		{name: "declared overflow", body: html + "x", contentLength: limit + 1, wantRead: 0, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedResponseBody{Reader: strings.NewReader(tc.body)}
			fetcher, err := newHTTPFetcher(&config.CrawlerConfig{})
			if err != nil {
				t.Fatal(err)
			}
			fetcher.client.Transport = httpTestTransport(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode:    http.StatusOK,
					Header:        http.Header{"Content-Type": {"text/html"}},
					Body:          body,
					ContentLength: tc.contentLength,
					Request:       req,
				}, nil
			})
			const url = "https://example.com/page"
			finalURL, content, links, err := fetcher.fetchPage(t.Context(), url)
			if body.read != tc.wantRead || !body.closed {
				t.Errorf("response read %d bytes and closed=%v, want %d bytes and closed", body.read, body.closed, tc.wantRead)
			}
			if tc.wantErr {
				if err == nil || finalURL != "" || content != "" || len(links) != 0 {
					t.Fatalf("oversized response was not rejected: err=%v, URL=%q, bytes=%d, links=%d", err, finalURL, len(content), len(links))
				}
				return
			}
			if err != nil || finalURL != url || content != html || !slices.Equal(links, []string{"/last"}) {
				t.Fatalf("HTML at the limit was rejected or truncated: err=%v, URL=%q, bytes=%d, links=%v", err, finalURL, len(content), links)
			}
		})
	}
}

func TestHTTPFetcherRejectsOversizedHTML(t *testing.T) {
	// The limit applies to decoded HTML, including bodies without Content-Length.
	body := bytes.Repeat([]byte("x"), (32<<20)+1)
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		data := body
		switch r.URL.Path {
		case "/known":
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		case "/chunked":
			w.(http.Flusher).Flush()
		case "/gzip":
			data = compressed.Bytes()
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		}
		// Closing an oversized response can interrupt the server's write.
		if _, err := w.Write(data); err != nil {
			return
		}
	}))
	defer server.Close()
	for _, path := range []string{"/known", "/chunked", "/gzip"} {
		t.Run(path, func(t *testing.T) {
			fetcher, err := newHTTPFetcher(&config.CrawlerConfig{})
			if err != nil {
				t.Fatal(err)
			}
			defer fetcher.client.CloseIdleConnections()
			finalURL, content, links, err := fetcher.fetchPage(t.Context(), server.URL+path)
			if err == nil || !strings.Contains(err.Error(), "32 MiB") {
				t.Errorf("oversized HTML error = %v, want size limit error", err)
			}
			if finalURL != "" || content != "" || len(links) != 0 {
				t.Errorf("oversized response returned partial document: URL=%q, bytes=%d, links=%d", finalURL, len(content), len(links))
			}
		})
	}
}

func TestRobotsCacheBoundsDownloads(t *testing.T) {
	const limit = 8 * 1024 * 1024
	const rules = "User-agent: *\nDisallow: /private\nAllow: /public\n"
	content := rules + "#" + strings.Repeat("x", limit-len(rules)-2) + "\n"
	for _, tc := range []struct {
		name          string
		body          string
		contentLength int64
		status        int
		wantRead      int
		allowPublic   bool
		allowPrivate  bool
	}{
		{name: "exact limit", body: content, contentLength: limit, status: http.StatusOK, wantRead: limit, allowPublic: true},
		{name: "streamed overflow", body: content + strings.Repeat("# extra\n", 1024), contentLength: -1, status: http.StatusOK, wantRead: limit + 1},
		{name: "declared overflow", body: content + "#", contentLength: limit + 1, status: http.StatusOK},
		{name: "missing robots", body: content + "#", contentLength: limit + 1, status: http.StatusNotFound, allowPublic: true, allowPrivate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedResponseBody{Reader: strings.NewReader(tc.body)}
			requests := 0
			cache := NewRobotsCache("Nogon")
			cache.client.Transport = httpTestTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.Path != "/robots.txt" || req.Header.Get("User-Agent") != "Nogon" {
					t.Errorf("unexpected robots request: %s, agent=%q", req.URL, req.Header.Get("User-Agent"))
				}
				return &http.Response{
					StatusCode:    tc.status,
					Header:        http.Header{"Content-Type": {"text/plain"}},
					Body:          body,
					ContentLength: tc.contentLength,
					Request:       req,
				}, nil
			})
			for _, check := range []struct {
				path string
				want bool
			}{{"/public", tc.allowPublic}, {"/private", tc.allowPrivate}, {"/public", tc.allowPublic}} {
				if got := cache.Allowed(t.Context(), "https://example.com"+check.path); got != check.want {
					t.Errorf("Allowed(%q) = %v, want %v", check.path, got, check.want)
				}
			}
			if body.read != tc.wantRead || !body.closed {
				t.Errorf("robots response read %d bytes and closed=%v, want %d bytes and closed", body.read, body.closed, tc.wantRead)
			}
			if requests != 1 {
				t.Errorf("robots requests = %d, want cached result", requests)
			}
		})
	}
}

func TestRobotsCacheLimitsDecompressedResponse(t *testing.T) {
	const limit = 8 * 1024 * 1024
	const rules = "User-agent: *\nAllow: /\n#"
	content := rules + strings.Repeat("x", limit+1-len(rules))
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := io.WriteString(zw, content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", strconv.Itoa(compressed.Len()))
		if _, err := w.Write(compressed.Bytes()); err != nil {
			return
		}
	}))
	defer server.Close()
	cache := NewRobotsCache("Nogon")
	defer cache.client.CloseIdleConnections()
	if cache.Allowed(t.Context(), server.URL+"/public") {
		t.Fatal("oversized decompressed robots.txt allowed crawling")
	}
}

func TestHTTPClientRequestSettings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "custom agent" || r.Header.Get("X-Test") != "import" {
			t.Errorf("request headers = %v", r.Header)
		}
		cookie, err := r.Cookie("session")
		if r.URL.Path == "/private/page" {
			if err != nil || cookie.Value != "secret" {
				t.Errorf("missing configured cookie: %v", err)
			}
		} else if err == nil {
			t.Error("cookie sent outside its configured path")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewHTTPClient(&config.CrawlerConfig{
		Timeout:   2,
		UserAgent: "default agent",
		Headers:   map[string]string{"User-Agent": "custom agent", "X-Test": "import"},
		Cookies:   []config.CrawlerCookie{{Name: "session", Value: "secret", Domain: serverURL.Hostname(), Path: "/private"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	if client.Timeout != 2*time.Second {
		t.Fatalf("timeout = %s, want 2s", client.Timeout)
	}
	for _, path := range []string{"/private/page", "/public"} {
		resp, err := client.Get(t.Context(), server.URL+path)
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := io.Copy(io.Discard, resp.Body)
		closeErr := resp.Body.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.Get(ctx, server.URL); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request returned %v", err)
	}
}
