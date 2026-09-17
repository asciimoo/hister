package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/indexer/searchschema"
	"github.com/asciimoo/hister/server/metrics"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"
	"github.com/asciimoo/hister/server/timeline"
)

func newPublicTokenTestServer(t *testing.T) (*config.Config, http.Handler) {
	return newTokenTestServer(t, true)
}

func newServerTestIndexer(t *testing.T, cfg *config.Config) *indexer.Indexer {
	t.Helper()
	idx, err := indexer.New(cfg)
	if err != nil {
		t.Fatalf("initialize test indexer: %v", err)
	}
	t.Cleanup(idx.Close)
	return idx
}

func newTokenTestServer(t *testing.T, public bool) (*config.Config, http.Handler) {
	return newTokenTestServerWithProfiler(t, public, false)
}

func newTokenTestServerWithProfiler(t *testing.T, public, profiler bool) (*config.Config, http.Handler) {
	t.Helper()
	cfg := testutil.Config(t)
	cfg.App.AccessToken = "secret"
	cfg.App.Public = public
	cfg.App.Profiler = profiler
	cfg.Server.Address = "127.0.0.1:4433"
	if err := cfg.UpdateBaseURL("http://127.0.0.1:4433"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveRules(); err != nil {
		t.Fatal(err)
	}
	cfg.Server.Database = "file::memory:"
	testutil.InitModelWithConfig(t, cfg)
	sessionStore = newSessionStore([]byte(strings.Repeat("x", 32)), cfg.BaseURL(""), sessionMaxAge)
	return cfg, registerEndpoints(cfg, newServerTestIndexer(t, cfg))
}

func TestPublicModeConfigResponse(t *testing.T) {
	cfg, handler := newPublicTokenTestServer(t)
	cfg.App.ColorScheme = "dark"
	rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/config", nil, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Title          string                    `json:"title"`
		Subtitle       string                    `json:"subtitle"`
		ColorScheme    string                    `json:"colorScheme"`
		Public         bool                      `json:"public"`
		Authenticated  bool                      `json:"authenticated"`
		CanWrite       bool                      `json:"canWrite"`
		HistoryEnabled bool                      `json:"historyEnabled"`
		Search         searchschema.Capabilities `json:"search"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Title != "Hister" {
		t.Fatalf("title = %q, want %q", body.Title, "Hister")
	}
	if body.Subtitle != "Your own search engine" {
		t.Fatalf("subtitle = %q, want %q", body.Subtitle, "Your own search engine")
	}
	if body.ColorScheme != "dark" {
		t.Fatalf("colorScheme = %q, want %q", body.ColorScheme, "dark")
	}
	if !body.Public {
		t.Fatal("public = false, want true")
	}
	if body.Authenticated {
		t.Fatal("authenticated = true, want false")
	}
	if body.CanWrite {
		t.Fatal("canWrite = true, want false")
	}
	if body.HistoryEnabled {
		t.Fatal("historyEnabled = true, want false")
	}
	if body.Search.Version != searchschema.Version {
		t.Fatalf("search schema version = %d, want %d", body.Search.Version, searchschema.Version)
	}
	if len(body.Search.Facets) == 0 || len(body.Search.Sort.Options) == 0 {
		t.Fatal("search schema is missing facets or sort options")
	}
}

func TestMetricsRequiresTokenAndHonorsBasePath(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.App.AccessToken = "secret"
	cfg.Server.Metrics = true
	cfg.Server.Database = "file::memory:"
	if err := cfg.UpdateBaseURL("http://127.0.0.1:4433/hister"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveRules(); err != nil {
		t.Fatal(err)
	}
	testutil.InitModelWithConfig(t, cfg)
	sessionStore = newSessionStore([]byte(strings.Repeat("x", 32)), cfg.BaseURL(""), sessionMaxAge)

	idx := newServerTestIndexer(t, cfg)
	m := metrics.New(context.Background(), idx)
	idx.SetMetrics(m)
	t.Cleanup(m.Stop)
	handler := registerEndpoints(cfg, idx)

	for _, tc := range []struct {
		name   string
		target string
		token  string
		status int
	}{
		{name: "unprefixed", target: "/metrics", status: http.StatusNotFound},
		{name: "anonymous", target: "/hister/metrics", status: http.StatusForbidden},
		{name: "authenticated", target: "/hister/metrics", token: "secret", status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := map[string]string{}
			if tc.token != "" {
				headers["Authorization"] = "Bearer " + tc.token
			}
			rec := testutil.ServeHTTP(t, handler, http.MethodGet, tc.target, nil, headers)
			if rec.Code != tc.status {
				t.Fatalf("GET %s status = %d, want %d; body=%s", tc.target, rec.Code, tc.status, rec.Body)
			}
		})
	}

	docs := testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api", nil, map[string]string{"Authorization": "Bearer secret"})
	if !apiDocsContain(t, docs.Body.Bytes(), "Metrics") {
		t.Fatal("API documentation does not include enabled Metrics endpoint")
	}
}

func TestAPIDocsHideDisabledMetrics(t *testing.T) {
	_, handler := newPublicTokenTestServer(t)
	rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/api", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api status = %d, want %d", rec.Code, http.StatusOK)
	}
	if apiDocsContain(t, rec.Body.Bytes(), "Metrics") {
		t.Fatal("API documentation includes disabled Metrics endpoint")
	}
}

func apiDocsContain(t *testing.T, body []byte, name string) bool {
	t.Helper()
	var endpoints []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &endpoints); err != nil {
		t.Fatalf("decode API documentation: %v", err)
	}
	for _, endpoint := range endpoints {
		if endpoint.Name == name {
			return true
		}
	}
	return false
}

func TestPublicModeAllowsDocumentedPublicRoutes(t *testing.T) {
	_, handler := newPublicTokenTestServer(t)
	tests := []struct {
		name   string
		method string
		target string
		body   string
		want   int
	}{
		{name: "api docs", method: http.MethodGet, target: "/api", want: http.StatusOK},
		{name: "search", method: http.MethodGet, target: "/search?format=json", want: http.StatusBadRequest},
		{name: "legacy file path", method: http.MethodGet, target: "/api/file?path=/tmp/note.txt", want: http.StatusBadRequest},
		{name: "file", method: http.MethodGet, target: "/api/file?id=missing", want: http.StatusNotFound},
		{name: "mcp tools list", method: http.MethodPost, target: "/mcp", body: `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, want: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := testutil.ServeHTTP(t, handler, tt.method, tt.target, strings.NewReader(tt.body), nil)

			if rec.Code != tt.want {
				t.Fatalf("%s %s status = %d, want %d; body=%s", tt.method, tt.target, rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestPublicModeProtectsWriteRoutes(t *testing.T) {
	_, handler := newPublicTokenTestServer(t)
	tests := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "delete", method: http.MethodPost, target: "/api/delete", body: `{"query":"*"}`},
		{name: "add", method: http.MethodPost, target: "/api/add", body: `{"url":"https://example.com"}`},
		{name: "rules", method: http.MethodGet, target: "/api/rules"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := testutil.ServeHTTP(t, handler, tt.method, tt.target, strings.NewReader(tt.body), nil)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s status = %d, want %d", tt.method, tt.target, rec.Code, http.StatusForbidden)
			}
		})
	}
}

func TestPublicModeAllowsAuthenticatedProtectedRoutes(t *testing.T) {
	_, handler := newPublicTokenTestServer(t)
	rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/add", nil, map[string]string{
		"Origin":         "hister://",
		"X-Access-Token": "secret",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/add status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestTokenLoginSetsHttpOnlySessionCookieAndAuthenticates(t *testing.T) {
	_, handler := newPublicTokenTestServer(t)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/token-login", strings.NewReader(`{"token":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("Origin", "hister://")
	loginRec := httptest.NewRecorder()

	handler.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("POST /api/token-login status = %d, want %d; body=%s", loginRec.Code, http.StatusOK, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("POST /api/token-login did not set a cookie")
	}
	var sessionCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == storeName {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("POST /api/token-login did not set %q cookie", storeName)
	}
	if !sessionCookie.HttpOnly {
		t.Fatal("session cookie HttpOnly = false, want true")
	}
	if sessionCookie.Secure {
		t.Fatal("HTTP session cookie Secure = true, want false")
	}
	if sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie SameSite = %v, want %v", sessionCookie.SameSite, http.SameSiteLaxMode)
	}
	if sessionCookie.MaxAge != sessionMaxAge {
		t.Fatalf("session cookie MaxAge = %d, want %d", sessionCookie.MaxAge, sessionMaxAge)
	}
	if !validSessionToken(sessionCookie.Value) {
		t.Fatalf("session cookie does not contain an opaque %d byte identifier", sessionTokenBytes)
	}
	if strings.Contains(sessionCookie.Value, "secret") {
		t.Fatal("session cookie contains the application access token")
	}
	storedSession, err := model.GetWebSession(sessionTokenHash(sessionCookie.Value))
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.TokenHash == sessionCookie.Value {
		t.Fatal("database stores the raw session identifier")
	}
	if strings.Contains(string(storedSession.Data), "secret") {
		t.Fatal("server side session data contains the application access token")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/add", nil)
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/add with session cookie status = %d, want %d", rec.Code, http.StatusOK)
	}

	configReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	configReq.AddCookie(sessionCookie)
	configRec := httptest.NewRecorder()
	handler.ServeHTTP(configRec, configReq)

	if configRec.Code != http.StatusOK {
		t.Fatalf("GET /api/config with session cookie status = %d, want %d", configRec.Code, http.StatusOK)
	}
	var configBody struct {
		Authenticated  bool `json:"authenticated"`
		HistoryEnabled bool `json:"historyEnabled"`
	}
	if err := json.Unmarshal(configRec.Body.Bytes(), &configBody); err != nil {
		t.Fatal(err)
	}
	if !configBody.Authenticated {
		t.Fatal("authenticated = false, want true")
	}
	if !configBody.HistoryEnabled {
		t.Fatal("historyEnabled = false, want true")
	}
}

func TestPublicModeEnablesHistoryForAuthenticatedCallers(t *testing.T) {
	_, handler := newPublicTokenTestServer(t)
	anonymousRec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/history", strings.NewReader(`{"query":"q","url":"https://example.com","title":"Example"}`), map[string]string{
		"Origin": "hister://",
	})

	if anonymousRec.Code != http.StatusForbidden {
		t.Fatalf("anonymous POST /api/history status = %d, want %d", anonymousRec.Code, http.StatusForbidden)
	}

	readRec := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/history?opened=true", nil, map[string]string{
		"X-Access-Token": "secret",
	})

	if readRec.Code != http.StatusOK {
		t.Fatalf("authenticated GET /api/history status = %d, want %d", readRec.Code, http.StatusOK)
	}

	rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/history", strings.NewReader(`{"query":"q","url":"https://example.com","title":"Example"}`), map[string]string{
		"Origin":         "hister://",
		"X-Access-Token": "secret",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/history status = %d, want %d", rec.Code, http.StatusOK)
	}
	items, err := model.GetLatestHistoryItems(0, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Query != "q" || items[0].URL != "https://example.com" {
		t.Fatalf("saved history = %+v, want submitted item", items)
	}

	timelineRec := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/history/timeline?opened=true&timezone=UTC", nil, map[string]string{
		"X-Access-Token": "secret",
	})
	if timelineRec.Code != http.StatusOK {
		t.Fatalf("GET /api/history/timeline status = %d, want %d; body=%s", timelineRec.Code, http.StatusOK, timelineRec.Body.String())
	}
	var timelineBody timeline.Result
	if err := json.Unmarshal(timelineRec.Body.Bytes(), &timelineBody); err != nil {
		t.Fatal(err)
	}
	if len(timelineBody.Days) != 7 || timelineBody.Days[0].Count != 1 {
		t.Fatalf("timeline days = %+v, want one item today", timelineBody.Days)
	}
	drilldownURL := fmt.Sprintf(
		"/api/history/timeline?opened=true&timezone=UTC&date_from=%d&date_to=%d",
		timelineBody.Days[0].From,
		timelineBody.Days[0].To,
	)
	drilldownRec := testutil.ServeHTTP(t, handler, http.MethodGet, drilldownURL, nil, map[string]string{
		"X-Access-Token": "secret",
	})
	if drilldownRec.Code != http.StatusOK {
		t.Fatalf("timeline drilldown status = %d, want %d; body=%s", drilldownRec.Code, http.StatusOK, drilldownRec.Body.String())
	}
	var drilldownBody timeline.DailyResult
	if err := json.Unmarshal(drilldownRec.Body.Bytes(), &drilldownBody); err != nil {
		t.Fatal(err)
	}
	if len(drilldownBody.Days) != 1 || drilldownBody.Days[0].Count != 1 {
		t.Fatalf("timeline drilldown days = %+v, want one item today", drilldownBody.Days)
	}
}

func TestMCPGetHistoryOpenedMode(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	if err := model.UpdateHistory(0, "hister mcp", "https://example.com/mcp", "MCP result"); err != nil {
		t.Fatal(err)
	}
	if err := model.UpdateHistory(0, "history view", "https://example.com/history", "History result"); err != nil {
		t.Fatal(err)
	}

	rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_history","arguments":{"mode":"opened","limit":10}}}`), map[string]string{
		"X-Access-Token": "secret",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /mcp get_history status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Result struct {
			Content           []mcpTextContent    `json:"content"`
			StructuredContent mcpStructuredResult `json:"structuredContent"`
		} `json:"result"`
		Error *mcpRPCError `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error != nil {
		t.Fatalf("MCP error = %+v", body.Error)
	}
	if len(body.Result.Content) != 1 {
		t.Fatalf("content length = %d, want 1", len(body.Result.Content))
	}
	if !strings.HasPrefix(body.Result.Content[0].Text, "SECURITY NOTICE:") {
		t.Fatalf("text fallback lacks security notice: %s", body.Result.Content[0].Text)
	}
	if body.Result.StructuredContent.Trusted["mode"] != "opened" {
		t.Fatalf("history mode = %#v, want opened", body.Result.StructuredContent.Trusted["mode"])
	}
	if len(body.Result.StructuredContent.UntrustedContent) != 2 {
		t.Fatalf("untrusted history length = %d, want 2", len(body.Result.StructuredContent.UntrustedContent))
	}
	encodedContent, err := json.Marshal(body.Result.StructuredContent.UntrustedContent)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encodedContent)
	for _, want := range []string{
		`"trust":"untrusted"`,
		`"query":"hister mcp"`,
		`"url":"https://example.com/mcp"`,
		`"query":"history view"`,
		`"url":"https://example.com/history"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("history response missing %q in:\n%s", want, text)
		}
	}
}

func TestMCPGetHistoryDefaultsToIndexedMode(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_history","arguments":{"limit":10}}}`), map[string]string{
		"X-Access-Token": "secret",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /mcp get_history status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Result struct {
			Content           []mcpTextContent    `json:"content"`
			StructuredContent mcpStructuredResult `json:"structuredContent"`
		} `json:"result"`
		Error *mcpRPCError `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error != nil {
		t.Fatalf("MCP error = %+v", body.Error)
	}
	if len(body.Result.Content) != 1 {
		t.Fatalf("content length = %d, want 1", len(body.Result.Content))
	}
	if body.Result.StructuredContent.Trusted["mode"] != "indexed" {
		t.Fatalf("default history mode = %#v, want indexed", body.Result.StructuredContent.Trusted["mode"])
	}
}

func TestTokenAuthStillProtectsPublicRoutesWhenPublicModeDisabled(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/search?format=json", nil, nil)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /search status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
