// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
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
	maxBytes  int64
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

	ua := cfg.UserAgent

	maxBytes := cfg.Limits.MaxResponseBytes
	if maxBytes == 0 {
		maxBytes = 10 * 1024 * 1024
	}

	return &httpFetcher{
		client: &http.Client{
			Timeout:   timeout,
			Jar:       jar,
			Transport: transportWithProxy(proxyURL),
		},
		userAgent: ua,
		headers:   cfg.Headers,
		maxBytes:  maxBytes,
	}, nil
}

func (f *httpFetcher) fetchPage(ctx context.Context, rawURL string) (string, []byte, []Link, FetchMeta, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", nil, nil, FetchMeta{}, err
	}

	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")

	if f.userAgent != "" {
		req.Header.Set("User-Agent", f.userAgent)
	}
	for k, v := range f.headers {
		req.Header.Set(k, v)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return "", nil, nil, FetchMeta{}, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warn().Err(err).Msg("crawler: failed to close response body")
		}
	}()

	meta := FetchMeta{
		StatusCode: resp.StatusCode,
	}

	if resp.StatusCode != http.StatusOK {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		meta.RetryAfter = retryAfter
		return "", nil, nil, meta, &HTTPStatusError{Status: resp.StatusCode, RetryAfter: retryAfter}
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "html") {
		return "", nil, nil, meta, fmt.Errorf("not an HTML response: %s", ct)
	}

	// Check Content-Length before reading.
	if cl := resp.ContentLength; cl > 0 && cl > f.maxBytes {
		return "", nil, nil, meta, errResponseTooLarge
	}

	limitedBody := io.LimitReader(resp.Body, f.maxBytes+1)
	body, err := io.ReadAll(limitedBody)
	if err != nil {
		return "", nil, nil, meta, err
	}
	if int64(len(body)) > f.maxBytes {
		return "", nil, nil, meta, errResponseTooLarge
	}

	finalURL := resp.Request.URL.String()
	links, _ := extractLinks(bytes.NewReader(body))
	return finalURL, body, links, meta, nil
}

func (f *httpFetcher) close() error { return nil }

// parseRetryAfter parses the Retry-After header value as either a
// delta-seconds integer or an HTTP-date string.
func parseRetryAfter(s string) time.Duration {
	if s == "" {
		return 0
	}
	// Try delta-seconds first.
	if n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	// Try HTTP-date.
	for _, layout := range []string{
		time.RFC1123,
		"Monday, 02-Jan-06 15:04:05 MST",
		time.RFC850,
		time.ANSIC,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			d := time.Until(t)
			if d > 0 {
				return d
			}
			return 0
		}
	}
	return 0
}
