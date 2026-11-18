package document

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/files"
)

type faviconTestTransport func(*http.Request) (*http.Response, error)

func (f faviconTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type trackedFaviconBody struct {
	io.Reader
	read   int
	closed bool
}

func (b *trackedFaviconBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += n
	return n, err
}

func (b *trackedFaviconBody) Close() error {
	b.closed = true
	return nil
}

func TestDownloadFaviconBoundsBodyReads(t *testing.T) {
	const limit = 8 * 1024 * 1024
	for _, tc := range []struct {
		name          string
		contentLength int64
		wantRead      int
	}{
		{name: "declared overflow", contentLength: limit + 1},
		{name: "streamed overflow", contentLength: -1, wantRead: limit + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedFaviconBody{Reader: strings.NewReader(strings.Repeat("x", limit+8192))}
			previousTransport := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previousTransport })
			http.DefaultTransport = faviconTestTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/favicon.ico" || req.Header.Get("User-Agent") != "Nogon" {
					t.Errorf("unexpected favicon request: %s, agent=%q", req.URL, req.Header.Get("User-Agent"))
				}
				return &http.Response{
					StatusCode:    http.StatusOK,
					Header:        http.Header{"Content-Type": {"image/png"}},
					Body:          body,
					ContentLength: tc.contentLength,
					Request:       req,
				}, nil
			})
			d := &Document{URL: "https://example.com/page", Favicon: "previous favicon"}
			if err := d.DownloadFavicon("Nogon"); err == nil || d.Favicon != "previous favicon" {
				t.Fatalf("oversized favicon was not rejected: %v", err)
			}
			if body.read != tc.wantRead || !body.closed {
				t.Errorf("favicon response read %d bytes and closed=%v, want %d bytes and closed", body.read, body.closed, tc.wantRead)
			}
		})
	}
}

func TestDownloadFaviconLimitsResponseSize(t *testing.T) {
	const limit = 8 * 1024 * 1024
	body := bytes.Repeat([]byte("x"), limit+1)
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		data := body
		switch r.URL.Path {
		case "/exact":
			data = data[:limit]
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		case "/known":
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		case "/chunked":
			w.(http.Flusher).Flush()
		case "/gzip":
			data = compressed.Bytes()
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		}
		// The client can close an oversized response before the write completes.
		if _, err := w.Write(data); err != nil {
			return
		}
	}))
	defer server.Close()
	for _, path := range []string{"/exact", "/known", "/chunked", "/gzip"} {
		t.Run(path, func(t *testing.T) {
			d := &Document{URL: server.URL, faviconURL: server.URL + path, Favicon: "previous favicon"}
			err := d.DownloadFavicon("Nogon")
			if path != "/exact" {
				if err == nil || !strings.Contains(err.Error(), "8 MiB") {
					t.Errorf("oversized favicon error = %v, want size limit error", err)
				}
				if d.Favicon != "previous favicon" {
					t.Error("oversized favicon replaced the previous value")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			encoded, ok := strings.CutPrefix(d.Favicon, "data:image/png;base64,")
			if !ok {
				t.Fatal("favicon lost its content type or data URL encoding")
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil || !bytes.Equal(decoded, body[:limit]) {
				t.Fatalf("favicon at the limit was truncated or corrupted: %v", err)
			}
		})
	}
}

func TestProcessPreparedLocalFileDoesNotReadFilesystem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("replacement content outside the prepared snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A file can become empty after its size was checked. Processing must use
	// the prepared snapshot instead of reopening its potentially replaced path.
	d := &Document{URL: files.PathToFileURL(path)}
	d.SetTrustedLocalFile()
	if err := d.Process(nil, nil); err != nil {
		t.Fatal(err)
	}
	if d.Text != "" {
		t.Fatalf("processing reread the source file: %q", d.Text)
	}
	if d.Type != Local || !d.Processed {
		t.Fatalf("prepared local file was not processed: %#v", d)
	}
}

func TestProcessSubmittedHTMLFileDoesNotReadFilesystem(t *testing.T) {
	d := &Document{
		URL:  "file:///path/that/does/not/exist/page.html",
		HTML: "<html><body>saved content</body></html>",
	}

	err := d.ProcessContext(context.Background(), nil, func(_ context.Context, got *Document) error {
		got.Text = "saved content"
		return nil
	})
	if err != nil {
		t.Fatalf("ProcessContext() unexpected error: %v", err)
	}
	if d.Text != "saved content" {
		t.Errorf("Text = %q, want extracted saved content", d.Text)
	}
	if d.Type != Web {
		t.Errorf("Type = %v, want web", d.Type)
	}
	if d.Domain != "local" {
		t.Errorf("Domain = %q, want local", d.Domain)
	}
	if !d.Processed {
		t.Error("document was not marked as processed")
	}
}

func TestProcessRemoteFilePreservesImportedSnapshot(t *testing.T) {
	d := &Document{
		URL:     "remote-file://workstation/home/alice/notes.md",
		Text:    "imported content",
		Updated: 1234,
		Type:    RemoteFile,
	}

	err := d.ProcessContext(context.Background(), nil, func(_ context.Context, _ *Document) error {
		t.Fatal("remote file processing called the web extractor")
		return nil
	})
	if err != nil {
		t.Fatalf("ProcessContext() unexpected error: %v", err)
	}
	if d.Type != RemoteFile {
		t.Errorf("Type = %v, want remote", d.Type)
	}
	if d.Domain != "workstation" {
		t.Errorf("Domain = %q, want workstation", d.Domain)
	}
	if d.Title != "alice/notes.md" {
		t.Errorf("Title = %q, want alice/notes.md", d.Title)
	}
	if d.Updated != 1234 {
		t.Errorf("Updated = %d, want 1234", d.Updated)
	}
	if !d.Processed {
		t.Error("document was not marked as processed")
	}
}

func TestProcessRemoteFileValidation(t *testing.T) {
	t.Run("invalid URL", func(t *testing.T) {
		d := &Document{URL: "file:///tmp/notes.txt", Text: "content", Type: RemoteFile}
		if err := d.Process(nil, nil); err == nil {
			t.Fatal("Process() accepted a local file URL for a remote file")
		}
	})

	t.Run("sensitive text", func(t *testing.T) {
		d := &Document{URL: "remote-file://client/tmp/notes.txt", Text: "secret token", Type: RemoteFile}
		err := d.ProcessWithSensitivePattern(nil, nil, regexp.MustCompile("secret"))
		if err != ErrSensitiveContent {
			t.Fatalf("ProcessWithSensitivePattern() error = %v, want %v", err, ErrSensitiveContent)
		}
	})
}
