// SPDX-License-Identifier: AGPL-3.0-or-later

package languages

import (
	"slices"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  Resolution
	}{
		{" EN-us ", Resolution{"en-US", "en", "en", "en", "en"}},
		{"no-NO", Resolution{"nb-NO", "nb", "nb", "no", "no"}},
		{"in-ID", Resolution{"id-ID", "id", "id", "default", ""}},
		{"zh-hant-TW", Resolution{"zh-Hant-TW", "zh", "zh", "cjk", "zh"}},
		{"ja", Resolution{"ja", "ja", "ja", "cjk", "ja"}},
		{"ko-KR", Resolution{"ko-KR", "ko", "ko", "cjk", "ko"}},
		{"bg", Resolution{"bg", "bg", "bg", "default", ""}},
		{"uk-UA", Resolution{"uk-UA", "uk", "", "default", ""}},
		{"ckb", Resolution{"ckb", "ckb", "", "ckb", "ckb"}},
		{"cs", Resolution{"cs", "cs", "", "default", ""}},
		{"UNKNOWN", Resolution{"unknown", "unknown", "", "default", ""}},
		{"und", Resolution{"unknown", "unknown", "", "default", ""}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := Resolve(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("Resolve = %+v, %v; want %+v", got, err, tc.want)
			}
			if got.Index != "" && !IsIndexCode(got.Index) {
				t.Fatalf("unsafe index code %q", got.Index)
			}
		})
	}
}

func TestDetectorCatalogPreservesSupportedLanguages(t *testing.T) {
	want := strings.Fields("ar bg ca da de el en es eu fa fi fr ga hi hr hu hy id it ja ko nb nl pl pt ro ru sv tr zh")
	codes := DetectorCodes()
	if !slices.Equal(codes, want) {
		t.Fatalf("detection candidates = %v, want %v", codes, want)
	}
	names := DetectorNames()
	if len(names) != len(want) {
		t.Fatalf("got %d named detection candidates, want %d", len(names), len(want))
	}
	for _, code := range want {
		if names[code] == "" {
			t.Errorf("missing display name for %s", code)
		}
		resolved, err := Resolve(code)
		if err != nil || resolved.Detector != code {
			t.Errorf("candidate %s cannot be detected: %+v, %v", code, resolved, err)
		}
	}
	// Consumers may change their copies without changing the supported set.
	codes[0] = "ckb"
	names["en"] = "Changed"
	names["ckb"] = "Central Kurdish"
	delete(names, "hu")
	if !slices.Equal(DetectorCodes(), want) || DetectorNames()["en"] != "English" || DetectorNames()["hu"] != "Hungarian" || DetectorNames()["ckb"] != "" {
		t.Fatal("a caller changed the shared language registry")
	}
}

func TestAnalyzerAndStoredIndexRouting(t *testing.T) {
	want := map[string]string{
		"ar": "ar", "ckb": "ckb", "da": "da", "de": "de", "en": "en",
		"es": "es", "fa": "fa", "fi": "fi", "fr": "fr", "hi": "hi",
		"hr": "hr", "hu": "hu", "it": "it", "ja": "cjk", "ko": "cjk",
		"nb": "no", "nl": "nl", "pl": "pl", "pt": "pt", "ro": "ro",
		"ru": "ru", "sv": "sv", "tr": "tr", "zh": "cjk",
	}
	for code, analyzer := range want {
		index := code
		if code == "nb" {
			index = "no"
		}
		resolved, err := Resolve(code)
		if err != nil || resolved.Analyzer != analyzer || resolved.Index != index {
			t.Errorf("routing %s = %+v, %v; want analyzer %s and index %s", code, resolved, err, analyzer, index)
		}
		if !IsIndexCode(index) {
			t.Errorf("stored index %s is no longer readable", index)
		}
	}
	if !IsIndexCode("cjk") {
		t.Fatal("legacy combined CJK index is no longer readable")
	}
	for _, code := range []string{"", "bg", "hy", "nb", "zh-Hant", "../en"} {
		if IsIndexCode(code) {
			t.Errorf("accepted non-index code %q", code)
		}
	}
}

func TestRejectInvalidLanguageIdentity(t *testing.T) {
	for _, value := range []string{"", " ", "zz", "../en", "en/US", "en_US", "en--US", "en-zzzzzzz", "auto", "default", "standard", "x-custom", "qaa", "und-Latn", "en-x-custom", "en-u-ca-gregory", strings.Repeat("a", 129)} {
		if _, err := Resolve(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	for _, code := range []string{"nb", "en-US", "uk", "../en", "zz", "standard", "EN"} {
		if IsIndexCode(code) {
			t.Errorf("accepted index code %q", code)
		}
	}
}
