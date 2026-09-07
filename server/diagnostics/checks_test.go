package diagnostics

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/indexer"
)

type testMetadata struct {
	version             int
	analyzer, embedding string
	err                 error
}

func (m testMetadata) GetMetadata() (int, string, error)        { return m.version, m.analyzer, m.err }
func (m testMetadata) GetEmbeddingFingerprint() (string, error) { return m.embedding, m.err }

func TestIndexCompatibility(t *testing.T) {
	c := config.CreateDefaultConfig()
	c.SemanticSearch.Enable = true
	matching := testMetadata{version: indexer.Version, analyzer: indexer.AnalyzerFingerprint(c.Indexer.DetectLanguages, c.Indexer.KeepStopwords), embedding: c.SemanticSearch.EmbeddingFingerprint()}
	for _, tc := range []struct {
		name   string
		change func(*testMetadata)
		failed string
	}{
		{"matching", func(*testMetadata) {}, ""},
		{"old version", func(m *testMetadata) { m.version-- }, "index.version"},
		{"changed analyzer", func(m *testMetadata) { m.analyzer = "different" }, "index.analyzer"},
		{"missing embeddings", func(m *testMetadata) { m.embedding = "" }, "index.embeddings"},
		{"metadata read failure", func(m *testMetadata) { m.err = errors.New("private details") }, "index.metadata"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadata := matching
			tc.change(&metadata)
			found := ""
			for _, check := range Index(c, metadata) {
				if check.Status == "error" {
					found = check.Name
				}
			}
			if found != tc.failed {
				t.Fatalf("failed check=%s want %s", found, tc.failed)
			}
		})
	}
}

func TestExtractorValidationAndDependencies(t *testing.T) {
	c := config.CreateDefaultConfig()
	c.Extractors = map[string]*config.Extractor{"typo": {Enable: true}}
	if _, err := Extractors(c); err == nil {
		t.Fatal("unknown extractor was accepted")
	}
	c.Extractors = map[string]*config.Extractor{"ytdlp": {Enable: true, Options: map[string]any{"typo": true}}}
	if _, err := Extractors(c); err == nil {
		t.Fatal("unknown option was accepted")
	}
	// Using the test executable proves availability without executing it.
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		enabled        bool
		binary, status string
	}{
		{true, binary, "ok"},
		{true, filepath.Join(t.TempDir(), "missing-binary"), "error"},
		{false, filepath.Join(t.TempDir(), "missing-binary"), "ok"},
	} {
		c.Extractors = map[string]*config.Extractor{"ytdlp": {Enable: tc.enabled, Options: map[string]any{"binary": tc.binary}}}
		r, err := Extractors(c)
		if err != nil {
			t.Fatal(err)
		}
		checks := Dependencies(r)
		if len(checks) != 1 || checks[0].Status != tc.status {
			t.Fatalf("enabled=%v checks=%v", tc.enabled, checks)
		}
	}
}
