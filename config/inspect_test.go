package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestFieldCatalogCoversMainConfig(t *testing.T) {
	// Walk serialized fields independently of the catalog. Collections are whole
	// settings; their dynamic members belong to the category's editing workflow.
	want := make(map[string]bool)
	var visit func(reflect.Type, string)
	visit = func(typ reflect.Type, prefix string) {
		for property := range typ.Fields() {
			if !property.IsExported() {
				continue
			}
			key, _, _ := strings.Cut(property.Tag.Get("yaml"), ",")
			if key == "-" {
				continue
			}
			if key == "" || key != property.Tag.Get("mapstructure") {
				t.Fatalf("%s%s needs matching YAML and decoder keys", prefix, property.Name)
			}
			path := prefix + key
			if property.Type.Kind() == reflect.Struct {
				visit(property.Type, path+".")
			} else {
				want[path] = true
			}
		}
	}
	visit(reflect.TypeFor[Config](), "")
	seen := make(map[string]bool)
	for _, field := range FieldCatalog() {
		if seen[field.Path] {
			t.Errorf("duplicate catalog field %s", field.Path)
		}
		seen[field.Path] = true
		if !want[field.Path] {
			t.Errorf("catalog field %s has no main config setting", field.Path)
		}
	}
	for path := range want {
		if !seen[path] {
			t.Errorf("main config setting %s has no catalog field", path)
		}
	}
}

func TestFieldCatalogPreservesEditorContract(t *testing.T) {
	fields := make(map[string]FieldMetadata)
	for _, field := range FieldCatalog() {
		if _, exists := fields[field.Path]; exists {
			t.Fatalf("duplicate setting %s", field.Path)
		}
		fields[field.Path] = field
		category, _, _ := strings.Cut(field.Path, ".")
		if field.Label == "" || field.Description == "" || field.Category != category {
			t.Fatalf("incomplete description for %s", field.Path)
		}
		if field.Category != "app" && field.Path != "server.address" {
			if field.Schema.Type != "unknown" || field.Schema.Sensitivity != "unknown" || field.Default != nil || field.ReadOnlyReason == "" {
				t.Fatalf("unsupported setting %s gained editing support or exposed defaults", field.Path)
			}
			continue
		}
		if field.Effect != "runtime" {
			t.Fatalf("incorrect effect for %s", field.Path)
		}
		if err := field.Schema.ValidateDefinition(); err != nil {
			t.Fatalf("invalid control for %s: %v", field.Path, err)
		}
		if field.Default != nil {
			if _, message := field.Schema.ValidateValue(field.Default); message != "" {
				t.Fatalf("default for %s fails its own constraints: %s", field.Path, message)
			}
		}
	}
	title := fields["app.title"]
	if title.Label != "Title" || title.Default != "Hister" || title.Schema.MaxLength != 256 {
		t.Fatal("lost the title's description, default, or length limit")
	}
	snippet := fields["app.snippet_length"]
	if snippet.Default != 300 || snippet.Schema.Control != "number" || snippet.Schema.Min == nil || *snippet.Schema.Min != 1 || snippet.Schema.Max == nil || *snippet.Schema.Max != 10000 {
		t.Fatal("lost the snippet default or inclusive limits")
	}
	if !slices.Equal(fields["app.color_scheme"].Schema.Choices, []string{"automatic", "light", "dark"}) || !slices.Equal(fields["app.log_format"].Schema.Choices, []string{"", "text", "json"}) {
		t.Fatal("lost select choices, including the empty default option")
	}
	if toggle := fields["app.show_suggestions"]; toggle.Default != false || toggle.Schema.Control != "switch" {
		t.Fatal("lost an explicit false default or boolean control")
	}
	for _, path := range []string{"app.access_token", "app.search_url"} {
		field := fields[path]
		if field.Schema.Sensitivity == "none" || field.Default != nil || field.ReadOnlyReason == "" {
			t.Fatalf("lost secret handling for %s", path)
		}
	}
	for _, path := range []string{"app.access_token", "app.user_handling", "app.public"} {
		if !fields[path].RequiresConfirmation {
			t.Fatalf("lost confirmation requirement for %s", path)
		}
	}
	if fields["app.directory"].ReadOnlyReason == "" || fields["app.log_file"].ReadOnlyReason == "" {
		t.Fatal("storage locations became editable")
	}
}

func TestListenAddressSourceInEditor(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "commented", true: "explicit"}[explicit], func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HISTER_DATA_DIR", dir)
			filename := filepath.Join(dir, "config.yml")
			content := "# server:\n#   address: " + DefaultServerAddress + "\n"
			if explicit {
				content = "server:\n  address: " + DefaultServerAddress + "\n"
			}
			if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, _, err := LoadForInspection(filename, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range FieldCatalog() {
				if field.Path != "server.address" {
					continue
				}
				state := cfg.FieldStates(field)[0]
				if state.Value != DefaultServerAddress || state.Source.Locked != explicit || state.Editable || !field.RequiresConfirmation || field.ReadOnlyReason == "" {
					t.Fatalf("address must show its source and value without allowing unconfirmed changes: %+v, %+v", field, state)
				}
			}
			if candidate, errors := cfg.PrepareDraft([]SettingEdit{{Path: "server.address", Action: "set", Value: json.RawMessage(`"0.0.0.0:4433"`)}}); candidate != nil || len(errors) == 0 {
				t.Fatal("address became writable before the confirmation workflow")
			}
		})
	}
}

func TestFieldStatesRedactDeferredSettings(t *testing.T) {
	c := CreateDefaultConfig()
	c.Server.Database = "password=private-credential"
	c.Server.OAuth = map[string]*OAuthEntry{"provider": {ClientSecret: "private-credential"}}
	c.Crawler.Headers = map[string]string{"Authorization": "private-credential"}
	c.Crawler.Cookies = []CrawlerCookie{{Name: "session", Value: "private-credential"}}
	c.Crawler.BackendOptions = map[string]any{"token": "private-credential"}
	c.SemanticSearch.APIKey = "private-credential"
	c.SemanticSearch.Headers = map[string]string{"Authorization": "private-credential"}
	c.Extractors = map[string]*Extractor{"custom": {Options: map[string]any{"token": "private-credential"}}}
	for _, populated := range []bool{true, false} {
		if !populated {
			c.Server.Database = ""
			c.Server.OAuth = nil
			c.Crawler.Headers = map[string]string{}
			c.Crawler.Cookies = []CrawlerCookie{}
			c.Crawler.BackendOptions = nil
			c.SemanticSearch.APIKey = ""
			c.SemanticSearch.Headers = nil
			c.Extractors = map[string]*Extractor{}
		}
		states := make(map[string]FieldState)
		for _, state := range c.FieldStates() {
			states[state.Path] = state
		}
		for _, path := range []string{"server.database", "server.oauth", "crawler.headers", "crawler.cookies", "crawler.backend_options", "semantic_search.api_key", "semantic_search.headers", "extractors"} {
			state, exists := states[path]
			if !exists || state.Editable || state.Reason == "" || state.Value != nil || state.SecretPresent != populated {
				t.Errorf("incorrect redaction for %s with populated=%v: %+v", path, populated, state)
			}
		}
		raw, err := json.Marshal(c.FieldStates())
		if err != nil || strings.Contains(string(raw), "private-credential") {
			t.Fatal("deferred settings exposed credentials")
		}
	}
}

func TestFieldStatesSummarizeCollectionSources(t *testing.T) {
	const selected = "server:\n  oauth:\n    github:\n      client_secret: private-credential\nextractors:\n  ytdlp:\n    enable: true\n"
	for _, tc := range []struct {
		name, main, web, kind string
		environment, locked   bool
	}{
		{name: "default", main: "{}", kind: "default"},
		{name: "empty containers", main: "server:\n  oauth: {}\nextractors: {}\n", kind: "default"},
		{name: "file", main: selected, kind: "file", locked: true},
		{name: "managed", main: "{}", web: selected, kind: "web"},
		{name: "environment", main: "{}", environment: true, kind: "environment", locked: true},
		{name: "mixed", main: selected, web: "server:\n  oauth:\n    github:\n      client_id: managed-id\nextractors:\n  basic:\n    enable: false\n", kind: "mixed", locked: true},
		{name: "shadowed", main: selected, web: selected, kind: "file", locked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HISTER_DATA_DIR", dir)
			if tc.environment {
				t.Setenv("HISTER__SERVER__OAUTH__GITHUB__CLIENT_SECRET", "private-credential")
				t.Setenv("HISTER__EXTRACTORS__YTDLP__ENABLE", "true")
			}
			filename := filepath.Join(dir, "config.yml")
			if err := os.WriteFile(filename, []byte(tc.main), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.web != "" {
				if err := os.WriteFile(filepath.Join(dir, "config.web.yml"), []byte(tc.web), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			c, _, err := LoadForInspection(filename, true)
			if err != nil {
				t.Fatal(err)
			}
			states := make(map[string]FieldState)
			for _, state := range c.FieldStates() {
				states[state.Path] = state
			}
			for _, path := range []string{"server.oauth", "extractors"} {
				state, exists := states[path]
				if !exists || state.Source.Kind != tc.kind || state.Source.Locked != tc.locked || state.Source.WebSelected != (tc.web != "") {
					t.Errorf("incorrect collection source for %s: %+v", path, state.Source)
				}
				if tc.kind != "default" && state.Source.Reason == "" {
					t.Errorf("missing source explanation for %s", path)
				}
				if tc.kind == "mixed" && state.Source.Name != "" {
					t.Errorf("mixed sources claim a single source name for %s", path)
				}
				if state.Editable || state.Value != nil {
					t.Errorf("collection summary granted editing or exposed values for %s", path)
				}
			}
			if tc.kind == "mixed" {
				if source := c.Source("extractors.ytdlp.enable"); source.Kind != "file" || !source.Locked {
					t.Fatal("collection summary changed the authoritative child source")
				}
				if source := c.Source("extractors.basic.enable"); source.Kind != "web" || source.Locked {
					t.Fatal("collection summary locked an independently editable child")
				}
			}
		})
	}
}

func TestValidateAppFieldConstraints(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"title", func(c *Config) { c.App.Title = strings.Repeat("x", 257) }},
		{"subtitle", func(c *Config) { c.App.Subtitle = strings.Repeat("x", 1025) }},
		{"snippet length", func(c *Config) { c.App.SnippetLength = 10001 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := CreateDefaultConfig()
			tc.change(c)
			if err := c.Validate(); err == nil {
				t.Fatal("accepted an App value outside the editor's supported bounds")
			}
			c.App.Directory = t.TempDir()
			t.Setenv("HISTER_DATA_DIR", c.App.Directory)
			raw, err := yaml.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			filename := filepath.Join(t.TempDir(), "config.yml")
			if err := os.WriteFile(filename, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(filename); err == nil {
				t.Fatal("startup accepted App values that inspection and reload reject")
			}
		})
	}
}

func TestPrepareDraftUsesDetachedLoadedSources(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	filename := filepath.Join(dir, "config.yml")
	managed := filepath.Join(dir, "config.web.yml")
	const main = "app:\n  subtitle: Locked subtitle\n  access_token: private-credential\n"
	const web = "app:\n  title: Saved title\nsemantic_search:\n  api_key: saved-credential\n"
	if err := os.WriteFile(filename, []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, []byte(web), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	next, fields := c.PrepareDraft([]SettingEdit{
		{Path: "app.title", Action: "set", Value: json.RawMessage(`"Draft title"`)},
		{Path: "app.color_scheme", Action: "set", Value: json.RawMessage(`"dark"`)},
		{Path: "app.snippet_length", Action: "set", Value: json.RawMessage(`180`)},
		{Path: "app.show_suggestions", Action: "set", Value: json.RawMessage(`true`)},
	})
	if len(fields) != 0 || next == nil {
		t.Fatalf("draft rejected: %v", fields)
	}
	if next.BaseURL("") != "http://"+DefaultServerAddress {
		t.Fatal("candidate did not retain validated server normalization")
	}
	if next.App.Title != "Draft title" || next.App.Subtitle != "Locked subtitle" || next.App.ColorScheme != "dark" || next.App.SnippetLength != 180 || !next.App.ShowSuggestions {
		t.Fatal("draft did not compose selected values")
	}
	if c.App.Title != "Saved title" || c.App.ColorScheme != "automatic" || c.App.ShowSuggestions {
		t.Fatal("draft changed active configuration")
	}
	if next.App.AccessToken != "private-credential" || next.SemanticSearch.APIKey != "saved-credential" {
		t.Fatal("draft lost unrelated secrets")
	}
	if source := next.Source("app.title"); source.Kind != "web" || source.Locked || !source.WebSelected {
		t.Fatalf("draft source = %+v", source)
	}
	// Preparing a second draft uses the loaded sources, never newly edited files
	// or environment variables. The future apply operation checks revisions.
	t.Setenv("HISTER__APP__TITLE", "New environment")
	if err := os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	reset, fields := next.PrepareDraft([]SettingEdit{{Path: "app.title", Action: "reset"}})
	if len(fields) != 0 || reset == nil {
		t.Fatalf("reset rejected: %v", fields)
	}
	if reset.App.Title != "Hister" || reset.Source("app.title").WebSelected || next.App.Title != "Draft title" {
		t.Fatal("reset did not expose inherited defaults independently")
	}
	got, err := os.ReadFile(managed)
	if err != nil || string(got) != web {
		t.Fatal("draft validation rewrote the managed file")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("draft validation created runtime files")
	}
}

func TestFieldStatesHideCredentialsInsideURLPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, []byte("app:\n  search_url: https://search.example/private-credential?q={query}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(c.FieldStates())
	if err != nil || strings.Contains(string(raw), "private-credential") {
		t.Fatal("editor state exposed credentials inside a URL path")
	}
}

func TestPrepareDraftRetainsLoadedDefaults(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("the macOS default data directory has no environment override")
	}
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "changed"))
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "changed"))
	next, fields := c.PrepareDraft([]SettingEdit{{Path: "app.title", Action: "set", Value: json.RawMessage(`"Draft"`)}})
	if len(fields) != 0 || next == nil {
		t.Fatalf("draft rejected: %v", fields)
	}
	if next.App.Directory != c.App.Directory {
		t.Fatal("an unrelated draft silently changed the default data directory")
	}
}

func TestDraftFieldStatesAndRestrictions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	filename := filepath.Join(dir, "config.yml")
	const main = "app:\n  title: Hister\n  access_token: private-credential\n  search_url: https://owner:private-credential@search.example/?q={query}&key=private-credential\n"
	if err := os.WriteFile(filename, []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	states := make(map[string]FieldState)
	for _, field := range c.FieldStates() {
		states[field.Path] = field
	}
	if states["app.title"].Editable || states["app.title"].Source.Kind != "file" || states["app.title"].Reason == "" {
		t.Fatal("explicit default title was not locked")
	}
	if !states["app.subtitle"].Editable || states["app.subtitle"].Source.Kind != "default" {
		t.Fatal("ordinary inherited field is not editable")
	}
	if !states["app.access_token"].SecretPresent || states["app.access_token"].Value != nil || states["app.access_token"].Editable {
		t.Fatal("stored token is not protected")
	}
	for _, value := range []any{c.FieldStates(), FieldCatalog()} {
		raw, err := json.Marshal(value)
		if err != nil || strings.Contains(string(raw), "private-credential") {
			t.Fatalf("editor description leaked credentials: %v", err)
		}
	}
	edits := []SettingEdit{
		{Path: "app.title", Action: "reset"},
		{Path: "app.directory", Action: "set", Value: json.RawMessage(`"/tmp/other"`)},
		{Path: "app.access_token", Action: "set", Value: json.RawMessage(`"replacement-secret"`)},
		{Path: "app.user_handling", Action: "set", Value: json.RawMessage(`false`)},
		{Path: "server.address", Action: "set", Value: json.RawMessage(`"0.0.0.0:4433"`)},
		{Path: "app", Action: "set", Value: json.RawMessage(`{"title":"bypass"}`)},
		{Path: "app.show_suggestions", Action: "set", Value: json.RawMessage(`"true"`)},
		{Path: "app.snippet_length", Action: "set", Value: json.RawMessage(`1.5`)},
		{Path: "app.color_scheme", Action: "set", Value: json.RawMessage(`"replacement-secret"`)},
		{Path: "app.subtitle", Action: "reset", Value: json.RawMessage(`null`)},
		{Path: "app.log_format", Action: "clear"},
	}
	next, fields := c.PrepareDraft(edits)
	if next != nil || len(fields) != len(edits) {
		t.Fatalf("want one rejection per edit, got %v", fields)
	}
	for i, field := range fields {
		if field.Path != edits[i].Path || field.Code == "" || field.Message == "" {
			t.Fatalf("unstructured error: %+v", field)
		}
	}
	raw, err := json.Marshal(fields)
	if err != nil || strings.Contains(string(raw), "replacement-secret") {
		t.Fatal("validation errors leaked values")
	}
	got, err := os.ReadFile(filename)
	if err != nil || string(got) != main {
		t.Fatal("rejected draft changed the main file")
	}
	for _, values := range [][]SettingEdit{
		{{Path: "app.subtitle", Action: "set", Value: json.RawMessage(`"first"`)}, {Path: "app.subtitle", Action: "reset"}},
		{{Path: "app.subtitle", Action: "set", Value: json.RawMessage(`"first" "second"`)}},
		{{Path: "app.subtitle", Action: "set", Value: json.RawMessage(`null`)}},
	} {
		if candidate, errors := c.PrepareDraft(values); candidate != nil || len(errors) == 0 {
			t.Fatal("accepted ambiguous or missing draft value")
		}
	}
}

func TestPrepareDraftValidatesCompleteCandidate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	filename := filepath.Join(dir, "config.yml")
	const raw = `app:
  color_scheme: invalid-credential
  snippet_length: 0
server:
  max_batch_body_size: 0
hotkeys:
  web: {alt+x: invalid-credential}
sensitive_content_patterns:
  broken: '[invalid-credential'
semantic_search:
  enable: true
  dimensions: 0
`
	if err := os.WriteFile(filename, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	next, fields := c.PrepareDraft([]SettingEdit{{Path: "app.title", Action: "set", Value: json.RawMessage(`"Valid title"`)}})
	if next != nil {
		t.Fatal("accepted a candidate with invalid inherited settings")
	}
	paths := make(map[string]bool)
	for _, field := range fields {
		paths[field.Path] = true
	}
	for _, path := range []string{"app.color_scheme", "app.snippet_length", "server.max_batch_body_size", "hotkeys.web", "sensitive_content_patterns", "semantic_search"} {
		if !paths[path] {
			t.Errorf("missing independent error for %s: %v", path, fields)
		}
	}
	encoded, err := json.Marshal(fields)
	if err != nil || strings.Contains(string(encoded), "invalid-credential") {
		t.Fatal("inherited values leaked into errors")
	}
	if c.Server.BaseURL != "" {
		t.Fatal("validation normalized the active configuration")
	}
}

func TestValidationRejectsIndividuallyInvalidSensitivePatterns(t *testing.T) {
	for _, patterns := range []map[string]string{
		{"opening": "[", "closing": "]"},
		{"opening": "(", "closing": ")"},
		{"opening": "[private_credential", "closing": "]"},
	} {
		c := CreateDefaultConfig()
		c.SensitiveContentPatterns = patterns
		if err := c.Validate(); err == nil {
			t.Error("configuration accepted individually invalid sensitive patterns")
		} else if strings.Contains(err.Error(), "private_credential") {
			t.Error("configuration error exposed a sensitive pattern")
		}
		fields := c.ValidateFields()
		found := false
		for _, field := range fields {
			if field.Path == "sensitive_content_patterns" {
				found = true
			}
			if strings.Contains(field.Message, "private_credential") {
				t.Error("field error exposed a sensitive pattern")
			}
		}
		if !found {
			t.Errorf("missing sensitive pattern validation error: %v", fields)
		}
	}
}

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
	c.Indexer.PDFParser.Args = []string{"--password", "pdf-credential"}
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
	if c.Indexer.PDFParser.Args[1] != "pdf-credential" {
		t.Fatal("redaction mutated PDF parser arguments")
	}
}
