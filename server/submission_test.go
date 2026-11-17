package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"
)

// submitTestDocuments exercises the public request formats and reports the
// status of each document, including per-operation failures in batch responses.
func submitTestDocuments(t *testing.T, c webContext, endpoint string, headers map[string]string, docs ...document.Document) []batchOpResult {
	t.Helper()
	var payload any
	if endpoint == "/api/batch" {
		ops := make([]batchOp, len(docs))
		for i, d := range docs {
			ops[i] = batchOp{Op: batchOpAdd, Document: d}
		}
		payload = batchRequest{Ops: ops}
	} else {
		if len(docs) != 1 {
			t.Fatal("single document requests require exactly one document")
		}
		payload = docs[0]
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	c.Request = httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		c.Request.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	c.Response = rec
	if endpoint == "/api/add" {
		serveAdd(&c)
		return []batchOpResult{{Status: rec.Code, Error: rec.Body.String()}}
	}
	serveBatch(&c)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch status = %d: %s", rec.Code, rec.Body.String())
	}
	var response batchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != len(docs) {
		t.Fatalf("batch returned %d results for %d documents", len(response.Results), len(docs))
	}
	return response.Results
}

func TestDocumentSubmissionValidation(t *testing.T) {
	tests := []struct {
		name string
		doc  document.Document
	}{
		{"missing URL", document.Document{Text: "note"}},
		{"invalid URL", document.Document{URL: "https://example.com/%", Text: "note"}},
		{"remote type requires remote URL", document.Document{URL: "https://example.com/note.txt", Text: "note", Type: document.RemoteFile}},
		{"remote URL requires remote type", document.Document{URL: "remote-file://client/tmp/note.txt", Text: "note"}},
		{"source host is required", document.Document{URL: "remote-file:///tmp/note.txt", Text: "note", Type: document.RemoteFile}},
		{"path is required", document.Document{URL: "remote-file://client/", Text: "note", Type: document.RemoteFile}},
		{"content is required", document.Document{URL: "remote-file://client/tmp/note.txt", Type: document.RemoteFile}},
		{"credentials are rejected", document.Document{URL: "remote-file://user:pass@client/tmp/note.txt", Text: "note", Type: document.RemoteFile}},
		{"query is rejected", document.Document{URL: "remote-file://client/tmp/note.txt?secret=value", Text: "note", Type: document.RemoteFile}},
		{"fragment is rejected", document.Document{URL: "remote-file://client/tmp/note.txt#part", Text: "note", Type: document.RemoteFile}},
	}
	for _, endpoint := range []string{"/api/add", "/api/batch"} {
		t.Run(endpoint, func(t *testing.T) {
			cfg := testutil.Config(t)
			idx := newServerTestIndexer(t, cfg)
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					// Client supplied processing state must not bypass validation.
					d := tt.doc
					d.Processed = true
					result := submitTestDocuments(t, webContext{Config: cfg, Indexer: idx}, endpoint, nil, d)[0]
					if result.Status != http.StatusBadRequest || result.Error == "" {
						t.Errorf("result = %+v, want a validation error with status 400", result)
					}
				})
			}
			if got := idx.Total(); got != 0 {
				t.Errorf("stored %d invalid documents", got)
			}
		})
	}
}

func TestDocumentSubmissionRechecksRemoteSensitiveContent(t *testing.T) {
	for _, endpoint := range []string{"/api/add", "/api/batch"} {
		t.Run(endpoint, func(t *testing.T) {
			cfg := testutil.Config(t)
			idx := newServerTestIndexer(t, cfg)
			d := document.Document{
				URL:       "remote-file://client/tmp/key.txt",
				Text:      "-----BEGIN OPENSSH PRIVATE KEY-----",
				Type:      document.RemoteFile,
				Processed: true,
			}
			c := webContext{Config: cfg, Indexer: idx}
			result := submitTestDocuments(t, c, endpoint, nil, d)[0]
			if result.Status != http.StatusUnprocessableEntity {
				t.Errorf("result = %+v, want status 422", result)
			}
			if idx.Total() != 0 {
				t.Error("sensitive document was indexed without an explicit override")
			}
			d.SkipSensitiveCheck = true
			result = submitTestDocuments(t, c, endpoint, nil, d)[0]
			if result.Status != http.StatusCreated || idx.GetByURLAndUser(d.URL, 0) == nil {
				t.Errorf("explicit sensitive content override was not honored: %+v", result)
			}
		})
	}
}

func TestDocumentSubmissionClearsRemoteStorageReferences(t *testing.T) {
	for _, endpoint := range []string{"/api/add", "/api/batch"} {
		t.Run(endpoint, func(t *testing.T) {
			cfg := testutil.Config(t)
			cfg.App.UserHandling = true
			idx := newServerTestIndexer(t, cfg)
			private := &document.Document{
				URL: "https://example.com/private", UserID: 99, Processed: true,
				HTML: "private archived content", Favicon: "data:image/png;base64,cHJpdmF0ZQ==",
			}
			if err := idx.Add(private); err != nil {
				t.Fatal(err)
			}
			d := document.Document{
				URL: "remote-file://workstation/tmp/note.txt", Type: document.RemoteFile,
				Title: "Remote note", Text: "Extracted body", Label: "notes", Updated: 1234,
				DocumentID: private.ID(), UserID: 99, Domain: "untrusted.example",
				HTMLKey: private.HTMLKey, FaviconKey: private.FaviconKey,
				AddCount: 100, Processed: true,
			}
			result := submitTestDocuments(t, webContext{Config: cfg, Indexer: idx, UserID: 1}, endpoint, nil, d)[0]
			if result.Status != http.StatusCreated {
				t.Fatalf("result = %+v, want status 201", result)
			}
			got := idx.GetByURLAndUser(d.URL, 1)
			if got == nil {
				t.Fatal("remote file was not indexed for the submitting user")
			}
			if got.HTML != "" || got.HTMLKey != "" || got.Favicon != "" || got.FaviconKey != "" {
				t.Errorf("remote snapshot retained another document's stored content: %+v", got)
			}
			if got.DocumentID != document.GetDocID(1, d.URL) || got.Domain != "workstation" || got.AddCount != 1 {
				t.Errorf("remote snapshot retained client supplied derived fields: %+v", got)
			}
			if got.Type != d.Type || got.Text != d.Text || got.Title != d.Title || got.Label != d.Label || got.Updated != d.Updated {
				t.Errorf("remote snapshot content or metadata changed: %+v", got)
			}
		})
	}
}

func TestDocumentSubmissionOwnership(t *testing.T) {
	tests := []struct {
		name    string
		admin   bool
		headers map[string]string
		owner   uint
	}{
		{name: "caller owns document", owner: 1},
		{name: "nonadmin cannot target another user", headers: map[string]string{"X-Hister-Target-User-ID": "2"}, owner: 1},
		{name: "admin targets another user", admin: true, headers: map[string]string{"X-Hister-Target-User-ID": "2"}, owner: 2},
		{name: "admin targets shared documents", admin: true, headers: map[string]string{"X-Hister-Target-User-ID": "0"}},
		{name: "invalid target keeps caller", admin: true, headers: map[string]string{"X-Hister-Target-User-ID": "invalid"}, owner: 1},
		{name: "public submission", headers: map[string]string{"X-Hister-Public": "true"}},
		{name: "public flag takes precedence", admin: true, headers: map[string]string{"X-Hister-Public": "true", "X-Hister-Target-User-ID": "2"}},
		{name: "false public flag", headers: map[string]string{"X-Hister-Public": "false"}, owner: 1},
	}
	for _, endpoint := range []string{"/api/add", "/api/batch"} {
		for _, tt := range tests {
			t.Run(endpoint+"/"+tt.name, func(t *testing.T) {
				cfg := testutil.Config(t)
				cfg.App.UserHandling = true
				idx := newServerTestIndexer(t, cfg)
				d := document.Document{URL: "remote-file://client/tmp/note.txt", Text: "note", Type: document.RemoteFile, UserID: 99, Processed: true}
				c := webContext{Config: cfg, Indexer: idx, UserID: 1, IsAdmin: tt.admin}
				result := submitTestDocuments(t, c, endpoint, tt.headers, d)[0]
				if result.Status != http.StatusCreated {
					t.Fatalf("result = %+v, want status 201", result)
				}
				if got := idx.GetByDocID(document.GetDocID(tt.owner, d.URL)); got == nil || got.UserID != tt.owner {
					t.Errorf("stored document = %+v, want owner %d", got, tt.owner)
				}
				if idx.Total() != 1 {
					t.Errorf("document count = %d, want 1", idx.Total())
				}
			})
		}
	}
}

func TestDocumentSubmissionRejectsOwnURLs(t *testing.T) {
	for _, endpoint := range []string{"/api/add", "/api/batch"} {
		t.Run(endpoint, func(t *testing.T) {
			cfg := testutil.Config(t)
			cfg.Server.BaseURL = "http://127.0.0.1:4433/hister"
			idx := newServerTestIndexer(t, cfg)
			for _, rawURL := range []string{"http://127.0.0.1:4433", "http://localhost:4433/elsewhere", "hister://search"} {
				d := document.Document{URL: rawURL, Text: "note", Processed: true}
				d.SetIgnoreSkipRules(true)
				result := submitTestDocuments(t, webContext{Config: cfg, Indexer: idx}, endpoint, nil, d)[0]
				if result.Status != http.StatusNotAcceptable {
					t.Errorf("URL %s: result = %+v, want status 406", rawURL, result)
				}
			}
			if idx.Total() != 0 {
				t.Error("indexed a URL belonging to this Hister server")
			}
		})
	}
}

func TestDocumentSubmissionBatchContinuesAfterRejections(t *testing.T) {
	cfg := testutil.Config(t)
	idx := newServerTestIndexer(t, cfg)
	docs := []document.Document{
		{URL: "remote-file://client/tmp/invalid.txt?query=1", Text: "invalid", Type: document.RemoteFile, Processed: true},
		{URL: "remote-file://client/tmp/first.txt", Text: "first", Type: document.RemoteFile, Processed: true},
		{URL: "remote-file://client/tmp/key.txt", Text: "-----BEGIN OPENSSH PRIVATE KEY-----", Type: document.RemoteFile, Processed: true},
		{URL: "remote-file://client/tmp/second.txt", Text: "second", Type: document.RemoteFile, Processed: true},
	}
	results := submitTestDocuments(t, webContext{Config: cfg, Indexer: idx}, "/api/batch", nil, docs...)
	for i, wantStatus := range []int{http.StatusBadRequest, http.StatusCreated, http.StatusUnprocessableEntity, http.StatusCreated} {
		if results[i].Status != wantStatus {
			t.Errorf("operation %d: result = %+v, want status %d", i, results[i], wantStatus)
		}
		stored := idx.GetByURLAndUser(docs[i].URL, 0)
		if (stored != nil) != (wantStatus == http.StatusCreated) {
			t.Errorf("operation %d: stored document = %+v", i, stored)
		}
	}
}
