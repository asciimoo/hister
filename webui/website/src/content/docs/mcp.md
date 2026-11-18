---
date: '2026-04-20T00:00:00+00:00'
draft: false
title: 'MCP Integration'
description: 'Connect assistants to Nogon search, previews, and history through the authenticated MCP endpoint.'
---

Nogon exposes a [Model Context Protocol](https://modelcontextprotocol.io) (MCP)
endpoint that lets AI assistants read from your Nogon index directly. Once
connected, the assistant can search indexed pages, explore search counts,
retrieve stored previews, and inspect Nogon history through MCP tools.

## Endpoint

```
POST /mcp
```

Configure MCP clients to use this URL with the Streamable HTTP transport.

MCP applies Nogon's usual origin checks when a client sends an `Origin` header.
The `nogon://` origin and its legacy alias `hister://` are supported. `localhost`
and `127.0.0.1` are interchangeable for local configurations with the same scheme and port. Requests
from unrelated origins receive HTTP 403, even with a valid access token or login
session. Native clients that omit `Origin` continue to use the normal
authentication settings.

When using a reverse proxy, set `server.base_url` to the public URL used by your
client. Browser clients on a different origin are not supported.

## Untrusted Content And Prompt Injection

Every indexed title, URL, metadata value, document body, and history field is
untrusted source data. A page can contain instructions aimed at the assistant
that reads it. Those instructions must never override the user request, cause
secret disclosure, or trigger another tool.

Tool responses place source controlled values under
`structuredContent.untrusted_content`. Every record has `trust: "untrusted"`
and `trust_scope: "all values in fields"`. A security instruction identifies
the exact untrusted path. The required text content block contains the same
structured JSON after a security notice.

Nogon removes invisible control characters. HTML is returned only when
explicitly requested by `search`, or as a rendered preview from `get_preview`.
It remains inside the untrusted structured record. These controls reduce risk
but cannot guarantee that every consuming model will resist prompt injection.
MCP clients must sanitize HTML before rendering it and should require user
confirmation before any action outside read only retrieval, especially before
using file, shell, browser, email, or network tools.

## Authentication

The default Nogon configuration does not require authentication. Authentication
is required only when Nogon is configured with `app.access_token` or
`app.user_handling`. The MCP endpoint uses the same token authentication as the
rest of the Nogon API.

### Static access token

Pass the value of `app.access_token` from your config file:

```http
Authorization: Bearer <your-access-token>
```

Alternatively, use the `X-Access-Token` header with the same value.

### Multi-user mode

Generate a personal token on the profile page (`/profile`) or via:

```bash
nogon user update <username> --regen-token
```

Then pass it the same way:

```http
Authorization: Bearer <your-user-token>
```

### Public mode

When `app.public: true` is enabled, unauthenticated MCP access is allowed for
public routes. MCP tools can read public search results, facet counts, and previews
for global documents. The `get_history` tool remains unavailable to anonymous callers but
is enabled when the request includes a valid global or personal access token.

## Available Tools

### `search`

Search your personal browsing history and indexed documents.

| Argument    | Type            | Required | Default | Description                                                                 |
| ----------- | --------------- | -------- | ------- | --------------------------------------------------------------------------- |
| `query`     | string          | yes      |         | Search query (see [Query Language](/docs/query-language))                   |
| `limit`     | integer         | no       | 10      | Maximum results to return. Values below 1 or above 50 use the default.      |
| `page_key`  | string          | no       |         | Pagination cursor. Use `next_page_key` from the previous response.          |
| `date_from` | string          | no       |         | Return only documents updated on or after this date. Format: `YYYY-MM-DD`.  |
| `date_to`   | string          | no       |         | Return only documents updated on or before this date. Format: `YYYY-MM-DD`. |
| `semantic`  | boolean         | no       | false   | Enable AI semantic search alongside keyword matching                        |
| `fields`    | array of string | no       | `[]`    | Extra fields to include in each result. See below.                          |

The limit applies to the combined list of history, keyword, and semantic matches.
Matching history entries come first, followed by keyword matches and additional
semantic matches.

To continue a search, pass the response's `trusted.next_page_key` as `page_key`.
Keep the same query, date filters, and semantic setting. You can change `limit`
between requests. Continue until `next_page_key` is absent, even if a page has
fewer results than the requested limit. A final request can return an empty page.
Results can shift if documents or opened history change between requests.

Semantic search is used only when it is enabled and available on the Nogon
server. If the server does not have semantic search configured, `"semantic":
true` falls back to normal keyword search.

By default the response includes title, URL, added and updated dates, and a short text
snippet per result. Pass `fields` to include additional data:

| Field value | Description                                      |
| ----------- | ------------------------------------------------ |
| `text`      | Full stored article text instead of a snippet    |
| `html`      | Raw HTML in an untrusted structured field        |
| `language`  | Detected language code, for example `en` or `de` |
| `label`     | User defined label                               |
| `domain`    | Domain name                                      |
| `score`     | Relevance score                                  |
| `type`      | Document type, either `web` or `local`           |

Example: to summarize articles on a topic without re-fetching any URLs:

```json
{ "query": "kubernetes networking", "limit": 5, "fields": ["text"] }
```

Example with a date range:

```json
{ "query": "postgres migration", "date_from": "2026-01-01", "date_to": "2026-01-31" }
```

### `get_facets`

Count indexed documents matching a query by domain, language, document type,
visit count, and update date. Use these counts to choose filters before requesting
search results. Omitting the query summarizes all documents you can access.

| Argument    | Type    | Required | Default | Description                                                                  |
| ----------- | ------- | -------- | ------- | ---------------------------------------------------------------------------- |
| `query`     | string  | no       | `*`     | Search query with the same filters and aliases as `search`.                  |
| `limit`     | integer | no       | 10      | Maximum domain and language values. Values outside 1 to 100 use the default. |
| `date_from` | string  | no       |         | Count documents updated on or after this date. Format: `YYYY-MM-DD`.         |
| `date_to`   | string  | no       |         | Count documents updated on or before this date. Format: `YYYY-MM-DD`.        |

Counts cover matching indexed documents. Opened history and semantic matches
are excluded. Anonymous callers receive counts for global documents; signed in
users also receive counts for their own documents.

Domain and language counts include an `other` count when additional values fall
outside the limit. Type, visit count, and date groups use predefined ranges.
Date ranges overlap, so their counts should not be added together.

```json
{ "query": "python", "limit": 5 }
```

Use a returned domain or language to narrow a subsequent search, for example
`python domain:docs.python.org` or `python language:en`.

### `get_preview`

Retrieve the stored preview for an indexed document by exact URL.

| Argument      | Type            | Required | Default                        | Description                                                            |
| ------------- | --------------- | -------- | ------------------------------ | ---------------------------------------------------------------------- |
| `url`         | string          | yes      |                                | Exact URL of the indexed document to preview.                          |
| `extractor`   | string          | no       |                                | Extractor name used to render the HTML preview.                        |
| `fields`      | array of string | no       | `["text", "html", "metadata"]` | Content to include. An empty array returns only title, URL, and dates. |
| `max_chars`   | integer         | no       | 0                              | Maximum characters per text or HTML field. Zero means unlimited.       |
| `text_offset` | integer         | no       | 0                              | Position in plain text. Use `next_text_offset` to continue.            |
| `html_offset` | integer         | no       | 0                              | Position in rendered HTML. Use `next_html_offset` to continue.         |

By default, the response contains the document title, URL, added and updated dates,
available preview metadata, complete stored plain text, and complete rendered
HTML when available. Metadata can include author, published date, modified date,
description, site name, type, language, image, JSON LD structured data, and
embedded video URLs. Rendered HTML remains untrusted data and clients must
sanitize it before placing it in a browser or another HTML renderer.

Example:

```json
{ "url": "https://example.com/article" }
```

To read a long article in smaller portions without receiving HTML:

```json
{
  "url": "https://example.com/article",
  "fields": ["text", "metadata"],
  "max_chars": 4000
}
```

When more text is available, the response includes `trusted.next_text_offset`.
Pass it as `text_offset` in the next call with the same URL and `max_chars`.
HTML uses `next_html_offset` and `html_offset` independently. The `trusted`
object also reports `text_total_chars`, `html_total_chars`, and whether each
returned body has more content through `text_truncated` and `html_truncated`.
Metadata is returned in full when selected.

Keep the same extractor while reading successive HTML portions. Join all HTML
portions in order before sanitizing or displaying them. If the document changes
between calls, restart from offset zero to read a consistent copy.

### `get_history`

Retrieve items from the Nogon history views. This tool is available only when
public mode is disabled or the caller is authenticated.

| Argument          | Type    | Required | Default   | Description                                                                             |
| ----------------- | ------- | -------- | --------- | --------------------------------------------------------------------------------------- |
| `mode`            | string  | no       | `indexed` | `indexed` returns recently indexed pages. `opened` returns opened result history.       |
| `filter`          | string  | no       |           | Filter titles and URLs using the same text matching as the history view.                |
| `date_from`       | string  | no       |           | Return items last opened or indexed on or after this UTC date. Format: `YYYY-MM-DD`.    |
| `date_to`         | string  | no       |           | Return items last opened or indexed on or before this UTC date. Format: `YYYY-MM-DD`.   |
| `limit`           | integer | no       | 20        | Maximum items to return. Values below 1 or above 100 use the default.                   |
| `page_key`        | string  | no       |           | Pagination cursor for `indexed` mode. Use `next_page_key` from the previous response.   |
| `last_id`         | integer | no       |           | ID cursor for `opened` mode. Use `next_last_id` together with `last_updated_at`.        |
| `last_updated_at` | string  | no       |           | Timestamp cursor for `opened` mode. Use `next_last_updated_at` together with `last_id`. |

Indexed history results include title, URL, indexed time, indexed version count,
and `next_page_key` when another page is available. Opened history results
include title, URL, original query, opened time, and indexed version count.
When an opened history page is full, its `trusted` object includes
`next_last_id` and `next_last_updated_at`. Pass both values as `last_id` and
`last_updated_at` to request the next page. Copy the RFC 3339 timestamp exactly,
including fractional seconds. Using only `last_id` can skip entries when an older
result has been reopened. A final request can return an empty page.

`filter` matches titles and URLs, ignoring case and surrounding whitespace.
It does not search page content or the original query, and it does not accept
the `search` tool's query syntax. Dates refer to the last opening in `opened`
mode and the last indexing update in `indexed` mode. Both dates include the
entire UTC day, and `date_from` must be on or before `date_to`. Keep the same
text and date filters when requesting another page.

Examples:

```json
{ "mode": "indexed", "limit": 20 }
```

```json
{ "mode": "opened", "limit": 20 }
```

To find opened pages about Go during January:

```json
{
  "mode": "opened",
  "filter": "golang",
  "date_from": "2026-01-01",
  "date_to": "2026-01-31"
}
```

To continue opened history using the cursor from the previous response:

```json
{
  "mode": "opened",
  "limit": 20,
  "last_id": 42,
  "last_updated_at": "2026-10-16T12:00:00.123456Z"
}
```

## Client Configuration

The examples use `nogon` as the connection name. Existing connections named
`hister` continue to work with the same endpoint and credentials.

### Claude Desktop

Add a `nogon` entry to your Claude Desktop configuration file.

**macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`  
**Windows**: `%APPDATA%\Claude\claude_desktop_config.json`  
**Linux**: `~/.config/Claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "nogon": {
      "url": "http://127.0.0.1:4433/mcp",
      "headers": {
        "Authorization": "Bearer <your-access-token>"
      }
    }
  }
}
```

Restart Claude Desktop after saving the file. The `search` tool will appear
in the tools panel when starting a new conversation.

### Cursor

Open **Settings** and locate the MCP servers section, or edit
`~/.cursor/mcp.json` directly:

```json
{
  "mcpServers": {
    "nogon": {
      "url": "http://127.0.0.1:4433/mcp",
      "headers": {
        "Authorization": "Bearer <your-access-token>"
      }
    }
  }
}
```

### Remote or self-hosted server

Replace `http://127.0.0.1:4433` with your server's `base_url`. If you run
Nogon behind a reverse proxy under a subpath (e.g. `https://example.com/nogon`),
the endpoint is `https://example.com/nogon/mcp`.

## Example Interaction

Once connected, you can ask the assistant things like:

> "Search my history for anything about Rust error handling."

The assistant calls the `search` tool with `query: "rust error handling"` and
includes the results in its response, citing the specific pages you previously
read.

You can also ask:

> "Open the stored preview for the article I read about SQLite migrations."

The assistant can search first, then call `get_preview` with the selected URL.

Semantic search can be enabled per query by passing `"semantic": true` in the
tool arguments. This requires [semantic search to be configured](/docs/configuration#semantic-search)
on the server.
