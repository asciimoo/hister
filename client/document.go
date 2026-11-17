package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/asciimoo/nogon/server/types"
)

// AddDocumentResult describes the outcome of one document in a bulk request.
type AddDocumentResult struct {
	Status int    `json:"status"`
	Error  string `json:"error,omitempty"`
}

type addDocumentOperation struct {
	Op string `json:"op"`
	*Document
}

type encodedAddDocument struct {
	data []byte
}

const maxBatchOperations = 100

// AddDocumentsJSON submits documents in byte bounded bulk requests.
func (c *Client) AddDocumentsJSON(docs []*Document) (results []AddDocumentResult, err error) {
	if len(docs) == 0 {
		return []AddDocumentResult{}, nil
	}
	ops := make([]encodedAddDocument, len(docs))
	for i, doc := range docs {
		c.applyDocumentOptions(doc)
		data, err := json.Marshal(addDocumentOperation{Op: "add", Document: doc})
		if err != nil {
			return results, err
		}
		ops[i] = encodedAddDocument{data: data}
	}

	limit := c.MaxBatchBodyBytes()
	results = make([]AddDocumentResult, 0, len(docs))
	for start := 0; start < len(ops); {
		if size := encodedBatchSize(ops[start : start+1]); size > limit {
			results = append(results, oversizedDocumentResult(size, limit))
			start++
			continue
		}

		end := start + 1
		for end < len(ops) && end-start < maxBatchOperations {
			if encodedBatchSize(ops[start:end+1]) > limit {
				break
			}
			end++
		}
		batchResults, batchErr := c.submitAddDocumentBatch(ops[start:end])
		results = append(results, batchResults...)
		if batchErr != nil {
			return results, batchErr
		}
		start = end
	}
	return results, nil
}

func encodedBatchSize(ops []encodedAddDocument) int64 {
	size := int64(len(`{"ops":[]}`))
	for i, op := range ops {
		size += int64(len(op.data))
		if i > 0 {
			size++
		}
	}
	return size
}

func encodeBatch(ops []encodedAddDocument) []byte {
	var body bytes.Buffer
	body.Grow(int(encodedBatchSize(ops)))
	body.WriteString(`{"ops":[`)
	for i, op := range ops {
		if i > 0 {
			body.WriteByte(',')
		}
		body.Write(op.data)
	}
	body.WriteString(`]}`)
	return body.Bytes()
}

func oversizedDocumentResult(size, limit int64) AddDocumentResult {
	return AddDocumentResult{
		Status: http.StatusRequestEntityTooLarge,
		Error:  fmt.Sprintf("encoded document request is %d bytes and exceeds the %d byte server limit", size, limit),
	}
}

func (c *Client) submitAddDocumentBatch(ops []encodedAddDocument) ([]AddDocumentResult, error) {
	data := encodeBatch(ops)
	results, err := c.sendAddDocumentBatch(data, len(ops))
	if err == nil {
		return results, nil
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusRequestEntityTooLarge {
		return nil, err
	}
	if len(ops) == 1 {
		message := fmt.Sprintf("server rejected encoded document request of %d bytes as too large", len(data))
		var response struct {
			Error      string `json:"error"`
			LimitBytes int64  `json:"limit_bytes"`
		}
		if json.Unmarshal([]byte(httpErr.Detail), &response) == nil {
			if response.Error != "" {
				message = response.Error
			}
			if response.LimitBytes > 0 {
				message = fmt.Sprintf("%s; encoded document request is %d bytes", message, len(data))
			}
		}
		return []AddDocumentResult{{Status: http.StatusRequestEntityTooLarge, Error: message}}, nil
	}

	middle := len(ops) / 2
	left, err := c.submitAddDocumentBatch(ops[:middle])
	if err != nil {
		return left, err
	}
	right, err := c.submitAddDocumentBatch(ops[middle:])
	return append(left, right...), err
}

func (c *Client) sendAddDocumentBatch(data []byte, documentCount int) (results []AddDocumentResult, err error) {
	err = c.request(context.Background(), http.MethodPost, "/api/batch", bytes.NewReader(data), "application/json", func(resp *http.Response) error {
		var result struct {
			Results []AddDocumentResult `json:"results"`
		}
		if err := decodeResponse(resp, &result); err != nil {
			return err
		}
		if len(result.Results) != documentCount {
			return fmt.Errorf("batch response contained %d results for %d documents", len(result.Results), documentCount)
		}
		results = result.Results
		return nil
	})
	return results, err
}

func (c *Client) AddDocumentJSON(doc *Document) error {
	return c.AddDocumentJSONContext(context.Background(), doc)
}

// AddDocumentJSONContext submits a prepared document until ctx is cancelled.
func (c *Client) AddDocumentJSONContext(ctx context.Context, doc *Document) error {
	c.applyDocumentOptions(doc)
	return c.requestJSON(ctx, http.MethodPost, "/api/add", doc, nil)
}

func (c *Client) applyDocumentOptions(doc *Document) {
	if c.allowSensitive {
		doc.SkipSensitiveCheck = true
	}
	if c.ignoreRules {
		if doc.Metadata == nil {
			doc.Metadata = make(map[string]any)
		}
		doc.Metadata["ignore_skip_rules"] = true
	}
}

func (c *Client) AddPage(u, title, text string) error {
	formData := url.Values{"url": {u}, "title": {title}, "text": {text}}
	return c.postForm("/api/add", formData)
}

func (c *Client) DocumentExists(u string) (bool, error) {
	return c.DocumentExistsContext(context.Background(), u)
}

func (c *Client) DocumentExistsContext(ctx context.Context, u string) (exists bool, err error) {
	err = c.request(ctx, http.MethodHead, "/api/document?url="+url.QueryEscape(u), nil, "", func(resp *http.Response) error {
		if resp.StatusCode != http.StatusNotFound {
			if err := checkStatus(resp); err != nil {
				return err
			}
		}
		exists = resp.StatusCode == http.StatusOK
		return nil
	})
	return exists, err
}

func (c *Client) Reindex(skipSensitive, detectLanguages bool) error {
	type reindexRequest struct {
		SkipSensitive   bool `json:"skipSensitive"`
		DetectLanguages bool `json:"detectLanguages"`
	}
	return c.requestJSON(context.Background(), http.MethodPost, "/api/reindex", reindexRequest{SkipSensitive: skipSensitive, DetectLanguages: detectLanguages}, nil)
}

func (c *Client) DeleteDocument(u string) error {
	return c.DeleteDocuments("url:" + u)
}

func (c *Client) DeleteDocuments(query string) error {
	return c.requestJSON(context.Background(), http.MethodPost, "/api/delete", map[string]string{"query": query}, nil)
}

// CountDocumentsForDeletion uses the same query and ownership scope as deletion.
func (c *Client) CountDocumentsForDeletion(query string) (uint64, error) {
	var result struct {
		Matched *uint64 `json:"matched"`
	}
	if err := c.requestJSON(context.Background(), http.MethodPost, "/api/delete", map[string]any{"query": query, "dry_run": true}, &result); err != nil {
		return 0, err
	}
	if result.Matched == nil {
		return 0, errors.New("deletion preview response is missing the matched count")
	}
	return *result.Matched, nil
}

// UpdateLabel sets or clears the user-defined label for a stored document.
func (c *Client) UpdateLabel(urlStr, label string) error {
	return c.requestJSON(context.Background(), http.MethodPost, "/api/label", map[string]string{"url": urlStr, "label": label}, nil)
}

// FetchPreview retrieves the server-rendered readable representation of a
// stored document.
func (c *Client) FetchPreview(urlStr string) (*PreviewResponse, error) {
	var preview PreviewResponse
	if err := c.requestJSON(context.Background(), http.MethodGet, "/api/preview?url="+url.QueryEscape(urlStr), nil, &preview); err != nil {
		return nil, err
	}
	return &preview, nil
}

// CleanupResult holds local file reconciliation and orphaned data cleanup counts.
type CleanupResult struct {
	LocalDocumentsChecked int `json:"localDocumentsChecked"`
	LocalDocumentsSkipped int `json:"localDocumentsSkipped"`
	LocalDocumentsRemoved int `json:"localDocumentsRemoved"`
	HTMLRemoved           int `json:"htmlRemoved"`
	FaviconRemoved        int `json:"faviconRemoved"`
}

func (c *Client) Cleanup() (result CleanupResult, err error) {
	err = c.requestJSON(context.Background(), http.MethodPost, "/api/cleanup", nil, &result)
	return result, err
}

// Purge previews or removes retained records on the configured server.
func (c *Client) Purge(ctx context.Context, request types.PurgeRequest) (*types.PurgeResult, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	var result *types.PurgeResult
	if err := c.requestJSON(ctx, http.MethodPost, "/api/purge", request, &result); err != nil {
		return nil, err
	}
	if result == nil || result.DryRun != request.DryRun {
		return nil, errors.New("invalid purge response")
	}
	for _, count := range []int64{result.Versions, result.HistoryEntries, result.HistoryQueries, result.HistoryURLs, result.CrawlJobs, result.CrawlURLs} {
		if count < 0 {
			return nil, errors.New("invalid purge count")
		}
	}
	return result, nil
}
