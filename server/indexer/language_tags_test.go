// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"testing"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"
	servertypes "github.com/asciimoo/nogon/server/types"

	"github.com/blevesearch/bleve/v2"
)

func TestLanguageTagsPreserveIdentityAndSelectAnalyzer(t *testing.T) {
	for _, tc := range []struct{ input, tag, index, search string }{
		{" EN-us ", "en-US", "index_en.db", "text:run"},
		{"en-GB", "en-GB", "index_en.db", "text:run"},
		{"nb-NO", "nb-NO", "index_no.db", "language:no"},
		{"no", "nb", "index_no.db", "language:nb"},
		{"in", "id", "index.db", "language:id"},
		{"uk-UA", "uk-UA", "index.db", "language:uk"},
		{"zh-hant-TW", "zh-Hant-TW", "index_zh.db", "language:zh"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			cfg := testutil.Config(t)
			cfg.Indexer.DetectLanguages = false
			idx := newTestIndexer(t, cfg)
			t.Cleanup(func() { idx.Close() })
			const url = "https://example.com/tag"
			if err := idx.Add(&document.Document{URL: url, Text: "running", LanguageOverride: tc.input}); err != nil {
				t.Fatal(err)
			}
			requireStoredLanguage(t, idx, url, 0, tc.tag)
			physical, ok := idx.indexByName(tc.index)
			if !ok {
				t.Fatalf("missing index %s", tc.index)
			}
			if count, err := physical.DocCount(); err != nil || count != 1 {
				t.Fatalf("index %s count = %d, error = %v", tc.index, count, err)
			}
			res, err := idx.Search(&Query{Text: tc.search})
			if err != nil || len(res.Documents) != 1 {
				t.Fatalf("search = %+v, error = %v", res, err)
			}
			idx.Close()
			idx = newTestIndexer(t, cfg)
			requireStoredLanguage(t, idx, url, 0, tc.tag)
			if _, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Language: new("de-DE")}, false); err != nil {
				t.Fatal(err)
			}
			requireStoredLanguage(t, idx, url, 0, "de-DE")
		})
	}
}

func TestRegionalLanguageSurvivesBatchAndReindex(t *testing.T) {
	cfg := testutil.Config(t)
	idx := newTestIndexer(t, cfg)
	defer idx.Close()
	batch := idx.NewMultiBatch()
	for _, tag := range []string{"en-US", "en-GB"} {
		if err := batch.Add(&document.Document{URL: "https://example.com/" + tag, Text: "running", LanguageOverride: tag}); err != nil {
			t.Fatal(err)
		}
	}
	if err := batch.Save(); err != nil {
		t.Fatal(err)
	}
	if err := idx.Reindex(&config.Rules{}, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"en-US", "en-GB"} {
		requireStoredLanguage(t, idx, "https://example.com/"+tag, 0, tag)
	}
	res, err := idx.Search(&Query{Text: "language:en text:run"})
	if err != nil || len(res.Documents) != 2 {
		t.Fatalf("search = %+v, error = %v", res, err)
	}
	if len(idx.indexes()) != 2 {
		t.Fatalf("got %d indexes, want default and English", len(idx.indexes()))
	}
}

func TestLegacyNorwegianIndexRemainsSearchable(t *testing.T) {
	cfg := testutil.Config(t)
	idx := newTestIndexer(t, cfg)
	t.Cleanup(func() { idx.Close() })
	const url = "https://example.com/legacy-norwegian"
	legacy := &document.Document{URL: url, Language: "no", LanguageOverride: "no", Text: "legacy content"}
	if err := idx.getOrCreate("nb").Index(legacy.ID(), legacy); err != nil {
		t.Fatal(err)
	}
	// Older releases also accepted the analyzer family as a manual label.
	if err := idx.addIndexer("index_cjk.db", "zh"); err != nil {
		t.Fatal(err)
	}
	cjkDoc := &document.Document{URL: "https://example.com/legacy-cjk", Language: "cjk", Text: "legacy content"}
	if err := idx.indexers["index_cjk.db"].Index(cjkDoc.ID(), cjkDoc); err != nil {
		t.Fatal(err)
	}
	idx.Close()
	idx = newTestIndexer(t, cfg)
	for _, query := range []string{"language:nb", "language:no", "language:cjk"} {
		res, err := idx.Search(&Query{Text: query})
		if err != nil || len(res.Documents) != 1 {
			t.Fatalf("%s: search = %+v, error = %v", query, res, err)
		}
	}
	if err := idx.Add(&document.Document{URL: url, Text: overrideTestEnglish}); err != nil {
		t.Fatal(err)
	}
	requireStoredLanguage(t, idx, url, 0, "nb")
	res, err := idx.idx.Search(bleve.NewSearchRequest(bleve.NewDocIDQuery([]string{legacy.ID()})))
	if err != nil || res.Total != 1 {
		t.Fatalf("resubmission = %+v, error = %v; want one copy", res, err)
	}
}

func TestProcessedLanguageIsNormalized(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	for input, want := range map[string]string{"EN-us": "en-US", "no": "nb", "../en": "unknown"} {
		d := &document.Document{URL: "https://example.com/" + want, Text: "running", Language: input, Processed: true}
		if err := idx.Add(d); err != nil {
			t.Fatal(err)
		}
		stored := idx.GetByURLAndUser(d.URL, 0)
		if stored == nil || stored.Language != want || stored.LanguageOverride != "" {
			t.Fatalf("stored = %+v, want unpinned %s", stored, want)
		}
	}
}
