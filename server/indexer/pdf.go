// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/server/document"

	"github.com/asciimoo/pdf"
)

type pdfFileType struct{}

func (pdfFileType) Match(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".pdf")
}

func (pdfFileType) Prepare(d *document.Document, pdfData []byte) error {
	text, err := extractPDFText(pdfData)
	if err != nil {
		return fmt.Errorf("pdf text extraction: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("pdf contains no extractable text")
	}
	d.Text = text
	d.AddMetadata("type", "pdf")
	return nil
}

// AddPDF extracts plain text from pdfData, stores it in d.Text, then indexes
// the document via AddContext. d.URL and d.Type must already be set by the caller.
// d.Title is set to the last path segment of the URL if it is not already set.
func (i *Indexer) AddPDF(d *document.Document, pdfData []byte, options ...AddOption) error {
	if err := (pdfFileType{}).Prepare(d, pdfData); err != nil {
		return err
	}
	return i.AddContext(context.Background(), d, options...)
}

// extractPDFText reads all pages of a PDF from pdfData and returns the
// concatenated plain text. It recovers from panics in the underlying PDF
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

	plainText, err := pr.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("read pdf text: %w", err)
	}

	var b strings.Builder
	if _, err := io.Copy(&b, plainText); err != nil {
		return "", fmt.Errorf("read pdf text stream: %w", err)
	}
	return b.String(), nil
}
