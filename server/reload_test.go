package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/extractor"
	"github.com/asciimoo/nogon/server/model"
	"github.com/asciimoo/nogon/server/testutil"
)

func newConfigurationTestServer(t *testing.T, mode, extra string) (*config.Config, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	filename := filepath.Join(dir, "config.yml")
	app := ""
	if mode != "none" {
		app = fmt.Sprintf("  access_token: private-token\n  public: %t\n", !strings.HasSuffix(mode, "-private"))
	}
	if strings.HasPrefix(mode, "user") {
		app += "  user_handling: true\n"
	}
	raw := fmt.Sprintf("app:\n  directory: %q\n  subtitle: File subtitle\n%sserver:\n  address: 127.0.0.1:4433\n  base_url: http://127.0.0.1:4433/nogon\n%s", dir, app, extra)
	if err := os.WriteFile(filename, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.web.yml"), []byte("app:\n  title: Web title\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	testutil.InitModelWithConfig(t, cfg)
	sessionStore = newSessionStore(cfg.SecretKey(), cfg.BaseURL(""), sessionMaxAge)
	return cfg, registerEndpoints(cfg, nil)
}

func TestConfigurationReadAuthorization(t *testing.T) {
	for _, mode := range []string{"none", "token", "user", "token-private", "user-private"} {
		t.Run(mode, func(t *testing.T) {
			_, handler := newConfigurationTestServer(t, mode, "")
			mode = strings.TrimSuffix(mode, "-private")
			type caller struct {
				name    string
				headers map[string]string
				allowed bool
			}
			callers := []caller{
				{"anonymous", nil, mode == "none"},
				{"invalid token", map[string]string{"Authorization": "Bearer invalid"}, mode == "none"},
				{"instance token", map[string]string{"Authorization": "Bearer private-token"}, mode != "user"},
			}
			if mode == "user" {
				user := testutil.CreateUser(t, "reader")
				admin, err := model.CreateUser("admin", "password123", true)
				if err != nil {
					t.Fatal(err)
				}
				for _, account := range []*model.User{user, admin} {
					callers = append(callers, caller{account.Username, map[string]string{"X-Access-Token": account.Token}, account.IsAdmin})
					login := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/login", strings.NewReader(fmt.Sprintf(`{"username":%q,"password":"password123"}`, account.Username)), map[string]string{"Origin": "nogon://"})
					if login.Code != http.StatusOK {
						t.Fatalf("login: %d %s", login.Code, login.Body)
					}
					callers = append(callers, caller{account.Username + " session", map[string]string{"Cookie": responseSessionCookie(t, login).String()}, account.IsAdmin})
				}
			}
			for _, caller := range callers {
				t.Run(caller.name, func(t *testing.T) {
					public := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/config", nil, caller.headers)
					var capability struct {
						CanConfigure *bool `json:"canConfigure"`
					}
					if err := json.Unmarshal(public.Body.Bytes(), &capability); err != nil || capability.CanConfigure == nil || *capability.CanConfigure != caller.allowed {
						t.Fatalf("incorrect configuration capability: %d %s", public.Code, public.Body)
					}
					response := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/settings", nil, caller.headers)
					want := http.StatusForbidden
					if caller.allowed {
						want = http.StatusOK
					}
					if response.Code != want {
						t.Fatalf("read status = %d, want %d: %s", response.Code, want, response.Body)
					}
					if !caller.allowed {
						for _, operation := range []string{"validate", "apply"} {
							denied := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/"+operation, strings.NewReader(`{"revision":"unknown","edits":[]}`), caller.headers)
							if denied.Code != http.StatusForbidden || denied.Header().Get("Cache-Control") != "no-store" {
								t.Fatalf("unauthorized %s: %d %s", operation, denied.Code, denied.Body)
							}
						}
						return
					}
					var state struct {
						Revision string                 `json:"revision"`
						Fields   []config.FieldMetadata `json:"fields"`
						Values   []config.FieldState    `json:"values"`
					}
					if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil || state.Revision == "" || len(state.Fields) == 0 || len(state.Values) == 0 {
						t.Fatalf("missing editor state: %s", response.Body)
					}
					if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "private-token") {
						t.Fatal("private response is cacheable or exposes a token")
					}
					states := map[string]config.FieldState{}
					for _, field := range state.Values {
						states[field.Path] = field
					}
					if title := states["app.title"]; title.Value != "Web title" || !title.Editable || title.Source.Kind != "web" {
						t.Fatalf("web title: %+v", title)
					}
					if subtitle := states["app.subtitle"]; subtitle.Value != "File subtitle" || subtitle.Editable || subtitle.Reason == "" {
						t.Fatalf("locked subtitle: %+v", subtitle)
					}
					var tokenValue any
					if mode == "none" {
						tokenValue = map[string]any{"action": "clear"}
					}
					if token := states["app.access_token"]; !reflect.DeepEqual(token.Value, tokenValue) || token.SecretPresent != (mode != "none") {
						t.Fatalf("token state: %+v", token)
					}
					headers := maps.Clone(caller.headers)
					if headers == nil {
						headers = map[string]string{}
					}
					headers["Origin"] = "nogon://"
					validation := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/validate", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[]}`, state.Revision)), headers)
					if validation.Code != http.StatusOK {
						t.Fatalf("validation: %d %s", validation.Code, validation.Body)
					}
				})
			}
		})
	}
}

func TestPrepareConfigurationDraftValidatesFreshExtractors(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprintf("invalid_%v", invalid), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HISTER_DATA_DIR", dir)
			filename := filepath.Join(dir, "config.yml")
			// A missing executable must not trigger runtime preparation or
			// execution during form validation.
			raw := fmt.Sprintf("extractors:\n  ytdlp:\n    enable: true\n    options:\n      binary: %q\n", filepath.Join(dir, "must-not-run"))
			if invalid {
				raw += "      max_concurrent_jobs: -1\n  basic:\n    options:\n      unknown: private-credential\nsensitive_content_patterns:\n  broken: '[private-credential'\n"
			}
			if err := os.WriteFile(filename, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			c, _, err := config.LoadForInspection(filename, true)
			if err != nil {
				t.Fatal(err)
			}
			before, err := json.Marshal(extractor.DefaultRegistry().List())
			if err != nil {
				t.Fatal(err)
			}
			next, fields := prepareConfigurationDraft(c, []config.SettingEdit{{Path: "app.title", Action: "set", Value: json.RawMessage(`"Candidate"`)}})
			if invalid {
				if next != nil || len(fields) != 3 {
					t.Fatalf("invalid extractor settings accepted or errors not aggregated: %v", fields)
				}
				paths := map[string]bool{}
				for _, field := range fields {
					paths[field.Path] = true
				}
				if !paths["extractors.ytdlp.options.max_concurrent_jobs"] || !paths["extractors.basic"] {
					t.Fatalf("extractor errors lack stable paths: %v", fields)
				}
				encoded, err := json.Marshal(fields)
				if err != nil || strings.Contains(string(encoded), "private-credential") {
					t.Fatal("extractor validation exposed option values")
				}
			} else if next == nil || len(fields) > 0 || next.App.Title != "Candidate" {
				t.Fatalf("valid draft rejected: %v", fields)
			}
			after, err := json.Marshal(extractor.DefaultRegistry().List())
			if err != nil || string(after) != string(before) {
				t.Fatal("draft validation changed the active extractor chain")
			}
			if c.App.Title != "Nogon" {
				t.Fatal("draft validation mutated the active configuration")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatal("draft validation created runtime files")
			}
		})
	}
}

func readConfigurationState(t *testing.T, handler http.Handler, headers map[string]string) (string, map[string]config.FieldState) {
	t.Helper()
	response := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/settings", nil, headers)
	var state struct {
		Revision string              `json:"revision"`
		Values   []config.FieldState `json:"values"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil || response.Code != http.StatusOK || state.Revision == "" {
		t.Fatalf("settings read failed: %d %s", response.Code, response.Body)
	}
	values := map[string]config.FieldState{}
	for _, field := range state.Values {
		values[field.Path] = field
	}
	return state.Revision, values
}

func newAccessSettingsTestServer(t *testing.T, mode string, serverSettings ...string) (*config.Config, http.Handler, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	filename := filepath.Join(dir, "config.yml")
	main := fmt.Sprintf("app:\n  directory: %q\nserver:\n  base_url: http://127.0.0.1:4433/nogon\n%sindexer:\n  detect_languages: false\n", dir, strings.Join(serverSettings, ""))
	if err := os.WriteFile(filename, []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	token := "secret"
	if mode == "none" {
		token = ""
	}
	web := fmt.Sprintf("app:\n  access_token: %q\n  user_handling: %t\n  public: %t\nserver:\n  address: 127.0.0.1:0\n", token, mode == "user", mode != "none")
	if err := os.WriteFile(filepath.Join(dir, "config.web.yml"), []byte(web), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	testutil.InitModelWithConfig(t, cfg)
	if mode == "user" {
		admin, err := model.CreateUser("review-owner", "password123", true)
		if err != nil {
			t.Fatal(err)
		}
		token = admin.Token
	}
	return cfg, startReloadTestRuntime(t, cfg), map[string]string{"Origin": "nogon://", "X-Access-Token": token, "Content-Type": "application/json"}
}

func TestSettingsAccessReviewRequiresAcknowledgment(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "user")
	revision, _ := readConfigurationState(t, handler, headers)
	before := map[string]string{}
	for _, filename := range []string{cfg.Filename(), cfg.FullPath("config.web.yml")} {
		raw, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		before[filename] = string(raw)
	}
	body := fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.user_handling","action":"set","value":false},{"path":"app.title","action":"set","value":"Reviewed title"},{"path":"server.address","action":"set","value":"0.0.0.0:5544"}]}`, revision)
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/validate", strings.NewReader(body), headers)
	var result struct {
		Review struct {
			Acknowledgment string                                 `json:"acknowledgment"`
			Changes        []struct{ Path, Before, After string } `json:"changes"`
			Consequences   []string                               `json:"consequences"`
			Destination    string                                 `json:"destination_url"`
		} `json:"review"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || result.Review.Acknowledgment == "" {
		t.Fatalf("sensitive review unavailable: %d %s (%v)", response.Code, response.Body, err)
	}
	if len(result.Review.Changes) != 3 || result.Review.Destination != "http://127.0.0.1:4433/nogon/settings" {
		t.Fatalf("incomplete review: %s", response.Body)
	}
	consequences := strings.Join(result.Review.Consequences, " ")
	if !strings.Contains(consequences, "instance token") || !strings.Contains(consequences, "other machines") {
		t.Fatalf("review omitted access consequences: %s", consequences)
	}
	response = testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
	if response.Code != http.StatusPreconditionRequired || !strings.Contains(response.Body.String(), `"code":"acknowledgment_required"`) {
		t.Fatalf("missing acknowledgment was not rejected: %d %s", response.Code, response.Body)
	}
	activeRevision, values := readConfigurationState(t, handler, headers)
	if activeRevision != revision || values["app.user_handling"].Value != true || values["server.address"].Value != "127.0.0.1:0" || values["app.title"].Value != "Nogon" {
		t.Fatal("review or rejected apply changed the running configuration")
	}
	for filename, original := range before {
		raw, err := os.ReadFile(filename)
		if err != nil || string(raw) != original {
			t.Fatalf("review or rejected apply changed %s", filename)
		}
	}
	if staged, err := filepath.Glob(cfg.FullPath(".config.web-*")); err != nil || len(staged) != 0 {
		t.Fatal("rejected apply staged a configuration file")
	}
}

func TestSettingsAccessControlsRespectSources(t *testing.T) {
	_, handler, headers := newAccessSettingsTestServer(t, "user")
	_, states := readConfigurationState(t, handler, headers)
	for _, path := range []string{"app.access_token", "app.user_handling", "app.public", "server.address", "server.oauth_only", "server.oauth.github.client_id", "server.oauth.github.client_secret", "server.oauth.google.scopes", "server.oauth.oidc.disable_pkce"} {
		if value := states[path]; !value.Editable || value.Source.Locked {
			t.Errorf("access control %s is unavailable: %+v", path, value)
		}
	}
	if value := states["server.base_url"]; value.Editable || !value.Source.Locked {
		t.Fatalf("authoritative connection setting became editable: %+v", value)
	}
	if value := states["app.access_token"]; !value.SecretPresent {
		t.Fatal("editable access token lost its stored value")
	} else if secret, ok := value.Value.(map[string]any); !ok || secret["action"] != "keep" || secret["ref"] == "" || len(secret) != 2 {
		t.Fatalf("editable access token did not use an opaque Keep reference: %+v", value)
	}
	if value := states["server.oauth.oidc.disable_pkce"].Value; value != false {
		t.Fatalf("unconfigured provider has no boolean default: %v", value)
	}
}

func TestSettingsAccessTokenRotationConfirmation(t *testing.T) {
	for _, session := range []bool{false, true} {
		t.Run(fmt.Sprintf("session_%t", session), func(t *testing.T) {
			cfg, handler, headers := newAccessSettingsTestServer(t, "token")
			login := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/token-login"), strings.NewReader(`{"token":"secret"}`), headers)
			oldCookie := responseSessionCookie(t, login)
			revision, _ := readConfigurationState(t, handler, headers)
			const edits = `[{"path":"app.access_token","action":"set","value":{"action":"replace","value":"replacement-token"}}]`
			ack := settingsReview(t, handler, headers, revision, edits)
			response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/apply"), strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s,"acknowledgment":%q}`, revision, edits, ack)), headers)
			if response.Code != http.StatusAccepted {
				t.Fatalf("temporary token rotation: %d %s", response.Code, response.Body)
			}
			var pending struct {
				Revision string `json:"revision"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &pending); err != nil || pending.Revision == "" {
				t.Fatalf("missing pending revision: %s", response.Body)
			}
			body := fmt.Sprintf(`{"revision":%q}`, pending.Revision)
			for _, denied := range []map[string]string{headers, {"Origin": "nogon://", "Cookie": oldCookie.String()}} {
				response = testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/confirm"), strings.NewReader(body), denied)
				if response.Code != http.StatusForbidden {
					t.Fatalf("old credential confirmed rotation: %d %s", response.Code, response.Body)
				}
			}
			fresh := map[string]string{"Origin": "nogon://", "X-Access-Token": "replacement-token"}
			if session {
				login = testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/token-login"), strings.NewReader(`{"token":"replacement-token"}`), fresh)
				fresh = map[string]string{"Origin": "nogon://", "Cookie": responseSessionCookie(t, login).String()}
			}
			response = testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/confirm"), strings.NewReader(body), fresh)
			if response.Code != http.StatusOK {
				t.Fatalf("fresh credential could not confirm: %d %s", response.Code, response.Body)
			}
			loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
			if err != nil || loaded.App.AccessToken != "replacement-token" {
				t.Fatalf("confirmed token did not persist: %v", err)
			}
		})
	}
}

func activateReviewedSettings(t *testing.T, cfg *config.Config, handler http.Handler, headers map[string]string, edits string) pendingAccessStatus {
	t.Helper()
	revision, _ := readConfigurationState(t, handler, headers)
	ack := settingsReview(t, handler, headers, revision, edits)
	response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/apply"), strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s,"acknowledgment":%q}`, revision, edits, ack)), headers)
	var pending pendingAccessStatus
	if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), &pending) != nil || pending.Revision == "" {
		t.Fatalf("temporary activation: %d %s", response.Code, response.Body)
	}
	return pending
}

func TestSettingsAccessRequiresFreshSession(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "user")
	login := func() *http.Cookie {
		t.Helper()
		response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/login"), strings.NewReader(`{"username":"review-owner","password":"password123"}`), headers)
		if response.Code != http.StatusOK {
			t.Fatalf("login: %d %s", response.Code, response.Body)
		}
		return responseSessionCookie(t, response)
	}
	old := login()
	pending := activateReviewedSettings(t, cfg, handler, headers, `[{"path":"app.public","action":"set","value":false}]`)
	body := fmt.Sprintf(`{"revision":%q}`, pending.Revision)
	response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/confirm"), strings.NewReader(body), map[string]string{"Origin": "nogon://", "Cookie": old.String()})
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "fresh_authentication_required") {
		t.Fatalf("old session confirmed an authentication change: %d %s", response.Code, response.Body)
	}
	fresh := login()
	response = testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/confirm"), strings.NewReader(body), map[string]string{"Origin": "nogon://", "Cookie": fresh.String()})
	if response.Code != http.StatusOK {
		t.Fatalf("fresh login could not confirm: %d %s", response.Code, response.Body)
	}
}

func TestSettingsAccessDisablingAuthenticationBindsCompletion(t *testing.T) {
	for _, operation := range []string{"confirm", "cancel"} {
		t.Run(operation, func(t *testing.T) {
			cfg, handler, headers := newAccessSettingsTestServer(t, "token")
			pending := activateReviewedSettings(t, cfg, handler, headers, `[{"path":"app.access_token","action":"set","value":{"action":"clear"}},{"path":"app.public","action":"set","value":false}]`)
			anonymous := map[string]string{"Origin": "nogon://"}
			body := fmt.Sprintf(`{"revision":%q}`, pending.Revision)
			response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/"+operation), strings.NewReader(body), anonymous)
			if response.Code != http.StatusForbidden {
				t.Fatalf("visitor completed someone else's access change: %d %s", response.Code, response.Body)
			}
			if pending.ConfirmationToken == "" {
				t.Fatal("initiator did not receive confirmation proof")
			}
			state := testutil.ServeHTTP(t, handler, http.MethodGet, cfg.BaseURL("/api/settings"), nil, anonymous)
			if !strings.Contains(state.Body.String(), pending.Revision) || strings.Contains(state.Body.String(), pending.ConfirmationToken) {
				t.Fatalf("pending status missing or exposes proof: %s", state.Body)
			}
			body = fmt.Sprintf(`{"revision":%q,"confirmation_token":%q}`, pending.Revision, pending.ConfirmationToken)
			response = testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/"+operation), strings.NewReader(body), anonymous)
			if response.Code != http.StatusOK {
				t.Fatalf("initiator completion failed: %d %s", response.Code, response.Body)
			}
		})
	}
}

func TestSettingsAccessConfirmationRequiresCandidateDestination(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	pending := activateReviewedSettings(t, cfg, handler, headers, `[{"path":"app.public","action":"set","value":false}]`)
	body := fmt.Sprintf(`{"revision":%q}`, pending.Revision)
	for _, destination := range []string{"http://other.example/nogon/api/settings/confirm", "http://127.0.0.1:4433/api/settings/confirm"} {
		response := testutil.ServeHTTP(t, handler, http.MethodPost, destination, strings.NewReader(body), headers)
		if response.Code != http.StatusNotFound && (response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "destination_required")) {
			t.Fatalf("confirmed through a different destination: %s: %d %s", destination, response.Code, response.Body)
		}
	}
	response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/confirm"), strings.NewReader(body), headers)
	if response.Code != http.StatusOK {
		t.Fatalf("candidate destination rejected: %d %s", response.Code, response.Body)
	}
}

func TestSettingsAccessConfirmationAcceptsDefaultPort(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	main, err := os.ReadFile(cfg.Filename())
	if err != nil {
		t.Fatal(err)
	}
	main = []byte(strings.Replace(string(main), "http://127.0.0.1:4433/nogon", "http://example.com:80/nogon", 1))
	if err := os.WriteFile(cfg.Filename(), main, 0o600); err != nil {
		t.Fatal(err)
	}
	if status := reloadRequest(t, handler, "secret"); status != http.StatusOK {
		t.Fatalf("setup reload: %d", status)
	}
	pending := activateReviewedSettings(t, cfg, handler, headers, `[{"path":"app.public","action":"set","value":false}]`)
	body := fmt.Sprintf(`{"revision":%q}`, pending.Revision)
	headers["Origin"] = "http://example.com"
	headers["Sec-Fetch-Site"] = "same-origin"
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "http://example.com/nogon/api/settings/confirm", strings.NewReader(body), headers)
	if response.Code != http.StatusOK {
		t.Fatalf("default port normalization rejected access: %d %s", response.Code, response.Body)
	}
}

func TestSettingsAccessOAuthVerification(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		t.Run(fmt.Sprintf("proxy=%t", proxy), func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					if err := r.ParseForm(); err != nil || r.Form.Get("client_secret") != "rotated-provider-secret" {
						t.Error("OAuth exchange did not use the temporary credential")
					}
					_, _ = io.WriteString(w, `{"access_token":"provider-token"}`)
				} else {
					_, _ = io.WriteString(w, `{"email":"owner@example.com"}`)
				}
			}))
			t.Cleanup(provider.Close)
			extra := fmt.Sprintf("  oauth:\n    oidc:\n      client_id: client\n      client_secret: provider-secret\n      auth_url: %s/auth\n      token_url: %s/token\n      userinfo_url: %s/userinfo\n", provider.URL, provider.URL, provider.URL)
			if proxy {
				extra += "  proxy_auth_header: X-Remote-User\n"
			}
			cfg, handler, headers := newAccessSettingsTestServer(t, "user", extra)
			main, err := os.ReadFile(cfg.Filename())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cfg.Filename(), []byte(strings.Replace(string(main), "      client_secret: provider-secret\n", "", 1)), 0o600); err != nil {
				t.Fatal(err)
			}
			web, err := os.ReadFile(cfg.FullPath("config.web.yml"))
			if err != nil {
				t.Fatal(err)
			}
			web = append(web, []byte("  oauth:\n    oidc:\n      client_secret: provider-secret\n")...)
			if err := os.WriteFile(cfg.FullPath("config.web.yml"), web, 0o600); err != nil {
				t.Fatal(err)
			}
			if status := reloadRequest(t, handler, headers["X-Access-Token"]); status != http.StatusOK {
				t.Fatalf("provider setup reload: %d", status)
			}
			user, err := model.CreateOAuthUser("provider-owner", "oidc-owner@example.com")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := model.SyncOAuthAdmin(user.OAuthID, true); err != nil {
				t.Fatal(err)
			}
			oldFlow, oldQuery := startOAuthLogin(t, handler, nil)
			pending := activateReviewedSettings(t, cfg, handler, headers, `[{"path":"server.oauth.oidc.scopes","action":"set","value":["openid","profile"]},{"path":"server.oauth.oidc.client_secret","action":"set","value":{"action":"replace","value":"rotated-provider-secret"}},{"path":"server.oauth_only","action":"set","value":true}]`)
			state := testutil.ServeHTTP(t, handler, http.MethodGet, cfg.BaseURL("/api/settings"), nil, headers)
			if strings.Contains(state.Body.String(), "provider-secret") {
				t.Fatal("pending state exposed an OAuth credential")
			}
			body := fmt.Sprintf(`{"revision":%q}`, pending.Revision)
			confirm := func(credentials map[string]string, want int) {
				t.Helper()
				credentials = maps.Clone(credentials)
				if proxy && credentials["X-Remote-User"] == "" {
					credentials["X-Remote-User"] = "provider-owner"
				}
				response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/confirm"), strings.NewReader(body), credentials)
				if response.Code != want {
					t.Fatalf("OAuth confirmation: %d %s, want %d", response.Code, response.Body, want)
				}
			}
			confirm(headers, http.StatusForbidden)
			oldLogin := oauthCallback(handler, oldFlow, url.Values{"provider": {"oidc"}, "state": {oldQuery.Get("state")}, "code": {"code"}})
			if oldLogin.Code != http.StatusFound {
				t.Fatalf("old login callback: %d %s", oldLogin.Code, oldLogin.Body)
			}
			confirm(map[string]string{"Origin": "nogon://", "Cookie": responseSessionCookie(t, oldLogin).String()}, http.StatusForbidden)
			flow, query := startOAuthLogin(t, handler, nil)
			login := oauthCallback(handler, flow, url.Values{"provider": {"oidc"}, "state": {query.Get("state")}, "code": {"code"}})
			if login.Code != http.StatusFound {
				t.Fatalf("fresh OAuth login: %d %s", login.Code, login.Body)
			}
			if proxy {
				confirm(map[string]string{"Origin": "nogon://", "Cookie": responseSessionCookie(t, login).String(), "X-Remote-User": "review-owner"}, http.StatusForbidden)
			}
			confirm(map[string]string{"Origin": "nogon://", "Cookie": responseSessionCookie(t, login).String()}, http.StatusOK)
			loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
			if err != nil || loaded.Server.OAuth["oidc"].ClientSecret != "rotated-provider-secret" || !reflect.DeepEqual(loaded.Server.OAuth["oidc"].Scopes, []string{"openid", "profile"}) {
				t.Fatalf("confirmed provider settings did not persist: %v", err)
			}
		})
	}
}

func TestSettingsAccessProxyPasswordVerification(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "user", "  proxy_auth_header: X-Remote-User\n  oauth:\n    oidc:\n      client_id: client\n      client_secret: provider-secret\n      auth_url: https://provider.example/auth\n      token_url: https://provider.example/token\n      userinfo_url: https://provider.example/userinfo\n")
	headers["X-Remote-User"] = "review-owner"
	if _, err := model.CreateUser("other-owner", "password123", true); err != nil {
		t.Fatal(err)
	}
	login := func(username string) *http.Cookie {
		t.Helper()
		response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/login"), strings.NewReader(fmt.Sprintf(`{"username":%q,"password":"password123"}`, username)), headers)
		if response.Code != http.StatusOK {
			t.Fatalf("password login: %d %s", response.Code, response.Body)
		}
		return responseSessionCookie(t, response)
	}
	old := login("review-owner")
	web, err := os.ReadFile(cfg.FullPath("config.web.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.FullPath("config.web.yml"), append(web, []byte("  oauth_only: true\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := reloadRequest(t, handler, headers["X-Access-Token"]); status != http.StatusOK {
		t.Fatalf("OAuth only setup reload: %d", status)
	}
	pending := activateReviewedSettings(t, cfg, handler, headers, `[{"path":"server.oauth_only","action":"set","value":false}]`)
	if pending.Authentication != "password" {
		t.Fatalf("confirmation method = %q, want password", pending.Authentication)
	}
	body := fmt.Sprintf(`{"revision":%q}`, pending.Revision)
	confirm := func(cookie *http.Cookie, want int) {
		t.Helper()
		credentials := maps.Clone(headers)
		if cookie != nil {
			credentials["Cookie"] = cookie.String()
		}
		response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/confirm"), strings.NewReader(body), credentials)
		if response.Code != want || (want == http.StatusForbidden && !strings.Contains(response.Body.String(), "fresh_authentication_required")) {
			t.Fatalf("password confirmation: %d %s, want %d", response.Code, response.Body, want)
		}
	}
	confirm(nil, http.StatusForbidden)
	confirm(old, http.StatusForbidden)
	confirm(login("other-owner"), http.StatusForbidden)
	revoked := login("review-owner")
	if err := model.UpdatePassword("review-owner", "password123"); err != nil {
		t.Fatal(err)
	}
	confirm(revoked, http.StatusForbidden)
	loggedOut := login("review-owner")
	logout := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/logout"), nil, map[string]string{"Origin": "nogon://", "X-Remote-User": "review-owner", "Cookie": loggedOut.String()})
	if logout.Code != http.StatusOK {
		t.Fatalf("logout: %d %s", logout.Code, logout.Body)
	}
	confirm(loggedOut, http.StatusForbidden)
	confirm(login("review-owner"), http.StatusOK)
	loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
	if err != nil || loaded.Server.OAuthOnly {
		t.Fatalf("confirmed password access did not persist: %v", err)
	}
}

func TestSettingsAccessProxyCredentialsIgnoreSession(t *testing.T) {
	for _, username := range []string{"", "review-owner", "other-owner"} {
		t.Run("session="+username, func(t *testing.T) {
			cfg, handler, headers := newAccessSettingsTestServer(t, "user", "  proxy_auth_header: X-Remote-User\n")
			delete(headers, "X-Access-Token")
			headers["X-Remote-User"] = "review-owner"
			if _, err := model.CreateUser("other-owner", "password123", false); err != nil {
				t.Fatal(err)
			}
			if username != "" {
				login := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/login"), strings.NewReader(fmt.Sprintf(`{"username":%q,"password":"password123"}`, username)), headers)
				if login.Code != http.StatusOK {
					t.Fatalf("login: %d %s", login.Code, login.Body)
				}
				headers["Cookie"] = responseSessionCookie(t, login).String()
			}
			pending := activateReviewedSettings(t, cfg, handler, headers, `[{"path":"app.public","action":"set","value":false}]`)
			body := fmt.Sprintf(`{"revision":%q}`, pending.Revision)
			response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/confirm"), strings.NewReader(body), headers)
			if response.Code != http.StatusOK {
				t.Fatalf("proxy credentials could not confirm: %d %s", response.Code, response.Body)
			}
		})
	}
}

func TestSettingsAccessChangedListenerAndBasePath(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	main, err := os.ReadFile(cfg.Filename())
	if err != nil {
		t.Fatal(err)
	}
	main = []byte(strings.Replace(string(main), "  base_url: http://127.0.0.1:4433/nogon\n", "", 1))
	if err := os.WriteFile(cfg.Filename(), main, 0o600); err != nil {
		t.Fatal(err)
	}
	managed := fmt.Sprintf("app:\n  access_token: secret\n  public: true\nserver:\n  address: 127.0.0.1:0\n  base_url: %s\n", cfg.Server.BaseURL)
	if err := os.WriteFile(cfg.FullPath("config.web.yml"), []byte(managed), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := reloadRequest(t, handler, "secret"); status != http.StatusOK {
		t.Fatalf("setup reload: %d", status)
	}
	address := runtimeTestAddress(t)
	base := "http://" + address + "/changed"
	edits := fmt.Sprintf(`[{"path":"server.address","action":"set","value":%q},{"path":"server.base_url","action":"set","value":%q}]`, address, base)
	pending := activateReviewedSettings(t, cfg, handler, headers, edits)
	if pending.DestinationURL != base+"/settings" {
		t.Fatalf("wrong destination: %s", pending.DestinationURL)
	}
	body := fmt.Sprintf(`{"revision":%q}`, pending.Revision)
	// Matching a destination header alone does not prove use of the new listener.
	reply := testutil.ServeHTTP(t, handler, http.MethodPost, base+"/api/settings/confirm", strings.NewReader(body), headers)
	if reply.Code != http.StatusForbidden {
		t.Fatalf("confirmation bypassed the replacement listener: %d %s", reply.Code, reply.Body)
	}
	client := &http.Client{}
	defer client.CloseIdleConnections()
	request, err := http.NewRequest(http.MethodPost, base+"/api/settings/confirm", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("new listener confirmation: %d %s", response.StatusCode, raw)
	}
	loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
	if err != nil || loaded.Server.Address != address || loaded.Server.BaseURL != base {
		t.Fatalf("destination was not persisted: %v", err)
	}
}

func TestSettingsAccessCompletionRequestGuards(t *testing.T) {
	for _, operation := range []string{"confirm", "cancel"} {
		t.Run(operation, func(t *testing.T) {
			cfg, handler, headers := newAccessSettingsTestServer(t, "user")
			reader := testutil.CreateUser(t, "reader")
			pending := activateReviewedSettings(t, cfg, handler, headers, `[{"path":"app.public","action":"set","value":false}]`)
			body := fmt.Sprintf(`{"revision":%q}`, pending.Revision)
			for _, denied := range []map[string]string{
				{"Origin": "nogon://"},
				{"Origin": "nogon://", "X-Access-Token": reader.Token},
				{"Origin": "https://other.example", "X-Access-Token": headers["X-Access-Token"]},
				{"Origin": "chrome-extension://example", "X-Access-Token": headers["X-Access-Token"]},
			} {
				response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/"+operation), strings.NewReader(body), denied)
				if response.Code != http.StatusForbidden || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("authorization or CSRF bypass: %d %s", response.Code, response.Body)
				}
			}
			for _, invalid := range []string{
				`{}`, `{"revision":null}`, body + `{}`, strings.TrimSuffix(body, "}") + `,"confirmed":true}`,
				strings.TrimSuffix(body, "}") + `,"revision":"duplicate"}`, strings.TrimSuffix(body, "}") + `,"confirmation_token":false}`,
				strings.TrimSuffix(body, "}") + `,"confirmation_token":null}`,
			} {
				response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/"+operation), strings.NewReader(invalid), headers)
				if response.Code != http.StatusBadRequest {
					t.Fatalf("ambiguous completion accepted: %s: %d %s", invalid, response.Code, response.Body)
				}
			}
			response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/"+operation), strings.NewReader(body), headers)
			if response.Code != http.StatusOK {
				t.Fatalf("valid completion rejected: %d %s", response.Code, response.Body)
			}
			response = testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/"+operation), strings.NewReader(body), headers)
			if response.Code != http.StatusConflict {
				t.Fatalf("replayed completion accepted: %d %s", response.Code, response.Body)
			}
		})
	}
}

func TestSettingsAccessValidationRetainsRecoveryRestrictions(t *testing.T) {
	for _, tc := range []struct{ name, mode, extra, edits string }{
		{"public without authentication", "token", "", `[{"path":"app.access_token","action":"set","value":{"action":"clear"}}]`},
		{"user mode without administrator", "token", "", `[{"path":"app.user_handling","action":"set","value":true}]`},
		{"OAuth only without provider", "user", "", `[{"path":"server.oauth_only","action":"set","value":true}]`},
		{"identity mapping", "user", "", `[{"path":"server.oauth.oidc.identity_claim","action":"set","value":"sub"}]`},
		{"provider endpoints", "user", "", `[{"path":"server.oauth.oidc.token_url","action":"set","value":"https://unreviewed.example"}]`},
		{"parent object", "user", "", `[{"path":"server.oauth","action":"set","value":{"oidc":{"admin_groups":["everyone"]}}}]`},
		{"role synchronization", "user", "  oauth:\n    oidc:\n      client_id: client\n      client_secret: provider-secret\n      auth_url: https://provider.example/auth\n      token_url: https://provider.example/token\n      userinfo_url: https://provider.example/userinfo\n      admin_groups: [owners]\n", `[{"path":"server.oauth.oidc.scopes","action":"set","value":["profile"]}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, handler, headers := newAccessSettingsTestServer(t, tc.mode, tc.extra)
			unchanged := assertSettingsUnchanged(t, cfg, handler, headers)
			revision, _ := readConfigurationState(t, handler, headers)
			for _, operation := range []string{"validate", "apply"} {
				body := fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, tc.edits)
				response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/"+operation), strings.NewReader(body), headers)
				want := http.StatusUnprocessableEntity
				if operation == "apply" && tc.name == "user mode without administrator" {
					want = http.StatusPreconditionRequired
				}
				if response.Code != want {
					t.Fatalf("unsafe access draft: %d %s, want %d", response.Code, response.Body, want)
				}
				if strings.Contains(response.Body.String(), "provider-secret") {
					t.Fatal("validation exposed provider credentials")
				}
				unchanged()
			}
		})
	}
}

func TestSettingsAccessEnablingUserHandlingRequiresAdministratorLogin(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	admin, err := model.CreateUser("future-owner", "password123", true)
	if err != nil {
		t.Fatal(err)
	}
	login := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/login"), strings.NewReader(`{"username":"future-owner","password":"password123"}`), headers)
	oldCookie := responseSessionCookie(t, login)
	pending := activateReviewedSettings(t, cfg, handler, headers, `[{"path":"app.user_handling","action":"set","value":true}]`)
	body := fmt.Sprintf(`{"revision":%q}`, pending.Revision)
	for _, denied := range []map[string]string{headers, {"Origin": "nogon://", "Cookie": oldCookie.String()}} {
		response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/confirm"), strings.NewReader(body), denied)
		if response.Code != http.StatusForbidden {
			t.Fatalf("old authentication confirmed user mode: %d %s", response.Code, response.Body)
		}
	}
	response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/confirm"), strings.NewReader(body), map[string]string{"Origin": "nogon://", "X-Access-Token": admin.Token})
	if response.Code != http.StatusOK {
		t.Fatalf("administrator could not confirm user mode: %d %s", response.Code, response.Body)
	}
}

func TestSettingsAccessConfirmationRechecksAuthorityAfterDraining(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	previousEndpoints := Endpoints
	Endpoints = append(append([]*Endpoint(nil), Endpoints...), &Endpoint{
		Path: "/api/test-demote", Method: GET,
		Handler: func(c *webContext) {
			close(entered)
			<-release
			if _, err := model.ToggleAdmin("review-owner"); err != nil {
				t.Error(err)
			}
			serve200(c)
		},
	})
	t.Cleanup(func() { Endpoints = previousEndpoints })
	cfg, handler, headers := newAccessSettingsTestServer(t, "user")
	backup, err := model.CreateUser("backup-owner", "password123", true)
	if err != nil {
		t.Fatal(err)
	}
	pending := activateReviewedSettings(t, cfg, handler, headers, `[{"path":"app.public","action":"set","value":false}]`)
	held := make(chan struct{})
	go func() {
		testutil.ServeHTTP(t, handler, http.MethodGet, cfg.BaseURL("/api/test-demote"), nil, headers)
		close(held)
	}()
	<-entered
	body := fmt.Sprintf(`{"revision":%q}`, pending.Revision)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/confirm"), strings.NewReader(body), headers)
	}()
	waitRuntimePause(t, handler)
	unblock()
	<-held
	if response := <-done; response.Code != http.StatusForbidden {
		t.Fatalf("demoted caller confirmed access: %d %s", response.Code, response.Body)
	}
	loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
	if err != nil || !loaded.App.Public {
		t.Fatalf("rejected confirmation persisted: %v", err)
	}
	response := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/cancel"), strings.NewReader(body), map[string]string{"Origin": "nogon://", "X-Access-Token": backup.Token})
	if response.Code != http.StatusOK {
		t.Fatalf("remaining administrator cannot cancel: %d %s", response.Code, response.Body)
	}
}

func TestSettingsAcknowledgmentAllowsOrdinaryActivation(t *testing.T) {
	for _, mode := range []string{"token", "user", "none"} {
		t.Run(mode, func(t *testing.T) {
			cfg, handler, headers := newAccessSettingsTestServer(t, mode)
			unchanged := assertSettingsUnchanged(t, cfg, handler, headers)
			revision, _ := readConfigurationState(t, handler, headers)
			const edits = `[{"path":"app.profiler","action":"set","value":true},{"path":"app.title","action":"set","value":"Reviewed diagnostics"}]`
			body := fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, edits)
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/validate", strings.NewReader(body), headers)
			var result struct {
				Required bool `json:"requires_confirmation"`
				Review   struct {
					Mode           string   `json:"mode"`
					Acknowledgment string   `json:"acknowledgment"`
					Destination    string   `json:"destination_url"`
					Consequences   []string `json:"consequences"`
				} `json:"review"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || !result.Required || result.Review.Mode != "acknowledge" || result.Review.Acknowledgment == "" {
				t.Fatalf("missing acknowledgment review: %d %s (%v)", response.Code, response.Body, err)
			}
			if mode == "none" {
				headers["Cookie"] = responseSessionCookie(t, response).String()
			}
			if result.Review.Destination != "" || !strings.Contains(strings.Join(result.Review.Consequences, " "), "private data") {
				t.Fatalf("incorrect diagnostics consequences: %s", response.Body)
			}
			response = testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
			if response.Code != http.StatusPreconditionRequired {
				t.Fatalf("unacknowledged diagnostics: %d %s", response.Code, response.Body)
			}
			unchanged()
			body = fmt.Sprintf(`{"revision":%q,"edits":%s,"acknowledgment":%q}`, revision, edits, result.Review.Acknowledgment)
			response = testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
			if response.Code != http.StatusOK {
				t.Fatalf("acknowledged save: %d %s", response.Code, response.Body)
			}
			loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
			if err != nil || !loaded.App.Profiler || loaded.App.Title != "Reviewed diagnostics" {
				t.Fatalf("acknowledged settings were not persisted: %v", err)
			}
			response = testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
			if response.Code != http.StatusConflict {
				t.Fatalf("replayed acknowledgment: %d %s", response.Code, response.Body)
			}
			for _, enabled := range []bool{true, false} {
				revision, _ = readConfigurationState(t, handler, headers)
				body = fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.profiler","action":"set","value":%t}]}`, revision, enabled)
				response = testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/validate", strings.NewReader(body), headers)
				var validation struct {
					Required *bool           `json:"requires_confirmation"`
					Review   json.RawMessage `json:"review"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &validation); err != nil || response.Code != http.StatusOK || validation.Required == nil || *validation.Required || len(validation.Review) != 0 {
					t.Fatalf("unchanged or disabled diagnostics required consent: %d %s", response.Code, response.Body)
				}
				response = testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
				if response.Code != http.StatusOK {
					t.Fatalf("ordinary diagnostics save: %d %s", response.Code, response.Body)
				}
				_, values := readConfigurationState(t, handler, headers)
				if values["app.profiler"].Value != enabled {
					t.Fatal("diagnostics setting did not activate")
				}
			}
		})
	}
}

func TestSettingsAcknowledgmentRetainsAccessAndWriteGuards(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "user")
	revision, _ := readConfigurationState(t, handler, headers)
	unchanged := assertSettingsUnchanged(t, cfg, handler, headers)
	const edits = `[{"path":"app.profiler","action":"set","value":true},{"path":"app.title","action":"set","value":"Reviewed"}]`
	ack := settingsReview(t, handler, headers, revision, edits)
	apply := func(edits, ack string, headers map[string]string, want int) {
		t.Helper()
		body := fmt.Sprintf(`{"revision":%q,"edits":%s,"acknowledgment":%q}`, revision, edits, ack)
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
		if response.Code != want {
			t.Fatalf("apply: %d, want %d: %s", response.Code, want, response.Body)
		}
		if want != http.StatusAccepted {
			unchanged()
		}
	}
	reader := testutil.CreateUser(t, "reader")
	apply(edits, ack, map[string]string{"X-Access-Token": reader.Token, "Origin": "nogon://"}, http.StatusForbidden)
	apply(strings.Replace(edits, "Reviewed", "Unreviewed", 1), ack, headers, http.StatusConflict)
	apply(edits, ack, headers, http.StatusConflict)
	for _, restricted := range []string{
		`{"path":"app.directory","action":"set","value":"/tmp/unreviewed"}`,
		`{"path":"server.base_url","action":"set","value":"https://unreviewed.example"}`,
		`{"path":"server.proxy_auth_header","action":"set","value":"X-User"}`,
	} {
		ack = settingsReview(t, handler, headers, revision, edits)
		apply(strings.TrimSuffix(edits, "]")+","+restricted+"]", ack, headers, http.StatusUnprocessableEntity)
	}
	mixed := strings.TrimSuffix(edits, "]") + `,{"path":"app.user_handling","action":"set","value":false}]`
	ack = settingsReview(t, handler, headers, revision, mixed)
	apply(mixed, ack, headers, http.StatusAccepted)
}

func settingsReview(t *testing.T, handler http.Handler, headers map[string]string, revision, edits string) string {
	t.Helper()
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/validate", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, edits)), headers)
	var result struct {
		Review struct {
			Acknowledgment string `json:"acknowledgment"`
		} `json:"review"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || result.Review.Acknowledgment == "" {
		t.Fatalf("review: %d %s (%v)", response.Code, response.Body, err)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("review can be cached")
	}
	return result.Review.Acknowledgment
}

func assertSettingsUnchanged(t *testing.T, cfg *config.Config, handler http.Handler, headers map[string]string) func() {
	t.Helper()
	revision, values := readConfigurationState(t, handler, headers)
	before := map[string]string{}
	for _, path := range []string{cfg.Filename(), cfg.FullPath("config.web.yml")} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = string(data)
	}
	return func() {
		t.Helper()
		current, active := readConfigurationState(t, handler, headers)
		if current != revision || !reflect.DeepEqual(active, values) {
			t.Fatal("rejected access draft changed running settings")
		}
		for path, original := range before {
			data, err := os.ReadFile(path)
			if err != nil || string(data) != original {
				t.Fatalf("rejected access draft changed %s", path)
			}
		}
		if staged, err := filepath.Glob(cfg.FullPath(".config.web-*")); err != nil || len(staged) != 0 {
			t.Fatal("rejected access draft left staging files")
		}
	}
}

func TestSettingsAccessAcknowledgmentBindsWholeDraft(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	unchanged := assertSettingsUnchanged(t, cfg, handler, headers)
	revision, _ := readConfigurationState(t, handler, headers)
	const edits = `[{"path":"app.access_token","action":"set","value":{"action":"replace","value":"new-secret"}},{"path":"app.title","action":"set","value":"Reviewed"}]`
	apply := func(edits, acknowledgment string, want int) {
		t.Helper()
		body := fmt.Sprintf(`{"revision":%q,"edits":%s,"acknowledgment":%q}`, revision, edits, acknowledgment)
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
		if response.Code != want {
			t.Fatalf("apply: %d %s, want %d", response.Code, response.Body, want)
		}
		if want != http.StatusAccepted {
			unchanged()
		}
	}
	for _, modified := range []string{
		strings.Replace(edits, "new-secret", "different-secret", 1),
		strings.Replace(edits, "Reviewed", "Unreviewed", 1),
		`[{"path":"app.title","action":"set","value":"Reviewed"}]`,
	} {
		ack := settingsReview(t, handler, headers, revision, edits)
		apply(modified, ack, http.StatusConflict)
		// Even reverting the edit must require another explicit review.
		apply(edits, ack, http.StatusConflict)
	}
	ack := settingsReview(t, handler, headers, revision, edits)
	invalid := strings.Replace(edits, `"value":"Reviewed"`, `"value":42`, 1)
	apply(invalid, ack, http.StatusUnprocessableEntity)
	apply(edits, ack, http.StatusConflict)
	ack = settingsReview(t, handler, headers, revision, edits)
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/validate", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, invalid)), headers)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid revised draft: %d %s", response.Code, response.Body)
	}
	apply(edits, ack, http.StatusConflict)
	old := settingsReview(t, handler, headers, revision, edits)
	latest := settingsReview(t, handler, headers, revision, edits)
	apply(edits, old, http.StatusConflict)
	apply(edits, "forged", http.StatusConflict)
	apply(edits, latest, http.StatusAccepted)
}

func TestSettingsAccessAcknowledgmentBindsActor(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "user")
	other, err := model.CreateUser("other-owner", "password123", true)
	if err != nil {
		t.Fatal(err)
	}
	reader := testutil.CreateUser(t, "reader")
	unchanged := assertSettingsUnchanged(t, cfg, handler, map[string]string{"X-Access-Token": other.Token})
	revision, _ := readConfigurationState(t, handler, headers)
	const edits = `[{"path":"app.user_handling","action":"set","value":false}]`
	ack := settingsReview(t, handler, headers, revision, edits)
	body := fmt.Sprintf(`{"revision":%q,"edits":%s,"acknowledgment":%q}`, revision, edits, ack)
	for _, caller := range []struct {
		token  string
		status int
	}{{other.Token, http.StatusConflict}, {reader.Token, http.StatusForbidden}, {"", http.StatusForbidden}, {"secret", http.StatusForbidden}} {
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), map[string]string{"Origin": "nogon://", "X-Access-Token": caller.token})
		if response.Code != caller.status {
			t.Fatalf("unauthorized acknowledgment: %d %s", response.Code, response.Body)
		}
		unchanged()
	}
	owner, err := model.GetUser("review-owner")
	if err != nil {
		t.Fatal(err)
	}
	for _, admin := range []bool{false, true} {
		owner, err = model.UpdateUser(other.ID, owner.ID, owner.Revision, model.UserChanges{IsAdmin: &admin})
		if err != nil {
			t.Fatal(err)
		}
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
		want := http.StatusForbidden
		if admin {
			want = http.StatusConflict
		}
		if response.Code != want {
			t.Fatalf("lost authority revived acknowledgment: %d %s", response.Code, response.Body)
		}
		unchanged()
	}
}

func TestSettingsAccessAcknowledgmentBindsSession(t *testing.T) {
	for _, mode := range []string{"user", "token", "none"} {
		t.Run(mode, func(t *testing.T) {
			cfg, handler, headers := newAccessSettingsTestServer(t, mode)
			unchanged := assertSettingsUnchanged(t, cfg, handler, headers)
			revision, _ := readConfigurationState(t, handler, headers)
			edits := fmt.Sprintf(`[{"path":"server.address","action":"set","value":%q}]`, runtimeTestAddress(t))
			var cookies []*http.Cookie
			for range 2 {
				path, body := "/api/token-login", `{"token":"secret"}`
				switch mode {
				case "user":
					path, body = "/api/login", `{"username":"review-owner","password":"password123"}`
				case "none":
					path, body = "/api/settings/validate", fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, edits)
				}
				response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon"+path, strings.NewReader(body), map[string]string{"Origin": "nogon://"})
				if response.Code != http.StatusOK {
					t.Fatalf("establish session: %d %s", response.Code, response.Body)
				}
				cookies = append(cookies, responseSessionCookie(t, response))
			}
			initiator := map[string]string{"Origin": "nogon://", "Cookie": cookies[0].String()}
			ack := settingsReview(t, handler, initiator, revision, edits)
			body := fmt.Sprintf(`{"revision":%q,"edits":%s,"acknowledgment":%q}`, revision, edits, ack)
			for _, cookie := range []*http.Cookie{cookies[1], cookies[0]} {
				response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), map[string]string{"Origin": "nogon://", "Cookie": cookie.String()})
				want := http.StatusConflict
				if cookie == cookies[0] {
					want = http.StatusAccepted
				}
				if response.Code != want {
					t.Fatalf("session binding: %d %s, want %d", response.Code, response.Body, want)
				}
				if want == http.StatusAccepted {
					var pending pendingAccessStatus
					if err := json.Unmarshal(response.Body.Bytes(), &pending); err != nil {
						t.Fatal(err)
					}
					cancelBody := fmt.Sprintf(`{"revision":%q,"confirmation_token":%q}`, pending.Revision, pending.ConfirmationToken)
					cancel := testutil.ServeHTTP(t, handler, http.MethodPost, cfg.BaseURL("/api/settings/cancel"), strings.NewReader(cancelBody), initiator)
					if cancel.Code != http.StatusOK {
						t.Fatalf("cancel temporary change: %d %s", cancel.Code, cancel.Body)
					}
					revision, _ = readConfigurationState(t, handler, headers)
					unchanged = assertSettingsUnchanged(t, cfg, handler, headers)
				}
				unchanged()
			}
			ack = settingsReview(t, handler, initiator, revision, edits)
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/logout", nil, initiator)
			if response.Code != http.StatusOK {
				t.Fatalf("logout: %d %s", response.Code, response.Body)
			}
			body = fmt.Sprintf(`{"revision":%q,"edits":%s,"acknowledgment":%q}`, revision, edits, ack)
			response = testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), initiator)
			if response.Code != http.StatusForbidden {
				t.Fatalf("revoked session: %d %s", response.Code, response.Body)
			}
			unchanged()
		})
	}
}

func TestSettingsAccessAcknowledgmentExpiresWithConfiguration(t *testing.T) {
	for _, change := range []string{"save", "reload", "main file", "managed file"} {
		t.Run(change, func(t *testing.T) {
			cfg, handler, headers := newAccessSettingsTestServer(t, "token")
			revision, _ := readConfigurationState(t, handler, headers)
			const edits = `[{"path":"app.public","action":"set","value":false}]`
			ack := settingsReview(t, handler, headers, revision, edits)
			wantCode := "stale_revision"
			switch change {
			case "save":
				body := fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.title","action":"set","value":"Another save"}]}`, revision)
				response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
				if response.Code != http.StatusOK {
					t.Fatalf("ordinary save: %d %s", response.Code, response.Body)
				}
			case "reload":
				if status := reloadRequest(t, handler, "secret"); status != http.StatusOK {
					t.Fatalf("reload: %d", status)
				}
			default:
				filename := cfg.Filename()
				if change == "managed file" {
					filename = cfg.FullPath("config.web.yml")
				}
				data, err := os.ReadFile(filename)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filename, append(data, []byte("\n# external edit\n")...), 0o600); err != nil {
					t.Fatal(err)
				}
				wantCode = "sources_changed"
			}
			unchanged := assertSettingsUnchanged(t, cfg, handler, headers)
			body := fmt.Sprintf(`{"revision":%q,"edits":%s,"acknowledgment":%q}`, revision, edits, ack)
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
			if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"`+wantCode+`"`) {
				t.Fatalf("stale acknowledgment: %d %s", response.Code, response.Body)
			}
			unchanged()
			if wantCode == "stale_revision" {
				// Copying an old acknowledgment into a current revision cannot revive it.
				current, _ := readConfigurationState(t, handler, headers)
				response = testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(strings.Replace(body, revision, current, 1)), headers)
				if response.Code != http.StatusConflict {
					t.Fatalf("rebound acknowledgment: %d %s", response.Code, response.Body)
				}
				unchanged()
			}
		})
	}
}

func TestSettingsAccessAdministratorPrerequisite(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	unchanged := assertSettingsUnchanged(t, cfg, handler, headers)
	revision, _ := readConfigurationState(t, handler, headers)
	const edits = `[{"path":"app.user_handling","action":"set","value":true}]`
	validate := fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, edits)
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/validate", strings.NewReader(validate), headers)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "admin_required") {
		t.Fatalf("review without administrator: %d %s", response.Code, response.Body)
	}
	user, err := model.CreateOAuthUser("provider-owner", "oidc-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	response = testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/validate", strings.NewReader(validate), headers)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("ordinary user satisfied prerequisite: %d %s", response.Code, response.Body)
	}
	if _, err := model.SyncOAuthAdmin(user.OAuthID, true); err != nil {
		t.Fatal(err)
	}
	ack := settingsReview(t, handler, headers, revision, edits)
	if _, err := model.SyncOAuthAdmin(user.OAuthID, false); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"revision":%q,"edits":%s,"acknowledgment":%q}`, revision, edits, ack)
	response = testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "admin_required") {
		t.Fatalf("apply did not recheck administrator: %d %s", response.Code, response.Body)
	}
	unchanged()
	if _, err := model.SyncOAuthAdmin(user.OAuthID, true); err != nil {
		t.Fatal(err)
	}
	response = testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
	if response.Code != http.StatusConflict {
		t.Fatalf("failed attempt reused: %d %s", response.Code, response.Body)
	}
	unchanged()
}

func TestSettingsAccessReviewRequiresIndependentRecoveryAdministrator(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "token", "  oauth_only: true\n  oauth:\n    oidc:\n      client_id: nogon\n      client_secret: provider-secret\n      auth_url: https://provider.example/auth\n      token_url: https://provider.example/token\n      userinfo_url: https://provider.example/userinfo\n      admin_groups: [owners]\n")
	unchanged := assertSettingsUnchanged(t, cfg, handler, headers)
	revision, _ := readConfigurationState(t, handler, headers)
	user, err := model.CreateOAuthUser("provider-owner", "oidc-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.SyncOAuthAdmin(user.OAuthID, true); err != nil {
		t.Fatal(err)
	}
	const edits = `[{"path":"app.user_handling","action":"set","value":true}]`
	body := fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, edits)
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/validate", strings.NewReader(body), headers)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "independent local administrator") || strings.Contains(response.Body.String(), "provider-secret") {
		t.Fatalf("missing independent recovery: %d %s", response.Code, response.Body)
	}
	if _, err := model.CreateUser("recovery-owner", "password123", true); err != nil {
		t.Fatal(err)
	}
	settingsReview(t, handler, headers, revision, edits)
	unchanged()
}

func TestSettingsAccessReviewRedactsAndResolvesSecrets(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "user")
	unchanged := assertSettingsUnchanged(t, cfg, handler, headers)
	revision, _ := readConfigurationState(t, handler, headers)
	for _, edit := range []string{
		`{"path":"app.access_token","action":"set","value":{"action":"replace","value":"private-review-secret"}}`,
		`{"path":"app.access_token","action":"set","value":{"action":"clear"}}`,
		`{"path":"app.access_token","action":"reset"}`,
	} {
		body := fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.user_handling","action":"set","value":false},{"path":"app.public","action":"set","value":false},%s]}`, revision, edit)
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/validate", strings.NewReader(body), headers)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"secret"`) || strings.Contains(response.Body.String(), "private-review-secret") {
			t.Fatalf("secret review: %d %s", response.Code, response.Body)
		}
		want := "without signing in"
		if strings.Contains(edit, "replace") {
			want = "instance token"
		}
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("review did not describe effective access: %s", response.Body)
		}
		unchanged()
	}
}

func TestSettingsAccessReviewRetainsRestrictions(t *testing.T) {
	t.Setenv("HISTER__SERVER__ADDRESS", "127.0.0.1:0")
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	unchanged := assertSettingsUnchanged(t, cfg, handler, headers)
	revision, _ := readConfigurationState(t, handler, headers)
	for _, edit := range []string{
		`{"path":"server.address","action":"set","value":"0.0.0.0:5544"}`,
		`{"path":"server.base_url","action":"reset"}`,
		`{"path":"app.directory","action":"set","value":"private-input"}`,
		`{"path":"server.proxy_auth_header","action":"set","value":"private-input"}`,
		`{"path":"server.oauth.oidc.admin_groups","action":"set","value":["private-input"]}`,
		`{"path":"extractors.ytdlp.options.binary","action":"set","value":"private-input"}`,
		`{"path":"server","action":"set","value":{"address":"private-input"}}`,
		`{"path":"app.access_token","action":"set","value":{"action":"clear"}}`,
	} {
		// Retain public=true in the last case to test invalid public access.
		body := fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.public","action":"set","value":true},%s]}`, revision, edit)
		for _, operation := range []string{"validate", "apply"} {
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/"+operation, strings.NewReader(body), headers)
			if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), "private-input") || strings.Contains(response.Body.String(), `"acknowledgment"`) {
				t.Fatalf("restricted %s: %d %s", operation, response.Code, response.Body)
			}
			unchanged()
		}
	}
}

func TestSettingsAccessAcknowledgmentIsStrictAndSingleUse(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	unchanged := assertSettingsUnchanged(t, cfg, handler, headers)
	revision, _ := readConfigurationState(t, handler, headers)
	const edits = `[{"path":"app.public","action":"set","value":false}]`
	ack := settingsReview(t, handler, headers, revision, edits)
	for _, extra := range []string{
		`"confirmed":true`, `"acknowledgment":true`, `"acknowledgment":null`, `"acknowledgment":{}`, `"Acknowledgment":"wrong"`,
		fmt.Sprintf(`"acknowledgment":%q,"acknowledgment":%q`, ack, ack),
		fmt.Sprintf(`"acknowledgment":%q`, strings.Repeat("x", 129)),
	} {
		body := fmt.Sprintf(`{"revision":%q,"edits":%s,%s}`, revision, edits, extra)
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("ambiguous acknowledgment: %d %s", response.Code, response.Body)
		}
		unchanged()
	}
	body := fmt.Sprintf(`{"revision":%q,"edits":%s,"acknowledgment":%q}`, revision, edits, ack)
	wrongOrigin := maps.Clone(headers)
	wrongOrigin["Origin"] = "https://other.example"
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), wrongOrigin)
	if response.Code != http.StatusForbidden {
		t.Fatalf("CSRF bypass: %d %s", response.Code, response.Body)
	}
	responses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			responses <- testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
		}()
	}
	statuses := map[int]int{}
	for range 2 {
		statuses[(<-responses).Code]++
	}
	if statuses[http.StatusAccepted] != 1 || statuses[http.StatusConflict] != 1 {
		t.Fatalf("acknowledgment reused concurrently: %v", statuses)
	}
	loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
	if err != nil || !loaded.App.Public {
		t.Fatalf("unconfirmed selection was persisted: %v", err)
	}
	_, values := readConfigurationState(t, handler, headers)
	if values["app.public"].Value != false {
		t.Fatal("acknowledged change did not activate")
	}
}

func TestSettingsAccessRechecksAuthenticatedCredential(t *testing.T) {
	for _, operation := range []string{"validate", "apply"} {
		t.Run(operation, func(t *testing.T) {
			cfg, handler, headers := newAccessSettingsTestServer(t, "user")
			revision, _ := readConfigurationState(t, handler, headers)
			const edits = `[{"path":"app.user_handling","action":"set","value":false}]`
			extra := ""
			if operation == "apply" {
				extra = fmt.Sprintf(`,"acknowledgment":%q`, settingsReview(t, handler, headers, revision, edits))
			}
			body := fmt.Sprintf(`{"revision":%q,"edits":%s%s}`, revision, edits, extra)
			actor, err := model.GetUser("review-owner")
			if err != nil {
				t.Fatal(err)
			}
			reader, writer := io.Pipe()
			defer func() {
				_ = reader.Close()
				_ = writer.Close()
			}()
			response := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				response <- testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/"+operation, reader, headers)
			}()
			// Reading the body starts only after middleware has authenticated the caller.
			if _, err := writer.Write([]byte(body[:1])); err != nil {
				t.Fatal(err)
			}
			actor, err = model.RotateUserToken(actor.ID, actor.ID, actor.Revision)
			if err != nil {
				t.Fatal(err)
			}
			currentHeaders := maps.Clone(headers)
			currentHeaders["X-Access-Token"] = actor.Token
			unchanged := assertSettingsUnchanged(t, cfg, handler, currentHeaders)
			if _, err := io.WriteString(writer, body[1:]); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			result := <-response
			if result.Code != http.StatusForbidden || !strings.Contains(result.Body.String(), "admin_required") {
				t.Fatalf("revoked credential: %d %s", result.Code, result.Body)
			}
			unchanged()
		})
	}
}

func TestSettingsWebHotkeySourceLocks(t *testing.T) {
	for _, source := range []string{"file", "environment"} {
		t.Run(source, func(t *testing.T) {
			extra := "hotkeys:\n  web:\n    alt+x: focus_search_input\n"
			if source == "environment" {
				extra = ""
				t.Setenv("HISTER__HOTKEYS__WEB__ALT+X", "focus_search_input")
			}
			_, handler := newConfigurationTestServer(t, "token", extra)
			headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "private-token"}
			revision, values := readConfigurationState(t, handler, headers)
			if state := values["hotkeys.web"]; state.Editable || !state.Source.Locked || state.Value == nil {
				t.Fatalf("authoritative member must lock the whole visible collection: %+v", state)
			}
			for _, edit := range []string{
				`{"path":"hotkeys.web","action":"set","value":{}}`,
				`{"path":"hotkeys.web","action":"reset"}`,
				`{"path":"hotkeys","action":"set","value":{"web":{}}}`,
				`{"path":"hotkeys.web.alt+x","action":"set","value":"show_hotkeys"}`,
				`{"path":"hotkeys.tui","action":"set","value":{}}`,
			} {
				response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/validate", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[%s]}`, revision, edit)), headers)
				if response.Code != http.StatusUnprocessableEntity {
					t.Fatalf("locked or unsupported edit accepted: %d %s", response.Code, response.Body)
				}
			}
		})
	}
}

func TestSettingsExtractorDrafts(t *testing.T) {
	cfg, handler := newConfigurationTestServer(t, "token", "")
	headers := map[string]string{"Authorization": "Bearer private-token", "Origin": "nogon://", "Content-Type": "application/json"}
	revision, initial := readConfigurationState(t, handler, headers)
	if !initial["extractors.markdown.enable"].Editable || !initial["extractors.ytdlp.options.timeout"].Editable || initial["extractors.ytdlp.options.timeout"].Value != float64(15) {
		t.Fatal("extractor controls do not expose editable defaults")
	}
	edits := `[{"path":"extractors.ytdlp.options.timeout","action":"set","value":10},{"path":"extractors.ytdlp.options.fetch_subtitles","action":"set","value":true},{"path":"extractors.ytdlp.options.extra_domains","action":"set","value":["video.example"]},{"path":"semantic_search.semantic_weight","action":"set","value":0.65}]`
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/validate", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, edits)), headers)
	if response.Code != http.StatusOK {
		t.Fatalf("typed extractor draft rejected: %d %s", response.Code, response.Body)
	}
	var result struct {
		Values []config.FieldState `json:"values"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for _, value := range result.Values {
		if value.Path == "extractors.ytdlp.enable" && value.Value != false {
			t.Fatal("editing an option enabled the extractor")
		}
		if value.Path == "extractors.ytdlp.options.timeout" && value.Value != float64(10) {
			t.Fatal("typed extractor selection was lost")
		}
	}
	for _, edit := range []string{
		`{"path":"extractors.ytdlp.options.binary","action":"set","value":"private-executable"}`,
		`{"path":"extractors.ytdlp.options.extra_args","action":"set","value":["--exec","private-command"]}`,
		`{"path":"extractors.ytdlp.options","action":"set","value":{"binary":"private-executable"}}`,
		`{"path":"extractors.ytdlp.options.unknown","action":"set","value":"private-secret"}`,
		`{"path":"extractors.ytdlp.options.timeout","action":"set","value":-1}`,
		`{"path":"extractors.ytdlp.options.extra_domains","action":"set","value":["https://private-host/path"]}`,
	} {
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/validate", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[%s]}`, revision, edit)), headers)
		if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), "private-") {
			t.Errorf("unsafe or invalid extractor draft: %d %s", response.Code, response.Body)
		}
	}
	if cfg.Extractors["ytdlp"] != nil {
		t.Fatal("draft validation mutated the active extractor selections")
	}
}

func TestExtractorManagedSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, []byte("# defaults\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	current, _, err := config.LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	edits := []config.SettingEdit{
		{Path: "extractors.ytdlp.options.fetch_subtitles", Action: "set", Value: json.RawMessage(`true`)},
		{Path: "extractors.ytdlp.options.extra_domains", Action: "set", Value: json.RawMessage(`[]`)},
		{Path: "extractors.ytdlp.options.cookies_file", Action: "set", Value: json.RawMessage(`{"action":"replace","value":"private-cookie-file"}`)},
		{Path: "semantic_search.api_key", Action: "set", Value: json.RawMessage(`{"action":"replace","value":"private-api-key"}`)},
	}
	next, fields := prepareConfigurationDraft(current, edits, "first")
	if len(fields) != 0 {
		t.Fatal(fields)
	}
	write, err := next.StageManaged()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := write.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := write.Commit(); err != nil {
		t.Fatal(err)
	}
	reloaded, _, err := config.LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Extractors["ytdlp"].Enabled(false) || reloaded.Extractors["ytdlp"].Options["cookies_file"] != "private-cookie-file" || reloaded.SemanticSearch.APIKey != "private-api-key" {
		t.Fatal("restart lost extractor defaults or stored secrets")
	}
	diagnostic, err := reloaded.Redacted()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(diagnostic)
	if err != nil || strings.Contains(string(encoded), "private-") {
		t.Fatal("diagnostics exposed an extractor or semantic credential")
	}
	for _, state := range configurationStates(reloaded, "second") {
		if state.Path == "extractors.ytdlp.options.cookies_file" {
			value, err := json.Marshal(state.Value)
			if err != nil || strings.Contains(string(value), "private-cookie-file") {
				t.Fatal("extractor secret exposed")
			}
			kept, fields := prepareConfigurationDraft(reloaded, []config.SettingEdit{{Path: state.Path, Action: "set", Value: value}}, "second")
			if len(fields) != 0 || kept.Extractors["ytdlp"].Options["cookies_file"] != "private-cookie-file" {
				t.Fatalf("cannot preserve extractor secret: %v", fields)
			}
		}
	}
	reset, fields := prepareConfigurationDraft(reloaded, []config.SettingEdit{{Path: "extractors.ytdlp.options.fetch_subtitles", Action: "reset"}})
	if len(fields) != 0 || reset.Extractors["ytdlp"].Options["fetch_subtitles"] != nil {
		t.Fatalf("reset did not remove only the selected option: %v", fields)
	}
	for _, options := range []string{"binary: private-executable", "extra_args: [--exec, private-command]", "unknown: private-secret", "timeout: invalid", "timeout: -1"} {
		if err := os.WriteFile(filepath.Join(dir, "config.web.yml"), []byte("extractors:\n  ytdlp:\n    options:\n      "+options+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := config.LoadForInspection(filename, true); err == nil || strings.Contains(err.Error(), "private-") {
			t.Fatalf("unsafe managed extractor option accepted or disclosed: %v", err)
		}
	}
}

func TestSettingsDirectoryOwnership(t *testing.T) {
	_, handler := newConfigurationTestServer(t, "user", "")
	admin, err := model.CreateUser("settings-admin", "password123", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.CreateUser("file-owner", "password123", false); err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Origin": "nogon://", "X-Access-Token": admin.Token}
	revision, _ := readConfigurationState(t, handler, headers)
	dir := t.TempDir()
	for _, owner := range []string{"", "file-owner", "unknown-owner"} {
		body := fmt.Sprintf(`{"revision":%q,"edits":[{"path":"indexer.directories","action":"set","value":[{"path":%q,"user":%q,"delete_on_remove":true}]}]}`, revision, dir, owner)
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/validate", strings.NewReader(body), headers)
		want := http.StatusOK
		if owner == "unknown-owner" {
			want = http.StatusUnprocessableEntity
		}
		if response.Code != want {
			t.Fatalf("owner %q: %d %s", owner, response.Code, response.Body)
		}
	}
}

func TestSettingsUserHandlingChangesValidateDirectoryOwnership(t *testing.T) {
	current, _ := newConfigurationTestServer(t, "none", "")
	if _, err := model.CreateUser("file-owner", "password123", false); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	current, fields := prepareConfigurationDraft(current, []config.SettingEdit{
		{Path: "app.user_handling", Action: "set", Value: json.RawMessage(`true`)},
		{Path: "indexer.directories", Action: "set", Value: json.RawMessage(fmt.Sprintf(`[{"path":%q,"user":"file-owner"}]`, directory))},
	})
	if len(fields) != 0 {
		t.Fatal(fields)
	}
	for _, tc := range []struct {
		name  string
		edits []config.SettingEdit
		valid bool
	}{
		{"disable users", []config.SettingEdit{{Path: "app.user_handling", Action: "set", Value: json.RawMessage(`false`)}}, false},
		{"reset users", []config.SettingEdit{{Path: "app.user_handling", Action: "reset"}}, false},
		{"clear owner", []config.SettingEdit{
			{Path: "app.user_handling", Action: "set", Value: json.RawMessage(`false`)},
			{Path: "indexer.directories", Action: "set", Value: json.RawMessage(fmt.Sprintf(`[{"path":%q,"user":""}]`, directory))},
		}, true},
		{"remove directories", []config.SettingEdit{
			{Path: "app.user_handling", Action: "set", Value: json.RawMessage(`false`)},
			{Path: "indexer.directories", Action: "set", Value: json.RawMessage(`[]`)},
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate, fields := prepareConfigurationDraft(current, tc.edits)
			if tc.valid {
				if candidate == nil || len(fields) != 0 || candidate.App.UserHandling {
					t.Fatalf("valid single user configuration rejected: %v", fields)
				}
			} else if candidate != nil || len(fields) != 1 || fields[0].Path != "indexer.directories" || !strings.Contains(fields[0].Message, "owner requires multi user mode") {
				t.Fatalf("owned directories accepted without user handling: candidate=%v, errors=%v", candidate != nil, fields)
			}
			if !current.App.UserHandling || current.Indexer.Directories[0].User != "file-owner" {
				t.Fatal("draft validation changed the active directory ownership")
			}
		})
	}
}

func TestConfigurationDraftValidation(t *testing.T) {
	cfg, handler := newConfigurationTestServer(t, "token", "semantic_search:\n  api_key: private-api-key\ncrawler:\n  headers:\n    X-Key: private-header\n  proxy: http://private-proxy.example.com:8080\n")
	headers := map[string]string{"Authorization": "Bearer private-token", "Origin": "nogon://", "Content-Type": "application/json"}
	revision, initial := readConfigurationState(t, handler, headers)
	mainBefore, err := os.ReadFile(cfg.Filename())
	if err != nil {
		t.Fatal(err)
	}
	webBefore, err := os.ReadFile(cfg.FullPath("config.web.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, edits, path, code string }{
		{"set title", `[{"path":"app.title","action":"set","value":"Draft title"}]`, "", ""},
		{"reset title", `[{"path":"app.title","action":"reset"}]`, "", ""},
		{"false", `[{"path":"app.disable_previews","action":"set","value":false}]`, "", ""},
		{"empty title", `[{"path":"app.title","action":"set","value":""}]`, "", ""},
		{"unknown", `[{"path":"app.unknown","action":"set","value":"private-input"}]`, "app.unknown", "unknown_field"},
		{"parent bypass", `[{"path":"app","action":"set","value":{"access_token":"private-input"}}]`, "app", "unknown_field"},
		{"file lock", `[{"path":"app.subtitle","action":"reset"}]`, "app.subtitle", "locked"},
		{"storage", `[{"path":"app.directory","action":"set","value":"private-input"}]`, "app.directory", "read_only"},
		{"secret", `[{"path":"app.access_token","action":"replace","value":"private-input"}]`, "app.access_token", "locked"},
		{"secret clear", `[{"path":"semantic_search.api_key","action":"clear"}]`, "semantic_search.api_key", "locked"},
		{"extractor option", `[{"path":"extractors.ytdlp.options.binary","action":"set","value":"private-input"}]`, "extractors.ytdlp.options.binary", "unknown_field"},
		{"duplicate operation", `[{"path":"app.title","action":"set","value":"One"},{"path":"app.title","action":"reset"}]`, "app.title", "duplicate"},
		{"wrong type", `[{"path":"app.snippet_length","action":"set","value":"private-input"}]`, "app.snippet_length", "invalid_value"},
		{"out of range", `[{"path":"app.snippet_length","action":"set","value":0}]`, "app.snippet_length", "invalid_value"},
		{"reset value", `[{"path":"app.title","action":"reset","value":null}]`, "app.title", "invalid_action"},
		{"null", `[{"path":"app.title","action":"set","value":null}]`, "app.title", "invalid_value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/validate", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, tc.edits)), headers)
			var result struct {
				Errors []config.FieldError `json:"errors"`
				Values []config.FieldState `json:"values"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if tc.code != "" {
				if response.Code != http.StatusUnprocessableEntity || len(result.Errors) != 1 || result.Errors[0].Path != tc.path || result.Errors[0].Code != tc.code {
					t.Fatalf("rejection: %d %s", response.Code, response.Body)
				}
			} else {
				if response.Code != http.StatusOK {
					t.Fatalf("valid draft: %d %s", response.Code, response.Body)
				}
				values := map[string]any{}
				for _, field := range result.Values {
					values[field.Path] = field.Value
				}
				switch tc.name {
				case "set title":
					if values["app.title"] != "Draft title" {
						t.Fatal("candidate title missing")
					}
				case "reset title":
					if values["app.title"] != "Nogon" {
						t.Fatal("reset did not expose default")
					}
				case "false":
					if values["app.disable_previews"] != false {
						t.Fatal("false was lost")
					}
				case "empty title":
					if values["app.title"] != "" {
						t.Fatal("empty string was lost")
					}
				}
			}
			if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "private-") {
				t.Fatal("validation leaked a secret or allowed caching")
			}
		})
	}
	afterRevision, after := readConfigurationState(t, handler, headers)
	if afterRevision != revision || after["app.title"].Value != initial["app.title"].Value {
		t.Fatal("validation changed active state")
	}
	for name, before := range map[string][]byte{cfg.Filename(): mainBefore, cfg.FullPath("config.web.yml"): webBefore} {
		after, err := os.ReadFile(name)
		if err != nil || string(after) != string(before) {
			t.Fatal("validation changed configuration files")
		}
	}
}

func TestConfigurationRequestShape(t *testing.T) {
	for _, operation := range []string{"validate", "apply"} {
		t.Run(operation, func(t *testing.T) {
			_, handler := newConfigurationTestServer(t, "none", "")
			revision, _ := readConfigurationState(t, handler, nil)
			for _, body := range []string{
				`null`, `{}`, `[]`, `{"revision":%q}`, `{"revision":%q,"edits":null}`,
				`{"revision":%q,"edits":[],"unknown":"private-input"}`,
				`{"revision":%q,"edits":[]} {}`,
				`{"revision":%q,"edits":[{"path":"app.title","action":"set","value":"title","unknown":"private-input"}]}`,
				`{"revision":%q,"edits":[],"edits":[]}`,
				`{"revision":%q,"edits":[{"path":"app.subtitle","path":"app.title","action":"set","value":"title"}]}`,
				`{"Revision":%q,"edits":[]}`,
			} {
				body = strings.Replace(body, "%q", fmt.Sprintf("%q", revision), 1)
				response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/"+operation, strings.NewReader(body), map[string]string{"Origin": "nogon://", "Content-Type": "application/json"})
				if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "private-input") {
					t.Errorf("body %s: %d %s", body, response.Code, response.Body)
				}
			}
			for _, chunked := range []bool{false, true} {
				body := fmt.Sprintf(`{"revision":%q,"edits":[]}`, revision) + strings.Repeat(" ", int(maxRequestBodyBytes))
				request := httptest.NewRequest(http.MethodPost, "/nogon/api/settings/"+operation, strings.NewReader(body))
				request.Header.Set("Origin", "nogon://")
				if chunked {
					request.ContentLength = -1
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != http.StatusRequestEntityTooLarge {
					t.Fatalf("oversized body: %d", response.Code)
				}
			}
		})
	}
}

func TestConfigurationPendingSources(t *testing.T) {
	cfg, handler := newConfigurationTestServer(t, "token", "")
	headers := map[string]string{"Authorization": "Bearer private-token"}
	revision, _ := readConfigurationState(t, handler, headers)
	read := func() (*config.FieldState, string) {
		t.Helper()
		response := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/settings", nil, headers)
		var state struct {
			Revision string
			Values   []config.FieldState
			Pending  *struct {
				Status string
				Values []config.FieldState
			}
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &state) != nil {
			t.Fatalf("read settings: %d %s", response.Code, response.Body)
		}
		if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "private-token") || strings.Contains(response.Body.String(), "future-token") {
			t.Fatal("pending settings are cacheable or expose a token")
		}
		if state.Revision != revision {
			t.Fatal("reading pending settings changed the active revision")
		}
		for _, field := range state.Values {
			if field.Path == "app.title" && field.Value != "Web title" {
				t.Fatal("reading pending settings changed the active value")
			}
		}
		if state.Pending == nil {
			return nil, ""
		}
		for _, field := range state.Pending.Values {
			if field.Path == "app.title" {
				return &field, state.Pending.Status
			}
		}
		t.Fatal("missing pending title")
		return nil, ""
	}
	if pending, _ := read(); pending != nil {
		t.Fatal("unchanged configuration has pending values")
	}
	raw, err := os.ReadFile(cfg.Filename())
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(raw), "  subtitle: File subtitle", "  subtitle: File subtitle\n  title: File title", 1)
	updated = strings.Replace(updated, "private-token", "future-token", 1)
	if err := os.WriteFile(cfg.Filename(), []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	pending, status := read()
	if pending == nil || status != "changed" || pending.Value != "File title" || pending.Editable || pending.Source.Kind != "file" || !pending.Source.WebSelected {
		t.Fatalf("missing unapplied file selection: %+v (%s)", pending, status)
	}
	if cfg.App.Title != "Web title" {
		t.Fatal("inspection modified the running configuration")
	}
	t.Setenv("HISTER__APP__TITLE", "Environment title")
	pending, status = read()
	if pending == nil || status != "changed" || pending.Value != "Environment title" || pending.Source.Kind != "environment" || !pending.Source.Locked {
		t.Fatalf("pending selection ignored environment precedence: %+v (%s)", pending, status)
	}
	managed, err := os.ReadFile(cfg.FullPath("config.web.yml"))
	if err != nil || string(managed) != "app:\n  title: Web title\n" {
		t.Fatal("inspection modified web selections")
	}
}

func TestConfigurationPendingSourceFailures(t *testing.T) {
	for _, scenario := range []string{"missing file", "invalid YAML", "invalid setting", "invalid extractor"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, handler := newConfigurationTestServer(t, "token", "")
			raw, err := os.ReadFile(cfg.Filename())
			if err != nil {
				t.Fatal(err)
			}
			want := "invalid"
			switch scenario {
			case "missing file":
				want = "unreadable"
				err = os.Remove(cfg.Filename())
			case "invalid YAML":
				err = os.WriteFile(cfg.Filename(), []byte("app: [private-input"), 0o600)
			case "invalid setting":
				err = os.WriteFile(cfg.Filename(), append(raw, []byte("crawler:\n  backend: private-input\n")...), 0o600)
			case "invalid extractor":
				err = os.WriteFile(cfg.Filename(), append(raw, []byte("extractors:\n  basic:\n    options:\n      unknown: private-input\n")...), 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			response := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/settings", nil, map[string]string{"Authorization": "Bearer private-token"})
			var state struct {
				Pending *struct {
					Status string
					Values []config.FieldState
				}
			}
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &state) != nil || state.Pending == nil || state.Pending.Status != want || len(state.Pending.Values) != 0 {
				t.Fatalf("missing safe pending failure (%s): %d %s", want, response.Code, response.Body)
			}
			if strings.Contains(response.Body.String(), "private-input") || strings.Contains(response.Body.String(), "private-token") {
				t.Fatal("pending source failure exposed private configuration")
			}
			if cfg.App.Title != "Web title" {
				t.Fatal("failed inspection changed the running configuration")
			}
		})
	}
}

func TestConfigurationPendingReload(t *testing.T) {
	_, handler, write := newReloadTestServer(t, false)
	headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret"}
	revision, _ := readConfigurationState(t, handler, headers)
	write("  title: Reloaded title\n")
	read := func() *pendingConfiguration {
		t.Helper()
		response := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/settings", nil, headers)
		var state struct{ Pending *pendingConfiguration }
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &state) != nil {
			t.Fatalf("read settings: %d", response.Code)
		}
		return state.Pending
	}
	if pending := read(); pending == nil || pending.Status != "changed" {
		t.Fatal("external edit was not reported")
	}
	draft := func(revision string) *strings.Reader {
		return strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.subtitle","action":"set","value":"Web draft"}]}`, revision))
	}
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", draft(revision), headers)
	if response.Code != http.StatusConflict {
		t.Fatalf("save before reload: %d", response.Code)
	}
	if status := reloadRequest(t, handler, "secret"); status != http.StatusOK {
		t.Fatalf("reload: %d", status)
	}
	if read() != nil {
		t.Fatal("applied sources still reported as pending")
	}
	nextRevision, values := readConfigurationState(t, handler, headers)
	if nextRevision == revision || values["app.title"].Value != "Reloaded title" || !values["app.title"].Source.Locked {
		t.Fatal("reload did not publish the new value, lock and revision")
	}
	response = testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", draft(nextRevision), headers)
	if response.Code != http.StatusOK {
		t.Fatalf("save after reload: %d %s", response.Code, response.Body)
	}
}

func TestConfigurationStaleSources(t *testing.T) {
	for _, change := range []string{"revision", "main", "managed", "removed main", "invalid managed", "environment", "reload"} {
		t.Run(change, func(t *testing.T) {
			cfg, handler := newConfigurationTestServer(t, "none", "")
			revision, _ := readConfigurationState(t, handler, nil)
			switch change {
			case "revision":
				revision = "outdated"
			case "main":
				raw, err := os.ReadFile(cfg.Filename())
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cfg.Filename(), append(raw, []byte("# external edit\n")...), 0o600); err != nil {
					t.Fatal(err)
				}
			case "managed":
				if err := os.WriteFile(cfg.FullPath("config.web.yml"), []byte("app:\n  title: External\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "removed main":
				if err := os.Remove(cfg.Filename()); err != nil {
					t.Fatal(err)
				}
			case "invalid managed":
				if err := os.WriteFile(cfg.FullPath("config.web.yml"), []byte("[private-input"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "environment":
				t.Setenv("HISTER__APP__TITLE", "External")
			case "reload":
				next, err := cfg.Reload()
				if err != nil {
					t.Fatal(err)
				}
				handler = registerEndpoints(next, nil)
			}
			wantCode := "sources_changed"
			if change == "revision" || change == "reload" {
				wantCode = "stale_revision"
			}
			for _, operation := range []string{"validate", "apply"} {
				response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/"+operation, strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.title","action":"set","value":"Candidate"}]}`, revision)), map[string]string{"Origin": "nogon://"})
				if response.Code != http.StatusConflict || strings.Contains(response.Body.String(), "private-input") {
					t.Fatalf("stale draft: %d %s", response.Code, response.Body)
				}
				var failure struct{ Code string }
				if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil || failure.Code != wantCode {
					t.Fatalf("%s failure code: %s (%v), want %q", operation, response.Body, err, wantCode)
				}
			}
		})
	}
}

func TestConfigurationSecretsAndExtractorStates(t *testing.T) {
	const extra = "semantic_search:\n  api_key: private-api-key\n  embedding_endpoint: https://embeddings.example/v1/embeddings\ncrawler:\n  headers:\n    X-Key: private-header\n  cookies:\n    - name: session\n      value: private-cookie\n      domain: example.com\nextractors:\n  ytdlp:\n    options:\n      binary: private-binary\n      extra_args: [private-argument]\n"
	t.Setenv("HISTER__EXTRACTORS__YTDLP__OPTIONS__COOKIES_FILE", "private-cookie-file")
	cfg, handler := newConfigurationTestServer(t, "token", extra)
	headers := map[string]string{"Authorization": "Bearer private-token"}
	_, values := readConfigurationState(t, handler, headers)
	for _, path := range []string{"app.access_token", "semantic_search.api_key", "crawler.headers", "crawler.cookies", "extractors", "extractors.ytdlp.options.binary", "extractors.ytdlp.options.extra_args", "extractors.ytdlp.options.cookies_file"} {
		field, exists := values[path]
		if !exists || field.Value != nil || !field.SecretPresent || field.Editable {
			t.Errorf("unsafe or missing field %s: %+v", path, field)
		}
	}
	if field := values["semantic_search.embedding_endpoint"]; field.Value != "https://embeddings.example/v1/embeddings" || field.SecretPresent || field.Editable || !field.Source.Locked {
		t.Fatalf("file managed endpoint should be visible and locked: %+v", field)
	}
	if field := values["extractors.ytdlp.enable"]; field.Value != false || !field.Editable || field.Source.Kind != "default" {
		t.Errorf("extractor default: %+v", field)
	}
	if field := values["extractors.basic.enable"]; field.Value != true || !field.Editable {
		t.Errorf("basic default: %+v", field)
	}
	for path, source := range map[string]string{"extractors.ytdlp.options.extra_args": "file", "extractors.ytdlp.options.cookies_file": "environment"} {
		field := values[path]
		if field.Source.Kind != source || !field.Source.Locked || field.Reason == "" {
			t.Errorf("missing optional extractor source for %s: %+v", path, field)
		}
	}
	read := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/settings", nil, headers)
	var state struct {
		Fields []config.FieldMetadata `json:"fields"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	metadata := map[string]config.FieldMetadata{}
	for _, field := range state.Fields {
		metadata[field.Path] = field
	}
	for _, path := range []string{"extractors.ytdlp.options.extra_args", "extractors.ytdlp.options.cookies_file"} {
		field, exists := metadata[path]
		if !exists || field.Default != nil || field.Schema.Sensitivity == "none" {
			t.Errorf("unsafe or missing optional extractor metadata for %s: %+v", path, field)
		}
	}
	old := extractor.DefaultRegistry().List()
	t.Cleanup(func() {
		previous := map[string]*config.Extractor{}
		for _, entry := range old {
			previous[strings.ToLower(entry.Name)] = &config.Extractor{Enable: entry.Enabled, Options: entry.Options}
		}
		for _, candidate := range extractor.DefaultRegistry().Extractors() {
			if err := candidate.SetConfig(previous[strings.ToLower(candidate.Name())]); err != nil {
				t.Error(err)
			}
		}
	})
	if err := extractor.Init(cfg.Extractors); err != nil {
		t.Fatal(err)
	}
	cfg.App.DisplayExtractorConfig = true
	for _, path := range []string{"/api/config", "/api/extractors", "/api/settings"} {
		response := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon"+path, nil, headers)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-") {
			t.Errorf("secret exposure at %s: status %d", path, response.Code)
		}
		if path != "/api/settings" && (strings.Contains(response.Body.String(), cfg.App.Directory) || strings.Contains(response.Body.String(), `"revision"`) || strings.Contains(response.Body.String(), `"source"`)) {
			t.Errorf("private state leaked through %s", path)
		}
	}
}

func TestConfigurationSessionAndCSRF(t *testing.T) {
	for _, operation := range []string{"validate", "apply"} {
		t.Run(operation, func(t *testing.T) {
			for _, mode := range []string{"none", "token", "user"} {
				t.Run(mode, func(t *testing.T) {
					_, handler := newConfigurationTestServer(t, mode, "")
					var cookie *http.Cookie
					if mode != "none" {
						path, body := "/api/token-login", `{"token":"private-token"}`
						if mode == "user" {
							if _, err := model.CreateUser("admin", "password123", true); err != nil {
								t.Fatal(err)
							}
							path, body = "/api/login", `{"username":"admin","password":"password123"}`
						}
						login := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon"+path, strings.NewReader(body), map[string]string{"Origin": "nogon://"})
						if login.Code != http.StatusOK {
							t.Fatalf("login: %d %s", login.Code, login.Body)
						}
						cookie = responseSessionCookie(t, login)
					}
					request := httptest.NewRequest(http.MethodGet, "/nogon/api/config", nil)
					if cookie != nil {
						request.AddCookie(cookie)
					}
					public := httptest.NewRecorder()
					handler.ServeHTTP(public, request)
					cookie = responseSessionCookie(t, public)
					revision, _ := readConfigurationState(t, handler, map[string]string{"Cookie": cookie.String()})
					for _, origin := range []string{"", "https://other.example", "moz-extension://extension", "chrome-extension://extension", "safari-web-extension://extension"} {
						for _, csrf := range []string{"", "wrong", public.Header().Get("X-CSRF-Token")} {
							request := httptest.NewRequest(http.MethodPost, "/nogon/api/settings/"+operation, strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[]}`, revision)))
							request.AddCookie(cookie)
							request.Header.Set("Content-Type", "application/json")
							request.Header.Set("Origin", origin)
							request.Header.Set("X-CSRF-Token", csrf)
							response := httptest.NewRecorder()
							handler.ServeHTTP(response, request)
							want := http.StatusForbidden
							if csrf != "" && csrf != "wrong" {
								want = http.StatusOK
								if operation == "apply" {
									want = http.StatusServiceUnavailable
								}
							}
							if response.Code != want {
								t.Fatalf("origin %q csrf %q: %d %s", origin, csrf, response.Code, response.Body)
							}
							// Successful requests rotate the CSRF token.
							if want != http.StatusForbidden {
								public = response
								cookie = responseSessionCookie(t, response)
							}
						}
					}
				})
			}
		})
	}
}

func TestConfigurationWithoutMainFilePreservesStartupFlags(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	overrides := []config.Override{{Path: "app.show_suggestions", Value: false, Source: config.ValueSource{Kind: "flag", Name: "--show-suggestions"}}}
	cfg, err := config.Load("", overrides...)
	if err != nil {
		t.Fatal(err)
	}
	testutil.InitModelWithConfig(t, cfg)
	sessionStore = newSessionStore(cfg.SecretKey(), cfg.BaseURL(""), sessionMaxAge)
	handler := registerEndpoints(cfg, nil, overrides...)
	headers := map[string]string{"Origin": "nogon://"}
	response := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/settings", nil, headers)
	var state struct {
		Revision string              `json:"revision"`
		Values   []config.FieldState `json:"values"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil || response.Code != http.StatusOK {
		t.Fatalf("read: %d %s", response.Code, response.Body)
	}
	for _, field := range state.Values {
		if field.Path == "app.show_suggestions" && (field.Value != false || field.Editable || field.Source.Kind != "flag") {
			t.Fatalf("explicit false flag lost: %+v", field)
		}
	}
	for _, tc := range []struct {
		path   string
		status int
	}{{"app.title", http.StatusOK}, {"app.show_suggestions", http.StatusUnprocessableEntity}} {
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/settings/validate", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":%q,"action":"reset"}]}`, state.Revision, tc.path)), headers)
		if response.Code != tc.status {
			t.Fatalf("draft %s: %d %s", tc.path, response.Code, response.Body)
		}
	}
	if _, err := os.Stat(cfg.FullPath("config.web.yml")); !os.IsNotExist(err) {
		t.Fatalf("validation created managed configuration: %v", err)
	}
}

func TestConfigurationCatalogKeepsUnsupportedCategoriesReadOnly(t *testing.T) {
	fields := configurationCatalog(config.CreateDefaultConfig())
	var app, enable, option bool
	for _, field := range fields {
		switch field.Path {
		case "app.title":
			app = field.ReadOnlyReason == "" && field.Default == "Nogon"
		case "extractors.ytdlp.enable":
			enable = field.ReadOnlyReason == "" && field.Default == false
		case "extractors.ytdlp.options.binary":
			option = field.ReadOnlyReason != "" && field.Schema.Sensitivity == "secret" && field.Default == nil
		}
	}
	if !app || !enable || !option {
		t.Fatal("combined catalog lost defaults or enabled an unsupported setting")
	}
}

func newReloadTestServer(t *testing.T, multiUser bool) (*config.Config, http.Handler, func(string)) {
	t.Helper()
	dir := t.TempDir()
	filename := filepath.Join(dir, "config.yml")
	write := func(extra string) {
		t.Helper()
		content := fmt.Sprintf("app:\n  directory: %q\n  access_token: secret\n  user_handling: %t\n  public: true\n  title: Original\nserver:\n  address: 127.0.0.1:0\n  base_url: http://127.0.0.1:4433/nogon\nindexer:\n  detect_languages: false\n", dir, multiUser)
		content = strings.Replace(content, "  title: Original\n", extra, 1)
		if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("  title: Original\n")
	cfg, err := config.Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	testutil.InitModelWithConfig(t, cfg)
	if multiUser {
		if _, err := model.CreateUser("runtime-owner", "password123", true); err != nil {
			t.Fatal(err)
		}
	}
	return cfg, startReloadTestRuntime(t, cfg), write
}

func startReloadTestRuntime(t *testing.T, cfg *config.Config) http.Handler {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &serverRuntime{ctx: ctx, failures: make(chan error, 1)}
	listener, err := newListener(cfg.Server)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	runtime.current, err = runtime.generation(cfg, newServerTestIndexer(t, cfg))
	if err != nil {
		cancel()
		_ = listener.Close()
		t.Fatal(err)
	}
	runtime.serving = runtime.startListener(listener)
	t.Cleanup(func() { cancel(); runtime.close() })
	return runtime
}

func reloadRequest(t *testing.T, handler http.Handler, token string) int {
	t.Helper()
	rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/reload", nil, map[string]string{"Origin": "nogon://", "X-Access-Token": token})
	return rec.Code
}

func TestReloadAppliesConfigurationAndRules(t *testing.T) {
	cfg, handler, write := newReloadTestServer(t, false)
	write("  title: Reloaded\n  color_scheme: dark\n")
	if err := os.WriteFile(cfg.RulesPath(), []byte(`{"skip":["private"],"aliases":{"docs":"documentation"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := reloadRequest(t, handler, "secret"); status != http.StatusOK {
		t.Fatalf("reload status = %d, want 200", status)
	}
	rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/config", nil, nil)
	var result struct{ Title, ColorScheme string }
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Title != "Reloaded" || result.ColorScheme != "dark" {
		t.Fatalf("configuration was not reloaded: %+v", result)
	}
	rec = testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/rules", nil, map[string]string{"X-Access-Token": "secret"})
	if !strings.Contains(rec.Body.String(), "private") || !strings.Contains(rec.Body.String(), "documentation") {
		t.Fatalf("rules were not reloaded: %s", rec.Body)
	}
	// Subsequent rule writes must update the active generation and its file.
	headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret", "Content-Type": "application/x-www-form-urlencoded"}
	rec = testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/rules", strings.NewReader(url.Values{"skip": {"updated"}}.Encode()), headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("rule update failed: %d %s", rec.Code, rec.Body)
	}
	write("")
	if status := reloadRequest(t, handler, "secret"); status != http.StatusOK {
		t.Fatalf("second reload status = %d", status)
	}
	rec = testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/config", nil, nil)
	if !strings.Contains(rec.Body.String(), `"title":"Nogon"`) {
		t.Fatalf("removed settings did not revert to defaults: %s", rec.Body)
	}
	rec = testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/rules", nil, headers)
	if !strings.Contains(rec.Body.String(), "updated") || strings.Contains(rec.Body.String(), "private") {
		t.Fatalf("reload lost a rule update: %s", rec.Body)
	}
}

func TestReloadRejectsInvalidConfigurationWithoutChangingServer(t *testing.T) {
	for _, failure := range []string{"invalid YAML", "missing config", "invalid rules", "missing rules", "log file", "log format", "invalid extractor", "invalid sensitive pattern"} {
		t.Run(failure, func(t *testing.T) {
			cfg, handler, write := newReloadTestServer(t, false)
			write("  title: Must not apply\n")
			var err error
			switch failure {
			case "invalid YAML":
				err = os.WriteFile(cfg.Filename(), []byte("app: ["), 0o600)
			case "missing config":
				err = os.Remove(cfg.Filename())
			case "invalid rules":
				err = os.WriteFile(cfg.RulesPath(), []byte(`{"skip":["["]}`), 0o600)
			case "missing rules":
				err = os.Remove(cfg.RulesPath())
			case "log file":
				write(fmt.Sprintf("  title: Must not apply\n  log_file: %q\n", filepath.Join(t.TempDir(), "missing", "nogon.log")))
			case "log format":
				write("  title: Must not apply\n  log_format: invalid\n")
			case "invalid extractor":
				write("  title: Must not apply\nextractors:\n  nonexistent:\n    enabled: true\n")
			case "invalid sensitive pattern":
				write("  title: Must not apply\nsensitive_content_patterns:\n  test: '['\n")
			}
			if err != nil {
				t.Fatal(err)
			}
			rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/reload", nil, map[string]string{"Origin": "nogon://", "X-Access-Token": "secret"})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("reload status = %d, want 400: %s", rec.Code, rec.Body)
			}
			if strings.HasPrefix(failure, "log ") && !strings.Contains(rec.Body.String(), "app.log_") {
				t.Fatalf("missing logging error explanation: %s", rec.Body)
			}
			rec = testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/config", nil, nil)
			if !strings.Contains(rec.Body.String(), `"title":"Original"`) {
				t.Fatalf("failed reload changed active configuration: %s", rec.Body)
			}
			if failure == "missing rules" {
				if _, err := os.Stat(cfg.RulesPath()); !os.IsNotExist(err) {
					t.Fatalf("reload recreated missing rules: %v", err)
				}
			}
		})
	}
}

func TestReloadConcurrentRequestsAndRuleUpdates(t *testing.T) {
	_, handler, _ := newReloadTestServer(t, false)
	var workers sync.WaitGroup
	for worker := range 3 {
		workers.Go(func() {
			for range 8 {
				method, path, body := http.MethodGet, "/api/rules", ""
				switch worker {
				case 1:
					method, path = http.MethodPost, "/api/reload"
				case 2:
					method, path, body = http.MethodPost, "/api/add_alias", "alias-keyword=docs&alias-value=documentation"
				}
				rec := testutil.ServeHTTP(t, handler, method, "/nogon"+path, strings.NewReader(body), map[string]string{"Origin": "nogon://", "X-Access-Token": "secret", "Content-Type": "application/x-www-form-urlencoded"})
				if rec.Code != http.StatusOK && rec.Code != http.StatusConflict && rec.Code != http.StatusServiceUnavailable {
					t.Errorf("%s: status %d, body %s", path, rec.Code, rec.Body)
				}
			}
		})
	}
	workers.Wait()
}

func TestReloadAuthorization(t *testing.T) {
	for _, multiUser := range []bool{false, true} {
		t.Run(fmt.Sprint(multiUser), func(t *testing.T) {
			_, handler, _ := newReloadTestServer(t, multiUser)
			adminToken := "secret"
			forbiddenTokens := []string{"", "wrong"}
			if multiUser {
				user := testutil.CreateUser(t, "reader")
				admin, err := model.CreateUser("admin", "password123", true)
				if err != nil {
					t.Fatal(err)
				}
				adminToken = admin.Token
				forbiddenTokens = append(forbiddenTokens, user.Token, "secret")
			}
			for _, token := range forbiddenTokens {
				if status := reloadRequest(t, handler, token); status != http.StatusForbidden {
					t.Fatalf("unauthorized reload status = %d, want 403", status)
				}
			}
			rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/reload", nil, map[string]string{"X-Access-Token": adminToken, "Origin": "https://other.example"})
			if rec.Code != http.StatusForbidden {
				t.Fatalf("cross origin reload status = %d, want 403", rec.Code)
			}
			if status := reloadRequest(t, handler, adminToken); status != http.StatusOK {
				t.Fatalf("authorized reload status = %d, want 200", status)
			}
		})
	}
}

func TestReloadPreservesAuthenticatedSession(t *testing.T) {
	_, handler, write := newReloadTestServer(t, false)
	login := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/token-login", strings.NewReader(`{"token":"secret"}`), map[string]string{"Origin": "nogon://", "Content-Type": "application/json"})
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body)
	}
	cookie := responseSessionCookie(t, login)
	write("  title: Reloaded\n")
	if status := reloadRequest(t, handler, "secret"); status != http.StatusOK {
		t.Fatalf("reload: %d", status)
	}
	req := httptest.NewRequest(http.MethodGet, "/nogon/api/history", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session lost after reload: %d %s", rec.Code, rec.Body)
	}
}
