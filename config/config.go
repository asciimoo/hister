// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPLv3+

package config

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

type Config struct {
	fname                    string
	App                      App               `yaml:"app"`
	Server                   Server            `yaml:"server"`
	Import                   Import            `yaml:"import"`
	Hotkeys                  Hotkeys           `yaml:"hotkeys"`
	SensitiveContentPatterns map[string]string `yaml:"sensitive_content_patterns"`
	Rules                    *Rules            `yaml:"-"`
	secretKey                []byte
}

type App struct {
	Directory           string `yaml:"directory"`
	SearchURL           string `yaml:"search_url"`
	LogLevel            string `yaml:"log_level"`
	DebugSQL            bool   `yaml:"debug_sql"`
	OpenResultsOnNewTab bool   `yaml:"open_results_on_new_tab"`
}

type Import struct {
	SkipPrivateURLs bool `yaml:"skip_private_urls"`
}

type Server struct {
	Address  string `yaml:"address"`
	BaseURL  string `yaml:"base_url"`
	Database string `yaml:"database"`
}

type Hotkeys struct {
	Web map[string]string `yaml:"web"`
	TUI map[string]string `yaml:"tui"`
}

type Rules struct {
	Skip     *Rule   `json:"skip"`
	Priority *Rule   `json:"priority"`
	Aliases  Aliases `json:"aliases"`
}

type Rule struct {
	ReStrs []string
	re     *regexp.Regexp
}

type Aliases map[string]string

var (
	secretKeyFilename                = ".secret_key"
	hotkeyKeyRe       *regexp.Regexp = regexp.MustCompile(`^((ctrl|alt|meta)\+)?([a-z0-9/?]|enter|tab|arrow(up|down|right|left))$`)
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
	}
	tuiHotkeyActions = []string{
		"quit",
		"toggle_help",
		"toggle_focus",
		"scroll_up",
		"scroll_down",
		"open_result",
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
	b, err := os.ReadFile(filename)
	if err == nil {
		return b, filename, nil
	}
	homeDir, err := os.UserHomeDir()
	if err == nil {
		filename = filepath.Join(homeDir, ".histerrc")
		b, err = os.ReadFile(filename)
		if err == nil {
			return b, filename, nil
		}
		filename = filepath.Join(homeDir, ".config/hister/config.yml")
		b, err = os.ReadFile(filename)
		if err == nil {
			return b, filename, nil
		}
	}
	return b, "", errors.New("configuration file not found. Use --config to specify a custom config file")
}

// Load reads and parses the configuration from the specified file.
func Load(filename string) (*Config, error) {
	b, fn, err := readConfigFile(filename)
	var c *Config
	if err != nil {
		log.Debug().Msg("No config file found, using default config")
		c = CreateDefaultConfig()
	} else {
		c, err = parseConfig(b)
		if err != nil {
			return nil, err
		}
	}
	c.fname = fn
	return c, c.init()
}

// CreateDefaultConfig returns a new Config with default values.
func CreateDefaultConfig() *Config {
	return &Config{
		App: App{
			SearchURL:           "https://google.com/search?q={query}",
			Directory:           getDefaultDataDir(),
			LogLevel:            "info",
			OpenResultsOnNewTab: false,
		},
		Import: Import{
			SkipPrivateURLs: true,
		},
		Server: Server{
			Address:  "127.0.0.1:4433",
			Database: "db.sqlite3",
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
			},
			TUI: map[string]string{
				"ctrl+c": "quit",
				"q":      "quit",
				"?":      "toggle_help",
				"tab":    "toggle_focus",
				"up":     "scroll_up",
				"k":      "scroll_up",
				"down":   "scroll_down",
				"j":      "scroll_down",
				"enter":  "open_result",
				"d":      "delete_result",
				"esc":    "toggle_focus", // Safely map esc away from quit
			},
		},
		SensitiveContentPatterns: map[string]string{
			"aws_access_key":      `AKIA[0-9A-Z]{16}`,
			"aws_secret_key":      `(?i)aws(.{0,20})?(secret)?(.{0,20})?['"][0-9a-zA-Z\/+]{40}['"]`,
			"generic_private_key": `-----BEGIN ((RSA|EC|DSA) )?PRIVATE KEY-----`,
			"github_token":        `(ghp|gho|ghu|ghs|ghr)_[a-zA-Z0-9]{36}`,
			"ssh_private_key":     `-----BEGIN OPENSSH PRIVATE KEY-----`,
			"pgp_private_key":     `-----BEGIN PGP PRIVATE KEY BLOCK-----`,
		},
	}
}

func parseConfig(rawConfig []byte) (*Config, error) {
	c := CreateDefaultConfig()
	err := yaml.Unmarshal(rawConfig, &c)
	if err != nil {
		return nil, err
	}
	if c.Server.BaseURL != "" {
		pu, err := url.Parse(c.Server.BaseURL)
		if err != nil || pu.Scheme == "" || pu.Host == "" {
			return nil, errors.New("invalid Server.BaseURL - use 'https://domain.tld/xy/' format")
		}
		c.Server.BaseURL = strings.TrimSuffix(c.Server.BaseURL, "/")
	}
	return c, nil
}

func (c *Config) init() error {
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

	if c.Server.BaseURL == "" {
		if strings.HasPrefix(c.Server.Address, "0.0.0.0") {
			return errors.New("server: base_url must be specified when listening on 0.0.0.0")
		}
		c.Server.BaseURL = fmt.Sprintf("http://%s", c.Server.Address)
	}
	if strings.HasPrefix(c.App.Directory, "~/") {
		u, _ := user.Current()
		dir := u.HomeDir
		c.App.Directory = filepath.Join(dir, c.App.Directory[2:])
	}
	if err := os.MkdirAll(c.App.Directory, os.ModePerm); err != nil {
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

		err = os.MkdirAll(c.App.Directory, os.ModePerm)
		if err != nil {
			return err
		}
	}
	if err := c.Hotkeys.Validate(); err != nil {
		return err
	}
	sPath := c.FullPath(secretKeyFilename)
	b, err := os.ReadFile(sPath)
	if err != nil {
		c.secretKey = []byte(rand.Text() + rand.Text())
		if err := os.WriteFile(sPath, c.secretKey, 0o644); err != nil {
			return fmt.Errorf("failed to create secret key file: %w", err)
		}
	} else {
		c.secretKey = b
	}
	return c.LoadRules()
}

func (c *Config) SecretKey() []byte {
	return c.secretKey
}

func (c *Config) FullPath(f string) string {
	if strings.HasPrefix(f, "/") {
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

func (c *Config) IndexPath() string {
	return c.FullPath("index.db")
}

func (c *Config) RulesPath() string {
	return c.FullPath("rules.json")
}

func (c *Config) DatabaseConnection() string {
	return c.FullPath(c.Server.Database)
}

func (c *Config) Filename() string {
	if c.fname == "" {
		return "*Default Config*"
	}
	return c.FullPath(c.fname)
}

func (c *Config) BaseURL(u string) string {
	if strings.HasPrefix(u, "/") && strings.HasSuffix(c.Server.BaseURL, "/") {
		u = u[1:]
	}
	if !strings.HasPrefix(u, "/") && !strings.HasSuffix(c.Server.BaseURL, "/") {
		u = "/" + u
	}
	return c.Server.BaseURL + u
}

func (c *Config) IsSameHost(h string) bool {
	bu, err := url.Parse(c.BaseURL(""))
	if err != nil {
		return false
	}
	ru, err := url.Parse(h)
	if err != nil {
		return false
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
	u, err := url.Parse(c.Server.BaseURL)
	if err != nil {
		return ""
	}
	return u.Host
}

func (c *Config) WebSocketURL() string {
	if strings.HasPrefix(c.BaseURL("/"), "https://") {
		return fmt.Sprintf("wss://%s/search", c.Host())
	}
	return fmt.Sprintf("ws://%s/search", c.Host())
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
	if c.Rules.Aliases == nil {
		c.Rules.Aliases = make(Aliases)
	}
	return c.Rules.Compile()
}

func (c *Config) SaveRules() error {
	f, err := os.OpenFile(c.RulesPath(), os.O_TRUNC|os.O_WRONLY|os.O_CREATE, os.ModePerm)
	if err != nil {
		return err
	}
	defer f.Close()
	if c.Rules == nil {
		c.Rules = &Rules{
			Skip:     &Rule{ReStrs: make([]string, 0)},
			Priority: &Rule{ReStrs: make([]string, 0)},
			Aliases:  make(Aliases),
		}
	}
	e := json.NewEncoder(f)
	e.SetIndent("", "  ")
	err = e.Encode(c.Rules)
	if err != nil {
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

func (r *Rules) Compile() error {
	if err := r.Skip.Compile(); err != nil {
		return err
	}
	if err := r.Priority.Compile(); err != nil {
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
		if !slices.Contains(tuiHotkeyActions, v) {
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
