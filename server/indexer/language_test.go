package indexer

import (
	"fmt"
	"slices"
	"testing"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"
)

func TestDisabledLanguageDetectionKeepsExistingDocumentsSearchable(t *testing.T) {
	cfg := testutil.Config(t)
	idx := newTestIndexer(t, cfg)
	t.Cleanup(func() { idx.Close() })
	const (
		englishURL = "https://example.com/english"
		frenchURL  = "https://example.com/french"
		unknownURL = "https://example.com/unknown"
		newURL     = "https://example.com/new"
	)
	for _, d := range []*document.Document{
		{URL: englishURL, Text: "running", Language: "en", Processed: true},
		{URL: frenchURL, Text: "bonjour", Language: "fr", Processed: true},
		{URL: unknownURL, Text: "granite", Language: document.UnknownLanguage, Processed: true},
	} {
		if err := idx.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	idx.Close()

	cfg.Indexer.DetectLanguages = false
	idx = newTestIndexer(t, cfg)
	if err := idx.Add(&document.Document{URL: newURL, Text: "running"}); err != nil {
		t.Fatal(err)
	}

	assertSearch := func(query string, want ...string) {
		t.Helper()
		res, err := idx.Search(&Query{Text: query})
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		var got []string
		for _, d := range res.Documents {
			got = append(got, d.URL)
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("search %q = %v, want %v", query, got, want)
		}
	}

	// Existing documents retain their language and analyzer. New documents use
	// the default analyzer, which does not stem "running" to "run".
	assertSearch("*", englishURL, frenchURL, unknownURL, newURL)
	assertSearch("language:en", englishURL)
	assertSearch("language:fr", frenchURL)
	assertSearch("language:unknown", unknownURL, newURL)
	assertSearch("text:run", englishURL)
	assertSearch("text:running", englishURL, newURL)
	if d := idx.GetByURLAndUser(englishURL, 0); d == nil || d.Language != "en" {
		t.Fatalf("existing document lookup = %+v, want English document", d)
	}

	// Reprocessing an existing document moves it into the default index and
	// removes its old language copy.
	if err := idx.Add(&document.Document{URL: englishURL, Text: "running"}); err != nil {
		t.Fatal(err)
	}
	assertSearch("*", englishURL, frenchURL, unknownURL, newURL)
	assertSearch("language:en")
	assertSearch("text:run")
	assertSearch("language:unknown", englishURL, unknownURL, newURL)

	// A full reindex can still consolidate the remaining language documents.
	if err := idx.Reindex(&config.Rules{}, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	assertSearch("language:unknown", englishURL, frenchURL, unknownURL, newURL)
	assertSearch("language:fr")
	assertSearch("text:bonjour", frenchURL)
}

func TestRestrictedLanguagesKeepExistingIndexesSearchable(t *testing.T) {
	cfg := testutil.Config(t)
	idx := newTestIndexer(t, cfg)
	d := &document.Document{
		URL: "https://example.com/french", Text: "Un document français.",
		Language: "fr", Processed: true,
	}
	if err := idx.Add(d); err != nil {
		idx.Close()
		t.Fatal(err)
	}
	idx.Close()

	cfg.Indexer.Languages = []string{"en", "de"}
	cfg.Indexer.LanguageDetectionAccuracy = "low"
	idx = newTestIndexer(t, cfg)
	defer idx.Close()
	res, err := idx.Search(&Query{Text: "language:fr"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Documents) != 1 || res.Documents[0].URL != d.URL {
		t.Fatalf("existing French document disappeared from search: %+v", res.Documents)
	}
}

func TestRestrictedLanguageSettingsSurviveReindex(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.Indexer.Languages = []string{"en", "de", "nl", "id"}
	cfg.Indexer.LanguageDetectionAccuracy = "low"
	idx := newTestIndexer(t, cfg)
	defer idx.Close()

	// Confident prose uses the English index; a short ambiguous word uses the
	// default index. Both decisions must survive a rebuild with the same settings.
	for round := range 2 {
		for text, want := range map[string]string{overrideTestEnglish: "en", "bonjour": document.UnknownLanguage} {
			d := &document.Document{
				URL: fmt.Sprintf("https://example.com/%d/%s", round, want), Text: text,
			}
			if err := idx.Add(d); err != nil {
				t.Fatal(err)
			}
			if d.Language != want {
				t.Errorf("round %d: %q detected as %q, want %q", round, text, d.Language, want)
			}
		}
		if round == 0 {
			if err := idx.Reindex(&config.Rules{}, false, true, false, nil); err != nil {
				t.Fatal(err)
			}
			for _, language := range []string{"en", document.UnknownLanguage} {
				url := "https://example.com/0/" + language
				stored := idx.GetByURLAndUser(url, 0)
				if stored == nil || stored.Language != language {
					t.Fatalf("reindexed %s: document = %+v", url, stored)
				}
			}
		}
	}
	for language, name := range map[string]string{"en": indexNameForLanguage("en"), document.UnknownLanguage: defaultIndexerName} {
		count, err := idx.indexers[name].DocCount()
		if err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Errorf("%s index count = %d, want 2", language, count)
		}
	}
}
