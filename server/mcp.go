// SPDX-License-Identifier: AGPL-3.0-or-later

// Package server MCP endpoint implements the Model Context Protocol (MCP)
// Streamable HTTP transport so that AI assistants (Claude Desktop, Cursor,
// etc.) can search the Hister index directly.
//
// Specification: https://modelcontextprotocol.io/specification/2024-11-05
//
// Exposed tools: search, get_preview, get_history. The handler lives at POST /mcp and uses
// the same authentication as the rest of the API. Bearer tokens are accepted
// via the standard Authorization header and are resolved by the global auth
// middleware (withTokenAuth / populateUserContext).
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/extractor"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/model"

	"github.com/rs/zerolog/log"
)

// mcpProtocolVersion is the MCP specification version this server targets.
const mcpProtocolVersion = "2024-11-05"

// JSON-RPC 2.0 error codes defined by the MCP specification.
const (
	mcpErrParse        = -32700
	mcpErrInvalidReq   = -32600
	mcpErrNotFound     = -32601
	mcpErrInvalidParam = -32602
	mcpErrInternal     = -32603
)

// mcpRequest is a JSON-RPC 2.0 request envelope.
// ID is kept as raw JSON so that its type (string / number / null) is
// reflected verbatim in the response.
type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// mcpResponse is a JSON-RPC 2.0 response envelope.
type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// mcpTextContent is an MCP text content block returned inside a tools/call result.
type mcpTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// serveMCP handles POST /mcp requests using the MCP Streamable HTTP transport.
func serveMCP(c *webContext) {
	var req mcpRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		mcpWriteError(c, nil, mcpErrParse, "parse error: "+err.Error())
		return
	}
	if req.JSONRPC != "2.0" {
		mcpWriteError(c, req.ID, mcpErrInvalidReq, `invalid jsonrpc version, expected "2.0"`)
		return
	}

	// Notifications carry no id. Acknowledge them with 202 and no body.
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	switch req.Method {
	case "initialize":
		semanticSearchEnabled := mcpSemanticSearchEnabled(c)
		mcpWriteResult(c, req.ID, map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":                  "hister",
				"version":               Version,
				"semanticSearchEnabled": semanticSearchEnabled,
			},
		})

	case "notifications/initialized", "notifications/cancelled":
		if isNotification {
			c.Response.WriteHeader(http.StatusAccepted)
			return
		}
		mcpWriteResult(c, req.ID, map[string]any{})

	case "ping":
		mcpWriteResult(c, req.ID, map[string]any{})

	case "tools/list":
		semanticSearchEnabled := mcpSemanticSearchEnabled(c)
		mcpWriteResult(c, req.ID, map[string]any{
			"tools":                 mcpToolList(semanticSearchEnabled),
			"semanticSearchEnabled": semanticSearchEnabled,
		})

	case "tools/call":
		mcpCallTool(c, req)

	default:
		if isNotification {
			c.Response.WriteHeader(http.StatusAccepted)
			return
		}
		mcpWriteError(c, req.ID, mcpErrNotFound, "unknown method: "+req.Method)
	}
}

func mcpSemanticSearchEnabled(c *webContext) bool {
	return c != nil && c.Config.SemanticSearch.Enable && indexer.SemanticSearchEnabled()
}

// mcpToolList returns the list of tools this MCP server exposes.
func mcpToolList(semanticSearchEnabled bool) []map[string]any {
	semanticStatus := "disabled"
	if semanticSearchEnabled {
		semanticStatus = "enabled"
	}
	return []map[string]any{
		{
			"name":        "search",
			"description": fmt.Sprintf("Search your personal browsing history and indexed documents. Returns titles, URLs, and text snippets for matching pages. Semantic search is currently %s on this Hister instance.", semanticStatus),
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type": "string",
						"description": `Search query. Supports plain keywords, "exact phrases", ` +
							`field filters (url:, domain:, title:, text:, language:, type:web/local), ` +
							`negation (-term), wildcards (term*), and disjunction (a|b|c).`,
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 10, max: 50).",
					},
					"date_from": map[string]any{
						"type":        "string",
						"description": `Only return documents updated on or after this date (ISO 8601, e.g. "2024-01-15").`,
					},
					"date_to": map[string]any{
						"type":        "string",
						"description": `Only return documents updated on or before this date (ISO 8601, e.g. "2024-06-30").`,
					},
					"semantic": map[string]any{
						"type":        "boolean",
						"description": fmt.Sprintf("Enable AI semantic similarity search alongside keyword matching. Current Hister instance semantic search status: %s.", semanticStatus),
					},
					"fields": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
							"enum": []string{"text", "html", "language", "label", "domain", "score", "type"},
						},
						"description": "Extra document fields to include in the response. " +
							`"text" returns the full stored article text instead of a short snippet. ` +
							`"html" returns the raw HTML. ` +
							`"language" returns the detected language code. ` +
							`"label" returns the user-defined label. ` +
							`"domain" returns the domain name. ` +
							`"score" returns the relevance score. ` +
							`"type" returns the document type (web or local).`,
					},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "get_preview",
			"description": "Retrieve the full preview of an indexed document by URL. Returns the page title, extracted text content, added and updated dates, and all available metadata (author, description, publication date, language, content type, JSON-LD structured data, embedded videos, etc.).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "The exact URL of the indexed document to preview.",
					},
					"extractor": map[string]any{
						"type":        "string",
						"description": "Name of the extractor to use for rendering the preview. Omit to use the default extractor.",
					},
				},
				"required": []string{"url"},
			},
		},
		{
			"name":        "get_history",
			"description": "Retrieve items shown in the Hister history view. Returns recently indexed pages by default, or opened search result history when mode is opened.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"mode": map[string]any{
						"type":        "string",
						"enum":        []string{"opened", "indexed"},
						"description": `History view mode to read. "indexed" returns recently indexed pages. "opened" returns search history items opened from Hister's result list. Default: indexed.`,
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of items to return (default: 20, max: 100).",
					},
					"last_id": map[string]any{
						"type":        "integer",
						"description": `Pagination cursor for opened mode. Use the "next_last_id" value from the previous response.`,
					},
					"page_key": map[string]any{
						"type":        "string",
						"description": `Pagination cursor for indexed mode. Use the "next_page_key" value from the previous response.`,
					},
				},
			},
		},
	}
}

// mcpCallTool dispatches a tools/call request to the appropriate tool handler.
func mcpCallTool(c *webContext, req mcpRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		mcpWriteError(c, req.ID, mcpErrInvalidParam, "invalid params: "+err.Error())
		return
	}
	switch params.Name {
	case "search":
		mcpToolSearch(c, req.ID, params.Arguments)
	case "get_preview":
		mcpToolGetPreview(c, req.ID, params.Arguments)
	case "get_history":
		mcpToolGetHistory(c, req.ID, params.Arguments)
	default:
		mcpWriteError(c, req.ID, mcpErrNotFound, "unknown tool: "+params.Name)
	}
}

type mcpSearchArgs struct {
	Query    string   `json:"query"`
	Limit    int      `json:"limit"`
	Semantic bool     `json:"semantic"`
	Fields   []string `json:"fields"`
	DateFrom string   `json:"date_from"`
	DateTo   string   `json:"date_to"`
}

// mcpToolSearch executes a Hister search and formats the results as MCP content.
func mcpToolSearch(c *webContext, id json.RawMessage, rawArgs json.RawMessage) {
	var args mcpSearchArgs
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			mcpWriteError(c, id, mcpErrInvalidParam, "invalid search arguments: "+err.Error())
			return
		}
	}
	if args.Query == "" {
		mcpWriteError(c, id, mcpErrInvalidParam, "query is required")
		return
	}
	if args.Limit <= 0 || args.Limit > 50 {
		args.Limit = 10
	}

	q := &indexer.Query{
		Text:            args.Query,
		Limit:           args.Limit,
		SemanticEnabled: args.Semantic && c.Config.SemanticSearch.Enable,
	}
	if args.DateFrom != "" {
		t, err := time.Parse("2006-01-02", args.DateFrom)
		if err != nil {
			mcpWriteError(c, id, mcpErrInvalidParam, "invalid date_from (expected YYYY-MM-DD): "+err.Error())
			return
		}
		q.DateFrom = t.Unix()
	}
	if args.DateTo != "" {
		t, err := time.Parse("2006-01-02", args.DateTo)
		if err != nil {
			mcpWriteError(c, id, mcpErrInvalidParam, "invalid date_to (expected YYYY-MM-DD): "+err.Error())
			return
		}
		// Use the final second of the given day, matching the HTTP date parameter.
		q.DateTo = t.AddDate(0, 0, 1).Add(-time.Second).Unix()
	}
	for _, f := range args.Fields {
		switch f {
		case "text":
			q.IncludeText = true
		case "html":
			q.IncludeHTML = true
		}
	}
	res, err := doSearch(q, c.Config, c.effectiveRules(), c.UserID, historyEnabled(c))
	if err != nil {
		log.Error().Err(err).Str("query", args.Query).Msg("MCP search failed")
		mcpWriteError(c, id, mcpErrInternal, "search failed")
		return
	}

	mcpWriteResult(c, id, map[string]any{
		"content": []mcpTextContent{
			{Type: "text", Text: mcpFormatResults(args.Query, res, args.Fields)},
		},
	})
}

type mcpGetPreviewArgs struct {
	URL       string `json:"url"`
	Extractor string `json:"extractor"`
}

type mcpGetHistoryArgs struct {
	Mode    string `json:"mode"`
	Limit   int    `json:"limit"`
	LastID  uint   `json:"last_id"`
	PageKey string `json:"page_key"`
}

type mcpHistoryOpenedItem struct {
	ID       uint
	URL      string
	Title    string
	Query    string
	Added    int64
	AddCount uint
}

// mcpToolGetPreview retrieves the stored preview for a document and returns its
// full content together with all available metadata.
func mcpToolGetPreview(c *webContext, id json.RawMessage, rawArgs json.RawMessage) {
	var args mcpGetPreviewArgs
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			mcpWriteError(c, id, mcpErrInvalidParam, "invalid get_preview arguments: "+err.Error())
			return
		}
	}
	if args.URL == "" {
		mcpWriteError(c, id, mcpErrInvalidParam, "url is required")
		return
	}

	doc := indexer.GetByURLAndUser(args.URL, c.UserID)
	if doc == nil {
		mcpWriteError(c, id, mcpErrNotFound, "document not found: "+args.URL)
		return
	}

	var content string
	if doc.HTML == "" {
		content = doc.Text
	} else {
		resp, err := extractor.Preview(doc, args.Extractor)
		if err != nil {
			log.Warn().Err(err).Str("url", args.URL).Msg("MCP get_preview extractor failed")
			content = doc.Text
		} else {
			content = resp.Content
		}
	}

	mcpWriteResult(c, id, map[string]any{
		"content": []mcpTextContent{
			{Type: "text", Text: mcpFormatPreview(doc.Title, args.URL, doc.Added, doc.Updated, doc.GetPreviewMeta(), content)},
		},
	})
}

func mcpToolGetHistory(c *webContext, id json.RawMessage, rawArgs json.RawMessage) {
	var args mcpGetHistoryArgs
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			mcpWriteError(c, id, mcpErrInvalidParam, "invalid get_history arguments: "+err.Error())
			return
		}
	}
	if !historyEnabled(c) {
		mcpWriteError(c, id, mcpErrNotFound, "history is disabled")
		return
	}
	if args.Mode == "" {
		args.Mode = "indexed"
	}
	if args.Limit <= 0 || args.Limit > 100 {
		args.Limit = 20
	}

	switch args.Mode {
	case "opened":
		items, err := model.GetLatestHistoryItems(c.UserID, args.Limit, args.LastID)
		if err != nil {
			log.Error().Err(err).Msg("MCP get_history opened mode failed")
			mcpWriteError(c, id, mcpErrInternal, "history lookup failed")
			return
		}
		historyItems := make([]mcpHistoryOpenedItem, 0, len(items))
		for _, item := range items {
			historyItems = append(historyItems, mcpHistoryOpenedItem{
				ID:       item.ID,
				URL:      item.URL,
				Title:    item.Title,
				Query:    item.Query,
				Added:    item.UpdatedAt.Unix(),
				AddCount: indexer.GetAddCountByURLAndUser(item.URL, c.UserID),
			})
		}
		var nextLastID uint
		if len(historyItems) == args.Limit && len(historyItems) > 0 {
			nextLastID = historyItems[len(historyItems)-1].ID
		}
		mcpWriteResult(c, id, map[string]any{
			"content": []mcpTextContent{
				{Type: "text", Text: mcpFormatOpenedHistory(historyItems, nextLastID)},
			},
		})

	case "indexed":
		res := indexer.GetLatestDocuments(args.Limit, args.PageKey, c.UserID)
		mcpWriteResult(c, id, map[string]any{
			"content": []mcpTextContent{
				{Type: "text", Text: mcpFormatIndexedHistory(res)},
			},
		})

	default:
		mcpWriteError(c, id, mcpErrInvalidParam, `mode must be "opened" or "indexed"`)
	}
}

// mcpFormatPreview renders a document preview as a human-readable text block
// with title, URL, added and updated dates, all metadata fields, and extracted content.
func mcpFormatPreview(title, url string, added, updated int64, meta map[string]any, content string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", title)
	fmt.Fprintf(&b, "URL: %s\n", url)
	fmt.Fprintf(&b, "Added: %s\n", time.Unix(added, 0).Format("2006-01-02"))
	fmt.Fprintf(&b, "Updated: %s\n", time.Unix(updated, 0).Format("2006-01-02"))

	if meta != nil {
		for _, k := range []string{"author", "published", "modified", "description", "site_name", "type", "language", "image"} {
			if v, ok := meta[k].(string); ok && v != "" {
				fmt.Fprintf(&b, "%s: %s\n", strings.Title(k), v) //nolint:staticcheck
			}
		}
		if nodes, ok := meta["jsonld"].([]map[string]any); ok && len(nodes) > 0 {
			if raw, err := json.Marshal(nodes); err == nil {
				fmt.Fprintf(&b, "JSON-LD: %s\n", raw)
			}
		}
		if videos, ok := meta["videos"].([]map[string]any); ok && len(videos) > 0 {
			fmt.Fprintf(&b, "Embedded videos (%d):\n", len(videos))
			for _, v := range videos {
				fmt.Fprintf(&b, "  - %s (%s)\n", v["url"], v["type"])
			}
		}
	}

	if t := strings.TrimSpace(content); t != "" {
		fmt.Fprintf(&b, "\n--- Content ---\n%s\n", t)
	}
	return b.String()
}

func mcpFormatOpenedHistory(items []mcpHistoryOpenedItem, nextLastID uint) string {
	if len(items) == 0 {
		return "No opened history items found."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Opened history items: %d\n", len(items))
	if nextLastID > 0 {
		fmt.Fprintf(&b, "next_last_id: %d\n", nextLastID)
	}
	for n, item := range items {
		added := time.Unix(item.Added, 0).Format("2006-01-02 15:04")
		title := item.Title
		if title == "" {
			title = item.URL
		}
		fmt.Fprintf(&b, "\n%d. %s\n   URL: %s\n   Query: %s\n   Opened: %s\n", n+1, title, item.URL, item.Query, added)
		if item.AddCount > 0 {
			fmt.Fprintf(&b, "   Indexed versions: %d\n", item.AddCount)
		}
	}
	return b.String()
}

func mcpFormatIndexedHistory(res *indexer.Results) string {
	if res == nil || len(res.Documents) == 0 {
		return "No indexed history items found."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Indexed history items: %d\n", len(res.Documents))
	if res.PageKey != "" {
		fmt.Fprintf(&b, "next_page_key: %s\n", res.PageKey)
	}
	for n, d := range res.Documents {
		updated := time.Unix(d.Updated, 0).Format("2006-01-02 15:04")
		title := d.Title
		if title == "" {
			title = d.URL
		}
		fmt.Fprintf(&b, "\n%d. %s\n   URL: %s\n   Updated: %s\n", n+1, title, d.URL, updated)
		if d.AddCount > 0 {
			fmt.Fprintf(&b, "   Indexed versions: %d\n", d.AddCount)
		}
	}
	return b.String()
}

// mcpFormatResults renders search results as a human-readable text block.
// fields is the optional list of extra document fields requested by the caller.
func mcpFormatResults(query string, res *indexer.Results, fields []string) string {
	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[f] = true
	}

	total := int(res.Total) + len(res.History)
	if total == 0 {
		return fmt.Sprintf("No results found for %q.", query)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d result(s) for %q (%s)\n", total, query, res.SearchDuration)

	n := 1
	renderedURLs := make(map[string]struct{}, len(res.History)+len(res.Documents))
	for _, h := range res.History {
		fmt.Fprintf(&b, "\n%d. %s\n   URL: %s\n", n, h.Title, h.URL)
		renderedURLs[h.URL] = struct{}{}
		if t := strings.TrimSpace(h.Text); t != "" {
			if fieldSet["text"] {
				fmt.Fprintf(&b, "   Text: %s\n", t)
			} else {
				fmt.Fprintf(&b, "   %s\n", mcpTruncate(t, 300))
			}
		}
		n++
	}
	for _, d := range res.Documents {
		mcpFormatDocumentResult(&b, n, d, fieldSet, 0, "")
		renderedURLs[d.URL] = struct{}{}
		n++
	}
	for _, h := range res.SemanticHits {
		if h.Document == nil {
			continue
		}
		if _, found := renderedURLs[h.Document.URL]; found {
			continue
		}
		mcpFormatDocumentResult(&b, n, h.Document, fieldSet, h.Similarity, h.MatchedChunk)
		renderedURLs[h.Document.URL] = struct{}{}
		n++
	}
	return b.String()
}

func mcpFormatDocumentResult(b *strings.Builder, n int, d *document.Document, fieldSet map[string]bool, similarity float64, matchedChunk string) {
	added := time.Unix(d.Added, 0).Format("2006-01-02")
	fmt.Fprintf(b, "\n%d. %s\n   URL: %s\n   Added: %s\n", n, d.Title, d.URL, added)
	if d.Updated != 0 {
		updated := time.Unix(d.Updated, 0).Format("2006-01-02")
		fmt.Fprintf(b, "   Updated: %s\n", updated)
	}
	if fieldSet["domain"] && d.Domain != "" {
		fmt.Fprintf(b, "   Domain: %s\n", d.Domain)
	}
	if fieldSet["language"] && d.Language != "" {
		fmt.Fprintf(b, "   Language: %s\n", d.Language)
	}
	if fieldSet["label"] && d.Label != "" {
		fmt.Fprintf(b, "   Label: %s\n", d.Label)
	}
	if fieldSet["score"] {
		if similarity != 0 {
			fmt.Fprintf(b, "   Similarity: %.4f\n", similarity)
		} else {
			fmt.Fprintf(b, "   Score: %.4f\n", d.Score)
		}
	}
	if fieldSet["type"] {
		fmt.Fprintf(b, "   Type: %s\n", d.Type.String())
	}
	text := strings.TrimSpace(d.Text)
	if text == "" {
		text = strings.TrimSpace(matchedChunk)
	}
	if text != "" {
		if fieldSet["text"] {
			fmt.Fprintf(b, "   Text: %s\n", text)
		} else {
			fmt.Fprintf(b, "   %s\n", mcpTruncate(text, 300))
		}
	}
	if fieldSet["html"] && d.HTML != "" {
		fmt.Fprintf(b, "   HTML: %s\n", d.HTML)
	}
}

// mcpTruncate truncates s at a rune boundary so that the result contains at most maxRunes runes.
func mcpTruncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

func mcpWriteResult(c *webContext, id json.RawMessage, result any) {
	c.JSON(mcpResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func mcpWriteError(c *webContext, id json.RawMessage, code int, message string) {
	c.JSON(mcpResponse{JSONRPC: "2.0", ID: id, Error: &mcpRPCError{Code: code, Message: message}})
}
