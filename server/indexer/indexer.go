package indexer

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/model"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/custom"
	"github.com/blevesearch/bleve/v2/analysis/tokenizer/single"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/blevesearch/bleve/v2/search/searcher"
	index "github.com/blevesearch/bleve_index_api"
)

type indexer struct {
	idx         bleve.Index
	initialized bool
}

type Query struct {
	Text      string   `json:"text"`
	Highlight string   `json:"highlight"`
	Fields    []string `json:"fields"`
	base      query.Query
	boostVal  *query.Boost
	cfg       *config.Config
}

type Document struct {
	URL     string  `json:"url"`
	HTML    string  `json:"html"`
	Title   string  `json:"title"`
	Text    string  `json:"text"`
	Favicon string  `json:"favicon"`
	Score   float64 `json:"score"`
}

type Results struct {
	Total     uint64            `json:"total"`
	Query     *Query            `json:"query"`
	Documents []*Document       `json:"documents"`
	History   []*model.URLCount `json:"history"`
}

var i *indexer

func Init(idxPath string) error {
	idx, err := bleve.Open(idxPath)
	if err != nil {
		mapping := createMapping()
		idx, err = bleve.New(idxPath, mapping)
		if err != nil {
			return err
		}
	}
	i = &indexer{
		idx: idx,
	}
	return nil
}

func Add(d *Document) error {
	return i.idx.Index(d.URL, d)
}

func Search(cfg *config.Config, q *Query) (*Results, error) {
	q.cfg = cfg
	req := bleve.NewSearchRequest(q.create())
	req.Fields = append(q.Fields, "favicon")
	if q.Highlight == "HTML" {
		req.Highlight = bleve.NewHighlight()
	}
	res, err := i.idx.Search(req)
	if err != nil {
		return nil, err
	}
	matches := make([]*Document, len(res.Hits))
	for j, v := range res.Hits {
		d := &Document{
			URL: v.ID,
		}
		if t, ok := v.Fragments["text"]; ok {
			d.Text = t[0]
		}
		if t, ok := v.Fragments["title"]; ok {
			d.Title = t[0]
		} else {
			s, ok := v.Fields["title"].(string)
			if ok {
				d.Title = s
			}
		}
		if i, ok := v.Fields["favicon"].(string); ok {
			d.Favicon = i
		}
		matches[j] = d
	}
	r := &Results{
		Total:     res.Total,
		Query:     q,
		Documents: matches,
	}
	return r, nil
}

func (d *Document) Process() error {
	if d.URL == "" {
		return errors.New("missing URL")
	}
	pu, err := url.Parse(d.URL)
	if err != nil {
		return err
	}
	if pu.Scheme == "" || pu.Host == "" {
		return errors.New("invalid URL: missing scheme/host")
	}
	if d.Text == "" || d.Title == "" || d.Favicon == "" {
		// TODO extract text/title/favicon from html
		return d.extractHTML()
	}
	return nil
}

func (d *Document) extractHTML() error {
	// TODO
	//r := bytes.NewReader(h)
	//doc := html.NewTokenizer(r)
	//for {
	//    tt := doc.Next()
	//    switch tt {
	//    case html.ErrorToken:
	//        err := doc.Err()
	//        if errors.Is(err, io.EOF) {
	//            return ret
	//        }
	//        ret.Error = err
	//        return ret
	//    case html.StartTagToken:
	//        tn, hasAttr := doc.TagName()
	//        if bytes.Equal(tn, []byte("body")) {
	//        }
	//	}
	//}
	return nil
}

func (q *Query) create() query.Query {
	if q.Fields == nil || len(q.Fields) == 0 {
		q.Fields = []string{"url", "title", "text"}
	}

	// add + to phrases to force matching phrases
	qp := strings.Fields(q.Text)
	inQuote := false
	for i, s := range qp {
		if len(s) == 0 {
			continue
		}
		if !inQuote && (s[0] == '-' || s[0] == '+') {
			continue
		}
		if !inQuote {
			qp[i] = "+" + qp[i]
		}
		quotes := strings.Count(s, "\"")
		if quotes%2 == 1 {
			inQuote = !inQuote
		}
	}

	sq := strings.Join(qp, " ")

	q.base = bleve.NewQueryStringQuery(sq)

	return q
}

func createMapping() mapping.IndexMapping {
	im := bleve.NewIndexMapping()
	im.AddCustomAnalyzer("url", map[string]interface{}{
		"type":         custom.Name,
		"char_filters": []string{},
		"tokenizer":    single.Name,
		"token_filters": []string{
			"to_lower",
		},
	})

	fm := bleve.NewTextFieldMapping()
	fm.Store = true
	fm.Index = true
	fm.IncludeTermVectors = true
	fm.IncludeInAll = true

	um := bleve.NewTextFieldMapping()
	um.Analyzer = "url"

	noIdxMap := bleve.NewTextFieldMapping()
	noIdxMap.Index = false

	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("title", fm)
	docMapping.AddFieldMappingsAt("url", um)
	docMapping.AddFieldMappingsAt("text", fm)
	docMapping.AddFieldMappingsAt("favicon", noIdxMap)
	docMapping.AddFieldMappingsAt("html", noIdxMap)

	im.DefaultMapping = docMapping

	return im
}

func (q *Query) SetBoost(b float64) {
	boost := query.Boost(b)
	q.boostVal = &boost
}

func (q *Query) Boost() float64 {
	return q.boostVal.Value()
}

func (q *Query) Searcher(ctx context.Context, i index.IndexReader, m mapping.IndexMapping, options search.SearcherOptions) (search.Searcher, error) {
	bs, err := q.base.Searcher(ctx, i, m, options)
	if err != nil {
		return nil, err
	}
	dvReader, err := i.DocValueReader(q.Fields)
	if err != nil {
		return nil, err
	}
	return searcher.NewFilteringSearcher(ctx, bs, q.makeFilter(dvReader)), nil
}

func (q *Query) makeFilter(dvReader index.DocValueReader) searcher.FilterFunc {
	boost := q.Boost()
	return func(sctx *search.SearchContext, d *search.DocumentMatch) bool {
		isPartOfMatch := make(map[string]bool, len(d.FieldTermLocations))
		for _, ftloc := range d.FieldTermLocations {
			isPartOfMatch[ftloc.Field] = true
		}
		seenFields := make(map[string]any, len(d.Fields))
		_ = dvReader.VisitDocValues(d.IndexInternalID, func(field string, term []byte) {
			if _, seen := seenFields[field]; seen {
				return
			}
			seenFields[field] = struct{}{}
			b := q.score(field, term, isPartOfMatch[field])
			d.Score *= boost * b
		})
		return true
	}
}

func (q *Query) score(field string, term []byte, match bool) float64 {
	var s float64 = 1
	if field == "title" && match {
		s *= 10
	}
	if field == "url" && q.cfg.Rules.IsPriority(string(term)) {
		s *= 10
	}
	return s
}
