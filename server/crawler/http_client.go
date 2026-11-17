// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"github.com/asciimoo/nogon/config"
)

const defaultTimeout = 5 * time.Second

// HTTPClient applies configured headers and a user agent to GET requests.
type HTTPClient struct {
	*http.Client
	userAgent string
	headers   map[string]string
}

// NewHTTPClient configures HTTP transport, cookies, headers, and timeout for
// crawlers and importers. The caller handles backend options.
func NewHTTPClient(cfg *config.CrawlerConfig) (*HTTPClient, error) {
	proxyURL, err := parseProxyURL(cfg.Proxy)
	if err != nil {
		return nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	for _, ck := range cfg.Cookies {
		cookiePath := ck.Path
		if cookiePath == "" {
			cookiePath = "/"
		}
		u, err := url.Parse("https://" + ck.Domain)
		if err != nil {
			return nil, fmt.Errorf("invalid cookie domain %q: %w", ck.Domain, err)
		}
		jar.SetCookies(u, []*http.Cookie{{
			Name: ck.Name, Value: ck.Value, Domain: ck.Domain, Path: cookiePath,
		}})
	}
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = defaultTimeout
	}
	return &HTTPClient{
		Client: &http.Client{
			Timeout: timeout, Jar: jar, Transport: transportWithProxy(proxyURL),
		},
		userAgent: cfg.UserAgent,
		headers:   cfg.Headers,
	}, nil
}

// Get sends a GET request with the configured request headers and context.
// The caller must close the response body.
func (c *HTTPClient) Get(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	for name, value := range c.headers {
		req.Header.Set(name, value)
	}
	return c.Do(req)
}
