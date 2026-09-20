// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/config"
)

const defaultTimeout = 5 * time.Second

type httpFetcher struct {
	client    *http.Client
	userAgent string
	headers   map[string]string
}

func newHTTPFetcher(cfg *config.CrawlerConfig) (*httpFetcher, error) {
	for k := range cfg.BackendOptions {
		return nil, fmt.Errorf("http backend: unknown option %q", k)
	}
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
			Name:   ck.Name,
			Value:  ck.Value,
			Domain: ck.Domain,
			Path:   cookiePath,
		}})
	}

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = defaultTimeout
	}

	return &httpFetcher{
		client: &http.Client{
			Timeout:   timeout,
			Jar:       jar,
			Transport: transportWithProxy(proxyURL),
		},
		userAgent: cfg.UserAgent,
		headers:   cfg.Headers,
	}, nil
}

func (f *httpFetcher) fetchPage(ctx context.Context, rawURL string) (string, string, []string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", nil, err
	}

	if f.userAgent != "" {
		req.Header.Set("User-Agent", f.userAgent)
	}
	for k, v := range f.headers {
		req.Header.Set(k, v)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return "", "", nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warn().Err(err).Msg("crawler: failed to close response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", "", nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "html") {
		return "", "", nil, fmt.Errorf("not an HTML response: %s", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", nil, err
	}

	htmlContent := string(body)
	finalURL := resp.Request.URL.String()
	return finalURL, htmlContent, extractLinks(htmlContent), nil
}

func (f *httpFetcher) close() error { return nil }
