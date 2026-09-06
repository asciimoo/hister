package extractor

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"sync"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/extractor/extractors/bluesky"
	"github.com/asciimoo/hister/server/extractor/extractors/chatgpt"
	"github.com/asciimoo/hister/server/extractor/extractors/discourse"
	"github.com/asciimoo/hister/server/extractor/extractors/email"
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
)

var (
	ErrInvalidExtractor   = errors.New("invalid extractor")
	ErrDuplicateExtractor = errors.New("duplicate extractor")
	ErrExtractorNotFound  = errors.New("extractor not found")
)

// Registry owns an ordered collection of extractor instances.
type Registry struct {
	mu         sync.RWMutex
	extractors []Extractor
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
		&email.EmailExtractor{},
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
	r.extractors = append(r.extractors, nil)
	copy(r.extractors[index+1:], r.extractors[index:])
	r.extractors[index] = candidate
	return nil
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
		defaults := candidate.GetConfig()
		merged := &config.Extractor{
			Enable:  defaults.Enable,
			Options: make(map[string]any, len(defaults.Options)),
		}
		maps.Copy(merged.Options, defaults.Options)
		if user, ok := cfgs[strings.ToLower(candidate.Name())]; ok && user != nil {
			merged.Enable = user.Enable
			maps.Copy(merged.Options, user.Options)
		}
		if err := candidate.SetConfig(merged); err != nil {
			return fmt.Errorf("extractor %s: %w", candidate.Name(), err)
		}
	}
	return nil
}
