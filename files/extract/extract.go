// Package extract parses supplied file bytes and renders stored content without
// opening source paths or assigning document identity, ownership, or storage state.
package extract

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/asciimoo/hister/config"
)

const MaxContentSize = 32 << 20

var (
	ErrBinaryFile               = errors.New("binary file")
	ErrExtractedContentTooLarge = errors.New("extracted content exceeds 32 MiB")
	ErrExternalFileReference    = errors.New("file extraction cannot read referenced files")
	ErrNoRenderer               = errors.New("file type has no preview renderer")
)

type Format string

const (
	PlainText Format = "text"
	PDF       Format = "pdf"
	DOCX      Format = "docx"
	Markdown  Format = "markdown"
	Org       Format = "org"
	HTML      Format = "html"
)

// Result contains only extracted content. HTML files retain their source HTML
// for the caller's web extraction chain. Rendered Markdown, Org, and DOCX HTML still
// needs sanitizing before display.
type Result struct {
	Format Format
	Title  string
	Text   string
	HTML   string
}

// CheckSize applies the field limits to extracted or subsequently enriched content.
func (r Result) CheckSize() error {
	if len(r.Text) > MaxContentSize || len(r.HTML) > MaxContentSize || len(r.Title) > MaxContentSize {
		return ErrExtractedContentTooLarge
	}
	return nil
}

type fileTypeHandler interface {
	Match(string) bool
	Prepare(context.Context, *Result, []byte) error
}

// RenderResult contains safe HTML, or serialized data for the named preview template.
type RenderResult struct {
	Content  string
	Template string
}

// Renderer is an optional capability of a file type handler. It renders stored
// content without reading source files. The filename is available for
// extension-specific rendering, such as syntax highlighting.
type Renderer interface {
	Render(context.Context, string, Result) (RenderResult, error)
}

// Extractor owns parser configuration and can be shared across calls.
type Extractor struct {
	handlers []fileTypeHandler
}

func New(pdfParser config.PDFParser) *Extractor {
	pdfParser.Args = slices.Clone(pdfParser.Args)
	return &Extractor{handlers: []fileTypeHandler{
		pdfFileType{parser: pdfParser},
		docxFileType{},
		markdownFileType{},
		orgFileType{},
		htmlFileType{},
		plainTextFileType{},
	}}
}

// Extract uses name only to select a format. It reads exclusively from content,
// except that a configured external PDF command receives those bytes on stdin.
// Errors always return an empty result, never partially extracted content.
func (e *Extractor) Extract(ctx context.Context, name string, content []byte) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	handler := e.forPath(name)
	switch handler.(type) {
	case plainTextFileType, htmlFileType, markdownFileType, orgFileType:
		if len(content) > MaxContentSize {
			return Result{}, ErrExtractedContentTooLarge
		}
	}
	var result Result
	if err := handler.Prepare(ctx, &result, content); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := result.CheckSize(); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (e *Extractor) forPath(name string) fileTypeHandler {
	for _, handler := range e.handlers {
		if handler.Match(name) {
			return handler
		}
	}
	return plainTextFileType{}
}

// Render uses the same handler selection as Extract, but only consumes stored
// content. ErrNoRenderer leaves preview fallback to the caller.
func (e *Extractor) Render(ctx context.Context, name string, content Result) (RenderResult, error) {
	if err := ctx.Err(); err != nil {
		return RenderResult{}, err
	}
	renderer, ok := e.forPath(name).(Renderer)
	if !ok {
		return RenderResult{}, ErrNoRenderer
	}
	rendered, err := renderer.Render(ctx, name, content)
	if err != nil {
		return RenderResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return RenderResult{}, err
	}
	return rendered, nil
}

type plainTextFileType struct{}

func (plainTextFileType) Match(string) bool { return true }

func (plainTextFileType) Prepare(_ context.Context, result *Result, content []byte) error {
	if !utf8.Valid(content) {
		return ErrBinaryFile
	}
	result.Format = PlainText
	result.Text = string(content)
	return nil
}

func (plainTextFileType) Render(_ context.Context, name string, content Result) (RenderResult, error) {
	data, err := json.Marshal(struct {
		Text   string `json:"text"`
		Syntax string `json:"syntax,omitempty"`
	}{Text: content.Text, Syntax: fileSyntax(name)})
	if err != nil {
		return RenderResult{}, err
	}
	return RenderResult{Template: "code", Content: string(data)}, nil
}

// fileSyntax supplies a hint for the browser's code preview. This is separate
// from natural language detection and does not inspect or modify source text.
func fileSyntax(name string) string {
	switch strings.ToLower(filepath.Base(name)) {
	case "dockerfile", "containerfile":
		return "dockerfile"
	case "makefile", "gnumakefile":
		return "makefile"
	}
	return fileSyntaxExtensions[strings.ToLower(filepath.Ext(name))]
}

var fileSyntaxExtensions = map[string]string{
	".c": "c", ".h": "c", ".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp", ".hpp": "cpp",
	".cs": "csharp", ".css": "css", ".scss": "scss", ".go": "go", ".java": "java",
	".js": "javascript", ".mjs": "javascript", ".cjs": "javascript", ".jsx": "jsx",
	".ts": "typescript", ".mts": "typescript", ".cts": "typescript", ".tsx": "tsx",
	".json": "json", ".jsonc": "jsonc", ".yaml": "yaml", ".yml": "yaml", ".toml": "toml",
	".xml": "xml", ".svg": "xml", ".ini": "ini", ".sql": "sql",
	".py": "python", ".pyw": "python", ".rb": "ruby", ".rs": "rust", ".php": "php",
	".sh": "shellscript", ".bash": "shellscript", ".zsh": "shellscript",
	".lua": "lua", ".nix": "nix", ".mk": "makefile", ".dockerfile": "dockerfile",
	".diff": "diff", ".patch": "diff",
}

func hasExtension(path string, exts ...string) bool {
	return slices.Contains(exts, strings.ToLower(filepath.Ext(path)))
}
