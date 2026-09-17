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
			testChunkAndEmbedRechunks(t, writeError)
		})
	}
}

func testChunkAndEmbedRechunks(t *testing.T, writeError func(http.ResponseWriter, int, int)) {
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
		strings.Repeat("longtoken ", 20),
		DocumentContext{Title: "Adaptive chunking"},
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
