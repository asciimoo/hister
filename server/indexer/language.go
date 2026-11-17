package indexer

import (
	"fmt"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/languages"

	_ "github.com/blevesearch/bleve/v2/analysis/lang/ar"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/cjk"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ckb"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/da"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/de"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/en"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/es"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/fa"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/fi"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/fr"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/hi"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/hr"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/hu"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/it"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/nl"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/no"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/pl"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/pt"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ro"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ru"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/sv"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/tr"
)

func normalizeLanguageOverride(language string) (string, error) {
	resolved, err := languages.Resolve(language)
	if err != nil {
		return "", fmt.Errorf("%w %q", ErrInvalidLanguage, language)
	}
	return resolved.Tag, nil
}

func normalizeDocumentLanguageOverride(d *document.Document) error {
	if d.LanguageOverride == "" {
		return nil
	}
	language, err := normalizeLanguageOverride(d.LanguageOverride)
	if err != nil {
		return err
	}
	d.LanguageOverride = language
	d.Language = language
	return nil
}

func analyzerForLanguage(language string) string {
	resolved, err := languages.Resolve(language)
	if err != nil {
		return languages.DefaultAnalyzer
	}
	return resolved.Analyzer
}
