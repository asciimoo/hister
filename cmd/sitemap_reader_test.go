// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/crawler"
)

func gzipSitemap(t *testing.T, content string) []byte {
	t.Helper()
	var output bytes.Buffer
	w := gzip.NewWriter(&output)
	if _, err := io.WriteString(w, content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestParseSitemap(t *testing.T) {
	for _, tc := range []struct {
		name, xml string
		index     bool
		urls      []string
	}{
		{
			name: "namespaces and extension URLs",
			xml: `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">
			<url><loc> https://example.com/page?a=1&amp;b=2#heading </loc><lastmod>2026-01-01</lastmod>
			<image:image><image:loc>https://example.com/photo.png</image:loc></image:image>
			<image:loc>https://example.com/ignored.png</image:loc></url></urlset>`,
			urls: []string{"https://example.com/page?a=1&b=2"},
		},
		{
			name: "without namespace",
			xml:  `<urlset><url><loc>https://example.com/one</loc></url><url><loc><![CDATA[https://example.com/two?a=1&b=2]]></loc></url></urlset>`,
			urls: []string{"https://example.com/one", "https://example.com/two?a=1&b=2"},
		},
		{
			name:  "prefixed index",
			xml:   `<?xml version="1.0"?><s:sitemapindex xmlns:s="http://www.sitemaps.org/schemas/sitemap/0.9"><s:sitemap><s:loc>https://example.com/map.xml.gz</s:loc></s:sitemap></s:sitemapindex><!--end-->`,
			index: true,
			urls:  []string{"https://example.com/map.xml.gz"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, compressed := range []bool{false, true} {
				input := []byte(tc.xml)
				if compressed {
					input = gzipSitemap(t, tc.xml)
				}
				got, err := parseSitemap(bytes.NewReader(input))
				if err != nil {
					t.Fatal(err)
				}
				if got.index != tc.index || !slices.Equal(got.locations, tc.urls) {
					t.Fatalf("parseSitemap() = %#v, want index %v and URLs %v", got, tc.index, tc.urls)
				}
			}
		})
	}
}

func TestParseSitemapRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{
		"", "not XML", `<html><body>Error</body></html>`,
		`<urlset xmlns="urn:unrelated"/>`,
		`<urlset><url><loc>https://example.com</loc></url>`,
		`<urlset/><urlset/>`, `<urlset/>trailing junk`,
		`<urlset><url/></urlset>`,
		`<urlset><url><loc/></url></urlset>`,
		`<urlset><url><loc>/relative</loc></url></urlset>`,
		`<urlset><url><loc>file:///etc/passwd</loc></url></urlset>`,
		`<urlset><url><loc>https://example.com/a b</loc></url></urlset>`,
		`<urlset><url><loc>https://example.com</loc><loc>https://example.org</loc></url></urlset>`,
		`<urlset><url xmlns:x="urn:extension"><x:loc>https://example.com</x:loc></url></urlset>`,
		`<sitemapindex><sitemap><loc>child.xml</loc></sitemap></sitemapindex>`,
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := parseSitemap(strings.NewReader(input)); err == nil {
				t.Fatal("invalid sitemap accepted")
			}
		})
	}
	compressed := gzipSitemap(t, `<urlset/>`)
	compressed[len(compressed)-8] ^= 0xff
	if _, err := parseSitemap(bytes.NewReader(compressed)); err == nil {
		t.Fatal("corrupt gzip checksum accepted")
	}
}

func TestParseSitemapLimits(t *testing.T) {
	oversize := strings.Repeat(" ", maxSitemapBytes+1)
	if _, err := parseSitemap(bytes.NewReader(gzipSitemap(t, oversize))); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversize compressed sitemap: %v", err)
	}
	tooMany := "<urlset>" + strings.Repeat("<url><loc>https://example.com/</loc></url>", maxSitemapEntries+1) + "</urlset>"
	if _, err := parseSitemap(strings.NewReader(tooMany)); err == nil || !strings.Contains(err.Error(), "entries") {
		t.Fatalf("too many entries: %v", err)
	}
}

func TestSitemapReaderExpandsIndexesAndDeduplicates(t *testing.T) {
	requests := make(map[string]int)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests[req.URL.Path]++
		if req.Header.Get("User-Agent") != "Nogon sitemap test" || req.Header.Get("X-Test") != "sitemap" {
			t.Error("crawler headers missing from sitemap request")
		}
		if cookie, err := req.Cookie("session"); err != nil || cookie.Value != "test" {
			t.Error("crawler cookie missing from sitemap request")
		}
		switch req.URL.Path {
		case "/index.xml":
			if _, err := fmt.Fprintf(w, `<sitemapindex><sitemap><loc>%s/one.xml.gz</loc></sitemap><sitemap><loc>%s/nested.xml</loc></sitemap><sitemap><loc>%s/one.xml.gz</loc></sitemap></sitemapindex>`, server.URL, server.URL, server.URL); err != nil {
				t.Error(err)
			}
		case "/nested.xml":
			if _, err := fmt.Fprintf(w, `<sitemapindex><sitemap><loc>%s/index.xml</loc></sitemap><sitemap><loc>%s/two.xml</loc></sitemap></sitemapindex>`, server.URL, server.URL); err != nil {
				t.Error(err)
			}
		case "/one.xml.gz":
			_, _ = w.Write(gzipSitemap(t, `<urlset><url><loc>https://example.com/one</loc></url><url><loc>https://example.com/one#fragment</loc></url></urlset>`))
		case "/two.xml":
			if _, err := fmt.Fprint(w, `<urlset><url><loc>https://example.com/one</loc></url><url><loc>https://example.com/two</loc></url></urlset>`); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected fetch: %s", req.URL)
		}
	}))
	defer server.Close()
	reader, err := newSitemapReader(&config.CrawlerConfig{
		UserAgent: "Nogon sitemap test",
		Headers:   map[string]string{"X-Test": "sitemap"},
		Cookies:   []config.CrawlerCookie{{Name: "session", Value: "test", Domain: "127.0.0.1", Path: "/"}},
		Backend:   "bidi", BackendOptions: map[string]any{"endpoint": "unused for XML"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := reader.Fetch(t.Context(), server.URL+"/index.xml")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://example.com/one", "https://example.com/two"}; !slices.Equal(got, want) {
		t.Fatalf("URLs = %v, want %v", got, want)
	}
	if len(requests) != 4 {
		t.Fatalf("requests = %v", requests)
	}
	for path, count := range requests {
		if count != 1 {
			t.Errorf("%s fetched %d times", path, count)
		}
	}
}

func TestSitemapReaderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			if _, err := fmt.Fprint(w, "User-agent: *\nDisallow: /blocked\n"); err != nil {
				t.Error(err)
			}
			return
		}
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer server.Close()
	reader, err := newSitemapReader(&config.CrawlerConfig{}, crawler.NewRobotsCache("Nogon"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, tc := range []struct{ path, want string }{{"/missing", "404"}, {"/blocked.xml", "robots.txt"}} {
		if _, err := reader.Fetch(t.Context(), server.URL+tc.path); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Fetch(%s): %v, want %s", tc.path, err, tc.want)
		}
	}
	if _, err := reader.Read(t.Context(), strings.NewReader(`<urlset/>`)); err == nil {
		t.Error("empty sitemap accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := reader.Fetch(ctx, server.URL+"/missing"); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled fetch: %v", err)
	}
}
