package indexer

import (
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/asciimoo/hister/server/document"
)

type fileTypeHandler interface {
	Match(path string) bool
	Prepare(*document.Document, []byte) error
}

var fileTypeHandlers = []fileTypeHandler{
	pdfFileType{},
	docxFileType{},
	markdownFileType{},
	orgFileType{},
	emailFileType{},
	plainTextFileType{},
}

// PrepareFileContent extracts indexable fields using the same handler as a
// watched local file. It does not read, identify, or index the document.
func PrepareFileContent(path string, d *document.Document, content []byte) error {
	return fileTypeHandlerForPath(path).Prepare(d, content)
}

func fileTypeHandlerForPath(path string) fileTypeHandler {
	for _, handler := range fileTypeHandlers {
		if handler.Match(path) {
			return handler
		}
	}
	return plainTextFileType{}
}

type plainTextFileType struct{}

func (plainTextFileType) Match(_ string) bool {
	return true
}

func (plainTextFileType) Prepare(d *document.Document, content []byte) error {
	if !utf8.Valid(content) {
		return ErrBinaryFile
	}

	d.Text = string(content)
	return nil
}

func hasExtension(path string, exts ...string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return slices.Contains(exts, ext)
}
