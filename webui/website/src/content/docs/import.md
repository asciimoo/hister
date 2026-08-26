---
date: '2026-07-22T00:00:00+02:00'
draft: false
title: 'Importing Documents'
description: 'Import files, browser history, bookmarks, archives, and supported reading services into Hister.'
---

The `hister import` command collects related import tools under one command. Every import sends documents to a running Hister server.

> Hister v0.16.0 uses the standalone `hister import-browser` command. To import
> browser history with that release, replace `hister import browser` in the
> examples below with `hister import-browser`. The unified import command is
> available in rolling builds and stable releases newer than v0.16.0.

## Available Import Sources

| Command                                     | Source                                     | Default label             |
| ------------------------------------------- | ------------------------------------------ | ------------------------- |
| `hister import file [INPUT...]`             | Exports, saved pages, and file snapshots   | `import` or watched label |
| `hister import browser [BROWSER] [DB_PATH]` | Browser history databases                  | `browser`                 |
| `hister import linkding INSTANCE_URL`       | A Linkding instance through its HTTP API   | `linkding`                |
| `hister import linkwarden INSTANCE_URL`     | A Linkwarden instance through its HTTP API | `linkwarden`              |
| `hister import karakeep INSTANCE_URL`       | A Karakeep instance through its HTTP API   | `karakeep`                |
| `hister import readeck INSTANCE_URL`        | A Readeck instance through its HTTP API    | `readeck`                 |
| `hister import shaarli INSTANCE_URL`        | A Shaarli instance through its HTTP API    | `shaarli`                 |
| `hister import wallabag INSTANCE_URL`       | A wallabag instance through its HTTP API   | `wallabag`                |

Use the global `--server-url` and `--token` flags when the destination Hister server differs from your configured server or requires authentication.

Use `--label LABEL` with any import source to attach the same label to every imported document. Without this flag, labels stored in imported documents or resumed browser jobs are preserved. The default shown above is applied only when no label was supplied by the user or the imported document.

Hister also limits each batch according to the byte limit advertised by the destination server. Documents are serialized before batching so stored HTML and other large fields are measured accurately. If another HTTP server imposes a smaller limit, Hister splits a rejected batch and retries it automatically. A document that exceeds the limit by itself is reported with its URL, encoded size, and the server limit when known.

## Importing Files

Use `hister import file` with any number of files or directories:

> **Important:** You do not need this command to add or track files configured in
> `indexer.directories`. Restart the Hister server after configuring a directory. The
> server automatically scans it at startup and watches it for later changes.

```bash
hister import file export.json page.html ~/Downloads/saved-pages
```

Supported inputs are:

| Input                       | Behavior                                                                    |
| --------------------------- | --------------------------------------------------------------------------- |
| Hister JSON export          | Restores serialized documents without extracting their stored content again |
| 7z archive                  | Reads the first JSON export inside the archive                              |
| HTML or HTM page with a URL | Extracts the canonical, OpenGraph, or Twitter URL                           |
| Other local file            | Extracts supported content locally and creates a remote file snapshot       |
| Directory                   | Imports matching files recursively                                          |

PDF, DOCX, Markdown, Org mode, and valid UTF 8 text use the same handlers as watched files. JSON that does not have the Hister export array shape and HTML that has no source URL are also treated as file snapshots.

With no input paths, the command recursively creates remote file snapshots from every configured watched directory. It applies the same file type, pattern, exclusion, hidden path, size, and label settings. Use this mode only when the command line client can read those directories but the server cannot:

```bash
hister import file
```

You can also provide individual files or directories:

```bash
hister import file ~/notes ~/Documents/report.pdf
```

Snapshot extraction happens in the command line process. Only the extracted document fields are sent through `/api/add`. Hister does not send or retain the original file bytes, and it does not monitor snapshots for later changes. Running this command is therefore not a substitute for the automatic tracking provided by the server file watcher.

Remote file documents use a `remote-file://SOURCE/absolute/path` identity. The default source is the client hostname. Set a stable name when hostnames may change or when several clients have the same paths:

```bash
hister import file --source alice-laptop ~/notes
```

The following options apply to file imports:

| Flag                      | Purpose                                                     |
| ------------------------- | ----------------------------------------------------------- |
| `--skip-existing`         | Keep documents whose URL already exists in Hister           |
| `--label LABEL`           | Override stored labels and the default `import` label       |
| `--batch-size N`          | Submit at most 1 through 100 documents per request          |
| `--start-date YYYY-MM-DD` | Import documents added on or after the date                 |
| `--end-date YYYY-MM-DD`   | Import documents added on or before the date                |
| `--global`                | Import for all users when authenticated as an administrator |
| `--user-id ID`            | Import for one user when authenticated as an administrator  |
| `--source NAME`           | Set the source namespace used in document URLs              |
| `--allow-sensitive`       | Skip sensitive content checks                               |

Running the command again replaces documents with the same source and absolute path. File changes and removals are not synchronized automatically.

The destination account and the `--global` or `--user-id` flags determine ownership. The `user` value on a watched directory is not resolved by this client side import.

## Importing Browser History

Browser history contains URLs and visit information, but it does not contain the page contents. Hister reads the URLs from the browser database and fetches the pages before indexing them.

### Automatic Detection

Run the command without arguments to detect supported browser databases in their standard locations:

```bash
hister import browser
```

Automatic detection supports Firefox, Firefox Developer Edition, Zen, Waterfox, Chrome, Chromium, Brave, Vivaldi, Edge, Opera, and Ladybird.

The same history import is also available with named flags:

```bash
hister import browser history
hister import browser history --browser firefox
hister import browser history --db ~/.mozilla/firefox/example.default/places.sqlite
```

### Selecting a Browser or Database

You can provide a browser name, a database path, or both:

```bash
# Detect the Firefox database path
hister import browser firefox

# Detect the browser type from a database
hister import browser ~/.mozilla/firefox/example.default/places.sqlite

# Specify both values
hister import browser firefox ~/.mozilla/firefox/example.default/places.sqlite
```

Firefox stores history in `places.sqlite` inside its profile directory. Chromium based browsers usually store it in a file named `History` inside their profile directory.

Use `--min-visit N` to import only URLs that have at least `N` recorded visits.
Use `--start-date YYYY-MM-DD` to import only URLs whose most recent recorded
visit is on or after that date:

```bash
hister import browser --start-date 2025-01-01
```

The date filter uses timestamps from the browser database. The indexed
document timestamp still describes when Hister fetched the page.

Browser history documents receive the `browser` label by default. Use `--label LABEL` to replace it. Resumed browser import jobs reuse their stored label unless this flag is supplied again.


## Importing Browser Bookmarks

Firefox stores bookmarks in the same `places.sqlite` file as history. Bookmark import reads only bookmark rows, then fetches the pages through the same crawl job used by history import.

```bash
hister import browser bookmarks
hister import browser bookmarks --browser firefox
hister import browser bookmarks --db ~/.mozilla/firefox/example.default/places.sqlite
```

Automatic detection covers Firefox, Firefox Developer Edition, Zen, and Waterfox. Chromium bookmark files are not imported by this command.

Skip rules apply the same way they do for history import. A browser's shipped default bookmarks are not filtered out unless they match a skip rule.

Bookmark documents receive the `bookmarks` label by default. Use `--label LABEL` to replace it. `--start-date` is not supported, because it would drop bookmarks that have never been visited.

Bookmark imports use persistent crawl jobs named `bookmark-import-YYYY-MM-DD`.

### Resume and Inspect a Browser Import

Browser imports use persistent crawl jobs named `browser-import-YYYY-MM-DD`. It is safe to interrupt the process and continue it later. Completed URLs remain completed, pending URLs resume, and failed URLs remain recorded for inspection.

```bash
hister crawl list
hister crawl show browser-import-YYYY-MM-DD
hister crawl errors browser-import-YYYY-MM-DD
hister crawl urls browser-import-YYYY-MM-DD
```

Add `--count` to `hister crawl urls` when only the number of tracked URLs is needed. Use
`--status pending`, `--status failed`, `--status done`, or `--status skipped` to filter the rows.

### Browser Import Backends

The default HTTP backend is fast, but it cannot execute JavaScript. Select a browser based backend when a site requires client side rendering:

```bash
hister import browser --backend chromedp
```

The supported backends are `http`, `chromedp`, and `bidi`. Backend options, request headers, and cookies can be supplied when necessary:

```bash
hister import browser \
  --backend chromedp \
  --backend-option exec_path=/usr/bin/chromium \
  --proxy http://127.0.0.1:8080 \
  --header "Accept-Language=en" \
  --cookie "session=abc; Domain=example.com"
```

The `--backend-option`, `--header`, and `--cookie` flags can be repeated. Use `--proxy` with an `http://` or `socks5://` URL. Cookies use `Set-Cookie` syntax and require a `Domain` attribute. See [Website Crawler](crawler) for all crawler settings and backend limitations.

Automated requests can be rejected by bot protection, expired sessions, removed pages, or network failures. Failed URLs remain visible through `hister crawl errors`, but resuming the job does not retry them. Export those URLs into a new URL input job when you want to retry them. See [Retrying Failed URLs](crawler#retrying-failed-urls).

## Importing from Linkding

Copy the API token from the Linkding settings page, then store it in the environment before running the import:

```bash
export HISTER_IMPORT_LINKDING_TOKEN='your-linkding-token'
hister import linkding https://linkding.example.com
```

You can use `--api-token` as a temporary override. The Linkding API token is separate from the global `--token` flag, which authenticates with the destination Hister server. Prefer the environment variable so the Linkding token does not appear in shell history or process listings.

### Incremental Linkding Imports

Every imported Linkding document receives `source: linkding` metadata. Hister searches for `metadata.source:linkding` and reads the newest imported document timestamp before calling Linkding. If a previous import exists, the importer supplies that timestamp through the `modified_since` filter for both active and archived bookmarks. Otherwise, it requests every bookmark.

Deleted Linkding bookmarks are not removed from Hister during an incremental import.

### Linkding Data Mapping

| Linkding value                                     | Hister value            |
| -------------------------------------------------- | ----------------------- |
| URL                                                | Normalized document URL |
| Title                                              | Title                   |
| Description, notes, and downloaded page content    | Searchable text         |
| Added date                                         | Added timestamp         |
| Modification date                                  | Updated timestamp       |
| Favicon                                            | Document favicon        |
| Tags, archive state, unread state, sharing, and ID | Document metadata       |
| Archive snapshot and preview image URLs            | Document metadata       |

Records without a URL are skipped because every Hister document requires a URL. Active and archived pagination and batch submission are automatic.

Linkding stores bookmark metadata rather than complete copies of linked pages. Hister therefore downloads every bookmarked page using the configured crawler backend and combines its extracted content with the stored description and notes.

Consult the [Linkding API documentation](https://linkding.link/api/) when troubleshooting API access.

## Importing from Linkwarden

Create an API token in Linkwarden, then store it in the environment before running the import:

```bash
export HISTER_IMPORT_LINKWARDEN_TOKEN='your-linkwarden-token'
hister import linkwarden https://links.example.com
```

You can use `--api-token` as a temporary override. The Linkwarden API token is separate from the global `--token` flag, which authenticates with the destination Hister server. Prefer the environment variable so the Linkwarden token does not appear in shell history or process listings.

### Incremental Linkwarden Imports

Every imported Linkwarden document receives `source: linkwarden` metadata. Before requesting Linkwarden records, Hister searches for `metadata.source:linkwarden`, sorts the results by update date, and reads the newest document's `updated` timestamp.

When a previous result exists, the importer adds an `after:` filter to the Linkwarden search request so only newer records are fetched. When no previous result exists, it performs a complete import. Repeating the command therefore continues from the most recent Linkwarden import automatically.

### Linkwarden Data Mapping

| Linkwarden value                       | Hister value            |
| -------------------------------------- | ----------------------- |
| URL                                    | Normalized document URL |
| Name                                   | Title                   |
| Description and extracted text content | Searchable text         |
| Import date, then creation date        | Added timestamp         |
| Update date                            | Updated timestamp       |
| Tags, collection, source type, and ID  | Document metadata       |

Records without a URL are skipped because every Hister document requires a URL. Pagination and batch submission are automatic.

If a Linkwarden URL record has no extracted text content, Hister downloads the page and extracts its contents before importing it. The configured crawler backend is used for these downloads. The crawler is initialized only when missing content is encountered and is reused for the rest of the import.

## Importing from Karakeep

Karakeep was formerly named Hoarder. Create an API key in Karakeep, then store it in the environment before running the import:

```bash
export HISTER_IMPORT_KARAKEEP_TOKEN='your-karakeep-token'
hister import karakeep https://karakeep.example.com
```

You can use `--api-token` as a temporary override. The Karakeep API token is separate from the global `--token` flag used for the destination Hister server.

### Incremental Karakeep Imports

Every imported Karakeep document receives `source: karakeep` metadata. Hister searches for `metadata.source:karakeep` and reads the newest imported document timestamp before calling Karakeep. If a previous import exists, the importer uses Karakeep search with an `after:` date. Otherwise, it requests every bookmark.

Karakeep applies the `after:` search qualifier to the bookmark creation date. A bookmark created before the checkpoint and edited afterward might therefore require a complete import to refresh it.

### Karakeep Data Mapping

| Karakeep value                             | Hister value            |
| ------------------------------------------ | ----------------------- |
| Link URL or source URL                     | Normalized document URL |
| Bookmark title, content title, or filename | Title                   |
| Note, summary, description, and content    | Searchable text         |
| Creation date                              | Added timestamp         |
| Modification date                          | Updated timestamp       |
| Stored favicon                             | Document favicon        |
| Tags, statuses, source, assets, and ID     | Document metadata       |

For link bookmarks, Hister extracts the stored Karakeep HTML when it is available. If stored HTML is absent or cannot be extracted, Hister downloads the page with the selected crawler backend. Text and asset bookmarks use their stored content when they have a source URL.

Text and asset bookmarks without a source URL are skipped because every Hister document requires a URL. Pagination and batch submission are automatic.

Consult the [Karakeep API documentation](https://docs.karakeep.app/api) when troubleshooting API access.

## Importing from Readeck

Create an API token in the Readeck profile settings, then store it in the environment before running the import:

```bash
export HISTER_IMPORT_READECK_TOKEN='your-readeck-token'
hister import readeck https://readeck.example.com
```

You can use `--api-token` as a temporary override. The Readeck API token is separate from the global `--token` flag used for the destination Hister server. Prefer the environment variable so the source token does not appear in shell history or process listings.

### Incremental Readeck Imports

Every imported Readeck document receives `source: readeck` metadata. Hister searches for `metadata.source:readeck` and reads the newest imported document timestamp before calling Readeck. If a previous import exists, Hister requests sync events from that timestamp. Otherwise, it requests every current bookmark.

Repeated update events are combined before content is requested. Deletion events are ignored because an import does not remove existing Hister documents.

### Readeck Data Mapping

| Readeck value                                               | Hister value            |
| ----------------------------------------------------------- | ----------------------- |
| URL                                                         | Normalized document URL |
| Title                                                       | Title                   |
| Description, authors, and stored or downloaded page content | Searchable text         |
| Creation date                                               | Added timestamp         |
| Update date                                                 | Updated timestamp       |
| Stored site icon                                            | Document favicon        |
| Labels, authors, type, status, reading progress, and ID     | Document metadata       |
| Site, publication, image, thumbnail, and resource URLs      | Document metadata       |

Hister requests bookmark metadata and stored article HTML through the Readeck sync API. Readeck returns article bodies as HTML fragments, so Hister wraps each fragment as a complete HTML document before extraction and storage. If stored HTML is absent or cannot be extracted, Hister downloads the original URL with the selected crawler backend.

Article images keep absolute Readeck resource URLs. Searchable text and HTML remain stored in Hister, but those images require the Readeck instance to remain available. Sync requests and Hister batch submissions use the selected `--batch-size`.

Consult the API reference on the `/api` route of your Readeck instance or the [Readeck documentation](https://readeck.org/en/docs/) when troubleshooting API access.

## Importing from Shaarli

Copy the API secret from the Shaarli administration page, then store it in the environment before running the import:

```bash
export HISTER_IMPORT_SHAARLI_SECRET='your-shaarli-api-secret'
hister import shaarli https://shaarli.example.com
```

You can use `--api-token` as a temporary override. For Shaarli, this option accepts the API secret from the administration page. Hister uses the secret to generate a short lived HS512 JWT for each API request. The secret itself is not sent to Shaarli. The global `--token` flag remains the access token for the destination Hister server.

### Incremental Shaarli Imports

Every imported Shaarli document receives `source: shaarli` metadata. Hister searches for `metadata.source:shaarli` and reads the newest imported document timestamp before calling Shaarli.

If a previous import exists, Hister requests Shaarli history since that timestamp and retrieves the current value of every created or updated Shaare. Repeated events for the same Shaare are combined. Deleted Shaares are ignored because an import does not remove existing Hister documents. Without a previous result, Hister requests every Shaare.

### Shaarli Data Mapping

| Shaarli value                                 | Hister value            |
| --------------------------------------------- | ----------------------- |
| Link URL or text note permalink               | Normalized document URL |
| Title                                         | Title                   |
| Description and downloaded page content       | Searchable text         |
| Creation date                                 | Added timestamp         |
| Update date                                   | Updated timestamp       |
| Tags, privacy, short URL, note status, and ID | Document metadata       |

Shaarli stores bookmark descriptions rather than complete copies of linked pages. Hister therefore downloads every ordinary bookmark with the configured crawler backend and combines its extracted content with the stored description. Text notes use their description directly and keep a stable Shaarli permalink, so they are not downloaded.

Pagination and batch submission are automatic. Consult the [Shaarli API documentation](https://shaarli.github.io/api-documentation/) and [Shaarli REST API authentication guide](https://shaarli.readthedocs.io/en/master/REST-API.html) when troubleshooting API access.

## Importing from wallabag

Obtain an OAuth access token from wallabag, then store it in the environment before running the import:

```bash
export HISTER_IMPORT_WALLABAG_TOKEN='your-wallabag-access-token'
hister import wallabag https://wallabag.example.com
```

You can use `--api-token` as a temporary override. The wallabag access token is separate from the global `--token` flag, which authenticates with the destination Hister server. Prefer the environment variable so the source token does not appear in shell history or process listings.

### Incremental wallabag Imports

Every imported wallabag document receives `source: wallabag` metadata. Hister searches for `metadata.source:wallabag` and reads the newest imported document timestamp before calling wallabag. If a previous import exists, Hister supplies that timestamp through the wallabag `since` filter and requests entries in ascending update order. Otherwise, it requests every entry.

Deleted wallabag entries are not removed from Hister during an incremental import.

### wallabag Data Mapping

| wallabag value                                            | Hister value            |
| --------------------------------------------------------- | ----------------------- |
| URL                                                       | Normalized document URL |
| Title                                                     | Title                   |
| Stored article HTML, or downloaded page content           | Searchable text         |
| Creation date                                             | Added timestamp         |
| Update date                                               | Updated timestamp       |
| Tags, authors, status values, reading time, and source ID | Document metadata       |

Hister extracts the article HTML already stored by wallabag and preserves it for offline previews. If the stored content is empty or cannot be extracted, Hister downloads the original URL with the selected crawler backend. Pagination and batch submission are automatic.

Consult the [wallabag OAuth documentation](https://doc.wallabag.org/developer/api/oauth/) and [wallabag API methods](https://doc.wallabag.org/developer/api/methods/) when troubleshooting API access.

## Service Import Options

The following options apply to Linkding, Linkwarden, Karakeep, Readeck, Shaarli, and wallabag imports:

Service imports preserve favicon data supplied by the source. When it is absent, Hister tries the favicon URL discovered while extracting the linked page, or the conventional `/favicon.ico` URL when no page icon is available. A favicon download failure does not stop the import.

| Flag                         | Purpose                                                      |
| ---------------------------- | ------------------------------------------------------------ |
| `--api-token TOKEN`          | Override the source service credential for this invocation   |
| `--backend BACKEND`          | Download missing content with `http`, `chromedp`, or `bidi`  |
| `--backend-option KEY=VALUE` | Set an option for the selected crawler backend               |
| `--proxy URL`                | Route content downloads through an HTTP or SOCKS5 proxy      |
| `--header KEY=VALUE`         | Add a request header when downloading missing content        |
| `--cookie COOKIE`            | Add a cookie when downloading missing content                |
| `--skip-existing`            | Keep documents whose normalized URL already exists in Hister |
| `--label LABEL`              | Override the default service source label                    |
| `--batch-size N`             | Submit from 1 through 100 documents per request              |
| `--start-date YYYY-MM-DD`    | Import documents added on or after the date                  |
| `--end-date YYYY-MM-DD`      | Import documents added on or before the date                 |
| `--global`                   | Import for all users when authenticated as an administrator  |
| `--user-id ID`               | Import for one user when authenticated as an administrator   |

For example:

```bash
hister import linkwarden https://links.example.com \
	--backend chromedp \
	--backend-option exec_path=/usr/bin/chromium \
	--skip-existing \
  --start-date 2024-01-01 \
  --batch-size 25
```

Linkwarden provides `/api/v1/search` and Bearer token authentication. Consult the [Linkwarden API documentation](https://docs.linkwarden.app/api/search-links) when troubleshooting API access.
