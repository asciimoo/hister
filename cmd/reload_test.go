package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asciimoo/hister/config"

	"github.com/spf13/cobra"
)

func TestReloadCommand(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusBadRequest} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/hister/api/reload" || r.Header.Get("X-Access-Token") != "secret" || r.Header.Get("Origin") != "hister://" {
					t.Errorf("unexpected reload request: %s %s %v", r.Method, r.URL, r.Header)
				}
				w.WriteHeader(status)
				if status != http.StatusOK {
					_, _ = w.Write([]byte("restart required: server.address"))
				}
			}))
			defer srv.Close()
			dir := t.TempDir()
			filename := filepath.Join(dir, "client.yml")
			if err := os.WriteFile(filename, []byte("app:\n  title: Client\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HISTER_DATA_DIR", filepath.Join(dir, "unused-data"))
			out, err := executeInspection(t, "reload", "--config", filename, "--server-url", srv.URL+"/hister", "--token", "secret")
			if calls != 1 {
				t.Fatalf("reload requests = %d, want 1; error = %v", calls, err)
			}
			if status == http.StatusOK {
				if err != nil || !strings.Contains(out, "reloaded") {
					t.Fatalf("output = %q, error = %v", out, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "server.address") {
				t.Fatalf("reload error = %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "unused-data")); !os.IsNotExist(err) {
				t.Fatalf("reload initialized local runtime files: %v", err)
			}
		})
	}
}

func TestReloadCommandHelp(t *testing.T) {
	out, err := executeInspection(t, "reload", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Remote. Uses the configured Hister HTTP server", "--client-timeout", "--server-url", "--token"} {
		if !strings.Contains(out, want) {
			t.Errorf("reload help missing %q", want)
		}
	}
}

func TestListenConfigOverridesPreservedForReload(t *testing.T) {
	for name, value := range map[string]string{"log-level": "debug", "search-url": "https://search.example/?q={query}", "server-url": "https://hister.example/base", "token": "startup-token", "address": "127.0.0.1:5544", "public": "true"} {
		flags := rootCmd.PersistentFlags()
		if name == "address" || name == "public" {
			flags = listenCmd.Flags()
		}
		flag := flags.Lookup(name)
		oldValue, oldChanged := flag.Value.String(), flag.Changed
		t.Cleanup(func() {
			_ = flag.Value.Set(oldValue)
			flag.Changed = oldChanged
		})
		if err := flags.Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	override := listenConfigOverrides(listenCmd)
	// Captured overrides keep startup values even if a flag variable later changes.
	if err := rootCmd.PersistentFlags().Set("token", "later-token"); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(filename, []byte("# Defaults\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := config.LoadForInspection(filename, true, override...)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.App.AccessToken != "startup-token" || !c.App.Public || c.App.LogLevel != "debug" || c.App.SearchURL != "https://search.example/?q={query}" || c.Server.Address != "127.0.0.1:5544" || c.BaseURL("") != "https://hister.example/base" {
		t.Fatalf("startup overrides not preserved: %+v", c)
	}
}

func TestListenExplicitFalseOverridesFile(t *testing.T) {
	flag := listenCmd.Flags().Lookup("public")
	previousValue, previousChanged := flag.Value.String(), flag.Changed
	t.Cleanup(func() {
		_ = flag.Value.Set(previousValue)
		flag.Changed = previousChanged
	})
	if err := listenCmd.Flags().Set("public", "false"); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(filename, []byte("app:\n  public: true\n  access_token: file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	override := listenConfigOverrides(listenCmd)
	c, _, err := config.LoadForInspection(filename, true, override...)
	if err != nil {
		t.Fatal(err)
	}
	if c.App.Public {
		t.Fatal("--public=false did not override the file")
	}
	if source := c.Source("app.public"); source.Kind != "flag" || source.Name != "--public" || !source.Locked {
		t.Fatalf("explicit false source = %+v", source)
	}
}

func TestListenDefaultFlagsDoNotLockSettings(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.yml")
	t.Setenv("HISTER_DATA_DIR", filepath.Dir(filename))
	if err := os.WriteFile(filename, []byte("# Defaults\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := config.LoadForInspection(filename, true, listenConfigOverrides(listenCmd)...)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"app.log_level", "app.search_url", "app.access_token", "app.public", "server.address", "server.base_url"} {
		if source := c.Source(path); source.Kind != "default" || source.Locked {
			t.Errorf("default flag pinned %s: %+v", path, source)
		}
	}
}

func TestStartupRetainsEmptySettingFallbacks(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, []byte("app:\n  search_url: ''\n  log_level: ''\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HISTER_DATA_DIR", filepath.Join(dir, "data"))
	c, err := config.Load(filename, globalConfigOverrides()...)
	if err != nil {
		t.Fatal(err)
	}
	defaults := config.CreateDefaultConfig()
	if c.App.SearchURL != defaults.App.SearchURL || c.App.LogLevel != defaults.App.LogLevel {
		t.Fatal("startup no longer fills empty search URL and log level settings")
	}
	for _, path := range []string{"app.search_url", "app.log_level"} {
		if source := c.Source(path); source.Kind != "file" || !source.Locked {
			t.Errorf("normalizing %s lost its explicit source: %+v", path, source)
		}
	}
}

func TestListenStartupAndReloadUseSameOverrides(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, []byte("app:\n  public: true\n  user_handling: true\n  access_token: file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HISTER_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("HISTER_PORT", "6655")
	oldRun, oldConfig := listenCmd.RunE, cfg
	t.Cleanup(func() { listenCmd.RunE, cfg = oldRun, oldConfig })
	listenCmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if cfg.App.Public || cfg.App.AccessToken != "" || cfg.Server.Address != "127.0.0.1:7788" {
			t.Fatal("startup did not apply explicit false, empty, and address flags")
		}
		next, err := cfg.Reload(listenConfigOverrides(cmd)...)
		if err != nil {
			return err
		}
		for path, flag := range map[string]string{
			"app.public": "--public", "app.access_token": "--token", "server.address": "--address",
			"server.base_url": "--server-url", "app.log_level": "--log-level", "app.search_url": "--search-url",
		} {
			for _, candidate := range []*config.Config{cfg, next} {
				if source := candidate.Source(path); source.Kind != "flag" || source.Name != flag || !source.Locked {
					t.Errorf("%s source = %+v, want %s", path, source, flag)
				}
			}
		}
		if next.App != cfg.App || next.Server.Address != cfg.Server.Address || next.BaseURL("") != cfg.BaseURL("") {
			t.Fatal("reload changed the startup override values")
		}
		return nil
	}
	_, _, err := executeUserCommandForTest(t, "listen", "--config", filename, "--public=false", "--token=",
		"--address", "127.0.0.1:7788", "--server-url", "http://127.0.0.1:7788/base", "--log-level", "info",
		"--search-url", "https://search.example/?q={query}")
	if err != nil {
		t.Fatal(err)
	}
}
