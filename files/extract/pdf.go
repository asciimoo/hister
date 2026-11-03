// SPDX-License-Identifier: AGPL-3.0-or-later

package extract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/config"

	"github.com/asciimoo/pdf"
)

type pdfFileType struct {
	parser config.PDFParser
}

func (pdfFileType) Match(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".pdf")
}

func (p pdfFileType) Prepare(ctx context.Context, d *Result, pdfData []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var text string
	var err error
	if p.parser.Command == "" {
		text, err = extractPDFText(pdfData)
	} else {
		text, err = extractExternalPDFText(ctx, pdfData, p.parser)
	}
	if err != nil {
		return fmt.Errorf("pdf text extraction: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("pdf contains no extractable text")
	}
	d.Text = text
	d.Format = PDF
	return nil
}

// Render formats the stored text, including page boundaries when the parser
// supplied form feeds. It never opens the original PDF or trusts stored HTML.
func (pdfFileType) Render(ctx context.Context, _ string, content Result) (RenderResult, error) {
	text := strings.ReplaceAll(content.Text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	numbered := strings.Contains(text, "\f")
	// PDF text tools commonly terminate the final page with a form feed.
	text = strings.TrimSuffix(strings.TrimRight(text, " \t\n"), "\f")
	var b strings.Builder
	b.WriteString(`<div class="pdf-preview" role="region" aria-label="PDF preview">`)
	page := 0
	for pageText := range strings.SplitSeq(text, "\f") {
		if err := ctx.Err(); err != nil {
			return RenderResult{}, err
		}
		page++
		b.WriteString(`<section class="pdf-page">`)
		if numbered {
			fmt.Fprintf(&b, `<h2 class="pdf-page-number">Page %d</h2>`, page)
		}
		inParagraph := false
		for line := range strings.SplitSeq(pageText, "\n") {
			if strings.TrimSpace(line) == "" {
				if inParagraph {
					b.WriteString("</p>")
					inParagraph = false
				}
				continue
			}
			if inParagraph {
				b.WriteString("<br>")
			} else {
				b.WriteString("<p>")
				inParagraph = true
			}
			b.WriteString(html.EscapeString(line))
		}
		if inParagraph {
			b.WriteString("</p>")
		}
		if strings.TrimSpace(pageText) == "" {
			b.WriteString("<p>No extractable text on this page.</p>")
		}
		b.WriteString("</section>")
		if b.Len() > MaxContentSize {
			return RenderResult{}, ErrExtractedContentTooLarge
		}
	}
	b.WriteString("</div>")
	return RenderResult{Template: "pdf", Content: b.String()}, nil
}

const maxExternalPDFTextSize = MaxContentSize

var errPDFOutputTooLarge = errors.New("external PDF parser output exceeds 32 MiB")

type pdfTextOutput struct {
	buffer   bytes.Buffer
	cancel   context.CancelFunc
	exceeded bool
}

func (b *pdfTextOutput) Write(p []byte) (int, error) {
	if len(p) > maxExternalPDFTextSize-b.buffer.Len() {
		b.exceeded = true
		b.cancel()
		return 0, errPDFOutputTooLarge
	}
	return b.buffer.Write(p)
}

func extractExternalPDFText(ctx context.Context, pdfData []byte, parser config.PDFParser) (string, error) {
	if parser.Timeout == 0 {
		parser.Timeout = 30
	}
	if err := parser.Validate(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(parser.Timeout)*time.Second)
	defer cancel()
	output := &pdfTextOutput{cancel: cancel}
	// The executable and arguments come only from local configuration. PDF data
	// goes through stdin and is never interpreted as arguments or shell code.
	cmd := exec.CommandContext(ctx, parser.Command, parser.Args...)
	cmd.Stdin = bytes.NewReader(pdfData)
	cmd.Stdout = output
	// Stderr may contain document text; leave it connected to the null device.
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if output.exceeded {
		return "", errPDFOutputTooLarge
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("external PDF parser: %w", ctx.Err())
	}
	if err != nil {
		return "", fmt.Errorf("external PDF parser: %w", err)
	}
	if !utf8.Valid(output.buffer.Bytes()) {
		return "", errors.New("external PDF parser output is not valid UTF-8")
	}
	return output.buffer.String(), nil
}

// extractPDFText reads all pages of a PDF from pdfData and returns the
// plain text with a form feed after each page. It recovers from panics in the underlying PDF
// library and converts them to errors.
func extractPDFText(pdfData []byte) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Debug().Msgf("pdf parser panic: %v\n%s", r, debug.Stack())
			err = fmt.Errorf("pdf parser panic: %v", r)
		}
	}()

	r := bytes.NewReader(pdfData)
	pr, err := pdf.NewReader(r, int64(len(pdfData)))
	if err != nil {
		return "", fmt.Errorf("open pdf: %w", err)
	}

	var b strings.Builder
	for page := 1; page <= pr.NumPage(); page++ {
		text, err := pr.Page(page).GetPlainText(nil)
		if err != nil {
			return "", fmt.Errorf("read pdf page %d: %w", page, err)
		}
		if len(text)+1 > MaxContentSize-b.Len() {
			return "", ErrExtractedContentTooLarge
		}
		b.WriteString(text)
		b.WriteByte('\f')
	}
	return b.String(), nil
}
