---
date: '2026-08-22T00:00:00+02:00'
draft: false
title: 'Developer Documentation'
description: 'Understand the source tree, test Hister, extend extractors, and inspect its HTTP and MCP interfaces.'
---

This page is for people who contribute changes, add extractors, or integrate
directly with Hister HTTP and MCP interfaces.

The primary source repository is on
[GitHub](https://github.com/asciimoo/hister). It is also mirrored on
[Codeberg](https://codeberg.org/asciimoo/hister).

## Contributing to Hister

Read the complete
[contribution guide](https://github.com/asciimoo/hister/blob/main/CONTRIBUTING.md)
before starting work. Keep each pull request focused on one concern, explain
why the change is needed, and make sure relevant tests and checks pass. Open an
issue before implementing a large architectural change, broad refactor, or
development tooling change so the approach can be discussed first.

Ask questions when requirements or existing behavior are unclear. Review
feedback concerns the work and the shared goal of improving Hister. A useful
contribution should provide more value to the project than the time required to
review and maintain it.

### Project Standards

**Go code.** Follow the GolangCI Lint v2 configuration in `.golangci.toml`.
Formatting uses `goimports` and `gofumpt`. Run focused package tests during
development and `go test ./...` before submitting a broad change.

**Frontend code.** Prettier uses single quotes, trailing commas, a print width
of 100, and two space indentation. Prefer standard Tailwind scale classes.
Check the shared component library before creating a new component.

**Change scope.** Preserve compatibility unless the change explicitly requires
otherwise. Update tests and user documentation whenever behavior, command
flags, configuration, query syntax, or public interfaces change. Keep release
notes for the release process.

**Licensing.** Contributions are accepted under the project
[AGPLv3 license](https://github.com/asciimoo/hister/blob/main/LICENSE).

**AI use.** The contribution guide requires disclosure of every AI tool and the
extent of its use. The human contributor must understand the complete change
and remain its main author. Issue descriptions, pull request descriptions, and
discussion comments must be written by a human. AI tools must not be used for
good first issues. Coding agents should also follow the repository
[agent reference](https://github.com/asciimoo/hister/blob/main/AGENTS.md).

### Useful Development Links

| Resource                                                                                                           | Purpose                                                        |
| ------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------- |
| [Contribution guide](https://github.com/asciimoo/hister/blob/main/CONTRIBUTING.md)                                 | Submission standards, code style, and AI policy                |
| [`manage.sh`](https://github.com/asciimoo/hister/blob/main/manage.sh)                                              | Common dependency, test, build, and artifact commands          |
| [Go module](https://github.com/asciimoo/hister/blob/main/go.mod)                                                   | Go version, module path, and backend dependencies              |
| [Go lint configuration](https://github.com/asciimoo/hister/blob/main/.golangci.toml)                               | Enabled Go formatters and linters                              |
| [Frontend workspace](https://github.com/asciimoo/hister/blob/main/package.json)                                    | npm workspaces, formatting, and frontend checks                |
| [Frontend notes](https://github.com/asciimoo/hister/blob/main/webui/README.md)                                     | Web project structure, builds, previews, and shared components |
| [`webui/build.sh`](https://github.com/asciimoo/hister/blob/main/webui/build.sh)                                    | Embedded web application build process                         |
| [API route registry](https://github.com/asciimoo/hister/blob/main/server/api.go)                                   | HTTP endpoint declarations and generated API documentation     |
| [Extractor template](https://github.com/asciimoo/hister/tree/main/server/extractor/extractors/_extractor_template) | Starting point for a new extractor                             |
| [Issue tracker](https://github.com/asciimoo/hister/issues)                                                         | Bug reports, proposals, and feature discussions                |
| [Security policy](https://github.com/asciimoo/hister/blob/main/SECURITY.md)                                        | Private security reporting guidance                            |

## Source Layout

| Path                | Purpose                                                                 |
| ------------------- | ----------------------------------------------------------------------- |
| `hister.go`         | Program entry point                                                     |
| `cmd/`              | Command implementations and the terminal interface                      |
| `client/`           | HTTP client used by commands                                            |
| `config/`           | Configuration loading and validation                                    |
| `files/`            | Watched directory matching and file ownership                           |
| `files/extract/`    | Parsing supplied file contents without document or index state          |
| `server/`           | HTTP server, models, indexing, crawling, extraction, and vector storage |
| `webui/app/`        | Web application embedded in the Go binary                               |
| `webui/components/` | Shared Svelte components                                                |
| `webui/ext/`        | Browser extension                                                       |
| `webui/website/`    | Public website and documentation                                        |
| `scripts/`          | Dataset utilities                                                       |
| `contrib/`          | Service integration files                                               |

The Go module is `github.com/asciimoo/hister`. The root npm workspace includes
the web application, website, shared components, and browser extension.

### Configuration loading

`config.ResolvePath` selects a main file. Pass the selected path to `config.Load`, or an empty
string to start with defaults and environment settings. `LoadForInspection` and `Config.Reload`
share candidate decoding without creating runtime files. Reload retains the original main file
selection, independently of the TUI preferences path.

`Config.Source(path)` reports a setting's origin and source lock without retaining its value.
Explicit empty and default valued settings remain distinguishable from omitted settings.
Collections share one source lock; extractor options are tracked individually. CLI overrides
use `config.Override` entries containing a path, typed value, and source. `Load`,
`LoadForInspection`, and `Config.Reload` accept these entries after environment settings.
Loading applies values and provenance together and copies mutable inputs for each candidate.
Bind a CLI flag to its configuration path with `bindConfigFlag` beside its registration.
Only explicitly supplied flags become overrides. This metadata does not grant editing
permission or replace field validation.

### Configuration editing API

`GET /api/settings` returns redacted field state and an opaque revision.
Field metadata includes a top level `category` and an optional display `group`.
The backend catalog defines groups and field order; clients preserve that order
when grouping fields within each category.
`POST /api/settings/validate` checks a draft without side effects.
`POST /api/settings/apply` saves and applies ordinary App edits. Both POST
operations accept `revision` and an `edits` array with explicit `path`, `action`
(`set` or `reset`), and a typed `value` for `set`. Authentication settings and
other unsupported fields remain read only. These routes use administrative
authorization, CSRF protection for writes, and `Cache-Control: no-store`.

The runtime serializes saves with reloads, drains requests, stages private YAML
beside `config.web.yml`, and commits it before publishing the replacement runtime.
Saves with unchanged web selections preserve the file, runtime, and revision.
Adding or removing an explicit selection is saved even when its value equals the default.
The main file is never rewritten. Conflicts return `409`; field errors return
`422`. Read settings again after success or a lost response. Persistence or
activation failures restore the previous configuration when possible; failed
recovery returns `503` and requires restarting the server. Rollback does not undo
indexed documents or other background work.

Runtime and persistence failures include a `code` and a safe `error` message:

| Code                 | HTTP status | Meaning                                                              |
| -------------------- | ----------- | -------------------------------------------------------------------- |
| `sources_changed`    | `409`       | Sources changed, including during activation or persistence.         |
| `admin_required`     | `403`       | The saving user lost administrator access while requests drained.    |
| `persistence_failed` | `500`       | Staging or commit failed; the previous configuration remains active. |
| `recovery_failed`    | `503`       | Recovery failed; the server requires manual recovery and restart.    |

Other activation failures use `apply_failed`. An outdated editor revision uses
`stale_revision`, separately from source changes.

Startup discards abandoned staging files. If restoring the managed file fails,
inspect `config.web.yml` and the private `.config.web-recovery-*` copies before
restarting.

Administrator availability is checked against the selected database before
startup and activation, outside pure configuration loading. OIDC role management
also requires an independent local recovery administrator. Account mutations
protect that account under the existing database write lock, while provider
revocations remain authoritative.

## Development Commands

Install the frontend dependencies once before running frontend commands:

```bash
./manage.sh install_js_deps
```

Use these commands for common work:

| Goal                                  | Command                               |
| ------------------------------------- | ------------------------------------- |
| Build the Go program                  | `go build`                            |
| Run Go tests                          | `go test ./...`                       |
| Run app browser tests                 | `npm run test:browser -w @hister/app` |
| Run browser extension tests           | `npm test -w @hister/ext`             |
| Rebuild embedded frontend assets      | `go generate`                         |
| Build the complete Hister binary      | `./manage.sh build`                   |
| Build the web application             | `npm run build -w @hister/app`        |
| Build this documentation website      | `npm run build -w @hister/website`    |
| Build the browser extension           | `npm run build -w @hister/ext`        |
| Run the app development servers       | `npm run serve:app`                   |
| Format and lint Go code               | `golangci-lint run --fix ./...`       |
| Check frontend formatting and linting | `npm run check`                       |
| Apply frontend formatting             | `npm run format`                      |

`go generate` runs `webui/build.sh`. That script builds `@hister/app`, replaces
`server/static/app`, and copies the new frontend build into the embedded asset
directory. Run it before building the Go binary whenever embedded frontend code
changes.

`npm run serve:app` starts the web application with hot reload and the Go
server with automatic rebuilds. It installs `air` when that tool is not already
available.

Use focused tests while developing. Run the complete Go suite before handing
off a broad or high risk change.

The app browser tests use Chromium with desktop and mobile layouts. Install the
browser before the first run:

```bash
npm exec -w @hister/app -- playwright install --with-deps chromium
```

Playwright starts the app development server and the tests mock the HTTP API and
search WebSocket, so these tests do not require a running Go server. Set
`PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` to use an existing Chromium installation.

Pull requests run app browser tests and extension tests in separate jobs when
their workspace, shared components, root npm dependencies, or the test workflow
changes. CI rejects focused browser tests such as `test.only` and runs one browser
worker. Failed browser runs upload an `app-browser-failures` artifact containing
the HTML report, traces, and screenshots, retained for seven days.

## ZIM Import Development

`cmd/zim.go` implements `hister import zim` with the pure Go [stazelabs/gozim](https://github.com/stazelabs/gozim) reader, pinned in `go.mod`. It supports uncompressed, XZ, and Zstd content without an external executable. The importer checks header and pointer table bounds before opening the archive, disables memory mapping, and caches one cluster. It reads selected metadata and uses error returning directory lookups so damaged entries cannot silently disappear.

The reader decompresses complete clusters before checking individual article sizes. Its limits are 4 GiB of encoded cluster data and 1 GiB of decoded compressed data; the article limit is not a total memory limit. This is a newer library than libzim, so verify real archives and malformed input tests before changing the pin.

URL discovery reads canonical, Open Graph, and Twitter metadata, or absolute entry URLs. Relative metadata uses the HTML base or another known page URL; `--base-url` provides an optional fallback. Entries without a URL produce a warning and increment the skipped count. Collection names are not used to infer origins.

The importer makes two passes. The first retains entry indices, source URLs, and redirect targets; the second prepares documents and rewrites relative article links through that lookup. This handles forward links and archive paths that differ from website paths without retaining all HTML in memory. Redirect traversal is bounded, unresolved links are disabled, and page URL metadata remains absolute after removing HTML base elements. Normal authenticated submission and server extraction handle the prepared documents. Importer metadata retains only `source` and a nonempty `zim_collection`. The archive publication date is stored in `Added`; entry paths remain local to link resolution and warnings.

`go test ./cmd -run TestZIM` always runs integration tests against the checked in archives in `cmd/testdata/zim`. These fixtures were generated from the test articles with libzim 3.13.1. The legacy fixture converts the article namespace and cluster compression to XZ and was verified with libzim. Tests also cover operation with an empty executable path and malformed file offsets. Set `HISTER_ZIM_LEGACY_FIXTURE` to `data/withns/small.zim` from the [official ZIM testing suite](https://github.com/openzim/zim-testing-suite) for an additional compatibility check.

## HTTP API Development

The running server publishes its generated API documentation at `/api`. API
routes are declared as `Endpoint` values in `server/api.go`. Add new API routes
there so routing, authorization, CSRF handling, and API documentation remain in
sync.

Authenticated clients can send either of these headers:

```http
Authorization: Bearer <access-token>
```

```http
X-Access-Token: <access-token>
```

### Browser Session Authentication

User sessions store the account's `user_id` and `user_session_version` at login.
Password and OAuth logins both copy the version from the authenticated user
record. Each request checks that the stored version matches the account's current
`SessionVersion`. Sessions contain no password or password hash.

`model.UpdatePassword` saves the password hash and increments `session_version`
in one database update. A failed update leaves both unchanged. Any code that
updates account passwords must preserve this atomic invalidation. A session
refresh saves the original version, so an in progress refresh cannot restore
access after a password reset. Personal access tokens are independent of the
session version.

The `session_version` column defaults to zero and is added to existing accounts
by AutoMigrate. Sessions created by earlier versions lack `user_session_version`
and are accepted only while the account's version is zero. An upgrade preserves
those sessions; a subsequent password reset invalidates them. Invalidated session
records remain until the normal expiration cleanup removes them.

### User Administration Reads

The user administration endpoints require `app.user_handling: true` on the server
and an administrator's personal token or authenticated session. Public mode does
not grant access to these endpoints.

| Endpoint                    | Response                                                |
| --------------------------- | ------------------------------------------------------- |
| `GET /api/users`            | A `users` array and an optional `next_after_id` cursor  |
| `GET /api/users/{id}`       | Metadata for one account                                |
| `GET /api/users/{id}/token` | The account's current access token as `{"token":"..."}` |

Account metadata contains only `id`, `username`, `is_admin`, `created_at`,
`updated_at`, and `revision`. Passwords, password hashes, tokens, rules, and OAuth identities are
excluded. Timestamps use RFC 3339 format. The token endpoint reads the existing
token without rotating it. Successful responses from all three endpoints use
`Cache-Control: no-store`.

Listing accepts these optional query parameters:

| Parameter  | Meaning                                                                 |
| ---------- | ----------------------------------------------------------------------- |
| `username` | Exact, case sensitive username. Wildcards are treated literally.        |
| `limit`    | Page size from 1 to 100, with a default of 50                           |
| `after_id` | Return users whose IDs are greater than this value, with a default of 0 |

Results are ordered by ascending user ID. Pass `next_after_id` as the next request's
`after_id`, keeping the same filter. The final page omits `next_after_id`, and an
empty result uses `"users": []`. Pagination does not freeze the user database;
accounts created or changed between requests can affect subsequent pages.

```http
GET /api/users?username=alice&limit=1
Authorization: Bearer <administrator-token>
```

User IDs in paths must be positive integers. Unknown, repeated, or malformed list
parameters are rejected. Handler errors use a JSON `code` and readable `error`:

```json
{ "code": "user_not_found", "error": "user not found" }
```

| Status | Code                     | Meaning                                  |
| ------ | ------------------------ | ---------------------------------------- |
| `400`  | `invalid_request`        | Invalid ID or query parameters           |
| `400`  | `user_handling_disabled` | User handling is disabled on this server |
| `404`  | `user_not_found`         | No account exists for the requested ID   |
| `500`  | `internal_error`         | The account lookup failed                |

Authentication failures use the existing `403` response from the authorization
middleware. A username filter with no match returns an empty list, not `404`.
Database error details are logged on the server and are not included in responses.

### User Creation

`POST /api/users` creates an account on the running server. It requires
`app.user_handling: true` and an existing administrator's token or session, even
in public mode. It cannot create the first administrator without authentication.

```http
POST /api/users
Authorization: Bearer <administrator-token>
Origin: hister://
Content-Type: application/json

{"username":"alice","password":"example-password","is_admin":false}
```

The body must be a single JSON object. `username` and `password` are required
strings. `is_admin` is an optional boolean that defaults to `false`. Unknown
fields, null values, and trailing JSON are rejected.

Usernames are case sensitive and stored unchanged. They may include Unicode and
spaces, but must not be blank or contain control characters. Passwords must
contain 8 to 72 UTF-8 bytes; a Unicode character may occupy multiple bytes.
Passwords are hashed before storage. Creating an account does not enable password
login when `server.oauth_only` is set.

Success returns `201 Created` with the same account metadata as
`GET /api/users/{id}` and `Cache-Control: no-store`. The `Location` header points
to that account's URL, including any configured base path. The response does not
include the password or access token. An administrator can explicitly retrieve
the generated token with `GET /api/users/{id}/token`.

Command line clients use `Origin: hister://` with their administrator token, as
shown above. Browser session requests follow the existing CSRF checks. Requests
from a different origin must supply the session's `X-CSRF-Token`, available from
`GET /api/config`.

Errors use the same JSON `code` and `error` fields as the account read endpoints:

| Status | Code                     | Meaning                                                       |
| ------ | ------------------------ | ------------------------------------------------------------- |
| `400`  | `invalid_request`        | Invalid body, username, password, or admin flag               |
| `400`  | `user_handling_disabled` | User handling is disabled on this server                      |
| `409`  | `username_exists`        | The username is taken, including concurrent creation attempts |
| `413`  | `request_too_large`      | The request exceeds the 1 MiB body limit                      |
| `500`  | `internal_error`         | The account could not be saved                                |

Authentication and CSRF failures use the existing `403` responses. A conflicting
creation request leaves the existing account's credentials and privileges
unchanged.

### User Updates

`PATCH /api/users/{id}` applies account changes in one transaction. It requires
user handling, an authenticated administrator, and the same CSRF protection as
account creation.

Read the account first, then send its current `revision` with at least one change:

```http
PATCH /api/users/2
Authorization: Bearer <administrator-token>
Origin: hister://
Content-Type: application/json

{"revision":1,"username":"alice-renamed","password":"replacement-password","is_admin":false,"regenerate_token":true}
```

`username`, `password`, and `is_admin` are optional. Omitted fields keep their
values. An explicit `false` removes administrator privileges. Username and
password validation matches account creation. `regenerate_token: true` replaces
the access token; `false` alone does not count as a change. Null values, unknown
or repeated fields, and trailing JSON are rejected. The body limit is 1 MiB.

Success returns `200 OK` with the updated account metadata and
`Cache-Control: no-store`. Only when `regenerate_token` is `true` does the response
also contain `token`, holding the new access token. A client rotating its own
token must retain that replacement. Ordinary updates never return credentials.

Account revisions start at `1` and increase when a username, password,
administrator status, or token is updated, including through existing account
commands and profile token rotation. Rule changes do not change this revision.
A stale revision returns `409` without applying any part of the request. Fetch
the latest account, review the changes, and submit a new request with its current
revision. Do not retry an update blindly with a new revision.

Password changes invalidate the account's existing browser sessions, including
OAuth sessions, while retaining its token unless rotation is also requested.
Setting a password does not bypass `server.oauth_only`. The last administrator
cannot be demoted or deleted.

Handler errors use the JSON `code` and `error` fields:

| Status | Code                     | Meaning                                                    |
| ------ | ------------------------ | ---------------------------------------------------------- |
| `400`  | `invalid_request`        | Invalid ID, revision, body, or field value                 |
| `400`  | `user_handling_disabled` | User handling is disabled                                  |
| `403`  | `admin_required`         | The caller lost administrator privileges before the update |
| `404`  | `user_not_found`         | The target account does not exist                          |
| `409`  | `stale_revision`         | The account changed since it was read                      |
| `409`  | `username_exists`        | Another account already uses the requested username        |
| `409`  | `last_admin`             | The change would remove the last administrator             |
| `409`  | `last_recovery_admin`    | OIDC role management requires this local recovery account  |
| `413`  | `request_too_large`      | The request exceeds the body limit                         |
| `500`  | `internal_error`         | The update could not be saved                              |

Authorization failures before the handler use the existing middleware's `403`
response. Validation failures and transaction failures leave all account fields
unchanged.

### User Token Rotation

`POST /api/users/{id}/token` lets account owners and administrators replace an
access token. It requires user handling and the same CSRF protection as account
updates. Get your `user_id` and `revision` from `GET /api/profile`; administrators
can read another account's revision through `GET /api/users/{id}`.

```http
POST /api/users/2/token
Authorization: Bearer <current-user-token>
Origin: hister://
Content-Type: application/json

{"revision":1}
```

Send only the current `revision`. Success returns `200 OK` with
`Cache-Control: no-store`:

```json
{ "token": "<new-access-token>", "revision": 2 }
```

Save the returned token: the old token stops working immediately. Browser
sessions remain valid. A stale revision returns `409` with code `stale_revision`
without changing the token. Refresh the account before retrying explicitly.

### User Deletion

`DELETE /api/users/{id}` deletes an account. It requires user handling, an
authenticated administrator, and the same CSRF protection as account updates.
Set `?purge=true` to remove the account's indexed documents first; the default
is `false`.

Success returns `200 OK` with `{"id":2,"username":"alice","deleted_documents":3}`.
An unknown ID returns `404`. Conflicts return `409` with code `purge_required`
when documents remain or `last_admin` when deleting the final administrator.

Purged documents are not restored if account deletion fails. See
[Data Lifecycle and Retention](data-lifecycle#multi-user-ownership) for retention limits.

### Retained Data Purge

`POST /api/purge` requires administrator access in multi user mode and the normal
authentication and CSRF checks. The selection applies across all users. Supply
an exclusive `before` cutoff as an RFC3339 timestamp in the past and at least one
of `versions`, `history`, or `crawl_jobs`:

```json
{
  "before": "2026-01-01T00:00:00Z",
  "versions": true,
  "history": true,
  "crawl_jobs": true,
  "dry_run": true
}
```

Success returns `dry_run` and integer counts for `versions`, `history_entries`,
`history_queries`, `history_urls`, `crawl_jobs`, and `crawl_urls`. A dry run counts
rows without deleting them, including query and URL rows that would become
unreferenced. Otherwise the counts describe rows deleted in one SQL transaction.
A transaction failure rolls back all selected categories and returns `500`.
Invalid cutoffs, empty selections, unknown fields, and trailing JSON return `400`.

Version age uses `created_at`; history entry, orphan query and URL, and crawl job
age uses `updated_at`. Pinned history is retained. Only completed crawl jobs with
no pending or in progress URLs qualify. Current index documents, assets, vectors,
and embedding jobs are unaffected. See [Purging Retained Data](data-lifecycle#purging-retained-data)
for user guidance and limitations.

### Request Limits

API request bodies have a default limit of 1 MiB, including MCP requests and
login requests. The following endpoints have their own limits:

| Requests                                                | Body limit                                       |
| ------------------------------------------------------- | ------------------------------------------------ |
| `POST /api/add`, `POST /api/add_pdf`, `POST /api/batch` | `server.max_batch_body_size` MiB, default 40 MiB |
| `POST /api/rules/preview`                               | 1 MiB                                            |

Limits apply to JSON and form data, including requests sent with chunked transfer
encoding. The PDF upload limit includes the base64 content and the surrounding
JSON. An oversized request returns HTTP `413` with a JSON body containing
`error`, `code: "request_too_large"`, and `limit_bytes`.

Search WebSocket messages have a limit of 64 KiB across all frames in a message.
An oversized message closes the connection with code `1009`.

The HTTP server allows 10 seconds to receive request headers and two minutes to
receive the entire request, including its body. It closes idle keepalive
connections after one minute. These limits do not impose a duration limit on
handlers such as reindexing after the request has been received.

### Document Versions

```http
GET /api/versions?url=<url>
```

The endpoint returns stored version differences for the given URL, with the
newest record first. Each entry contains `id`, `created_at`, `url`, `user_id`,
`html_diff`, and `text_diff`. The difference values use
[diff match patch](https://github.com/google/diff-match-patch) patch strings.

### Current Profile

`GET /api/profile` returns information about the currently authenticated user:

```json
{
  "user_id": 1,
  "username": "alice",
  "is_admin": true
}
```

## Document Language Handling

User configuration and correction workflows are described in
[Language Settings and Detection](/docs/language-detection).

`server/languages` resolves language tags, aliases, detector candidates, and
Bleve analyzer routing. These are separate concerns: a valid document tag can
lack a detector model or a language analyzer. Regional tags share their base
language's analyzer and index. Norwegian Bokmal (`nb`) uses analyzer `no` and
`index_no.db`; Chinese, Japanese, and Korean share the CJK analyzer while keeping
separate indexes. Unknown languages and tags without an analyzer use `index.db`.

`server/document` runs Lingua on extracted text. Low accuracy uses trigram
models; high accuracy can use models for one through five letters on short
text. Both modes use trigrams for text with at least 120 letters. Models load
lazily and remain cached for the process lifetime.

The built in detector requires at least 12 Unicode letters or four letters
from the Han, Hiragana, Katakana, or Hangul scripts. It then requires a top
confidence score of at least `0.5` with all languages, or `0.8` with an explicit
candidate list, and a margin of at least `0.15` over the runner up. These are
heuristics; scores are relative to the candidate set, not calibrated
probabilities. An excluded language can still receive an incorrect confident
classification.

`Document.ResolveLanguage` tries a web title only when the body has insufficient
text, not when a longer body is ambiguous. File titles are excluded from this
fallback. A root HTML `lang` or `xml:lang` declaration can refine an agreeing
base language. Parsing is capped at 64 KiB, and child attributes are ignored.
Declarations cannot bypass disabled detection or override conflicting text.
`EvidenceLanguageDetector` exposes transient scores and decision reasons;
implementations of the original `LanguageDetector` interface remain supported.
Resolution leaves document metadata unchanged.

The `language_override` document field persists a manual pin separately from
`language`. Export and import preserve it. Ordinary resubmissions retain an
existing pin. `/api/update` accepts `{"language":"auto"}` in `changes` to clear
it and reevaluate saved content. Title edits also reevaluate unpinned web
documents. Updates load stored HTML to preserve agreeing regional declarations.

The authored regression corpus in
`server/document/testdata/language_corpus.json` covers eleven languages and
short or empty inputs in both accuracy modes. It checks expected behavior,
not accuracy on representative browsing collections. Run the focused tests with:

```bash
go test ./server/languages ./server/document ./server/indexer/... ./cmd
```

## Extractor Development

Extractors are components that enrich metadata, produce searchable content,
and render document previews. Indexing first runs every matching enricher in
registration order. It then tries matching content extractors in registration
order until one succeeds. Preview selection considers only extractors that
declare the preview capability.

### Extractor Decisions

`Extract` and `Preview` return explicit result values. Each result records one
of three decisions:

| Decision | Meaning                                                                                              |
| -------- | ---------------------------------------------------------------------------------------------------- |
| Success  | The extractor handled the document successfully.                                                     |
| Fallback | The extractor was inconclusive, so the next matching extractor should be tried.                      |
| Abort    | A fatal error occurred. The chain stops immediately and returns the error without trying a fallback. |

Use `Extracted`, `ExtractFallback`, or `AbortExtraction` for extraction
results. Use `Previewed`, `PreviewFallback`, or `AbortPreview` for preview
results. The result fields are private, so an implementation cannot return a
contradictory decision and error combination. The zero value is invalid.

When a caller names a preview extractor explicitly, that extractor must be
enabled, preview capable, and matched to the document. Selection starts there.
If it returns `PreviewFallback`, later matching preview extractors may still
provide the response.

If no extractor succeeds, `ErrNoExtractor` is returned.

### Extractor Interface

A custom extractor can depend on the focused
[`server/extractor/sdk`](https://github.com/asciimoo/hister/tree/main/server/extractor/sdk)
package instead of importing Hister configuration, document, and result
packages separately. It must implement this interface:

```go
type Extractor interface {
    // Name returns a human readable identifier used in logs and config.
    Name() string

    // Description returns a short human readable summary for clients.
    Description() string

    // Capabilities declares the extractor phases this implementation joins.
    Capabilities() sdk.Capabilities

    // Match reports whether this extractor applies to the given document.
    // Extract and Preview are only called when Match returns true.
    Match(*sdk.Document) bool

    // Extract rewrites the document before it is added to the index.
    // Return an explicit success, fallback, or abort result.
    Extract(*sdk.Document) sdk.ExtractResult

    // Preview returns a rendered representation suitable for display.
    // Return an explicit success, fallback, or abort result.
    Preview(*sdk.Document) sdk.PreviewResult

    // GetConfig returns the extractor's current configuration.
    // It must return sensible defaults before SetConfig is called.
    GetConfig() *sdk.Config

    // SetConfig applies user supplied configuration on top of defaults.
    // Return an error for any unrecognised option key.
    SetConfig(*sdk.Config) error
}
```

#### `Capabilities`

Capabilities keep metadata enrichment, searchable content extraction, and
preview rendering independent:

```go
type Capabilities struct {
    Enrich  bool
    Extract bool
    Preview bool
}
```

An enricher annotates every matching document and does not compete to select
the searchable body. A content extractor can populate the title and text. A
preview extractor can render the stored document. Most specialist extractors
declare both content and preview capabilities.

#### Result Types

`sdk.ExtractResult` and `sdk.PreviewResult` describe the outcome of each phase.
Construct them with these helpers:

```go
return sdk.Extracted()
return sdk.ExtractFallback(err)
return sdk.AbortExtraction(err)

return sdk.Previewed(sdk.PreviewResponse{Content: html})
return sdk.PreviewFallback(err)
return sdk.AbortPreview(err)
```

Fallback errors are optional diagnostics. Abort helpers always carry an error.
Passing `nil` to an abort helper produces a descriptive error rather than an
ambiguous result.

#### `Document`

`sdk.Document` aliases the complete
[`document.Document`](https://github.com/asciimoo/hister/blob/main/server/document/document.go)
type. It is passed to `Match`, `Extract`, and `Preview`.

#### `PreviewResponse`

`sdk.PreviewResponse` carries the output of `Preview`:

```go
type PreviewResponse struct {
    Content  string // HTML or plain text to render
    Template string // optional custom frontend template name, leave blank for default
}
```

### Register an Extractor

Built in extractors are constructed by `DefaultExtractors` in
[`server/extractor/registry.go`](https://github.com/asciimoo/hister/blob/main/server/extractor/registry.go).
Add an instance there before the generic fallbacks so it takes priority for
the pages it targets.

Applications can also create an isolated chain or extend a fresh default one:

```go
registry := extractor.NewDefaultRegistry()
if err := registry.RegisterBefore("Readability", &MyExtractor{}); err != nil {
    return err
}
if err := registry.Init(configuredExtractors); err != nil {
    return err
}
```

The package level extraction functions remain available and use
`extractor.DefaultRegistry()`.

### Write a New Extractor

A ready to use starting point lives at
[`server/extractor/extractors/_extractor_template/extractor.go`](https://github.com/asciimoo/hister/blob/main/server/extractor/extractors/_extractor_template/extractor.go).
The directory begins with `_` so the Go toolchain ignores it during normal
builds, but the file itself is valid and fully commented Go.

1. Copy `server/extractor/extractors/_extractor_template/` to
   `server/extractor/extractors/<myname>/` and remove the leading `_`.
2. Change the `package` declaration to match the new directory name.
3. Rename `TemplateExtractor` to something descriptive, such as
   `HackerNewsExtractor`.
4. Update `matchURLPrefix` and the `Match` function for the target site.
5. Update `Capabilities` to declare the phases the extractor supports.
6. Implement `Extract` to populate `d.Title`, `d.Text`, and optional metadata.
7. Implement `Preview` to return sanitized HTML, or return `PreviewFallback` to
   reuse the generic readability preview.
8. Add an import and an extractor entry to `DefaultExtractors` in
   `server/extractor/registry.go`, before the readability extractor.

### Extractor Configuration Support

Embed `sdk.ConfigSupport` to provide `GetConfig` and `SetConfig`. Its zero value
enables the extractor and rejects every option key:

```go
type MyExtractor struct {
    sdk.ConfigSupport
}
```

Use `sdk.NewConfigSupport` when an extractor has custom defaults. Keys present in
the default options map are accepted automatically:

```go
func NewMyExtractor() *MyExtractor {
    return &MyExtractor{
        ConfigSupport: sdk.NewConfigSupport(sdk.Config{
            Enable: true,
            Options: map[string]any{
                "timeout": 10,
            },
        }),
    }
}
```

Additional accepted keys without defaults can be passed after the default
configuration. An extractor can implement its own configuration methods when
applying configuration requires extra work.

Config merging from defaults and user values is performed automatically by
`Registry.Init` before `SetConfig` is called. `SetConfig` always receives the
fully resolved configuration.

### Extractor Guidelines

**Avoid additional HTTP requests.** Work with the HTML and metadata already
available in the `Document` struct wherever possible. Making extra requests
inside an extractor adds latency, increases network traffic, and can fail
silently in offline or restricted environments. Outbound requests also expose
the user IP address and browsing activity to external servers. Make such a
request only when there is no reasonable alternative.

**Avoid embedding third party content.** Strip or discard remote images,
videos, iframes, and other externally hosted media before returning content
from `Extract` or `Preview` wherever possible. Embedded content causes the
browser to contact an external server whenever a preview is opened, which can
expose the user IP address without their knowledge. When multimedia is
essential, prefer a placeholder button that loads it only after explicit user
action.

**Use custom preview templates when they add value.** If extracted content has
a clear structure, return a template name in `PreviewResponse` and build a
dedicated Svelte template for it. Code documentation, question and answer
threads, and recipes often benefit from a tailored layout.

The reusable `issue` template accepts JSON encoded from `sdk.IssuePreview` in
`PreviewResponse.Content`. Set `Template` to `"issue"`. It supports a title,
number, repository, URL, state, labels, a description in `body`, and an ordered
`comments` array. The description and comments use `sdk.IssueComment`, with
optional `author`, `authorURL`, `createdAt`, and `url` fields plus `bodyHTML`.
Use plain text for metadata, absolute HTTP or HTTPS URLs for attribution links,
and sanitized HTML for `bodyHTML`. Resolve relative content URLs before
sanitizing. The web interface renders the layout, and the terminal client
converts the same payload into readable text. Include only comments present
in the saved page.

### Test Extractors Against Live Websites

Live extractor cases are declared in
[`server/extractor/live_cases.yaml`](https://github.com/asciimoo/hister/blob/main/server/extractor/live_cases.yaml).
Each case identifies a public URL, crawler backend, extractor, and a set of
stable content or metadata expectations. Prefer semantic invariants such as the
page type, a durable title fragment, and minimum content length over exact page
snapshots.

Run every live case with:

```bash
./manage.sh run_extractor_tests
```

Pass a case name fragment to run a subset:

```bash
./manage.sh run_extractor_tests discourse
```

The runner fetches each page through the configured Hister crawler backend and
respects its robots rules. It checks the selected extractor directly, checks the
complete extractor chain unless `run_chain` is false, and checks previews when
preview expectations are present. Identical fetches are reused within a test
run.

The live suite uses the `live` Go build tag and is not part of a normal
`go test ./...` run. The manifest structure is still validated during normal
tests, so invalid fields and unknown extractor names fail without making
network requests. Validation also requires at least one positive live case for
every registered extractor.

When a live case fails, the fetched HTML, extracted documents, preview, and a
summary are saved under `/tmp/hister-live-extractors`. Set
`HISTER_LIVE_ARTIFACT_DIR` to use another directory. These artifacts help
distinguish a website structure change from a transient fetch failure.

## MCP Protocol Testing

The MCP endpoint uses Streamable HTTP at `POST /mcp`. Every interaction is a
`POST` request with a JSON RPC 2.0 body. The server responds with a JSON object.
The authorization header is required only when authentication is enabled in
the Hister configuration.

### Initialize

```bash
curl -s -X POST http://127.0.0.1:4433/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-access-token>" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'
```

### List Tools

```bash
curl -s -X POST http://127.0.0.1:4433/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-access-token>" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
```

### Search

```bash
curl -s -X POST http://127.0.0.1:4433/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-access-token>" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search","arguments":{"query":"python async","limit":5}}}'
```

If `result.structuredContent.trusted.next_page_key` is present, pass its value
as `arguments.page_key` in the next call. Treat the cursor as opaque and keep
the query, date filters, and semantic setting unchanged. Each response respects
`limit`; the last page may be empty. Search cursors track both the index batch
and the position within the combined results so pagination does not skip
keyword matches when history fills a page. Semantic matches follow the keyword
batches, with earlier keyword and history matches excluded.

Include full text and language fields when testing optional result data:

```bash
curl -s -X POST http://127.0.0.1:4433/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-access-token>" \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"search","arguments":{"query":"python async","limit":5,"fields":["text","language"]}}}'
```

### Preview

```bash
curl -s -X POST http://127.0.0.1:4433/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-access-token>" \
  -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_preview","arguments":{"url":"https://example.com/article"}}}'
```

Use `fields` to select `text`, `html`, and `metadata`. Omitted fields default
to all three; an empty array returns only document identity and timestamps.
HTML preview rendering runs only when HTML is selected.

For bounded responses, set `max_chars` and continue with `text_offset` or
`html_offset`. These values count Unicode code points after source normalization,
not UTF-8 bytes. Each returned body reports its length and whether more remains
in `trusted.text_total_chars` / `trusted.html_total_chars` and
`trusted.text_truncated` / `trusted.html_truncated`. When more remains, use
`trusted.next_text_offset` or `trusted.next_html_offset` for the next request.
Zero `max_chars` means unlimited, and offsets at or beyond the end return an
empty body without a continuation offset. Metadata is not truncated. HTML
portions may split tags or entities and must be reassembled before rendering.

### Indexed History

```bash
curl -s -X POST http://127.0.0.1:4433/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-access-token>" \
  -d '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_history","arguments":{"mode":"indexed","limit":10}}}'
```

### Facet Counts

```bash
curl -s -X POST http://127.0.0.1:4433/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-access-token>" \
  -d '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"get_facets","arguments":{"query":"python","limit":5}}}'
```

The response's `trusted.reported_total` counts matching indexed documents.
Facet data is in `untrusted_content[0].fields`, with `terms` and
`date_histogram` using the same shape as `/api/facets`. A term facet contains
`terms` with `term`, `count`, and optional `label` values, plus an optional
`other` count. Date buckets have `name` and `count` values and may be absent
when there are no matches. Facet source values remain inside the untrusted
content boundary in both structured content and the text fallback.

### Supported Methods

The endpoint implements MCP specification version `2025-06-18` with these
methods:

| Method                      | Description                                                 |
| --------------------------- | ----------------------------------------------------------- |
| `initialize`                | Capability negotiation, required before any other call      |
| `ping`                      | Liveness check                                              |
| `tools/list`                | Returns the list of available tools and their input schemas |
| `tools/call`                | Executes a tool by name with the provided arguments         |
| `notifications/initialized` | Acknowledged with 202 when sent as a notification           |
| `notifications/cancelled`   | Acknowledged with 202 when sent as a notification           |

Every tool advertises an `outputSchema`. Tool call results include the required
`content` field and a `structuredContent` object conforming to that schema.
Clients should use `structuredContent` and enforce the `untrusted_content`
trust markers when placing results into model context.

## File Extraction

`files/extract` owns PDF, DOCX, Markdown, Org, HTML, and plain text parsing.
Construct a parser with `extract.New(cfg.Indexer.PDFParser)` and call
`Extract(ctx, name, content)`. The name selects a format; parsing uses the
supplied bytes. Results contain format, title, text, and HTML. Errors return
an empty result. Parser configuration is copied at construction, and content
limits apply to every call. Org file references are rejected.

`server/extractor.ExtractFile` adapts these results to a document and runs the
web extractor registry for saved HTML. It applies content fields only after
successful extraction and size checks. Both CLI snapshots and server indexing
use this adapter. File access, directory rules, timestamps, ownership, sensitive
content checks, language detection, and persistence remain with the callers.
The parser package has no dependency on documents, the database, or Bleve.

## Documentation Maintenance

Write user documentation around settings, actions, and observable behavior.
Keep implementation details, internal algorithms, and test design on this
Developer Documentation page.

Add documentation pages under `webui/website/src/content/docs` and register
their slugs in `webui/website/src/lib/docs-structure.ts`. The docs search index,
page routes, previous and next links, and sitemap are generated from this
content during the website build.

When changing query syntax, update query builder tests and the query language
guide. When changing configuration, update the generated configuration
reference data, examples, and related operator guidance. When adding an API
route, declare it in `server/api.go` so the built in API documentation remains
complete.
