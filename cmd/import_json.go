package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
)

const maxJSONExportLineSize = 64 * 1024 * 1024

type jsonExportState int

const (
	jsonExportStart jsonExportState = iota
	jsonExportFirstItem
	jsonExportNextItem
	jsonExportSeparator
	jsonExportLines
	jsonExportEnd
)

// jsonExportReader reads the line layout written by export, also accepting
// standalone document lines. Unexpected content must not be silently skipped.
type jsonExportReader struct {
	scanner     *bufio.Scanner
	line        int
	maxLineSize int
	state       jsonExportState
}

func newJSONExportReader(reader io.Reader, maxLineSize int) *jsonExportReader {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, min(64*1024, maxLineSize)), maxLineSize)
	return &jsonExportReader{scanner: scanner, maxLineSize: maxLineSize}
}

func (r *jsonExportReader) nextLine() ([]byte, error) {
	for r.scanner.Scan() {
		r.line++
		line := r.scanner.Bytes()
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		switch r.state {
		case jsonExportStart:
			if bytes.Equal(trimmed, []byte("[")) {
				r.state = jsonExportFirstItem
				continue
			}
			if trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']' && len(bytes.TrimSpace(trimmed[1:len(trimmed)-1])) == 0 {
				r.state = jsonExportEnd
				continue
			}
			if line[0] == '{' {
				r.state = jsonExportLines
				return line, nil
			}
		case jsonExportFirstItem, jsonExportNextItem:
			if r.state == jsonExportFirstItem && bytes.Equal(trimmed, []byte("]")) {
				r.state = jsonExportEnd
				continue
			}
			if line[0] == '{' {
				r.state = jsonExportSeparator
				return line, nil
			}
		case jsonExportSeparator:
			if bytes.Equal(trimmed, []byte(",")) {
				r.state = jsonExportNextItem
				continue
			}
			if bytes.Equal(trimmed, []byte("]")) {
				r.state = jsonExportEnd
				continue
			}
		case jsonExportLines:
			if line[0] == '{' {
				return line, nil
			}
		case jsonExportEnd:
			return nil, errors.New("unexpected content after the end of the export array")
		}
		return nil, errors.New("unsupported JSON export layout: each document must be on one line starting with '{', with array brackets and separating commas on their own lines. Use the original file produced by hister export")
	}
	if err := r.scanner.Err(); err != nil {
		r.line++
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("%w: export line exceeds the %d byte limit. Each serialized document must fit on one line; indexer.max_file_size_mb does not change this limit", err, r.maxLineSize)
		}
		return nil, err
	}
	switch r.state {
	case jsonExportStart:
		return nil, errors.New("export contains no documents or JSON array; an empty export must contain []")
	case jsonExportFirstItem, jsonExportNextItem, jsonExportSeparator:
		return nil, errors.New("incomplete JSON export: missing closing ']' or document. The file may be truncated; create a new export and retry")
	}
	return nil, io.EOF
}
