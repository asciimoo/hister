package querybuilder

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"regexp/syntax"

	"github.com/asciimoo/nogon/server/indexer/searchschema"

	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/blevesearch/bleve/v2/search/scorer"
	"github.com/blevesearch/bleve/v2/search/searcher"
	index "github.com/blevesearch/bleve_index_api"
)

// Bound term expansion independently of index size. Broad patterns retain the
// document filter instead of allocating one posting reader per matching URL.
const maxURLRegexpTerms = 256

type urlRegexpQuery struct {
	field  string
	re     *regexp.Regexp
	prefix string
}

// URLs are indexed as single, case preserving terms. Matching the dictionary
// therefore has the same Go MatchString semantics as matching URL doc values.
func buildURLRegexpQuery(field searchschema.FieldDefinition, pattern string) query.Query {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return query.NewMatchNoneQuery()
	}
	return &urlRegexpQuery{field: field.IndexField, re: re, prefix: anchoredRegexpPrefix(pattern)}
}

func anchoredRegexpPrefix(pattern string) string {
	parsed, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return ""
	}
	program, err := syntax.Compile(parsed.Simplify())
	if err != nil || program.StartCond()&syntax.EmptyBeginText == 0 {
		return ""
	}
	// Prefix normally stops at an anchor. Skip only leading zero width
	// instructions after confirming that every match starts at the text start.
	for {
		instruction := program.Inst[program.Start]
		switch instruction.Op {
		case syntax.InstEmptyWidth, syntax.InstCapture, syntax.InstNop:
			program.Start = int(instruction.Out)
		default:
			prefix, _ := program.Prefix()
			return prefix
		}
	}
}

func (q *urlRegexpQuery) Searcher(ctx context.Context, reader index.IndexReader, m mapping.IndexMapping, options search.SearcherOptions) (search.Searcher, error) {
	terms, expanded, err := q.matchingTerms(ctx, reader)
	if err != nil {
		return nil, err
	}
	if !expanded {
		return q.documentFilter().Searcher(ctx, reader, m, options)
	}
	// Candidate postings need neither relevance scoring nor term vectors.
	// Restore the original MatchAll filter's constant score and weight so
	// conjunctions, alternatives, and ranking retain their previous scores.
	candidateOptions := options
	candidateOptions.Score = "none"
	candidateOptions.Explain = false
	candidateOptions.IncludeTermVectors = false
	child, err := searcher.NewMultiTermSearcher(ctx, reader, terms, q.field, 1, candidateOptions, true)
	if err != nil {
		return nil, err
	}
	return &constantURLScoreSearcher{Searcher: child, scorer: scorer.NewConstantScorer(1, 1, options)}, nil
}

func (q *urlRegexpQuery) matchingTerms(ctx context.Context, reader index.IndexReader) (terms []string, expanded bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	var dict index.FieldDict
	if q.prefix == "" {
		dict, err = reader.FieldDict(q.field)
	} else {
		dict, err = reader.FieldDictPrefix(q.field, []byte(q.prefix))
	}
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if closeErr := dict.Close(); err == nil {
			err = closeErr
		}
	}()
	limit := maxURLRegexpTerms
	if searcher.DisjunctionMaxClauseCount > 0 {
		limit = min(limit, searcher.DisjunctionMaxClauseCount)
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		entry, err := dict.Next()
		if err != nil {
			return nil, false, err
		}
		if entry == nil {
			return terms, true, nil
		}
		if q.re.MatchString(entry.Term) {
			if len(terms) == limit {
				return nil, false, nil
			}
			terms = append(terms, entry.Term)
		}
	}
}

func (q *urlRegexpQuery) documentFilter() query.Query {
	return query.NewCustomFilterQueryWithFilter(query.NewMatchAllQuery(),
		func(ctx context.Context, match *search.DocumentMatch) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			value, ok := match.Fields[q.field].(string)
			return ok && q.re.MatchString(value), nil
		}, []string{q.field}, nil)
}

func (q *urlRegexpQuery) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"url_regexp": q.re.String(), "field": q.field})
}

type constantURLScoreSearcher struct {
	search.Searcher
	scorer *scorer.ConstantScorer
}

func (s *constantURLScoreSearcher) Weight() float64 { return s.scorer.Weight() }

func (s *constantURLScoreSearcher) SetQueryNorm(norm float64) { s.scorer.SetQueryNorm(norm) }

func (s *constantURLScoreSearcher) Size() int {
	return int(reflect.TypeFor[constantURLScoreSearcher]().Size()) + s.Searcher.Size() + s.scorer.Size()
}

func (s *constantURLScoreSearcher) DocumentMatchPoolSize() int {
	return s.Searcher.DocumentMatchPoolSize() + 1
}

func (s *constantURLScoreSearcher) Next(ctx *search.SearchContext) (*search.DocumentMatch, error) {
	hit, err := s.Searcher.Next(ctx)
	if err != nil || hit == nil {
		return hit, err
	}
	return s.score(ctx, hit), nil
}

func (s *constantURLScoreSearcher) Advance(ctx *search.SearchContext, id index.IndexInternalID) (*search.DocumentMatch, error) {
	hit, err := s.Searcher.Advance(ctx, id)
	if err != nil || hit == nil {
		return hit, err
	}
	return s.score(ctx, hit), nil
}

func (s *constantURLScoreSearcher) score(ctx *search.SearchContext, hit *search.DocumentMatch) *search.DocumentMatch {
	result := s.scorer.Score(ctx, hit.IndexInternalID)
	ctx.DocumentMatchPool.Put(hit)
	return result
}
