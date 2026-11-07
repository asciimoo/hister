package document

import (
	"strings"
	"unicode"

	"github.com/asciimoo/hister/server/languages"

	// register lingua-go language packs
	"github.com/asciimoo/lingua-go"
	_ "github.com/asciimoo/lingua-go/language-models/ar"
	_ "github.com/asciimoo/lingua-go/language-models/bg"
	_ "github.com/asciimoo/lingua-go/language-models/ca"
	_ "github.com/asciimoo/lingua-go/language-models/da"
	_ "github.com/asciimoo/lingua-go/language-models/de"
	_ "github.com/asciimoo/lingua-go/language-models/el"
	_ "github.com/asciimoo/lingua-go/language-models/en"
	_ "github.com/asciimoo/lingua-go/language-models/es"
	_ "github.com/asciimoo/lingua-go/language-models/eu"
	_ "github.com/asciimoo/lingua-go/language-models/fa"
	_ "github.com/asciimoo/lingua-go/language-models/fi"
	_ "github.com/asciimoo/lingua-go/language-models/fr"
	_ "github.com/asciimoo/lingua-go/language-models/ga"
	_ "github.com/asciimoo/lingua-go/language-models/hi"
	_ "github.com/asciimoo/lingua-go/language-models/hr"
	_ "github.com/asciimoo/lingua-go/language-models/hu"
	_ "github.com/asciimoo/lingua-go/language-models/hy"
	_ "github.com/asciimoo/lingua-go/language-models/id"
	_ "github.com/asciimoo/lingua-go/language-models/it"
	_ "github.com/asciimoo/lingua-go/language-models/ja"
	_ "github.com/asciimoo/lingua-go/language-models/ko"
	_ "github.com/asciimoo/lingua-go/language-models/nb"
	_ "github.com/asciimoo/lingua-go/language-models/nl"
	_ "github.com/asciimoo/lingua-go/language-models/pl"
	_ "github.com/asciimoo/lingua-go/language-models/pt"
	_ "github.com/asciimoo/lingua-go/language-models/ro"
	_ "github.com/asciimoo/lingua-go/language-models/ru"
	_ "github.com/asciimoo/lingua-go/language-models/sv"
	_ "github.com/asciimoo/lingua-go/language-models/tr"
	_ "github.com/asciimoo/lingua-go/language-models/zh"
)

const UnknownLanguage = languages.Unknown

var Languages = []lingua.Language{
	lingua.Arabic,    // ar
	lingua.Bokmal,    // nb
	lingua.Bulgarian, // bg
	lingua.Catalan,   // ca
	lingua.Chinese,   // zh, uses the CJK analyzer
	// lingua.Czech,      // cs
	lingua.Danish,     // da
	lingua.German,     // de
	lingua.Greek,      // el
	lingua.English,    // en
	lingua.Spanish,    // es
	lingua.Basque,     // eu
	lingua.Persian,    // fa
	lingua.Finnish,    // fi
	lingua.French,     // fr
	lingua.Irish,      // ga
	lingua.Hindi,      // hi
	lingua.Croatian,   // hr
	lingua.Hungarian,  // hu
	lingua.Armenian,   // hy
	lingua.Indonesian, // id
	lingua.Italian,    // it
	lingua.Japanese,   // ja, uses the CJK analyzer
	lingua.Korean,     // ko, uses the CJK analyzer
	lingua.Dutch,      // nl
	lingua.Polish,     // pl
	lingua.Portuguese, // pt
	lingua.Romanian,   // ro
	lingua.Russian,    // ru
	lingua.Swedish,    // sv
	lingua.Turkish,    // tr
}

// languageCode returns the ISO 639-1 code Hister uses for a lingua language.
func languageCode(l lingua.Language) string {
	return strings.ToLower(l.IsoCode639_1().String())
}

// SupportedLanguages maps each bundled model's canonical code to lingua.
var SupportedLanguages = func() map[string]lingua.Language {
	m := make(map[string]lingua.Language, len(Languages))
	for _, l := range Languages {
		m[languageCode(l)] = l
	}
	return m
}()

// UnsupportedLanguages returns the codes that ResolveLanguages would drop, so
// callers can reject a misspelled configuration instead of silently changing
// the set of detection candidates.
func UnsupportedLanguages(codes []string) []string {
	_, unsupported := languages.NormalizeCandidates(codes)
	return unsupported
}

// ResolveLanguages maps language tags onto lingua models, preserving the
// order of Languages and dropping codes that are not supported. An empty list
// resolves to every supported language.
func ResolveLanguages(codes []string) []lingua.Language {
	if len(codes) == 0 {
		return Languages
	}
	wanted, _ := languages.NormalizeCandidates(codes)
	langs := make([]lingua.Language, 0, len(wanted))
	for _, l := range Languages {
		if _, ok := wanted[languageCode(l)]; ok {
			langs = append(langs, l)
		}
	}
	return langs
}

// LanguageDetector detects the language of a text.
type LanguageDetector interface {
	DetectLanguage(string) string
}

// EvidenceLanguageDetector exposes the evidence behind a language decision.
// Detectors implementing only LanguageDetector remain supported.
type EvidenceLanguageDetector interface {
	LanguageDetector
	DetectLanguageWithEvidence(string) languages.Detection
}

type nullLangDetector struct{}

type langDetector struct {
	detector      lingua.LanguageDetector
	minConfidence float64
}

// restrictedLanguageMinConfidence rejects ambiguous results when the caller
// narrows the candidate languages. This is a heuristic, not a calibrated
// probability that the text belongs to one of those languages: lingua's scores
// are relative to the configured candidates, so excluded languages can still
// receive a confident but incorrect classification.
const (
	restrictedLanguageMinConfidence = 0.8
	defaultLanguageMinConfidence    = 0.5
	languageMinMargin               = 0.15
	minimumLanguageLetters          = 12
	minimumCJKLetters               = 4
)

func NewLanguageDetector() LanguageDetector {
	return NewLanguageDetectorFor(nil, true)
}

// NewLanguageDetectorFor builds a detector limited to the base languages of
// the given tags, or to every supported language when codes is empty.
//
// Both arguments bound memory. lingua caches decompressed n-gram models in
// package level maps that are never evicted, so every language and every n-gram
// order it is asked for stays on the heap for the lifetime of the process.
// Loading is lazy, so narrowing the language set keeps the excluded models from
// being decompressed at all, and lowAccuracy restricts lingua to trigram models
// instead of the unigram through fivegram set.
// Restricted detection requires a confidence of at least 0.8; unrestricted
// detection requires 0.5. Both require enough text and a margin of at least 0.15.
func NewLanguageDetectorFor(codes []string, lowAccuracy bool) LanguageDetector {
	langs := ResolveLanguages(codes)
	if len(langs) < 2 {
		// lingua's builder panics with fewer than two languages, and detection
		// between fewer than two is meaningless in any case.
		return NewNullLanguageDetector()
	}
	b := lingua.NewLanguageDetectorBuilder().FromLanguages(langs...)
	if lowAccuracy {
		b = b.WithLowAccuracyMode()
	}
	d := &langDetector{detector: b.Build(), minConfidence: defaultLanguageMinConfidence}
	if len(codes) > 0 {
		d.minConfidence = restrictedLanguageMinConfidence
	}
	return d
}

func NewNullLanguageDetector() LanguageDetector {
	return &nullLangDetector{}
}

func (d *langDetector) DetectLanguage(s string) string {
	return d.DetectLanguageWithEvidence(s).Language
}

func (d *langDetector) DetectLanguageWithEvidence(s string) languages.Detection {
	result := languages.Detection{Language: UnknownLanguage}
	cjkLetters := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			result.Letters++
			if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
				cjkLetters++
			}
		}
	}
	if result.Letters < minimumLanguageLetters && cjkLetters < minimumCJKLetters {
		result.Reason = languages.ReasonInsufficient
		return result
	}
	// Lingua sorts scores in descending order, so the first result is the best match.
	values := d.detector.ComputeLanguageConfidenceValues(s)
	if len(values) < 2 {
		result.Reason = languages.ReasonNoScores
		return result
	}
	result.Candidate = languageCode(values[0].Language())
	result.Confidence = values[0].Value()
	result.RunnerUp = languageCode(values[1].Language())
	result.RunnerUpConfidence = values[1].Value()
	result.Margin = result.Confidence - result.RunnerUpConfidence
	switch {
	case result.Confidence < d.minConfidence:
		result.Reason = languages.ReasonConfidence
	case result.Margin < languageMinMargin:
		result.Reason = languages.ReasonAmbiguous
	default:
		result.Language = result.Candidate
		result.Reason = languages.ReasonAccepted
	}
	return result
}

func (d *nullLangDetector) DetectLanguage(s string) string {
	return UnknownLanguage
}

func (d *nullLangDetector) DetectLanguageWithEvidence(string) languages.Detection {
	return languages.Detection{Language: UnknownLanguage, Reason: languages.ReasonDisabled}
}
