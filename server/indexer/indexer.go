package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/extractor"
	"github.com/asciimoo/hister/server/indexer/querybuilder"
	"github.com/asciimoo/hister/server/indexer/searchschema"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/types"
	"github.com/asciimoo/hister/server/vectorstore"

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

var Version = 8

// Indexer owns the search indexes and related document storage for one Hister
// data directory.
type Indexer struct {
	idx               bleve.IndexAlias       // used only for Search()
	indexers          map[string]bleve.Index // default and language specific indexers
	indexesMu         sync.RWMutex           // protects idx, indexers, and indexesClosed
	indexCreationMu   sync.Mutex             // serializes index creation, close, and adoption
	indexesClosed     bool
	dir               string
	data              *dataStore
	langDetector      document.LanguageDetector
	reindexInProgress atomic.Bool
	embedder          *vectorstore.Embedder
	vectorStore       vectorstore.VectorStore
	embedCtx          context.Context
	embedCancel       context.CancelFunc
	embeddingQueue    *embeddingQueue
	embeddingWorkers  int
	disablePreviews   bool
	keepStopwords     bool
	languages         []string
	lowAccuracyDetect bool
	directories       []*config.Directory
	maxFileSize       int64
	sensitivePattern  *regexp.Regexp
	semanticConfig    config.SemanticSearch
}

// indexOptions carries the settings that decide how indexes are created and
// which languages get one of their own.
type indexOptions struct {
	DetectLanguages bool
	KeepStopwords   bool
	// Languages restricts detection to these ISO 639-1 codes. Empty means every
	// language document.Languages supports.
	Languages []string
	// LowAccuracy restricts lingua to its trigram models.
	LowAccuracy bool
}

func optionsFromConfig(cfg *config.Config) indexOptions {
	return indexOptions{
		DetectLanguages: cfg.Indexer.DetectLanguages,
		KeepStopwords:   cfg.Indexer.KeepStopwords,
		Languages:       cfg.Indexer.Languages,
		LowAccuracy:     cfg.Indexer.LowAccuracyLanguageDetection(),
	}
}

// optionsWith combines the receiver's language settings with a per-request
// override of detectLanguages and keepStopwords, so a reindex keeps the
// configured language set while still honouring the request.
func (i *Indexer) optionsWith(detectLanguages, keepStopwords bool) indexOptions {
	return indexOptions{
		DetectLanguages: detectLanguages,
		KeepStopwords:   keepStopwords,
		Languages:       i.languages,
		LowAccuracy:     i.lowAccuracyDetect,
	}
}

const (
	defaultIndexerName = "index.db"
	langIndexerName    = "index_%s.db"
	updatedBackfillKey = "hister.updated_backfill_complete"
)

type Query struct {
	Text              string  `json:"text"`
	Highlight         string  `json:"highlight"`
	Limit             int     `json:"limit"`
	Sort              string  `json:"sort"`
	DateFrom          int64   `json:"date_from"`
	DateTo            int64   `json:"date_to"`
	UserID            uint    `json:"user_id"`
	SemanticEnabled   bool    `json:"semantic_enabled"`
	SemanticThreshold float64 `json:"semantic_threshold"`
	SemanticWeight    float64 `json:"semantic_weight"`
	PageKey           string  `json:"page_key"`
	IncludeHTML       bool    `json:"include_html"`
	IncludeText       bool    `json:"include_text"`
	Facets            bool    `json:"facets,omitempty"`
	// FacetSizes overrides the schema default cap per named facet.
	FacetSizes map[string]int `json:"facet_sizes,omitempty"`
	// FacetsOnly skips document fetching (size=0) and returns only facet
	// counts. Requires Facets=true. Used by the /api/facets endpoint.
	FacetsOnly bool `json:"facets_only,omitempty"`
	// MatchAll bypasses the text-DSL builder and runs a match-all query.
	// Combine with UserID / Facets / DateFrom / DateTo for cheap aggregate
	// queries (e.g. completion sources). Text is ignored when set.
	MatchAll bool `json:"match_all,omitempty"`
	// PriorityPatterns contains URL regex patterns whose matching documents
	// receive a score boost.  Set by doSearch from the user's priority rules.
	PriorityPatterns []string `json:"priority_patterns,omitempty"`
}

// TermCount and RangeCount are the shape of facet buckets returned by Search
// when Query.Facets is true.
type TermCount struct {
	Term  string `json:"term"`
	Count int    `json:"count"`
	Label string `json:"label,omitempty"`
}

type RangeCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// TermFacet holds the top-N terms for one named facet together with the count
// of documents that matched terms outside the top-N (Other).
type TermFacet struct {
	Terms []TermCount `json:"terms,omitempty"`
	Other int         `json:"other,omitempty"`
}

type FacetsResult struct {
	// Terms maps each term-facet name (e.g. "domains", "languages") to its
	// result. Adding a new term facet only requires a new AddFacet call; no
	// struct change is needed.
	Terms         map[string]TermFacet `json:"terms,omitempty"`
	DateHistogram []RangeCount         `json:"date_histogram,omitempty"`
}

func addFacets(req *bleve.SearchRequest, sizes map[string]int) {
	facetSize := func(definition searchschema.FacetDefinition) int {
		if n := sizes[definition.Name]; n > 0 {
			return n
		}
		return definition.DefaultSize
	}
	for _, definition := range searchschema.Facets() {
		field, ok := searchschema.Field(definition.QueryField)
		if !ok {
			continue
		}
		switch definition.Kind {
		case searchschema.FacetKindTerms:
			req.AddFacet(definition.Name, bleve.NewFacetRequest(field.IndexField, facetSize(definition)))
		case searchschema.FacetKindNumericRanges:
			values := searchschema.Values(field.ValueSet)
			facet := bleve.NewFacetRequest(field.IndexField, len(values))
			for _, value := range values {
				facet.AddNumericRange(value.BucketName(), value.Min, value.Max)
			}
			req.AddFacet(definition.Name, facet)
		case searchschema.FacetKindDateRanges:
			values := searchschema.Values(field.ValueSet)
			facet := bleve.NewFacetRequest(field.IndexField, len(values))
			now := time.Now()
			for _, value := range values {
				min, max, ok := value.RelativeTimeBounds(now)
				if !ok {
					continue
				}
				facet.AddNumericRange(value.BucketName(), min, max)
			}
			req.AddFacet(definition.Name, facet)
		}
	}
}

func extractTermFacet(f *search.FacetResult) TermFacet {
	if f == nil || f.Terms == nil {
		return TermFacet{}
	}
	terms := f.Terms.Terms()
	out := make([]TermCount, 0, len(terms))
	for _, t := range terms {
		out = append(out, TermCount{Term: t.Term, Count: t.Count})
	}
	return TermFacet{Terms: out, Other: f.Other}
}

func extractFacets(facets search.FacetResults) *FacetsResult {
	fr := &FacetsResult{Terms: make(map[string]TermFacet)}
	for _, definition := range searchschema.Facets() {
		facet := facets[definition.Name]
		if facet == nil {
			continue
		}
		switch definition.Kind {
		case searchschema.FacetKindTerms:
			fr.Terms[definition.Name] = extractTermFacet(facet)
		case searchschema.FacetKindNumericRanges:
			terms := make([]TermCount, 0, len(facet.NumericRanges))
			for _, numericRange := range facet.NumericRanges {
				value, _ := searchschema.FacetValue(definition.Name, numericRange.Name)
				terms = append(terms, TermCount{
					Term:  numericRange.Name,
					Count: numericRange.Count,
					Label: value.Label,
				})
			}
			fr.Terms[definition.Name] = TermFacet{Terms: terms}
		case searchschema.FacetKindDateRanges:
			for _, numericRange := range facet.NumericRanges {
				fr.DateHistogram = append(fr.DateHistogram, RangeCount{
					Name:  numericRange.Name,
					Count: numericRange.Count,
				})
			}
		}
	}
	return fr
}

// SemanticHit represents a document found via vector similarity search.
type SemanticHit struct {
	DocID        string             `json:"doc_id"`
	Similarity   float64            `json:"similarity"`
	MatchedChunk string             `json:"matched_chunk,omitempty"`
	Document     *document.Document `json:"document,omitempty"`
}

type Results struct {
	Total           uint64               `json:"total"`
	Query           *Query               `json:"query"`
	Documents       []*document.Document `json:"documents"`
	History         []*model.URLCount    `json:"history"`
	SearchDuration  string               `json:"search_duration"`
	QuerySuggestion string               `json:"query_suggestion"`
	PageKey         string               `json:"page_key"`
	SemanticHits    []SemanticHit        `json:"semantic_hits,omitempty"`
	SemanticEnabled bool                 `json:"semantic_enabled"`
	Facets          *FacetsResult        `json:"facets,omitempty"`
}

type documentWritePlan struct {
	target         bleve.Index
	staleIndexes   []bleve.Index
	oldHTMLKeys    []string
	oldFaviconKeys []string
	newHTMLKey     string
	newFaviconKey  string
	needsEmbedding bool
}

type documentWriteFunc func(*document.Document, documentWritePlan) error

type indexBatch struct {
	index bleve.Index
	batch *bleve.Batch
}

type storedDocumentState struct {
	htmlKeys    []string
	faviconKeys []string
	texts       []string
	indexNames  map[string]struct{}
	addCount    uint
	found       bool
	label       string
	added       int64
}

func (s storedDocumentState) embeddingTextChanged(text string) bool {
	return !s.found || !slices.Contains(s.texts, text)
}

type MultiBatch struct {
	indexer             *Indexer
	batches             map[string]*indexBatch
	orphanedHTMLKeys    []string
	orphanedFaviconKeys []string
	embeddingIDs        map[string]struct{}
	deletedIDs          map[string]struct{}
	incrementAddCount   bool
}

func (i *Indexer) searchIndexes(req *bleve.SearchRequest) (*bleve.SearchResult, error) {
	i.indexesMu.RLock()
	defer i.indexesMu.RUnlock()
	return i.idx.Search(req)
}

func (i *Indexer) indexes() map[string]bleve.Index {
	i.indexesMu.RLock()
	defer i.indexesMu.RUnlock()
	return maps.Clone(i.indexers)
}

func (i *Indexer) indexByName(name string) (bleve.Index, bool) {
	i.indexesMu.RLock()
	defer i.indexesMu.RUnlock()
	idx, ok := i.indexers[name]
	return idx, ok
}

var (
	registerHighlightersOnce sync.Once
	registerHighlightersErr  error
	// allFields      []string       = []string{"url", "title", "text", "favicon", "html", "domain", "added", "updated", "type", "user_id"}
	allFields            []string       = []string{"*"}
	ErrEmptyFilter                      = errors.New("delete query must not be empty")
	ErrFileURLNotAllowed                = errors.New("file URL is not allowed")
	bleveConfig          map[string]any = map[string]any{
		"bolt_timeout": "2s",
		// https://github.com/blevesearch/bleve/blob/master/docs/persister.md
		"scorchPersisterOptions": map[string]any{
			"NumPersisterWorkers":           4,
			"MaxSizeInMemoryMergePerWorker": 40 * 1024 * 1024, // bytes
			// default is 1000. With 200 we increases persisting occurences to reduce memory usage
			// https://github.com/blevesearch/bleve/blob/master/index/scorch/persister.go
			"PersisterNapUnderNumFiles": 200,
		},
		"scorchMergePlanOptions": map[string]any{
			"FloorSegmentFileSize": 20 * 1024 * 1024, // bytes
			"SegmentsPerMergeTask": 10,               // default is 10
		},
	}
)

// New creates an independent indexer instance from cfg.
func New(cfg *config.Config) (*Indexer, error) {
	sp := make([]string, 0, len(cfg.SensitiveContentPatterns))
	for _, v := range cfg.SensitiveContentPatterns {
		sp = append(sp, v)
	}
	sensitivePattern := regexp.MustCompile(fmt.Sprintf("(%s)", strings.Join(sp, "|")))
	idx, err := initializeIndexer(cfg.FullPath(""), optionsFromConfig(cfg))
	if err != nil {
		return nil, err
	}
	idx.disablePreviews = cfg.App.DisablePreviews
	idx.directories = cfg.Indexer.Directories
	idx.maxFileSize = defaultMaxFileSize
	idx.sensitivePattern = sensitivePattern
	idx.semanticConfig = cfg.SemanticSearch
	if cfg.Indexer.MaxFileSize > 0 {
		idx.maxFileSize = cfg.Indexer.MaxFileSize * 1024 * 1024 // bytes
	}
	if cfg.SemanticSearch.Enable {
		vs, err := vectorstore.New(cfg)
		if err != nil {
			log.Warn().Err(err).Msg("failed to create vector store, semantic search disabled")
		} else if err := vs.Init(); err != nil {
			log.Warn().Err(err).Msg("failed to init vector store, semantic search disabled")
			if closeErr := vs.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Msg("failed to close vector store")
			}
		} else {
			workers := normalizeEmbeddingWorkerCount(cfg.SemanticSearch.MaxEmbeddingConcurrency)
			embedCfg := cfg.SemanticSearch
			embedCfg.MaxEmbeddingConcurrency = workers
			idx.vectorStore = vs
			idx.embedder = vectorstore.NewEmbedder(&embedCfg)
			if err := idx.startEmbeddingQueue(workers); err != nil {
				idx.vectorStore = nil
				idx.embedder = nil
				if closeErr := vs.Close(); closeErr != nil {
					log.Warn().Err(closeErr).Msg("failed to close vector store")
				}
				idx.Close()
				return nil, fmt.Errorf("start embedding queue: %w", err)
			}
			log.Info().Msg("semantic search enabled")
		}
	}
	if err := registerHighlighters(); err != nil {
		idx.Close()
		return nil, err
	}
	return idx, nil
}

func registerHighlighters() error {
	registerHighlightersOnce.Do(func() {
		if err := registry.RegisterHighlighter("ansi", invertedAnsiHighlighter); err != nil && !strings.Contains(err.Error(), "duplicate highlighter") {
			registerHighlightersErr = err
			return
		}
		if err := registry.RegisterHighlighter("tui", tuiHighlighter); err != nil && !strings.Contains(err.Error(), "duplicate highlighter") {
			registerHighlightersErr = err
		}
	})
	return registerHighlightersErr
}

func initializeIndexer(basePath string, opts indexOptions) (*Indexer, error) {
	if unsupported := document.UnsupportedLanguages(opts.Languages); len(unsupported) > 0 {
		return nil, fmt.Errorf("unsupported language(s) in indexer.languages: %s", strings.Join(unsupported, ", "))
	}
	detectLanguages, keepStopwords := opts.DetectLanguages, opts.KeepStopwords
	if _, err := os.Stat(basePath); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(basePath, os.ModePerm); err != nil {
			return nil, err
		}
	}
	idxPath := filepath.Join(basePath, defaultIndexerName)
	created := false
	idx, err := bleve.OpenUsing(idxPath, bleveRuntimeConfig())
	if err != nil {
		if err.Error() == "timeout" {
			return nil, errors.New("cannot open index: index is already opened - close other Hister instances and try again")
		}
		mapping := createMapping("default", keepStopwords)
		idx, err = bleve.NewUsing(idxPath, mapping, bleve.Config.DefaultIndexType, bleve.Config.DefaultMemKVStore, bleveRuntimeConfig())
		if err != nil {
			return nil, err
		}
		created = true
	}
	idx.SetName(defaultIndexerName)
	embedCtx, embedCancel := context.WithCancel(context.Background())
	i := &Indexer{
		idx: bleve.NewIndexAlias(idx),
		indexers: map[string]bleve.Index{
			defaultIndexerName: idx,
		},
		dir:               basePath,
		keepStopwords:     keepStopwords,
		languages:         opts.Languages,
		lowAccuracyDetect: opts.LowAccuracy,
		embedCtx:          embedCtx,
		embedCancel:       embedCancel,
		data:              newDataStore(filepath.Join(basePath, dataDirName)),
		maxFileSize:       defaultMaxFileSize,
	}
	initialized := false
	defer func() {
		if !initialized {
			i.Close()
		}
	}()
	if !detectLanguages {
		i.langDetector = document.NewNullLanguageDetector()
	} else {
		i.langDetector = document.NewLanguageDetectorFor(opts.Languages, opts.LowAccuracy)
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
		langIdx, err := bleve.OpenUsing(filepath.Join(basePath, fn), bleveRuntimeConfig())
		if err != nil {
			return nil, err
		}
		langIdx.SetName(fn)
		i.indexesMu.Lock()
		i.idx.Add(langIdx)
		i.indexers[fn] = langIdx
		i.indexesMu.Unlock()
	}
	if err := i.backfillUpdatedTimestamps(); err != nil {
		return nil, err
	}
	if created {
		if err := writeIndexMetadata(idx, configuredIndexMetadata(detectLanguages, keepStopwords)); err != nil {
			return nil, fmt.Errorf("store initial index metadata: %w", err)
		}
	}
	initialized = true
	return i, nil
}

// backfillUpdatedTimestamps exists only for backward compatibility with
// indexes created before Document.Updated was added. Remove this function and
// its marker after support for those indexes is no longer needed.
func (i *Indexer) backfillUpdatedTimestamps() error {
	for name, idx := range i.indexes() {
		marker, err := idx.GetInternal([]byte(updatedBackfillKey))
		if err != nil {
			return fmt.Errorf("read updated timestamp backfill marker for %s: %w", name, err)
		}
		if len(marker) > 0 {
			continue
		}

		updatedExists := bleve.NewNumericRangeQuery(nil, nil)
		updatedExists.SetField("updated")
		missingUpdated := query.NewBooleanQuery(nil, nil, []query.Query{updatedExists})
		req := bleve.NewSearchRequest(missingUpdated)
		req.Fields = allFields
		req.Size = 200
		req.SortBy([]string{"_id"})

		count := 0
		var sortKey []string
		for {
			if len(sortKey) > 0 {
				req.SetSearchAfter(sortKey)
			}
			res, err := idx.Search(req)
			if err != nil {
				return fmt.Errorf("find documents missing updated timestamp in %s: %w", name, err)
			}
			if len(res.Hits) == 0 {
				break
			}

			batch := idx.NewBatch()
			for _, hit := range res.Hits {
				added, ok := hit.Fields["added"]
				if !ok {
					return fmt.Errorf("backfill updated timestamp for %s: document %s has no added timestamp", name, hit.ID)
				}
				fields := maps.Clone(hit.Fields)
				fields["updated"] = added
				if err := batch.Index(hit.ID, fields); err != nil {
					return fmt.Errorf("backfill updated timestamp for %s: %w", name, err)
				}
			}
			if err := idx.Batch(batch); err != nil {
				return fmt.Errorf("save updated timestamp backfill for %s: %w", name, err)
			}
			count += len(res.Hits)
			sortKey = res.Hits[len(res.Hits)-1].Sort
		}

		if err := idx.SetInternal([]byte(updatedBackfillKey), []byte("1")); err != nil {
			return fmt.Errorf("save updated timestamp backfill marker for %s: %w", name, err)
		}
		if count > 0 {
			log.Info().Str("index", name).Int("documents", count).Msg("Backfilled updated timestamps")
		}
	}
	return nil
}

func openReindexSources(basePath string, current map[string]bleve.Index) (map[string]bleve.Index, func(), error) {
	sources := maps.Clone(current)
	extraSources := []bleve.Index{}
	closeExtraSources := func() {
		for _, source := range extraSources {
			if err := source.Close(); err != nil {
				log.Warn().Err(err).Str("index", source.Name()).Msg("failed to close reindex source")
			}
		}
		extraSources = nil
	}

	entries, err := os.ReadDir(basePath)
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "index_") || !strings.HasSuffix(name, ".db") {
			continue
		}
		if _, exists := sources[name]; exists {
			continue
		}
		source, err := bleve.OpenUsing(filepath.Join(basePath, name), bleveRuntimeConfig())
		if err != nil {
			closeExtraSources()
			return nil, nil, err
		}
		source.SetName(name)
		sources[name] = source
		extraSources = append(extraSources, source)
	}
	return sources, closeExtraSources, nil
}

func (i *Indexer) Reindex(rules *config.Rules, skipSensitiveChecks bool, detectLanguages, keepStopwords bool, dirs []*config.Directory) error {
	return i.reindex(i.dir, rules, skipSensitiveChecks, detectLanguages, keepStopwords, dirs)
}

func (idx *Indexer) reindex(basePath string, rules *config.Rules, skipSensitiveChecks bool, detectLanguages, keepStopwords bool, dirs []*config.Directory) (retErr error) {
	// TODO store new documents in both indexes while running reindex to guarantee not losing any data.
	if !idx.reindexInProgress.CompareAndSwap(false, true) {
		return errors.New("Reindex is already running")
	}
	workers := idx.embeddingWorkers
	queueStopped := false
	oldIndexClosed := false
	defer func() {
		idx.reindexInProgress.Store(false)
		if retErr != nil && queueStopped && !oldIndexClosed && idx.embeddingQueue == nil {
			if err := idx.startEmbeddingQueue(workers); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("restart embedding queue: %w", err))
			}
		}
	}()
	tmpBasePath := filepath.Join(basePath, "reindex")
	if _, err := os.Stat(tmpBasePath); err == nil {
		if err := os.RemoveAll(tmpBasePath); err != nil {
			return err
		}
	}
	sourceIndexes, closeExtraSources, err := openReindexSources(basePath, idx.indexes())
	if err != nil {
		return err
	}
	defer closeExtraSources()
	tmpIdx, err := initializeIndexer(tmpBasePath, idx.optionsWith(detectLanguages, keepStopwords))
	if err != nil {
		return err
	}
	tmpIdx.directories = dirs
	tmpIdx.maxFileSize = idx.maxFileSize
	tmpIdx.sensitivePattern = idx.sensitivePattern
	// Propagate the disablePreviews flag so the temp indexer skips HTML storage too.
	tmpIdx.disablePreviews = idx.disablePreviews
	// The data store is shared between the live and temp indexers so that
	// content-addressed files written during reindex are immediately usable
	// after the rename step. No data directory rename is needed.
	tmpIdx.data = idx.data
	if idx.embeddingQueue != nil {
		idx.stopEmbeddingQueue()
		queueStopped = true
	}

	// Carry the vector store and embedder into the temporary indexer so that
	// MultiBatch.Add() re-embeds every surviving document.  The vector store is
	// rebuilt in-place (no temp-dir / rename dance is needed because it is a
	// separate file from the Bleve indexes).
	vs := idx.vectorStore
	embedder := idx.embedder
	if vs != nil && embedder != nil {
		if err := vs.Clear(); err != nil {
			log.Warn().Err(err).Msg("failed to clear vector store before reindex")
		} else {
			tmpIdx.vectorStore = vs
			tmpIdx.embedder = embedder
		}
	}
	abortReindex := func(err error) error {
		// The live indexer still owns the shared vector store when reindexing
		// aborts. Do not let closing the temporary indexer close that store.
		tmpIdx.vectorStore = nil
		tmpIdx.Close()
		if rerr := os.RemoveAll(tmpBasePath); rerr != nil {
			log.Warn().Err(rerr).Msg("failed to clean up temp index path")
		}
		return err
	}
	q := query.NewMatchAllQuery()
	var total uint64
	for name, source := range sourceIndexes {
		count, err := source.DocCount()
		if err != nil {
			log.Warn().Err(err).Str("index", name).Msg("failed to count reindex source")
			continue
		}
		total += count
	}
	batchSize := 50
	processed := 0
	for subIdxName, subIdx := range sourceIndexes {
		log.Info().Str("sub-index", subIdxName).Msg("Reindexing sub-index")
		var sortKey []string
		req := bleve.NewSearchRequest(q)
		req.Fields = allFields
		req.Size = batchSize
		req.SortBy([]string{"_id"})
		for {
			if len(sortKey) > 0 {
				req.SetSearchAfter(sortKey)
			}
			res, err := subIdx.Search(req)
			if err != nil || len(res.Hits) < 1 {
				break
			}
			n := len(res.Hits)
			b := newMultiBatch(tmpIdx)
			for _, h := range res.Hits {
				d := idx.resFromHit(h, resultIncludeAll)
				if d.Type == types.Local {
					filePath := filepath.Clean(files.FileURLToPath(d.URL))
					dir := files.FindMatchingDir(dirs, filePath)
					if !files.DirectoryMatchesPath(dir, filePath) {
						log.Warn().Str("URL", d.URL).Msg("Skipping document, file no longer matches directory configuration")
						continue
					}
					ownerID, err := files.FindDirUser(dirs, filePath)
					if err != nil || ownerID != d.UserID {
						log.Warn().Err(err).Str("URL", d.URL).Msg("Skipping document, directory owner changed")
						continue
					}
					if _, err := os.Stat(filePath); errors.Is(err, os.ErrNotExist) {
						log.Warn().Str("URL", d.URL).Msg("Skipping document, file not found")
						continue
					}
					if dir.Label != "" {
						d.Label = dir.Label
					}
				}
				log.Debug().Str("URL", d.URL).Msg("Indexing")
				d.SetSkipSensitiveCheck(skipSensitiveChecks)
				origAdded := d.Added
				origUpdated := d.Updated
				if err := tmpIdx.processDocument(d); err != nil {
					if errors.Is(err, document.ErrSensitiveContent) {
						log.Warn().Err(err).Str("URL", d.URL).Msg("Skipping document, sensitive content")
						continue
					} else if errors.Is(err, extractor.ErrNoExtractor) {
						log.Warn().Err(err).Str("URL", d.URL).Msg("Skipping document, can't extract content")
						continue
					} else if errors.Is(err, extractor.ErrExtractorAbort) {
						log.Warn().Err(err).Str("URL", d.URL).Msg("Skipping document, extractor aborted")
						continue
					} else if errors.Is(err, document.ErrReadFile) {
						log.Warn().Err(err).Str("Path", d.URL).Msg("Skipping document, can't read file")
						continue
					} else {
						return abortReindex(err)
					}
				}
				if rules.IsSkip(d.URL) {
					log.Info().Str("URL", d.URL).Msg("Dropping URL that has since been added to skip rules.")
					continue
				}
				d.Added = origAdded
				if origUpdated == 0 {
					d.Updated = origAdded
				} else {
					d.Updated = origUpdated
				}
				if err := b.Add(d); err != nil {
					return abortReindex(err)
				}
			}
			if err := b.Save(); err != nil {
				return abortReindex(err)
			}
			runtime.GC()
			processed += n
			sortKey = res.Hits[n-1].Sort
			log.Info().Msg(fmt.Sprintf("Reindexed [%d/%d]", processed, total))
		}
	}
	if err := tmpIdx.setMetadata(configuredIndexMetadata(detectLanguages, keepStopwords)); err != nil {
		return abortReindex(fmt.Errorf("store replacement index metadata: %w", err))
	}
	closeExtraSources()
	idx.vectorStore = nil // prevent Close() from closing the store we're still using
	idx.Close()
	oldIndexClosed = true
	tmpIdx.vectorStore = nil // already referenced by vs; prevent double-close
	tmpIdx.Close()
	for n := range sourceIndexes {
		idxPath := filepath.Join(basePath, n)
		if err := os.RemoveAll(idxPath); err != nil {
			return err
		}
	}
	var renameError error
	for n := range tmpIdx.indexes() {
		idxPath := filepath.Join(basePath, n)
		tmpIdxPath := filepath.Join(tmpBasePath, n)
		if err := os.Rename(tmpIdxPath, idxPath); err != nil {
			renameError = err
		}
	}
	if renameError != nil {
		return errors.New("failed to rename tmp indexes during the reindex, resolve the issue manually")
	}
	replacement, err := initializeIndexer(basePath, idx.optionsWith(detectLanguages, keepStopwords))
	if err != nil {
		return err
	}
	replacement.maxFileSize = idx.maxFileSize
	replacement.sensitivePattern = idx.sensitivePattern
	replacement.semanticConfig = idx.semanticConfig
	idx.adopt(replacement)
	// Restore settings that are not part of the index state.
	idx.disablePreviews = tmpIdx.disablePreviews
	idx.directories = dirs
	// Restore the vector store and embedder on the replacement indexer.
	if vs != nil && embedder != nil {
		idx.vectorStore = vs
		idx.embedder = embedder
		if workers > 0 {
			if err := idx.startEmbeddingQueue(workers); err != nil {
				return fmt.Errorf("restart embedding queue: %w", err)
			}
			queueStopped = false
		}
	}
	if err := os.RemoveAll(tmpBasePath); err != nil {
		return err
	}
	// Remove data files no longer referenced by any document.
	if _, err := idx.data.cleanup("html_key", htmlSubdir, idx.countKeyRefs); err != nil {
		log.Warn().Err(err).Msg("failed to clean up orphaned HTML data files")
	}
	if _, err := idx.data.cleanup("favicon_key", faviconSubdir, idx.countKeyRefs); err != nil {
		log.Warn().Err(err).Msg("failed to clean up orphaned favicon data files")
	}
	return nil
}

// CleanupResult describes local index reconciliation and orphaned data cleanup.
type CleanupResult struct {
	LocalDocumentsChecked int `json:"localDocumentsChecked"`
	LocalDocumentsSkipped int `json:"localDocumentsSkipped"`
	LocalDocumentsRemoved int `json:"localDocumentsRemoved"`
	HTMLRemoved           int `json:"htmlRemoved"`
	FaviconRemoved        int `json:"faviconRemoved"`
}

// Cleanup removes local documents that no longer match the directory config,
// then removes orphaned HTML and favicon data files.
func (i *Indexer) Cleanup(dirs []*config.Directory) (CleanupResult, error) {
	result := CleanupResult{}
	localResult, err := i.CleanupLocalDocuments(dirs)
	result.LocalDocumentsChecked = localResult.Checked
	result.LocalDocumentsSkipped = localResult.Skipped
	result.LocalDocumentsRemoved = localResult.Removed
	if err != nil {
		return result, err
	}
	result.HTMLRemoved, result.FaviconRemoved, err = i.CleanupDataFiles()
	return result, err
}

// CleanupDataFiles removes orphaned HTML and favicon files from the data
// directories (files that exist on disk but are no longer referenced by any
// document in the index). It returns the number of HTML and favicon files
// removed, and any walk error encountered.
// Each candidate file is checked with a live ref-count query while holding the
// per-key shard lock, so the check and the removal are atomic and safe against
// concurrent /api/add calls.
func (i *Indexer) CleanupDataFiles() (int, int, error) {
	htmlRemoved, err := i.data.cleanup("html_key", htmlSubdir, i.countKeyRefs)
	if err != nil {
		return htmlRemoved, 0, fmt.Errorf("failed to clean up orphaned HTML data files: %w", err)
	}
	faviconRemoved, err := i.data.cleanup("favicon_key", faviconSubdir, i.countKeyRefs)
	if err != nil {
		return htmlRemoved, faviconRemoved, fmt.Errorf("failed to clean up orphaned favicon data files: %w", err)
	}
	return htmlRemoved, faviconRemoved, nil
}

func (i *Indexer) ReadFavicon(key string) ([]byte, error) {
	if !validDataKey(key) {
		return nil, errors.New("invalid favicon key")
	}
	return i.data.read(faviconSubdir, key)
}

func validDataKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	for _, c := range key {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (i *Indexer) SemanticSearchEnabled() bool {
	return i != nil && i.embedder != nil && i.vectorStore != nil
}

// semanticTextPreviewLen is the maximum number of runes returned in
// MatchedChunk and semantic-only Document.Text fields. Keeps response payloads
// comparable to Bleve's keyword result snippets.
const semanticTextPreviewLen = 500

// truncateText trims s to at most maxRunes runes, appending "…" when cut.
func truncateText(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

func documentMetadataString(d *document.Document, key string) string {
	if d.Metadata == nil {
		return ""
	}
	value, _ := d.Metadata[key].(string)
	return value
}

func documentEmbeddingContext(d *document.Document) vectorstore.DocumentContext {
	documentType := documentMetadataString(d, "type")
	var keywords []string
	for _, key := range []string{"topics", "tags", "categories", "languages"} {
		if value := documentMetadataString(d, key); value != "" {
			keywords = append(keywords, value)
		}
	}
	return vectorstore.DocumentContext{
		Title:       d.Title,
		URL:         d.URL,
		Type:        documentType,
		Language:    d.Language,
		Author:      documentMetadataString(d, "author"),
		Description: documentMetadataString(d, "description"),
		Keywords:    strings.Join(keywords, ", "),
	}
}

// embedDocumentChunks creates metadata and body chunk embeddings and stores the
// resulting vectors. Errors are logged and returned so durable queue workers
// can retry them. Synchronous callers may ignore the error and continue Bleve
// indexing.
func embedDocumentChunks(ctx context.Context, idx *Indexer, d *document.Document) error {
	start := time.Now()
	chunks, err := idx.embedder.ChunkAndEmbed(ctx, d.Text, documentEmbeddingContext(d))
	if err != nil {
		if errors.Is(err, context.Canceled) {
			log.Debug().Str("url", d.URL).Msg("chunk embedding canceled")
			return err
		}
		log.Warn().Err(err).Str("url", d.URL).Msg("chunk embedding failed, skipping vectors")
		return err
	}
	if len(chunks) == 0 {
		return nil
	}
	if err := idx.vectorStore.PutChunks(d.ID(), d.UserID, chunks); err != nil {
		log.Warn().Err(err).Str("url", d.URL).Msg("vector store write failed")
		return err
	}
	log.Debug().Str("url", d.URL).Int("chunks", len(chunks)).Dur("duration", time.Since(start)).Msg("embedded document chunks")
	return nil
}

func (i *Indexer) Add(d *document.Document) error {
	if err := i.validateFileDocument(d); err != nil {
		return err
	}
	return i.AddDocument(d)
}

func (i *Indexer) processDocument(d *document.Document) error {
	return d.ProcessWithSensitivePattern(i.langDetector, extractor.Extract, i.sensitivePattern)
}

func (i *Indexer) validateFileDocument(d *document.Document) error {
	pu, err := url.Parse(d.URL)
	if err != nil {
		return err
	}
	if !strings.EqualFold(pu.Scheme, "file") {
		return nil
	}
	if d.Text == "" {
		return fmt.Errorf("%w: submitted content is required", ErrFileURLNotAllowed)
	}

	filePath := filepath.Clean(files.FileURLToPath(d.URL))
	dir := files.FindMatchingDir(i.directories, filePath)
	if !filepath.IsAbs(filePath) || !files.DirectoryMatchesPath(dir, filePath) {
		return ErrFileURLNotAllowed
	}
	ownerID, err := files.FindDirUser(i.directories, filePath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrFileURLNotAllowed, err)
	}
	if ownerID != d.UserID {
		return fmt.Errorf("%w: directory owner mismatch", ErrFileURLNotAllowed)
	}
	return nil
}

func (i *Indexer) Total() uint64 {
	q := query.NewMatchAllQuery()
	req := bleve.NewSearchRequest(q)
	req.Size = 1
	res, err := i.searchIndexes(req)
	if err != nil {
		return 0
	}
	return res.Total
}

func (i *Indexer) TotalByUser(userID uint) uint64 {
	uid := float64(userID)
	q := bleve.NewNumericRangeInclusiveQuery(&uid, &uid, new(true), new(true))
	q.SetField("user_id")
	req := bleve.NewSearchRequest(q)
	req.Size = 1
	res, err := i.searchIndexes(req)
	if err != nil {
		return 0
	}
	return res.Total
}

func (i *Indexer) AddDocument(d *document.Document) error {
	return i.addDocument(d, true, i.applyDocumentWrite)
}

func (i *Indexer) addDocument(d *document.Document, incrementAddCount bool, write documentWriteFunc) error {
	plan, err := i.prepareDocumentWrite(d, incrementAddCount)
	if err != nil {
		return err
	}
	if plan != nil {
		if err := write(d, *plan); err != nil {
			return err
		}
	}
	for _, extra := range d.ExtraDocuments {
		if err := i.addDocument(extra, false, write); err != nil {
			log.Warn().Err(err).Str("url", extra.URL).Msg("failed to index extra document")
		}
	}
	return nil
}

func (i *Indexer) prepareDocumentWrite(d *document.Document, incrementAddCount bool) (*documentWritePlan, error) {
	if !d.IsProcessed() {
		if err := i.processDocument(d); err != nil {
			return nil, err
		}
	}
	if d.SkipIndexing {
		return nil, nil
	}
	state := i.getStoredDocumentState(d.ID())
	needsEmbedding := i.embedder != nil && i.vectorStore != nil && state.embeddingTextChanged(d.Text)
	applySubmissionTimestamps(d, state)
	if incrementAddCount {
		if state.found {
			d.AddCount = state.addCount + 1
		} else {
			d.AddCount = 1
		}
	} else if d.AddCount < 1 {
		d.AddCount = 1
	}
	if d.Label == "" {
		d.Label = state.label
	}
	plan, err := i.prepareStorageWrite(d, state)
	if err != nil {
		return nil, err
	}
	plan.needsEmbedding = needsEmbedding
	return &plan, nil
}

func applySubmissionTimestamps(d *document.Document, state storedDocumentState) {
	now := time.Now().Unix()
	if state.found && state.added != 0 {
		d.Added = state.added
	} else if d.Added == 0 {
		d.Added = now
	}
	if d.Updated == 0 {
		if state.found {
			d.Updated = now
		} else {
			d.Updated = d.Added
		}
	}
}

func (i *Indexer) save(d *document.Document) error {
	return i.saveWithState(d, i.getStoredDocumentState(d.ID()))
}

func (i *Indexer) saveWithState(d *document.Document, state storedDocumentState) error {
	plan, err := i.prepareStorageWrite(d, state)
	if err != nil {
		return err
	}
	return i.applyDocumentWrite(d, plan)
}

func (i *Indexer) prepareStorageWrite(d *document.Document, state storedDocumentState) (documentWritePlan, error) {
	plan := documentWritePlan{}
	existingIndexes := state.indexNames
	if existingIndexes == nil {
		indexes := i.indexes()
		existingIndexes = make(map[string]struct{}, len(indexes))
		for name := range indexes {
			existingIndexes[name] = struct{}{}
		}
	}
	if err := i.prepareForStorage(d); err != nil {
		return plan, err
	}
	plan.oldHTMLKeys = state.htmlKeys
	plan.oldFaviconKeys = state.faviconKeys
	plan.newHTMLKey = d.HTMLKey
	plan.newFaviconKey = d.FaviconKey
	plan.target = i.getOrCreate(d.Language)
	for name := range existingIndexes {
		if name == plan.target.Name() {
			continue
		}
		idx, ok := i.indexByName(name)
		if !ok {
			continue
		}
		plan.staleIndexes = append(plan.staleIndexes, idx)
	}
	return plan, nil
}

func (i *Indexer) applyDocumentWrite(d *document.Document, plan documentWritePlan) error {
	log.Debug().Str("URL", d.URL).Msg("item added to index")
	if err := plan.target.Index(d.ID(), d); err != nil {
		return err
	}
	for _, idx := range plan.staleIndexes {
		if err := idx.Delete(d.ID()); err != nil {
			return err
		}
	}
	i.cleanupDataKeys(plan.oldHTMLKeys, plan.newHTMLKey, plan.oldFaviconKeys, plan.newFaviconKey)
	if plan.needsEmbedding {
		if err := i.enqueueEmbedding(d.ID()); err != nil {
			return fmt.Errorf("enqueue embedding: %w", err)
		}
	}
	return nil
}

func (i *Indexer) cleanupDataKeys(oldHTMLKeys []string, newHTMLKey string, oldFaviconKeys []string, newFaviconKey string) {
	for _, key := range oldHTMLKeys {
		if key != "" && key != newHTMLKey {
			i.data.deleteIfOrphaned("html_key", htmlSubdir, key, i.countKeyRefs)
		}
	}
	for _, key := range oldFaviconKeys {
		if key != "" && key != newFaviconKey {
			i.data.deleteIfOrphaned("favicon_key", faviconSubdir, key, i.countKeyRefs)
		}
	}
}

// getStoredDocumentState fetches the fields needed to update an existing
// document. The same document can appear in more than one index when its
// detected language changes between additions.
func (i *Indexer) getStoredDocumentState(id string) storedDocumentState {
	var state storedDocumentState
	q := bleve.NewDocIDQuery([]string{id})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"html_key", "favicon_key", "language", "add_count", "label", "added"}
	if i.embedder != nil && i.vectorStore != nil {
		req.Fields = append(req.Fields, "text")
	}
	i.indexesMu.RLock()
	req.Size = len(i.indexers) + 1 // at most one entry per index
	res, err := i.idx.Search(req)
	i.indexesMu.RUnlock()
	if err != nil {
		return state
	}
	seenHTML := make(map[string]struct{})
	seenFav := make(map[string]struct{})
	seenText := make(map[string]struct{})
	state.indexNames = make(map[string]struct{})
	if len(res.Hits) < 1 {
		return state
	}
	state.found = true
	state.addCount = 1
	for _, h := range res.Hits {
		indexName := h.Index
		if indexName == "" {
			lang, _ := h.Fields["language"].(string)
			indexName = indexNameForLanguage(lang)
		}
		state.indexNames[indexName] = struct{}{}
		if n, ok := h.Fields["add_count"].(float64); ok {
			state.addCount = max(state.addCount, uint(n))
		}
		if state.label == "" {
			state.label, _ = h.Fields["label"].(string)
		}
		if n, ok := h.Fields["added"].(float64); ok {
			added := int64(n)
			if state.added == 0 || added < state.added {
				state.added = added
			}
		}
		if text, ok := h.Fields["text"].(string); ok {
			if _, dup := seenText[text]; !dup {
				state.texts = append(state.texts, text)
				seenText[text] = struct{}{}
			}
		}
		if k, ok := h.Fields["html_key"].(string); ok && k != "" {
			if _, dup := seenHTML[k]; !dup {
				state.htmlKeys = append(state.htmlKeys, k)
				seenHTML[k] = struct{}{}
			}
		}
		if k, ok := h.Fields["favicon_key"].(string); ok && k != "" {
			if _, dup := seenFav[k]; !dup {
				state.faviconKeys = append(state.faviconKeys, k)
				seenFav[k] = struct{}{}
			}
		}
	}
	return state
}

func (i *Indexer) getDocKeysByID(id string) (htmlKeys, faviconKeys []string) {
	state := i.getStoredDocumentState(id)
	return state.htmlKeys, state.faviconKeys
}

// countKeyRefs returns the number of indexed documents that reference the
// given key in the specified field (html_key or favicon_key).
// Returns 1 on search error as a safe default to avoid accidental deletion.
func (i *Indexer) countKeyRefs(field, key string) uint64 {
	q := bleve.NewTermQuery(key)
	q.SetField(field)
	req := bleve.NewSearchRequest(q)
	req.Size = 0
	res, err := i.searchIndexes(req)
	if err != nil {
		return 1
	}
	return res.Total
}

// prepareForStorage writes HTML and favicon to the data dir (if not already done)
// and stores their SHA-256 hash keys on the document, clearing the inline fields
// so that large blobs are not persisted inside the Bleve index.
// When disablePreviews is true, HTML is discarded entirely and HTMLKey is cleared.
// Inline blobs are cleared whenever a key is set, so they are never written into
// the Bleve index DB (e.g. during reindex where resFromHit populates both fields).
func (i *Indexer) prepareForStorage(d *document.Document) error {
	if i.disablePreviews {
		d.HTML = ""
		d.HTMLKey = ""
	} else {
		if d.HTML != "" {
			key, err := i.data.write(htmlSubdir, []byte(d.HTML))
			if err != nil {
				return fmt.Errorf("store HTML: %w", err)
			}
			d.HTMLKey = key
		}
		if d.HTMLKey != "" {
			d.HTML = ""
		}
	}
	if d.Favicon != "" {
		key, err := i.data.write(faviconSubdir, []byte(d.Favicon))
		if err != nil {
			return fmt.Errorf("store favicon: %w", err)
		}
		d.FaviconKey = key
	}
	if d.FaviconKey != "" {
		d.Favicon = ""
	}
	return nil
}

// Saves a document without any processing
func (i *Indexer) Save(d *document.Document) error {
	return i.save(d)
}

func (i *Indexer) getOrCreate(lang string) bleve.Index {
	idxName := indexNameForLanguage(lang)
	if idx, ok := i.indexByName(idxName); ok {
		return idx
	}
	if err := i.addIndexer(idxName, lang); err != nil {
		log.Warn().Err(err).Str("Name", idxName).Msg("Failed to create language indexer")
		idx, _ := i.indexByName(defaultIndexerName)
		return idx
	}
	idx, _ := i.indexByName(idxName)
	return idx
}

func indexNameForLanguage(lang string) string {
	if lang == document.UnknownLanguage || lang == "" {
		return defaultIndexerName
	}
	return fmt.Sprintf(langIndexerName, lang)
}

func (i *Indexer) addIndexer(name, lang string) error {
	i.indexCreationMu.Lock()
	defer i.indexCreationMu.Unlock()
	if _, exists := i.indexByName(name); exists {
		return nil
	}
	mapping := createMapping(lang, i.keepStopwords)
	indexPath := filepath.Join(i.dir, name)
	idx, err := bleve.NewUsing(indexPath, mapping, bleve.Config.DefaultIndexType, bleve.Config.DefaultMemKVStore, bleveRuntimeConfig())
	if err != nil {
		return err
	}
	idx.SetName(name)
	i.indexesMu.Lock()
	defer i.indexesMu.Unlock()
	if i.indexesClosed {
		closeErr := idx.Close()
		removeErr := os.RemoveAll(indexPath)
		return errors.Join(bleve.ErrorIndexClosed, closeErr, removeErr)
	}
	if err := copyIndexMetadata(i.indexers[defaultIndexerName], idx); err != nil {
		closeErr := idx.Close()
		removeErr := os.RemoveAll(indexPath)
		return errors.Join(err, closeErr, removeErr)
	}
	i.indexers[name] = idx
	i.idx.Add(idx)
	return nil
}

func (i *Indexer) Close() {
	i.stopEmbeddingQueue()
	if i.embedCancel != nil {
		i.embedCancel()
	}
	if i.vectorStore != nil {
		if err := i.vectorStore.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close vector store")
		}
	}
	i.indexCreationMu.Lock()
	defer i.indexCreationMu.Unlock()
	i.indexesMu.Lock()
	defer i.indexesMu.Unlock()
	if i.indexesClosed {
		return
	}
	i.indexesClosed = true
	for name, idx := range i.indexers {
		if err := idx.Close(); err != nil {
			log.Warn().Err(err).Str("index", name).Msg("failed to close index")
		}
	}
	if err := i.idx.Close(); err != nil {
		log.Warn().Err(err).Msg("failed to close index alias")
	}
}

func (i *Indexer) adopt(replacement *Indexer) {
	replacement.indexesMu.RLock()
	defer replacement.indexesMu.RUnlock()
	i.indexCreationMu.Lock()
	defer i.indexCreationMu.Unlock()
	i.indexesMu.Lock()
	defer i.indexesMu.Unlock()
	i.idx = replacement.idx
	i.indexers = maps.Clone(replacement.indexers)
	i.indexesClosed = false
	i.dir = replacement.dir
	i.data = replacement.data
	i.langDetector = replacement.langDetector
	i.embedder = replacement.embedder
	i.vectorStore = replacement.vectorStore
	i.embedCtx = replacement.embedCtx
	i.embedCancel = replacement.embedCancel
	i.embeddingQueue = replacement.embeddingQueue
	i.embeddingWorkers = replacement.embeddingWorkers
	i.disablePreviews = replacement.disablePreviews
	i.keepStopwords = replacement.keepStopwords
	i.languages = replacement.languages
	i.lowAccuracyDetect = replacement.lowAccuracyDetect
	i.directories = replacement.directories
	i.maxFileSize = replacement.maxFileSize
	i.sensitivePattern = replacement.sensitivePattern
	i.semanticConfig = replacement.semanticConfig
}

func (i *Indexer) NewMultiBatch() *MultiBatch {
	b := newMultiBatch(i)
	b.incrementAddCount = true
	return b
}

func newMultiBatch(idx *Indexer) *MultiBatch {
	return &MultiBatch{
		indexer:           idx,
		batches:           make(map[string]*indexBatch),
		embeddingIDs:      make(map[string]struct{}),
		deletedIDs:        make(map[string]struct{}),
		incrementAddCount: false,
	}
}

func (b *MultiBatch) getOrCreateBatch(name string, idx bleve.Index) *bleve.Batch {
	entry, ok := b.batches[name]
	if !ok {
		entry = &indexBatch{index: idx, batch: idx.NewBatch()}
		b.batches[name] = entry
	}
	return entry.batch
}

func (b *MultiBatch) Add(d *document.Document) error {
	if err := b.indexer.validateFileDocument(d); err != nil {
		return err
	}
	return b.indexer.addDocument(d, b.incrementAddCount, b.applyDocumentWrite)
}

func (b *MultiBatch) applyDocumentWrite(d *document.Document, plan documentWritePlan) error {
	delete(b.deletedIDs, d.ID())
	if plan.needsEmbedding {
		if b.indexer.embeddingWorkers > 0 {
			b.embeddingIDs[d.ID()] = struct{}{}
		} else {
			_ = embedDocumentChunks(b.indexer.embedCtx, b.indexer, d)
		}
	}
	for _, idx := range plan.staleIndexes {
		b.getOrCreateBatch(idx.Name(), idx).Delete(d.ID())
	}
	if err := b.getOrCreateBatch(plan.target.Name(), plan.target).Index(d.ID(), d); err != nil {
		return err
	}
	for _, key := range plan.oldHTMLKeys {
		if key != plan.newHTMLKey {
			b.orphanedHTMLKeys = append(b.orphanedHTMLKeys, key)
		}
	}
	for _, key := range plan.oldFaviconKeys {
		if key != plan.newFaviconKey {
			b.orphanedFaviconKeys = append(b.orphanedFaviconKeys, key)
		}
	}
	return nil
}

func (b *MultiBatch) Delete(id string) {
	oldHTMLKeys, oldFaviconKeys := b.indexer.getDocKeysByID(id)
	delete(b.embeddingIDs, id)
	b.deletedIDs[id] = struct{}{}
	for name, idx := range b.indexer.indexes() {
		b.getOrCreateBatch(name, idx).Delete(id)
	}
	b.orphanedHTMLKeys = append(b.orphanedHTMLKeys, oldHTMLKeys...)
	b.orphanedFaviconKeys = append(b.orphanedFaviconKeys, oldFaviconKeys...)
}

func (b *MultiBatch) Save() error {
	for _, entry := range b.batches {
		if err := entry.index.Batch(entry.batch); err != nil {
			return err
		}
	}
	b.indexer.cleanupDataKeys(b.orphanedHTMLKeys, "", b.orphanedFaviconKeys, "")
	for id := range b.deletedIDs {
		if err := b.indexer.cancelEmbedding(id); err != nil {
			log.Warn().Err(err).Str("id", id).Msg("failed to cancel embedding job")
		}
		if b.indexer.vectorStore != nil {
			if err := b.indexer.vectorStore.Delete(id); err != nil {
				log.Warn().Err(err).Str("id", id).Msg("vector store delete failed")
			}
		}
	}
	for id := range b.embeddingIDs {
		if err := b.indexer.enqueueEmbedding(id); err != nil {
			return fmt.Errorf("enqueue embedding for %s: %w", id, err)
		}
	}
	return nil
}

func (i *Indexer) Delete(id string) error {
	htmlKeys, faviconKeys := i.getDocKeysByID(id)
	for _, idx := range i.indexes() {
		if err := idx.Delete(id); err != nil {
			return err
		}
	}
	if err := i.cancelEmbedding(id); err != nil {
		log.Warn().Err(err).Str("id", id).Msg("failed to cancel embedding job")
	}
	if i.vectorStore != nil {
		if err := i.vectorStore.Delete(id); err != nil {
			log.Warn().Err(err).Str("id", id).Msg("vector store delete failed")
		}
	}
	for _, k := range htmlKeys {
		i.data.deleteIfOrphaned("html_key", htmlSubdir, k, i.countKeyRefs)
	}
	for _, k := range faviconKeys {
		i.data.deleteIfOrphaned("favicon_key", faviconSubdir, k, i.countKeyRefs)
	}
	return nil
}

func (i *Indexer) DeleteByQuery(text string, userID *uint, onDelete func(url string, userID uint)) (int, error) {
	if strings.TrimSpace(text) == "" {
		return 0, ErrEmptyFilter
	}
	q := querybuilder.Build(text)
	if userID != nil {
		uid := float64(*userID)
		userQ := bleve.NewNumericRangeInclusiveQuery(&uid, &uid, new(true), new(true))
		userQ.SetField("user_id")
		q = bleve.NewConjunctionQuery(q, userQ)
	}

	count := 0
	const pageSize = 200
	var searchAfter []string
	for {
		req := bleve.NewSearchRequest(q)
		req.Fields = []string{"url", "user_id"}
		req.Size = pageSize
		req.SortBy([]string{"_id"})
		if len(searchAfter) > 0 {
			req.SetSearchAfter(searchAfter)
		}
		res, err := i.searchIndexes(req)
		if err != nil {
			return count, err
		}
		n := len(res.Hits)
		if n == 0 {
			break
		}
		batch := newMultiBatch(i)
		for _, h := range res.Hits {
			batch.Delete(h.ID)
		}
		if err := batch.Save(); err != nil {
			return count, err
		}
		if onDelete != nil {
			for _, h := range res.Hits {
				url, _ := h.Fields["url"].(string)
				uid := uint(0)
				if u, ok := h.Fields["user_id"].(float64); ok {
					uid = uint(u)
				}
				if url != "" {
					onDelete(url, uid)
				}
			}
		}
		count += n
		searchAfter = res.Hits[n-1].Sort
	}
	return count, nil
}

func (i *Indexer) Search(q *Query) (*Results, error) {
	return i.search(i.semanticConfig, q)
}

func (i *Indexer) search(semanticConfig config.SemanticSearch, q *Query) (*Results, error) {
	expression := querybuilder.ParseSearch(q.Text)
	if expression.HasSort {
		q.Sort = expression.Sort
	}
	req := bleve.NewSearchRequest(q.create(expression.Text))
	req.Fields = allFields

	if q.FacetsOnly {
		req.Size = 0
		req.Fields = nil
	} else if q.Limit > 0 {
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

	// TODO / question: should we store the length of the URL path and sort by it,
	// prefering shorter path names for tied score?
	sortDefinition := searchschema.Sort(q.Sort)
	sortByScore := sortDefinition.ByScore
	req.SortBy(sortDefinition.Fields)

	if q.PageKey != "" {
		var after []string
		if err := json.Unmarshal([]byte(q.PageKey), &after); err == nil {
			req.SetSearchAfter(after)
		}
	}

	if q.Facets {
		addFacets(req, q.FacetSizes)
	}

	res, err := i.searchIndexes(req)
	if err != nil {
		return nil, err
	}
	include := resultInclude(0)
	if q.IncludeText {
		include |= resultIncludeText
	}
	if q.IncludeHTML {
		include |= resultIncludeHTML
	}
	matches := make([]*document.Document, len(res.Hits))
	for j, v := range res.Hits {
		matches[j] = i.resFromHit(v, include)
	}
	r := &Results{
		Total:     res.Total,
		Query:     q,
		Documents: matches,
	}
	if q.Facets && len(res.Facets) > 0 {
		r.Facets = extractFacets(res.Facets)
	}
	if len(res.Hits) > 0 && req.Size > 0 && len(res.Hits) >= req.Size {
		lastHit := res.Hits[len(res.Hits)-1]
		lastSort := lastHit.Sort
		// https://github.com/blevesearch/bleve/issues/2308
		if sortByScore {
			for i, k := range lastSort {
				if k == "_score" {
					lastSort[i] = fmt.Sprintf("%v", lastHit.Score)
				}
			}
		}
		if pk, err := json.Marshal(lastSort); err == nil {
			r.PageKey = string(pk)
			q.PageKey = r.PageKey
		}
	}

	// Run semantic search if enabled and the embedding infrastructure is available.
	semanticText := querybuilder.RemoveStandaloneWildcards(expression.Text)
	if q.SemanticEnabled && i.embedder != nil && i.vectorStore != nil &&
		strings.TrimSpace(semanticText) != "" {
		r.SemanticEnabled = true
		vec, err := i.embedder.EmbedQuery(context.Background(), semanticText)
		if err != nil {
			log.Warn().Err(err).Msg("semantic query embedding failed")
		} else {
			threshold := q.SemanticThreshold
			if threshold <= 0 {
				threshold = semanticConfig.SimilarityThreshold
			}
			resultLimit := semanticConfig.ResultLimit
			vsResults, err := i.vectorStore.Search(vec, resultLimit, threshold, q.UserID)
			if err != nil {
				log.Warn().Err(err).Msg("vector store search failed")
			} else {
				// Build a set of URLs already in keyword results to avoid duplicating docs.
				keywordURLs := make(map[string]struct{}, len(matches))
				for _, d := range matches {
					keywordURLs[d.URL] = struct{}{}
				}
				// Aggregate chunk-level results by doc_id, keeping the best
				// similarity and the text of the best-matching chunk.
				type docHit struct {
					similarity float64
					chunkText  string
				}
				bestByDoc := make(map[string]*docHit)
				// Preserve insertion order for stable output.
				var docOrder []string
				for _, vr := range vsResults {
					if existing, ok := bestByDoc[vr.DocID]; ok {
						if vr.Similarity > existing.similarity {
							existing.similarity = vr.Similarity
							existing.chunkText = vr.ChunkText
						}
					} else {
						bestByDoc[vr.DocID] = &docHit{
							similarity: vr.Similarity,
							chunkText:  vr.ChunkText,
						}
						docOrder = append(docOrder, vr.DocID)
					}
				}
				for _, docID := range docOrder {
					dh := bestByDoc[docID]
					hit := SemanticHit{
						DocID:        docID,
						Similarity:   dh.similarity,
						MatchedChunk: truncateText(dh.chunkText, semanticTextPreviewLen),
					}
					// For semantic-only hits, populate the document with a truncated text preview.
					d := i.getByDocID(docID, resultIncludeText|resultIncludeHTML)
					if d != nil {
						if _, inKeyword := keywordURLs[d.URL]; !inKeyword {
							d.Text = truncateText(d.Text, semanticTextPreviewLen)
							hit.Document = d
						}
					}
					r.SemanticHits = append(r.SemanticHits, hit)
				}
			}
		}
	}

	// Bump the total to reflect semantic matches that are not in the
	// keyword results.
	semanticOnlyCnt := uint64(0)
	for _, sh := range r.SemanticHits {
		if sh.Document != nil {
			semanticOnlyCnt++
		}
	}
	r.Total = max(r.Total, uint64(len(r.Documents))+semanticOnlyCnt)

	return r, nil
}

// GetByURLAndUser returns the document at u owned by uid. The url field is
// shared across owners in multi-user mode, so callers must pass their own
// UserID to avoid returning another user's copy of the same URL. A uid of 0
// selects the global (single-user) owner; an instance that mixes uid-0 public
// docs with per-user private docs still gets the right one because the lookup
// goes through document.GetDocID.
func (i *Indexer) GetByURLAndUser(u string, uid uint) *document.Document {
	if uid > 0 {
		if d := i.GetByDocID(document.GetDocID(uid, u)); d != nil {
			return d
		}
	}
	// try to get the document with 0 UID if the document was not found for the > 0 UID
	return i.GetByDocID(document.GetDocID(0, u))
}

func (i *Indexer) GetAddCountByURLAndUser(u string, uid uint) uint {
	if uid > 0 {
		if count, found := i.getAddCountByDocID(document.GetDocID(uid, u)); found {
			return count
		}
	}
	count, _ := i.getAddCountByDocID(document.GetDocID(0, u))
	return count
}

func (i *Indexer) getAddCountByDocID(id string) (uint, bool) {
	q := bleve.NewDocIDQuery([]string{id})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"add_count"}
	i.indexesMu.RLock()
	req.Size = len(i.indexers) + 1
	res, err := i.idx.Search(req)
	i.indexesMu.RUnlock()
	if err != nil || len(res.Hits) < 1 {
		return 0, false
	}
	var maxCount uint = 1
	for _, h := range res.Hits {
		if n, ok := h.Fields["add_count"].(float64); ok {
			count := uint(n)
			if count > maxCount {
				maxCount = count
			}
		}
	}
	return maxCount, true
}

// GetByDocID returns the document with the given bleve document ID, or nil if
// none exists. The ID is the uid-prefixed form produced by document.GetDocID.
func (i *Indexer) GetByDocID(id string) *document.Document {
	return i.getByDocID(id, resultIncludeAll)
}

func (i *Indexer) getByDocID(id string, include resultInclude) *document.Document {
	q := bleve.NewDocIDQuery([]string{id})
	req := bleve.NewSearchRequest(q)
	req.Fields = allFields
	req.Highlight = bleve.NewHighlight()
	res, err := i.searchIndexes(req)
	if err != nil || len(res.Hits) < 1 {
		return nil
	}
	return i.resFromHit(res.Hits[0], include)
}

func (i *Indexer) Iterate(fn func(*document.Document)) {
	q := query.NewMatchAllQuery()
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"url"}
	req.Size = 200
	req.SortBy([]string{"_id"})
	var sortKey []string
	for {
		if len(sortKey) > 0 {
			req.SetSearchAfter(sortKey)
		}
		res, err := i.searchIndexes(req)
		n := len(res.Hits)
		if err != nil || n < 1 {
			return
		}
		for _, h := range res.Hits {
			d := i.resFromHit(h, resultIncludeAll)
			fn(d)
		}
		sortKey = res.Hits[n-1].Sort
	}
}

type resultInclude uint8

const (
	resultIncludeText resultInclude = 1 << iota
	resultIncludeHTML
	resultIncludeFavicon

	resultIncludeAll = resultIncludeText | resultIncludeHTML | resultIncludeFavicon
)

func (include resultInclude) has(flag resultInclude) bool {
	return include&flag != 0
}

func (idx *Indexer) resFromHit(h *search.DocumentMatch, include resultInclude) *document.Document {
	d := &document.Document{DocumentID: h.ID}
	if t, ok := h.Fragments["title"]; ok {
		d.Title = t[0]
	} else if s, ok := h.Fields["title"].(string); ok {
		d.Title = s
	}
	if s, ok := h.Fields["url"].(string); ok {
		d.URL = s
	}
	if include.has(resultIncludeText) {
		if s, ok := h.Fields["text"].(string); ok {
			d.Text = s
		}
	} else if t, ok := h.Fragments["text"]; ok {
		d.Text = t[0]
	}
	if s, ok := h.Fields["html_key"].(string); ok {
		d.HTMLKey = s
	}
	if s, ok := h.Fields["favicon_key"].(string); ok {
		d.FaviconKey = s
	}
	if include.has(resultIncludeHTML) {
		if d.HTMLKey != "" {
			data, err := idx.data.read(htmlSubdir, d.HTMLKey)
			if err != nil {
				log.Warn().Err(err).Str("key", d.HTMLKey).Msg("failed to load HTML from data store")
			} else {
				d.HTML = string(data)
			}
		} else if s, ok := h.Fields["html"].(string); ok {
			// backward compat: old documents still have HTML inline in Bleve
			d.HTML = s
		}
	}
	if include.has(resultIncludeFavicon) && d.FaviconKey != "" {
		data, err := idx.data.read(faviconSubdir, d.FaviconKey)
		if err != nil {
			log.Warn().Err(err).Str("key", d.FaviconKey).Msg("failed to load favicon from data store")
		} else {
			d.Favicon = string(data)
		}
	} else if d.FaviconKey == "" {
		if s, ok := h.Fields["favicon"].(string); ok {
			// backward compat: old documents still have favicon inline in Bleve
			d.Favicon = s
		}
	}
	if s, ok := h.Fields["domain"].(string); ok {
		d.Domain = s
	}
	if t, ok := h.Fields["added"].(float64); ok {
		d.Added = int64(t)
	}
	if t, ok := h.Fields["updated"].(float64); ok {
		d.Updated = int64(t)
	} else {
		d.Updated = d.Added
	}
	if t, ok := h.Fields["type"].(float64); ok {
		d.Type = types.DocType(t)
	}
	if t, ok := h.Fields["user_id"].(float64); ok {
		d.UserID = uint(t)
	}
	if s, ok := h.Fields["language"].(string); ok {
		d.Language = s
	}
	if s, ok := h.Fields["label"].(string); ok {
		d.Label = s
	}
	if n, ok := h.Fields["add_count"].(float64); ok {
		d.AddCount = uint(n)
	}
	if d.AddCount < 1 {
		d.AddCount = 1
	}
	d.Score = h.Score
	for k, v := range h.Fields {
		if mk, found := strings.CutPrefix(k, "metadata."); found {
			if d.Metadata == nil {
				d.Metadata = make(map[string]any)
			}
			d.Metadata[mk] = v
		}
	}
	return d
}

func (q *Query) create(text string) query.Query {
	var sq query.Query
	if q.MatchAll {
		sq = query.NewMatchAllQuery()
	} else {
		sq = querybuilder.Build(text)
	}

	if q.DateFrom != 0 && q.DateTo == 0 {
		q.DateTo = time.Now().Unix()
	}
	if dateQuery, ok := q.legacyDateFilterQuery(); ok {
		sq = bleve.NewConjunctionQuery(sq, dateQuery)
	}

	if q.UserID > 0 {
		uid := float64(q.UserID)
		userQuery := bleve.NewNumericRangeInclusiveQuery(&uid, &uid, new(true), new(true))
		userQuery.SetField("user_id")
		// userid 0 is preserved for global results
		zeroF := float64(0)
		globalQuery := bleve.NewNumericRangeInclusiveQuery(&zeroF, &zeroF, new(true), new(true))
		globalQuery.SetField("user_id")
		userOrGlobal := bleve.NewDisjunctionQuery(userQuery, globalQuery)
		sq = bleve.NewConjunctionQuery(sq, userOrGlobal)
	}

	if !q.MatchAll && len(q.PriorityPatterns) > 0 {
		bq := query.NewBooleanQuery([]query.Query{sq}, nil, nil)
		for _, p := range q.PriorityPatterns {
			if p == "" {
				continue
			}
			rq := bleve.NewRegexpQuery(p)
			rq.SetField("url")
			rq.SetBoost(100)
			bq.AddShould(rq)
		}
		return bq
	}

	return sq
}

func (q *Query) legacyDateFilterQuery() (query.Query, bool) {
	if q.DateFrom == 0 && q.DateTo == 0 {
		return nil, false
	}
	var min, max *int64
	if q.DateFrom != 0 {
		min = new(int64)
		*min = q.DateFrom
	}
	if q.DateTo != 0 {
		max = new(int64)
		*max = q.DateTo
	}
	return querybuilder.BuildTimestampRange("updated", min, max, true, true)
}

func createMapping(lang string, keepStopwords bool) mapping.IndexMapping {
	im := bleve.NewIndexMapping()
	textAnalyzer := analyzerForLanguage(lang)
	addDefaultAnalyzer := func() {
		if err := im.AddCustomAnalyzer("default", map[string]any{
			"type":         custom.Name,
			"char_filters": []string{},
			"tokenizer":    unicode.Name,
			"token_filters": []string{
				lowercase.Name,
			},
		}); err != nil {
			panic(err)
		}
	}
	if lang == document.UnknownLanguage || lang == "" || lang == "default" {
		addDefaultAnalyzer()
		textAnalyzer = "default"
	} else if keepStopwords {
		if im.AnalyzerNamed(textAnalyzer) == nil {
			log.Warn().Str("language", lang).Msg("Language analyzer unavailable, using default analyzer")
			addDefaultAnalyzer()
			textAnalyzer = "default"
		} else {
			languageAnalyzer := textAnalyzer
			textAnalyzer = languageAnalyzer + "_keep_stopwords"
			if err := im.AddCustomAnalyzer(textAnalyzer, map[string]any{
				"type":     keepStopwordsAnalyzerType,
				"language": languageAnalyzer,
			}); err != nil {
				panic(err)
			}
		}
	}
	err := im.AddCustomAnalyzer("url", map[string]any{
		"type":          custom.Name,
		"char_filters":  []string{},
		"tokenizer":     single.Name,
		"token_filters": []string{
			// lowercase.Name,
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
	docMapping.AddFieldMappingsAt("label", um)
	docMapping.AddFieldMappingsAt("url", um)
	docMapping.AddFieldMappingsAt("domain", um)
	docMapping.AddFieldMappingsAt("language", um)
	docMapping.AddFieldMappingsAt("favicon", noIdxMap)
	docMapping.AddFieldMappingsAt("favicon_key", um)
	docMapping.AddFieldMappingsAt("html", noIdxMap)
	docMapping.AddFieldMappingsAt("html_key", um)
	docMapping.AddFieldMappingsAt("metadata", noIdxMap)
	ignoredTextMap := bleve.NewTextFieldMapping()
	ignoredTextMap.Store = false
	ignoredTextMap.Index = false
	ignoredTextMap.IncludeTermVectors = false
	ignoredTextMap.IncludeInAll = false
	ignoredTextMap.DocValues = false
	docMapping.AddFieldMappingsAt("id", ignoredTextMap)
	noStoreMap := bleve.NewBooleanFieldMapping()
	noStoreMap.Store = false
	noStoreMap.Index = false
	noStoreMap.IncludeInAll = false
	noStoreMap.DocValues = false
	docMapping.AddFieldMappingsAt("processed", noStoreMap)
	numMap := bleve.NewNumericFieldMapping()
	numMap.Store = true
	docMapping.AddFieldMappingsAt("added", numMap)
	docMapping.AddFieldMappingsAt("updated", numMap)
	docMapping.AddFieldMappingsAt("add_count", numMap)
	docMapping.AddFieldMappingsAt("type", numMap)
	docMapping.AddFieldMappingsAt("user_id", numMap)

	im.DefaultMapping = docMapping

	return im
}

func (q *Query) ToJSON() []byte {
	r, _ := json.Marshal(q)
	return r
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

func bleveRuntimeConfig() map[string]any {
	c := make(map[string]any, len(bleveConfig))
	maps.Copy(c, bleveConfig)
	return c
}
