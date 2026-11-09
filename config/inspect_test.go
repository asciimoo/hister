package config

import (
	"encoding/json"
	"fmt"
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
		ordinaryIndexer := field.Category == "indexer" && !strings.HasPrefix(field.Path, "indexer.pdf_parser.")
		if field.Category != "app" && field.Path != "server.address" && !ordinaryIndexer && field.Category != "crawler" && field.Category != "semantic_search" {
			if field.Schema.Type != "unknown" || field.Schema.Sensitivity != "unknown" || field.Default != nil || field.ReadOnlyReason == "" {
				t.Fatalf("unsupported setting %s gained editing support or exposed defaults", field.Path)
			}
			continue
		}
		if field.Effect != "runtime" && field.Effect != "reindex" && field.Effect != "reembed" {
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
	endpoint := fields["semantic_search.embedding_endpoint"]
	if endpoint.Schema.Sensitivity != "none" || endpoint.Schema.Control != "url" || endpoint.Default != "http://localhost:11434/v1/embeddings" {
		t.Fatal("embedding endpoint must expose its default as an ordinary URL field")
	}
	title := fields["app.title"]
	if title.Label != "Title" || title.Default != "Hister" || title.Schema.MaxLength != 256 {
		t.Fatal("lost the title's description, default, or length limit")
	}
	snippet := fields["app.snippet_length"]
	if snippet.Default != 300 || snippet.Schema.Control != "number" || snippet.Schema.Min == nil || *snippet.Schema.Min != 1 || snippet.Schema.Max == nil || *snippet.Schema.Max != 10000 {
		t.Fatal("lost the snippet default or inclusive limits")
	}
	if !slices.Equal(fields["app.color_scheme"].Schema.Choices, []string{"automatic", "light", "dark"}) || !slices.Equal(fields["app.log_format"].Schema.Choices, []string{"text", "json"}) {
		t.Fatal("lost concrete select choices")
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

func TestSelectSettingsUseConcreteDefaults(t *testing.T) {
	c := CreateDefaultConfig()
	if c.App.LogLevel != "info" || c.App.LogFormat != "text" || c.Crawler.Backend != "http" || c.Indexer.LanguageDetectionAccuracy != "low" {
		t.Fatalf("defaults must contain concrete choices: log level=%q, log format=%q, backend=%q, accuracy=%q", c.App.LogLevel, c.App.LogFormat, c.Crawler.Backend, c.Indexer.LanguageDetectionAccuracy)
	}
	for _, field := range FieldCatalog() {
		if field.Schema.Control != "select" {
			continue
		}
		if slices.Contains(field.Schema.Choices, "") {
			t.Errorf("%s offers an empty choice", field.Path)
		}
		if _, message := field.Schema.ValidateValue(""); message == "" {
			t.Errorf("%s accepts an empty choice", field.Path)
		}
	}
}

func TestSelectDraftsRequireChoicesAndResetToConcreteDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, []byte("# defaults\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, choice, inherited string }{
		{"app.log_level", "error", "info"},
		{"app.log_format", "json", "text"},
		{"indexer.language_detection_accuracy", "high", "low"},
		{"crawler.backend", "bidi", "http"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			empty, fields := c.PrepareDraft([]SettingEdit{{Path: tc.path, Action: "set", Value: json.RawMessage(`""`)}})
			if empty != nil || len(fields) != 1 || fields[0].Path != tc.path || fields[0].Code != "invalid_value" {
				t.Fatalf("empty selection must be rejected: %v", fields)
			}
			choice, err := json.Marshal(tc.choice)
			if err != nil {
				t.Fatal(err)
			}
			next, fields := c.PrepareDraft([]SettingEdit{{Path: tc.path, Action: "set", Value: choice}})
			if next == nil || len(fields) != 0 {
				t.Fatalf("concrete choice rejected: %v", fields)
			}
			reset, fields := next.PrepareDraft([]SettingEdit{{Path: tc.path, Action: "reset"}})
			if reset == nil || len(fields) != 0 {
				t.Fatalf("reset rejected: %v", fields)
			}
			for _, state := range reset.FieldStates() {
				if state.Path == tc.path {
					if state.Value != tc.inherited || state.Source.Kind != "default" || state.Source.WebSelected {
						t.Fatalf("reset did not restore %q without a web override: %+v", tc.inherited, state)
					}
					return
				}
			}
			t.Fatal("reset setting is missing")
		})
	}
}

func TestSemanticSearchDraftControls(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, []byte("# defaults\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	edits := []SettingEdit{
		{Path: "semantic_search.enable", Action: "set", Value: json.RawMessage(`true`)},
		{Path: "semantic_search.embedding_endpoint", Action: "set", Value: json.RawMessage(`"https://embeddings.example/v1/embeddings"`)},
		{Path: "semantic_search.embedding_model", Action: "set", Value: json.RawMessage(`"small-model"`)},
		{Path: "semantic_search.api_key", Action: "set", Value: json.RawMessage(`{"action":"replace","value":"private-api-key"}`)},
		{Path: "semantic_search.headers", Action: "set", Value: json.RawMessage(`{"x-key":{"action":"replace","value":"private-header"}}`)},
		{Path: "semantic_search.dimensions", Action: "set", Value: json.RawMessage(`128`)},
		{Path: "semantic_search.semantic_weight", Action: "set", Value: json.RawMessage(`0.75`)},
	}
	next, fields := c.PrepareDraft(edits)
	if len(fields) != 0 || next == nil {
		t.Fatalf("semantic settings rejected: %v", fields)
	}
	if !next.SemanticSearch.Enable || next.SemanticSearch.Dimensions != 128 || next.SemanticSearch.SemanticWeight != 0.75 || next.SemanticSearch.Headers["x-key"] != "private-header" {
		t.Fatalf("semantic draft lost typed selections: %+v", next.SemanticSearch)
	}
	for _, state := range next.FieldStatesForRevision("revision") {
		if state.Path == "semantic_search.embedding_endpoint" && (state.Value != "https://embeddings.example/v1/embeddings" || state.SecretPresent || !state.Editable) {
			t.Fatalf("embedding endpoint is not visible and editable: %+v", state)
		}
	}
	raw, err := json.Marshal(next.FieldStatesForRevision("revision"))
	if err != nil || strings.Contains(string(raw), "private-") {
		t.Fatal("semantic settings exposed secret values")
	}
	for _, tc := range []struct{ path, value string }{
		{"embedding_endpoint", `"file:///embeddings"`},
		{"embedding_endpoint", `"not-a-url"`},
		{"embedding_endpoint", `""`},
		{"embedding_model", `""`},
		{"dimensions", `0`},
		{"semantic_weight", `1.1`},
		{"similarity_threshold", `-0.1`},
		{"result_limit", `0`},
		{"max_context_length", `0`},
		{"chunk_overlap", `512`},
		{"query_embedding_timeout", `-1`},
		{"max_query_embedding_concurrency", `-1`},
		{"headers", `{"invalid header":{"action":"replace","value":"private-header"}}`},
	} {
		path := "semantic_search." + tc.path
		candidate, errors := next.PrepareDraft([]SettingEdit{{Path: path, Action: "set", Value: json.RawMessage(tc.value)}})
		if candidate != nil || len(errors) == 0 || errors[0].Path != path {
			t.Errorf("accepted invalid %s or returned the wrong field: %v", path, errors)
		}
	}
	if c.SemanticSearch.Enable || c.SemanticSearch.APIKey != "" {
		t.Fatal("validation mutated active semantic settings")
	}
}

func TestLanguageChoicesExposeNamesAndRetainCodes(t *testing.T) {
	for _, field := range FieldCatalog() {
		if field.Path != "indexer.languages" {
			continue
		}
		raw, err := json.Marshal(field.Schema.Items)
		if err != nil {
			t.Fatal(err)
		}
		var choices struct {
			Values []string          `json:"choices"`
			Labels map[string]string `json:"choice_labels"`
		}
		if err := json.Unmarshal(raw, &choices); err != nil {
			t.Fatal(err)
		}
		for _, code := range choices.Values {
			if label := choices.Labels[code]; label == "" || label == code {
				t.Errorf("language %q has no readable name", code)
			}
		}
		for code, name := range map[string]string{"en": "English", "hu": "Hungarian", "de": "German", "nb": "Norwegian Bokmål", "zh": "Chinese"} {
			if choices.Labels[code] != name {
				t.Errorf("language %q name = %q, want %q", code, choices.Labels[code], name)
			}
		}
		if value, message := field.Schema.ValidateValue([]string{"en", "hu"}); message != "" || !reflect.DeepEqual(value, []any{"en", "hu"}) {
			t.Fatalf("language codes no longer accepted unchanged: %v %s", value, message)
		}
		if _, message := field.Schema.ValidateValue([]string{"English", "Hungarian"}); message == "" {
			t.Fatal("display names became stored language values")
		}
		return
	}
	t.Fatal("language choices are missing")
}

func TestFieldCatalogIncludesDisplayGroups(t *testing.T) {
	raw, err := json.Marshal(FieldCatalog())
	if err != nil {
		t.Fatal(err)
	}
	var fields []struct {
		Path     string `json:"path"`
		Category string `json:"category"`
		Group    string `json:"group"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	wantGroups := map[string][]string{
		"app":             {"Appearance", "Search behavior", "Access", "Logging and diagnostics", "Storage"},
		"server":          {"Connection", "Authentication", "Storage", "Request limits", "Monitoring"},
		"indexer":         {"Language and text", "Local files", "PDF extraction"},
		"crawler":         {"Crawl behavior", "Page fetching", "Requests"},
		"semantic_search": {"Search behavior", "Embedding service", "Document chunks", "Document processing"},
	}
	groups := make(map[string][]string)
	byPath := make(map[string]string)
	for _, field := range fields {
		byPath[field.Path] = field.Group
		if _, grouped := wantGroups[field.Category]; !grouped {
			if field.Group != "" {
				t.Errorf("single purpose category %s gained an unnecessary group", field.Category)
			}
			continue
		}
		if field.Group == "" {
			t.Fatalf("%s has no display group", field.Path)
		}
		if !slices.Contains(groups[field.Category], field.Group) {
			groups[field.Category] = append(groups[field.Category], field.Group)
		}
	}
	for category, want := range wantGroups {
		if !slices.Equal(groups[category], want) {
			t.Errorf("%s groups = %v, want %v", category, groups[category], want)
		}
	}
	for path, want := range map[string]string{
		"app.title":                       "Appearance",
		"app.snippet_length":              "Search behavior",
		"app.directory":                   "Storage",
		"server.oauth":                    "Authentication",
		"indexer.pdf_parser.command":      "PDF extraction",
		"indexer.pdf_parser.args":         "PDF extraction",
		"crawler.cookies":                 "Requests",
		"semantic_search.embedding_model": "Embedding service",
		"semantic_search.chunk_overlap":   "Document chunks",
	} {
		if byPath[path] != want {
			t.Errorf("%s group = %q, want %q", path, byPath[path], want)
		}
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
		for _, path := range []string{"server.database", "server.oauth", "extractors"} {
			state, exists := states[path]
			if !exists || state.Editable || state.Reason == "" || state.Value != nil || state.SecretPresent != populated {
				t.Errorf("incorrect redaction for %s with populated=%v: %+v", path, populated, state)
			}
		}
		for _, path := range []string{"crawler.headers", "crawler.cookies", "semantic_search.api_key", "semantic_search.headers"} {
			if state := states[path]; !state.Editable || state.SecretPresent != populated {
				t.Fatalf("incorrect editable secret collection: %s", path)
			}
		}
		if populated && (states["crawler.backend_options"].Value != nil || states["crawler.backend_options"].Editable) {
			t.Fatal("undescribed backend options were exposed")
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

func TestWatchedDirectoryDraftSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, []byte("# defaults\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	valid := `[{"path":"~/Documents","label":"notes","filetypes":["md"],"patterns":["note*"],"excludes":["secret*"],"include_hidden":true,"delete_on_remove":true,"user":"alice"}]`
	next, fields := c.PrepareDraft([]SettingEdit{{Path: "indexer.directories", Action: "set", Value: json.RawMessage(valid)}})
	if next == nil || len(fields) != 0 {
		t.Fatalf("directory draft rejected: %v", fields)
	}
	want := &Directory{Path: "~/Documents", Label: "notes", Filetypes: []string{"md"}, Patterns: []string{"note*"}, Excludes: []string{"secret*"}, IncludeHidden: true, DeleteOnRemove: true, User: "alice"}
	if len(next.Indexer.Directories) != 1 || !reflect.DeepEqual(next.Indexer.Directories[0], want) {
		t.Fatalf("incorrect directory: %+v", next.Indexer.Directories)
	}
	raw, err := json.Marshal(next.FieldStates())
	if err != nil || !strings.Contains(string(raw), `"delete_on_remove":true`) || !strings.Contains(string(raw), `"path":"~/Documents"`) {
		t.Fatalf("directory read does not use catalog keys: %s %v", raw, err)
	}
	for _, value := range []string{
		`null`, `[null]`, `[{}]`, `[{"path":""}]`, `[{"path":"relative"}]`,
		`[{"path":"/docs","patterns":["["]}]`, `[{"path":"/docs","excludes":["["]}]`,
		`[{"path":"/docs","filetypes":[".md"]}]`, `[{"path":"/docs","filetypes":["*"]}]`,
		`[{"path":"/docs","delete_on_remove":"true"}]`, `[{"path":"/docs","unknown":true}]`,
		`[{"path":"/docs","path":"/other"}]`,
	} {
		candidate, errors := c.PrepareDraft([]SettingEdit{{Path: "indexer.directories", Action: "set", Value: json.RawMessage(value)}})
		if candidate != nil || len(errors) == 0 || errors[0].Path != "indexer.directories" {
			t.Errorf("accepted invalid directories %s: %v", value, errors)
		}
	}
	for _, edit := range []SettingEdit{{Path: "indexer.directories", Action: "set", Value: json.RawMessage(`[]`)}, {Path: "indexer.directories", Action: "reset"}} {
		cleared, errors := next.PrepareDraft([]SettingEdit{edit})
		if cleared == nil || len(errors) != 0 || len(cleared.Indexer.Directories) != 0 || cleared.Source(edit.Path).WebSelected != (edit.Action == "set") {
			t.Fatalf("clear/reset failed: %v", errors)
		}
	}
	if err := os.WriteFile(filename, []byte("indexer:\n  directories:\n    - path: /locked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	locked, _, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, edit := range []SettingEdit{{Path: "indexer.directories", Action: "set", Value: json.RawMessage(`[]`)}, {Path: "indexer.directories", Action: "reset"}, {Path: "indexer.directories.0.path", Action: "set", Value: json.RawMessage(`"/bypass"`)}} {
		candidate, errors := locked.PrepareDraft([]SettingEdit{edit})
		if candidate != nil || len(errors) == 0 {
			t.Fatalf("collection lock bypassed: %v", edit)
		}
	}
}

func TestWatchedDirectoryStateCanBeEdited(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, []byte("# defaults\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.web.yml"), []byte("indexer:\n  directories:\n    - path: ~/docs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range c.FieldStates() {
		if state.Path != "indexer.directories" {
			continue
		}
		value, err := json.Marshal(state.Value)
		if err != nil {
			t.Fatal(err)
		}
		// A browser preserves untouched properties when changing one row member.
		value = []byte(strings.Replace(string(value), "~/docs", "~/other", 1))
		next, fields := c.PrepareDraft([]SettingEdit{{Path: state.Path, Action: "set", Value: value}})
		if next == nil || len(fields) != 0 {
			t.Fatalf("cannot edit a stored directory row: %v", fields)
		}
		if len(next.Indexer.Directories) != 1 || next.Indexer.Directories[0].Path != "~/other" {
			t.Fatalf("edited directory path was not retained: %+v", next.Indexer.Directories)
		}
		return
	}
	t.Fatal("indexer.directories field is missing")
}

func TestCrawlerProxyValidationPreservesEmptyProxyBehavior(t *testing.T) {
	c := CreateDefaultConfig()
	c.Crawler.Proxy = " \t"
	if fields := c.ValidateFields(); len(fields) > 0 {
		t.Fatalf("blank proxy should retain default behavior: %v", fields)
	}
}

func TestCrawlerSocketSurvivesBackendOptionEdits(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, []byte("# defaults\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.web.yml"), []byte("crawler:\n  backend: bidi\n  backend_options:\n    socket: ws://127.0.0.1:9222/private-socket-secret\n    capture_delay: 0.25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range c.FieldStatesForRevision("current-revision") {
		if state.Path != "crawler.backend_options" {
			continue
		}
		options := state.Value.(map[string]any)
		options["capture_delay"] = 0.5
		value, _ := json.Marshal(options)
		next, fields := c.PrepareDraftForRevision("current-revision", []SettingEdit{{Path: state.Path, Action: "set", Value: value}})
		if next == nil || len(fields) > 0 || next.Crawler.BackendOptions["socket"] != "ws://127.0.0.1:9222/private-socket-secret" {
			t.Fatalf("changing capture delay lost stored socket: %v", fields)
		}
		return
	}
	t.Fatal("backend options absent")
}

func TestCrawlerSocketDiagnosticRedaction(t *testing.T) {
	c := CreateDefaultConfig()
	c.Crawler.BackendOptions = map[string]any{"socket": "ws://127.0.0.1:9222/private-socket-secret", "capture_delay": 0.25}
	redacted, err := c.Redacted()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(redacted)
	if strings.Contains(string(raw), "private-socket-secret") {
		t.Fatal("diagnostic output exposed socket credential")
	}
}

func TestCrawlerSecretCollectionDrafts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, []byte("# defaults\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw := "crawler:\n  headers:\n    Authorization: private-header\n    X-Remove: old-value\n  cookies:\n    - name: session\n      value: private-cookie\n      domain: example.com\n      path: /\n"
	if err := os.WriteFile(filepath.Join(dir, "config.web.yml"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]FieldState{}
	for _, state := range c.FieldStatesForRevision("revision-one") {
		states[state.Path] = state
	}
	encoded, _ := json.Marshal(states)
	if strings.Contains(string(encoded), "private-header") || strings.Contains(string(encoded), "private-cookie") {
		t.Fatal("read exposed credentials")
	}
	headers, ok := states["crawler.headers"].Value.(map[string]any)
	if !ok || !states["crawler.headers"].Editable {
		t.Fatalf("header editor unavailable: %+v", states["crawler.headers"])
	}
	delete(headers, "x-remove")
	headers["x-new"] = map[string]any{"action": "replace", "value": "new-secret"}
	headerJSON, _ := json.Marshal(headers)
	cookieJSON, _ := json.Marshal(states["crawler.cookies"].Value)
	edits := []SettingEdit{{Path: "crawler.headers", Action: "set", Value: headerJSON}, {Path: "crawler.cookies", Action: "set", Value: cookieJSON}}
	next, fields := c.PrepareDraftForRevision("revision-one", edits)
	if next == nil || len(fields) > 0 {
		t.Fatalf("secret preservation failed: %v", fields)
	}
	if next.Crawler.Headers["authorization"] != "private-header" || next.Crawler.Headers["x-new"] != "new-secret" || len(next.Crawler.Headers) != 2 || next.Crawler.Cookies[0].Value != "private-cookie" {
		t.Fatal("collection replacement lost kept secrets or retained removed members")
	}
	if len(c.Crawler.Headers) != 2 || c.Crawler.Headers["x-new"] != "" {
		t.Fatal("draft mutated active secrets")
	}
	for _, rev := range []string{"", "revision-two"} {
		if candidate, errors := c.PrepareDraftForRevision(rev, edits); candidate != nil || len(errors) == 0 {
			t.Fatal("accepted secret references outside their revision")
		}
	}
	moved := strings.Replace(string(cookieJSON), "example.com", "other.example", 1)
	if candidate, errors := c.PrepareDraftForRevision("revision-one", []SettingEdit{{Path: "crawler.cookies", Action: "set", Value: json.RawMessage(moved)}}); candidate != nil || len(errors) == 0 {
		t.Fatal("allowed kept cookie credential to move to another domain")
	}
	for _, value := range []string{`{"authorization":{"action":"replace","value":"one"},"Authorization":{"action":"replace","value":"two"}}`, `{"authorization":"private-input"}`, `{"authorization":{"action":"keep","ref":"forged"}}`, `{"authorization":{"action":"replace","value":"private-input","extra":true}}`, `{"Bad Header":{"action":"replace","value":"private-input"}}`, `{"authorization":{"action":"replace","value":"private-input\r\nInjected: value"}}`} {
		candidate, errors := c.PrepareDraftForRevision("revision-one", []SettingEdit{{Path: "crawler.headers", Action: "set", Value: json.RawMessage(value)}})
		if candidate != nil || len(errors) == 0 {
			t.Fatalf("accepted invalid secret draft: %s", value)
		}
		if strings.Contains(fmt.Sprint(errors), "private-input") {
			t.Fatal("validation exposed a credential")
		}
	}
	for _, value := range []string{`{"authorization":{"action":"clear"}}`, `{}`} {
		candidate, errors := c.PrepareDraftForRevision("revision-one", []SettingEdit{{Path: "crawler.headers", Action: "set", Value: json.RawMessage(value)}})
		if candidate == nil || len(errors) > 0 || candidate.Crawler.Headers["authorization"] != "" {
			t.Fatalf("cannot clear headers: %v", errors)
		}
	}
}

func TestCrawlerBackendOptionDrafts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, []byte("# defaults\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	next, fields := c.PrepareDraft([]SettingEdit{
		{Path: "crawler.backend", Action: "set", Value: json.RawMessage(`"bidi"`)},
		{Path: "crawler.backend_options", Action: "set", Value: json.RawMessage(`{"capture_delay":0.25,"host":"127.0.0.1","port":9223}`)},
	})
	if next == nil || len(fields) > 0 {
		t.Fatalf("backend options rejected: %v", fields)
	}
	if next.Crawler.BackendOptions["capture_delay"] != 0.25 || next.Crawler.BackendOptions["port"] != 9223 {
		t.Fatalf("typed options lost: %v", next.Crawler.BackendOptions)
	}
	write, err := next.StageManaged()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = write.Close() }()
	if err := write.Commit(); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadForInspection(filename, true)
	if err != nil || loaded.Crawler.BackendOptions["capture_delay"] != 0.25 {
		t.Fatalf("backend options failed after restart: %v", err)
	}
	for _, value := range []string{`{"exec_path":"/bin/sh"}`, `{"args":["--unsafe"]}`, `{"unknown":true}`, `{"capture_delay":-1}`, `{"capture_delay":1e100}`, `{"port":0}`, `{"port":65536}`, `{"host":"private-input/path"}`, `{"capture_delay":"1s"}`} {
		candidate, errors := loaded.PrepareDraft([]SettingEdit{{Path: "crawler.backend_options", Action: "set", Value: json.RawMessage(value)}})
		if candidate != nil || len(errors) == 0 || errors[0].Path != "crawler.backend_options" {
			t.Errorf("accepted unsupported options %s: %v", value, errors)
		}
	}
	if candidate, errors := loaded.PrepareDraft([]SettingEdit{{Path: "crawler.backend", Action: "set", Value: json.RawMessage(`"http"`)}}); candidate != nil || len(errors) == 0 {
		t.Fatal("accepted options for the wrong backend")
	}
	cleared, fields := loaded.PrepareDraft([]SettingEdit{{Path: "crawler.backend", Action: "set", Value: json.RawMessage(`"http"`)}, {Path: "crawler.backend_options", Action: "set", Value: json.RawMessage(`{}`)}})
	if cleared == nil || len(fields) > 0 || len(cleared.Crawler.BackendOptions) != 0 {
		t.Fatalf("cannot clear options while changing backend: %v", fields)
	}
	for _, content := range []string{"crawler:\n  backend_options:\n    exec_path: /bin/sh\n", "crawler:\n  backend_options:\n    unknown: true\n"} {
		if err := os.WriteFile(filepath.Join(dir, "config.web.yml"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadForInspection(filename, true); err == nil {
			t.Fatal("managed file allowed restricted options")
		}
	}
}

func TestCrawlerDraftSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, []byte("# defaults\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	next, fields := c.PrepareDraft([]SettingEdit{
		{Path: "crawler.timeout", Action: "set", Value: json.RawMessage(`30`)},
		{Path: "crawler.delay", Action: "set", Value: json.RawMessage(`2`)},
		{Path: "crawler.no_robots", Action: "set", Value: json.RawMessage(`true`)},
		{Path: "crawler.backend", Action: "set", Value: json.RawMessage(`"chromedp"`)},
		{Path: "crawler.user_agent", Action: "set", Value: json.RawMessage(`"ArchiveBot/1.0"`)},
	})
	if next == nil || len(fields) != 0 {
		t.Fatalf("crawler draft rejected: %v", fields)
	}
	if next.Crawler.Timeout != 30 || next.Crawler.Delay != 2 || !next.Crawler.NoRobots || next.Crawler.Backend != "chromedp" || next.Crawler.UserAgent != "ArchiveBot/1.0" {
		t.Fatalf("crawler draft not applied: %+v", next.Crawler)
	}
	if c.Crawler.Timeout != 5 || c.Crawler.Backend != "http" {
		t.Fatal("draft changed active configuration")
	}
	for _, tc := range []struct{ path, value string }{
		{"crawler.timeout", `-1`},
		{"crawler.timeout", `9223372036854775807`},
		{"crawler.delay", `-1`},
		{"crawler.delay", `1.5`},
		{"crawler.backend", `"unknown"`},
		{"crawler.user_agent", `"private-input\r\nInjected: value"`},
		{"crawler", `{"backend_options":{"exec_path":"/bin/sh"}}`},
	} {
		candidate, errors := c.PrepareDraft([]SettingEdit{{Path: tc.path, Action: "set", Value: json.RawMessage(tc.value)}})
		if candidate != nil || len(errors) == 0 || errors[0].Path != tc.path {
			t.Errorf("accepted %s: %v", tc.path, errors)
		}
		if strings.Contains(fmt.Sprint(errors), "private-input") {
			t.Fatal("validation disclosed a value")
		}
	}
}

func TestIndexerDraftSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTER_DATA_DIR", dir)
	filename := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(filename, []byte("# indexer settings inherit defaults\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := LoadForInspection(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	next, fields := c.PrepareDraft([]SettingEdit{
		{Path: "indexer.detect_languages", Action: "set", Value: json.RawMessage(`false`)},
		{Path: "indexer.keep_stopwords", Action: "set", Value: json.RawMessage(`true`)},
		{Path: "indexer.languages", Action: "set", Value: json.RawMessage(`["en", "hu"]`)},
		{Path: "indexer.language_detection_accuracy", Action: "set", Value: json.RawMessage(`"high"`)},
		{Path: "indexer.max_file_size_mb", Action: "set", Value: json.RawMessage(`8`)},
	})
	if next == nil || len(fields) != 0 {
		t.Fatalf("indexer draft rejected: %v", fields)
	}
	if next.Indexer.DetectLanguages || !next.Indexer.KeepStopwords || next.Indexer.MaxFileSize != 8 || next.Indexer.LanguageDetectionAccuracy != "high" || !reflect.DeepEqual(next.Indexer.Languages, []string{"en", "hu"}) {
		t.Fatalf("indexer selections not composed: %+v", next.Indexer)
	}
	for _, tc := range []struct{ path, value string }{
		{"indexer.max_file_size_mb", "0"},
		{"indexer.max_file_size_mb", "8796093022208"},
		{"indexer.languages", `["en"]`},
		{"indexer.languages", `["en", "en"]`},
		{"indexer.languages", `["en", "unknown-language"]`},
		{"indexer.language_detection_accuracy", `"invalid"`},
		{"indexer.pdf_parser.command", `"executable"`},
		{"indexer", `{"pdf_parser":{"command":"executable"}}`},
	} {
		candidate, errors := c.PrepareDraft([]SettingEdit{{Path: tc.path, Action: "set", Value: json.RawMessage(tc.value)}})
		if candidate != nil || len(errors) == 0 || errors[0].Path != tc.path {
			t.Errorf("expected field error for %s: %v", tc.path, errors)
		}
	}
	cleared, errors := next.PrepareDraft([]SettingEdit{{Path: "indexer.languages", Action: "set", Value: json.RawMessage(`[]`)}})
	if cleared == nil || len(errors) != 0 || len(cleared.Indexer.Languages) != 0 || !cleared.Source("indexer.languages").WebSelected {
		t.Fatalf("cannot explicitly clear language restriction: %v", errors)
	}
}

func TestValidateFieldsReportsSafeLanguageErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		accuracy  string
		languages []string
		path      string
	}{
		{"invalid accuracy", "private-input", []string{"en", "de"}, "indexer.language_detection_accuracy"},
		{"unsupported language", "high", []string{"en", "private-input"}, "indexer.languages"},
		{"duplicate aliases", "", []string{"nb", "no"}, "indexer.languages"},
		{"accuracy checked first", "private-input", []string{"private-input"}, "indexer.language_detection_accuracy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := CreateDefaultConfig()
			c.Indexer.LanguageDetectionAccuracy = tc.accuracy
			c.Indexer.Languages = tc.languages
			fields := c.ValidateFields()
			if len(fields) != 1 || fields[0].Path != tc.path || fields[0].Code != "invalid_configuration" || fields[0].Message == "" {
				t.Fatalf("field errors = %+v, want one validation error for %s", fields, tc.path)
			}
			encoded, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "private-input") || strings.Contains(fields[0].Error(), "private-input") {
				t.Fatal("submitted language settings leaked into field errors")
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
	for _, path := range []string{"app.color_scheme", "app.snippet_length", "server.max_batch_body_size", "hotkeys.web", "sensitive_content_patterns", "semantic_search.dimensions"} {
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
