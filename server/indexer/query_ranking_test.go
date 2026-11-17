package indexer

import (
	"testing"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"
)

func TestRankingPreservesSearchAndDeleteConstraints(t *testing.T) {
	for _, test := range []struct {
		name        string
		query       string
		matching    string
		nonmatching string
	}{
		{
			name:        "phrase",
			query:       `"privacy policy" guide`,
			matching:    "privacy policy guide",
			nonmatching: "privacy complete policy guide",
		},
		{
			name:        "grouped filters",
			query:       "needle (site:example.com|site:example.org)",
			matching:    "needle",
			nonmatching: "needle site:example.com site:example.org",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			idx := newTestIndexer(t, testutil.Config(t))
			defer idx.Close()
			docs := []*document.Document{
				{URL: "https://example.com/own", Domain: "example.com", Title: test.matching, UserID: 1},
				{URL: "https://other.test/own", Domain: "other.test", Title: test.nonmatching, UserID: 1},
				{URL: "https://example.com/private", Domain: "example.com", Title: test.matching, UserID: 2},
				{URL: "https://example.com/global", Domain: "example.com", Title: test.matching},
			}
			for _, doc := range docs {
				doc.Processed = true
				if err := idx.Add(doc); err != nil {
					t.Fatal(err)
				}
			}
			assertFilterURLs(t, idx, &Query{Text: test.query, UserID: 1}, []string{docs[0].URL, docs[3].URL})
			count, err := idx.DeleteByQuery(test.query, new(uint(1)), nil)
			if err != nil || count != 1 {
				t.Fatalf("DeleteByQuery(%q) = %d, %v; want 1, nil", test.query, count, err)
			}
			assertFilterURLs(t, idx, &Query{Text: "*"}, []string{docs[1].URL, docs[2].URL, docs[3].URL})
		})
	}
}
