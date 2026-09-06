package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"

	"github.com/asciimoo/hister/server/types"
)

func executeInspection(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, _, err := executeInspectionWithStderr(t, args...)
	return out, err
}

func executeInspectionWithStderr(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	for _, flags := range []*pflag.FlagSet{rootCmd.PersistentFlags(), doctorCmd.Flags()} {
		flags.VisitAll(func(flag *pflag.Flag) {
			value, changed := flag.Value.String(), flag.Changed
			t.Cleanup(func() {
				if err := flag.Value.Set(value); err != nil {
					t.Error(err)
				}
				flag.Changed = changed
			})
		})
	}
	var out, stderr bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	err := rootCmd.Execute()
	return out.String(), stderr.String(), err
}

func TestConfigCommandsAvoidRuntimeInitialization(t *testing.T) {
	for _, command := range []string{"path", "show", "validate"} {
		t.Run(command, func(t *testing.T) {
			dir := t.TempDir()
			filename := filepath.Join(dir, "config.yml")
			content := fmt.Sprintf("app:\n  directory: %q\n  log_file: %q\n  public: true\nserver:\n  address: 0.0.0.0:4433\n", filepath.Join(dir, "data"), filepath.Join(dir, "hister.log"))
			if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HISTER_CONFIG", filename)
			output, err := executeInspection(t, "config", command, "--token", "private-credential", "--server-url", "https://example.com/hister/")
			if err != nil {
				t.Fatal(err)
			}
			if command == "show" {
				var values map[string]any
				if err := yaml.Unmarshal([]byte(output), &values); err != nil {
					t.Fatal(err)
				}
				server := values["server"].(map[string]any)
				if server["base_url"] != "https://example.com/hister" || strings.Contains(output, "private-credential") {
					t.Fatalf("incorrect override or credential redaction: %s", output)
				}
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("runtime initialization created files: %v, err=%v", entries, err)
			}
		})
	}
}

func TestDoctorReportsFailuresAndClosesJSON(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		authenticated, available bool
		status                   int
		body                     string
		want                     string
		wantExit                 int
	}{
		{"healthy", true, true, 200, `[{"name":"index.version","status":"ok","message":"Matches"}]`, "server.index.version", 0},
		{"bad token", false, true, 200, "", "Server requires a valid token", 1},
		{"old server", true, false, 200, "", "Server does not support diagnostics", 0},
		{"index mismatch", true, true, 200, `[{"name":"index.analyzer","status":"error","message":"Run hister reindex"}]`, "Run hister reindex", 1},
		{"admin required", true, true, 403, "private-credential", "admin token", 1},
		{"bad server response", true, true, 200, "private-credential", "Cannot read a Hister response", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Access-Token") != "private-credential" {
					t.Error("token was not sent")
				}
				if r.URL.Path == "/hister/api/config" {
					if _, err := fmt.Fprintf(w, `{"authMode":"token","authenticated":%t,"diagnosticsAvailable":%t}`, tc.authenticated, tc.available); err != nil {
						t.Errorf("write server config response: %v", err)
					}
					return
				}
				if r.URL.Path != "/hister/api/diagnostics" || !tc.available || !tc.authenticated {
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Errorf("write diagnostics response: %v", err)
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			filename := filepath.Join(dir, "config.yml")
			if err := os.WriteFile(filename, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HISTER_CONFIG", filename)
			t.Setenv("HISTER_DATA_DIR", filepath.Join(dir, "data"))
			output, err := executeInspection(t, "doctor", "--format", "json", "--server-url", server.URL+"/hister", "--token", "private-credential")
			if ExitCode(err) != tc.wantExit {
				t.Fatalf("error=%v output=%s", err, output)
			}
			var checks []types.DiagnosticCheck
			if err := json.Unmarshal([]byte(output), &checks); err != nil {
				t.Fatalf("invalid JSON: %v; output=%s", err, output)
			}
			if !strings.Contains(output, tc.want) || strings.Contains(output, "private-credential") {
				t.Fatalf("incorrect diagnostic output: %s", output)
			}
			if _, err := os.Stat(filepath.Join(dir, "data")); !os.IsNotExist(err) {
				t.Fatalf("doctor created data directory: %v", err)
			}
		})
	}
}
