package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/hister/server/testutil"

	"github.com/gorilla/websocket"
)

func TestInvalidQueryHTTPResponses(t *testing.T) {
	_, handler := newPublicTokenTestServer(t)
	headers := map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream", "X-Access-Token": "secret", "Origin": "hister://"}
	for _, input := range []string{"needle OR", `"needle`, "title:", "url_re:["} {
		t.Run(input, func(t *testing.T) {
			rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/search?q="+url.QueryEscape(input), nil, headers)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "at character") {
				t.Errorf("search status %d: %s", rec.Code, rec.Body)
			}
			rec = testutil.ServeHTTP(t, handler, http.MethodGet, "/api/facets?q="+url.QueryEscape(input), nil, headers)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "at character") {
				t.Errorf("facets status %d: %s", rec.Code, rec.Body)
			}
			for _, path := range []string{"/api/delete", "/api/update"} {
				for _, dryRun := range []bool{true, false} {
					body, err := json.Marshal(map[string]any{"query": input, "dry_run": dryRun, "changes": map[string]string{"label": "changed"}})
					if err != nil {
						t.Fatal(err)
					}
					rec := testutil.ServeHTTP(t, handler, http.MethodPost, path, strings.NewReader(string(body)), headers)
					if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "at character") {
						t.Errorf("%s dry_run=%v status %d: %s", path, dryRun, rec.Code, rec.Body)
					}
				}
			}
			body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "search", "arguments": map[string]string{"query": input}}})
			if err != nil {
				t.Fatal(err)
			}
			rec = testutil.ServeHTTP(t, handler, http.MethodPost, "/mcp", strings.NewReader(string(body)), headers)
			var response struct {
				Error struct {
					Code    int
					Message string
				}
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Error.Code != mcpErrInvalidParam || !strings.Contains(response.Error.Message, "at character") {
				t.Errorf("MCP diagnostic: %s", rec.Body)
			}
		})
	}
}

func TestInvalidQueryDoesNotRedirectAwayFromDiagnostics(t *testing.T) {
	cfg, handler := newPublicTokenTestServer(t)
	cfg.App.RedirectOnNoResults = true
	previousFiles := staticTextFiles
	staticTextFiles = make(map[string][]byte)
	t.Cleanup(func() { staticTextFiles = previousFiles })
	if err := parseStaticFiles(""); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"needle OR", `"needle`, "url_re:["} {
		rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/?q="+url.QueryEscape(input), nil, nil)
		if rec.Code != http.StatusOK || rec.Header().Get("Location") != "" {
			t.Errorf("query %q redirected away from the app: status %d, location %q", input, rec.Code, rec.Header().Get("Location"))
		}
	}
	rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/?q=needle", nil, nil)
	if rec.Code != http.StatusFound {
		t.Errorf("valid query with no results did not redirect: status %d", rec.Code)
	}
}

func TestQueryWebSocketReportsErrorsAndRecovers(t *testing.T) {
	cfg, handler := newPublicTokenTestServer(t)
	server := httptest.NewServer(handler)
	defer server.Close()
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/search", http.Header{"Origin": {cfg.Server.BaseURL}})
	if resp != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"needle OR", "url_re:[", "needle"} {
		if err := conn.WriteJSON(map[string]string{"text": input}); err != nil {
			t.Fatal(err)
		}
		var result struct {
			Error string
			Query struct{ Text string }
			Total int
		}
		if err := conn.ReadJSON(&result); err != nil {
			t.Fatal(err)
		}
		if result.Query.Text != input {
			t.Errorf("response query = %q, want %q", result.Query.Text, input)
		}
		if input == "needle" {
			if result.Error != "" {
				t.Errorf("valid query error: %s", result.Error)
			}
		} else if !strings.Contains(result.Error, "at character") || result.Total != 0 {
			t.Errorf("invalid query response: %+v", result)
		}
	}
}
