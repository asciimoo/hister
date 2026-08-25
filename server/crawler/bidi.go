// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/config"
)

// bidiFetcher implements the fetcher interface using the W3C WebDriver BiDi
// protocol. It talks directly to the browser over a WebSocket — no external
// driver binary or library needed.
type bidiFetcher struct {
	conn         *websocket.Conn
	timeout      time.Duration
	captureDelay time.Duration // extra wait after navigation before capturing HTML

	// Command-response bookkeeping.
	nextID  atomic.Uint64
	pending sync.Map // id → chan bidiResult

	// Set after session.new succeeds; used to send session.end on close.
	ownsSession bool

	// Closed when the reader goroutine exits.
	done chan struct{}
}

type bidiResult struct {
	data json.RawMessage
	err  error
}

// newBidiFetcher creates a bidiFetcher that connects to the browser's
// WebDriver BiDi WebSocket endpoint.
//
// Supported backend_options:
//   - "socket" (string): full WebSocket URL, e.g. "ws://127.0.0.1:9222/session"
//   - "host" (string): hostname or IP, defaults to "127.0.0.1"
//   - "port" (string|int): port number, defaults to "9222"
//   - "capture_delay" (float): seconds to wait after page load before capturing HTML (default: 0)
//
// When "socket" is provided, "host" and "port" are ignored.
func newBidiFetcher(cfg *config.CrawlerConfig) (*bidiFetcher, error) {
	knownOptions := map[string]struct{}{
		"socket":        {},
		"host":          {},
		"port":          {},
		"capture_delay": {},
	}
	for k := range cfg.BackendOptions {
		if _, ok := knownOptions[k]; !ok {
			return nil, fmt.Errorf("bidi backend: unknown option %q", k)
		}
	}
	proxyURL, err := parseProxyURL(cfg.Proxy)
	if err != nil {
		return nil, err
	}

	wsURL, err := buildBidiWSURL(cfg.BackendOptions)
	if err != nil {
		return nil, fmt.Errorf("bidi backend: %w", err)
	}

	log.Debug().Str("ws_url", wsURL).Msg("crawler: connecting to BiDi browser")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("bidi backend: failed to connect to %s: %w", wsURL, err)
	}

	// The default read limit (32 KB) is far too small for BiDi responses
	// that contain full-page HTML.  Allow up to 100 MB.
	conn.SetReadLimit(100 * 1024 * 1024)

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = defaultTimeout
	}

	var captureDelay time.Duration
	if value, ok := cfg.BackendOptions["capture_delay"]; ok {
		captureDelay, err = parseCaptureDelay(value)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("bidi backend: %w", err)
		}
		if captureDelay > 0 {
			log.Debug().Dur("capture_delay", captureDelay).Msg("bidi: capture delay configured")
		}
	}

	f := &bidiFetcher{
		conn:         conn,
		timeout:      timeout,
		captureDelay: captureDelay,
		done:         make(chan struct{}),
	}

	go f.readLoop()

	// Establish a BiDi session (with a timeout so we never hang).
	sessionCtx, sessionCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sessionCancel()

	capabilities := map[string]any{}
	if proxyURL != nil {
		capabilities["alwaysMatch"] = map[string]any{
			"proxy": bidiProxyCapability(proxyURL),
		}
	}
	_, err = f.call(sessionCtx, "session.new", map[string]any{
		"capabilities": capabilities,
	})
	if err != nil {
		if proxyURL != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("cannot create a BiDi session with the configured proxy: %w", err)
		}
		// session.new may fail if the browser already has max sessions.
		// Continue without owning a session — commands may still work.
		log.Debug().Err(err).Msg("bidi: session.new failed, continuing without owning session")
	} else {
		f.ownsSession = true
	}

	return f, nil
}

// buildBidiWSURL constructs the WebSocket URL from backend_options.
func buildBidiWSURL(opts map[string]any) (string, error) {
	if socket, ok := opts["socket"]; ok {
		s, ok := socket.(string)
		if !ok {
			return "", fmt.Errorf("option \"socket\" must be a string")
		}
		u, err := url.Parse(s)
		if err != nil {
			return "", fmt.Errorf("invalid socket URL %q: %w", s, err)
		}
		if u.Scheme != "ws" && u.Scheme != "wss" {
			return "", fmt.Errorf("socket URL must use ws:// or wss:// scheme, got %q", u.Scheme)
		}
		return s, nil
	}

	host := "127.0.0.1"
	port := "9222"

	if h, ok := opts["host"]; ok {
		s, ok := h.(string)
		if !ok {
			return "", fmt.Errorf("option \"host\" must be a string")
		}
		host = s
	}
	if p, ok := opts["port"]; ok {
		switch v := p.(type) {
		case string:
			port = v
		case int:
			port = fmt.Sprintf("%d", v)
		case float64:
			port = fmt.Sprintf("%d", int(v))
		default:
			return "", fmt.Errorf("option \"port\" must be a string or int")
		}
	}

	return fmt.Sprintf("ws://%s:%s/session", host, port), nil
}

// call sends a BiDi command and waits for the response.
func (f *bidiFetcher) call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	id := f.nextID.Add(1)

	ch := make(chan bidiResult, 1)
	f.pending.Store(id, ch)
	defer f.pending.Delete(id)

	msg := map[string]any{
		"id":     id,
		"method": method,
		"params": params,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("bidi: marshal command: %w", err)
	}

	log.Trace().Str("method", method).Uint64("id", id).Msg("bidi: sending command")

	if err := f.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return nil, fmt.Errorf("bidi: write: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.done:
		return nil, fmt.Errorf("bidi: connection closed while waiting for response")
	case res := <-ch:
		return res.data, res.err
	}
}

// readLoop reads WebSocket messages and dispatches responses to pending callers.
func (f *bidiFetcher) readLoop() {
	defer close(f.done)
	for {
		_, data, err := f.conn.ReadMessage()
		if err != nil {
			// Notify all pending callers.
			f.pending.Range(func(key, val any) bool {
				ch := val.(chan bidiResult)
				select {
				case ch <- bidiResult{err: fmt.Errorf("bidi: read error: %w", err)}:
				default:
				}
				return true
			})
			return
		}

		// Parse the message to determine if it's a response or an event.
		// W3C WebDriver BiDi response format:
		//   success: {"type":"success","id":N,"result":{...}}
		//   error:   {"type":"error","id":N,"error":"code","message":"..."}
		//   event:   {"type":"event","method":"...","params":{...}}
		var envelope struct {
			Type    string          `json:"type"`
			ID      *uint64         `json:"id"`
			Error   string          `json:"error"`   // error code string
			Message string          `json:"message"` // error message
			Result  json.RawMessage `json:"result"`
			Method  string          `json:"method"` // present for events
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			log.Warn().Err(err).RawJSON("data", data).Msg("bidi: failed to parse message")
			continue
		}

		// Events (no ID) are logged and discarded.
		if envelope.ID == nil {
			log.Trace().Str("type", envelope.Type).Str("method", envelope.Method).Msg("bidi: received event")
			continue
		}

		val, ok := f.pending.Load(*envelope.ID)
		if !ok {
			log.Trace().Uint64("id", *envelope.ID).Msg("bidi: received response for unknown ID")
			continue
		}

		ch := val.(chan bidiResult)
		if envelope.Type == "error" || envelope.Error != "" {
			ch <- bidiResult{err: fmt.Errorf("bidi: %s: %s", envelope.Error, envelope.Message)}
		} else {
			ch <- bidiResult{data: envelope.Result}
		}
	}
}

func (f *bidiFetcher) fetchPage(ctx context.Context, rawURL string, _ RequestHints) (string, []byte, []Link, FetchMeta, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	// Create a new tab.
	bcData, err := f.call(timeoutCtx, "browsingContext.create", map[string]any{
		"type": "tab",
	})
	if err != nil {
		return "", nil, nil, FetchMeta{}, fmt.Errorf("bidi: failed to create tab: %w", err)
	}

	var bc struct {
		Context string `json:"context"`
	}
	if err := json.Unmarshal(bcData, &bc); err != nil {
		return "", nil, nil, FetchMeta{}, fmt.Errorf("bidi: failed to parse browsingContext.create result: %w", err)
	}

	contextID := bc.Context
	defer func() {
		if _, err := f.call(context.Background(), "browsingContext.close", map[string]any{
			"context": contextID,
		}); err != nil {
			log.Debug().Err(err).Str("context", contextID).Msg("bidi: failed to close tab")
		}
	}()

	// Navigate to the URL and wait for it to finish loading.
	log.Debug().Str("url", rawURL).Str("context", contextID).Msg("bidi: navigating")

	navData, err := f.call(timeoutCtx, "browsingContext.navigate", map[string]any{
		"context": contextID,
		"url":     rawURL,
		"wait":    "complete",
	})
	if err != nil {
		return "", nil, nil, FetchMeta{}, fmt.Errorf("bidi: navigation to %s failed: %w", rawURL, err)
	}

	var nav struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(navData, &nav); err != nil {
		log.Debug().Err(err).Msg("bidi: failed to parse navigation result")
	}
	finalURL := nav.URL
	if finalURL == "" {
		finalURL = rawURL
	}

	// Optional extra wait for slow-loading pages (JS rendering, etc.).
	if f.captureDelay > 0 {
		log.Debug().Dur("delay", f.captureDelay).Str("url", rawURL).Msg("bidi: waiting before capture")
		select {
		case <-time.After(f.captureDelay):
		case <-timeoutCtx.Done():
			return "", nil, nil, FetchMeta{}, fmt.Errorf("bidi: capture delay interrupted: %w", timeoutCtx.Err())
		}
	}

	// Extract the full page HTML using script.evaluate.
	htmlContent, err := f.evaluateString(timeoutCtx, contextID, `document.documentElement.outerHTML`)
	if err != nil {
		return "", nil, nil, FetchMeta{}, fmt.Errorf("bidi: failed to get page HTML from %s: %w", rawURL, err)
	}

	// Extract link hrefs and rel attributes.
	linkData, err := f.evaluateLinkArray(
		timeoutCtx, contextID,
		`Array.from(document.querySelectorAll('a[href]')).map(a => [a.getAttribute('href'), a.getAttribute('rel') || ''])`,
	)
	if err != nil {
		log.Debug().Err(err).Str("url", rawURL).Msg("bidi: failed to extract links")
		linkData = nil
	}

	return finalURL, []byte(htmlContent), linkData, FetchMeta{}, nil
}

// evaluateString runs a JS expression and returns the string result.
func (f *bidiFetcher) evaluateString(ctx context.Context, contextID, expression string) (string, error) {
	data, err := f.call(ctx, "script.evaluate", map[string]any{
		"expression":      expression,
		"target":          map[string]any{"context": contextID},
		"awaitPromise":    false,
		"resultOwnership": "none",
	})
	if err != nil {
		return "", err
	}

	var res struct {
		Type   string `json:"type"` // "success" or "exception"
		Result struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails,omitempty"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return "", fmt.Errorf("bidi: failed to parse script.evaluate result: %w", err)
	}
	if res.Type == "exception" {
		return "", fmt.Errorf("bidi: script exception: %s", string(data))
	}
	if res.Result.Type != "string" {
		return "", fmt.Errorf("bidi: expected string result, got %q", res.Result.Type)
	}
	return res.Result.Value, nil
}

// evaluateLinkArray runs a JS expression that returns [[href, rel], ...] and
// maps the results to []Link.
func (f *bidiFetcher) evaluateLinkArray(ctx context.Context, contextID, expression string) ([]Link, error) {
	data, err := f.call(ctx, "script.evaluate", map[string]any{
		"expression":      expression,
		"target":          map[string]any{"context": contextID},
		"awaitPromise":    false,
		"resultOwnership": "none",
	})
	if err != nil {
		return nil, err
	}

	var res struct {
		Type   string `json:"type"`
		Result struct {
			Type  string `json:"type"`
			Value []struct {
				Type  string `json:"type"`
				Value []struct {
					Type  string `json:"type"`
					Value string `json:"value"`
				} `json:"value"`
			} `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("bidi: failed to parse link array result: %w", err)
	}
	if res.Type == "exception" {
		return nil, fmt.Errorf("bidi: script exception: %s", string(data))
	}
	if res.Result.Type != "array" {
		return nil, fmt.Errorf("bidi: expected array result, got %q", res.Result.Type)
	}

	links := make([]Link, 0, len(res.Result.Value))
	for _, item := range res.Result.Value {
		if item.Type != "array" || len(item.Value) < 2 {
			continue
		}
		href := item.Value[0].Value
		rel := item.Value[1].Value
		if href != "" {
			links = append(links, Link{Href: href, Rel: rel})
		}
	}
	return links, nil
}

func (f *bidiFetcher) close() error {
	if f.ownsSession {
		_, _ = f.call(context.Background(), "session.end", map[string]any{})
	}
	return f.conn.Close()
}
