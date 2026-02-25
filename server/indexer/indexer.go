package indexer

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/indexer/querybuilder"
	"github.com/asciimoo/hister/server/model"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/custom"
	"github.com/blevesearch/bleve/v2/analysis/tokenizer/single"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/registry"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/bleve/v2/search/highlight"
	simpleFragmenter "github.com/blevesearch/bleve/v2/search/highlight/fragmenter/simple"
	simpleHighlighter "github.com/blevesearch/bleve/v2/search/highlight/highlighter/simple"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/charmbracelet/lipgloss"
	"github.com/microcosm-cc/bluemonday"
	"github.com/rs/zerolog/log"
)

var Version = 1

type indexer struct {
	idx bleve.Index
}

type Query struct {
	Text      string `json:"text"`
	Highlight string `json:"highlight"`
	Limit     int    `json:"limit"`
	Sort      string `json:"sort"`
	DateFrom  int64  `json:"date_from"`
	DateTo    int64  `json:"date_to"`
	cfg       *config.Config
}

type Results struct {
	Total           uint64            `json:"total"`
	Query           *Query            `json:"query"`
	Documents       []*Document       `json:"documents"`
	History         []*model.URLCount `json:"history"`
	SearchDuration  string            `json:"search_duration"`
	QuerySuggestion string            `json:"query_suggestion"`
}

var (
	i                   *indexer
	allFields           []string = []string{"url", "title", "text", "favicon", "html", "domain", "added"}
	ErrSensitiveContent          = errors.New("document contains sensitive data")
	sensitiveContentRe  *regexp.Regexp
	sanitizer           *bluemonday.Policy
	bleveConfig         map[string]any = map[string]any{
		"bolt_timeout": "2s",
	}
)

func Init(cfg *config.Config) error {
	sp := make([]string, 0, len(cfg.SensitiveContentPatterns))
	for _, v := range cfg.SensitiveContentPatterns {
		sp = append(sp, v)
	}
	sensitiveContentRe = regexp.MustCompile(fmt.Sprintf("(%s)", strings.Join(sp, "|")))
	idx, err := bleve.OpenUsing(cfg.IndexPath(), bleveConfig)
	if err != nil {
		if err.Error() == "timeout" {
			return errors.New("cannot open index: index is already opened - close other Hister instances and try again")
		}
		mapping := createMapping()
		idx, err = bleve.New(cfg.IndexPath(), mapping)
		if err != nil {
			return err
		}
	}
	i = &indexer{
		idx: idx,
	}
	registry.RegisterHighlighter("ansi", invertedAnsiHighlighter)
	registry.RegisterHighlighter("tui", tuiHighlighter)
	return nil
}

func init() {
	sanitizer = bluemonday.StrictPolicy()
}

func Reindex(idxPath, tmpIdxPath string, rules *config.Rules, skipSensitiveChecks bool) error {
	idx, err := bleve.OpenUsing(idxPath, bleveConfig)
	if err != nil {
		return err
	}
	mapping := createMapping()
	tmpIdx, err := bleve.New(tmpIdxPath, mapping)
	if err != nil {
		return err
	}
	q := query.NewMatchAllQuery()
	resultNum := 20
	page := 0
	for {
		req := bleve.NewSearchRequest(q)
		req.Size = resultNum
		req.From = page * resultNum
		req.Fields = allFields
		res, err := idx.Search(req)
		if err != nil || len(res.Hits) < 1 {
			break
		}
		for _, h := range res.Hits {
			d := docFromHit(h)
			log.Debug().Str("URL", d.URL).Msg("Indexing")
			d.skipSensitiveCheck = skipSensitiveChecks
			if err := d.Process(); err != nil {
				if errors.Is(err, ErrSensitiveContent) {
					log.Warn().Err(err).Str("URL", d.URL).Msg("Skipping document, sensitive content")
					continue
				} else if errors.Is(err, ErrNoExtractor) {
					log.Warn().Err(err).Str("URL", d.URL).Msg("Skipping document, can't extract content")
					continue
				} else {
					tmpIdx.Close()
					os.RemoveAll(tmpIdxPath)
					return err
				}
			}
			if rules.IsSkip(d.URL) {
				log.Info().Str("URL", d.URL).Msg("Dropping URL that has since been added to skip rules.")
				continue
			}
			// priority/score are updated implicitly by bleve
			if err := tmpIdx.Index(d.URL, d); err != nil {
				tmpIdx.Close()
				os.RemoveAll(tmpIdxPath)
				return err
			}
		}
		page += 1
		log.Info().Int("Page", page).Msg("Reindexed")
	}
	idx.Close()
	tmpIdx.Close()
	if err := os.RemoveAll(idxPath); err != nil {
		return nil
	}
	return os.Rename(tmpIdxPath, idxPath)
}

func Add(d *Document) error {
	if !d.processed {
		if err := d.Process(); err != nil {
			return err
		}
	}
	return i.idx.Index(d.URL, d)
}

func Delete(u string) error {
	return i.idx.Delete(u)
}

func Search(cfg *config.Config, q *Query) (*Results, error) {
	q.cfg = cfg
	req := bleve.NewSearchRequest(q.create())
	req.Fields = allFields

	if q.Limit > 0 {
		req.Size = q.Limit
	} else {
		req.Size = 100
	}

	switch q.Highlight {
	case "HTML":
		req.Highlight = bleve.NewHighlight()
	case "text":
		req.Highlight = bleve.NewHighlightWithStyle("ansi")
	case "tui":
		req.Highlight = bleve.NewHighlightWithStyle("tui")
	}
	switch q.Sort {
	case "domain":
		req.SortBy([]string{"domain"})
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
		if t, ok := v.Fields["added"].(float64); ok {
			d.Added = int64(t)
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

func GetByURL(u string) *Document {
	q := query.NewTermQuery(strings.ToLower(u))
	q.SetField("url")
	req := bleve.NewSearchRequest(q)
	req.Fields = allFields
	req.Highlight = bleve.NewHighlight()
	res, err := i.idx.Search(req)
	if err != nil || len(res.Hits) < 1 {
		return nil
	}
	return docFromHit(res.Hits[0])
}

func Iterate(fn func(*Document)) {
	q := query.NewMatchAllQuery()
	resultNum := 20
	page := 0
	for {
		req := bleve.NewSearchRequest(q)
		req.Size = resultNum
		req.From = page * resultNum
		req.Fields = allFields
		res, err := i.idx.Search(req)
		if err != nil || len(res.Hits) < 1 {
			return
		}
		for _, h := range res.Hits {
			d := docFromHit(h)
			fn(d)
		}
		page += 1
	}
}

func docFromHit(h *search.DocumentMatch) *Document {
	d := &Document{}
	if t, ok := h.Fragments["title"]; ok {
		d.Title = t[0]
	} else if s, ok := h.Fields["title"].(string); ok {
		d.Title = s
	}
	if s, ok := h.Fields["url"].(string); ok {
		d.URL = s
	}
	if t, ok := h.Fragments["text"]; ok {
		d.Text = t[0]
	}
	if s, ok := h.Fields["html"].(string); ok {
		d.HTML = s
	}
	if s, ok := h.Fields["favicon"].(string); ok {
		d.Favicon = s
	}
	if s, ok := h.Fields["domain"].(string); ok {
		d.Domain = s
	}
	if t, ok := h.Fields["added"].(float64); ok {
		d.Added = int64(t)
	}
	return d
}

func (q *Query) create() query.Query {
	var sq query.Query
	sq = querybuilder.Build(q.Text)

	if q.DateFrom != 0 || q.DateTo != 0 {
		if q.DateFrom != 0 && q.DateTo == 0 {
			q.DateTo = time.Now().Unix()
		}
		var min, max *float64
		if q.DateFrom != 0 {
			min = new(float64)
			*min = float64(q.DateFrom)
		}
		if q.DateTo != 0 {
			max = new(float64)
			*max = float64(q.DateTo)
		}
		dateQuery := bleve.NewNumericRangeQuery(min, max)
		dateQuery.SetField("added")
		sq = bleve.NewConjunctionQuery(sq, dateQuery)
	}

	return sq
}

func createMapping() mapping.IndexMapping {
	im := bleve.NewIndexMapping()
	im.AddCustomAnalyzer("url", map[string]any{
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
	docMapping.AddFieldMappingsAt("domain", um)
	docMapping.AddFieldMappingsAt("text", fm)
	docMapping.AddFieldMappingsAt("favicon", noIdxMap)
	docMapping.AddFieldMappingsAt("html", noIdxMap)
	docMapping.AddFieldMappingsAt("added", bleve.NewNumericFieldMapping())

	im.DefaultMapping = docMapping

	return im
}

func (q *Query) ToJSON() []byte {
	r, _ := json.Marshal(q)
	return r
}

func fullURL(base, u string) string {
	if strings.HasPrefix(u, "data:") {
		return u
	}
	pu, err := url.Parse(u)
	if err != nil {
		return ""
	}
	pb, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return pb.ResolveReference(pu).String()
}

type lipglossFormatter struct {
	style lipgloss.Style
}

func newLipglossFormatter(style lipgloss.Style) *lipglossFormatter {
	return &lipglossFormatter{style: style}
}

func (f *lipglossFormatter) Format(fragment *highlight.Fragment, orderedTermLocations highlight.TermLocations) string {
	var sb strings.Builder
	curr := fragment.Start

	for _, tl := range orderedTermLocations {
		if tl == nil || !tl.ArrayPositions.Equals(fragment.ArrayPositions) || tl.Start < curr || tl.End > fragment.End {
			continue
		}
		sb.WriteString(string(fragment.Orig[curr:tl.Start]))
		sb.WriteString(f.style.Render(string(fragment.Orig[tl.Start:tl.End])))
		curr = tl.End
	}
	sb.WriteString(string(fragment.Orig[curr:fragment.End]))

	return sb.String()
}

func invertedAnsiHighlighter(config map[string]any, cache *registry.Cache) (highlight.Highlighter, error) {
	fragmenter, err := cache.FragmenterNamed(simpleFragmenter.Name)
	if err != nil {
		return nil, fmt.Errorf("error building fragmenter: %v", err)
	}

	style := lipgloss.NewStyle().Reverse(true)
	formatter := newLipglossFormatter(style)

	return simpleHighlighter.NewHighlighter(
		fragmenter,
		formatter,
		simpleHighlighter.DefaultSeparator,
	), nil
}

func tuiHighlighter(config map[string]any, cache *registry.Cache) (highlight.Highlighter, error) {
	fragmenter, err := cache.FragmenterNamed(simpleFragmenter.Name)
	if err != nil {
		return nil, fmt.Errorf("error building fragmenter: %v", err)
	}

	style := lipgloss.NewStyle().Bold(true).Underline(true)
	formatter := newLipglossFormatter(style)

	return simpleHighlighter.NewHighlighter(
		fragmenter,
		formatter,
		simpleHighlighter.DefaultSeparator,
	), nil
}
