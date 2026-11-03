package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/testutil"
	"github.com/asciimoo/hister/server/types"
)

type statusCountingWriter struct {
	http.ResponseWriter
	statusCodes []int
}

func (w *statusCountingWriter) WriteHeader(statusCode int) {
	w.statusCodes = append(w.statusCodes, statusCode)
	w.ResponseWriter.WriteHeader(statusCode)
}

func TestRemovedAddEndpoints(t *testing.T) {
	previousFiles := staticTextFiles
	staticTextFiles = map[string][]byte{"index.html": []byte("<html>app</html>")}
	t.Cleanup(func() { staticTextFiles = previousFiles })
	for _, prefix := range []string{"", "/hister"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			cfg := testutil.InitModel(t)
			cfg.App.AccessToken = "secret"
			if err := cfg.UpdateBaseURL("http://127.0.0.1:4433" + prefix); err != nil {
				t.Fatal(err)
			}
			sessionStore = newSessionStore([]byte(strings.Repeat("x", 32)), cfg.BaseURL(""), sessionMaxAge)
			idx := newServerTestIndexer(t, cfg)
			handler := registerEndpoints(cfg, idx)
			headers := map[string]string{
				"Authorization": "Bearer secret",
				"Content-Type":  "application/json",
				"Origin":        "hister://",
			}
			for _, tc := range []struct {
				method, path string
				status       int
			}{
				{http.MethodGet, "/api/add", http.StatusNotFound},
				{http.MethodHead, "/api/add", http.StatusNotFound},
				{http.MethodPost, "/add", http.StatusMethodNotAllowed},
			} {
				response := testutil.ServeHTTP(t, handler, tc.method, prefix+tc.path,
					strings.NewReader(`{"url":"https://example.com/removed-add","text":"Must not be indexed"}`), headers)
				if response.Code != tc.status {
					t.Errorf("%s %s status = %d, want %d", tc.method, tc.path, response.Code, tc.status)
				}
			}
			if idx.GetByURLAndUser("https://example.com/removed-add", 0) != nil {
				t.Error("removed endpoint indexed a document")
			}

			response := testutil.ServeHTTP(t, handler, http.MethodGet, prefix+"/api", nil, headers)
			var endpoints []struct{ Method, Path string }
			if err := json.Unmarshal(response.Body.Bytes(), &endpoints); err != nil || response.Code != http.StatusOK {
				t.Fatalf("API documentation: %d %s, error = %v", response.Code, response.Body, err)
			}
			foundAdd := false
			for _, endpoint := range endpoints {
				if endpoint.Path == "/add" || endpoint.Path == "/api/add" && endpoint.Method != http.MethodPost {
					t.Errorf("API documentation still lists %s %s", endpoint.Method, endpoint.Path)
				}
				if endpoint.Method == http.MethodPost && endpoint.Path == "/api/add" {
					foundAdd = true
				}
			}
			if !foundAdd {
				t.Error("API documentation is missing POST /api/add")
			}
			response = testutil.ServeHTTP(t, handler, http.MethodGet, prefix+"/add", nil, nil)
			if response.Code != http.StatusOK || response.Body.String() != "<html>app</html>" {
				t.Fatalf("add page must remain available: %d %s", response.Code, response.Body)
			}
		})
	}
}

func TestAddFormAccessTokenDoesNotAuthenticate(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.App.AccessToken = "secret"
	sessionStore = newSessionStore([]byte(strings.Repeat("x", 32)), cfg.BaseURL(""), sessionMaxAge)
	called := false
	h := endpointHandler(func(c *webContext) {
		called = true
		c.Response.WriteHeader(http.StatusNoContent)
	})
	h = withCSRF(h)
	h = withTokenAuth(h)
	handler := http.HandlerFunc(createHandler(cfg, nil, h))
	body := url.Values{
		"access_token": {"secret"},
		"url":          {"https://example.com"},
	}.Encode()

	rec := testutil.ServeHTTP(
		t,
		handler,
		http.MethodPost,
		"/api/add",
		strings.NewReader(body),
		map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
			"Origin":       "https://unrelated.example",
		},
	)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /api/add status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if called {
		t.Fatal("POST /api/add with a form access token reached the protected handler")
	}
}

func TestServeAddFormWritesStatusOnce(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.Server.BaseURL = "http://127.0.0.1:4433"
	body := url.Values{
		"url": {"http://127.0.0.1:4433/already-local"},
	}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/api/add", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	writer := &statusCountingWriter{ResponseWriter: rec}

	serveAdd(&webContext{
		Request:  req,
		Response: writer,
		Config:   cfg,
	})

	if len(writer.statusCodes) != 1 {
		t.Fatalf("WriteHeader calls = %v, want one call", writer.statusCodes)
	}
	if writer.statusCodes[0] != http.StatusNotAcceptable {
		t.Fatalf("status = %d, want %d", writer.statusCodes[0], http.StatusNotAcceptable)
	}
}

func TestServeAddIndexesRemoteFileSnapshot(t *testing.T) {
	cfg := testutil.Config(t)
	idx, err := indexer.New(cfg)
	if err != nil {
		t.Fatalf("create indexer: %v", err)
	}
	t.Cleanup(idx.Close)

	d := &document.Document{
		URL:       "remote-file://workstation/home/alice/note.md",
		Text:      "Remote note\n\nExtracted body",
		Title:     "Remote note",
		Label:     "notes",
		Updated:   1234,
		Type:      document.RemoteFile,
		Domain:    "untrusted.example",
		Processed: true,
	}
	body, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/add", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	serveAdd(&webContext{Request: req, Response: rec, Config: cfg, Indexer: idx})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	got := idx.GetByURLAndUser(d.URL, 0)
	if got == nil {
		t.Fatal("remote file was not indexed")
	}
	if got.Type != document.RemoteFile {
		t.Errorf("type = %v, want remote", got.Type)
	}
	if got.Domain != "workstation" {
		t.Errorf("domain = %q, want workstation", got.Domain)
	}
	if got.Title != "Remote note" || got.Text != d.Text {
		t.Errorf("stored content = %#v, want submitted title and text", got)
	}
	if got.Updated != 1234 || got.Label != "notes" {
		t.Errorf("stored metadata = %#v, want updated time and label", got)
	}
}

func TestFilePreviewPreservesPlainText(t *testing.T) {
	cfg := testutil.InitModel(t)
	idx := newServerTestIndexer(t, cfg)
	handler := registerEndpoints(cfg, idx)
	const content = "\nFirst line\n\n\tindented <script>alert(1)</script> & <img src=x onerror=alert(1)>\nLast line\n"
	for _, tc := range []struct {
		url, syntax string
		kind        document.DocType
	}{
		{"file:///missing/notes.txt", "", document.Local},
		{"file:///missing/notes.TXT", "", document.Local},
		{"file:///missing/main.go", "go", document.Local},
		{"file:///missing/config.JSON", "json", document.Local},
		{"file:///missing/config.yml", "yaml", document.Local},
		{"file:///missing/config.toml", "toml", document.Local},
		{"file:///missing/source%2Epy", "python", document.Local},
		{"file:///missing/Dockerfile", "dockerfile", document.Local},
		{"file:///missing/Makefile", "makefile", document.Local},
		{"file:///missing/notes.unknown", "", document.Local},
		{"file:///missing/README", "", document.Local},
		{"remote-file://workstation/missing/notes.txt", "", document.RemoteFile},
		{"remote-file://workstation/missing/index.ts", "typescript", document.RemoteFile},
	} {
		t.Run(tc.url, func(t *testing.T) {
			// Preview must use the indexed snapshot even when the source is unavailable.
			d := &document.Document{
				URL: tc.url, Type: tc.kind,
				Title: "Notes", Text: content, Processed: true,
			}
			if err := idx.AddDocument(d); err != nil {
				t.Fatal(err)
			}
			response := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/preview?url="+url.QueryEscape(d.URL), nil, nil)
			if response.Code != http.StatusOK {
				t.Fatalf("preview status = %d: %s", response.Code, response.Body)
			}
			var preview types.DocumentPreviewResponse
			if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
				t.Fatal(err)
			}
			if preview.Template != "code" {
				t.Fatalf("preview template = %q, want code", preview.Template)
			}
			var code struct {
				Text   string `json:"text"`
				Syntax string `json:"syntax"`
			}
			if err := json.Unmarshal([]byte(preview.Content), &code); err != nil {
				t.Fatal(err)
			}
			if code.Text != content || code.Syntax != tc.syntax {
				t.Fatalf("code preview = %#v, want original text and syntax %q", code, tc.syntax)
			}
		})
	}
}

func TestFilePreviewRetainsOtherFormats(t *testing.T) {
	cfg := testutil.InitModel(t)
	idx := newServerTestIndexer(t, cfg)
	handler := registerEndpoints(cfg, idx)
	for _, tc := range []struct {
		url, html, want string
		kind            document.DocType
	}{
		{url: "file:///missing/paper.PDF", kind: document.Local, want: "Saved\nbody &lt;tag&gt;"},
		{url: "file:///missing/notes.docx", kind: document.Local, want: "Saved\nbody &lt;tag&gt;"},
		{url: "file:///missing/notes.md", kind: document.Local, html: "<p>Saved <strong>body</strong></p><script>alert(1)</script>", want: "<p>Saved <strong>body</strong></p>"},
		{url: "file:///missing/notes.ORG", kind: document.Local, html: "<p>Saved <strong>body</strong></p>", want: "<p>Saved <strong>body</strong></p>"},
		{url: "file:///missing/encoded%2Emd", kind: document.Local, want: "Saved\nbody &lt;tag&gt;"},
		{url: "https://example.com/notes.txt", kind: document.Web, want: "Saved\nbody &lt;tag&gt;"},
	} {
		t.Run(tc.url, func(t *testing.T) {
			d := &document.Document{
				URL: tc.url, Type: tc.kind, Title: "Notes",
				Text: "Saved\nbody <tag>", HTML: tc.html, Processed: true,
			}
			if err := idx.AddDocument(d); err != nil {
				t.Fatal(err)
			}
			response := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/preview?url="+url.QueryEscape(d.URL), nil, nil)
			if response.Code != http.StatusOK {
				t.Fatalf("preview status = %d: %s", response.Code, response.Body)
			}
			var preview types.DocumentPreviewResponse
			if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
				t.Fatal(err)
			}
			if preview.Content != tc.want {
				t.Fatalf("preview = %q, want %q", preview.Content, tc.want)
			}
		})
	}
}

func TestServeAddValidatesRemoteFileSnapshot(t *testing.T) {
	tests := []struct {
		name string
		doc  document.Document
	}{
		{
			name: "remote type requires remote URL",
			doc:  document.Document{URL: "file:///tmp/note.txt", Text: "note", Type: document.RemoteFile},
		},
		{
			name: "remote URL requires remote type",
			doc:  document.Document{URL: "remote-file://client/tmp/note.txt", Text: "note", Type: document.Web},
		},
		{
			name: "source host is required",
			doc:  document.Document{URL: "remote-file:///tmp/note.txt", Text: "note", Type: document.RemoteFile},
		},
		{
			name: "path is required",
			doc:  document.Document{URL: "remote-file://client/", Text: "note", Type: document.RemoteFile},
		},
		{
			name: "content is required",
			doc:  document.Document{URL: "remote-file://client/tmp/note.txt", Type: document.RemoteFile},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testutil.Config(t)
			body, err := json.Marshal(tt.doc)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/add", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			serveAdd(&webContext{Request: req, Response: rec, Config: cfg})

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestServeAddFormRejectsRemoteFileURLWithoutType(t *testing.T) {
	cfg := testutil.Config(t)
	body := url.Values{
		"url":  {"remote-file://client/tmp/note.txt"},
		"text": {"note"},
	}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/api/add", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	serveAdd(&webContext{Request: req, Response: rec, Config: cfg})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestServeAddRechecksSensitiveRemoteFileContent(t *testing.T) {
	cfg := testutil.Config(t)
	idx, err := indexer.New(cfg)
	if err != nil {
		t.Fatalf("create indexer: %v", err)
	}
	t.Cleanup(idx.Close)

	d := document.Document{
		URL:       "remote-file://client/tmp/key.txt",
		Text:      "-----BEGIN OPENSSH PRIVATE KEY-----",
		Type:      document.RemoteFile,
		Processed: true,
	}
	body, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/add", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	serveAdd(&webContext{Request: req, Response: rec, Config: cfg, Indexer: idx})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}
