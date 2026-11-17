// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/files"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"
	servertypes "github.com/asciimoo/nogon/server/types"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

const overrideTestEnglish = "This document explains how a search engine stores web pages and helps people find useful information. Readers can search through their saved documents and return to the original pages whenever they need more details."

func requireStoredLanguage(t *testing.T, idx *Indexer, url string, userID uint, want string) {
	t.Helper()
	d := idx.GetByURLAndUser(url, userID)
	if d == nil {
		t.Fatalf("document %q for user %d is missing", url, userID)
	}
	if d.Language != want {
		t.Fatalf("document %q for user %d has language %q, want %q", url, userID, d.Language, want)
	}
}

func TestManualLanguageWorksWithExistingIndexMapping(t *testing.T) {
	cfg := testutil.Config(t)
	// Older indexes have no explicit mapping for the override field.
	legacyMapping := createMapping("default", false).(*mapping.IndexMappingImpl)
	delete(legacyMapping.DefaultMapping.Properties, "language_override")
	legacy, err := bleve.NewUsing(filepath.Join(cfg.App.Directory, defaultIndexerName), legacyMapping,
		bleve.Config.DefaultIndexType, bleve.Config.DefaultMemKVStore, bleveRuntimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := writeIndexMetadata(legacy, configuredIndexMetadata(true, false, "")); err != nil {
		t.Fatal(errors.Join(err, legacy.Close()))
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	idx := newTestIndexer(t, cfg)
	t.Cleanup(func() { idx.Close() })
	const url = "https://example.com/legacy-pin"
	addUpdateTestDocument(t, idx, &document.Document{URL: url, Text: overrideTestEnglish, Language: document.UnknownLanguage})
	if _, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Language: new(document.UnknownLanguage)}, false); err != nil {
		t.Fatal(err)
	}
	idx.Close()
	idx = newTestIndexer(t, cfg)
	if err := idx.Add(&document.Document{URL: url, Text: overrideTestEnglish}); err != nil {
		t.Fatal(err)
	}
	requireStoredLanguage(t, idx, url, 0, document.UnknownLanguage)
}

func TestManualLanguageSurvivesReprocessing(t *testing.T) {
	for _, language := range []string{"de", document.UnknownLanguage} {
		for _, operation := range []string{"resubmit", "batch", "reindex", "disable detection"} {
			t.Run(language+"/"+operation, func(t *testing.T) {
				cfg := testutil.Config(t)
				idx := newTestIndexer(t, cfg)
				t.Cleanup(func() { idx.Close() })
				const url = "https://example.com/manual-language"
				addUpdateTestDocument(t, idx, &document.Document{URL: url, Text: overrideTestEnglish, Language: "en"})
				if _, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Language: &language}, false); err != nil {
					t.Fatal(err)
				}
				idx.Close()
				idx = newTestIndexer(t, cfg)
				requireStoredLanguage(t, idx, url, 0, language)
				switch operation {
				case "resubmit":
					if err := idx.Add(&document.Document{URL: url + "?utm_source=browser", Text: overrideTestEnglish}); err != nil {
						t.Fatal(err)
					}
				case "batch":
					batch := idx.NewMultiBatch()
					if err := batch.Add(&document.Document{URL: url, Text: overrideTestEnglish, Language: "en", LanguageOverride: "en", Processed: true}); err != nil {
						t.Fatal(err)
					}
					if err := batch.Save(); err != nil {
						t.Fatal(err)
					}
				default:
					if err := idx.Reindex(&config.Rules{}, false, operation == "reindex", false, nil); err != nil {
						t.Fatal(err)
					}
				}
				requireStoredLanguage(t, idx, url, 0, language)
				res, err := idx.Search(&Query{Text: "*"})
				if err != nil || len(res.Documents) != 1 {
					t.Fatalf("search after %s = %+v, error = %v; want one document", operation, res, err)
				}
			})
		}
	}
}

func TestManualLanguagePinsUnchangedLanguage(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	const url = "https://example.com/pin"
	addUpdateTestDocument(t, idx, &document.Document{URL: url, Text: overrideTestEnglish, Language: "en"})
	for round, wantUpdated := range []int{1, 0} {
		result, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Language: new("en")}, false)
		if err != nil || result.Updated != wantUpdated {
			t.Fatalf("pin round %d = %+v, error = %v; want %d updated", round, result, err, wantUpdated)
		}
	}
	if err := idx.Reindex(&config.Rules{}, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	requireStoredLanguage(t, idx, url, 0, "en")
}

func TestManualLanguageCanBeCleared(t *testing.T) {
	for _, detect := range []bool{true, false} {
		cfg := testutil.Config(t)
		cfg.Indexer.DetectLanguages = detect
		idx := newTestIndexer(t, cfg)
		t.Cleanup(idx.Close)
		const url = "https://example.com/clear"
		addUpdateTestDocument(t, idx, &document.Document{URL: url, Text: overrideTestEnglish, Language: "en"})
		if _, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Language: new("de")}, false); err != nil {
			t.Fatal(err)
		}
		changes := servertypes.DocumentChanges{Language: new(" AUTO ")}
		result, err := idx.UpdateByQuery("*", nil, changes, true)
		if err != nil || result.Updated != 1 {
			t.Fatalf("clear preview = %+v, error = %v", result, err)
		}
		requireStoredLanguage(t, idx, url, 0, "de")
		if _, err := idx.UpdateByQuery("*", nil, changes, false); err != nil {
			t.Fatal(err)
		}
		want := document.UnknownLanguage
		if detect {
			want = "en"
		}
		requireStoredLanguage(t, idx, url, 0, want)
		result, err = idx.UpdateByQuery("*", nil, changes, false)
		if err != nil || result.Updated != 0 || result.Unchanged != 1 {
			t.Fatalf("repeat clear = %+v, error = %v", result, err)
		}
		if err := idx.Add(&document.Document{URL: url, Text: "12345"}); err != nil {
			t.Fatal(err)
		}
		requireStoredLanguage(t, idx, url, 0, document.UnknownLanguage)
	}
}

func TestManualLanguageIsScopedToDocumentOwner(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	const url = "https://example.com/shared-url"
	for _, userID := range []uint{0, 7, 8} {
		addUpdateTestDocument(t, idx, &document.Document{URL: url, UserID: userID, Text: overrideTestEnglish, Language: "en"})
	}
	if _, err := idx.UpdateByQuery("*", new(uint(7)), servertypes.DocumentChanges{Language: new("de")}, false); err != nil {
		t.Fatal(err)
	}
	for _, userID := range []uint{0, 7, 8} {
		if err := idx.Add(&document.Document{URL: url, UserID: userID, Text: overrideTestEnglish}); err != nil {
			t.Fatal(err)
		}
		want := "en"
		if userID == 7 {
			want = "de"
		}
		requireStoredLanguage(t, idx, url, userID, want)
	}
}

func TestManualLanguageSurvivesLocalFileReload(t *testing.T) {
	dir := t.TempDir()
	path := testutil.WriteFile(t, dir, "note.md", []byte(overrideTestEnglish))
	cfg := testutil.Config(t)
	cfg.Indexer.Directories = []*config.Directory{{Path: dir}}
	idx := newTestIndexer(t, cfg)
	defer idx.Close()
	if err := idx.IndexFile(path, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Language: new("de")}, false); err != nil {
		t.Fatal(err)
	}
	if err := idx.Reindex(&config.Rules{}, false, true, false, cfg.Indexer.Directories); err != nil {
		t.Fatal(err)
	}
	requireStoredLanguage(t, idx, files.PathToFileURL(path), 0, "de")
	if err := idx.IndexFile(path, 0); err != nil {
		t.Fatal(err)
	}
	requireStoredLanguage(t, idx, files.PathToFileURL(path), 0, "de")
}

func TestImportedLanguageOverride(t *testing.T) {
	for _, language := range []string{" DE ", "unknown", "../en", "zz", "auto"} {
		t.Run(language, func(t *testing.T) {
			idx := newTestIndexer(t, testutil.Config(t))
			defer idx.Close()
			data, err := json.Marshal(map[string]any{
				"url": "https://example.com/import", "text": overrideTestEnglish,
				"language": "en", "language_override": language, "processed": true,
			})
			if err != nil {
				t.Fatal(err)
			}
			var d document.Document
			if err := json.Unmarshal(data, &d); err != nil {
				t.Fatal(err)
			}
			err = idx.Add(&d)
			switch language {
			case " DE ", "unknown":
				if err != nil {
					t.Fatal(err)
				}
				want := "de"
				if language == "unknown" {
					want = "unknown"
				}
				requireStoredLanguage(t, idx, d.URL, 0, want)
			default:
				if !errors.Is(err, ErrInvalidLanguage) {
					t.Fatalf("import override %q error = %v, want invalid language", language, err)
				}
				if idx.GetByDocID(d.URL) != nil {
					t.Fatal("invalid document was stored")
				}
				// An existing valid pin must not mask malformed imported input.
				if err := idx.Add(&document.Document{URL: d.URL, Text: overrideTestEnglish, LanguageOverride: "de"}); err != nil {
					t.Fatal(err)
				}
				if err := idx.Add(&d); !errors.Is(err, ErrInvalidLanguage) {
					t.Fatalf("invalid override on a pinned document: %v, want invalid language", err)
				}
			}
		})
	}
}
