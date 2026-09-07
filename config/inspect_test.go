package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestInspectionUsesEnvironmentWithoutCreatingRuntimeFiles(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, []byte("app:\n  title: From file\nserver:\n  address: 127.0.0.1:4433\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HISTER__APP__TITLE", "From environment")
	t.Setenv("HISTER_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("HISTER_PORT", "5432")
	c, name, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if name != filename || c.App.Title != "From environment" || c.BaseURL("") != "http://127.0.0.1:5432" {
		t.Fatalf("unexpected effective configuration: path=%s title=%s baseURL=%s", name, c.App.Title, c.BaseURL(""))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.yml" {
		t.Fatalf("inspection created runtime files: %v", entries)
	}
}

func TestInspectionRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"unknown key", "app:\n  colour_scheme: dark\n", "app.colour_scheme"},
		{"invalid type", "crawler:\n  timeout: a-private-value\n", "crawler.timeout"},
		{"invalid YAML", "app: [\n", "YAML"},
		{"invalid public mode", "app:\n  public: true\n", "app.public"},
		{"invalid backend", "crawler:\n  backend: typo\n", "crawler.backend"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "config.yml")
			if err := os.WriteFile(filename, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			c, _, err := LoadForInspection(filename, true)
			if err == nil {
				err = c.Validate()
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "a-private-value") {
				t.Fatalf("error = %v, want %s without credential values", err, tc.want)
			}
		})
	}
	if _, err := ResolvePath(filepath.Join(t.TempDir(), "missing.yml"), true); err == nil {
		t.Fatal("explicit missing file was accepted")
	}
}

func TestValidateRejectsNilOAuthEntry(t *testing.T) {
	c := CreateDefaultConfig()
	c.Server.OAuth = map[string]*OAuthEntry{"github": nil}
	if err := c.Validate(); err == nil {
		t.Fatal("nil OAuth entry was accepted")
	}
}

func TestResolvePathDoesNotParseYAML(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(filename, []byte("["), 0o600); err != nil {
		t.Fatal(err)
	}
	if path, err := ResolvePath(filename, true); err != nil || path != filename {
		t.Fatalf("path=%s err=%v", path, err)
	}
}

func TestRedactedConfigHidesCredentialsWithoutMutation(t *testing.T) {
	c := CreateDefaultConfig()
	c.App.AccessToken = "access-credential"
	c.Server.Database = "host=localhost password=database-credential"
	c.Server.OAuth = map[string]*OAuthEntry{"github": {ClientID: "visible-id", ClientSecret: "oauth-credential"}}
	c.Crawler.Headers = map[string]string{"Authorization": "header-credential", "X-Custom": "custom-credential"}
	c.Crawler.Cookies = []CrawlerCookie{{Name: "session", Value: "cookie-credential"}}
	c.Crawler.Proxy = "http://proxy-user:proxy-credential@localhost:8080?key=query-credential#fragment-credential"
	c.SemanticSearch.APIKey = "embedding-credential"
	c.Extractors = map[string]*Extractor{"ytdlp": {Options: map[string]any{"extra_args": []string{"--password", "argument-credential"}, "nested": map[string]any{"api_key": "nested-credential"}}}}
	values, err := c.Redacted()
	if err != nil {
		t.Fatal(err)
	}
	output, err := yaml.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "credential") || strings.Contains(string(output), "proxy-user") {
		t.Fatalf("output contains credentials: %s", output)
	}
	if !strings.Contains(string(output), "visible-id") || !strings.Contains(string(output), "generic_private_key") {
		t.Fatal("nonsecret settings were lost")
	}
	if c.App.AccessToken != "access-credential" || c.Crawler.Headers["Authorization"] != "header-credential" || c.Crawler.Cookies[0].Value != "cookie-credential" {
		t.Fatal("redaction mutated the source configuration")
	}
}
