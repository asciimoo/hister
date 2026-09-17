package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/indexer"
)

func fileWatchResponse(r *http.Request, status int) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}
}

func waitForWatchValue[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for watch import")
		var zero T
		return zero
	}
}

func TestWatchImportFilesUpdatesExistingAndNewSnapshots(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	checked := make(chan string, 8)
	submitted := make(chan document.Document, 8)
	c := client.New("http://hister.test", client.WithTargetUserID(42), client.WithAllowSensitive(), client.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("X-Hister-Target-User-ID"); got != "42" {
			t.Errorf("owner header = %q", got)
		}
		if r.Method == http.MethodHead {
			checked <- r.URL.Query().Get("url")
			return fileWatchResponse(r, http.StatusOK), nil
		}
		if r.URL.Path != "/api/add" {
			t.Errorf("unexpected request to %s", r.URL.Path)
		}
		var d document.Document
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			return nil, err
		}
		submitted <- d
		return fileWatchResponse(r, http.StatusCreated), nil
	})}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := watchImportFiles(ctx, c, nil, []*config.Directory{{Path: root, Label: "notes", Filetypes: []string{"txt"}, DeleteOnRemove: true}}, fileWatchOptions{
			Source: "laptop", MaxFileSize: 1024, SkipExisting: true, RetryDelay: time.Millisecond,
		})
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		if err := waitForWatchValue(t, done); err != nil {
			t.Error(err)
		}
	})
	wantURL, _ := remoteFileURL("laptop", path)
	if got := waitForWatchValue(t, checked); got != wantURL {
		t.Fatalf("checked URL = %q, want %q", got, wantURL)
	}
	for _, text := range []string{"first save", "second save"} {
		temporary := filepath.Join(root, "save.tmp")
		if err := os.WriteFile(temporary, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(temporary, path); err != nil {
			t.Fatal(err)
		}
		d := waitForWatchValue(t, submitted)
		if d.URL != wantURL || d.Text != text || d.Type != document.RemoteFile || d.Label != "notes" || !d.SkipSensitiveCheck {
			t.Fatalf("unexpected snapshot: %#v", d)
		}
	}
	newPath := filepath.Join(root, "new.txt")
	if err := os.WriteFile(newPath, []byte("new file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if d := waitForWatchValue(t, submitted); d.Text != "new file" {
		t.Fatalf("new snapshot text = %q", d.Text)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	select {
	case <-checked:
		t.Fatal("skip_existing applied after the initial scan")
	case <-submitted:
		t.Fatal("unexpected request after removal")
	case <-time.After(400 * time.Millisecond):
	}
}

func TestWatchImportFilesRetriesAndCancelsRequests(t *testing.T) {
	for _, mode := range []string{"retry", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "note.txt")
			if err := os.WriteFile(path, []byte("initial"), 0o600); err != nil {
				t.Fatal(err)
			}
			requests := make(chan int, 8)
			var attempts atomic.Int32
			c := client.New("http://hister.test", client.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				n := int(attempts.Add(1))
				requests <- n
				if mode == "cancel" {
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				if n == 1 {
					return fileWatchResponse(r, http.StatusServiceUnavailable), nil
				}
				return fileWatchResponse(r, http.StatusCreated), nil
			})}))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := watchImportFiles(ctx, c, []string{path}, nil, fileWatchOptions{Source: "laptop", MaxFileSize: 1024, RetryDelay: 10 * time.Millisecond})
				done <- err
			}()
			waitForWatchValue(t, requests)
			if mode == "retry" {
				if n := waitForWatchValue(t, requests); n != 2 {
					t.Fatalf("retry attempt = %d", n)
				}
			}
			cancel()
			if err := waitForWatchValue(t, done); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestImportWatchedFileFormatsAndLimits(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		skip          bool
		err           error
	}{
		{name: "notes.txt", content: "notes"},
		{name: "notes", content: "notes without an extension"},
		{name: "bracket", content: "["},
		{name: "links.bak", content: `[{"url":"https://example.com"}]`},
		{name: "notes.md", content: "# Notes"},
		{name: "settings.json", content: `{"setting":true}`},
		{name: "notes.html", content: "<html><title>Notes</title></html>"},
		{name: "backup.json", content: `[{"url":"https://example.com","type":0,"processed":true}]`, skip: true},
		{name: "backup", content: `[{"url":"https://example.com","type":0,"processed":true}]`},
		{name: "backup.bak", content: `[{"url":"https://example.com","type":0,"processed":true}]`},
		{name: "backup.txt", content: `[{"url":"https://example.com","type":0,"processed":true}]`},
		{name: "backup.html", content: `[{"url":"https://example.com","type":0,"processed":true}]`},
		{name: "large-backup", content: `[{"url":"https://example.com","type":0,"processed":true,"text":"` + strings.Repeat("x", 1025) + `"}]`, err: indexer.ErrFileTooLarge},
		{name: "empty-backup", content: "[\n]\n"},
		{name: "backup.7z", content: "archive", skip: true},
		{name: "page.html", content: `<html><head><link rel="canonical" href="https://example.com"></head></html>`, skip: true},
		{name: "large.txt", content: strings.Repeat("x", 1025), err: indexer.ErrFileTooLarge},
		{name: "empty.txt", err: indexer.ErrEmptyFile},
		{name: "binary.txt", content: "\xff", err: indexer.ErrBinaryFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.name)
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			requests := 0
			c := client.New("http://hister.test", client.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				var d document.Document
				if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
					return nil, err
				}
				if d.Label != "override" || d.Updated == 0 {
					t.Errorf("snapshot metadata = %#v", d)
				}
				return fileWatchResponse(r, http.StatusCreated), nil
			})}))
			skipped, err := importWatchedFile(context.Background(), c, importFileInput{Path: path, Label: "directory"}, fileWatchOptions{
				Source: "laptop", MaxFileSize: 1024, Label: documentLabelOverride{set: true, value: "override"},
			}, false)
			if skipped != tc.skip || !errors.Is(err, tc.err) {
				t.Fatalf("result = (%t, %v), want (%t, %v)", skipped, err, tc.skip, tc.err)
			}
			want := 1
			if tc.skip || tc.err != nil {
				want = 0
			}
			if requests != want {
				t.Fatalf("requests = %d, want %d", requests, want)
			}
		})
	}
}

func TestFileImportQueueKeepsLatestUpdateDuringRetry(t *testing.T) {
	queue := newFileImportQueue()
	queue.enqueue("note.txt", true, time.Now(), false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls int
	queue.run(ctx, func(_ context.Context, path string, skip bool) (bool, error) {
		calls++
		if calls == 1 {
			// A fresh write arrives while the initial request is in flight.
			queue.enqueue(path, false, time.Now(), false)
			return false, &client.HTTPError{StatusCode: http.StatusServiceUnavailable}
		}
		if skip {
			t.Error("retry replaced the newer write with skip_existing")
		}
		cancel()
		return false, nil
	}, time.Hour)
	if calls != 2 {
		t.Fatalf("processed %d requests, want 2", calls)
	}
}

func TestFileWatchInputsLimitExplicitPaths(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "subdir")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	explicit := filepath.Join(root, ".explicit.txt")
	if err := os.WriteFile(explicit, []byte("explicit"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := &config.Directory{Path: root, Filetypes: []string{"md"}, Label: "notes", Excludes: []string{"draft*"}}
	inputs, err := newFileWatchInputs([]string{subdir, explicit}, []*config.Directory{nil, dir})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, label string
		match       bool
	}{
		{path: filepath.Join(subdir, "note.md"), label: "notes", match: true},
		{path: explicit, match: true},
		{path: filepath.Join(root, "outside.md")},
		{path: filepath.Join(subdir, "note.txt")},
		{path: filepath.Join(subdir, "draft.md")},
		{path: filepath.Join(subdir, ".hidden", "note.md")},
	} {
		input, ok := inputs.input(tc.path)
		if ok != tc.match || input.Label != tc.label {
			t.Errorf("input(%q) = (%#v, %t), want label %q and match %t", tc.path, input, ok, tc.label, tc.match)
		}
	}
	if dir.Path != root {
		t.Fatal("watch setup mutated the configured directory")
	}
}
