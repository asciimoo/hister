// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPLv3+

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

type Config struct {
	fname  string
	App    App    `yaml:"app"`
	Server Server `yaml:"server"`
	Rules  *Rules `yaml:"-"`
}

type App struct {
	Directory string `yaml:"directory"`
	SearchURL string `yaml:"search_url"`
	LogLevel  string `yaml:"log_level"`
	DebugSQL  bool   `yaml:"debug_sql"`
}

type Server struct {
	Address  string `yaml:"address"`
	BaseURL  string `yaml:"base_url"`
	Database string `yaml:"database"`
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
			SearchURL: "https://google.com/search?q={query}",
			Directory: "~/.config/hister/",
			LogLevel:  "info",
		},
		Server: Server{
			Address:  "127.0.0.1:4433",
			Database: "db.sqlite3",
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
		if strings.HasSuffix(c.Server.BaseURL, "/") {
			c.Server.BaseURL = c.Server.BaseURL[:len(c.Server.BaseURL)-1]
		}
	}
	return c, nil
}

func (c *Config) init() error {
	if c.Server.BaseURL == "" {
		c.Server.BaseURL = fmt.Sprintf("http://%s", c.Server.Address)
	}
	if strings.HasPrefix(c.App.Directory, "~/") {
		u, _ := user.Current()
		dir := u.HomeDir
		c.App.Directory = filepath.Join(dir, c.App.Directory[2:])
	}
	err := os.MkdirAll(c.App.Directory, os.ModePerm)
	if err != nil {
		return err
	}
	return c.LoadRules()
}

func (c *Config) FullPath(f string) string {
	if strings.HasPrefix(f, "/") {
		return f
	}
	if strings.HasPrefix(f, ".") {
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

func (c *Config) Host() string {
	u, err := url.Parse(c.Server.BaseURL)
	if err != nil {
		return ""
	}
	return u.Host
}

func (c *Config) WebSocketURL() string {
	return fmt.Sprintf("ws://%s/search", c.Host())
}

func (c *Config) LoadRules() error {
	b, err := os.ReadFile(c.RulesPath())
	if err != nil {
		return err
	}
	err = json.Unmarshal(b, &c.Rules)
	if err != nil {
		return err
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
	if r.re == nil {
		if len(r.ReStrs) == 0 {
			return false
		}
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
