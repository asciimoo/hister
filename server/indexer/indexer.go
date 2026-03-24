package indexer

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/indexer/querybuilder"
	"github.com/asciimoo/hister/server/indexer/types"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/xsync"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/custom"
	"github.com/blevesearch/bleve/v2/analysis/token/lowercase"
	"github.com/blevesearch/bleve/v2/analysis/tokenizer/single"
	"github.com/blevesearch/bleve/v2/analysis/tokenizer/unicode"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/registry"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/bleve/v2/search/highlight"
	simpleFragmenter "github.com/blevesearch/bleve/v2/search/highlight/fragmenter/simple"
	simpleHighlighter "github.com/blevesearch/bleve/v2/search/highlight/highlighter/simple"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/charmbracelet/lipgloss"
	"github.com/rs/zerolog/log"
)

var Version = 3

type indexer struct {
	idx          bleve.IndexAlias               // used only for Search()
	indexers     xsync.Map[string, bleve.Index] // default and language specific indexers
	dir          string
	langDetector LanguageDetector
}

const (
	defaultIndexerName = "index.db"
	langIndexerName    = "index_%s.db"
)

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
	PageKey         string            `json:"page_key"`
}

type MultiBatch struct {
	indexer *indexer
	batches map[string]*bleve.Batch
}

var (
	i                   *indexer
	allFields           []string = []string{"url", "title", "text", "favicon", "html", "domain", "added"}
	ErrSensitiveContent          = errors.New("document contains sensitive data")
	sensitiveContentRe  *regexp.Regexp
	bleveConfig         map[string]any = map[string]any{
		"bolt_timeout": "2s",
		// https://github.com/blevesearch/bleve/blob/master/docs/persister.md
		"scorchPersisterOptions": map[string]any{
			"NumPersisterWorkers":           4,
			"MaxSizeInMemoryMergePerWorker": 80 * 1024 * 1024, // bytes
			// default is 1000. With 0 we drastically increases persisting occurences to reduce memory usage
			// https://github.com/blevesearch/bleve/blob/master/index/scorch/persister.go
			"PersisterNapUnderNumFiles": 0,
		},
		"scorchMergePlanOptions": map[string]any{
			"FloorSegmentFileSize": 20 * 1024 * 1024, // bytes
		},
	}
)

func Init(cfg *config.Config) error {
	if cfg.Indexer.MaxFileSize > 0 {
		maxFileSize = cfg.Indexer.MaxFileSize * 1024 * 1024 // bytes
	}
	sp := make([]string, 0, len(cfg.SensitiveContentPatterns))
	for _, v := range cfg.SensitiveContentPatterns {
		sp = append(sp, v)
	}
	sensitiveContentRe = regexp.MustCompile(fmt.Sprintf("(%s)", strings.Join(sp, "|")))
	var err error
	i, err = initializeIndexer(cfg.FullPath(""), cfg.Indexer.DetectLanguages)
	if err != nil {
		return err
	}
	if err := registry.RegisterHighlighter("ansi", invertedAnsiHighlighter); err != nil {
		return err
	}
	if err := registry.RegisterHighlighter("tui", tuiHighlighter); err != nil {
		return err
	}
	return nil
}

func initializeIndexer(basePath string, detectLanguages bool) (*indexer, error) {
	if _, err := os.Stat(basePath); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(basePath, os.ModePerm); err != nil {
			return nil, err
		}
	}
	idxPath := filepath.Join(basePath, defaultIndexerName)
	idx, err := bleve.OpenUsing(idxPath, bleveConfig)
	if err != nil {
		if err.Error() == "timeout" {
			return nil, errors.New("cannot open index: index is already opened - close other Hister instances and try again")
		}
		mapping := createMapping("default")
		idx, err = bleve.NewUsing(idxPath, mapping, bleve.Config.DefaultIndexType, bleve.Config.DefaultMemKVStore, bleveConfig)
		if err != nil {
			return nil, err
		}
	}
	idx.SetName(defaultIndexerName)
	i = &indexer{
		idx: bleve.NewIndexAlias(idx),
		dir: basePath,
	}
	i.indexers.Store(defaultIndexerName, idx)
	if !detectLanguages {
		i.langDetector = NewNullLanguageDetector()
	} else {
		i.langDetector = NewLanguageDetector()
	}
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		fn := e.Name()
		// TODO do more precise name check
		if !strings.HasPrefix(fn, "index_") || !strings.HasSuffix(fn, ".db") {
			continue
		}
		if !detectLanguages {
			log.Warn().Str("Index", fn).Msg("Language specific index database found while language detection is turned off. Run hister reindex to be able to use the content of this index.")
			continue
		}
		langIdx, err := bleve.OpenUsing(filepath.Join(basePath, fn), bleveConfig)
		if err != nil {
			return nil, err
		}
		langIdx.SetName(fn)
		i.idx.Add(langIdx)
		i.indexers.Store(fn, langIdx)
	}
	return i, nil
}

func Reindex(basePath string, rules *config.Rules, skipSensitiveChecks bool, detectLanguages bool) error {
	idx, err := initializeIndexer(basePath, true)
	if err != nil {
		return err
	}
	tmpBasePath := filepath.Join(basePath, "reindex")
	if _, err := os.Stat(tmpBasePath); err == nil {
		if err := os.RemoveAll(tmpBasePath); err != nil {
			return err
		}
	}
	tmpIdx, err := initializeIndexer(tmpBasePath, detectLanguages)
	if err != nil {
		return err
	}
	q := query.NewMatchAllQuery()
	total := idx.Total()
	batchSize := 50
	page := 0
	latest := ""
	req := bleve.NewSearchRequest(q)
	req.Fields = allFields
	req.Size = batchSize
	req.SortBy([]string{"_id"})
	for {
		if latest != "" {
			req.SetSearchAfter([]string{latest})
		}
		res, err := idx.idx.Search(req)
		if err != nil || len(res.Hits) < 1 {
			break
		}
		n := len(res.Hits)
		b := newMultiBatch(tmpIdx)
		for _, h := range res.Hits {
			d := docFromHit(h)
			log.Debug().Str("URL", d.URL).Msg("Indexing")
			d.skipSensitiveCheck = skipSensitiveChecks
			origDate := d.Added
			if err := d.Process(tmpIdx.langDetector); err != nil {
				if errors.Is(err, ErrSensitiveContent) {
					log.Warn().Err(err).Str("URL", d.URL).Msg("Skipping document, sensitive content")
					continue
				} else if d.Type == types.Local && errors.Is(err, os.ErrNotExist) {
					log.Warn().Str("URL", d.URL).Msg("Skipping document, file not found")
					continue
				} else if errors.Is(err, ErrNoExtractor) {
					log.Warn().Err(err).Str("URL", d.URL).Msg("Skipping document, can't extract content")
					continue
				} else if errors.Is(err, ErrReadFile) {
					log.Warn().Err(err).Str("Path", d.URL).Msg("Skipping document, can't read file")
					continue
				} else {
					tmpIdx.Close()
					if rerr := os.RemoveAll(tmpBasePath); rerr != nil {
						log.Warn().Err(rerr).Msg("failed to clean up temp index path")
					}
					return err
				}
			}
			if rules.IsSkip(d.URL) {
				log.Info().Str("URL", d.URL).Msg("Dropping URL that has since been added to skip rules.")
				continue
			}
			d.Added = origDate
			if err := b.Add(d); err != nil {
				tmpIdx.Close()
				if rerr := os.RemoveAll(tmpBasePath); rerr != nil {
					log.Warn().Err(rerr).Msg("failed to clean up temp index path")
				}
				return err
			}
		}
		if err := b.Save(); err != nil {
			tmpIdx.Close()
			if rerr := os.RemoveAll(tmpBasePath); rerr != nil {
				log.Warn().Err(rerr).Msg("failed to clean up temp index path")
			}
			return err
		}
		runtime.GC()
		page += 1
		latest = res.Hits[n-1].Fields["url"].(string)
		log.Info().Msg(fmt.Sprintf("Reindexed [%d/%d]", page*batchSize, total))
	}
	idx.Close()
	tmpIdx.Close()
	for n := range idx.indexers.All {
		idxPath := filepath.Join(basePath, n)
		if err := os.RemoveAll(idxPath); err != nil {
			return err
		}
	}
	var renameError error
	for n := range tmpIdx.indexers.All {
		idxPath := filepath.Join(basePath, n)
		tmpIdxPath := filepath.Join(tmpBasePath, n)
		if err := os.Rename(tmpIdxPath, idxPath); err != nil {
			renameError = err
		}
	}
	if renameError != nil {
		return errors.New("failed to rename tmp indexes during the reindex, resolve the issue manually")
	}
	return os.RemoveAll(tmpBasePath)
}

func DocumentCount() uint64 {
	return i.Total()
}

func Add(d *Document) error {
	return i.AddDocument(d)
}

func (i *indexer) Total() uint64 {
	q := query.NewMatchAllQuery()
	req := bleve.NewSearchRequest(q)
	req.Size = 1
	res, err := i.idx.Search(req)
	if err != nil {
		return 0
	}
	return res.Total
}

func (i *indexer) AddDocument(d *Document) error {
	if !d.processed {
		if err := d.Process(i.langDetector); err != nil {
			return err
		}
	}
	return i.getOrCreate(d.Language).Index(d.URL, d)
}

func GetLatestDocuments(limit int, latest string) *Results {
	q := query.NewMatchAllQuery()
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"url", "title", "added"}
	req.Size = limit
	req.SortByCustom(search.SortOrder{
		&search.SortField{
			Field: "added",
			Desc:  true,
		},
	})
	if latest != "" {
		req.SetSearchAfter([]string{latest})
	}
	res, err := i.idx.Search(req)
	if err != nil || len(res.Hits) < 1 {
		return nil
	}
	docs := make([]*Document, len(res.Hits))
	for i, h := range res.Hits {
		d := &Document{
			Title: h.Fields["title"].(string),
			URL:   h.Fields["url"].(string),
			Added: int64(h.Fields["added"].(float64)),
		}
		docs[i] = d
	}
	return &Results{
		Documents: docs,
		PageKey:   res.Hits[len(res.Hits)-1].Sort[0],
	}
}

func (i *indexer) getOrCreate(lang string) bleve.Index {
	if lang == UnknownLanguage || lang == "" {
		idx, _ := i.indexers.Load(defaultIndexerName)
		return idx
	}
	idxName := fmt.Sprintf(langIndexerName, lang)
	idx, err := i.indexers.LoadOrCompute(idxName, func() (bleve.Index, error) {
		return i.createIndex(idxName, lang)
	})
	if err != nil {
		log.Warn().Err(err).Str("Name", idxName).Msg("Failed to create language indexer")
		fallback, _ := i.indexers.Load(defaultIndexerName)
		return fallback
	}
	return idx
}

func (i *indexer) createIndex(name, lang string) (bleve.Index, error) {
	mapping := createMapping(lang)
	idx, err := bleve.NewUsing(filepath.Join(i.dir, name), mapping, bleve.Config.DefaultIndexType, bleve.Config.DefaultMemKVStore, bleveConfig)
	if err != nil {
		return nil, err
	}
	idx.SetName(name)
	i.idx.Add(idx)
	return idx, nil
}

func (i *indexer) Close() {
	for name, idx := range i.indexers.All {
		if err := idx.Close(); err != nil {
			log.Warn().Err(err).Str("index", name).Msg("failed to close index")
		}
	}
}

func Close() {
	if i != nil {
		i.Close()
	}
}

func NewMultiBatch() *MultiBatch {
	return newMultiBatch(i)
}

func newMultiBatch(idx *indexer) *MultiBatch {
	return &MultiBatch{
		indexer: idx,
		batches: make(map[string]*bleve.Batch),
	}
}

func (b *MultiBatch) Add(d *Document) error {
	if !d.processed {
		if err := d.Process(b.indexer.langDetector); err != nil {
			return err
		}
	}
	idx := b.indexer.getOrCreate(d.Language)
	if _, ok := b.batches[d.Language]; !ok {
		b.batches[d.Language] = idx.NewBatch()
	}
	return b.batches[d.Language].Index(d.URL, d)
}

func (b *MultiBatch) Delete(u string) error {
	for _, idx := range b.indexer.indexers.All {
		if err := idx.Delete(u); err != nil {
			return err
		}
	}
	return nil
}

func (b *MultiBatch) Save() error {
	for l, lb := range b.batches {
		idx := b.indexer.getOrCreate(l)
		if err := idx.Batch(lb); err != nil {
			return err
		}
	}
	return nil
}

func Delete(u string) error {
	for _, idx := range i.indexers.All {
		if err := idx.Delete(u); err != nil {
			return err
		}
	}
	return nil
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
		req.Highlight.Fields = []string{"text"}
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
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"url"}
	req.Size = 200
	req.SortBy([]string{"_id"})
	latest := ""
	for {
		if latest != "" {
			req.SetSearchAfter([]string{latest})
		}
		res, err := i.idx.Search(req)
		n := len(res.Hits)
		if err != nil || n < 1 {
			return
		}
		for _, h := range res.Hits {
			d := docFromHit(h)
			fn(d)
		}
		latest = res.Hits[n-1].Fields["url"].(string)
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

func createMapping(lang string) mapping.IndexMapping {
	im := bleve.NewIndexMapping()
	textAnalyzer := lang
	if lang == UnknownLanguage || lang == "" || lang == "default" {
		err := im.AddCustomAnalyzer("default", map[string]any{
			"type":         custom.Name,
			"char_filters": []string{},
			"tokenizer":    unicode.Name,
			"token_filters": []string{
				lowercase.Name,
			},
		})
		if err != nil {
			panic(err)
		}
		textAnalyzer = "default"
	}
	err := im.AddCustomAnalyzer("url", map[string]any{
		"type":         custom.Name,
		"char_filters": []string{},
		"tokenizer":    single.Name,
		"token_filters": []string{
			lowercase.Name,
		},
	})
	if err != nil {
		panic(err)
	}

	fm := bleve.NewTextFieldMapping()
	fm.Store = true
	fm.Index = true
	fm.IncludeTermVectors = true
	fm.IncludeInAll = true
	fm.Analyzer = textAnalyzer

	um := bleve.NewTextFieldMapping()
	um.Analyzer = "url"
	um.IncludeTermVectors = false

	noIdxMap := bleve.NewTextFieldMapping()
	noIdxMap.Store = true
	noIdxMap.Index = false
	noIdxMap.IncludeTermVectors = false
	noIdxMap.IncludeInAll = false
	noIdxMap.DocValues = false

	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("title", fm)
	docMapping.AddFieldMappingsAt("text", fm)
	docMapping.AddFieldMappingsAt("url", um)
	docMapping.AddFieldMappingsAt("domain", um)
	docMapping.AddFieldMappingsAt("language", um)
	docMapping.AddFieldMappingsAt("favicon", noIdxMap)
	docMapping.AddFieldMappingsAt("html", noIdxMap)
	docMapping.AddFieldMappingsAt("added", bleve.NewNumericFieldMapping())
	docMapping.AddFieldMappingsAt("type", bleve.NewNumericFieldMapping())

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

	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("205")).
		Bold(true)
	formatter := newLipglossFormatter(style)

	return simpleHighlighter.NewHighlighter(
		fragmenter,
		formatter,
		simpleHighlighter.DefaultSeparator,
	), nil
}
