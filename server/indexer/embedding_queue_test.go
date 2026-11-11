// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"
	servertypes "github.com/asciimoo/hister/server/types"
	"github.com/asciimoo/hister/server/vectorstore"
)

func TestLanguageOverridesRefreshEmbeddingContext(t *testing.T) {
	var requests atomic.Int64
	server := embeddingTestServer(t, &requests)
	defer server.Close()
	cfg := testutil.Config(t)
	cfg.Indexer.DetectLanguages = false
	cfg.Server.Database = "hister-test.sqlite3"
	cfg.SemanticSearch.Enable = true
	cfg.SemanticSearch.EmbeddingEndpoint = server.URL
	cfg.SemanticSearch.EmbeddingModel = "test"
	cfg.SemanticSearch.Dimensions = 2
	cfg.SemanticSearch.MaxContextLength = 512
	cfg.SemanticSearch.MaxEmbeddingConcurrency = 1
	testutil.InitModelWithConfig(t, cfg)
	idx := newTestIndexer(t, cfg)
	defer idx.Close()
	const url = "https://example.com/language-context"
	if err := idx.Add(&document.Document{URL: url, Text: "original text", Language: "en", Processed: true}); err != nil {
		t.Fatal(err)
	}
	waitForEmbeddingJobs(t, &requests, 1)

	// Pinning the already stored language only changes provenance.
	if _, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Language: new("en")}, false); err != nil {
		t.Fatal(err)
	}
	waitForEmbeddingJobs(t, &requests, 1)
	if _, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Language: new("de")}, false); err != nil {
		t.Fatal(err)
	}
	waitForEmbeddingJobs(t, &requests, 2)
	if err := idx.Add(&document.Document{URL: url, Text: "original text", Language: "en", Processed: true}); err != nil {
		t.Fatal(err)
	}
	waitForEmbeddingJobs(t, &requests, 2)

	if _, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Language: new("auto")}, false); err != nil {
		t.Fatal(err)
	}
	waitForEmbeddingJobs(t, &requests, 3)
	if err := idx.Add(&document.Document{URL: url, Text: "original text", LanguageOverride: "en", Processed: true}); err != nil {
		t.Fatal(err)
	}
	waitForEmbeddingJobs(t, &requests, 4)
}

func TestNormalizingLegacyLanguageRefreshesEmbeddingContext(t *testing.T) {
	var requests atomic.Int64
	server := embeddingTestServer(t, &requests)
	defer server.Close()
	cfg := testutil.Config(t)
	cfg.Indexer.DetectLanguages = false
	cfg.SemanticSearch.Enable = true
	cfg.SemanticSearch.EmbeddingEndpoint = server.URL
	cfg.SemanticSearch.EmbeddingModel = "test"
	cfg.SemanticSearch.Dimensions = 2
	cfg.SemanticSearch.MaxContextLength = 512
	cfg.SemanticSearch.MaxEmbeddingConcurrency = 1
	testutil.InitModelWithConfig(t, cfg)
	idx := newTestIndexer(t, cfg)
	defer idx.Close()
	legacy := &document.Document{URL: "https://example.com/legacy-language-context", Text: "original text", Language: "no", LanguageOverride: "no"}
	if err := idx.getOrCreate("nb").Index(legacy.ID(), legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.UpdateByQuery("*", nil, servertypes.DocumentChanges{Label: new("research")}, false); err != nil {
		t.Fatal(err)
	}
	requireStoredLanguage(t, idx, legacy.URL, 0, "nb")
	waitForEmbeddingJobs(t, &requests, 1)
}

func embeddingTestServer(t *testing.T, requests *atomic.Int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var request struct {
			Input json.RawMessage `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		count := 1
		if len(request.Input) > 0 && request.Input[0] == '[' {
			var inputs []string
			if err := json.Unmarshal(request.Input, &inputs); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			count = len(inputs)
		}
		data := make([]map[string]any, count)
		for idx := range data {
			data[idx] = map[string]any{"embedding": []float64{0.25, 0.75}}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
			t.Errorf("failed to encode embedding response: %v", err)
		}
	}))
}

func waitForEmbeddingJobs(t *testing.T, requests *atomic.Int64, wantRequests int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var jobs int64
		if err := model.DB.Model(&model.EmbeddingJob{}).Count(&jobs).Error; err != nil {
			t.Fatalf("failed to count embedding jobs: %v", err)
		}
		if jobs == 0 && requests.Load() == wantRequests {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("embedding queue did not drain: requests = %d, want %d", requests.Load(), wantRequests)
}

func TestEmbeddingQueueSkipsUnchangedDocumentText(t *testing.T) {
	var requests atomic.Int64
	server := embeddingTestServer(t, &requests)
	defer server.Close()

	cfg := testutil.Config(t)
	cfg.Server.Database = "hister-test.sqlite3"
	cfg.SemanticSearch.Enable = true
	cfg.SemanticSearch.EmbeddingEndpoint = server.URL
	cfg.SemanticSearch.EmbeddingModel = "test"
	cfg.SemanticSearch.Dimensions = 2
	cfg.SemanticSearch.MaxContextLength = 32
	cfg.SemanticSearch.ChunkOverlap = 4
	cfg.SemanticSearch.MaxEmbeddingConcurrency = 1
	testutil.InitModelWithConfig(t, cfg)
	idx := newTestIndexer(t, cfg)
	defer idx.Close()

	const url = "https://example.com/queue"
	if err := idx.Add(&document.Document{
		URL:       url,
		Title:     "Queued document",
		Text:      "original text",
		Processed: true,
	}); err != nil {
		t.Fatalf("first Add() error: %v", err)
	}
	waitForEmbeddingJobs(t, &requests, 1)

	if err := idx.Add(&document.Document{
		URL:       url,
		Title:     "A title change is intentionally ignored",
		Text:      "original text",
		Processed: true,
	}); err != nil {
		t.Fatalf("unchanged Add() error: %v", err)
	}
	var jobs int64
	if err := model.DB.Model(&model.EmbeddingJob{}).Count(&jobs).Error; err != nil {
		t.Fatalf("failed to count unchanged embedding jobs: %v", err)
	}
	if jobs != 0 {
		t.Fatalf("unchanged document queued %d embedding jobs, want 0", jobs)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("unchanged document embedding requests = %d, want 1", got)
	}

	if err := idx.Add(&document.Document{
		URL:       url,
		Title:     "Queued document",
		Text:      "changed text",
		Processed: true,
	}); err != nil {
		t.Fatalf("changed Add() error: %v", err)
	}
	waitForEmbeddingJobs(t, &requests, 2)
}

func TestReindexRebuildsEmbeddingsAfterDimensionChange(t *testing.T) {
	var requests atomic.Int64
	server := embeddingTestServer(t, &requests)
	defer server.Close()
	cfg := testutil.Config(t)
	cfg.SemanticSearch.EmbeddingEndpoint = server.URL
	cfg.SemanticSearch.EmbeddingModel = "test"
	cfg.SemanticSearch.Dimensions = 768
	cfg.SemanticSearch.MaxEmbeddingConcurrency = 1
	testutil.InitModelWithConfig(t, cfg)
	idx := newTestIndexer(t, cfg)
	doc := &document.Document{
		URL:       "https://example.com/changed-dimensions",
		Title:     "Dimension change",
		Text:      "Document to embed again with the new dimensions",
		Processed: true,
	}
	if err := idx.Add(doc); err != nil {
		idx.Close()
		t.Fatal(err)
	}
	idx.Close()

	// Seed an existing vector table with a size that differs from the endpoint.
	store, err := vectorstore.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	oldVector := make([]float32, 768)
	oldVector[0] = 1
	if err := store.PutChunks(doc.ID(), 0, []vectorstore.Chunk{{Text: doc.Text, Embedding: oldVector}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	cfg.SemanticSearch.Enable = true
	cfg.SemanticSearch.Dimensions = 2
	idx = newTestIndexer(t, cfg)
	defer idx.Close()
	if !idx.SemanticSearchEnabled() {
		t.Fatal("schema mismatch disabled the vector store needed for reindexing")
	}
	if err := idx.Reindex(&config.Rules{}, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	results, err := idx.vectorStore.Search([]float32{0.25, 0.75}, 5, 0.9, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].DocID != doc.ID() {
		t.Fatalf("semantic search after dimension change = %#v, want %q", results, doc.ID())
	}
	if idx.GetByURLAndUser(doc.URL, 0) == nil {
		t.Fatal("document missing after reindex")
	}
	fingerprint, err := idx.GetEmbeddingFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint != cfg.SemanticSearch.EmbeddingFingerprint() {
		t.Fatal("reindex did not record the updated embedding configuration")
	}
	// Rebuilding the Bleve indexes must leave normal asynchronous indexing usable.
	before := requests.Load()
	after := &document.Document{URL: "https://example.com/after-reindex", Text: "New content after rebuilding", Processed: true}
	if err := idx.Add(after); err != nil {
		t.Fatal(err)
	}
	waitForEmbeddingJobs(t, &requests, before+1)
	results, err = idx.vectorStore.Search([]float32{0.25, 0.75}, 5, 0.9, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, result := range results {
		if result.DocID == after.ID() {
			found = true
		}
	}
	if !found {
		t.Fatal("document added after reindex has no embeddings")
	}
}

func TestReindexRejectsEmbeddingDimensionMismatchBeforeClearingVectors(t *testing.T) {
	var requests atomic.Int64
	endpoint := embeddingTestServer(t, &requests)
	defer endpoint.Close()
	cfg := testutil.Config(t)
	cfg.SemanticSearch.EmbeddingEndpoint = endpoint.URL
	cfg.SemanticSearch.EmbeddingModel = "test"
	cfg.SemanticSearch.Dimensions = 2
	testutil.InitModelWithConfig(t, cfg)
	idx := newTestIndexer(t, cfg)
	defer idx.Close()
	doc := &document.Document{URL: "https://example.com/mismatch", Text: "Original content", Processed: true}
	if err := idx.Add(doc); err != nil {
		t.Fatal(err)
	}
	store, err := vectorstore.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	idx.vectorStore = store
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	vector := []float32{0.25, 0.75}
	if err := store.PutChunks(doc.ID(), 0, []vectorstore.Chunk{{Text: doc.Text, Embedding: vector}}); err != nil {
		t.Fatal(err)
	}
	before, err := idx.GetEmbeddingFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	cfg.SemanticSearch.Enable = true
	cfg.SemanticSearch.Dimensions = 2000
	idx.semanticConfig = cfg.SemanticSearch
	idx.embedder = vectorstore.NewEmbedder(&cfg.SemanticSearch)

	err = idx.Reindex(&config.Rules{}, false, false, false, nil)
	if err == nil || !strings.Contains(err.Error(), "expected 2000, got 2") {
		t.Errorf("reindex error = %v, want embedding dimension mismatch", err)
	}
	results, err := store.Search(vector, 5, 0.9, 0)
	if err != nil || len(results) != 1 || results[0].DocID != doc.ID() {
		t.Errorf("failed reindex changed existing vectors: results=%v error=%v", results, err)
	}
	after, err := idx.GetEmbeddingFingerprint()
	if err != nil || after != before {
		t.Errorf("failed reindex changed embedding metadata: before=%q after=%q error=%v", before, after, err)
	}
	if got := idx.GetByURLAndUser(doc.URL, 0); got == nil || got.Text != doc.Text {
		t.Error("failed reindex changed the stored document")
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("embedding requests = %d, want one validation request", got)
	}
}

func TestReindexContinuesWhenDocumentEmbeddingFails(t *testing.T) {
	docs := []*document.Document{
		{URL: "https://example.com/first", Text: "Stored content first", Processed: true},
		{URL: "https://example.com/second", Text: "Stored content second", Processed: true},
	}
	// Reindex visits documents in ID order. Fail the first and ensure the next
	// still receives vectors, as well as retaining both keyword entries.
	slices.SortFunc(docs, func(a, b *document.Document) int { return strings.Compare(a.ID(), b.ID()) })
	var requests atomic.Int64
	var rejected atomic.Int64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, input := range request.Input {
			if strings.Contains(input, docs[0].Text) {
				rejected.Add(1)
				http.Error(w, "cannot embed this document", http.StatusInternalServerError)
				return
			}
		}
		data := make([]map[string]any, len(request.Input))
		for n := range data {
			data[n] = map[string]any{"embedding": []float64{0.25, 0.75}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer endpoint.Close()
	cfg := testutil.Config(t)
	cfg.SemanticSearch.EmbeddingEndpoint = endpoint.URL
	cfg.SemanticSearch.Dimensions = 2
	testutil.InitModelWithConfig(t, cfg)
	idx := newTestIndexer(t, cfg)
	defer idx.Close()
	for _, doc := range docs {
		if err := idx.Add(doc); err != nil {
			t.Fatal(err)
		}
	}
	store, err := vectorstore.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	cfg.SemanticSearch.Enable = true
	idx.semanticConfig = cfg.SemanticSearch
	idx.embedder = vectorstore.NewEmbedder(&cfg.SemanticSearch)
	idx.vectorStore = store
	err = idx.Reindex(&config.Rules{}, false, false, false, nil)
	if err != nil {
		t.Fatalf("reindex should retain keyword entries after an embedding failure: %v", err)
	}
	for _, doc := range docs {
		if got := idx.GetByURLAndUser(doc.URL, 0); got == nil || got.Text != doc.Text {
			t.Errorf("reindex lost document %s", doc.URL)
		}
	}
	results, err := store.Search([]float32{0.25, 0.75}, 10, 0.9, 0)
	if err != nil || len(results) == 0 {
		t.Fatalf("next document has no vectors: results=%v error=%v", results, err)
	}
	for _, result := range results {
		if result.DocID != docs[1].ID() {
			t.Errorf("unexpected vector for document %s", result.DocID)
		}
	}
	if rejected.Load() == 0 || requests.Load() <= rejected.Load()+1 {
		t.Fatal("reindex did not continue embedding after the rejected document")
	}
}

func TestReindexCancelsActiveDocumentEmbedding(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	var requests atomic.Int64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"data":[{"embedding":[0.25,0.75]}]}`))
			return
		}
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer endpoint.Close()
	defer close(release)
	cfg := testutil.Config(t)
	cfg.SemanticSearch.EmbeddingEndpoint = endpoint.URL
	cfg.SemanticSearch.Dimensions = 2
	idx := newTestIndexer(t, cfg)
	defer idx.Close()
	doc := &document.Document{URL: "https://example.com/cancel-embedding", Text: "Stored content", Processed: true}
	if err := idx.Add(doc); err != nil {
		t.Fatal(err)
	}
	idx.embedder = vectorstore.NewEmbedder(&cfg.SemanticSearch)
	idx.vectorStore = &metadataVectorStore{}
	done := make(chan error, 1)
	go func() { done <- idx.ReindexContext(ctx, &config.Rules{}, false, false, false, nil) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("reindex did not reach document embedding")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("reindex error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		// Release the request before closing the indexer after a failed test.
		idx.embedCancel()
		err := <-done
		t.Fatalf("reindex did not cancel the active embedding request; shutdown returned %v", err)
	}
	if got := idx.GetByURLAndUser(doc.URL, 0); got == nil || got.Text != doc.Text {
		t.Fatal("canceled reindex changed the stored document")
	}
}

type failingEmbeddingWriteStore struct {
	metadataVectorStore
	err error
}

func (s *failingEmbeddingWriteStore) PutChunks(string, uint, []vectorstore.Chunk) error {
	return s.err
}

func TestReindexDoesNotIgnoreEmbeddingIntegrityFailures(t *testing.T) {
	writeErr := errors.New("vector storage failed")
	for _, name := range []string{"dimensions", "storage"} {
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int64
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Input []string `json:"input"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				vector := []float64{0.25, 0.75}
				if requests.Add(1) > 1 && name == "dimensions" {
					vector = []float64{1}
				}
				data := make([]map[string]any, len(request.Input))
				for n := range data {
					data[n] = map[string]any{"embedding": vector}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
			}))
			defer endpoint.Close()
			cfg := testutil.Config(t)
			cfg.SemanticSearch.EmbeddingEndpoint = endpoint.URL
			cfg.SemanticSearch.Dimensions = 2
			idx := newTestIndexer(t, cfg)
			defer idx.Close()
			doc := &document.Document{URL: "https://example.com/retained", Text: "Stored content", Processed: true}
			if err := idx.Add(doc); err != nil {
				t.Fatal(err)
			}
			idx.embedder = vectorstore.NewEmbedder(&cfg.SemanticSearch)
			idx.vectorStore = &failingEmbeddingWriteStore{err: writeErr}
			err := idx.Reindex(&config.Rules{}, false, false, false, nil)
			if name == "dimensions" {
				if _, ok := errors.AsType[*vectorstore.DimensionMismatchError](err); !ok {
					t.Fatalf("reindex error = %v, want dimension mismatch", err)
				}
			} else if !errors.Is(err, writeErr) {
				t.Fatalf("reindex error = %v, want vector storage failure", err)
			}
			if got := idx.GetByURLAndUser(doc.URL, 0); got == nil || got.Text != doc.Text {
				t.Fatal("failed reindex lost the original keyword entry")
			}
		})
	}
}

type cancelingReindexStore struct {
	vectorstore.VectorStore
	cancel context.CancelFunc
}

func (s cancelingReindexStore) Clear() error {
	err := s.VectorStore.Clear()
	s.cancel()
	return err
}

func TestReindexRestartsEmbeddingQueueAfterCancellation(t *testing.T) {
	var requests atomic.Int64
	server := embeddingTestServer(t, &requests)
	defer server.Close()
	cfg := testutil.Config(t)
	cfg.SemanticSearch.Enable = true
	cfg.SemanticSearch.EmbeddingEndpoint = server.URL
	cfg.SemanticSearch.EmbeddingModel = "test"
	cfg.SemanticSearch.Dimensions = 2
	cfg.SemanticSearch.MaxEmbeddingConcurrency = 1
	testutil.InitModelWithConfig(t, cfg)
	idx := newTestIndexer(t, cfg)
	defer idx.Close()
	doc := &document.Document{URL: "https://example.com/canceled-reindex", Text: "Original content", Processed: true}
	if err := idx.Add(doc); err != nil {
		t.Fatal(err)
	}
	waitForEmbeddingJobs(t, &requests, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	idx.vectorStore = cancelingReindexStore{VectorStore: idx.vectorStore, cancel: cancel}
	if err := idx.ReindexContext(ctx, &config.Rules{}, false, false, false, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("reindex error = %v, want context.Canceled", err)
	}
	if idx.GetByURLAndUser(doc.URL, 0) == nil {
		t.Fatal("canceled reindex removed the original document")
	}
	if idx.reindexInProgress.Load() {
		t.Fatal("reindex remains in progress after cancellation")
	}
	if err := idx.Add(&document.Document{URL: "https://example.com/after-cancellation", Text: "New content", Processed: true}); err != nil {
		t.Fatal(err)
	}
	waitForEmbeddingJobs(t, &requests, 3)
}

func TestReindexRestartsPreviouslyStoppedEmbeddingQueue(t *testing.T) {
	var requests atomic.Int64
	server := embeddingTestServer(t, &requests)
	defer server.Close()
	cfg := testutil.Config(t)
	cfg.SemanticSearch.Enable = true
	cfg.SemanticSearch.EmbeddingEndpoint = server.URL
	cfg.SemanticSearch.EmbeddingModel = "test"
	cfg.SemanticSearch.Dimensions = 2
	cfg.SemanticSearch.MaxEmbeddingConcurrency = 1
	testutil.InitModelWithConfig(t, cfg)
	idx := newTestIndexer(t, cfg)
	defer idx.Close()

	// A failed queue restart after an aborted reindex can leave workers stopped.
	idx.stopEmbeddingQueue()
	if err := idx.Reindex(&config.Rules{}, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := idx.Add(&document.Document{URL: "https://example.com/after-recovery", Text: "New content", Processed: true}); err != nil {
		t.Fatal(err)
	}
	waitForEmbeddingJobs(t, &requests, 2)
}

func TestEmbeddingQueueReprocessesDocumentChangedWhileActive(t *testing.T) {
	var requests atomic.Int64
	var inputsMu sync.Mutex
	var receivedInputs [][]string
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber := requests.Add(1)
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		inputsMu.Lock()
		receivedInputs = append(receivedInputs, request.Input)
		inputsMu.Unlock()
		if requestNumber == 1 {
			close(firstStarted)
			select {
			case <-releaseFirst:
			case <-r.Context().Done():
				return
			}
		}
		data := make([]map[string]any, len(request.Input))
		for idx := range data {
			data[idx] = map[string]any{"embedding": []float64{0.25, 0.75}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()

	cfg := testutil.Config(t)
	cfg.Server.Database = "hister-active-test.sqlite3"
	cfg.SemanticSearch.Enable = true
	cfg.SemanticSearch.EmbeddingEndpoint = server.URL
	cfg.SemanticSearch.EmbeddingModel = "test"
	cfg.SemanticSearch.Dimensions = 2
	cfg.SemanticSearch.MaxContextLength = 32
	cfg.SemanticSearch.ChunkOverlap = 4
	cfg.SemanticSearch.MaxEmbeddingConcurrency = 1
	testutil.InitModelWithConfig(t, cfg)
	idx := newTestIndexer(t, cfg)
	defer idx.Close()

	const url = "https://example.com/active"
	if err := idx.Add(&document.Document{URL: url, Title: "Active", Text: "old contents", Processed: true}); err != nil {
		t.Fatalf("first Add() error: %v", err)
	}
	select {
	case <-firstStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first embedding request did not start")
	}
	if err := idx.Add(&document.Document{URL: url, Title: "Active", Text: "latest contents", Processed: true}); err != nil {
		t.Fatalf("changed Add() error: %v", err)
	}
	if err := idx.Add(&document.Document{URL: url, Title: "Active", Text: "latest contents", Processed: true}); err != nil {
		t.Fatalf("duplicate changed Add() error: %v", err)
	}
	close(releaseFirst)
	waitForEmbeddingJobs(t, &requests, 2)

	inputsMu.Lock()
	defer inputsMu.Unlock()
	if len(receivedInputs) != 2 {
		t.Fatalf("received %d embedding inputs, want 2 requests", len(receivedInputs))
	}
	if !strings.Contains(strings.Join(receivedInputs[1], "\n"), "latest contents") {
		t.Fatalf("second request did not embed latest contents: %q", receivedInputs[1])
	}
}

func TestDeleteCancelsActiveEmbeddingJob(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-releaseHandler
	}))
	defer func() {
		close(releaseHandler)
		server.Close()
	}()

	cfg := testutil.Config(t)
	cfg.Server.Database = "hister-delete-test.sqlite3"
	cfg.SemanticSearch.Enable = true
	cfg.SemanticSearch.EmbeddingEndpoint = server.URL
	cfg.SemanticSearch.EmbeddingModel = "test"
	cfg.SemanticSearch.Dimensions = 2
	cfg.SemanticSearch.MaxContextLength = 32
	cfg.SemanticSearch.ChunkOverlap = 4
	cfg.SemanticSearch.MaxEmbeddingConcurrency = 1
	testutil.InitModelWithConfig(t, cfg)
	idx := newTestIndexer(t, cfg)
	defer idx.Close()

	const url = "https://example.com/delete-active"
	if err := idx.Add(&document.Document{URL: url, Title: "Delete", Text: "active contents", Processed: true}); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	select {
	case <-requestStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("embedding request did not start")
	}

	deleted := make(chan error, 1)
	go func() {
		deleted <- idx.Delete(url)
	}()
	select {
	case err := <-deleted:
		if err != nil {
			t.Fatalf("Delete() error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Delete() did not cancel the active embedding")
	}
	var jobs int64
	if err := model.DB.Model(&model.EmbeddingJob{}).Count(&jobs).Error; err != nil {
		t.Fatalf("failed to count embedding jobs: %v", err)
	}
	if jobs != 0 {
		t.Fatalf("embedding job count after delete = %d, want 0", jobs)
	}
	if d := idx.GetByDocID(url); d != nil {
		t.Fatalf("deleted document remains indexed: %#v", d)
	}
}

func TestEmbeddingQueueQuarantinesJobAfterMaximumAttempts(t *testing.T) {
	testutil.InitModel(t)
	const docID = "https://example.com/poison"

	if err := model.EnqueueEmbeddingJob(docID); err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	job, err := model.ClaimNextEmbeddingJob()
	if err != nil || job == nil {
		t.Fatalf("claim job = %#v, %v", job, err)
	}
	job.Attempts = embeddingMaxJobAttempts
	if err := model.DB.Model(&model.EmbeddingJob{}).
		Where("doc_id = ?", docID).
		Update("attempts", embeddingMaxJobAttempts).Error; err != nil {
		t.Fatalf("set job attempts: %v", err)
	}
	queue := &embeddingQueue{wake: make(chan struct{}, 1)}
	queue.retry(job, errors.New("embedding timed out"))

	var stored model.EmbeddingJob
	if err := model.DB.Where("doc_id = ?", docID).First(&stored).Error; err != nil {
		t.Fatalf("load quarantined job: %v", err)
	}
	if stored.Status != model.EmbeddingJobFailed {
		t.Fatalf("job status = %q, want %q", stored.Status, model.EmbeddingJobFailed)
	}
	if stored.LastError != "embedding timed out" {
		t.Fatalf("job error = %q", stored.LastError)
	}
	if stored.Attempts != embeddingMaxJobAttempts {
		t.Fatalf("job attempts = %d, want %d", stored.Attempts, embeddingMaxJobAttempts)
	}
}
