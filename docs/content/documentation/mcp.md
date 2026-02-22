+++
date = '2026-02-22T00:00:00+01:00'
draft = false
title = 'MCP Integration'
layout = 'documentation'
+++

Hister supports the [Model Context Protocol (MCP)](https://modelcontextprotocol.io), allowing AI assistants like [Claude](https://claude.ai) to search your browser history, index URLs, and manage your search index — directly from a chat interface, without opening a browser.

## How It Works

Running `hister mcp` starts a stdio MCP server. MCP clients connect to it as a subprocess and communicate via JSON-RPC over stdin/stdout. No network port is opened.

## Available Tools

| Tool | Input | Description |
|------|-------|-------------|
| `search` | `query` (string), `limit` (int, default 10) | Full-text search through indexed history |
| `index` | `url` (string) | Download and index a URL |
| `delete` | `url` (string) | Remove a URL from the index |
| `list_recent` | `limit` (int, default 10) | List most recently visited pages |
| `top_visited` | `limit` (int, default 10) | List most frequently visited pages, ordered by visit count |

## Setup

### Claude Code

Register Hister as a local MCP server using the Claude CLI:

```bash
claude mcp add hister /usr/local/bin/hister mcp
```

To verify it's working, restart Claude Code — the `hister` tools will appear in the MCP tools list.

### Claude Desktop

Add the following to your Claude Desktop configuration file:

**macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
**Windows**: `%APPDATA%\Claude\claude_desktop_config.json`
**Linux**: `~/.config/Claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "hister": {
      "command": "/usr/local/bin/hister",
      "args": ["mcp"]
    }
  }
}
```

Restart Claude Desktop after saving the file.

### Other MCP Clients

Any MCP-compatible client can use Hister. Configure it as a stdio server:

- **Command**: `hister`
- **Args**: `["mcp"]`

## Requirements

The Hister server (`hister listen`) does **not** need to be running for the MCP server to work. The MCP server accesses the search index and database directly.

However, the `index` tool does make an HTTP request to download the target URL, so an internet connection is required for that tool.

## Example Usage

Once configured, you can ask Claude things like:

- *"Search my browser history for articles about Rust error handling"*
- *"What are the 10 most visited sites in my history?"*
- *"Index this URL for me: https://example.com/article"*
- *"Show me the pages I visited most recently"*
- *"Delete https://example.com from my search index"*

## Configuration

The MCP server uses the same configuration file as the rest of Hister (`~/.config/hister/config.yml`). You can specify a custom config path with the `--config` flag:

```bash
hister --config /path/to/config.yml mcp
```

## Building from Source

If you build Hister from source, install it to your PATH before configuring MCP clients:

```bash
go install github.com/asciimoo/hister@latest
```

Or from a local clone:

```bash
git clone https://github.com/asciimoo/hister.git
cd hister
go install .
```
