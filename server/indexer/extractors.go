package indexer

import (
	"bytes"
	"errors"
	"io"
	"net/url"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/html"
)

type Extractor interface {
	Name() string
	Match(*Document) bool
	Extract(*Document) error
}

var extractors []Extractor = []Extractor{
	&readabilityExtractor{},
	&defaultExtractor{},
}

var ErrNoExtractor = errors.New("no extractor found")

type defaultExtractor struct{}

type readabilityExtractor struct{}

func Extract(d *Document) error {
	for _, e := range extractors {
		if e.Match(d) {
			if err := e.Extract(d); err != nil {
				log.Warn().Err(err).Str("URL", d.URL).Str("Extractor", e.Name()).Msg("Failed to extract content")
			} else {
				return nil
			}
		}
	}
	return ErrNoExtractor
}

func (e *defaultExtractor) Name() string {
	return "Default"
}

func (e *defaultExtractor) Match(_ *Document) bool {
	return true
}

func (e *defaultExtractor) Extract(d *Document) error {
	d.Title = ""
	r := bytes.NewReader([]byte(d.HTML))
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
			return errors.New("failed to parse html: " + err.Error())
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
				d.Title += strings.TrimSpace(string(doc.Text()))
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
	d.Text = strings.TrimSpace(text.String())
	if err := d.extractHTML(); err != nil {
		return err
	}
	return nil
}

func (e *readabilityExtractor) Name() string {
	return "Readability"
}

func (e *readabilityExtractor) Match(_ *Document) bool {
	return true
}

func (e *readabilityExtractor) Extract(d *Document) error {
	r := bytes.NewReader([]byte(d.HTML))

	u, err := url.Parse(d.URL)
	if err != nil {
		return err
	}
	a, err := readability.FromReader(r, u)
	if err != nil {
		return err
	}
	buf := bytes.NewBuffer(nil)
	if err := a.RenderText(buf); err != nil {
		return err
	}
	d.Text = buf.String()
	d.Title = a.Title()
	d.faviconURL = a.Favicon()
	return nil
}
