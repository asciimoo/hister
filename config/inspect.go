// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"gopkg.in/yaml.v3"
)

// ResolvePath uses the normal configuration search order. Explicit paths must
// exist. An empty result means defaults and environment variables are in use.
// Unlike runtime loading, inspection reports unreadable files instead of
// silently falling back to another configuration.
func ResolvePath(filename string, explicit bool) (string, error) {
	paths := []string{filename}
	if !explicit {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		paths = append(paths, getConfigSearchPaths(home)...)
	}
	for _, name := range paths {
		info, err := os.Stat(name)
		if !explicit && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("inspect config file: %w", err)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("config path %q is not a regular file", name)
		}
		return filepath.Abs(name)
	}
	return "", nil
}

// LoadForInspection decodes defaults, YAML and environment variables without
// creating runtime files or opening databases. Call Validate after applying
// command line overrides. Rules and the separate TUI config are not loaded.
func LoadForInspection(filename string, explicit bool) (*Config, string, error) {
	name, err := ResolvePath(filename, explicit)
	if err != nil {
		return nil, name, err
	}
	var raw []byte
	if name != "" {
		raw, err = os.ReadFile(name)
		if err != nil {
			return nil, name, fmt.Errorf("read config file: %w", err)
		}
	}
	v, err := loadViper(raw)
	if err != nil {
		return nil, name, errors.New("invalid configuration YAML")
	}
	c := CreateDefaultConfig()
	var metadata mapstructure.Metadata
	if err := v.Unmarshal(c, func(dc *mapstructure.DecoderConfig) { dc.Metadata = &metadata }); err != nil {
		// Decoder errors can include credential values. Do not echo them.
		var field *mapstructure.DecodeError
		if errors.As(err, &field) {
			return nil, name, fmt.Errorf("invalid configuration value at %s", field.Name())
		}
		return nil, name, errors.New("invalid configuration keys or value types")
	}
	if len(metadata.Unused) > 0 {
		slices.Sort(metadata.Unused)
		return nil, name, fmt.Errorf("unknown configuration keys: %s", strings.Join(metadata.Unused, ", "))
	}
	c.fname = name
	return c, name, c.normalize()
}

// Validate checks the main configuration without initializing runtime state.
func (c *Config) Validate() error {
	if err := c.validateBasic(); err != nil {
		return err
	}
	if err := c.UpdateBaseURL(c.Server.BaseURL); err != nil {
		return errors.New("invalid server.address or server.base_url; set an explicit base URL when listening on all interfaces")
	}
	u := c.parsedBaseURL
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("server.base_url must be an HTTP or HTTPS URL without a query or fragment")
	}
	if err := c.ValidatePublicMode(); err != nil {
		return err
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
	switch c.App.LogLevel {
	case "", "error", "err", "warning", "warn", "info", "debug", "trace":
	default:
		return errors.New("app.log_level must be error, warning, info, debug, or trace")
	}
	switch c.App.LogFormat {
	case "", "text", "json":
	default:
		return errors.New("app.log_format must be text or json")
	}
	switch c.Crawler.Backend {
	case "", "http", "chromedp", "bidi":
	default:
		return errors.New("crawler.backend must be http, chromedp, or bidi")
	}
	return nil
}

const redactedValue = "[REDACTED]"

// Redacted returns a detached YAML representation for diagnostic output.
func (c *Config) Redacted() (map[string]any, error) {
	b, err := yaml.Marshal(c)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := yaml.Unmarshal(b, &result); err != nil {
		return nil, err
	}
	redactMap(result)
	return result, nil
}

func redactMap(values map[string]any) {
	for key, value := range values {
		k := strings.ToLower(key)
		if k == "sensitive_content_patterns" {
			continue
		}
		if strings.Contains(k, "secret") || strings.Contains(k, "password") || k == "token" || strings.HasSuffix(k, "_token") || k == "api_key" || k == "authorization" || k == "extra_args" {
			if value != nil && value != "" {
				values[key] = redactedValue
			}
			continue
		}
		switch v := value.(type) {
		case map[string]any:
			if k == "headers" {
				for header := range v {
					v[header] = redactedValue
				}
			} else {
				redactMap(v)
			}
		case []any:
			for _, item := range v {
				if fields, ok := item.(map[string]any); ok {
					if k == "cookies" {
						fields["value"] = redactedValue
					}
					redactMap(fields)
				}
			}
		case string:
			if k == "database" && strings.Contains(v, "=") {
				values[key] = redactedValue
			} else if strings.Contains(v, "://") {
				values[key] = redactURL(v)
			}
		}
	}
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return redactedValue
	}
	if u.User != nil {
		u.User = url.User(redactedValue)
	}
	query := u.Query()
	for key := range query {
		query.Set(key, redactedValue)
	}
	u.RawQuery = query.Encode()
	if u.Fragment != "" {
		u.Fragment = redactedValue
	}
	return u.String()
}
