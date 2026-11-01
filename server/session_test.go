// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"
)

func TestSessionCookieSecurityFollowsBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		secure  bool
	}{
		{name: "HTTP", baseURL: "http://127.0.0.1:4433", secure: false},
		{name: "HTTPS", baseURL: "https://hister.example", secure: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newSessionStore([]byte(strings.Repeat("x", 32)), tt.baseURL, sessionMaxAge)
			if store.Options.Secure != tt.secure {
				t.Fatalf("Secure = %v, want %v", store.Options.Secure, tt.secure)
			}
			if !store.Options.HttpOnly {
				t.Fatal("HttpOnly = false, want true")
			}
			if store.Options.SameSite != http.SameSiteLaxMode {
				t.Fatalf("SameSite = %v, want %v", store.Options.SameSite, http.SameSiteLaxMode)
			}
		})
	}
}

func TestTokenSessionRotationAndLogoutRevokeSession(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	firstCookie := tokenLoginCookie(t, handler, nil)
	secondCookie := tokenLoginCookie(t, handler, firstCookie)
	if firstCookie.Value == secondCookie.Value {
		t.Fatal("token login did not rotate the session identifier")
	}
	assertSessionRejected(t, handler, firstCookie)
	assertSessionAccepted(t, handler, secondCookie)

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	logoutReq.Header.Set("Origin", "hister://")
	logoutReq.AddCookie(secondCookie)
	logoutRec := httptest.NewRecorder()
	handler.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("POST /api/logout status = %d, want %d", logoutRec.Code, http.StatusOK)
	}
	var expired bool
	for _, cookie := range logoutRec.Result().Cookies() {
		if cookie.Name == storeName && cookie.MaxAge < 0 {
			expired = true
		}
	}
	if !expired {
		t.Fatal("logout did not expire the session cookie")
	}
	if _, err := model.GetWebSession(sessionTokenHash(secondCookie.Value)); !errors.Is(err, model.ErrWebSessionNotFound) {
		t.Fatalf("logged out session lookup error = %v, want %v", err, model.ErrWebSessionNotFound)
	}
	assertSessionRejected(t, handler, secondCookie)
}

func TestTokenSessionExpirationRefreshesOnValidRequest(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	cookie := tokenLoginCookie(t, handler, nil)
	shortExpiry := time.Now().Add(time.Hour)
	if err := model.DB.Model(&model.WebSession{}).
		Where("token_hash = ?", sessionTokenHash(cookie.Value)).
		Update("expires_at", shortExpiry).Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session status = %d, want %d", rec.Code, http.StatusOK)
	}
	refreshedCookie := responseSessionCookie(t, rec)
	if refreshedCookie.Value != cookie.Value {
		t.Fatal("session refresh changed the session identifier")
	}
	if refreshedCookie.MaxAge != sessionMaxAge {
		t.Fatalf("refreshed cookie MaxAge = %d, want %d", refreshedCookie.MaxAge, sessionMaxAge)
	}
	minimumCookieExpiry := time.Now().Add(time.Duration(sessionMaxAge)*time.Second - time.Minute)
	if !refreshedCookie.Expires.After(minimumCookieExpiry) {
		t.Fatalf("refreshed cookie expiry = %v, want after %v", refreshedCookie.Expires, minimumCookieExpiry)
	}
	storedSession, err := model.GetWebSession(sessionTokenHash(cookie.Value))
	if err != nil {
		t.Fatal(err)
	}
	minimumExpiry := time.Now().Add(time.Duration(sessionMaxAge)*time.Second - time.Minute)
	if !storedSession.ExpiresAt.After(minimumExpiry) {
		t.Fatalf("refreshed expiry = %v, want after %v", storedSession.ExpiresAt, minimumExpiry)
	}
	if !storedSession.ExpiresAt.After(shortExpiry) {
		t.Fatalf("refreshed expiry = %v, want after previous expiry %v", storedSession.ExpiresAt, shortExpiry)
	}
}

func TestExpiredTokenSessionIsNotRefreshed(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	cookie := tokenLoginCookie(t, handler, nil)
	if err := model.DB.Model(&model.WebSession{}).
		Where("token_hash = ?", sessionTokenHash(cookie.Value)).
		Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}

	assertSessionRejected(t, handler, cookie)
	if _, err := model.GetWebSession(sessionTokenHash(cookie.Value)); !errors.Is(err, model.ErrWebSessionNotFound) {
		t.Fatalf("expired session lookup error = %v, want %v", err, model.ErrWebSessionNotFound)
	}
}

func TestTokenChangeInvalidatesBrowserSession(t *testing.T) {
	cfg, handler := newTokenTestServer(t, false)
	cookie := tokenLoginCookie(t, handler, nil)
	cfg.App.AccessToken = "replacement"
	assertSessionRejected(t, handler, cookie)
}

func TestHeaderTokenDoesNotCreateBrowserSession(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/profile", nil, map[string]string{
		"Origin":         "hister://",
		"X-Access-Token": "secret",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/profile status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatalf("header token response set cookies: %v", rec.Result().Cookies())
	}
	var count int64
	if err := model.DB.Model(&model.WebSession{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("web session count = %d, want 0", count)
	}
}

func TestTokenLoginAcceptsCSRFTokenFromConfigResponse(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	origin := "https://proxy.example"

	missingCSRFReq := httptest.NewRequest(http.MethodPost, "/api/token-login", strings.NewReader(`{"token":"secret"}`))
	missingCSRFReq.Header.Set("Content-Type", "application/json")
	missingCSRFReq.Header.Set("Origin", origin)
	missingCSRFRec := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFRec, missingCSRFReq)
	if missingCSRFRec.Code != http.StatusForbidden {
		t.Fatalf("POST /api/token-login without CSRF token status = %d, want %d", missingCSRFRec.Code, http.StatusForbidden)
	}

	configReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	configReq.Header.Set("Origin", origin)
	configRec := httptest.NewRecorder()
	handler.ServeHTTP(configRec, configReq)
	if configRec.Code != http.StatusOK {
		t.Fatalf("GET /api/config status = %d, want %d", configRec.Code, http.StatusOK)
	}
	csrfToken := configRec.Header().Get("X-CSRF-Token")
	if csrfToken == "" {
		t.Fatal("GET /api/config did not return a CSRF token")
	}
	csrfCookie := responseSessionCookie(t, configRec)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/token-login", strings.NewReader(`{"token":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("Origin", origin)
	loginReq.Header.Set("X-CSRF-Token", csrfToken)
	loginReq.AddCookie(csrfCookie)
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("POST /api/token-login status = %d, want %d; body=%s", loginRec.Code, http.StatusOK, loginRec.Body.String())
	}
	assertSessionAccepted(t, handler, responseSessionCookie(t, loginRec))
}

func TestUserSessionStoresAccountReferenceAndRejectsDeletedUser(t *testing.T) {
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
	user := testutil.CreateUser(t, "alice")
	handler := registerEndpoints(cfg, newServerTestIndexer(t, cfg))

	loginReq := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"alice","password":"password123"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("Origin", "hister://")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("POST /api/login status = %d, want %d", loginRec.Code, http.StatusOK)
	}
	cookie := responseSessionCookie(t, loginRec)
	storedSession, err := model.GetWebSession(sessionTokenHash(cookie.Value))
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[any]any)
	if err := decodeSessionValues(storedSession.Data, &values); err != nil {
		t.Fatal(err)
	}
	if values["user_id"] != user.ID {
		t.Fatalf("stored user_id = %v, want %d", values["user_id"], user.ID)
	}
	if _, ok := values["username"]; ok {
		t.Fatal("server side session stores the username")
	}

	profileReq := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	profileReq.AddCookie(cookie)
	profileRec := httptest.NewRecorder()
	handler.ServeHTTP(profileRec, profileReq)
	if profileRec.Code != http.StatusOK {
		t.Fatalf("GET /api/profile status = %d, want %d", profileRec.Code, http.StatusOK)
	}
	var profile struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(profileRec.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.Username != user.Username {
		t.Fatalf("profile username = %q, want %q", profile.Username, user.Username)
	}

	if err := model.DeleteUser(user.Username); err != nil {
		t.Fatal(err)
	}
	deletedReq := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	deletedReq.AddCookie(cookie)
	deletedRec := httptest.NewRecorder()
	handler.ServeHTTP(deletedRec, deletedReq)
	if deletedRec.Code != http.StatusForbidden {
		t.Fatalf("deleted user GET /api/profile status = %d, want %d", deletedRec.Code, http.StatusForbidden)
	}
}

func TestPasswordResetRevokesUserSessions(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	alice := testutil.CreateUser(t, "alice")
	login := func(username, password string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]string{"username": username, "password": password})
		if err != nil {
			t.Fatal(err)
		}
		return testutil.ServeHTTP(t, handler, http.MethodPost, "/api/login", strings.NewReader(string(body)), map[string]string{"Origin": "hister://", "Content-Type": "application/json"})
	}
	profile := func(cookie *http.Cookie, want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
		req.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("session profile status = %d, want %d: %s", response.Code, want, response.Body)
		}
	}
	first := responseSessionCookie(t, login("alice", "password123"))
	second := responseSessionCookie(t, login("alice", "password123"))
	other := responseSessionCookie(t, login("admin", "password123"))
	profile(first, http.StatusOK)
	profile(second, http.StatusOK)

	// Simulate a request that loaded the session before the password reset.
	staleRequest := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	staleRequest.AddCookie(first)
	staleSession, err := sessionStore.Get(staleRequest, storeName)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.UpdatePassword("alice", "newpassword123"); err != nil {
		t.Fatal(err)
	}
	if err := staleSession.Save(staleRequest, httptest.NewRecorder()); err != nil {
		t.Fatal(err)
	}
	profile(first, http.StatusForbidden)
	profile(second, http.StatusForbidden)
	profile(other, http.StatusOK)
	if response := login("alice", "password123"); response.Code != http.StatusUnauthorized {
		t.Fatalf("old password login = %d, want 401", response.Code)
	}
	newLogin := login("alice", "newpassword123")
	if newLogin.Code != http.StatusOK {
		t.Fatalf("new password login = %d: %s", newLogin.Code, newLogin.Body)
	}
	fresh := responseSessionCookie(t, newLogin)
	profile(fresh, http.StatusOK)
	// Password resets do not rotate personal access tokens.
	for _, token := range []string{alice.Token, admin.Token} {
		response := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/profile", nil, map[string]string{"Authorization": "Bearer " + token})
		if response.Code != http.StatusOK {
			t.Fatalf("personal token profile = %d, want 200", response.Code)
		}
	}
	// Resetting to the same password must still revoke current sessions.
	if err := model.UpdatePassword("alice", "newpassword123"); err != nil {
		t.Fatal(err)
	}
	profile(fresh, http.StatusForbidden)
}

func TestPasswordResetRevokesLegacyUserSession(t *testing.T) {
	_, handler, _, _ := newUserDeletionTestServer(t)
	alice := testutil.CreateUser(t, "alice")
	// Sessions saved by earlier versions contain the account ID without a version.
	data, err := encodeSessionValues(map[any]any{"user_id": alice.ID})
	if err != nil {
		t.Fatal(err)
	}
	token, err := newSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := model.CreateWebSession(&model.WebSession{TokenHash: sessionTokenHash(token), Data: data, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for _, reset := range []bool{false, true} {
		want := http.StatusOK
		if reset {
			if err := model.UpdatePassword("alice", "newpassword123"); err != nil {
				t.Fatal(err)
			}
			want = http.StatusForbidden
		}
		req := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
		req.AddCookie(&http.Cookie{Name: storeName, Value: token})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("legacy session after reset = %t: status = %d, want %d", reset, response.Code, want)
		}
	}
}

func TestFailedPasswordResetPreservesSession(t *testing.T) {
	_, handler, _, _ := newUserDeletionTestServer(t)
	testutil.CreateUser(t, "alice")
	login := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/login", strings.NewReader(`{"username":"alice","password":"password123"}`), map[string]string{"Origin": "hister://"})
	cookie := responseSessionCookie(t, login)
	for _, password := range []string{"", strings.Repeat("x", 73)} {
		if err := model.UpdatePassword("alice", password); err == nil {
			t.Fatal("invalid password reset succeeded")
		}
	}
	// The password and session invalidation must succeed or fail together.
	if err := model.DB.Exec(`CREATE TRIGGER reject_password_reset BEFORE UPDATE ON users
		WHEN NEW.password != OLD.password BEGIN SELECT RAISE(ABORT, 'password update rejected'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.UpdatePassword("alice", "newpassword123"); err == nil {
		t.Fatal("database rejected the update but password reset succeeded")
	}
	if _, err := model.AuthenticateUser("alice", "password123"); err != nil {
		t.Fatalf("failed reset changed the password: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	req.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("failed reset revoked the session: status = %d", response.Code)
	}
}

func newUserDeletionTestServer(t *testing.T) (*config.Config, http.Handler, *indexer.Indexer, *model.User) {
	t.Helper()
	cfg := testutil.ModelConfig(t)
	cfg.App.UserHandling = true
	cfg.App.Public = true
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
	idx := newServerTestIndexer(t, cfg)
	return cfg, registerEndpoints(cfg, idx), idx, admin
}

func TestRemovedDeleteUserEndpoint(t *testing.T) {
	_, handler, idx, admin := newUserDeletionTestServer(t)
	alice := testutil.CreateUser(t, "alice")
	if err := idx.Add(&document.Document{URL: "https://example.com/private", UserID: alice.ID, Text: "Private", Processed: true}); err != nil {
		t.Fatal(err)
	}
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/delete_user", strings.NewReader(`{"username":"alice","purge":true}`), map[string]string{
		"Authorization": "Bearer " + admin.Token, "Origin": "hister://", "Content-Type": "application/json",
	})
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("removed endpoint status = %d, want 405: %s", response.Code, response.Body)
	}
	if user, err := model.GetUserByID(alice.ID); err != nil || *user != *alice {
		t.Fatalf("removed endpoint changed account: %v", err)
	}
	if count, err := idx.CountByQuery("*", &alice.ID); err != nil || count != 1 {
		t.Fatalf("removed endpoint changed documents: count = %d, error = %v", count, err)
	}
	response = testutil.ServeHTTP(t, handler, http.MethodGet, "/api", nil, nil)
	var endpoints []struct{ Method, Path string }
	if err := json.Unmarshal(response.Body.Bytes(), &endpoints); err != nil || response.Code != http.StatusOK {
		t.Fatalf("API documentation: %d %s, error = %v", response.Code, response.Body, err)
	}
	foundDeletion := false
	for _, endpoint := range endpoints {
		if endpoint.Path == "/api/delete_user" {
			t.Error("API documentation still lists the removed endpoint")
		}
		if endpoint.Method == http.MethodDelete && endpoint.Path == "/api/users/{id}" {
			foundDeletion = true
		}
	}
	if !foundDeletion {
		t.Error("API documentation is missing deletion by ID")
	}
}

func TestDeleteUserByIDAPI(t *testing.T) {
	cfg, _, idx, admin := newUserDeletionTestServer(t)
	if err := cfg.UpdateBaseURL("http://127.0.0.1:4433/hister"); err != nil {
		t.Fatal(err)
	}
	handler := registerEndpoints(cfg, idx)
	alice := testutil.CreateUser(t, "alice")
	bob := testutil.CreateUser(t, "bob")
	const sharedURL = "https://example.com/shared"
	for _, d := range []*document.Document{
		{URL: sharedURL, UserID: alice.ID, Text: "Alice", Processed: true},
		{URL: "https://example.com/private", UserID: alice.ID, Text: "Private", Processed: true},
		{URL: sharedURL, UserID: bob.ID, Text: "Bob", Processed: true},
		{URL: sharedURL, Text: "Global", Processed: true},
	} {
		if err := idx.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	for _, user := range []*model.User{alice, bob} {
		if err := model.UpdateHistory(user.ID, "test", sharedURL, user.Username); err != nil {
			t.Fatal(err)
		}
	}
	login := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/login", strings.NewReader(`{"username":"alice","password":"password123"}`), map[string]string{"Origin": "hister://"})
	cookie := responseSessionCookie(t, login)
	path := fmt.Sprintf("/hister/api/users/%d", alice.ID)
	headers := map[string]string{"Authorization": "Bearer " + admin.Token, "Origin": "hister://"}
	for _, query := range []string{"", "?purge=false"} {
		response := testutil.ServeHTTP(t, handler, http.MethodDelete, path+query, nil, headers)
		assertUserDeletionError(t, response, http.StatusConflict, "purge_required")
		if _, err := model.GetUserByID(alice.ID); err != nil {
			t.Fatalf("refused deletion removed account: %v", err)
		}
		if count, err := idx.CountByQuery("*", &alice.ID); err != nil || count != 2 {
			t.Fatalf("refused deletion changed documents: count = %d, error = %v", count, err)
		}
	}
	// The selected ID remains the target after a rename and reuse of its old name.
	name := "renamed"
	if _, err := model.UpdateUser(admin.ID, alice.ID, alice.Revision, model.UserChanges{Username: &name}); err != nil {
		t.Fatal(err)
	}
	replacement := testutil.CreateUser(t, "alice")
	response := testutil.ServeHTTP(t, handler, http.MethodDelete, path+"?purge=true", nil, headers)
	var result struct {
		ID               uint   `json:"id"`
		Username         string `json:"username"`
		DeletedDocuments int    `json:"deleted_documents"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || result.ID != alice.ID || result.Username != name || result.DeletedDocuments != 2 {
		t.Fatalf("delete response: %d %s, error = %v", response.Code, response.Body, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil || len(fields) != 3 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected response fields or caching: %s, error = %v", response.Body, err)
	}
	if _, err := model.GetUserByID(alice.ID); !errors.Is(err, model.ErrUserNotFound) {
		t.Fatalf("deleted account lookup = %v", err)
	}
	if count, err := idx.CountByQuery("*", &alice.ID); err != nil || count != 0 {
		t.Fatalf("owned documents remain: count = %d, error = %v", count, err)
	}
	if idx.GetByURLAndUser(sharedURL, bob.ID) == nil || idx.GetByURLAndUser(sharedURL, 0) == nil {
		t.Fatal("purge removed another owner's document")
	}
	for _, user := range []*model.User{admin, bob, replacement} {
		if _, err := model.GetUserByID(user.ID); err != nil {
			t.Fatalf("purge removed another account: %v", err)
		}
	}
	for _, user := range []*model.User{alice, bob} {
		want := 0
		if user.ID == bob.ID {
			want = 1
		}
		if history, err := model.GetURLsByQuery(user.ID, "test"); err != nil || len(history) != want {
			t.Fatalf("history for user %d: %v, error = %v", user.ID, history, err)
		}
	}
	for _, tokenAuth := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodGet, "/hister/api/profile", nil)
		if tokenAuth {
			req.Header.Set("Authorization", "Bearer "+alice.Token)
		} else {
			req.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusForbidden {
			t.Errorf("deleted account still authenticated: token = %t, status = %d", tokenAuth, response.Code)
		}
	}
	response = testutil.ServeHTTP(t, handler, http.MethodDelete, path+"?purge=true", nil, headers)
	assertUserDeletionError(t, response, http.StatusNotFound, "user_not_found")
}

func TestDeleteUserByIDAPIValidation(t *testing.T) {
	_, handler, idx, admin := newUserDeletionTestServer(t)
	alice := testutil.CreateUser(t, "alice")
	if err := idx.Add(&document.Document{URL: "https://example.com/private", UserID: alice.ID, Text: "Private", Processed: true}); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/users/%d", alice.ID)
	for _, tc := range []struct {
		path, code string
		status     int
	}{
		{"/api/users/0", "invalid_request", http.StatusBadRequest},
		{"/api/users/-1", "invalid_request", http.StatusBadRequest},
		{"/api/users/abc", "invalid_request", http.StatusBadRequest},
		{"/api/users/18446744073709551616", "invalid_request", http.StatusBadRequest},
		{"/api/users/999999", "user_not_found", http.StatusNotFound},
		{path + "?purge=", "invalid_request", http.StatusBadRequest},
		{path + "?purge=yes", "invalid_request", http.StatusBadRequest},
		{path + "?purge=true&purge=false", "invalid_request", http.StatusBadRequest},
		{path + "?purge=true&purge=true", "invalid_request", http.StatusBadRequest},
		{path + "?purge=true&username=admin", "invalid_request", http.StatusBadRequest},
		{path + "?purge=true&bad=%zz", "invalid_request", http.StatusBadRequest},
		{path + "?purge=true;ignored=false", "invalid_request", http.StatusBadRequest},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := testutil.ServeHTTP(t, handler, http.MethodDelete, tc.path, nil, map[string]string{"X-Access-Token": admin.Token, "Origin": "hister://"})
			assertUserDeletionError(t, response, tc.status, tc.code)
			if user, err := model.GetUserByID(alice.ID); err != nil || *user != *alice {
				t.Fatalf("invalid request changed account: %v", err)
			}
			if count, err := idx.CountByQuery("*", &alice.ID); err != nil || count != 1 {
				t.Fatalf("invalid request changed documents: count = %d, error = %v", count, err)
			}
		})
	}
}

func TestDeleteUserByIDAPIAuthorization(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	alice := testutil.CreateUser(t, "alice")
	bob := testutil.CreateUser(t, "bob")
	path := fmt.Sprintf("/api/users/%d", alice.ID)
	for _, tc := range []struct{ token, origin string }{
		{"", "hister://"},
		{"invalid", "hister://"},
		{alice.Token, "hister://"},
		{bob.Token, "hister://"},
		{admin.Token, "https://other.example"},
	} {
		response := testutil.ServeHTTP(t, handler, http.MethodDelete, path+"?purge=true", nil, map[string]string{"Authorization": "Bearer " + tc.token, "Origin": tc.origin})
		if response.Code != http.StatusForbidden {
			t.Fatalf("unauthorized deletion: %d %s", response.Code, response.Body)
		}
		if _, err := model.GetUserByID(alice.ID); err != nil {
			t.Fatalf("unauthorized request deleted account: %v", err)
		}
	}
	login := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"password123"}`), map[string]string{"Origin": "hister://"})
	cookie := responseSessionCookie(t, login)
	for _, csrf := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodDelete, path, nil)
		req.Header.Set("Origin", "https://proxy.example")
		req.AddCookie(cookie)
		if csrf {
			configReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
			configReq.Header.Set("Origin", "https://proxy.example")
			configReq.AddCookie(cookie)
			configRec := httptest.NewRecorder()
			handler.ServeHTTP(configRec, configReq)
			req.Header.Set("X-CSRF-Token", configRec.Header().Get("X-CSRF-Token"))
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		want := http.StatusForbidden
		if csrf {
			want = http.StatusOK
		}
		if response.Code != want {
			t.Fatalf("deletion with CSRF token %t: %d %s", csrf, response.Code, response.Body)
		}
		_, err := model.GetUserByID(alice.ID)
		if csrf && !errors.Is(err, model.ErrUserNotFound) || !csrf && err != nil {
			t.Fatalf("deletion with CSRF token %t: account lookup = %v", csrf, err)
		}
		if csrf && !strings.Contains(response.Body.String(), `"deleted_documents":0`) {
			t.Fatalf("empty account deletion response: %s", response.Body)
		}
	}
}

func TestDeleteUserByIDAPILastAdmin(t *testing.T) {
	_, handler, idx, admin := newUserDeletionTestServer(t)
	if err := idx.Add(&document.Document{URL: "https://example.com/admin", UserID: admin.ID, Text: "Admin", Processed: true}); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/users/%d?purge=true", admin.ID)
	headers := map[string]string{"Authorization": "Bearer " + admin.Token, "Origin": "hister://"}
	response := testutil.ServeHTTP(t, handler, http.MethodDelete, path, nil, headers)
	assertUserDeletionError(t, response, http.StatusConflict, "last_admin")
	if user, err := model.GetUserByID(admin.ID); err != nil || *user != *admin {
		t.Fatalf("last administrator changed: %v", err)
	}
	if count, err := idx.CountByQuery("*", &admin.ID); err != nil || count != 1 {
		t.Fatalf("last administrator deletion purged documents: count = %d, error = %v", count, err)
	}
	if _, err := model.CreateUser("other-admin", "password123", true); err != nil {
		t.Fatal(err)
	}
	response = testutil.ServeHTTP(t, handler, http.MethodDelete, path, nil, headers)
	if response.Code != http.StatusOK {
		t.Fatalf("administrator self deletion with another admin: %d %s", response.Code, response.Body)
	}
}

func TestDeleteUserByIDAPIRequiresUserHandling(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	alice := testutil.CreateUser(t, "alice")
	response := testutil.ServeHTTP(t, handler, http.MethodDelete, fmt.Sprintf("/api/users/%d?purge=true", alice.ID), nil, map[string]string{"X-Access-Token": "secret", "Origin": "hister://"})
	assertUserDeletionError(t, response, http.StatusBadRequest, "user_handling_disabled")
	if _, err := model.GetUserByID(alice.ID); err != nil {
		t.Fatalf("disabled user handling request deleted account: %v", err)
	}
}

func TestDeleteUserByIDAPIFailures(t *testing.T) {
	for _, failure := range []string{"index", "database"} {
		t.Run(failure, func(t *testing.T) {
			_, handler, idx, admin := newUserDeletionTestServer(t)
			alice := testutil.CreateUser(t, "alice")
			if failure == "index" {
				idx.Close()
			} else if err := model.DB.Exec(`CREATE TRIGGER reject_user_delete BEFORE DELETE ON users BEGIN SELECT RAISE(ABORT, 'private database error'); END`).Error; err != nil {
				t.Fatal(err)
			}
			for _, query := range []string{"", "?purge=true"} {
				response := testutil.ServeHTTP(t, handler, http.MethodDelete, fmt.Sprintf("/api/users/%d%s", alice.ID, query), nil, map[string]string{"X-Access-Token": admin.Token, "Origin": "hister://"})
				assertUserDeletionError(t, response, http.StatusInternalServerError, "internal_error")
				if strings.Contains(response.Body.String(), "private database error") {
					t.Fatal("failed deletion exposed private database details")
				}
				if user, err := model.GetUserByID(alice.ID); err != nil || *user != *alice {
					t.Fatalf("failed deletion changed account: %v", err)
				}
			}
		})
	}
}

func assertUserDeletionError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var result map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != status || result["code"] != code || result["error"] == "" {
		t.Fatalf("deletion response: %d %s, want %d %s, decode error = %v", response.Code, response.Body, status, code, err)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("deletion response is missing Cache-Control: no-store")
	}
}

func TestUserReadAPI(t *testing.T) {
	cfg, _, idx, admin := newUserDeletionTestServer(t)
	if err := cfg.UpdateBaseURL("http://127.0.0.1:4433/hister"); err != nil {
		t.Fatal(err)
	}
	handler := registerEndpoints(cfg, idx)
	alice := testutil.CreateUser(t, "alice")
	bob := testutil.CreateUser(t, "bob")
	literal := testutil.CreateUser(t, "a%_' OR 1=1 --")
	deleted := testutil.CreateUser(t, "deleted")
	if err := model.DeleteUserByID(deleted.ID); err != nil {
		t.Fatal(err)
	}
	request := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		response := testutil.ServeHTTP(t, handler, http.MethodGet, "/hister"+path, nil, map[string]string{"Authorization": "Bearer " + admin.Token})
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, response.Code, response.Body)
		}
		return response
	}
	checkUser := func(data map[string]json.RawMessage, user *model.User) {
		t.Helper()
		if len(data) != 6 || string(data["revision"]) != "1" {
			t.Fatalf("user fields = %v, want only id, username, is_admin, created_at, updated_at, revision", data)
		}
		var got struct {
			ID        uint      `json:"id"`
			Username  string    `json:"username"`
			IsAdmin   bool      `json:"is_admin"`
			CreatedAt time.Time `json:"created_at"`
			UpdatedAt time.Time `json:"updated_at"`
		}
		encoded, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &got); err != nil {
			t.Fatal(err)
		}
		if got.ID != user.ID || got.Username != user.Username || got.IsAdmin != user.IsAdmin || !got.CreatedAt.Equal(user.CreatedAt) || !got.UpdatedAt.Equal(user.UpdatedAt) {
			t.Fatalf("user = %s, want metadata for %q", encoded, user.Username)
		}
	}
	var ids []uint
	path := "/api/users?limit=2"
	for page := 0; ; page++ {
		if page > 2 {
			t.Fatal("pagination did not terminate")
		}
		response := request(path)
		var result struct {
			Users       []map[string]json.RawMessage `json:"users"`
			NextAfterID uint                         `json:"next_after_id"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Users) != 2 {
			t.Fatalf("page %d has %d users, want 2", page, len(result.Users))
		}
		for _, user := range result.Users {
			var id uint
			if err := json.Unmarshal(user["id"], &id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		if result.NextAfterID == 0 {
			break
		}
		if result.NextAfterID != ids[len(ids)-1] {
			t.Fatalf("cursor %d does not point at last returned user", result.NextAfterID)
		}
		path = fmt.Sprintf("/api/users?limit=2&after_id=%d", result.NextAfterID)
	}
	if !slices.Equal(ids, []uint{admin.ID, alice.ID, bob.ID, literal.ID}) {
		t.Fatalf("listed IDs = %v", ids)
	}
	for _, user := range []*model.User{admin, alice, literal} {
		response := request("/api/users?username=" + url.QueryEscape(user.Username))
		var result struct {
			Users []map[string]json.RawMessage `json:"users"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || len(result.Users) != 1 {
			t.Fatalf("exact username lookup: %s, error = %v", response.Body, err)
		}
		checkUser(result.Users[0], user)
		response = request(fmt.Sprintf("/api/users/%d", user.ID))
		var details map[string]json.RawMessage
		if err := json.Unmarshal(response.Body.Bytes(), &details); err != nil {
			t.Fatal(err)
		}
		checkUser(details, user)
	}
	for _, query := range []string{"username=ali", "username=Alice", "username=missing", "username=deleted", "after_id=999999"} {
		response := request("/api/users?" + query)
		var result map[string]json.RawMessage
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if string(result["users"]) != "[]" || result["next_after_id"] != nil {
			t.Fatalf("empty page = %s", response.Body)
		}
	}
	response := request(fmt.Sprintf("/api/users/%d/token", alice.ID))
	var token map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &token); err != nil || len(token) != 1 || token["token"] != alice.Token {
		t.Fatalf("token response invalid, error = %v", err)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("token response may be cached")
	}
	unchanged, err := model.GetUserByID(alice.ID)
	if err != nil || unchanged.Token != alice.Token {
		t.Fatal("reading the token changed it")
	}
}

func TestUserReadAPIAuthorization(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	user := testutil.CreateUser(t, "alice")
	login := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"password123"}`), map[string]string{"Origin": "hister://"})
	if login.Code != http.StatusOK {
		t.Fatalf("admin login: %d %s", login.Code, login.Body)
	}
	cookie := responseSessionCookie(t, login)
	for _, path := range []string{"/api/users", fmt.Sprintf("/api/users/%d", user.ID), fmt.Sprintf("/api/users/%d/token", user.ID)} {
		for _, token := range []string{"", "invalid", user.Token, admin.Token} {
			response := testutil.ServeHTTP(t, handler, http.MethodGet, path, nil, map[string]string{"X-Access-Token": token})
			want := http.StatusForbidden
			if token == admin.Token {
				want = http.StatusOK
			}
			if response.Code != want {
				t.Errorf("GET %s: status = %d, want %d", path, response.Code, want)
			}
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Errorf("admin session GET %s: %d", path, response.Code)
		}
	}
}

func TestUserReadAPIValidation(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	for _, path := range []string{
		"/api/users?limit=0", "/api/users?limit=-1", "/api/users?limit=101", "/api/users?limit=x", "/api/users?limit=",
		"/api/users?after_id=-1", "/api/users?after_id=18446744073709551615", "/api/users?after_id=x",
		"/api/users?username=", "/api/users?username=%20", "/api/users?limit=1&limit=2", "/api/users?name=alice", "/api/users?username=%zz",
		"/api/users/0", "/api/users/-1", "/api/users/nope", "/api/users/18446744073709551615", "/api/users/nope/token",
		"/api/users/999999", "/api/users/999999/token",
	} {
		t.Run(path, func(t *testing.T) {
			response := testutil.ServeHTTP(t, handler, http.MethodGet, path, nil, map[string]string{"X-Access-Token": admin.Token})
			wantStatus, wantCode := http.StatusBadRequest, "invalid_request"
			if strings.Contains(path, "999999") {
				wantStatus, wantCode = http.StatusNotFound, "user_not_found"
			}
			var result struct {
				Code  string `json:"code"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != wantStatus || result.Code != wantCode || result.Error == "" {
				t.Fatalf("status = %d, body = %s, decode error = %v", response.Code, response.Body, err)
			}
		})
	}
}

func TestUserReadAPIRequiresUserHandling(t *testing.T) {
	_, handler := newTokenTestServer(t, true)
	user := testutil.CreateUser(t, "alice")
	for _, path := range []string{"/api/users", fmt.Sprintf("/api/users/%d", user.ID), fmt.Sprintf("/api/users/%d/token", user.ID)} {
		response := testutil.ServeHTTP(t, handler, http.MethodGet, path, nil, map[string]string{"X-Access-Token": "secret"})
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"user_handling_disabled"`) {
			t.Errorf("single user GET %s: %d %s", path, response.Code, response.Body)
		}
	}
}

func TestUserReadAPIPaginationLimits(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	for i := range 100 {
		name := fmt.Sprintf("user-%03d", i)
		if _, err := model.CreateOAuthUser(name, name); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		query string
		count int
	}{
		{"", 50},
		{"?limit=1", 1},
		{"?limit=100", 100},
	} {
		response := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/users"+tc.query, nil, map[string]string{"X-Access-Token": admin.Token})
		var result struct {
			Users []struct {
				ID uint `json:"id"`
			} `json:"users"`
			NextAfterID uint `json:"next_after_id"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || len(result.Users) != tc.count {
			t.Fatalf("page %s: status = %d, count = %d, error = %v", tc.query, response.Code, len(result.Users), err)
		}
		if result.NextAfterID != result.Users[len(result.Users)-1].ID {
			t.Fatalf("page %s: wrong cursor %d", tc.query, result.NextAfterID)
		}
	}
}

func TestUserReadAPIDatabaseErrors(t *testing.T) {
	cfg, _, _, _ := newUserDeletionTestServer(t)
	db, err := model.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// Call the handlers after authentication to isolate failures during the account lookup.
	for _, handler := range []endpointHandler{serveUsers, serveUser, serveUserToken} {
		req := httptest.NewRequest(http.MethodGet, "/api/users/1", nil)
		req.SetPathValue("id", "1")
		response := httptest.NewRecorder()
		handler(&webContext{Request: req, Response: response, Config: cfg})
		var result map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusInternalServerError || result["code"] != "internal_error" {
			t.Fatalf("database error: status = %d, body = %s, decode error = %v", response.Code, response.Body, err)
		}
		if strings.Contains(result["error"], "sql:") {
			t.Fatal("response disclosed the database error")
		}
	}
}

func TestCreateUserAPI(t *testing.T) {
	cfg, _, idx, admin := newUserDeletionTestServer(t)
	if err := cfg.UpdateBaseURL("http://127.0.0.1:4433/hister"); err != nil {
		t.Fatal(err)
	}
	handler := registerEndpoints(cfg, idx)
	for _, tc := range []struct {
		username string
		password string
		isAdmin  bool
	}{
		{"ál ice@example.org", "12345678", false},
		{"another-admin", strings.Repeat("é", 36), true},
	} {
		t.Run(tc.username, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"username": tc.username, "password": tc.password, "is_admin": tc.isAdmin})
			if err != nil {
				t.Fatal(err)
			}
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/users", strings.NewReader(string(body)), map[string]string{
				"Authorization": "Bearer " + admin.Token, "Origin": "hister://", "Content-Type": "application/json",
			})
			if response.Code != http.StatusCreated {
				t.Fatalf("create status = %d, want 201: %s", response.Code, response.Body)
			}
			user, err := model.AuthenticateUser(tc.username, tc.password)
			if err != nil {
				t.Fatalf("created account cannot authenticate: %v", err)
			}
			if user.IsAdmin != tc.isAdmin || user.Token == "" || user.Token == admin.Token || user.Password == tc.password {
				t.Fatal("created account has incorrect privileges or credentials")
			}
			var fields map[string]json.RawMessage
			var metadata userResponse
			if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(response.Body.Bytes(), &metadata); err != nil {
				t.Fatal(err)
			}
			if len(fields) != 6 || string(fields["revision"]) != "1" || metadata.ID != user.ID || metadata.Username != tc.username || metadata.IsAdmin != tc.isAdmin || !metadata.CreatedAt.Equal(user.CreatedAt) || !metadata.UpdatedAt.Equal(user.UpdatedAt) {
				t.Fatalf("response must contain only the account's metadata: %s", response.Body)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("creation response may be cached")
			}
			location, err := url.Parse(response.Header().Get("Location"))
			if err != nil || location.Path != fmt.Sprintf("/hister/api/users/%d", user.ID) {
				t.Fatalf("incorrect resource location: %q", response.Header().Get("Location"))
			}
			lookup := testutil.ServeHTTP(t, handler, http.MethodGet, location.RequestURI(), nil, map[string]string{"Authorization": "Bearer " + admin.Token})
			if lookup.Code != http.StatusOK || lookup.Body.String() != response.Body.String() {
				t.Fatalf("resource location does not return the created account: %d %s", lookup.Code, lookup.Body)
			}
		})
	}
}

func TestCreateUserAPIValidation(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	for _, tc := range []struct {
		name string
		body string
	}{
		{"missing body", ""},
		{"null body", `null`},
		{"array", `[]`},
		{"missing username", `{"password":"password123"}`},
		{"blank username", `{"username":" \t ","password":"password123"}`},
		{"control in username", `{"username":"ali\nce","password":"password123"}`},
		{"null username", `{"username":null,"password":"password123"}`},
		{"missing password", `{"username":"alice"}`},
		{"null password", `{"username":"alice","password":null}`},
		{"short password", `{"username":"alice","password":"1234567"}`},
		{"long password", `{"username":"alice","password":"` + strings.Repeat("x", 73) + `"}`},
		{"long UTF8 password", `{"username":"alice","password":"` + strings.Repeat("é", 37) + `"}`},
		{"invalid admin", `{"username":"alice","password":"password123","is_admin":"true"}`},
		{"null admin", `{"username":"alice","password":"password123","is_admin":null}`},
		{"unknown field", `{"username":"alice","password":"password123","token":"chosen-token"}`},
		{"trailing object", `{"username":"alice","password":"password123"} {}`},
		{"trailing null", `{"username":"alice","password":"password123"} null`},
		{"trailing garbage", `{"username":"alice","password":"password123"} secret`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/users", strings.NewReader(tc.body), map[string]string{"X-Access-Token": admin.Token, "Origin": "hister://"})
			var result map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusBadRequest || result["code"] != "invalid_request" || result["error"] == "" {
				t.Fatalf("invalid request: status = %d, body = %s, decode error = %v", response.Code, response.Body, err)
			}
			if strings.Contains(response.Body.String(), "password123") || strings.Contains(response.Body.String(), "chosen-token") {
				t.Fatal("validation response disclosed submitted credentials")
			}
		})
	}
	users, err := model.ListUsers("", 0, 100)
	if err != nil || len(users) != 1 || users[0].ID != admin.ID {
		t.Fatalf("invalid requests changed accounts: %v, error = %v", users, err)
	}
}

func TestCreateUserAPIDuplicate(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/users", strings.NewReader(`{"username":"admin","password":"replacement","is_admin":false}`), map[string]string{"X-Access-Token": admin.Token, "Origin": "hister://"})
	var result map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusConflict || result["code"] != "username_exists" {
		t.Fatalf("duplicate request: status = %d, body = %s, decode error = %v", response.Code, response.Body, err)
	}
	unchanged, err := model.AuthenticateUser("admin", "password123")
	if err != nil || !unchanged.IsAdmin || unchanged.Token != admin.Token {
		t.Fatalf("duplicate creation changed the existing account: %v", err)
	}
}

func TestCreateUserAPIAuthorization(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	user := testutil.CreateUser(t, "alice")
	for _, token := range []string{"", "invalid", user.Token} {
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/users", strings.NewReader(`{"username":"unauthorized","password":"password123","is_admin":true}`), map[string]string{"Authorization": "Bearer " + token, "Origin": "hister://"})
		if response.Code != http.StatusForbidden {
			t.Fatalf("unauthorized creation: status = %d, want 403: %s", response.Code, response.Body)
		}
	}
	if _, err := model.GetUser("unauthorized"); !errors.Is(err, model.ErrUserNotFound) {
		t.Fatalf("unauthorized creation changed accounts: %v", err)
	}
	// A session must pass CSRF validation before it can create even an ordinary account.
	login := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"password123"}`), map[string]string{"Origin": "hister://"})
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body)
	}
	cookie := responseSessionCookie(t, login)
	for _, withToken := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"username":"bob","password":"password123"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://proxy.example")
		req.AddCookie(cookie)
		if withToken {
			configReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
			configReq.Header.Set("Origin", "https://proxy.example")
			configReq.AddCookie(cookie)
			configRec := httptest.NewRecorder()
			handler.ServeHTTP(configRec, configReq)
			req.Header.Set("X-CSRF-Token", configRec.Header().Get("X-CSRF-Token"))
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		want := http.StatusForbidden
		if withToken {
			want = http.StatusCreated
		}
		if response.Code != want {
			t.Fatalf("session creation with CSRF token = %t: status = %d, want %d: %s", withToken, response.Code, want, response.Body)
		}
		bob, err := model.GetUser("bob")
		if !withToken && !errors.Is(err, model.ErrUserNotFound) {
			t.Fatalf("failed CSRF check changed accounts: %v", err)
		}
		if withToken && (err != nil || bob.IsAdmin || bob.Token == admin.Token) {
			t.Fatalf("creation without is_admin did not create an ordinary account: %v", err)
		}
	}
}

func TestCreateUserAPIRequiresUserHandling(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/users", strings.NewReader(`{"username":"alice","password":"password123","is_admin":true}`), map[string]string{"X-Access-Token": "secret", "Origin": "hister://"})
	var result map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusBadRequest || result["code"] != "user_handling_disabled" {
		t.Fatalf("single user creation: status = %d, body = %s, decode error = %v", response.Code, response.Body, err)
	}
	if _, err := model.GetUser("alice"); !errors.Is(err, model.ErrUserNotFound) {
		t.Fatalf("single user creation changed accounts: %v", err)
	}
}

func TestCreateUserAPIBodyLimit(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	for _, knownLength := range []bool{true, false} {
		// Trailing whitespace must still count toward the limit before an account is created.
		req := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"username":"alice","password":"password123"}`+strings.Repeat(" ", int(maxRequestBodyBytes))))
		req.Header.Set("X-Access-Token", admin.Token)
		req.Header.Set("Origin", "hister://")
		if !knownLength {
			req.ContentLength = -1
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		var result struct{ Code string }
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusRequestEntityTooLarge || result.Code != "request_too_large" {
			t.Fatalf("oversized request: status = %d, body = %s, decode error = %v", response.Code, response.Body, err)
		}
	}
	if _, err := model.GetUser("alice"); !errors.Is(err, model.ErrUserNotFound) {
		t.Fatalf("oversized request created an account: %v", err)
	}
}

func TestCreateUserAPIDatabaseError(t *testing.T) {
	cfg, _, _, _ := newUserDeletionTestServer(t)
	db, err := model.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// Isolate a database failure after administrator authentication has succeeded.
	for _, endpoint := range Endpoints {
		if endpoint.Path != "/api/users" || endpoint.Method != POST {
			continue
		}
		req := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"username":"alice","password":"password123"}`))
		response := httptest.NewRecorder()
		endpoint.Handler(&webContext{Request: req, Response: response, Config: cfg})
		var result map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusInternalServerError || result["code"] != "internal_error" {
			t.Fatalf("database failure: status = %d, body = %s, decode error = %v", response.Code, response.Body, err)
		}
		if strings.Contains(response.Body.String(), "sql:") || strings.Contains(response.Body.String(), "password123") {
			t.Fatal("creation response disclosed private details")
		}
		return
	}
	t.Fatal("user creation endpoint is missing")
}

func tokenLoginCookie(t *testing.T, handler http.Handler, current *http.Cookie) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/token-login", strings.NewReader(`{"token":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "hister://")
	if current != nil {
		req.AddCookie(current)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/token-login status = %d, want %d", rec.Code, http.StatusOK)
	}
	return responseSessionCookie(t, rec)
}

func responseSessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	var sessionCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == storeName {
			sessionCookie = cookie
		}
	}
	if sessionCookie != nil {
		return sessionCookie
	}
	t.Fatalf("response did not set %q cookie", storeName)
	return nil
}

func assertSessionAccepted(t *testing.T, handler http.Handler, cookie *http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func assertSessionRejected(t *testing.T, handler http.Handler, cookie *http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("session status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestUpdateUserAPI(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	alice := testutil.CreateUser(t, "alice")
	login := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/login", strings.NewReader(`{"username":"alice","password":"password123"}`), map[string]string{"Origin": "hister://"})
	cookie := responseSessionCookie(t, login)
	path := fmt.Sprintf("/api/users/%d", alice.ID)
	response := testutil.ServeHTTP(t, handler, http.MethodPatch, path, strings.NewReader(`{"revision":1,"username":"renamed","password":"replacement123","is_admin":true,"regenerate_token":true}`), map[string]string{"Authorization": "Bearer " + admin.Token, "Origin": "hister://"})
	if response.Code != http.StatusOK {
		t.Fatalf("update: %d %s", response.Code, response.Body)
	}
	var result struct {
		ID       uint
		Username string
		IsAdmin  bool `json:"is_admin"`
		Revision uint64
		Token    string
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != alice.ID || result.Username != "renamed" || !result.IsAdmin || result.Revision != 2 || result.Token == "" || result.Token == alice.Token {
		t.Fatalf("incorrect updated metadata or rotated token: %s", response.Body)
	}
	if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "replacement123") || strings.Contains(response.Body.String(), "password") {
		t.Fatalf("unsafe update response: %s", response.Body)
	}
	updated, err := model.AuthenticateUser("renamed", "replacement123")
	if err != nil || updated.Token != result.Token || !updated.IsAdmin || updated.RulesJSON != alice.RulesJSON {
		t.Fatalf("combined changes not persisted: %v", err)
	}
	if _, err := model.AuthenticateUser("renamed", "password123"); !errors.Is(err, model.ErrInvalidPassword) {
		t.Fatalf("old password still accepted: %v", err)
	}
	for _, token := range []string{alice.Token, result.Token} {
		profile := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/profile", nil, map[string]string{"Authorization": "Bearer " + token})
		want := http.StatusForbidden
		if token == result.Token {
			want = http.StatusOK
		}
		if profile.Code != want {
			t.Fatalf("token authentication = %d, want %d", profile.Code, want)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("password update preserved old session: %d", rec.Code)
	}

	// An explicit false value demotes, and omitted fields keep their values.
	response = testutil.ServeHTTP(t, handler, http.MethodPatch, path, strings.NewReader(`{"revision":2,"is_admin":false}`), map[string]string{"X-Access-Token": admin.Token, "Origin": "hister://"})
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil || response.Code != http.StatusOK || len(fields) != 6 || string(fields["revision"]) != "3" {
		t.Fatalf("partial update: %d %s, error %v", response.Code, response.Body, err)
	}
	unchanged, err := model.AuthenticateUser("renamed", "replacement123")
	if err != nil || unchanged.IsAdmin || unchanged.Token != result.Token {
		t.Fatalf("omitted fields changed: %v", err)
	}
}

func TestUpdateUserAPIRejectedChanges(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	alice := testutil.CreateUser(t, "alice")
	for _, tc := range []struct {
		body, code string
		status     int
	}{
		{`null`, "invalid_request", 400},
		{`[]`, "invalid_request", 400},
		{`{}`, "invalid_request", 400},
		{`{"username":"renamed"}`, "invalid_request", 400},
		{`{"revision":0,"username":"renamed"}`, "invalid_request", 400},
		{`{"revision":-1,"username":"renamed"}`, "invalid_request", 400},
		{`{"revision":9223372036854775808,"username":"renamed"}`, "invalid_request", 400},
		{`{"revision":1}`, "invalid_request", 400},
		{`{"revision":1,"regenerate_token":false}`, "invalid_request", 400},
		{`{"revision":1,"username":null}`, "invalid_request", 400},
		{`{"revision":1,"password":null}`, "invalid_request", 400},
		{`{"revision":1,"is_admin":null}`, "invalid_request", 400},
		{`{"revision":1,"regenerate_token":null}`, "invalid_request", 400},
		{`{"revision":1,"username":" \t "}`, "invalid_request", 400},
		{`{"revision":1,"username":"a\nb"}`, "invalid_request", 400},
		{`{"revision":1,"password":"short","is_admin":true}`, "invalid_request", 400},
		{`{"revision":1,"password":"` + strings.Repeat("é", 37) + `"}`, "invalid_request", 400},
		{`{"revision":1,"token":"chosen-token"}`, "invalid_request", 400},
		{`{"revision":1,"is_admin":"true"}`, "invalid_request", 400},
		{`{"revision":1,"is_admin":true,"is_admin":false}`, "invalid_request", 400},
		{`{"revision":1,"username":"renamed"} {}`, "invalid_request", 400},
		{`{"revision":2,"username":"renamed","password":"replacement123","is_admin":true,"regenerate_token":true}`, "stale_revision", 409},
		{`{"revision":1,"username":"admin","password":"replacement123","is_admin":true,"regenerate_token":true}`, "username_exists", 409},
	} {
		t.Run(tc.body, func(t *testing.T) {
			response := testutil.ServeHTTP(t, handler, http.MethodPatch, fmt.Sprintf("/api/users/%d", alice.ID), strings.NewReader(tc.body), map[string]string{"X-Access-Token": admin.Token, "Origin": "hister://"})
			var result map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != tc.status || result["code"] != tc.code {
				t.Fatalf("rejected update: %d %s, error %v", response.Code, response.Body, err)
			}
			if strings.Contains(response.Body.String(), "replacement123") || strings.Contains(response.Body.String(), "chosen-token") {
				t.Fatal("response disclosed credentials")
			}
		})
	}
	unchanged, err := model.AuthenticateUser("alice", "password123")
	if err != nil || unchanged.Token != alice.Token || unchanged.IsAdmin || unchanged.SessionVersion != alice.SessionVersion || !unchanged.UpdatedAt.Equal(alice.UpdatedAt) {
		t.Fatalf("rejected updates changed account: %v", err)
	}
}

func TestUpdateUserAPILastAdmin(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	body := `{"revision":1,"is_admin":false,"password":"replacement123","regenerate_token":true}`
	response := testutil.ServeHTTP(t, handler, http.MethodPatch, fmt.Sprintf("/api/users/%d", admin.ID), strings.NewReader(body), map[string]string{"X-Access-Token": admin.Token, "Origin": "hister://"})
	var result map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusConflict || result["code"] != "last_admin" {
		t.Fatalf("last admin removal: %d %s, error %v", response.Code, response.Body, err)
	}
	unchanged, err := model.AuthenticateUser("admin", "password123")
	if err != nil || !unchanged.IsAdmin || unchanged.Token != admin.Token {
		t.Fatalf("last admin changed: %v", err)
	}
}

func TestUpdateUserAPIAuthorization(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	alice := testutil.CreateUser(t, "alice")
	path := fmt.Sprintf("/api/users/%d", alice.ID)
	body := `{"revision":1,"is_admin":true}`
	for _, token := range []string{"", "invalid", alice.Token} {
		response := testutil.ServeHTTP(t, handler, http.MethodPatch, path, strings.NewReader(body), map[string]string{"Authorization": "Bearer " + token, "Origin": "hister://"})
		if response.Code != http.StatusForbidden {
			t.Fatalf("unauthorized update: %d %s", response.Code, response.Body)
		}
	}
	login := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"password123"}`), map[string]string{"Origin": "hister://"})
	cookie := responseSessionCookie(t, login)
	for _, csrf := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
		req.Header.Set("Origin", "https://proxy.example")
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		if csrf {
			configReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
			configReq.Header.Set("Origin", "https://proxy.example")
			configReq.AddCookie(cookie)
			configRec := httptest.NewRecorder()
			handler.ServeHTTP(configRec, configReq)
			req.Header.Set("X-CSRF-Token", configRec.Header().Get("X-CSRF-Token"))
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		want := http.StatusForbidden
		if csrf {
			want = http.StatusOK
		}
		if response.Code != want {
			t.Fatalf("update with CSRF token %t: %d %s", csrf, response.Code, response.Body)
		}
		user, err := model.GetUserByID(alice.ID)
		if err != nil || user.IsAdmin != csrf {
			t.Fatalf("authorization affected account incorrectly: %v", err)
		}
	}
	// Self rotation must return a usable replacement for the token being revoked.
	response := testutil.ServeHTTP(t, handler, http.MethodPatch, fmt.Sprintf("/api/users/%d", admin.ID), strings.NewReader(`{"revision":1,"regenerate_token":true}`), map[string]string{"Authorization": "Bearer " + admin.Token, "Origin": "hister://"})
	var rotated struct{ Token string }
	if err := json.Unmarshal(response.Body.Bytes(), &rotated); err != nil || response.Code != http.StatusOK || rotated.Token == "" || rotated.Token == admin.Token {
		t.Fatalf("self rotation: %d %s, error %v", response.Code, response.Body, err)
	}
	for _, token := range []string{admin.Token, rotated.Token} {
		response := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/users", nil, map[string]string{"Authorization": "Bearer " + token})
		want := http.StatusForbidden
		if token == rotated.Token {
			want = http.StatusOK
		}
		if response.Code != want {
			t.Fatalf("self rotation access: %d, want %d", response.Code, want)
		}
	}
}

func TestUpdateUserAPIRequestLimits(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	path := fmt.Sprintf("/api/users/%d", admin.ID)
	for _, id := range []string{"0", "-1", "abc", "999999", "18446744073709551616"} {
		response := testutil.ServeHTTP(t, handler, http.MethodPatch, "/api/users/"+id, strings.NewReader(`{"revision":1,"username":"renamed"}`), map[string]string{"X-Access-Token": admin.Token, "Origin": "hister://"})
		var result struct{ Code string }
		want, code := http.StatusBadRequest, "invalid_request"
		if id == "999999" {
			want, code = http.StatusNotFound, "user_not_found"
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != want || result.Code != code {
			t.Fatalf("invalid ID: %d %s, error %v", response.Code, response.Body, err)
		}
	}
	for _, knownLength := range []bool{true, false} {
		req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(`{"revision":1,"username":"renamed"}`+strings.Repeat(" ", int(maxRequestBodyBytes))))
		req.Header.Set("X-Access-Token", admin.Token)
		req.Header.Set("Origin", "hister://")
		if !knownLength {
			req.ContentLength = -1
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized update: %d %s", response.Code, response.Body)
		}
	}
	user, err := model.GetUserByID(admin.ID)
	if err != nil || user.Username != admin.Username || user.Revision != admin.Revision {
		t.Fatalf("invalid request changed user: %v", err)
	}
}

func TestUpdateUserAPIRequiresUserHandling(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	response := testutil.ServeHTTP(t, handler, http.MethodPatch, "/api/users/1", strings.NewReader(`{"revision":1,"username":"renamed"}`), map[string]string{"X-Access-Token": "secret", "Origin": "hister://"})
	var result struct{ Code string }
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusBadRequest || result.Code != "user_handling_disabled" {
		t.Fatalf("single user update: %d %s, error %v", response.Code, response.Body, err)
	}
}

func TestUpdateUserAPIDatabaseFailure(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	alice := testutil.CreateUser(t, "alice")
	if err := model.DB.Exec(`CREATE TRIGGER reject_user_update BEFORE UPDATE OF password ON users BEGIN SELECT RAISE(ABORT, 'private database error'); END`).Error; err != nil {
		t.Fatal(err)
	}
	response := testutil.ServeHTTP(t, handler, http.MethodPatch, fmt.Sprintf("/api/users/%d", alice.ID), strings.NewReader(`{"revision":1,"username":"renamed","password":"replacement123","is_admin":true,"regenerate_token":true}`), map[string]string{"X-Access-Token": admin.Token, "Origin": "hister://"})
	var result struct{ Code string }
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusInternalServerError || result.Code != "internal_error" {
		t.Fatalf("failed update: %d %s, error %v", response.Code, response.Body, err)
	}
	if strings.Contains(response.Body.String(), "private database error") || strings.Contains(response.Body.String(), "replacement123") {
		t.Fatal("failed update exposed private data")
	}
	user, err := model.GetUserByID(alice.ID)
	if err != nil || *user != *alice {
		t.Fatalf("database failure left partial changes: %v", err)
	}
}

func TestRotateUserTokenAPI(t *testing.T) {
	cfg, _, idx, admin := newUserDeletionTestServer(t)
	if err := cfg.UpdateBaseURL("http://127.0.0.1:4433/hister"); err != nil {
		t.Fatal(err)
	}
	handler := registerEndpoints(cfg, idx)
	alice := testutil.CreateUser(t, "alice")
	login := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/login", strings.NewReader(`{"username":"alice","password":"password123"}`), map[string]string{"Origin": "hister://"})
	cookie := responseSessionCookie(t, login)
	path := fmt.Sprintf("/hister/api/users/%d/token", alice.ID)
	response := testutil.ServeHTTP(t, handler, http.MethodPost, path, strings.NewReader(`{"revision":1}`), map[string]string{"Authorization": "Bearer " + admin.Token, "Origin": "hister://"})
	var result struct {
		Token    string
		Revision uint64
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK {
		t.Fatalf("rotation: %d %s, error %v", response.Code, response.Body, err)
	}
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || result.Token == "" || result.Token == alice.Token || result.Revision != 2 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("incorrect rotation response: %s", response.Body)
	}
	updated, err := model.AuthenticateUser("alice", "password123")
	if err != nil || updated.Token != result.Token || updated.Revision != 2 {
		t.Fatalf("rotation was not persisted: %v", err)
	}
	// Rotation changes only the token, revision, and modification timestamp.
	unchanged := *updated
	unchanged.Token, unchanged.Revision, unchanged.UpdatedAt = alice.Token, alice.Revision, alice.UpdatedAt
	if unchanged != *alice {
		t.Fatal("rotation changed other account fields")
	}
	for _, token := range []string{alice.Token, result.Token} {
		profile := testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/profile", nil, map[string]string{"Authorization": "Bearer " + token})
		want := http.StatusForbidden
		if token == result.Token {
			want = http.StatusOK
		}
		if profile.Code != want {
			t.Fatalf("token authentication = %d, want %d", profile.Code, want)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/hister/api/profile", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotation revoked the browser session: %d %s", rec.Code, rec.Body)
	}
	// Replaying the old revision must not invalidate the token just returned.
	response = testutil.ServeHTTP(t, handler, http.MethodPost, path, strings.NewReader(`{"revision":1}`), map[string]string{"X-Access-Token": admin.Token, "Origin": "hister://"})
	var conflict struct{ Code string }
	if err := json.Unmarshal(response.Body.Bytes(), &conflict); err != nil || response.Code != http.StatusConflict || conflict.Code != "stale_revision" {
		t.Fatalf("stale rotation: %d %s, error %v", response.Code, response.Body, err)
	}
	read := testutil.ServeHTTP(t, handler, http.MethodGet, path, nil, map[string]string{"X-Access-Token": admin.Token})
	var current struct{ Token string }
	if err := json.Unmarshal(read.Body.Bytes(), &current); err != nil || read.Code != http.StatusOK || current.Token != result.Token {
		t.Fatalf("stale rotation replaced the token: %d %s, error %v", read.Code, read.Body, err)
	}
	response = testutil.ServeHTTP(t, handler, http.MethodPost, path, strings.NewReader(`{"revision":2}`), map[string]string{"X-Access-Token": admin.Token, "Origin": "hister://"})
	var next struct {
		Token    string
		Revision uint64
	}
	if err := json.Unmarshal(response.Body.Bytes(), &next); err != nil || response.Code != http.StatusOK || next.Revision != 3 || next.Token == "" || next.Token == result.Token {
		t.Fatalf("fresh rotation: %d %s, error %v", response.Code, response.Body, err)
	}
}

func TestRotateUserTokenAPIInvalidRequest(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	alice := testutil.CreateUser(t, "alice")
	path := fmt.Sprintf("/api/users/%d/token", alice.ID)
	for _, body := range []string{
		"", `null`, `[]`, `{}`, `{"revision":null}`, `{"revision":0}`, `{"revision":-1}`,
		`{"revision":"1"}`, `{"revision":1.5}`, `{"revision":9223372036854775807}`, `{"revision":18446744073709551616}`,
		`{"revision":1,"revision":1}`, `{"revision":1} {}`, `{"revision":1} null`, `{"revision":1} trailing`,
		`{"revision":1,"token":"chosen-token"}`, `{"revision":1,"username":"renamed"}`, `{"revision":1,"password":"password123"}`,
		`{"revision":1,"is_admin":true}`, `{"revision":1,"regenerate_token":false}`,
	} {
		t.Run(body, func(t *testing.T) {
			response := testutil.ServeHTTP(t, handler, http.MethodPost, path, strings.NewReader(body), map[string]string{"X-Access-Token": admin.Token, "Origin": "hister://"})
			var result struct{ Code string }
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusBadRequest || result.Code != "invalid_request" {
				t.Fatalf("invalid rotation: %d %s, error %v", response.Code, response.Body, err)
			}
			if strings.Contains(response.Body.String(), "chosen-token") || strings.Contains(response.Body.String(), "password123") {
				t.Fatal("validation response exposed submitted credentials")
			}
		})
	}
	current, err := model.GetUserByID(alice.ID)
	if err != nil || *current != *alice {
		t.Fatalf("invalid rotation changed the account: %v", err)
	}
}

func TestRotateUserTokenAPIAuthorization(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	alice := testutil.CreateUser(t, "alice")
	for _, id := range []uint{admin.ID, alice.ID} {
		for _, token := range []string{"", "invalid", alice.Token} {
			if id == alice.ID && token == alice.Token {
				continue
			}
			response := testutil.ServeHTTP(t, handler, http.MethodPost, fmt.Sprintf("/api/users/%d/token", id), strings.NewReader(`{"revision":1}`), map[string]string{"Authorization": "Bearer " + token, "Origin": "hister://"})
			if response.Code != http.StatusForbidden {
				t.Fatalf("unauthorized rotation: %d %s", response.Code, response.Body)
			}
		}
		current, err := model.GetUserByID(id)
		if err != nil {
			t.Fatal(err)
		}
		original := alice
		if id == admin.ID {
			original = admin
		}
		if *current != *original {
			t.Fatal("unauthorized request changed the account")
		}
	}
	// Session authentication still requires CSRF validation.
	login := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"password123"}`), map[string]string{"Origin": "hister://"})
	cookie := responseSessionCookie(t, login)
	for _, csrf := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/users/%d/token", alice.ID), strings.NewReader(`{"revision":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://proxy.example")
		req.AddCookie(cookie)
		if csrf {
			configReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
			configReq.Header.Set("Origin", "https://proxy.example")
			configReq.AddCookie(cookie)
			configRec := httptest.NewRecorder()
			handler.ServeHTTP(configRec, configReq)
			req.Header.Set("X-CSRF-Token", configRec.Header().Get("X-CSRF-Token"))
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		want := http.StatusForbidden
		if csrf {
			want = http.StatusOK
		}
		if response.Code != want {
			t.Fatalf("session rotation with CSRF token %t: %d %s", csrf, response.Code, response.Body)
		}
		current, err := model.GetUserByID(alice.ID)
		if err != nil || (current.Token != alice.Token) != csrf {
			t.Fatalf("CSRF check affected token incorrectly: %v", err)
		}
	}
	// A caller rotating its own token receives a usable replacement in this response.
	response := testutil.ServeHTTP(t, handler, http.MethodPost, fmt.Sprintf("/api/users/%d/token", admin.ID), strings.NewReader(`{"revision":1}`), map[string]string{"X-Access-Token": admin.Token, "Origin": "hister://"})
	var result struct{ Token string }
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || result.Token == "" || result.Token == admin.Token {
		t.Fatalf("self rotation: %d %s, error %v", response.Code, response.Body, err)
	}
	for _, token := range []string{admin.Token, result.Token} {
		response := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/users", nil, map[string]string{"X-Access-Token": token})
		want := http.StatusForbidden
		if token == result.Token {
			want = http.StatusOK
		}
		if response.Code != want {
			t.Fatalf("self rotation access = %d, want %d", response.Code, want)
		}
	}
}

func TestRotateUserTokenAPIRequestLimits(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	for _, id := range []string{"0", "-1", "abc", "999999", "18446744073709551616"} {
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/users/"+id+"/token", strings.NewReader(`{"revision":1}`), map[string]string{"X-Access-Token": admin.Token, "Origin": "hister://"})
		want, code := http.StatusBadRequest, "invalid_request"
		if id == "999999" {
			want, code = http.StatusNotFound, "user_not_found"
		}
		var result struct{ Code string }
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != want || result.Code != code {
			t.Fatalf("invalid ID: %d %s, error %v", response.Code, response.Body, err)
		}
	}
	for _, knownLength := range []bool{true, false} {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/users/%d/token", admin.ID), strings.NewReader(`{"revision":1}`+strings.Repeat(" ", int(maxRequestBodyBytes))))
		req.Header.Set("X-Access-Token", admin.Token)
		req.Header.Set("Origin", "hister://")
		if !knownLength {
			req.ContentLength = -1
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized rotation: %d %s", response.Code, response.Body)
		}
	}
	current, err := model.GetUserByID(admin.ID)
	if err != nil || *current != *admin {
		t.Fatalf("invalid request changed the account: %v", err)
	}
}

func TestRotateUserTokenAPIRequiresUserHandling(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/users/1/token", strings.NewReader(`{"revision":1}`), map[string]string{"X-Access-Token": "secret", "Origin": "hister://"})
	var result struct{ Code string }
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusBadRequest || result.Code != "user_handling_disabled" {
		t.Fatalf("single user rotation: %d %s, error %v", response.Code, response.Body, err)
	}
}

func TestRotateUserTokenAPIDatabaseFailure(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	alice := testutil.CreateUser(t, "alice")
	if err := model.DB.Exec(`CREATE TRIGGER reject_token_rotation BEFORE UPDATE OF token ON users BEGIN SELECT RAISE(ABORT, 'private database error'); END`).Error; err != nil {
		t.Fatal(err)
	}
	response := testutil.ServeHTTP(t, handler, http.MethodPost, fmt.Sprintf("/api/users/%d/token", alice.ID), strings.NewReader(`{"revision":1}`), map[string]string{"X-Access-Token": admin.Token, "Origin": "hister://"})
	var result struct{ Code string }
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusInternalServerError || result.Code != "internal_error" {
		t.Fatalf("failed rotation: %d %s, error %v", response.Code, response.Body, err)
	}
	if strings.Contains(response.Body.String(), "private database error") || strings.Contains(response.Body.String(), alice.Token) {
		t.Fatal("failed rotation exposed private data")
	}
	current, err := model.GetUserByID(alice.ID)
	if err != nil || *current != *alice {
		t.Fatalf("failed rotation changed the account: %v", err)
	}
}

func TestProfileIncludesAccountRevision(t *testing.T) {
	_, handler, _, admin := newUserDeletionTestServer(t)
	alice := testutil.CreateUser(t, "alice")
	for _, user := range []*model.User{alice, admin} {
		for revision := uint64(1); revision <= 2; revision++ {
			if revision == 2 {
				if err := model.UpdatePassword(user.Username, "replacement123"); err != nil {
					t.Fatal(err)
				}
			}
			response := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/profile", nil, map[string]string{"X-Access-Token": user.Token})
			var profile struct {
				ID       uint `json:"user_id"`
				Username string
				Revision uint64
				IsAdmin  bool `json:"is_admin"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &profile); err != nil || response.Code != http.StatusOK || profile.ID != user.ID || profile.Username != user.Username || profile.Revision != revision || profile.IsAdmin != user.IsAdmin {
				t.Fatalf("profile: %d %s, error %v", response.Code, response.Body, err)
			}
			if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), user.Token) || strings.Contains(response.Body.String(), "replacement123") {
				t.Fatalf("unsafe profile response: %s", response.Body)
			}
		}
	}
}

func TestRotateOwnUserTokenAPI(t *testing.T) {
	for _, sessionAuth := range []bool{false, true} {
		t.Run(fmt.Sprintf("session=%t", sessionAuth), func(t *testing.T) {
			_, handler, _, admin := newUserDeletionTestServer(t)
			alice := testutil.CreateUser(t, "alice")
			var cookie *http.Cookie
			if sessionAuth {
				login := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/login", strings.NewReader(`{"username":"alice","password":"password123"}`), map[string]string{"Origin": "hister://"})
				cookie = responseSessionCookie(t, login)
			}
			request := func(method, path, body string, csrf bool) *httptest.ResponseRecorder {
				t.Helper()
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				if sessionAuth {
					req.AddCookie(cookie)
					req.Header.Set("Origin", "https://proxy.example")
					if csrf {
						configReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
						configReq.AddCookie(cookie)
						configReq.Header.Set("Origin", "https://proxy.example")
						configRec := httptest.NewRecorder()
						handler.ServeHTTP(configRec, configReq)
						req.Header.Set("X-CSRF-Token", configRec.Header().Get("X-CSRF-Token"))
					}
				} else {
					req.Header.Set("Authorization", "Bearer "+alice.Token)
					req.Header.Set("Origin", "hister://")
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				return response
			}
			path := fmt.Sprintf("/api/users/%d/token", alice.ID)
			// Other accounts, including nonexistent IDs, must be denied without disclosing existence.
			for _, id := range []uint{admin.ID, alice.ID + 1} {
				response := request(http.MethodPost, fmt.Sprintf("/api/users/%d/token", id), `{"revision":1}`, true)
				if response.Code != http.StatusForbidden {
					t.Fatalf("other account rotation: %d %s", response.Code, response.Body)
				}
			}
			if sessionAuth {
				if response := request(http.MethodPost, path, `{"revision":1}`, false); response.Code != http.StatusForbidden {
					t.Fatalf("rotation without CSRF: %d %s", response.Code, response.Body)
				}
			}
			// Account mutation and token retrieval remain privileged, even for one's own account.
			for _, response := range []*httptest.ResponseRecorder{
				request(http.MethodPatch, fmt.Sprintf("/api/users/%d", alice.ID), `{"revision":1,"is_admin":true}`, true),
				request(http.MethodGet, path, "", false),
			} {
				if response.Code != http.StatusForbidden {
					t.Fatalf("self service allowed administrative access: %d %s", response.Code, response.Body)
				}
			}
			response := request(http.MethodPost, path, `{"revision":1}`, true)
			var result struct {
				Token    string
				Revision uint64
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || result.Token == "" || result.Token == alice.Token || result.Revision != 2 {
				t.Fatalf("own token rotation: %d %s, error %v", response.Code, response.Body, err)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("token response may be cached")
			}
			if !sessionAuth {
				alice.Token = result.Token
			}
			profile := request(http.MethodGet, "/api/profile", "", false)
			var current struct{ Revision uint64 }
			if err := json.Unmarshal(profile.Body.Bytes(), &current); err != nil || profile.Code != http.StatusOK || current.Revision != 2 {
				t.Fatalf("updated profile: %d %s, error %v", profile.Code, profile.Body, err)
			}
			stale := request(http.MethodPost, path, `{"revision":1}`, true)
			if stale.Code != http.StatusConflict {
				t.Fatalf("stale self rotation: %d %s", stale.Code, stale.Body)
			}
		})
	}
}

func TestRemovedProfileTokenEndpoint(t *testing.T) {
	previousFiles := staticTextFiles
	staticTextFiles = map[string][]byte{"index.html": []byte("<html>app</html>")}
	t.Cleanup(func() { staticTextFiles = previousFiles })
	_, handler, _, _ := newUserDeletionTestServer(t)
	alice := testutil.CreateUser(t, "alice")
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/profile/token", nil, map[string]string{"X-Access-Token": alice.Token, "Origin": "hister://"})
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("removed endpoint still succeeded: %d %s", response.Code, response.Body)
	}
	current, err := model.GetUserByID(alice.ID)
	if err != nil || *current != *alice {
		t.Fatalf("removed endpoint changed account: %v", err)
	}
}
