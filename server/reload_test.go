package server

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/extractor"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"
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
	raw := fmt.Sprintf("app:\n  directory: %q\n  subtitle: File subtitle\n%sserver:\n  address: 127.0.0.1:4433\n  base_url: http://127.0.0.1:4433/hister\n%s", dir, app, extra)
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
					login := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/login", strings.NewReader(fmt.Sprintf(`{"username":%q,"password":"password123"}`, account.Username)), map[string]string{"Origin": "hister://"})
					if login.Code != http.StatusOK {
						t.Fatalf("login: %d %s", login.Code, login.Body)
					}
					callers = append(callers, caller{account.Username + " session", map[string]string{"Cookie": responseSessionCookie(t, login).String()}, account.IsAdmin})
				}
			}
			for _, caller := range callers {
				t.Run(caller.name, func(t *testing.T) {
					public := testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/config", nil, caller.headers)
					var capability struct {
						CanConfigure *bool `json:"canConfigure"`
					}
					if err := json.Unmarshal(public.Body.Bytes(), &capability); err != nil || capability.CanConfigure == nil || *capability.CanConfigure != caller.allowed {
						t.Fatalf("incorrect configuration capability: %d %s", public.Code, public.Body)
					}
					response := testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/settings", nil, caller.headers)
					want := http.StatusForbidden
					if caller.allowed {
						want = http.StatusOK
					}
					if response.Code != want {
						t.Fatalf("read status = %d, want %d: %s", response.Code, want, response.Body)
					}
					if !caller.allowed {
						for _, operation := range []string{"validate", "apply"} {
							denied := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/"+operation, strings.NewReader(`{"revision":"unknown","edits":[]}`), caller.headers)
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
					if token := states["app.access_token"]; token.Value != nil || token.SecretPresent != (mode != "none") {
						t.Fatalf("token state: %+v", token)
					}
					headers := maps.Clone(caller.headers)
					if headers == nil {
						headers = map[string]string{}
					}
					headers["Origin"] = "hister://"
					validation := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/validate", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[]}`, state.Revision)), headers)
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
				if !paths["extractors.ytdlp"] || !paths["extractors.basic"] {
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
			if c.App.Title != "Hister" {
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
	response := testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/settings", nil, headers)
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

func TestConfigurationDraftValidation(t *testing.T) {
	cfg, handler := newConfigurationTestServer(t, "token", "semantic_search:\n  api_key: private-api-key\ncrawler:\n  headers:\n    X-Key: private-header\n  proxy: https://user:private-proxy@example.com\n")
	headers := map[string]string{"Authorization": "Bearer private-token", "Origin": "hister://", "Content-Type": "application/json"}
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
		{"secret", `[{"path":"app.access_token","action":"replace","value":"private-input"}]`, "app.access_token", "read_only"},
		{"secret clear", `[{"path":"semantic_search.api_key","action":"clear"}]`, "semantic_search.api_key", "read_only"},
		{"extractor option", `[{"path":"extractors.ytdlp.options.binary","action":"set","value":"private-input"}]`, "extractors.ytdlp.options.binary", "unknown_field"},
		{"duplicate operation", `[{"path":"app.title","action":"set","value":"One"},{"path":"app.title","action":"reset"}]`, "app.title", "duplicate"},
		{"wrong type", `[{"path":"app.snippet_length","action":"set","value":"private-input"}]`, "app.snippet_length", "invalid_value"},
		{"out of range", `[{"path":"app.snippet_length","action":"set","value":0}]`, "app.snippet_length", "invalid_value"},
		{"reset value", `[{"path":"app.title","action":"reset","value":null}]`, "app.title", "invalid_action"},
		{"null", `[{"path":"app.title","action":"set","value":null}]`, "app.title", "invalid_value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/validate", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, tc.edits)), headers)
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
					if values["app.title"] != "Hister" {
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
				response := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/"+operation, strings.NewReader(body), map[string]string{"Origin": "hister://", "Content-Type": "application/json"})
				if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "private-input") {
					t.Errorf("body %s: %d %s", body, response.Code, response.Body)
				}
			}
			for _, chunked := range []bool{false, true} {
				body := fmt.Sprintf(`{"revision":%q,"edits":[]}`, revision) + strings.Repeat(" ", int(maxRequestBodyBytes))
				request := httptest.NewRequest(http.MethodPost, "/hister/api/settings/"+operation, strings.NewReader(body))
				request.Header.Set("Origin", "hister://")
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
				response := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/"+operation, strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.title","action":"set","value":"Candidate"}]}`, revision)), map[string]string{"Origin": "hister://"})
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
	const extra = "semantic_search:\n  api_key: private-api-key\n  embedding_endpoint: https://user:private-url@example.com/private-path\ncrawler:\n  headers:\n    X-Key: private-header\n  cookies:\n    - name: session\n      value: private-cookie\nextractors:\n  ytdlp:\n    options:\n      binary: private-binary\n      extra_args: [private-argument]\n"
	t.Setenv("HISTER__EXTRACTORS__YTDLP__OPTIONS__COOKIES_FILE", "private-cookie-file")
	cfg, handler := newConfigurationTestServer(t, "token", extra)
	headers := map[string]string{"Authorization": "Bearer private-token"}
	_, values := readConfigurationState(t, handler, headers)
	for _, path := range []string{"app.access_token", "semantic_search.api_key", "semantic_search.embedding_endpoint", "crawler.headers", "crawler.cookies", "extractors", "extractors.ytdlp.options.binary", "extractors.ytdlp.options.extra_args", "extractors.ytdlp.options.cookies_file"} {
		field, exists := values[path]
		if !exists || field.Value != nil || !field.SecretPresent || field.Editable {
			t.Errorf("unsafe or missing field %s: %+v", path, field)
		}
	}
	if field := values["extractors.ytdlp.enable"]; field.Value != false || field.Editable || field.Source.Kind != "default" {
		t.Errorf("extractor default: %+v", field)
	}
	if field := values["extractors.basic.enable"]; field.Value != true || field.Editable {
		t.Errorf("basic default: %+v", field)
	}
	for path, source := range map[string]string{"extractors.ytdlp.options.extra_args": "file", "extractors.ytdlp.options.cookies_file": "environment"} {
		field := values[path]
		if field.Source.Kind != source || !field.Source.Locked || field.Reason == "" {
			t.Errorf("missing optional extractor source for %s: %+v", path, field)
		}
	}
	read := testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/settings", nil, headers)
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
		if !exists || field.Default != nil || field.Schema.Type != "unknown" || field.Schema.Sensitivity != "unknown" || field.ReadOnlyReason == "" {
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
		response := testutil.ServeHTTP(t, handler, http.MethodGet, "/hister"+path, nil, headers)
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
						login := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister"+path, strings.NewReader(body), map[string]string{"Origin": "hister://"})
						if login.Code != http.StatusOK {
							t.Fatalf("login: %d %s", login.Code, login.Body)
						}
						cookie = responseSessionCookie(t, login)
					}
					request := httptest.NewRequest(http.MethodGet, "/hister/api/config", nil)
					if cookie != nil {
						request.AddCookie(cookie)
					}
					public := httptest.NewRecorder()
					handler.ServeHTTP(public, request)
					cookie = responseSessionCookie(t, public)
					revision, _ := readConfigurationState(t, handler, map[string]string{"Cookie": cookie.String()})
					for _, origin := range []string{"", "https://other.example", "moz-extension://extension", "chrome-extension://extension", "safari-web-extension://extension"} {
						for _, csrf := range []string{"", "wrong", public.Header().Get("X-CSRF-Token")} {
							request := httptest.NewRequest(http.MethodPost, "/hister/api/settings/"+operation, strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[]}`, revision)))
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
	headers := map[string]string{"Origin": "hister://"}
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
			app = field.ReadOnlyReason == "" && field.Default == "Hister"
		case "extractors.ytdlp.enable":
			enable = field.ReadOnlyReason != "" && field.Default == false
		case "extractors.ytdlp.options.binary":
			option = field.ReadOnlyReason != "" && field.Schema.Type == "unknown" && field.Default == nil
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
		content := fmt.Sprintf("app:\n  directory: %q\n  access_token: secret\n  user_handling: %t\n  public: true\n  title: Original\nserver:\n  address: 127.0.0.1:0\n  base_url: http://127.0.0.1:4433/hister\nindexer:\n  detect_languages: false\n", dir, multiUser)
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
	rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/reload", nil, map[string]string{"Origin": "hister://", "X-Access-Token": token})
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
	rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/config", nil, nil)
	var result struct{ Title, ColorScheme string }
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Title != "Reloaded" || result.ColorScheme != "dark" {
		t.Fatalf("configuration was not reloaded: %+v", result)
	}
	rec = testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/rules", nil, map[string]string{"X-Access-Token": "secret"})
	if !strings.Contains(rec.Body.String(), "private") || !strings.Contains(rec.Body.String(), "documentation") {
		t.Fatalf("rules were not reloaded: %s", rec.Body)
	}
	// Subsequent rule writes must update the active generation and its file.
	headers := map[string]string{"Origin": "hister://", "X-Access-Token": "secret", "Content-Type": "application/x-www-form-urlencoded"}
	rec = testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/rules", strings.NewReader(url.Values{"skip": {"updated"}}.Encode()), headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("rule update failed: %d %s", rec.Code, rec.Body)
	}
	write("")
	if status := reloadRequest(t, handler, "secret"); status != http.StatusOK {
		t.Fatalf("second reload status = %d", status)
	}
	rec = testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/config", nil, nil)
	if !strings.Contains(rec.Body.String(), `"title":"Hister"`) {
		t.Fatalf("removed settings did not revert to defaults: %s", rec.Body)
	}
	rec = testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/rules", nil, headers)
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
				write(fmt.Sprintf("  title: Must not apply\n  log_file: %q\n", filepath.Join(t.TempDir(), "missing", "hister.log")))
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
			rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/reload", nil, map[string]string{"Origin": "hister://", "X-Access-Token": "secret"})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("reload status = %d, want 400: %s", rec.Code, rec.Body)
			}
			if strings.HasPrefix(failure, "log ") && !strings.Contains(rec.Body.String(), "app.log_") {
				t.Fatalf("missing logging error explanation: %s", rec.Body)
			}
			rec = testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/config", nil, nil)
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
				rec := testutil.ServeHTTP(t, handler, method, "/hister"+path, strings.NewReader(body), map[string]string{"Origin": "hister://", "X-Access-Token": "secret", "Content-Type": "application/x-www-form-urlencoded"})
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
			rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/reload", nil, map[string]string{"X-Access-Token": adminToken, "Origin": "https://other.example"})
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
	login := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/token-login", strings.NewReader(`{"token":"secret"}`), map[string]string{"Origin": "hister://", "Content-Type": "application/json"})
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body)
	}
	cookie := responseSessionCookie(t, login)
	write("  title: Reloaded\n")
	if status := reloadRequest(t, handler, "secret"); status != http.StatusOK {
		t.Fatalf("reload: %d", status)
	}
	req := httptest.NewRequest(http.MethodGet, "/hister/api/history", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session lost after reload: %d %s", rec.Code, rec.Body)
	}
}
