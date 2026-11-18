package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/files"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/extractor"
	"github.com/asciimoo/nogon/server/indexer"
	"github.com/asciimoo/nogon/server/model"
	"github.com/asciimoo/nogon/server/testutil"
	"github.com/asciimoo/nogon/server/types"
)

func TestSettingsPendingActivationKeepsConfirmedConfiguration(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	r := handler.(*serverRuntime)
	before, err := os.ReadFile(cfg.FullPath("config.web.yml"))
	if err != nil {
		t.Fatal(err)
	}
	revision, _ := readConfigurationState(t, handler, headers)
	next, fields := prepareConfigurationDraft(cfg, []config.SettingEdit{
		{Path: "app.subtitle", Action: "set", Value: json.RawMessage(`"Temporary settings"`)},
	}, revision)
	if len(fields) != 0 {
		t.Fatal(fields)
	}
	lease := &runtimeRequest{runtime: r, generation: r.current, finish: func() {}, actor: configurationTokenActor(nil, "secret")}
	started := time.Now()
	status, pending, err := r.activatePending(lease, next)
	if err != nil || status != http.StatusAccepted {
		t.Fatalf("temporary activation: %d %v", status, err)
	}
	if pending.Revision == "" || pending.Deadline.Before(started.Add(2*time.Minute)) || pending.DestinationURL != cfg.BaseURL("/settings") {
		t.Fatalf("missing confirmation information: %+v", pending)
	}
	_, values := readConfigurationState(t, handler, headers)
	if values["app.subtitle"].Value != "Temporary settings" {
		t.Fatal("candidate was not activated")
	}
	after, err := os.ReadFile(cfg.FullPath("config.web.yml"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("temporary activation replaced the confirmed file")
	}
	loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
	if err != nil || loaded.App.Subtitle == "Temporary settings" {
		t.Fatal("an unconfirmed configuration would survive restart")
	}
}

func TestSettingsPendingConfirmation(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	r := handler.(*serverRuntime)
	pending := activateTestPendingSettings(t, r, headers)
	lease := pendingTestLease(r, headers)
	if status, _ := r.confirmPending(lease, "another-operation"); status != http.StatusConflict {
		t.Fatalf("accepted unrelated confirmation: %d", status)
	}
	if status, err := r.confirmPending(lease, pending.Revision); status != http.StatusOK || err != nil {
		t.Fatalf("confirmation failed: %d %v", status, err)
	}
	loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
	if err != nil || loaded.App.Subtitle != "Temporary settings" {
		t.Fatalf("confirmed configuration did not persist: %v", err)
	}
	if status, _ := r.confirmPending(lease, pending.Revision); status != http.StatusConflict {
		t.Fatalf("replayed confirmation was accepted: %d", status)
	}
	if staged, err := filepath.Glob(cfg.FullPath(".config.web-*")); err != nil || len(staged) != 0 {
		t.Fatalf("confirmation left staging files: %v %v", staged, err)
	}
}

func pendingTestLease(r *serverRuntime, headers map[string]string) *runtimeRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return &runtimeRequest{runtime: r, generation: r.current, finish: func() {}, actor: configurationTokenActor(nil, headers["X-Access-Token"]), request: httptest.NewRequest(http.MethodPost, r.current.cfg.BaseURL("/api/settings/confirm"), nil)}
}

func TestSettingsPendingCancellationPreservesRules(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	r := handler.(*serverRuntime)
	before, err := os.ReadFile(cfg.FullPath("config.web.yml"))
	if err != nil {
		t.Fatal(err)
	}
	pending := activateTestPendingSettings(t, r, headers)
	ruleHeaders := maps.Clone(headers)
	ruleHeaders["Content-Type"] = "application/x-www-form-urlencoded"
	response := testutil.ServeHTTP(t, r, http.MethodPost, "/nogon/api/rules", strings.NewReader(url.Values{"skip": {"pending-rule"}}.Encode()), ruleHeaders)
	if response.Code != http.StatusOK {
		t.Fatalf("rule write during temporary activation: %d %s", response.Code, response.Body)
	}
	if status, err := r.cancelPending(pendingTestLease(r, headers), pending.Revision); err != nil || status != http.StatusOK {
		t.Fatalf("cancel: %d %v", status, err)
	}
	_, values := readConfigurationState(t, r, headers)
	if values["app.subtitle"].Value != cfg.App.Subtitle {
		t.Fatal("cancellation did not restore confirmed settings")
	}
	rules := testutil.ServeHTTP(t, r, http.MethodGet, "/nogon/api/rules", nil, headers)
	if rules.Code != http.StatusOK || !strings.Contains(rules.Body.String(), "pending-rule") {
		t.Fatalf("rollback lost a rule write: %d %s", rules.Code, rules.Body)
	}
	after, err := os.ReadFile(cfg.FullPath("config.web.yml"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("cancellation changed confirmed selections")
	}
	if status, _ := r.confirmPending(pendingTestLease(r, headers), pending.Revision); status != http.StatusConflict {
		t.Fatalf("accepted confirmation after cancellation: %d", status)
	}
}

func TestSettingsPendingTimeoutRestoresConfiguration(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	r := handler.(*serverRuntime)
	r.confirmationTimeout = 50 * time.Millisecond
	pending := activateTestPendingSettings(t, r, headers)
	deadline := time.Now().Add(5 * time.Second)
	for {
		response := testutil.ServeHTTP(t, r, http.MethodGet, "/nogon/api/config", nil, nil)
		var active struct{ Subtitle string }
		if response.Code == http.StatusOK && json.Unmarshal(response.Body.Bytes(), &active) == nil && active.Subtitle == cfg.App.Subtitle {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout did not independently restore confirmed settings")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status, _ := r.confirmPending(pendingTestLease(r, headers), pending.Revision); status != http.StatusConflict {
		t.Fatalf("expired confirmation was accepted: %d", status)
	}
	loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
	if err != nil || loaded.App.Subtitle != cfg.App.Subtitle {
		t.Fatal("timeout persisted unconfirmed settings")
	}
}

func TestSettingsPendingRejectsCompetingWrites(t *testing.T) {
	for _, operation := range []string{"save", "reload"} {
		t.Run(operation, func(t *testing.T) {
			_, handler, headers := newAccessSettingsTestServer(t, "token")
			r := handler.(*serverRuntime)
			pending := activateTestPendingSettings(t, r, headers)
			revision, _ := readConfigurationState(t, r, headers)
			path, body := "/nogon/api/reload", ""
			if operation == "save" {
				path = "/nogon/api/settings/apply"
				body = fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.subtitle","action":"set","value":"Competing settings"}]}`, revision)
			}
			response := testutil.ServeHTTP(t, r, http.MethodPost, path, strings.NewReader(body), headers)
			if response.Code != http.StatusConflict {
				t.Fatalf("competing %s was accepted: %d %s", operation, response.Code, response.Body)
			}
			if status, err := r.confirmPending(pendingTestLease(r, headers), pending.Revision); status != http.StatusOK || err != nil {
				t.Fatalf("competing write invalidated confirmation: %d %v", status, err)
			}
			_, values := readConfigurationState(t, r, headers)
			if values["app.subtitle"].Value != "Temporary settings" {
				t.Fatal("competing write changed pending configuration")
			}
		})
	}
}

func TestSettingsPendingConfirmationFailureRestoresConfiguration(t *testing.T) {
	for _, scenario := range []string{"staging disappeared", "main file changed", "managed file changed"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, handler, headers := newAccessSettingsTestServer(t, "token")
			r := handler.(*serverRuntime)
			pending := activateTestPendingSettings(t, r, headers)
			managed := cfg.FullPath("config.web.yml")
			before, err := os.ReadFile(managed)
			if err != nil {
				t.Fatal(err)
			}
			want := http.StatusConflict
			if scenario == "staging disappeared" {
				files, err := filepath.Glob(cfg.FullPath(".config.web-*"))
				if err != nil || len(files) == 0 {
					t.Fatal("no staged write")
				}
				for _, name := range files {
					if !strings.Contains(filepath.Base(name), "recovery") {
						if err := os.Remove(name); err != nil {
							t.Fatal(err)
						}
					}
				}
				want = http.StatusInternalServerError
			} else {
				name := cfg.Filename()
				if scenario == "managed file changed" {
					name = managed
					before = append(before, []byte("# external edit\n")...)
				}
				file, err := os.OpenFile(name, os.O_APPEND|os.O_WRONLY, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = file.WriteString("# external edit\n")
				if closeErr := file.Close(); err != nil || closeErr != nil {
					t.Fatalf("external edit: %v %v", err, closeErr)
				}
				state := testutil.ServeHTTP(t, r, http.MethodGet, cfg.BaseURL("/api/settings"), nil, headers)
				var pendingState struct {
					Pending *pendingConfiguration `json:"pending"`
					Access  *pendingAccessStatus  `json:"pending_access"`
				}
				if json.Unmarshal(state.Body.Bytes(), &pendingState) != nil || pendingState.Pending == nil || pendingState.Access == nil {
					t.Fatalf("pending access hid an external edit: %s", state.Body)
				}
			}
			status, err := r.confirmPending(pendingTestLease(r, headers), pending.Revision)
			if status != want || err == nil {
				t.Fatalf("confirmation failure: %d %v, want %d", status, err, want)
			}
			_, values := readConfigurationState(t, r, headers)
			if values["app.subtitle"].Value != cfg.App.Subtitle {
				t.Fatal("failed confirmation left unconfirmed settings active")
			}
			after, err := os.ReadFile(managed)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed confirmation overwrote confirmed or external settings")
			}
			if staged, err := filepath.Glob(cfg.FullPath(".config.web-*")); err != nil || len(staged) != 0 {
				t.Fatalf("failed confirmation left staging files: %v %v", staged, err)
			}
		})
	}
}

func TestSettingsPendingRestartUsesConfirmedConfiguration(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	r := handler.(*serverRuntime)
	pending := activateTestPendingSettings(t, r, headers)
	r.close()
	loaded, err := config.Load(cfg.Filename())
	if err != nil {
		t.Fatal(err)
	}
	if err := model.Init(loaded); err != nil {
		t.Fatal(err)
	}
	restarted := startReloadTestRuntime(t, loaded).(*serverRuntime)
	_, values := readConfigurationState(t, restarted, headers)
	if values["app.subtitle"].Value != cfg.App.Subtitle {
		t.Fatal("restart activated an unconfirmed configuration")
	}
	if status, _ := restarted.confirmPending(pendingTestLease(restarted, headers), pending.Revision); status != http.StatusConflict {
		t.Fatalf("restart accepted an abandoned confirmation: %d", status)
	}
	if staged, err := filepath.Glob(cfg.FullPath(".config.web-*")); err != nil || len(staged) != 0 {
		t.Fatalf("shutdown left unconfirmed staging files: %v %v", staged, err)
	}
}

func TestSettingsPendingConcurrentConfirmationAndCancellation(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	r := handler.(*serverRuntime)
	pending := activateTestPendingSettings(t, r, headers)
	lease := pendingTestLease(r, headers)
	confirmed, cancelled := make(chan int, 1), make(chan int, 1)
	go func() { status, _ := r.confirmPending(lease, pending.Revision); confirmed <- status }()
	go func() { status, _ := r.cancelPending(lease, pending.Revision); cancelled <- status }()
	confirmation, cancellation := <-confirmed, <-cancelled
	if (confirmation != http.StatusOK || cancellation != http.StatusConflict) && (cancellation != http.StatusOK || confirmation != http.StatusConflict) {
		t.Fatalf("operations did not serialize: confirmation %d, cancellation %d", confirmation, cancellation)
	}
	loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
	if err != nil {
		t.Fatal(err)
	}
	_, values := readConfigurationState(t, r, headers)
	if loaded.App.Subtitle != values["app.subtitle"].Value || (loaded.App.Subtitle == "Temporary settings") != (confirmation == http.StatusOK) {
		t.Fatal("concurrent completion left runtime and persistence inconsistent")
	}
}

func TestSettingsPendingListenerRestoration(t *testing.T) {
	for _, blockRecovery := range []bool{false, true} {
		t.Run(fmt.Sprintf("blocked_%t", blockRecovery), func(t *testing.T) {
			cfg, handler, headers := newAccessSettingsTestServer(t, "token")
			r := handler.(*serverRuntime)
			previousAddress := runtimeTestAddress(t)
			web := fmt.Sprintf("app:\n  access_token: secret\n  public: true\nserver:\n  address: %s\n", previousAddress)
			if err := os.WriteFile(cfg.FullPath("config.web.yml"), []byte(web), 0o600); err != nil {
				t.Fatal(err)
			}
			if status := reloadRequest(t, r, "secret"); status != http.StatusOK {
				t.Fatalf("prepare listener: %d", status)
			}
			revision, _ := readConfigurationState(t, r, headers)
			address := runtimeTestAddress(t)
			lease := pendingTestLease(r, headers)
			next, fields := prepareConfigurationDraft(lease.generation.cfg, []config.SettingEdit{
				{Path: "server.address", Action: "set", Value: json.RawMessage(fmt.Sprintf("%q", address))},
			}, revision)
			if len(fields) != 0 {
				t.Fatal(fields)
			}
			status, pending, err := r.activatePending(lease, next)
			if status != http.StatusAccepted || err != nil {
				t.Fatalf("temporary listener: %d %v", status, err)
			}
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			check := func(address string) {
				t.Helper()
				response, err := client.Get("http://" + address + "/nogon/api/config")
				if err != nil {
					t.Fatal(err)
				}
				_ = response.Body.Close()
				if response.StatusCode != http.StatusOK {
					t.Fatalf("listener unavailable: %d", response.StatusCode)
				}
			}
			check(address)
			if blockRecovery {
				listener, err := net.Listen("tcp", previousAddress)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = listener.Close() }()
			}
			status, err = r.cancelPending(pendingTestLease(r, headers), pending.Revision)
			if blockRecovery {
				if status != http.StatusServiceUnavailable || err == nil || !strings.Contains(err.Error(), "restart") {
					t.Fatalf("failed recovery was not reported: %d %v", status, err)
				}
				response := testutil.ServeHTTP(t, r, http.MethodGet, "/nogon/api/config", nil, nil)
				if response.Code != http.StatusServiceUnavailable {
					t.Fatal("failed recovery continued serving the unconfirmed configuration")
				}
			} else {
				if status != http.StatusOK || err != nil {
					t.Fatalf("listener rollback: %d %v", status, err)
				}
				check(previousAddress)
			}
			loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
			if err != nil || loaded.Server.Address != previousAddress {
				t.Fatal("temporary listener replaced confirmed settings")
			}
		})
	}
}

func TestSettingsPendingConfirmationExpiresWhileDraining(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	previousEndpoints := Endpoints
	Endpoints = append(append([]*Endpoint(nil), Endpoints...), &Endpoint{
		Path: "/api/test-held", Method: GET,
		Handler: func(c *webContext) { close(entered); <-release; serve200(c) },
	})
	t.Cleanup(func() { Endpoints = previousEndpoints })
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	r := handler.(*serverRuntime)
	r.confirmationTimeout = 250 * time.Millisecond
	pending := activateTestPendingSettings(t, r, headers)
	held := make(chan struct{})
	go func() { testutil.ServeHTTP(t, r, http.MethodGet, "/nogon/api/test-held", nil, headers); close(held) }()
	<-entered
	lease := pendingTestLease(r, headers)
	done := make(chan int, 1)
	go func() { status, _ := r.confirmPending(lease, pending.Revision); done <- status }()
	waitRuntimePause(t, r)
	time.Sleep(time.Until(pending.Deadline) + 10*time.Millisecond)
	unblock()
	<-held
	if status := <-done; status != http.StatusConflict {
		t.Fatalf("confirmation crossed the deadline: %d", status)
	}
	_, values := readConfigurationState(t, r, headers)
	if values["app.subtitle"].Value != cfg.App.Subtitle {
		t.Fatal("late confirmation did not restore the previous configuration")
	}
}

func TestSettingsPendingActivationFailureKeepsRuntime(t *testing.T) {
	cfg, handler, headers := newAccessSettingsTestServer(t, "token")
	r := handler.(*serverRuntime)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	revision, _ := readConfigurationState(t, r, headers)
	lease := pendingTestLease(r, headers)
	next, fields := prepareConfigurationDraft(lease.generation.cfg, []config.SettingEdit{
		{Path: "server.address", Action: "set", Value: json.RawMessage(fmt.Sprintf("%q", listener.Addr().String()))},
	}, revision)
	if len(fields) != 0 {
		t.Fatal(fields)
	}
	status, _, err := r.activatePending(lease, next)
	if status != http.StatusBadRequest || err == nil {
		t.Fatalf("unavailable listener activation: %d %v", status, err)
	}
	if current, _ := readConfigurationState(t, r, headers); current != revision {
		t.Fatal("failed activation changed the runtime")
	}
	if staged, err := filepath.Glob(cfg.FullPath(".config.web-*")); err != nil || len(staged) != 0 {
		t.Fatalf("failed activation left staging files: %v %v", staged, err)
	}
	if status := reloadRequest(t, r, "secret"); status != http.StatusOK {
		t.Fatalf("failed activation blocked later operations: %d", status)
	}
}

func TestSettingsPendingActivationRechecksAdministrator(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	previousEndpoints := Endpoints
	Endpoints = append(append([]*Endpoint(nil), Endpoints...), &Endpoint{
		Path: "/api/test-remove-administrator", Method: GET,
		Handler: func(c *webContext) {
			close(entered)
			<-release
			if _, err := model.SyncOAuthAdmin("oidc-future-owner", false); err != nil {
				t.Error(err)
			}
			serve200(c)
		},
	})
	t.Cleanup(func() { Endpoints = previousEndpoints })
	_, handler, headers := newAccessSettingsTestServer(t, "token")
	r := handler.(*serverRuntime)
	if _, err := model.CreateOAuthUser("future-owner", "oidc-future-owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := model.SyncOAuthAdmin("oidc-future-owner", true); err != nil {
		t.Fatal(err)
	}
	revision, _ := readConfigurationState(t, r, headers)
	lease := pendingTestLease(r, headers)
	next, fields := prepareConfigurationDraft(lease.generation.cfg, []config.SettingEdit{
		{Path: "app.user_handling", Action: "set", Value: json.RawMessage(`true`)},
	}, revision)
	if len(fields) != 0 {
		t.Fatal(fields)
	}
	held := make(chan struct{})
	go func() {
		testutil.ServeHTTP(t, r, http.MethodGet, "/nogon/api/test-remove-administrator", nil, headers)
		close(held)
	}()
	<-entered
	done := make(chan int, 1)
	go func() { status, _, _ := r.activatePending(lease, next); done <- status }()
	waitRuntimePause(t, r)
	unblock()
	<-held
	if status := <-done; status != http.StatusUnprocessableEntity {
		t.Fatalf("activated user handling after the administrator role was revoked: %d", status)
	}
	_, values := readConfigurationState(t, r, headers)
	if values["app.user_handling"].Value != false {
		t.Fatal("rejected activation changed authentication")
	}
}

func activateTestPendingSettings(t *testing.T, r *serverRuntime, headers map[string]string) pendingAccessStatus {
	t.Helper()
	revision, _ := readConfigurationState(t, r, headers)
	lease := pendingTestLease(r, headers)
	next, fields := prepareConfigurationDraft(lease.generation.cfg, []config.SettingEdit{
		{Path: "app.subtitle", Action: "set", Value: json.RawMessage(`"Temporary settings"`)},
	}, revision)
	if len(fields) != 0 {
		t.Fatal(fields)
	}
	status, pending, err := r.activatePending(lease, next)
	if err != nil || status != http.StatusAccepted {
		t.Fatalf("temporary activation: %d %v", status, err)
	}
	return pending
}

func TestSettingsOrdinaryAppAndServerApply(t *testing.T) {
	for _, multiUser := range []bool{false, true} {
		t.Run(fmt.Sprintf("multi_user_%t", multiUser), func(t *testing.T) {
			cfg, handler, _ := newReloadTestServer(t, multiUser)
			token := "secret"
			var ordinaryToken string
			if multiUser {
				admin, err := model.GetUser("runtime-owner")
				if err != nil {
					t.Fatal(err)
				}
				token = admin.Token
				ordinary, err := model.CreateUser("reader", "password123", false)
				if err != nil {
					t.Fatal(err)
				}
				ordinaryToken = ordinary.Token
			}
			headers := map[string]string{"Origin": "nogon://", "X-Access-Token": token, "Content-Type": "application/json"}
			mainBefore, err := os.ReadFile(cfg.Filename())
			if err != nil {
				t.Fatal(err)
			}
			paths := []string{"app.display_extractor_config", "app.debug_sql", "app.profiler", "server.metrics", "server.expose_version"}
			for _, action := range []string{"enable", "disable", "reset"} {
				revision, _ := readConfigurationState(t, handler, headers)
				edits := make([]config.SettingEdit, 0)
				for _, path := range append(slices.Clone(paths), "app.search_url", "server.max_batch_body_size") {
					edit := config.SettingEdit{Path: path, Action: "set", Value: json.RawMessage(fmt.Sprint(action == "enable"))}
					switch path {
					case "app.search_url":
						edit.Value = json.RawMessage(`"https://search.example/?q={query}"`)
						if action == "disable" {
							edit.Value = json.RawMessage(`""`)
						}
					case "server.max_batch_body_size":
						edit.Value = json.RawMessage(`1`)
					}
					if action == "reset" {
						edit.Action, edit.Value = "reset", nil
					}
					edits = append(edits, edit)
				}
				draft := map[string]any{"revision": revision, "edits": edits}
				if action == "enable" {
					raw, err := json.Marshal(edits)
					if err != nil {
						t.Fatal(err)
					}
					draft["acknowledgment"] = settingsReview(t, handler, headers, revision, string(raw))
				}
				body, err := json.Marshal(draft)
				if err != nil {
					t.Fatal(err)
				}
				response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", bytes.NewReader(body), headers)
				if response.Code != http.StatusOK {
					t.Fatalf("%s: %d %s", action, response.Code, response.Body)
				}
				loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
				if err != nil {
					t.Fatal(err)
				}
				if err := loaded.Validate(); err != nil {
					t.Fatal(err)
				}
				if status := reloadRequest(t, handler, token); status != http.StatusOK {
					t.Fatalf("reload saved settings: %d", status)
				}
				_, values := readConfigurationState(t, handler, headers)
				for _, path := range paths {
					if values[path].Value != (action == "enable") || values[path].Source.WebSelected != (action != "reset") {
						t.Fatalf("%s did not persist %s: %+v", action, path, values[path])
					}
				}
				public := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/config", nil, nil)
				var active struct {
					SearchURL         string `json:"searchUrl"`
					Version           string `json:"version"`
					MaxBatchBodyBytes int64  `json:"maxBatchBodyBytes"`
				}
				if err := json.Unmarshal(public.Body.Bytes(), &active); err != nil {
					t.Fatal(err)
				}
				wantURL := config.CreateDefaultConfig().App.SearchURL
				if action == "enable" {
					wantURL = "https://search.example/?q={query}"
				}
				if searchURL := values["app.search_url"]; searchURL.Value != wantURL || searchURL.SecretPresent || !searchURL.Editable || searchURL.Source.WebSelected != (action != "reset") {
					t.Fatalf("search URL is not visible after %s: %+v", action, searchURL)
				}
				if active.SearchURL != wantURL || active.SearchURL != loaded.App.SearchURL || active.MaxBatchBodyBytes != loaded.Server.MaxBatchBodyBytes() || (active.Version != "") != (action == "enable") {
					t.Fatalf("public configuration did not refresh: %s", public.Body)
				}
				batch := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/batch", strings.NewReader(`{"ops":[{"op":"get","url":"https://example.com"}]}`+strings.Repeat(" ", 1<<20)), headers)
				wantBatch := http.StatusRequestEntityTooLarge
				if action == "reset" {
					wantBatch = http.StatusOK
				}
				if batch.Code != wantBatch {
					t.Fatalf("batch limit not applied: %d %s", batch.Code, batch.Body)
				}
				for _, path := range []string{"/metrics", "/debug/pprof/"} {
					response := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon"+path, nil, headers)
					if action == "enable" {
						if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "<title>Nogon") {
							t.Fatalf("monitoring unavailable: %s %d", path, response.Code)
						}
						for _, denied := range []string{"", ordinaryToken} {
							response := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon"+path, nil, map[string]string{"X-Access-Token": denied})
							if response.Code != http.StatusForbidden {
								t.Fatalf("monitoring exposed to nonadministrative visitor: %s %d", path, response.Code)
							}
						}
					} else if strings.Contains(response.Body.String(), "hister_documents") || strings.Contains(response.Body.String(), "Types of profiles available") {
						t.Fatalf("disabled monitoring still served at %s", path)
					}
				}
				extractors := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/extractors", nil, headers)
				if strings.Contains(extractors.Body.String(), `"timeout"`) != (action == "enable") {
					t.Fatal("extractor option visibility did not change")
				}
			}
			mainAfter, err := os.ReadFile(cfg.Filename())
			if err != nil || !bytes.Equal(mainBefore, mainAfter) {
				t.Fatal("ordinary settings changed the main file")
			}
		})
	}
}

func TestSettingsSensitivePatternApply(t *testing.T) {
	cfg, handler, _ := newReloadTestServer(t, false)
	headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret", "Content-Type": "application/json"}
	mainBefore, err := os.ReadFile(cfg.Filename())
	if err != nil {
		t.Fatal(err)
	}
	revision, _ := readConfigurationState(t, handler, headers)
	invalid := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"sensitive_content_patterns","action":"set","value":{"private":"[private-credential"}}]}`, revision)), headers)
	if invalid.Code != http.StatusUnprocessableEntity || strings.Contains(invalid.Body.String(), "private-credential") {
		t.Fatalf("invalid pattern was saved or exposed: %d %s", invalid.Code, invalid.Body)
	}
	if _, err := os.Stat(cfg.FullPath("config.web.yml")); !os.IsNotExist(err) {
		t.Fatal("rejected pattern wrote managed settings")
	}
	if current, _ := readConfigurationState(t, handler, headers); current != revision {
		t.Fatal("rejected pattern changed the runtime")
	}
	for _, tc := range []struct {
		name, edit string
		want       map[string]string
		web        bool
		blocked    string
	}{
		{"replace", `"action":"set","value":{"private":"private-[0-9]+"}`, map[string]string{"private": "private-[0-9]+"}, true, "private-123"},
		{"clear", `"action":"set","value":{}`, map[string]string{}, true, ""},
		{"reset", `"action":"reset"`, config.CreateDefaultConfig().SensitiveContentPatterns, false, "-----BEGIN OPENSSH PRIVATE KEY-----"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			revision, _ := readConfigurationState(t, handler, headers)
			edits := fmt.Sprintf(`[{"path":"sensitive_content_patterns",%s},{"path":"app.subtitle","action":"set","value":"Pattern draft"}]`, tc.edit)
			extra := ""
			if tc.name != "reset" {
				extra = fmt.Sprintf(`,"acknowledgment":%q`, settingsReview(t, handler, headers, revision, edits))
			}
			body := fmt.Sprintf(`{"revision":%q,"edits":%s%s}`, revision, edits, extra)
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
			if response.Code != http.StatusOK {
				t.Fatalf("apply patterns: %d %s", response.Code, response.Body)
			}
			loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
			if err != nil || !maps.Equal(loaded.SensitiveContentPatterns, tc.want) || loaded.Source("sensitive_content_patterns").WebSelected != tc.web {
				t.Fatalf("saved patterns did not survive loading: %v", err)
			}
			for _, reload := range []bool{false, true} {
				if reload && reloadRequest(t, handler, "secret") != http.StatusOK {
					t.Fatal("reload failed")
				}
				for i, content := range []string{"ordinary text", "private-123", "-----BEGIN OPENSSH PRIVATE KEY-----"} {
					body := fmt.Sprintf(`{"url":"https://example.com/%s/%t/%d","html":%q,"title":"Test document"}`, tc.name, reload, i, "<html><body><p>"+content+"</p></body></html>")
					added := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/add", strings.NewReader(body), headers)
					if content == tc.blocked {
						if added.Code != http.StatusUnprocessableEntity || !strings.Contains(added.Body.String(), "sensitive") {
							t.Fatalf("pattern did not block matching document: %d %s", added.Code, added.Body)
						}
					} else if added.Code != http.StatusCreated {
						t.Fatalf("pattern rejected allowed document: %d %s", added.Code, added.Body)
					}
				}
			}
			_, values := readConfigurationState(t, handler, headers)
			got, err := json.Marshal(values["sensitive_content_patterns"].Value)
			want, marshalErr := json.Marshal(tc.want)
			if err != nil || marshalErr != nil || string(got) != string(want) {
				t.Fatalf("reloaded patterns: %s, want %s", got, want)
			}
			public := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/config", nil, nil)
			if strings.Contains(public.Body.String(), "private-[0-9]+") || !strings.Contains(public.Body.String(), "Pattern draft") {
				t.Fatal("public configuration exposed patterns or lost the mixed draft")
			}
		})
	}
	mainAfter, err := os.ReadFile(cfg.Filename())
	if err != nil || string(mainAfter) != string(mainBefore) {
		t.Fatal("saving patterns rewrote the main configuration")
	}
}

func TestSettingsWebHotkeyApply(t *testing.T) {
	cfg, handler, _ := newReloadTestServer(t, false)
	headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret"}
	mainBefore, err := os.ReadFile(cfg.Filename())
	if err != nil {
		t.Fatal(err)
	}
	revision, _ := readConfigurationState(t, handler, headers)
	invalid := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"hotkeys.web","action":"set","value":{"ALT+X":"show_hotkeys"}}]}`, revision)), headers)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid shortcut saved: %d %s", invalid.Code, invalid.Body)
	}
	if _, err := os.Stat(cfg.FullPath("config.web.yml")); !os.IsNotExist(err) {
		t.Fatal("rejected shortcut wrote managed settings")
	}
	if current, _ := readConfigurationState(t, handler, headers); current != revision {
		t.Fatal("rejected shortcut changed the runtime")
	}
	for _, tc := range []struct {
		name, edit string
		want       map[string]string
		web        bool
	}{
		{"replace", `"action":"set","value":{"alt+x":"focus_search_input","?":"show_hotkeys"}`, map[string]string{"alt+x": "focus_search_input", "?": "show_hotkeys"}, true},
		{"clear", `"action":"set","value":{}`, map[string]string{}, true},
		{"reset", `"action":"reset"`, config.CreateDefaultConfig().Hotkeys.Web, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			revision, _ := readConfigurationState(t, handler, headers)
			body := fmt.Sprintf(`{"revision":%q,"edits":[{"path":"hotkeys.web",%s}]}`, revision, tc.edit)
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
			if response.Code != http.StatusOK {
				t.Fatalf("apply shortcuts: %d %s", response.Code, response.Body)
			}
			// The public response drives keyboard handling, including anonymous search.
			public := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/config", nil, nil)
			var active struct {
				Hotkeys map[string]string `json:"hotkeys"`
			}
			if err := json.Unmarshal(public.Body.Bytes(), &active); err != nil || !maps.Equal(active.Hotkeys, tc.want) {
				t.Fatalf("active shortcuts: %s, error: %v", public.Body, err)
			}
			loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
			if err != nil || !maps.Equal(loaded.Hotkeys.Web, tc.want) || loaded.Source("hotkeys.web").WebSelected != tc.web {
				t.Fatalf("saved shortcuts did not survive loading: %v", err)
			}
			if status := reloadRequest(t, handler, "secret"); status != http.StatusOK {
				t.Fatalf("reload shortcuts: %d", status)
			}
			_, values := readConfigurationState(t, handler, headers)
			got, err := json.Marshal(values["hotkeys.web"].Value)
			want, marshalErr := json.Marshal(tc.want)
			if err != nil || marshalErr != nil || string(got) != string(want) {
				t.Fatalf("reloaded shortcuts: %s, want %s", got, want)
			}
		})
	}
	mainAfter, err := os.ReadFile(cfg.Filename())
	if err != nil || string(mainAfter) != string(mainBefore) {
		t.Fatal("saving shortcuts rewrote the main configuration")
	}
}

func TestSettingsExtractionAndSemanticApply(t *testing.T) {
	var requests atomic.Int32
	embeddings := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"embedding":[1,0,0],"index":0}]}`)
	}))
	defer embeddings.Close()
	cfg, handler, _ := newReloadTestServer(t, false)
	mainBefore, err := os.ReadFile(cfg.Filename())
	if err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret", "Content-Type": "application/json"}
	added := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/add", strings.NewReader(`{"url":"https://example.com/existing","text":"Existing document without embeddings"}`), headers)
	if added.Code != http.StatusCreated {
		t.Fatalf("add existing document: %d %s", added.Code, added.Body)
	}
	revision, _ := readConfigurationState(t, handler, headers)
	edits := fmt.Sprintf(`[
		{"path":"extractors.markdown.enable","action":"set","value":false},
		{"path":"extractors.ytdlp.options.timeout","action":"set","value":10},
		{"path":"extractors.ytdlp.options.cookies_file","action":"set","value":{"action":"replace","value":"private-cookie-file"}},
		{"path":"semantic_search.enable","action":"set","value":true},
		{"path":"semantic_search.embedding_endpoint","action":"set","value":%q},
		{"path":"semantic_search.dimensions","action":"set","value":3},
		{"path":"semantic_search.api_key","action":"set","value":{"action":"replace","value":"private-api-key"}},
		{"path":"semantic_search.headers","action":"set","value":{"x-key":{"action":"replace","value":"private-header"}}}
	]`, embeddings.URL)
	acknowledgment := ""
	for _, operation := range []string{"validate", "apply"} {
		extra := ""
		if operation == "apply" {
			extra = fmt.Sprintf(`,"acknowledgment":%q`, acknowledgment)
		}
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/"+operation, strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s%s}`, revision, edits, extra)), headers)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-") {
			t.Fatalf("%s: %d %s", operation, response.Code, response.Body)
		}
		if requests.Load() != 0 {
			t.Fatalf("%s probed the endpoint or rebuilt existing embeddings", operation)
		}
		if operation == "validate" {
			var result struct {
				Review struct{ Acknowledgment string }
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Review.Acknowledgment == "" {
				t.Fatalf("missing embedding destination review: %v", err)
			}
			acknowledgment = result.Review.Acknowledgment
		}
	}
	_, values := readConfigurationState(t, handler, headers)
	if values["extractors.markdown.enable"].Value != false || values["extractors.ytdlp.enable"].Value != false || values["extractors.ytdlp.options.timeout"].Value != float64(10) || values["semantic_search.enable"].Value != true {
		t.Fatal("mixed settings draft was not activated")
	}
	for _, path := range []string{"/api/settings", "/api/config", "/api/extractors"} {
		response := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon"+path, nil, headers)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-") {
			t.Fatalf("stored credential exposed by %s: %d %s", path, response.Code, response.Body)
		}
	}
	loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
	if err != nil || !loaded.SemanticSearch.Enable || loaded.SemanticSearch.APIKey != "private-api-key" || loaded.SemanticSearch.Headers["x-key"] != "private-header" || loaded.Extractors["ytdlp"].Options["timeout"] != 10 {
		t.Fatalf("saved configuration cannot be loaded: %v", err)
	}
	if status := reloadRequest(t, handler, "secret"); status != http.StatusOK {
		t.Fatalf("reload saved settings: %d", status)
	}
	if requests.Load() != 0 {
		t.Fatal("reloading saved settings started embedding existing documents")
	}
	mainAfter, err := os.ReadFile(cfg.Filename())
	if err != nil || string(mainAfter) != string(mainBefore) {
		t.Fatal("saving rewrote the main configuration")
	}
}

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
	headers := map[string]string{"Origin": "nogon://", "X-Access-Token": admin.Token}
	revision, _ := readConfigurationState(t, handler, headers)
	held := make(chan struct{})
	go func() {
		testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/test-remove-owner", nil, headers)
		close(held)
	}()
	<-entered
	body := fmt.Sprintf(`{"revision":%q,"edits":[{"path":"indexer.directories","action":"set","value":[{"path":%q,"user":"directory-owner"}]}]}`, revision, t.TempDir())
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
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
	headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret"}
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
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/"+op, strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, edits)), headers)
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

func TestSettingsCrawlerSecretPersistence(t *testing.T) {
	cfg, handler, _ := newReloadTestServer(t, false)
	mainBefore, err := os.ReadFile(cfg.Filename())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.FullPath("config.web.yml"), []byte("crawler:\n  headers:\n    Authorization: saved-header-secret\n    X-Remove: old\n  cookies:\n    - name: session\n      value: saved-cookie-secret\n      domain: example.com\n      path: /\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := reloadRequest(t, handler, "secret"); status != http.StatusOK {
		t.Fatalf("reload: %d", status)
	}
	headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret"}
	revision, states := readConfigurationState(t, handler, headers)
	values := states["crawler.headers"].Value.(map[string]any)
	delete(values, "x-remove")
	values["x-new"] = map[string]any{"action": "replace", "value": "new-header-secret"}
	headerJSON, _ := json.Marshal(values)
	cookieJSON, _ := json.Marshal(states["crawler.cookies"].Value)
	body := fmt.Sprintf(`{"revision":%q,"edits":[{"path":"crawler.headers","action":"set","value":%s},{"path":"crawler.cookies","action":"set","value":%s},{"path":"crawler.timeout","action":"set","value":12}]}`, revision, headerJSON, cookieJSON)
	for _, op := range []string{"validate", "apply"} {
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/"+op, strings.NewReader(body), headers)
		if response.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", op, response.Code, response.Body)
		}
		for _, secret := range []string{"saved-header-secret", "saved-cookie-secret", "new-header-secret"} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatal("response exposed credentials")
			}
		}
	}
	loaded, _, err := config.LoadForInspection(cfg.Filename(), true)
	if err != nil || loaded.Crawler.Timeout != 12 || loaded.Crawler.Headers["authorization"] != "saved-header-secret" || loaded.Crawler.Headers["x-new"] != "new-header-secret" || len(loaded.Crawler.Headers) != 2 || loaded.Crawler.Cookies[0].Value != "saved-cookie-secret" {
		t.Fatalf("restart lost crawler selections: %v", err)
	}
	fresh, _ := readConfigurationState(t, handler, headers)
	// Updating the request revision cannot make an old secret reference valid.
	stale := strings.Replace(body, revision, fresh, 1)
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(stale), headers)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("stale credential accepted: %d %s", response.Code, response.Body)
	}
	for _, action := range []string{"set", "reset"} {
		revision, _ = readConfigurationState(t, handler, headers)
		edits := `[{"path":"crawler.headers","action":"set","value":{}},{"path":"crawler.cookies","action":"set","value":[]}]`
		if action == "reset" {
			edits = `[{"path":"crawler.headers","action":"reset"},{"path":"crawler.cookies","action":"reset"}]`
		}
		response = testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, edits)), headers)
		if response.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", action, response.Code, response.Body)
		}
		loaded, _, err = config.LoadForInspection(cfg.Filename(), true)
		if err != nil || len(loaded.Crawler.Headers) != 0 || len(loaded.Crawler.Cookies) != 0 || loaded.Source("crawler.cookies").WebSelected != (action == "set") {
			t.Fatalf("clear/reset did not persist: %v", err)
		}
	}
	after, _ := os.ReadFile(cfg.Filename())
	if string(after) != string(mainBefore) {
		t.Fatal("crawler save changed main file")
	}
}

func TestSettingsIndexerPersistenceAndWatching(t *testing.T) {
	cfg, handler, _ := newReloadTestServer(t, false)
	mainPath := filepath.Join(cfg.App.Directory, "config.yml")
	original, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret"}
	dir := t.TempDir()
	filename := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(filename, []byte("Saved watcher content"), 0o600); err != nil {
		t.Fatal(err)
	}
	save := func(edits string) {
		t.Helper()
		revision, _ := readConfigurationState(t, handler, headers)
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, edits)), headers)
		if response.Code != http.StatusOK {
			t.Fatalf("save: %d %s", response.Code, response.Body)
		}
	}
	save(fmt.Sprintf(`[{"path":"indexer.directories","action":"set","value":[{"path":%q,"label":"web-watched","filetypes":["txt"],"delete_on_remove":true}]},{"path":"indexer.languages","action":"set","value":["en","hu"]},{"path":"indexer.max_file_size_mb","action":"set","value":8}]`, dir))
	loaded, _, err := config.LoadForInspection(mainPath, true)
	if err != nil || len(loaded.Indexer.Directories) != 1 || loaded.Indexer.Directories[0].Path != dir || !loaded.Indexer.Directories[0].DeleteOnRemove || loaded.Indexer.MaxFileSize != 8 || !slices.Equal(loaded.Indexer.Languages, []string{"en", "hu"}) {
		t.Fatalf("restart lost indexer settings: %v", err)
	}
	path := "/nogon/api/document?url=" + url.QueryEscape(files.PathToFileURL(filename))
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
	headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret"}
	login := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/token-login", strings.NewReader(`{"token":"secret"}`), headers)
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
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
		if response.Code != http.StatusOK {
			t.Fatalf("save: %d %s", response.Code, response.Body)
		}
		fresh, values := readConfigurationState(t, handler, headers)
		if fresh == revision || values["app.disable_previews"].Value != true {
			t.Fatalf("save did not publish a new configuration: %s %+v", fresh, values["app.disable_previews"])
		}
		// A client that lost the response must read state, not replay its old draft.
		replay := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(body), headers)
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
		session := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/history", nil, map[string]string{"Cookie": cookie.String()})
		if session.Code != http.StatusOK {
			t.Fatalf("save invalidated the authenticated session: %d %s", session.Code, session.Body)
		}
		revision = fresh
	}
	_, values := readConfigurationState(t, handler, headers)
	if values["app.title"].Value != "Nogon" || values["app.title"].Source.WebSelected {
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
			headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret"}
			revision, _ := readConfigurationState(t, handler, headers)
			server := httptest.NewServer(handler)
			defer server.Close()
			conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/nogon/search", http.Header{"Origin": {cfg.Server.BaseURL}, "X-Access-Token": {"secret"}})
			if resp != nil {
				defer func() { _ = resp.Body.Close() }()
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, tc.edits)), headers)
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
	headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret"}
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
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(string(raw)), headers)
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
	headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret"}
	for round := range 4 {
		revision, _ := readConfigurationState(t, handler, headers)
		start := make(chan struct{})
		results := make(chan int, 3)
		for worker := range 3 {
			go func() {
				<-start
				path, body := "/nogon/api/reload", ""
				if worker != 2 {
					path = "/nogon/api/settings/apply"
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

func TestFullReloadAllowsMissingRecoveryAdministrator(t *testing.T) {
	for _, account := range []string{"empty", "ordinary", "administrator", "proxy", "oidc administrator"} {
		t.Run(account, func(t *testing.T) {
			cfg, handler, _ := newReloadTestServer(t, false)
			var admin *model.User
			if account == "ordinary" || account == "administrator" || account == "oidc administrator" {
				var err error
				if account == "oidc administrator" {
					admin, err = model.CreateOAuthUser("owner", "oidc-owner@example.com")
					if err == nil {
						admin, err = model.SyncOAuthAdmin(admin.OAuthID, true)
					}
				} else {
					admin, err = model.CreateUser("owner", "password123", account == "administrator")
				}
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
			if account == "oidc administrator" {
				raw = []byte(strings.Replace(string(raw), "server:\n", "server:\n  oauth_only: true\n  oauth:\n    oidc:\n      client_id: nogon\n      client_secret: secret\n      auth_url: https://provider.example/auth\n      token_url: https://provider.example/token\n      userinfo_url: https://provider.example/userinfo\n      admin_groups: [owners]\n", 1))
			}
			if err := os.WriteFile(cfg.Filename(), raw, 0o600); err != nil {
				t.Fatal(err)
			}
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/reload", nil, map[string]string{"Origin": "nogon://", "X-Access-Token": "secret"})
			if response.Code != http.StatusOK {
				t.Fatalf("missing recovery access must not prevent reload: %d %s", response.Code, response.Body)
			}
			for _, token := range []string{"", "secret"} {
				denied := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/users", nil, map[string]string{"X-Access-Token": token})
				if denied.Code != http.StatusForbidden {
					t.Fatalf("reload exposed administration to an anonymous or instance token caller: %d", denied.Code)
				}
			}
			if admin == nil {
				return
			}
			headers := map[string]string{"Origin": "nogon://", "X-Access-Token": admin.Token}
			if !admin.IsAdmin {
				denied := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/users", nil, headers)
				if denied.Code != http.StatusForbidden {
					t.Fatalf("reload granted administrator access to an ordinary user: %d", denied.Code)
				}
				return
			}
			// Existing OIDC deployments can also apply unrelated web settings.
			revision, _ := readConfigurationState(t, handler, headers)
			response = testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.subtitle","action":"set","value":"Still multiuser"}]}`, revision)), headers)
			if response.Code != http.StatusOK {
				t.Fatalf("ordinary multiuser save: %d %s", response.Code, response.Body)
			}
		})
	}
}

func TestRuntimeWarnsAboutUnavailableAdministration(t *testing.T) {
	for _, tc := range []struct {
		account string
		oidc    bool
		groups  bool
		warning string
	}{
		{account: "empty", warning: "No usable administrator"},
		{account: "ordinary", warning: "No usable administrator"},
		{account: "administrator"},
		{account: "database error"},
		{account: "empty", oidc: true, groups: true, warning: "No usable administrator"},
		{account: "ordinary", oidc: true, groups: true, warning: "No usable administrator"},
		{account: "administrator", oidc: true, groups: true, warning: "No independent local recovery administrator"},
		{account: "administrator", oidc: true},
		{account: "recovery", oidc: true, groups: true},
	} {
		t.Run(fmt.Sprintf("%s/oidc=%t/groups=%t", tc.account, tc.oidc, tc.groups), func(t *testing.T) {
			var logs bytes.Buffer
			previous, level := log.Logger, zerolog.GlobalLevel()
			log.Logger = zerolog.New(zerolog.SyncWriter(&logs))
			zerolog.SetGlobalLevel(zerolog.InfoLevel)
			t.Cleanup(func() {
				log.Logger = previous
				zerolog.SetGlobalLevel(level)
			})
			cfg := testutil.Config(t)
			cfg.App.UserHandling = true
			cfg.Server.Address = "127.0.0.1:0"
			if tc.oidc {
				cfg.Server.OAuthOnly = true
				cfg.Server.OAuth = map[string]*config.OAuthEntry{"oidc": {}}
				if tc.groups {
					cfg.Server.OAuth["oidc"].AdminGroups = []string{"owners"}
				}
			}
			testutil.InitModelWithConfig(t, cfg)
			var token string
			if tc.account == "ordinary" || tc.account == "administrator" || tc.account == "recovery" {
				var user *model.User
				var err error
				if tc.oidc && tc.account != "recovery" {
					user, err = model.CreateOAuthUser("owner", "oidc-owner@example.com")
					if err == nil && tc.account == "administrator" {
						user, err = model.SyncOAuthAdmin(user.OAuthID, true)
					}
				} else {
					user, err = model.CreateUser("owner", "password123", tc.account != "ordinary")
				}
				if err != nil {
					t.Fatal(err)
				}
				token = user.Token
			}
			idx := newServerTestIndexer(t, cfg)
			if tc.account == "database error" {
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
			if tc.account == "database error" {
				if err == nil || !strings.Contains(err.Error(), "database query failed") {
					t.Fatalf("database failure must stop startup: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("missing administration must not prevent startup: %v", err)
			}
			if tc.warning != "" {
				for _, want := range []string{`"level":"warn"`, tc.warning, "nogon user create --offline --admin USERNAME", "API token"} {
					if !strings.Contains(logs.String(), want) {
						t.Errorf("startup log missing %q: %s", want, &logs)
					}
				}
				if tc.groups && !strings.Contains(logs.String(), "OIDC login") {
					t.Errorf("warning must explain OIDC recovery: %s", &logs)
				}
			} else if strings.Contains(logs.String(), `"level":"warn"`) {
				t.Errorf("usable administration should not warn: %s", &logs)
			}
			if strings.Contains(logs.String(), "password123") || (token != "" && strings.Contains(logs.String(), token)) {
				t.Fatal("startup log exposed credentials")
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
	for _, change := range []string{"demote", "rotate token"} {
		t.Run(change, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			oldEndpoints := Endpoints
			Endpoints = append(append([]*Endpoint(nil), Endpoints...), &Endpoint{
				Path: "/api/test-demote", Method: GET,
				Handler: func(c *webContext) {
					close(entered)
					<-release
					if change == "demote" {
						if _, err := model.ToggleAdmin("saving-admin"); err != nil {
							t.Error(err)
						}
					} else {
						actor, err := model.GetUser("saving-admin")
						if err == nil {
							_, err = model.RotateUserToken(actor.ID, actor.ID, actor.Revision)
						}
						if err != nil {
							t.Error(err)
						}
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
			headers := map[string]string{"Origin": "nogon://", "X-Access-Token": actor.Token}
			revision, _ := readConfigurationState(t, handler, headers)
			held := make(chan struct{})
			go func() {
				testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/test-demote", nil, headers)
				close(held)
			}()
			<-entered
			result := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				result <- testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.subtitle","action":"set","value":"Unauthorized"}]}`, revision)), headers)
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
		})
	}
}

func TestReloadRejectsRevokedCredentialsAfterDraining(t *testing.T) {
	for _, method := range []string{"user token", "user session", "instance session"} {
		t.Run(method, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			oldEndpoints := Endpoints
			Endpoints = append(append([]*Endpoint(nil), Endpoints...), &Endpoint{
				Path: "/api/test-revoke", Method: GET,
				Handler: func(c *webContext) {
					close(entered)
					<-release
					if method != "user token" {
						serveLogout(c)
						return
					}
					actor, err := model.GetUser("runtime-owner")
					if err == nil {
						_, err = model.RotateUserToken(actor.ID, actor.ID, actor.Revision)
					}
					if err != nil {
						t.Error(err)
					}
					serve200(c)
				},
			})
			t.Cleanup(func() { Endpoints = oldEndpoints })
			_, handler, write := newReloadTestServer(t, method != "instance session")
			headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret"}
			if method != "instance session" {
				actor, err := model.GetUser("runtime-owner")
				if err != nil {
					t.Fatal(err)
				}
				headers["X-Access-Token"] = actor.Token
			}
			if method != "user token" {
				path, body := "/nogon/api/login", `{"username":"runtime-owner","password":"password123"}`
				if method == "instance session" {
					path, body = "/nogon/api/token-login", `{"token":"secret"}`
				}
				login := testutil.ServeHTTP(t, handler, http.MethodPost, path, strings.NewReader(body), headers)
				if login.Code != http.StatusOK {
					t.Fatalf("login: %d %s", login.Code, login.Body)
				}
				headers = map[string]string{"Origin": "nogon://", "Cookie": responseSessionCookie(t, login).String()}
			}
			write("  title: Unauthorized reload\n")
			held := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				held <- testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/test-revoke", nil, headers)
			}()
			<-entered
			result := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				result <- testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/reload", nil, headers)
			}()
			waitRuntimePause(t, handler)
			unblock()
			if response := <-held; response.Code != http.StatusOK {
				t.Fatalf("credential revocation: %d %s", response.Code, response.Body)
			}
			if response := <-result; response.Code != http.StatusForbidden {
				t.Errorf("revoked credential reloaded configuration: %d %s", response.Code, response.Body)
			}
			if response := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/settings", nil, headers); response.Code != http.StatusForbidden {
				t.Errorf("revoked credential remains authorized: %d", response.Code)
			}
			response := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/config", nil, nil)
			var current struct{ Title string }
			if err := json.Unmarshal(response.Body.Bytes(), &current); err != nil || response.Code != http.StatusOK || current.Title != "Original" {
				t.Fatalf("denied reload did not retain active configuration: %d %s (%v)", response.Code, response.Body, err)
			}
		})
	}
}

func waitRuntimePause(t *testing.T, handler http.Handler) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if response := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/health", nil, nil); response.Code == http.StatusServiceUnavailable {
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
			headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret"}
			revision, _ := readConfigurationState(t, handler, headers)
			held := make(chan struct{})
			go func() {
				testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/test-held", nil, headers)
				close(held)
			}()
			<-entered
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			request := httptest.NewRequest(http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.subtitle","action":"set","value":"Saved subtitle"}]}`, revision))).WithContext(ctx)
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
				if reply := testutil.ServeHTTP(t, handler, method, "/nogon"+path, nil, headers); reply.Code != http.StatusServiceUnavailable {
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
				if reply := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/health", nil, nil); reply.Code != http.StatusServiceUnavailable {
					t.Fatal("failed runtime still accepts requests")
				}
			} else {
				_, values := readConfigurationState(t, handler, headers)
				if (values["app.subtitle"].Value == "Saved subtitle") != (scenario == "lost response") {
					t.Fatalf("unexpected active subtitle: %+v", values["app.subtitle"])
				}
				rules := testutil.ServeHTTP(t, handler, http.MethodGet, "/nogon/api/rules", nil, headers)
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
			t.Setenv("HISTER__SERVER__BASE_URL", "http://127.0.0.1:4433/nogon")
			t.Setenv("HISTER__APP__ACCESS_TOKEN", token)
			t.Setenv("HISTER__INDEXER__DETECT_LANGUAGES", "false")
			cfg, err := config.Load("")
			if err != nil || cfg.Filename() != "*Default Config*" {
				t.Fatalf("load without main file: %v", err)
			}
			testutil.InitModelWithConfig(t, cfg)
			handler := startReloadTestRuntime(t, cfg)
			headers := map[string]string{"Origin": "nogon://", "X-Access-Token": token}
			revision, _ := readConfigurationState(t, handler, headers)
			response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.title","action":"set","value":"No main file"}]}`, revision)), headers)
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
	headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret"}
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
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":%s}`, revision, edits)), headers)
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
		response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.subtitle","action":"set","value":"Cannot stage"}]}`, revision)), headers)
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
	headers := map[string]string{"Origin": "nogon://", "X-Access-Token": "secret"}
	revision, before := readConfigurationState(t, handler, headers)
	// Rules are read during activation, after the managed file has been staged.
	if err := os.Remove(cfg.RulesPath()); err != nil {
		t.Fatal(err)
	}
	response := testutil.ServeHTTP(t, handler, http.MethodPost, "/nogon/api/settings/apply", strings.NewReader(fmt.Sprintf(`{"revision":%q,"edits":[{"path":"app.subtitle","action":"set","value":"Candidate"}]}`, revision)), headers)
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
		req.Header.Set("Origin", "nogon://")
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
