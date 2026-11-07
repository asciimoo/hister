package document

import (
	"slices"
	"testing"

	"golang.org/x/text/language"

	"github.com/asciimoo/hister/server/languages"
	"github.com/asciimoo/lingua-go"
)

func codesOf(t *testing.T, codes []string) []string {
	t.Helper()
	langs := ResolveLanguages(codes)
	out := make([]string, 0, len(langs))
	for _, l := range langs {
		out = append(out, languageCode(l))
	}
	slices.Sort(out)
	return out
}

func TestResolveLanguages(t *testing.T) {
	for _, tc := range []struct {
		name  string
		codes []string
		want  []string
	}{
		{"normalizes case and spacing", []string{"EN", " de ", "nl", "id"}, []string{"de", "en", "id", "nl"}},
		{"drops unsupported", []string{"en", "de", "xx"}, []string{"de", "en"}},
		{"legacy norwegian alias", []string{"no", "en"}, []string{"en", "nb"}},
		{"bokmal and regions", []string{"nb-NO", "en-US", "en-GB", "no"}, []string{"en", "nb"}},
		{"deprecated indonesian alias", []string{"in", "de-DE"}, []string{"de", "id"}},
		{"unsupported input does not select defaults", []string{"cs", "ckb", "unknown", "zz"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := codesOf(t, tc.codes); !slices.Equal(got, tc.want) {
				t.Fatalf("ResolveLanguages(%v) = %v, want %v", tc.codes, got, tc.want)
			}
		})
	}

	for _, empty := range [][]string{nil, {}} {
		if got, want := codesOf(t, empty), languages.DetectorCodes(); !slices.Equal(got, want) {
			t.Fatalf("ResolveLanguages(%v) = %v, want all supported languages %v", empty, got, want)
		}
	}
}

func TestResolveLanguagesPreservesModelOrder(t *testing.T) {
	codes := []string{"ja-JP", " EN-us ", "no", "en-GB", "nb-NO", "de-DE", "ckb"}
	original := slices.Clone(codes)
	var got []string
	for _, model := range ResolveLanguages(codes) {
		got = append(got, languageCode(model))
	}
	if want := []string{"nb", "de", "en", "ja"}; !slices.Equal(got, want) {
		t.Fatalf("resolved model order = %v, want %v", got, want)
	}
	if !slices.Equal(codes, original) {
		t.Fatalf("resolution changed configured language values: %v", codes)
	}
}

func TestResolveLanguagesReturnsIndependentSelections(t *testing.T) {
	for _, tc := range []struct {
		name  string
		codes []string
	}{
		{"nil selects all", nil},
		{"empty selects all", []string{}},
		{"restricted selection", []string{"en", "de"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			all := slices.Clone(ResolveLanguages(nil))
			selected := ResolveLanguages(tc.codes)
			want := slices.Clone(selected)
			if len(selected) == 0 {
				t.Fatal("expected a nonempty language selection")
			}
			// Restore the selection if a regression exposes shared state.
			t.Cleanup(func() { copy(selected, want) })
			selected[0] = lingua.Czech
			if got := ResolveLanguages(tc.codes); !slices.Equal(got, want) {
				t.Errorf("changing a returned selection affected the next selection: got %v, want %v", got, want)
			}
			if got := ResolveLanguages(nil); !slices.Equal(got, all) {
				t.Errorf("changing a returned selection affected the defaults: got %v, want %v", got, all)
			}
		})
	}
}

func TestUnsupportedLanguages(t *testing.T) {
	if got := UnsupportedLanguages([]string{"en", "de", "nl", "id"}); len(got) != 0 {
		t.Fatalf("UnsupportedLanguages = %v, want none", got)
	}
	got := UnsupportedLanguages([]string{"en", "xx", "zz"})
	if !slices.Equal(got, []string{"xx", "zz"}) {
		t.Fatalf("UnsupportedLanguages = %v, want [xx zz]", got)
	}
	// Czech has no bundled model, so it must be reported rather than silently ignored.
	if got := UnsupportedLanguages([]string{"cs"}); !slices.Equal(got, []string{"cs"}) {
		t.Fatalf("UnsupportedLanguages([cs]) = %v, want [cs]", got)
	}
	codes := []string{"EN-us", "no", "nb-NO", "in-ID", " ckb ", "uk-UA", "zz", " ckb ", "unknown", " "}
	want := []string{" ckb ", "uk-UA", "zz", " ckb ", "unknown", " "}
	if got := UnsupportedLanguages(codes); !slices.Equal(got, want) {
		t.Fatalf("unsupported inputs = %q, want original spellings and order %q", got, want)
	}
}

func TestDetectionRegistryMatchesBundledModels(t *testing.T) {
	models := make(map[string]bool)
	for _, model := range ResolveLanguages(nil) {
		models[languageCode(model)] = true
	}
	seen := make(map[string]bool)
	for _, base := range language.Supported.BaseLanguages() {
		resolved, err := languages.Resolve(base.String())
		if err != nil || resolved.Detector == "" {
			continue
		}
		if !models[resolved.Detector] {
			t.Errorf("candidate %s has no bundled model", resolved.Detector)
		}
		seen[resolved.Detector] = true
	}
	for code := range models {
		if !seen[code] {
			t.Errorf("bundled model %s is not a candidate", code)
		}
	}
}

// lingua's builder panics with fewer than two languages, so the constructor has
// to degrade to the null detector instead of passing the list through.
func TestNewLanguageDetectorForRequiresTwoLanguages(t *testing.T) {
	for _, codes := range [][]string{{"en"}, {"en", "en"}, {"xx"}} {
		d := NewLanguageDetectorFor(codes, true)
		if _, ok := d.(*nullLangDetector); !ok {
			t.Fatalf("NewLanguageDetectorFor(%v) = %T, want the null detector", codes, d)
		}
	}
}

func TestDetectLanguageRespectsAllowlist(t *testing.T) {
	const german = "Der schnelle braune Fuchs springt über den faulen Hund, während " +
		"der Ausschuss prüft, ob die vorgeschlagene Änderung angenommen werden soll."

	if got := NewLanguageDetectorFor([]string{"en", "de"}, true).DetectLanguage(german); got != "de" {
		t.Fatalf("DetectLanguage(german) with de allowed = %q, want \"de\"", got)
	}
	// A restricted detector must not report a language outside the list.
	// Uncertain text may be classified as unknown.
	if got := NewLanguageDetectorFor([]string{"en", "id"}, true).DetectLanguage(german); got == "de" {
		t.Fatal("DetectLanguage(german) returned \"de\" although de is not in the allowlist")
	}
}

func TestRestrictedLanguageDetectionRejectsUncertainText(t *testing.T) {
	for _, lowAccuracy := range []bool{false, true} {
		d := NewLanguageDetectorFor([]string{"en", "de", "nl", "id"}, lowAccuracy)
		for _, text := range []string{"", "12345 !?", "a", "test", "bonjour", "Hola mundo", "Это русский текст"} {
			if got := d.DetectLanguage(text); got != UnknownLanguage {
				t.Errorf("lowAccuracy=%v: DetectLanguage(%q) = %q, want unknown", lowAccuracy, text, got)
			}
		}
		if got := d.DetectLanguage("languages are awesome"); got != "en" {
			t.Errorf("lowAccuracy=%v: confident English detected as %q", lowAccuracy, got)
		}
	}
}

func TestDefaultLanguageDetectorUsesLowAccuracy(t *testing.T) {
	d := NewLanguageDetector()
	if got := d.DetectLanguage("a"); got != UnknownLanguage {
		t.Errorf("default detector classified a single Latin letter as %q, want unknown", got)
	}
	// Short phrases can be misclassified in low accuracy mode. Check ordinary
	// page content with enough letters for both modes to use trigram models.
	const english = "This document explains how a search engine stores web pages and helps people find useful information. " +
		"Readers can search through their saved documents and return to the original pages whenever they need more details."
	if got := d.DetectLanguage(english); got != "en" {
		t.Errorf("default detector classified English text as %q, want en", got)
	}
}

type fixedLanguageConfidence struct {
	language lingua.Language
	value    float64
}

func (v fixedLanguageConfidence) Language() lingua.Language { return v.language }
func (v fixedLanguageConfidence) Value() float64            { return v.value }

type fixedConfidenceDetector struct {
	lingua.LanguageDetector
	values []lingua.ConfidenceValue
}

func (d fixedConfidenceDetector) ComputeLanguageConfidenceValues(string) []lingua.ConfidenceValue {
	return d.values
}

func TestLanguageConfidenceCutoff(t *testing.T) {
	for _, tc := range []struct {
		name       string
		codes      []string
		confidence float64
		want       string
	}{
		{"below cutoff", []string{"en", "de"}, 0.799, UnknownLanguage},
		{"at cutoff", []string{"en", "de"}, 0.8, "en"},
		{"certain", []string{"en", "de"}, 1, "en"},
		{"unrestricted preserves winner", nil, 0.6, "en"},
		{"unrestricted rejects tie", nil, 0.5, UnknownLanguage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewLanguageDetectorFor(tc.codes, false).(*langDetector)
			d.detector = fixedConfidenceDetector{values: []lingua.ConfidenceValue{
				fixedLanguageConfidence{lingua.English, tc.confidence},
				fixedLanguageConfidence{lingua.German, 1 - tc.confidence},
			}}
			if got := d.DetectLanguage(evidenceEnglish); got != tc.want {
				t.Fatalf("DetectLanguage = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLanguageDetectionSelectsHighestQualifyingConfidence(t *testing.T) {
	// Use a lower cutoff to allow multiple normalized scores to qualify.
	// Lingua returns confidence values in descending order.
	for _, tc := range []struct {
		name   string
		values []lingua.ConfidenceValue
		want   string
	}{
		{
			name: "multiple qualifying languages",
			values: []lingua.ConfidenceValue{
				fixedLanguageConfidence{lingua.German, 0.5},
				fixedLanguageConfidence{lingua.English, 0.35},
				fixedLanguageConfidence{lingua.Dutch, 0.15},
			},
			want: "de",
		},
		{
			name: "tied highest scores remain unknown",
			values: []lingua.ConfidenceValue{
				fixedLanguageConfidence{lingua.German, 0.4},
				fixedLanguageConfidence{lingua.English, 0.4},
				fixedLanguageConfidence{lingua.Dutch, 0.2},
			},
			want: UnknownLanguage,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &langDetector{
				detector:      fixedConfidenceDetector{values: tc.values},
				minConfidence: 0.3,
			}
			if got := d.DetectLanguage(evidenceEnglish); got != tc.want {
				t.Fatalf("DetectLanguage = %q, want %q", got, tc.want)
			}
		})
	}
}
