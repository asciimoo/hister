// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/testutil"
	"github.com/asciimoo/hister/server/vectorstore"

	"github.com/blevesearch/bleve/v2"
)

type metadataVectorStore struct {
	clearErr error
}

func (*metadataVectorStore) Init() error { return nil }

func (*metadataVectorStore) PutChunks(string, uint, []vectorstore.Chunk) error { return nil }

func (*metadataVectorStore) Delete(string) error { return nil }

func (*metadataVectorStore) Search([]float32, int, float64, uint) ([]vectorstore.Result, error) {
	return nil, nil
}

func (s *metadataVectorStore) Clear() error { return s.clearErr }

func (*metadataVectorStore) Close() error { return nil }

func requireIndexMetadata(t *testing.T, idx bleve.Index, want indexMetadata) {
	t.Helper()
	got, complete, err := readIndexMetadata(idx)
	if err != nil {
		t.Fatal(err)
	}
	if !complete {
		t.Fatalf("index metadata = %#v, want complete metadata", got)
	}
	if got != want {
		t.Fatalf("index metadata = %#v, want %#v", got, want)
	}
}

func TestIndexMetadataPersistence(t *testing.T) {
	cfg := testutil.Config(t)
	idx, err := initializeIndexer(cfg.FullPath(""), true, true, "embedding")
	if err != nil {
		t.Fatalf("initialize indexer: %v", err)
	}
	metadata, err := idx.getMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Version != Version {
		t.Fatalf("version = %d, want %d", metadata.Version, Version)
	}
	wantFingerprint := AnalyzerFingerprint(true, true)
	if metadata.AnalyzerFingerprint != wantFingerprint {
		t.Fatalf("fingerprint = %q, want %q", metadata.AnalyzerFingerprint, wantFingerprint)
	}
	if metadata.EmbeddingFingerprint != "embedding" {
		t.Fatalf("embedding fingerprint = %q, want embedding", metadata.EmbeddingFingerprint)
	}

	if err := idx.SetMetadata(Version-1, "custom"); err != nil {
		t.Fatal(err)
	}
	idx.Close()

	idx, err = initializeIndexer(cfg.FullPath(""), true, true, "different")
	if err != nil {
		t.Fatalf("reopen indexer: %v", err)
	}
	defer idx.Close()

	metadata, err = idx.getMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Version != Version-1 {
		t.Fatalf("reopened version = %d, want %d", metadata.Version, Version-1)
	}
	if metadata.AnalyzerFingerprint != "custom" {
		t.Fatalf("reopened fingerprint = %q, want custom", metadata.AnalyzerFingerprint)
	}
	if metadata.EmbeddingFingerprint != "embedding" {
		t.Fatalf("reopened embedding fingerprint = %q, want embedding", metadata.EmbeddingFingerprint)
	}
	storedEmbeddingFingerprint, err := idx.GetEmbeddingFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if storedEmbeddingFingerprint != "embedding" {
		t.Fatalf("GetEmbeddingFingerprint() = %q, want embedding", storedEmbeddingFingerprint)
	}
}

func TestBackfillEmbeddingFingerprint(t *testing.T) {
	cfg := testutil.Config(t)
	idx, err := initializeIndexer(cfg.FullPath(""), false, false, "")
	if err != nil {
		t.Fatalf("initialize indexer: %v", err)
	}
	defer idx.Close()

	if err := idx.save(&document.Document{
		URL:      "https://example.com/legacy",
		Title:    "Legacy document",
		Text:     "Document with embeddings created before fingerprint metadata",
		AddCount: 1,
	}); err != nil {
		t.Fatalf("save legacy document: %v", err)
	}
	if err := idx.backfillEmbeddingFingerprint("current"); err != nil {
		t.Fatal(err)
	}
	storedFingerprint, err := idx.GetEmbeddingFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if storedFingerprint != "current" {
		t.Fatalf("embedding fingerprint = %q, want current", storedFingerprint)
	}

	if err := idx.backfillEmbeddingFingerprint("changed"); err != nil {
		t.Fatal(err)
	}
	storedFingerprint, err = idx.GetEmbeddingFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if storedFingerprint != "current" {
		t.Fatalf("embedding fingerprint after second backfill = %q, want current", storedFingerprint)
	}
}

func TestGetMetadataRejectsInvalidInternalValue(t *testing.T) {
	cfg := testutil.Config(t)
	idx, err := initializeIndexer(cfg.FullPath(""), false, false, "embedding")
	if err != nil {
		t.Fatalf("initialize indexer: %v", err)
	}
	defer idx.Close()

	if err := idx.indexers[defaultIndexerName].DeleteInternal([]byte(indexVersionKey)); err != nil {
		t.Fatal(err)
	}
	version, fingerprint, err := idx.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if version != -1 {
		t.Fatalf("missing version = %d, want -1", version)
	}
	if fingerprint != AnalyzerFingerprint(false, false) {
		t.Fatalf("fingerprint = %q, want the stored fingerprint", fingerprint)
	}

	if err := idx.indexers[defaultIndexerName].SetInternal([]byte(indexVersionKey), []byte("invalid")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := idx.GetMetadata(); err == nil {
		t.Fatal("GetMetadata accepted an invalid internal value")
	}
}

func TestGetMetadataValidatesAllSubIndexes(t *testing.T) {
	cfg := testutil.Config(t)
	idx, err := initializeIndexer(cfg.FullPath(""), true, false, "embedding")
	if err != nil {
		t.Fatalf("initialize indexer: %v", err)
	}
	defer idx.Close()

	languageIndexName := indexNameForLanguage("en")
	if err := idx.addIndexer(languageIndexName, "en"); err != nil {
		t.Fatalf("add language index: %v", err)
	}
	languageIndex := idx.indexers[languageIndexName]
	fingerprint := AnalyzerFingerprint(true, false)
	if err := writeIndexMetadata(languageIndex, indexMetadata{
		Version:              Version - 1,
		AnalyzerFingerprint:  fingerprint,
		EmbeddingFingerprint: "embedding",
	}); err != nil {
		t.Fatal(err)
	}

	metadata, err := idx.getMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Version != Version-1 {
		t.Fatalf("version = %d, want oldest version %d", metadata.Version, Version-1)
	}
	if metadata.AnalyzerFingerprint != fingerprint {
		t.Fatalf("fingerprint = %q, want %q", metadata.AnalyzerFingerprint, fingerprint)
	}
	if metadata.EmbeddingFingerprint != "embedding" {
		t.Fatalf("embedding fingerprint = %q, want embedding", metadata.EmbeddingFingerprint)
	}

	if err := languageIndex.DeleteInternal([]byte(indexVersionKey)); err != nil {
		t.Fatal(err)
	}
	metadata, err = idx.getMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Version != -1 {
		t.Fatalf("version with incomplete subindex metadata = %d, want -1", metadata.Version)
	}
	if metadata.AnalyzerFingerprint != fingerprint {
		t.Fatalf("fingerprint = %q, want %q", metadata.AnalyzerFingerprint, fingerprint)
	}
	if metadata.EmbeddingFingerprint != "embedding" {
		t.Fatalf("embedding fingerprint = %q, want embedding", metadata.EmbeddingFingerprint)
	}

	if err := writeIndexMetadata(languageIndex, indexMetadata{
		Version:              Version,
		AnalyzerFingerprint:  fingerprint,
		EmbeddingFingerprint: "different",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := idx.GetMetadata(); err == nil {
		t.Fatal("GetMetadata accepted inconsistent embedding fingerprints")
	}

	if err := writeIndexMetadata(languageIndex, indexMetadata{
		Version:              Version,
		AnalyzerFingerprint:  "different",
		EmbeddingFingerprint: "embedding",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := idx.GetMetadata(); err == nil {
		t.Fatal("GetMetadata accepted inconsistent analyzer fingerprints")
	}
}

func TestLanguageIndexMetadataIsSelfContained(t *testing.T) {
	cfg := testutil.Config(t)
	idx, err := initializeIndexer(cfg.FullPath(""), true, true, "embedding")
	if err != nil {
		t.Fatalf("initialize indexer: %v", err)
	}
	languageIndexName := indexNameForLanguage("en")
	if err := idx.addIndexer(languageIndexName, "en"); err != nil {
		idx.Close()
		t.Fatalf("add language index: %v", err)
	}
	wantMetadata := indexMetadata{Version: Version - 1, AnalyzerFingerprint: "custom", EmbeddingFingerprint: "custom-embedding"}
	if err := idx.setMetadata(wantMetadata); err != nil {
		idx.Close()
		t.Fatal(err)
	}

	for _, subIndex := range idx.indexers {
		requireIndexMetadata(t, subIndex, wantMetadata)
	}
	idx.Close()

	copiedPath := filepath.Join(t.TempDir(), languageIndexName)
	if err := os.CopyFS(copiedPath, os.DirFS(filepath.Join(cfg.FullPath(""), languageIndexName))); err != nil {
		t.Fatalf("copy language index: %v", err)
	}
	copiedIndex, err := bleve.OpenUsing(copiedPath, bleveRuntimeConfig())
	if err != nil {
		t.Fatalf("open copied language index: %v", err)
	}
	defer func() {
		if err := copiedIndex.Close(); err != nil {
			t.Errorf("close copied language index: %v", err)
		}
	}()

	requireIndexMetadata(t, copiedIndex, wantMetadata)
}

func TestReindexStoresReplacementIndexMetadata(t *testing.T) {
	cfg := testutil.Config(t)
	idx, err := initializeIndexer(cfg.FullPath(""), false, false, "stored-embedding")
	if err != nil {
		t.Fatalf("initialize indexer: %v", err)
	}
	if err := idx.save(&document.Document{
		URL:      "https://example.com/english",
		Title:    "English document",
		Text:     "This is a deliberately long English document with enough common words for reliable language detection during the reindex operation.",
		Language: "en",
		AddCount: 1,
	}); err != nil {
		idx.Close()
		t.Fatalf("save English document: %v", err)
	}
	if err := idx.setMetadata(indexMetadata{
		Version:              Version - 1,
		AnalyzerFingerprint:  "old",
		EmbeddingFingerprint: "stored-embedding",
	}); err != nil {
		idx.Close()
		t.Fatal(err)
	}

	if err := idx.Reindex(
		&config.Rules{},
		false,
		true,
		true,
		nil,
	); err != nil {
		t.Fatalf("Reindex returned an error: %v", err)
	}
	defer idx.Close()

	metadata, err := idx.getMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Version != Version {
		t.Fatalf("version = %d, want %d", metadata.Version, Version)
	}
	wantFingerprint := AnalyzerFingerprint(true, true)
	if metadata.AnalyzerFingerprint != wantFingerprint {
		t.Fatalf("fingerprint = %q, want %q", metadata.AnalyzerFingerprint, wantFingerprint)
	}
	if metadata.EmbeddingFingerprint != "stored-embedding" {
		t.Fatalf("embedding fingerprint = %q, want stored-embedding", metadata.EmbeddingFingerprint)
	}

	languageIndexName := indexNameForLanguage("en")
	if _, exists := idx.indexers[languageIndexName]; !exists {
		t.Fatalf("replacement language index %s was not created", languageIndexName)
	}
	wantMetadata := indexMetadata{Version: Version, AnalyzerFingerprint: wantFingerprint, EmbeddingFingerprint: "stored-embedding"}
	for _, subIndex := range idx.indexers {
		requireIndexMetadata(t, subIndex, wantMetadata)
	}
}

func TestReindexStoresActiveEmbeddingFingerprint(t *testing.T) {
	cfg := testutil.Config(t)
	idx, err := initializeIndexer(cfg.FullPath(""), false, false, "stored-embedding")
	if err != nil {
		t.Fatalf("initialize indexer: %v", err)
	}
	semanticConfig := config.SemanticSearch{
		Enable:            true,
		EmbeddingEndpoint: "http://localhost:11434/v1/embeddings",
		EmbeddingModel:    "model",
		Dimensions:        768,
		MaxContextLength:  512,
		ChunkOverlap:      64,
		DocumentPrefix:    "document: ",
	}
	idx.semanticConfig = semanticConfig
	idx.embedder = vectorstore.NewEmbedder(&semanticConfig)
	idx.vectorStore = &metadataVectorStore{}

	if err := idx.Reindex(&config.Rules{}, false, false, false, nil); err != nil {
		idx.Close()
		t.Fatalf("Reindex returned an error: %v", err)
	}
	defer idx.Close()

	storedFingerprint, err := idx.GetEmbeddingFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	wantFingerprint := semanticConfig.EmbeddingFingerprint()
	if storedFingerprint != wantFingerprint {
		t.Fatalf("embedding fingerprint = %q, want %q", storedFingerprint, wantFingerprint)
	}
}

func TestReindexPreservesIndexWhenVectorRebuildFails(t *testing.T) {
	cfg := testutil.Config(t)
	idx, err := initializeIndexer(cfg.FullPath(""), false, false, "stored-embedding")
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	doc := &document.Document{URL: "https://example.com/rebuild-failure", Text: "original text", Processed: true}
	if err := idx.Add(doc); err != nil {
		t.Fatal(err)
	}
	idx.semanticConfig = config.SemanticSearch{Enable: true, Dimensions: 4}
	idx.embedder = vectorstore.NewEmbedder(&idx.semanticConfig)
	wantErr := errors.New("vector table rebuild failed")
	idx.vectorStore = &metadataVectorStore{clearErr: wantErr}
	if err := idx.Reindex(&config.Rules{}, false, false, false, nil); !errors.Is(err, wantErr) {
		t.Fatalf("Reindex error = %v, want %v", err, wantErr)
	}
	if idx.GetByURLAndUser(doc.URL, 0) == nil {
		t.Fatal("existing document was lost after failed vector rebuild")
	}
	fingerprint, err := idx.GetEmbeddingFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint != "stored-embedding" {
		t.Fatalf("embedding fingerprint changed after failed rebuild: %q", fingerprint)
	}
	if idx.reindexInProgress.Load() {
		t.Fatal("reindex remains in progress after failed vector rebuild")
	}
	if _, err := os.Stat(filepath.Join(cfg.App.Directory, "reindex")); !os.IsNotExist(err) {
		t.Fatalf("temporary reindex directory remains after failure: %v", err)
	}
}
