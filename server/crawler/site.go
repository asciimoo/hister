// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/asciimoo/hister/config"
)

// NewSite creates a bounded public-website crawl for use by the web server.
// It shares the CLI's traversal, HTTP parser and robots handling, but never
// uses configured cookies, headers, proxies or browser backends.
func NewSite(rawURL string, maxPages int, skip SkipURLChecker) (Crawler, *Validator, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid site URL")
	}
	if err = checkSiteURL(u, u.Hostname()); err != nil {
		return nil, nil, err
	}
	if maxPages < 1 || maxPages > 1000 {
		return nil, nil, fmt.Errorf("page limit must be between 1 and 1000")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialPublic
	client := &http.Client{Timeout: 10 * time.Second, Transport: &siteTransport{host: u.Hostname(), transport: transport}}
	cfg := &config.CrawlerConfig{Delay: 1, UserAgent: "Hister"}
	robots := NewRobotsCache(cfg.UserAgent)
	robots.client = client
	// Robots requests use the same guarded transport without recursively checking
	// their own redirect targets against robots.txt.
	pageClient := *client
	pageClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		if err := checkSiteURL(req.URL, u.Hostname()); err != nil {
			return err
		}
		if skip != nil {
			skipped, err := skip(req.URL.String())
			if err != nil {
				return err
			}
			if skipped {
				return fmt.Errorf("redirect excluded by skip rules")
			}
		}
		if !robots.Allowed(req.Context(), req.URL.String()) {
			return fmt.Errorf("redirect excluded by robots.txt")
		}
		return nil
	}
	hostPattern := regexp.QuoteMeta(u.Hostname())
	if strings.Contains(u.Hostname(), ":") {
		hostPattern = `\[` + hostPattern + `\]`
	}
	v, err := NewValidator(&ValidatorRules{
		MaxLinks:        maxPages,
		AllowedPatterns: []string{`(?i)^https?://` + hostPattern + `(?::(?:80|443))?(?:/|$)`},
	})
	if err != nil {
		return nil, nil, err
	}
	return &siteCrawler{
		baseCrawler: &baseCrawler{
			fetcher: &httpFetcher{client: &pageClient, userAgent: cfg.UserAgent}, cfg: cfg,
			robots: robots, skipURLChecker: skip, maxQueue: 10000,
		},
		client: &pageClient,
	}, v, nil
}

// Keep error reporting and connection cleanup specific to server-started crawls.
type siteCrawler struct {
	*baseCrawler
	client *http.Client
}

func (c *siteCrawler) Err() error { return c.err }

func (c *siteCrawler) Close() error {
	c.client.CloseIdleConnections()
	return c.baseCrawler.Close()
}

func checkSiteURL(u *url.URL, host string) error {
	if (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || !strings.EqualFold(u.Hostname(), host) {
		return fmt.Errorf("crawl URLs must use HTTP(S) on the selected hostname")
	}
	expectedPort := "80"
	if u.Scheme == "https" {
		expectedPort = "443"
	}
	if port := u.Port(); port != "" && port != expectedPort {
		return fmt.Errorf("site crawling only supports standard HTTP(S) ports")
	}
	return nil
}

// Resolve and dial the checked address, preventing a second DNS lookup from
// turning an initially public hostname into an internal request.
func dialPublic(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, fmt.Errorf("site crawling cannot access private or reserved addresses")
		}
	}
	var dialer net.Dialer
	for _, ip := range ips {
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
	}
	if err == nil {
		err = fmt.Errorf("site hostname has no addresses")
	}
	return nil, err
}

var reservedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	// Only currently allocated global IPv6 space; excludes NAT64/local mappings.
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range reservedNetworks {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

type siteTransport struct {
	host      string
	transport http.RoundTripper
}

// This check applies to every request, including redirects and robots.txt.
func (t *siteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := checkSiteURL(req.URL, t.host); err != nil {
		return nil, err
	}
	res, err := t.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	res.Body = &limitedSiteBody{ReadCloser: res.Body, remaining: 5 << 20}
	return res, nil
}

func (t *siteTransport) CloseIdleConnections() {
	if closer, ok := t.transport.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

type limitedSiteBody struct {
	io.ReadCloser
	remaining int64
}

func (b *limitedSiteBody) Read(p []byte) (int, error) {
	if b.remaining < 0 {
		return 0, fmt.Errorf("site response exceeds 5 MiB")
	}
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	if b.remaining < 0 {
		return 0, fmt.Errorf("site response exceeds 5 MiB")
	}
	return n, err
}
