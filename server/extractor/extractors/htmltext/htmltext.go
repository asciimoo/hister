package htmltext

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"

	"golang.org/x/net/html"

	"github.com/asciimoo/hister/server/extractor"
	"github.com/asciimoo/hister/server/indexer/types"
)

// Extractor is a fallback that parses raw HTML tokens to extract title and body text.
type Extractor struct{}

func (e *Extractor) Name() string {
	return "Default"
}

func (e *Extractor) Initialize(_ map[string]any) error {
	return nil
}

func (e *Extractor) Match(_, _ string) bool {
	return true
}

func (e *Extractor) Extract(_ context.Context, doc *types.Document) (*extractor.Result, error) {
	result := &extractor.Result{}
	r := bytes.NewReader([]byte(doc.HTML))
	tokenizer := html.NewTokenizer(r)
	inBody := false
	skip := false
	var text strings.Builder
	var currentTag string
out:
	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			err := tokenizer.Err()
			if errors.Is(err, io.EOF) {
				break out
			}
			return nil, errors.New("failed to parse html: " + err.Error())
		case html.SelfClosingTagToken, html.StartTagToken:
			tn, _ := tokenizer.TagName()
			currentTag = string(tn)
			switch currentTag {
			case "body":
				inBody = true
			case "script", "style", "noscript":
				skip = true
			}
		case html.TextToken:
			if currentTag == "title" {
				result.Title += strings.TrimSpace(string(tokenizer.Text()))
			}
			if inBody && !skip {
				text.Write(tokenizer.Text())
			}
		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
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
