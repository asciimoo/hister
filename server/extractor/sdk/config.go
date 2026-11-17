package sdk

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/asciimoo/nogon/config"
)

// OptionMetadata is supplied by an extractor, never inferred from GetConfig.
// Defaults describe the installed extractor, not its current credentials.
type OptionMetadata struct {
	Key            string
	Label          string
	Description    string
	Schema         config.SettingSchema
	Default        any
	Effect         string
	ReadOnlyReason string
}

// OptionDescriber is optional. Undescribed options remain hidden and read only.
// Implementations must return fresh metadata independent of applied configuration.
type OptionDescriber interface {
	OptionMetadata() []OptionMetadata
}

var optionKey = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func (m OptionMetadata) Validate() error {
	if !optionKey.MatchString(m.Key) || m.Label == "" || m.Description == "" {
		return errors.New("option metadata requires a key, label, and description")
	}
	switch m.Effect {
	case "runtime", "reindex", "reembed":
	default:
		return errors.New("option metadata requires an explicit runtime or index effect")
	}
	if err := m.Schema.ValidateDefinition(); err != nil {
		return err
	}
	if m.Default != nil {
		if _, message := m.Schema.ValidateValue(m.Default); message != "" {
			return errors.New("option metadata has an invalid default")
		}
	}
	return nil
}

// ConfigSupport provides the standard configuration behavior for an extractor.
// Its zero value is enabled by default and rejects every option key.
// Embed it in an extractor to provide GetConfig and SetConfig automatically.
type ConfigSupport struct {
	config         *Config
	defaults       Config
	allowedOptions map[string]struct{}
	hasDefaults    bool
}

// NewConfigSupport returns a configuration store with custom defaults. Option
// keys present in defaults or allowedOptions are accepted by SetConfig.
func NewConfigSupport(defaults Config, allowedOptions ...string) ConfigSupport {
	allowed := make(map[string]struct{}, len(defaults.Options)+len(allowedOptions))
	for key := range defaults.Options {
		allowed[key] = struct{}{}
	}
	for _, key := range allowedOptions {
		allowed[key] = struct{}{}
	}
	return ConfigSupport{
		defaults:       copyConfig(defaults),
		allowedOptions: allowed,
		hasDefaults:    true,
	}
}

// GetConfig returns the applied configuration or a fresh copy of the
// defaults. The zero value defaults to an enabled extractor with no options.
func (s *ConfigSupport) GetConfig() *Config {
	if s.config != nil {
		return s.config
	}
	if !s.hasDefaults {
		return &Config{Enable: true, Options: map[string]any{}}
	}
	defaults := copyConfig(s.defaults)
	return &defaults
}

// SetConfig validates option names and stores the supplied configuration.
func (s *ConfigSupport) SetConfig(config *Config) error {
	for key := range config.Options {
		if _, ok := s.allowedOptions[key]; !ok {
			return fmt.Errorf("unknown option %q", key)
		}
	}
	s.config = config
	return nil
}

func copyConfig(config Config) Config {
	copy := config.Clone()
	if copy.Options == nil {
		copy.Options = make(map[string]any)
	}
	return *copy
}
