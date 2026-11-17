package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/server/testutil"
)

func TestBatchRequestAcceptsCompleteDocument(t *testing.T) {
	var req batchRequest
	data := []byte(`{"ops":[{"op":"add","url":"https://example.com","title":"Example","label":"reference","metadata":{"source":"export"}}]}`)
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatal(err)
	}
	if len(req.Ops) != 1 {
		t.Fatalf("operation count = %d, want 1", len(req.Ops))
	}
	op := req.Ops[0]
	if op.Op != batchOpAdd || op.URL != "https://example.com" || op.Title != "Example" {
		t.Fatalf("unexpected operation: %#v", op)
	}
	if op.Label != "reference" {
		t.Fatalf("label = %q, want reference", op.Label)
	}
	if op.Metadata["source"] != "export" {
		t.Fatalf("metadata source = %v, want export", op.Metadata["source"])
	}
}

func TestServeBatchReportsOversizedRequest(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.Server.MaxBatchBodySize = 1
	body := `{"ops":[{"op":"add","url":"https://example.com","text":"` +
		strings.Repeat("x", int(cfg.Server.MaxBatchBodyBytes())) + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/batch", strings.NewReader(body))
	rec := httptest.NewRecorder()

	serveBatch(&webContext{Request: req, Response: rec, Config: cfg})

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	var response batchResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if want := "request body exceeds the 1 MiB limit"; response.Error != want {
		t.Fatalf("error = %q, want %q", response.Error, want)
	}
	if response.Code != "request_too_large" {
		t.Fatalf("code = %q, want request_too_large", response.Code)
	}
	if response.LimitBytes != 1<<20 {
		t.Fatalf("limit_bytes = %d, want %d", response.LimitBytes, 1<<20)
	}
}

func TestServeBatchReportsInvalidJSON(t *testing.T) {
	cfg := testutil.Config(t)
	req := httptest.NewRequest(http.MethodPost, "/api/batch", strings.NewReader(`{"ops":`))
	rec := httptest.NewRecorder()

	serveBatch(&webContext{Request: req, Response: rec, Config: cfg})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var response batchResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error != "invalid JSON" {
		t.Fatalf("error = %q, want %q", response.Error, "invalid JSON")
	}
}

func TestServeConfigAdvertisesBatchBodyLimit(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.Server.MaxBatchBodySize = 12
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rec := httptest.NewRecorder()

	serveConfig(&webContext{Request: req, Response: rec, Config: cfg})

	var response struct {
		MaxBatchBodyBytes int64 `json:"maxBatchBodyBytes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.MaxBatchBodyBytes != 12<<20 {
		t.Fatalf("maxBatchBodyBytes = %d, want %d", response.MaxBatchBodyBytes, 12<<20)
	}
}
