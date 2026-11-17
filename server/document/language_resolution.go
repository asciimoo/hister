package document

import (
	"io"
	"strings"

	"golang.org/x/net/html"

	"github.com/asciimoo/nogon/server/languages"
)

// ResolveLanguage applies a pin or evaluates current content. A page declaration
// may refine a confident base language, but never overrides conflicting or
// insufficient text evidence. Overrides are validated by the indexer.
func (d *Document) ResolveLanguage(ld LanguageDetector) {
	if d.LanguageOverride != "" {
		d.Language = d.LanguageOverride
		return
	}
	if ld == nil {
		ld = NewNullLanguageDetector()
	}
	detect := func(text string) languages.Detection {
		if detector, ok := ld.(EvidenceLanguageDetector); ok {
			return detector.DetectLanguageWithEvidence(text)
		}
		return languages.Detection{Language: ld.DetectLanguage(text)}
	}
	result := detect(d.Text)
	// File titles can be generated from paths, so they are not language evidence.
	if result.Reason == languages.ReasonInsufficient && d.Type == Web && strings.TrimSpace(d.Title) != "" {
		titleResult := detect(d.Title)
		if titleResult.Reason != languages.ReasonInsufficient {
			result = titleResult
		}
	}
	if result.Reason != languages.ReasonDisabled {
		if hint, ok := htmlLanguageHint(d.HTML); ok {
			if detected, err := languages.Resolve(result.Language); err == nil && detected.Base != languages.Unknown && detected.Base == hint.Base {
				result.Language = hint.Tag
			}
		}
	}
	d.Language = result.Language
}

func htmlLanguageHint(content string) (languages.Resolution, bool) {
	// Read only the opening element, with a cap even for malformed HTML.
	const limit = 64 << 10
	tokenizer := html.NewTokenizer(io.LimitReader(strings.NewReader(content), limit))
	tokenizer.SetMaxBuf(limit)
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return languages.Resolution{}, false
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if token.Data != "html" {
				return languages.Resolution{}, false
			}
			for _, name := range []string{"lang", "xml:lang"} {
				for _, attr := range token.Attr {
					if attr.Key == name {
						hint, err := languages.Resolve(attr.Val)
						return hint, err == nil && hint.Base != languages.Unknown
					}
				}
			}
			return languages.Resolution{}, false
		}
	}
}
