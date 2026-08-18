// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/testutil"

	"github.com/blevesearch/bleve/v2"
)

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
	idx, err := initializeIndexer(cfg.FullPath(""), indexOptions{DetectLanguages: true, KeepStopwords: true})
	if err != nil {
		t.Fatalf("initialize indexer: %v", err)
	}
	version, fingerprint, err := idx.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if version != Version {
		t.Fatalf("version = %d, want %d", version, Version)
	}
	wantFingerprint := AnalyzerFingerprint(true, true)
	if fingerprint != wantFingerprint {
		t.Fatalf("fingerprint = %q, want %q", fingerprint, wantFingerprint)
	}

	if err := idx.SetMetadata(Version-1, "custom"); err != nil {
		t.Fatal(err)
	}
	idx.Close()

	idx, err = initializeIndexer(cfg.FullPath(""), indexOptions{DetectLanguages: true, KeepStopwords: true})
	if err != nil {
		t.Fatalf("reopen indexer: %v", err)
	}
	defer idx.Close()

	version, fingerprint, err = idx.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if version != Version-1 {
		t.Fatalf("reopened version = %d, want %d", version, Version-1)
	}
	if fingerprint != "custom" {
		t.Fatalf("reopened fingerprint = %q, want custom", fingerprint)
	}
}

func TestGetMetadataRejectsInvalidInternalValue(t *testing.T) {
	cfg := testutil.Config(t)
	idx, err := initializeIndexer(cfg.FullPath(""), indexOptions{DetectLanguages: false, KeepStopwords: false})
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
	idx, err := initializeIndexer(cfg.FullPath(""), indexOptions{DetectLanguages: true, KeepStopwords: false})
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
		Version:             Version - 1,
		AnalyzerFingerprint: fingerprint,
	}); err != nil {
		t.Fatal(err)
	}

	version, gotFingerprint, err := idx.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if version != Version-1 {
		t.Fatalf("version = %d, want oldest version %d", version, Version-1)
	}
	if gotFingerprint != fingerprint {
		t.Fatalf("fingerprint = %q, want %q", gotFingerprint, fingerprint)
	}

	if err := languageIndex.DeleteInternal([]byte(indexVersionKey)); err != nil {
		t.Fatal(err)
	}
	version, gotFingerprint, err = idx.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if version != -1 {
		t.Fatalf("version with incomplete subindex metadata = %d, want -1", version)
	}
	if gotFingerprint != fingerprint {
		t.Fatalf("fingerprint = %q, want %q", gotFingerprint, fingerprint)
	}

	if err := writeIndexMetadata(languageIndex, indexMetadata{
		Version:             Version,
		AnalyzerFingerprint: "different",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := idx.GetMetadata(); err == nil {
		t.Fatal("GetMetadata accepted inconsistent analyzer fingerprints")
	}
}

func TestLanguageIndexMetadataIsSelfContained(t *testing.T) {
	cfg := testutil.Config(t)
	idx, err := initializeIndexer(cfg.FullPath(""), indexOptions{DetectLanguages: true, KeepStopwords: true})
	if err != nil {
		t.Fatalf("initialize indexer: %v", err)
	}
	languageIndexName := indexNameForLanguage("en")
	if err := idx.addIndexer(languageIndexName, "en"); err != nil {
		idx.Close()
		t.Fatalf("add language index: %v", err)
	}
	wantMetadata := indexMetadata{Version: Version - 1, AnalyzerFingerprint: "custom"}
	if err := idx.SetMetadata(wantMetadata.Version, wantMetadata.AnalyzerFingerprint); err != nil {
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
	idx, err := initializeIndexer(cfg.FullPath(""), indexOptions{DetectLanguages: false, KeepStopwords: false})
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
	if err := idx.SetMetadata(Version-1, "old"); err != nil {
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

	version, fingerprint, err := idx.GetMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if version != Version {
		t.Fatalf("version = %d, want %d", version, Version)
	}
	wantFingerprint := AnalyzerFingerprint(true, true)
	if fingerprint != wantFingerprint {
		t.Fatalf("fingerprint = %q, want %q", fingerprint, wantFingerprint)
	}

	languageIndexName := indexNameForLanguage("en")
	if _, exists := idx.indexers[languageIndexName]; !exists {
		t.Fatalf("replacement language index %s was not created", languageIndexName)
	}
	wantMetadata := indexMetadata{Version: Version, AnalyzerFingerprint: wantFingerprint}
	for _, subIndex := range idx.indexers {
		requireIndexMetadata(t, subIndex, wantMetadata)
	}
}
