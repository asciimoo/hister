package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/asciimoo/hister/config"

	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

func TestNogonConfigSelection(t *testing.T) {
	for _, command := range []string{"path", "show", "validate"} {
		for _, selection := range []string{"nogon", "missing", "invalid", "empty", "flag"} {
			t.Run(command+"/"+selection, func(t *testing.T) {
				dir := t.TempDir()
				legacy := filepath.Join(dir, "legacy.yml")
				current := filepath.Join(dir, "nogon.yml")
				for _, filename := range []string{legacy, current} {
					content := fmt.Sprintf("app:\n  title: %q\n", filepath.Base(filename))
					if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				t.Setenv("HISTER_CONFIG", legacy)
				t.Setenv("NOGON_DATA_DIR", filepath.Join(dir, "data"))
				switch selection {
				case "missing":
					current = filepath.Join(dir, "missing.yml")
				case "invalid":
					if err := os.WriteFile(current, []byte("["), 0o600); err != nil {
						t.Fatal(err)
					}
				case "empty":
					current = ""
				}
				t.Setenv("NOGON_CONFIG", current)
				args := []string{"config", command}
				if selection == "flag" {
					args = append(args, "--config", legacy)
					current = legacy
				}
				out, err := executeInspection(t, args...)
				wantError := selection == "missing" || selection == "empty" || (selection == "invalid" && command != "path")
				if (err != nil) != wantError {
					t.Fatalf("error = %v, want error %v; output = %q", err, wantError, out)
				}
				if !wantError && !strings.Contains(out, filepath.Base(current)) {
					t.Fatalf("output did not select %q: %s", current, out)
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 2 {
					t.Fatalf("inspection created runtime files: %v, %v", entries, err)
				}
			})
		}
	}
}

func TestConfigFlagsCaptureRegisteredPathsGenerically(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("heading", "", "")
	bindConfigFlag(flags, "heading", "app.title")
	flags.Bool("suggest", false, "")
	bindConfigFlag(flags, "suggest", "app.show_suggestions")
	flags.Int("pause", 5, "")
	bindConfigFlag(flags, "pause", "crawler.delay")
	flags.StringSlice("langs", nil, "")
	bindConfigFlag(flags, "langs", "indexer.languages")
	flags.StringToString("headers", nil, "")
	bindConfigFlag(flags, "headers", "crawler.headers")
	flags.String("unchanged", "", "")
	bindConfigFlag(flags, "unchanged", "app.subtitle")
	flags.Bool("dry", false, "")
	// Subcommands parse inherited flags through a different FlagSet.
	inherited := pflag.NewFlagSet("subcommand", pflag.ContinueOnError)
	inherited.AddFlagSet(flags)
	if err := inherited.Parse([]string{"--heading=", "--suggest=false", "--pause=0", "--langs=en,de", "--headers=x-test=value", "--dry"}); err != nil {
		t.Fatal(err)
	}
	overrides := configFlagOverrides(flags, flags)
	if len(overrides) != 5 {
		t.Fatalf("captured %d settings, want five distinct explicitly bound flags", len(overrides))
	}
	if err := flags.Set("langs", "fr,hu"); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(filename, []byte("app:\n  title: File\n  show_suggestions: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := config.LoadForInspection(filename, true, overrides...)
	if err != nil {
		t.Fatal(err)
	}
	if c.App.Title != "" || c.App.ShowSuggestions || c.Crawler.Delay != 0 || !reflect.DeepEqual(c.Indexer.Languages, []string{"en", "de"}) || c.Crawler.Headers["x-test"] != "value" {
		t.Fatal("registered flags did not retain their captured typed values")
	}
	for path, name := range map[string]string{"app.title": "--heading", "app.show_suggestions": "--suggest", "crawler.delay": "--pause", "indexer.languages": "--langs", "crawler.headers": "--headers"} {
		if source := c.Source(path); source.Kind != "flag" || source.Name != name || !source.Locked {
			t.Errorf("%s source = %+v, want %s", path, source, name)
		}
	}
	if c.Source("app.subtitle").Locked {
		t.Fatal("an unchanged flag pinned its setting")
	}
}

func TestWriteDefaultConfigFileCreatesParentDirectories(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "nested", "config", "config.yml")
	content := []byte("app:\n  search_url: https://example.com\n")

	if err := writeDefaultConfigFile(filename, content); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("config content = %q, want %q", got, content)
	}
}

func TestWriteDefaultConfigFilePreservesExistingFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.yml")
	original := []byte("app:\n  title: Custom title\n")
	if err := os.WriteFile(filename, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeDefaultConfigFile(filename, []byte("replacement")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("error = %v, want file exists", err)
	}
	got, err := os.ReadFile(filename)
	if err != nil || string(got) != string(original) {
		t.Fatalf("existing file changed: %q, err=%v", got, err)
	}
}

func TestConfigCreateWithoutRuntimeInitialization(t *testing.T) {
	for _, tc := range []struct {
		name string
		full bool
		file bool
	}{
		{name: "commented stdout"},
		{name: "full stdout", full: true},
		{name: "commented file", file: true},
		{name: "full file", full: true, file: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			filename := filepath.Join(dir, "broken.yml")
			if err := os.WriteFile(filename, []byte("["), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HISTER_CONFIG", filename)
			t.Setenv("HISTER_DATA_DIR", filepath.Join(dir, "data"))
			t.Setenv("HISTER__APP__ACCESS_TOKEN", "private-token")
			t.Setenv("HISTER__APP__LOG_FILE", filepath.Join(dir, "runtime.log"))
			args := []string{"config", "create"}
			if tc.full {
				args = append(args, "--full")
			}
			var destination string
			if tc.file {
				destination = filepath.Join(dir, "nested", "config.yml")
				args = append(args, destination)
			}
			out, stderr, err := executeInspectionWithStderr(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			if stderr != "" {
				t.Fatalf("unexpected stderr: %q", stderr)
			}
			if tc.file {
				if !strings.Contains(out, destination) {
					t.Fatalf("missing created filename: %q", out)
				}
				content, err := os.ReadFile(destination)
				if err != nil {
					t.Fatal(err)
				}
				out = string(content)
				for _, path := range []string{destination, filepath.Dir(destination)} {
					info, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
						t.Errorf("%s is accessible by other users: %o", path, info.Mode().Perm())
					}
				}
			}
			assertDefaultConfigOutput(t, out, tc.full)
			wantEntries := 1
			if tc.file {
				wantEntries++
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != wantEntries {
				t.Fatalf("config creation initialized runtime files: %v, err=%v", entries, err)
			}
		})
	}
}

func assertDefaultConfigOutput(t *testing.T, output string, full bool) {
	t.Helper()
	if !full {
		var active map[string]any
		if err := yaml.Unmarshal([]byte(output), &active); err != nil || len(active) != 0 {
			t.Fatalf("commented config must not set any values: %v, err=%v", active, err)
		}
		const header = "# Commented settings are examples and do not override web configuration.\n" +
			"# Uncomment a section and selected settings to manage them in this file.\n"
		if !strings.HasPrefix(output, header) {
			t.Fatal("commented config is missing guidance for activating settings")
		}
		var uncommented strings.Builder
		for line := range strings.SplitAfterSeq(strings.TrimPrefix(output, header), "\n") {
			if line == "" {
				continue
			}
			value, ok := strings.CutPrefix(line, "# ")
			if !ok {
				t.Fatalf("uncommented YAML line: %q", line)
			}
			uncommented.WriteString(value)
		}
		output = uncommented.String()
	}
	want, err := yaml.Marshal(config.CreateDefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal([]byte(output), want) {
		t.Fatalf("output did not preserve all serialized defaults and indentation:\n%s", output)
	}
	var values struct {
		Server     struct{ Address string }
		Extractors map[string]any
	}
	if err := yaml.Unmarshal([]byte(output), &values); err != nil {
		t.Fatal(err)
	}
	if values.Server.Address != config.DefaultServerAddress || len(values.Extractors) != 0 {
		t.Fatalf("incorrect default address or extractor mapping: %+v", values)
	}
}

func TestConfigCreateUsesBuildDefaults(t *testing.T) {
	previousAddress := config.DefaultServerAddress
	t.Cleanup(func() { config.DefaultServerAddress = previousAddress })
	config.DefaultServerAddress = "0.0.0.0:5544"
	for _, full := range []bool{true, false} {
		t.Run(fmt.Sprintf("full=%t", full), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HISTER_CONFIG", filepath.Join(dir, "missing.yml"))
			t.Setenv("HISTER_DATA_DIR", filepath.Join(dir, "data"))
			t.Setenv("HISTER__SERVER__ADDRESS", "127.0.0.1:9999")
			t.Setenv("HISTER_PORT", "8888")
			args := []string{"config", "create"}
			if full {
				args = append(args, "--full")
			}
			out, err := executeInspection(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			assertDefaultConfigOutput(t, out, full)
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("config creation initialized runtime files: %v, err=%v", entries, err)
			}
		})
	}
}

func TestConfigCreatePreservesExistingFile(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(fmt.Sprintf("full=%t", full), func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "config.yml")
			const original = "app:\n  title: Custom title\n"
			if err := os.WriteFile(filename, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := executeInspection(t, "config", "create", fmt.Sprintf("--full=%t", full), filename)
			if err == nil || !strings.Contains(err.Error(), "already exists") {
				t.Fatalf("error = %v, want refusal to overwrite", err)
			}
			if strings.Contains(out, "Config file created") {
				t.Fatalf("reported success after refusing to overwrite: %q", out)
			}
			content, err := os.ReadFile(filename)
			if err != nil || string(content) != original {
				t.Fatalf("existing file changed: %q, err=%v", content, err)
			}
		})
	}
}

func TestConfigCreateReferenceSelectivelyUncommentsSettings(t *testing.T) {
	out, err := executeInspection(t, "config", "create")
	if err != nil {
		t.Fatal(err)
	}
	out = strings.Replace(out, "# server:\n", "server:\n", 1)
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimPrefix(line, "# "), "    address:") {
			lines[i] = strings.TrimPrefix(line, "# ")
		}
	}
	var values map[string]any
	if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &values); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"server": map[string]any{"address": config.DefaultServerAddress}}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("selective uncommenting set unexpected values: %v", values)
	}
}

func TestConfigValidateRejectsInvalidLanguageSettings(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"accuracy", "indexer:\n  language_detection_accuracy: medium\n", "indexer.language_detection_accuracy"},
		{"single language", "indexer:\n  languages: [en]\n", "indexer.languages"},
		{"duplicate language", "indexer:\n  languages: [en, EN]\n", "indexer.languages"},
		{"unsupported language", "indexer:\n  languages: [en, xx]\n", "indexer.languages"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			filename := filepath.Join(dir, "config.yml")
			if err := os.WriteFile(filename, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HISTER_CONFIG", filename)
			t.Setenv("HISTER_DATA_DIR", filepath.Join(dir, "data"))
			output, err := executeInspection(t, "config", "validate")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want %s; output = %q", err, tc.want, output)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("validation created runtime files: %v, err=%v", entries, err)
			}
		})
	}
}
