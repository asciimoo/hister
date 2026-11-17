// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"
	"github.com/asciimoo/hister/server/types"
)

func TestPurgeCommandPreviewsBeforeDeletion(t *testing.T) {
	for _, tc := range []struct {
		name, input, want  string
		flags              []string
		apply, failPreview bool
		previewBody        string
	}{
		{name: "dry run", flags: []string{"--dry"}, want: "2 version(s)"},
		{name: "yes", flags: []string{"--yes"}, apply: true, want: "Purged"},
		{name: "confirmation", input: "yes\n", apply: true, want: "Purged"},
		{name: "cancel", input: "no\n", want: "Purge cancelled"},
		{name: "failed preview", flags: []string{"--yes"}, failPreview: true},
		{name: "invalid preview", flags: []string{"--yes"}, failPreview: true, previewBody: `{}`},
		{name: "negative count", flags: []string{"--yes"}, failPreview: true, previewBody: `{"dry_run":true,"versions":-1}`},
		{name: "no matches", flags: []string{"--yes"}, want: "0 version(s)", previewBody: `{"dry_run":true,"versions":0}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldConfig, oldTransport, oldPreRun := cfg, http.DefaultTransport, rootCmd.PersistentPreRun
			t.Cleanup(func() {
				cfg, http.DefaultTransport, rootCmd.PersistentPreRun = oldConfig, oldTransport, oldPreRun
				rootCmd.SetIn(nil)
			})
			cfg = testutil.Config(t)
			cfg.App.AccessToken = "purge-token"
			if err := cfg.UpdateBaseURL("https://remote.example/hister"); err != nil {
				t.Fatal(err)
			}
			rootCmd.PersistentPreRun = func(*cobra.Command, []string) {}
			rootCmd.SetIn(strings.NewReader(tc.input))
			previews, deletions := 0, 0
			http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodPost || req.URL.Path != "/hister/api/purge" || req.Header.Get("X-Access-Token") != "purge-token" {
					t.Fatalf("unexpected purge request: %s %s", req.Method, req.URL)
				}
				var body types.PurgeRequest
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if !body.Versions || !body.History || body.CrawlJobs || body.Before.Format(time.RFC3339) != "2026-01-01T00:00:00Z" {
					t.Fatalf("purge body=%+v", body)
				}
				if body.DryRun {
					previews++
				} else {
					deletions++
					if previews != 1 {
						t.Fatal("deletion did not follow a preview")
					}
				}
				if tc.previewBody != "" {
					return jsonHTTPResponse(req, 200, tc.previewBody), nil
				}
				if tc.failPreview {
					return jsonHTTPResponse(req, 500, `{}`), nil
				}
				return jsonHTTPResponse(req, 200, fmt.Sprintf(`{"dry_run":%t,"versions":2,"history_entries":3,"history_queries":1,"history_urls":1,"crawl_jobs":0,"crawl_urls":0}`, body.DryRun)), nil
			})
			args := append([]string{"purge", "--versions", "--history", "--before", "2026-01-01"}, tc.flags...)
			output, _, err := executeUserCommandForTest(t, args...)
			if (err != nil) != tc.failPreview {
				t.Fatalf("error=%v, output=%s", err, output)
			}
			if !tc.failPreview && !strings.Contains(output, tc.want) {
				t.Fatalf("output=%q, want %q", output, tc.want)
			}
			wantDeletions := 0
			if tc.apply {
				wantDeletions = 1
			}
			if previews != 1 || deletions != wantDeletions {
				t.Fatalf("previews=%d, deletions=%d", previews, deletions)
			}
		})
	}
}

func TestPurgeCommandRejectsInvalidSelectionBeforeRequest(t *testing.T) {
	for _, args := range [][]string{
		{"purge", "--versions"},
		{"purge", "--before", "2026-01-01"},
		{"purge", "--versions", "--before", "tomorrow"},
		{"purge", "--versions", "--before", "9999-01-01"},
		{"purge", "--versions", "--before", "2026-01-01", "extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			oldPreRun, oldTransport := rootCmd.PersistentPreRun, http.DefaultTransport
			t.Cleanup(func() { rootCmd.PersistentPreRun, http.DefaultTransport = oldPreRun, oldTransport })
			rootCmd.PersistentPreRun = func(*cobra.Command, []string) {}
			http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid purge selection sent a request")
				return nil, nil
			})
			if _, _, err := executeUserCommandForTest(t, args...); err == nil {
				t.Fatal("invalid purge selection accepted")
			}
		})
	}
}

func executeUserCommandForTest(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	command, _, err := rootCmd.Find(args)
	if err != nil {
		t.Fatal(err)
	}
	command.InitDefaultHelpFlag()
	command.Flags().VisitAll(func(flag *pflag.Flag) {
		value, changed := flag.Value.String(), flag.Changed
		t.Cleanup(func() {
			if err := flag.Value.Set(value); err != nil {
				t.Error(err)
			}
			flag.Changed = changed
		})
	})
	return executeInspectionWithStderr(t, args...)
}

func executeDeleteCommandForTest(t *testing.T, input string, args ...string) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	streams := make([]*os.File, 3)
	for n, name := range []string{"stdin", "stdout", "stderr"} {
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		streams[n] = file
		t.Cleanup(func() {
			if err := file.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	if _, err := streams[0].WriteString(input); err != nil {
		t.Fatal(err)
	}
	if _, err := streams[0].Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	stdin, stdout, stderr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = streams[0], streams[1], streams[2]
	defer func() { os.Stdin, os.Stdout, os.Stderr = stdin, stdout, stderr }()
	command := &cobra.Command{Use: deleteCmd.Use, Args: deleteCmd.Args, Run: deleteCmd.Run, RunE: deleteCmd.RunE, SilenceUsage: true, SilenceErrors: true}
	command.Flags().Bool("dry", false, "")
	command.Flags().Bool("verbose", false, "")
	command.Flags().Bool("yes", false, "")
	command.SetArgs(args)
	err := command.Execute()
	output := make([]string, 2)
	for n, file := range streams[1:] {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatal(err)
		}
		output[n] = string(data)
	}
	return output[0], output[1], err
}

func TestDeleteDocumentsUsesDeletionScope(t *testing.T) {
	for _, tc := range []struct {
		name, input, output, prompt string
		flags                       []string
		matched                     int
		searchEmpty                 bool
		deleted                     bool
	}{
		{name: "dry run", flags: []string{"--dry"}, matched: 3, output: "3 document(s) would be deleted"},
		{name: "verbose dry run", flags: []string{"--dry", "--verbose"}, matched: 3, output: "3 document(s) would be deleted"},
		{name: "confirmation", input: "y\n", matched: 3, prompt: "Delete 3 document(s)?", deleted: true},
		{name: "cancellation", input: "n\n", matched: 3, prompt: "Delete 3 document(s)?", output: "Deletion cancelled"},
		{name: "matches outside search visibility", flags: []string{"--yes"}, matched: 3, searchEmpty: true, deleted: true},
		{name: "no deletable documents", flags: []string{"--yes"}, matched: 0, output: "No documents matched the query"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previousConfig, previousTransport := cfg, http.DefaultTransport
			t.Cleanup(func() { cfg, http.DefaultTransport = previousConfig, previousTransport })
			cfg = testutil.Config(t)
			cfg.App.AccessToken = "admin-token"
			if err := cfg.UpdateBaseURL("https://remote.example/hister"); err != nil {
				t.Fatal(err)
			}
			deleted := false
			http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("X-Access-Token") != "admin-token" {
					t.Error("request did not authenticate with the configured token")
				}
				switch req.URL.Path {
				case "/hister/search":
					if tc.searchEmpty {
						return jsonHTTPResponse(req, http.StatusOK, `{"total":0,"documents":[]}`), nil
					}
					return jsonHTTPResponse(req, http.StatusOK, `{"total":1,"documents":[{"url":"https://example.com/visible"}]}`), nil
				case "/hister/api/delete":
					var body struct {
						Query  string `json:"query"`
						DryRun bool   `json:"dry_run"`
					}
					if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if req.Method != http.MethodPost || body.Query != "domain:example.com" {
						t.Errorf("unexpected deletion request: %s %+v", req.Method, body)
					}
					if body.DryRun {
						return jsonHTTPResponse(req, http.StatusOK, fmt.Sprintf(`{"matched":%d}`, tc.matched)), nil
					}
					deleted = true
					return jsonHTTPResponse(req, http.StatusOK, fmt.Sprintf(`{"deleted":%d}`, tc.matched)), nil
				default:
					t.Errorf("unexpected request: %s", req.URL)
					return jsonHTTPResponse(req, http.StatusNotFound, ""), nil
				}
			})
			out, stderr, err := executeDeleteCommandForTest(t, tc.input, append([]string{"domain:example.com"}, tc.flags...)...)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, tc.output) || !strings.Contains(stderr, tc.prompt) {
				t.Errorf("stdout = %q, stderr = %q; want %q and %q", out, stderr, tc.output, tc.prompt)
			}
			if tc.name == "verbose dry run" && (!strings.Contains(out, "https://example.com/visible") || !strings.Contains(stderr, "may differ from the documents selected for deletion")) {
				t.Errorf("verbose output did not explain search visibility: stdout = %q, stderr = %q", out, stderr)
			}
			if deleted != tc.deleted {
				t.Errorf("deleted = %t, want %t", deleted, tc.deleted)
			}
		})
	}
}

func TestDeleteDocumentsRejectsInvalidPreview(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{name: "server error", status: http.StatusInternalServerError, body: "cannot count documents"},
		{name: "missing count", status: http.StatusOK, body: `{}`},
		{name: "null count", status: http.StatusOK, body: `{"matched":null}`},
		{name: "negative count", status: http.StatusOK, body: `{"matched":-1}`},
		{name: "invalid JSON", status: http.StatusOK, body: `not JSON`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previousConfig, previousTransport := cfg, http.DefaultTransport
			t.Cleanup(func() { cfg, http.DefaultTransport = previousConfig, previousTransport })
			cfg = testutil.Config(t)
			http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				var body struct {
					DryRun bool `json:"dry_run"`
				}
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if req.URL.Path != "/api/delete" || !body.DryRun {
					t.Errorf("attempted deletion or fallback after an invalid preview: %s", req.URL)
				}
				return jsonHTTPResponse(req, tc.status, tc.body), nil
			})
			out, stderr, err := executeDeleteCommandForTest(t, "", "domain:example.com", "--yes")
			if err == nil || !strings.Contains(err.Error(), "failed to inspect documents for deletion") {
				t.Errorf("error = %v, want a deletion preview error", err)
			}
			if out != "" || stderr != "" {
				t.Errorf("invalid preview produced output: stdout = %q, stderr = %q", out, stderr)
			}
		})
	}
}

func TestDeprecatedCommandsAreRejected(t *testing.T) {
	for _, name := range []string{"create-config", "create-user", "show-user", "update-user", "delete-user"} {
		t.Run(name, func(t *testing.T) {
			_, _, err := executeInspectionWithStderr(t, name)
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("unknown command %q", name)) {
				t.Errorf("expected unknown command error for %s, got %v", name, err)
			}
		})
	}
}

func TestUserCommandHelp(t *testing.T) {
	previousPreRun := rootCmd.PersistentPreRun
	rootCmd.PersistentPreRun = func(*cobra.Command, []string) { t.Error("help initialized runtime state") }
	t.Cleanup(func() { rootCmd.PersistentPreRun = previousPreRun })
	for _, args := range [][]string{{"--help"}, {"user"}, {"user", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, _, err := executeUserCommandForTest(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, "user") {
				t.Errorf("help does not mention user commands: %s", out)
			}
			for _, action := range []string{"create", "show", "update", "delete"} {
				if strings.Contains(out, action+"-user") {
					t.Errorf("help advertises deprecated command %s-user", action)
				}
				if args[0] == "user" && !strings.Contains(out, action) {
					t.Errorf("user help does not list %s", action)
				}
			}
		})
	}
	for _, tc := range []struct {
		action string
		flags  []string
	}{
		{"create", []string{"--admin", "--offline"}},
		{"show", []string{"--show-token"}},
		{"update", []string{"--username", "--password", "--regen-token", "--toggle-admin"}},
		{"delete", []string{"--purge"}},
	} {
		args := []string{"user", tc.action}
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, _, err := executeUserCommandForTest(t, append(args, "--help")...)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range append(tc.flags, "nogon "+strings.Join(args, " ")+" USERNAME", "--server-url", "--token", "--client-timeout") {
				if !strings.Contains(out, want) {
					t.Errorf("help missing %q: %s", want, out)
				}
			}
			if strings.Contains(out, "--search-url") {
				t.Error("user help includes an unrelated search flag")
			}
		})
	}
}

func TestUserCommands(t *testing.T) {
	for _, tc := range []struct {
		action, method, path, response, output string
		flags                                  []string
		payload                                map[string]any
	}{
		{"create", http.MethodPost, "/hister/api/users", `{"id":77,"username":"alice","is_admin":true}`, "User created: alice", []string{"--admin"}, map[string]any{"username": "alice", "password": "password123", "is_admin": true}},
		{"show", http.MethodGet, "/hister/api/users", `{"users":[{"id":77,"username":"alice","revision":4}]}`, "Username:   alice", nil, nil},
		{"show", http.MethodGet, "/hister/api/users/77/token", `{"token":"user-token"}`, "Token:      user-token", []string{"--show-token"}, nil},
		{"update", http.MethodPatch, "/hister/api/users/77", `{"id":77,"username":"renamed","is_admin":true,"revision":5,"token":"new-token"}`, "New token for renamed: new-token", []string{"--username", "renamed", "--password", "--regen-token", "--toggle-admin"}, map[string]any{"username": "renamed", "password": "password123", "regenerate_token": true, "is_admin": true, "revision": float64(4)}},
		{"delete", http.MethodDelete, "/hister/api/users/77", `{"id":77,"username":"alice","deleted_documents":0}`, "User deleted: alice", nil, nil},
		{"delete", http.MethodDelete, "/hister/api/users/77?purge=true", `{"id":77,"username":"alice","deleted_documents":3}`, "Purged 3 document(s)", []string{"--purge"}, nil},
	} {
		t.Run(fmt.Sprintf("%s/%v", tc.action, tc.flags), func(t *testing.T) {
			oldCfg, oldAgent, oldTransport, oldPrompt := cfg, UserAgent, http.DefaultTransport, promptPassword
			oldDB := model.DB
			t.Cleanup(func() {
				cfg, UserAgent, http.DefaultTransport, promptPassword = oldCfg, oldAgent, oldTransport, oldPrompt
			})
			dir := t.TempDir()
			filename := filepath.Join(dir, "client.yml")
			if err := os.WriteFile(filename, []byte("app:\n  user_handling: false\nserver:\n  database: /does/not/exist/hister.db\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HISTER_DATA_DIR", filepath.Join(dir, "unused-data"))
			promptPassword = func(string) (string, error) { return "password123", nil }
			calls := 0
			http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.Host != "remote.example" || req.Header.Get("X-Access-Token") != "admin-token" || req.Header.Get("Origin") != "hister://" {
					t.Errorf("unexpected server or authentication: %s", req.URL)
				}
				if req.Method == http.MethodGet && req.URL.Path == "/hister/api/users" {
					if req.URL.Query().Get("username") != "alice" || req.URL.Query().Get("limit") != "1" {
						t.Errorf("unexpected user lookup: %s", req.URL)
					}
					return jsonHTTPResponse(req, http.StatusOK, `{"users":[{"id":77,"username":"alice","revision":4}]}`), nil
				}
				if req.Method != tc.method || req.URL.RequestURI() != tc.path {
					t.Errorf("unexpected request: %s %s", req.Method, req.URL)
				}
				if tc.payload != nil {
					body, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatal(err)
					}
					assertContractJSON(t, body, contractJSON(t, tc.payload))
				}
				status := http.StatusOK
				if tc.action == "create" {
					status = http.StatusCreated
				}
				return jsonHTTPResponse(req, status, tc.response), nil
			})
			globals := []string{"--config", filename, "--server-url", "https://remote.example/hister", "--token", "admin-token", "--client-timeout", "2"}
			args := append(globals, "user", tc.action, "alice")
			out, stderr, err := executeUserCommandForTest(t, append(args, tc.flags...)...)
			if err != nil {
				t.Fatal(err)
			}
			out = ansi.Strip(out)
			wantCalls := 2
			if tc.action == "create" || tc.action == "show" && len(tc.flags) == 0 {
				wantCalls = 1
			}
			if calls != wantCalls || !strings.Contains(out, tc.output) {
				t.Errorf("requests = %d, want %d; output = %q", calls, wantCalls, out)
			}
			if strings.Contains(out, "deprecated") || strings.Contains(out, "admin-token") || strings.Contains(out, "password123") {
				t.Errorf("stdout contains a warning or credentials: %q", out)
			}
			if stderr != "" {
				t.Errorf("unexpected stderr: %q", stderr)
			}
			if model.DB != oldDB {
				t.Error("remote command opened a local database")
			}
		})
	}
}

func TestUserCommandsRequireOneUsername(t *testing.T) {
	previousPreRun := rootCmd.PersistentPreRun
	rootCmd.PersistentPreRun = func(*cobra.Command, []string) { t.Error("invalid arguments reached runtime initialization") }
	t.Cleanup(func() { rootCmd.PersistentPreRun = previousPreRun })
	for _, action := range []string{"create", "show", "update", "delete"} {
		for _, names := range [][]string{nil, {"alice", "bob"}} {
			args := append([]string{"user", action}, names...)
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				_, _, err := executeUserCommandForTest(t, args...)
				if err == nil || !strings.Contains(err.Error(), "accepts 1 arg(s)") {
					t.Errorf("expected username argument error, got %v", err)
				}
			})
		}
	}
}

func TestDeleteUserUsesRemoteAccount(t *testing.T) {
	previousConfig, previousDB, previousTransport := cfg, model.DB, http.DefaultTransport
	t.Cleanup(func() { cfg, model.DB, http.DefaultTransport = previousConfig, previousDB, previousTransport })
	cfg = testutil.InitModel(t)
	cfg.App.AccessToken = "admin-token"
	if err := cfg.UpdateBaseURL("https://remote.example/hister"); err != nil {
		t.Fatal(err)
	}
	localUser := testutil.CreateUser(t, "alice")
	localDB := model.DB
	var paths []string
	var output bytes.Buffer
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.URL.Host != "remote.example" || req.Header.Get("X-Access-Token") != "admin-token" || req.Header.Get("Origin") != "hister://" {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL)
		}
		body := `{}`
		switch req.URL.Path {
		case "/hister/api/users":
			if req.Method != http.MethodGet || req.URL.Query().Get("username") != "alice" || req.URL.Query().Get("limit") != "1" {
				t.Errorf("unexpected lookup: %s %s", req.Method, req.URL)
			}
			body = `{"users":[{"id":77,"username":"alice"}]}`
		case "/hister/api/users/77":
			if len(paths) != 2 || req.Method != http.MethodDelete || req.URL.RawQuery != "purge=true" || req.Body != nil {
				t.Errorf("unexpected deletion: %s %s", req.Method, req.URL)
			}
			if output.Len() != 0 {
				t.Errorf("printed success before deletion: %s", &output)
			}
			// A rename after the lookup must not change the selected account ID.
			body = `{"id":77,"username":"renamed","deleted_documents":3}`
		default:
			t.Errorf("unexpected request: %s %s", req.Method, req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	cmd := newDeleteUserTestCommand()
	cmd.SetArgs([]string{"alice", "--purge"})
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if current, err := model.GetUserByID(localUser.ID); err != nil || *current != *localUser || model.DB != localDB {
		t.Errorf("remote deletion changed the local account: %v", err)
	}
	if len(paths) != 2 || paths[0] != "/hister/api/users" || paths[1] != "/hister/api/users/77" {
		t.Errorf("requests = %v, want one lookup and one deletion by ID", paths)
	}
	if text := ansi.Strip(output.String()); !strings.Contains(text, "Purged 3 document(s)") || !strings.Contains(text, "User deleted: renamed") {
		t.Errorf("output = %q", output.String())
	}
}

func TestDeleteUserRemoteErrorsDoNotFallBack(t *testing.T) {
	previousConfig, previousDB, previousTransport := cfg, model.DB, http.DefaultTransport
	t.Cleanup(func() { cfg, model.DB, http.DefaultTransport = previousConfig, previousDB, previousTransport })
	cfg = testutil.InitModel(t)
	// Remote deletion must work without enabling local user handling or opening its database.
	cfg.App.UserHandling = false
	cfg.Server.Database = "/does/not/exist/hister.db"
	localUser := testutil.CreateUser(t, "alice")
	if deleteUserCmd.Annotations[executionScopeAnnotation] != string(executionScopeRemote) {
		t.Error("user delete is not classified as remote")
	}
	for _, tc := range []struct {
		name, body, wantError string
		status                int
		lookup, transport     bool
	}{
		{name: "empty account", body: `{"id":77,"username":"alice","deleted_documents":0}`, status: http.StatusOK},
		{name: "lookup denied", body: "denied", status: http.StatusForbidden, lookup: true, wantError: "access denied"},
		{name: "lookup unsupported", body: "not found", status: http.StatusNotFound, lookup: true, wantError: "not found"},
		{name: "missing account", body: `{"users":[]}`, status: http.StatusOK, lookup: true, wantError: "not found"},
		{name: "wrong account", body: `{"users":[{"id":77,"username":"bob"}]}`, status: http.StatusOK, lookup: true, wantError: "invalid user lookup response"},
		{name: "denied", body: "permission denied", status: http.StatusForbidden, wantError: "access denied"},
		{name: "missing endpoint", body: "not found", status: http.StatusNotFound, wantError: "not found"},
		{name: "unsupported method", body: "method not allowed", status: http.StatusMethodNotAllowed, wantError: "method not allowed"},
		{name: "purge required", body: `{"code":"purge_required","error":"enable purge"}`, status: http.StatusConflict, wantError: "purge_required"},
		{name: "last administrator", body: `{"code":"last_admin","error":"cannot remove the last administrator"}`, status: http.StatusConflict, wantError: "last_admin"},
		{name: "failed purge", body: "internal server error", status: http.StatusInternalServerError, wantError: "server error"},
		{name: "empty response", body: `{}`, status: http.StatusOK, wantError: "invalid user deletion response"},
		{name: "connection lost during lookup", lookup: true, transport: true, wantError: "connection lost"},
		{name: "connection lost during deletion", transport: true, wantError: "connection lost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					if req.Method != http.MethodGet || req.URL.Path != "/api/users" {
						t.Errorf("unexpected lookup: %s %s", req.Method, req.URL)
					}
					if !tc.lookup {
						return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"users":[{"id":77,"username":"alice"}]}`))}, nil
					}
				} else if calls != 2 || req.Method != http.MethodDelete || req.URL.Path != "/api/users/77" || req.URL.RawQuery != "" || req.Body != nil {
					t.Errorf("unexpected deletion without purge: %s %s", req.Method, req.URL)
				}
				if tc.transport {
					return nil, errors.New("connection lost")
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})
			cmd := newDeleteUserTestCommand()
			cmd.SetArgs([]string{"alice"})
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); (tc.wantError == "" && err != nil) || (tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError))) {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
			wantCalls := 2
			if tc.lookup {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Errorf("requests = %d, want %d", calls, wantCalls)
			}
			if tc.wantError != "" && output.Len() != 0 {
				t.Errorf("reported success after failure: %s", &output)
			}
			if tc.wantError == "" && !strings.Contains(ansi.Strip(output.String()), "User deleted: alice") {
				t.Errorf("missing success output: %s", &output)
			}
			if _, err := model.GetUserByID(localUser.ID); err != nil {
				t.Errorf("remote command changed the local account: %v", err)
			}
		})
	}
}

func newDeleteUserTestCommand() *cobra.Command {
	command := &cobra.Command{Use: deleteUserCmd.Use, Args: deleteUserCmd.Args, PreRun: deleteUserCmd.PreRun, PreRunE: deleteUserCmd.PreRunE, RunE: deleteUserCmd.RunE, SilenceUsage: true, SilenceErrors: true}
	command.Flags().Bool("purge", false, "")
	return command
}

func TestDeleteUserInvalidNameDoesNotContactServer(t *testing.T) {
	previousConfig, previousTransport := cfg, http.DefaultTransport
	t.Cleanup(func() { cfg, http.DefaultTransport = previousConfig, previousTransport })
	cfg = config.CreateDefaultConfig()
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Error("invalid username contacted the server")
		return nil, errors.New("unexpected request")
	})
	for _, name := range []string{"", " \t ", "ali\nce", "alice\x7f"} {
		t.Run(name, func(t *testing.T) {
			command := newDeleteUserTestCommand()
			command.SetArgs([]string{name, "--purge"})
			var output bytes.Buffer
			command.SetOut(&output)
			if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "username") || output.Len() != 0 {
				t.Fatalf("invalid name: error = %v, output = %q", err, output.String())
			}
		})
	}
}

func newShowUserTestCommand() *cobra.Command {
	cmd := &cobra.Command{Use: showUserCmd.Use, Args: showUserCmd.Args, Run: showUserCmd.Run, RunE: showUserCmd.RunE, SilenceUsage: true}
	cmd.Flags().Bool("show-token", false, "")
	cmd.SetErr(io.Discard)
	return cmd
}

func newCreateUserTestCommand() *cobra.Command {
	command := &cobra.Command{Use: createUserCmd.Use, Args: createUserCmd.Args, Run: createUserCmd.Run, RunE: createUserCmd.RunE, PreRunE: createUserCmd.PreRunE, SilenceUsage: true}
	command.Flags().Bool("admin", false, "")
	command.Flags().Bool("offline", false, "")
	command.SetErr(io.Discard)
	return command
}

func TestCreateUserUsesServer(t *testing.T) {
	previousConfig, previousDB, previousTransport, previousPrompt := cfg, model.DB, http.DefaultTransport, promptPassword
	t.Cleanup(func() {
		cfg, model.DB, http.DefaultTransport, promptPassword = previousConfig, previousDB, previousTransport, previousPrompt
	})
	cfg = testutil.InitModel(t)
	cfg.App.UserHandling = false
	cfg.App.AccessToken = "admin-token"
	if err := cfg.UpdateBaseURL("https://remote.example/hister"); err != nil {
		t.Fatal(err)
	}
	localDB := model.DB
	prompts := 0
	promptPassword = func(string) (string, error) {
		prompts++
		return "password123", nil
	}
	calls := 0
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodPost || req.URL.String() != "https://remote.example/hister/api/users" || req.Header.Get("X-Access-Token") != "admin-token" || req.Header.Get("Origin") != "hister://" {
			t.Errorf("unexpected creation request: %s %s", req.Method, req.URL)
		}
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload) != 3 || payload["username"] != "alice" || payload["password"] != "password123" || payload["is_admin"] != true {
			t.Error("creation request has incorrect account fields")
		}
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(`{"id":77,"username":"alice","is_admin":true}`))}, nil
	})
	command := newCreateUserTestCommand()
	command.SetArgs([]string{"alice", "--admin"})
	var output bytes.Buffer
	command.SetOut(&output)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || prompts != 2 {
		t.Errorf("requests = %d, password prompts = %d, want 1 and 2", calls, prompts)
	}
	if model.DB != localDB {
		t.Fatal("remote creation opened a local database")
	}
	if _, err := model.GetUser("alice"); err == nil {
		t.Error("remote creation added a local account")
	}
	if !strings.Contains(output.String(), "User created: alice") || strings.Contains(output.String(), "password123") || strings.Contains(output.String(), "admin-token") {
		t.Errorf("unexpected command output: %q", output.String())
	}
}

func TestCreateUserConnectionFlags(t *testing.T) {
	if createUserCmd.Annotations[executionScopeAnnotation] != string(executionScopeHybrid) {
		t.Error("user create is not classified as hybrid")
	}
	flags, _ := applicableGlobalFlags(createUserCmd)
	for _, name := range []string{"server-url", "token", "client-timeout"} {
		if !flags[name] {
			t.Errorf("connection flag %s is not applicable", name)
		}
	}
	if createUserCmd.PreRun != nil {
		t.Error("user create still unconditionally initializes the local database")
	}
	if flag := createUserCmd.Flags().Lookup("offline"); flag == nil || flag.Value.Type() != "bool" || flag.DefValue != "false" {
		t.Error("user create does not have an explicit --offline flag")
	}
}

func TestCreateUserWithoutLocalDatabase(t *testing.T) {
	previousConfig, previousDB, previousTransport, previousPrompt := cfg, model.DB, http.DefaultTransport, promptPassword
	t.Cleanup(func() {
		cfg, model.DB, http.DefaultTransport, promptPassword = previousConfig, previousDB, previousTransport, previousPrompt
	})
	cfg = config.CreateDefaultConfig()
	cfg.App.UserHandling = false
	cfg.Server.Database = "/does/not/exist/hister.db"
	model.DB = nil
	promptPassword = func(string) (string, error) { return "password123", nil }
	for _, tc := range []struct {
		name, body string
		status     int
		wantError  bool
	}{
		{"ordinary user", `{"id":77,"username":"alice","is_admin":false}`, http.StatusCreated, false},
		{"denied", "permission denied", http.StatusForbidden, true},
		{"old server", "not found", http.StatusNotFound, true},
		{"duplicate", `{"code":"username_exists","error":"username already exists"}`, http.StatusConflict, true},
		{"invalid response", `<html>old server</html>`, http.StatusOK, true},
		{"connection failure", "", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				var payload map[string]any
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if payload["is_admin"] != false {
					t.Error("creation requested administrator privileges by default")
				}
				if tc.status == 0 {
					return nil, io.ErrUnexpectedEOF
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			command := newCreateUserTestCommand()
			command.SetArgs([]string{"alice"})
			var output bytes.Buffer
			command.SetOut(&output)
			if err := command.Execute(); (err != nil) != tc.wantError {
				t.Fatalf("error = %v, want error = %t", err, tc.wantError)
			}
			if calls != 1 || model.DB != nil {
				t.Fatalf("requests = %d, opened local database = %t", calls, model.DB != nil)
			}
			if tc.wantError && output.Len() != 0 {
				t.Fatalf("reported success on failure: %s", &output)
			}
		})
	}
}

func TestCreateUserOffline(t *testing.T) {
	for _, isAdmin := range []bool{false, true} {
		t.Run(fmt.Sprintf("admin=%t", isAdmin), func(t *testing.T) {
			previousConfig, previousDB, previousTransport, previousPrompt := cfg, model.DB, http.DefaultTransport, promptPassword
			t.Cleanup(func() {
				cfg, model.DB, http.DefaultTransport, promptPassword = previousConfig, previousDB, previousTransport, previousPrompt
			})
			cfg = testutil.Config(t)
			cfg.App.UserHandling = true
			cfg.App.AccessToken = "remote-admin-token"
			model.DB = nil
			t.Cleanup(func() {
				if model.DB != nil {
					db, err := model.DB.DB()
					if err != nil {
						t.Error(err)
					} else if err := db.Close(); err != nil {
						t.Error(err)
					}
				}
			})
			promptPassword = func(string) (string, error) { return "password123", nil }
			http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Error("offline creation contacted the server")
				return nil, errors.New("unexpected request")
			})
			command := newCreateUserTestCommand()
			command.SetArgs([]string{"alice", "--offline", fmt.Sprintf("--admin=%t", isAdmin)})
			var output bytes.Buffer
			command.SetOut(&output)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if model.DB == nil {
				t.Fatal("offline creation did not open the database")
			}
			user, err := model.AuthenticateUser("alice", "password123")
			if err != nil || user.IsAdmin != isAdmin || user.Token == "" {
				t.Fatalf("offline account has incorrect credentials or privileges: %v", err)
			}
			if !strings.Contains(output.String(), "User created: alice") || strings.Contains(output.String(), user.Token) != isAdmin {
				t.Fatal("offline creation must print the token only for an administrator")
			}
			if strings.Contains(output.String(), "password123") || strings.Contains(output.String(), "remote-admin-token") {
				t.Fatal("offline output disclosed other credentials")
			}
		})
	}
}

func TestCreateUserInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name, username, password, confirmation, wantError string
		offline, disabled, invalidDB                      bool
		promptError                                       error
	}{
		{name: "blank username", username: " ", wantError: "username"},
		{name: "control character", username: "ali\nce", wantError: "username"},
		{name: "short password", username: "alice", password: "1234567", wantError: "password"},
		{name: "long password", username: "alice", password: strings.Repeat("é", 37), wantError: "password"},
		{name: "mismatch", username: "alice", password: "password123", confirmation: "different", wantError: "match"},
		{name: "cancelled", username: "alice", promptError: errors.New("cancelled"), wantError: "cancelled"},
		{name: "offline disabled", username: "alice", offline: true, disabled: true, wantError: "user_handling"},
		{name: "offline cancelled", username: "alice", offline: true, promptError: errors.New("cancelled"), wantError: "cancelled"},
		{name: "offline database unavailable", username: "alice", offline: true, invalidDB: true, password: "password123", confirmation: "password123", wantError: "database"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previousConfig, previousDB, previousTransport, previousPrompt := cfg, model.DB, http.DefaultTransport, promptPassword
			t.Cleanup(func() {
				cfg, model.DB, http.DefaultTransport, promptPassword = previousConfig, previousDB, previousTransport, previousPrompt
			})
			cfg = testutil.Config(t)
			cfg.App.UserHandling = !tc.disabled
			if tc.invalidDB {
				cfg.App.Directory = filepath.Join(cfg.App.Directory, "missing")
			}
			model.DB = nil
			prompts := 0
			promptPassword = func(string) (string, error) {
				prompts++
				if prompts == 1 {
					return tc.password, tc.promptError
				}
				return tc.confirmation, nil
			}
			http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Error("invalid input contacted the server")
				return nil, errors.New("unexpected request")
			})
			command := newCreateUserTestCommand()
			command.SetArgs([]string{tc.username, fmt.Sprintf("--offline=%t", tc.offline)})
			var output bytes.Buffer
			command.SetOut(&output)
			if err := command.Execute(); err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
			if !tc.invalidDB && model.DB != nil {
				t.Fatal("invalid input opened a local database")
			}
			if output.Len() != 0 {
				t.Fatalf("reported success on failure: %s", &output)
			}
		})
	}
}

func TestShowUserUsesRemoteAccount(t *testing.T) {
	for _, showToken := range []bool{false, true} {
		t.Run(fmt.Sprintf("show token=%t", showToken), func(t *testing.T) {
			previousConfig, previousDB, previousTransport := cfg, model.DB, http.DefaultTransport
			t.Cleanup(func() { cfg, model.DB, http.DefaultTransport = previousConfig, previousDB, previousTransport })
			cfg = testutil.InitModel(t)
			cfg.App.AccessToken = "admin-token"
			if err := cfg.UpdateBaseURL("https://remote.example/hister"); err != nil {
				t.Fatal(err)
			}
			localUser := testutil.CreateUser(t, "alice")
			const remoteUser = `{"id":77,"username":"alice","is_admin":true,"created_at":"2024-01-02T03:04:05Z","updated_at":"2025-06-07T08:09:10Z"}`
			var paths []string
			http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				paths = append(paths, req.URL.Path)
				if req.Method != http.MethodGet || req.URL.Host != "remote.example" || req.Header.Get("X-Access-Token") != "admin-token" {
					t.Errorf("unexpected request: %s %s", req.Method, req.URL)
				}
				body := ""
				switch req.URL.Path {
				case "/hister/api/users":
					if req.URL.Query().Get("username") != "alice" || req.URL.Query().Get("limit") != "1" {
						t.Errorf("unexpected lookup query: %s", req.URL.RawQuery)
					}
					body = `{"users":[` + remoteUser + `]}`
				case "/hister/api/users/77/token":
					if !showToken {
						t.Error("retrieved a token without --show-token")
					}
					body = `{"token":"remote-user-token"}`
				default:
					t.Errorf("unexpected request path: %s", req.URL.Path)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})
			cmd := newShowUserTestCommand()
			cmd.SetArgs([]string{"alice", fmt.Sprintf("--show-token=%t", showToken)})
			var output bytes.Buffer
			cmd.SetOut(&output)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			wantCalls := 1
			if showToken {
				wantCalls++
			}
			if len(paths) != wantCalls {
				t.Errorf("requests = %v, want %d", paths, wantCalls)
			}
			for _, want := range []string{
				"Username:   alice", "ID:         77", "Admin:      yes",
				"Created at: " + formatLocalTimestamp(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)),
				"Updated at: " + formatLocalTimestamp(time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)),
			} {
				if !strings.Contains(output.String(), want) {
					t.Errorf("output missing %q: %q", want, output.String())
				}
			}
			if strings.Contains(output.String(), "remote-user-token") != showToken || strings.Contains(output.String(), "Token:") != showToken {
				t.Error("token output did not follow --show-token")
			}
			if strings.Contains(output.String(), localUser.Token) || strings.Contains(output.String(), "admin-token") || strings.Contains(output.String(), "\x1b") {
				t.Error("output contains an unintended credential or terminal escape sequence")
			}
		})
	}
}

func TestShowUserConnectionFlags(t *testing.T) {
	if showUserCmd.Annotations[executionScopeAnnotation] != string(executionScopeRemote) {
		t.Error("user show is not classified as remote")
	}
	flags, _ := applicableGlobalFlags(showUserCmd)
	for _, name := range []string{"server-url", "token", "client-timeout"} {
		if !flags[name] {
			t.Errorf("connection flag %s is not applicable", name)
		}
	}
	if showUserCmd.PreRun != nil || showUserCmd.PreRunE != nil {
		t.Error("user show still initializes local user handling")
	}
	showToken := showUserCmd.Flags().Lookup("show-token")
	if showToken == nil || showToken.Value.Type() != "bool" {
		t.Fatal("--show-token is not registered as a boolean")
	}
	authToken := rootCmd.PersistentFlags().Lookup("token")
	if flag := showUserCmd.Flags().Lookup("token"); flag != nil && flag != authToken {
		t.Fatal("local --token shadows the global authentication flag")
	}
	oldToken, oldTokenChanged := authToken.Value.String(), authToken.Changed
	oldShowToken, oldShowTokenChanged := showToken.Value.String(), showToken.Changed
	t.Cleanup(func() {
		if err := authToken.Value.Set(oldToken); err != nil {
			t.Error(err)
		}
		if err := showToken.Value.Set(oldShowToken); err != nil {
			t.Error(err)
		}
		authToken.Changed, showToken.Changed = oldTokenChanged, oldShowTokenChanged
	})
	if err := showUserCmd.ParseFlags([]string{"alice", "--token", "admin-token", "--show-token"}); err != nil {
		t.Fatal(err)
	}
	if authToken.Value.String() != "admin-token" || showToken.Value.String() != "true" {
		t.Error("authentication and token display flags were not parsed independently")
	}
}

func TestShowUserWithoutLocalDatabase(t *testing.T) {
	previousConfig, previousDB, previousTransport := cfg, model.DB, http.DefaultTransport
	t.Cleanup(func() { cfg, model.DB, http.DefaultTransport = previousConfig, previousDB, previousTransport })
	cfg = config.CreateDefaultConfig()
	cfg.App.UserHandling = false
	cfg.Server.Database = "/does/not/exist/hister.db"
	model.DB = nil
	const metadata = `{"users":[{"id":77,"username":"alice","is_admin":false}]}`
	for _, tc := range []struct {
		name, response          string
		status                  int
		wantError, tokenRequest bool
	}{
		{"remote account", metadata, http.StatusOK, false, false},
		{"denied", "permission denied", http.StatusForbidden, true, false},
		{"old server", "not found", http.StatusNotFound, true, false},
		{"server failure", "unavailable", http.StatusInternalServerError, true, false},
		{"missing user", `{"users":[]}`, http.StatusOK, true, false},
		{"invalid response", "<html>old server</html>", http.StatusOK, true, false},
		{"token denied", "permission denied", http.StatusForbidden, true, true},
		{"account deleted after lookup", "not found", http.StatusNotFound, true, true},
		{"invalid token response", `{}`, http.StatusOK, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if tc.tokenRequest && calls == 1 {
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(metadata))}, nil
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.response))}, nil
			})
			cmd := newShowUserTestCommand()
			cmd.SetArgs([]string{"alice", fmt.Sprintf("--show-token=%t", tc.tokenRequest)})
			var output bytes.Buffer
			cmd.SetOut(&output)
			if err := cmd.Execute(); (err != nil) != tc.wantError {
				t.Fatalf("error = %v, want error = %t", err, tc.wantError)
			}
			wantCalls := 1
			if tc.tokenRequest {
				wantCalls++
			}
			if calls != wantCalls || model.DB != nil {
				t.Errorf("requests = %d, local database opened = %t", calls, model.DB != nil)
			}
			if tc.wantError && output.Len() != 0 {
				t.Errorf("partial output on failure: %s", &output)
			}
			if !tc.wantError && !strings.Contains(output.String(), "Admin:      no") {
				t.Errorf("remote account output missing: %s", &output)
			}
		})
	}
}

func TestInitialAnalyzerFingerprint(t *testing.T) {
	tests := []struct {
		name            string
		indexerVersion  int
		detectLanguages bool
		keepStopwords   bool
		want            string
	}{
		{
			name:            "fresh index uses active configuration",
			indexerVersion:  -1,
			detectLanguages: true,
			keepStopwords:   true,
			want:            indexer.AnalyzerFingerprint(true, true),
		},
		{
			name:            "upgraded index with defaults matches active configuration",
			indexerVersion:  indexer.Version,
			detectLanguages: true,
			keepStopwords:   false,
			want:            indexer.AnalyzerFingerprint(true, false),
		},
		{
			name:            "upgraded index uses legacy configuration",
			indexerVersion:  indexer.Version,
			detectLanguages: true,
			keepStopwords:   true,
			want:            indexer.AnalyzerFingerprint(true, false),
		},
		{
			name:            "upgraded index retains disabled language detection",
			indexerVersion:  indexer.Version,
			detectLanguages: false,
			keepStopwords:   true,
			want:            indexer.AnalyzerFingerprint(false, false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := initialAnalyzerFingerprint(tt.indexerVersion, tt.detectLanguages, tt.keepStopwords)
			if got != tt.want {
				t.Fatalf("initialAnalyzerFingerprint() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEmbeddingConfigWarning(t *testing.T) {
	tests := []struct {
		name   string
		stored string
		active string
		want   string
	}{
		{
			name:   "matching configuration",
			stored: "same",
			active: "same",
		},
		{
			name:   "semantic search disabled",
			stored: "stored",
		},
		{
			name:   "legacy indexed configuration",
			active: "active",
		},
		{
			name:   "changed configuration",
			stored: "stored",
			active: "active",
			want:   "The semantic search embedding configuration differs from the indexed configuration. Run `nogon reindex` to update your embeddings.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := embeddingConfigWarning(tt.stored, tt.active); got != tt.want {
				t.Fatalf("embeddingConfigWarning() = %q, want %q", got, tt.want)
			}
		})
	}
}

func newUpdateUserTestCommand() *cobra.Command {
	command := &cobra.Command{Use: updateUserCmd.Use, Args: updateUserCmd.Args, Run: updateUserCmd.Run, RunE: updateUserCmd.RunE, SilenceUsage: true, SilenceErrors: true}
	command.Flags().String("username", "", "")
	command.Flags().Bool("password", false, "")
	command.Flags().Bool("regen-token", false, "")
	command.Flags().Bool("toggle-admin", false, "")
	return command
}

func TestUpdateUserUsesRemoteAccount(t *testing.T) {
	previousConfig, previousDB, previousTransport, previousPrompt := cfg, model.DB, http.DefaultTransport, promptPassword
	t.Cleanup(func() {
		cfg, model.DB, http.DefaultTransport, promptPassword = previousConfig, previousDB, previousTransport, previousPrompt
	})
	cfg = testutil.InitModel(t)
	cfg.App.UserHandling = false
	cfg.App.AccessToken = "admin-token"
	if err := cfg.UpdateBaseURL("https://remote.example/hister"); err != nil {
		t.Fatal(err)
	}
	local := testutil.CreateUser(t, "alice")
	localDB := model.DB
	prompts, calls := 0, 0
	promptPassword = func(string) (string, error) {
		prompts++
		if calls != 1 {
			t.Errorf("password prompt must follow the remote lookup, requests = %d", calls)
		}
		return "replacement123", nil
	}
	var output bytes.Buffer
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Host != "remote.example" || req.Header.Get("X-Access-Token") != "admin-token" || req.Header.Get("Origin") != "hister://" {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL)
		}
		body := `{"users":[{"id":77,"username":"alice","is_admin":true,"revision":4}]}`
		if calls == 1 {
			if req.Method != http.MethodGet || req.URL.Path != "/hister/api/users" || req.URL.Query().Get("username") != "alice" {
				t.Errorf("unexpected lookup: %s %s", req.Method, req.URL)
			}
		} else {
			if calls != 2 || req.Method != http.MethodPatch || req.URL.Path != "/hister/api/users/77" {
				t.Errorf("unexpected update: %s %s", req.Method, req.URL)
			}
			var fields map[string]any
			if err := json.NewDecoder(req.Body).Decode(&fields); err != nil {
				t.Fatal(err)
			}
			if len(fields) != 5 || fields["revision"] != float64(4) || fields["username"] != "renamed" || fields["password"] != "replacement123" || fields["is_admin"] != false || fields["regenerate_token"] != true {
				t.Errorf("incorrect combined update fields: %v", fields)
			}
			if output.Len() != 0 || prompts != 2 {
				t.Errorf("output before success = %q, password prompts = %d", output.String(), prompts)
			}
			body = `{"id":77,"username":"renamed","is_admin":false,"revision":5,"token":"new-user-token"}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	command := newUpdateUserTestCommand()
	command.SetOut(&output)
	command.SetArgs([]string{"alice", "--username", "renamed", "--password", "--regen-token", "--toggle-admin"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || prompts != 2 {
		t.Errorf("requests = %d, password prompts = %d", calls, prompts)
	}
	current, err := model.GetUserByID(local.ID)
	if err != nil || *current != *local || model.DB != localDB {
		t.Fatalf("remote update changed local data: %v", err)
	}
	text := ansi.Strip(output.String())
	for _, want := range []string{"Username changed: alice", "renamed", "Password changed for renamed", "Admin disabled for renamed", "new-user-token"} {
		if !strings.Contains(text, want) {
			t.Errorf("output = %q, missing %q", text, want)
		}
	}
	if strings.Contains(text, "replacement123") || strings.Contains(text, "admin-token") || strings.Contains(text, local.Token) {
		t.Fatal("output disclosed unrelated credentials")
	}
}

func TestUpdateUserConnectionFlags(t *testing.T) {
	if updateUserCmd.Annotations[executionScopeAnnotation] != string(executionScopeRemote) {
		t.Error("user update is not classified as remote")
	}
	flags, _ := applicableGlobalFlags(updateUserCmd)
	for _, name := range []string{"server-url", "token", "client-timeout"} {
		if !flags[name] {
			t.Errorf("connection flag %q is not applicable", name)
		}
	}
	if updateUserCmd.PreRun != nil || updateUserCmd.PreRunE != nil {
		t.Error("user update still initializes local user handling")
	}
}

func TestUpdateUserRemoteErrorsDoNotFallBack(t *testing.T) {
	for _, tc := range []struct {
		name, response, wantError string
		status                    int
		lookup                    bool
		transportError            bool
	}{
		{name: "lookup denied", response: "denied", status: 403, lookup: true, wantError: "access denied"},
		{name: "lookup unsupported", response: "not found", status: 404, lookup: true, wantError: "not found"},
		{name: "missing account", response: `{"users":[]}`, status: 200, lookup: true, wantError: "not found"},
		{name: "missing revision", response: `{"users":[{"id":77,"username":"alice","is_admin":true}]}`, status: 200, lookup: true, wantError: "upgrade"},
		{name: "stale revision", response: `{"code":"stale_revision","error":"user has changed"}`, status: 409, wantError: "stale_revision"},
		{name: "last administrator", response: `{"code":"last_admin","error":"cannot remove the last administrator"}`, status: 409, wantError: "last_admin"},
		{name: "duplicate username", response: `{"code":"username_exists","error":"username already exists"}`, status: 409, wantError: "username_exists"},
		{name: "update denied", response: "denied", status: 403, wantError: "access denied"},
		{name: "update unsupported", response: "not found", status: 404, wantError: "not found"},
		{name: "server error", response: "unavailable", status: 500, wantError: "server error"},
		{name: "invalid update response", response: `{}`, status: 200, wantError: "invalid user update response"},
		{name: "connection lost", transportError: true, wantError: "connection lost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previousConfig, previousDB, previousTransport, previousPrompt := cfg, model.DB, http.DefaultTransport, promptPassword
			t.Cleanup(func() {
				cfg, model.DB, http.DefaultTransport, promptPassword = previousConfig, previousDB, previousTransport, previousPrompt
			})
			cfg = config.CreateDefaultConfig()
			cfg.App.UserHandling = false
			cfg.Server.Database = "/does/not/exist/hister.db"
			model.DB = nil
			calls, prompts := 0, 0
			promptPassword = func(string) (string, error) { prompts++; return "replacement123", nil }
			http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if !tc.lookup && calls == 1 {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"users":[{"id":77,"username":"alice","is_admin":true,"revision":4}]}`))}, nil
				}
				if tc.transportError {
					return nil, errors.New("connection lost")
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.response))}, nil
			})
			command := newUpdateUserTestCommand()
			command.SetArgs([]string{"alice", "--username", "renamed", "--password", "--regen-token", "--toggle-admin"})
			var output bytes.Buffer
			command.SetOut(&output)
			if err := command.Execute(); err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
			wantCalls, wantPrompts := 2, 2
			if tc.lookup {
				wantCalls, wantPrompts = 1, 0
			}
			if calls != wantCalls || prompts != wantPrompts || output.Len() != 0 || model.DB != nil {
				t.Fatalf("requests = %d, prompts = %d, output = %q, opened database = %t", calls, prompts, output.String(), model.DB != nil)
			}
		})
	}
}

func TestUpdateUserInvalidInputDoesNotApplyChanges(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		args                              []string
		password, confirmation, wantError string
		lookup                            bool
		promptError                       error
	}{
		{name: "no changes", args: []string{"alice"}, wantError: "no changes"},
		{name: "disabled changes", args: []string{"alice", "--password=false", "--toggle-admin=false", "--regen-token=false"}, wantError: "no changes"},
		{name: "blank account", args: []string{" ", "--password"}, wantError: "username"},
		{name: "empty new name", args: []string{"alice", "--username="}, wantError: "new username"},
		{name: "blank new name", args: []string{"alice", "--username", " \t "}, wantError: "new username"},
		{name: "control in name", args: []string{"alice", "--username", "ali\nce"}, wantError: "new username"},
		{name: "short password", password: "1234567", lookup: true, wantError: "password"},
		{name: "long password", password: strings.Repeat("é", 37), lookup: true, wantError: "password"},
		{name: "mismatched password", password: "replacement123", confirmation: "different123", lookup: true, wantError: "match"},
		{name: "cancelled password", promptError: errors.New("cancelled"), lookup: true, wantError: "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previousConfig, previousDB, previousTransport, previousPrompt := cfg, model.DB, http.DefaultTransport, promptPassword
			t.Cleanup(func() {
				cfg, model.DB, http.DefaultTransport, promptPassword = previousConfig, previousDB, previousTransport, previousPrompt
			})
			cfg = config.CreateDefaultConfig()
			model.DB = nil
			calls, prompts := 0, 0
			promptPassword = func(string) (string, error) {
				prompts++
				if prompts == 1 {
					return tc.password, tc.promptError
				}
				return tc.confirmation, nil
			}
			http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodGet {
					t.Error("invalid input sent an update")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"users":[{"id":77,"username":"alice","is_admin":true,"revision":4}]}`))}, nil
			})
			command := newUpdateUserTestCommand()
			args := tc.args
			if tc.lookup {
				args = []string{"alice", "--username", "renamed", "--password", "--toggle-admin", "--regen-token"}
			}
			command.SetArgs(args)
			var output bytes.Buffer
			command.SetOut(&output)
			if err := command.Execute(); err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
			wantCalls := 0
			if tc.lookup {
				wantCalls = 1
			}
			if calls != wantCalls || (!tc.lookup && prompts != 0) || output.Len() != 0 || model.DB != nil {
				t.Fatalf("requests = %d, prompts = %d, output = %q, opened database = %t", calls, prompts, output.String(), model.DB != nil)
			}
		})
	}
}

func TestUpdateUserToggleUsesRemotePrivileges(t *testing.T) {
	previousConfig, previousDB, previousTransport := cfg, model.DB, http.DefaultTransport
	t.Cleanup(func() { cfg, model.DB, http.DefaultTransport = previousConfig, previousDB, previousTransport })
	cfg = config.CreateDefaultConfig()
	model.DB = nil
	calls := 0
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		body := `{"users":[{"id":77,"username":"alice","is_admin":false,"revision":4}]}`
		if req.Method == http.MethodPatch {
			var fields map[string]any
			if err := json.NewDecoder(req.Body).Decode(&fields); err != nil {
				t.Fatal(err)
			}
			if len(fields) != 2 || fields["revision"] != float64(4) || fields["is_admin"] != true {
				t.Errorf("toggle changed omitted fields: %v", fields)
			}
			body = `{"id":77,"username":"alice","is_admin":true,"revision":5}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	command := newUpdateUserTestCommand()
	command.SetArgs([]string{"alice", "--toggle-admin"})
	var output bytes.Buffer
	command.SetOut(&output)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || model.DB != nil || !strings.Contains(ansi.Strip(output.String()), "Admin enabled for alice") || strings.Contains(strings.ToLower(output.String()), "token") {
		t.Fatalf("requests = %d, output = %q, opened database = %t", calls, output.String(), model.DB != nil)
	}
}

func TestCommandBranding(t *testing.T) {
	for _, tc := range []struct{ argument, want string }{
		{"--help", "Nogon, your own search engine"},
		{"--version", "nogon version " + Version},
	} {
		t.Run(tc.argument, func(t *testing.T) {
			out, _, err := executeUserCommandForTest(t, tc.argument)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("output = %q, want %q", out, tc.want)
			}
		})
	}
	if !strings.Contains(UserAgent, "Nogon/"+Version) {
		t.Fatalf("crawler user agent = %q", UserAgent)
	}
}
