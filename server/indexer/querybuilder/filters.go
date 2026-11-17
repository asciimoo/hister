package querybuilder

import (
	"context"
	"regexp"
	"strings"

	"github.com/asciimoo/nogon/server/indexer/searchschema"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/bleve/v2/search/query"
	index "github.com/blevesearch/bleve_index_api"
)

func buildSiteQuery(field searchschema.FieldDefinition, value string) query.Query {
	host := strings.TrimSuffix(value, ".")
	if host == "" {
		return query.NewMatchNoneQuery()
	}
	// Bleve regexps match complete terms. Escape the hostname so punctuation
	// cannot turn this shortcut into a wildcard or regexp query.
	q := bleve.NewRegexpQuery(`(?i)(.+\.)?` + regexp.QuoteMeta(host) + `\.?`)
	q.SetField(field.IndexField)
	return q
}

func buildPresenceQuery(value string) query.Query {
	field, ok := searchschema.PresenceField(value)
	if !ok {
		return query.NewMatchNoneQuery()
	}
	return &presenceQuery{Field: field}
}

// presenceQuery checks stored values rather than analyzed terms: a title made
// entirely of stopwords still exists, and false and zero are present values.
// It uses the search's index reader so it also works with existing indexes and
// does not depend on document storage or indexer state.
type presenceQuery struct {
	Field string `json:"has"`
}

func (q *presenceQuery) Searcher(ctx context.Context, reader index.IndexReader, m mapping.IndexMapping, options search.SearcherOptions) (search.Searcher, error) {
	metadata := strings.HasPrefix(q.Field, "metadata.")
	filter := query.NewCustomFilterQueryWithFilter(query.NewMatchAllQuery(),
		func(ctx context.Context, match *search.DocumentMatch) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			doc, err := reader.Document(match.ID)
			if err != nil || doc == nil {
				return false, err
			}
			present := false
			doc.VisitFields(func(field index.Field) {
				if present || (field.Name() != q.Field && (!metadata || !strings.HasPrefix(field.Name(), q.Field+"."))) {
					return
				}
				if text, ok := field.(index.TextField); ok {
					present = strings.TrimSpace(text.Text()) != ""
				} else {
					present = len(field.Value()) > 0
				}
			})
			return present, nil
		}, nil, nil)
	return filter.Searcher(ctx, reader, m, options)
}
