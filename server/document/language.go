package document

import (
	"strings"

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

const UnknownLanguage = "unknown"

var Languages = []lingua.Language{
	lingua.Arabic,    // ar
	lingua.Bokmal,    // nb - Norewgian Bokmal, gets rewritten to "no" in Hister
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
	// supported by bleve but not by lingua: gl, in
}

// languageCode returns the ISO 639-1 code Hister uses for a lingua language.
func languageCode(l lingua.Language) string {
	code := strings.ToLower(l.IsoCode639_1().String())
	if code == "nb" {
		// use generic "no" code for Norwegian
		return "no"
	}
	return code
}

// SupportedLanguages maps every code Hister accepts in indexer.languages to its
// lingua counterpart. It is derived from Languages so the two cannot drift.
var SupportedLanguages = func() map[string]lingua.Language {
	m := make(map[string]lingua.Language, len(Languages))
	for _, l := range Languages {
		m[languageCode(l)] = l
	}
	return m
}()

// UnsupportedLanguages returns the codes that ResolveLanguages would drop, so
// callers can reject a misspelled configuration instead of silently indexing
// the affected documents into the default index.
func UnsupportedLanguages(codes []string) []string {
	var unsupported []string
	for _, code := range codes {
		if _, ok := SupportedLanguages[strings.ToLower(strings.TrimSpace(code))]; !ok {
			unsupported = append(unsupported, code)
		}
	}
	return unsupported
}

// ResolveLanguages maps ISO 639-1 codes onto lingua languages, preserving the
// order of Languages and dropping codes that are not supported. An empty list
// resolves to every supported language.
func ResolveLanguages(codes []string) []lingua.Language {
	if len(codes) == 0 {
		return Languages
	}
	wanted := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		wanted[strings.ToLower(strings.TrimSpace(c))] = struct{}{}
	}
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

type nullLangDetector struct{}

type langDetector struct {
	detector lingua.LanguageDetector
}

func NewLanguageDetector() LanguageDetector {
	return NewLanguageDetectorFor(nil, false)
}

// NewLanguageDetectorFor builds a detector limited to the given ISO 639-1
// codes, or to every supported language when codes is empty.
//
// Both arguments bound memory. lingua caches decompressed n-gram models in
// package level maps that are never evicted, so every language and every n-gram
// order it is asked for stays on the heap for the lifetime of the process.
// Loading is lazy, so narrowing the language set keeps the excluded models from
// being decompressed at all, and lowAccuracy restricts lingua to trigram models
// instead of the unigram through fivegram set.
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
	return &langDetector{detector: b.Build()}
}

func NewNullLanguageDetector() LanguageDetector {
	return &nullLangDetector{}
}

func (d *langDetector) DetectLanguage(s string) string {
	if language, exists := d.detector.DetectLanguageOf(s); exists {
		return languageCode(language)
	}
	return UnknownLanguage
}

func (d *nullLangDetector) DetectLanguage(s string) string {
	return UnknownLanguage
}
