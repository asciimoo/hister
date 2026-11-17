---
date: '2026-02-13T10:59:19+01:00'
draft: false
title: 'Terminal Client'
description: 'Use Nogon commands, global flags, terminal search, keyboard controls, mouse controls, and themes.'
---

The examples use `nogon`. If your package provides `hister`, use that command
instead. See [upgrading from Hister](installing#upgrading-from-hister) and
[configuration](configuration).

## Global Flags

These flags are accepted on every command for compatibility. Subcommand help only displays the
flags that affect that command.

| Flag                       | Description                                                                                     |
| -------------------------- | ----------------------------------------------------------------------------------------------- |
| `--config PATH`            | Path to config file; otherwise searches `./config.yml`, then Nogon and legacy Hister locations  |
| `--server-url URL` / `-u`  | Nogon server URL (overrides `server.base_url` from config)                                      |
| `--token TOKEN` / `-t`     | Access token for server authentication (overrides `app.access_token` from config)               |
| `--log-level LEVEL` / `-l` | Log level: `error`, `warning`, `info`, `debug`, `trace` (default: `info`)                       |
| `--search-url URL` / `-s`  | Default search engine URL with `{query}` placeholder                                            |
| `--client-timeout N`       | HTTP client timeout in seconds for server communication (0 = no timeout; default if unset: 10s) |

Example: index a slow extractor, such as `yt-dlp`, with a longer timeout:

```bash
nogon --client-timeout 20 index https://example.com
```

When using an access token, set `--server-url` or `server.base_url` to the final server address.
Authenticated commands refuse redirects to another host or port, or between HTTP and HTTPS,
to keep your token on the selected server.

## Execution Scope

Root help groups Nogon operations by scope, and each operation help page identifies where the
command operates:

**Local** commands use configured files or local Nogon data directly and do not contact the
configured Nogon HTTP server. Examples include `nogon crawl list` and `nogon list-files`.

**Remote** commands use the configured Nogon HTTP server without opening the local Nogon
database or search index. Examples include `nogon search`, `nogon export`, `nogon cleanup`,
`nogon user show`, `nogon user update`, and `nogon user delete`.

**Hybrid** commands use or can use both local Nogon state and the configured server. Examples
include persistent `nogon index` modes, `nogon import browser`, and `nogon list-urls --offline`.
`nogon user create` also belongs to this group: it uses the server by default and
creates an account directly in the configured database only with `--offline`.

Use the scope label before running a command against a remote deployment. Local commands operate
on the data directory selected by the local configuration even when `--server-url` is supplied.

## Command-Line Usage

View all available commands:

```bash
./nogon help
```

### Check for Updates

Check whether a newer Nogon release is available:

```bash
nogon check-update
```

The command displays the installed version and the latest published version when an update is
available. It also provides a link to the release. When Nogon is current, the command reports
that it is up to date.

This check requires internet access. It does not download or install the update.

### Configuration and Diagnostics

```bash
nogon config create ~/.config/nogon/config.yml
nogon config path
nogon config show
nogon --config /etc/hister/config.yml config validate
nogon reload
nogon doctor
nogon --server-url https://hister.example.com --token "$HISTER_TOKEN" doctor --format json
```

`reload` applies updated settings to the running server, including logging changes. Expect a brief
interruption and refresh open Nogon web pages afterwards. An admin token is required in multi
user mode. See [Reloading a Running Server](configuration#reloading-a-running-server) for examples
and limitations.

`config create [FILENAME]` writes all default settings as commented YAML to a new file, or
prints them when no filename is given. Uncomment a setting and its enclosing section to
activate it, preserving indentation and leaving unrelated settings commented. Use
`config create --full [FILENAME]` to write all defaults as active YAML values. Both forms
refuse to overwrite existing files and work without a valid current configuration.

`config path` prints the absolute path of the selected main config file, or `(defaults)` when
no file is found. It works even when the file contains invalid YAML. `config show` prints
effective YAML after applying defaults, environment variables, and global flags. It redacts
credentials, header and cookie values, URL credentials and query values, PostgreSQL connection
strings, and external command arguments. Separate `rules.json` and `tui.yaml` files are not included.

`config validate` checks main configuration keys and value types, supported settings, hotkeys,
OAuth, public mode, semantic search settings, and extractor names and options. It exits with
status `1` on an error and `0` on success. It does not test network access or executable availability.

These inspection commands do not create data directories, secret keys, rules, TUI files, or log
files. They follow the normal [config search order](configuration#default-config-locations).
An explicit `--config`, `NOGON_CONFIG`, or legacy `HISTER_CONFIG`
path must exist, and unreadable files are reported instead of silently falling back to defaults.

`doctor` checks local configuration and enabled extractor executables, then verifies server
connectivity and authentication. The server checks its own index version, analyzer and embedding
configuration fingerprints, and extractor executables. This supports remote deployments without
opening a local index. Currently, yt-dlp is the extractor that requires an external executable.
The executable is located without running it. Doctor does not repair data or call an embedding provider.

Server checks use `GET /api/diagnostics`. Token authentication is required when configured, including
in public mode. Multi user deployments require an admin token. Older servers report a warning when
diagnostics are unavailable. Use `--client-timeout` to control the HTTP timeout for each request.

Doctor supports `--format` / `-f` with `text`, `json`, `jsonl`, or `csv`. Each record contains
`name`, `status`, and `message`. Status is `ok`, `warning`, or `error`; check names use `local.`
or `server.` prefixes where applicable. Exit status is `1` if any check fails, and `0` when all
completed checks pass or only warnings remain. JSON output remains a complete array when a
diagnostic check reports an error.

### Maintenance

```bash
nogon reindex
nogon cleanup
```

Both commands require a running server. `reindex` rebuilds search indexes from stored documents.
`cleanup` removes indexed local documents that no longer match configured directories, plus
unreferenced preview and favicon files. See [Data Storage and Lifecycle](data-lifecycle) for
details about what cleanup removes.

Both commands print a start message and a completion message with elapsed time to stderr.
When stderr is connected to an interactive terminal, a timer updates every second while the
command waits for the server to finish. Redirected stderr contains plain messages without
timer updates. A failed operation reports an error without printing a completion message.

Cleanup also prints its checked, skipped, and removed counts to stdout, so you can save those
results separately:

```bash
nogon cleanup > cleanup-results.txt
```

### Purge Retained Data

Preview old stored versions, unpinned opened result history, and completed crawl records:

```bash
nogon purge --versions --history --crawl-jobs --before 2026-01-01 --dry
```

Select one or more categories and a `--before` date or RFC3339 timestamp. Omit `--dry` to preview and then confirm deletion, or add `--yes` to skip confirmation. The operation requires a running server and administrator access in multi user mode, and applies across all users. Current documents remain searchable. See [Purging Retained Data](data-lifecycle#purging-retained-data) for the cutoff rules and retained records.

### Index a URL Manually

To manually index a specific URL:

```bash
./nogon index https://example.com
```

For persistent recursive crawls, URL input jobs, custom job names, resume behavior, request
backends, and every `crawl` subcommand, see [Website Crawler](crawler).

### Search Output And Scripting

Provide search terms to print results. Use `--fields` to select fields and `--limit` to stop
after a given number of documents:

```bash
nogon search 'language:en' --format json --fields title,url --limit 20
nogon search 'domain:example.com' --format jsonl --fields url,text
nogon search 'label:research' --format csv --fields title,url > research.csv
```

By default, text output shows only the title and URL. Use `--fields text` (or `-F text`)
to print the complete stored text without truncation. JSON, JSONL, and CSV include the
complete text by default. Selecting fields such as `--fields title,url` omits the text.
HTML is included only when selected with `--fields html`, which can be combined with
other fields, for example `--fields url,text,html`.

When a text search finds no matches in an interactive terminal, Nogon prints a message with
a spelling suggestion when available, or a hint to broaden the search. An empty search result
still exits successfully. Redirected or piped text output stays empty, and structured formats
keep the output described below.

Search, indexing summaries, crawl inspection commands, doctor, and file and service import summaries
share `--format` / `-f`:

| Format  | Output                                                                             |
| ------- | ---------------------------------------------------------------------------------- |
| `text`  | The default human readable output for each command.                                |
| `json`  | A JSON array of records, including an empty array when there are no results.       |
| `jsonl` | One JSON object per line, with no surrounding array. No results produce no output. |
| `csv`   | A header followed by records. Commas, quotes, and newlines in values are escaped.  |

JSON always uses an array, including commands that return one job, count, or import summary.
Unknown format names are rejected. Search preserves the selected field order in CSV and text.
Structured records contain plain data without terminal styling.

Command errors are written to stderr. Search results stream as pages arrive, so a failed request
can leave partial output. Check the exit status before treating a result as complete. JSON arrays
are closed only when the search succeeds; JSONL retains complete records from earlier pages.

File and service imports can emit a summary with numeric `imported`, `skipped`, and `errors` fields:

```bash
nogon import file backup.json --format json
nogon import linkding https://bookmarks.example.com --format jsonl
```

These formats apply to file imports and the Linkding, Linkwarden, Karakeep, Raindrop, Readeck, Readwise Reader, Shaarli, and
wallabag importers. Browser import retains its interactive output. See [Website Crawler](crawler)
for structured crawl inspection.

### Indexing Results And Exit Status

Indexing reports `indexed`, `skipped`, and `failed` counts. Already indexed URLs and URLs excluded
by robots.txt count as skipped. Individual URL failures do not stop the remaining URLs:

```bash
nogon index https://example.com/one https://example.com/two --format json
nogon index --input urls.txt --failed-urls failed-urls.txt
```

The exit statuses for `index`, `import file`, and service imports are:

| Status | Meaning                                                                               |
| ------ | ------------------------------------------------------------------------------------- |
| `0`    | Processing finished without reported item errors. Skipped items are allowed.          |
| `1`    | A setup, source, cancellation, output, or other execution error prevented completion. |
| `2`    | Processing finished with item errors, including when every item failed.               |

File and service imports retain their `imported`, `skipped`, and `errors` summary fields. A service
import also prints its accumulated counts if fetching a later source page fails, then exits with
status `1`. Content extraction errors can count toward `errors` even if bookmark metadata was
successfully imported. Optional favicon download failures remain diagnostic messages.

The `index --failed-urls PATH` option writes one failed URL per line and replaces the file's
contents. It creates an empty file when there are no failed URLs. An unusable report path fails
before indexing starts. Retry the saved URLs with:

```bash
nogon index --force --input failed-urls.txt --failed-urls still-failed.txt
```

Persistent indexing summaries include `job_id` and `pending`. Their counts describe the entire
stored job, including earlier runs and redirect tracking URLs, rather than only the current run.
The retry file likewise contains all URLs currently marked failed in that job. A completed job
with stored failures continues to return status `2` when resumed without more pending work.
Stopping at a configured crawl limit can leave pending URLs without being an error.

### Updating Document Attributes

Use `nogon update` to change attributes on every document selected by the query language:

```bash
nogon update 'user_id:0' --user-id 2
nogon update 'domain:example.com' --label research
nogon update 'language:unknown' --language en
```

The supported attributes are `user_id`, `label`, `title`, and `language`. Pass an empty label or
title to clear it. A language choice remains pinned across reindexing and resubmission.
Known language codes such as `en-US`, `nb`, and `uk` are accepted. Support for matching related
word forms varies by language, but documents remain searchable. Use `unknown` to keep a document
without an identified language, or `auto` to clear the pin and identify the language from saved content
using the running server's detection settings. Nogon reports the number of
matching, changed, unchanged, and conflicting documents.

The command previews the operation and asks for confirmation. Use `--dry` to preview without
applying changes, or `--yes` to apply immediately:

```bash
nogon update 'label:inbox' --label archive --dry
nogon update 'label:inbox' --label archive --yes
```

Changing `user_id` requires administrator access. User ID `0` is reserved for global documents and
does not represent a user account. Documents indexed before user handling was enabled normally
belong to user ID `0`. Global documents are visible to every authenticated user and, when public
mode is enabled, to anonymous visitors. Use `user_id:0` in the query to select global documents.
Use `--user-id 0` as the change to make matching documents global.

An ownership change never overwrites a document that already has the same URL and destination
owner. Those documents are reported as conflicts and skipped. Stored version records move with a
changed owner, while search history stays with the user who created it.

For a watched local file, its configured directory `user` must match the destination owner. Future
indexing can replace a manually changed title or owner, so update the responsible
collector or directory configuration when the change should remain permanent.

Language pins also survive watched file updates and export/import. They apply only to the selected
documents and their owners. See [Language Settings and Detection](/docs/language-detection#checking-and-correcting-languages)
for examples of pinning and clearing languages.

### Exporting Documents

Use `nogon export` to write indexed documents to a JSON file. This is useful for
backups or for moving documents between Nogon instances. By default every indexed
document is exported:

```bash
nogon export backup.json
```

If the output filename has no extension, Nogon appends `.json` automatically.
For example, `nogon export backup` writes `backup.json`. Explicit extensions are
preserved, including `.JSON`. Other extensions produce a warning because the file
importer expects `.json`. The success message shows the actual output filename.

An existing export stays unchanged until the new export is complete. If fetching or
saving fails, the command reports an error and keeps the previous file. Allow enough
free space for both exports during replacement. Saved exports are readable and writable
only by their owner on Unix systems. The destination must be a regular file or a new
filename; symbolic links, directories, and special files are rejected.

You can limit the export to documents matching a search query by passing it after the
output file (see the [query language](query-language) reference):

```bash
nogon export rust.json "rust language:en"
```

Each document is written as a single JSON line; lines that do not start with `{` are
structural markers (`[`, `]`, `,`) and can be safely skipped by parsers. Pass `-` as the
output file to write to standard output instead, which can be piped into other tools:

```bash
nogon export - | gzip > backup.json.gz
```

Standard output can contain partial data if an export fails. Shell redirection also
replaces its destination before Nogon runs. To preserve an existing export until its
replacement succeeds, pass the filename directly to `nogon export`.

Use `--start-date` / `--end-date` (`YYYY-MM-DD`) to only export documents whose `updated`
timestamp falls within the given date range. The resulting file can be re-imported with
`nogon import file` (see below).

### Importing Documents

Use `nogon import file` to add documents from files on disk. It accepts an arbitrary
number of files or directories, which are imported in order and reported as a
combined total:

> **Important:** Use `nogon import file` to restore Nogon JSON exports, including
> exports in 7z archives, regardless of whether the server can access them.
>
> For ordinary documents such as PDFs, notes, and text files, **importing is only
> needed when the Nogon server cannot access the files directly**. Otherwise, use
> [local file indexing](configuration#local-directory-indexing). Configure
> `indexer.directories` and restart the server to scan and watch those directories
> automatically.

```bash
nogon import file export.json page.html ~/Downloads/saved-pages
```

Several input formats are supported:

- **JSON export files** files previously created by `nogon export`. They are read
  line by line and each serialized document is restored without running content extraction
  again.
- **7z archives** (`.7z`) a 7z compressed archive containing a single JSON export file.
- **HTML files** (`.html` or `.htm`) a saved web page. The document URL is extracted
  from the HTML itself (the `<link rel="canonical">` tag, OpenGraph/Twitter `url` meta
  tags, etc.) and the page is submitted to the running server for processing.
- **Local file snapshots** PDF, DOCX, Markdown, Org mode, HTML without source URL metadata,
  JSON without the Nogon export array shape, and valid UTF 8 text are extracted locally.
  Only prepared document fields are sent to the server.

When a directory is passed, Nogon imports matching files recursively. With no input,
it creates remote file snapshots from configured watched directories and applies their
filters. Use this mode only when the command line client can read those directories but
the server cannot. Add `--watch` to keep importing new and changed snapshots while
the command is active. Source removals retain the indexed snapshots.

```bash
# Import a single saved web page
nogon import file ~/Downloads/article.html

# Import all supported files recursively from a directory
nogon import file ~/Downloads/saved-pages

# Create snapshots when the server cannot access configured directories
nogon import file
```

Useful flags:

- `--skip-existing` do not overwrite documents that are already in the index.
- `--start-date` / `--end-date` (`YYYY-MM-DD`) only import documents whose `added`
  timestamp falls within the given date range (applies to JSON exports).
- `--batch-size` controls how many documents are submitted in each bulk request.
  The default is `10` and the maximum is `100`.
- `--source` sets the stable source namespace for remote file snapshot URLs.
- `--watch` performs an initial scan and continues importing new and changed snapshots until interrupted. `--skip-existing` applies only to the initial scan. Watch mode skips exports, 7z archives, and saved HTML with source URLs, and cannot be combined with date filters.
- `--allow-sensitive` skips sensitive content checks for imported snapshots.

> **Note:** `nogon import file` talks to a running Nogon server, so make sure the server
> is started before importing. See [Importing Documents](import) for file,
> browser history, browser bookmark, and reading service import instructions.

`nogon import file` creates searchable snapshots when the server cannot read files directly. Snapshot extraction occurs locally and only the prepared document is sent through the normal add API. It is not needed for normal local file tracking. Starting or restarting the server scans every configured directory and starts its file watcher automatically. With no paths, the command creates snapshots for every file matched by configured watched directories. Explicit directories are recursive.

```bash
nogon import file
nogon import file --source work-laptop ~/notes ~/Documents/report.pdf
nogon import file --watch --source work-laptop ~/notes
```

The command extracts the same PDF, DOCX, Markdown, Org mode, and plain text formats used by watched directories. It does not send the original bytes. Run the command again to replace a snapshot with the same source name and absolute path, or use `--watch` to update snapshots while the command runs. Watch mode retries temporary server failures, prints a combined summary on exit, and scans all inputs again on restart. Source removals never delete remote snapshots, including when `delete_on_remove` is configured. See [Importing Documents](import) for details.

### List Watched Files

```bash
nogon list-files
nogon list-files --relative
```

This command lists files matching the configured directory watch rules. It does not check file contents or report indexing status. The list includes files that indexing rejects because of sensitive content, size, or format checks.

Sensitive content rejections produce a warning in the server logs at the default log level, with the file path and no matched content. Rejected files remain watched and are checked again when they change.

### Importing Browser History and Bookmarks

Use the browser subcommands to import visit history or saved bookmarks into a running Nogon server:

```bash
# Import visit history from Firefox
nogon import browser history --browser firefox

# Import saved bookmarks from Firefox
nogon import browser bookmarks --browser firefox

# Import a specific Chromium bookmark store
nogon import browser bookmarks --db ~/.config/google-chrome/Default/Bookmarks
```

Omit `--browser` and `--db` to detect all supported browser profiles. The bare `nogon import browser` command also imports history. History documents use the `browser` label and bookmark documents use `bookmarks`; use `--label LABEL` to override either default.

Both importers fetch the current page contents and keep persistent crawl jobs that can resume after interruption. See [Importing Browser History](import#importing-browser-history) and [Importing Browser Bookmarks](import#importing-browser-bookmarks) for supported browsers and options.

## TUI (Terminal UI)

Nogon provides a terminal-based user interface for searching your browsing history without leaving your terminal.

### Start the TUI

Run the search command without any arguments:

```bash
nogon search
```

### TUI Keybindings

The TUI uses the following keybindings by default:

| Key          | Action          | Description                                          |
| ------------ | --------------- | ---------------------------------------------------- |
| `ctrl+c`     | quit            | Exit the TUI                                         |
| `f1`         | toggle_help     | Show/hide the complete keybindings help overlay      |
| `tab`, `esc` | toggle_focus    | Change focus or return to the previous workspace     |
| `up`, `k`    | scroll_up       | Navigate up                                          |
| `down`, `j`  | scroll_down     | Navigate down                                        |
| `enter`      | open_result     | Open, edit, or submit the focused item               |
| `y`          | copy_result     | Copy the selected URL using the terminal clipboard   |
| `v`          | toggle_preview  | Show or hide the readable preview pane               |
| `l`          | edit_label      | Edit the selected document's label                   |
| `ctrl+d`     | delete_result   | Delete the selected item                             |
| `ctrl+t`     | toggle_theme    | Open the interactive theme picker                    |
| `ctrl+s`     | toggle_settings | Open appearance and keybinding settings              |
| `ctrl+o`     | toggle_sort     | Toggle domain-based sorting for search results       |
| `ctrl+e`     | toggle_semantic | Toggle semantic search when enabled in server config |
| `alt+1`      | tab_search      | Switch to the Search tab                             |
| `alt+2`      | tab_history     | Switch to the History tab                            |
| `alt+3`      | tab_rules       | Switch to the Rules tab                              |
| `alt+4`      | tab_add         | Switch to the Add tab                                |

### Mouse Controls

Right clicking a result opens its context menu.

When a preview is open on a wide terminal, use `tab` to move focus between the
result list and the preview. Arrow keys or `j`/`k` then navigate the focused
side. Clicking or scrolling either side focuses it.

Search input and result navigation are separate focus modes. While the search
field is focused, printable keys edit the query and the footer only shows input
actions. Press `down` or `tab`, or scroll/click inside the results, to focus the
result list; result shortcuts such as `y` and `v` then become available.

### Customizing TUI

TUI settings are stored in a separate `tui.yaml` file alongside your main config file. This file is automatically created with default values when you first run `nogon search`.

On a fresh Linux setup the usual path is `~/.config/nogon/tui.yaml`. Existing
preferences in Hister locations are reused when no main config file is selected.
With a main config file, `tui.yaml` stays beside it. See
[TUI configuration](configuration#tui-configuration) for the search locations.

#### tui.yaml Structure

```yaml
# Theme settings
dark_theme: 'tokyonight'
light_theme: 'catppuccin-latte'
color_scheme: 'terminal'
# themes_dir: "/path/to/custom/themes"  # optional

# TUI keybindings
hotkeys:
  ctrl+c: 'quit'
  ctrl+t: 'toggle_theme'
  ctrl+s: 'toggle_settings'
  ctrl+o: 'toggle_sort'
  ctrl+e: 'toggle_semantic'
  y: 'copy_result'
  v: 'toggle_preview'
  l: 'edit_label'
  alt+1: 'tab_search'
  alt+2: 'tab_history'
  alt+3: 'tab_rules'
  alt+4: 'tab_add'
  # ... and all other TUI keybindings
```

`terminal` is the default appearance. It leaves your terminal background and
normal text color untouched and uses your terminal's ANSI palette for accents.
Use `auto` to select the configured dark or light Nogon theme based on the
terminal background, or choose `dark` or `light` explicitly. Press `ctrl+s`
and select **Appearance** to cycle these modes from Settings; `ctrl+t` opens the
full theme picker.

#### Available TUI Actions

- `quit` - Exit the TUI application
- `toggle_help` - Show/hide the help overlay
- `toggle_focus` - Change focus or return to the previous workspace
- `scroll_up`/`scroll_down` - Move selection up/down
- `open_result` - Open selected URL in browser
- `copy_result` - Copy the selected URL
- `toggle_preview` - Show/hide selected result details
- `edit_label` - Edit the selected document label
- `delete_result` - Delete selected entry from index
- `toggle_theme` - Open theme picker
- `toggle_settings` - Open appearance and keybinding settings
- `toggle_sort` - Toggle sorting mode
- `toggle_semantic` - Toggle semantic search
- `tab_search`/`tab_history`/`tab_rules`/`tab_add` - Switch tabs

Note: After modifying `tui.yaml`, restart the `nogon search` command to apply changes.
