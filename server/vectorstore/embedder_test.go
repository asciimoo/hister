// SPDX-License-Identifier: AGPL-3.0-or-later

package vectorstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asciimoo/hister/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestEmbedder(endpoint string) *Embedder {
	return NewEmbedder(&config.SemanticSearch{
		EmbeddingEndpoint: endpoint,
		EmbeddingModel:    "test-model",
		Dimensions:        3,
		MaxContextLength:  128,
	})
}

func writeEmbeddingResponse(w http.ResponseWriter) {
	writeEmbeddingBatchResponse(w, 1)
}

func writeEmbeddingBatchResponse(w http.ResponseWriter, count int) {
	data := make([]struct {
		Embedding []float64 `json:"embedding"`
	}, count)
	for i := range data {
		data[i].Embedding = []float64{1, 2, 3}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(embeddingResponse{
		Data: data,
	})
}

func writeContextSizeError(w http.ResponseWriter, promptTokens, contextLength int) {
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":            http.StatusBadRequest,
			"message":         "request exceeds the available context size",
			"type":            "exceed_context_size_error",
			"n_prompt_tokens": promptTokens,
			"n_ctx":           contextLength,
		},
	})
}

func writePhysicalBatchSizeError(w http.ResponseWriter, promptTokens, batchSize int) {
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    http.StatusInternalServerError,
			"message": fmt.Sprintf("input (%d tokens) is too large to process. increase the physical batch size (current batch size: %d)", promptTokens, batchSize),
			"type":    "server_error",
		},
	})
}

func TestEmbeddingContextErrorDetailsPhysicalBatchSize(t *testing.T) {
	err := &embeddingStatusError{
		statusCode: http.StatusInternalServerError,
		body:       `{"error":{"code":500,"message":"input (553 tokens) is too large to process. increase the physical batch size (current batch size: 512)","type":"server_error"}}`,
	}
	promptTokens, contextLength, ok := embeddingContextErrorDetails(err)
	if !ok || promptTokens != 553 || contextLength != 512 {
		t.Fatalf("context error details = (%d, %d, %v), want (553, 512, true)", promptTokens, contextLength, ok)
	}
}

func TestContextLengthWithHeadroom(t *testing.T) {
	tests := []struct {
		configured int
		want       int
	}{
		{configured: 32000, want: 30400},
		{configured: 512, want: 486},
		{configured: 100, want: 95},
		{configured: 19, want: 18},
		{configured: 1, want: 1},
	}
	for _, test := range tests {
		if got := contextLengthWithHeadroom(test.configured); got != test.want {
			t.Errorf("contextLengthWithHeadroom(%d) = %d, want %d", test.configured, got, test.want)
		}
	}
}

func TestNewEmbedderAppliesContextHeadroom(t *testing.T) {
	embedder := NewEmbedder(&config.SemanticSearch{MaxContextLength: 32000})
	if got, want := embedder.maxContextLength, 30400; got != want {
		t.Errorf("max context length = %d, want %d", got, want)
	}
}

func TestEmbedRequestIncludesConfiguredDimensions(t *testing.T) {
	var request embeddingRequest
	embedder := newTestEmbedder("http://example.com/v1/embeddings")
	embedder.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"embedding":[1,2,3]}]}`)),
			Request:    req,
		}, nil
	})

	if _, err := embedder.Embed(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if got, want := request.Dimensions, 3; got != want {
		t.Errorf("request dimensions = %d, want %d", got, want)
	}
}

func TestDocumentEmbeddingInputsSeparateMetadataAndBody(t *testing.T) {
	embedder := newTestEmbedder("")
	embedder.documentPrefix = "passage: "
	documentContext := DocumentContext{
		Title:       "Semantic Search",
		URL:         "https://example.com/search",
		Type:        "article",
		Language:    "en",
		Author:      "Ada Example",
		Description: "How semantic retrieval works.",
		Keywords:    "search, embeddings",
	}

	inputs := embedder.documentEmbeddingInputs("Body content about vector retrieval.", documentContext)
	if len(inputs) != 2 {
		t.Fatalf("expected metadata and body inputs, got %d: %#v", len(inputs), inputs)
	}

	metadata := inputs[0]
	for _, expected := range []string{
		"passage: document:\n",
		"title: Semantic Search",
		"type: article",
		"language: en",
		"author: Ada Example",
		"description: How semantic retrieval works.",
		"keywords: search, embeddings",
		"url: https://example.com/search",
	} {
		if !strings.Contains(metadata.embeddingText, expected) {
			t.Errorf("metadata input does not contain %q: %q", expected, metadata.embeddingText)
		}
	}
	if metadata.chunkText == "" {
		t.Error("metadata chunk text must be available for matched chunk previews")
	}
	if strings.Contains(metadata.embeddingText, "\ndomain:") {
		t.Errorf("metadata input unexpectedly contains a separate domain field: %q", metadata.embeddingText)
	}

	body := inputs[1]
	for _, expected := range []string{
		"passage: title: Semantic Search",
		"language: en",
		"content:\nBody content about vector retrieval.",
	} {
		if !strings.Contains(body.embeddingText, expected) {
			t.Errorf("body input does not contain %q: %q", expected, body.embeddingText)
		}
	}
	for _, repeatedMetadata := range []string{"domain:", "type:", "author:", "description:", "keywords:", "url:"} {
		if strings.Contains(body.embeddingText, repeatedMetadata) {
			t.Errorf("body input unexpectedly repeats %q metadata: %q", repeatedMetadata, body.embeddingText)
		}
	}
	if body.chunkText != "Body content about vector retrieval." {
		t.Errorf("body chunk text = %q", body.chunkText)
	}
	for i, input := range inputs {
		if got := len(tokenize(input.embeddingText)); got > embedder.maxContextLength {
			t.Errorf("input %d exceeds context limit: got %d, limit %d", i, got, embedder.maxContextLength)
		}
	}
}

func TestDocumentEmbeddingInputsSupportMetadataOnlyDocument(t *testing.T) {
	embedder := newTestEmbedder("")
	inputs := embedder.documentEmbeddingInputs("", DocumentContext{Title: "Saved title"})
	if len(inputs) != 1 {
		t.Fatalf("expected one metadata input, got %d: %#v", len(inputs), inputs)
	}
	if !strings.Contains(inputs[0].embeddingText, "title: Saved title") {
		t.Errorf("metadata input does not contain title: %q", inputs[0].embeddingText)
	}
}

func TestDocumentEmbeddingInputsBoundsLongMetadata(t *testing.T) {
	embedder := newTestEmbedder("")
	for name, d := range map[string]DocumentContext{
		"single field":    {Title: strings.Repeat("1234567890", 2000)},
		"combined fields": {Title: strings.Repeat("1234", 150), Description: strings.Repeat("é", 300)},
	} {
		t.Run(name, func(t *testing.T) {
			inputs := embedder.documentEmbeddingInputs(strings.Repeat("9876543210", 1000), d)
			if len(inputs) < 2 {
				t.Fatalf("expected metadata and body inputs, got %d", len(inputs))
			}
			for i, input := range inputs {
				if len(input.embeddingText) > 1024 {
					t.Errorf("input %d has %d bytes, want at most 1024", i, len(input.embeddingText))
				}
			}
		})
	}
}

func TestDocumentEmbeddingInputsEmptyDocument(t *testing.T) {
	embedder := newTestEmbedder("")
	if inputs := embedder.documentEmbeddingInputs("", DocumentContext{}); inputs != nil {
		t.Fatalf("expected no inputs, got %#v", inputs)
	}
}

func TestFormatEmbeddingFieldsRespectsBudget(t *testing.T) {
	fields := []embeddingField{
		{name: "title", value: "A title with\nextra whitespace"},
		{name: "description", value: strings.Repeat("word ", 50)},
	}
	formatted := formatEmbeddingFields(fields, 12)
	if got := len(tokenize(formatted)); got > 12 {
		t.Fatalf("formatted fields exceed budget: got %d tokens in %q", got, formatted)
	}
	if strings.Contains(formatted, "\nextra") {
		t.Errorf("field value whitespace was not normalized: %q", formatted)
	}
}

func TestEmbedBatchSplitsAggregateContextOverflow(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(request.Input) > 2 {
			writeContextSizeError(w, len(request.Input)*10, 20)
			return
		}
		writeEmbeddingBatchResponse(w, len(request.Input))
	}))
	defer srv.Close()

	vectors, err := newTestEmbedder(srv.URL).EmbedBatch(
		context.Background(),
		[]string{"one", "two", "three", "four", "five"},
	)
	if err != nil {
		t.Fatalf("EmbedBatch returned error: %v", err)
	}
	if len(vectors) != 5 {
		t.Fatalf("vector count = %d, want 5", len(vectors))
	}
	if requests.Load() <= 1 {
		t.Fatalf("request count = %d, want a split batch", requests.Load())
	}
}

func TestEmbedBatchHonorsConfiguredMaximum(t *testing.T) {
	var requests atomic.Int32
	var largestBatch atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		batchSize := int32(len(request.Input))
		if batchSize > largestBatch.Load() {
			largestBatch.Store(batchSize)
		}
		writeEmbeddingBatchResponse(w, len(request.Input))
	}))
	defer srv.Close()

	embedder := NewEmbedder(&config.SemanticSearch{
		EmbeddingEndpoint:     srv.URL,
		EmbeddingModel:        "test-model",
		EmbeddingTimeout:      17,
		Dimensions:            3,
		MaxContextLength:      128,
		MaxEmbeddingBatchSize: 2,
	})
	if got, want := embedder.client.Timeout, 17*time.Second; got != want {
		t.Errorf("client timeout = %v, want %v", got, want)
	}
	vectors, err := embedder.EmbedBatch(
		context.Background(),
		[]string{"one", "two", "three", "four", "five"},
	)
	if err != nil {
		t.Fatalf("EmbedBatch returned error: %v", err)
	}
	if len(vectors) != 5 {
		t.Fatalf("vector count = %d, want 5", len(vectors))
	}
	if got := requests.Load(); got != 3 {
		t.Errorf("request count = %d, want 3", got)
	}
	if got := largestBatch.Load(); got != 2 {
		t.Errorf("largest batch = %d, want 2", got)
	}
}

func TestChunkAndEmbedRechunksAfterEndpointContextOverflow(t *testing.T) {
	for name, writeError := range map[string]func(http.ResponseWriter, int, int){
		"context size":        writeContextSizeError,
		"physical batch size": writePhysicalBatchSizeError,
	} {
		t.Run(name, func(t *testing.T) {
			for name, input := range map[string]struct{ text, title string }{
				"words":       {strings.Repeat("longtoken ", 20), "Adaptive chunking"},
				"long number": {strings.Repeat("1234567890", 2000), "Adaptive chunking"},
				"long title":  {strings.Repeat("longtoken ", 20), strings.Repeat("1234567890", 2000)},
			} {
				t.Run(name, func(t *testing.T) {
					testChunkAndEmbedRechunks(t, writeError, input.text, input.title)
				})
			}
		})
	}
}

func testChunkAndEmbedRechunks(t *testing.T, writeError func(http.ResponseWriter, int, int), text, title string) {
	t.Helper()
	const endpointContextLength = 64
	var rejected atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, input := range request.Input {
			if actualTokens := len([]rune(input)); actualTokens > endpointContextLength {
				rejected.Add(1)
				writeError(w, actualTokens, endpointContextLength)
				return
			}
		}
		writeEmbeddingBatchResponse(w, len(request.Input))
	}))
	defer srv.Close()

	embedder := newTestEmbedder(srv.URL)
	embedder.maxContextLength = endpointContextLength
	chunks, err := embedder.ChunkAndEmbed(
		context.Background(),
		text,
		DocumentContext{Title: title},
	)
	if err != nil {
		t.Fatalf("ChunkAndEmbed returned error: %v", err)
	}
	if rejected.Load() == 0 {
		t.Fatal("endpoint did not reject the initial oversized chunk")
	}
	if len(chunks) < 3 {
		t.Fatalf("chunk count = %d, want rechunked inputs", len(chunks))
	}
	for i, chunk := range chunks {
		if len(chunk.Embedding) != 3 {
			t.Fatalf("chunk %d embedding length = %d, want 3", i, len(chunk.Embedding))
		}
	}
}

func TestEmbedRetriesTransientStatus(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			testEmbedRetriesTransientStatus(t, status)
		})
	}
}

func testEmbedRetriesTransientStatus(t *testing.T, status int) {
	t.Helper()
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, "warming up", status)
			return
		}
		writeEmbeddingResponse(w)
	}))
	defer srv.Close()

	vec, err := newTestEmbedder(srv.URL).Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
	if len(vec) != 3 || vec[0] != 1 || vec[1] != 2 || vec[2] != 3 {
		t.Fatalf("unexpected vector: %#v", vec)
	}
}

func TestEmbedDoesNotRetryNonTransientStatus(t *testing.T) {
	for name, writeError := range map[string]func(http.ResponseWriter){
		"bad request": func(w http.ResponseWriter) {
			http.Error(w, "bad input", http.StatusBadRequest)
		},
		"physical batch size": func(w http.ResponseWriter) {
			writePhysicalBatchSizeError(w, 553, 512)
		},
	} {
		t.Run(name, func(t *testing.T) {
			testEmbedDoesNotRetryNonTransientStatus(t, writeError)
		})
	}
}

func testEmbedDoesNotRetryNonTransientStatus(t *testing.T, writeError func(http.ResponseWriter)) {
	t.Helper()
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		writeError(w)
	}))
	defer srv.Close()

	_, err := newTestEmbedder(srv.URL).Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed returned nil error")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestEmbedRequestUsesContext(t *testing.T) {
	requestStarted := make(chan struct{})
	unblockHandler := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		select {
		case <-r.Context().Done():
		case <-unblockHandler:
		}
	}))
	defer func() {
		close(unblockHandler)
		srv.Close()
	}()

	errc := make(chan error, 1)
	go func() {
		_, err := newTestEmbedder(srv.URL).Embed(ctx, "hello")
		errc <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("embedding request did not reach test server")
	}

	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Embed error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Embed did not return after context cancellation")
	}
}

func TestEmbedClientTimeoutDoesNotRetryImmediately(t *testing.T) {
	var requests atomic.Int32
	unblockHandler := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		select {
		case <-r.Context().Done():
		case <-unblockHandler:
		}
	}))
	defer func() {
		close(unblockHandler)
		srv.Close()
	}()

	embedder := newTestEmbedder(srv.URL)
	embedder.client.Timeout = 20 * time.Millisecond
	_, err := embedder.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed returned nil error")
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("request count = %d, want 1", got)
	}
}

func TestEmbedQueryDoesNotWaitForIndexingSlot(t *testing.T) {
	indexing := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.Input == "document" {
			indexing <- struct{}{}
			<-release
		}
		writeEmbeddingResponse(w)
	}))
	defer server.Close()
	defer close(release)

	embedder := NewEmbedder(&config.SemanticSearch{
		EmbeddingEndpoint:       server.URL,
		EmbeddingModel:          "test-model",
		Dimensions:              3,
		MaxContextLength:        128,
		MaxEmbeddingConcurrency: 1,
	})

	indexErr := make(chan error, 1)
	go func() {
		_, err := embedder.Embed(context.Background(), "document")
		indexErr <- err
	}()
	select {
	case <-indexing:
	case err := <-indexErr:
		t.Fatalf("indexing stopped before acquiring a slot: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("indexing request did not start")
	}

	// The only indexing slot is held, so another indexing request must wait.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := embedder.Embed(ctx, "document"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second indexing request error = %v, want deadline exceeded", err)
	}

	if _, err := embedder.EmbedQuery(context.Background(), "query"); err != nil {
		t.Fatalf("EmbedQuery while indexing slot is held: %v", err)
	}

	select {
	case release <- struct{}{}:
	case <-time.After(3 * time.Second):
		t.Fatal("indexing handler did not receive release")
	}
	select {
	case err := <-indexErr:
		if err != nil {
			t.Fatalf("indexing request: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("indexing request did not finish")
	}
}

func TestEmbedQueryDeadline(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	embedder := NewEmbedder(&config.SemanticSearch{
		EmbeddingEndpoint:     server.URL,
		EmbeddingModel:        "test-model",
		Dimensions:            3,
		MaxContextLength:      128,
		QueryEmbeddingTimeout: 1,
	})

	start := time.Now()
	_, err := embedder.EmbedQuery(context.Background(), "query")
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("EmbedQuery error = %v, want deadline exceeded", err)
	}
	if elapsed < time.Second || elapsed > 3*time.Second {
		t.Fatalf("EmbedQuery returned after %v, want about one second", elapsed)
	}
}

func TestQueryEmbeddingTimeoutDefault(t *testing.T) {
	embedder := newTestEmbedder("http://localhost")
	if embedder.queryTimeout != defaultQueryTimeout {
		t.Fatalf("queryTimeout = %v, want %v", embedder.queryTimeout, defaultQueryTimeout)
	}
}

func TestQueryEmbeddingConcurrency(t *testing.T) {
	for _, configured := range []int{-1, 0, 3} {
		e := NewEmbedder(&config.SemanticSearch{MaxEmbeddingConcurrency: 2, MaxQueryEmbeddingConcurrency: configured})
		want := configured
		if want <= 0 {
			want = 1
		}
		if cap(e.querySem) != want || cap(e.sem) != 2 {
			t.Fatalf("configured %d: query slots %d, indexing slots %d", configured, cap(e.querySem), cap(e.sem))
		}
	}
}

func TestEmbedQueryDeadlineWhileWaitingForQuerySlot(t *testing.T) {
	e := NewEmbedder(&config.SemanticSearch{})
	e.queryTimeout = 40 * time.Millisecond
	e.querySem <- struct{}{}
	defer func() { <-e.querySem }()
	_, err := e.EmbedQuery(context.Background(), "query")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if len(e.querySem) != 1 {
		t.Fatal("released an occupied query slot")
	}
}

func TestEmbedQueryRespectsCallerDeadline(t *testing.T) {
	e := newTestEmbedder("http://example.com")
	e.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := e.EmbedQuery(ctx, "query")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("ignored caller deadline")
	}
	if len(e.querySem) != 0 {
		t.Fatal("query slot leaked")
	}
}

func TestEmbedQueryDeadlineCancelsRetry(t *testing.T) {
	e := newTestEmbedder("http://example.com")
	e.queryTimeout = 40 * time.Millisecond
	calls := 0
	e.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 503, Body: http.NoBody, Header: make(http.Header), Request: r}, nil
	})
	_, err := e.EmbedQuery(context.Background(), "query")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("requests = %d, want 1", calls)
	}
	if len(e.querySem) != 0 {
		t.Fatal("query slot leaked")
	}
}
