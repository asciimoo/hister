package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/asciimoo/hister/server/testutil"
)

func newProxyAuthTestServer(t *testing.T, headerName string) (*http.ServeMux, uint) {
	t.Helper()
	cfg := testutil.Config(t)
	cfg.Server.ProxyAuthHeader = headerName
	cfg.App.UserHandling = true
	cfg.Server.Address = "127.0.0.1:4433"
	if err := cfg.UpdateBaseURL("http://127.0.0.1:4433"); err != nil {
		t.Fatal(err)
	}
	cfg.Server.Database = "file::memory:"
	if err := cfg.SaveRules(); err != nil {
		t.Fatal(err)
	}
	testutil.InitModelWithConfig(t, cfg)
	sessionStore = newSessionStore([]byte(strings.Repeat("x", 32)), cfg.BaseURL(""), sessionMaxAge)
	user := testutil.CreateUser(t, "alice")
	handler := registerEndpoints(cfg, newServerTestIndexer(t, cfg))
	return handler.(*http.ServeMux), user.ID
}

func TestProxyAuthHeaderPresent_UserExists(t *testing.T) {
	handler, _ := newProxyAuthTestServer(t, "Remote-User")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Header.Set("Remote-User", "ali")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestProxyAuthHeaderPresent_UserAutoCreated(t *testing.T) {
	handler, _ := newProxyAuthTestServer(t, "Remote-User")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Header.Set("Remote-User", "bob")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for auto-created proxy user, got %d", rec.Code)
	}
}

func TestProxyAuthHeaderMissing(t *testing.T) {
	handler, _ := newProxyAuthTestServer(t, "Remote-User")

	req := httptest.NewRequest(http.MethodGet, "/api/bookmarks", nil)
	// No Remote-User header set.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-200 when proxy header is missing, got %d", rec.Code)
	}
}

func TestProxyAuthHeaderEmpty(t *testing.T) {
	handler, _ := newProxyAuthTestServer(t, "Remote-User")

	req := httptest.NewRequest(http.MethodGet, "/api/bookmarks", nil)
	req.Header.Set("Remote-User", "   ")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-200 for empty/whitespace proxy header, got %d", rec.Code)
	}
}

func TestProxyAuthDisabled_FallsBackToOldAuth(t *testing.T) {
	// When ProxyAuthHeader is empty, the proxy auth path is skipped entirely.
	cfg := testutil.Config(t)
	cfg.App.UserHandling = true
	cfg.Server.Address = "127.0.0.1:4433"
	if err := cfg.UpdateBaseURL("http://127.0.0.1:4433"); err != nil {
		t.Fatal(err)
	}
	cfg.Server.Database = "file::memory:"
	if err := cfg.SaveRules(); err != nil {
		t.Fatal(err)
	}
	testutil.InitModelWithConfig(t, cfg)
	sessionStore = newSessionStore([]byte(strings.Repeat("x", 32)), cfg.BaseURL(""), sessionMaxAge)
	testutil.CreateUser(t, "alice")
	handler := registerEndpoints(cfg, newServerTestIndexer(t, cfg))

	// Even with a Remote-User header, proxy auth is disabled so it's ignored.
	req := httptest.NewRequest(http.MethodGet, "/api/bookmarks", nil)
	req.Header.Set("Remote-User", "alice")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Without a valid session cookie, the user is unauthenticated.
	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-200 when proxy auth is disabled, got %d", rec.Code)
	}
}

func TestProxyAuthCustomHeaderName(t *testing.T) {
	handler, _ := newProxyAuthTestServer(t, "X-Forwarded-User")

	// Using the wrong header name should fail.
	req := httptest.NewRequest(http.MethodGet, "/api/bookmarks", nil)
	req.Header.Set("Remote-User", "alice")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-200 when using wrong header name, got %d", rec.Code)
	}

	// Using the correct custom header name should succeed.
	req2 := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req2.Header.Set("X-Forwarded-User", "alice")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d with correct custom header", rec2.Code, http.StatusOK)
	}
}
