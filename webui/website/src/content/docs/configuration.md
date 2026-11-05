---
date: '2026-02-25T00:00:00+00:00'
draft: false
title: 'Configuration Reference'
description: 'Explore every configuration section, option, default value, environment override, and complete example.'
---

<script>
  import ConfigReference from '#lib/ConfigReference.svelte';

  const appOptions = [
    {
      name: 'directory',
      type: 'string',
      defaultValue: 'platform default',
      scopes: ['server', 'client'],
      description: 'Directory where Hister stores its data, including the index, rules, and secret key.',
    },
    {
      name: 'title',
      type: 'string',
      defaultValue: 'Hister',
      scopes: ['server'],
      description: 'Main title shown on the web UI home page.',
    },
    {
      name: 'subtitle',
      type: 'string',
      defaultValue: 'Your own search engine',
      scopes: ['server'],
      description: 'Secondary title shown below the main title on the web UI home page. Set it to an empty string to hide it.',
    },
    {
      name: 'color_scheme',
      type: 'string',
      defaultValue: 'automatic',
      scopes: ['server'],
      description: 'Default web UI color scheme. Supported values are automatic, dark, and light. Visitors can override this default from the appearance menu.',
    },
    {
      name: 'snippet_length',
      type: 'int',
      defaultValue: '300',
      scopes: ['server'],
      description: 'Maximum number of characters in search result text snippets and semantic previews, excluding highlighting and ellipses. Must be positive. Restart after changing; no reindexing is needed.',
    },
    {
      name: 'search_url',
      type: 'string',
      defaultValue: 'https://google.com/search?q={query}',
      scopes: ['server'],
      description: 'Fallback web search URL. Use {query} as the placeholder for the search term.',
    },
    {
      name: 'access_token',
      type: 'string',
      defaultValue: '(none)',
      scopes: ['server', 'client'],
      description: 'Optional access token for securing the API. See the Access Token section below.',
    },
    {
      name: 'user_handling',
      type: 'bool',
      defaultValue: 'false',
      scopes: ['server', 'client'],
      description: 'Enables multiple user mode. See the User Handling documentation for details.',
    },
    {
      name: 'public',
      type: 'bool',
      defaultValue: 'false',
      scopes: ['server', 'client'],
      description: 'Allows unauthenticated users to search public documents, use API docs, MCP search, previews, and file serving. Requires access_token or user_handling.',
    },
    {
      name: 'log_level',
      type: 'string',
      defaultValue: 'info',
      scopes: ['server', 'client'],
      description: 'Log verbosity. Supported values are error, warning, info, debug, and trace. The err and warn aliases are also accepted.',
    },
    {
      name: 'log_format',
      type: 'string',
      defaultValue: 'text',
      scopes: ['server', 'client'],
      description: 'Log output format. Text emits colored human readable lines. JSON emits one object per entry for log aggregators.',
    },
    {
      name: 'log_file',
      type: 'string',
      defaultValue: '(none)',
      scopes: ['server', 'client'],
      description: 'Path to a log file. Hister creates or appends to this file instead of writing logs to standard error.',
    },
    {
      name: 'debug_sql',
      type: 'bool',
      defaultValue: 'false',
      scopes: ['server'],
      description: 'Enables verbose SQL query logging.',
    },
    {
      name: 'open_results_on_new_tab',
      type: 'bool',
      defaultValue: 'false',
      scopes: ['server'],
      description: 'Opens search results in a new browser tab instead of the current tab.',
    },
    {
      name: 'show_suggestions',
      type: 'bool',
      defaultValue: 'false',
      scopes: ['server'],
      description: 'Shows the suggestion area in the results view by default. Change Show suggestions in the results page Actions menu to override this setting. Your choice is saved in the current browser.',
    },
    {
      name: 'redirect_on_no_results',
      type: 'bool',
      defaultValue: 'true',
      scopes: ['server'],
      description: 'Redirects to the configured search_url when a query returns no results. Disable it to remain within Hister.',
    },
    {
      name: 'display_extractor_config',
      type: 'bool',
      defaultValue: 'false',
      scopes: ['server'],
      description: 'Includes extractor option definitions in the extractor API response so clients can display configuration details.',
    },
    {
      name: 'disable_previews',
      type: 'bool',
      defaultValue: 'false',
      scopes: ['server'],
      description: 'Disables the preview panel and prevents HTML storage. See the Disable Previews section below.',
    },
    {
      name: 'profiler',
      type: 'bool',
      defaultValue: 'false',
      scopes: ['server'],
      description: 'Serves the Go runtime profiling endpoints under /debug/pprof, behind the same authentication as the rest of the API.',
    },
  ];

  const serverOptions = [
    {
      name: 'address',
      type: 'string',
      defaultValue: '127.0.0.1:4433',
      description: 'Host and port to listen on, or unix:/absolute/path for a Unix socket. Use [::]:4433 or 0.0.0.0:4433 to listen on all interfaces.',
    },
    {
      name: 'base_url',
      type: 'string',
      defaultValue: 'derived from address',
      scopes: ['server', 'client'],
      description: 'Public URL of the Hister instance. Required when listening on all interfaces or a Unix socket. It must match how you access Hister.',
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
      description: 'Maximum request size in MiB for document uploads, PDF uploads, and batches. For PDFs, the limit includes the encoded content. Import clients use this value to keep submitted batches within the server limit.',
    },
    {
      name: 'metrics',
      type: 'bool',
      defaultValue: 'false',
      description: 'Exposes Prometheus metrics at /metrics under the configured base URL. Uses token authentication when configured and requires an administrator in multiple user mode. See Monitoring for setup and metric definitions.',
    },
    {
      name: 'expose_version',
      type: 'bool',
      defaultValue: 'false',
      description: 'Includes the running Hister version in the public /api/config response without authentication. Disabled by default. See Monitoring for setup.',
    },
    {
      name: 'proxy_auth_header',
      type: 'string',
      defaultValue: '(empty, disabled)',
      description: 'Authenticates users by a header set by a trusted authentication proxy and automatically enables user handling. Unsafe without an authentication proxy and protection against direct access. See Reverse Proxy Authentication below.',
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
      defaultValue: 'terminal',
      description: 'Appearance mode. Terminal inherits your terminal background, foreground, and ANSI palette. Use auto to choose the configured dark or light Hister theme, or select dark or light explicitly.',
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
      description: 'Enables automatic language detection for newly processed documents. Existing documents remain searchable; reindex to apply the setting to them.',
    },
    {
      name: 'languages',
      type: 'string[]',
      defaultValue: '(none)',
      description: 'Limits automatic detection to at least two distinct supported languages. Regional codes and aliases count as the same language. Leave empty to consider all supported languages. A shorter list can reduce memory use, but content in other languages may be misidentified.',
    },
    {
      name: 'language_detection_accuracy',
      type: 'string',
      defaultValue: 'low',
      description: 'Use low (the default) to reduce memory use, or high to improve detection of short text at the cost of more memory. Very short or unclear text can remain unknown in either mode.',
    },
    {
      name: 'keep_stopwords',
      type: 'bool',
      defaultValue: 'false',
      scopes: ['server'],
      description: 'Keeps common words such as for and your searchable. Changing this setting requires reindexing.',
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
    {
      name: 'pdf_parser.command',
      type: 'string',
      defaultValue: '(empty, built in reader)',
      description: 'Optional PDF parser executable. Reads PDF bytes from standard input and writes plain UTF 8 text to standard output. See File Types for setup and examples.',
    },
    {
      name: 'pdf_parser.args',
      type: 'string[]',
      defaultValue: '(none)',
      description: 'Arguments passed literally to the PDF parser command. Requires pdf_parser.command. Each argument is a separate list entry.',
    },
    {
      name: 'pdf_parser.timeout',
      type: 'int',
      defaultValue: '30',
      description: 'Maximum time in seconds for an external PDF parser command. Must be positive.',
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
      scopes: ['server'],
      description: 'Automatically removes a file from the index when it is deleted or renamed.',
    },
    {
      name: 'user',
      type: 'string',
      defaultValue: '""',
      scopes: ['server'],
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
    {
      name: 'identity_claim',
      type: 'string',
      defaultValue: 'email',
      requirement: 'Optional',
      description: 'OIDC only. UserInfo claim used to match a Hister account, such as sub. Empty uses email for compatibility. See OIDC Account Identity before changing this for existing accounts.',
    },
    {
      name: 'admin_groups',
      type: '[]string',
      defaultValue: '[]',
      requirement: 'Optional',
      description: 'OIDC only. Membership in any listed group grants administrator access. When configured, each OIDC login also removes administrator access from nonmembers.',
    },
    {
      name: 'groups_claim',
      type: 'string',
      defaultValue: 'groups',
      requirement: 'Optional',
      description: 'OIDC only. Exact UserInfo claim name containing group memberships. Used when admin_groups is configured. Empty uses groups.',
    },
    {
      name: 'disable_pkce',
      type: 'bool',
      defaultValue: 'false',
      requirement: 'Optional',
      description: 'Disables S256 PKCE for this provider. Use only for older providers that reject PKCE parameters.',
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
    {
      name: 'capture_delay',
      type: 'float',
      defaultValue: '0',
      description: 'Seconds to wait after page load before capturing HTML for pages that rely on JavaScript rendering.',
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
      defaultValue: '2000',
      description: 'Requested vector dimensionality. The selected embedding model and endpoint must support this output size. Hister limits PostgreSQL vector storage to 2000 dimensions.',
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
      description: 'Maximum embedding workers and simultaneous indexing requests to the endpoint. Query requests have a separate limit. Values below one use the default of two.',
    },
    {
      name: 'max_query_embedding_concurrency',
      type: 'int',
      defaultValue: '1',
      description: 'Maximum simultaneous query embedding requests, independent of indexing. Values below one use the default of one. Total endpoint concurrency can reach the sum of both limits. Separate slots bypass the local indexing queue but do not reserve capacity or priority on the embedding server.',
    },
    {
      name: 'query_embedding_timeout',
      type: 'int',
      defaultValue: '2',
      description: 'Maximum seconds allowed for query slot acquisition, the embedding request, and retries combined. On timeout, the search returns its keyword results. This does not bound keyword retrieval or vector lookup. Increase it for slow hardware or model startup. Values below one use the default of two seconds; an earlier caller deadline or embedding_timeout still applies.',
    },
  ];
</script>

Hister is configured via a YAML file. It selects a primary config path using this precedence:

1. The path supplied with `--config`.
2. The path in `HISTER_CONFIG`, when `--config` is not supplied.
3. `./config.yml` in the current working directory.

If the selected primary path does not exist, Hister searches the platform paths below and uses the first config file it finds.

## Default Config Locations

**Linux** (respects `$XDG_CONFIG_HOME`):

- `$XDG_CONFIG_HOME/hister/config.yml` if `$XDG_CONFIG_HOME` is set, otherwise `~/.config/hister/config.yml`
- `~/.histerrc` (legacy, deprecated)

**macOS** (respects `$XDG_CONFIG_HOME`):

- `$XDG_CONFIG_HOME/hister/config.yml` if `$XDG_CONFIG_HOME` is set, otherwise `~/Library/Preferences/hister/config.yml`
- `~/Library/Application Support/hister/config.yml` (backwards compatible)
- `~/.histerrc` (legacy, deprecated)
- `~/.config/hister/config.yml` (legacy)

**Windows** (respects environment variables):

- `%LOCALAPPDATA%\hister\config.yml` (recommended)
- `$XDG_CONFIG_HOME\hister\config.yml` (if `$XDG_CONFIG_HOME` is set)
- `%APPDATA%\hister\config.yml` (fallback)
- `~\.histerrc` (legacy, deprecated)
- `~\.config\hister\config.yml` (legacy)

If you have a legacy `~/.histerrc` file and Hister finds it, you'll see a deprecation warning suggesting you move it to the recommended location.

## Creating a Config File

Use `hister config path` to locate the selected main config, `hister config show` to inspect
effective YAML with credentials redacted, and `hister config validate` to check configuration
without creating runtime files. `hister doctor` also checks connectivity, authentication, index
compatibility, and extractor dependencies. See [Terminal Client](terminal-client) for examples,
output formats, and exit statuses.

Generate a commented reference containing all default settings:

```bash
hister config create ~/.config/hister/config.yml
```

Every YAML line is commented, including the listen address for your installed build. Comments
do not set values. To activate a setting, remove its leading `# ` and do the same for its
enclosing section, preserving indentation. Leave unrelated settings commented. For example:

```yaml
app:
  title: My Hister
#   subtitle: Your own search engine
```

The examples reflect your installed version. Defaults can change after an upgrade. Existing
active configuration files keep their meaning. To hand an editable setting to the web editor,
comment out or remove that key and reload Hister. A stored web selection then takes effect,
or the default if no web selection exists. Environment variables and startup flags still take precedence.

Use `--full` to write all defaults as active YAML values:

```bash
hister config create --full /path/to/config.yml
```

Omit the filename to print YAML to standard output. Existing files are never overwritten.
Config creation works even when the current configuration is missing or invalid, and does not
initialize runtime data.

Or use your platform's recommended location:

```bash
# Linux
hister config create ~/.config/hister/config.yml

# macOS
hister config create ~/Library/Preferences/hister/config.yml

# Windows
hister config create %LOCALAPPDATA%\hister\config.yml
```

You can also specify a custom config location using the `--config` flag:

```bash
hister listen --config /path/to/my/config.yml
```

## Settings in the Web Interface

Open **Settings** from the navigation menu to change appearance, snippet length, search
behavior, and log level or format. Administrators can configure multi user instances.
Token protected instances require signing in; instances without authentication need no login.

Keep one draft across category tabs, then choose **Save and apply**. Saving briefly interrupts
service and refreshes the interface. Validation errors preserve your draft. If settings changed
elsewhere or a save response was lost, reload settings and check the active values before retrying.

Each setting shows its source. Explicit values in the main config, environment, or startup
flags are locked, even when they equal a default. Commented examples do not lock settings.
**Reset** removes a web selection and restores the inherited value. **Clear** selects an empty
value. A locked setting can retain a web selection that takes effect when its overriding source is removed.

Web selections persist in `config.web.yml` inside the data directory, including when there is
no main config file. Saving does not rewrite your main configuration. Other categories and
connection or authentication settings are currently read only. Stored credentials stay hidden.

## Reloading a Running Server

After editing the main configuration or `rules.json`, run:

```bash
hister reload
```

This applies updated settings without manually restarting Hister. Expect a brief interruption,
then refresh any open Hister web pages. Restart terminal clients separately to apply changes to
their own settings.

Edit the configuration file used to start the server. Settings supplied through startup flags or
environment variables still take precedence. An admin token is required in multi user mode.

If the server started without a configuration file, reload keeps using defaults and startup
overrides. Restart with `--config FILENAME` to select a new file. If the server started with a
file, that file must remain readable; reload never silently switches to another one.

Logging changes take effect on reload, including `app.log_file`, `app.log_format`, and
`app.log_level`. You can also reload after renaming a log file for rotation. Clearing
`app.log_file` sends logs back to the terminal or service logs. If the new log file cannot be
opened, Hister keeps the previous settings.

Invalid settings are rejected. If applying new settings fails, Hister tries to continue with the
previous settings. If it cannot recover, restart the server.

Changing the database or data directory does not copy or move your existing data. Some search
setting changes also require `hister reindex`; Hister reports when this is needed.

The command waits for reload to finish. Use `--client-timeout N` to limit the wait to N seconds.
Update the server URL or credentials in your clients if you changed them.

Use the running server's current URL and token if you have edited connection settings locally:

```bash
hister reload --server-url http://127.0.0.1:4433 --token CURRENT_TOKEN
```

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

| Variable          | Description                                                                                   |
| ----------------- | --------------------------------------------------------------------------------------------- |
| `HISTER_CONFIG`   | Select a config file when `--config` is not supplied                                          |
| `HISTER_PORT`     | Override the TCP port while keeping the host from `server.address`. Ignored for Unix sockets. |
| `HISTER_DATA_DIR` | Override `app.directory`                                                                      |

## Full Configuration

Use the configuration generated by your installed Hister version instead of copying a static example. Print all current default values as comments:

```bash
hister config create
```

To write all defaults as active YAML values, add `--full` and a destination path:

```bash
hister config create --full /path/to/config.yml
```

Each option below has one or both scope tags. The `server` tag marks settings used by the Hister server, its index, or the web interface it serves. The `client` tag marks settings used by command line operations or the terminal interface on the machine where they run. If your client and server use separate configuration files, add a setting to each tagged side where you need its behavior.

## `app` Section

<ConfigReference items={appOptions} />

## `server` Section

<ConfigReference items={serverOptions} scopes={['server']} />

For `server.metrics` and `server.expose_version`, see [Monitoring](monitoring) for setup, access rules, and available information.

For Unix socket permissions and reverse proxy examples, see [Unix sockets](server-setup#unix-sockets).

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

<ConfigReference items={semanticConnectionOptions} scopes={['server']} />

### Chunking and Input

<ConfigReference items={semanticInputOptions} scopes={['server']} />

### Retrieval

<ConfigReference items={semanticRetrievalOptions} scopes={['server']} />

### Processing

<ConfigReference items={semanticProcessingOptions} scopes={['server']} />

### Vector Storage Backends

The vector store backend is chosen automatically based on `server.database`:

- **SQLite** (default) stores vectors in a separate `vectors.sqlite3` file in the same directory as the main database, using the [sqlite-vec](https://github.com/asg017/sqlite-vec) extension. No extra setup required.
- **PostgreSQL** stores vectors in the same database as the main data using the [pgvector](https://github.com/pgvector/pgvector) extension. Hister uses an HNSW index with the `vector` type, which supports at most 2000 dimensions. Make sure `pgvector` is installed and enabled (`CREATE EXTENSION vector;`) before starting Hister.

Use `HISTER__SEMANTIC_SEARCH__QUERY_EMBEDDING_TIMEOUT` and `HISTER__SEMANTIC_SEARCH__MAX_QUERY_EMBEDDING_CONCURRENCY` to override the query deadline and concurrency through environment variables. Changing these settings does not require reindexing.

Set `semantic_search.dimensions` to the output size supported by your embedding endpoint. If the endpoint returns a different size, Hister rejects the embeddings. After changing dimensions with SQLite, restart Hister and run `hister reindex` to rebuild the vector table and regenerate embeddings. Reindexing checks the endpoint before clearing existing vectors and stops if that check fails. Startup logs report when the stored dimensions differ from the configuration.

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
  max_query_embedding_concurrency: 1
  query_embedding_timeout: 2
  # api_key: 'sk-...'            # required for hosted providers
  # headers: {}                  # extra HTTP headers for proxies or custom auth
```

The example above uses [nomic-embed-text](https://ollama.com/library/nomic-embed-text) via Ollama, which produces 768-dimensional vectors and fits well in a 512-token context window. The `query_prefix` and `document_prefix` values shown are the ones recommended by the Nomic model. Other models use different conventions: `"query: "` / `"passage: "` for E5 and BGE families (this is also the built-in default for `query_prefix`), `"Represent this sentence for searching relevant passages: "` for GTE. Check your model's documentation for the expected prefix strings. Set both to `""` for models that do not use prefixes (such as OpenAI `text-embedding-3-*`).

## TUI Settings

TUI settings are configured in a separate `tui.yaml` file located in the same directory as your main config file. This file is automatically created with default values when you first run `hister search`.

### Theme Settings

<ConfigReference items={tuiThemeOptions} scopes={['client']} />

**Built-in themes**: catppuccin-frappe, catppuccin-latte, catppuccin-macchiato, catppuccin-mocha, dracula, gruvbox, gruvbox-light, material-lighter, nord, nord-light, one-light, rose-pine, rose-pine-dawn, solarized-light, tokyonight, and tomorrow.

## `indexer` Section

<ConfigReference items={indexerOptions} scopes={['server', 'client']} />

See [External PDF Parser](file-types#external-pdf-parser) to use a command such as `pdftotext`. Environment overrides include `HISTER__INDEXER__PDF_PARSER__COMMAND=pdftotext`, `HISTER__INDEXER__PDF_PARSER__ARGS=-enc,UTF-8,-,-`, and `HISTER__INDEXER__PDF_PARSER__TIMEOUT=30`. Environment arguments are separated by commas; use YAML when an argument contains a comma.

### Directory Entry

Each entry in `directories` is an object with the following keys:

<ConfigReference items={directoryOptions} scopes={['server', 'client']} />

When multiple filters are specified, they are applied in order: excludes first, then filetypes, then patterns. A file must pass all specified filters to be indexed. When a filter is omitted, it is not applied (all files pass).

## Local Directory Indexing

The `indexer.directories` option lets you index local files so they appear alongside your browser history in search results. You do not need to run `hister import file` for configured directories that the server can access. After changing this configuration, restart the Hister server. It scans the directories automatically at startup, then a file watcher monitors them so new and modified files are indexed without another restart.

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
- Files matching `sensitive_content_patterns` are skipped with a warning that includes the path but no matched content

`hister list-files` lists files matching the configured directory watch rules. It does not check file contents or report indexing status, so it also lists files rejected by sensitive content, size, or format checks. Rejected files remain watched and are checked again when they change.

Changes to indexed directories are picked up automatically by the file watcher, no server restart is needed. On server start, only files that have been modified since they were last indexed are re-processed. File results appear with the domain `local` and are served through the Hister web interface directly.

When a configured directory is available to the command line client but not to the server, run `hister import file` with no path arguments. It applies these directory rules locally, extracts matching content, and creates remote file snapshots through the normal add API. The original bytes are not sent. Add `--watch` to keep importing new and changed snapshots until interrupted. Watch mode skips exports, archives, and saved HTML with source URL metadata. Remote snapshots are never removed automatically, even with `delete_on_remove: true`. See [Importing Documents](import) for details.

For directories watched by the server, `delete_on_remove: true` makes deleting or renaming a file on the filesystem remove it from the index automatically. This is optional and disabled by default. It does not apply to remote snapshots imported by the client.

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

Use `hister listen --public=false` to override public mode enabled in the file or environment.
This explicit choice also remains in effect after reload.

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

## Reverse Proxy Authentication

> **Security warning: Do not enable `server.proxy_auth_header` without a trusted authentication proxy in front of Hister.** Hister trusts the configured header as proof of identity and does not verify that the sender is your proxy. Anyone who can supply that header can impersonate an existing user, including an administrator, or create a new account. HTTPS or a reverse proxy that only forwards traffic does not make this safe.

Before enabling this feature, configure your proxy to authenticate requests, discard any client supplied value of the configured header, and set it from the verified user's identity. Prevent untrusted clients from reaching Hister directly and bypassing the proxy. Use a loopback listener when the proxy runs on the same host, a Unix socket with restricted permissions, or network rules that restrict backend access to the trusted proxy. Do not expose Hister's backend port publicly, including through container port publishing.

Once these protections are in place, set the header name in your Hister configuration. This example assumes the authentication proxy runs on the same host and sets `Remote-User`:

```yaml
server:
  address: 127.0.0.1:4433
  base_url: https://hister.example.com
  proxy_auth_header: 'Remote-User'
```

The equivalent environment variable is `HISTER__SERVER__PROXY_AUTH_HEADER=Remote-User`. Use the header name configured in your proxy, such as `Remote-User` or `X-Forwarded-User`. Leaving the setting empty disables proxy authentication.

When enabled:

- Hister automatically enables `app.user_handling`.
- The header value, with surrounding whitespace removed, identifies the Hister username. An existing account keeps its documents, rules, token, and permissions, including administrator privileges. Use unique identities controlled by the authentication service; users must not be able to choose another Hister user's name.
- A username without an existing account is automatically created on its first request. New accounts have no administrator privileges and no password, so password login is unavailable unless an administrator assigns one.
- A successfully authenticated proxy identity takes precedence over session cookies and API tokens. Missing or empty headers fall back to normal session and personal access token authentication. Enabling this option does not disable existing password or OAuth login.

CLI, browser extension, and MCP clients can still use a personal access token through `X-Access-Token` or `Authorization: Bearer TOKEN` when the proxy header is absent. They must also satisfy your proxy's access policy. Do not expose the backend to bypass that policy. In multiple user mode, a token must belong to a Hister user; a standalone `app.access_token` does not grant access.

Hister's **Logout** action only clears its own session. It does not sign you out of the authentication proxy, so subsequent requests carrying the identity header remain authenticated. Sign out through your authentication proxy to end that access.

See [User Handling](/docs/user-handling) for account management and document ownership, and [Server Setup](/docs/server-setup) for listener and reverse proxy configuration.

## OAuth

When [user handling](/docs/user-handling) is enabled, Hister supports delegating authentication to external OAuth 2.0 / OpenID Connect providers. Users can then sign in with their existing accounts instead of a Hister-local password.

The `server.oauth` key is a map where each key is a provider name and the value is its configuration. Three providers are built in:

| Provider | Description                                                  |
| -------- | ------------------------------------------------------------ |
| `github` | GitHub accounts via the GitHub OAuth app                     |
| `google` | Google accounts via Google Identity                          |
| `oidc`   | Any OpenID Connect provider (Keycloak, Authentik, Dex, etc.) |

Each entry supports the following fields:

<ConfigReference items={oauthOptions} scopes={['server']} />

### GitHub Example

Register an OAuth app at [github.com/settings/developers](https://github.com/settings/developers). Set the **Authorization callback URL** to `https://your-hister-instance/api/oauth/callback?provider=github`.

```yaml
server:
  oauth:
    github:
      client_id: 'your-github-client-id'
      client_secret: 'your-github-client-secret'
```

GitHub sign in identifies your account by its permanent account ID. A GitHub username change keeps the same Hister account.

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

### OIDC Account Identity

Set `identity_claim: 'sub'` to identify an OIDC account by its subject, so changing the user's email address does not create a new Hister account. The [OIDC specification](https://openid.net/specs/openid-connect-core-1_0.html#ClaimStability) defines `sub` as stable and unique within its issuer. Keep the same issuer when using existing account links.

```yaml
server:
  oauth:
    oidc:
      client_id: 'hister'
      client_secret: 'your-client-secret'
      configuration_url: 'https://accounts.example.com/.well-known/openid-configuration'
      identity_claim: 'sub'
```

The equivalent environment variable is `HISTER__SERVER__OAUTH__OIDC__IDENTITY_CLAIM=sub`. This works with discovery or manually configured endpoints. The option selects an exact, case sensitive field in the UserInfo response. Custom claim names, including URI names, are supported; dots do not select nested fields. Choose a unique, immutable identifier controlled by the provider. Fields users can edit, such as `preferred_username`, are unsuitable for account matching.

The selected claim must be a nonempty JSON string containing more than whitespace. If it is missing or invalid, login fails without falling back to email. An email address is optional when another claim identifies the account. New usernames use `preferred_username`, then email, then the OAuth identity if neither is available. Existing usernames are preserved on subsequent logins.

Omitting `identity_claim`, leaving it empty, or setting it to `email` preserves existing `oidc-<email>` account links. Other claims use `oidc:<escaped claim name>:<claim value>`, for example `oidc:sub:abc123`. The claim name uses URL query escaping. This separates identities from different claims and from legacy email identities.

Before switching an existing deployment, back up the database and explicitly relink each affected user's `o_auth_id` to the new identity using a verified mapping from the provider. Hister does not automatically link accounts by matching email addresses or usernames. Without relinking, a successful login with the new claim creates a separate account, and the old account retains its documents and history. Changing the issuer also requires a verified mapping because subjects are unique only within an issuer.

### OIDC Administrator Groups

Set `admin_groups` to manage administrator access through your identity provider:

```yaml
server:
  oauth:
    oidc:
      client_id: 'hister'
      client_secret: 'your-client-secret'
      configuration_url: 'https://accounts.example.com/.well-known/openid-configuration'
      scopes: ['groups'] # Only if your provider requires this scope.
      admin_groups: ['hister-admins']
      groups_claim: 'groups'
```

Configure your provider to include group memberships in its UserInfo response as a list of strings. Membership in any configured group grants administrator access. Names match exactly and are case sensitive. Use groups controlled by your identity provider's administrators. If the claim has another name, set `groups_claim` to that exact name; dots do not select nested fields. Groups available only in an ID token are not used.

Each successful OIDC login synchronizes the user's role. Missing, null, or empty groups remove administrator access. Invalid group values prevent login. Manual role changes are replaced at the next OIDC login.

Before enabling administrator groups, create an independent [local recovery administrator](/docs/user-handling#user-create) with the server stopped. Hister requires this account when starting or reloading and prevents removing or demoting the last usable local recovery administrator. Keep its personal access token for command line recovery when `oauth_only` disables password login. Provider role revocations still take effect.

Group changes take effect at the next OIDC login, then apply to the user's existing sessions and API token. Hister does not check group membership between logins. Omitting `admin_groups` or leaving it empty preserves manually managed roles. Local accounts and other OAuth providers are unaffected.

Environment equivalents are `HISTER__SERVER__OAUTH__OIDC__ADMIN_GROUPS=hister-admins,operators` and `HISTER__SERVER__OAUTH__OIDC__GROUPS_CLAIM=groups`.

### PKCE and Compatibility

Hister uses PKCE with the S256 challenge method for GitHub, Google, and OIDC logins by default. Each login generates a fresh private verifier stored in the server side session. The authorization request includes its SHA256 challenge, and the token exchange sends the original verifier. This supports providers such as Kanidm with PKCE enforcement enabled, including when OIDC discovery does not advertise PKCE support.

Existing client IDs, client secrets, scopes, callback URLs, and linked accounts continue to work. Client secrets are still required. No database migration or reindexing is needed, and existing authenticated sessions remain valid.

For an older provider that rejects PKCE parameters, explicitly set `disable_pkce: true` on that provider:

```yaml
server:
  oauth:
    oidc:
      client_id: 'hister'
      client_secret: 'your-client-secret'
      configuration_url: 'https://accounts.example.com/.well-known/openid-configuration'
      disable_pkce: true
```

The equivalent environment variable is `HISTER__SERVER__OAUTH__OIDC__DISABLE_PKCE=true`. Disabling PKCE removes the protection it provides against authorization code interception. Hister does not automatically retry without PKCE after a provider error.

Logins started before upgrading must be restarted because their sessions lack the verifier and provider binding. A callback consumes its matching login state before exchanging the code, so failed or denied logins must also be restarted. GitHub token exchanges now use a form POST to keep credentials and the verifier out of the URL; custom GitHub token endpoints or proxies must accept POST.

### How It Works

1. The login page shows a **Sign in with &lt;Provider&gt;** button for each configured provider.
2. Clicking the button redirects the user to the provider's authorization page.
3. After the user grants access the provider redirects back to `/api/oauth/callback?provider=<name>`.
4. Hister verifies the state token and provider, exchanges the authorization code with the PKCE verifier for a token, and fetches the user's identity from the provider.
5. If no local account is linked to that identity, one is created automatically. GitHub uses the login name, Google uses the account name with the full email address as a fallback, and OIDC uses `preferred_username`, then the full email address, then the OAuth identity as the username.
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

## Search Result Context

Increase `app.snippet_length` to show more text around keyword matches and longer semantic previews:

```yaml
app:
  snippet_length: 800
```

The default is 300 characters. The value must be a positive integer. Highlighting and ellipses do not count toward this limit. Short documents and semantic chunks can return less text. Larger values increase the amount of data returned by each search.

The web app shows up to three lines of each snippet. Select **Show more** to expand a clipped snippet or **Show less** to collapse it. To expand snippets by default, enable **Always expand snippets** in **Actions**. This preference is remembered in your browser.

You can also set `HISTER__APP__SNIPPET_LENGTH=800`. Restart Hister after changing the setting. Existing documents use the new length immediately after restart; no reindexing is needed.

## Language Detection

Hister identifies document languages locally to help searches match related word forms. The defaults consider all supported languages with reduced memory use:

```yaml
indexer:
  detect_languages: true
  languages: []
  language_detection_accuracy: 'low'
  keep_stopwords: false
```

Use `languages` to limit detection to at least two distinct supported languages, `language_detection_accuracy: high` to improve detection of short text, or `keep_stopwords: true` to keep common words searchable. Regional codes and aliases count as the same language.

Setting `detect_languages: false` turns off automatic detection for newly processed documents. Existing documents remain searchable, and manual language pins are preserved. These settings also apply to service imports run from the command line.

Restart after changing the server configuration. Reindex to apply the new settings to existing documents. Until then, those documents remain searchable with their previous settings.

See [Language Settings and Detection](/docs/language-detection) for supported codes, configuration examples, accuracy and memory tradeoffs, and instructions for correcting document languages.

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
color_scheme: 'terminal'

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
  y: 'copy_result'
  v: 'toggle_preview'
  l: 'edit_label'
  ctrl+d: 'delete_result'
  ctrl+t: 'toggle_theme'
  ctrl+s: 'toggle_settings'
  ctrl+o: 'toggle_sort'
  ctrl+e: 'toggle_semantic'
  alt+1: 'tab_search'
  alt+2: 'tab_history'
  alt+3: 'tab_rules'
  alt+4: 'tab_add'
```

The default `terminal` mode does not paint a terminal-wide foreground or
background. Normal text inherits your terminal colors, while semantic accents
use its configurable ANSI palette. Set `color_scheme` to `auto`, `dark`, or
`light` to opt into Hister's full built-in themes. The Settings overlay
(`ctrl+s`) exposes this as **Appearance — Terminal (pass-through)**; press Enter
to cycle modes, or use `ctrl+t` for the full theme picker.

### TUI Hotkeys

TUI keyboard shortcuts are configured in `tui.yaml` under the `hotkeys` section. See the [tui.yaml example](#tui-configuration) above.

| Action            | Description                                                                       |
| ----------------- | --------------------------------------------------------------------------------- |
| `quit`            | Exit the TUI                                                                      |
| `toggle_help`     | Show/hide the keybindings help overlay                                            |
| `toggle_focus`    | Change focus or return to the previous workspace                                  |
| `scroll_up`       | Move selection up                                                                 |
| `scroll_down`     | Move selection down                                                               |
| `open_result`     | Open, edit, or submit the focused item                                            |
| `copy_result`     | Copy the selected URL                                                             |
| `toggle_preview`  | Show/hide selected result details                                                 |
| `edit_label`      | Edit the selected document label                                                  |
| `delete_result`   | Delete the selected entry from the index                                          |
| `toggle_theme`    | Open the interactive theme picker overlay                                         |
| `toggle_settings` | Open appearance and keybinding settings                                           |
| `toggle_sort`     | Toggle domain-based sorting for search results                                    |
| `toggle_semantic` | Toggle semantic search when enabled                                               |
| `tab_search`      | Switch to the Search tab                                                          |
| `tab_history`     | Switch to the History tab (view recent searches)                                  |
| `tab_rules`       | Switch to the Rules tab (manage allow/skip/priority/versioning rules and aliases) |
| `tab_add`         | Switch to the Add tab (manually add URLs and multiline text)                      |

## `crawler` Section

The `crawler` section configures the web crawler used by `hister index`, `hister index --recursive`,
browser imports, and service imports that need to download bookmark content. These commands share
the same backend and request settings.
Every recursive crawl runs as a persistent job so it can be interrupted and resumed
without losing progress. See [Website Crawler](crawler) for usage details.

<ConfigReference items={crawlerOptions} scopes={['client']} />

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

**`http` backend**: no backend specific options supported.

**`chromedp` backend**:

Launches Chrome or Chromium on the machine running `hister index`. Install the browser there and
use `exec_path` if it is not found automatically. This backend launches its own browser process;
Hister does not currently expose an option to connect it to a remote CDP socket.

<ConfigReference items={chromedpOptions} scopes={['client']} />

```yaml
crawler:
  backend: 'chromedp'
  backend_options:
    exec_path: '/usr/bin/chromium'
    capture_delay: 1.5
  timeout: 15
```

**`bidi` backend** (WebDriver BiDi):

Connects to an already running browser that exposes a
[WebDriver BiDi](https://w3c.github.io/webdriver-bidi/) WebSocket endpoint. Hister opens the supplied
WebSocket and sends `session.new`; it does not launch a browser or create a session through a
WebDriver HTTP server. Firefox supports this direct connection at `/session`.

Chrome and Chromium's `--remote-debugging-port` exposes the Chrome DevTools Protocol (CDP).
That socket cannot be used as a BiDi endpoint. Use Hister's `chromedp` backend for a local Chrome
or Chromium installation.

See [Firefox with BiDi](crawler#firefox-with-bidi) for browser startup and indexing commands, or
[browser backends with Docker](docker#browser-backends-with-docker) when the server is containerized.

<ConfigReference items={bidiOptions} scopes={['client']} />

Apply these settings to the configuration used by the indexing command:

```yaml
crawler:
  backend: 'bidi'
  backend_options:
    host: '127.0.0.1'
    port: '9222'
    capture_delay: 1.5 # wait 1.5s after load for JS to render
  timeout: 15
```

The `host` and `port` options above produce `ws://127.0.0.1:9222/session`. Alternatively, specify
the full socket URL, including `/session`. When `socket` is set, it overrides `host` and `port`:

```yaml
crawler:
  backend: 'bidi'
  backend_options:
    socket: 'ws://127.0.0.1:9222/session'
```

The `bidi` backend subscribes to `network.responseCompleted` for the tab it
navigates, so pages served with a `4xx` or `5xx` status are reported as fetch
failures instead of being indexed as error pages. Redirects are followed and
judged by the status of the final response. Statuses of subresources such as
images or favicons are ignored. When the browser does not report the
subscription, the status stays unknown and the page is indexed as before.

Firefox only allows a single WebDriver session per browser instance. If another
client (an IDE integration, a test runner, Selenium, …) already holds the
session, Hister cannot create one and every command fails with
`invalid session id`. Start a dedicated browser instance with its own profile
and port for crawling.

### Crawler Cookies

Each entry in `cookies` is an object with the following keys:

<ConfigReference items={crawlerCookieOptions} scopes={['client']} />

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
