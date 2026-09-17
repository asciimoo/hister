package cmd

import (
	"bufio"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestJSONExportReaderLayouts(t *testing.T) {
	const first = `{"url":"https://example.com/first","type":0,"processed":true}`
	const second = `{"url":"https://example.com/second","type":0,"processed":true}`
	for _, tc := range []struct {
		name, input string
		wantLines   []int
		wantError   string
	}{
		{name: "export", input: "[\n" + first + "\n,\n" + second + "\n]\n", wantLines: []int{2, 4}},
		{name: "document lines", input: first + "\n" + second, wantLines: []int{1, 2}},
		{name: "empty array", input: "[]\n"},
		{name: "empty array with spaces", input: " \t[ \t ]\r\n"},
		{name: "empty array on multiple lines", input: "[\n \n]\n"},
		{name: "blank lines and CRLF", input: "\r\n[\r\n\r\n" + first + "\r\n]\r\n", wantLines: []int{4}},
		{name: "compact array", input: "[" + first + "]", wantError: "unsupported JSON export layout"},
		{name: "indented record", input: "[\n  " + first + "\n]", wantError: "unsupported JSON export layout"},
		{name: "pretty printed record", input: "[\n  {\n    \"url\": \"https://example.com\"\n  }\n]", wantError: "unsupported JSON export layout"},
		{name: "missing comma", input: "[\n" + first + "\n" + second + "\n]", wantLines: []int{2}, wantError: "unsupported JSON export layout"},
		{name: "trailing comma", input: "[\n" + first + "\n,\n]", wantLines: []int{2}, wantError: "unsupported JSON export layout"},
		{name: "truncated array", input: "[\n" + first + "\n", wantLines: []int{2}, wantError: "incomplete JSON export"},
		{name: "truncated empty array", input: "[\n", wantError: "incomplete JSON export"},
		{name: "content after array", input: "[]\n" + first, wantError: "unexpected content after"},
		{name: "empty file", wantError: "empty export must contain []"},
		{name: "whitespace only", input: " \n\t\n", wantError: "empty export must contain []"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := newJSONExportReader(strings.NewReader(tc.input), maxJSONExportLineSize)
			var lines []int
			var readErr error
			for {
				line, err := reader.nextLine()
				if err != nil {
					readErr = err
					break
				}
				if string(line) != first && string(line) != second {
					t.Fatalf("unexpected document line: %q", line)
				}
				lines = append(lines, reader.line)
			}
			if !reflect.DeepEqual(lines, tc.wantLines) {
				t.Fatalf("document lines = %v, want %v", lines, tc.wantLines)
			}
			if tc.wantError == "" {
				if !errors.Is(readErr, io.EOF) {
					t.Fatalf("unexpected read error: %v", readErr)
				}
			} else if readErr == nil || !strings.Contains(readErr.Error(), tc.wantError) {
				t.Fatalf("read error = %v, want %q", readErr, tc.wantError)
			}
		})
	}
}

func TestJSONExportReaderLineLimit(t *testing.T) {
	reader := newJSONExportReader(strings.NewReader("[\n"+strings.Repeat("x", 128)), 128)
	_, err := reader.nextLine()
	if !errors.Is(err, bufio.ErrTooLong) || reader.line != 2 {
		t.Fatalf("read error = %v at line %d, want oversized line 2", err, reader.line)
	}
	if !strings.Contains(err.Error(), "128 byte limit") || !strings.Contains(err.Error(), "indexer.max_file_size_mb does not change this limit") {
		t.Fatalf("error does not explain the export line limit: %v", err)
	}
}
