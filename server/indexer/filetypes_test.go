package indexer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/testutil"
)

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
	switch args[0] {
	case "success":
		if _, err := fmt.Printf("External PDF text: %s\n%s", data, strings.Join(args[1:], "|")); err != nil {
			os.Exit(2)
		}
	case "fail":
		if _, err := fmt.Fprint(os.Stdout, "partial output"); err != nil {
			os.Exit(2)
		}
		if _, err := fmt.Fprint(os.Stderr, "private document diagnostic"); err != nil {
			os.Exit(2)
		}
		os.Exit(3)
	case "empty":
		if _, err := fmt.Fprint(os.Stdout, " \n"); err != nil {
			os.Exit(2)
		}
	case "invalid":
		_, _ = os.Stdout.Write([]byte{0xff})
	case "timeout":
		time.Sleep(10 * time.Second)
	case "large":
		chunk := strings.Repeat("x", 1<<20)
		for range 33 {
			if _, err := fmt.Fprint(os.Stdout, chunk); err != nil {
				os.Exit(2)
			}
		}
	}
	os.Exit(0)
}

func externalPDFTestConfig(t *testing.T, mode string) *config.Config {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := testutil.Config(t)
	cfg.Indexer.DetectLanguages = false
	// Allow for the race detector's one second shutdown delay in the helper process.
	timeout := 10
	if mode == "timeout" {
		timeout = 1
	}
	data, err := yaml.Marshal(map[string]any{"indexer": map[string]any{"pdf_parser": map[string]any{
		"command": executable,
		"args":    []string{"-test.run=^TestPDFParserProcess$", "--", "--pdf-parser-test", mode, "literal ; $(echo unsafe)"},
		"timeout": timeout,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestExternalPDFParserUploadsAndWatchedFiles(t *testing.T) {
	cfg := externalPDFTestConfig(t, "success")
	dir := t.TempDir()
	cfg.Indexer.Directories = []*config.Directory{{Path: dir}}
	idx := newTestIndexer(t, cfg)
	t.Cleanup(idx.Close)
	const input = "%PDF-1.4 test bytes"
	d := &document.Document{URL: "https://example.com/paper.pdf"}
	if err := idx.AddPDF(d, []byte(input)); err != nil {
		t.Fatalf("external parser was not used for upload: %v", err)
	}
	if d.Text != "External PDF text: "+input+"\nliteral ; $(echo unsafe)" || d.Metadata["type"] != "pdf" {
		t.Fatalf("unexpected parsed PDF: %#v", d)
	}
	path := testutil.WriteFile(t, dir, "paper.pdf", []byte(input))
	if err := idx.IndexFile(path, 0); err != nil {
		t.Fatal(err)
	}
	if got := idx.GetByURLAndUser(files.PathToFileURL(path), 0); got == nil || got.Text != d.Text {
		t.Fatalf("watched PDF was not parsed externally: %#v", got)
	}
	if err := idx.Reindex(&config.Rules{}, false, false, cfg.Indexer.KeepStopwords, cfg.Indexer.Directories); err != nil {
		t.Fatal(err)
	}
	if got := idx.GetByURLAndUser(files.PathToFileURL(path), 0); got == nil || got.Text != d.Text {
		t.Fatalf("reindex lost PDF parser settings: %#v", got)
	}
	if err := idx.AddPDF(&document.Document{URL: "https://example.com/after.pdf"}, []byte(input)); err != nil {
		t.Fatalf("PDF parser settings were lost after reindex: %v", err)
	}
	builtin := newTestIndexer(t, testutil.Config(t))
	t.Cleanup(builtin.Close)
	if err := builtin.AddPDF(&document.Document{URL: "https://example.com/builtin.pdf"}, []byte(input)); err == nil {
		t.Fatal("parser configuration leaked to another indexer")
	}
}

func TestExternalPDFParserRejectsFailures(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{
		{"fail", "exit status"},
		{"empty", "no extractable text"},
		{"invalid", "UTF"},
		{"timeout", "deadline exceeded"},
		{"large", "output exceeds"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			idx := newTestIndexer(t, externalPDFTestConfig(t, tc.mode))
			t.Cleanup(idx.Close)
			d := &document.Document{URL: "https://example.com/fail.pdf"}
			err := idx.AddPDF(d, []byte("%PDF-1.4 test bytes"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "private document diagnostic") || d.Text != "" {
				t.Fatal("failed parser exposed or stored partial output")
			}
			if idx.GetByURLAndUser(d.URL, 0) != nil {
				t.Fatal("failed PDF was indexed")
			}
		})
	}
}

func TestExternalPDFParserCancellationAndMissingCommand(t *testing.T) {
	cfg := externalPDFTestConfig(t, "timeout")
	idx := newTestIndexer(t, cfg)
	t.Cleanup(idx.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := idx.AddPDFContext(ctx, &document.Document{URL: "https://example.com/canceled.pdf"}, []byte("%PDF-1.4 test bytes")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want the caller's deadline", err)
	}
	cfg.Indexer.PDFParser.Command = filepath.Join(t.TempDir(), "missing-parser")
	cfg.App.Directory = t.TempDir()
	missing := newTestIndexer(t, cfg)
	t.Cleanup(missing.Close)
	if err := missing.AddPDF(&document.Document{URL: "https://example.com/missing.pdf"}, []byte("%PDF-1.4 test bytes")); !errors.Is(err, os.ErrNotExist) && !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("error = %v, want missing executable error", err)
	}
}
