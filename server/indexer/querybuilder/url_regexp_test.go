package querybuilder

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"testing"

	"github.com/asciimoo/nogon/server/indexer/searchschema"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/bleve/v2/search/query"
	index "github.com/blevesearch/bleve_index_api"
)

func TestURLRegexpAvoidsDocumentScans(t *testing.T) {
	idx := newRankingTestIndex(t, map[string]map[string]string{
		"private": {"url": "https://example.com/private"},
		"public":  {"url": "https://example.com/public"},
		"other":   {"url": "https://other.test/private"},
		"upper":   {"url": "HTTPS://EXAMPLE.COM/PRIVATE"},
		"local":   {"url": "file:///home/user/private.txt"},
	})
	for _, tc := range []struct {
		pattern string
		prefix  string
		terms   int
		matches uint64
	}{
		{`^https://example\.com/private`, "https://example.com/private", 1, 1},
		{`\Ahttps://example\.com/(private|public)$`, "https://example.com/p", 2, 2},
		{`private`, "", 5, 3},
		{`(?i)^https://example\.com/private`, "", 5, 2},
		{`^https://missing\.test/`, "https://missing.test/", 0, 0},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			q, err := BuildValidated(`url_re:"` + tc.pattern + `"`)
			if err != nil {
				t.Fatal(err)
			}
			stats := new(urlReadStats)
			result, err := idx.Search(bleve.NewSearchRequest(&measuredURLQuery{Query: q, stats: stats}))
			if err != nil {
				t.Fatal(err)
			}
			if result.Total != tc.matches {
				t.Fatalf("got %d matches, want %d", result.Total, tc.matches)
			}
			if stats.docValues != 0 || stats.allDocuments != 0 {
				t.Errorf("URL regexp read %d document values and opened %d document scans; want neither", stats.docValues, stats.allDocuments)
			}
			if stats.prefix != tc.prefix || stats.terms != tc.terms {
				t.Errorf("scanned prefix %q and %d terms, want %q and %d", stats.prefix, stats.terms, tc.prefix, tc.terms)
			}
		})
	}
}

func TestURLRegexpPreservesMatchesAndScores(t *testing.T) {
	docs := map[string]map[string]string{
		"private":   {"url": "https://example.com/private/1", "title": "privacy guide"},
		"duplicate": {"url": "https://example.com/private/1", "title": "privacy privacy guide"},
		"public":    {"url": "https://example.com/public", "title": "privacy guide reference"},
		"other":     {"url": "https://other.test/private", "title": "security guide"},
		"upper":     {"url": "HTTPS://EXAMPLE.COM/PRIVATE", "title": "privacy guide"},
		"newline":   {"url": "https://example.com/\nprivate", "title": "privacy guide"},
		"unicode":   {"url": "file:///home/user/日本語/K.txt", "title": "privacy guide"},
		"missing":   {"title": "privacy guide"},
	}
	for i := range 600 {
		docs[fmt.Sprintf("wide%d", i)] = map[string]string{"url": fmt.Sprintf("https://wide.test/%d", i), "title": "privacy guide"}
	}
	idx := newRankingTestIndex(t, docs)
	for _, pattern := range []string{
		`private`, `^https://example\.com/private/\d+$`, `(^https://example\.com)/private`,
		`(?i)^https://example\.com/private`, `(?m)^private`, `\Ahttps://example\.com/`,
		`(?:^https://example\.com/public|private$)`, `^https?://example\.com/`,
		`(?i)k\.txt$`, `\p{Han}+`, `\bprivate\b`, `(?s)^https://example\.com/.*private$`,
		`^https://missing\.test/`, `.*`, `^https://wide\.test/`, `^$`,
	} {
		t.Run(pattern, func(t *testing.T) {
			actual, err := BuildValidated(`url_re:"` + pattern + `"`)
			if err != nil {
				t.Fatal(err)
			}
			reference := query.NewBooleanQuery([]query.Query{legacyURLRegexp(pattern)}, nil, nil)
			for _, combine := range []func(query.Query) query.Query{
				func(q query.Query) query.Query { return q },
				func(q query.Query) query.Query { return bleve.NewConjunctionQuery(Build("privacy guide"), q) },
				func(q query.Query) query.Query { return bleve.NewDisjunctionQuery(Build("security"), q) },
				func(q query.Query) query.Query {
					return query.NewBooleanQuery([]query.Query{Build("privacy")}, nil, []query.Query{q})
				},
			} {
				results := make([]*bleve.SearchResult, 2)
				for i, q := range []query.Query{actual, reference} {
					req := bleve.NewSearchRequestOptions(combine(q), len(docs), 0, true)
					req.SortBy([]string{"-_score", "_id"})
					results[i], err = idx.Search(req)
					if err != nil {
						t.Fatal(err)
					}
				}
				if results[0].Total != results[1].Total || len(results[0].Hits) != len(results[1].Hits) {
					t.Fatalf("got %d matches, want %d", results[0].Total, results[1].Total)
				}
				for i, got := range results[0].Hits {
					want := results[1].Hits[i]
					if got.ID != want.ID || math.Abs(got.Score-want.Score) > 1e-12 {
						t.Fatalf("hit %d = %s (%g), want %s (%g)", i, got.ID, got.Score, want.ID, want.Score)
					}
				}
			}
		})
	}
}

func legacyURLRegexp(pattern string) query.Query {
	re := regexp.MustCompile(pattern)
	return query.NewCustomFilterQueryWithFilter(query.NewMatchAllQuery(),
		func(ctx context.Context, match *search.DocumentMatch) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			value, ok := match.Fields["url"].(string)
			return ok && re.MatchString(value), nil
		}, []string{"url"}, nil)
}

type urlReadStats struct {
	docValues, allDocuments, terms int
	prefix                         string
}

type measuredURLQuery struct {
	query.Query
	stats *urlReadStats
}

func (q *measuredURLQuery) Searcher(ctx context.Context, reader index.IndexReader, m mapping.IndexMapping, options search.SearcherOptions) (search.Searcher, error) {
	return q.Query.Searcher(ctx, &measuredURLReader{IndexReader: reader, stats: q.stats}, m, options)
}

type measuredURLReader struct {
	index.IndexReader
	stats *urlReadStats
}

func (r *measuredURLReader) DocIDReaderAll() (index.DocIDReader, error) {
	r.stats.allDocuments++
	return r.IndexReader.DocIDReaderAll()
}

func (r *measuredURLReader) DocValueReader(fields []string) (index.DocValueReader, error) {
	reader, err := r.IndexReader.DocValueReader(fields)
	return &measuredURLValues{DocValueReader: reader, stats: r.stats}, err
}

func (r *measuredURLReader) FieldDict(field string) (index.FieldDict, error) {
	dict, err := r.IndexReader.FieldDict(field)
	return &measuredURLDict{FieldDict: dict, stats: r.stats}, err
}

func (r *measuredURLReader) FieldDictPrefix(field string, prefix []byte) (index.FieldDict, error) {
	r.stats.prefix = string(prefix)
	dict, err := r.IndexReader.FieldDictPrefix(field, prefix)
	return &measuredURLDict{FieldDict: dict, stats: r.stats}, err
}

type measuredURLValues struct {
	index.DocValueReader
	stats *urlReadStats
}

func (v *measuredURLValues) VisitDocValues(id index.IndexInternalID, visitor index.DocValueVisitor) error {
	v.stats.docValues++
	return v.DocValueReader.VisitDocValues(id, visitor)
}

type measuredURLDict struct {
	index.FieldDict
	stats *urlReadStats
}

func (d *measuredURLDict) Next() (*index.DictEntry, error) {
	entry, err := d.FieldDict.Next()
	if entry != nil {
		d.stats.terms++
	}
	return entry, err
}

func TestURLRegexpDictionaryFailuresAndCancellation(t *testing.T) {
	failure := errors.New("dictionary failure")
	for _, phase := range []string{"open", "read", "close", "cancel"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			dict := &failingURLDict{next: func() (*index.DictEntry, error) { return nil, nil }}
			reader := &failingURLReader{dict: dict}
			want := failure
			switch phase {
			case "open":
				reader.err = failure
			case "read":
				dict.next = func() (*index.DictEntry, error) { return nil, failure }
			case "close":
				dict.err = failure
			case "cancel":
				want = context.Canceled
				dict.next = func() (*index.DictEntry, error) {
					cancel()
					return &index.DictEntry{Term: "https://example.com/"}, nil
				}
			}
			field, _ := searchschema.Field("url_re")
			q := buildURLRegexpQuery(field, `^https://example\.com/`)
			if _, err := q.Searcher(ctx, reader, bleve.NewIndexMapping(), search.SearcherOptions{}); !errors.Is(err, want) {
				t.Fatalf("Searcher error = %v, want %v", err, want)
			}
			if phase != "open" && !dict.closed {
				t.Fatal("URL dictionary was not closed")
			}
		})
	}
}

type failingURLReader struct {
	index.IndexReader
	dict index.FieldDict
	err  error
}

func (r *failingURLReader) FieldDictPrefix(string, []byte) (index.FieldDict, error) {
	return r.dict, r.err
}

type failingURLDict struct {
	index.FieldDict
	next   func() (*index.DictEntry, error)
	err    error
	closed bool
}

func (d *failingURLDict) Next() (*index.DictEntry, error) { return d.next() }

func (d *failingURLDict) Close() error {
	d.closed = true
	return d.err
}
