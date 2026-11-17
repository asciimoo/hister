package indexer

import (
	"context"

	"github.com/asciimoo/nogon/files/extract"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/extractor"
)

var ErrExtractedContentTooLarge = extract.ErrExtractedContentTooLarge

// AddPDF extracts and indexes a PDF with the configured parser.
func (i *Indexer) AddPDF(d *document.Document, content []byte, options ...AddOption) error {
	return i.AddPDFContext(context.Background(), d, content, options...)
}

// AddPDFContext extracts and indexes a PDF with caller cancellation.
func (i *Indexer) AddPDFContext(ctx context.Context, d *document.Document, content []byte, options ...AddOption) error {
	if err := extractor.ExtractFile(ctx, i.fileExtractor, "document.pdf", d, content); err != nil {
		return err
	}
	return i.AddContext(ctx, d, options...)
}

func (i *Indexer) AddDocx(d *document.Document, content []byte) error {
	if err := extractor.ExtractFile(context.Background(), i.fileExtractor, "document.docx", d, content); err != nil {
		return err
	}
	return i.Add(d)
}

// AddMarkdown renders Markdown to HTML and text, then indexes the document.
func (i *Indexer) AddMarkdown(d *document.Document, content []byte) error {
	if err := extractor.ExtractFile(context.Background(), i.fileExtractor, "document.md", d, content); err != nil {
		return err
	}
	return i.Add(d)
}

// AddOrg renders Org to HTML and text, then indexes the document.
func (i *Indexer) AddOrg(d *document.Document, content []byte) error {
	if err := extractor.ExtractFile(context.Background(), i.fileExtractor, "document.org", d, content); err != nil {
		return err
	}
	return i.Add(d)
}
