package main

import (
	"context"
	"fmt"
	"strings"

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

Add to Claude Code with:
  hister mcp

Or configure in .claude/settings.json:
  "mcpServers": {
    "hister": { "command": "/path/to/hister", "args": ["mcp"] }
  }`,
	PreRun: func(_ *cobra.Command, _ []string) {
		initIndex()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMCPServer(cmd, args)
	},
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

	results, err := indexer.Search(cfg, &indexer.Query{
		Text:  q,
		Limit: limit,
	})
	if err != nil {
		return mcpmcp.NewToolResultErrorFromErr("search failed", err), nil
	}

	if results == nil || len(results.Documents) == 0 {
		return mcpmcp.NewToolResultText("No results found."), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d result(s) for %q:\n\n", results.Total, q)
	for i, doc := range results.Documents {
		fmt.Fprintf(&sb, "[%d] %s\n    %s\n    Score: %.4f\n\n", i+1, doc.Title, doc.URL, doc.Score)
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

	if err := indexer.Delete(u); err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to delete URL", err), nil
	}

	return mcpmcp.NewToolResultText("Deleted: " + u), nil
}

func handleListRecent(_ context.Context, req mcpmcp.CallToolRequest) (*mcpmcp.CallToolResult, error) {
	limit := mcpmcp.ParseInt(req, "limit", 10)
	if limit <= 0 {
		limit = 10
	}

	items, err := model.GetLatestHistoryItems(limit)
	if err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to get recent history", err), nil
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

	items, err := model.GetURLsByQuery("")
	if err != nil {
		return mcpmcp.NewToolResultErrorFromErr("failed to get top visited URLs", err), nil
	}

	if len(items) == 0 {
		return mcpmcp.NewToolResultText("No visited URLs found."), nil
	}

	if len(items) > limit {
		items = items[:limit]
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
