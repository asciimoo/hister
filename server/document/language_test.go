package document

import (
	"slices"
	"testing"
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
		// Norwegian Bokmal is exposed as the generic "no" code, not lingua's "nb".
		{"uses generic norwegian code", []string{"no", "en"}, []string{"en", "no"}},
		{"rejects lingua's nb spelling", []string{"nb", "en"}, []string{"en"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := codesOf(t, tc.codes); !slices.Equal(got, tc.want) {
				t.Fatalf("ResolveLanguages(%v) = %v, want %v", tc.codes, got, tc.want)
			}
		})
	}

	for _, empty := range [][]string{nil, {}} {
		if got, want := len(ResolveLanguages(empty)), len(Languages); got != want {
			t.Fatalf("ResolveLanguages(%v) = %d languages, want all %d", empty, got, want)
		}
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
	// "cs" is commented out of Languages, so it must be reported rather than
	// silently ignored.
	if got := UnsupportedLanguages([]string{"cs"}); !slices.Equal(got, []string{"cs"}) {
		t.Fatalf("UnsupportedLanguages([cs]) = %v, want [cs]", got)
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
	// Outside the allowlist German can only be answered with one of the
	// configured languages, never "de".
	if got := NewLanguageDetectorFor([]string{"en", "id"}, true).DetectLanguage(german); got == "de" {
		t.Fatal("DetectLanguage(german) returned \"de\" although de is not in the allowlist")
	}
}
