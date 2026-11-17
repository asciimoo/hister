// Package sdk defines the stable contracts used to implement Nogon extractors.
package sdk

import (
	"context"
	"errors"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/document"
)

// Document is the document passed through the extractor chain.
type Document = document.Document

// Config contains the enabled state and options for one extractor.
type Config = config.Extractor

// Capabilities declares the phases in which an extractor participates.
type Capabilities struct {
	Enrich  bool `json:"enrich"`
	Extract bool `json:"extract"`
	Preview bool `json:"preview"`
}

// Decision describes whether an extractor succeeded, requested a fallback, or
// aborted its phase.
type Decision int

const (
	ExtractorInvalid Decision = iota
	ExtractorSuccess
	ExtractorFallback
	ExtractorAbort
)

var errExtractorAbortedWithoutError = errors.New("extractor aborted without an error")

// ExtractResult is the outcome of extraction or enrichment. Its fields are
// private so callers must use a valid constructor.
type ExtractResult struct {
	decision Decision
	err      error
}

// PreviewResponse contains rendered preview content and its optional template.
type PreviewResponse struct {
	Content  string
	Template string
}

// PreviewResult is the outcome of preview rendering. Its fields are private so
// a successful result always contains an explicit response and an aborted
// result always contains an error.
type PreviewResult struct {
	decision Decision
	response PreviewResponse
	err      error
}

// Extracted reports successful extraction or enrichment.
func Extracted() ExtractResult {
	return ExtractResult{decision: ExtractorSuccess}
}

// ExtractFallback requests the next matching content extractor.
func ExtractFallback(err error) ExtractResult {
	return ExtractResult{decision: ExtractorFallback, err: err}
}

// AbortExtraction stops extraction with a fatal error.
func AbortExtraction(err error) ExtractResult {
	if err == nil {
		err = errExtractorAbortedWithoutError
	}
	return ExtractResult{decision: ExtractorAbort, err: err}
}

// Decision returns the extraction decision.
func (r ExtractResult) Decision() Decision {
	return r.decision
}

// Err returns the optional diagnostic or fatal error.
func (r ExtractResult) Err() error {
	return r.err
}

// Unpack returns the validated decision and its optional diagnostic error.
func (r ExtractResult) Unpack() (Decision, error) {
	return r.decision, r.err
}

// Previewed reports successful preview rendering.
func Previewed(response PreviewResponse) PreviewResult {
	return PreviewResult{decision: ExtractorSuccess, response: response}
}

// PreviewFallback requests the next matching preview extractor.
func PreviewFallback(err error) PreviewResult {
	return PreviewResult{decision: ExtractorFallback, err: err}
}

// AbortPreview stops preview rendering with a fatal error.
func AbortPreview(err error) PreviewResult {
	if err == nil {
		err = errExtractorAbortedWithoutError
	}
	return PreviewResult{decision: ExtractorAbort, err: err}
}

// Decision returns the preview decision.
func (r PreviewResult) Decision() Decision {
	return r.decision
}

// Response returns the preview response.
func (r PreviewResult) Response() PreviewResponse {
	return r.response
}

// Err returns the optional diagnostic or fatal error.
func (r PreviewResult) Err() error {
	return r.err
}

// Unpack returns the validated response, decision, and optional diagnostic
// error.
func (r PreviewResult) Unpack() (PreviewResponse, Decision, error) {
	return r.response, r.decision, r.err
}

// Extractor defines a component in the extraction and preview chains.
type Extractor interface {
	Name() string
	Description() string
	Capabilities() Capabilities
	Match(*Document) bool
	Extract(*Document) ExtractResult
	Preview(*Document) PreviewResult
	GetConfig() *Config
	SetConfig(*Config) error
}

// ContextExtractor supports cancellation during extraction.
type ContextExtractor interface {
	ExtractContext(context.Context, *Document) ExtractResult
}

// ContextPreviewer supports cancellation during preview rendering.
type ContextPreviewer interface {
	PreviewContext(context.Context, *Document) PreviewResult
}
