package extract

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mmonterroca/docxgo/v2/domain"

	"github.com/asciimoo/hister/config"
)

func TestExtract(t *testing.T) {
	docxData := buildDocx(t, []docxTestParagraph{
		{style: domain.StyleIDTitle, text: "Document title"},
		{text: "Document body"},
	})
	for _, tc := range []struct {
		name, content, title, text, html string
		format                           Format
	}{
		{name: "notes.txt", content: "Plain body", text: "Plain body", format: PlainText},
		{name: "unknown.extension", content: "Fallback body", text: "Fallback body", format: PlainText},
		{name: "page.HTML", content: "<html><title>Page</title><body>Body</body></html>", html: "<html><title>Page</title><body>Body</body></html>", format: HTML},
		{name: "page.htm", content: "<p>Body</p>", html: "<p>Body</p>", format: HTML},
		{name: "notes.md", content: "# Heading\n\nBody", title: "Heading", text: "Body", html: "<h1", format: Markdown},
		{name: "notes.MARKDOWN", content: "# Heading\n\nBody", title: "Heading", text: "Body", html: "<h1", format: Markdown},
		{name: "notes.ORG", content: "#+TITLE: Org title\n\n* Heading\n\nBody", title: "Org title", text: "Body", html: "<h2", format: Org},
		{name: "notes.DOCX", content: string(docxData), title: "Document title", text: "Document body", format: DOCX},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := New(config.PDFParser{}).Extract(context.Background(), tc.name, []byte(tc.content))
			if err != nil {
				t.Fatal(err)
			}
			if result.Format != tc.format || result.Title != tc.title || !strings.Contains(result.Text, tc.text) || !strings.Contains(result.HTML, tc.html) {
				t.Fatalf("unexpected extraction result: %#v", result)
			}
			if tc.format == HTML && (result.HTML != tc.content || result.Text != "") {
				t.Fatal("HTML should be passed intact to the caller's web extraction chain")
			}
		})
	}
}

func TestExtractUsesSuppliedBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("content on disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	parser := New(config.PDFParser{})
	for _, name := range []string{path, filepath.Join(t.TempDir(), "missing.txt")} {
		result, err := parser.Extract(context.Background(), name, []byte("supplied snapshot"))
		if err != nil || result.Text != "supplied snapshot" {
			t.Fatalf("result = %#v, error = %v", result, err)
		}
	}
}

func TestExtractRejectsInvalidContent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content []byte
		want    error
	}{
		{name: "notes.txt", content: []byte{0xff}, want: ErrBinaryFile},
		{name: "page.html", content: []byte{0xff}, want: ErrBinaryFile},
		{name: "paper.pdf", content: []byte("not a PDF")},
		{name: "notes.md"},
		{name: "notes.org"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := New(config.PDFParser{}).Extract(context.Background(), tc.name, tc.content)
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) || result != (Result{}) {
				t.Fatalf("result = %#v, error = %v, want empty result and %v", result, err, tc.want)
			}
		})
	}
}

func TestExtractLimitsContent(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		tooLarge      bool
	}{
		{name: "exact.txt", content: strings.Repeat("x", 32<<20)},
		{name: "large.txt", content: strings.Repeat("x", (32<<20)+1), tooLarge: true},
		{name: "expanded.md", content: strings.Repeat("&", 7<<20), tooLarge: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := New(config.PDFParser{}).Extract(context.Background(), tc.name, []byte(tc.content))
			if tc.tooLarge {
				if !errors.Is(err, ErrExtractedContentTooLarge) || result != (Result{}) {
					t.Fatalf("oversized extraction returned %d bytes and error %v", len(result.Text)+len(result.HTML), err)
				}
			} else if err != nil || result.Text != tc.content {
				t.Fatalf("content at limit rejected or truncated: %v", err)
			}
		})
	}
}

func TestExtractRejectsOrgFileReferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.org")
	const secret = "private content outside the supplied snapshot"
	if err := os.WriteFile(path, []byte("#+TITLE: "+secret+"\n\n"+secret), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, directive := range []string{"INCLUDE", "SETUPFILE"} {
		t.Run(directive, func(t *testing.T) {
			reference := path
			if directive == "INCLUDE" {
				reference = fmt.Sprintf("%q src text", path)
			}
			content := fmt.Appendf(nil, "#+%s: %s\n\nSnapshot body", directive, reference)
			result, err := New(config.PDFParser{}).Extract(context.Background(), "snapshot.org", content)
			if !errors.Is(err, ErrExternalFileReference) || result != (Result{}) {
				t.Fatalf("result = %#v, error = %v, want empty result and forbidden reference", result, err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatal("extraction error exposed referenced file contents")
			}
		})
	}
}

func TestConfiguredExtraction(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Allow for the race detector's one second shutdown delay in the helper process.
	cfg := config.PDFParser{Command: executable, Args: []string{"-test.run=^TestPDFParserProcess$", "--", "--pdf-parser-test", "success", "literal ; $(echo unsafe)"}, Timeout: 10}
	parser := New(cfg)
	cfg.Args[len(cfg.Args)-1] = "changed after construction"
	other := New(cfg)
	const input = "%PDF-1.4 test bytes"
	for _, tc := range []struct {
		parser   *Extractor
		argument string
	}{
		{parser, "literal ; $(echo unsafe)"},
		{other, "changed after construction"},
	} {
		result, err := tc.parser.Extract(context.Background(), "paper.PDF", []byte(input))
		if err != nil || result.Text != "External PDF text: "+input+"\n"+tc.argument || result.Format != PDF {
			t.Fatalf("result = %#v, error = %v", result, err)
		}
	}
	if _, err := New(config.PDFParser{}).Extract(context.Background(), "paper.pdf", []byte(input)); err == nil {
		t.Fatal("configured parser leaked into another instance")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := parser.Extract(ctx, "paper.pdf", []byte(input))
	if !errors.Is(err, context.Canceled) || result != (Result{}) {
		t.Fatalf("result = %#v, error = %v, want cancellation", result, err)
	}
}

func TestPDFParserProcess(t *testing.T) {
	separator := slices.Index(os.Args, "--pdf-parser-test")
	if separator < 0 {
		return
	}
	args := os.Args[separator+1:]
	data, err := io.ReadAll(os.Stdin)
	if err != nil || !strings.HasPrefix(string(data), "%PDF-") {
		os.Exit(2)
	}
	if _, err := fmt.Printf("External PDF text: %s\n%s", data, strings.Join(args[1:], "|")); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestRenderPDF(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		contains   []string
		pages      int
	}{
		{
			name: "saved text without page boundaries",
			text: "First line\r\n  indented line\r\n \r\nSecond paragraph <img src=x onerror=alert(1)> & text",
			contains: []string{
				"<p>First line<br>  indented line</p>",
				"<p>Second paragraph &lt;img src=x onerror=alert(1)&gt; &amp; text</p>",
			},
			pages: 1,
		},
		{
			name: "pages including a blank page and trailing form feed",
			text: "First page\f\fThird page\f\n",
			contains: []string{
				"Page 1", "Page 2", "Page 3", "<p>First page</p>", "<p>Third page</p>",
				"No extractable text on this page.",
			},
			pages: 3,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Rendering must work from stored text without a source file or parser command.
			result, err := New(config.PDFParser{Command: "/missing/parser"}).Render(context.Background(), "/missing/paper.PDF", Result{Text: tc.text, HTML: "<script>untrusted()</script>"})
			if err != nil {
				t.Fatal(err)
			}
			if result.Template != "pdf" || strings.Count(result.Content, `class="pdf-page"`) != tc.pages {
				t.Fatalf("unexpected PDF preview: %#v", result)
			}
			for _, want := range tc.contains {
				if !strings.Contains(result.Content, want) {
					t.Errorf("preview %q does not contain %q", result.Content, want)
				}
			}
			if strings.ContainsAny(result.Content, "\r\f") || strings.Contains(result.Content, "<img") || strings.Contains(result.Content, "<script") {
				t.Fatalf("preview contains unsafe markup or unrendered control characters: %q", result.Content)
			}
			if !strings.Contains(tc.text, "\f") && strings.Contains(result.Content, "Page 1") {
				t.Fatal("preview invented page numbers for previously stored text")
			}
		})
	}
}

func TestExtractPDFPreservesPagesAndLimitsOutput(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		pages      int
		tooLarge   bool
	}{
		{name: "small", text: "Hello", pages: 2},
		{name: "at limit", text: strings.Repeat("x", (1<<20)-2), pages: 32},
		{name: "page break exceeds limit", text: strings.Repeat("x", (1<<20)-1), pages: 32, tooLarge: true},
		{name: "large", text: strings.Repeat("x", 1<<20), pages: 33, tooLarge: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := "BT /F1 12 Tf (" + tc.text + ") Tj ET"
			objects := []string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Repeat("3 0 R ", tc.pages), tc.pages),
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
			}
			var data strings.Builder
			data.WriteString("%PDF-1.4\n")
			offsets := make([]int, len(objects))
			for i, object := range objects {
				offsets[i] = data.Len()
				fmt.Fprintf(&data, "%d 0 obj\n%s\nendobj\n", i+1, object)
			}
			xref := data.Len()
			fmt.Fprintf(&data, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
			for _, offset := range offsets {
				fmt.Fprintf(&data, "%010d 00000 n \n", offset)
			}
			fmt.Fprintf(&data, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
			result, err := New(config.PDFParser{}).Extract(context.Background(), "paper.pdf", []byte(data.String()))
			if tc.tooLarge {
				if !errors.Is(err, ErrExtractedContentTooLarge) || result != (Result{}) {
					t.Fatalf("oversized PDF produced %d text bytes and error %v", len(result.Text), err)
				}
			} else if err != nil || result.Text != strings.Repeat("\n"+tc.text+"\f", tc.pages) {
				t.Fatalf("PDF text length = %d, error = %v", len(result.Text), err)
			}
		})
	}
}
