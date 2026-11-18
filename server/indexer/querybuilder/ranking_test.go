package querybuilder

import (
	"slices"
	"testing"

	"github.com/blevesearch/bleve/v2"
)

func TestRankingPreservesRequiredMatches(t *testing.T) {
	for _, test := range []struct {
		name  string
		query string
		docs  map[string]map[string]string
		want  []string
	}{
		{
			name:  "phrase with another term",
			query: `"privacy policy" guide`,
			docs: map[string]map[string]string{
				"exact":        {"title": "privacy policy guide"},
				"scattered":    {"title": "privacy complete policy guide"},
				"missing-term": {"title": "privacy policy"},
				"split-fields": {"title": "privacy policy", "text": "guide"},
			},
			want: []string{"exact", "split-fields"},
		},
		{
			name:  "multiple phrases",
			query: `"privacy policy" "setup guide"`,
			docs: map[string]map[string]string{
				"exact":            {"title": "privacy policy setup guide"},
				"first-scattered":  {"title": "privacy complete policy setup guide"},
				"second-scattered": {"title": "privacy policy setup complete guide"},
				"split-fields":     {"title": "privacy policy", "text": "setup guide"},
			},
			want: []string{"exact", "split-fields"},
		},
		{
			name:  "grouped site filters",
			query: "needle (site:example.com|site:example.org)",
			docs: map[string]map[string]string{
				"com":          {"title": "needle", "domain": "example.com"},
				"org":          {"title": "needle", "domain": "example.org"},
				"wrong-site":   {"title": "needle site:example.com site:example.org", "domain": "other.test"},
				"missing-term": {"title": "reference", "domain": "example.com"},
			},
			want: []string{"com", "org"},
		},
		{
			name:  "negation with a phrase",
			query: `"privacy policy" guide -archived`,
			docs: map[string]map[string]string{
				"exact":     {"title": "privacy policy guide"},
				"excluded":  {"title": "privacy policy guide archived"},
				"scattered": {"title": "privacy complete policy guide"},
			},
			want: []string{"exact"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			idx := newRankingTestIndex(t, test.docs)
			q, err := BuildValidated(test.query)
			if err != nil {
				t.Fatal(err)
			}
			result, err := idx.Search(bleve.NewSearchRequest(q))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, hit := range result.Hits {
				got = append(got, hit.ID)
			}
			slices.Sort(got)
			if !slices.Equal(got, test.want) || result.Total != uint64(len(test.want)) {
				t.Fatalf("Search(%q) = %v (total %d), want %v", test.query, got, result.Total, test.want)
			}
		})
	}
}

func TestRankingPrefersPhraseAndBaseURLMatches(t *testing.T) {
	for _, test := range []struct {
		name  string
		query string
		docs  map[string]map[string]string
	}{
		{
			name:  "phrase",
			query: "amber birch",
			docs: map[string]map[string]string{
				"preferred": {"title": "amber birch cedar"},
				"other":     {"title": "amber cedar birch"},
				"unrelated": {"title": "cedar"},
			},
		},
		{
			name:  "base URL",
			query: "nogon",
			docs: map[string]map[string]string{
				"preferred": {"url": "https://nogon.eu/", "domain": "nogon.eu"},
				"other":     {"title": "nogon", "url": "https://example.com/article"},
				"unrelated": {"title": "other page", "url": "https://example.com/"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			idx := newRankingTestIndex(t, test.docs)
			result, err := idx.Search(bleve.NewSearchRequest(Build(test.query)))
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Hits) != 2 || result.Hits[0].ID != "preferred" || result.Hits[1].ID != "other" {
				t.Fatalf("Search(%q) = %v, want preferred then other", test.query, result.Hits)
			}
			if result.Hits[0].Score <= result.Hits[1].Score {
				t.Fatalf("preferred score %g must exceed other score %g", result.Hits[0].Score, result.Hits[1].Score)
			}
		})
	}
}

func newRankingTestIndex(t *testing.T, docs map[string]map[string]string) bleve.Index {
	t.Helper()
	mapping := bleve.NewIndexMapping()
	for _, field := range []string{"url", "domain"} {
		mapping.DefaultMapping.AddFieldMappingsAt(field, bleve.NewKeywordFieldMapping())
	}
	idx, err := bleve.NewMemOnly(mapping)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := idx.Close(); err != nil {
			t.Error(err)
		}
	})
	for id, doc := range docs {
		if err := idx.Index(id, doc); err != nil {
			t.Fatal(err)
		}
	}
	return idx
}
