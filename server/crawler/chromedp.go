// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/asciimoo/hister/config"
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

func (f *chromedpFetcher) fetchPage(ctx context.Context, rawURL string, _ RequestHints) (string, []byte, []Link, FetchMeta, error) {
	taskCtx, taskCancel := chromedp.NewContext(f.allocCtx)
	defer taskCancel()

	timeoutCtx, timeoutCancel := context.WithTimeout(taskCtx, f.timeout)
	defer timeoutCancel()

	// Also honour the caller's context for graceful shutdown.
	go func() {
		select {
		case <-ctx.Done():
			timeoutCancel()
		case <-timeoutCtx.Done():
		}
	}()

	var actions []chromedp.Action

	if len(f.headers) > 0 {
		h := make(network.Headers, len(f.headers))
		for k, v := range f.headers {
			h[k] = v
		}
		actions = append(actions, network.SetExtraHTTPHeaders(h))
	}

	if len(f.cookies) > 0 {
		actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
			for _, ck := range f.cookies {
				cookiePath := ck.Path
				if cookiePath == "" {
					cookiePath = "/"
				}
				expr := cdp.TimeSinceEpoch(time.Now().Add(24 * time.Hour))
				if err := network.SetCookie(ck.Name, ck.Value).
					WithDomain(ck.Domain).
					WithPath(cookiePath).
					WithExpires(&expr).
					Do(ctx); err != nil {
					return err
				}
			}
			return nil
		}))
	}

	var htmlContent string
	var linkData []string
	var finalURL string

	actions = append(
		actions,
		chromedp.Navigate(rawURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
	)
	if f.captureDelay > 0 {
		actions = append(actions, chromedp.Sleep(f.captureDelay))
	}
	actions = append(
		actions,
		chromedp.Location(&finalURL),
		chromedp.OuterHTML("html", &htmlContent, chromedp.ByQuery),
		chromedp.Evaluate(
			`Array.from(document.querySelectorAll('a[href]')).map(a => ({href: a.getAttribute('href'), rel: a.getAttribute('rel') || ''}))`,
			&linkData,
		),
	)

	if err := chromedp.Run(timeoutCtx, actions...); err != nil {
		return "", nil, nil, FetchMeta{}, err
	}

	if finalURL == "" {
		finalURL = rawURL
	}

	// Parse the JS result into []Link. The Evaluate above uses a struct-like
	// object but chromedp decodes it as a JSON array into []string when using
	// a string slice target. Use a map slice target instead.
	links := jsObjectsToLinks(linkData)

	return finalURL, []byte(htmlContent), links, FetchMeta{}, nil
}

// jsObjectsToLinks is a best-effort parser for the chromedp JS result.
// The evaluate expression returns [{href:"...", rel:"..."}] serialised as JSON.
// chromedp may decode each element as a string representation; we handle both.
func jsObjectsToLinks(raw []string) []Link {
	// chromedp with a []string target will return JSON string representations
	// of each object. Fall back to href-only extraction.
	links := make([]Link, 0, len(raw))
	for _, s := range raw {
		// Each element is something like: map[href:URL rel:nofollow]
		// or the string form. Since we can't easily parse, just use as href.
		if s != "" {
			links = append(links, Link{Href: s})
		}
	}
	return links
}

func (f *chromedpFetcher) close() error {
	f.allocCancel()
	return nil
}
