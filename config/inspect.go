// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"gopkg.in/yaml.v3"
)

// ResolvePath uses the normal configuration search order. Explicit paths must
// exist. An empty result means defaults and environment variables are in use.
// Unreadable files are errors rather than permission to use another source.
func ResolvePath(filename string, explicit bool) (string, error) {
	paths := []string{filename}
	if !explicit {
		// Without a home directory the platform search paths are unavailable;
		// fall back to defaults and environment variables.
		if home, err := os.UserHomeDir(); err == nil {
			paths = append(paths, getConfigSearchPaths(home)...)
		}
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

// LoadForInspection composes defaults, managed YAML, main YAML, environment, and
// explicit overrides without creating runtime files or opening databases. Call Validate
// on the result. Rules and the separate TUI config are not loaded.
func LoadForInspection(filename string, explicit bool, overrides ...Override) (*Config, string, error) {
	name, err := ResolvePath(filename, explicit)
	if err != nil {
		return nil, name, err
	}
	c, err := loadSelected(name, true, overrides...)
	return c, name, err
}

// loadSelected composes a detached candidate without discovery or runtime IO.
// An empty name deliberately retains the absence of a main configuration file.
func loadSelected(name string, strict bool, overrides ...Override) (*Config, error) {
	var raw []byte
	if name != "" {
		var err error
		raw, err = os.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read config file: %w", err)
		}
	}
	inputs, err := loadInputs(raw, name)
	if err != nil {
		return nil, err
	}
	c, err := inputs.decode(strict)
	if err != nil {
		return nil, err
	}
	environment, err := c.environmentOverrides()
	if err != nil {
		return nil, err
	}
	if len(environment)+len(overrides) > 0 {
		if err := inputs.apply(append(environment, overrides...)...); err != nil {
			return nil, err
		}
		c, err = inputs.decode(strict)
		if err != nil {
			return nil, err
		}
	}
	if err := c.normalize(); err != nil {
		return nil, err
	}
	composition := &configComposition{
		mainDigest:    sha256.Sum256(raw),
		defaults:      inputs.defaults,
		authoritative: cloneInputValue(reflect.ValueOf(inputs.values.AllSettings())).Interface().(map[string]any),
		sources:       maps.Clone(inputs.sources),
	}
	if err := inputs.loadManaged(c.App.Directory); err != nil {
		return nil, err
	}
	c, err = inputs.decode(strict)
	if err != nil {
		return nil, err
	}
	c.fname = name
	composition.managed = inputs.managed
	composition.managedDigest = inputs.managedDigest
	c.composition = composition
	return c, c.normalize()
}

func decodeConfig(raw []byte, name string, strict bool) (*Config, error) {
	inputs, err := loadInputs(raw, name)
	if err != nil {
		return nil, err
	}
	return inputs.decode(strict)
}

func (inputs *configInputs) decode(strict bool) (*Config, error) {
	if inputs.defaults == nil {
		inputs.defaults = CreateDefaultConfig()
	}
	c := cloneInputValue(reflect.ValueOf(inputs.defaults)).Interface().(*Config)
	inputs.clearWebDefaults(reflect.ValueOf(c).Elem(), "")
	c.sources = maps.Clone(inputs.sources)
	var metadata mapstructure.Metadata
	if err := inputs.values.Unmarshal(c, func(dc *mapstructure.DecoderConfig) { dc.Metadata = &metadata }); err != nil {
		// Decoder errors can include credential values. Do not echo them.
		if field, ok := errors.AsType[*mapstructure.DecodeError](err); ok {
			return nil, fmt.Errorf("invalid configuration value at %s", field.Name())
		}
		return nil, errors.New("invalid configuration keys or value types")
	}
	if strict && len(metadata.Unused) > 0 {
		slices.Sort(metadata.Unused)
		return nil, fmt.Errorf("unknown configuration keys: %s", strings.Join(metadata.Unused, ", "))
	}
	for name, extractor := range c.Extractors {
		if extractor != nil {
			_, explicit := inputs.sources["extractors."+name+".enable"]
			extractor.enableOmitted = !explicit
		}
	}
	return c, nil
}

// Validate checks the main configuration without initializing runtime state.
func (c *Config) Validate() error {
	if err := c.validateBasic(); err != nil {
		return err
	}
	if err := c.UpdateBaseURL(c.Server.BaseURL); err != nil {
		return errors.New("invalid server.address or server.base_url; set an explicit base URL when listening on all interfaces or a Unix socket")
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
	if err := c.Indexer.Validate(); err != nil {
		return err
	}
	if err := c.validateOAuth(); err != nil {
		return err
	}
	switch c.Crawler.Backend {
	case "", "http", "chromedp", "bidi":
	default:
		return errors.New("crawler.backend must be http, chromedp, or bidi")
	}
	_, err := c.CompileSensitiveContentPatterns()
	return err
}

// ValidateFields aggregates independent candidate errors without returning raw
// validator errors, which can contain credentials or user supplied patterns.
// A shallow copy is sufficient for these validators: only normalization of
// scalar server fields and the parsed base URL mutates configuration.
func (c *Config) ValidateFields() []FieldError {
	candidate := *c
	fields := candidate.validateAppFields()
	check := func(path, message string, err error) {
		if err != nil {
			fields = append(fields, FieldError{Path: path, Code: "invalid_configuration", Message: message})
		}
	}
	check("indexer.pdf_parser", "Check the parser command, arguments, and positive timeout.", candidate.Indexer.PDFParser.Validate())
	check("server.max_batch_body_size", "Use a positive batch size within the supported integer range.", candidate.Server.validateMaxBatchBodySize())
	_, _, listenErr := candidate.Server.ListenEndpoint()
	check("server.address", "Use a TCP address or unix: followed by an absolute socket path.", listenErr)
	if err := candidate.UpdateBaseURL(candidate.Server.BaseURL); err != nil {
		check("server.base_url", "Set a valid base URL for the configured listen address.", err)
	} else {
		u := candidate.parsedBaseURL
		if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			check("server.base_url", "Use an HTTP or HTTPS URL without credentials, a query, or a fragment.", errors.New("invalid base URL"))
		}
	}
	check("app.public", "Public access requires a token or user handling.", candidate.ValidatePublicMode())
	check("hotkeys.web", "Use supported key combinations and actions.", candidate.Hotkeys.Validate())
	check("semantic_search", "Check the embedding endpoint, model, dimensions, and context length.", candidate.validateSemanticSearch())
	check("indexer", "Choose a supported detection accuracy and at least two distinct languages, or no language restriction.", candidate.Indexer.Validate())
	check("server.oauth", "Check provider names, required credentials, and provider settings.", candidate.validateOAuth())
	switch candidate.Crawler.Backend {
	case "", "http", "chromedp", "bidi":
	default:
		check("crawler.backend", "Choose http, chromedp, or bidi.", errors.New("invalid backend"))
	}
	_, err := candidate.CompileSensitiveContentPatterns()
	check("sensitive_content_patterns", "Use valid regular expressions that can be combined.", err)
	// Keep the existing complete validator authoritative as more checks are added.
	if err := candidate.Validate(); err != nil && len(fields) == 0 {
		check("", "The complete configuration is invalid. Check the inherited settings.", err)
	}
	return fields
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
		if strings.Contains(k, "secret") || strings.Contains(k, "password") || k == "token" || strings.HasSuffix(k, "_token") || k == "api_key" || k == "authorization" || k == "extra_args" || k == "args" {
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
