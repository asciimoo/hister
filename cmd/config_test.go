package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

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

func TestConfigCreateKeepsYAMLSeparateFromDeprecationNotice(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		deprecated bool
	}{
		{[]string{"config", "create"}, false},
		{[]string{"create-config"}, true},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			dir := t.TempDir()
			filename := filepath.Join(dir, "broken.yml")
			if err := os.WriteFile(filename, []byte("["), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HISTER_CONFIG", filename)
			t.Setenv("HISTER_DATA_DIR", filepath.Join(dir, "data"))
			out, stderr, err := executeInspectionWithStderr(t, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			var values map[string]any
			if err := yaml.Unmarshal([]byte(out), &values); err != nil || values["app"] == nil || values["server"] == nil {
				t.Fatalf("invalid config output: %s, err=%v", out, err)
			}
			if strings.Contains(out, "deprecated") {
				t.Fatal("deprecation notice contaminated YAML")
			}
			if tc.deprecated {
				if !strings.Contains(stderr, "deprecated") || !strings.Contains(stderr, "hister config create") {
					t.Fatalf("missing migration notice: %q", stderr)
				}
			} else if stderr != "" {
				t.Fatalf("unexpected stderr: %q", stderr)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("config creation initialized runtime files: %v, err=%v", entries, err)
			}
		})
	}
}
