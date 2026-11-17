// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/nogon/config"
)

// Match the extracted content allowance while bounding downloads before parsing.
const maxHTMLResponseSize = 32 * 1024 * 1024 // 32 MiB in bytes.

type httpFetcher struct {
	client *HTTPClient
}

func newHTTPFetcher(cfg *config.CrawlerConfig) (*httpFetcher, error) {
	for k := range cfg.BackendOptions {
		return nil, fmt.Errorf("http backend: unknown option %q", k)
	}
	client, err := NewHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	return &httpFetcher{client: client}, nil
}

func (f *httpFetcher) fetchPage(ctx context.Context, rawURL string) (string, string, []string, error) {
	resp, err := f.client.Get(ctx, rawURL)
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

	if resp.ContentLength > maxHTMLResponseSize {
		return "", "", nil, fmt.Errorf("HTML response exceeds the %d MiB limit", maxHTMLResponseSize>>20)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTMLResponseSize+1))
	if err != nil {
		return "", "", nil, err
	}
	if len(body) > maxHTMLResponseSize {
		return "", "", nil, fmt.Errorf("HTML response exceeds the %d MiB limit", maxHTMLResponseSize>>20)
	}

	htmlContent := string(body)
	finalURL := resp.Request.URL.String()
	return finalURL, htmlContent, extractLinks(htmlContent), nil
}

func (f *httpFetcher) close() error { return nil }
