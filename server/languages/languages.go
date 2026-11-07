// SPDX-License-Identifier: AGPL-3.0-or-later

// Package languages resolves document tags, detection candidates, and analyzer
// choices without importing the detector models or the search engine.
package languages

import (
	"fmt"
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

// Only these languages have complete Bleve analyzers. Other language packages
// may provide stop word filters without providing an analyzer.
var analyzers = map[string]string{
	"ar": "ar", "ckb": "ckb", "da": "da", "de": "de", "en": "en",
	"es": "es", "fa": "fa", "fi": "fi", "fr": "fr", "hi": "hi",
	"hr": "hr", "hu": "hu", "it": "it", "ja": "cjk", "ko": "cjk",
	"nb": "no", "nl": "nl", "pl": "pl", "pt": "pt", "ro": "ro",
	"ru": "ru", "sv": "sv", "tr": "tr", "zh": "cjk",
}

// Keep this set in sync with the model packs in server/document. A contract
// test verifies that each candidate has a bundled model and vice versa.
const detectorCodes = " ar bg ca da de el en es eu fa fi fr ga hi hr hu hy id it ja ko nb nl pl pt ro ru sv tr zh "

// DetectorCodes lists the languages with bundled detection models.
func DetectorCodes() []string { return strings.Fields(detectorCodes) }

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
	if strings.Contains(detectorCodes, " "+code+" ") {
		result.Detector = code
	}
	if analyzer, ok := analyzers[code]; ok {
		result.Analyzer = analyzer
		result.Index = code
		if code == "nb" {
			result.Index = "no"
		}
	}
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
	if code == "no" || code == "cjk" {
		return true
	}
	_, ok := analyzers[code]
	return ok && code != "nb"
}
