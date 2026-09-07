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
	"net/netip"
	"net/url"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

type Config struct {
	fname                    string
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
	SearchURL              string `yaml:"search_url" mapstructure:"search_url"`
	AccessToken            string `yaml:"access_token" mapstructure:"access_token"`
	UserHandling           bool   `yaml:"user_handling" mapstructure:"user_handling"`
	Public                 bool   `yaml:"public" mapstructure:"public"`
	LogLevel               string `yaml:"log_level" mapstructure:"log_level"`
	LogFile                string `yaml:"log_file" mapstructure:"log_file"`
	LogFormat              string `yaml:"log_format" mapstructure:"log_format"`
	DebugSQL               bool   `yaml:"debug_sql" mapstructure:"debug_sql"`
	OpenResultsOnNewTab    bool   `yaml:"open_results_on_new_tab" mapstructure:"open_results_on_new_tab"`
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
	OAuth            map[string]*OAuthEntry `yaml:"oauth"                    mapstructure:"oauth"`
	OAuthOnly        bool                   `yaml:"oauth_only"               mapstructure:"oauth_only"`
}

func (s Server) MaxBatchBodyBytes() int64 {
	return s.MaxBatchBodySize << 20
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
}

type Directory struct {
	Path           string   `yaml:"path"              mapstructure:"path"`
	Label          string   `yaml:"label"             mapstructure:"label"`
	Filetypes      []string `yaml:"filetypes"         mapstructure:"filetypes"`
	Patterns       []string `yaml:"patterns"          mapstructure:"patterns"`
	Excludes       []string `yaml:"excludes"          mapstructure:"excludes"`
	IncludeHidden  bool     `yaml:"include_hidden"    mapstructure:"include_hidden"`
	DeleteOnRemove bool     `yaml:"delete_on_remove"  mapstructure:"delete_on_remove"`
	User           string   `yaml:"user"              mapstructure:"user"`
}

type Indexer struct {
	DetectLanguages bool         `yaml:"detect_languages" mapstructure:"detect_languages"`
	KeepStopwords   bool         `yaml:"keep_stopwords" mapstructure:"keep_stopwords"`
	Directories     []*Directory `yaml:"directories" mapstructure:"directories"`
	MaxFileSize     int64        `yaml:"max_file_size_mb" mapstructure:"max_file_size_mb"`
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

type Hotkeys struct {
	Web map[string]string `yaml:"web" mapstructure:"web"`
	TUI map[string]string `yaml:"-" mapstructure:"tui"`
}

type Rules struct {
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
	Enable  bool           `yaml:"enable" mapstructure:"enable"`
	Options map[string]any `yaml:"options" mapstructure:"options"`
}

// SemanticSearch holds configuration for optional vector similarity search.
type SemanticSearch struct {
	Enable                  bool              `yaml:"enable" mapstructure:"enable"`
	EmbeddingEndpoint       string            `yaml:"embedding_endpoint" mapstructure:"embedding_endpoint"`
	EmbeddingModel          string            `yaml:"embedding_model" mapstructure:"embedding_model"`
	EmbeddingTimeout        int               `yaml:"embedding_timeout" mapstructure:"embedding_timeout"`
	APIKey                  string            `yaml:"api_key" mapstructure:"api_key"`
	Headers                 map[string]string `yaml:"headers" mapstructure:"headers"`
	Dimensions              int               `yaml:"dimensions" mapstructure:"dimensions"`
	MaxContextLength        int               `yaml:"max_context_length" mapstructure:"max_context_length"`
	ChunkOverlap            int               `yaml:"chunk_overlap" mapstructure:"chunk_overlap"`
	MaxEmbeddingBatchSize   int               `yaml:"max_embedding_batch_size" mapstructure:"max_embedding_batch_size"`
	QueryPrefix             string            `yaml:"query_prefix" mapstructure:"query_prefix"`
	DocumentPrefix          string            `yaml:"document_prefix" mapstructure:"document_prefix"`
	SimilarityThreshold     float64           `yaml:"similarity_threshold" mapstructure:"similarity_threshold"`
	ResultLimit             int               `yaml:"result_limit" mapstructure:"result_limit"`
	SemanticWeight          float64           `yaml:"semantic_weight" mapstructure:"semantic_weight"`
	MaxEmbeddingConcurrency int               `yaml:"max_embedding_concurrency" mapstructure:"max_embedding_concurrency"`
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

func readConfigFile(filename string) ([]byte, string, error) {
	// If a specific config file is provided, use it directly
	b, err := os.ReadFile(filename)
	if err == nil {
		return b, filename, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, "", errors.New("configuration file not found. Use --config to specify a custom config file")
	}

	// List of paths to search for config file, in order of preference
	paths := getConfigSearchPaths(homeDir)

	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err == nil {
			// Check if this is the deprecated ~/.histerrc path and warn if so
			if strings.HasSuffix(path, ".histerrc") {
				log.Warn().
					Str("oldPath", path).
					Str("newPath", paths[0]).
					Msg("Configuration file found at deprecated location ~/.histerrc. Please move it to the recommended location for your OS")
			}
			return b, path, nil
		}
	}

	return nil, "", errors.New("configuration file not found. Use --config to specify a custom config file")
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

func loadViper(rawConfig []byte) (*viper.Viper, error) {
	v := viper.New()
	v.SetConfigType("yaml")

	bindEnvironment(v)

	if len(rawConfig) > 0 {
		if err := v.ReadConfig(bytes.NewBuffer(rawConfig)); err != nil {
			return nil, err
		}
	}

	return v, nil
}

func bindEnvironment(v *viper.Viper) {
	v.SetEnvPrefix("HISTER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "__"))
	v.AutomaticEnv()

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
		v.Set(normalizedKey, val)
		log.Debug().Str("env", parts[0]).Str("key", normalizedKey).Msg("Loaded configuration from environment variable")
	}
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

// Load reads and parses the configuration from the specified file.
func Load(filename string) (*Config, error) {
	b, fn, err := readConfigFile(filename)
	if err != nil {
		log.Debug().Msg("No config file found, using default config")
	}

	c, err := parseConfig(b)
	if err != nil {
		return nil, err
	}

	c.fname = fn
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
			LogLevel:               "info",
			OpenResultsOnNewTab:    false,
			RedirectOnNoResults:    true,
			DisplayExtractorConfig: false,
		},
		Server: Server{
			Address:          DefaultServerAddress,
			BaseURL:          DefaultServerBaseURL,
			Database:         "db.sqlite3",
			MaxBatchBodySize: DefaultMaxBatchBodySize,
		},
		Indexer: Indexer{
			DetectLanguages: true,
			KeepStopwords:   false,
			MaxFileSize:     1,
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
			Enable:                  false,
			EmbeddingEndpoint:       "http://localhost:11434/v1/embeddings",
			EmbeddingModel:          "qwen3-embedding:8b",
			EmbeddingTimeout:        300,
			APIKey:                  "",
			Headers:                 map[string]string{},
			Dimensions:              postgresHNSWMaxDimensions,
			MaxContextLength:        512,
			ChunkOverlap:            64,
			MaxEmbeddingBatchSize:   8,
			QueryPrefix:             "query: ",
			DocumentPrefix:          "",
			SimilarityThreshold:     0.1,
			ResultLimit:             50,
			SemanticWeight:          0.4,
			MaxEmbeddingConcurrency: 2,
		},
	}
}

func parseConfig(rawConfig []byte) (*Config, error) {
	v, err := loadViper(rawConfig)
	if err != nil {
		return nil, err
	}

	c := CreateDefaultConfig()
	if err := v.Unmarshal(&c); err != nil {
		return nil, err
	}
	return c, c.validateBasic()
}

func (c *Config) validateBasic() error {
	maxBatchBodySize := int64(^uint64(0)>>1) >> 20
	if c.Server.MaxBatchBodySize < 1 || c.Server.MaxBatchBodySize > maxBatchBodySize {
		return fmt.Errorf("server.max_batch_body_size must be between 1 and %d", maxBatchBodySize)
	}
	switch c.App.ColorScheme {
	case "automatic", "dark", "light":
	default:
		return errors.New("app.color_scheme must be one of automatic, dark, or light")
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

func (c *Config) normalize() error {
	if dataDir := os.Getenv("HISTER_DATA_DIR"); dataDir != "" {
		c.App.Directory = dataDir
	}

	if envPort := os.Getenv("HISTER_PORT"); envPort != "" {
		host, _, err := net.SplitHostPort(c.Server.Address)
		if err != nil || host == "" {
			host = c.Server.Address
		}
		c.Server.Address = net.JoinHostPort(host, envPort)
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
	if err := c.normalize(); err != nil {
		return err
	}
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
		if name == "oidc" && entry.ConfigurationURL == "" && entry.AuthURL == "" {
			return fmt.Errorf("oauth provider oidc: configuration_url or auth_url is required")
		}
	}
	return nil
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
	c.Server.Address = a
	if c.usesDefaultBaseURL {
		return c.UpdateBaseURL("")
	}
	return nil
}

func (c *Config) UpdateBaseURL(u string) error {
	// If the base URL is unspecified, it defaults to the listen address.
	if u == "" {
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
	if c.fname == "" {
		c.TUI = DefaultTUIConfig
		c.Hotkeys.TUI = copyMap(DefaultTUIHotkeys)
		c.fname = c.defaultConfigPath()
		tuiPath := filepath.Join(filepath.Dir(c.fname), "tui.yaml")
		if _, err := os.Stat(tuiPath); os.IsNotExist(err) {
			if err := c.SaveTUIConfig(); err != nil {
				log.Debug().Err(err).Msg("Failed to create tui.yaml")
			}
		}
		return
	}
	tuiPath := filepath.Join(filepath.Dir(c.fname), "tui.yaml")
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
	if c.fname == "" {
		c.fname = c.defaultConfigPath()
		if err := os.MkdirAll(filepath.Dir(c.fname), 0o755); err != nil {
			return err
		}
	}
	tuiPath := filepath.Join(filepath.Dir(c.fname), "tui.yaml")
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
	err = json.Unmarshal(b, &c.Rules)
	if err != nil {
		return err
	}
	if c.Rules == nil {
		c.Rules = &Rules{}
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

func (r *Rules) IsSkip(s string) bool {
	if r == nil || r.Skip == nil {
		return false
	}
	return r.Skip.Match(s)
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
	return len(r.Skip.ReStrs) + len(r.Priority.ReStrs) + len(r.Versioning.ReStrs)
}

func (r *Rules) Compile() error {
	if err := r.Skip.Compile(); err != nil {
		return err
	}
	if err := r.Priority.Compile(); err != nil {
		return err
	}
	if err := r.Versioning.Compile(); err != nil {
		return err
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

func (s SemanticSearch) Validate() error {
	if !s.Enable {
		return nil
	}
	if s.EmbeddingEndpoint == "" {
		return errors.New("semantic_search.embedding_endpoint must not be empty when semantic search is enabled")
	}
	if s.EmbeddingModel == "" {
		return errors.New("semantic_search.embedding_model must not be empty when semantic search is enabled")
	}
	if s.Dimensions <= 0 {
		return fmt.Errorf("semantic_search.dimensions must be a positive integer, got %d", s.Dimensions)
	}
	if s.MaxContextLength <= 0 {
		return fmt.Errorf("semantic_search.max_context_length must be a positive integer, got %d", s.MaxContextLength)
	}
	return nil
}

func (c *Config) validateSemanticSearch() error {
	if err := c.SemanticSearch.Validate(); err != nil {
		return err
	}
	if !c.SemanticSearch.Enable {
		return nil
	}
	dbType, _ := c.DatabaseConnection()
	if dbType == Psql && c.SemanticSearch.Dimensions > postgresHNSWMaxDimensions {
		return fmt.Errorf(
			"semantic_search.dimensions must not exceed %d when using PostgreSQL, got %d",
			postgresHNSWMaxDimensions,
			c.SemanticSearch.Dimensions,
		)
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
