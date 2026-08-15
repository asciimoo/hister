---
date: '2026-02-25T00:00:00+00:00'
draft: false
title: 'Configuration Reference'
description: 'Explore every configuration section, option, default value, environment override, and complete example.'
---

<script>
  import ConfigReference from '$lib/ConfigReference.svelte';

  const appOptions = [
    {
      name: 'directory',
      type: 'string',
      defaultValue: 'platform default',
      description: 'Directory where Hister stores its data, including the index, rules, and secret key.',
    },
    {
      name: 'title',
      type: 'string',
      defaultValue: 'Hister',
      description: 'Main title shown on the web UI home page.',
    },
    {
      name: 'subtitle',
      type: 'string',
      defaultValue: 'Your own search engine',
      description: 'Secondary title shown below the main title on the web UI home page. Set it to an empty string to hide it.',
    },
    {
      name: 'color_scheme',
      type: 'string',
      defaultValue: 'automatic',
      description: 'Default web UI color scheme. Supported values are automatic, dark, and light. Visitors can override this default with the theme toggle.',
    },
    {
      name: 'search_url',
      type: 'string',
      defaultValue: 'https://google.com/search?q={query}',
      description: 'Fallback web search URL. Use {query} as the placeholder for the search term.',
    },
    {
      name: 'access_token',
      type: 'string',
      defaultValue: '(none)',
      description: 'Optional access token for securing the API. See the Access Token section below.',
    },
    {
      name: 'user_handling',
      type: 'bool',
      defaultValue: 'false',
      description: 'Enables multiple user mode. See the User Handling documentation for details.',
    },
    {
      name: 'public',
      type: 'bool',
      defaultValue: 'false',
      description: 'Allows unauthenticated users to search public documents, use API docs, MCP search, previews, and file serving. Requires access_token or user_handling.',
    },
    {
      name: 'log_level',
      type: 'string',
      defaultValue: 'info',
      description: 'Log verbosity. Supported values are error, warning, info, debug, and trace. The err and warn aliases are also accepted.',
    },
    {
      name: 'log_format',
      type: 'string',
      defaultValue: 'text',
      description: 'Log output format. Text emits colored human readable lines. JSON emits one object per entry for log aggregators.',
    },
    {
      name: 'log_file',
      type: 'string',
      defaultValue: '(none)',
      description: 'Path to a log file. Hister creates or appends to this file instead of writing logs to standard error.',
    },
    {
      name: 'debug_sql',
      type: 'bool',
      defaultValue: 'false',
      description: 'Enables verbose SQL query logging.',
    },
    {
      name: 'open_results_on_new_tab',
      type: 'bool',
      defaultValue: 'false',
      description: 'Opens search results in a new browser tab instead of the current tab.',
    },
    {
      name: 'redirect_on_no_results',
      type: 'bool',
      defaultValue: 'true',
      description: 'Redirects to the configured search_url when a query returns no results. Disable it to remain within Hister.',
    },
    {
      name: 'display_extractor_config',
      type: 'bool',
      defaultValue: 'false',
      description: 'Includes extractor option definitions in the extractor API response so clients can display configuration details.',
    },
    {
      name: 'disable_previews',
      type: 'bool',
      defaultValue: 'false',
      description: 'Disables the preview panel and prevents HTML storage. See the Disable Previews section below.',
    },
  ];

  const serverOptions = [
    {
      name: 'address',
      type: 'string',
      defaultValue: '127.0.0.1:4433',
      description: 'Host and port to listen on. Use [::]:4433 or 0.0.0.0:4433 to listen on all interfaces.',
    },
    {
      name: 'base_url',
      type: 'string',
      defaultValue: 'derived from address',
      description: 'Public URL of the Hister instance. It is required when address uses 0.0.0.0 and must match how you access Hister.',
    },
    {
      name: 'database',
      type: 'string',
      defaultValue: 'db.sqlite3',
      description: 'SQLite filename relative to app.directory or a PostgreSQL DSN. See Database Backends below.',
    },
    {
      name: 'max_batch_body_size',
      type: 'int',
      defaultValue: '40',
      description: 'Maximum request body size in MiB accepted by the batch API. Import clients use this value to keep submitted batches within the server limit.',
    },
    {
      name: 'oauth',
      type: 'map',
      defaultValue: '(none)',
      description: 'Optional map of OAuth or OIDC provider configurations. See OAuth below.',
    },
    {
      name: 'oauth_only',
      type: 'bool',
      defaultValue: 'false',
      description: 'Disables password login. OAuth and token based API access remain accepted. In multiple user mode, a token must belong to a user.',
    },
  ];

  const tuiThemeOptions = [
    {
      name: 'dark_theme',
      type: 'string',
      defaultValue: 'tokyonight',
      description: 'Theme used in dark mode. Available themes include Catppuccin, Dracula, Gruvbox, Nord, Rose Pine, and Tokyo Night.',
    },
    {
      name: 'light_theme',
      type: 'string',
      defaultValue: 'catppuccin-latte',
      description: 'Theme used in light mode.',
    },
    {
      name: 'color_scheme',
      type: 'string',
      defaultValue: 'auto',
      description: 'Color scheme mode. Use auto to follow the system, or select dark or light.',
    },
    {
      name: 'themes_dir',
      type: 'string',
      defaultValue: '(built in themes)',
      description: 'Optional directory containing custom theme YAML files.',
    },
  ];

  const indexerOptions = [
    {
      name: 'detect_languages',
      type: 'bool',
      defaultValue: 'true',
      description: 'Enables automatic language detection. Changing this setting requires reindexing.',
    },
    {
      name: 'languages',
      type: 'string[]',
      defaultValue: '(none)',
      description: 'Restricts language detection to these ISO 639-1 codes, which reduces memory use. Empty detects every supported language. Changing this setting does not require reindexing.',
    },
    {
      name: 'language_detection_accuracy',
      type: 'string',
      defaultValue: 'high',
      description: 'Detection accuracy, high or low. Low uses only trigram models and less memory. The two settings detect text of 120 letters or more identically, so only shorter text is affected.',
    },
    {
      name: 'keep_stopwords',
      type: 'bool',
      defaultValue: 'false',
      description: 'Preserves stop words while retaining language analysis. Changing this setting requires reindexing.',
    },
    {
      name: 'directories',
      type: 'Directory[]',
      defaultValue: '(none)',
      description: 'List of local directories to index. See Local Directory Indexing below.',
    },
    {
      name: 'max_file_size_mb',
      type: 'int',
      defaultValue: '1',
      description: 'Maximum file size in megabytes to index. Larger files are skipped.',
    },
  ];

  const directoryOptions = [
    {
      name: 'path',
      type: 'string',
      defaultValue: '""',
      requirement: 'Required',
      description: 'Directory path to index. Paths beginning with ~/ are expanded to the home directory.',
    },
    {
      name: 'label',
      type: 'string',
      defaultValue: '""',
      description: 'Label applied automatically to every file indexed from this directory.',
    },
    {
      name: 'filetypes',
      type: 'string[]',
      defaultValue: '(none)',
      description: 'Only indexes files with these extensions, without the dot. For example: ["txt", "md"].',
    },
    {
      name: 'patterns',
      type: 'string[]',
      defaultValue: '(none)',
      description: 'Only indexes filenames matching at least one glob pattern. For example: ["doc_*", "README*"].',
    },
    {
      name: 'excludes',
      type: 'string[]',
      defaultValue: '(none)',
      description: 'Skips filenames matching any listed glob pattern. For example: ["*secret*", "*.tmp"].',
    },
    {
      name: 'include_hidden',
      type: 'bool',
      defaultValue: 'false',
      description: 'Includes hidden files, hidden directories, and common dependency or cache directories. Explicit excludes still apply.',
    },
    {
      name: 'delete_on_remove',
      type: 'bool',
      defaultValue: 'false',
      description: 'Automatically removes a file from the index when it is deleted or renamed.',
    },
    {
      name: 'user',
      type: 'string',
      defaultValue: '""',
      description: 'Username that owns files in this directory. Leave it empty for global access.',
    },
  ];

  const oauthOptions = [
    {
      name: 'client_id',
      type: 'string',
      requirement: 'Required',
      description: 'OAuth application client ID issued by the provider.',
    },
    {
      name: 'client_secret',
      type: 'string',
      requirement: 'Required',
      description: 'OAuth application client secret issued by the provider.',
    },
    {
      name: 'configuration_url',
      type: 'string',
      requirement: 'Conditional',
      description: 'OIDC discovery URL. For OIDC, either set this or configure auth_url, token_url, and userinfo_url directly.',
    },
    {
      name: 'auth_url',
      type: 'string',
      requirement: 'Conditional',
      description: 'Overrides the provider authorization endpoint. Required for OIDC when configuration_url is not set. Optional for GitHub and Google.',
    },
    {
      name: 'token_url',
      type: 'string',
      requirement: 'Conditional',
      description: 'Overrides the provider token endpoint. Required for OIDC when configuration_url is not set. Optional for GitHub and Google.',
    },
    {
      name: 'userinfo_url',
      type: 'string',
      requirement: 'Conditional',
      description: 'Overrides the OIDC user information endpoint. Required when configuration_url is not set or its discovery response does not provide this endpoint.',
    },
    {
      name: 'scopes',
      type: '[]string',
      requirement: 'Optional',
      description: 'Additional OAuth scopes to request. Provider defaults are always included.',
    },
  ];

  const crawlerOptions = [
    {
      name: 'backend',
      type: 'string',
      defaultValue: 'http',
      description: 'Scraping backend. Supported values are http, chromedp, and bidi.',
    },
    {
      name: 'backend_options',
      type: 'map',
      defaultValue: '(none)',
      description: 'Options for the selected backend. See Crawler Backend Options below.',
    },
    {
      name: 'proxy',
      type: 'string',
      defaultValue: '(none)',
      description: 'HTTP or SOCKS5 proxy URL used by every crawler backend.',
    },
    {
      name: 'timeout',
      type: 'int',
      defaultValue: '5',
      description: 'Request timeout in seconds.',
    },
    {
      name: 'delay',
      type: 'int',
      defaultValue: '0',
      description: 'Seconds to wait between requests to avoid overloading target servers.',
    },
    {
      name: 'user_agent',
      type: 'string',
      defaultValue: '(none)',
      description: 'Custom User Agent header sent with every request.',
    },
    {
      name: 'headers',
      type: 'map[string]string',
      defaultValue: '(none)',
      description: 'Extra HTTP headers sent with every request.',
    },
    {
      name: 'cookies',
      type: 'Cookie[]',
      defaultValue: '(none)',
      description: 'Cookies sent with every request. See Crawler Cookies below.',
    },
    {
      name: 'no_robots',
      type: 'bool',
      defaultValue: 'false',
      description: 'Disables robots.txt compliance during crawling.',
    },
  ];

  const chromedpOptions = [
    {
      name: 'exec_path',
      type: 'string',
      defaultValue: '(none)',
      description: 'Path to the Chrome or Chromium binary.',
    },
  ];

  const bidiOptions = [
    {
      name: 'socket',
      type: 'string',
      defaultValue: '(none)',
      description: 'Full WebSocket URL. When set, host and port are ignored.',
    },
    {
      name: 'host',
      type: 'string',
      defaultValue: '127.0.0.1',
      description: 'Hostname or IP address of the browser WebDriver BiDi endpoint.',
    },
    {
      name: 'port',
      type: 'string',
      defaultValue: '9222',
      description: 'Port of the browser WebDriver BiDi endpoint.',
    },
    {
      name: 'capture_delay',
      type: 'float',
      defaultValue: '0',
      description: 'Seconds to wait after page load before capturing HTML for pages that rely on JavaScript rendering.',
    },
  ];

  const crawlerCookieOptions = [
    {
      name: 'name',
      type: 'string',
      requirement: 'Required',
      description: 'Cookie name.',
    },
    {
      name: 'value',
      type: 'string',
      requirement: 'Required',
      description: 'Cookie value.',
    },
    {
      name: 'domain',
      type: 'string',
      requirement: 'Required',
      description: 'Domain to which the cookie applies, such as example.com.',
    },
    {
      name: 'path',
      type: 'string',
      defaultValue: '/',
      requirement: 'Optional',
      description: 'Cookie path.',
    },
  ];

  const semanticConnectionOptions = [
    {
      name: 'enable',
      type: 'bool',
      defaultValue: 'false',
      description: 'Enables semantic search. All other semantic search settings are ignored when this is disabled.',
    },
    {
      name: 'embedding_endpoint',
      type: 'string',
      defaultValue: 'http://localhost:11434/v1/embeddings',
      description: 'URL of the OpenAI compatible embeddings endpoint.',
    },
    {
      name: 'embedding_model',
      type: 'string',
      defaultValue: 'qwen3-embedding:8b',
      description: 'Model name passed in each embedding request. It must match a model served by the endpoint.',
    },
    {
      name: 'api_key',
      type: 'string',
      defaultValue: '""',
      description: 'Optional API key sent as an Authorization bearer token. Hosted providers commonly require it.',
    },
    {
      name: 'headers',
      type: 'map[string]string',
      defaultValue: '{}',
      description: 'Optional HTTP headers added to every embedding request for proxies or custom authentication.',
    },
    {
      name: 'dimensions',
      type: 'int',
      defaultValue: '4096',
      description: 'Vector dimensionality. This must match the output of the selected embedding model.',
    },
  ];

  const semanticInputOptions = [
    {
      name: 'max_context_length',
      type: 'int',
      defaultValue: '512',
      description: 'Hard context ceiling for each text chunk. Hister reserves five percent as tokenizer headroom, giving the default an approximate budget of 486 tokens. Endpoint rejections trigger another retry with smaller chunks.',
    },
    {
      name: 'chunk_overlap',
      type: 'int',
      defaultValue: '64',
      description: 'Approximate token allowance shared between consecutive chunks while preserving structural boundaries.',
    },
    {
      name: 'query_prefix',
      type: 'string',
      defaultValue: '"query: "',
      description: 'Text prepended to every search query. Many embedding models use distinct query and document prefixes for better recall.',
    },
    {
      name: 'document_prefix',
      type: 'string',
      defaultValue: '""',
      description: 'Text prepended to every document chunk. Set it according to the convention expected by the embedding model.',
    },
  ];

  const semanticRetrievalOptions = [
    {
      name: 'similarity_threshold',
      type: 'float',
      defaultValue: '0.1',
      description: 'Minimum cosine similarity required for a semantic chunk to be included in the results.',
    },
    {
      name: 'result_limit',
      type: 'int',
      defaultValue: '50',
      description: 'Maximum number of semantic hits retrieved for each query.',
    },
    {
      name: 'semantic_weight',
      type: 'float',
      defaultValue: '0.4',
      description: 'Weight applied to semantic scores when merging them with keyword scores. Zero uses keyword results only, while one uses semantic results only.',
    },
  ];

  const semanticProcessingOptions = [
    {
      name: 'embedding_timeout',
      type: 'int',
      defaultValue: '300',
      description: 'Maximum seconds allowed for one embedding request. Values below one use the default of 300 seconds.',
    },
    {
      name: 'max_embedding_batch_size',
      type: 'int',
      defaultValue: '8',
      description: 'Maximum chunks sent in one request. Smaller batches keep local endpoints responsive and let long documents make incremental progress. Values below one use the default of eight.',
    },
    {
      name: 'max_embedding_concurrency',
      type: 'int',
      defaultValue: '2',
      description: 'Maximum embedding workers and simultaneous endpoint requests. Increase this for fast remote endpoints. Values below one use the default of two.',
    },
  ];
</script>

Hister is configured via a YAML file. The config file is searched in the following order:

### Default Config Locations

**Linux** (respects `$XDG_CONFIG_HOME`):

- `$XDG_CONFIG_HOME/hister/config.yml` (if `$XDG_CONFIG_HOME` is set)
- `~/.config/hister/config.yml` (default)
- `~/.histerrc` (legacy, deprecated)

**macOS** (respects `$XDG_CONFIG_HOME`):

- `$XDG_CONFIG_HOME/hister/config.yml` (if `$XDG_CONFIG_HOME` is set)
- `~/Library/Preferences/hister/config.yml` (recommended)
- `~/Library/Application Support/hister/config.yml` (backwards compatible)
- `~/.histerrc` (legacy, deprecated)
- `~/.config/hister/config.yml` (legacy)

**Windows** (respects environment variables):

- `%LOCALAPPDATA%\hister\config.yml` (recommended)
- `$XDG_CONFIG_HOME\hister\config.yml` (if `$XDG_CONFIG_HOME` is set)
- `%APPDATA%\hister\config.yml` (fallback)
- `~\.histerrc` (legacy, deprecated)
- `~\.config\hister\config.yml` (legacy)

Hister searches these paths in order and uses the first config file it finds. If you have a legacy `~/.histerrc` file and Hister finds it, you'll see a deprecation warning suggesting you move it to the recommended location.

### Creating a Config File

Generate a config file with default values:

```bash
hister create-config ~/.config/hister/config.yml
```

Or use your platform's recommended location:

```bash
# Linux
hister create-config ~/.config/hister/config.yml

# macOS
hister create-config ~/Library/Preferences/hister/config.yml

# Windows
hister create-config %LOCALAPPDATA%\hister\config.yml
```

You can also specify a custom config location using the `--config` flag:

```bash
hister listen --config /path/to/my/config.yml
```

**Important**: Restart the Hister server after modifying the configuration file.

## Full Example

```yaml
app:
  directory: '~/.config/hister'
  title: 'Hister'
  subtitle: 'Your own search engine'
  color_scheme: 'automatic'
  search_url: 'https://google.com/search?q={query}'
  public: false
  log_level: 'info'
  log_format: 'text'
  log_file: ''
  open_results_on_new_tab: false

server:
  address: '127.0.0.1:4433'
  base_url: 'http://127.0.0.1:4433'
  database: 'db.sqlite3'
  oauth:
    github:
      client_id: 'your-github-client-id'
      client_secret: 'your-github-client-secret'

indexer:
  detect_languages: true
  language_detection_accuracy: 'high'
  languages: ['en', 'de']
  keep_stopwords: false
  directories:
    - path: '~/notes'
      label: 'notes'
      filetypes: ['txt', 'md']

crawler:
  backend: 'http'
  proxy: 'http://127.0.0.1:8080'
  timeout: 5
  delay: 1
  user_agent: 'Hister'

semantic_search:
  enable: false
  embedding_endpoint: 'http://localhost:11434/v1/embeddings'
  embedding_model: 'qwen3-embedding:8b'
  embedding_timeout: 300
  dimensions: 4096
  max_context_length: 512
  chunk_overlap: 64
  max_embedding_batch_size: 8
  similarity_threshold: 0.1
  result_limit: 50
  semantic_weight: 0.4
  max_embedding_concurrency: 2

hotkeys:
  web:
    '/': 'focus_search_input'
    'enter': 'open_result'
    'alt+enter': 'open_result_in_new_tab'
    'alt+j': 'select_next_result'
    'alt+k': 'select_previous_result'
    'alt+o': 'open_query_in_search_engine'
    'alt+v': 'view_result_popup'
    'alt+d': 'delete_result'
    'tab': 'autocomplete'
    '?': 'show_hotkeys'

sensitive_content_patterns:
  aws_access_key: '(^|[\s"''])AKIA[0-9A-Z]{16}([\s"'']|$)'
  github_token: '(ghp|gho|ghu|ghs|ghr)_[a-zA-Z0-9]{36}'
```

## `app` Section

<ConfigReference items={appOptions} />

## `server` Section

<ConfigReference items={serverOptions} />

## Database Backends

Hister supports **SQLite** (default) and **PostgreSQL**.

The `server.database` value determines which backend is used:

- If the value contains `=` it is treated as a **PostgreSQL DSN**.
- Otherwise it is treated as an **SQLite filename** relative to `app.directory`.

### SQLite (default)

```yaml
server:
  database: 'db.sqlite3'
```

### PostgreSQL

```yaml
server:
  database: 'host=localhost user=hister password=hister dbname=hister port=5432 sslmode=disable TimeZone=Europe/Budapest'
```

Hister uses the standard PostgreSQL DSN key=value format. Adjust `host`, `user`, `password`, `dbname`, `port`, `sslmode`, and `TimeZone` to match your setup.

## Semantic Search

Hister can augment keyword search with vector similarity search. When enabled, each indexed document gets a metadata vector containing its title, URL, specific extractor type when available, language, author, description, and topic metadata. Document text is split into overlapping structural chunks with compact title and language context, and each chunk is converted to a floating point vector by an external embedding model. The vectors are stored alongside the main index. At search time the query is also embedded and the closest chunks are retrieved, then merged with keyword results and reranked.

Semantic search is **opt-in** and disabled by default. It requires an OpenAI-compatible embeddings endpoint such as [Ollama](https://ollama.com), a local [llama.cpp](https://github.com/ggml-org/llama.cpp) server, or the OpenAI API itself.

### Connection and Model

<ConfigReference items={semanticConnectionOptions} />

### Chunking and Input

<ConfigReference items={semanticInputOptions} />

### Retrieval

<ConfigReference items={semanticRetrievalOptions} />

### Processing

<ConfigReference items={semanticProcessingOptions} />

### Vector Storage Backends

The vector store backend is chosen automatically based on `server.database`:

- **SQLite** (default) stores vectors in a separate `vectors.sqlite3` file in the same directory as the main database, using the [sqlite-vec](https://github.com/asg017/sqlite-vec) extension. No extra setup required.
- **PostgreSQL** stores vectors in the same database as the main data using the [pgvector](https://github.com/pgvector/pgvector) extension. Make sure `pgvector` is installed and enabled (`CREATE EXTENSION vector;`) before starting Hister.

### Example

```yaml
semantic_search:
  enable: true
  embedding_endpoint: 'http://localhost:11434/v1/embeddings'
  embedding_model: 'nomic-embed-text'
  embedding_timeout: 300
  dimensions: 768
  max_context_length: 512
  chunk_overlap: 50
  max_embedding_batch_size: 8
  query_prefix: 'search_query: '
  document_prefix: 'search_document: '
  similarity_threshold: 0.5
  result_limit: 10
  semantic_weight: 0.4
  max_embedding_concurrency: 2
  # api_key: 'sk-...'            # required for hosted providers
  # headers: {}                  # extra HTTP headers for proxies or custom auth
```

The example above uses [nomic-embed-text](https://ollama.com/library/nomic-embed-text) via Ollama, which produces 768-dimensional vectors and fits well in a 512-token context window. The `query_prefix` and `document_prefix` values shown are the ones recommended by the Nomic model. Other models use different conventions: `"query: "` / `"passage: "` for E5 and BGE families (this is also the built-in default for `query_prefix`), `"Represent this sentence for searching relevant passages: "` for GTE. Check your model's documentation for the expected prefix strings. Set both to `""` for models that do not use prefixes (such as OpenAI `text-embedding-3-*`).

## TUI Settings

TUI settings are configured in a separate `tui.yaml` file located in the same directory as your main config file. This file is automatically created with default values when you first run `hister search`.

### Theme Settings

<ConfigReference items={tuiThemeOptions} />

**Built-in themes**: catppuccin-frappe, catppuccin-latte, catppuccin-macchiato, catppuccin-mocha, dracula, gruvbox, gruvbox-light, material-lighter, nord, nord-light, one-light, rose-pine, rose-pine-dawn, solarized-light, tokyonight, and tomorrow.

## `indexer` Section

<ConfigReference items={indexerOptions} />

### Directory Entry

Each entry in `directories` is an object with the following keys:

<ConfigReference items={directoryOptions} />

When multiple filters are specified, they are applied in order: excludes first, then filetypes, then patterns. A file must pass all specified filters to be indexed. When a filter is omitted, it is not applied (all files pass).

## Local Directory Indexing

The `indexer.directories` option lets you index local files so they appear alongside your browser history in search results. Files are indexed automatically when the server starts, running in the background so the server is available immediately. A file watcher monitors configured directories for changes, so new and modified files are indexed automatically without needing to restart the server.

```yaml
indexer:
  directories:
    - path: '~/notes'
      filetypes: ['txt', 'md']
      patterns: ['doc_*']
      excludes: ['*secret*']
    - path: '~/Documents/wiki'
      label: 'wiki'
    - path: '/path/to/project'
      label: 'project'
      filetypes: ['go', 'py', 'js']
```

Set `label` on a directory to apply the same searchable label to every file indexed from it. Changing a nonempty configured label updates existing files during the next startup scan, even when their contents have not changed. Leaving it empty preserves labels assigned manually.

### User-scoped directories

When `user_handling` is enabled, you can scope indexed files to specific users using the `user` field. Files in a user-scoped directory are only visible to that user in search results. Global directories (no `user` set) are visible to all users.

```yaml
indexer:
  directories:
    - path: '/nextcloud/notes/alice'
      user: 'alice'
      filetypes: ['txt', 'md']
    - path: '/nextcloud/notes/bob'
      user: 'bob'
      filetypes: ['txt', 'md']
    - path: '/shared/docs'
      # no user = global, visible to all
      filetypes: ['pdf', 'docx']
```

Visibility rules:

- A user sees files from directories where `user == username` plus global directories (`user` empty or unset) plus their own web documents
- Admins have the same visibility as regular users (no special access to other users' files)
- Unauthenticated users see global directories only

Files are indexed recursively, with the following rules:

- Hidden files and directories (starting with `.`) are skipped unless `include_hidden: true`
- Well-known dependency/cache directories (`node_modules`, `bower_components`, `jspm_packages`, `__pycache__`, `__pypackages__`) are skipped unless `include_hidden: true`
- Binary files are skipped
- Files larger than `indexer.max_file_size_mb` (default: 1 MB) are skipped
- Files matching `sensitive_content_patterns` are skipped

Changes to indexed directories are picked up automatically by the file watcher, no server restart is needed. On server start, only files that have been modified since they were last indexed are re-processed. File results appear with the domain `local` and are served through the Hister web interface directly.

When `delete_on_remove: true` is set on a directory, deleting or renaming a file on the filesystem also removes it from the index automatically. This is opt-in and disabled by default.

No reindex is required when adding or removing files. Files are detected and indexed automatically. After making directory filters more restrictive, run `hister cleanup` to remove indexed local documents that no longer match the configuration. Cleanup compares indexed paths with the configuration and does not scan or read the filesystem.

## Disable Previews

By default, Hister stores the full HTML content of every indexed page on disk and makes it available in a split-pane preview panel in the search and history UI. Setting `disable_previews: true` turns this off completely:

- HTML content is **never written to disk** during indexing or re-indexing. Only the extracted plain text, title, URL, domain, language, and favicon are kept.
- Running `hister reindex` with this option enabled will delete all previously stored HTML files, reclaiming disk space.
- The preview panel, the per-result **view** button, and the **Preview** toggle are hidden in the web UI.

This is useful when disk space is limited or when you prefer not to retain full page snapshots.

```yaml
app:
  disable_previews: true
```

> **Note**: Favicons are unaffected by this setting and are always stored.

## Access Token

The `app.access_token` setting provides a simple authentication mechanism to secure your Hister instance. When configured, clients must include the token in API requests using the `X-Access-Token` header or the `Authorization: Bearer TOKEN` header. This is particularly useful when exposing Hister to the network or internet, preventing unauthorized access to your browsing history and search index.

To use the access token, set it in your configuration file:

```yaml
app:
  access_token: 'your-secret-token-here'
```

The web UI prompts for the access token and exchanges it for an opaque browser session. It does not retain the access token in the cookie or in browser storage. The access token has to be added to the browser extension separately when the extension uses token authentication.

## Public Mode

Public mode lets anonymous visitors search the shared index while write access remains authenticated. Enable it with `app.public: true` or by starting the server with `hister listen --public`. A public instance must also configure either `app.access_token` or `app.user_handling`, otherwise Hister refuses to start.

```yaml
app:
  public: true
  access_token: 'your-secret-token-here'

server:
  base_url: https://hister.example.com
```

With user handling, anonymous visitors can only see documents stored under user ID `0`. User-owned documents remain visible only to the matching authenticated user.

```yaml
app:
  public: true
  user_handling: true
```

Public mode exposes search, suggestions, document reads, previews, file serving, API documentation, and MCP search. It does not allow anonymous users to add, edit, label, delete, change rules, reindex, clean up, access web history, or access profile APIs. Authenticated callers can access web history normally.

Only index content that is meant to be public. Local files, previews, and MCP search can expose indexed document content to anonymous visitors.

For command-line usage with `curl` or similar tools, include the header in your requests:

```bash
curl -H "X-Access-Token: your-secret-token-here" http://localhost:4433/api/config
```

**Security note**: API clients transmit the access token in plain text with each request, and the web UI transmits it during login. When exposing Hister over the network, always use HTTPS through a reverse proxy to encrypt credentials and sessions in transit. The token provides basic access control but does not replace proper authentication systems for multiple user scenarios.

## OAuth

When [user handling](/docs/user-handling) is enabled, Hister supports delegating authentication to external OAuth 2.0 / OpenID Connect providers. Users can then sign in with their existing accounts instead of a Hister-local password.

The `server.oauth` key is a map where each key is a provider name and the value is its configuration. Three providers are built in:

| Provider | Description                                                  |
| -------- | ------------------------------------------------------------ |
| `github` | GitHub accounts via the GitHub OAuth app                     |
| `google` | Google accounts via Google Identity                          |
| `oidc`   | Any OpenID Connect provider (Keycloak, Authentik, Dex, etc.) |

Each entry supports the following fields:

<ConfigReference items={oauthOptions} />

### GitHub Example

Register an OAuth app at [github.com/settings/developers](https://github.com/settings/developers). Set the **Authorization callback URL** to `https://your-hister-instance/api/oauth/callback?provider=github`.

```yaml
server:
  oauth:
    github:
      client_id: 'your-github-client-id'
      client_secret: 'your-github-client-secret'
```

### Google Example

Create OAuth credentials in the [Google Cloud Console](https://console.cloud.google.com/). Add `https://your-hister-instance/api/oauth/callback?provider=google` as an authorised redirect URI.

```yaml
server:
  oauth:
    google:
      client_id: 'your-google-client-id'
      client_secret: 'your-google-client-secret'
```

### Generic OIDC Example

```yaml
server:
  oauth:
    oidc:
      client_id: 'hister'
      client_secret: 'your-client-secret'
      configuration_url: 'https://accounts.example.com/.well-known/openid-configuration'
```

If your provider does not publish a discovery document, set `auth_url`, `token_url`, and `userinfo_url` directly and omit `configuration_url`:

```yaml
server:
  oauth:
    oidc:
      client_id: 'hister'
      client_secret: 'your-client-secret'
      auth_url: 'https://accounts.example.com/oauth/authorize'
      token_url: 'https://accounts.example.com/oauth/token'
      userinfo_url: 'https://accounts.example.com/oauth/userinfo'
```

### How It Works

1. The login page shows a **Sign in with &lt;Provider&gt;** button for each configured provider.
2. Clicking the button redirects the user to the provider's authorization page.
3. After the user grants access the provider redirects back to `/api/oauth/callback?provider=<name>`.
4. Hister verifies the state token, exchanges the authorization code for a token, and fetches the user's identity from the provider.
5. If no local account is linked to that identity, one is created automatically. GitHub uses the login name, Google uses the account name with the full email address as a fallback, and OIDC uses `preferred_username` with the full email address as a fallback.
6. The user is logged in and redirected to the home page.

> **Note**: OAuth login requires `app.user_handling: true`. The buttons only appear on the login page when user handling is active and at least one provider is configured.

## OAuth-Only Mode

Setting `server.oauth_only: true` prevents users from authenticating with a username/password. Only OAuth logins are accepted through the web interface.

```yaml
server:
  oauth_only: true
  oauth:
    github:
      client_id: 'your-github-client-id'
      client_secret: 'your-github-client-secret'
```

Personal access tokens continue to work regardless of this setting, so API clients, the CLI, and the browser extension can authenticate without a browser login. In multiple user mode, `app.access_token` is a client default and must contain a user's personal token to authenticate.

The login page hides the username/password form when `oauth_only` is active, showing only the OAuth provider buttons.

> **Note**: Use `oauth_only` with `app.user_handling: true` and at least one configured OAuth provider. Enabling it without a provider leaves users with no browser login path.

## Language Detection

The `indexer.detect_languages` option (default: `true`) controls automatic language detection for indexed pages. When enabled, Hister uses language detection libraries to identify the language of each page's content, creating separate language-specific indexes that improve search accuracy through language-aware tokenization and stemming.

The `indexer.keep_stopwords` option defaults to `false`. When enabled together with language detection, Hister retains stop words while continuing to apply the other language analyzer operations, including normalization and stemming. This is useful when quoted phrases must include common words such as `for` and `your`.

**Performance considerations**: Language detection increases both CPU usage and memory consumption. Each document requires additional processing to analyze text and determine its language, and separate indexes are maintained for each detected language. If you're experiencing memory pressure or slow indexing performance, especially with large numbers of documents, consider disabling this feature.

**Important**: Changing `detect_languages` or `keep_stopwords` requires a full reindex to take effect. Neither `languages` nor `language_detection_accuracy` does; both apply to documents indexed after the change, and existing indexes stay searchable. After changing `detect_languages` or `keep_stopwords`, run:

```bash
hister reindex
```

The reindex operation will rebuild all indexes according to the new settings. Hister stores an analyzer configuration fingerprint and warns at startup when the configured settings differ from those used by the current index. With language detection disabled, all documents are indexed using a single default analyzer, reducing memory overhead and simplifying the indexing process at the cost of potentially less accurate search results.

## `hotkeys.web` Section

Defines keyboard shortcuts for the web interface. Each entry maps a key combination to an action.

**Key format**: `[modifier+]key` where modifier is `ctrl`, `alt`, or `meta`. Key can be a letter, digit, or special key (`enter`, `tab`, `arrowup`, `arrowdown`, etc.).

| Action                        | Description                                                     |
| ----------------------------- | --------------------------------------------------------------- |
| `focus_search_input`          | Move focus to the search input box                              |
| `open_result`                 | Open the selected (or first) result                             |
| `open_result_in_new_tab`      | Open the selected result in a new tab                           |
| `select_next_result`          | Move selection to the next result                               |
| `select_previous_result`      | Move selection to the previous result                           |
| `open_query_in_search_engine` | Open the current query in the configured fallback search engine |
| `view_result_popup`           | Open the offline preview popup for the selected result          |
| `delete_result`               | Delete the selected result                                      |
| `autocomplete`                | Accept the autocomplete suggestion                              |
| `show_hotkeys`                | Show the keyboard shortcuts help overlay                        |

## TUI Configuration

TUI-specific settings are stored in a separate `tui.yaml` file in the same directory as your main config. This file is automatically created with defaults the first time you run `hister search`.

**Default location**: `~/.config/hister/tui.yaml` (or alongside your config file)

### tui.yaml Example

```yaml
dark_theme: 'tokyonight'
light_theme: 'catppuccin-latte'
color_scheme: 'auto'

hotkeys:
  ctrl+c: 'quit'
  f1: 'toggle_help'
  tab: 'toggle_focus'
  esc: 'toggle_focus'
  up: 'scroll_up'
  k: 'scroll_up'
  down: 'scroll_down'
  j: 'scroll_down'
  enter: 'open_result'
  ctrl+d: 'delete_result'
  ctrl+t: 'toggle_theme'
  ctrl+s: 'toggle_settings'
  ctrl+o: 'toggle_sort'
  alt+1: 'tab_search'
  alt+2: 'tab_history'
  alt+3: 'tab_rules'
  alt+4: 'tab_add'
```

### TUI Hotkeys

TUI keyboard shortcuts are configured in `tui.yaml` under the `hotkeys` section. See the [tui.yaml example](#tui-configuration) above.

| Action            | Description                                                                 |
| ----------------- | --------------------------------------------------------------------------- |
| `quit`            | Exit the TUI                                                                |
| `toggle_help`     | Show/hide the keybindings help overlay                                      |
| `toggle_focus`    | Switch between search input and results list                                |
| `scroll_up`       | Move selection up                                                           |
| `scroll_down`     | Move selection down                                                         |
| `open_result`     | Open the selected result in your browser                                    |
| `delete_result`   | Delete the selected entry from the index                                    |
| `toggle_theme`    | Open the interactive theme picker overlay                                   |
| `toggle_settings` | Open the keybinding editor overlay                                          |
| `toggle_sort`     | Toggle domain-based sorting for search results                              |
| `tab_search`      | Switch to the Search tab                                                    |
| `tab_history`     | Switch to the History tab (view recent searches)                            |
| `tab_rules`       | Switch to the Rules tab (manage skip/priority/versioning rules and aliases) |
| `tab_add`         | Switch to the Add tab (manually add URLs to index)                          |

## `crawler` Section

The `crawler` section configures the web crawler used by `hister index`, `hister index --recursive`,
browser imports, and service imports that need to download bookmark content. These commands share
the same backend and request settings.
Every recursive crawl runs as a persistent job so it can be interrupted and resumed
without losing progress. See [Website Crawler](crawler) for usage details.

<ConfigReference items={crawlerOptions} />

Set `proxy` to an `http://` or `socks5://` URL. The HTTP backend uses it as its transport proxy,
Chromedp passes it to the browser process, and BiDi requests it when creating the browser session.
robots.txt requests use the same proxy. For example:

```yaml
crawler:
  proxy: 'socks5://127.0.0.1:1080'
```

Proxy URLs with embedded credentials are rejected because browser backends cannot apply them
consistently. You can also set the proxy with `HISTER__CRAWLER__PROXY` or the `--proxy` flag.

For BiDi, a configured proxy requires the remote browser to accept `session.new` with the proxy
capability. Initialization fails if the endpoint already owns a session or rejects that capability,
so Hister never continues while silently ignoring the proxy.

### Crawler Backend Options

The `backend_options` map passes configuration to the selected backend. Each backend validates its own options and rejects unknown keys.

**`http` backend** — no backend-specific options supported.

**`chromedp` backend**:

<ConfigReference items={chromedpOptions} />

```yaml
crawler:
  backend: 'chromedp'
  backend_options:
    exec_path: '/usr/bin/chromium'
  timeout: 15
```

**`bidi` backend** (WebDriver BiDi):

Connects to an **already-running** browser that exposes a [WebDriver BiDi](https://w3c.github.io/webdriver-bidi/) WebSocket endpoint. This is the W3C-standard automation protocol supported by Firefox (≥ 102), Chrome (≥ 106), Edge, and other modern browsers. Unlike `chromedp`, the `bidi` backend does **not** launch a browser process — it reuses one you have started yourself (headless or not).

<ConfigReference items={bidiOptions} />

Start your browser with BiDi enabled, for example:

```bash
# Firefox
firefox --remote-debugging-port 9222

# Chrome / Chromium
chromium --remote-debugging-port=9222
```

Then configure Hister to use it:

```yaml
crawler:
  backend: 'bidi'
  backend_options:
    host: '127.0.0.1'
    port: '9222'
    capture_delay: 1.5 # wait 1.5s after load for JS to render
  timeout: 15
```

Or using a full socket URL:

```yaml
crawler:
  backend: 'bidi'
  backend_options:
    socket: 'ws://127.0.0.1:9222/session'
```

### Crawler Cookies

Each entry in `cookies` is an object with the following keys:

<ConfigReference items={crawlerCookieOptions} />

### Full Crawler Example

```yaml
crawler:
  backend: 'http'
  proxy: 'http://127.0.0.1:8080'
  timeout: 10
  delay: 2
  user_agent: 'Hister'
  headers:
    Accept-Language: 'en-US,en;q=0.9'
  cookies:
    - name: 'session'
      value: 'abc123'
      domain: 'example.com'
      path: '/'
```

## `sensitive_content_patterns` Section

A map of named [Go regular expression](https://pkg.go.dev/regexp/syntax) patterns. Hister rejects a web page or local file when its HTML or extracted text matches any pattern. The content is not redacted or indexed. Indexing commands that support `--allow-sensitive` can explicitly bypass this check.

```yaml
sensitive_content_patterns:
  my_pattern: 'regex here'
```

Default patterns cover common secrets: AWS keys, GitHub tokens, SSH/PGP private keys.

## Environment Variables

You can override configuration values using environment variables. The naming convention is:

```textplain
HISTER__<SECTION>__<KEY>=value
```

For example:

- `HISTER__APP__LOG_LEVEL=debug` overrides `app.log_level`
- `HISTER__APP__LOG_FORMAT=json` overrides `app.log_format`
- `HISTER__APP__LOG_FILE=/var/log/hister.log` overrides `app.log_file`
- `HISTER__SERVER__ADDRESS=0.0.0.0:8080` overrides `server.address`

Three special purpose variables are also supported:

| Variable          | Description                                                                  |
| ----------------- | ---------------------------------------------------------------------------- |
| `HISTER_CONFIG`   | Select a config file when `--config` is not supplied                         |
| `HISTER_PORT`     | Override the port only while keeping the existing host from `server.address` |
| `HISTER_DATA_DIR` | Override `app.directory`                                                     |
