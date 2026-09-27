# Changelog

## v0.20.0

### New Features

#### Browser Bookmark Imports

`hister import browser bookmarks` imports bookmarks from Firefox based browsers,
Chromium based browsers, and Ladybird. It supports automatic detection and
explicit `--browser` and `--db` options, applies the `bookmarks` label by default,
and uses persistent crawl jobs that can be interrupted and resumed.

The new `hister import browser history` command accepts the same named options.
The existing positional history command and older browser import jobs remain
supported.

#### Raindrop.io Imports

`hister import raindrop` imports bookmarks through the Raindrop.io API or from a
CSV export with `--input`. API imports preserve titles, dates, notes, highlights,
annotations, tags, favorites, and collection paths. Linked pages are downloaded
with the configured crawler, while bookmark details are retained if a page
cannot be retrieved. CSV input can also be read from standard input.

#### Continuous File Imports

`hister import file --watch` performs an initial scan and keeps remote file
snapshots updated as files change. It supports explicit files, recursive
directories, or configured directory rules, including labels and ownership.
Temporary server failures are retried while the command runs.

Watch mode handles file snapshots rather than restoring exports or archives.
Removing or renaming a source file leaves its previously imported snapshot in
the index.

#### Configuration and Diagnostics

The new `hister config` commands create default configuration, report its path,
show effective settings with credentials redacted, and validate configuration
and extractor options. Inspection does not create runtime files.

`hister doctor` checks local configuration and extractor dependencies, then
verifies server connectivity, authentication, index compatibility, and server
extractor dependencies. It supports structured output and uses the new
`GET /api/diagnostics` endpoint, which requires an administrator in multiple
user mode.

#### Allow Rules and Guided Rule Editing

The new `allow` rule group restricts indexing to URLs matching at least one
allowed pattern. Skip rules still take precedence, and an empty allow list
preserves the existing behavior. The Rules page can now create patterns from
domains or exact URLs, optionally include subdomains, and validate and test
patterns before saving them.

Manual indexing from the browser extension and `--ignore-rules` on `hister index`
or import commands can override allow and skip rules. This choice is saved with
the document and survives reindexing. Sensitive content checks, robots rules,
and source selection filters still apply.

#### Structured Command Line Output

Search, indexing summaries, crawl inspection, diagnostics, and file and service
import summaries share text, JSON, JSONL, and CSV output formats. Search results
stream as pages arrive and preserve selected field order in text and CSV.

Indexing reports indexed, skipped, and failed counts and continues after
individual URL failures. `hister index --failed-urls PATH` writes failed URLs to
a file that can be reused with `--input`.

#### Prometheus Monitoring and Unix Sockets

Setting `server.metrics: true` enables `GET /metrics` with search, indexing,
document count, storage, Go runtime, and process metrics. The endpoint follows
the configured base path and authentication settings and requires an
administrator in multiple user mode, including when public mode is enabled.

The server can listen on a Unix socket using a `server.address` such as
`unix:/run/hister/hister.sock`. An explicit public `server.base_url` is required
for clients connecting through a reverse proxy. The socket is removed on a
clean shutdown.

### Enhancements

1. **Browser extension**: automatic capture reduces redundant submissions and
   preserves final page snapshots during navigation and tab closure.
2. **Search interface**: loading feedback is clearer, filters and sort controls
   remain available for empty results, and history results display index counts.
3. **Web interface**: the Add page clarifies file import and indexing options,
   the profile page can copy access tokens, and the menu links to the browser's
   extension store.
4. **OAuth**: GitHub, Google, and OIDC logins use S256 PKCE by default. GitHub
   token exchanges now send credentials in a form POST.
5. **Imports and backups**: import failures provide clearer explanations,
   backups receive a `.json` extension when omitted, and sensitive content
   matches are easier to identify.
6. **Offline commands**: operations that only read SQLite data open the database
   in read only mode.
7. **Documentation**: new monitoring and Homebrew instructions accompany expanded
   crawler backend, import, configuration, and command line guides.
8. **Dependencies**: Go modules, npm packages, container bases, Nix inputs, and
   GitHub Actions were updated.

### Bug Fixes

1. SQLite vector stores tolerate changed embedding dimensions at startup, and
   reindexing recreates the store with the configured dimensions. A failed
   vector store rebuild now aborts reindexing instead of silently proceeding.
2. Embedding retries recognize llama.cpp physical batch size errors.
3. Semantic search respects the selected result order, removes deleted results
   from the interface, and initially uses the server's configured defaults.
4. Languages without a registered Bleve analyzer use the default index instead
   of attempting to create an unsupported language index.
5. Local file indexing applies configured labels, watches directory roots that
   are symbolic links, and handles home directory and absolute paths correctly.
6. Imported HTML files follow the appropriate saved page or file snapshot path,
   and the index command initializes configured extractors.
7. Database timestamps are stored consistently in UTC, with existing SQLite
   timestamps normalized while preserving their instants and precision.
8. Browser import detection handles profile paths more reliably, including
   Ladybird profiles, and retains support for resuming older history jobs.
9. Safari and other browser extensions receive valid CORS preflight responses,
   including when custom authentication headers are used.
10. Text previews escape content when HTML is unavailable. Favicon responses use
    stricter headers, and oversized thumbnails are rejected.
11. Lobsters extraction avoids duplicate comments, and Mastodon extraction no
    longer applies incorrect link resolution.
12. History loading no longer blocks navigation, empty results clear stale
    previews, and the TUI refreshes its viewport when a WebSocket connects.
13. Client errors preserve server explanations for HTTP 404 responses.
14. FreeBSD builds work again, and the systemd service correctly excludes
    privileged system calls and uses paths compatible with user services.

### Backward Compatibility Notes

The following changes require attention when upgrading from v0.19.0:

1. **Command line automation**: `--format json` consistently returns an array,
   including single record summaries. Indexing, file imports, and service
   imports return status `2` when processing completes with item failures and
   status `1` for errors that prevent completion. Update scripts that depend on
   earlier output shapes or exit statuses.
2. **Configuration creation**: use `hister config create` in place of
   `hister create-config`. The old command remains as a deprecated alias and
   writes its notice to standard error. Configuration creation refuses to
   overwrite an existing file.
3. **OAuth providers**: providers that reject PKCE parameters need
   `disable_pkce: true` in their provider configuration. Restart logins begun
   before the upgrade; existing authenticated sessions remain valid. Custom
   GitHub token endpoints and proxies must accept form POST requests.
4. **Allow rules and reindexing**: existing rules without an `allow` group retain
   their behavior. Once allow rules are added, reindexing removes documents
   excluded by allow or skip rules unless they carry an explicit manual
   override.
5. **SQLite timestamps**: an automatic migration normalizes stored timestamps
   to UTC. Command line displays continue to use local time.

## v0.19.0

### New Features

#### ChatGPT and Hacker News Extractors

New ChatGPT and Hacker News extractors preserve complete conversations and
discussion threads. ChatGPT extraction keeps visible user and assistant turns,
including headings, lists, tables, code blocks, and safe links. It supports
authenticated conversations, public shares, and custom GPT conversation URLs.

Hacker News extraction records submission metadata, optional post text, and the
complete nested comment tree. Shared discussion utilities also improve how
Reddit and Discourse comments are represented.

#### URL Regular Expression Search and Cleanup

Queries can now use `url_re:` to match normalized document URLs with Go regular
expressions. Matching supports anchors, case folding, negation, and grouped
alternatives. Invalid or excessively long expressions return a clear error.

The delete API supports a `dry_run` mode that counts matches without changing
the index. The Rules page and result actions can use this preview to delete
documents that match a new or edited skip rule. Hister shows the match count
and asks for confirmation while keeping the saved rule if deletion is canceled.

#### Safari History Imports

`hister import browser` can now import Safari history on macOS, including visit
counts and start date filtering. Browser databases are detected from their
schema instead of their filename, which also resolves ambiguous Safari and
Ladybird database names. Permission errors explain the Full Disk Access setting
required by macOS.

#### Terminal Focus and Native Appearance

The terminal interface now separates search input from result navigation.
Keyboard, pointer, and scroll actions move focus predictably, while contextual
shortcuts and feedback reflect the active area. Dialogs, tabs, rules, previews,
and narrow layouts have more consistent navigation and pointer behavior.

The new default `terminal` appearance inherits the terminal foreground,
background, and ANSI palette. The Settings overlay can switch among terminal,
automatic, dark, and light modes, while the theme picker remains available for
choosing complete Hister themes.

#### Richer Suggestions and Previews

OpenSearch suggestions now include pinned and history results before index
matches, together with descriptions and direct target URLs. Local and remote
file suggestions open the appropriate file or readable preview.

Readable previews display document type, language, label, visit count,
ownership, and extractor metadata. They also provide links to the source and a
standalone preview. Exact document identifiers keep previews and version
history on the correct document when users index the same URL separately.

### Enhancements

1. **Home page**: indexed pages, files, rules, and aliases use compact clickable
   statistics, including a direct file search.
2. **Web navigation**: frequently used destinations are visible again, and the
   menu provides automatic, light, and dark appearance choices.
3. **User administration**: `hister update-user --password` can assign or change
   a password, including for an account originally created through OAuth. Token
   regeneration now requires confirmation in the profile interface.
4. **Semantic search**: index metadata records the embedding configuration and
   warns when stored vectors need reindexing. PostgreSQL configurations are
   validated against the 2000 dimension limit of its HNSW vector index.
5. **Indexer maintenance**: compatibility backfills run in interruptible
   background batches, so older indexes no longer delay server startup.
6. **Operations**: `/health` is available outside any configured base path.
   Generated pages avoid stale caching while immutable assets use long lived
   cache headers.
7. **OAuth**: provider discovery, token exchange, and profile requests now have
   bounded request timeouts.
8. **Video extraction**: the `ytdlp` extractor accepts an `extra_domains` list
   for additional supported sites. Archive.org pages no longer invoke it by
   default.
9. **Configuration documentation**: settings show their server or client scope,
   environment overrides appear earlier, and config path precedence is clearer.
10. **Nix**: the flake uses the upstream NixOS Hister module, and dependency
    hashes are maintained through `nix-update`.
11. **Dependencies**: Go modules, npm packages, container bases, Nix inputs, and
    GitHub Actions were updated.

### Bug Fixes

1. Local files pass through the normal processing chain during reindexing, and
   Markdown snapshots remain classified as local files.
2. File statistics include global documents in multiple user mode.
3. Preview content and archived versions resolve by exact document identifier
   instead of selecting another user's copy of the same URL.
4. Partial rule updates preserve any omitted rule groups.
5. Search suggestions no longer contain duplicates.
6. Input method composition no longer starts searches before text composition
   completes.
7. Leaving the search page no longer displays an expected WebSocket error.
8. PostgreSQL semantic search rejects unsupported vector dimensions before
   attempting to create the vector store.
9. `GET /mcp` returns method not allowed instead of the web application.
10. The container health check works when Hister uses a nonroot base URL.
11. Safari browser extension origins are accepted by CSRF validation.
12. Unrelated files whose names resemble language indexes are ignored.
13. Bleve keeps its established open file allowance, avoiding index failures
    without reducing performance.

### Backward Compatibility Notes

The following changes require attention when upgrading from v0.18.0:

1. **Nix flake**: `x86_64-darwin` is no longer exported because Nix 26.11 does
   not support it. The Hister flake module now relies on the upstream
   `services.hister` module supplied by nixpkgs.

## v0.18.0

### New Features

#### Search Suggestions and Sorting

The web search box now suggests matching queries from search history, recent
searches, aliases, fields, sort options, and facet values. Facet suggestions
show result counts. The panel supports keyboard and pointer use, can be resized,
and remembers its height.

A shared server schema keeps fields, facets, values, and sort options consistent
across search clients. Queries can use `sort:relevance`, `sort:date`,
`sort:visits`, or `sort:domain`. Prefix the value with a minus sign to reverse
the order, as in `sort:-date`.

#### Social and Discussion Extractors

New Twitter, Bluesky, Reddit, and Discourse extractors preserve useful post,
comment, reply, author, date, and reaction data. Twitter and Bluesky can split
feeds and threads into separate post documents. Twitter also expands known
shortened links to their original destinations.

Documents can now include JSON LD metadata. A new extractor SDK and registry
define explicit results, capabilities, and optional context support.

#### Terminal Result Details

The terminal interface now has responsive workspaces and a readable result
details pane. Wide terminals show the list and preview together. Narrow
terminals use the full workspace. Text layout accounts for terminal cell width.

#### Local File Format Imports

`hister import file` can now import PDF, DOCX, Markdown, Org mode, valid UTF 8
text, and other supported local formats. It accepts individual files or
directories. Extraction runs in the command line process and creates remote
snapshots, so the server does not need access to the source files.

#### Service and Browser Imports

Hister can now import incrementally from Linkding, Readeck, and wallabag. Stored
content is used when available, with a page download fallback. Browser imports
can start at `--start-date`.

#### Bulk Document Updates

The new `hister update` command changes the owner, label, title, or language of
documents matched by a search query. It supports dry runs, asks for
confirmation, reports conflicts, and keeps file ownership rules intact.

#### Complete History Timeline

The history page now shows counts for all indexed or opened history. Recent
days and older months are grouped for easier browsing, with daily details for a
selected period.

#### Persistent Web Sessions

Web sessions are now stored in the configured SQL database. They survive server
restarts, use rolling expiration, and are revoked at logout. Cookie security
follows the configured server URL.

#### Structured MCP Results

The MCP endpoint now targets protocol version `2025-06-18` and defines output
schemas for every tool. Results separate trusted metadata from untrusted source
content, remove invisible control characters, and include safe handling
guidance. Search callers can select full document fields.

### Enhancements

1. **Search interface**: search duration is visible, index statistics are
   clickable, empty indexes show quick start help, errors are clearer, and
   recent searches are easier to identify and clear.
2. **Query history**: results promoted by query history are marked and can be
   forgotten for that query. Existing rules remain intact.
3. **Command line**: help now groups commands by scope. `check-update` reports
   new releases, and `crawl urls` can filter or count stored crawl URLs.
4. **URL input**: `hister index --input` accepts a file or standard input and
   creates a persistent job.
5. **Files**: watched directories can apply labels. Cleanup removes local
   documents that no longer match configured directories. File previews verify
   ownership and directory rules.
6. **Batch imports**: clients split batches at the configured request limit and
   report documents that are too large.
7. **Browser extension**: the extension supports access tokens and copied
   browser sessions. It refreshes changed cookies after authentication failures
   and submits pending page changes when a tab closes.
8. **Web interface and docs**: the header is more compact, the initial color
   scheme is configurable, accessibility is improved, and documentation now
   has search and clearer navigation.
9. **Operations**: profiling has an explicit setting. Container builds are
   smaller, and release images use Buildx Bake for supported architectures.
10. **Crawler rendering**: capture delay is shared by rendered backends and is
    now supported by Chromedp.
11. **Terminal compatibility**: command output and the TUI now use Charm v2,
    respect terminal color support, and preserve custom hotkeys.
12. **Dependencies**: Go modules, npm packages, Nix inputs, container bases, and
    GitHub Actions were updated.

### Bug Fixes

1. Failed embedding jobs no longer starve later queued documents.
2. Index metadata access is safe during concurrent indexing.
3. Batch requests respect the server body limit and report oversized documents.
4. Field specific alternatives work correctly, and URL wildcard matching
   ignores letter case.
5. Browser imports count URLs correctly and handle empty history databases.
6. OAuth provider scopes are combined with default scopes.
7. Copied extension sessions use consistent expiry, and the background script
   works in browsers that require a classic script.
8. Sensitive content matches are no longer exposed in debug logs.
9. File indexing continues after an invalid symbolic link.
10. Opened history responses are decoded correctly.
11. Mastodon pages without a detected post are no longer stored.
12. Optional versioning rules remain optional, and prioritizing a result keeps
    the full rule set intact.
13. The middle pointer button still opens results while suggestions are visible.
14. Database conflict clauses now qualify column names correctly.
15. The configured color theme is applied before the page becomes visible.

### Backward Compatibility Notes

The following changes require attention when upgrading from v0.17.0:

1. **Login sessions**: web users and extensions that copied a browser session
   must sign in once after upgrading. Access tokens are unchanged.
2. **File preview API**: `/api/file` now accepts a document `id` instead of an
   absolute `path`. Search and history results include this identifier.
3. **MCP output**: tool results now use schema version `1.0`. Untrusted source
   values are under `structuredContent.untrusted_content`. Text output contains
   the same JSON and a security notice.
4. **Extractor implementations**: custom extractors must use the new SDK result
   constructors, capability declarations, and registry.
5. **URL list input**: `hister index --url-list` remains as a hidden deprecated
   alias. Use `hister index --input`; it also accepts `-` for standard input.

## v0.17.0

### New Features

#### Unified Imports

The import interface is now grouped under `hister import`. In addition to local
files and browser history, Hister can import bookmarks and archived content from
Linkwarden, Karakeep, and Shaarli. Service imports preserve source dates and
metadata, apply source specific or overridden labels, fetch missing page content
and favicons, insert documents in batches, and continue from the latest imported
update after an interruption.

#### Persistent Crawl Job Tooling

Persistent indexing jobs can now start from URL lists, and browser history
imports use the same resumable crawl workflow. Jobs can be resumed with only a
job ID. New `crawl show`, `crawl errors`, and `crawl queue` commands expose job
state, failed URLs, queue contents, and queue counts.

#### Expanded Query Language and Search Filters

Documents now track an updated timestamp alongside their original added time.
Search queries support `added` and `updated` filters with relative or absolute
dates, visit count ranges, standalone wildcard expressions, and exact phrases
within specific fields. The search interface represents filters directly in the
query, making filtered searches easier to understand, edit, and share. Existing
index entries have their updated value backfilled from their added time on
startup.

#### Durable Semantic Indexing

Embedding work now uses a persistent queue and deduplicates pending documents.
Token measurement is more precise, requests reserve capacity below provider
limits, and batch size plus request timeout are configurable. Chunk construction
also uses document structure and more useful metadata.

#### Extensible File Types and DOCX Support

Local file handling now uses an extensible file type interface. DOCX documents
can be indexed with extracted text and metadata, and the documentation explains
the supported local formats and configuration.

#### Chinese, Japanese, and Korean Search

Language detection and language specific indexes now support Chinese, Japanese,
and Korean documents.

#### GitHub Issue and Pull Request Extraction

The GitHub extractor now understands issue and pull request pages. It indexes
the title, body, open date, comments, repository metadata, and useful page text.

#### Crawler Proxy Support

HTTP and SOCKS5 proxy URLs can now be configured for every crawler backend. The
new `--proxy` option also allows crawler backed index and import operations to
override the configured proxy, including robots.txt requests.

#### RFC Dataset

A new RFC dataset and fetch command make it easier to build a searchable local
collection of Internet standards.

#### Qutebrowser DevTools Companion

The new `hister companion qutebrowser` command connects to the Qt WebEngine
DevTools interface and indexes rendered tabs from qutebrowser. It accesses the
final document after client rendering without requiring a page userscript.

### Enhancements

1. **Search interface**: sort choice is preserved in the URL, result index
   counts are visible, and the search input remains available while browsing
   results.
2. **Semantic results**: returned chunks are limited per document, metadata is
   richer, and index metadata is stored with the index itself.
3. **Language aware indexing**: `indexer.keep_stopwords` can retain common words
   while preserving language detection, normalization, and stemming.
4. **Crawler efficiency**: already skipped URLs no longer incur the configured
   crawl delay.
5. **Import performance**: imports use bulk insertion, retain useful imported
   titles and processed state, apply labels, and retrieve missing favicons.
6. **Favicon delivery**: favicons are served through a dedicated endpoint rather
   than being embedded in document responses.
7. **Mastodon extraction**: remote toot URLs are resolved to their canonical
   locations before indexing.
8. **Command line workflow**: search supports `--sort`, delete supports `--yes`,
   list files supports `--relative`, and command descriptions are clearer.
9. **History**: filtering now happens on the server and the selected filter is
   represented in the page URL.
10. **Rules interface**: multiple rules can be entered at once and rule lists use
    descending order by default.
11. **Browser extension**: pages may be submitted as public documents, Firefox
    mobile capability differences are handled, and automatic indexing accepts
    only HTML, XHTML, or plain text documents.
12. **Website and documentation**: the landing page, onboarding, navigation,
    accessibility, configuration reference, SEO metadata, and 404 page have been
    refreshed.
13. **Operations**: debug profile endpoints are available at debug log level, a
    sample systemd unit is included, and crawler failure codes are retained for
    inspection.
14. **External tools**: `yt-dlp` installation uses upstream releases and
    concurrent `yt-dlp` extraction is limited.
15. **Dependencies**: Go modules, npm packages, Nix inputs, GitHub Actions, and
    browser extension dependencies were updated.

### Bug Fixes

1. SQLite semantic search uses cosine scoring consistently.
2. MCP search returns semantic results correctly.
3. Embedding requests avoid provider token limits and reduce batch size when a
   provider reports an oversized request.
4. Index resources are closed when initialization fails, absent documents are
   not deleted again, and document insertion avoids repeated lookups.
5. Startup only warns about reindexing for analyzer changes when analyzer
   configuration is present.
6. Stored previews reject meta refresh redirects.
7. Document domains no longer include ports, and invalid favicon schemes are
   ignored.
8. Public mode history is available to authenticated users and response headers
   are no longer written more than once.
9. Field specific phrase queries and a single wildcard query produce the
   expected matches.
10. Imports preserve existing titles when extraction finds no replacement, and
    the JSON input scanner accepts larger records.
11. The browser extension avoids unavailable command APIs on Firefox mobile and
    uses a compatible badge text color.
12. Search result and history titles and URLs retain usable space in narrow
    layouts.

### Backward Compatibility Notes

The following changes require attention when upgrading from v0.16.0:

1. **Import commands**: `hister import-browser` has been replaced by
   `hister import browser`. File and directory imports now require the source
   subcommand, as in `hister import file INPUT`. Imported JSON exports retain
   their processed content without running extractors again. Browser imports now
   use persistent crawl jobs and can prompt to resume an unfinished job.
2. **Date behavior**: the structured `date_from` and `date_to` search parameters,
   facet date ranges, export date flags, date sorting, history ordering, and
   history RSS dates now use the document `updated` timestamp instead of
   `added`. The web interface no longer reads `date_from` or `date_to` from its
   page URL. Put `added:` or `updated:` expressions in the `q` parameter instead.
   For newly indexed local files, `added` is the indexing time and `updated` is
   the file modification time.
3. **Favicon responses**: search and history results return `favicon_key` instead
   of embedding stored favicon data. API clients should fetch the image from
   `/api/favicon?key=FAVICON_KEY`. Legacy inline favicon values remain readable.
4. **Domain normalization**: newly indexed and reindexed documents store only the
   hostname in `domain`, without the port. Queries and integrations that expect a
   host and port value must be adjusted.
5. **Semantic search**: SQLite vector stores migrate from Euclidean distance to
   cosine distance. Custom `similarity_threshold` values may need adjustment.
   When omitted from configuration, the default context length changes from
   4096 to 512, chunk overlap from 128 to 64, and embedding concurrency from 10
   to 2.
6. **Browser extension submissions**: the extension no longer submits images,
   videos, or other unsupported document types. This content type restriction
   also applies to manual submission.

## v0.16.0

### New Features

#### Public Search Mode

Hister can now run in public mode, allowing unauthenticated read-only access to
global search results and previews while keeping write operations protected.
This makes it possible to publish a shared or community search instance without
exposing private user data. Public mode is documented across configuration,
server setup, user handling, and the new public search blog post.

#### Documentation Datasets

Hister now ships dataset metadata and tooling for importing common reference
documentation. New datasets cover Go standard library, PowerShell, Python, Rust,
Node.js, and refreshed MDN datasets. Compressed dataset imports are supported, so
pre-built documentation archives can be imported directly.

#### StackExchange Extractor

The StackOverflow extractor has been replaced by a generic StackExchange
extractor. It supports StackExchange-style question pages more broadly and
indexes answers with useful metadata.

#### Markdown and Org Mode Extractors

Local Markdown and Org files now render as HTML previews. Markdown titles are
extracted automatically, Markdown syntax is stripped from indexed text, and Org
files are rendered through go-org for cleaner previews.

#### MCP History and Capability Reporting

The MCP endpoint now exposes a `get_history` tool for recently indexed pages and
opened search result history. MCP search supports date filters, and MCP clients
can discover whether semantic search is enabled on the current Hister instance.
The MCP documentation and examples have been expanded accordingly.

#### Browser Extension Hotkeys and Indexing Indicators

The browser extension now includes configurable commands to manually index the
current page, disable indexing for the current page, and disable indexing for
the current domain. Badge feedback indicates success or failure. The extension
can also show whether the current page has already been indexed, with a popup
setting to control the indicator.

#### Import and Export Improvements

Imports now support HTML files, multiple input files in a single command,
compressed JSON archives, directory imports for supported local file types, and
optional user IDs. Import and export commands can be limited by date range.
Browser history import gained a redesigned interactive flow and a `--backend`
flag for selecting the scraping backend.

#### Search JSON Output

The `/search` endpoint now supports `format=json`, making it easier to integrate
Hister search results with scripts and external tools.

#### Search Sorting

Search results can now be sorted by newest date, domain, and most visited pages.
Documents track an `add_count` field that records how many times a URL has been
indexed, and the web UI exposes this as the "Most visited" sort mode.

#### History RSS Feed

The history endpoint and history UI now expose an RSS feed for recently indexed
documents.

### Enhancements

- **File indexing startup**: initial file indexing now runs in the background so
  server startup is not blocked by watched directory indexing (closes #499)
- **Directory indexing queue**: watched directory indexing uses a proper queue,
  logs queue errors, and avoids duplicating file indexing logic
- **Sensitive content override**: CLI indexing can explicitly allow sensitive
  content when requested
- **Semantic search**: embedding generation has a concurrency limit, semantic
  result counts are more accurate, and shared plus user-specific vector results
  are merged
- **History view**: the history timeline has been redesigned, supports loading
  more items, shows when a day has unloaded items, and displays indexed version
  counts
- **Search UI**: the results layout, home layout, dark theme, add entry form,
  search input, and shared chrome have been refined
- **Facets**: type facets were added, inactive facet categories are hidden when
  they only have one candidate, and facet rendering was refactored
- **Preview and history integration**: history supports diff previews for
  versioned documents, preview links are proper anchors, and previously opened
  results stay visible after unpinning
- **Configurable branding**: the web UI title and subtitle can now be configured
- **Logging**: log output format and log file path are configurable, and panic
  stack traces are available in debug logs
- **Environment variables**: environment variable overrides now preserve typed
  values instead of treating everything as a string
- **Terminal UI**: the TUI package now lives under `cmd/tui`, includes updated
  command documentation, and supports result sorting
- **CLI structure**: the command line implementation was split into a dedicated
  `cmd` package for easier maintenance
- **Docker image**: `yt-dlp` is now installed in the Docker image
- **Dependencies**: Go, npm, GitHub Actions, Nix, GORM, SQLite, and PDF related
  dependencies were updated

### Bug Fixes

- Lobsters extraction works with the updated Lobsters HTML structure
- Document labels are added and preserved correctly
- Opened history filtering and history list layout no longer squeeze content
- Result text wraps correctly in the web UI
- Description lines no longer force table scrolling
- Base URL handling is more consistent for app URLs, CSRF checks, and invalid
  base URL values
- The preview panel is cleared when a search returns no results
- Reindexing no longer stops when an extractor aborts one document
- Lazy loading works when the previous page returned fewer than the configured
  page size
- Database migrations no longer unintentionally update `UpdatedAt`
- PDF parsing panics are recovered, and PDF dependency updates address memory
  issues
- Versioned document diffs use the newly stored document when calculating
  changes
- Batch operations handle user IDs correctly
- Extra documents no longer increment the parent document add count
- Subtitle rendering is centered correctly
- Python dataset generation fetches only standard library documentation
- Nix paths and hashes were updated after Go and npm dependency changes

## v0.15.0

### New Features

#### Document Versioning

A new `versioning` rule type instructs Hister to track changes to a document each
time it is re-indexed. A diff-style changelog appears inside the preview panel so
you can see what changed between versions, and the preview endpoint returns the
full version history. Combine with a priority or skip rule to version only the
pages that matter to you.

#### Priority Rules

Priority rules have been reintroduced (closes #222). Documents whose URLs match a
priority rule are pinned to the top of search results regardless of their relevance
score. Rules can be created, edited, and sorted directly in the web UI.

#### PDF Indexing

Local PDF files can now be indexed with full text extraction. The browser extension
gains a new endpoint for sending PDFs directly to the server (closes #55). Indexed
PDFs are stored as a distinct document type and can be filtered with
`metadata.type:pdf` in search queries.

#### Faceted Filtering

Search results can now be filtered through facets that group documents by common
fields such as language, and domain. Facet counts update live as you refine your
query. Date-range filters have been moved into the same filters dropdown for a
unified experience, and a "load more" control expands facets with many values.

#### Embedded Video Extractor

A new extractor detects embedded videos (YouTube, Vimeo, and similar platforms)
on indexed pages and stores the embedding metadata as a dedicated document type.
Embedded videos are rendered directly inside the result preview panel, so you can
watch them without leaving Hister (closes #446).

#### Notion Extractor

A dedicated extractor for Notion pages extracts article content cleanly from
public Notion URLs, removing navigation chrome and other non-content elements.

#### Compressed HTML and Favicon Storage

HTML content and favicons are now stored gzip-compressed in separate files on
disk rather than inline inside the Bleve index. This significantly reduces index
size on disk and lowers memory pressure during search (closes #384).

#### Disable HTML Storage

A new config option lets you turn off full HTML storage and preview generation
entirely. Disabling storage trades preview functionality for a smaller footprint,
useful for bulk or headless indexing workflows (closes #440).

#### User-Specific Directory Indexing

The `indexer.directories` config now accepts a `user` field per directory entry.
Files under that path are indexed only for the specified user, making it easy to
share a single Hister instance while keeping personal file indexes private.

#### Label Editing from Results

Document labels can be edited inline directly from search result cards without
navigating away. The browser extension can also apply a one-off label to a
document at index time (closes #407). Updating a document no longer clears
previously assigned labels.

#### File Deletion Tracking

When a locally-watched file is deleted from the filesystem, Hister now
automatically removes it from the index (closes #230). Batch and single-document
deletions also clean up the associated HTML and favicon files from disk.

#### MCP Document Preview Endpoint

A new MCP endpoint exposes document previews to LLM agents and MCP-compatible
tools, complementing the existing MCP search endpoint.

#### Browser Import: Ladybird Support

The `import-browser` command now supports importing history from the
[Ladybird](https://ladybird.org/) browser.

#### Browser Import: Auto-Detect Database

The `import-browser` command now automatically detects the browser database file
path, so passing an explicit path is no longer required for supported browsers.

### Enhancements

- **Copy URL button**: a copy-to-clipboard icon appears next to each result URL
- **Preview extractor selector**: switch between available extractors when viewing
  a document preview without re-indexing
- **Rules table**: columns are sortable; values are filterable; filter toggle
  buttons are visually distinct from column headers; regexp validation runs before
  saving a rule
- **CLI index flags**: `--delay`, `--timeout`, and `--user-agent` are now
  available directly on `hister index` without needing a config file
- **Configurable client timeout**: the HTTP client timeout used during indexing
  is configurable (fixes #429)
- **Standardized config file lookup**: config file discovery follows a consistent
  search order across all platforms (closes #424)
- **History autoscroll**: the history view scrolls to keep the selected entry
  visible (#427)
- **Admin profile version**: the admin profile page now shows the running Hister
  version (closes #409)
- **Log level aliases**: common short aliases are accepted for log level values
  (fixes #411)
- **Random tips**: a rotating set of usage tips is shown on the front page when
  navigating back from search results
- **yt-dlp diagnostics**: clearer error messages and debug logging help diagnose
  yt-dlp configuration problems
- **Batch crawl insertion**: URLs discovered during a recursive crawl are
  inserted in batches for better throughput
- **Extension popup header**: the popup header is now a link to the configured
  Hister server
- **Per-sub-index paging**: each language sub-index is iterated independently to
  prevent paging gaps in multi-language setups
- **Improved embedding handling**: fallow and embedding request processing
  is more robust

### Bug Fixes

- Docker `BASE_URL` environment variable can now be correctly overridden by a
  config file (closes #442)
- Search results now contain all queried terms rather than any of them
- HTML is no longer re-written to the indexer during a reindex run
- yt-dlp subtitle download no longer skipped when the sub language differs from
  the original language (#429)
- History page stops loading more entries when the last page has been reached
- "Show all" in history now scrolls back to the top
- Rule type filter no longer incorrectly excludes rules
- Debug-level init messages are correctly suppressed at higher log levels
- Result paging uses a valid sort key, fixing out-of-order pages
- Times are displayed in the browser's configured timezone

## v0.14.0

### New Features

#### Mastodon Extractor

A dedicated extractor for Mastodon detects Mastodon pages and indexes each
toot as its own separate document rather than one big blob of text. Works
with any Mastodon instance without configuration. Every toot gets a
`metadata.type:toot` field so you can filter toots in search queries.
Combined with a search alias (`!toot → metadata.type:toot`) this makes
finding past toots fast and convenient.

#### Metadata Query Filtering

Documents can now be filtered by arbitrary metadata fields using the
`metadata.key:value` query syntax. Extractors (including the new Mastodon
extractor) populate these fields at index time.

#### Full Screen Preview

The split-screen preview can now be toggled into a full-screen mode that
occupies the entire content area (closes #401). The URL changes to
`/preview/[id]` so the view survives a page reload. Pressing the "view
result content" hotkey switches between split and full-screen. Full-screen
preview is also available on the history page.

#### Preview Panel on History Page

The history page now shows the same interactive preview panel as the search
page (closes #395). Hotkey navigation between history entries works the same
way as on the search page.

#### Infinite Scroll

Search results now load more pages automatically as you scroll to the bottom
of the list, removing the need to manually page through results (closes #1).

#### Image Lightbox

Images displayed inside the preview panel can be clicked to open a full-size
lightbox view.

#### Delete All Results

A new "delete all" action removes every document that matches the current
search query at once, without having to delete results one by one.

#### Keyboard Shortcut for Result Deletion

A dedicated hotkey deletes the currently focused search result directly from
the keyboard.

#### Quick Skip Rule

A skip-rule component lets you add a URL to your skip list directly from a
search result, available in both the web app and the browser extension
(closes #380).

#### robots.txt Support

The crawler and `hister index` now respect `robots.txt` by default (closes
#386). A new `ignore_robots_txt` config option disables this check when
needed.

#### Document Labeling

Documents can be tagged with custom labels when indexing from the CLI
(`--label`) or from the browser extension. Labels are stored as metadata
and can be used in search queries (closes #156, #373).

#### Editable Rules and Aliases

Existing skip/priority rules and search aliases can now be edited in the
web UI instead of having to delete and recreate them (closes #270).

#### Exact Phrase Matching

Multi-word queries now also attempt an exact phrase match across title and
text, so searching for `open source` surfaces pages that contain that exact
phrase more prominently (closes #394).

#### Extractor Templates and Extra Document Creation

Extractors can now supply a custom preview template, giving each content
type its own presentation in the preview panel. Extractors can also produce
additional sub-documents from a single page (used by the Mastodon extractor
to create one document per toot). A template scaffold is included to make
writing new extractors easier.

#### WebDriver BiDi Crawler Backend

A new `bidi` crawler backend uses the W3C [WebDriver BiDi](https://w3c.github.io/webdriver-bidi/)
protocol to drive an **already-running** browser over a WebSocket connection.
Unlike the `chromedp` backend, it does not launch a browser process: you start
the browser yourself (headless or not) and point Hister at it:

```bash
# Firefox
firefox --remote-debugging-port 9222

# Chrome / Chromium
chromium --remote-debugging-port=9222
```

```yaml
crawler:
  backend: bidi
  backend_options:
    host: '127.0.0.1'
    port: '9222'
    capture_delay: 1.5 # extra seconds to wait after load for JS rendering
```

Supported by Firefox (≥ 102), Chrome/Chromium (≥ 106), and Edge. Options:
`socket` (full WebSocket URL), `host`, `port`, `capture_delay`. The crawler
reuses a single BiDi session for all URLs in one `hister index` run, making
multi-URL indexing significantly more efficient than opening a new browser
session per URL (closes #284).

### Enhancements

- **Resizable preview panel**: drag the divider in split-screen view to
  adjust the panel width; the chosen width persists across sessions
- **History hotkey navigation**: keyboard navigation between history entries
  on the history page
- **Secondary date sorting**: when search scores are equal, results are
  sorted by indexed date
- **yt-dlp multi-language subtitles**: configure which subtitle languages
  the yt-dlp extractor indexes
- **MCP date filtering**: MCP search requests can now be filtered by date
- **Semantic search chunking**: punctuation-based boundaries used for
  chunk splitting, improving relevance for sentence-level queries
- **OIDC enhancements**: userinfo endpoint is now configurable for
  providers without auto-discovery (#279); password login can be disabled
  when OAuth is the only configured auth method
- **Clearer CLI error messages**: client-side HTTP errors now explain the
  problem in plain words and suggest which flag to use (#400)
- **Duplicate rule prevention**: the server and UI both reject duplicate
  skip/priority rules and aliases (#399)
- **Deletion error feedback**: errors during document deletion are now
  surfaced in the UI
- **Rules/aliases UX**: input fields moved above their respective lists
- **Version string in server log**: the server start message now includes
  the version number (#372)
- **Silent WebSocket disconnect**: closing the browser tab no longer shows
  a connection-error message

### Bug Fixes

- Search terms are now properly escaped before query execution
- Focused result index computed correctly when priority results are present
- Phrase queries are only applied when no field-specific terms exist in the
  query
- `/api/delete` requests from the browser extension are now accepted
- `text` and `html` fields always included in search results (#374)
- Language field included in document search results
- OIDC scopes correctly forwarded to the provider (#371)
- Auth page is scrollable on small screens (#370)
- TUI client now passes the access token to the server (#368)
- Optional peer dependencies no longer excluded on non-Linux platforms (#299)

## v0.13.0

### New Features

#### Semantic Search

Full vector search support via sentence-embedding models. Documents are chunked
and embedded at index time; search queries are embedded at query time and ranked
by cosine similarity. Two storage backends are supported:

- **SQLite** (default, via bundled `sqlite-vec`) zero extra infrastructure required
- **PostgreSQL** with `pgvector` auto-selected when the database is Postgres

Configure the embedding API endpoint, model, dimensions, and chunking parameters
in the new `semantic` config section. Semantic search is opt-in and off by default.
Relevance scores are shown alongside results when semantic search is active.

#### OAuth / SSO Authentication

OAuth 2.0 and OpenID Connect (OIDC) providers can now be configured as login
methods. Add one or more entries to the new `server.oauth` config section with
`client_id`, `client_secret`, `configuration_url` (for OIDC auto-discovery), or
manual `auth_url` / `token_url`, and optional `scopes`. Multiple providers can be
active at the same time alongside the built-in username/password login.

#### MCP Server

Hister now exposes a [Model Context Protocol](https://modelcontextprotocol.io/)
endpoint at `/api/mcp`, enabling LLM agents and MCP-compatible tools to search
the index directly.

#### Persistent Crawler State Management

Recursive crawl jobs (`hister index -r`) are now stored in the database and
survive interruptions. Each job gets a unique ID (auto-generated or set via
`--job-id`). Pass `--job-id <id>` without `--recursive` to resume an
interrupted crawl from exactly where it left off, including original validator
rules and visited-URL counts.

#### New Extractors

- **Wikipedia** extracts the article body and infobox, rewrites relative links, and sanitizes the output
- **GitHub project** extracts repository descriptions and README content from GitHub project pages
- **Lobste.rs** dedicated extractor for Lobste.rs story and comment pages
- **yt-dlp** extracts video metadata (title, description, channel) from video pages via yt-dlp
- **JSON-LD** surfaces structured metadata (`@type`, `headline`, description) from pages that embed JSON-LD

All extractors now expose a `Description()` method, and an extractor information
page is available at `/extractors` in the web UI.

#### OpenSearch Suggestions

The server now serves an OpenSearch suggestions endpoint (`/api/suggest`),
allowing browsers to display search-as-you-type completions when Hister is
configured as a search engine.

### Enhancements

#### Crawler Backend for All Index Operations

The `--backend` flag (and `--backend-option`) is now available on both
`hister index` (plain and `--recursive`) and `hister import-browser`, allowing
a headless Chrome/Chromium backend for JavaScript-heavy pages without running a
full recursive crawl:

```bash
hister index --backend chromedp https://example.com
hister import-browser --backend chromedp --backend-option exec_path=/usr/bin/chromium
```

Headers and cookies can also be injected per-invocation:

```bash
hister index --header "Accept-Language=en" --cookie "session=abc; Domain=example.com" https://example.com
```

Cookies use standard `Set-Cookie` format with a required `Domain` attribute.

#### CLI Search Improvements

- `--limit N` flag caps the number of results returned
- `--fields` flag selects which document fields to include in output
- `--html` flag includes raw HTML content in the output
- Paging support added to both CLI search and `list-urls`
- `list-urls` now fetches results from the server by default; `--offline` connects directly to the index without a running server

#### Quoted Field Queries

Field-qualified queries now support quoted values, enabling correct deletion and
lookup of URLs that contain spaces (common on Windows file paths):

```
url:"file:///C:/Users/My Documents/notes.txt"
```

#### Preview Panel Polish

- Preview title is now clickable (opens the result URL)
- Preview panel maximises available content width
- JSON-LD metadata surfaced inside the preview panel
- Dark theme font colors fixed in preview popup

#### NixOS / Nix Module

- `systemd` and `launchd` hardening applied to the Hister service units
- New `services.hister.environmentFile` option for secrets injection
- `openFirewall` now requires explicit opt-in
- `services.hister.config` renamed to `services.hister.settings`

#### Other

- Executable size reduced ~70 MB by switching to a trimmed `lingua-go` fork
- Sensitive content rejection errors surfaced in the browser extension
- `--verbose` flag on `hister delete` lists matched URLs before deleting
- Priority result deduplication now copies body text from the original result
- `/suggest` endpoint protected by auth middleware and `Sec-Fetch-Site` header check
- Version information included in the MCP endpoint response
- Timezone data bundled into the binary for environments without a system `tzdata`

### Bug Fixes

- File URLs (`file://`) now handled correctly in the UI for both opening and deletion (#362)
- Browser extension authentication documentation corrected (#366)
- URLs no longer lowercased during query building, preventing mismatches on case-sensitive paths
- History view correctly filtered per-user in multi-user mode (#314)
- Token authentication middleware now respects `NoAuth` flag (#348)
- Documents with no HTML content no longer attempt HTML extraction (#351)
- Extension no longer resubmits documents after a `406 Not Acceptable` response
- Priority results correctly deduplicated against standard results
- File indexing fixed on Windows
- Wide tables no longer overflow the preview panel
- Score field populated correctly in search responses
- `aws_access_key` sensitive content pattern tightened to reduce false positives
- Home-manager service units correctly gated on host platform in Nix module

## v0.12.0

### New Features

#### Web Crawler

New `hister index -r <url>` command crawls sites recursively using BFS traversal.
Supports an HTTP backend and a headless Chrome backend (chromedp).
Configurable depth, link count, allowed/excluded domains, and URL patterns.

#### PostgreSQL Backend

Full PostgreSQL support as an alternative to SQLite, including pgvector for semantic search.
Configure via a `postgres://` connection string in `server.database`.

#### Extractor Pipeline Overhaul

Extractors are now configurable, have explicit states (continue/done), and expose
a `Preview()` method used by the readability panel. New extractors included:

- Custom `pkg.go.dev` extractor for Go documentation pages
- Basic Stack Overflow extractor

#### Desktop Readability Panel

Focused search results load automatically in a split-pane reader on the right side
on screens wider than 1280 px. The panel is togglable and its open/closed state persists.

### Enhancements

- HTML sanitizer (bluemonday) applied to all extracted content
- `metadata` field added to documents for arbitrary key/value data
- `search` input type attribute on search fields for better mobile UX
- Build commit ID shown in the version string
- Admin users can create global indexes or indexes on behalf of other users
- `hister index` skips already-indexed URLs by default; pass `--force` to reindex them
- URL and domain wildcard matching automatically anchors to start and end
- Table of contents added to the API docs page
- Document indexed date shown in the preview panel
- Search query reflected in the browser tab title
- WebSocket communication optimised to reduce redundant round-trips
- Automatic redirect on zero results is now optional (configurable)
- `import` command renamed to `import-browser` to free `import` for index import/export

### Bug Fixes

- Browser history database opened read-only to avoid lock conflicts (#304)
- History entries now deleted when their associated document is deleted (#303)
- Crawler user-agent correctly applied after redirect handling (#302)
- Fixed field-specific alternation parts in query parser (#274)
- Negated query terms no longer trimmed twice
- HTML field no longer leaks into search results (#268)
- Expanded query hint only shown when the expansion is longer than the original query
- URL changes after HTTP redirects now resolved correctly
- Crawler no longer stops on HTTP errors
- Crawler timeout now applied during browser history import (#278)
- Pinned result titles no longer truncated on narrow screens
- Dark mode handled correctly in the preview panel
- Mobile layout no longer introduces unwanted line breaks
