package indexer

import (
	"errors"
	"strings"
	"testing"

	"github.com/mmonterroca/docxgo/v2/domain"

	"github.com/asciimoo/hister/server/document"
)

func TestFileTypeHandlerForPath(t *testing.T) {
	tests := []struct {
		path string
		want any
	}{
		{path: "paper.pdf", want: pdfFileType{}},
		{path: "paper.PDF", want: pdfFileType{}},
		{path: "paper.docx", want: docxFileType{}},
		{path: "paper.DOCX", want: docxFileType{}},
		{path: "notes.md", want: markdownFileType{}},
		{path: "notes.markdown", want: markdownFileType{}},
		{path: "notes.org", want: orgFileType{}},
		{path: "mail.eml", want: emailFileType{}},
		{path: "mail.EML", want: emailFileType{}},
		{path: "notes.txt", want: plainTextFileType{}},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := fileTypeHandlerForPath(tt.path)
			if got != tt.want {
				t.Fatalf("handler = %T, want %T", got, tt.want)
			}
		})
	}
}

func TestPrepareFileContent(t *testing.T) {
	docxData := buildDocx(t, []docxTestParagraph{
		{style: domain.StyleIDTitle, text: "Document title"},
		{text: "Document body"},
	})
	tests := []struct {
		name         string
		path         string
		content      []byte
		wantTitle    string
		wantText     []string
		wantHTML     string
		wantMetadata string
	}{
		{
			name:     "plain text",
			path:     "notes.txt",
			content:  []byte("Plain text body"),
			wantText: []string{"Plain text body"},
		},
		{
			name:         "markdown",
			path:         "notes.md",
			content:      []byte("# Markdown title\n\nMarkdown body"),
			wantTitle:    "Markdown title",
			wantText:     []string{"Markdown title", "Markdown body"},
			wantHTML:     "<h1",
			wantMetadata: "markdown",
		},
		{
			name:         "org",
			path:         "notes.org",
			content:      []byte("#+TITLE: Org title\n\n* Org heading\n\nOrg body"),
			wantTitle:    "Org title",
			wantText:     []string{"Org heading", "Org body"},
			wantHTML:     "<h2",
			wantMetadata: "org",
		},
		{
			name:         "docx",
			path:         "notes.docx",
			content:      docxData,
			wantTitle:    "Document title",
			wantText:     []string{"Document title", "Document body"},
			wantMetadata: "docx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &document.Document{
				URL:    "file:///client/notes",
				Type:   document.Local,
				UserID: 42,
				Label:  "notes",
			}
			if err := PrepareFileContent(tt.path, d, tt.content); err != nil {
				t.Fatalf("PrepareFileContent failed: %v", err)
			}
			if d.URL != "file:///client/notes" || d.Type != document.Local || d.UserID != 42 || d.Label != "notes" {
				t.Fatalf("document identity changed during preparation: %#v", d)
			}
			if d.Processed {
				t.Fatal("document was marked processed during content preparation")
			}
			if d.Title != tt.wantTitle {
				t.Fatalf("title = %q, want %q", d.Title, tt.wantTitle)
			}
			for _, want := range tt.wantText {
				if !strings.Contains(d.Text, want) {
					t.Errorf("text %q does not contain %q", d.Text, want)
				}
			}
			if tt.wantHTML != "" && !strings.Contains(d.HTML, tt.wantHTML) {
				t.Errorf("HTML %q does not contain %q", d.HTML, tt.wantHTML)
			}
			if tt.wantMetadata != "" && d.Metadata["type"] != tt.wantMetadata {
				t.Errorf("metadata type = %v, want %q", d.Metadata["type"], tt.wantMetadata)
			}
		})
	}
}

func TestPrepareFileContentRejectsInvalidContent(t *testing.T) {
	t.Run("binary plain text", func(t *testing.T) {
		err := PrepareFileContent("notes.txt", &document.Document{}, []byte{0xff})
		if !errors.Is(err, ErrBinaryFile) {
			t.Fatalf("error = %v, want %v", err, ErrBinaryFile)
		}
	})

	t.Run("invalid PDF", func(t *testing.T) {
		err := PrepareFileContent("notes.pdf", &document.Document{}, []byte("not a PDF"))
		if err == nil || !strings.Contains(err.Error(), "pdf text extraction") {
			t.Fatalf("error = %v, want PDF extraction error", err)
		}
	})
}
