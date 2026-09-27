// Package extractor provides HTML content extraction for documents.
package extractor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"net/url"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/html"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/extractor/sdk"
	"github.com/asciimoo/hister/server/sanitizer"
)

// Extractor is retained as an alias for the stable SDK contract.
type Extractor = sdk.Extractor

// ContextExtractor is retained as an alias for the stable SDK contract.
type ContextExtractor = sdk.ContextExtractor

// ContextPreviewer is retained as an alias for the stable SDK contract.
type ContextPreviewer = sdk.ContextPreviewer

func extractWithContext(ctx context.Context, e Extractor, d *document.Document) sdk.ExtractResult {
	if contextual, ok := e.(ContextExtractor); ok {
		return contextual.ExtractContext(ctx, d)
	}
	return e.Extract(d)
}

func previewWithContext(ctx context.Context, e Extractor, d *document.Document) sdk.PreviewResult {
	if contextual, ok := e.(ContextPreviewer); ok {
		return contextual.PreviewContext(ctx, d)
	}
	return e.Preview(d)
}

// ErrNoExtractor is returned when no extractor can handle the document.
var ErrNoExtractor = errors.New("no extractor found")

// ErrExtractorAbort is returned when an extractor aborts its current phase.
var ErrExtractorAbort = errors.New("extractor aborted")

// ErrInvalidExtractorResult is returned when an extractor returns the zero
// value or another result that was not created by a result constructor.
var ErrInvalidExtractorResult = errors.New("invalid extractor result")

// ExtractorInfo holds a summary of an extractor's identity and current state.
type ExtractorInfo struct {
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	Enabled      bool             `json:"enabled"`
	Capabilities sdk.Capabilities `json:"capabilities"`
	Options      map[string]any   `json:"options,omitempty"`
}

// ListMatching returns an ExtractorInfo entry for every enabled extractor that
// matches d, in chain order. Options is omitted so the result is safe to send
// to clients.
func ListMatching(d *document.Document) []ExtractorInfo {
	return defaultRegistry.ListMatching(d)
}

// ListMatching returns matching enabled extractors in registry order.
func (r *Registry) ListMatching(d *document.Document) []ExtractorInfo {
	infos := make([]ExtractorInfo, 0)
	for _, e := range r.Extractors() {
		if !e.GetConfig().Enable {
			continue
		}
		if e.Match(d) {
			infos = append(infos, ExtractorInfo{
				Name:         e.Name(),
				Description:  e.Description(),
				Enabled:      true,
				Capabilities: e.Capabilities(),
			})
		}
	}
	return infos
}

// ListMatchingPreview returns every enabled preview extractor matching d in
// chain order. Metadata-only enrichers and content-only extractors are omitted.
func ListMatchingPreview(d *document.Document) []ExtractorInfo {
	return defaultRegistry.ListMatchingPreview(d)
}

// ListMatchingPreview returns matching enabled preview extractors in registry
// order.
func (r *Registry) ListMatchingPreview(d *document.Document) []ExtractorInfo {
	infos := make([]ExtractorInfo, 0)
	for _, e := range r.Extractors() {
		if !e.GetConfig().Enable || !e.Capabilities().Preview {
			continue
		}
		if e.Match(d) {
			infos = append(infos, ExtractorInfo{
				Name:         e.Name(),
				Description:  e.Description(),
				Enabled:      true,
				Capabilities: e.Capabilities(),
			})
		}
	}
	return infos
}

// ListEnabled returns an ExtractorInfo entry for every enabled extractor in
// chain order. Options is omitted so the result is safe to send to clients.
func ListEnabled() []ExtractorInfo {
	return defaultRegistry.ListEnabled()
}

// ListEnabled returns enabled extractors in registry order.
func (r *Registry) ListEnabled() []ExtractorInfo {
	extractors := r.Extractors()
	infos := make([]ExtractorInfo, 0, len(extractors))
	for _, e := range extractors {
		if !e.GetConfig().Enable {
			continue
		}
		infos = append(infos, ExtractorInfo{
			Name:         e.Name(),
			Description:  e.Description(),
			Enabled:      true,
			Capabilities: e.Capabilities(),
		})
	}
	return infos
}

// List returns an ExtractorInfo entry for every registered extractor in chain
// order. Options is always populated; callers that should not expose
// configuration must clear or omit it before sending to clients.
func List() []ExtractorInfo {
	return defaultRegistry.List()
}

// List returns every extractor in registry order, including configuration.
func (r *Registry) List() []ExtractorInfo {
	extractors := r.Extractors()
	infos := make([]ExtractorInfo, 0, len(extractors))
	for _, e := range extractors {
		cfg := e.GetConfig()
		infos = append(infos, ExtractorInfo{
			Name:         e.Name(),
			Description:  e.Description(),
			Enabled:      cfg.Enable,
			Capabilities: e.Capabilities(),
			Options:      cfg.Options,
		})
	}
	return infos
}

// Init applies user-supplied extractor configurations on top of each
// extractor's defaults. It must be called before Extract or Preview.
// cfgs is keyed by lowercased extractor name (as Viper lowercases YAML keys).
func Init(cfgs map[string]*config.Extractor) error {
	return defaultRegistry.Init(cfgs)
}

// Extract first runs every matching enricher, then tries matching content
// extractors in registration order and returns the first successful result.
// Returns ErrNoExtractor if no content extractor succeeds.
func Extract(d *document.Document) error {
	return defaultRegistry.Extract(d)
}

// Extract runs this registry's extraction chain.
func (r *Registry) Extract(d *document.Document) error {
	return r.ExtractContext(context.Background(), d)
}

// ExtractContext runs the extraction chain with caller cancellation.
func ExtractContext(ctx context.Context, d *document.Document) error {
	return defaultRegistry.ExtractContext(ctx, d)
}

// ExtractContext runs this registry's extraction chain with caller
// cancellation.
func (r *Registry) ExtractContext(ctx context.Context, d *document.Document) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	extractors := r.Extractors()
	for _, e := range extractors {
		if !e.GetConfig().Enable || !e.Capabilities().Enrich {
			continue
		}
		if e.Match(d) {
			result := extractWithContext(ctx, e, d)
			if err := ctx.Err(); err != nil {
				return err
			}
			log.Debug().Str("URL", d.URL).Str("Extractor", e.Name()).Msg("Enriching document")
			switch result.Decision() {
			case sdk.ExtractorSuccess:
			case sdk.ExtractorFallback:
				if result.Err() != nil {
					log.Warn().Err(result.Err()).Str("URL", d.URL).Str("Extractor", e.Name()).Msg("Failed to enrich document")
				}
			case sdk.ExtractorAbort:
				return fmt.Errorf("extractor %s: %w: %w", e.Name(), ErrExtractorAbort, result.Err())
			default:
				return fmt.Errorf("extractor %s: %w", e.Name(), ErrInvalidExtractorResult)
			}
		}
	}
	for _, e := range extractors {
		if !e.GetConfig().Enable || !e.Capabilities().Extract {
			continue
		}
		if e.Match(d) {
			result := extractWithContext(ctx, e, d)
			if err := ctx.Err(); err != nil {
				return err
			}
			log.Debug().Str("URL", d.URL).Str("Extractor", e.Name()).Msg("Extracting data")
			switch result.Decision() {
			case sdk.ExtractorSuccess:
				return nil
			case sdk.ExtractorAbort:
				return fmt.Errorf("extractor %s: %w: %w", e.Name(), ErrExtractorAbort, result.Err())
			case sdk.ExtractorFallback:
				if result.Err() != nil {
					log.Warn().Err(result.Err()).Str("URL", d.URL).Str("Extractor", e.Name()).Msg("Failed to extract content")
				}
			default:
				return fmt.Errorf("extractor %s: %w", e.Name(), ErrInvalidExtractorResult)
			}
		}
	}
	return ErrNoExtractor
}

// Preview returns a rendered preview of the document. When name is empty, the
// first successful matching preview extractor is used. A name selects the
// starting extractor, with later matching preview extractors acting as
// fallbacks. Explicit selection still requires the extractor to be enabled,
// preview capable, and matched to the document.
func Preview(d *document.Document, name string) (sdk.PreviewResponse, error) {
	return defaultRegistry.Preview(d, name)
}

// Preview runs this registry's preview chain.
func (r *Registry) Preview(d *document.Document, name string) (sdk.PreviewResponse, error) {
	return r.PreviewContext(context.Background(), d, name)
}

// PreviewContext renders a preview with caller cancellation.
func PreviewContext(ctx context.Context, d *document.Document, name string) (sdk.PreviewResponse, error) {
	return defaultRegistry.PreviewContext(ctx, d, name)
}

// PreviewContext runs this registry's preview chain with caller cancellation.
func (r *Registry) PreviewContext(ctx context.Context, d *document.Document, name string) (sdk.PreviewResponse, error) {
	if err := ctx.Err(); err != nil {
		return sdk.PreviewResponse{}, err
	}
	extractors := r.Extractors()
	start := 0
	if name != "" {
		lower := strings.ToLower(name)
		found := false
		for i, e := range extractors {
			if strings.ToLower(e.Name()) == lower {
				found = true
				if !e.GetConfig().Enable {
					return sdk.PreviewResponse{}, fmt.Errorf("%w: %s is disabled", ErrNoExtractor, name)
				}
				if !e.Capabilities().Preview {
					return sdk.PreviewResponse{}, fmt.Errorf("%w: %s does not provide previews", ErrNoExtractor, name)
				}
				if !e.Match(d) {
					return sdk.PreviewResponse{}, fmt.Errorf("%w: %s does not match document", ErrNoExtractor, name)
				}
				start = i
				break
			}
		}
		if !found {
			return sdk.PreviewResponse{}, fmt.Errorf("%w: %s", ErrNoExtractor, name)
		}
	}
	for _, e := range extractors[start:] {
		if !e.GetConfig().Enable || !e.Capabilities().Preview {
			continue
		}
		if e.Match(d) {
			log.Debug().Str("URL", d.URL).Str("Extractor", e.Name()).Msg("Creating preview")
			result := previewWithContext(ctx, e, d)
			if err := ctx.Err(); err != nil {
				return sdk.PreviewResponse{}, err
			}
			switch result.Decision() {
			case sdk.ExtractorSuccess:
				return result.Response(), nil
			case sdk.ExtractorAbort:
				return sdk.PreviewResponse{}, fmt.Errorf("extractor %s: %w: %w", e.Name(), ErrExtractorAbort, result.Err())
			case sdk.ExtractorFallback:
				if result.Err() != nil {
					log.Warn().Err(result.Err()).Str("URL", d.URL).Str("Extractor", e.Name()).Msg("Failed to preview content")
				}
			default:
				return sdk.PreviewResponse{}, fmt.Errorf("extractor %s: %w", e.Name(), ErrInvalidExtractorResult)
			}
		}
	}
	return sdk.PreviewResponse{}, ErrNoExtractor
}

type basicExtractor struct {
	sdk.ConfigSupport
}

type readabilityExtractor struct {
	sdk.ConfigSupport
}

func (e *basicExtractor) Name() string {
	return "Basic"
}

func (e *basicExtractor) Description() string {
	return "Fallback extractor that strips HTML tags and extracts plain text from any web page."
}

func (e *basicExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

func (e *basicExtractor) Match(_ *sdk.Document) bool {
	return true
}

func (e *basicExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	var extractedTitle strings.Builder
	r := strings.NewReader(d.HTML)
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
			return sdk.AbortExtraction(errors.New("failed to parse html: " + err.Error()))
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
				extractedTitle.WriteString(strings.TrimSpace(string(doc.Text())))
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
	if extractedTitle.String() != "" {
		d.Title = extractedTitle.String()
	}
	if d.Text == "" && d.Title == "" {
		return sdk.ExtractFallback(errors.New("no content found"))
	}
	return sdk.Extracted()
}

func (e *basicExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	return sdk.Previewed(sdk.PreviewResponse{Content: stdhtml.EscapeString(d.Text)})
}

func (e *readabilityExtractor) Name() string {
	return "Readability"
}

func (e *readabilityExtractor) Description() string {
	return "Extracts the main article content from any web page using the go-readability library, filtering out navigation, ads, and other boilerplate."
}

func (e *readabilityExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

func (e *readabilityExtractor) Match(_ *sdk.Document) bool {
	return true
}

func (e *readabilityExtractor) Extract(d *sdk.Document) sdk.ExtractResult {
	r := strings.NewReader(d.HTML)

	u, err := url.Parse(d.URL)
	if err != nil {
		return sdk.AbortExtraction(err)
	}
	a, err := readability.FromReader(r, u)
	if err != nil {
		return sdk.ExtractFallback(err)
	}
	buf := bytes.NewBuffer(nil)
	if err := a.RenderText(buf); err != nil {
		return sdk.ExtractFallback(err)
	}
	d.Text = buf.String()
	if t := a.Title(); t != "" {
		d.Title = t
	}
	d.SetFaviconURL(a.Favicon())
	writeReadabilityMeta(d, a)
	return sdk.Extracted()
}

// writeReadabilityMeta copies the rich fields readability already parsed
// (internally from JSON-LD, OpenGraph, and meta tags) onto d.Metadata so
// downstream consumers have byline/date/description without re-parsing.
// The JSON-LD extractor only writes type and headline, so these keys do
// not collide.
func writeReadabilityMeta(d *sdk.Document, a readability.Article) {
	if d.Metadata == nil {
		d.Metadata = make(map[string]any)
	}
	set := func(k, v string) {
		if v != "" {
			d.Metadata[k] = v
		}
	}
	set("author", a.Byline())
	set("description", a.Excerpt())
	set("site_name", a.SiteName())
	set("image", a.ImageURL())
	set("language", a.Language())
	if t, err := a.PublishedTime(); err == nil && !t.IsZero() {
		d.Metadata["published"] = t.Format(time.RFC3339)
	}
	if t, err := a.ModifiedTime(); err == nil && !t.IsZero() {
		d.Metadata["modified"] = t.Format(time.RFC3339)
	}
}

func (e *readabilityExtractor) Preview(d *sdk.Document) sdk.PreviewResult {
	r := strings.NewReader(d.HTML)
	u, err := url.Parse(d.URL)
	if err != nil {
		return sdk.AbortPreview(err)
	}
	a, err := readability.FromReader(r, u)
	if err != nil {
		return sdk.PreviewFallback(err)
	}
	var htmlContent strings.Builder
	if err := a.RenderHTML(&htmlContent); err != nil {
		return sdk.PreviewFallback(err)
	}
	return sdk.Previewed(sdk.PreviewResponse{Content: sanitizer.SanitizeHTML(htmlContent.String())})
}
