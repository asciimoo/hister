package indexer

import (
	"slices"
	"testing"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"
)

func TestSearchSiteFilter(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()

	domains := []string{
		"example.com", "docs.example.com", "deep.docs.example.com", "EXAMPLE.COM", "example.com.",
		"notexample.com", "example.com.evil.test", "exampleXcom", "other.test",
	}
	for _, domain := range domains {
		if err := idx.Add(&document.Document{
			URL: "https://" + domain + "/", Domain: domain, Title: "reference", Processed: true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	for _, test := range []struct {
		query string
		want  []string
	}{
		{"site:example.com", domains[:5]},
		{"site:EXAMPLE.COM.", domains[:5]},
		{`site:"example.com"`, domains[:5]},
		{"reference site:example.com", domains[:5]},
		{"site:docs.example.com", domains[1:3]},
		{"-site:example.com", domains[5:]},
		{"site:(docs.example.com|other.test)", []string{domains[1], domains[2], domains[8]}},
		{"site:*.com", nil},
		{"domain:example.com", domains[:1]},
	} {
		t.Run(test.query, func(t *testing.T) {
			var want []string
			for _, domain := range test.want {
				want = append(want, "https://"+domain+"/")
			}
			assertFilterURLs(t, idx, &Query{Text: test.query}, want)
		})
	}
}

func TestSearchHasFilter(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()

	docs := []*document.Document{
		{URL: "https://example.com/present", Label: "research", Title: "the", Language: "en", Metadata: map[string]any{"author": "Ada", "count": 0, "enabled": false}},
		{URL: "https://example.com/empty", Metadata: map[string]any{"author": "", "tags": []string{}}},
		{URL: "https://example.com/missing"},
		{URL: "https://example.com/null", Label: " \t", Metadata: map[string]any{"author": nil}},
		{URL: "https://example.com/array", Metadata: map[string]any{"author": []string{"", "Grace"}}},
		{URL: "https://example.com/blank", Metadata: map[string]any{"author": " \t", "tags": []string{"", " "}}},
		{URL: "https://example.com/nested", Metadata: map[string]any{"author": map[string]any{"name": "Lin"}}},
	}
	for _, doc := range docs {
		doc.Processed = true
		if err := idx.Add(doc); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		query string
		want  []int
	}{
		{"has:label", []int{0}},
		{"-has:label", []int{1, 2, 3, 4, 5, 6}},
		{`has:"label"`, []int{0}},
		{"has:title", []int{0}},
		{"has:metadata.author", []int{0, 4, 6}},
		{"-has:metadata.author", []int{1, 2, 3, 5}},
		{"has:metadata.author.name", []int{6}},
		{"has:metadata.count", []int{0}},
		{"has:metadata.enabled", []int{0}},
		{"has:metadata.tags", nil},
		{"has:metadata.unknown", nil},
		{"has:visits", []int{0, 1, 2, 3, 4, 5, 6}},
		{"has:add_count", []int{0, 1, 2, 3, 4, 5, 6}},
		{"has:user_id", []int{0, 1, 2, 3, 4, 5, 6}},
		{"has:(label|metadata.author.name)", []int{0, 6}},
		{"has:metadata.author -has:label", []int{4, 6}},
		{"url:*present has:label", []int{0}},
		{"has:processed", nil},
	} {
		t.Run(test.query, func(t *testing.T) {
			var want []string
			for _, n := range test.want {
				want = append(want, docs[n].URL)
			}
			assertFilterURLs(t, idx, &Query{Text: test.query}, want)
		})
	}
}

func TestConvenienceFiltersRespectOwnershipAndMutations(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()

	for _, doc := range []*document.Document{
		{URL: "https://example.com/global", Domain: "example.com", Label: "research"},
		{URL: "https://docs.example.com/own", Domain: "docs.example.com", UserID: 1, Label: "research"},
		{URL: "https://docs.example.com/private", Domain: "docs.example.com", UserID: 2, Label: "research"},
		{URL: "https://notexample.com/own", Domain: "notexample.com", UserID: 1, Label: "research"},
	} {
		doc.Processed = true
		if err := idx.Add(doc); err != nil {
			t.Fatal(err)
		}
	}
	queryText := "site:example.com has:label"
	assertFilterURLs(t, idx, &Query{Text: queryText, UserID: 1}, []string{
		"https://example.com/global", "https://docs.example.com/own",
	})
	count, err := idx.DeleteByQuery(queryText, new(uint(1)), nil)
	if err != nil || count != 1 {
		t.Fatalf("DeleteByQuery = %d, %v; want 1, nil", count, err)
	}
	assertFilterURLs(t, idx, &Query{Text: "*"}, []string{
		"https://example.com/global", "https://docs.example.com/private", "https://notexample.com/own",
	})
}

func assertFilterURLs(t *testing.T, idx *Indexer, q *Query, want []string) {
	t.Helper()
	result, err := idx.Search(q)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, doc := range result.Documents {
		got = append(got, doc.URL)
	}
	slices.Sort(got)
	want = slices.Clone(want)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("Search(%q) = %v, want %v", q.Text, got, want)
	}
}
