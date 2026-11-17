---
date: '2026-03-14T11:49:00+00:00'
draft: false
title: 'Indexing Local Files with Hister'
description: "How to search your local documents, notes, and code alongside your browser history with Hister's new file indexing feature"
---

<script>
  import { CalloutNote } from '@nogon/components';
</script>

One of the most exciting features we've added to Hister is the ability to automatically index and search local files alongside your browser history. This means you can now find meeting notes, project documents, org files just as easily as you'd find a web page you visited last week.

<CalloutNote title="Note">
  Local file indexing is an optional feature and is <b>turned off by default</b>. You need to explicitly configure <code>indexer.directories</code> in your Hister config to enable it.
</CalloutNote>

## Setting It Up

Add an `indexer.directories` section to your Hister configuration:

```yaml
indexer:
  directories:
    - path: ~/Documents
      label: documents
      filetypes: ['md', 'txt']
    - path: ~/code/projects
      filetypes: ['txt']
      excludes: [, 'secret/*', '*key']
    - path: ~/notes
      patterns: ['*.org', 'doc_*', 'README']
```

Each directory configuration lets you specify:

- **path**: The directory to index (supports `~` for home directory)
- **label**: A searchable label applied to every indexed file in the directory
- **filetypes**: File extensions to include (e.g., `md`, `txt`, `py`)
- **patterns**: Glob patterns for more precise matching
- **excludes**: Patterns to skip (like build directories or dependencies)

Once configured, start your Hister server and it will automatically begin indexing.

## Why Index Local Files?

If you're like most developers and knowledge workers, you have important information scattered across two worlds: web pages you've browsed and local files on your computer. Before this feature, you'd need to use separate tools to search these two sources. With Hister's file indexing, everything lives in one searchable index.

Imagine searching for "authentication implementation" and getting results from:

- Stack Overflow pages you visited
- Your project's auth code files
- That README you wrote about setting up OAuth
- Blog posts you bookmarked

All in one place, sorted by relevance.

## How It Works

Hister's file indexing is designed to be simple and automatic. When the server starts, it indexes files from directories you've configured, then monitors those directories for changes. New or modified files are automatically re-indexed without requiring a server restart.

### What Gets Indexed

By default, Hister indexes text-based files that are:

- Under 1MB in size (configurable)
- Valid UTF-8 text
- Not hidden (files starting with `.` are skipped)

You have full control over which file types and patterns to include or exclude. Common formats like `.md`, `.txt`, `.py`, `.js`, `.go`, `.rs`, and many more are supported out of the box.

Later on we'd like to support other formats as well. (`pdf`, `docx`...)

### Smart Filtering

Hidden files and directories (those starting with `.`) are automatically skipped to avoid indexing system files, git repositories' internal files, and build artifacts. You can also configure custom patterns to include only specific file types or exclude certain paths.

## Searching Local Files

Local files appear in your search results just like web pages, with their file paths displayed instead of URLs. The search experience is seamless, Hister treats local content and web content equally, ranking results by relevance.

When you open a result that's a local file (identified by the `file://` URL scheme), Hister serves the content through its web interface, preserving your searchable history while giving you quick access to the actual file.

## Use Cases

Here are some ways we're using local file indexing:

**Code Search Across Projects**: Index your source code directories to find function definitions, API examples, and implementation patterns across all your projects.

**Personal Knowledge Base**: Index your markdown notes, org-mode files, or plain text journals to build a searchable second brain.

**Documentation**: Keep your project READMEs, technical specs, and design documents searchable alongside the web resources you reference.

**Configuration Files**: Find that one config setting you wrote months ago across dozens of dotfiles and config directories.

## Privacy and Security

Since Hister is self-hosted, all your local files remain on your machine. The indexing happens locally, and nothing is sent to external servers.

**Important**: If you're making Hister accessible over the network or internet, you **must** secure it properly. Your indexed files may contain sensitive information like code with API keys, personal notes, configuration files with credentials, or proprietary documentation.

We strongly recommend:

1. **Use the access token** - Set `app.access_token` in your configuration to require authentication for all API requests. The browser extension supports this natively.

2. **Deploy behind a reverse proxy** - Use nginx, Caddy, or Apache with HTTPS and additional authentication (HTTP basic auth, OAuth, etc.).

3. **Keep it local** - Only expose Hister on localhost (127.0.0.1) and access it through SSH tunnels or VPNs when away from your machine.

4. **Review your firewall** - Ensure your server's firewall rules don't accidentally expose Hister to the public internet.

Remember: indexing your local files means they become searchable through Hister's web interface. Only expose your Hister instance on networks you trust, and always use authentication when making it available beyond localhost.

## Future Improvements

We'd like to improve the file indexing feature by:

- Supporting new file types
- Command line interface to list indexed/excluded files
- Better handling of structured formats (JSON, YAML, TOML)
- Incremental indexing improvements for very large directories

## Get Started

Local file indexing is available now in Hister. Update your configuration, restart your server, and start searching your local files alongside your browsing history. Check out the [configuration documentation](/docs/configuration) for complete setup instructions.

Have ideas for improving file indexing? Join the discussion on [GitHub](https://github.com/asciimoo/hister) or reach out on social media. We'd love to hear how you're using this feature!
