package indexer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"
	servertypes "github.com/asciimoo/nogon/server/types"
)

func TestInvalidQueriesDoNotSearchOrMutate(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	doc := &document.Document{URL: "https://example.com/diagnostic", Title: "needle", UserID: 1}
	addUpdateTestDocument(t, idx, doc)
	for _, input := range []string{`needle OR`, `"needle`, `NOT ()`, `title:`, `url_re:[`} {
		t.Run(input, func(t *testing.T) {
			if _, err := idx.Search(&Query{Text: input, UserID: 1, SemanticEnabled: true}); err == nil {
				t.Error("search accepted an invalid query")
			}
			if n, err := idx.CountByQuery(input, new(uint(1))); err == nil || n != 0 {
				t.Errorf("CountByQuery = %d, %v", n, err)
			}
			for _, dryRun := range []bool{true, false} {
				res, err := idx.UpdateByQuery(input, new(uint(1)), servertypes.DocumentChanges{Label: new("changed")}, dryRun)
				if err == nil || res.Matched != 0 || res.Updated != 0 {
					t.Errorf("UpdateByQuery = %+v, %v", res, err)
				}
			}
			if n, err := idx.DeleteByQuery(input, new(uint(1)), func(string, uint) { t.Error("delete callback called") }); err == nil || n != 0 {
				t.Errorf("DeleteByQuery = %d, %v", n, err)
			}
		})
	}
	stored := idx.GetByURLAndUser(doc.URL, doc.UserID)
	if stored == nil || stored.Label != "" {
		t.Fatalf("invalid queries changed the document: %+v", stored)
	}
}

func TestSearchDiagnosticKeepsPositionAfterSortDirective(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()
	for _, input := range []string{"sort:date 世界 AND", "世界 sort:date AND", "世界 AND sort:date"} {
		_, err := idx.Search(&Query{Text: input})
		position := len([]rune(input[:strings.Index(input, "AND")])) + 1
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("at character %d:", position)) {
			t.Errorf("Search(%q): %v, want character %d", input, err, position)
		}
	}
}
