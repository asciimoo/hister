// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/asciimoo/nogon/config"
)

type chromedpFetcher struct {
	allocCtx     context.Context
	allocCancel  context.CancelFunc
	cookies      []config.CrawlerCookie
	headers      map[string]string
	timeout      time.Duration
	captureDelay time.Duration
}

func newChromedpFetcher(cfg *config.CrawlerConfig) (*chromedpFetcher, error) {
	knownOptions := map[string]struct{}{
		"exec_path":     {},
		"capture_delay": {},
	}
	for k := range cfg.BackendOptions {
		if _, ok := knownOptions[k]; !ok {
			return nil, fmt.Errorf("chromedp backend: unknown option %q", k)
		}
	}
	proxyURL, err := parseProxyURL(cfg.Proxy)
	if err != nil {
		return nil, err
	}

	opts := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoSandbox,
	)
	if proxyURL != nil {
		opts = append(opts, chromedp.ProxyServer(proxyURL.String()))
	}
	if execPath, ok := cfg.BackendOptions["exec_path"]; ok {
		s, ok := execPath.(string)
		if !ok {
			return nil, fmt.Errorf("chromedp option \"exec_path\" must be a string")
		}
		opts = append(opts, chromedp.ExecPath(s))
	}
	if cfg.UserAgent != "" {
		opts = append(opts, chromedp.UserAgent(cfg.UserAgent))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = defaultTimeout
	}
	var captureDelay time.Duration
	if value, ok := cfg.BackendOptions["capture_delay"]; ok {
		captureDelay, err = parseCaptureDelay(value)
		if err != nil {
			allocCancel()
			return nil, fmt.Errorf("chromedp backend: %w", err)
		}
	}

	return &chromedpFetcher{
		allocCtx:     allocCtx,
		allocCancel:  allocCancel,
		cookies:      cfg.Cookies,
		headers:      cfg.Headers,
		timeout:      timeout,
		captureDelay: captureDelay,
	}, nil
}

func (f *chromedpFetcher) fetchPage(ctx context.Context, rawURL string) (string, string, []string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", nil, err
	}

	taskCtx, taskCancel := chromedp.NewContext(f.allocCtx)
	defer taskCancel()

	timeoutCtx, timeoutCancel := context.WithTimeout(taskCtx, f.timeout)
	defer timeoutCancel()
	// Keep the allocator in the context while honoring the caller's cancellation.
	stopCancel := context.AfterFunc(ctx, timeoutCancel)
	defer stopCancel()

	finalURL, htmlContent, links, err := f.capturePage(timeoutCtx, rawURL)
	if err != nil && ctx.Err() != nil {
		return "", "", nil, ctx.Err()
	}
	return finalURL, htmlContent, links, err
}

func (f *chromedpFetcher) capturePage(ctx context.Context, rawURL string) (string, string, []string, error) {
	if len(f.headers) > 0 {
		h := make(network.Headers, len(f.headers))
		for k, v := range f.headers {
			h[k] = v
		}
		if _, err := chromedp.Call(ctx, network.SetExtraHTTPHeaders, network.SetExtraHTTPHeadersParams{Headers: h}); err != nil {
			return "", "", nil, err
		}
	}

	for _, ck := range f.cookies {
		cookiePath := ck.Path
		if cookiePath == "" {
			cookiePath = "/"
		}
		if _, err := chromedp.Call(ctx, network.SetCookie, network.SetCookieParams{
			Name:    ck.Name,
			Value:   ck.Value,
			Domain:  ck.Domain,
			Path:    cookiePath,
			Expires: cdp.TimeSinceEpoch(time.Now().Add(24 * time.Hour).Unix()),
		}); err != nil {
			return "", "", nil, err
		}
	}

	actions := []chromedp.Action[chromedp.Void]{
		chromedp.Navigate(rawURL),
		chromedp.WaitReady(chromedp.CSS("body")),
	}
	if f.captureDelay > 0 {
		actions = append(actions, chromedp.Sleep(f.captureDelay))
	}
	if err := chromedp.Do(ctx, actions...); err != nil {
		return "", "", nil, err
	}

	finalURL, err := chromedp.Run(ctx, chromedp.Location())
	if err != nil {
		return "", "", nil, err
	}
	htmlContent, err := chromedp.Run(ctx, chromedp.OuterHTML(chromedp.CSS("html")))
	if err != nil {
		return "", "", nil, err
	}
	linkHrefs, err := chromedp.Run(ctx, chromedp.Evaluate[[]string](
		`Array.from(document.querySelectorAll('a[href]')).map(a => a.getAttribute('href'))`,
	))
	if err != nil {
		return "", "", nil, err
	}

	if finalURL == "" {
		finalURL = rawURL
	}
	return finalURL, htmlContent, linkHrefs, nil
}

func (f *chromedpFetcher) close() error {
	f.allocCancel()
	return nil
}
