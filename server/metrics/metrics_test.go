// SPDX-License-Identifier: AGPL-3.0-or-later

package metrics

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mockGaugeSource struct {
	total   uint64
	dataDir string
}

func (m *mockGaugeSource) Total() uint64 {
	return m.total
}

func (m *mockGaugeSource) DataDir() string {
	return m.dataDir
}

func TestMetricsNewAndHandler(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	content := []byte("hello metrics")
	if err := os.WriteFile(testFile, content, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	mockSrc := &mockGaugeSource{
		total:   42,
		dataDir: tempDir,
	}

	ctx := t.Context()
	m := New(ctx, mockSrc)
	defer m.Stop()

	// Record some sample metrics
	m.QueriesTotal.WithLabelValues("hit").Inc()
	m.SearchDuration.Observe(0.123)
	m.DocumentsIndexedTotal.WithLabelValues("web").Inc()
	m.IndexingDuration.Observe(0.045)

	// Force gauge refresh
	m.refreshGauges()

	handler := m.Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d", rec.Code)
	}

	body := rec.Body.String()
	t.Logf("Metrics output snippet:\n%s", body[:min(500, len(body))])

	expectedStrings := []string{
		"hister_queries_total",
		"hister_search_duration_seconds",
		"hister_documents_indexed_total",
		"hister_indexing_duration_seconds",
		"hister_datastore_size_bytes",
		"hister_index_document_count",
	}

	for _, str := range expectedStrings {
		if !strings.Contains(body, str) {
			t.Errorf("expected metrics response to contain %q", str)
		}
	}
}

func TestStopCancelsContext(t *testing.T) {
	ctx := t.Context()
	m := New(ctx, &mockGaugeSource{total: 1, dataDir: t.TempDir()})
	m.Stop()
	// Calling Stop again should be safe (cancel is idempotent)
	m.Stop()
}

func TestDirSize(t *testing.T) {
	tempDir := t.TempDir()
	f1 := filepath.Join(tempDir, "file1.bin")
	f2 := filepath.Join(tempDir, "file2.bin")

	_ = os.WriteFile(f1, []byte("12345"), 0o644)
	_ = os.WriteFile(f2, []byte("1234567890"), 0o644)

	size, err := dirSize(tempDir)
	if err != nil {
		t.Fatalf("dirSize failed: %v", err)
	}
	if size != 15 {
		t.Errorf("expected dirSize 15, got %d", size)
	}
}

func TestDirSizeReturnsError(t *testing.T) {
	_, err := dirSize("/nonexistent/path/that/should/fail")
	if err == nil {
		t.Error("expected dirSize to return an error for a nonexistent path")
	}
}
