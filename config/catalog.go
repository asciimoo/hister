// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/url"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/asciimoo/hister/server/languages"
)

// SettingSchema describes a control independently of any stored value. Both
// collection and sensitivity must be explicit, including for ordinary scalars.
type SettingSchema struct {
	Type         string            `json:"type"`
	Control      string            `json:"control"`
	Collection   string            `json:"collection"`
	Sensitivity  string            `json:"sensitivity"`
	Min          *int              `json:"min,omitempty"`
	Max          *int              `json:"max,omitempty"`
	MaxLength    int               `json:"max_length,omitempty"`
	MinLength    int               `json:"min_length,omitempty"`
	MaxItems     int               `json:"max_items,omitempty"`
	Choices      []string          `json:"choices,omitempty"`
	ChoiceLabels map[string]string `json:"choice_labels,omitempty"`
	KeyLabel     string            `json:"key_label,omitempty"`
	ValueLabel   string            `json:"value_label,omitempty"`
	Items        *SettingSchema    `json:"items,omitempty"`
	Properties   []SettingProperty `json:"properties,omitempty"`
	SecretScope  []string          `json:"secret_scope,omitempty"`
}

// SettingProperty describes one member of a structured collection item.
type SettingProperty struct {
	Name        string        `json:"name"`
	Label       string        `json:"label"`
	Description string        `json:"description"`
	Required    bool          `json:"required"`
	Schema      SettingSchema `json:"schema"`
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

// RegisterExtractorSettings supplies extension schemas during package
// initialization, before loading configuration. Owners retain their catalog and
// defaults; config uses these descriptors only to validate web selections.
// Undescribed and file managed options are deliberately excluded.
func RegisterExtractorSettings(fields []FieldMetadata) {
	for _, field := range fields {
		if field.ReadOnlyReason != "" {
			continue
		}
		parts := strings.Split(field.Path, ".")
		isEnable := len(parts) == 3 && parts[2] == "enable"
		isOption := len(parts) == 4 && parts[2] == "options" && parts[3] != ""
		if len(parts) < 3 || parts[0] != "extractors" || parts[1] == "" ||
			(!isEnable && !isOption) {
			panic("invalid extractor setting path")
		}
		if _, exists := extractorSettings[field.Path]; exists {
			panic("duplicate extractor setting")
		}
		if err := field.Schema.ValidateDefinition(); err != nil {
			panic(err)
		}
		extractorSettings[field.Path] = cloneInputValue(reflect.ValueOf(field)).Interface().(FieldMetadata)
	}
}

var extractorSettings = make(map[string]FieldMetadata)

// FieldStates contains only described values and safe displays of secrets.
// It does not expose the main configuration or managed selections wholesale.
func (c *Config) FieldStates(fields ...FieldMetadata) []FieldState {
	return c.FieldStatesForRevision("", fields...)
}

// FieldStatesForRevision supplies opaque secret references for one active revision.
func (c *Config) FieldStatesForRevision(revision string, fields ...FieldMetadata) []FieldState {
	if len(fields) == 0 {
		fields = FieldCatalog()
	}
	var states []FieldState
	for _, field := range fields {
		source := c.fieldSource(field.Path)
		state := FieldState{Path: field.Path, Value: settingValue(c, field.Path), Source: source, Editable: !source.Locked && field.ReadOnlyReason == "", Reason: field.ReadOnlyReason}
		if field.Schema.Type == "dictionary" && !settingHasValue(state.Value) {
			state.Value = map[string]any{}
		}
		if source.Locked {
			state.Reason = source.Reason
		}
		if state.Editable && (field.Schema.Sensitivity == "mixed" || field.Schema.Sensitivity == "secret" || field.Schema.Sensitivity == "url") {
			value, refs, err := c.secretSettingState(field, revision)
			state.Value, state.SecretPresent = value, len(refs) > 0
			if err != nil {
				state.Value, state.Editable, state.Reason = nil, false, "This collection contains undescribed or file managed options."
			}
			states = append(states, state)
			continue
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
// Fields accept set and reset. Secret values carry keep, replace, or clear actions.
type SettingEdit struct {
	Path   string          `json:"path"`
	Action string          `json:"action"`
	Value  json.RawMessage `json:"value,omitempty"`
}

// PrepareDraft constructs a detached candidate from a loaded configuration.
// It never reads files, changes the process environment, or opens runtime resources.
// Additional validators belong to extension owners and must also be side effect free.
func (c *Config) PrepareDraft(edits []SettingEdit, validators ...func(*Config) []FieldError) (*Config, []FieldError) {
	return c.PrepareDraftForRevision("", edits, validators...)
}

// PrepareDraftForRevision resolves kept secrets only within the active revision.
func (c *Config) PrepareDraftForRevision(revision string, edits []SettingEdit, validators ...func(*Config) []FieldError) (*Config, []FieldError) {
	if c.composition == nil {
		return nil, []FieldError{{Code: "unavailable", Message: "Load the configuration before preparing a draft."}}
	}
	fields := make(map[string]FieldMetadata)
	for _, field := range FieldCatalog() {
		fields[field.Path] = field
	}
	maps.Copy(fields, extractorSettings)
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
			decoder := json.NewDecoder(bytes.NewReader(edit.Value))
			decoder.UseNumber()
			value, err := decodeSettingValue(decoder)
			if err != nil {
				reject("invalid_value", "Supply one valid JSON value.")
				continue
			}
			var extra any
			if err := decoder.Decode(&extra); err != io.EOF {
				reject("invalid_value", "Supply one valid JSON value.")
				continue
			}
			if field.Schema.Sensitivity != "none" {
				value, err = c.resolveSettingSecrets(field, revision, value)
				if err != nil {
					reject("invalid_secret", err.Error())
					continue
				}
			}
			normalized, message := field.Schema.ValidateValue(value)
			if message != "" {
				reject("invalid_value", message)
				continue
			}
			switch edit.Path {
			case "sensitive_content_patterns":
				for name := range normalized.(map[string]any) {
					if strings.TrimSpace(name) == "" || name != strings.ToLower(name) || utf8.RuneCountInString(name) > 128 || strings.ContainsFunc(name, unicode.IsControl) {
						reject("invalid_value", "Use nonblank lowercase names of at most 128 characters without control characters.")
						break
					}
				}
			case "crawler.headers", "semantic_search.headers", "hotkeys.web":
				entries := make(map[string]string)
				for name, value := range normalized.(map[string]any) {
					entries[name] = value.(string)
				}
				var message string
				if edit.Path == "hotkeys.web" {
					// Composition normalizes map keys. Reject invalid shortcuts
					// before that can rename or merge user selections.
					if err := (Hotkeys{Web: entries}).Validate(); err != nil {
						message = "Use supported lowercase keys with at most one ctrl+, alt+, or meta+ prefix."
					}
				} else {
					message = validateHTTPHeaders(entries)
				}
				if message != "" {
					reject("invalid_value", message)
					continue
				}
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
	if seen["indexer.directories"] {
		for i, dir := range next.Indexer.Directories {
			if message := dir.validateSetting(); message != "" {
				validation = append(validation, FieldError{Path: "indexer.directories", Code: "invalid_value", Message: fmt.Sprintf("Directory %d: %s", i+1, message)})
			}
		}
	}
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

// decodeSettingValue preserves numeric types and rejects duplicate keys at every
// nesting level, including properties inside structured collection rows.
func decodeSettingValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		value := make(map[string]any)
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, errors.New("invalid object key")
			}
			if _, exists := value[name]; exists {
				return nil, errors.New("duplicate object key")
			}
			item, err := decodeSettingValue(decoder)
			if err != nil {
				return nil, err
			}
			value[name] = item
		}
		_, err := decoder.Token()
		return value, err
	case json.Delim('['):
		value := make([]any, 0)
		for decoder.More() {
			item, err := decodeSettingValue(decoder)
			if err != nil {
				return nil, err
			}
			value = append(value, item)
		}
		_, err := decoder.Token()
		return value, err
	default:
		return token, nil
	}
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
	minSeconds, maxSeconds := 0, int(min(int64(^uint(0)>>1), int64((1<<63-1)/1000000000)))
	minFileSize, maxFileSize := 1, int(min(int64(^uint(0)>>1), int64((1<<63-1)/(1024*1024))))
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
			Description: "Choose how much detail the server writes to its log.",
			Schema:      SettingSchema{Sensitivity: "none", Choices: []string{"error", "err", "warning", "warn", "info", "debug", "trace"}},
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
			Description: "Choose plain text or JSON logs.",
			Schema:      SettingSchema{Sensitivity: "none", Choices: []string{"text", "json"}},
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
		{Path: "indexer.detect_languages", Group: groupLanguageAndText, Label: "Detect languages", Description: "Detect languages for newly processed documents. Reindex to update existing documents.", Schema: SettingSchema{Sensitivity: "none"}, Effect: "reindex"},
		{Path: "indexer.languages", Group: groupLanguageAndText, Label: "Languages", Description: "Select at least two languages, or clear the selection to detect all supported languages. Reindex to update existing documents.", Schema: SettingSchema{Type: "list", Control: "list", Collection: "replace", Sensitivity: "none", Items: &SettingSchema{Type: "string", Control: "select", Collection: "none", Sensitivity: "none", Choices: languages.DetectorCodes(), ChoiceLabels: languages.DetectorNames()}}, Effect: "reindex"},
		{Path: "indexer.language_detection_accuracy", Group: groupLanguageAndText, Label: "Language detection accuracy", Description: "Low uses less memory. High improves detection accuracy. Reindex to update existing documents.", Schema: SettingSchema{Sensitivity: "none", Choices: []string{"low", "high"}}, Effect: "reindex"},
		{Path: "indexer.keep_stopwords", Group: groupLanguageAndText, Label: "Keep stopwords", Description: "Keep common words in the search index. Reindex to update existing documents.", Schema: SettingSchema{Sensitivity: "none"}, Effect: "reindex"},
		{Path: "indexer.directories", Group: groupLocalFiles, Label: "Watched directories", Description: "Server directories to scan and watch after saving. Blank owner makes files visible to everyone with search access. Removing a row stops watching but retains indexed documents.", Schema: directorySchema()},
		{Path: "indexer.max_file_size_mb", Group: groupLocalFiles, Label: "Maximum file size", Description: "Maximum file size in MiB. Applies to subsequent file processing.", Schema: SettingSchema{Sensitivity: "none", Min: &minFileSize, Max: &maxFileSize}, Effect: "runtime"},
		fileManagedField("indexer.pdf_parser.command", groupPDFExtraction, "PDF parser command", "Optional executable used to extract PDF text."),
		fileManagedField("indexer.pdf_parser.args", groupPDFExtraction, "PDF parser arguments", "Arguments passed to the PDF parser command."),
		fileManagedField("indexer.pdf_parser.timeout", groupPDFExtraction, "PDF parser timeout", "Maximum time in seconds for parsing a PDF."),
		{Path: "crawler.timeout", Group: groupCrawlBehavior, Label: "Crawler timeout", Description: "Maximum time in seconds for a crawl request. Zero uses five seconds.", Schema: SettingSchema{Sensitivity: "none", Min: &minSeconds, Max: &maxSeconds}},
		{Path: "crawler.delay", Group: groupCrawlBehavior, Label: "Crawler delay", Description: "Delay in seconds between crawl requests. Zero disables the delay.", Schema: SettingSchema{Sensitivity: "none", Min: &minSeconds, Max: &maxSeconds}},
		{Path: "crawler.no_robots", Group: groupCrawlBehavior, Label: "Ignore robots rules", Description: "Disable robots.txt checks for subsequent crawls.", Schema: SettingSchema{Sensitivity: "none"}},
		{Path: "crawler.backend", Group: groupPageFetching, Label: "Crawler backend", Description: "Backend for subsequent crawls. Browser backends require an installed or running browser.", Schema: SettingSchema{Sensitivity: "none", Choices: []string{"http", "chromedp", "bidi"}}},
		{Path: "crawler.backend_options", Group: groupPageFetching, Label: "Crawler backend options", Description: "Options replace the entire collection. Capture delay applies to browser backends. Host and port apply only to BiDi. Executable selections remain file managed.", Schema: crawlerBackendSchema()},
		{Path: "crawler.proxy", Group: groupRequests, Label: "Crawler proxy", Description: "HTTP or SOCKS5 proxy URL for subsequent crawls. Proxy credentials are not supported. Clear uses the default proxy behavior.", Schema: SettingSchema{Sensitivity: "secret", MaxLength: 4096}},
		{Path: "crawler.user_agent", Group: groupRequests, Label: "Crawler user agent", Description: "User agent for HTTP and Chrome crawls. BiDi uses its browser settings. Empty uses the crawler default.", Schema: SettingSchema{Sensitivity: "none", MaxLength: 4096}},
		{Path: "crawler.headers", Group: groupRequests, Label: "Crawler headers", Description: "Headers for HTTP and Chrome crawls. BiDi uses its browser settings. Values remain hidden. Removing a row removes that header; Clear removes all headers.", Schema: SettingSchema{Type: "dictionary", Control: "dictionary", Collection: "replace", Sensitivity: "mixed", MaxItems: 128, Items: &SettingSchema{Type: "string", Control: "secret", Collection: "none", Sensitivity: "secret", MaxLength: 16384}}},
		{Path: "crawler.cookies", Group: groupRequests, Label: "Crawler cookies", Description: "Cookies for HTTP and Chrome crawls, scoped to domains and paths. BiDi uses its browser cookies. Keep preserves a stored value. Changing its name, domain, or path requires Replace or Clear.", Schema: crawlerCookieSchema()},
		{Path: "semantic_search.enable", Group: groupSearchBehavior, Label: "Semantic search", Description: "Enable search by meaning. Reindex existing documents to generate embeddings.", Schema: SettingSchema{Sensitivity: "none"}, Effect: "reembed"},
		{Path: "semantic_search.similarity_threshold", Group: groupSearchBehavior, Label: "Similarity threshold", Description: "Minimum similarity required for a semantic result.", Schema: SettingSchema{Sensitivity: "none", Min: new(0), Max: new(1)}, Effect: "runtime"},
		{Path: "semantic_search.result_limit", Group: groupSearchBehavior, Label: "Semantic result limit", Description: "Maximum number of semantic results to consider.", Schema: SettingSchema{Sensitivity: "none", Min: new(1)}, Effect: "runtime"},
		{Path: "semantic_search.semantic_weight", Group: groupSearchBehavior, Label: "Semantic weight", Description: "Weight of semantic scores when combining results, from zero to one.", Schema: SettingSchema{Sensitivity: "none", Min: new(0), Max: new(1)}, Effect: "runtime"},
		{Path: "semantic_search.query_prefix", Group: groupSearchBehavior, Label: "Query prefix", Description: "Text prepended to queries before generating embeddings.", Schema: SettingSchema{Sensitivity: "none", MaxLength: 4096}, Effect: "runtime"},
		{Path: "semantic_search.query_embedding_timeout", Group: groupSearchBehavior, Label: "Query embedding timeout", Description: "Maximum seconds for a query embedding request. Zero uses two seconds.", Schema: SettingSchema{Sensitivity: "none", Min: new(0), Max: new(86400)}, Effect: "runtime"},
		{Path: "semantic_search.max_query_embedding_concurrency", Group: groupSearchBehavior, Label: "Query embedding concurrency", Description: "Concurrent query embedding requests. Zero uses one request.", Schema: SettingSchema{Sensitivity: "none", Min: new(0), Max: new(1024)}, Effect: "runtime"},
		{Path: "semantic_search.embedding_endpoint", Group: groupEmbeddingService, Label: "Embedding endpoint", Description: "HTTP or HTTPS URL of the embedding service.", Schema: SettingSchema{Control: "url", Sensitivity: "none", MinLength: 1, MaxLength: 4096}, Effect: "reembed"},
		{Path: "semantic_search.embedding_model", Group: groupEmbeddingService, Label: "Embedding model", Description: "Model used to generate embeddings.", Schema: SettingSchema{Sensitivity: "none", MinLength: 1, MaxLength: 1024}, Effect: "reembed"},
		{Path: "semantic_search.api_key", Group: groupEmbeddingService, Label: "Embedding API key", Description: "Credential used to access the embedding service.", Schema: SettingSchema{Sensitivity: "secret", MaxLength: 16384}, Effect: "runtime"},
		{Path: "semantic_search.headers", Group: groupEmbeddingService, Label: "Embedding headers", Description: "Additional HTTP headers. Values stay hidden; removing a header removes its value.", Schema: SettingSchema{Type: "dictionary", Control: "dictionary", Collection: "replace", Sensitivity: "mixed", MaxItems: 128, Items: &SettingSchema{Type: "string", Control: "secret", Collection: "none", Sensitivity: "secret", MaxLength: 16384}}, Effect: "runtime"},
		{Path: "semantic_search.dimensions", Group: groupEmbeddingService, Label: "Embedding dimensions", Description: "Dimensions returned by the model. PostgreSQL supports at most 2000.", Schema: SettingSchema{Sensitivity: "none", Min: new(1)}, Effect: "reembed"},
		{Path: "semantic_search.max_context_length", Group: groupDocumentChunks, Label: "Maximum context length", Description: "Maximum tokens in a document chunk.", Schema: SettingSchema{Sensitivity: "none", Min: new(1)}, Effect: "reembed"},
		{Path: "semantic_search.chunk_overlap", Group: groupDocumentChunks, Label: "Chunk overlap", Description: "Tokens shared by adjacent chunks. Must be less than the maximum context length.", Schema: SettingSchema{Sensitivity: "none", Min: new(0)}, Effect: "reembed"},
		{Path: "semantic_search.document_prefix", Group: groupDocumentChunks, Label: "Document prefix", Description: "Text prepended to document chunks before generating embeddings.", Schema: SettingSchema{Sensitivity: "none", MaxLength: 4096}, Effect: "reembed"},
		{Path: "semantic_search.embedding_timeout", Group: groupDocumentProcessing, Label: "Embedding timeout", Description: "Maximum seconds for document embedding requests. Zero uses five minutes.", Schema: SettingSchema{Sensitivity: "none", Min: new(0), Max: new(86400)}, Effect: "runtime"},
		{Path: "semantic_search.max_embedding_batch_size", Group: groupDocumentProcessing, Label: "Embedding batch size", Description: "Maximum chunks per embedding request. Zero uses eight chunks.", Schema: SettingSchema{Sensitivity: "none", Min: new(0), Max: new(4096)}, Effect: "runtime"},
		{Path: "semantic_search.max_embedding_concurrency", Group: groupDocumentProcessing, Label: "Embedding concurrency", Description: "Concurrent document embedding requests. Zero uses the default worker count.", Schema: SettingSchema{Sensitivity: "none", Min: new(0), Max: new(1024)}, Effect: "runtime"},
		{Path: "hotkeys.web", Label: "Web keyboard shortcuts", Description: "Shortcuts apply on the search page after saving. Use lowercase letters, digits, /, ?, enter, tab, arrowup, arrowdown, arrowleft, arrowright, or f1 through f12, optionally prefixed with ctrl+, alt+, or meta+. Some combinations are reserved by your browser or system. Clear removes configured shortcuts; built in navigation remains. Reset restores inherited shortcuts. Terminal shortcuts are configured separately.", Schema: webHotkeySchema()},
		{Path: "sensitive_content_patterns", Label: "Sensitive content patterns", Description: "Named regular expressions that block matching documents during subsequent indexing. Use lowercase names. Replacing this collection removes omitted patterns, including defaults. Clear disables these checks; Reset restores inherited patterns. Existing documents are unchanged.", Schema: SettingSchema{Type: "dictionary", Control: "dictionary", Collection: "replace", Sensitivity: "none", KeyLabel: "Pattern name", ValueLabel: "Regular expression", MaxItems: 256, Items: &SettingSchema{Type: "string", Control: "text", Collection: "none", Sensitivity: "none", MinLength: 1, MaxLength: 4096}}},
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
		if field.Schema.Type == "object" && value == nil {
			value = map[string]any{}
		}
		if field.Schema.Type == "object" && reflect.ValueOf(value).Kind() == reflect.Map && reflect.ValueOf(value).IsNil() {
			value = map[string]any{}
		}
		if field.Schema.Type == "list" && reflect.ValueOf(value).IsNil() {
			value = []any{}
		}
		switch value.(type) {
		case string:
			field.Schema.Type = "string"
			if field.Schema.Control == "" {
				field.Schema.Control = "text"
			}
		case bool:
			field.Schema.Type, field.Schema.Control = "boolean", "switch"
		case float64:
			field.Schema.Type, field.Schema.Control = "number", "number"
		case int, int64:
			field.Schema.Type, field.Schema.Control = "integer", "number"
		}
		if field.Schema.Collection == "" {
			field.Schema.Collection = "none"
		}
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

func crawlerBackendSchema() SettingSchema {
	zero, maxSeconds, minPort, maxPort := 0, 86400, 1, 65535
	return SettingSchema{Type: "object", Control: "object", Collection: "replace", Sensitivity: "mixed", Properties: []SettingProperty{
		{Name: "capture_delay", Label: "Capture delay", Description: "Seconds to wait after page load before capturing HTML. Browser backends only. Default: 0.", Schema: SettingSchema{Type: "number", Control: "number", Collection: "none", Sensitivity: "none", Min: &zero, Max: &maxSeconds}},
		{Name: "host", Label: "Browser host", Description: "BiDi browser host. Default: 127.0.0.1.", Schema: SettingSchema{Type: "string", Control: "text", Collection: "none", Sensitivity: "none", MaxLength: 253}},
		{Name: "port", Label: "Browser port", Description: "BiDi browser port. Default: 9222.", Schema: SettingSchema{Type: "integer", Control: "number", Collection: "none", Sensitivity: "none", Min: &minPort, Max: &maxPort}},
		{Name: "socket", Label: "Browser socket URL", Description: "Full ws:// or wss:// BiDi URL. When selected, overrides host and port. Remove to use host and port.", Schema: SettingSchema{Type: "string", Control: "secret", Collection: "none", Sensitivity: "secret", MinLength: 1, MaxLength: 4096}},
	}}
}

func crawlerCookieSchema() SettingSchema {
	text := SettingSchema{Type: "string", Control: "text", Collection: "none", Sensitivity: "none", MaxLength: 4096}
	secret := SettingSchema{Type: "string", Control: "secret", Collection: "none", Sensitivity: "secret", MaxLength: 16384}
	return SettingSchema{Type: "list", Control: "list", Collection: "replace", Sensitivity: "mixed", MaxItems: 128, Items: &SettingSchema{
		Type: "object", Control: "object", Collection: "none", Sensitivity: "mixed", SecretScope: []string{"name", "domain", "path"}, Properties: []SettingProperty{
			{Name: "name", Label: "Name", Required: true, Schema: text},
			{Name: "domain", Label: "Domain", Required: true, Schema: text},
			{Name: "path", Label: "Path", Description: "Empty uses /.", Schema: text},
			{Name: "value", Label: "Value", Required: true, Schema: secret},
		},
	}}
}

func directorySchema() SettingSchema {
	text := SettingSchema{Type: "string", Control: "text", Collection: "none", Sensitivity: "none", MaxLength: 4096}
	toggle := SettingSchema{Type: "boolean", Control: "switch", Collection: "none", Sensitivity: "none"}
	list := SettingSchema{Type: "list", Control: "list", Collection: "replace", Sensitivity: "none", MaxItems: 256, Items: &text}
	return SettingSchema{
		Type: "list", Control: "list", Collection: "replace", Sensitivity: "none", MaxItems: 256, Items: &SettingSchema{
			Type: "object", Control: "object", Collection: "none", Sensitivity: "none", Properties: []SettingProperty{
				{Name: "path", Label: "Path", Description: "Existing absolute server path or ~/ path. Directories must not overlap.", Required: true, Schema: text},
				{Name: "label", Label: "Label", Description: "Label assigned to indexed files.", Schema: text},
				{Name: "user", Label: "Owner", Description: "Existing username in multi user mode. Leave empty for global files.", Schema: text},
				{Name: "filetypes", Label: "File types", Description: "Extensions without a dot, one per line. Empty allows every file type.", Schema: list},
				{Name: "patterns", Label: "Include patterns", Description: "File name glob patterns, one per line. Empty includes all names.", Schema: list},
				{Name: "excludes", Label: "Exclude patterns", Description: "File and directory name glob patterns, one per line.", Schema: list},
				{Name: "include_hidden", Label: "Include hidden files", Description: "Also includes hidden directories and common dependency and cache folders.", Schema: toggle},
				{Name: "delete_on_remove", Label: "Delete from index when removed", Description: "Deleting or renaming a file removes its indexed document. This never deletes files from disk.", Schema: toggle},
			},
		},
	}
}

func (d *Directory) validateSetting() string {
	if d == nil || strings.TrimSpace(d.Path) == "" || (!filepath.IsAbs(d.Path) && !strings.HasPrefix(d.Path, "~/")) {
		return "Use an absolute server directory path or a ~/ path."
	}
	for _, patterns := range [][]string{d.Patterns, d.Excludes} {
		for _, pattern := range patterns {
			if _, err := filepath.Match(pattern, ""); err != nil || pattern == "" {
				return "Use valid nonempty file name glob patterns."
			}
		}
	}
	for _, ext := range d.Filetypes {
		if ext == "" || strings.ContainsAny(ext, ". /\\*?[]") {
			return "Use file extensions without dots, spaces, separators, or glob characters."
		}
	}
	return ""
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
		if utf8.RuneCountInString(v) < s.MinLength {
			return nil, fmt.Sprintf("Must contain at least %d characters.", s.MinLength)
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
	case "number":
		var n float64
		switch v := value.(type) {
		case json.Number:
			var err error
			n, err = v.Float64()
			if err != nil {
				return nil, "Must be a finite number."
			}
		case float64:
			n = v
		case int:
			n = float64(v)
		default:
			return nil, "Must be a number."
		}
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, "Must be a finite number."
		}
		if s.Min != nil && n < float64(*s.Min) {
			return nil, fmt.Sprintf("Must be at least %d.", *s.Min)
		}
		if s.Max != nil && n > float64(*s.Max) {
			return nil, fmt.Sprintf("Must be at most %d.", *s.Max)
		}
		return n, ""
	case "integer":
		var n int
		switch v := value.(type) {
		case int:
			n = v
		case int64:
			if int64(int(v)) != v {
				return nil, "Must be a supported integer."
			}
			n = int(v)
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
	case "object":
		v, ok := value.(map[string]any)
		if !ok || v == nil {
			return nil, "Must be an object with described properties."
		}
		properties := make(map[string]SettingProperty, len(s.Properties))
		for _, property := range s.Properties {
			properties[property.Name] = property
		}
		result := make(map[string]any, len(v))
		for name, value := range v {
			property, ok := properties[name]
			if !ok {
				return nil, "Unknown object property."
			}
			item, message := property.Schema.ValidateValue(value)
			if message != "" {
				return nil, property.Label + ": " + message
			}
			result[name] = item
		}
		for _, property := range s.Properties {
			if _, ok := result[property.Name]; !ok && property.Required {
				return nil, property.Label + " is required."
			}
		}
		return result, ""
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
				return nil, fmt.Sprintf("Item %d: %s", i+1, message)
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
	if s.Sensitivity != "none" && s.Sensitivity != "secret" && s.Sensitivity != "url" && s.Sensitivity != "mixed" {
		return invalid()
	}
	if s.Min != nil && s.Max != nil && *s.Min > *s.Max {
		return invalid()
	}
	if s.MaxLength < 0 || s.MinLength < 0 || (s.MaxLength > 0 && s.MinLength > s.MaxLength) || s.MaxItems < 0 {
		return invalid()
	}
	if s.Type != "integer" && s.Type != "number" && (s.Min != nil || s.Max != nil) {
		return invalid()
	}
	if s.Type != "string" && (s.MaxLength != 0 || s.MinLength != 0 || len(s.Choices) != 0) {
		return invalid()
	}
	if s.Type != "object" && (len(s.Properties) != 0 || len(s.SecretScope) != 0) {
		return invalid()
	}
	switch s.Type {
	case "string":
		if s.Control != "text" && s.Control != "select" && s.Control != "url" && s.Control != "secret" {
			return invalid()
		}
		if s.Sensitivity == "mixed" {
			return invalid()
		}
		if s.Control == "select" && len(s.Choices) == 0 {
			return invalid()
		}
		if len(s.Choices) > 0 && s.Control != "select" {
			return invalid()
		}
		if (s.Control == "secret" && s.Sensitivity != "secret") || (s.Control == "url" && s.Sensitivity != "url" && s.Sensitivity != "none") {
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
	case "integer", "number":
		if s.Control != "number" || s.Sensitivity != "none" {
			return invalid()
		}
	case "object":
		if s.Control != "object" || (s.Sensitivity != "none" && s.Sensitivity != "mixed") || len(s.Properties) == 0 {
			return invalid()
		}
		seen := make(map[string]bool)
		for _, property := range s.Properties {
			if property.Name == "" || property.Label == "" || seen[property.Name] || (property.Schema.Sensitivity != "none" && s.Sensitivity != "mixed") {
				return invalid()
			}
			if err := property.Schema.ValidateDefinition(); err != nil {
				return err
			}
			seen[property.Name] = true
		}
		for _, name := range s.SecretScope {
			index := slices.IndexFunc(s.Properties, func(p SettingProperty) bool { return p.Name == name })
			if index < 0 || s.Properties[index].Schema.Sensitivity != "none" || s.Properties[index].Schema.Type != "string" {
				return invalid()
			}
		}
	case "list", "dictionary":
		if s.Control != s.Type || s.Collection != "replace" || s.Items == nil || s.Sensitivity == "url" {
			return invalid()
		}
		if err := s.Items.ValidateDefinition(); err != nil {
			return err
		}
		if s.Items.Sensitivity != "none" && s.Sensitivity != "secret" && s.Sensitivity != "mixed" {
			return invalid()
		}
		return nil
	default:
		return invalid()
	}
	if (s.Collection != "none" && (s.Type != "object" || s.Collection != "replace")) || s.Items != nil || s.MaxItems != 0 {
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
		if field.Category != "app" || field.ReadOnlyReason != "" {
			continue
		}
		if _, message := field.Schema.ValidateValue(settingValue(c, field.Path)); message != "" {
			result = append(result, FieldError{Path: field.Path, Code: "invalid_value", Message: message})
		}
	}
	return result
}
