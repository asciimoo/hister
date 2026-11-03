// SPDX-License-Identifier: AGPL-3.0-or-later

package extract

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"runtime/debug"
	"strings"

	docx "github.com/mmonterroca/docxgo/v2"
	"github.com/mmonterroca/docxgo/v2/domain"
	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/server/sanitizer"
)

type docxFileType struct{}

const maxDOCXUncompressedSize = 64 << 20

func (docxFileType) Match(path string) bool {
	return hasExtension(path, ".docx")
}

func (docxFileType) Prepare(_ context.Context, d *Result, docxData []byte) error {
	text, title, preview, err := extractDocxContent(docxData)
	if err != nil {
		return fmt.Errorf("docx text extraction: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("docx contains no extractable text")
	}
	d.Text = text
	d.HTML = preview
	if d.Title == "" {
		d.Title = title
	}
	d.Format = DOCX
	return nil
}

// Render uses saved formatting when available and preserves paragraph breaks
// in older text snapshots. Stored HTML can come from imports, so sanitize it.
func (docxFileType) Render(ctx context.Context, _ string, content Result) (RenderResult, error) {
	if err := content.CheckSize(); err != nil {
		return RenderResult{}, err
	}
	markup := sanitizer.SanitizeHTML(content.HTML)
	if markup == "" {
		text := strings.ReplaceAll(content.Text, "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
		var b strings.Builder
		inParagraph := false
		for line := range strings.SplitSeq(text, "\n") {
			if err := ctx.Err(); err != nil {
				return RenderResult{}, err
			}
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
			if b.Len() > MaxContentSize {
				return RenderResult{}, ErrExtractedContentTooLarge
			}
		}
		if inParagraph {
			b.WriteString("</p>")
		}
		markup = b.String()
	}
	markup = `<div class="docx-preview" role="region" aria-label="DOCX preview">` + markup + `</div>`
	if len(markup) > MaxContentSize {
		return RenderResult{}, ErrExtractedContentTooLarge
	}
	return RenderResult{Template: "docx", Content: markup}, nil
}

func extractDocxContent(docxData []byte) (text, title, preview string, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Debug().Msgf("docx parser panic: %v\n%s", r, debug.Stack())
			err = fmt.Errorf("docx parser panic: %v", r)
		}
	}()

	archive, err := zip.NewReader(bytes.NewReader(docxData), int64(len(docxData)))
	if err != nil {
		return "", "", "", fmt.Errorf("open docx archive: %w", err)
	}
	// The parser loads every ZIP entry, including media, into memory. Check the
	// total before handing it the archive; archive/zip also rejects entries that
	// expand beyond their declared size while the parser reads them.
	var uncompressed uint64
	for _, entry := range archive.File {
		if entry.UncompressedSize64 > maxDOCXUncompressedSize-uncompressed {
			return "", "", "", errors.New("docx uncompressed content exceeds 64 MiB")
		}
		uncompressed += entry.UncompressedSize64
	}

	doc, err := docx.OpenDocumentFromBytes(docxData)
	if err != nil {
		return "", "", "", fmt.Errorf("open docx: %w", err)
	}

	if meta := doc.Metadata(); meta != nil {
		title = strings.TrimSpace(meta.Title)
		if len(title) > MaxContentSize {
			return "", "", "", ErrExtractedContentTooLarge
		}
	}

	paragraphs := doc.Paragraphs()
	textParts := make([]string, 0, len(paragraphs))
	textSize := 0
	var markup strings.Builder
	for _, p := range paragraphs {
		paragraphText := strings.TrimSpace(p.Text())
		if paragraphText == "" {
			continue
		}
		if title == "" && isDocxTitleParagraph(p) {
			title = paragraphText
		}
		partSize := len(paragraphText)
		if len(textParts) > 0 {
			partSize += 2 // Paragraph separator.
		}
		if partSize > MaxContentSize-textSize {
			return "", "", "", ErrExtractedContentTooLarge
		}
		textSize += partSize
		textParts = append(textParts, paragraphText)
		if err := renderDocxParagraph(&markup, p); err != nil {
			return "", "", "", err
		}
	}
	if title == "" && len(textParts) > 0 {
		title = textParts[0]
	}
	return strings.Join(textParts, "\n\n"), title, markup.String(), nil
}

func isDocxTitleParagraph(p domain.Paragraph) bool {
	return docxParagraphTag(p) == "h1"
}

func docxParagraphTag(p domain.Paragraph) string {
	var names []string
	style := p.Style()
	if style != nil {
		names = append(names, style.ID(), style.Name())
	}
	// docxgo currently returns nil from Style, but exposes the saved style ID.
	if named, ok := p.(interface{ StyleName() string }); ok {
		names = append(names, named.StyleName())
	}
	for _, name := range names {
		name = strings.ToLower(strings.ReplaceAll(name, " ", ""))
		if name == "title" {
			return "h1"
		}
		if level, ok := strings.CutPrefix(name, "heading"); ok && len(level) == 1 && level[0] >= '1' && level[0] <= '6' {
			return "h" + level
		}
	}
	return "p"
}

func renderDocxParagraph(b *strings.Builder, p domain.Paragraph) error {
	tag := docxParagraphTag(p)
	fmt.Fprintf(b, "<%s>", tag)
	for _, run := range p.Runs() {
		text := html.EscapeString(run.Text())
		if run.Underline() != domain.UnderlineNone {
			text = "<u>" + text + "</u>"
		}
		if run.Italic() {
			text = "<em>" + text + "</em>"
		}
		if run.Bold() {
			text = "<strong>" + text + "</strong>"
		}
		if len(text) > MaxContentSize-b.Len() {
			return ErrExtractedContentTooLarge
		}
		b.WriteString(text)
	}
	fmt.Fprintf(b, "</%s>", tag)
	if b.Len() > MaxContentSize {
		return ErrExtractedContentTooLarge
	}
	return nil
}
