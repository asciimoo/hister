// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"

	"gopkg.in/yaml.v3"
)

func TestOAuthAdminGroupsSync(t *testing.T) {
	cfg, handler := newOAuthTestServer(t)
	entry := cfg.Server.OAuth["oidc"]
	if err := yaml.Unmarshal([]byte("admin_groups: [hister-admins]\n"), entry); err != nil {
		t.Fatal(err)
	}
	var userInfo atomic.Value
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = fmt.Fprint(w, `{"access_token":"token"}`)
		} else {
			_, _ = fmt.Fprint(w, userInfo.Load().(string))
		}
	}))
	t.Cleanup(provider.Close)
	entry.TokenURL = provider.URL + "/token"
	entry.UserInfoURL = provider.URL + "/userinfo"
	var cookies []*http.Cookie
	var userID uint
	for _, step := range []struct {
		groups   string
		admin    bool
		revision uint64
	}{
		{`,"groups":["users"]`, false, 1},
		{`,"groups":["users","hister-admins"]`, true, 2},
		{`,"groups":["hister-admins"]`, true, 2},
		{`,"groups":["users"]`, false, 3},
		{`,"groups":["hister-admins"]`, true, 4},
		{"", false, 5},
	} {
		userInfo.Store(`{"email":"alice@example.com"` + step.groups + `}`)
		cookie, query := startOAuthLogin(t, handler, nil)
		rec := oauthCallback(handler, cookie, url.Values{"provider": {"oidc"}, "state": {query.Get("state")}, "code": {"code"}})
		if rec.Code != http.StatusFound {
			t.Fatalf("login status = %d: %s", rec.Code, rec.Body.String())
		}
		cookies = append(cookies, responseSessionCookie(t, rec))
		user, err := model.GetUserByOAuthID("oidc-alice@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if userID != 0 && user.ID != userID {
			t.Fatal("login created a different account")
		}
		userID = user.ID
		if user.IsAdmin != step.admin || user.Revision != step.revision {
			t.Fatalf("groups %s: admin = %v, revision = %d; want %v, %d", step.groups, user.IsAdmin, user.Revision, step.admin, step.revision)
		}
		wantStatus := http.StatusForbidden
		if step.admin {
			wantStatus = http.StatusOK
		}
		// Both existing browser sessions and API tokens must use the new role.
		for _, existing := range cookies {
			for _, path := range []string{"/hister/api/users", "/hister/api/settings"} {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.AddCookie(existing)
				result := httptest.NewRecorder()
				handler.ServeHTTP(result, req)
				if result.Code != wantStatus {
					t.Fatalf("session admin access = %d, want %d", result.Code, wantStatus)
				}
			}
		}
		result := testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/users", nil, map[string]string{"Authorization": "Bearer " + user.Token})
		if result.Code != wantStatus {
			t.Fatalf("token admin access = %d, want %d", result.Code, wantStatus)
		}
	}
}

func TestOAuthAdminGroupClaims(t *testing.T) {
	for _, tc := range []struct {
		name, settings, claims string
		existingAdmin, admin   bool
		invalid, discovery     bool
	}{
		{name: "new administrator", settings: "admin_groups: [operators, hister-admins]\n", claims: `,"groups":["hister-admins"]`, admin: true},
		{name: "custom claim", settings: "admin_groups: [hister-admins]\ngroups_claim: roles\n", claims: `,"roles":["hister-admins"]`, admin: true},
		{name: "discovery preserves mapping", settings: "admin_groups: [hister-admins]\ngroups_claim: roles\n", claims: `,"roles":["hister-admins"]`, admin: true, discovery: true},
		{name: "discovery cannot grant access", settings: "admin_groups: [hister-admins]\ngroups_claim: roles\n", claims: `,"groups":["intruders"]`, discovery: true},
		{name: "literal dotted claim", settings: "admin_groups: [hister-admins]\ngroups_claim: app.roles\n", claims: `,"app.roles":["hister-admins"],"app":{"roles":[]}`, admin: true},
		{name: "URI claim", settings: "admin_groups: [hister-admins]\ngroups_claim: https://example.com/roles\n", claims: `,"https://example.com/roles":["hister-admins"]`, admin: true},
		{name: "no fallback to groups", settings: "admin_groups: [hister-admins]\ngroups_claim: roles\n", claims: `,"groups":["hister-admins"]`, existingAdmin: true},
		{name: "exact matching", settings: "admin_groups: [hister-admins]\n", claims: `,"groups":["HISTER-ADMINS","hister-admins-extra"," hister-admins","/hister-admins"]`, existingAdmin: true},
		{name: "empty groups", settings: "admin_groups: [hister-admins]\n", claims: `,"groups":[]`, existingAdmin: true},
		{name: "null groups", settings: "admin_groups: [hister-admins]\n", claims: `,"groups":null`, existingAdmin: true},
		{name: "mapping disabled", claims: `,"groups":["hister-admins"]`},
		{name: "preserve manual administrator", claims: `,"groups":123`, existingAdmin: true, admin: true},
		{name: "string groups", settings: "admin_groups: [hister-admins]\n", claims: `,"groups":"hister-admins"`, invalid: true},
		{name: "object groups", settings: "admin_groups: [hister-admins]\n", claims: `,"groups":{"hister-admins":true}`, invalid: true},
		{name: "mixed groups", settings: "admin_groups: [hister-admins]\n", claims: `,"groups":["hister-admins",123]`, invalid: true},
		{name: "null group member", settings: "admin_groups: [hister-admins]\n", claims: `,"groups":["hister-admins",null]`, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, handler := newOAuthTestServer(t)
			entry := cfg.Server.OAuth["oidc"]
			if err := yaml.Unmarshal([]byte(tc.settings), entry); err != nil {
				t.Fatal(err)
			}
			if tc.existingAdmin {
				user, err := model.CreateOAuthUser("alice", "oidc-alice@example.com")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := model.ToggleAdmin(user.Username); err != nil {
					t.Fatal(err)
				}
			}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/discovery":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"authorization_endpoint": entry.AuthURL, "token_endpoint": entry.TokenURL,
						"userinfo_endpoint": entry.UserInfoURL, "scopes_supported": []string{"openid"},
						"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code"},
						"AdminGroups": []string{"intruders"}, "admin_groups": []string{"intruders"},
						"GroupsClaim": "groups", "groups_claim": "groups",
					})
				case "/token":
					_, _ = fmt.Fprint(w, `{"access_token":"token"}`)
				default:
					_, _ = fmt.Fprint(w, `{"email":"alice@example.com"`+tc.claims+`}`)
				}
			}))
			t.Cleanup(provider.Close)
			entry.TokenURL = provider.URL + "/token"
			entry.UserInfoURL = provider.URL + "/userinfo"
			if tc.discovery {
				entry.ConfigurationURL = provider.URL + "/discovery"
			}
			cookie, query := startOAuthLogin(t, handler, nil)
			rec := oauthCallback(handler, cookie, url.Values{"provider": {"oidc"}, "state": {query.Get("state")}, "code": {"code"}})
			if tc.invalid {
				if rec.Code != http.StatusInternalServerError {
					t.Fatalf("invalid groups login status = %d", rec.Code)
				}
				if _, err := model.GetUserByOAuthID("oidc-alice@example.com"); err != model.ErrUserNotFound {
					t.Fatalf("invalid groups created an account: %v", err)
				}
				assertOAuthFlowConsumed(t, cookie)
				return
			}
			if rec.Code != http.StatusFound {
				t.Fatalf("login status = %d: %s", rec.Code, rec.Body.String())
			}
			user, err := model.GetUserByOAuthID("oidc-alice@example.com")
			if err != nil {
				t.Fatal(err)
			}
			if user.IsAdmin != tc.admin {
				t.Fatalf("admin = %v, want %v", user.IsAdmin, tc.admin)
			}
		})
	}
}

func newOAuthTestServer(t *testing.T) (*config.Config, http.Handler) {
	t.Helper()
	cfg := testutil.InitModel(t)
	cfg.App.UserHandling = true
	cfg.Server.OAuth = map[string]*config.OAuthEntry{
		"oidc":   {ClientID: "hister", ClientSecret: "secret", AuthURL: "https://provider.example/auth"},
		"google": {ClientID: "google-client", ClientSecret: "google-secret"},
	}
	if err := cfg.UpdateBaseURL("http://hister.example/hister"); err != nil {
		t.Fatal(err)
	}
	previousStore := sessionStore
	t.Cleanup(func() { sessionStore = previousStore })
	sessionStore = newSessionStore([]byte(strings.Repeat("x", 32)), cfg.BaseURL(""), sessionMaxAge)
	return cfg, registerEndpoints(cfg, nil)
}

func startOAuthLogin(t *testing.T, handler http.Handler, cookie *http.Cookie, returnTo ...string) (*http.Cookie, url.Values) {
	t.Helper()
	params := url.Values{"provider": {"oidc"}}
	if len(returnTo) > 0 {
		params.Set("return_to", returnTo[0])
	}
	req := httptest.NewRequest(http.MethodGet, "/hister/api/oauth?"+params.Encode(), nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("OAuth redirect status = %d: %s", rec.Code, rec.Body.String())
	}
	redirect, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return responseSessionCookie(t, rec), redirect.Query()
}

func oauthCallback(handler http.Handler, cookie *http.Cookie, query url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/hister/api/oauth/callback?"+query.Encode(), nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func oauthSessionValues(t *testing.T, cookie *http.Cookie) map[any]any {
	t.Helper()
	record, err := model.GetWebSession(sessionTokenHash(cookie.Value))
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[any]any)
	if err := decodeSessionValues(record.Data, &values); err != nil {
		t.Fatal(err)
	}
	return values
}

func assertOAuthFlowConsumed(t *testing.T, cookie *http.Cookie) {
	t.Helper()
	values := oauthSessionValues(t, cookie)
	for _, key := range []string{oauthStateKey, oauthProviderKey, oauthVerifierKey, oauthReturnToKey} {
		if _, ok := values[key]; ok {
			t.Errorf("session still contains %s", key)
		}
	}
}

func TestOAuthReturnDestination(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = fmt.Fprint(w, `{"access_token":"token"}`)
		} else {
			_, _ = fmt.Fprint(w, `{"email":"alice@example.com"}`)
		}
	}))
	t.Cleanup(provider.Close)
	for _, tc := range []struct {
		name, target, want string
	}{
		{"preview", "/hister/preview?id=https%3A%2F%2Fexample.com%2Farticle#saved-section", "/preview?id=https%3A%2F%2Fexample.com%2Farticle#saved-section"},
		{"search", "/hister/?q=needle+label%3Aresearch#results", "/?q=needle+label%3Aresearch#results"},
		{"rules", "/hister/rules#aliases", "/rules#aliases"},
		{"settings", "/hister/settings#server", "/settings#server"},
		{"default", "", "/"},
		{"external", "https://outside.example/", "/"},
		{"scheme relative", "//outside.example/", "/"},
		{"backslash", `/\outside.example/`, "/"},
		{"outside base", "/preview", "/"},
		{"base sibling", "/hister-other/preview", "/"},
		{"traversal", "/hister/../preview", "/"},
		{"encoded traversal", "/hister/%2e%2e/preview", "/"},
		{"encoded separator", "/hister/%2f%2foutside.example", "/"},
		{"auth loop", "/hister/auth?return_to=/hister/auth", "/"},
		{"API endpoint", "/hister/api/oauth?provider=oidc", "/"},
		{"invalid escape", "/hister/%xx", "/"},
		{"control character", "/hister/preview\r\nLocation: https://outside.example/", "/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, handler := newOAuthTestServer(t)
			cfg.Server.OAuth["oidc"].TokenURL = provider.URL + "/token"
			cfg.Server.OAuth["oidc"].UserInfoURL = provider.URL + "/userinfo"
			cookie, query := startOAuthLogin(t, handler, nil, tc.target)
			if query.Has("return_to") {
				t.Fatal("return destination was exposed to the OAuth provider")
			}
			callback := url.Values{"provider": {"oidc"}, "state": {query.Get("state")}, "code": {"code"}, "return_to": {"/hister/about"}}
			rec := oauthCallback(handler, cookie, callback)
			if rec.Code != http.StatusFound || rec.Header().Get("Location") != cfg.BaseURL(tc.want) {
				t.Fatalf("callback status = %d, location = %q, want %q; body = %s", rec.Code, rec.Header().Get("Location"), cfg.BaseURL(tc.want), rec.Body.String())
			}
			assertOAuthFlowConsumed(t, responseSessionCookie(t, rec))
		})
	}
}

func TestOAuthPKCEFlow(t *testing.T) {
	for _, mode := range []string{"manual", "discovery", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			cfg, handler := newOAuthTestServer(t)
			cfg.Server.OAuthOnly = true
			entry := cfg.Server.OAuth["oidc"]
			entry.DisablePKCE = mode == "disabled"
			var cookie *http.Cookie
			var challenge string
			var tokenCalls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/discovery":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"authorization_endpoint": entry.AuthURL, "token_endpoint": entry.TokenURL,
						"userinfo_endpoint": entry.UserInfoURL, "scopes_supported": []string{"openid"},
						"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code"},
						// Deliberately omit PKCE metadata. It is not required to use S256.
					})
				case "/token":
					tokenCalls.Add(1)
					record, err := model.GetWebSession(sessionTokenHash(cookie.Value))
					values := make(map[any]any)
					if err == nil {
						err = decodeSessionValues(record.Data, &values)
					}
					if err != nil {
						t.Errorf("read session during token exchange: %v", err)
						http.Error(w, "session failure", http.StatusInternalServerError)
						return
					}
					for _, key := range []string{oauthStateKey, oauthProviderKey, oauthVerifierKey, oauthReturnToKey} {
						if _, ok := values[key]; ok {
							t.Errorf("token exchange started before consuming %s", key)
						}
					}
					if err := r.ParseForm(); err != nil {
						t.Error(err)
						http.Error(w, "invalid form", http.StatusBadRequest)
						return
					}
					verifier := r.PostForm.Get("code_verifier")
					digest := sha256.Sum256([]byte(verifier))
					if mode != "disabled" && (len(verifier) != 43 || base64.RawURLEncoding.EncodeToString(digest[:]) != challenge) {
						t.Error("token verifier does not match the authorization challenge")
						http.Error(w, "invalid verifier", http.StatusBadRequest)
						return
					}
					if mode == "disabled" && r.PostForm.Has("code_verifier") {
						t.Error("disabled PKCE sent a verifier")
					}
					for key, want := range map[string]string{
						"client_id": "hister", "client_secret": "secret", "code": "authorization-code",
						"redirect_uri": cfg.BaseURL("/api/oauth/callback") + "?provider=oidc",
					} {
						if got := r.PostForm.Get(key); got != want {
							t.Errorf("token %s = %q, want %q", key, got, want)
						}
					}
					_, _ = fmt.Fprint(w, `{"access_token":"token"}`)
				case "/userinfo":
					if r.Header.Get("Authorization") != "Bearer token" {
						t.Error("missing userinfo authorization")
					}
					_, _ = fmt.Fprint(w, `{"email":"alice@example.com","preferred_username":"alice"}`)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(provider.Close)
			entry.AuthURL = provider.URL + "/authorize"
			entry.TokenURL = provider.URL + "/token"
			entry.UserInfoURL = provider.URL + "/userinfo"
			if mode == "discovery" {
				entry.ConfigurationURL = provider.URL + "/discovery"
			}

			// A second login must reuse the account and replace the existing session.
			for attempt := range 2 {
				var query url.Values
				cookie, query = startOAuthLogin(t, handler, cookie)
				challenge = query.Get("code_challenge")
				values := oauthSessionValues(t, cookie)
				if values[oauthProviderKey] != "oidc" || values[oauthStateKey] != query.Get("state") || query.Get("state") == "" {
					t.Fatal("authorization request is not bound to the session")
				}
				if mode == "disabled" {
					if query.Has("code_challenge") || query.Has("code_challenge_method") {
						t.Fatal("disabled PKCE sent a challenge")
					}
				} else if challenge == "" || query.Get("code_challenge_method") != "S256" || values[oauthVerifierKey] == "" {
					t.Fatal("missing PKCE challenge or verifier")
				}
				if query.Has("code_verifier") || strings.Contains(cookie.String(), fmt.Sprint(values[oauthVerifierKey])) && mode != "disabled" {
					t.Fatal("verifier exposed to browser")
				}
				callback := url.Values{"provider": {"oidc"}, "state": {query.Get("state")}, "code": {"authorization-code"}}
				rec := oauthCallback(handler, cookie, callback)
				if rec.Code != http.StatusFound || rec.Header().Get("Location") != cfg.BaseURL("/") {
					t.Fatalf("callback status = %d, body = %s", rec.Code, rec.Body.String())
				}
				oldCookie := cookie
				cookie = responseSessionCookie(t, rec)
				if oldCookie.Value == cookie.Value {
					t.Fatal("login did not rotate the session")
				}
				assertOAuthFlowConsumed(t, cookie)
				user, err := model.GetUserByOAuthID("oidc-alice@example.com")
				if err != nil {
					t.Fatal(err)
				}
				if oauthSessionValues(t, cookie)["user_id"] != user.ID {
					t.Fatal("session does not authenticate the OAuth user")
				}
				if got := oauthCallback(handler, oldCookie, callback).Code; got != http.StatusBadRequest {
					t.Fatalf("replayed callback status = %d", got)
				}
				profile := func() int {
					req := httptest.NewRequest(http.MethodGet, "/hister/api/profile", nil)
					req.AddCookie(cookie)
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, req)
					return rec.Code
				}
				if got := profile(); got != http.StatusOK {
					t.Fatalf("fresh OAuth session profile status = %d, want 200", got)
				}
				if attempt == 0 {
					if err := model.UpdatePassword(user.Username, "resetpassword123"); err != nil {
						t.Fatal(err)
					}
					if got := profile(); got != http.StatusForbidden {
						t.Fatalf("OAuth session after password reset status = %d, want 403", got)
					}
				}
			}
			if tokenCalls.Load() != 2 {
				t.Fatalf("token requests = %d, want 2", tokenCalls.Load())
			}
			var count int64
			if err := model.DB.Model(&model.User{}).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("users = %d, error = %v", count, err)
			}
		})
	}
}

func TestOAuthCallbackFailures(t *testing.T) {
	for _, failure := range []string{"state", "provider", "cookie", "legacy session", "missing verifier", "empty verifier", "denied", "missing code", "token", "userinfo"} {
		t.Run(failure, func(t *testing.T) {
			cfg, handler := newOAuthTestServer(t)
			var tokenCalls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					tokenCalls.Add(1)
					if failure == "userinfo" {
						_, _ = fmt.Fprint(w, `{"access_token":"token"}`)
						return
					}
				}
				http.Error(w, "provider failure", http.StatusBadGateway)
			}))
			t.Cleanup(provider.Close)
			cfg.Server.OAuth["oidc"].TokenURL = provider.URL + "/token"
			cfg.Server.OAuth["oidc"].UserInfoURL = provider.URL + "/userinfo"
			cookie, query := startOAuthLogin(t, handler, nil, "/hister/preview?id=article")
			callback := url.Values{"provider": {"oidc"}, "state": {query.Get("state")}, "code": {"code"}}
			wantStatus := http.StatusBadRequest
			wantCalls := int32(0)
			consumed := true
			switch failure {
			case "state":
				callback.Set("state", "incorrect")
				consumed = false
			case "provider":
				callback.Set("provider", "google")
				consumed = false
			case "cookie":
				cookie = nil
				consumed = false
			case "legacy session", "missing verifier", "empty verifier":
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.AddCookie(cookie)
				session, err := sessionStore.Get(req, storeName)
				if err != nil {
					t.Fatal(err)
				}
				delete(session.Values, oauthVerifierKey)
				switch failure {
				case "empty verifier":
					session.Values[oauthVerifierKey] = ""
				case "legacy session":
					delete(session.Values, oauthProviderKey)
					consumed = false
				}
				if err := session.Save(req, httptest.NewRecorder()); err != nil {
					t.Fatal(err)
				}
			case "denied":
				callback.Set("error", "access_denied")
			case "missing code":
				callback.Del("code")
			case "token", "userinfo":
				wantStatus = http.StatusInternalServerError
				wantCalls = 1
			}
			if rec := oauthCallback(handler, cookie, callback); rec.Code != wantStatus {
				t.Fatalf("callback status = %d, want %d: %s", rec.Code, wantStatus, rec.Body.String())
			}
			if consumed {
				assertOAuthFlowConsumed(t, cookie)
				if got := oauthCallback(handler, cookie, callback).Code; got != http.StatusBadRequest {
					t.Fatalf("replayed callback status = %d", got)
				}
			}
			if got := tokenCalls.Load(); got != wantCalls {
				t.Fatalf("token requests = %d, want %d", got, wantCalls)
			}
		})
	}
}

func TestOAuthNewLoginReplacesPendingFlow(t *testing.T) {
	_, handler := newOAuthTestServer(t)
	cookie, first := startOAuthLogin(t, handler, nil, "/hister/preview?id=article")
	cookie, second := startOAuthLogin(t, handler, cookie)
	if first.Get("state") == second.Get("state") || first.Get("code_challenge") == second.Get("code_challenge") {
		t.Fatal("new login reused state or verifier")
	}
	callback := url.Values{"provider": {"oidc"}, "state": {first.Get("state")}, "code": {"code"}}
	if got := oauthCallback(handler, cookie, callback).Code; got != http.StatusBadRequest {
		t.Fatalf("replaced flow status = %d", got)
	}
	if oauthSessionValues(t, cookie)[oauthStateKey] != second.Get("state") {
		t.Fatal("invalid callback consumed the current flow")
	}
	if got := oauthSessionValues(t, cookie)[oauthReturnToKey]; got != "/hister/" {
		t.Fatalf("new login retained the previous return destination: %v", got)
	}
}

func TestOAuthIdentityClaimFlow(t *testing.T) {
	for _, discovery := range []bool{false, true} {
		for _, tt := range []struct {
			name         string
			bodies       []string
			wantUsername string
			existing     *model.User
			wantError    bool
		}{
			{
				name: "email changes preserve account",
				bodies: []string{
					`{"sub":"s1","email":"before@example.com","preferred_username":"alice"}`,
					`{"sub":"s1","email":"after@example.com","preferred_username":"alice-renamed"}`,
				},
				wantUsername: "alice",
			},
			{name: "email username", bodies: []string{`{"sub":"s1","email":"alice@example.com"}`}, wantUsername: "alice@example.com"},
			{name: "identity username", bodies: []string{`{"sub":"s1"}`}, wantUsername: "oidc:sub:s1"},
			{
				name: "username collision", bodies: []string{`{"sub":"s1","preferred_username":"alice"}`},
				wantUsername: "alice-c:sub:s1", existing: &model.User{Username: "alice", OAuthID: "other-identity"},
			},
			{
				name: "identity username collision", bodies: []string{`{"sub":"s1"}`},
				wantUsername: "oidc:sub:s1-c:sub:s1", existing: &model.User{Username: "oidc:sub:s1", OAuthID: "other-identity"},
			},
			{
				name: "legacy identity remains separate", bodies: []string{`{"sub":"s1","preferred_username":"alice"}`},
				wantUsername: "alice", existing: &model.User{Username: "legacy", OAuthID: "oidc-s1"},
			},
			{
				name: "other claim remains separate", bodies: []string{`{"sub":"s1","preferred_username":"alice"}`},
				wantUsername: "alice", existing: &model.User{Username: "other", OAuthID: "oidc:account_id:s1"},
			},
			{
				name: "email never links existing account", bodies: []string{`{"sub":"s1","email":"alice@example.com"}`},
				wantUsername: "alice@example.com", existing: &model.User{Username: "legacy", OAuthID: "oidc-alice@example.com"},
			},
			{
				name: "missing identity never authenticates by email", bodies: []string{`{"email":"alice@example.com"}`},
				existing: &model.User{Username: "legacy", OAuthID: "oidc-alice@example.com"}, wantError: true,
			},
		} {
			t.Run(fmt.Sprintf("%s/discovery=%t", tt.name, discovery), func(t *testing.T) {
				cfg, handler := newOAuthTestServer(t)
				entry := cfg.Server.OAuth["oidc"]
				entry.IdentityClaim = "sub"
				if tt.existing != nil {
					if _, err := model.CreateOAuthUser(tt.existing.Username, tt.existing.OAuthID); err != nil {
						t.Fatal(err)
					}
				}
				var userInfoCalls atomic.Int32
				provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/discovery":
						_ = json.NewEncoder(w).Encode(map[string]any{
							"authorization_endpoint": entry.AuthURL, "token_endpoint": entry.TokenURL,
							"userinfo_endpoint": entry.UserInfoURL, "scopes_supported": []string{"openid"},
							"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code"},
						})
					case "/token":
						_, _ = fmt.Fprint(w, `{"access_token":"token"}`)
					case "/userinfo":
						_, _ = fmt.Fprint(w, tt.bodies[int(userInfoCalls.Add(1))-1])
					default:
						http.NotFound(w, r)
					}
				}))
				t.Cleanup(provider.Close)
				entry.TokenURL = provider.URL + "/token"
				entry.UserInfoURL = provider.URL + "/userinfo"
				if discovery {
					entry.ConfigurationURL = provider.URL + "/discovery"
				}
				var firstUserID uint
				for range tt.bodies {
					// Each login starts without an authenticated session.
					cookie, query := startOAuthLogin(t, handler, nil)
					callback := url.Values{"provider": {"oidc"}, "state": {query.Get("state")}, "code": {"code"}}
					rec := oauthCallback(handler, cookie, callback)
					if tt.wantError {
						if rec.Code != http.StatusInternalServerError {
							t.Fatalf("callback status = %d, want 500", rec.Code)
						}
						assertOAuthFlowConsumed(t, cookie)
						if oauthSessionValues(t, cookie)["user_id"] != nil {
							t.Fatal("invalid identity authenticated a user")
						}
						continue
					}
					if rec.Code != http.StatusFound {
						t.Fatalf("callback status = %d: %s", rec.Code, rec.Body.String())
					}
					user, err := model.GetUserByOAuthID("oidc:sub:s1")
					if err != nil {
						t.Fatal(err)
					}
					if user.Username != tt.wantUsername {
						t.Fatalf("username = %q, want %q", user.Username, tt.wantUsername)
					}
					if firstUserID != 0 && user.ID != firstUserID {
						t.Fatal("email change created a different user")
					}
					firstUserID = user.ID
					if oauthSessionValues(t, responseSessionCookie(t, rec))["user_id"] != user.ID {
						t.Fatal("session does not authenticate the selected identity")
					}
				}
				wantCount := int64(1)
				if tt.existing != nil {
					wantCount++
				}
				if tt.wantError {
					wantCount--
				}
				var count int64
				if err := model.DB.Model(&model.User{}).Count(&count).Error; err != nil || count != wantCount {
					t.Fatalf("users = %d, want %d; error = %v", count, wantCount, err)
				}
			})
		}
	}
}
