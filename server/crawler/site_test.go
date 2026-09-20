package crawler

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/asciimoo/hister/config"
)

type siteRoundTripFunc func(*http.Request) (*http.Response, error)

func (f siteRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPublicSiteAddresses(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "172.16.1.1", "192.168.1.1", "169.254.169.254", "100.100.100.200", "0.0.0.0", "192.0.2.1", "198.18.1.1", "224.0.0.1", "240.1.1.1", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "64:ff9b::7f00:1", "2002:7f00:1::", "2001:db8::1"} {
		if publicIP(netip.MustParseAddr(address)) {
			t.Errorf("allowed reserved address %s", address)
		}
	}
	for _, address := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(address)) {
			t.Errorf("blocked public address %s", address)
		}
	}
	if conn, err := dialPublic(context.Background(), "tcp", "127.0.0.1:80"); err == nil {
		_ = conn.Close()
		t.Fatal("dialed private address")
	}
}

func TestSiteRequestScope(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "http://user:pass@example.com/", "https://other.example/", "https://example.com.evil/", "https://example.com:8443/", "http://example.com:443/"} {
		u, _ := url.Parse(raw)
		if err := checkSiteURL(u, "example.com"); err == nil {
			t.Errorf("allowed %s", raw)
		}
	}
	for _, limit := range []int{0, -1, 1001} {
		if _, _, err := NewSite("https://example.com/", limit, nil); err == nil {
			t.Errorf("allowed limit %d", limit)
		}
	}
}

func TestSiteCrawlTraversal(t *testing.T) {
	cr, v, err := NewSite("https://example.com/", 4, func(u string) (bool, error) { return strings.HasSuffix(u, "/skip"), nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cr.Close() }()
	base := cr.(*siteCrawler).baseCrawler
	base.cfg.Delay = 0
	visited := map[string]int{}
	base.fetcher.(*httpFetcher).client.Transport.(*siteTransport).transport = siteRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		visited[req.URL.String()]++
		body := `<a href="/a">A</a><a href="/a#fragment">duplicate</a><a href="https://other.example/">other</a><a href="/blocked">robots</a><a href="/skip">skip</a><a href="/b">limit</a>`
		if req.URL.Path == "/robots.txt" {
			body = "User-agent: *\nDisallow: /blocked\n"
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	docs, err := cr.Crawl(context.Background(), "https://example.com/", v)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for range docs {
		count++
	}
	if count != 2 {
		t.Fatalf("got %d documents, want root and /a", count)
	}
	for _, forbidden := range []string{"https://other.example/", "https://example.com/blocked", "https://example.com/skip", "https://example.com/b"} {
		if visited[forbidden] != 0 {
			t.Errorf("fetched %s", forbidden)
		}
	}
	if visited["https://example.com/a"] != 1 || visited["https://example.com/robots.txt"] != 1 {
		t.Fatalf("requests: %v", visited)
	}
}

func TestSiteRedirectBlocked(t *testing.T) {
	for _, target := range []string{"http://127.0.0.1/", "https://other.example/", "https://example.com:8443/"} {
		t.Run(target, func(t *testing.T) {
			requests := 0
			client := &http.Client{Transport: &siteTransport{host: "example.com", transport: siteRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{StatusCode: 302, Header: http.Header{"Location": {target}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
			})}}
			res, err := client.Get("https://example.com/")
			if res != nil {
				_ = res.Body.Close()
			}
			if err == nil || requests != 1 {
				t.Fatalf("err=%v requests=%d", err, requests)
			}
		})
	}
}

func TestSiteResponseLimit(t *testing.T) {
	body := &limitedSiteBody{ReadCloser: io.NopCloser(strings.NewReader("12345")), remaining: 4}
	if _, err := io.ReadAll(body); err == nil {
		t.Fatal("accepted oversized body")
	}
	body = &limitedSiteBody{ReadCloser: io.NopCloser(strings.NewReader("1234")), remaining: 4}
	if got, err := io.ReadAll(body); err != nil || string(got) != "1234" {
		t.Fatalf("body=%q err=%v", got, err)
	}
}

func TestSiteRedirectRespectsRules(t *testing.T) {
	for _, target := range []string{"/skip", "/blocked"} {
		t.Run(target, func(t *testing.T) {
			cr, v, err := NewSite("https://example.com/", 2, func(u string) (bool, error) { return strings.HasSuffix(u, "/skip"), nil })
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cr.Close() }()
			base := cr.(*siteCrawler).baseCrawler
			base.cfg.Delay = 0
			base.fetcher.(*httpFetcher).client.Transport.(*siteTransport).transport = siteRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == target {
					t.Errorf("fetched excluded redirect %s", target)
				}
				res := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Request: req, Body: io.NopCloser(strings.NewReader("User-agent: *\nDisallow: /blocked\n"))}
				if req.URL.Path == "/" {
					res.StatusCode = 302
					res.Header.Set("Location", target)
				}
				return res, nil
			})
			docs, err := cr.Crawl(context.Background(), "https://example.com/", v)
			if err != nil {
				t.Fatal(err)
			}
			for range docs {
				t.Error("indexed excluded redirect")
			}
			if cr.(ErrorReporter).Err() == nil {
				t.Fatal("missing redirect failure")
			}
		})
	}
}

type siteCloseTrackingTransport struct {
	siteRoundTripFunc
	closes int
}

func (t *siteCloseTrackingTransport) CloseIdleConnections() { t.closes++ }

func TestSiteLifecycleDoesNotChangeRegularCrawlers(t *testing.T) {
	for _, site := range []bool{false, true} {
		t.Run(map[bool]string{false: "regular", true: "site"}[site], func(t *testing.T) {
			var cr Crawler
			var v *Validator
			var err error
			var base *baseCrawler
			if site {
				cr, v, err = NewSite("https://example.com/", 1, nil)
				if err != nil {
					t.Fatal(err)
				}
				base = cr.(*siteCrawler).baseCrawler
				base.cfg.Delay = 0
				base.robots = nil
			} else {
				cr, err = New(&config.CrawlerConfig{}, nil)
				if err != nil {
					t.Fatal(err)
				}
				base = cr.(*baseCrawler)
				v, err = NewValidator(&ValidatorRules{MaxLinks: 1})
				if err != nil {
					t.Fatal(err)
				}
			}
			transport := &siteCloseTrackingTransport{siteRoundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
			}}
			base.fetcher.(*httpFetcher).client.Transport = transport
			docs, err := cr.Crawl(context.Background(), "https://example.com/", v)
			if err != nil {
				t.Fatal(err)
			}
			for range docs {
				t.Error("unexpected document for failed fetch")
			}
			reporter, reportsErrors := cr.(ErrorReporter)
			if reportsErrors != site {
				t.Fatalf("ErrorReporter exposed = %v, want %v", reportsErrors, site)
			}
			if reportsErrors && reporter.Err() == nil {
				t.Error("missing site fetch error")
			}
			if err := cr.Close(); err != nil {
				t.Fatal(err)
			}
			wantCloses := 0
			if site {
				wantCloses = 1
			}
			if transport.closes != wantCloses {
				t.Errorf("idle connection cleanup calls = %d, want %d", transport.closes, wantCloses)
			}
		})
	}
}
