package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/model"
	mcpmcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server (stdio)",
	Long: `Start a Model Context Protocol server over stdio.

The MCP server communicates with a running Hister HTTP server.
Start the Hister server first:
  hister listen

Then add to Claude Code with:
  claude mcp add hister /path/to/hister mcp

Or configure in .claude/settings.json:
  "mcpServers": {
    "hister": { "command": "/path/to/hister", "args": ["mcp"] }
  }`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMCPServer(cmd, args)
	},
}

func mcpHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

func runMCPServer(_ *cobra.Command, _ []string) error {
	s := server.NewMCPServer("hister", "v0.1.0")

	s.AddTool(
		mcpmcp.NewTool("search",
			mcpmcp.WithDescription("Search browser history by full-text query"),
			mcpmcp.WithString("query",
				mcpmcp.Required(),
				mcpmcp.Description("Search query text"),
			),
			mcpmcp.WithNumber("limit",
				mcpmcp.Description("Maximum number of results to return (default 10)"),
			),
		),
		handleSearch,
	)

	s.AddTool(
		mcpmcp.NewTool("index",
			mcpmcp.WithDescription("Download and index a URL into the search index"),
			mcpmcp.WithString("url",
				mcpmcp.Required(),
				mcpmcp.Description("URL to download and index"),
			),
		),
		handleIndex,
	)

	s.AddTool(
		mcpmcp.NewTool("delete",
			mcpmcp.WithDescription("Delete a URL from the search index"),
			mcpmcp.WithString("url",
				mcpmcp.Required(),
				mcpmcp.Description("URL to delete from the index"),
			),
		),
		handleDelete,
	)

	s.AddTool(
		mcpmcp.NewTool("list_recent",
			mcpmcp.WithDescription("List the most recently visited pages from browser history"),
			mcpmcp.WithNumber("limit",
				mcpmcp.Description("Maximum number of results to return (default 10)"),
			),
		),
		handleListRecent,
	)

	s.AddTool(
		mcpmcp.NewTool("top_visited",
			mcpmcp.WithDescription("List the most frequently visited pages from browser history"),
			mcpmcp.WithNumber("limit",
				mcpmcp.Description("Maximum number of results to return (default 10)"),
			),
		),
		handleTopVisited,
	)

	return server.ServeStdio(s)
}

func handleSearch(_ context.Context, req mcpmcp.CallToolRequest) (*mcpmcp.CallToolResult, error) {
	q := mcpmcp.ParseString(req, "query", "")
	if q == "" {
		return mcpmcp.NewToolResultError("query parameter is required"), nil
	}
	limit := mcpmcp.ParseInt(req, "limit", 10)
	if limit <= 0 {
		limit = 10
	}

	apiURL := cfg.BaseURL("/search") + "?q=" + url.QueryEscape(q) + fmt.Sprintf("&limit=%d", limit)
	httpReq, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to create request", err), nil
	}
	httpReq.Header.Set("Origin", cfg.BaseURL("/"))

	resp, err := mcpHTTPClient().Do(httpReq)
	if err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to reach Hister server — is it running?", err), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to read response", err), nil
	}

	var results indexer.Results
	if err := json.Unmarshal(body, &results); err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to parse response", err), nil
	}

	if len(results.Documents) == 0 {
		return mcpmcp.NewToolResultText("No results found."), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d result(s) for %q:\n\n", results.Total, q)
	for i, doc := range results.Documents {
		fmt.Fprintf(&sb, "[%d] %s\n    %s\n\n", i+1, doc.Title, doc.URL)
	}

	return mcpmcp.NewToolResultText(sb.String()), nil
}

func handleIndex(_ context.Context, req mcpmcp.CallToolRequest) (*mcpmcp.CallToolResult, error) {
	u := mcpmcp.ParseString(req, "url", "")
	if u == "" {
		return mcpmcp.NewToolResultError("url parameter is required"), nil
	}

	if err := indexURL(u); err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to index URL", err), nil
	}

	return mcpmcp.NewToolResultText("Indexed: " + u), nil
}

func handleDelete(_ context.Context, req mcpmcp.CallToolRequest) (*mcpmcp.CallToolResult, error) {
	u := mcpmcp.ParseString(req, "url", "")
	if u == "" {
		return mcpmcp.NewToolResultError("url parameter is required"), nil
	}

	formData := url.Values{"url": {u}}
	httpReq, err := http.NewRequest("POST", cfg.BaseURL("/delete"), strings.NewReader(formData.Encode()))
	if err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to create request", err), nil
	}
	httpReq.Header.Set("Origin", "hister://")
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := mcpHTTPClient().Do(httpReq)
	if err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to reach Hister server — is it running?", err), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return mcpmcp.NewToolResultError(fmt.Sprintf("delete failed: status %d", resp.StatusCode)), nil
	}

	return mcpmcp.NewToolResultText("Deleted: " + u), nil
}

func handleListRecent(_ context.Context, req mcpmcp.CallToolRequest) (*mcpmcp.CallToolResult, error) {
	limit := mcpmcp.ParseInt(req, "limit", 10)
	if limit <= 0 {
		limit = 10
	}

	apiURL := cfg.BaseURL("/api/history") + fmt.Sprintf("?kind=recent&limit=%d", limit)
	httpReq, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to create request", err), nil
	}
	httpReq.Header.Set("Origin", "hister://")

	resp, err := mcpHTTPClient().Do(httpReq)
	if err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to reach Hister server — is it running?", err), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to read response", err), nil
	}

	var items []*model.HistoryItem
	if err := json.Unmarshal(body, &items); err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to parse response", err), nil
	}

	if len(items) == 0 {
		return mcpmcp.NewToolResultText("No recent history found."), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Recent history (%d items):\n\n", len(items))
	for i, item := range items {
		title := item.Title
		if title == "" {
			title = "(no title)"
		}
		fmt.Fprintf(&sb, "[%d] %s\n    %s\n", i+1, title, item.URL)
		if item.Query != "" {
			fmt.Fprintf(&sb, "    Query: %s\n", item.Query)
		}
		fmt.Fprintln(&sb)
	}

	return mcpmcp.NewToolResultText(sb.String()), nil
}

func handleTopVisited(_ context.Context, req mcpmcp.CallToolRequest) (*mcpmcp.CallToolResult, error) {
	limit := mcpmcp.ParseInt(req, "limit", 10)
	if limit <= 0 {
		limit = 10
	}

	apiURL := cfg.BaseURL("/api/history") + fmt.Sprintf("?kind=top&limit=%d", limit)
	httpReq, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to create request", err), nil
	}
	httpReq.Header.Set("Origin", "hister://")

	resp, err := mcpHTTPClient().Do(httpReq)
	if err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to reach Hister server — is it running?", err), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to read response", err), nil
	}

	var items []*model.URLCount
	if err := json.Unmarshal(body, &items); err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to parse response", err), nil
	}

	if len(items) == 0 {
		return mcpmcp.NewToolResultText("No visited URLs found."), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Top visited URLs (%d items):\n\n", len(items))
	for i, item := range items {
		title := item.Title
		if title == "" {
			title = "(no title)"
		}
		fmt.Fprintf(&sb, "[%d] %s (%d visits)\n    %s\n\n", i+1, title, item.Count, item.URL)
	}

	return mcpmcp.NewToolResultText(sb.String()), nil
}

