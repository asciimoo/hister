package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/asciimoo/hister/server/document"
)

// AddDocumentResult describes the outcome of one document in a bulk request.
type AddDocumentResult struct {
	Status int    `json:"status"`
	Error  string `json:"error,omitempty"`
}

type addDocumentOperation struct {
	Op string `json:"op"`
	*document.Document
}

type encodedAddDocument struct {
	data []byte
}

const maxBatchOperations = 100

// AddDocumentsJSON submits documents in byte bounded bulk requests.
func (c *Client) AddDocumentsJSON(docs []*document.Document) (results []AddDocumentResult, err error) {
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

func (c *Client) sendAddDocumentBatch(data []byte, documentCount int) (_ []AddDocumentResult, err error) {
	req, err := c.newRequest(http.MethodPost, "/api/batch", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp, &err)
	if err = checkStatus(resp); err != nil {
		return nil, err
	}
	var result struct {
		Results []AddDocumentResult `json:"results"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Results) != documentCount {
		return nil, fmt.Errorf("batch response contained %d results for %d documents", len(result.Results), documentCount)
	}
	return result.Results, nil
}

func (c *Client) AddDocumentJSON(doc *document.Document) (err error) {
	return c.AddDocumentJSONContext(context.Background(), doc)
}

// AddDocumentJSONContext submits a prepared document until ctx is cancelled.
func (c *Client) AddDocumentJSONContext(ctx context.Context, doc *document.Document) (err error) {
	c.applyDocumentOptions(doc)
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	req, err := c.newRequest("POST", "/api/add", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer closeBody(resp, &err)
	return checkStatus(resp)
}

func (c *Client) applyDocumentOptions(doc *document.Document) {
	if c.allowSensitive {
		doc.SkipSensitiveCheck = true
	}
	if c.ignoreRules {
		doc.SetIgnoreSkipRules(true)
	}
}

func (c *Client) AddPage(u, title, text string) (err error) {
	formData := url.Values{"url": {u}, "title": {title}, "text": {text}}
	req, err := c.newRequest("POST", "/api/add", strings.NewReader(formData.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer closeBody(resp, &err)
	return checkStatus(resp)
}

func (c *Client) DocumentExists(u string) (_ bool, err error) {
	return c.DocumentExistsContext(context.Background(), u)
}

func (c *Client) DocumentExistsContext(ctx context.Context, u string) (_ bool, err error) {
	req, err := c.newRequest("HEAD", "/api/document?url="+url.QueryEscape(u), nil)
	if err != nil {
		return false, err
	}
	resp, err := c.httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return false, err
	}
	defer closeBody(resp, &err)
	if resp.StatusCode != http.StatusNotFound {
		if err := checkStatus(resp); err != nil {
			return false, err
		}
	}
	return resp.StatusCode == http.StatusOK, nil
}

func (c *Client) Reindex(skipSensitive, detectLanguages bool) (err error) {
	type reindexRequest struct {
		SkipSensitive   bool `json:"skipSensitive"`
		DetectLanguages bool `json:"detectLanguages"`
	}
	data, err := json.Marshal(reindexRequest{SkipSensitive: skipSensitive, DetectLanguages: detectLanguages})
	if err != nil {
		return err
	}
	req, err := c.newRequest("POST", "/api/reindex", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer closeBody(resp, &err)
	return checkStatus(resp)
}

func (c *Client) DeleteDocument(u string) (err error) {
	return c.DeleteDocuments("url:" + u)
}

func (c *Client) DeleteDocuments(query string) (err error) {
	data, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return err
	}
	req, err := c.newRequest("POST", "/api/delete", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer closeBody(resp, &err)
	return checkStatus(resp)
}

// UpdateLabel sets or clears the user-defined label for a stored document.
func (c *Client) UpdateLabel(urlStr, label string) (err error) {
	data, err := json.Marshal(map[string]string{"url": urlStr, "label": label})
	if err != nil {
		return err
	}
	req, err := c.newRequest("POST", "/api/label", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer closeBody(resp, &err)
	return checkStatus(resp)
}

// FetchPreview retrieves the server-rendered readable representation of a
// stored document.
func (c *Client) FetchPreview(urlStr string) (_ *PreviewResponse, err error) {
	req, err := c.newRequest("GET", "/api/preview?url="+url.QueryEscape(urlStr), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp, &err)
	if err = checkStatus(resp); err != nil {
		return nil, err
	}
	var preview PreviewResponse
	if err = json.NewDecoder(resp.Body).Decode(&preview); err != nil {
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
	req, err := c.newRequest("POST", "/api/cleanup", nil)
	if err != nil {
		return result, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return result, err
	}
	defer closeBody(resp, &err)
	if err = checkStatus(resp); err != nil {
		return result, err
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}
