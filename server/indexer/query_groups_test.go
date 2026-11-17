package indexer

import (
	"testing"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"
	servertypes "github.com/asciimoo/nogon/server/types"
)

func TestBooleanGroupsRespectOwnershipAndMutations(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	docs := []*document.Document{
		{URL: "https://example.com/phrase", Title: "privacy policy", UserID: 1, Updated: 100},
		{URL: "https://example.com/alternative", Title: "security", UserID: 1, Updated: 200},
		{URL: "https://example.com/scattered", Title: "privacy complete policy", UserID: 1},
		{URL: "https://example.com/excluded", Title: "security archived", UserID: 1},
		{URL: "https://example.com/private", Title: "security", UserID: 2},
		{URL: "https://example.com/global", Title: "security", Updated: 50},
	}
	for _, doc := range docs {
		addUpdateTestDocument(t, idx, doc)
	}
	const input = `title:("privacy policy" OR (security NOT archived))`
	assertFilterURLs(t, idx, &Query{Text: input, UserID: 1}, []string{docs[0].URL, docs[1].URL, docs[5].URL})
	result, err := idx.Search(&Query{Text: input + " sort:date", UserID: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 1 || result.Documents[0].URL != docs[1].URL {
		t.Fatalf("sorted group query = %+v, want %s", result.Documents, docs[1].URL)
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
	assertFilterURLs(t, idx, &Query{Text: "*"}, []string{docs[2].URL, docs[3].URL, docs[4].URL, docs[5].URL})
}
