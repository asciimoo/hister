package indexer

import (
	"context"
	"errors"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/server/extractor"
	"github.com/asciimoo/hister/server/extractor/extractors/htmltext"
	"github.com/asciimoo/hister/server/extractor/extractors/readability"
)

var syncExtractors []extractor.Extractor = []extractor.Extractor{
	&readability.Extractor{},
	&htmltext.Extractor{},
}

var ErrNoExtractor = errors.New("no extractor found")

func Extract(d *Document) error {
	input := &extractor.Input{
		URL:    d.URL,
		Domain: d.Domain,
		HTML:   d.HTML,
	}
	for _, e := range syncExtractors {
		if e.Match(d.URL, d.Domain) {
			result, err := e.Extract(context.Background(), input)
			if err != nil {
				log.Warn().Err(err).Str("URL", d.URL).Str("Extractor", e.Name()).Msg("Failed to extract content")
				continue
			}
			d.Title = result.Title
			d.Text = result.Text
			d.faviconURL = result.FaviconURL
			if result.Properties != nil {
				d.Properties = result.Properties
			}
			return nil
		}
	}
	return ErrNoExtractor
}
