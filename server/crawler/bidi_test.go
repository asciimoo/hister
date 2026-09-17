// SPDX-License-Identifier: AGPL-3.0-or-later

package crawler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/asciimoo/hister/config"
)

const (
	testBidiContext    = "ctx-1"
	testBidiNavigation = "nav-1"
	testBidiHTML       = "<html><body><a href=\"/next\">next</a></body></html>"
)

// fakeBidiServer is a minimal WebDriver BiDi remote end. It answers the
// commands bidiFetcher sends and emits the configured network events while
// handling browsingContext.navigate, which is when a real browser reports them.
type fakeBidiServer struct {
	*httptest.Server

	// navEvents returns the network events to emit before the navigation
	// result is sent back.
	navEvents func() []map[string]any

	// subscriptionID is returned by session.subscribe. An empty value mimics
	// browsers that do not report subscription ids yet.
	subscriptionID string

	// failSubscribe makes session.subscribe return a protocol error.
	failSubscribe bool
}

func newFakeBidiServer(t *testing.T, s *fakeBidiServer) *fakeBidiServer {
	t.Helper()
	upgrader := websocket.Upgrader{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		s.serve(conn)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *fakeBidiServer) wsURL() string {
	return "ws" + strings.TrimPrefix(s.URL, "http") + "/session"
}

func (s *fakeBidiServer) serve(conn *websocket.Conn) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var cmd struct {
			ID     uint64         `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(data, &cmd); err != nil {
			return
		}

		if cmd.Method == "browsingContext.navigate" {
			for _, ev := range s.eventsForNavigation() {
				if err := conn.WriteJSON(ev); err != nil {
					return
				}
			}
		}

		if cmd.Method == "session.subscribe" && s.failSubscribe {
			err = conn.WriteJSON(map[string]any{
				"type": "error", "id": cmd.ID,
				"error": "invalid argument", "message": "no such event",
			})
		} else {
			err = conn.WriteJSON(map[string]any{
				"type": "success", "id": cmd.ID, "result": s.resultFor(cmd.Method, cmd.Params),
			})
		}
		if err != nil {
			return
		}
	}
}

func (s *fakeBidiServer) eventsForNavigation() []map[string]any {
	if s.navEvents == nil {
		return []map[string]any{bidiResponseEvent(testBidiContext, testBidiNavigation, 200)}
	}
	return s.navEvents()
}

func (s *fakeBidiServer) resultFor(method string, params map[string]any) map[string]any {
	switch method {
	case "session.new":
		return map[string]any{"sessionId": "session-1"}
	case "session.subscribe":
		if s.subscriptionID == "" {
			return map[string]any{}
		}
		return map[string]any{"subscription": s.subscriptionID}
	case "browsingContext.create":
		return map[string]any{"context": testBidiContext}
	case "browsingContext.navigate":
		url, _ := params["url"].(string)
		return map[string]any{"navigation": testBidiNavigation, "url": url}
	case "script.evaluate":
		return bidiEvaluateResult(params)
	default:
		return map[string]any{}
	}
}

func bidiEvaluateResult(params map[string]any) map[string]any {
	expression, _ := params["expression"].(string)
	if strings.Contains(expression, "querySelectorAll") {
		return map[string]any{
			"type": "success",
			"result": map[string]any{
				"type":  "array",
				"value": []any{map[string]any{"type": "string", "value": "/next"}},
			},
		}
	}
	return map[string]any{
		"type":   "success",
		"result": map[string]any{"type": "string", "value": testBidiHTML},
	}
}

// bidiResponseEvent builds a network.responseCompleted event. A navigation of
// "" produces the null navigation of a subresource request.
func bidiResponseEvent(contextID, navigation string, status int) map[string]any {
	params := map[string]any{
		"context":       contextID,
		"isBlocked":     false,
		"navigation":    nil,
		"redirectCount": 0,
		"request":       map[string]any{"request": "req-1", "url": "http://example.invalid/"},
		"timestamp":     1,
		"response": map[string]any{
			"url":        "http://example.invalid/",
			"protocol":   "http/1.1",
			"status":     status,
			"statusText": http.StatusText(status),
			"fromCache":  false,
			"mimeType":   "text/html",
		},
	}
	if navigation != "" {
		params["navigation"] = navigation
	}
	return map[string]any{
		"type":   "event",
		"method": "network.responseCompleted",
		"params": params,
	}
}

func newTestBidiFetcher(t *testing.T, s *fakeBidiServer) *bidiFetcher {
	t.Helper()
	f, err := newBidiFetcher(&config.CrawlerConfig{
		Timeout:        10,
		Backend:        "bidi",
		BackendOptions: map[string]any{"socket": s.wsURL()},
	})
	if err != nil {
		t.Fatalf("newBidiFetcher() error = %v", err)
	}
	t.Cleanup(func() { _ = f.close() })
	return f
}

func TestBidiFetchPageRejectsErrorStatus(t *testing.T) {
	tests := []struct {
		name      string
		events    []map[string]any
		wantErr   string
		wantNoErr bool
	}{
		{
			name:      "ok",
			events:    []map[string]any{bidiResponseEvent(testBidiContext, testBidiNavigation, 200)},
			wantNoErr: true,
		},
		{
			name:    "not found",
			events:  []map[string]any{bidiResponseEvent(testBidiContext, testBidiNavigation, 404)},
			wantErr: "unexpected status 404",
		},
		{
			name:    "server error",
			events:  []map[string]any{bidiResponseEvent(testBidiContext, testBidiNavigation, 503)},
			wantErr: "unexpected status 503",
		},
		{
			name: "redirect to success",
			events: []map[string]any{
				bidiResponseEvent(testBidiContext, testBidiNavigation, 301),
				bidiResponseEvent(testBidiContext, testBidiNavigation, 200),
			},
			wantNoErr: true,
		},
		{
			name: "redirect to error",
			events: []map[string]any{
				bidiResponseEvent(testBidiContext, testBidiNavigation, 301),
				bidiResponseEvent(testBidiContext, testBidiNavigation, 404),
			},
			wantErr: "unexpected status 404",
		},
		{
			name:      "not modified is not an error",
			events:    []map[string]any{bidiResponseEvent(testBidiContext, testBidiNavigation, 304)},
			wantNoErr: true,
		},
		{
			name:      "no response observed",
			events:    []map[string]any{},
			wantNoErr: true,
		},
		{
			name: "subresource status is ignored",
			events: []map[string]any{
				bidiResponseEvent(testBidiContext, testBidiNavigation, 200),
				bidiResponseEvent(testBidiContext, "", 404),
			},
			wantNoErr: true,
		},
		{
			name:      "status of another navigation is ignored",
			events:    []map[string]any{bidiResponseEvent(testBidiContext, "nav-other", 404)},
			wantNoErr: true,
		},
		{
			name:      "status of another context is ignored",
			events:    []map[string]any{bidiResponseEvent("ctx-other", testBidiNavigation, 404)},
			wantNoErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := tt.events
			srv := newFakeBidiServer(t, &fakeBidiServer{
				navEvents: func() []map[string]any { return events },
			})
			f := newTestBidiFetcher(t, srv)

			finalURL, htmlContent, links, err := f.fetchPage(context.Background(), "http://example.invalid/")
			if tt.wantNoErr {
				if err != nil {
					t.Fatalf("fetchPage() error = %v", err)
				}
				if finalURL != "http://example.invalid/" {
					t.Errorf("finalURL = %q", finalURL)
				}
				if htmlContent != testBidiHTML {
					t.Errorf("htmlContent = %q", htmlContent)
				}
				if len(links) != 1 || links[0] != "/next" {
					t.Errorf("links = %v", links)
				}
				return
			}
			if err == nil {
				t.Fatal("fetchPage() error = nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("fetchPage() error = %v, want it to contain %q", err, tt.wantErr)
			}
			if htmlContent != "" {
				t.Errorf("htmlContent = %q, want empty", htmlContent)
			}
		})
	}
}

// A browser that refuses the subscription must not break crawling; the status
// simply stays unknown.
func TestBidiFetchPageWithoutNetworkEvents(t *testing.T) {
	srv := newFakeBidiServer(t, &fakeBidiServer{
		failSubscribe: true,
		navEvents:     func() []map[string]any { return nil },
	})
	f := newTestBidiFetcher(t, srv)

	_, htmlContent, _, err := f.fetchPage(context.Background(), "http://example.invalid/")
	if err != nil {
		t.Fatalf("fetchPage() error = %v", err)
	}
	if htmlContent != testBidiHTML {
		t.Errorf("htmlContent = %q", htmlContent)
	}
}

// Statuses must not leak between fetches: the map entry of a closed context is
// removed, so a stale 404 cannot reject a later page.
func TestBidiNavigationStatusIsPerFetch(t *testing.T) {
	status := 404
	srv := newFakeBidiServer(t, &fakeBidiServer{
		subscriptionID: "sub-1",
		navEvents: func() []map[string]any {
			return []map[string]any{bidiResponseEvent(testBidiContext, testBidiNavigation, status)}
		},
	})
	f := newTestBidiFetcher(t, srv)

	if _, _, _, err := f.fetchPage(context.Background(), "http://example.invalid/"); err == nil {
		t.Fatal("fetchPage() error = nil for a 404")
	}
	f.navMu.Lock()
	tracked := len(f.navStatus)
	f.navMu.Unlock()
	if tracked != 0 {
		t.Errorf("navStatus holds %d entries after fetchPage, want 0", tracked)
	}

	status = 200
	if _, _, _, err := f.fetchPage(context.Background(), "http://example.invalid/"); err != nil {
		t.Fatalf("fetchPage() error = %v", err)
	}
}
