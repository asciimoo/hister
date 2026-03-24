package htmltext

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"

	"golang.org/x/net/html"

	"github.com/asciimoo/hister/server/extractor"
)

// Extractor is a fallback that parses raw HTML tokens to extract title and body text.
type Extractor struct{}

func (e *Extractor) Name() string {
	return "Default"
}

func (e *Extractor) Match(_, _ string) bool {
	return true
}

func (e *Extractor) Extract(_ context.Context, input *extractor.Input) (*extractor.Result, error) {
	result := &extractor.Result{}
	r := bytes.NewReader([]byte(input.HTML))
	doc := html.NewTokenizer(r)
	inBody := false
	skip := false
	var text strings.Builder
	var currentTag string
out:
	for {
		tt := doc.Next()
		switch tt {
		case html.ErrorToken:
			err := doc.Err()
			if errors.Is(err, io.EOF) {
				break out
			}
			return nil, errors.New("failed to parse html: " + err.Error())
		case html.SelfClosingTagToken, html.StartTagToken:
			tn, _ := doc.TagName()
			currentTag = string(tn)
			switch currentTag {
			case "body":
				inBody = true
			case "script", "style", "noscript":
				skip = true
			}
		case html.TextToken:
			if currentTag == "title" {
				result.Title += strings.TrimSpace(string(doc.Text()))
			}
			if inBody && !skip {
				text.Write(doc.Text())
			}
		case html.EndTagToken:
			tn, _ := doc.TagName()
			switch string(tn) {
			case "body":
				inBody = false
			case "script", "style", "noscript":
				skip = false
			}
		}
	}
	result.Text = strings.TrimSpace(text.String())
	if result.Text == "" && result.Title == "" {
		return nil, errors.New("no content found")
	}
	return result, nil
}

func (e *Extractor) Rebuild(_ string) (*extractor.Result, error) {
	return nil, extractor.ErrRebuildNotSupported
}
