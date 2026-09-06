package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"
	"github.com/asciimoo/hister/server/types"
)

func TestDiagnosticsRequiresTokenInPublicMode(t *testing.T) {
	_, handler := newPublicTokenTestServer(t)
	for _, tc := range []struct {
		token  string
		status int
	}{
		{"", http.StatusForbidden},
		{"wrong", http.StatusForbidden},
		{"secret", http.StatusOK},
	} {
		rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/diagnostics", nil, map[string]string{"Authorization": "Bearer " + tc.token})
		if rec.Code != tc.status {
			t.Fatalf("status=%d, want %d, body=%s", rec.Code, tc.status, rec.Body)
		}
		if tc.status == http.StatusOK {
			var checks []types.DiagnosticCheck
			if err := json.Unmarshal(rec.Body.Bytes(), &checks); err != nil || len(checks) < 3 {
				t.Fatalf("checks=%v err=%v", checks, err)
			}
			for _, check := range checks {
				if check.Status != "ok" {
					t.Fatalf("fresh server failed diagnostics: %v", check)
				}
			}
		}
	}
}

func TestDiagnosticsRequiresAdminInMultiUserMode(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.App.UserHandling = true
	cfg.App.Public = true
	cfg.Server.Database = "file::memory:"
	if err := cfg.UpdateBaseURL("http://127.0.0.1:4433"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveRules(); err != nil {
		t.Fatal(err)
	}
	testutil.InitModelWithConfig(t, cfg)
	sessionStore = newSessionStore([]byte(strings.Repeat("x", 32)), cfg.BaseURL(""), sessionMaxAge)
	admin, err := model.CreateUser("admin", "password123", true)
	if err != nil {
		t.Fatal(err)
	}
	user := testutil.CreateUser(t, "alice")
	handler := registerEndpoints(cfg, newServerTestIndexer(t, cfg))
	for _, tc := range []struct {
		token  string
		status int
	}{
		{user.Token, http.StatusForbidden},
		{admin.Token, http.StatusOK},
	} {
		rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/diagnostics", nil, map[string]string{"Authorization": "Bearer " + tc.token})
		if rec.Code != tc.status {
			t.Fatalf("status=%d, want %d, body=%s", rec.Code, tc.status, rec.Body)
		}
	}
}
