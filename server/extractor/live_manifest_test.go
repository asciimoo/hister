package extractor

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/asciimoo/nogon/server/extractor/sdk"
)

const liveManifestPath = "live_cases.yaml"

type liveManifest struct {
	Version int        `json:"version" yaml:"version"`
	Cases   []liveCase `json:"cases"   yaml:"cases"`
}

type liveCase struct {
	Name            string         `json:"name"                      yaml:"name"`
	URL             string         `json:"url"                       yaml:"url"`
	DocumentURL     string         `json:"document_url,omitempty"    yaml:"document_url,omitempty"`
	Fetch           *bool          `json:"fetch,omitempty"           yaml:"fetch,omitempty"`
	RequiresBinary  string         `json:"requires_binary,omitempty" yaml:"requires_binary,omitempty"`
	Backend         string         `json:"backend"                   yaml:"backend"`
	BackendOptions  map[string]any `json:"backend_options,omitempty" yaml:"backend_options,omitempty"`
	Extractor       string         `json:"extractor"                 yaml:"extractor"`
	Timeout         int            `json:"timeout,omitempty"         yaml:"timeout,omitempty"`
	Match           *bool          `json:"match,omitempty"           yaml:"match,omitempty"`
	ExtractDecision string         `json:"extract_decision,omitempty" yaml:"extract_decision,omitempty"`
	RunChain        *bool          `json:"run_chain,omitempty"       yaml:"run_chain,omitempty"`
	Expect          liveExpect     `json:"expect,omitzero"           yaml:"expect,omitempty"`
}

type liveExpect struct {
	FinalURLContains   string              `json:"final_url_contains,omitempty" yaml:"final_url_contains,omitempty"`
	TitleContains      []string            `json:"title_contains,omitempty"     yaml:"title_contains,omitempty"`
	TextContains       []string            `json:"text_contains,omitempty"      yaml:"text_contains,omitempty"`
	TextNotContains    []string            `json:"text_not_contains,omitempty"  yaml:"text_not_contains,omitempty"`
	MinTextLength      int                 `json:"min_text_length,omitempty"     yaml:"min_text_length,omitempty"`
	Metadata           map[string]any      `json:"metadata,omitempty"           yaml:"metadata,omitempty"`
	MetadataContains   map[string][]string `json:"metadata_contains,omitempty"  yaml:"metadata_contains,omitempty"`
	MetadataMinimums   map[string]float64  `json:"metadata_minimums,omitempty"  yaml:"metadata_minimums,omitempty"`
	AbsentMetadata     []string            `json:"absent_metadata,omitempty"    yaml:"absent_metadata,omitempty"`
	SkipIndexing       *bool               `json:"skip_indexing,omitempty"       yaml:"skip_indexing,omitempty"`
	MinExtraDocuments  int                 `json:"min_extra_documents,omitempty" yaml:"min_extra_documents,omitempty"`
	ExtraTitleContains []string            `json:"extra_title_contains,omitempty" yaml:"extra_title_contains,omitempty"`
	ExtraTextContains  []string            `json:"extra_text_contains,omitempty" yaml:"extra_text_contains,omitempty"`
	PreviewDecision    string              `json:"preview_decision,omitempty"   yaml:"preview_decision,omitempty"`
	PreviewContains    []string            `json:"preview_contains,omitempty"   yaml:"preview_contains,omitempty"`
	PreviewNotContains []string            `json:"preview_not_contains,omitempty" yaml:"preview_not_contains,omitempty"`
	MinPreviewLength   int                 `json:"min_preview_length,omitempty"  yaml:"min_preview_length,omitempty"`
}

func TestLiveExtractorManifest(t *testing.T) {
	manifest, err := loadLiveManifest()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateLiveManifest(manifest); err != nil {
		t.Fatal(err)
	}
}

func loadLiveManifest() (*liveManifest, error) {
	data, err := os.ReadFile(liveManifestPath)
	if err != nil {
		return nil, fmt.Errorf("read live extractor manifest: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var manifest liveManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode live extractor manifest: %w", err)
	}
	return &manifest, nil
}

func validateLiveManifest(manifest *liveManifest) error {
	if manifest.Version != 1 {
		return fmt.Errorf("live extractor manifest version is %d, want 1", manifest.Version)
	}
	if len(manifest.Cases) == 0 {
		return fmt.Errorf("live extractor manifest has no cases")
	}
	seen := make(map[string]struct{}, len(manifest.Cases))
	extractors := DefaultRegistry().Extractors()
	covered := make(map[string]struct{}, len(extractors))
	for index, testCase := range manifest.Cases {
		prefix := fmt.Sprintf("live extractor case %d", index+1)
		if testCase.Name == "" {
			return fmt.Errorf("%s has no name", prefix)
		}
		if _, exists := seen[testCase.Name]; exists {
			return fmt.Errorf("duplicate live extractor case name %q", testCase.Name)
		}
		seen[testCase.Name] = struct{}{}
		parsedURL, err := url.Parse(testCase.URL)
		if err != nil || parsedURL.Hostname() == "" ||
			(parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			return fmt.Errorf("live extractor case %q has invalid URL %q", testCase.Name, testCase.URL)
		}
		if testCase.DocumentURL != "" {
			documentURL, err := url.Parse(testCase.DocumentURL)
			if err != nil || documentURL.Scheme == "" {
				return fmt.Errorf("live extractor case %q has invalid document URL %q", testCase.Name, testCase.DocumentURL)
			}
		}
		switch testCase.Backend {
		case "", "http", "chromedp", "bidi":
		default:
			return fmt.Errorf("live extractor case %q has unknown backend %q", testCase.Name, testCase.Backend)
		}
		if liveExtractorByName(testCase.Extractor) == nil {
			return fmt.Errorf("live extractor case %q names unknown extractor %q", testCase.Name, testCase.Extractor)
		}
		if liveBool(testCase.Match, true) {
			covered[strings.ToLower(testCase.Extractor)] = struct{}{}
		}
		if _, err := parseLiveDecision(testCase.ExtractDecision, sdk.ExtractorSuccess); err != nil {
			return fmt.Errorf("live extractor case %q: %w", testCase.Name, err)
		}
		if _, err := parseLiveDecision(testCase.Expect.PreviewDecision, sdk.ExtractorSuccess); err != nil {
			return fmt.Errorf("live extractor case %q: %w", testCase.Name, err)
		}
	}
	var missing []string
	for _, candidate := range extractors {
		if _, ok := covered[strings.ToLower(candidate.Name())]; !ok {
			missing = append(missing, candidate.Name())
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("live extractor manifest has no positive case for: %s", strings.Join(missing, ", "))
	}
	return nil
}

func liveExtractorByName(name string) Extractor {
	for _, candidate := range DefaultRegistry().Extractors() {
		if strings.EqualFold(candidate.Name(), name) {
			return candidate
		}
	}
	return nil
}

func parseLiveDecision(value string, defaultDecision sdk.Decision) (sdk.Decision, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return defaultDecision, nil
	case "success":
		return sdk.ExtractorSuccess, nil
	case "fallback":
		return sdk.ExtractorFallback, nil
	case "abort":
		return sdk.ExtractorAbort, nil
	default:
		return 0, fmt.Errorf("unknown extractor decision %q", value)
	}
}

func liveBool(value *bool, defaultValue bool) bool {
	if value == nil {
		return defaultValue
	}
	return *value
}
