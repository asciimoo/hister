// SPDX-License-Identifier: AGPL-3.0-or-later

package vectorstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/asciimoo/hister/config"
)

// Embedder calls an OpenAI-compatible /v1/embeddings endpoint to convert text
// into float32 vectors. It also handles text chunking for long documents.
type Embedder struct {
	endpoint         string
	model            string
	apiKey           string
	headers          map[string]string
	dimensions       int
	client           *http.Client
	maxContextLength int
	chunkOverlap     int
	maxBatchSize     int
	queryPrefix      string
	documentPrefix   string
	sem              chan struct{} // nil means unlimited concurrency
}

// DocumentContext contains stable metadata used to contextualize document
// embeddings. Description and keywords are embedded only in the dedicated
// metadata vector, not repeated in every body chunk.
type DocumentContext struct {
	Title       string
	URL         string
	Type        string
	Language    string
	Author      string
	Description string
	Keywords    string
}

type embeddingField struct {
	name  string
	value string
}

type documentEmbeddingInput struct {
	embeddingText string
	chunkText     string
}

const (
	embeddingMaxAttempts      = 3
	defaultEmbeddingTimeout   = 5 * time.Minute
	defaultEmbeddingBatchSize = 8
	embeddingHeadroomDivisor  = 20 // Five percent
)

// contextLengthWithHeadroom keeps the configured context length as a hard
// ceiling. Token counts are approximate because the endpoint may use any
// model tokenizer, so reserve a small margin before constructing chunks.
func contextLengthWithHeadroom(configured int) int {
	if configured <= 1 {
		return 1
	}
	headroom := configured / embeddingHeadroomDivisor
	if configured%embeddingHeadroomDivisor != 0 {
		headroom++
	}
	return max(1, configured-headroom)
}

// NewEmbedder creates an Embedder from the semantic search config.
func NewEmbedder(cfg *config.SemanticSearch) *Embedder {
	var sem chan struct{}
	if cfg.MaxEmbeddingConcurrency > 0 {
		sem = make(chan struct{}, cfg.MaxEmbeddingConcurrency)
	}
	timeout := time.Duration(cfg.EmbeddingTimeout) * time.Second
	if timeout <= 0 {
		timeout = defaultEmbeddingTimeout
	}
	maxBatchSize := cfg.MaxEmbeddingBatchSize
	if maxBatchSize <= 0 {
		maxBatchSize = defaultEmbeddingBatchSize
	}
	return &Embedder{
		endpoint:         cfg.EmbeddingEndpoint,
		model:            cfg.EmbeddingModel,
		apiKey:           cfg.APIKey,
		headers:          cfg.Headers,
		dimensions:       cfg.Dimensions,
		maxContextLength: contextLengthWithHeadroom(cfg.MaxContextLength),
		chunkOverlap:     cfg.ChunkOverlap,
		maxBatchSize:     maxBatchSize,
		queryPrefix:      cfg.QueryPrefix,
		documentPrefix:   cfg.DocumentPrefix,
		client: &http.Client{
			Timeout: timeout,
		},
		sem: sem,
	}
}

type embeddingRequest struct {
	Model      string `json:"model"`
	Input      any    `json:"input"` // string for single, []string for batch
	Dimensions int    `json:"dimensions,omitempty"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

type embeddingStatusError struct {
	statusCode int
	body       string
}

type embeddingAPIError struct {
	Error struct {
		Message       string `json:"message"`
		Type          string `json:"type"`
		PromptTokens  int    `json:"n_prompt_tokens"`
		ContextLength int    `json:"n_ctx"`
	} `json:"error"`
}

func (e *embeddingStatusError) Error() string {
	return fmt.Sprintf("embedding endpoint returned %d: %s", e.statusCode, e.body)
}

func (e *embeddingStatusError) transient() bool {
	switch e.statusCode {
	case http.StatusRequestTimeout,
		http.StatusTooEarly,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func embeddingContextErrorDetails(err error) (promptTokens, contextLength int, ok bool) {
	var statusErr *embeddingStatusError
	if !errors.As(err, &statusErr) ||
		(statusErr.statusCode != http.StatusBadRequest && statusErr.statusCode != http.StatusInternalServerError) {
		return 0, 0, false
	}

	var apiErr embeddingAPIError
	if json.Unmarshal([]byte(statusErr.body), &apiErr) != nil {
		return 0, 0, false
	}
	errorType := strings.ToLower(apiErr.Error.Type)
	message := strings.ToLower(apiErr.Error.Message)
	// llama.cpp reports inputs exceeding its physical batch size as HTTP 500.
	// Treat that limit like a context overflow so smaller chunks can be retried.
	if n, scanErr := fmt.Sscanf(message,
		"input (%d tokens) is too large to process. increase the physical batch size (current batch size: %d)",
		&promptTokens, &contextLength); scanErr == nil && n == 2 && promptTokens > 0 && contextLength > 0 {
		return promptTokens, contextLength, true
	}
	if statusErr.statusCode != http.StatusBadRequest {
		return 0, 0, false
	}
	isContextError := errorType == "exceed_context_size_error" ||
		errorType == "context_length_exceeded" ||
		(strings.Contains(message, "context") &&
			(strings.Contains(message, "exceed") || strings.Contains(message, "too long")))
	if !isContextError {
		return 0, 0, false
	}
	return apiErr.Error.PromptTokens, apiErr.Error.ContextLength, true
}

func embeddingRetryDelay(attempt int) time.Duration {
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}

func shouldRetryEmbeddingError(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if _, _, contextError := embeddingContextErrorDetails(err); contextError {
		return false
	}

	if statusErr, ok := errors.AsType[*embeddingStatusError](err); ok {
		return statusErr.transient()
	}

	var urlErr *url.Error
	return errors.As(err, &urlErr) && !urlErr.Timeout()
}

// doEmbeddingRequestOnce sends one embedding request to the endpoint and returns
// the parsed response. input is either a string (single) or []string (batch).
func (e *Embedder) doEmbeddingRequestOnce(ctx context.Context, input any) (_ *embeddingResponse, err error) {
	body, err := json.Marshal(embeddingRequest{
		Model:      e.model,
		Input:      input,
		Dimensions: e.dimensions,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &embeddingStatusError{statusCode: resp.StatusCode, body: string(respBody)}
	}

	var result embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	return &result, nil
}

// doEmbeddingRequest sends an embedding request, retrying transient endpoint or
// network failures while respecting the caller's context.
func (e *Embedder) doEmbeddingRequest(ctx context.Context, input any) (*embeddingResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if e.sem != nil {
		select {
		case e.sem <- struct{}{}:
			defer func() { <-e.sem }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	var err error
	for attempt := range embeddingMaxAttempts {
		var result *embeddingResponse
		result, err = e.doEmbeddingRequestOnce(ctx, input)
		if err == nil {
			return result, nil
		}
		if attempt == embeddingMaxAttempts-1 || !shouldRetryEmbeddingError(ctx, err) {
			return nil, err
		}

		timer := time.NewTimer(embeddingRetryDelay(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, err
}

// Embed converts a single text into a float32 vector.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	result, err := e.doEmbeddingRequest(ctx, text)
	if err != nil {
		return nil, err
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embedding response contained no data")
	}
	if got := len(result.Data[0].Embedding); e.dimensions > 0 && got != e.dimensions {
		return nil, fmt.Errorf("embedding dimension mismatch: expected %d, got %d", e.dimensions, got)
	}
	return toFloat32(result.Data[0].Embedding), nil
}

// EmbedQuery embeds a search query, prepending the configured query prefix
// (e.g. "search_query: ") when set. Many embedding models (BGE, E5, Nomic,
// GTE) produce better recall when queries and documents use distinct prefixes.
func (e *Embedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return e.Embed(ctx, e.queryPrefix+text)
}

func embeddingVectors(result *embeddingResponse, dimensions int) ([][]float32, error) {
	vectors := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		if got := len(d.Embedding); dimensions > 0 && got != dimensions {
			return nil, fmt.Errorf("embedding dimension mismatch at index %d: expected %d, got %d", i, dimensions, got)
		}
		vectors[i] = toFloat32(d.Embedding)
	}
	return vectors, nil
}

// embedBatch converts one bounded batch, splitting it further when an endpoint
// applies a context limit to the complete request.
func (e *Embedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	result, err := e.doEmbeddingRequest(ctx, texts)
	if err != nil {
		if _, _, contextError := embeddingContextErrorDetails(err); contextError && len(texts) > 1 {
			middle := len(texts) / 2
			left, leftErr := e.embedBatch(ctx, texts[:middle])
			if leftErr != nil {
				return nil, leftErr
			}
			right, rightErr := e.embedBatch(ctx, texts[middle:])
			if rightErr != nil {
				return nil, rightErr
			}
			return append(left, right...), nil
		}
		return nil, err
	}
	return embeddingVectors(result, e.dimensions)
}

// EmbedBatch converts multiple texts in bounded requests. Smaller requests
// avoid long documents monopolizing a local embedding server while preserving
// response order.
func (e *Embedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	var vectors [][]float32
	for start := 0; start < len(texts); start += e.maxBatchSize {
		end := min(start+e.maxBatchSize, len(texts))
		batchVectors, err := e.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		vectors = append(vectors, batchVectors...)
	}
	return vectors, nil
}

func toFloat32(f64 []float64) []float32 {
	f32 := make([]float32, len(f64))
	for i, v := range f64 {
		f32[i] = float32(v)
	}
	return f32
}

func cleanEmbeddingField(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// formatEmbeddingFields formats as many complete metadata fields as fit in
// tokenBudget. The last field may be shortened when part of it still fits.
func formatEmbeddingFields(fields []embeddingField, tokenBudget int) string {
	if tokenBudget <= 0 {
		return ""
	}
	lines := make([]string, 0, len(fields))
	remaining := tokenBudget
	for _, field := range fields {
		value := cleanEmbeddingField(field.value)
		if value == "" {
			continue
		}
		line := field.name + ": " + value
		lineTokens := len(tokenize(line))
		if lineTokens > remaining {
			labelTokens := len(tokenize(field.name + ":"))
			valueBudget := remaining - labelTokens
			if valueBudget <= 0 {
				break
			}
			valueTokens := tokenize(value)
			if len(valueTokens) > valueBudget {
				valueTokens = valueTokens[:valueBudget]
			}
			line = field.name + ": " + strings.Join(valueTokens, " ")
			lineTokens = len(tokenize(line))
		}
		lines = append(lines, line)
		remaining -= lineTokens
		if remaining <= 0 {
			break
		}
	}
	return strings.Join(lines, "\n")
}

func fullDocumentFields(d DocumentContext) []embeddingField {
	return []embeddingField{
		{name: "title", value: d.Title},
		{name: "type", value: d.Type},
		{name: "language", value: d.Language},
		{name: "author", value: d.Author},
		{name: "description", value: d.Description},
		{name: "keywords", value: d.Keywords},
		{name: "url", value: d.URL},
	}
}

func bodyDocumentFields(d DocumentContext) []embeddingField {
	return []embeddingField{
		{name: "title", value: d.Title},
		{name: "language", value: d.Language},
	}
}

func (e *Embedder) documentEmbeddingInputsWithLimit(text string, d DocumentContext, contextLength int) []documentEmbeddingInput {
	var inputs []documentEmbeddingInput
	metadataLabel := e.documentPrefix + "document:\n"
	metadataBudget := contextLength - len(tokenize(metadataLabel))
	metadata := formatEmbeddingFields(fullDocumentFields(d), metadataBudget)
	if metadata != "" {
		inputs = append(inputs, documentEmbeddingInput{
			embeddingText: metadataLabel + metadata,
			chunkText:     metadata,
		})
	}

	bodyFieldBudget := max(1, contextLength/4)
	bodyContext := formatEmbeddingFields(bodyDocumentFields(d), bodyFieldBudget)
	bodyHeader := e.documentPrefix
	if bodyContext != "" {
		bodyHeader += bodyContext + "\n"
	}
	bodyHeader += "content:\n"
	contentLimit := max(1, contextLength-len(tokenize(bodyHeader)))
	textChunks := ChunkText(text, contentLimit, e.chunkOverlap)
	for _, chunk := range textChunks {
		inputs = append(inputs, documentEmbeddingInput{
			embeddingText: bodyHeader + chunk.Text,
			chunkText:     chunk.Text,
		})
	}
	return inputs
}

func (e *Embedder) documentEmbeddingInputs(text string, d DocumentContext) []documentEmbeddingInput {
	return e.documentEmbeddingInputsWithLimit(text, d, e.maxContextLength)
}

func reducedEmbeddingContextLength(err error, inputs []documentEmbeddingInput, current int) (int, bool) {
	promptTokens, endpointContextLength, contextError := embeddingContextErrorDetails(err)
	if !contextError || current <= 1 {
		return 0, false
	}

	next := current * 3 / 4
	if promptTokens > 0 && endpointContextLength > 0 {
		estimatedTokens := 0
		for _, input := range inputs {
			estimatedTokens = max(estimatedTokens, len(tokenize(input.embeddingText)))
		}
		if estimatedTokens > 0 {
			// Keep ten percent free because the endpoint and local tokenizer may
			// continue to differ at the new chunk boundary.
			next = int(int64(estimatedTokens) * int64(endpointContextLength) * 9 /
				(int64(promptTokens) * 10))
		}
	}
	if next >= current {
		next = current * 9 / 10
	}
	next = max(1, next)
	return next, next < current
}

// ChunkAndEmbed creates a dedicated metadata embedding and separate structured
// body chunk embeddings, then returns them ready for storage. Returns nil when
// both document context and text are empty.
func (e *Embedder) ChunkAndEmbed(ctx context.Context, text string, d DocumentContext) ([]Chunk, error) {
	contextLength := e.maxContextLength
	for {
		inputs := e.documentEmbeddingInputsWithLimit(text, d, contextLength)
		if len(inputs) == 0 {
			return nil, nil
		}

		texts := make([]string, len(inputs))
		for i, input := range inputs {
			texts[i] = input.embeddingText
		}

		vectors, err := e.EmbedBatch(ctx, texts)
		if err != nil {
			nextContextLength, retry := reducedEmbeddingContextLength(err, inputs, contextLength)
			if retry {
				contextLength = nextContextLength
				continue
			}
			return nil, err
		}
		if len(vectors) != len(inputs) {
			return nil, fmt.Errorf("embedding count mismatch: expected %d, got %d", len(inputs), len(vectors))
		}

		chunks := make([]Chunk, len(inputs))
		for i := range inputs {
			chunks[i] = Chunk{
				Index:     i,
				Text:      inputs[i].chunkText,
				Embedding: vectors[i],
			}
		}
		return chunks, nil
	}
}
