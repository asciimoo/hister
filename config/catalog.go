// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"
)

// SettingSchema describes a control independently of any stored value. Both
// collection and sensitivity must be explicit, including for ordinary scalars.
type SettingSchema struct {
	Type        string         `json:"type"`
	Control     string         `json:"control"`
	Collection  string         `json:"collection"`
	Sensitivity string         `json:"sensitivity"`
	Min         *int           `json:"min,omitempty"`
	Max         *int           `json:"max,omitempty"`
	MaxLength   int            `json:"max_length,omitempty"`
	MaxItems    int            `json:"max_items,omitempty"`
	Choices     []string       `json:"choices,omitempty"`
	Items       *SettingSchema `json:"items,omitempty"`
}

// FieldMetadata contains static descriptions only. Runtime values and sources
// belong to FieldState, so secret data cannot accidentally become a default.
type FieldMetadata struct {
	Path                 string        `json:"path"`
	Category             string        `json:"category"`
	Group                string        `json:"group,omitempty"`
	Label                string        `json:"label"`
	Description          string        `json:"description"`
	Schema               SettingSchema `json:"schema"`
	Default              any           `json:"default,omitempty"`
	Effect               string        `json:"effect"`
	ReadOnlyReason       string        `json:"read_only_reason,omitempty"`
	RequiresConfirmation bool          `json:"requires_confirmation"`
}

type FieldState struct {
	Path          string      `json:"path"`
	Value         any         `json:"value,omitempty"`
	Source        ValueSource `json:"source"`
	Editable      bool        `json:"editable"`
	Reason        string      `json:"reason,omitempty"`
	SecretPresent bool        `json:"secret_present"`
}

// FieldStates contains only described values and safe displays of secrets.
// It does not expose the main configuration or managed selections wholesale.
func (c *Config) FieldStates(fields ...FieldMetadata) []FieldState {
	if len(fields) == 0 {
		fields = FieldCatalog()
	}
	var states []FieldState
	for _, field := range fields {
		source := c.fieldSource(field.Path)
		state := FieldState{Path: field.Path, Value: settingValue(c, field.Path), Source: source, Editable: !source.Locked && field.ReadOnlyReason == "", Reason: field.ReadOnlyReason}
		if source.Locked {
			state.Reason = source.Reason
		}
		switch field.Schema.Sensitivity {
		case "secret", "url":
			// URLs can carry credentials in paths as well as userinfo and query
			// parameters. Keep the entire value private until replacement controls exist.
			state.SecretPresent = settingHasValue(state.Value)
			state.Value = nil
		case "none":
		default:
			state.SecretPresent = settingHasValue(state.Value)
			state.Value = nil
			state.Editable = false
		}
		states = append(states, state)
	}
	return states
}

// Structural maps have independently sourced members. Summarize them for the
// read only collection entry without changing any member's source or lock.
func (c *Config) fieldSource(path string) ValueSource {
	source := c.Source(path)
	if sourceSettingPath(path) != "" {
		return source
	}
	for child := range c.sources {
		if !strings.HasPrefix(child, path+".") {
			continue
		}
		member := c.Source(child)
		if source.Kind == "default" {
			source = member
			continue
		}
		if source.Kind != member.Kind || source.Name != member.Name {
			source.Kind, source.Name = "mixed", ""
		}
		source.Locked = source.Locked || member.Locked
		source.WebSelected = source.WebSelected || member.WebSelected
	}
	if source.Kind == "mixed" {
		source.Reason = "Members are set by multiple sources."
	} else if source.Kind != "default" {
		source.Reason = "Set by " + source.Kind + ": " + source.Name
		if source.Locked && source.WebSelected {
			source.Reason += "; overrides stored web selections"
		}
	}
	return source
}

type FieldError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e FieldError) Error() string { return e.Path + ": " + e.Message }

// SettingEdit expresses intent without accepting complete configuration objects.
// Ordinary fields accept set and reset. Secret actions belong to later categories.
type SettingEdit struct {
	Path   string          `json:"path"`
	Action string          `json:"action"`
	Value  json.RawMessage `json:"value,omitempty"`
}

// PrepareDraft constructs a detached candidate from a loaded configuration.
// It never reads files, changes the process environment, or opens runtime resources.
// Additional validators belong to extension owners and must also be side effect free.
func (c *Config) PrepareDraft(edits []SettingEdit, validators ...func(*Config) []FieldError) (*Config, []FieldError) {
	if c.composition == nil {
		return nil, []FieldError{{Code: "unavailable", Message: "Load the configuration before preparing a draft."}}
	}
	fields := make(map[string]FieldMetadata)
	for _, field := range FieldCatalog() {
		fields[field.Path] = field
	}
	selections := maps.Clone(c.composition.managed)
	if selections == nil {
		selections = make(map[string]any)
	}
	seen := make(map[string]bool)
	var result []FieldError
	for _, edit := range edits {
		reject := func(code, message string) {
			result = append(result, FieldError{Path: edit.Path, Code: code, Message: message})
		}
		field, ok := fields[edit.Path]
		if !ok {
			reject("unknown_field", "This setting is not available for editing.")
			continue
		}
		if seen[edit.Path] {
			reject("duplicate", "Specify each setting only once.")
			continue
		}
		seen[edit.Path] = true
		if field.ReadOnlyReason != "" {
			reject("read_only", field.ReadOnlyReason)
			continue
		}
		if source := c.Source(edit.Path); source.Locked {
			reject("locked", "This setting is controlled by an authoritative source.")
			continue
		}
		switch edit.Action {
		case "reset":
			if len(edit.Value) != 0 {
				reject("invalid_action", "Reset must not include a value.")
				continue
			}
			delete(selections, edit.Path)
		case "set":
			var value any
			decoder := json.NewDecoder(bytes.NewReader(edit.Value))
			decoder.UseNumber()
			if err := decoder.Decode(&value); err != nil {
				reject("invalid_value", "Supply one valid JSON value.")
				continue
			}
			var extra any
			if err := decoder.Decode(&extra); err != io.EOF {
				reject("invalid_value", "Supply one valid JSON value.")
				continue
			}
			normalized, message := field.Schema.ValidateValue(value)
			if message != "" {
				reject("invalid_value", message)
				continue
			}
			selections[edit.Path] = normalized
		default:
			reject("invalid_action", "Use set or reset for this setting.")
		}
	}
	if len(result) > 0 {
		return nil, result
	}
	next, err := c.composeDraft(selections)
	if err != nil {
		return nil, []FieldError{{Code: "invalid_configuration", Message: "Cannot compose the candidate configuration."}}
	}
	validation := next.ValidateFields()
	for _, validate := range validators {
		validation = append(validation, validate(next)...)
	}
	if len(validation) > 0 {
		return nil, validation
	}
	// Validate also normalizes the candidate's base URL for subsequent runtime
	// preparation. ValidateFields leaves its argument untouched.
	if err := next.Validate(); err != nil {
		return nil, []FieldError{{Code: "invalid_configuration", Message: "The complete configuration is invalid."}}
	}
	return next, nil
}

// FieldCatalog explicitly describes every setting in the main YAML config.
// Types and safe defaults come from Config and CreateDefaultConfig. Collections
// are whole settings; extractor option metadata belongs to the extractor registry.
// Categories without editing support have explicit, redacted file managed entries.
// Declaration order defines the display order of groups and their fields.
func FieldCatalog() []FieldMetadata {
	const (
		groupAppearance            = "Appearance"
		groupSearchBehavior        = "Search behavior"
		groupAccess                = "Access"
		groupLoggingAndDiagnostics = "Logging and diagnostics"
		groupStorage               = "Storage"
		groupConnection            = "Connection"
		groupAuthentication        = "Authentication"
		groupRequestLimits         = "Request limits"
		groupMonitoring            = "Monitoring"
		groupLanguageAndText       = "Language and text"
		groupLocalFiles            = "Local files"
		groupPDFExtraction         = "PDF extraction"
		groupCrawlBehavior         = "Crawl behavior"
		groupPageFetching          = "Page fetching"
		groupRequests              = "Requests"
		groupEmbeddingService      = "Embedding service"
		groupDocumentChunks        = "Document chunks"
		groupDocumentProcessing    = "Document processing"
	)
	minSnippet, maxSnippet := 1, 10000
	fields := []FieldMetadata{
		{
			Path:        "app.title",
			Group:       groupAppearance,
			Label:       "Title",
			Description: "Name displayed in the web interface.",
			Schema:      SettingSchema{Sensitivity: "none", MaxLength: 256},
		},
		{
			Path:        "app.subtitle",
			Group:       groupAppearance,
			Label:       "Subtitle",
			Description: "Text displayed below the title.",
			Schema:      SettingSchema{Sensitivity: "none", MaxLength: 1024},
		},
		{
			Path:        "app.color_scheme",
			Group:       groupAppearance,
			Label:       "Color scheme",
			Description: "Use the system preference or choose a light or dark appearance.",
			Schema:      SettingSchema{Sensitivity: "none", Choices: []string{"automatic", "light", "dark"}},
		},
		{
			Path:           "app.display_extractor_config",
			Group:          groupAppearance,
			Label:          "Display extractor configuration",
			Description:    "Display extractor options in the web interface.",
			Schema:         SettingSchema{Sensitivity: "none"},
			ReadOnlyReason: "Changing option visibility is not supported here.",
		},
		{
			Path:        "app.snippet_length",
			Group:       groupSearchBehavior,
			Label:       "Snippet length",
			Description: "Maximum number of characters in a search result snippet.",
			Schema:      SettingSchema{Sensitivity: "none", Min: &minSnippet, Max: &maxSnippet},
		},
		{
			Path:           "app.search_url",
			Group:          groupSearchBehavior,
			Label:          "Search engine URL",
			Description:    "Destination for searches outside the index.",
			Schema:         SettingSchema{Sensitivity: "url"},
			ReadOnlyReason: "Manage search engine URLs in the main configuration.",
		},
		{
			Path:        "app.open_results_on_new_tab",
			Group:       groupSearchBehavior,
			Label:       "Open results in a new tab",
			Description: "Open search results in a separate browser tab.",
			Schema:      SettingSchema{Sensitivity: "none"},
		},
		{
			Path:        "app.show_suggestions",
			Group:       groupSearchBehavior,
			Label:       "Show suggestions",
			Description: "Suggest terms while typing a search.",
			Schema:      SettingSchema{Sensitivity: "none"},
		},
		{
			Path:        "app.redirect_on_no_results",
			Group:       groupSearchBehavior,
			Label:       "Redirect empty searches",
			Description: "Open the search engine when no indexed results match.",
			Schema:      SettingSchema{Sensitivity: "none"},
		},
		{
			Path:        "app.disable_previews",
			Group:       groupSearchBehavior,
			Label:       "Disable previews",
			Description: "Hide document previews in search results.",
			Schema:      SettingSchema{Sensitivity: "none"},
		},
		{
			Path:                 "app.access_token",
			Group:                groupAccess,
			Label:                "Access token",
			Description:          "Credential used to access the instance.",
			Schema:               SettingSchema{Sensitivity: "secret"},
			ReadOnlyReason:       "Authentication changes require the access confirmation flow.",
			RequiresConfirmation: true,
		},
		{
			Path:                 "app.user_handling",
			Group:                groupAccess,
			Label:                "User handling",
			Description:          "Require individual user accounts.",
			Schema:               SettingSchema{Sensitivity: "none"},
			ReadOnlyReason:       "Authentication changes require the access confirmation flow.",
			RequiresConfirmation: true,
		},
		{
			Path:                 "app.public",
			Group:                groupAccess,
			Label:                "Public access",
			Description:          "Allow anonymous visitors to search.",
			Schema:               SettingSchema{Sensitivity: "none"},
			ReadOnlyReason:       "Access changes require the access confirmation flow.",
			RequiresConfirmation: true,
		},
		{
			Path:        "app.log_level",
			Group:       groupLoggingAndDiagnostics,
			Label:       "Log level",
			Description: "Choose how much detail the server writes to its log. An empty selection uses the default level.",
			Schema:      SettingSchema{Sensitivity: "none", Choices: []string{"", "error", "err", "warning", "warn", "info", "debug", "trace"}},
		},
		{
			Path:           "app.log_file",
			Group:          groupLoggingAndDiagnostics,
			Label:          "Log file",
			Description:    "File receiving server log output.",
			Schema:         SettingSchema{Sensitivity: "none"},
			ReadOnlyReason: "Storage locations are file managed.",
		},
		{
			Path:        "app.log_format",
			Group:       groupLoggingAndDiagnostics,
			Label:       "Log format",
			Description: "Choose plain text or JSON logs. An empty selection uses the default format.",
			Schema:      SettingSchema{Sensitivity: "none", Choices: []string{"", "text", "json"}},
		},
		{
			Path:           "app.debug_sql",
			Group:          groupLoggingAndDiagnostics,
			Label:          "SQL debugging",
			Description:    "Include database queries in diagnostic logs.",
			Schema:         SettingSchema{Sensitivity: "none"},
			ReadOnlyReason: "Manage diagnostic exposure in the main configuration.",
		},
		{
			Path:           "app.profiler",
			Group:          groupLoggingAndDiagnostics,
			Label:          "Profiler",
			Description:    "Enable server profiling.",
			Schema:         SettingSchema{Sensitivity: "none"},
			ReadOnlyReason: "Manage diagnostic exposure in the main configuration.",
		},
		{
			Path:           "app.directory",
			Group:          groupStorage,
			Label:          "Data directory",
			Description:    "Directory containing the instance's stored data.",
			Schema:         SettingSchema{Sensitivity: "none"},
			ReadOnlyReason: "Storage locations are file managed.",
		},
		{
			Path:                 "server.address",
			Group:                groupConnection,
			Label:                "Listen address",
			Description:          "Address where the server accepts connections.",
			Schema:               SettingSchema{Sensitivity: "none"},
			ReadOnlyReason:       "Connection changes require confirmation and are not available in this editor yet.",
			RequiresConfirmation: true,
		},
		fileManagedField("server.base_url", groupConnection, "Base URL", "Public URL used to reach this instance."),
		fileManagedField("server.proxy_auth_header", groupAuthentication, "Proxy authentication header", "Header identifying users authenticated by a trusted proxy."),
		fileManagedField("server.oauth", groupAuthentication, "OAuth providers", "Providers used to sign in with external accounts."),
		fileManagedField("server.oauth_only", groupAuthentication, "Require OAuth", "Allow sign in only through an OAuth provider."),
		fileManagedField("server.database", groupStorage, "Database", "SQLite filename or PostgreSQL connection settings."),
		fileManagedField("server.max_batch_body_size", groupRequestLimits, "Maximum batch size", "Maximum batch request body size in MiB."),
		fileManagedField("server.metrics", groupMonitoring, "Metrics", "Expose server metrics."),
		fileManagedField("server.expose_version", groupMonitoring, "Expose version", "Include the server version in public configuration."),
		fileManagedField("indexer.detect_languages", groupLanguageAndText, "Detect languages", "Detect document languages when indexing."),
		fileManagedField("indexer.languages", groupLanguageAndText, "Languages", "Languages considered during language detection."),
		fileManagedField("indexer.language_detection_accuracy", groupLanguageAndText, "Language detection accuracy", "Accuracy and memory usage of language detection."),
		fileManagedField("indexer.keep_stopwords", groupLanguageAndText, "Keep stopwords", "Keep common words in the search index."),
		fileManagedField("indexer.directories", groupLocalFiles, "Watched directories", "Directories and matching rules for indexing local files."),
		fileManagedField("indexer.max_file_size_mb", groupLocalFiles, "Maximum file size", "Maximum size of indexed files in MiB."),
		fileManagedField("indexer.pdf_parser.command", groupPDFExtraction, "PDF parser command", "Optional executable used to extract PDF text."),
		fileManagedField("indexer.pdf_parser.args", groupPDFExtraction, "PDF parser arguments", "Arguments passed to the PDF parser command."),
		fileManagedField("indexer.pdf_parser.timeout", groupPDFExtraction, "PDF parser timeout", "Maximum time in seconds for parsing a PDF."),
		fileManagedField("crawler.timeout", groupCrawlBehavior, "Crawler timeout", "Maximum time in seconds for a crawl request."),
		fileManagedField("crawler.delay", groupCrawlBehavior, "Crawler delay", "Delay in seconds between crawl requests."),
		fileManagedField("crawler.no_robots", groupCrawlBehavior, "Ignore robots rules", "Disable robots.txt checks during crawling."),
		fileManagedField("crawler.backend", groupPageFetching, "Crawler backend", "Backend used to fetch web pages."),
		fileManagedField("crawler.backend_options", groupPageFetching, "Crawler backend options", "Additional options supplied to the selected backend."),
		fileManagedField("crawler.proxy", groupRequests, "Crawler proxy", "Proxy used for crawl requests."),
		fileManagedField("crawler.user_agent", groupRequests, "Crawler user agent", "User agent sent with crawl requests."),
		fileManagedField("crawler.headers", groupRequests, "Crawler headers", "Additional HTTP headers sent with crawl requests."),
		fileManagedField("crawler.cookies", groupRequests, "Crawler cookies", "Cookies sent with crawl requests."),
		fileManagedField("semantic_search.enable", groupSearchBehavior, "Semantic search", "Enable search by meaning using document embeddings."),
		fileManagedField("semantic_search.similarity_threshold", groupSearchBehavior, "Similarity threshold", "Minimum similarity required for a semantic result."),
		fileManagedField("semantic_search.result_limit", groupSearchBehavior, "Semantic result limit", "Maximum number of semantic results to consider."),
		fileManagedField("semantic_search.semantic_weight", groupSearchBehavior, "Semantic weight", "Weight of semantic scores when combining search results."),
		fileManagedField("semantic_search.query_prefix", groupSearchBehavior, "Query prefix", "Text prepended to queries before generating embeddings."),
		fileManagedField("semantic_search.query_embedding_timeout", groupSearchBehavior, "Query embedding timeout", "Maximum time in seconds for a query embedding request."),
		fileManagedField("semantic_search.max_query_embedding_concurrency", groupSearchBehavior, "Query embedding concurrency", "Maximum number of concurrent query embedding requests."),
		fileManagedField("semantic_search.embedding_endpoint", groupEmbeddingService, "Embedding endpoint", "URL of the service that generates embeddings."),
		fileManagedField("semantic_search.embedding_model", groupEmbeddingService, "Embedding model", "Model used to generate embeddings."),
		fileManagedField("semantic_search.api_key", groupEmbeddingService, "Embedding API key", "Credential used to access the embedding service."),
		fileManagedField("semantic_search.headers", groupEmbeddingService, "Embedding headers", "Additional HTTP headers sent to the embedding service."),
		fileManagedField("semantic_search.dimensions", groupEmbeddingService, "Embedding dimensions", "Number of dimensions in each embedding."),
		fileManagedField("semantic_search.max_context_length", groupDocumentChunks, "Maximum context length", "Maximum number of tokens in a document chunk."),
		fileManagedField("semantic_search.chunk_overlap", groupDocumentChunks, "Chunk overlap", "Number of tokens shared by adjacent document chunks."),
		fileManagedField("semantic_search.document_prefix", groupDocumentChunks, "Document prefix", "Text prepended to document chunks before generating embeddings."),
		fileManagedField("semantic_search.embedding_timeout", groupDocumentProcessing, "Embedding timeout", "Maximum time in seconds for document embedding requests."),
		fileManagedField("semantic_search.max_embedding_batch_size", groupDocumentProcessing, "Embedding batch size", "Maximum number of chunks per embedding request."),
		fileManagedField("semantic_search.max_embedding_concurrency", groupDocumentProcessing, "Embedding concurrency", "Maximum number of concurrent document embedding requests."),
		fileManagedField("hotkeys.web", "", "Web keyboard shortcuts", "Keyboard shortcuts for the web interface."),
		fileManagedField("sensitive_content_patterns", "", "Sensitive content patterns", "Patterns used to detect sensitive content before indexing."),
		fileManagedField("extractors", "", "Extractors", "Extractor enable switches and options described by the extractor registry."),
	}
	defaults := CreateDefaultConfig()
	for i := range fields {
		field := &fields[i]
		field.Category, _, _ = strings.Cut(field.Path, ".")
		if field.Effect == "" {
			field.Effect = "runtime"
		}
		if field.Schema.Type == "unknown" {
			continue
		}
		value := settingValue(defaults, field.Path)
		switch value.(type) {
		case string:
			field.Schema.Type, field.Schema.Control = "string", "text"
		case bool:
			field.Schema.Type, field.Schema.Control = "boolean", "switch"
		case int:
			field.Schema.Type, field.Schema.Control = "integer", "number"
		}
		field.Schema.Collection = "none"
		if len(field.Schema.Choices) > 0 {
			field.Schema.Control = "select"
		}
		switch field.Schema.Sensitivity {
		case "secret", "url":
			field.Schema.Control = field.Schema.Sensitivity
		}
		if err := field.Schema.ValidateDefinition(); err != nil {
			field.Schema = SettingSchema{Type: "unknown", Control: "none", Collection: "unknown", Sensitivity: "unknown"}
			field.ReadOnlyReason = "This setting has incomplete or unsupported editor metadata."
			continue
		}
		if field.Schema.Sensitivity == "none" {
			field.Default = value
		}
	}
	return fields
}

// A placeholder records coverage without guessing controls or disclosing values.
// Replace it with a full definition when the category supports safe editing.
func fileManagedField(path, group, label, description string) FieldMetadata {
	return FieldMetadata{
		Path: path, Group: group, Label: label, Description: description,
		Schema:         SettingSchema{Type: "unknown", Control: "none", Collection: "unknown", Sensitivity: "unknown"},
		Effect:         "unknown",
		ReadOnlyReason: "Manage this setting in the main configuration.",
	}
}

// ValidateValue returns a normalized value or a message containing no supplied
// values. Callers attach their own stable field path to the message.
func (s SettingSchema) ValidateValue(value any) (any, string) {
	switch s.Type {
	case "string":
		v, ok := value.(string)
		if !ok {
			return nil, "Must be a string."
		}
		if !utf8.ValidString(v) || strings.ContainsRune(v, '\x00') {
			return nil, "Must be valid text without NUL characters."
		}
		if s.MaxLength > 0 && utf8.RuneCountInString(v) > s.MaxLength {
			return nil, fmt.Sprintf("Must contain at most %d characters.", s.MaxLength)
		}
		if len(s.Choices) > 0 && !slices.Contains(s.Choices, v) {
			return nil, "Choose one of the listed values."
		}
		if s.Control == "url" && v != "" {
			parsed, err := url.Parse(v)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Fragment != "" {
				return nil, "Use an HTTP or HTTPS URL without a fragment."
			}
		}
		return v, ""
	case "boolean":
		if v, ok := value.(bool); ok {
			return v, ""
		}
		return nil, "Must be a boolean."
	case "integer":
		var n int
		switch v := value.(type) {
		case int:
			n = v
		case json.Number:
			parsed, err := v.Int64()
			if err != nil || int64(int(parsed)) != parsed {
				return nil, "Must be a supported integer."
			}
			n = int(parsed)
		default:
			return nil, "Must be an integer."
		}
		if s.Min != nil && n < *s.Min {
			return nil, fmt.Sprintf("Must be at least %d.", *s.Min)
		}
		if s.Max != nil && n > *s.Max {
			return nil, fmt.Sprintf("Must be at most %d.", *s.Max)
		}
		return n, ""
	case "list":
		v := reflect.ValueOf(value)
		if !v.IsValid() || (v.Kind() != reflect.Slice && v.Kind() != reflect.Array) || s.Items == nil {
			return nil, "Must be a list with described items."
		}
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil, "Use an empty list to clear this setting."
		}
		if s.MaxItems > 0 && v.Len() > s.MaxItems {
			return nil, fmt.Sprintf("Must contain at most %d items.", s.MaxItems)
		}
		items := make([]any, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			item, message := s.Items.ValidateValue(v.Index(i).Interface())
			if message != "" {
				return nil, "Invalid list item. " + message
			}
			items = append(items, item)
		}
		return items, ""
	case "dictionary":
		v := reflect.ValueOf(value)
		if !v.IsValid() || v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String || v.IsNil() || s.Items == nil {
			return nil, "Must be a dictionary with described values."
		}
		if s.MaxItems > 0 && v.Len() > s.MaxItems {
			return nil, fmt.Sprintf("Must contain at most %d entries.", s.MaxItems)
		}
		items := make(map[string]any, v.Len())
		for iter := v.MapRange(); iter.Next(); {
			item, message := s.Items.ValidateValue(iter.Value().Interface())
			if message != "" {
				return nil, "Invalid dictionary value. " + message
			}
			items[iter.Key().String()] = item
		}
		return items, ""
	default:
		return nil, "This value type is not supported for editing."
	}
}

// ValidateDefinition rejects incomplete metadata rather than deriving controls
// or secret policies from a current value. Extension owners call it on registration.
func (s SettingSchema) ValidateDefinition() error {
	invalid := func() error { return fmt.Errorf("incomplete or inconsistent setting schema") }
	if s.Sensitivity != "none" && s.Sensitivity != "secret" && s.Sensitivity != "url" {
		return invalid()
	}
	if s.Min != nil && s.Max != nil && *s.Min > *s.Max {
		return invalid()
	}
	if s.MaxLength < 0 || s.MaxItems < 0 {
		return invalid()
	}
	if s.Type != "integer" && (s.Min != nil || s.Max != nil) {
		return invalid()
	}
	if s.Type != "string" && (s.MaxLength != 0 || len(s.Choices) != 0) {
		return invalid()
	}
	switch s.Type {
	case "string":
		if s.Control != "text" && s.Control != "select" && s.Control != "url" && s.Control != "secret" {
			return invalid()
		}
		if s.Control == "select" && len(s.Choices) == 0 {
			return invalid()
		}
		if len(s.Choices) > 0 && s.Control != "select" {
			return invalid()
		}
		if (s.Control == "secret" && s.Sensitivity != "secret") || (s.Control == "url" && s.Sensitivity != "url") {
			return invalid()
		}
		if s.Sensitivity == "secret" && s.Control != "secret" {
			return invalid()
		}
		if s.Sensitivity == "url" && s.Control != "url" {
			return invalid()
		}
	case "boolean":
		if s.Control != "switch" || s.Sensitivity != "none" {
			return invalid()
		}
	case "integer":
		if s.Control != "number" || s.Sensitivity != "none" {
			return invalid()
		}
	case "list", "dictionary":
		if s.Control != s.Type || s.Collection != "replace" || s.Items == nil || s.Sensitivity == "url" {
			return invalid()
		}
		if err := s.Items.ValidateDefinition(); err != nil {
			return err
		}
		if s.Items.Sensitivity != "none" && s.Sensitivity != "secret" {
			return invalid()
		}
		return nil
	default:
		return invalid()
	}
	if s.Collection != "none" || s.Items != nil || s.MaxItems != 0 {
		return invalid()
	}
	return nil
}

func settingHasValue(value any) bool {
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return false
	}
	switch v.Kind() {
	case reflect.String, reflect.Map, reflect.Slice:
		return v.Len() > 0
	default:
		return !v.IsZero()
	}
}

func settingValue(c *Config, path string) any {
	value := reflect.ValueOf(c).Elem()
	for key := range strings.SplitSeq(path, ".") {
		for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
			value = value.Elem()
		}
		if !value.IsValid() {
			return nil
		}
		if value.Kind() == reflect.Map && value.Type().Key().Kind() == reflect.String {
			value = value.MapIndex(reflect.ValueOf(key))
			continue
		}
		if value.Kind() != reflect.Struct {
			return nil
		}
		found := false
		for field := range value.Type().Fields() {
			if field.Tag.Get("mapstructure") == key {
				value = value.FieldByIndex(field.Index)
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	if !value.IsValid() {
		return nil
	}
	return value.Interface()
}

func (c *Config) validateAppFields() []FieldError {
	var result []FieldError
	for _, field := range FieldCatalog() {
		if field.ReadOnlyReason != "" {
			continue
		}
		if _, message := field.Schema.ValidateValue(settingValue(c, field.Path)); message != "" {
			result = append(result, FieldError{Path: field.Path, Code: "invalid_value", Message: message})
		}
	}
	return result
}
