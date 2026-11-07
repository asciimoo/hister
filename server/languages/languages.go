// SPDX-License-Identifier: AGPL-3.0-or-later

// Package languages resolves document tags, detection candidates, and analyzer
// choices without importing the detector models or the search engine.
package languages

import (
	"fmt"
	"slices"
	"strings"

	"golang.org/x/text/language"
)

const (
	Unknown         = "unknown"
	DefaultAnalyzer = "default"
)

// Resolution keeps language identity independent of available text analysis.
type Resolution struct {
	Tag      string
	Base     string
	Detector string // Empty when no detection model is bundled.
	Analyzer string
	Index    string // Empty selects the default index; otherwise an allowlisted code.
}

// A definition records Hister's capabilities, not whether a language tag is
// valid. Resolve also accepts valid tags without a detector or custom analyzer.
type definition struct {
	name          string
	detector      bool
	analyzer      string // Empty selects DefaultAnalyzer.
	indexOverride string // Empty uses the language code when an analyzer exists.
}

// Only complete Bleve analyzers belong here; a stopword filter alone is not an
// analyzer. Detector support must match the model packs in server/document,
// which is checked by the bundled-model contract test.
var definitions = map[string]definition{
	"ar":  {name: "Arabic", detector: true, analyzer: "ar"},
	"bg":  {name: "Bulgarian", detector: true},
	"ca":  {name: "Catalan", detector: true},
	"ckb": {name: "Central Kurdish", analyzer: "ckb"},
	"da":  {name: "Danish", detector: true, analyzer: "da"},
	"de":  {name: "German", detector: true, analyzer: "de"},
	"el":  {name: "Greek", detector: true},
	"en":  {name: "English", detector: true, analyzer: "en"},
	"es":  {name: "Spanish", detector: true, analyzer: "es"},
	"eu":  {name: "Basque", detector: true},
	"fa":  {name: "Persian", detector: true, analyzer: "fa"},
	"fi":  {name: "Finnish", detector: true, analyzer: "fi"},
	"fr":  {name: "French", detector: true, analyzer: "fr"},
	"ga":  {name: "Irish", detector: true},
	"hi":  {name: "Hindi", detector: true, analyzer: "hi"},
	"hr":  {name: "Croatian", detector: true, analyzer: "hr"},
	"hu":  {name: "Hungarian", detector: true, analyzer: "hu"},
	"hy":  {name: "Armenian", detector: true},
	"id":  {name: "Indonesian", detector: true},
	"it":  {name: "Italian", detector: true, analyzer: "it"},
	"ja":  {name: "Japanese", detector: true, analyzer: "cjk"},
	"ko":  {name: "Korean", detector: true, analyzer: "cjk"},
	"nb":  {name: "Norwegian Bokmål", detector: true, analyzer: "no", indexOverride: "no"},
	"nl":  {name: "Dutch", detector: true, analyzer: "nl"},
	"pl":  {name: "Polish", detector: true, analyzer: "pl"},
	"pt":  {name: "Portuguese", detector: true, analyzer: "pt"},
	"ro":  {name: "Romanian", detector: true, analyzer: "ro"},
	"ru":  {name: "Russian", detector: true, analyzer: "ru"},
	"sv":  {name: "Swedish", detector: true, analyzer: "sv"},
	"tr":  {name: "Turkish", detector: true, analyzer: "tr"},
	"zh":  {name: "Chinese", detector: true, analyzer: "cjk"},
}

func (d definition) indexCode(code string) string {
	if d.analyzer == "" {
		return ""
	}
	if d.indexOverride != "" {
		return d.indexOverride
	}
	return code
}

// DetectorCodes lists the languages with bundled detection models.
func DetectorCodes() []string {
	codes := make([]string, 0, len(definitions))
	for code, definition := range definitions {
		if definition.detector {
			codes = append(codes, code)
		}
	}
	slices.Sort(codes)
	return codes
}

// DetectorNames resolves supported detection codes to English display names.
// The returned map can be changed without affecting language resolution.
func DetectorNames() map[string]string {
	names := make(map[string]string)
	for code, definition := range definitions {
		if definition.detector {
			names[code] = definition.name
		}
	}
	return names
}

// no was historically used for Bokmal in stored Hister documents. The other
// entries are deprecated ISO language codes accepted by BCP 47 canonicalization.
var aliases = map[string]string{"no": "nb", "in": "id", "iw": "he", "ji": "yi", "mo": "ro"}

// Resolve accepts known BCP 47 language tags with scripts, regions, and variants.
// Undefined languages use unknown. Extensions and private use are not document
// language identities and are rejected. Input never becomes an index filename.
func Resolve(value string) (Resolution, error) {
	value = strings.TrimSpace(value)
	invalid := func() (Resolution, error) { return Resolution{}, fmt.Errorf("invalid language tag %q", value) }
	if strings.EqualFold(value, Unknown) || strings.EqualFold(value, "und") {
		return Resolution{Tag: Unknown, Base: Unknown, Analyzer: DefaultAnalyzer}, nil
	}
	if value == "" || len(value) > 128 {
		return invalid()
	}
	for _, c := range value {
		if c == '-' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		return invalid()
	}
	tag, err := language.Parse(value)
	if err != nil {
		return invalid()
	}
	base, _, _ := tag.Raw()
	if base.String() == "und" || base.IsPrivateUse() || len(tag.Extensions()) != 0 {
		return invalid()
	}
	canonical := tag.String()
	code := base.String()
	if replacement, ok := aliases[code]; ok {
		canonical = replacement + strings.TrimPrefix(canonical, code)
		code = replacement
	}
	result := Resolution{Tag: canonical, Base: code, Analyzer: DefaultAnalyzer}
	definition := definitions[code]
	if definition.detector {
		result.Detector = code
	}
	if definition.analyzer != "" {
		result.Analyzer = definition.analyzer
	}
	result.Index = definition.indexCode(code)
	return result, nil
}

// StoredTags includes historical spellings so filters work before reindexing.
func StoredTags(tag string) []string {
	base, _, _ := strings.Cut(tag, "-")
	values := []string{tag}
	for alias, canonical := range aliases {
		if canonical == base {
			values = append(values, alias+strings.TrimPrefix(tag, base))
		}
	}
	return values
}

// IsIndexCode accepts only the finite set of language index names Hister has
// written. Keep legacy analyzer names readable independently of new routing.
func IsIndexCode(code string) bool {
	// Older versions combined Chinese, Japanese, and Korean in one CJK index.
	if code == "cjk" {
		return true
	}
	if code == "" {
		return false
	}
	for language, definition := range definitions {
		if definition.indexCode(language) == code {
			return true
		}
	}
	return false
}
