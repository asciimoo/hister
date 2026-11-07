package extractor

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/extractor/extractors/bluesky"
	"github.com/asciimoo/hister/server/extractor/extractors/chatgpt"
	"github.com/asciimoo/hister/server/extractor/extractors/discourse"
	"github.com/asciimoo/hister/server/extractor/extractors/duckduckgo"
	"github.com/asciimoo/hister/server/extractor/extractors/embeddedvideo"
	"github.com/asciimoo/hister/server/extractor/extractors/github"
	"github.com/asciimoo/hister/server/extractor/extractors/godoc"
	"github.com/asciimoo/hister/server/extractor/extractors/hackernews"
	"github.com/asciimoo/hister/server/extractor/extractors/jsonld"
	"github.com/asciimoo/hister/server/extractor/extractors/lobsters"
	"github.com/asciimoo/hister/server/extractor/extractors/markdown"
	"github.com/asciimoo/hister/server/extractor/extractors/mastodon"
	"github.com/asciimoo/hister/server/extractor/extractors/notion"
	"github.com/asciimoo/hister/server/extractor/extractors/org"
	"github.com/asciimoo/hister/server/extractor/extractors/reddit"
	"github.com/asciimoo/hister/server/extractor/extractors/stackexchange"
	"github.com/asciimoo/hister/server/extractor/extractors/twitter"
	"github.com/asciimoo/hister/server/extractor/extractors/wikipedia"
	"github.com/asciimoo/hister/server/extractor/extractors/ytdlp"
	"github.com/asciimoo/hister/server/extractor/sdk"
)

var (
	ErrInvalidExtractor   = errors.New("invalid extractor")
	ErrDuplicateExtractor = errors.New("duplicate extractor")
	ErrExtractorNotFound  = errors.New("extractor not found")
)

// Registry owns an ordered collection of extractor instances.
type Registry struct {
	mu              sync.RWMutex
	extractors      []Extractor
	enabledDefaults map[string]bool
}

// NewRegistry constructs a registry in the order supplied.
func NewRegistry(extractors ...Extractor) (*Registry, error) {
	registry := &Registry{}
	for _, candidate := range extractors {
		if err := registry.Register(candidate); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// DefaultExtractors returns fresh instances of the built in extractor chain.
func DefaultExtractors() []Extractor {
	return []Extractor{
		&markdown.MarkdownExtractor{},
		&org.OrgModeExtractor{},
		&embeddedvideo.EmbeddedVideoExtractor{},
		&discourse.DiscourseExtractor{},
		&jsonld.JSONLDExtractor{},
		&reddit.RedditExtractor{},
		&stackexchange.StackExchangeExtractor{},
		&godoc.GoDocExtractor{},
		&github.GitHubExtractor{},
		&lobsters.LobstersExtractor{},
		&hackernews.HackerNewsExtractor{},
		&wikipedia.WikipediaExtractor{},
		&mastodon.MastodonExtractor{},
		&bluesky.BlueskyExtractor{},
		&twitter.TwitterExtractor{},
		&notion.NotionExtractor{},
		&ytdlp.YtdlpExtractor{},
		&chatgpt.ChatGPTExtractor{},
		&duckduckgo.DuckDuckGoExtractor{},
		&readabilityExtractor{},
		&basicExtractor{},
	}
}

func mustNewRegistry(extractors ...Extractor) *Registry {
	registry, err := NewRegistry(extractors...)
	if err != nil {
		panic(err)
	}
	return registry
}

// NewDefaultRegistry returns an independent registry containing fresh built in
// extractor instances.
func NewDefaultRegistry() *Registry {
	return mustNewRegistry(DefaultExtractors()...)
}

var defaultRegistry = NewDefaultRegistry()

// DefaultRegistry returns the registry used by package level operations.
func DefaultRegistry() *Registry {
	return defaultRegistry
}

// Replace publishes fresh, already configured extractor instances. Existing
// callers keep their old snapshot until their operation completes.
func (r *Registry) Replace(next *Registry) {
	next.mu.RLock()
	extractors := append([]Extractor(nil), next.extractors...)
	defaults := maps.Clone(next.enabledDefaults)
	next.mu.RUnlock()
	r.mu.Lock()
	r.extractors = extractors
	r.enabledDefaults = defaults
	r.mu.Unlock()
}

// Extractors returns a snapshot of the registered instances in chain order.
func (r *Registry) Extractors() []Extractor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Extractor(nil), r.extractors...)
}

// Register appends an extractor to the registry.
func (r *Registry) Register(candidate Extractor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.validateLocked(candidate); err != nil {
		return err
	}
	r.rememberDefault(candidate)
	r.extractors = append(r.extractors, candidate)
	return nil
}

// RegisterBefore inserts an extractor before the named registered extractor.
func (r *Registry) RegisterBefore(name string, candidate Extractor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.validateLocked(candidate); err != nil {
		return err
	}
	index := -1
	for i, registered := range r.extractors {
		if strings.EqualFold(registered.Name(), name) {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("%w: %s", ErrExtractorNotFound, name)
	}
	r.rememberDefault(candidate)
	r.extractors = append(r.extractors, nil)
	copy(r.extractors[index+1:], r.extractors[index:])
	r.extractors[index] = candidate
	return nil
}

func (r *Registry) rememberDefault(candidate Extractor) {
	if r.enabledDefaults == nil {
		r.enabledDefaults = make(map[string]bool)
	}
	r.enabledDefaults[strings.ToLower(candidate.Name())] = candidate.GetConfig().Enable
}

func (r *Registry) validateLocked(candidate Extractor) error {
	if isNilExtractor(candidate) {
		return fmt.Errorf("%w: value is nil", ErrInvalidExtractor)
	}
	name := strings.TrimSpace(candidate.Name())
	if name == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidExtractor)
	}
	capabilities := candidate.Capabilities()
	if !capabilities.Enrich && !capabilities.Extract && !capabilities.Preview {
		return fmt.Errorf("%w: %s declares no capabilities", ErrInvalidExtractor, name)
	}
	if describer, ok := candidate.(sdk.OptionDescriber); ok {
		seen := make(map[string]bool)
		for _, option := range describer.OptionMetadata() {
			if err := option.Validate(); err != nil {
				return fmt.Errorf("%w: %s has invalid option metadata", ErrInvalidExtractor, name)
			}
			if seen[option.Key] {
				return fmt.Errorf("%w: %s has duplicate option metadata", ErrInvalidExtractor, name)
			}
			seen[option.Key] = true
		}
	}
	for _, registered := range r.extractors {
		if strings.EqualFold(registered.Name(), name) {
			return fmt.Errorf("%w: %s", ErrDuplicateExtractor, name)
		}
	}
	return nil
}

func isNilExtractor(candidate Extractor) bool {
	if candidate == nil {
		return true
	}
	value := reflect.ValueOf(candidate)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Init merges user configuration into the defaults of every registered
// extractor. Register all extractors before calling Init.
func (r *Registry) Init(cfgs map[string]*config.Extractor) error {
	for _, candidate := range r.Extractors() {
		defaults := candidate.GetConfig().Clone()
		merged := &config.Extractor{
			Enable:  defaults.Enable,
			Options: make(map[string]any, len(defaults.Options)),
		}
		maps.Copy(merged.Options, defaults.Options)
		if user, ok := cfgs[strings.ToLower(candidate.Name())]; ok && user != nil {
			merged.Enable = user.Enabled(defaults.Enable)
			maps.Copy(merged.Options, user.Clone().Options)
		}
		if describer, ok := candidate.(sdk.OptionDescriber); ok {
			for _, option := range describer.OptionMetadata() {
				if value, present := merged.Options[option.Key]; present {
					normalized, message := option.Schema.ValidateValue(value)
					if message != "" {
						return fmt.Errorf("extractor %s option %s: %s", candidate.Name(), option.Key, message)
					}
					merged.Options[option.Key] = normalized
				}
			}
		}
		if err := candidate.SetConfig(merged); err != nil {
			return fmt.Errorf("extractor %s: %w", candidate.Name(), err)
		}
	}
	return nil
}

// ConfigurationFields describes defaults and configured option keys. Extractor
// editing remains disabled until its category has controls and the secret
// preservation protocol. Undescribed options are explicitly unknown; supplied
// values never become defaults and no extractor is initialized here.
func (r *Registry) ConfigurationFields(cfgs map[string]*config.Extractor) []config.FieldMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var fields []config.FieldMetadata
	for _, candidate := range r.extractors {
		prefix := "extractors." + strings.ToLower(candidate.Name())
		fields = append(fields, config.FieldMetadata{Path: prefix + ".enable", Category: "extractors", Label: candidate.Name(), Description: candidate.Description(), Default: r.enabledDefaults[strings.ToLower(candidate.Name())], Schema: config.SettingSchema{Type: "boolean", Control: "switch", Collection: "none", Sensitivity: "none"}, Effect: "runtime", ReadOnlyReason: "Extractor editing is not available yet."})
		described := make(map[string]bool)
		if describer, ok := candidate.(sdk.OptionDescriber); ok {
			for _, option := range describer.OptionMetadata() {
				described[option.Key] = true
				field := config.FieldMetadata{Path: prefix + ".options." + option.Key, Category: "extractors", Label: option.Label, Description: option.Description, Schema: option.Schema, Effect: option.Effect, ReadOnlyReason: option.ReadOnlyReason}
				if field.ReadOnlyReason == "" {
					field.ReadOnlyReason = "Extractor editing is not available yet."
				}
				if option.Schema.Sensitivity == "none" {
					field.Default = option.Default
				}
				fields = append(fields, field)
			}
		}
		keys := make(map[string]struct{})
		for key := range candidate.GetConfig().Options {
			keys[key] = struct{}{}
		}
		if selected := cfgs[strings.ToLower(candidate.Name())]; selected != nil {
			for key := range selected.Options {
				keys[key] = struct{}{}
			}
		}
		for _, key := range slices.Sorted(maps.Keys(keys)) {
			if described[key] {
				continue
			}
			fields = append(fields, config.FieldMetadata{Path: prefix + ".options." + key, Category: "extractors", Label: key, Description: "This option has no editing metadata.", Schema: config.SettingSchema{Type: "unknown", Control: "none", Collection: "unknown", Sensitivity: "unknown"}, ReadOnlyReason: "Undescribed options are file managed."})
		}
	}
	return fields
}

// ConfigurationStates combines extractor defaults with explicit selections
// without configuring an extractor or modifying the supplied configuration.
// The config owner handles source locks and metadata based redaction.
func (r *Registry) ConfigurationStates(current *config.Config) []config.FieldState {
	snapshot := *current
	snapshot.Extractors = make(map[string]*config.Extractor)
	for _, candidate := range r.Extractors() {
		name := strings.ToLower(candidate.Name())
		merged := candidate.GetConfig().Clone()
		if selected := current.Extractors[name]; selected != nil {
			merged.Enable = selected.Enabled(merged.Enable)
			if merged.Options == nil {
				merged.Options = make(map[string]any)
			}
			maps.Copy(merged.Options, selected.Clone().Options)
		}
		snapshot.Extractors[name] = merged
	}
	return snapshot.FieldStates(r.ConfigurationFields(current.Extractors)...)
}

// ValidateConfiguration uses fresh built in instances and reports only safe
// field errors. It never publishes a registry, extracts content, or probes tools.
func ValidateConfiguration(cfgs map[string]*config.Extractor) []config.FieldError {
	r := NewDefaultRegistry()
	known := make(map[string]bool)
	var fields []config.FieldError
	for _, candidate := range r.Extractors() {
		name := strings.ToLower(candidate.Name())
		known[name] = true
		entry, present := cfgs[name]
		if !present {
			continue
		}
		invalid := entry == nil
		if !invalid {
			// This registry owns fresh instances. Initializing one at a time
			// collects independent failures instead of stopping at the first.
			single := mustNewRegistry(candidate)
			invalid = single.Init(map[string]*config.Extractor{name: entry}) != nil
		}
		if invalid {
			fields = append(fields, config.FieldError{Path: "extractors." + name, Code: "invalid_extractor", Message: "Check this extractor's option names, types, and values."})
		}
	}
	for _, name := range slices.Sorted(maps.Keys(cfgs)) {
		if !known[name] {
			fields = append(fields, config.FieldError{Path: "extractors", Code: "unknown_extractor", Message: "Use a registered lower case extractor name."})
		}
	}
	return fields
}
