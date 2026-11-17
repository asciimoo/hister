package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/logging"
)

func TestInitializeLogEnvironmentAliases(t *testing.T) {
	for _, tc := range []struct {
		name, nogon, hister string
		setNogon, debug     bool
	}{
		{name: "default"},
		{name: "legacy debug", hister: "debug", debug: true},
		{name: "Nogon debug", nogon: "debug", setNogon: true, debug: true},
		{name: "Nogon debug overrides legacy info", nogon: "debug", hister: "info", setNogon: true, debug: true},
		{name: "Nogon info overrides legacy debug", nogon: "info", hister: "debug", setNogon: true},
		{name: "empty Nogon suppresses legacy debug", hister: "debug", setNogon: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			filename := filepath.Join(dir, "config.yml")
			if err := os.WriteFile(filename, []byte("app:\n  log_level: info\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("NOGON_CONFIG", filename)
			t.Setenv("NOGON_DATA_DIR", filepath.Join(dir, "data"))
			t.Setenv("NOGON__APP__LOG_FILE", "")
			t.Setenv("NOGON__APP__LOG_FORMAT", "json")
			t.Setenv("NOGON__APP__ACCESS_TOKEN", "startup-test-secret")
			t.Setenv("HISTER__APP__LOG_LEVEL", tc.hister)
			t.Setenv("NOGON__APP__LOG_LEVEL", tc.nogon)
			if !tc.setNogon {
				if err := os.Unsetenv("NOGON__APP__LOG_LEVEL"); err != nil {
					t.Fatal(err)
				}
			}
			previous, logger, manager, level, userAgent := cfg, log.Logger, logging.Default, zerolog.GlobalLevel(), UserAgent
			var output bytes.Buffer
			logging.Default = logging.New(&output)
			log.Logger = zerolog.New(logging.Default)
			t.Cleanup(func() {
				if err := logging.Default.Close(); err != nil {
					t.Error(err)
				}
				cfg, log.Logger, logging.Default, UserAgent = previous, logger, manager, userAgent
				zerolog.SetGlobalLevel(level)
			})
			initialize(&cobra.Command{})
			gotDebug := strings.Contains(output.String(), "Loaded configuration from environment variable")
			if gotDebug != tc.debug {
				t.Errorf("configuration loading debug output = %v, want %v: %s", gotDebug, tc.debug, output.String())
			}
			if strings.Contains(output.String(), "startup-test-secret") {
				t.Error("startup logging disclosed the access token")
			}
		})
	}
}

func TestInitLogUsesReloadableOutput(t *testing.T) {
	dir := t.TempDir()
	previous, level := cfg, zerolog.GlobalLevel()
	t.Cleanup(func() {
		_ = logging.Default.Close()
		cfg = previous
		zerolog.SetGlobalLevel(level)
	})
	cfg = config.CreateDefaultConfig()
	cfg.App.LogFile = filepath.Join(dir, "startup.log")
	cfg.App.LogFormat = "json"
	initLog()
	derived := log.With().Str("component", "existing").Logger()
	derived.Info().Msg("startup-output")
	updated := cfg.App
	updated.LogFile = filepath.Join(dir, "reloaded.log")
	change, err := logging.Default.Prepare(updated)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := change.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := change.Apply(); err != nil {
		t.Fatal(err)
	}
	derived.Info().Msg("reloaded-output")
	for path, want := range map[string]string{cfg.App.LogFile: "startup-output", updated.LogFile: "reloaded-output"} {
		raw, err := os.ReadFile(path)
		if err != nil || !json.Valid(raw) || !strings.Contains(string(raw), want) {
			t.Fatalf("incorrect log output in %s: %s (%v)", path, raw, err)
		}
	}
}
