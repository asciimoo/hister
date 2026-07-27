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
	"sync/atomic"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/extractor"
	"github.com/asciimoo/hister/server/indexer/querybuilder"
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

type indexer struct {
	idx               bleve.IndexAlias       // used only for Search()
	indexers          map[string]bleve.Index // default and language specific indexers
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
	// FacetSizes overrides the default top-N cap per named facet.
	// Key is the facet name (e.g. "domains", "languages"); zero/missing
	// values fall back to defaultFacetTermSize.
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
	cfg              *config.Config
}

const defaultFacetTermSize = 10

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

// dateFacetBuckets drives the "updated" histogram. Each entry is a non-
// overlapping slice of time ending at the previous bucket's boundary; the
// final "older" bucket is appended implicitly. Order matters, the loop
// walks most-recent -> oldest so each range's upper bound is the prior
// range's lower bound.
var dateFacetBuckets = []struct {
	name string
	age  time.Duration
}{
	{"last_24h", 24 * time.Hour},
	{"last_7d", 7 * 24 * time.Hour},
	{"last_30d", 30 * 24 * time.Hour},
	{"last_year", 365 * 24 * time.Hour},
}

var visitCountFacetBuckets = []struct {
	name  string
	label string
	min   *float64
	max   *float64
}{
	{"1", "1 visit", new(float64(1)), new(float64(2))},
	{"2..4", "2 to 4", new(float64(2)), new(float64(5))},
	{"5..9", "5 to 9", new(float64(5)), new(float64(10))},
	{"10..", "10 or more", new(float64(10)), nil},
}

func addFacets(req *bleve.SearchRequest, sizes map[string]int) {
	facetSize := func(name string) int {
		if n := sizes[name]; n > 0 {
			return n
		}
		return defaultFacetTermSize
	}
	req.AddFacet("domains", bleve.NewFacetRequest("domain", facetSize("domains")))
	req.AddFacet("languages", bleve.NewFacetRequest("language", facetSize("languages")))
	tf := bleve.NewFacetRequest("type", 2)
	web, local, afterLocal := float64(types.Web), float64(types.Local), float64(types.Local)+1
	tf.AddNumericRange(types.Web.String(), &web, &local)
	tf.AddNumericRange(types.Local.String(), &local, &afterLocal)
	req.AddFacet("types", tf)
	vf := bleve.NewFacetRequest("add_count", len(visitCountFacetBuckets))
	for _, b := range visitCountFacetBuckets {
		vf.AddNumericRange(b.name, b.min, b.max)
	}
	req.AddFacet("visits", vf)
	now := time.Now()
	dh := bleve.NewFacetRequest("updated", len(dateFacetBuckets)+1)
	var prev *float64
	for _, b := range dateFacetBuckets {
		ts := float64(now.Add(-b.age).Unix())
		dh.AddNumericRange(b.name, &ts, prev)
		prev = &ts
	}
	dh.AddNumericRange("older", nil, prev)
	req.AddFacet("updated", dh)
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

func visitCountFacetLabel(name string) string {
	for _, b := range visitCountFacetBuckets {
		if b.name == name {
			return b.label
		}
	}
	return ""
}

func extractFacets(facets search.FacetResults) *FacetsResult {
	fr := &FacetsResult{Terms: make(map[string]TermFacet)}
	for _, name := range []string{"domains", "languages"} {
		if f := facets[name]; f != nil {
			fr.Terms[name] = extractTermFacet(f)
		}
	}
	if f := facets["types"]; f != nil {
		terms := make([]TermCount, 0, len(f.NumericRanges))
		for _, nr := range f.NumericRanges {
			terms = append(terms, TermCount{Term: nr.Name, Count: nr.Count})
		}
		fr.Terms["types"] = TermFacet{Terms: terms}
	}
	if f := facets["visits"]; f != nil {
		terms := make([]TermCount, 0, len(f.NumericRanges))
		for _, nr := range f.NumericRanges {
			terms = append(terms, TermCount{
				Term:  nr.Name,
				Count: nr.Count,
				Label: visitCountFacetLabel(nr.Name),
			})
		}
		fr.Terms["visits"] = TermFacet{Terms: terms}
	}
	if f := facets["updated"]; f != nil {
		for _, nr := range f.NumericRanges {
			fr.DateHistogram = append(fr.DateHistogram, RangeCount{Name: nr.Name, Count: nr.Count})
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

type batchKeyChange struct {
	oldHTMLKey    string
	newHTMLKey    string
	oldFaviconKey string
	newFaviconKey string
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
	indexer           *indexer
	batches           map[string]*bleve.Batch
	keyChanges        []batchKeyChange
	embeddingIDs      map[string]struct{}
	deletedIDs        map[string]struct{}
	incrementAddCount bool
}

var (
	i *indexer
	// allFields      []string       = []string{"url", "title", "text", "favicon", "html", "domain", "added", "updated", "type", "user_id"}
	allFields      []string       = []string{"*"}
	ErrEmptyFilter                = errors.New("delete query must not be empty")
	bleveConfig    map[string]any = map[string]any{
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

func Init(cfg *config.Config) error {
	if cfg.Indexer.MaxFileSize > 0 {
		maxFileSize = cfg.Indexer.MaxFileSize * 1024 * 1024 // bytes
	}
	sp := make([]string, 0, len(cfg.SensitiveContentPatterns))
	for _, v := range cfg.SensitiveContentPatterns {
		sp = append(sp, v)
	}
	document.SetSensitiveContentPattern(regexp.MustCompile(fmt.Sprintf("(%s)", strings.Join(sp, "|"))))
	var err error
	i, err = initializeIndexer(cfg.FullPath(""), cfg.Indexer.DetectLanguages, cfg.Indexer.KeepStopwords)
	if err != nil {
		return err
	}
	i.disablePreviews = cfg.App.DisablePreviews
	if cfg.SemanticSearch.Enable {
		vs, err := vectorstore.New(cfg)
		if err != nil {
			log.Warn().Err(err).Msg("failed to create vector store, semantic search disabled")
		} else if err := vs.Init(); err != nil {
			log.Warn().Err(err).Msg("failed to init vector store, semantic search disabled")
		} else {
			workers := normalizeEmbeddingWorkerCount(cfg.SemanticSearch.MaxEmbeddingConcurrency)
			embedCfg := cfg.SemanticSearch
			embedCfg.MaxEmbeddingConcurrency = workers
			i.vectorStore = vs
			i.embedder = vectorstore.NewEmbedder(&embedCfg)
			if err := i.startEmbeddingQueue(workers); err != nil {
				i.vectorStore = nil
				i.embedder = nil
				if closeErr := vs.Close(); closeErr != nil {
					log.Warn().Err(closeErr).Msg("failed to close vector store")
				}
				i.Close()
				return fmt.Errorf("start embedding queue: %w", err)
			}
			log.Info().Msg("semantic search enabled")
		}
	}
	if err := registry.RegisterHighlighter("ansi", invertedAnsiHighlighter); err != nil {
		if !strings.Contains(err.Error(), "duplicate highlighter") {
			return err
		}
	}
	if err := registry.RegisterHighlighter("tui", tuiHighlighter); err != nil {
		if !strings.Contains(err.Error(), "duplicate highlighter") {
			return err
		}
	}
	return nil
}

func initializeIndexer(basePath string, detectLanguages, keepStopwords bool) (*indexer, error) {
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
	i := &indexer{
		idx: bleve.NewIndexAlias(idx),
		indexers: map[string]bleve.Index{
			defaultIndexerName: idx,
		},
		dir:           basePath,
		keepStopwords: keepStopwords,
		embedCtx:      embedCtx,
		embedCancel:   embedCancel,
		data:          newDataStore(filepath.Join(basePath, dataDirName)),
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
		i.langDetector = document.NewLanguageDetector()
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
		i.idx.Add(langIdx)
		i.indexers[fn] = langIdx
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
func (i *indexer) backfillUpdatedTimestamps() error {
	for name, idx := range i.indexers {
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

func Reindex(basePath string, rules *config.Rules, skipSensitiveChecks bool, detectLanguages, keepStopwords bool, dirs []*config.Directory) (retErr error) {
	// TODO store new documents in both indexes while running reindex to guarantee not losing any data.
	if !i.reindexInProgress.CompareAndSwap(false, true) {
		return errors.New("Reindex is already running")
	}
	idx := i
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
	sourceIndexes, closeExtraSources, err := openReindexSources(basePath, idx.indexers)
	if err != nil {
		return err
	}
	defer closeExtraSources()
	tmpIdx, err := initializeIndexer(tmpBasePath, detectLanguages, keepStopwords)
	if err != nil {
		return err
	}
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
					pu, err := url.Parse(d.URL)
					if err == nil {
						if _, err := os.Stat(pu.Path); errors.Is(err, os.ErrNotExist) {
							log.Warn().Str("URL", d.URL).Msg("Skipping document, file not found")
							continue
						}
						if files.FindMatchingDir(dirs, pu.Path) == nil {
							log.Warn().Str("URL", d.URL).Msg("Skipping document, directory no longer configured")
							continue
						}
					}
				}
				log.Debug().Str("URL", d.URL).Msg("Indexing")
				d.SetSkipSensitiveCheck(skipSensitiveChecks)
				origAdded := d.Added
				origUpdated := d.Updated
				if err := d.Process(tmpIdx.langDetector, extractor.Extract); err != nil {
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
	for n := range tmpIdx.indexers {
		idxPath := filepath.Join(basePath, n)
		tmpIdxPath := filepath.Join(tmpBasePath, n)
		if err := os.Rename(tmpIdxPath, idxPath); err != nil {
			renameError = err
		}
	}
	if renameError != nil {
		return errors.New("failed to rename tmp indexes during the reindex, resolve the issue manually")
	}
	i, err = initializeIndexer(basePath, detectLanguages, keepStopwords)
	if err != nil {
		return err
	}
	// Restore settings that are not part of the index state.
	i.disablePreviews = idx.disablePreviews
	// Restore the vector store and embedder on the newly initialized global indexer.
	if vs != nil && embedder != nil {
		i.vectorStore = vs
		i.embedder = embedder
		if workers > 0 {
			if err := i.startEmbeddingQueue(workers); err != nil {
				return fmt.Errorf("restart embedding queue: %w", err)
			}
			queueStopped = false
		}
	}
	if err := os.RemoveAll(tmpBasePath); err != nil {
		return err
	}
	// Remove data files no longer referenced by any document.
	if _, err := i.data.cleanup("html_key", htmlSubdir, i.countKeyRefs); err != nil {
		log.Warn().Err(err).Msg("failed to clean up orphaned HTML data files")
	}
	if _, err := i.data.cleanup("favicon_key", faviconSubdir, i.countKeyRefs); err != nil {
		log.Warn().Err(err).Msg("failed to clean up orphaned favicon data files")
	}
	return nil
}

// CleanupDataFiles removes orphaned HTML and favicon files from the data
// directories (files that exist on disk but are no longer referenced by any
// document in the index). It returns the number of HTML and favicon files
// removed, and any walk error encountered.
// Each candidate file is checked with a live ref-count query while holding the
// per-key shard lock, so the check and the removal are atomic and safe against
// concurrent /api/add calls.
func CleanupDataFiles(basePath string) (int, int, error) {
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

func ReadFavicon(key string) ([]byte, error) {
	if i == nil {
		return nil, errors.New("indexer is not initialized")
	}
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

func DocumentCount() uint64 {
	return i.Total()
}

func DocumentCountByUser(userID uint) uint64 {
	return i.TotalByUser(userID)
}

// SemanticSearchEnabled reports whether the vector store and embedder are active.
func SemanticSearchEnabled() bool {
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
func embedDocumentChunks(ctx context.Context, idx *indexer, d *document.Document) error {
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

func Add(d *document.Document) error {
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

func (i *indexer) TotalByUser(userID uint) uint64 {
	uid := float64(userID)
	q := bleve.NewNumericRangeInclusiveQuery(&uid, &uid, new(true), new(true))
	q.SetField("user_id")
	req := bleve.NewSearchRequest(q)
	req.Size = 1
	res, err := i.idx.Search(req)
	if err != nil {
		return 0
	}
	return res.Total
}

func (i *indexer) AddDocument(d *document.Document) error {
	return i.addDocument(d, true)
}

func (i *indexer) addDocument(d *document.Document, incrementAddCount bool) error {
	if !d.IsProcessed() {
		if err := d.Process(i.langDetector, extractor.Extract); err != nil {
			return err
		}
	}
	if !d.SkipIndexing {
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
		if err := i.saveWithState(d, state); err != nil {
			return err
		}
		if needsEmbedding {
			if err := i.enqueueEmbedding(d.ID()); err != nil {
				return fmt.Errorf("enqueue embedding: %w", err)
			}
		}
	}
	for _, extra := range d.ExtraDocuments {
		if err := i.addDocument(extra, false); err != nil {
			log.Warn().Err(err).Str("url", extra.URL).Msg("failed to index extra document")
		}
	}
	return nil
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

func (i *indexer) save(d *document.Document) error {
	return i.saveWithState(d, i.getStoredDocumentState(d.ID()))
}

func (i *indexer) saveWithState(d *document.Document, state storedDocumentState) error {
	existingIndexes := state.indexNames
	if existingIndexes == nil {
		existingIndexes = make(map[string]struct{}, len(i.indexers))
		for name := range i.indexers {
			existingIndexes[name] = struct{}{}
		}
	}
	if err := i.prepareForStorage(d); err != nil {
		return err
	}
	log.Debug().Str("URL", d.URL).Msg("item added to index")
	targetIdx := i.getOrCreate(d.Language)
	if err := targetIdx.Index(d.ID(), d); err != nil {
		return err
	}
	for name := range existingIndexes {
		if name == targetIdx.Name() {
			continue
		}
		idx, ok := i.indexers[name]
		if !ok {
			continue
		}
		if err := idx.Delete(d.ID()); err != nil {
			return err
		}
	}
	for _, k := range state.htmlKeys {
		if k != d.HTMLKey {
			i.data.deleteIfOrphaned("html_key", htmlSubdir, k, i.countKeyRefs)
		}
	}
	for _, k := range state.faviconKeys {
		if k != d.FaviconKey {
			i.data.deleteIfOrphaned("favicon_key", faviconSubdir, k, i.countKeyRefs)
		}
	}
	return nil
}

// getStoredDocumentState fetches the fields needed to update an existing
// document. The same document can appear in more than one index when its
// detected language changes between additions.
func (i *indexer) getStoredDocumentState(id string) storedDocumentState {
	var state storedDocumentState
	q := bleve.NewDocIDQuery([]string{id})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"html_key", "favicon_key", "language", "add_count", "label", "added"}
	if i.embedder != nil && i.vectorStore != nil {
		req.Fields = append(req.Fields, "text")
	}
	req.Size = len(i.indexers) + 1 // at most one entry per index
	res, err := i.idx.Search(req)
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

func (i *indexer) getDocKeysByID(id string) (htmlKeys, faviconKeys []string) {
	state := i.getStoredDocumentState(id)
	return state.htmlKeys, state.faviconKeys
}

// countKeyRefs returns the number of indexed documents that reference the
// given key in the specified field (html_key or favicon_key).
// Returns 1 on search error as a safe default to avoid accidental deletion.
func (i *indexer) countKeyRefs(field, key string) uint64 {
	q := bleve.NewTermQuery(key)
	q.SetField(field)
	req := bleve.NewSearchRequest(q)
	req.Size = 0
	res, err := i.idx.Search(req)
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
func (i *indexer) prepareForStorage(d *document.Document) error {
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
func Save(d *document.Document) error {
	return i.save(d)
}

func GetLatestDocuments(limit int, latest string, userID uint) *Results {
	return GetLatestDocumentsFiltered(limit, latest, userID, "")
}

func GetLatestDocumentsFiltered(limit int, latest string, userID uint, filter string) *Results {
	if i == nil {
		return nil
	}
	var q query.Query
	if userID > 0 {
		uid := float64(userID)
		userQuery := bleve.NewNumericRangeInclusiveQuery(&uid, &uid, new(true), new(true))
		userQuery.SetField("user_id")
		zeroF := float64(0)
		globalQuery := bleve.NewNumericRangeInclusiveQuery(&zeroF, &zeroF, new(true), new(true))
		globalQuery.SetField("user_id")
		q = bleve.NewDisjunctionQuery(userQuery, globalQuery)
	} else {
		q = query.NewMatchAllQuery()
	}
	if filter = strings.TrimSpace(filter); filter != "" {
		pattern := "(?i).*" + regexp.QuoteMeta(filter) + ".*"
		urlQuery := bleve.NewRegexpQuery(pattern)
		urlQuery.SetField("url")
		titlePhraseQuery := bleve.NewMatchPhraseQuery(filter)
		titlePhraseQuery.SetField("title")
		filterQueries := []query.Query{urlQuery, titlePhraseQuery}
		if !strings.ContainsAny(filter, " \t\r\n") {
			titleRegexpQuery := bleve.NewRegexpQuery(pattern)
			titleRegexpQuery.SetField("title")
			filterQueries = append(filterQueries, titleRegexpQuery)
		}
		q = bleve.NewConjunctionQuery(q, bleve.NewDisjunctionQuery(filterQueries...))
	}
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"url", "title", "added", "updated", "add_count", "favicon_key", "favicon"}
	req.Size = limit
	req.SortByCustom(search.SortOrder{
		&search.SortField{
			Field: "updated",
			Desc:  true,
		},
	})
	if latest != "" {
		var after []string
		if err := json.Unmarshal([]byte(latest), &after); err == nil {
			req.SetSearchAfter(after)
		}
	}
	res, err := i.idx.Search(req)
	if err != nil || len(res.Hits) < 1 {
		return nil
	}
	docs := make([]*document.Document, len(res.Hits))
	for i, h := range res.Hits {
		d := &document.Document{
			Title: h.Fields["title"].(string),
			URL:   h.Fields["url"].(string),
		}
		if n, ok := h.Fields["added"].(float64); ok {
			d.Added = int64(n)
		}
		if n, ok := h.Fields["updated"].(float64); ok {
			d.Updated = int64(n)
		} else {
			d.Updated = d.Added
		}
		if n, ok := h.Fields["add_count"].(float64); ok {
			d.AddCount = uint(n)
		}
		if s, ok := h.Fields["favicon_key"].(string); ok {
			d.FaviconKey = s
		} else if s, ok := h.Fields["favicon"].(string); ok {
			// backward compat: old documents still have favicon inline in Bleve
			d.Favicon = s
		}
		if d.AddCount < 1 {
			d.AddCount = 1
		}
		docs[i] = d
	}
	r := &Results{Documents: docs}
	if pk, err := json.Marshal(res.Hits[len(res.Hits)-1].Sort); err == nil {
		r.PageKey = string(pk)
	}
	return r
}

func (i *indexer) getOrCreate(lang string) bleve.Index {
	idxName := indexNameForLanguage(lang)
	if idxName == defaultIndexerName {
		return i.indexers[defaultIndexerName]
	}
	idx, ok := i.indexers[idxName]
	if !ok {
		err := i.addIndexer(idxName, lang)
		if err != nil {
			log.Warn().Err(err).Str("Name", idxName).Msg("Failed to create language indexer")
			return i.indexers[defaultIndexerName]
		}
		idx = i.indexers[idxName]
	}
	return idx
}

func indexNameForLanguage(lang string) string {
	if lang == document.UnknownLanguage || lang == "" {
		return defaultIndexerName
	}
	return fmt.Sprintf(langIndexerName, lang)
}

func (i *indexer) addIndexer(name, lang string) error {
	mapping := createMapping(lang, i.keepStopwords)
	indexPath := filepath.Join(i.dir, name)
	idx, err := bleve.NewUsing(indexPath, mapping, bleve.Config.DefaultIndexType, bleve.Config.DefaultMemKVStore, bleveRuntimeConfig())
	if err != nil {
		return err
	}
	idx.SetName(name)
	if err := copyIndexMetadata(i.indexers[defaultIndexerName], idx); err != nil {
		closeErr := idx.Close()
		removeErr := os.RemoveAll(indexPath)
		return errors.Join(err, closeErr, removeErr)
	}
	i.indexers[name] = idx
	i.idx.Add(idx)
	return nil
}

func (i *indexer) Close() {
	i.stopEmbeddingQueue()
	if i.embedCancel != nil {
		i.embedCancel()
	}
	if i.vectorStore != nil {
		if err := i.vectorStore.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close vector store")
		}
	}
	for name, idx := range i.indexers {
		if err := idx.Close(); err != nil {
			log.Warn().Err(err).Str("index", name).Msg("failed to close index")
		}
	}
	if err := i.idx.Close(); err != nil {
		log.Warn().Err(err).Msg("failed to close index alias")
	}
}

func NewMultiBatch() *MultiBatch {
	b := newMultiBatch(i)
	b.incrementAddCount = true
	return b
}

func newMultiBatch(idx *indexer) *MultiBatch {
	return &MultiBatch{
		indexer:           idx,
		batches:           make(map[string]*bleve.Batch),
		embeddingIDs:      make(map[string]struct{}),
		deletedIDs:        make(map[string]struct{}),
		incrementAddCount: false,
	}
}

func (b *MultiBatch) getOrCreateBatch(name string, idx bleve.Index) *bleve.Batch {
	if _, ok := b.batches[name]; !ok {
		b.batches[name] = idx.NewBatch()
	}
	return b.batches[name]
}

func (b *MultiBatch) Add(d *document.Document) error {
	return b.add(d, b.incrementAddCount)
}

func (b *MultiBatch) add(d *document.Document, incrementAddCount bool) error {
	if !d.IsProcessed() {
		if err := d.Process(i.langDetector, extractor.Extract); err != nil {
			return err
		}
	}
	if !d.SkipIndexing {
		state := b.indexer.getStoredDocumentState(d.ID())
		needsEmbedding := b.indexer.embedder != nil && b.indexer.vectorStore != nil && state.embeddingTextChanged(d.Text)
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
		delete(b.deletedIDs, d.ID())
		if needsEmbedding {
			if b.indexer.embeddingWorkers > 0 {
				b.embeddingIDs[d.ID()] = struct{}{}
			} else {
				_ = embedDocumentChunks(b.indexer.embedCtx, b.indexer, d)
			}
		}
		if err := b.indexer.prepareForStorage(d); err != nil {
			return err
		}
		idx := b.indexer.getOrCreate(d.Language)
		for name, subIdx := range b.indexer.indexers {
			if name == idx.Name() {
				continue
			}
			b.getOrCreateBatch(name, subIdx).Delete(d.ID())
		}
		if err := b.getOrCreateBatch(idx.Name(), idx).Index(d.ID(), d); err != nil {
			return err
		}
		for _, k := range state.htmlKeys {
			if k != d.HTMLKey {
				b.keyChanges = append(b.keyChanges, batchKeyChange{oldHTMLKey: k, newHTMLKey: d.HTMLKey})
			}
		}
		for _, k := range state.faviconKeys {
			if k != d.FaviconKey {
				b.keyChanges = append(b.keyChanges, batchKeyChange{oldFaviconKey: k, newFaviconKey: d.FaviconKey})
			}
		}
	}
	for _, extra := range d.ExtraDocuments {
		if err := b.add(extra, false); err != nil {
			log.Warn().Err(err).Str("url", extra.URL).Msg("failed to index extra document")
		}
	}
	return nil
}

func (b *MultiBatch) Delete(id string) {
	oldHTMLKeys, oldFaviconKeys := b.indexer.getDocKeysByID(id)
	delete(b.embeddingIDs, id)
	b.deletedIDs[id] = struct{}{}
	for name, idx := range b.indexer.indexers {
		b.getOrCreateBatch(name, idx).Delete(id)
	}
	for _, k := range oldHTMLKeys {
		b.keyChanges = append(b.keyChanges, batchKeyChange{oldHTMLKey: k})
	}
	for _, k := range oldFaviconKeys {
		b.keyChanges = append(b.keyChanges, batchKeyChange{oldFaviconKey: k})
	}
}

func (b *MultiBatch) Save() error {
	for name, lb := range b.batches {
		if err := b.indexer.indexers[name].Batch(lb); err != nil {
			return err
		}
	}
	for _, kc := range b.keyChanges {
		if kc.oldHTMLKey != "" && kc.oldHTMLKey != kc.newHTMLKey {
			b.indexer.data.deleteIfOrphaned("html_key", htmlSubdir, kc.oldHTMLKey, b.indexer.countKeyRefs)
		}
		if kc.oldFaviconKey != "" && kc.oldFaviconKey != kc.newFaviconKey {
			b.indexer.data.deleteIfOrphaned("favicon_key", faviconSubdir, kc.oldFaviconKey, b.indexer.countKeyRefs)
		}
	}
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

func Delete(id string) error {
	htmlKeys, faviconKeys := i.getDocKeysByID(id)
	for _, idx := range i.indexers {
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

func DeleteByQuery(text string, userID *uint, onDelete func(url string, userID uint)) (int, error) {
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
		res, err := i.idx.Search(req)
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

func Search(cfg *config.Config, q *Query) (*Results, error) {
	q.cfg = cfg
	req := bleve.NewSearchRequest(q.create())
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

	sortByScore := false
	// TODO / question: should we store the length of the URL path and sort by it,
	// prefering shorter path names for tied score?
	switch q.Sort {
	case "domain":
		req.SortBy([]string{"domain", "_id"})
	case "date":
		req.SortBy([]string{"-updated", "_id"})
	case "visits":
		req.SortBy([]string{"-add_count", "-updated", "_id"})
	default:
		sortByScore = true
		req.SortBy([]string{"-_score", "-updated", "_id"})
	}

	if q.PageKey != "" {
		var after []string
		if err := json.Unmarshal([]byte(q.PageKey), &after); err == nil {
			req.SetSearchAfter(after)
		}
	}

	if q.Facets {
		addFacets(req, q.FacetSizes)
	}

	res, err := i.idx.Search(req)
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
	semanticText := querybuilder.RemoveStandaloneWildcards(q.Text)
	if q.SemanticEnabled && i.embedder != nil && i.vectorStore != nil &&
		strings.TrimSpace(semanticText) != "" {
		r.SemanticEnabled = true
		vec, err := i.embedder.EmbedQuery(context.Background(), semanticText)
		if err != nil {
			log.Warn().Err(err).Msg("semantic query embedding failed")
		} else {
			threshold := q.SemanticThreshold
			if threshold <= 0 {
				threshold = cfg.SemanticSearch.SimilarityThreshold
			}
			resultLimit := cfg.SemanticSearch.ResultLimit
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
func GetByURLAndUser(u string, uid uint) *document.Document {
	if uid > 0 {
		if d := GetByDocID(document.GetDocID(uid, u)); d != nil {
			return d
		}
	}
	// try to get the document with 0 UID if the document was not found for the > 0 UID
	return GetByDocID(document.GetDocID(0, u))
}

func GetAddCountByURLAndUser(u string, uid uint) uint {
	if i == nil {
		return 0
	}
	if uid > 0 {
		if count, found := i.getAddCountByDocID(document.GetDocID(uid, u)); found {
			return count
		}
	}
	count, _ := i.getAddCountByDocID(document.GetDocID(0, u))
	return count
}

func (i *indexer) getAddCountByDocID(id string) (uint, bool) {
	q := bleve.NewDocIDQuery([]string{id})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"add_count"}
	req.Size = len(i.indexers) + 1
	res, err := i.idx.Search(req)
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
func GetByDocID(id string) *document.Document {
	return i.getByDocID(id, resultIncludeAll)
}

func (i *indexer) getByDocID(id string, include resultInclude) *document.Document {
	q := bleve.NewDocIDQuery([]string{id})
	req := bleve.NewSearchRequest(q)
	req.Fields = allFields
	req.Highlight = bleve.NewHighlight()
	res, err := i.idx.Search(req)
	if err != nil || len(res.Hits) < 1 {
		return nil
	}
	return i.resFromHit(res.Hits[0], include)
}

func Iterate(fn func(*document.Document)) {
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
		res, err := i.idx.Search(req)
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

func (idx *indexer) resFromHit(h *search.DocumentMatch, include resultInclude) *document.Document {
	d := &document.Document{}
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

func (q *Query) create() query.Query {
	var sq query.Query
	if q.MatchAll {
		sq = query.NewMatchAllQuery()
	} else {
		sq = querybuilder.Build(q.Text)
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
