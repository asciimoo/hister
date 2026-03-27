package readability

import (
	"bytes"
	"context"
	"net/url"

	goreadability "codeberg.org/readeck/go-readability/v2"

	"github.com/asciimoo/hister/server/extractor"
)

// Extractor uses the go-readability library to extract article content from HTML.
type Extractor struct{}

func (e *Extractor) Name() string {
	return "Readability"
}

func (e *Extractor) Initialize(_ map[string]any) error {
	return nil
}

func (e *Extractor) Match(_, _ string) bool {
	return true
}

func (e *Extractor) Extract(_ context.Context, input *extractor.Input) (*extractor.Result, error) {
	r := bytes.NewReader([]byte(input.HTML))

	u, err := url.Parse(input.URL)
	if err != nil {
		return nil, err
	}
	a, err := goreadability.FromReader(r, u)
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(nil)
	if err := a.RenderText(buf); err != nil {
		return nil, err
	}
	return &extractor.Result{
		Title:      a.Title(),
		Text:       buf.String(),
		FaviconURL: a.Favicon(),
	}, nil
}
