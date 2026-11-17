package indexer

import (
	"encoding/json"
	"testing"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"
	servertypes "github.com/asciimoo/nogon/server/types"

	"github.com/blevesearch/bleve/v2"
)

func requireLanguageWithoutMetadata(t *testing.T, idx *Indexer, url, language string) *document.Document {
	t.Helper()
	d := idx.GetByURLAndUser(url, 0)
	if d == nil || d.Language != language || len(d.Metadata) != 0 {
		t.Fatalf("document = %+v, want language %s and no metadata", d, language)
	}
	return d
}

func TestLanguageResolutionDoesNotPopulateStoredMetadata(t *testing.T) {
	cfg := testutil.Config(t)
	idx := newTestIndexer(t, cfg)
	t.Cleanup(func() { idx.Close() })
	const url = "https://example.com/evidence"
	if err := idx.Add(&document.Document{URL: url, Text: overrideTestEnglish}); err != nil {
		t.Fatal(err)
	}
	d := requireLanguageWithoutMetadata(t, idx, url, "en")

	idx.Close()
	idx = newTestIndexer(t, cfg)
	requireLanguageWithoutMetadata(t, idx, url, "en")
	// Export and import must not add language diagnostics either.
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var imported document.Document
	if err := json.Unmarshal(data, &imported); err != nil {
		t.Fatal(err)
	}
	imported.URL = "https://example.com/imported-evidence"
	imported.Processed = true
	batch := idx.NewMultiBatch()
	if err := batch.Add(&imported); err != nil {
		t.Fatal(err)
	}
	if err := batch.Save(); err != nil {
		t.Fatal(err)
	}
	requireLanguageWithoutMetadata(t, idx, imported.URL, "en")
	if _, err := idx.UpdateByQuery("url:"+url, nil, servertypes.DocumentChanges{Language: new("de")}, false); err != nil {
		t.Fatal(err)
	}
	requireLanguageWithoutMetadata(t, idx, url, "de")
	if err := idx.Add(&document.Document{URL: url, Text: overrideTestEnglish}); err != nil {
		t.Fatal(err)
	}
	requireLanguageWithoutMetadata(t, idx, url, "de")

	if err := idx.Reindex(&config.Rules{}, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	requireLanguageWithoutMetadata(t, idx, url, "de")
	if _, err := idx.UpdateByQuery("url:"+url, nil, servertypes.DocumentChanges{Language: new("auto")}, false); err != nil {
		t.Fatal(err)
	}
	requireLanguageWithoutMetadata(t, idx, url, document.UnknownLanguage)
}

func TestAutoWithoutLanguageChangeIsUnchanged(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	const url = "https://example.com/old-evidence"
	// Rechecking an unpinned document with the same result is a no-op.
	old := &document.Document{URL: url, Language: "en", Text: overrideTestEnglish}
	if err := idx.getOrCreate("en").Index(old.ID(), old); err != nil {
		t.Fatal(err)
	}
	for _, dry := range []bool{true, false} {
		res, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Language: new("auto")}, dry)
		if err != nil || res.Updated != 0 || res.Unchanged != 1 {
			t.Fatalf("dry=%v: update=%+v, error=%v", dry, res, err)
		}
	}
	requireLanguageWithoutMetadata(t, idx, url, "en")
	res, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Language: new("auto")}, false)
	if err != nil || res.Updated != 0 {
		t.Fatalf("repeated update=%+v, error=%v", res, err)
	}
	q := bleve.NewSearchRequest(bleve.NewDocIDQuery([]string{old.ID()}))
	if res, err := idx.idx.Search(q); err != nil || res.Total != 1 {
		t.Fatalf("search=%+v, error=%v", res, err)
	}
}

func TestTitleUpdatesRefreshFallbackLanguage(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	const url = "https://example.com/title-language"
	if err := idx.Add(&document.Document{URL: url, Text: "123", Title: overrideTestEnglish}); err != nil {
		t.Fatal(err)
	}
	requireLanguageWithoutMetadata(t, idx, url, "en")
	for _, tc := range []struct{ title, language string }{
		{"Eine Suchmaschine organisiert gespeicherte Artikel, damit Leser wichtige Informationen später wiederfinden können. Diese Anleitung erklärt die Suche in einer persönlichen Bibliothek.", "de"},
		{"", document.UnknownLanguage},
		{overrideTestEnglish, "en"},
	} {
		if _, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Title: &tc.title}, false); err != nil {
			t.Fatal(err)
		}
		requireLanguageWithoutMetadata(t, idx, url, tc.language)
	}
	if _, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Language: new("de")}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Title: new("")}, false); err != nil {
		t.Fatal(err)
	}
	requireLanguageWithoutMetadata(t, idx, url, "de")
}

func TestLanguageRechecksPreserveHTMLHint(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	const url = "https://example.com/regional-language"
	if err := idx.Add(&document.Document{URL: url, HTML: `<html lang="en-US"><head><title>Search guide</title></head><body><p>` + overrideTestEnglish + `</p></body></html>`}); err != nil {
		t.Fatal(err)
	}
	for _, changes := range []servertypes.DocumentChanges{
		{Title: new("Eine deutsche Überschrift")},
		{Language: new("auto")},
	} {
		if _, err := idx.UpdateByQuery("*", nil, changes, false); err != nil {
			t.Fatal(err)
		}
		d := idx.GetByURLAndUser(url, 0)
		if d == nil || d.Language != "en-US" {
			t.Fatalf("document = %+v, want language en-US", d)
		}
	}
}
