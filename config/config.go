// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/asciimoo/hister/server/languages"

	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

type Config struct {
	fname                    string
	tuiPath                  string
	sources                  map[string]ValueSource
	composition              *configComposition
	App                      App                   `yaml:"app" mapstructure:"app"`
	Server                   Server                `yaml:"server" mapstructure:"server"`
	Indexer                  Indexer               `yaml:"indexer" mapstructure:"indexer"`
	Crawler                  CrawlerConfig         `yaml:"crawler" mapstructure:"crawler"`
	SemanticSearch           SemanticSearch        `yaml:"semantic_search" mapstructure:"semantic_search"`
	Hotkeys                  Hotkeys               `yaml:"hotkeys" mapstructure:"hotkeys"`
	TUI                      TUI                   `yaml:"-" mapstructure:"tui"`
	SensitiveContentPatterns map[string]string     `yaml:"sensitive_content_patterns" mapstructure:"sensitive_content_patterns"`
	Rules                    *Rules                `yaml:"-" mapstructure:"-"`
	Extractors               map[string]*Extractor `yaml:"extractors" mapstructure:"extractors"`
	secretKey                []byte
	parsedBaseURL            *url.URL
	usesDefaultBaseURL       bool
}

type App struct {
	Directory              string `yaml:"directory" mapstructure:"directory"`
	Title                  string `yaml:"title" mapstructure:"title"`
	Subtitle               string `yaml:"subtitle" mapstructure:"subtitle"`
	ColorScheme            string `yaml:"color_scheme" mapstructure:"color_scheme"`
	SnippetLength          int    `yaml:"snippet_length" mapstructure:"snippet_length"`
	SearchURL              string `yaml:"search_url" mapstructure:"search_url"`
	AccessToken            string `yaml:"access_token" mapstructure:"access_token"`
	UserHandling           bool   `yaml:"user_handling" mapstructure:"user_handling"`
	Public                 bool   `yaml:"public" mapstructure:"public"`
	LogLevel               string `yaml:"log_level" mapstructure:"log_level"`
	LogFile                string `yaml:"log_file" mapstructure:"log_file"`
	LogFormat              string `yaml:"log_format" mapstructure:"log_format"`
	DebugSQL               bool   `yaml:"debug_sql" mapstructure:"debug_sql"`
	OpenResultsOnNewTab    bool   `yaml:"open_results_on_new_tab" mapstructure:"open_results_on_new_tab"`
	ShowSuggestions        bool   `yaml:"show_suggestions" mapstructure:"show_suggestions"`
	RedirectOnNoResults    bool   `yaml:"redirect_on_no_results" mapstructure:"redirect_on_no_results"`
	DisplayExtractorConfig bool   `yaml:"display_extractor_config" mapstructure:"display_extractor_config"`
	DisablePreviews        bool   `yaml:"disable_previews" mapstructure:"disable_previews"`
	Profiler               bool   `yaml:"profiler" mapstructure:"profiler"`
}

type TUI struct {
	DarkTheme   string `yaml:"dark_theme" mapstructure:"dark_theme"`
	LightTheme  string `yaml:"light_theme" mapstructure:"light_theme"`
	ColorScheme string `yaml:"color_scheme" mapstructure:"color_scheme"`
	ThemesDir   string `yaml:"themes_dir" mapstructure:"themes_dir"`
}

var (
	DefaultServerAddress = "127.0.0.1:4433"
	DefaultServerBaseURL = ""
)

const DefaultMaxBatchBodySize int64 = 40

type Server struct {
	Address          string                 `yaml:"address"                  mapstructure:"address"`
	BaseURL          string                 `yaml:"base_url"                 mapstructure:"base_url"`
	Database         string                 `yaml:"database"                 mapstructure:"database"`
	MaxBatchBodySize int64                  `yaml:"max_batch_body_size"      mapstructure:"max_batch_body_size"`
	ProxyAuthHeader  string                 `yaml:"proxy_auth_header"        mapstructure:"proxy_auth_header"`
	OAuth            map[string]*OAuthEntry `yaml:"oauth"                    mapstructure:"oauth"`
	OAuthOnly        bool                   `yaml:"oauth_only"               mapstructure:"oauth_only"`
	Metrics          bool                   `yaml:"metrics"                  mapstructure:"metrics"`
	ExposeVersion    bool                   `yaml:"expose_version"           mapstructure:"expose_version"`
}

func (s Server) MaxBatchBodyBytes() int64 {
	return s.MaxBatchBodySize << 20
}

func (s Server) validateMaxBatchBodySize() error {
	maxBatchBodySize := int64(^uint64(0)>>1) >> 20
	if s.MaxBatchBodySize < 1 || s.MaxBatchBodySize > maxBatchBodySize {
		return fmt.Errorf("server.max_batch_body_size must be between 1 and %d", maxBatchBodySize)
	}
	return nil
}

// ListenEndpoint returns the network and address used by net.Listen.
// Unix sockets use unix:/absolute/path; other addresses use TCP.
func (s Server) ListenEndpoint() (network, address string, err error) {
	if address, ok := strings.CutPrefix(s.Address, "unix:"); ok {
		if !filepath.IsAbs(address) || strings.ContainsRune(address, '\x00') {
			return "", "", errors.New("server.address: Unix sockets require an absolute path, such as unix:/run/hister/hister.sock")
		}
		return "unix", address, nil
	}
	return "tcp", s.Address, nil
}

// OAuthEntry holds configuration for a single OAuth 2.0 / OIDC provider.
type OAuthEntry struct {
	ClientID         string   `yaml:"client_id"         mapstructure:"client_id"`
	ClientSecret     string   `yaml:"client_secret"     mapstructure:"client_secret"`
	ConfigurationURL string   `yaml:"configuration_url" mapstructure:"configuration_url"`
	AuthURL          string   `yaml:"auth_url"          mapstructure:"auth_url"`
	TokenURL         string   `yaml:"token_url"         mapstructure:"token_url"`
	UserInfoURL      string   `yaml:"userinfo_url"      mapstructure:"userinfo_url"`
	Scopes           []string `yaml:"scopes"            mapstructure:"scopes"`
	DisablePKCE      bool     `yaml:"disable_pkce"      mapstructure:"disable_pkce"`

	// IdentityClaim selects the OIDC UserInfo claim used to match accounts. Empty means email.
	IdentityClaim string `yaml:"identity_claim" mapstructure:"identity_claim"`
	// AdminGroups enables authoritative OIDC administrator synchronization at login.
	AdminGroups []string `yaml:"admin_groups" mapstructure:"admin_groups"`
	// GroupsClaim selects an exact OIDC UserInfo claim name. Empty means groups.
	GroupsClaim string `yaml:"groups_claim" mapstructure:"groups_claim"`
}

type Directory struct {
	Path           string   `yaml:"path"              mapstructure:"path" json:"path"`
	Label          string   `yaml:"label"             mapstructure:"label" json:"label"`
	Filetypes      []string `yaml:"filetypes"         mapstructure:"filetypes" json:"filetypes,omitempty"`
	Patterns       []string `yaml:"patterns"          mapstructure:"patterns" json:"patterns,omitempty"`
	Excludes       []string `yaml:"excludes"          mapstructure:"excludes" json:"excludes,omitempty"`
	IncludeHidden  bool     `yaml:"include_hidden"    mapstructure:"include_hidden" json:"include_hidden"`
	DeleteOnRemove bool     `yaml:"delete_on_remove"  mapstructure:"delete_on_remove" json:"delete_on_remove"`
	User           string   `yaml:"user"              mapstructure:"user" json:"user"`
}

type Indexer struct {
	DetectLanguages           bool         `yaml:"detect_languages" mapstructure:"detect_languages"`
	KeepStopwords             bool         `yaml:"keep_stopwords" mapstructure:"keep_stopwords"`
	Directories               []*Directory `yaml:"directories" mapstructure:"directories"`
	MaxFileSize               int64        `yaml:"max_file_size_mb" mapstructure:"max_file_size_mb"`
	Languages                 []string     `yaml:"languages" mapstructure:"languages"`
	LanguageDetectionAccuracy string       `yaml:"language_detection_accuracy" mapstructure:"language_detection_accuracy"`
	PDFParser                 PDFParser    `yaml:"pdf_parser" mapstructure:"pdf_parser"`
}

// PDFParser selects an optional command that reads PDF bytes from stdin and
// writes plain UTF-8 text to stdout. An empty Command uses the built-in reader.
type PDFParser struct {
	Command string   `yaml:"command" mapstructure:"command"`
	Args    []string `yaml:"args" mapstructure:"args"`
	Timeout int      `yaml:"timeout" mapstructure:"timeout"`
}

func (p PDFParser) Validate() error {
	if (p.Command != "" && strings.TrimSpace(p.Command) == "") || strings.ContainsRune(p.Command, '\x00') {
		return errors.New("indexer.pdf_parser.command must name an executable")
	}
	if p.Command == "" && len(p.Args) != 0 {
		return errors.New("indexer.pdf_parser.args requires a command")
	}
	for _, arg := range p.Args {
		if strings.ContainsRune(arg, '\x00') {
			return errors.New("indexer.pdf_parser.args cannot contain NUL bytes")
		}
	}
	if p.Timeout < 1 || int64(p.Timeout) > int64((1<<63-1)/time.Second) {
		return errors.New("indexer.pdf_parser.timeout must be a positive number of seconds within the supported duration range")
	}
	return nil
}

const (
	LanguageDetectionAccuracyHigh = "high"
	LanguageDetectionAccuracyLow  = "low"
)

// LowAccuracyLanguageDetection reports whether lingua should run in its reduced
// memory mode. An unspecified accuracy uses the low accuracy default.
func (i Indexer) LowAccuracyLanguageDetection() bool {
	return i.LanguageDetectionAccuracy == "" || i.LanguageDetectionAccuracy == LanguageDetectionAccuracyLow
}

// validationError retains detailed diagnostics while exposing a field error
// without user supplied values through errors.As.
type validationError struct {
	field  FieldError
	detail string
}

func (e validationError) Error() string { return e.detail }
func (e validationError) Unwrap() error { return e.field }

// Validate checks detector settings using the shared language registry without
// loading the detector's models. Errors expose a safe FieldError through errors.As.
func (i Indexer) Validate() error {
	switch i.LanguageDetectionAccuracy {
	case "", LanguageDetectionAccuracyHigh, LanguageDetectionAccuracyLow:
	default:
		return validationError{
			field: FieldError{
				Path: "indexer.language_detection_accuracy", Code: "invalid_configuration",
				Message: "Choose low or high detection accuracy.",
			},
			detail: fmt.Sprintf(
				"invalid indexer.language_detection_accuracy %q: must be %q or %q",
				i.LanguageDetectionAccuracy, LanguageDetectionAccuracyHigh, LanguageDetectionAccuracyLow,
			),
		}
	}
	if len(i.Languages) == 0 {
		return nil
	}
	// Use the same candidate set as detection so aliases and regional variants
	// cannot make a single language satisfy the two-language minimum.
	candidates, unsupported := languages.NormalizeCandidates(i.Languages)
	field := FieldError{
		Path: "indexer.languages", Code: "invalid_configuration",
		Message: "Choose at least two distinct supported languages, or no language restriction.",
	}
	if len(unsupported) > 0 {
		return validationError{field: field, detail: fmt.Sprintf("unsupported language in indexer.languages: %q", unsupported[0])}
	}
	if len(candidates) < 2 {
		return validationError{field: field, detail: "indexer.languages needs at least two distinct languages, or none at all to detect every supported language"}
	}
	return nil
}

type CrawlerCookie struct {
	Name   string `yaml:"name"   mapstructure:"name"`
	Value  string `yaml:"value"  mapstructure:"value"`
	Domain string `yaml:"domain" mapstructure:"domain"`
	Path   string `yaml:"path"   mapstructure:"path"`
}

// CrawlerConfig holds global crawler settings and backend-specific options.
// Timeout and Delay default to 5s and 0s respectively when zero.
type CrawlerConfig struct {
	Timeout        int               `yaml:"timeout"         mapstructure:"timeout"`
	Delay          int               `yaml:"delay"           mapstructure:"delay"`
	Backend        string            `yaml:"backend"         mapstructure:"backend"`
	BackendOptions map[string]any    `yaml:"backend_options" mapstructure:"backend_options"`
	Proxy          string            `yaml:"proxy"           mapstructure:"proxy"`
	UserAgent      string            `yaml:"user_agent"      mapstructure:"user_agent"`
	Headers        map[string]string `yaml:"headers"         mapstructure:"headers"`
	Cookies        []CrawlerCookie   `yaml:"cookies"         mapstructure:"cookies"`
	NoRobots       bool              `yaml:"no_robots"       mapstructure:"no_robots"`
}

// ValidateFields checks crawler settings without opening a connection or browser.
func (c *CrawlerConfig) ValidateFields() []FieldError {
	var fields []FieldError
	reject := func(path, message string) {
		fields = append(fields, FieldError{Path: "crawler." + path, Code: "invalid_value", Message: message})
	}
	for _, field := range FieldCatalog() {
		if field.Category != "crawler" || field.ReadOnlyReason != "" {
			continue
		}
		// Collection and secret values are checked by their owning validators.
		if field.Schema.Sensitivity != "none" || field.Schema.Collection != "none" {
			continue
		}
		if _, message := field.Schema.ValidateValue(settingValue(&Config{Crawler: *c}, field.Path)); message != "" {
			fields = append(fields, FieldError{Path: field.Path, Code: "invalid_value", Message: message})
		}
	}
	for name, value := range c.BackendOptions {
		allowed := (name == "capture_delay" && (c.Backend == "chromedp" || c.Backend == "bidi")) ||
			((name == "host" || name == "port" || name == "socket") && c.Backend == "bidi") ||
			(name == "exec_path" && c.Backend == "chromedp")
		if !allowed {
			reject("backend_options", "An option is unsupported by the selected backend.")
			continue
		}
		switch name {
		case "capture_delay":
			// Main files retain duration string support used by the crawler.
			if text, ok := value.(string); ok {
				duration, err := time.ParseDuration(text)
				if err != nil || duration < 0 {
					reject("backend_options", "Use a nonnegative capture delay in seconds or a duration string.")
				}
			} else if _, message := crawlerBackendSchema().Properties[0].Schema.ValidateValue(value); message != "" {
				reject("backend_options", "Use a capture delay between 0 and 86400 seconds.")
			}
		case "port":
			port, err := strconv.Atoi(fmt.Sprint(value))
			if err != nil || port < 1 || port > 65535 {
				reject("backend_options", "Use a browser port between 1 and 65535.")
			}
		case "host":
			host, ok := value.(string)
			if !ok || host == "" || strings.ContainsAny(host, " /\\?#@\r\n\t") {
				reject("backend_options", "Use a browser hostname or IP address without a path or credentials.")
			}
		case "socket":
			text, ok := value.(string)
			u, err := url.Parse(text)
			if !ok || err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Hostname() == "" || u.Fragment != "" {
				reject("backend_options", "Use a ws:// or wss:// browser socket URL with a host and no fragment.")
			}
		case "exec_path":
			if _, ok := value.(string); !ok {
				reject("backend_options", "The file managed executable path must be a string.")
			}
		}
	}
	if strings.ContainsAny(c.UserAgent, "\r\n") {
		reject("user_agent", "Use a single line for the user agent.")
	}
	if proxy := strings.TrimSpace(c.Proxy); proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil || (u.Scheme != "http" && u.Scheme != "socks5") || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			reject("proxy", "Use an HTTP or SOCKS5 proxy URL with a host, without credentials, a path, query, or fragment.")
		}
	}
	if message := validateHTTPHeaders(c.Headers); message != "" {
		reject("headers", message)
	}
	seenCookies := make(map[string]bool)
	for _, cookie := range c.Cookies {
		domain := strings.TrimPrefix(cookie.Domain, ".")
		key := secretScope(cookie.Name, strings.ToLower(domain), cookie.Path)
		parsed := http.Cookie{Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path}
		if parsed.Valid() != nil || domain == "" || strings.ContainsAny(domain, " /\\:@?#\r\n\t") || (cookie.Path != "" && !strings.HasPrefix(cookie.Path, "/")) || seenCookies[key] {
			reject("cookies", "Use valid cookies with unique names per domain and path. A domain is required and paths must start with /.")
		}
		seenCookies[key] = true
	}

	return fields
}

func validateHTTPHeaders(headers map[string]string) string {
	seen := make(map[string]bool)
	for name, value := range headers {
		lower := strings.ToLower(name)
		if !validHTTPToken(name) || seen[lower] || !validHeaderValue(value) {
			return "Use unique HTTP header names and values without control characters."
		}
		seen[lower] = true
	}
	return ""
}

func validHTTPToken(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		letter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		digit := c >= '0' && c <= '9'
		if !letter && !digit && !strings.ContainsRune("!#$%&'*+-.^_`|~", c) {
			return false
		}
	}
	return true
}

func validHeaderValue(value string) bool {
	for _, c := range value {
		if c == 127 || (c < 32 && c != '\t') {
			return false
		}
	}
	return true
}

type Hotkeys struct {
	Web map[string]string `yaml:"web" mapstructure:"web"`
	TUI map[string]string `yaml:"-" mapstructure:"tui"`
}

type Rules struct {
	Allow      *Rule   `json:"allow"`
	Skip       *Rule   `json:"skip"`
	Priority   *Rule   `json:"priority"`
	Versioning *Rule   `json:"versioning"`
	Aliases    Aliases `json:"aliases"`
}

type Rule struct {
	ReStrs []string
	re     *regexp.Regexp
}

type Extractor struct {
	Enable        bool           `yaml:"enable" mapstructure:"enable"`
	Options       map[string]any `yaml:"options" mapstructure:"options"`
	enableOmitted bool
}

// Enabled preserves the extractor's own default when loaded configuration only
// selects options. Programmatically constructed configurations remain explicit.
func (e *Extractor) Enabled(defaultValue bool) bool {
	if e.enableOmitted {
		return defaultValue
	}
	return e.Enable
}

// Clone detaches option collections so preparing an extractor cannot mutate
// the loaded configuration or another registry's defaults.
func (e *Extractor) Clone() *Extractor {
	copy := *e
	copy.Options = cloneInputValue(reflect.ValueOf(e.Options)).Interface().(map[string]any)
	return &copy
}

// SemanticSearch holds configuration for optional vector similarity search.
type SemanticSearch struct {
	Enable                       bool              `yaml:"enable" mapstructure:"enable"`
	EmbeddingEndpoint            string            `yaml:"embedding_endpoint" mapstructure:"embedding_endpoint"`
	EmbeddingModel               string            `yaml:"embedding_model" mapstructure:"embedding_model"`
	EmbeddingTimeout             int               `yaml:"embedding_timeout" mapstructure:"embedding_timeout"`
	APIKey                       string            `yaml:"api_key" mapstructure:"api_key"`
	Headers                      map[string]string `yaml:"headers" mapstructure:"headers"`
	Dimensions                   int               `yaml:"dimensions" mapstructure:"dimensions"`
	MaxContextLength             int               `yaml:"max_context_length" mapstructure:"max_context_length"`
	ChunkOverlap                 int               `yaml:"chunk_overlap" mapstructure:"chunk_overlap"`
	MaxEmbeddingBatchSize        int               `yaml:"max_embedding_batch_size" mapstructure:"max_embedding_batch_size"`
	QueryPrefix                  string            `yaml:"query_prefix" mapstructure:"query_prefix"`
	DocumentPrefix               string            `yaml:"document_prefix" mapstructure:"document_prefix"`
	SimilarityThreshold          float64           `yaml:"similarity_threshold" mapstructure:"similarity_threshold"`
	ResultLimit                  int               `yaml:"result_limit" mapstructure:"result_limit"`
	SemanticWeight               float64           `yaml:"semantic_weight" mapstructure:"semantic_weight"`
	MaxEmbeddingConcurrency      int               `yaml:"max_embedding_concurrency" mapstructure:"max_embedding_concurrency"`
	QueryEmbeddingTimeout        int               `yaml:"query_embedding_timeout" mapstructure:"query_embedding_timeout"`
	MaxQueryEmbeddingConcurrency int               `yaml:"max_query_embedding_concurrency" mapstructure:"max_query_embedding_concurrency"`
}

// EmbeddingFingerprint identifies configuration that changes stored document
// embeddings. Query and ranking settings are intentionally excluded because
// they do not require document embeddings to be rebuilt.
func (s SemanticSearch) EmbeddingFingerprint() string {
	if !s.Enable {
		return ""
	}
	payload := struct {
		Version          int    `json:"version"`
		Endpoint         string `json:"endpoint"`
		Model            string `json:"model"`
		Dimensions       int    `json:"dimensions"`
		MaxContextLength int    `json:"max_context_length"`
		ChunkOverlap     int    `json:"chunk_overlap"`
		DocumentPrefix   string `json:"document_prefix"`
	}{
		Version:          1,
		Endpoint:         s.EmbeddingEndpoint,
		Model:            s.EmbeddingModel,
		Dimensions:       s.Dimensions,
		MaxContextLength: s.MaxContextLength,
		ChunkOverlap:     s.ChunkOverlap,
		DocumentPrefix:   s.DocumentPrefix,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

const postgresHNSWMaxDimensions = 2000

// DBTypedef represents the type of database being used.
type DBTypedef int

const (
	// Sqlite represents SQLite database type.
	Sqlite DBTypedef = iota
	// Psql represents PostgreSQL database type.
	Psql
)

type Aliases map[string]string

type Action string

const (
	ActionQuit           Action = "quit"
	ActionToggleHelp     Action = "toggle_help"
	ActionToggleFocus    Action = "toggle_focus"
	ActionScrollUp       Action = "scroll_up"
	ActionScrollDown     Action = "scroll_down"
	ActionOpenResult     Action = "open_result"
	ActionCopyResult     Action = "copy_result"
	ActionTogglePreview  Action = "toggle_preview"
	ActionEditLabel      Action = "edit_label"
	ActionDeleteResult   Action = "delete_result"
	ActionToggleTheme    Action = "toggle_theme"
	ActionToggleSettings Action = "toggle_settings"
	ActionToggleSort     Action = "toggle_sort"
	ActionToggleSemantic Action = "toggle_semantic"
	ActionTabSearch      Action = "tab_search"
	ActionTabHistory     Action = "tab_history"
	ActionTabRules       Action = "tab_rules"
	ActionTabAdd         Action = "tab_add"
)

// ValidTUIActions is the set of valid TUI hotkey actions.
var ValidTUIActions = map[Action]bool{
	ActionQuit:           true,
	ActionToggleHelp:     true,
	ActionToggleFocus:    true,
	ActionScrollUp:       true,
	ActionScrollDown:     true,
	ActionOpenResult:     true,
	ActionCopyResult:     true,
	ActionTogglePreview:  true,
	ActionEditLabel:      true,
	ActionDeleteResult:   true,
	ActionToggleTheme:    true,
	ActionToggleSettings: true,
	ActionToggleSort:     true,
	ActionToggleSemantic: true,
	ActionTabSearch:      true,
	ActionTabHistory:     true,
	ActionTabRules:       true,
	ActionTabAdd:         true,
}

var DefaultTUIHotkeys = map[string]string{
	"ctrl+c": "quit",
	"f1":     "toggle_help",
	"tab":    "toggle_focus",
	"esc":    "toggle_focus",
	"up":     "scroll_up",
	"k":      "scroll_up",
	"down":   "scroll_down",
	"j":      "scroll_down",
	"enter":  "open_result",
	"y":      "copy_result",
	"v":      "toggle_preview",
	"l":      "edit_label",
	"ctrl+d": "delete_result",
	"ctrl+t": "toggle_theme",
	"ctrl+s": "toggle_settings",
	"ctrl+o": "toggle_sort",
	"ctrl+e": "toggle_semantic",
	"alt+1":  "tab_search",
	"alt+2":  "tab_history",
	"alt+3":  "tab_rules",
	"alt+4":  "tab_add",
}

var DefaultTUIConfig = TUI{
	DarkTheme:   "tokyonight",
	LightTheme:  "catppuccin-latte",
	ColorScheme: "terminal",
}

func copyMap(m map[string]string) map[string]string {
	cp := make(map[string]string, len(m))
	maps.Copy(cp, m)
	return cp
}

var (
	secretKeyFilename                = ".secret_key"
	hotkeyKeyRe       *regexp.Regexp = regexp.MustCompile(`^((ctrl|alt|meta)\+)?([a-z0-9/?]|enter|tab|arrow(up|down|right|left)|f[1-9]|f1[012])$`)
	hotkeyActions                    = []string{
		"select_previous_result",
		"select_next_result",
		"focus_search_input",
		"open_result",
		"open_result_in_new_tab",
		"open_query_in_search_engine",
		"view_result_popup",
		"autocomplete",
		"show_hotkeys",
		"delete_result",
	}
)

func getDefaultDataDir() string {
	switch runtime.GOOS {
	case "darwin":
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, "Library/Application Support/hister")

	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			return filepath.Join(localAppData, "hister")
		}
		// fallback to APPDATA
		appData := os.Getenv("APPDATA")
		return filepath.Join(appData, "hister")

	default:
		if xdgState := os.Getenv("XDG_STATE_HOME"); xdgState != "" {
			return filepath.Join(xdgState, "hister")
		}
		if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
			return filepath.Join(xdgData, "hister")
		}
		// fall back to ~/.config/hister
		configDir, _ := os.UserConfigDir()
		return filepath.Join(configDir, "hister")
	}
}

// getConfigSearchPaths returns the list of paths to search for config files
// in order of preference, respecting XDG Base Directory standards.
func getConfigSearchPaths(homeDir string) []string {
	var paths []string

	switch runtime.GOOS {
	case "darwin":
		// macOS: XDG_CONFIG_HOME defaults to ~/Library/Preferences
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig == "" {
			xdgConfig = filepath.Join(homeDir, "Library/Preferences")
		}
		paths = append(paths, filepath.Join(xdgConfig, "hister/config.yml"))

		// Application Support for backwards compatibility with some users
		paths = append(paths, filepath.Join(homeDir, "Library/Application Support/hister/config.yml"))

		// Legacy paths (for deprecation warning)
		paths = append(paths, filepath.Join(homeDir, ".histerrc"))
		paths = append(paths, filepath.Join(homeDir, ".config/hister/config.yml"))

	case "windows":
		// Windows: use LOCALAPPDATA
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			paths = append(paths, filepath.Join(localAppData, "hister/config.yml"))
		}

		// XDG_CONFIG_HOME if set
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig != "" {
			paths = append(paths, filepath.Join(xdgConfig, "hister/config.yml"))
		}

		// APPDATA fallback
		appData := os.Getenv("APPDATA")
		if appData != "" {
			paths = append(paths, filepath.Join(appData, "hister/config.yml"))
		}

		// Legacy paths
		paths = append(paths, filepath.Join(homeDir, ".histerrc"))
		paths = append(paths, filepath.Join(homeDir, ".config/hister/config.yml"))

	default:
		// Linux and other Unix-like systems: follow XDG Base Directory
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig != "" {
			paths = append(paths, filepath.Join(xdgConfig, "hister/config.yml"))
		} else {
			paths = append(paths, filepath.Join(homeDir, ".config/hister/config.yml"))
		}

		// Legacy paths
		paths = append(paths, filepath.Join(homeDir, ".histerrc"))
	}

	return paths
}

func loadInputs(rawConfig []byte, name string) (*configInputs, error) {
	inputs, err := fileInputs(rawConfig, name)
	if err != nil {
		return nil, err
	}
	// Preserve the older environment spelling for keys present in the file.
	// Capture it explicitly so decoding cannot introduce untracked values.
	for _, key := range inputs.values.AllKeys() {
		name := "HISTER_" + strings.ToUpper(strings.ReplaceAll(key, ".", "__"))
		if value := os.Getenv(name); value != "" {
			if err := inputs.apply(Override{Path: key, Value: value, Source: ValueSource{Kind: "environment", Name: name}}); err != nil {
				return nil, err
			}
		}
	}
	// HISTER__ bindings have higher precedence, including explicit empty values.
	if err := bindEnvironment(inputs); err != nil {
		return nil, err
	}

	return inputs, nil
}

func bindEnvironment(inputs *configInputs) error {
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "HISTER__") {
			continue
		}
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimPrefix(parts[0], "HISTER__")
		normalizedKey := strings.ToLower(strings.ReplaceAll(key, "__", "."))
		var val any = parts[1]
		if isUntypedOptionKey(normalizedKey) {
			val = parseEnvValue(parts[1])
		}
		if err := inputs.apply(Override{Path: normalizedKey, Value: val, Source: ValueSource{Kind: "environment", Name: parts[0]}}); err != nil {
			return err
		}
		log.Debug().Str("env", parts[0]).Str("key", normalizedKey).Msg("Loaded configuration from environment variable")
	}
	return nil
}

func isUntypedOptionKey(normalizedKey string) bool {
	seg := strings.Split(normalizedKey, ".")
	switch {
	case len(seg) >= 4 && seg[0] == "extractors" && seg[2] == "options":
		return true
	case len(seg) >= 3 && seg[0] == "crawler" && seg[1] == "backend_options":
		return true
	}
	return false
}

func parseEnvValue(s string) any {
	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil && strconv.FormatInt(i, 10) == s {
		return int(i)
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && strconv.FormatFloat(f, 'f', -1, 64) == s {
		return f
	}
	return s
}

// Load initializes runtime configuration from a selected file. An empty name
// uses defaults, managed selections, and environment; discovery belongs to ResolvePath.
func Load(filename string, overrides ...Override) (*Config, error) {
	name := filename
	var err error
	if filename != "" {
		name, err = ResolvePath(filename, true)
	}
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(name, ".histerrc") {
		log.Warn().Str("oldPath", name).Msg("Configuration file found at deprecated location ~/.histerrc. Please move it to the recommended location for your OS")
	}
	c, err := loadSelected(name, false, overrides...)
	if err != nil {
		return nil, err
	}
	if err := c.validateBasic(); err != nil {
		return nil, err
	}
	return c, c.init()
}

func CreateDefaultConfig() *Config {
	return &Config{
		App: App{
			SearchURL:              "https://google.com/search?q={query}",
			Directory:              getDefaultDataDir(),
			Title:                  "Hister",
			Subtitle:               "Your own search engine",
			ColorScheme:            "automatic",
			SnippetLength:          300,
			LogLevel:               "info",
			LogFormat:              "text",
			OpenResultsOnNewTab:    false,
			ShowSuggestions:        false,
			RedirectOnNoResults:    true,
			DisplayExtractorConfig: false,
		},
		Server: Server{
			Address:          DefaultServerAddress,
			BaseURL:          DefaultServerBaseURL,
			Database:         "db.sqlite3",
			MaxBatchBodySize: DefaultMaxBatchBodySize,
			Metrics:          false,
			ExposeVersion:    false,
		},
		Indexer: Indexer{
			DetectLanguages:           true,
			KeepStopwords:             false,
			MaxFileSize:               1,
			LanguageDetectionAccuracy: LanguageDetectionAccuracyLow,
			PDFParser:                 PDFParser{Timeout: 30},
		},
		Crawler: CrawlerConfig{
			Backend: "http",
			Timeout: 5,
		},
		Hotkeys: Hotkeys{
			Web: map[string]string{
				"alt+j":     "select_next_result",
				"alt+k":     "select_previous_result",
				"/":         "focus_search_input",
				"enter":     "open_result",
				"alt+enter": "open_result_in_new_tab",
				"alt+o":     "open_query_in_search_engine",
				"alt+v":     "view_result_popup",
				"tab":       "autocomplete",
				"?":         "show_hotkeys",
				"alt+d":     "delete_result",
			},
		},
		SensitiveContentPatterns: map[string]string{
			"aws_access_key":      `(^|[\s"'])AKIA[0-9A-Z]{16}([\s"']|$)`,
			"aws_secret_key":      `(?i)aws(.{0,20})?(secret)?(.{0,20})?['"][0-9a-zA-Z\/+]{40}['"]`,
			"generic_private_key": `-----BEGIN ((RSA|EC|DSA) )?PRIVATE KEY-----`,
			"github_token":        `(ghp|gho|ghu|ghs|ghr)_[a-zA-Z0-9]{36}`,
			"ssh_private_key":     `-----BEGIN OPENSSH PRIVATE KEY-----`,
			"pgp_private_key":     `-----BEGIN PGP PRIVATE KEY BLOCK-----`,
		},
		SemanticSearch: SemanticSearch{
			Enable:                       false,
			EmbeddingEndpoint:            "http://localhost:11434/v1/embeddings",
			EmbeddingModel:               "qwen3-embedding:8b",
			EmbeddingTimeout:             300,
			APIKey:                       "",
			Headers:                      map[string]string{},
			Dimensions:                   postgresHNSWMaxDimensions,
			MaxContextLength:             512,
			ChunkOverlap:                 64,
			MaxEmbeddingBatchSize:        8,
			QueryPrefix:                  "query: ",
			DocumentPrefix:               "",
			SimilarityThreshold:          0.1,
			ResultLimit:                  50,
			SemanticWeight:               0.4,
			MaxEmbeddingConcurrency:      2,
			QueryEmbeddingTimeout:        2,
			MaxQueryEmbeddingConcurrency: 1,
		},
	}
}

func parseConfig(rawConfig []byte) (*Config, error) {
	c, err := decodeConfig(rawConfig, "", false)
	if err != nil {
		return nil, err
	}

	return c, c.validateBasic()
}

func (c *Config) validateBasic() error {
	if fields := c.validateAppFields(); len(fields) > 0 {
		return fields[0]
	}
	if err := c.Indexer.PDFParser.Validate(); err != nil {
		return err
	}
	if _, _, err := c.Server.ListenEndpoint(); err != nil {
		return err
	}
	if err := c.Server.validateMaxBatchBodySize(); err != nil {
		return err
	}
	if c.Server.BaseURL != "" {
		pu, err := url.Parse(c.Server.BaseURL)
		if err != nil || pu.Scheme == "" || pu.Host == "" {
			return errors.New("invalid server.base_url: use 'https://domain.tld/xy/' format")
		}
		c.Server.BaseURL = strings.TrimSuffix(c.Server.BaseURL, "/")
	}
	return nil
}

func (c *Config) environmentOverrides() ([]Override, error) {
	var overrides []Override
	if dataDir := os.Getenv("HISTER_DATA_DIR"); dataDir != "" {
		overrides = append(overrides, Override{Path: "app.directory", Value: dataDir, Source: ValueSource{Kind: "environment", Name: "HISTER_DATA_DIR"}})
	}

	network, _, err := c.Server.ListenEndpoint()
	if err != nil {
		return nil, err
	}
	if envPort := os.Getenv("HISTER_PORT"); envPort != "" && network == "tcp" {
		host, _, err := net.SplitHostPort(c.Server.Address)
		if err != nil || host == "" {
			host = c.Server.Address
		}
		overrides = append(overrides, Override{Path: "server.address", Value: net.JoinHostPort(host, envPort), Source: ValueSource{Kind: "environment", Name: "HISTER_PORT"}})
	}
	return overrides, nil
}

func (c *Config) normalize() error {
	defaults := CreateDefaultConfig()
	if c.App.LogLevel == "" && c.Source("app.log_level").Kind != "flag" {
		c.App.LogLevel = defaults.App.LogLevel
	}
	if c.App.SearchURL == "" && c.Source("app.search_url").Kind != "flag" {
		c.App.SearchURL = defaults.App.SearchURL
	}
	if c.Server.ProxyAuthHeader != "" {
		c.App.UserHandling = true
	}

	if strings.HasPrefix(c.App.Directory, "~/") {
		u, err := user.Current()
		if err != nil {
			return fmt.Errorf("resolve home directory: %w", err)
		}
		c.App.Directory = filepath.Join(u.HomeDir, c.App.Directory[2:])
	}
	return nil
}

func (c *Config) init() error {
	if err := c.UpdateBaseURL(c.Server.BaseURL); err != nil {
		return err
	}
	if err := os.MkdirAll(c.App.Directory, 0o750); err != nil {
		isPermissionErr := errors.Is(err, os.ErrPermission) ||
			strings.Contains(strings.ToLower(err.Error()), "permission denied") ||
			strings.Contains(strings.ToLower(err.Error()), "operation not permitted")

		if isPermissionErr {
			home, _ := os.UserHomeDir()
			useFallback := home == "/var/empty" || c.App.Directory != getDefaultDataDir()

			if useFallback {
				c.App.Directory = "/var/lib/hister"
				log.Info().Str("directory", c.App.Directory).Str("fallback", "/var/lib/hister").Msg("System user detected, using system-wide data directory")
			} else {
				log.Warn().Str("directory", c.App.Directory).Msg("Cannot write to data directory. Set HISTER_DATA_DIR environment variable or configure app.directory")
				return fmt.Errorf("cannot create data directory: %w. Set HISTER_DATA_DIR environment variable or configure app.directory in your config file", err)
			}

			c.App.Directory = "/var/lib/hister"
		}

		err = os.MkdirAll(c.App.Directory, 0o750)
		if err != nil {
			return err
		}
	}
	if err := c.Hotkeys.Validate(); err != nil {
		return err
	}
	if err := c.validateSemanticSearch(); err != nil {
		return err
	}
	if err := c.Indexer.Validate(); err != nil {
		return err
	}
	if err := c.validateOAuth(); err != nil {
		return err
	}
	sPath := c.FullPath(secretKeyFilename)
	b, err := os.ReadFile(sPath)
	if err != nil {
		c.secretKey = []byte(rand.Text() + rand.Text())
		if err := os.WriteFile(sPath, c.secretKey, 0o600); err != nil {
			return fmt.Errorf("failed to create secret key file: %w", err)
		}
	} else {
		c.secretKey = b
	}
	c.LoadTUIConfig()
	return c.LoadRules()
}

var validOAuthProviders = map[string]bool{"github": true, "google": true, "oidc": true}

func (c *Config) validateOAuth() error {
	for name, entry := range c.Server.OAuth {
		if !validOAuthProviders[name] {
			return fmt.Errorf("unknown oauth provider %q: valid providers are github, google, oidc", name)
		}
		if entry == nil || entry.ClientID == "" {
			return fmt.Errorf("oauth provider %q: client_id is required", name)
		}
		if entry.ClientSecret == "" {
			return fmt.Errorf("oauth provider %q: client_secret is required", name)
		}
		if entry.IdentityClaim != "" {
			if name != "oidc" {
				return fmt.Errorf("oauth provider %q: identity_claim is only supported for oidc", name)
			}
			if strings.ContainsFunc(entry.IdentityClaim, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
				return fmt.Errorf("oauth provider %q: identity_claim must not contain whitespace or control characters", name)
			}
		}
		if name != "oidc" && (len(entry.AdminGroups) > 0 || entry.GroupsClaim != "") {
			return fmt.Errorf("oauth provider %q: admin_groups and groups_claim are only supported for oidc", name)
		}
		if strings.ContainsFunc(entry.GroupsClaim, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return fmt.Errorf("oauth provider %q: groups_claim must not contain whitespace or control characters", name)
		}
		for _, group := range entry.AdminGroups {
			if strings.TrimSpace(group) == "" || strings.ContainsFunc(group, unicode.IsControl) {
				return fmt.Errorf("oauth provider %q: admin_groups must contain nonblank names without control characters", name)
			}
		}
		if name == "oidc" && entry.ConfigurationURL == "" && entry.AuthURL == "" {
			return fmt.Errorf("oauth provider oidc: configuration_url or auth_url is required")
		}
	}
	return nil
}

// CompileSensitiveContentPatterns validates each expression and combines them
// with independent flag scopes in a stable order.
func (c *Config) CompileSensitiveContentPatterns() (*regexp.Regexp, error) {
	patterns := make([]string, 0, len(c.SensitiveContentPatterns))
	for _, name := range slices.Sorted(maps.Keys(c.SensitiveContentPatterns)) {
		pattern := c.SensitiveContentPatterns[name]
		if _, err := regexp.Compile(pattern); err != nil {
			return nil, fmt.Errorf("invalid sensitive content pattern %q", name)
		}
		patterns = append(patterns, "(?:"+pattern+")")
	}
	combined, err := regexp.Compile("(" + strings.Join(patterns, "|") + ")")
	if err != nil {
		return nil, errors.New("cannot combine sensitive content patterns")
	}
	return combined, nil
}

func (c *Config) ValidatePublicMode() error {
	if !c.App.Public {
		return nil
	}
	if c.App.AccessToken == "" && !c.App.UserHandling {
		return errors.New("app.public requires app.access_token or app.user_handling")
	}
	return nil
}

func (c *Config) UpdateListenAddress(a string) error {
	s := c.Server
	s.Address = a
	if _, _, err := s.ListenEndpoint(); err != nil {
		return err
	}
	c.Server.Address = a
	if c.usesDefaultBaseURL {
		return c.UpdateBaseURL("")
	}
	return nil
}

func (c *Config) UpdateBaseURL(u string) error {
	network, _, err := c.Server.ListenEndpoint()
	if err != nil {
		return err
	}
	// If the base URL is unspecified, it defaults to the listen address.
	if u == "" {
		if network == "unix" {
			return errors.New("server.base_url must be specified when listening on a Unix socket")
		}
		c.usesDefaultBaseURL = true
		addr_port, err := netip.ParseAddrPort(c.Server.Address)
		if err != nil {
			return fmt.Errorf("failed to parse listen address: %w", err)
		}
		if addr_port.Addr().IsUnspecified() {
			return fmt.Errorf("server: base_url must be specified when listening on %v", addr_port.Addr())
		}
		c.Server.BaseURL = fmt.Sprintf("http://%s", c.Server.Address)
	} else {
		c.usesDefaultBaseURL = false
		c.Server.BaseURL = strings.TrimSuffix(u, "/")
	}

	pu, err := url.Parse(c.Server.BaseURL)
	if err != nil {
		return fmt.Errorf("failed to parse base URL: %w", err)
	}
	c.parsedBaseURL = pu
	return nil
}

func (c *Config) LoadTUIConfig() {
	tuiPath := c.tuiConfigPath()
	b, err := os.ReadFile(tuiPath)
	if err != nil {
		c.TUI = DefaultTUIConfig
		c.Hotkeys.TUI = copyMap(DefaultTUIHotkeys)
		if os.IsNotExist(err) {
			if err := c.SaveTUIConfig(); err != nil {
				log.Warn().Err(err).Str("file", tuiPath).Msg("Failed to create tui.yaml")
			} else {
				log.Debug().Str("file", tuiPath).Msg("Created tui.yaml with defaults")
			}
		} else {
			log.Warn().Err(err).Str("file", tuiPath).Msg("Failed to read tui.yaml, using defaults")
		}
		return
	}
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(bytes.NewBuffer(b)); err != nil {
		log.Warn().Err(err).Str("file", tuiPath).Msg("Failed to parse tui.yaml")
		c.TUI = DefaultTUIConfig
		c.Hotkeys.TUI = copyMap(DefaultTUIHotkeys)
		return
	}
	c.TUI.DarkTheme = v.GetString("dark_theme")
	c.TUI.LightTheme = v.GetString("light_theme")
	c.TUI.ColorScheme = v.GetString("color_scheme")
	c.TUI.ThemesDir = v.GetString("themes_dir")
	if c.TUI.DarkTheme == "" {
		c.TUI.DarkTheme = DefaultTUIConfig.DarkTheme
	}
	if c.TUI.LightTheme == "" {
		c.TUI.LightTheme = DefaultTUIConfig.LightTheme
	}
	if c.TUI.ColorScheme == "" {
		c.TUI.ColorScheme = DefaultTUIConfig.ColorScheme
	}
	if v.IsSet("hotkeys") {
		hotkeys := v.GetStringMapString("hotkeys")
		if len(hotkeys) > 0 {
			c.Hotkeys.TUI = hotkeys
		}
	} else {
		c.Hotkeys.TUI = copyMap(DefaultTUIHotkeys)
	}
	if c.Hotkeys.TUI == nil {
		c.Hotkeys.TUI = make(map[string]string)
	}
	mergeDefaultTUIHotkeys(c.Hotkeys.TUI)
	log.Debug().Str("file", tuiPath).Msg("Loaded TUI config")
}

func mergeDefaultTUIHotkeys(hotkeys map[string]string) {
	boundActions := make(map[string]bool, len(hotkeys))
	for _, action := range hotkeys {
		boundActions[action] = true
	}
	for key, action := range DefaultTUIHotkeys {
		if _, keyTaken := hotkeys[key]; keyTaken || boundActions[action] {
			continue
		}
		hotkeys[key] = action
		boundActions[action] = true
	}
}

func (c *Config) SecretKey() []byte {
	return c.secretKey
}

func (c *Config) FullPath(f string) string {
	if filepath.IsAbs(f) {
		return f
	}
	if strings.HasPrefix(f, "./") || strings.HasPrefix(f, "../") {
		ex, err := os.Executable()
		if err != nil {
			return f
		}
		return filepath.Join(filepath.Dir(ex), f)
	}
	return filepath.Join(c.App.Directory, f)
}

func (c *Config) RulesPath() string {
	return c.FullPath("rules.json")
}

func (c *Config) DatabaseConnection() (DBTypedef, string) {
	if strings.Contains(c.Server.Database, "=") {
		return Psql, c.Server.Database
	}
	return Sqlite, c.FullPath(c.Server.Database)
}

func (c *Config) Filename() string {
	if c.fname == "" {
		return "*Default Config*"
	}
	return c.FullPath(c.fname)
}

func (c *Config) SaveTUIConfig() error {
	tuiPath := c.tuiConfigPath()
	if err := os.MkdirAll(filepath.Dir(tuiPath), 0o755); err != nil {
		return err
	}
	v := viper.New()
	v.SetConfigType("yaml")
	v.Set("dark_theme", c.TUI.DarkTheme)
	v.Set("light_theme", c.TUI.LightTheme)
	v.Set("color_scheme", c.TUI.ColorScheme)
	if c.TUI.ThemesDir != "" {
		v.Set("themes_dir", c.TUI.ThemesDir)
	}
	if len(c.Hotkeys.TUI) > 0 && c.Hotkeys.TUI != nil {
		v.Set("hotkeys", c.Hotkeys.TUI)
	}
	return v.WriteConfigAs(tuiPath)
}

func (c *Config) tuiConfigPath() string {
	if c.tuiPath == "" {
		mainPath := c.fname
		if mainPath == "" {
			mainPath = c.defaultConfigPath()
		}
		c.tuiPath = filepath.Join(filepath.Dir(mainPath), "tui.yaml")
	}
	return c.tuiPath
}

func (c *Config) defaultConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	paths := getConfigSearchPaths(homeDir)
	if len(paths) > 0 {
		return paths[0]
	}
	// Fallback (shouldn't happen)
	return filepath.Join(homeDir, ".config/hister/config.yml")
}

func (c *Config) BaseURL(u string) string {
	if u == "" {
		return c.Server.BaseURL
	}
	if strings.HasPrefix(u, "/") && strings.HasSuffix(c.Server.BaseURL, "/") {
		u = u[1:]
	}
	if !strings.HasPrefix(u, "/") && !strings.HasSuffix(c.Server.BaseURL, "/") {
		u = "/" + u
	}
	return c.Server.BaseURL + u
}

func (c *Config) IsSameHost(h string) bool {
	bu, err := c.baseURLParsed()
	if err != nil {
		return false
	}
	ru, err := url.Parse(h)
	if err != nil {
		return false
	}
	if ru.Scheme == "hister" {
		return true
	}
	if ru.Scheme != bu.Scheme {
		return false
	}
	if ru.Port() != bu.Port() {
		return false
	}
	if bu.Hostname() == "127.0.0.1" || bu.Hostname() == "localhost" {
		return ru.Hostname() == "127.0.0.1" || ru.Hostname() == "localhost"
	}
	return bu.Hostname() == ru.Hostname()
}

func (c *Config) Host() string {
	u, err := c.baseURLParsed()
	if err != nil {
		return ""
	}
	return u.Host
}

func (c *Config) WebSocketURL() string {
	bu, err := c.baseURLParsed()
	if err != nil {
		return ""
	}
	u := *bu
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	basePath := strings.TrimSuffix(u.Path, "/")
	if basePath == "/" {
		basePath = ""
	}
	wsPath := path.Join(basePath, "/search")
	if !strings.HasPrefix(wsPath, "/") {
		wsPath = "/" + wsPath
	}
	u.Scheme = scheme
	u.Path = wsPath
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// BasePathPrefix returns the URL path component of Server.BaseURL without a trailing slash.
// It returns "" when Server.BaseURL points to the domain root.
func (c *Config) BasePathPrefix() string {
	u, err := c.baseURLParsed()
	if err != nil {
		return ""
	}
	p := strings.TrimSuffix(u.Path, "/")
	if p == "/" {
		return ""
	}
	return p
}

func (c *Config) baseURLParsed() (*url.URL, error) {
	if c.parsedBaseURL != nil {
		return c.parsedBaseURL, nil
	}
	return url.Parse(c.Server.BaseURL)
}

func (c *Config) LoadRules() error {
	b, err := os.ReadFile(c.RulesPath())
	if err != nil {
		err = c.SaveRules()
		if err != nil {
			return err
		}
		b, err = os.ReadFile(c.RulesPath())
		if err != nil {
			return err
		}
	}
	return c.parseRules(b)
}

func (c *Config) parseRules(b []byte) error {
	err := json.Unmarshal(b, &c.Rules)
	if err != nil {
		return err
	}
	if c.Rules == nil {
		c.Rules = &Rules{}
	}
	if c.Rules.Allow == nil {
		c.Rules.Allow = &Rule{ReStrs: make([]string, 0)}
	}
	if c.Rules.Skip == nil {
		c.Rules.Skip = &Rule{ReStrs: make([]string, 0)}
	}
	if c.Rules.Priority == nil {
		c.Rules.Priority = &Rule{ReStrs: make([]string, 0)}
	}
	if c.Rules.Versioning == nil {
		c.Rules.Versioning = &Rule{ReStrs: make([]string, 0)}
	}
	if c.Rules.Aliases == nil {
		c.Rules.Aliases = make(Aliases)
	}
	return c.Rules.Compile()
}

func (c *Config) SaveRules() error {
	f, err := os.OpenFile(c.RulesPath(), os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if c.Rules == nil {
		c.Rules = &Rules{
			Allow:      &Rule{ReStrs: make([]string, 0)},
			Skip:       &Rule{ReStrs: make([]string, 0)},
			Priority:   &Rule{ReStrs: make([]string, 0)},
			Versioning: &Rule{ReStrs: make([]string, 0)},
			Aliases:    make(Aliases),
		}
	}
	e := json.NewEncoder(f)
	e.SetIndent("", "  ")
	if err = e.Encode(c.Rules); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return c.LoadRules()
}

func (r *Rules) IsPriority(s string) bool {
	if r == nil || r.Priority == nil {
		return false
	}
	return r.Priority.Match(s)
}

func (r *Rules) IsVersioning(s string) bool {
	if r == nil || r.Versioning == nil {
		return false
	}
	return r.Versioning.Match(s)
}

// IsSkip reports whether a URL is excluded by the allow or skip rules.
// A nonempty allow list requires at least one match; skip always takes precedence.
func (r *Rules) IsSkip(s string) bool {
	if r == nil {
		return false
	}
	if r.Allow != nil && len(r.Allow.ReStrs) > 0 && !r.Allow.Match(s) {
		return true
	}
	return r.Skip != nil && r.Skip.Match(s)
}

func (r *Rule) Match(s string) bool {
	if len(r.ReStrs) == 0 {
		return false
	}
	if r.re == nil {
		if err := r.Compile(); err != nil {
			log.Debug().Err(err).Msg("Failed to compile rule regexp")
			return false
		}
	}
	return r.re.MatchString(s)
}

func (r *Rule) Compile() error {
	var err error
	rs := fmt.Sprintf("(%s)", strings.Join(r.ReStrs, ")|("))
	r.re, err = regexp.Compile(rs)
	return err
}

func (r *Rule) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.ReStrs)
}

func (r *Rule) UnmarshalJSON(data []byte) error {
	var rs []string
	if err := json.Unmarshal(data, &rs); err != nil {
		return err
	}
	r.ReStrs = rs
	return nil
}

func (r *Rules) Count() int {
	count := 0
	for _, rule := range []*Rule{r.Allow, r.Skip, r.Priority, r.Versioning} {
		if rule != nil {
			count += len(rule.ReStrs)
		}
	}
	return count
}

func (r *Rules) Compile() error {
	for _, rule := range []*Rule{r.Allow, r.Skip, r.Priority, r.Versioning} {
		if rule != nil {
			if err := rule.Compile(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Rules) ResolveAliases(s string) string {
	sp := strings.Fields(s)
	changed := false
	for i, ss := range sp {
		for k, v := range r.Aliases {
			if ss == k {
				sp[i] = v
				changed = true
			}
		}
	}
	if !changed {
		return s
	}
	return strings.Join(sp, " ")
}

// ValidateFields reports safe, stable paths without contacting the embedding service.
func (s SemanticSearch) ValidateFields() []FieldError {
	if !s.Enable {
		return nil
	}
	var fields []FieldError
	for _, field := range FieldCatalog() {
		if field.Category != "semantic_search" {
			continue
		}
		if _, message := field.Schema.ValidateValue(settingValue(&Config{SemanticSearch: s}, field.Path)); message != "" {
			fields = append(fields, FieldError{Path: field.Path, Code: "invalid_value", Message: message})
		}
	}
	if s.ChunkOverlap >= s.MaxContextLength && s.MaxContextLength > 0 {
		fields = append(fields, FieldError{Path: "semantic_search.chunk_overlap", Code: "invalid_value", Message: "Chunk overlap must be less than the maximum context length."})
	}
	if message := validateHTTPHeaders(s.Headers); message != "" {
		fields = append(fields, FieldError{Path: "semantic_search.headers", Code: "invalid_value", Message: message})
	}
	return fields
}

func (s SemanticSearch) Validate() error {
	if fields := s.ValidateFields(); len(fields) > 0 {
		return fields[0]
	}
	return nil
}

func (c *Config) semanticSearchErrors() []FieldError {
	fields := c.SemanticSearch.ValidateFields()
	dbType, _ := c.DatabaseConnection()
	if c.SemanticSearch.Enable && dbType == Psql && c.SemanticSearch.Dimensions > postgresHNSWMaxDimensions {
		fields = append(fields, FieldError{Path: "semantic_search.dimensions", Code: "invalid_value", Message: fmt.Sprintf("Embedding dimensions must not exceed %d when using PostgreSQL.", postgresHNSWMaxDimensions)})
	}
	return fields
}

func (c *Config) validateSemanticSearch() error {
	if fields := c.semanticSearchErrors(); len(fields) > 0 {
		return fields[0]
	}
	return nil
}

func (h Hotkeys) Validate() error {
	for k, v := range h.Web {
		if !slices.Contains(hotkeyActions, v) {
			return errors.New("unknown hotkey action: " + v)
		}
		if !hotkeyKeyRe.MatchString(k) {
			return errors.New("invalid hotkey definition: " + k)
		}
	}
	for _, v := range h.TUI {
		if !ValidTUIActions[Action(v)] {
			return errors.New("unknown tui hotkey action: " + v)
		}
	}
	return nil
}

func (h Hotkeys) ToJSON() template.JS {
	if h.Web == nil {
		b, _ := json.Marshal(map[string]string{})
		return template.JS(b)
	}
	b, err := json.Marshal(h.Web)
	if err != nil {
		return template.JS("")
	}
	return template.JS(b)
}

func (d *Directory) IsMatching(name string) bool {
	name = filepath.Base(name)
	if !d.IncludeHidden && strings.HasPrefix(name, ".") {
		return false
	}
	if len(d.Excludes) > 0 {
		for _, pattern := range d.Excludes {
			if matched, _ := filepath.Match(pattern, name); matched {
				return false
			}
		}
	}
	if len(d.Filetypes) > 0 {
		ext := strings.TrimPrefix(filepath.Ext(name), ".")
		if !slices.ContainsFunc(d.Filetypes, func(ft string) bool {
			return strings.EqualFold(ft, ext)
		}) {
			return false
		}
	}
	if len(d.Patterns) > 0 {
		for _, pattern := range d.Patterns {
			if matched, _ := filepath.Match(pattern, name); matched {
				return true
			}
		}
		return false
	}
	return true
}
