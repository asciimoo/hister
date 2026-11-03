// SPDX-License-Identifier: AGPL-3.0-or-later

package extract

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/asciimoo/hister/config"

	docx "github.com/mmonterroca/docxgo/v2"
	"github.com/mmonterroca/docxgo/v2/domain"
)

func TestExtractDOCXPreservesParagraphFormatting(t *testing.T) {
	data := buildDocx(t, []docxTestParagraph{
		{style: domain.StyleIDTitle, text: "Document title"},
		{style: domain.StyleIDHeading2, text: "Section heading"},
		{text: "  Indented\ttext & <script>alert(1)</script>", bold: true, italic: true, underline: true},
		{text: "Another paragraph."},
	})
	result, err := New(config.PDFParser{}).Extract(context.Background(), "report.DOCX", data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<h1>Document title</h1>", "<h2>Section heading</h2>",
		"<strong><em><u>  Indented\ttext &amp; &lt;script&gt;alert(1)&lt;/script&gt;</u></em></strong>",
		"<p>Another paragraph.</p>",
	} {
		if !strings.Contains(result.HTML, want) {
			t.Errorf("DOCX HTML %q does not contain %q", result.HTML, want)
		}
	}
	if result.Title != "Document title" || !strings.Contains(result.Text, "Section heading\n\nIndented\ttext & <script>alert(1)</script>\n\nAnother paragraph.") {
		t.Fatalf("formatting changed searchable text or title: %#v", result)
	}
}

func TestRenderDOCXFromStoredContent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content Result
		want    []string
	}{
		{
			name:    "formatted snapshot",
			content: Result{HTML: `<h1>Heading</h1><p><strong>Bold</strong> and <em>italic</em> <u>underlined</u></p><script>alert(1)</script><a href="javascript:alert(1)">link</a><img src=x onerror="alert(1)">`},
			want:    []string{"<h1>Heading</h1>", "<strong>Bold</strong>", "<em>italic</em>", "<u>underlined</u>"},
		},
		{
			name:    "previously indexed text",
			content: Result{Text: "First line\r\n  indented\tline\r\n \r\nNext paragraph <script> & text"},
			want:    []string{"<p>First line<br>  indented\tline</p>", "<p>Next paragraph &lt;script&gt; &amp; text</p>"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := New(config.PDFParser{}).Render(context.Background(), "/missing/report.DOCX", tc.content)
			if err != nil {
				t.Fatal(err)
			}
			if result.Template != "docx" || !strings.Contains(result.Content, `aria-label="DOCX preview"`) {
				t.Fatalf("unexpected DOCX preview: %#v", result)
			}
			for _, want := range tc.want {
				if !strings.Contains(result.Content, want) {
					t.Errorf("preview %q does not contain %q", result.Content, want)
				}
			}
			for _, unsafe := range []string{"<script", "javascript:", "onerror="} {
				if strings.Contains(result.Content, unsafe) {
					t.Errorf("preview contains unsafe markup: %q", result.Content)
				}
			}
		})
	}
}

func TestDOCXFormattingLimits(t *testing.T) {
	parser := New(config.PDFParser{})
	// HTML escaping can exceed the limit even when the searchable text fits.
	text := strings.Repeat("&", 7<<20)
	result, err := parser.Extract(context.Background(), "report.docx", buildDocx(t, []docxTestParagraph{{text: text}}))
	if !errors.Is(err, ErrExtractedContentTooLarge) || result != (Result{}) {
		t.Fatalf("oversized DOCX HTML returned %d bytes and error %v", len(result.HTML), err)
	}
	preview, err := parser.Render(context.Background(), "report.docx", Result{Text: text})
	if !errors.Is(err, ErrExtractedContentTooLarge) || preview != (RenderResult{}) {
		t.Fatalf("oversized DOCX preview returned %d bytes and error %v", len(preview.Content), err)
	}
}

func TestExtractDocxTextLimitsUncompressedContent(t *testing.T) {
	original := buildDocx(t, []docxTestParagraph{{text: "Document body"}})
	reader, err := zip.NewReader(bytes.NewReader(original), int64(len(original)))
	if err != nil {
		t.Fatal(err)
	}
	var originalSize int
	for _, entry := range reader.File {
		originalSize += int(entry.UncompressedSize64)
	}
	for _, tc := range []struct {
		name     string
		sizes    []int
		tooLarge bool
	}{
		{name: "at limit", sizes: []int{(64 << 20) - originalSize}},
		{name: "single entry", sizes: []int{65 << 20}, tooLarge: true},
		{name: "aggregate", sizes: []int{33 << 20, 33 << 20}, tooLarge: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			writer := zip.NewWriter(&buf)
			for _, entry := range reader.File {
				if err := writer.Copy(entry); err != nil {
					t.Fatal(err)
				}
			}
			chunk := strings.Repeat("x", 1<<20)
			for i, size := range tc.sizes {
				entry, err := writer.Create(fmt.Sprintf("extra/%d.bin", i))
				if err != nil {
					t.Fatal(err)
				}
				for size > 0 {
					n := min(size, len(chunk))
					if _, err := io.WriteString(entry, chunk[:n]); err != nil {
						t.Fatal(err)
					}
					size -= n
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			text, _, _, err := extractDocxContent(buf.Bytes())
			if tc.tooLarge {
				if err == nil || !strings.Contains(err.Error(), "uncompressed") {
					t.Fatalf("oversized DOCX returned text %q and error %v", text, err)
				}
			} else if err != nil || text != "Document body" {
				t.Fatalf("DOCX at limit returned text %q and error %v", text, err)
			}
		})
	}
}

func TestExtractDocxTextUsesStyledTitleAndParagraphs(t *testing.T) {
	docxData := buildDocx(t, []docxTestParagraph{
		{style: domain.StyleIDTitle, text: "Document title"},
		{text: "First body paragraph."},
		{text: "Second body paragraph."},
	})

	text, title, _, err := extractDocxContent(docxData)
	if err != nil {
		t.Fatalf("extractDocxText failed: %v", err)
	}
	if title != "Document title" {
		t.Fatalf("title = %q, want %q", title, "Document title")
	}
	for _, want := range []string{
		"Document title",
		"First body paragraph.",
		"Second body paragraph.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text %q missing %q", text, want)
		}
	}
}

func TestExtractDocxTextFallsBackToStyledTitle(t *testing.T) {
	docxData := buildDocx(t, []docxTestParagraph{
		{style: domain.StyleIDHeading1, text: "Styled heading"},
		{text: "Body paragraph."},
	})

	text, title, _, err := extractDocxContent(docxData)
	if err != nil {
		t.Fatalf("extractDocxText failed: %v", err)
	}
	if title != "Styled heading" {
		t.Fatalf("title = %q, want %q", title, "Styled heading")
	}
	if text != "Styled heading\n\nBody paragraph." {
		t.Fatalf("text = %q", text)
	}
}

func TestExtractDocxTextFallsBackToFirstParagraph(t *testing.T) {
	docxData := buildDocx(t, []docxTestParagraph{
		{text: "First paragraph."},
		{text: "Second paragraph."},
	})

	text, title, _, err := extractDocxContent(docxData)
	if err != nil {
		t.Fatalf("extractDocxText failed: %v", err)
	}
	if title != "First paragraph." {
		t.Fatalf("title = %q, want %q", title, "First paragraph.")
	}
	if text != "First paragraph.\n\nSecond paragraph." {
		t.Fatalf("text = %q", text)
	}
}

type docxTestParagraph struct {
	style                   string
	text                    string
	bold, italic, underline bool
}

func buildDocx(t *testing.T, paragraphs []docxTestParagraph) []byte {
	t.Helper()

	doc := docx.NewDocument()
	for _, paragraph := range paragraphs {
		p, err := doc.AddParagraph()
		if err != nil {
			t.Fatalf("AddParagraph failed: %v", err)
		}
		if paragraph.style != "" {
			if err := p.SetStyle(paragraph.style); err != nil {
				t.Fatalf("SetStyle failed: %v", err)
			}
		}
		run, err := p.AddRun()
		if err != nil {
			t.Fatalf("AddRun failed: %v", err)
		}
		if err := run.SetText(paragraph.text); err != nil {
			t.Fatalf("SetText failed: %v", err)
		}
		if err := run.SetBold(paragraph.bold); err != nil {
			t.Fatal(err)
		}
		if err := run.SetItalic(paragraph.italic); err != nil {
			t.Fatal(err)
		}
		if paragraph.underline {
			if err := run.SetUnderline(domain.UnderlineSingle); err != nil {
				t.Fatal(err)
			}
		}
	}

	var buf bytes.Buffer
	if _, err := doc.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}
	return buf.Bytes()
}
