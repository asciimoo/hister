package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/extractor"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"
	"github.com/asciimoo/hister/server/types"
)

func TestSettingsDirectoryOwnerRemovedWhileDraining(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	oldEndpoints := Endpoints
	Endpoints = append(append([]*Endpoint(nil), Endpoints...), &Endpoint{
		Path: "/api/test-remove-owner", Method: GET,
		Handler: func(c *webContext) {
			close(entered)
			<-release
			if err := model.DeleteUser("directory-owner"); err != nil {
				t.Error(err)
			}
			serve200(c)
		},
	})
	t.Cleanup(func() { Endpoints = oldEndpoints })
	cfg, handler, _ := newReloadTestServer(t, true)
	admin, err := model.GetUser("runtime-owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.CreateUser("directory-owner", "password123", false); err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Origin": "hister://", "X-Access-Token": admin.Token}
	revision, _ := readConfigurationState(t, handler, headers)
	held := make(chan struct{})
	go func() {
		testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/test-remove-owner", nil, headers)
		close(held)
	}()
	<-entered
	body := fmt.Sprintf(`{"revision":%q,"edits":[{"path":"indexer.directories","action":"set","value":[{"path":%q,"user":"directory-owner"}]}]}`, revision, t.TempDir())
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/apply", strings.NewReader(body), headers)
	}()
	waitRuntimePause(t, handler)
	unblock()
	<-held
	response := <-result
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("removed owner accepted: %d %s", response.Code, response.Body)
	}
	if _, err := os.Stat(cfg.FullPath("config.web.yml")); !os.IsNotExist(err) {
		t.Fatal("removed owner persisted")
	}
	fresh, _ := readConfigurationState(t, handler, headers)
	if fresh != revision {
		t.Fatal("removed owner replaced runtime")
	}
}

func TestSettingsDirectoryValidation(t *testing.T) {
	cfg, handler, _ := newReloadTestServer(t, false)
	headers := map[string]string{"Origin": "hister://", "X-Access-Token": "secret"}
	revision, _ := readConfigurationState(t, handler, headers)
	dir := t.TempDir()
	file := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dirs := range [][]map[string]any{
		{{"path": filepath.Join(dir, "missing")}},
		{{"path": file}},
		{{"path": dir, "user": "missing-owner"}},
		{{"path": dir}, {"path": dir}},
	} {
		edits, err := json.Marshal([]map[string]any{{"path": "indexer.directories", "action": "set", "value": dirs}})
		if err != nil {
			t.Fatal(err)
		}
		for _, op := range []string{"validate", "apply"} {
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/"+op, strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, edits)), headers)
			if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"path":"indexer.directories"`) {
				t.Errorf("%s accepted invalid directories: %d %s", op, response.Code, response.Body)
			}
		}
	}
	if _, err := os.Stat(cfg.FullPath("config.web.yml")); !os.IsNotExist(err) {
		t.Fatalf("invalid draft was persisted: %v", err)
	}
	fresh, _ := readConfigurationState(t, handler, headers)
	if fresh != revision {
		t.Fatal("invalid draft replaced runtime")
	}
}

func TestSettingsIndexerPersistenceAndWatching(t *testing.T) {
	cfg, handler, _ := newReloadTestServer(t, false)
	mainPath := filepath.Join(cfg.App.Directory, "config.yml")
	original, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Origin": "hister://", "X-Access-Token": "secret"}
	dir := t.TempDir()
	filename := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(filename, []byte("Saved watcher content"), 0o600); err != nil {
		t.Fatal(err)
	}
	save := func(edits string) {
		t.Helper()
		revision, _ := readConfigurationState(t, handler, headers)
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, edits)), headers)
		if response.Code != http.StatusOK {
			t.Fatalf("save: %d %s", response.Code, response.Body)
		}
	}
	save(fmt.Sprintf(`[{"path":"indexer.directories","action":"set","value":[{"path":%q,"label":"web-watched","filetypes":["txt"],"delete_on_remove":true}]},{"path":"indexer.languages","action":"set","value":["en","hu"]},{"path":"indexer.max_file_size_mb","action":"set","value":8}]`, dir))
	loaded, _, err := config.LoadForInspection(mainPath, true)
	if err != nil || len(loaded.Indexer.Directories) != 1 || loaded.Indexer.Directories[0].Path != dir || !loaded.Indexer.Directories[0].DeleteOnRemove || loaded.Indexer.MaxFileSize != 8 || !slices.Equal(loaded.Indexer.Languages, []string{"en", "hu"}) {
		t.Fatalf("restart lost indexer settings: %v", err)
	}
	path := "/hister/api/document?url=" + url.QueryEscape(files.PathToFileURL(filename))
	waitDocument := func(present bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			response := testutil.ServeHTTP(t, handler, http.MethodGet, path, nil, headers)
			if present && response.Code == http.StatusOK && strings.Contains(response.Body.String(), "Saved watcher content") && strings.Contains(response.Body.String(), `"label":"web-watched"`) {
				return
			}
			if !present && response.Code == http.StatusNotFound {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("watcher state (present %t): %d %s", present, response.Code, response.Body)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitDocument(true)
	if err := os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	waitDocument(false)
	save(`[{"path":"indexer.directories","action":"set","value":[]}]`)
	loaded, _, err = config.LoadForInspection(mainPath, true)
	if err != nil || len(loaded.Indexer.Directories) != 0 || !loaded.Source("indexer.directories").WebSelected {
		t.Fatalf("clear not retained: %v", err)
	}
	save(`[{"path":"indexer.directories","action":"reset"}]`)
	loaded, _, err = config.LoadForInspection(mainPath, true)
	if err != nil || len(loaded.Indexer.Directories) != 0 || loaded.Source("indexer.directories").WebSelected {
		t.Fatalf("reset not retained: %v", err)
	}
	after, err := os.ReadFile(mainPath)
	if err != nil || string(after) != string(original) {
		t.Fatal("Indexer save rewrote main configuration")
	}
}

func TestSettingsSavePersistsAndResets(t *testing.T) {
	cfg, handler, write := newReloadTestServer(t, false)
	write("") // Leave ordinary App settings to the web editor.
	if err := os.WriteFile(cfg.FullPath("config.web.yml"), []byte("app:\n  public: false\nsemantic_search:\n  api_key: retained-private-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := reloadRequest(t, handler, "secret"); status != http.StatusOK {
		t.Fatalf("reload: %d", status)
	}
	mainPath := filepath.Join(cfg.App.Directory, "config.yml")
	mainBefore, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Origin": "hister://", "X-Access-Token": "secret"}
	login := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/token-login", strings.NewReader(`{"token":"secret"}`), headers)
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body)
	}
	cookie := responseSessionCookie(t, login)
	revision, _ := readConfigurationState(t, handler, headers)
	for _, edits := range []string{
		`[{"path":"app.title","action":"set","value":"Saved title"},{"path":"app.disable_previews","action":"set","value":true}]`,
		`[{"path":"app.title","action":"reset"}]`,
	} {
		body := fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, edits)
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/apply", strings.NewReader(body), headers)
		if response.Code != http.StatusOK {
			t.Fatalf("save: %d %s", response.Code, response.Body)
		}
		fresh, values := readConfigurationState(t, handler, headers)
		if fresh == revision || values["app.disable_previews"].Value != true {
			t.Fatalf("save did not publish a new configuration: %s %+v", fresh, values["app.disable_previews"])
		}
		// A client that lost the response must read state, not replay its old draft.
		replay := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/apply", strings.NewReader(body), headers)
		if replay.Code != http.StatusConflict {
			t.Fatalf("replayed save: %d %s", replay.Code, replay.Body)
		}
		loaded, _, err := config.LoadForInspection(mainPath, true)
		if err != nil || loaded.App.Title != values["app.title"].Value || !loaded.App.DisablePreviews || loaded.SemanticSearch.APIKey != "retained-private-key" || !loaded.App.Public || !loaded.Source("app.public").WebSelected {
			t.Fatalf("persisted configuration differs from running configuration: %v", err)
		}
		info, err := os.Stat(filepath.Join(cfg.App.Directory, "config.web.yml"))
		if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
			t.Fatalf("managed file must be private: %v", err)
		}
		session := testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/history", nil, map[string]string{"Cookie": cookie.String()})
		if session.Code != http.StatusOK {
			t.Fatalf("save invalidated the authenticated session: %d %s", session.Code, session.Body)
		}
		revision = fresh
	}
	_, values := readConfigurationState(t, handler, headers)
	if values["app.title"].Value != "Hister" || values["app.title"].Source.WebSelected {
		t.Fatalf("reset did not expose inherited title: %+v", values["app.title"])
	}
	mainAfter, err := os.ReadFile(mainPath)
	if err != nil || string(mainAfter) != string(mainBefore) {
		t.Fatal("save changed the main file")
	}
}

func TestSettingsSaveSkipsUnchangedSelections(t *testing.T) {
	for _, tc := range []struct {
		name, managed, edits string
	}{
		{"empty draft without file", "", `[]`},
		{"empty draft with file", "# retain this comment\napp:\n  title: Saved\n", `[]`},
		{"reset absent selection", "", `[{"path":"app.title","action":"reset"}]`},
		{"same string", "# retain shadowed selections and secrets too\napp:\n  title: Saved\n  public: false\nsemantic_search:\n  api_key: retained-secret\n", `[{"path":"app.title","action":"set","value":"Saved"}]`},
		{"same empty string", "app:\n  subtitle: ''\n", `[{"path":"app.subtitle","action":"set","value":""}]`},
		{"same boolean", "app:\n  disable_previews: false\n", `[{"path":"app.disable_previews","action":"set","value":false}]`},
		{"same integer", "app:\n  snippet_length: 123\n", `[{"path":"app.snippet_length","action":"set","value":123}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, handler, write := newReloadTestServer(t, false)
			write("")
			managed := cfg.FullPath("config.web.yml")
			var before os.FileInfo
			if tc.managed != "" {
				if err := os.WriteFile(managed, []byte(tc.managed), 0o600); err != nil {
					t.Fatal(err)
				}
				var err error
				before, err = os.Stat(managed)
				if err != nil {
					t.Fatal(err)
				}
			}
			if status := reloadRequest(t, handler, "secret"); status != http.StatusOK {
				t.Fatalf("reload: %d", status)
			}
			headers := map[string]string{"Origin": "hister://", "X-Access-Token": "secret"}
			revision, _ := readConfigurationState(t, handler, headers)
			server := httptest.NewServer(handler)
			defer server.Close()
			conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/hister/search", http.Header{"Origin": {cfg.Server.BaseURL}, "X-Access-Token": {"secret"}})
			if resp != nil {
				defer func() { _ = resp.Body.Close() }()
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, tc.edits)), headers)
			if response.Code != http.StatusOK {
				t.Fatalf("unchanged save: %d %s", response.Code, response.Body)
			}
			fresh, _ := readConfigurationState(t, handler, headers)
			if fresh != revision {
				t.Error("unchanged save replaced the active revision")
			}
			if before == nil {
				if _, err := os.Stat(managed); !os.IsNotExist(err) {
					t.Error("unchanged save created a managed file")
				}
			} else {
				after, err := os.Stat(managed)
				if err != nil || !os.SameFile(before, after) {
					t.Error("unchanged save replaced the managed file")
				}
				raw, err := os.ReadFile(managed)
				if err != nil || string(raw) != tc.managed {
					t.Error("unchanged save rewrote managed selections")
				}
			}
			if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			if err := conn.WriteJSON(map[string]string{"text": "retained connection"}); err != nil {
				t.Fatal(err)
			}
			var result json.RawMessage
			if err := conn.ReadJSON(&result); err != nil {
				t.Fatalf("unchanged save interrupted the search WebSocket: %v", err)
			}
		})
	}
}

func TestSettingsSavePersistsDefaultSelections(t *testing.T) {
	cfg, handler, write := newReloadTestServer(t, false)
	write("")
	if status := reloadRequest(t, handler, "secret"); status != http.StatusOK {
		t.Fatalf("reload: %d", status)
	}
	headers := map[string]string{"Origin": "hister://", "X-Access-Token": "secret"}
	revision, values := readConfigurationState(t, handler, headers)
	title := values["app.title"].Value
	for _, action := range []string{"set", "reset"} {
		edit := map[string]any{"path": "app.title", "action": action}
		if action == "set" {
			edit["value"] = title
		}
		raw, err := json.Marshal(map[string]any{"revision": revision, "edits": []any{edit}})
		if err != nil {
			t.Fatal(err)
		}
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/apply", strings.NewReader(string(raw)), headers)
		if response.Code != http.StatusOK {
			t.Fatalf("%s default selection: %d %s", action, response.Code, response.Body)
		}
		fresh, values := readConfigurationState(t, handler, headers)
		selected := action == "set"
		if fresh == revision || values["app.title"].Value != title || values["app.title"].Source.WebSelected != selected {
			t.Fatalf("%s did not change the source while preserving its value: %+v", action, values["app.title"])
		}
		loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
		if err != nil || loaded.Source("app.title").WebSelected != selected || loaded.App.Title != title {
			t.Fatalf("%s did not persist the default selection: %v", action, err)
		}
		revision = fresh
	}
}

func TestSettingsConcurrentSavesAndReloads(t *testing.T) {
	cfg, handler, _ := newReloadTestServer(t, false)
	headers := map[string]string{"Origin": "hister://", "X-Access-Token": "secret"}
	for round := range 4 {
		revision, _ := readConfigurationState(t, handler, headers)
		start := make(chan struct{})
		results := make(chan int, 3)
		for worker := range 3 {
			go func() {
				<-start
				path, body := "/hister/api/reload", ""
				if worker != 2 {
					path = "/hister/api/settings/apply"
					body = fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.subtitle","action":"set","value":"Round %d worker %d"}]}`, revision, round, worker)
				}
				response := testutil.ServeHTTP(t, handler, http.MethodPost, path, strings.NewReader(body), headers)
				if response.Code != http.StatusOK && response.Code != http.StatusConflict && response.Code != http.StatusServiceUnavailable {
					t.Errorf("concurrent operation: %d %s", response.Code, response.Body)
				}
				results <- response.Code
			}()
		}
		close(start)
		var succeeded int
		for range 3 {
			if <-results == http.StatusOK {
				succeeded++
			}
		}
		if succeeded == 0 {
			t.Fatal("no configuration operation completed")
		}
		_, values := readConfigurationState(t, handler, headers)
		persisted, _, err := config.LoadForInspection(cfg.Filename(), true)
		if err != nil || persisted.App.Subtitle != values["app.subtitle"].Value {
			t.Fatalf("concurrent operations split persisted and running state: %v", err)
		}
	}
}

func TestFullReloadRequiresAdministrator(t *testing.T) {
	for _, account := range []string{"empty", "ordinary", "administrator", "proxy"} {
		t.Run(account, func(t *testing.T) {
			cfg, handler, _ := newReloadTestServer(t, false)
			var admin *model.User
			if account == "ordinary" || account == "administrator" {
				var err error
				admin, err = model.CreateUser("owner", "password123", account == "administrator")
				if err != nil {
					t.Fatal(err)
				}
			}
			raw, err := os.ReadFile(cfg.Filename())
			if err != nil {
				t.Fatal(err)
			}
			if account == "proxy" {
				raw = []byte(strings.Replace(string(raw), "server:\n", "server:\n  proxy_auth_header: X-Remote-User\n", 1))
			} else {
				raw = []byte(strings.Replace(string(raw), "user_handling: false", "user_handling: true", 1))
			}
			if err := os.WriteFile(cfg.Filename(), raw, 0o600); err != nil {
				t.Fatal(err)
			}
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/reload", nil, map[string]string{"Origin": "hister://", "X-Access-Token": "secret"})
			if account != "administrator" {
				if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "administrator") {
					t.Fatalf("unusable administration accepted: %d %s", response.Code, response.Body)
				}
				return
			}
			if response.Code != http.StatusOK {
				t.Fatalf("valid administrator rejected: %d %s", response.Code, response.Body)
			}
			// Recheck availability even when user handling stays enabled.
			headers := map[string]string{"Origin": "hister://", "X-Access-Token": admin.Token}
			revision, _ := readConfigurationState(t, handler, headers)
			response = testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.subtitle","action":"set","value":"Still multiuser"}]}`, revision)), headers)
			if response.Code != http.StatusOK {
				t.Fatalf("ordinary multiuser save: %d %s", response.Code, response.Body)
			}
		})
	}
}

func TestRuntimeRequiresAdministratorBeforeServing(t *testing.T) {
	for _, account := range []string{"empty", "ordinary", "administrator", "database error"} {
		t.Run(account, func(t *testing.T) {
			cfg := testutil.Config(t)
			cfg.App.UserHandling = true
			cfg.Server.Address = "127.0.0.1:0"
			testutil.InitModelWithConfig(t, cfg)
			if account == "ordinary" || account == "administrator" {
				if _, err := model.CreateUser("owner", "password123", account == "administrator"); err != nil {
					t.Fatal(err)
				}
			}
			idx := newServerTestIndexer(t, cfg)
			if account == "database error" {
				db, err := model.DB.DB()
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := Listen(ctx, cfg, idx)
			if account == "administrator" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "administrator") {
				t.Fatalf("runtime accepted unavailable administration: %v", err)
			}
		})
	}
}

func TestRuntimeDiscardsAbandonedConfigurationStaging(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.Server.Address = "127.0.0.1:0"
	testutil.InitModelWithConfig(t, cfg)
	files := map[string]bool{
		".config.web-12345":          true,
		".config.web-recovery-12345": false,
		".config.web-notes":          false,
		"config.web.yml":             false,
	}
	for name := range files {
		if err := os.WriteFile(cfg.FullPath(name), []byte("app:\n  title: Interrupted\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Listen(ctx, cfg, newServerTestIndexer(t, cfg)); err != nil {
		t.Fatal(err)
	}
	for name, discarded := range files {
		_, err := os.Stat(cfg.FullPath(name))
		if os.IsNotExist(err) != discarded {
			t.Fatalf("staging cleanup for %s: %v", name, err)
		}
	}
}

func TestSettingsSaveRechecksActorAfterDraining(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	oldEndpoints := Endpoints
	Endpoints = append(append([]*Endpoint(nil), Endpoints...), &Endpoint{
		Path: "/api/test-demote", Method: GET,
		Handler: func(c *webContext) {
			close(entered)
			<-release
			if _, err := model.ToggleAdmin("saving-admin"); err != nil {
				t.Error(err)
			}
			serve200(c)
		},
	})
	t.Cleanup(func() { Endpoints = oldEndpoints })
	cfg, handler, _ := newReloadTestServer(t, true)
	actor, err := model.CreateUser("saving-admin", "password123", true)
	if err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Origin": "hister://", "X-Access-Token": actor.Token}
	revision, _ := readConfigurationState(t, handler, headers)
	held := make(chan struct{})
	go func() {
		testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/test-demote", nil, headers)
		close(held)
	}()
	<-entered
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.subtitle","action":"set","value":"Unauthorized"}]}`, revision)), headers)
	}()
	waitRuntimePause(t, handler)
	unblock()
	<-held
	response := <-result
	if response.Code != http.StatusForbidden {
		t.Fatalf("demoted actor applied settings: %d %s", response.Code, response.Body)
	}
	var failure struct{ Code string }
	if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil || failure.Code != "admin_required" {
		t.Fatalf("demoted actor failure code: %s (%v)", response.Body, err)
	}
	if _, err := os.Stat(filepath.Join(cfg.App.Directory, "config.web.yml")); !os.IsNotExist(err) {
		t.Fatal("denied save changed the managed file")
	}
}

func waitRuntimePause(t *testing.T, handler http.Handler) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if response := testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/health", nil, nil); response.Code == http.StatusServiceUnavailable {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("configuration apply did not pause requests")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSettingsSaveDrainingAndRecovery(t *testing.T) {
	for _, scenario := range []string{"lost response", "external edit", "commit failure", "recovery failure"} {
		t.Run(scenario, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			oldEndpoints := Endpoints
			Endpoints = append(append([]*Endpoint(nil), Endpoints...), &Endpoint{
				Path: "/api/test-held", Method: GET,
				Handler: func(c *webContext) {
					close(entered)
					<-release
					// Model a rule write finishing while the save drains requests.
					c.Config.Rules.Aliases["retained"] = "rule update"
					if err := c.Config.SaveRules(); err != nil {
						t.Error(err)
					}
					serve200(c)
				},
			})
			t.Cleanup(func() { Endpoints = oldEndpoints })
			cfg, handler, _ := newReloadTestServer(t, false)
			headers := map[string]string{"Origin": "hister://", "X-Access-Token": "secret"}
			revision, _ := readConfigurationState(t, handler, headers)
			held := make(chan struct{})
			go func() {
				testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/test-held", nil, headers)
				close(held)
			}()
			<-entered
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			request := httptest.NewRequest(http.MethodPost, "/hister/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.subtitle","action":"set","value":"Saved subtitle"}]}`, revision))).WithContext(ctx)
			for key, value := range headers {
				request.Header.Set(key, value)
			}
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { handler.ServeHTTP(response, request); close(done) }()
			waitRuntimePause(t, handler)
			managed := cfg.FullPath("config.web.yml")
			if _, err := os.Stat(managed); !os.IsNotExist(err) {
				t.Fatal("staging changed the confirmed configuration")
			}
			staging, err := filepath.Glob(cfg.FullPath(".config.web-*"))
			if err != nil || len(staging) != 1 {
				t.Fatalf("missing staged configuration: %v %v", staging, err)
			}
			info, err := os.Stat(staging[0])
			if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
				t.Fatal("staged secrets are not private")
			}
			for _, path := range []string{"/api/settings", "/api/reload", "/api/settings/apply"} {
				method := http.MethodGet
				if path != "/api/settings" {
					method = http.MethodPost
				}
				if reply := testutil.ServeHTTP(t, handler, method, "/hister"+path, nil, headers); reply.Code != http.StatusServiceUnavailable {
					t.Fatalf("request observed partial apply: %s %d", path, reply.Code)
				}
			}
			want, wantCode := http.StatusOK, ""
			switch scenario {
			case "lost response":
				cancel()
			case "external edit":
				if err := os.WriteFile(managed, []byte("app:\n  subtitle: External\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				want, wantCode = http.StatusConflict, "sources_changed"
			case "commit failure":
				// Simulate staging storage disappearing before atomic replacement.
				if err := os.Remove(staging[0]); err != nil {
					t.Fatal(err)
				}
				want, wantCode = http.StatusInternalServerError, "persistence_failed"
			case "recovery failure":
				// Both candidate activation and recovery must report unavailable
				// when the database path becomes unusable during draining.
				// Close the connection before moving its file on Windows too.
				db, err := model.DB.DB()
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				database := cfg.FullPath(cfg.Server.Database)
				if err := os.Rename(database, database+".saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(database, 0o700); err != nil {
					t.Fatal(err)
				}
				want, wantCode = http.StatusServiceUnavailable, "recovery_failed"
			}
			unblock()
			<-held
			<-done
			if response.Code != want {
				t.Fatalf("save: %d %s, want %d", response.Code, response.Body, want)
			}
			var result struct{ Code string }
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Code != wantCode {
				t.Errorf("save failure code: %s (%v), want %q", response.Body, err, wantCode)
			}
			if scenario == "recovery failure" {
				if !strings.Contains(response.Body.String(), "recovery failed") || !strings.Contains(response.Body.String(), "restart") {
					t.Fatal("response promised recovery that did not happen")
				}
				if reply := testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/health", nil, nil); reply.Code != http.StatusServiceUnavailable {
					t.Fatal("failed runtime still accepts requests")
				}
			} else {
				_, values := readConfigurationState(t, handler, headers)
				if (values["app.subtitle"].Value == "Saved subtitle") != (scenario == "lost response") {
					t.Fatalf("unexpected active subtitle: %+v", values["app.subtitle"])
				}
				rules := testutil.ServeHTTP(t, handler, http.MethodGet, "/hister/api/rules", nil, headers)
				if rules.Code != http.StatusOK || !strings.Contains(rules.Body.String(), "rule update") {
					t.Fatalf("lost drained rule write: %d %s", rules.Code, rules.Body)
				}
			}
			if scenario == "commit failure" || scenario == "recovery failure" {
				if _, err := os.Stat(managed); !os.IsNotExist(err) {
					t.Fatal("failed save replaced confirmed file")
				}
			}
			if staging, err := filepath.Glob(cfg.FullPath(".config.web-*")); err != nil || len(staging) != 0 {
				t.Fatalf("abandoned staging files: %v %v", staging, err)
			}
		})
	}
}

func TestSettingsSaveWithoutMainFile(t *testing.T) {
	for _, token := range []string{"", "secret"} {
		t.Run("token_"+token, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HISTER_DATA_DIR", dir)
			t.Setenv("XDG_CONFIG_HOME", dir)
			t.Setenv("LOCALAPPDATA", dir)
			t.Setenv("HISTER__SERVER__ADDRESS", "127.0.0.1:0")
			t.Setenv("HISTER__SERVER__BASE_URL", "http://127.0.0.1:4433/hister")
			t.Setenv("HISTER__APP__ACCESS_TOKEN", token)
			t.Setenv("HISTER__INDEXER__DETECT_LANGUAGES", "false")
			cfg, err := config.Load("")
			if err != nil || cfg.Filename() != "*Default Config*" {
				t.Fatalf("load without main file: %v", err)
			}
			testutil.InitModelWithConfig(t, cfg)
			handler := startReloadTestRuntime(t, cfg)
			headers := map[string]string{"Origin": "hister://", "X-Access-Token": token}
			revision, _ := readConfigurationState(t, handler, headers)
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.title","action":"set","value":"No main file"}]}`, revision)), headers)
			if response.Code != http.StatusOK {
				t.Fatalf("save: %d %s", response.Code, response.Body)
			}
			restarted, err := config.Load("")
			if err != nil || restarted.App.Title != "No main file" {
				t.Fatalf("restart did not retain settings: %v", err)
			}
			if status := reloadRequest(t, handler, token); status != http.StatusOK {
				t.Fatalf("reload without main file: %d", status)
			}
		})
	}
}

func TestSettingsSaveRejectsInvalidDraftsAndDiskFailures(t *testing.T) {
	cfg, handler, _ := newReloadTestServer(t, false)
	headers := map[string]string{"Origin": "hister://", "X-Access-Token": "secret"}
	revision, before := readConfigurationState(t, handler, headers)
	for _, edits := range []string{
		`[{"path":"app.title","action":"set","value":"Locked"}]`,
		`[{"path":"app.title","action":"set","value":"Original"}]`,
		`[{"path":"app.title","action":"reset"}]`,
		`[{"path":"app.subtitle","action":"set","value":7}]`,
		`[{"path":"app.access_token","action":"set","value":"sensitive"}]`,
		`[{"path":"app","action":"set","value":{"subtitle":"Bypass"}}]`,
		`[{"path":"app.subtitle","action":"reset"},{"path":"app.subtitle","action":"set","value":"Duplicate"}]`,
	} {
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, edits)), headers)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid save: %d %s", response.Code, response.Body)
		}
	}
	t.Run("staging permission failure", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Chmod does not enforce directory write permissions on Windows")
		}
		if err := os.Chmod(cfg.App.Directory, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(cfg.App.Directory, 0o700) })
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.subtitle","action":"set","value":"Cannot stage"}]}`, revision)), headers)
		if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "persistence_failed") {
			t.Fatalf("staging failure: %d %s", response.Code, response.Body)
		}
	})
	fresh, after := readConfigurationState(t, handler, headers)
	if fresh != revision || after["app.subtitle"].Value != before["app.subtitle"].Value {
		t.Fatal("rejected settings changed the runtime")
	}
	if _, err := os.Stat(cfg.FullPath("config.web.yml")); !os.IsNotExist(err) {
		t.Fatal("rejected settings changed the managed file")
	}
}

func TestSettingsSaveReportsOtherApplyFailures(t *testing.T) {
	cfg, handler, _ := newReloadTestServer(t, false)
	headers := map[string]string{"Origin": "hister://", "X-Access-Token": "secret"}
	revision, before := readConfigurationState(t, handler, headers)
	// Rules are read during activation, after the managed file has been staged.
	if err := os.Remove(cfg.RulesPath()); err != nil {
		t.Fatal(err)
	}
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/hister/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.subtitle","action":"set","value":"Candidate"}]}`, revision)), headers)
	var failure struct{ Code string }
	if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil || response.Code != http.StatusBadRequest || failure.Code != "apply_failed" {
		t.Fatalf("activation failure misclassified: %d %s (%v)", response.Code, response.Body, err)
	}
	fresh, after := readConfigurationState(t, handler, headers)
	if fresh != revision || after["app.subtitle"].Value != before["app.subtitle"].Value {
		t.Fatal("failed activation changed the runtime")
	}
	if _, err := os.Stat(cfg.FullPath("config.web.yml")); !os.IsNotExist(err) {
		t.Fatal("failed activation changed the managed file")
	}
}

func runtimeTestAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func startRuntimeTestServer(t *testing.T) (*config.Config, func(), func(string, string, string, string) (int, string)) {
	t.Helper()
	candidate := config.CreateDefaultConfig()
	candidate.App.Directory = t.TempDir()
	candidate.App.AccessToken = "secret"
	candidate.Indexer.DetectLanguages = false
	candidate.Server.Address = runtimeTestAddress(t)
	candidate.Server.BaseURL = "http://" + candidate.Server.Address
	filename := filepath.Join(candidate.App.Directory, "config.yml")
	write := func() {
		t.Helper()
		raw, err := yaml.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	active, err := config.Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.Init(active); err != nil {
		t.Fatal(err)
	}
	if err := extractor.Init(active.Extractors); err != nil {
		t.Fatal(err)
	}
	idx, err := indexer.New(active)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Listen(ctx, active, idx) }()
	hc := &http.Client{Timeout: 10 * time.Second}
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error("server failed: ", err)
			}
		case <-time.After(15 * time.Second):
			t.Error("server did not stop")
		}
		hc.CloseIdleConnections()
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := hc.Get(active.BaseURL("/health"))
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not start: ", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	request := func(baseURL, path, token, body string) (int, string) {
		t.Helper()
		method := http.MethodGet
		if body != "" || path == "/api/reload" {
			method = http.MethodPost
		}
		req, err := http.NewRequest(method, baseURL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "hister://")
		req.Header.Set("X-Access-Token", token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Error(err)
			}
		}()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(raw)
	}
	return candidate, write, request
}

func TestFullReloadReopensIndexerAndAuthentication(t *testing.T) {
	c, write, request := startRuntimeTestServer(t)
	base := c.Server.BaseURL
	if status, body := request(base, "/api/add", "secret", `{"url":"https://example.com/saved","title":"Saved","text":"Retained content"}`); status != http.StatusCreated {
		t.Fatalf("add: %d %s", status, body)
	}
	c.App.DisablePreviews = true
	c.App.AccessToken = "replacement"
	c.Server.Metrics = true
	write()
	if status, body := request(base, "/api/reload", "secret", ""); status != http.StatusOK {
		t.Fatalf("reload: %d %s", status, body)
	}
	if status, _ := request(base, "/api/stats", "secret", ""); status != http.StatusForbidden {
		t.Fatalf("old token still accepted: %d", status)
	}
	if status, body := request(base, "/metrics", "replacement", ""); status != http.StatusOK {
		t.Fatalf("new server metrics: %d %s", status, body)
	}
	status, body := request(base, "/search?format=json&q=Retained", "replacement", "")
	var result indexer.Results
	if err := json.Unmarshal([]byte(body), &result); status != http.StatusOK || err != nil || result.Total != 1 {
		t.Fatalf("saved index unavailable after reload: %d %s (%v)", status, body, err)
	}
	if status, body := request(base, "/api/add", "replacement", `{"url":"https://example.com/after","title":"After","text":"New content","html":"<p>New content</p>"}`); status != http.StatusCreated {
		t.Fatalf("add after reload: %d %s", status, body)
	}
	status, body = request(base, "/api/document?url=https://example.com/after", "replacement", "")
	var saved document.Document
	if err := json.Unmarshal([]byte(body), &saved); status != http.StatusOK || err != nil || saved.HTMLKey != "" {
		t.Fatalf("indexer did not apply disable_previews: %d %s (%v)", status, body, err)
	}
}

func TestReloadPDFParserProcess(t *testing.T) {
	marker := slices.Index(os.Args, "--pdf-reload-parser-test")
	if marker < 0 {
		return
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil || string(data) != "%PDF-1.4 reload fixture" || marker+1 >= len(os.Args) {
		os.Exit(2)
	}
	_, _ = io.WriteString(os.Stdout, os.Args[marker+1])
	os.Exit(0)
}

func TestFullReloadChangesPDFParser(t *testing.T) {
	c, write, request := startRuntimeTestServer(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"document": document.Document{URL: "https://example.com/reload.pdf"},
		"pdf":      base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 reload fixture")),
	})
	if err != nil {
		t.Fatal(err)
	}
	upload := func(want int) {
		t.Helper()
		if status, body := request(c.Server.BaseURL, "/api/add_pdf", "secret", string(payload)); status != want {
			t.Fatalf("PDF upload: %d %s, want status %d", status, body, want)
		}
	}
	upload(http.StatusInternalServerError) // The built in parser rejects the fake PDF.
	for _, text := range []string{"First parser text", "Replacement parser text"} {
		c.Indexer.PDFParser = config.PDFParser{
			Command: executable,
			Args:    []string{"-test.run=^TestReloadPDFParserProcess$", "--", "--pdf-reload-parser-test", text},
			Timeout: 5,
		}
		write()
		if status, body := request(c.Server.BaseURL, "/api/reload", "secret", ""); status != http.StatusOK {
			t.Fatalf("reload PDF parser: %d %s", status, body)
		}
		upload(http.StatusCreated)
		status, body := request(c.Server.BaseURL, "/api/document?url=https://example.com/reload.pdf", "secret", "")
		var saved document.Document
		if err := json.Unmarshal([]byte(body), &saved); status != http.StatusOK || err != nil || saved.Text != text {
			t.Fatalf("reloaded parser output: %d %s (%v), want text %q", status, body, err, text)
		}
	}
	c.Indexer.PDFParser = config.PDFParser{Timeout: 30}
	write()
	if status, body := request(c.Server.BaseURL, "/api/reload", "secret", ""); status != http.StatusOK {
		t.Fatalf("restore built in PDF parser: %d %s", status, body)
	}
	upload(http.StatusInternalServerError)
}

func TestFullReloadChangesListenerAndDatabase(t *testing.T) {
	c, write, request := startRuntimeTestServer(t)
	oldBase := c.Server.BaseURL
	c.Server.Address = runtimeTestAddress(t)
	c.Server.BaseURL = "http://" + c.Server.Address + "/new"
	c.Server.Database = "replacement.sqlite3"
	c.App.Title = "Replacement"
	write()
	if status, body := request(oldBase, "/api/reload", "secret", ""); status != http.StatusOK {
		t.Fatalf("reload: %d %s", status, body)
	}
	if status, body := request(c.Server.BaseURL, "/api/config", "secret", ""); status != http.StatusOK || !strings.Contains(body, `"title":"Replacement"`) {
		t.Fatalf("new listener unavailable: %d %s", status, body)
	}
	if _, err := os.Stat(filepath.Join(c.App.Directory, c.Server.Database)); err != nil {
		t.Fatal("new database was not opened: ", err)
	}
}

func TestFullReloadRestoresPreviousRuntimeOnOpenFailure(t *testing.T) {
	c, write, request := startRuntimeTestServer(t)
	base := c.Server.BaseURL
	c.Server.Database = filepath.Join("missing-parent", "db.sqlite3")
	c.App.Title = "Must not apply"
	write()
	if status, body := request(base, "/api/reload", "secret", ""); status != http.StatusInternalServerError || !strings.Contains(body, "restored") {
		t.Fatalf("failed reload: %d %s", status, body)
	}
	if status, body := request(base, "/api/config", "secret", ""); status != http.StatusOK || strings.Contains(body, "Must not apply") {
		t.Fatalf("previous runtime not restored: %d %s", status, body)
	}
	c.Server.Database = "db.sqlite3"
	c.App.Title = "Recovered"
	write()
	if status, body := request(base, "/api/reload", "secret", ""); status != http.StatusOK {
		t.Fatalf("retry failed: %d %s", status, body)
	}
}

func TestFullReloadConcurrentRequests(t *testing.T) {
	c, _, request := startRuntimeTestServer(t)
	var workers sync.WaitGroup
	for worker := range 4 {
		workers.Go(func() {
			for range 8 {
				path := "/api/stats"
				if worker < 2 {
					path = "/api/reload"
				}
				status, body := request(c.Server.BaseURL, path, "secret", "")
				if status != http.StatusOK && status != http.StatusConflict && status != http.StatusServiceUnavailable {
					t.Errorf("%s: %d %s", path, status, body)
				}
			}
		})
	}
	workers.Wait()
	if status, body := request(c.Server.BaseURL, "/api/stats", "secret", ""); status != http.StatusOK {
		t.Fatalf("server unavailable after concurrent reloads: %d %s", status, body)
	}
}

func TestFullReloadClosesWebSockets(t *testing.T) {
	c, _, request := startRuntimeTestServer(t)
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(c.Server.BaseURL, "http")+"/search", http.Header{"Origin": {c.Server.BaseURL}, "X-Access-Token": {"secret"}})
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
	if status, body := request(c.Server.BaseURL, "/api/reload", "secret", ""); status != http.StatusOK {
		t.Fatalf("reload with open WebSocket: %d %s", status, body)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("old WebSocket survived reload")
	} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		t.Fatal("WebSocket was not closed")
	}
}

func TestFullReloadRetainsRuntimeWhenListenerUnavailable(t *testing.T) {
	c, write, request := startRuntimeTestServer(t)
	base := c.Server.BaseURL
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := occupied.Close(); err != nil {
			t.Error(err)
		}
	}()
	c.Server.Address = occupied.Addr().String()
	c.App.AccessToken = "replacement"
	write()
	if status, body := request(base, "/api/reload", "secret", ""); status != http.StatusBadRequest || !strings.Contains(body, "server.address") {
		t.Fatalf("occupied listener: %d %s", status, body)
	}
	if status, body := request(base, "/api/stats", "secret", ""); status != http.StatusOK {
		t.Fatalf("old runtime changed: %d %s", status, body)
	}
}

func TestFullReloadSwitchesDataDirectories(t *testing.T) {
	c, write, request := startRuntimeTestServer(t)
	base, original := c.Server.BaseURL, c.App.Directory
	if status, body := request(base, "/api/add", "secret", `{"url":"https://example.com/original","text":"Original content"}`); status != http.StatusCreated {
		t.Fatalf("seed index: %d %s", status, body)
	}
	c.App.Directory = filepath.Join(t.TempDir(), "new-data")
	write()
	if status, body := request(base, "/api/reload", "secret", ""); status != http.StatusOK {
		t.Fatalf("new directory: %d %s", status, body)
	}
	if status, body := request(base, "/api/document?url=https://example.com/original", "secret", ""); status != http.StatusNotFound {
		t.Fatalf("new directory unexpectedly contains old document: %d %s", status, body)
	}
	status, body := request(base, "/api/diagnostics", "secret", "")
	var checks []types.DiagnosticCheck
	if err := json.Unmarshal([]byte(body), &checks); status != http.StatusOK || err != nil {
		t.Fatalf("diagnostics: %d %s", status, body)
	}
	for _, check := range checks {
		if strings.HasPrefix(check.Name, "index.") && check.Status != "ok" {
			t.Fatalf("new index metadata: %+v", check)
		}
	}
	c.App.Directory = original
	write()
	if status, body := request(base, "/api/reload", "secret", ""); status != http.StatusOK {
		t.Fatalf("restore directory: %d %s", status, body)
	}
	if status, body := request(base, "/api/document?url=https://example.com/original", "secret", ""); status != http.StatusOK {
		t.Fatalf("original data lost: %d %s", status, body)
	}
}

func TestFullReloadDrainsInFlightRequests(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	oldEndpoints := Endpoints
	Endpoints = append(append([]*Endpoint(nil), Endpoints...), &Endpoint{
		Path: "/api/test-drain", Method: GET,
		Handler: func(c *webContext) {
			close(entered)
			<-release
			if _, _, err := c.Indexer.GetMetadata(); err != nil {
				http.Error(c.Response, "index closed before request finished", 500)
				return
			}
			serve200(c)
		},
	})
	t.Cleanup(func() { Endpoints = oldEndpoints })
	c, _, request := startRuntimeTestServer(t)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	slowDone := make(chan int, 1)
	go func() { status, _ := request(c.Server.BaseURL, "/api/test-drain", "secret", ""); slowDone <- status }()
	<-entered
	reloadDone := make(chan int, 1)
	go func() { status, _ := request(c.Server.BaseURL, "/api/reload", "secret", ""); reloadDone <- status }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, _ := request(c.Server.BaseURL, "/health", "", "")
		if status == http.StatusServiceUnavailable {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reload never paused incoming requests")
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case status := <-reloadDone:
		t.Fatalf("reload returned before draining: %d", status)
	default:
	}
	close(release)
	if status := <-slowDone; status != http.StatusOK {
		t.Fatalf("in flight request failed: %d", status)
	}
	if status := <-reloadDone; status != http.StatusOK {
		t.Fatalf("reload failed: %d", status)
	}
}

func TestFullReloadRestartsDirectoryWatchers(t *testing.T) {
	c, write, request := startRuntimeTestServer(t)
	dir := t.TempDir()
	filename := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(filename, []byte("Initial watched content"), 0o600); err != nil {
		t.Fatal(err)
	}
	// File indexing compares modification times at second precision.
	initialTime := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(filename, initialTime, initialTime); err != nil {
		t.Fatal(err)
	}
	c.Indexer.Directories = []*config.Directory{{Path: dir, Filetypes: []string{"txt"}, Label: "watched"}}
	write()
	if status, body := request(c.Server.BaseURL, "/api/reload", "secret", ""); status != http.StatusOK {
		t.Fatalf("reload: %d %s", status, body)
	}
	path := "/api/document?url=" + url.QueryEscape(files.PathToFileURL(filename))
	waitText := func(text string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			status, body := request(c.Server.BaseURL, path, "secret", "")
			if status == http.StatusOK && strings.Contains(body, text) && strings.Contains(body, `"label":"watched"`) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("watcher did not index %q: %d %s", text, status, body)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitText("Initial watched content")
	if err := os.WriteFile(filename, []byte("Updated watched content"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitText("Updated watched content")
	c.Indexer.Directories = nil
	write()
	if status, body := request(c.Server.BaseURL, "/api/reload", "secret", ""); status != http.StatusOK {
		t.Fatalf("remove watcher: %d %s", status, body)
	}
	if err := os.WriteFile(filename, []byte("Must not index"), 0o600); err != nil {
		t.Fatal(err)
	}
	laterTime := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filename, laterTime, laterTime); err != nil {
		t.Fatal(err)
	}
	// Allow the former watcher's debounce interval to elapse.
	time.Sleep(350 * time.Millisecond)
	if status, body := request(c.Server.BaseURL, path, "secret", ""); status != http.StatusOK || !strings.Contains(body, "Updated watched content") {
		t.Fatalf("removed watcher still active: %d %s", status, body)
	}
}

func TestFullReloadRestartsSemanticIndexing(t *testing.T) {
	embeddings := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"embedding":[1,0,0],"index":0}]}`)
	}))
	defer embeddings.Close()
	c, write, request := startRuntimeTestServer(t)
	c.SemanticSearch.Enable = true
	c.SemanticSearch.Dimensions = 3
	c.SemanticSearch.EmbeddingEndpoint = embeddings.URL
	write()
	if status, body := request(c.Server.BaseURL, "/api/reload", "secret", ""); status != http.StatusOK {
		t.Fatalf("enable semantic search: %d %s", status, body)
	}
	if status, body := request(c.Server.BaseURL, "/api/config", "secret", ""); status != http.StatusOK || !strings.Contains(body, `"semanticEnabled":true`) {
		t.Fatalf("semantic search unavailable: %d %s", status, body)
	}
	if status, body := request(c.Server.BaseURL, "/api/add", "secret", `{"url":"https://example.com/semantic","text":"Semantic queue work"}`); status != http.StatusCreated {
		t.Fatalf("add: %d %s", status, body)
	}
	c.SemanticSearch.Enable = false
	write()
	if status, body := request(c.Server.BaseURL, "/api/reload", "secret", ""); status != http.StatusOK {
		t.Fatalf("stop semantic search: %d %s", status, body)
	}
	if status, body := request(c.Server.BaseURL, "/api/config", "secret", ""); status != http.StatusOK || !strings.Contains(body, `"semanticEnabled":false`) {
		t.Fatalf("semantic search still enabled: %d %s", status, body)
	}
}
