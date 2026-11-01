package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/asciimoo/hister/server/model"
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
	cfg.Server.Database = "test.sqlite3"
	if err := cfg.SaveRules(); err != nil {
		t.Fatal(err)
	}
	testutil.InitModelWithConfig(t, cfg)
	sessionStore = newSessionStore([]byte(strings.Repeat("x", 32)), cfg.BaseURL(""), sessionMaxAge)
	user := testutil.CreateUser(t, "alice")
	handler := registerEndpoints(cfg, newServerTestIndexer(t, cfg))
	return handler.(*http.ServeMux), user.ID
}

func serveProxyAuthProfile(t *testing.T, handler http.Handler, headers map[string]string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var body map[string]any
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode /api/profile response: %v", err)
		}
	}
	return rec.Code, body
}

func TestProxyAuthHeaderPresent_UserExists(t *testing.T) {
	handler, aliceID := newProxyAuthTestServer(t, "Remote-User")

	code, body := serveProxyAuthProfile(t, handler, map[string]string{"Remote-User": "alice"})
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if body["username"] != "alice" {
		t.Fatalf("username = %v, want %q", body["username"], "alice")
	}
	if uint(body["user_id"].(float64)) != aliceID {
		t.Fatalf("user_id = %v, want %d", body["user_id"], aliceID)
	}
}

func TestProxyAuthHeaderPresent_UserAutoCreated(t *testing.T) {
	handler, _ := newProxyAuthTestServer(t, "Remote-User")

	code, body := serveProxyAuthProfile(t, handler, map[string]string{"Remote-User": "bob"})
	if code != http.StatusOK {
		t.Fatalf("expected 200 for auto-created proxy user, got %d", code)
	}
	if body["username"] != "bob" {
		t.Fatalf("username = %v, want %q", body["username"], "bob")
	}

	created, err := model.GetUser("bob")
	if err != nil {
		t.Fatalf("auto-created proxy user not found: %v", err)
	}
	if created.Password != "" {
		t.Fatal("auto-created proxy user must not have a password hash")
	}
	if _, err := model.AuthenticateUser("bob", ""); !errors.Is(err, model.ErrInvalidPassword) {
		t.Fatalf("proxy user empty-password login error = %v, want %v", err, model.ErrInvalidPassword)
	}
	if _, err := model.AuthenticateUser("bob", "anything"); !errors.Is(err, model.ErrInvalidPassword) {
		t.Fatalf("proxy user password login error = %v, want %v", err, model.ErrInvalidPassword)
	}
}

func TestProxyAuthHeaderMissing(t *testing.T) {
	handler, _ := newProxyAuthTestServer(t, "Remote-User")

	code, _ := serveProxyAuthProfile(t, handler, nil)
	if code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d when proxy header is missing", code, http.StatusForbidden)
	}
}

func TestProxyAuthHeaderEmpty(t *testing.T) {
	handler, _ := newProxyAuthTestServer(t, "Remote-User")

	code, _ := serveProxyAuthProfile(t, handler, map[string]string{"Remote-User": "   "})
	if code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d for empty/whitespace proxy header", code, http.StatusForbidden)
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
	cfg.Server.Database = "test.sqlite3"
	if err := cfg.SaveRules(); err != nil {
		t.Fatal(err)
	}
	testutil.InitModelWithConfig(t, cfg)
	sessionStore = newSessionStore([]byte(strings.Repeat("x", 32)), cfg.BaseURL(""), sessionMaxAge)
	testutil.CreateUser(t, "alice")
	handler := registerEndpoints(cfg, newServerTestIndexer(t, cfg))

	// Even with a Remote-User header, proxy auth is disabled so it's ignored.
	code, _ := serveProxyAuthProfile(t, handler, map[string]string{"Remote-User": "alice"})
	if code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d when proxy auth is disabled", code, http.StatusForbidden)
	}
}

func TestProxyAuthCustomHeaderName(t *testing.T) {
	handler, _ := newProxyAuthTestServer(t, "X-Forwarded-User")

	// Using the wrong header name should fail.
	if code, _ := serveProxyAuthProfile(t, handler, map[string]string{"Remote-User": "alice"}); code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d when using wrong header name", code, http.StatusForbidden)
	}

	// Using the correct custom header name should succeed.
	code, body := serveProxyAuthProfile(t, handler, map[string]string{"X-Forwarded-User": "alice"})
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d with correct custom header", code, http.StatusOK)
	}
	if body["username"] != "alice" {
		t.Fatalf("username = %v, want %q", body["username"], "alice")
	}
}

func TestProxyAuthPreservesTokenAuth(t *testing.T) {
	handler, _ := newProxyAuthTestServer(t, "Remote-User")

	alice, err := model.GetUser("alice")
	if err != nil {
		t.Fatal(err)
	}
	if alice.Token == "" {
		t.Fatal("test user has no API token")
	}

	// No proxy header, but a valid per-user API token must still authenticate.
	for name, headers := range map[string]map[string]string{
		"access token header": {"X-Access-Token": alice.Token},
		"bearer header":       {"Authorization": "Bearer " + alice.Token},
	} {
		code, body := serveProxyAuthProfile(t, handler, headers)
		if code != http.StatusOK {
			t.Fatalf("%s: status = %d, want %d", name, code, http.StatusOK)
		}
		if body["username"] != "alice" {
			t.Fatalf("%s: username = %v, want %q", name, body["username"], "alice")
		}
	}

	// An invalid token without a proxy header must still be rejected.
	if code, _ := serveProxyAuthProfile(t, handler, map[string]string{"X-Access-Token": "invalid"}); code != http.StatusForbidden {
		t.Fatalf("invalid token status = %d, want %d", code, http.StatusForbidden)
	}
}
