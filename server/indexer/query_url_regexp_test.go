package indexer

import (
	"testing"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"
	servertypes "github.com/asciimoo/nogon/server/types"
)

func TestIndexedURLRegexpOwnershipAndMutations(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	docs := []*document.Document{
		{URL: "https://example.com/private/one", Title: "privacy guide", UserID: 1, Language: "en", Updated: 20},
		{URL: "https://example.com/private/two", Title: "privacy guide", UserID: 1, Language: "de", Updated: 10},
		{URL: "https://example.com/private/archive", Title: "privacy guide", UserID: 1, Language: "en", Updated: 30},
		{URL: "https://example.com/private/one", Title: "privacy guide", UserID: 2, Language: "en", Updated: 100},
		{URL: "https://example.com/private/global", Title: "privacy guide", Language: "de", Updated: 5},
		{URL: "https://other.test/public", Title: "privacy guide", UserID: 1, Updated: 40},
	}
	for _, doc := range docs {
		addUpdateTestDocument(t, idx, doc)
	}
	const input = `url_re:"^https://example\.com/private/" NOT url_re:archive$`
	assertFilterURLs(t, idx, &Query{Text: input, UserID: 1}, []string{docs[0].URL, docs[1].URL, docs[4].URL})
	result, err := idx.Search(&Query{Text: input + " sort:date", UserID: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 || len(result.Documents) != 1 || result.Documents[0].URL != docs[0].URL {
		t.Fatalf("unexpected sorted regexp results: %+v", result)
	}
	changes := servertypes.DocumentChanges{Label: new("selected")}
	updated, err := idx.UpdateByQuery(input, new(uint(1)), changes, false)
	if err != nil || updated.Updated != 2 {
		t.Fatalf("UpdateByQuery = %+v, %v; want two updates", updated, err)
	}
	assertFilterURLs(t, idx, &Query{Text: "label:selected"}, []string{docs[0].URL, docs[1].URL})
	deleted, err := idx.DeleteByQuery(input, new(uint(1)), nil)
	if err != nil || deleted != 2 {
		t.Fatalf("DeleteByQuery = %d, %v; want two deletions", deleted, err)
	}
	assertFilterURLs(t, idx, &Query{Text: input, UserID: 1}, []string{docs[4].URL})
	assertFilterURLs(t, idx, &Query{Text: input, UserID: 2}, []string{docs[3].URL, docs[4].URL})
}
