package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/asciimoo/hister/server/testutil"
)

func TestExtensionCSRFAllowsOrionReferer(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	cookie := tokenLoginCookie(t, handler, nil)
	for _, referrer := range []string{
		"chrome-extension://cciilamhchpmbdnniabclekddabkifhb/_generated_background_page.html",
		"moz-extension://aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/_generated_background_page.html",
	} {
		for _, auth := range []string{"access token", "bearer token", "session"} {
			t.Run(referrer+"/"+auth, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, "/api/add", strings.NewReader(`{"url":"https://example.com/orion","title":"Orion page","text":"A page submitted from Orion."}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Referer", referrer)
				switch auth {
				case "access token":
					req.Header.Set("X-Access-Token", "secret")
				case "bearer token":
					req.Header.Set("Authorization", "Bearer secret")
				case "session":
					req.AddCookie(cookie)
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusCreated {
					t.Fatalf("POST /api/add status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body)
				}
				if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
					t.Fatalf("request without Origin received Access-Control-Allow-Origin: %q", got)
				}
			})
		}
	}
}

func TestExtensionCSRFRefererRestrictions(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	cookie := tokenLoginCookie(t, handler, nil)
	referrer := "chrome-extension://cciilamhchpmbdnniabclekddabkifhb/_generated_background_page.html"
	for _, tc := range []struct {
		name     string
		origin   string
		referrer string
		path     string
	}{
		{name: "missing referrer"},
		{name: "web page", referrer: "https://evil.example/"},
		{name: "extension URL in web path", referrer: "https://evil.example/" + referrer},
		{name: "wrong Chrome extension", referrer: "chrome-extension://other-extension/background.html"},
		{name: "Chrome ID prefix", referrer: "chrome-extension://cciilamhchpmbdnniabclekddabkifhb.evil.example/background.html"},
		{name: "Chrome ID as username", referrer: "chrome-extension://cciilamhchpmbdnniabclekddabkifhb@evil.example/background.html"},
		{name: "credentials", referrer: "moz-extension://user@aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/background.html"},
		{name: "empty extension host", referrer: "moz-extension:///background.html"},
		{name: "malformed URL", referrer: "moz-extension://[invalid/background.html"},
		{name: "relative URL", referrer: "//cciilamhchpmbdnniabclekddabkifhb/background.html"},
		{name: "untrusted Origin takes precedence", origin: "https://evil.example", referrer: referrer},
		{name: "null Origin takes precedence", origin: "null", referrer: referrer},
		{name: "endpoint outside extension API", referrer: referrer, path: "/api/token-login"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path
			if path == "" {
				path = "/api/add"
			}
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Referer", tc.referrer)
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden || strings.TrimSpace(rec.Body.String()) != "CSRF token mismatch" {
				t.Fatalf("POST %s status = %d, body=%s; want CSRF rejection", path, rec.Code, rec.Body)
			}
		})
	}

	for _, token := range []string{"", "incorrect"} {
		rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/add", strings.NewReader(`{}`), map[string]string{
			"Content-Type":   "application/json",
			"Referer":        referrer,
			"X-Access-Token": token,
		})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("extension referrer bypassed authentication: status = %d, body=%s", rec.Code, rec.Body)
		}
	}
}

func TestExtensionCORSPreflightAllowsSafari(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	origin := "safari-web-extension://aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	rec := testutil.ServeHTTP(t, handler, http.MethodOptions, "/api/add", nil, map[string]string{
		"Origin":                         origin,
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "content-type,x-access-token",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS /api/add status = %d, want %d; body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, origin)
	}
	allowHeaders := strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers"))
	if !strings.Contains(allowHeaders, "x-access-token") {
		t.Fatalf("Access-Control-Allow-Headers = %q, want to include x-access-token", rec.Header().Get("Access-Control-Allow-Headers"))
	}
}

func TestExtensionCORSPreflightEchoesCustomRequestHeaders(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	origin := "safari-web-extension://aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	requested := "content-type,x-access-token,x-proxy-auth"
	rec := testutil.ServeHTTP(t, handler, http.MethodOptions, "/api/add", nil, map[string]string{
		"Origin":                         origin,
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": requested,
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS /api/add status = %d, want %d; body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != requested {
		t.Fatalf("Access-Control-Allow-Headers = %q, want echoed %q", got, requested)
	}
}

func TestExtensionCORSPreflightRejectsUntrustedOrigin(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	rec := testutil.ServeHTTP(t, handler, http.MethodOptions, "/api/add", nil, map[string]string{
		"Origin":                         "https://evil.example",
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "content-type",
	})
	if rec.Header().Get("Access-Control-Allow-Origin") == "https://evil.example" {
		t.Fatal("untrusted origin received Access-Control-Allow-Origin")
	}
	if rec.Code == http.StatusNoContent {
		t.Fatal("OPTIONS from an untrusted origin was treated as an extension preflight")
	}
}

func TestExtensionCORSPreflightRejectsRemovedAddPath(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	rec := testutil.ServeHTTP(t, handler, http.MethodOptions, "/add", nil, map[string]string{
		"Origin":                        "safari-web-extension://aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		"Access-Control-Request-Method": "POST",
	})
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("OPTIONS /add status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("removed endpoint received Access-Control-Allow-Origin: %q", got)
	}
}

func TestExtensionCSRFAllowsSafariHistoryAndProfile(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	origin := "safari-web-extension://aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	headers := map[string]string{
		"Origin":         origin,
		"Content-Type":   "application/json",
		"X-Access-Token": "secret",
	}

	history := testutil.ServeHTTP(
		t,
		handler,
		http.MethodPost,
		"/api/history",
		strings.NewReader(`{"url":"https://example.com","title":"Example","query":"q"}`),
		headers,
	)
	if history.Code == http.StatusForbidden {
		t.Fatalf("POST /api/history from Safari extension was CSRF-blocked: %s", history.Body.String())
	}

	profile := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/profile", nil, headers)
	if profile.Code == http.StatusForbidden {
		t.Fatalf("GET /api/profile from Safari extension was CSRF-blocked: %s", profile.Body.String())
	}
	if got := profile.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("GET /api/profile ACAO = %q, want %q", got, origin)
	}
}
