# Hister

**Web history on steroids**

Hister is a web history management tool that provides blazing fast, content-based search for visited websites. Unlike traditional browser history that only searches URLs and titles, Hister indexes the full content of web pages you visit.

![hister screenshot](docs/assets/screenshot.png)

![hister screencast](docs/assets/demo.gif)


## Features

- **Privacy-focused**: Keep your browsing history indexed locally - don't use remote search engines if it isn't necessary
- **Full-text indexing**: Search through the actual content of web pages you've visited
- **Advanced search capabilities**: Utilize a powerful [query language](https://blevesearch.com/docs/Query-String-Query/) for precise results
- **Efficient retrieval**: Use keyword aliases to quickly find content
- **Flexible content management**: Configure blacklist and priority rules for better control
- **AI assistant integration**: Use Hister as an [MCP server](#mcp-model-context-protocol-integration) for Claude and other AI tools

## Setup & run

### Install the extension

Available for [Chrome](https://chromewebstore.google.com/detail/hister/cciilamhchpmbdnniabclekddabkifhb) and [Firefox](https://addons.mozilla.org/en-US/firefox/addon/hister/)

### Download pre-built binary

Grab a pre-built binary from the [latest release](https://github.com/asciimoo/hister/releases/latest). (Don't forget to `chmod +x`)

Execute `./hister` to see all available commands.

### Build for yourself

 - Clone the repository
 - Build with `go build`
 - Run `./hister help` to list the available commands
 - Execute `./hister listen` to start the web application

### Use pre-built [Docker container](https://github.com/asciimoo/hister/pkgs/container/hister)

## Configuration

Settings can be configured in `~/.config/hister/config.yml` config file - don't forget to restart webapp after updating.

Execute `./hister create-config config.yml` to generate a configuration file with the default configuration values.


## MCP (Model Context Protocol) Integration

Hister can act as an MCP server, allowing AI assistants like [Claude](https://claude.ai) to search, index, and manage your browser history directly.

```bash
hister mcp
```

This starts a stdio MCP server exposing 5 tools:

| Tool | Description |
|------|-------------|
| `search` | Full-text search through indexed history |
| `index` | Download and index a URL |
| `delete` | Remove a URL from the index |
| `list_recent` | List recently visited pages |
| `top_visited` | List most frequently visited pages |

### Claude Code

```bash
claude mcp add hister /usr/local/bin/hister mcp
```

### Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

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

See the [MCP documentation](docs/content/documentation/mcp.md) for details.

## Check out our [Documentation](https://hister.org/documentation/) for more details

## Bugs

Bugs or suggestions? Visit the [issue tracker](https://github.com/asciimoo/hister/issues).


## License

AGPLv3
