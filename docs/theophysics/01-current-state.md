# Current-state inventory

## Inspection boundary

The checkout was clean before preparation. No AGENTS.md was found under `/workspace`. The only project checkout discovered was `/workspace/hister_fork`; `/workspace/.git` is workspace metadata. Origin read access succeeded with `git ls-remote origin HEAD`, resolving to the inspected local revision. No origin credentials were printed. The upstream fork comparison below comes from David's supplied research, not independent inspection of those forks.

| Existing component | Repository evidence | Reuse / limit |
| --- | --- | --- |
| Go CLI and HTTP server | `hister.go`, `cmd/root.go`, `server/server.go`, `server/api.go`, `server/endpoints.go` | Keep API/CLI/MCP adapters; proposed research API is separate |
| Svelte app, shared components, extension, website | root npm workspaces, `webui/app`, `webui/components`, `webui/ext`, `webui/website` | Reuse visual components and clients; no new UI implemented here |
| Full-text/fielded retrieval | `server/indexer/indexer.go`, `server/indexer/querybuilder`, `server/indexer/searchschema` | Bleve is an on-disk dependency even with PostgreSQL |
| Operational relational database | `server/model/model.go`, `config/config.go:DatabaseConnection` | GORM SQLite or PostgreSQL; startup performs migrations, so never aim a development binary at live data |
| Vector retrieval | `server/vectorstore/postgres.go`, `sqlite.go`, `embedder.go` | pgvector optional, HNSW and user filters; extension initialization needs controlled provisioning privileges |
| Content-addressed previews | `server/indexer/datastore.go` | Existing SHA-256 HTML/favicon storage and pending-reference protection; authoritative source archive remains a separate contract |
| Persistent crawling | `server/model/crawl.go`, `server/crawler/persistent.go`, `cmd/crawl.go` | Durable frontier, transactional seed insertion; URL completion follows synchronous processing success; retry can duplicate a submission after crash |
| HTTP and browser fetchers | `server/crawler/http.go`, `http_client.go`, `chromedp.go`, `bidi.go`, `robots.go`, `validator.go` | Reuse fetchers, domain/depth validation and robots. Robots can be disabled in current constructors; research adapter must enforce approved policy |
| File ingestion and extraction | `files`, `cmd/import_file.go`, `server/indexer/files.go`, `server/extractor` | Reuse supported types; desktop paths must be uploaded or mapped through approved agents, not read directly by the NAS |
| Rules and updates | `server/rules_test.go`, `server/indexer/update.go`, `config/reload.go` | Preserve Hister rules; add research-aware impact previews, never turn a label into a research job |
| Versions | `server/model/version.go` | Rule-selected text/HTML reverse diffs; research exact-span records need immutable full text versions independent of mutable previews |
| MCP | `server/mcp.go` | Streamable HTTP, protocol 2025-06-18; `structuredContent.untrusted_content[*].fields`, schema version 1.0. Preserve untrusted boundaries |
| Auth and users | `server/model/user.go`, `server/session.go`, `server/oauth`, `server/proxy_auth_test.go` | Multiuser and proxy/OIDC support exist; map hub identity deliberately, never equate global admin with shared search corpus |
| Build/test | README, CONTRIBUTING, `manage.sh`, `generate.go`, `.github/workflows/test.yml` | Go 1.26+, npm and CGO C compiler; app build required for embedded SPA. CI runs race tests and browser tests |
| Deployment example | `compose.yml`, Dockerfile | Existing compose exposes 4433 and uses named volume; target gateway/private mounts require reviewed changes later |

## Important mismatches to resolve in implementation

Hister CrawlJob is a crawler execution with `running/completed/interrupted`; it is not the durable research container. Create a research job and link one or more crawl executions by instance ID and crawl ID. Existing crawl IDs are strings, not research UUIDs. Do not migrate them into UUID columns or replace the existing frontier.

An index update is not a PostgreSQL research transaction. A successful HTTP response may be retried, and skip rules can cause no index write. Require stable external mapping, revision acknowledgments and reconciliation before marking a research document searchable. Hister `AddCount` may increase on replay: either add a cohesive idempotent adapter patch or tolerate only presentation counters, never treat it as independent source evidence.

Hister PostgreSQL support does not imply that Bleve, previews, file snapshots or backups live in PostgreSQL. Nor does conditional diff versioning establish preservation-grade original captures.

The previously reported NAS port 4433 and desktop port 4484 are historical context only. No current NAS, DSM, Cloudflare, desktop or database settings were observed. No Synology API Hub auth/router/storage abstractions could be inspected. Phase 0 must resolve these before binding new routes or choosing its implementation language.

## Fork strategy

Keep `origin` for `YellowKidokc/hister_fork`; add `upstream` for `https://github.com/asciimoo/hister.git` only in the future maintenance task. Do not rewrite remotes or rebase user work during onboarding. Track upstream SHA, fork SHA, licenses and patch provenance in a future root `FORK.md`; this document is the initial maintenance specification. Record each departure's purpose, affected paths, tests, upstream issue/commit, conflicts and removal criteria. Preserve the upstream Go module identity until an explicit compatibility decision.

Prefer additive research adapter modules and stable metadata IDs over changes in extraction/ranking internals. Use cohesive commits: idempotent ingest acknowledgments; crawl control adapter; filter-before-ranking correctness; optional extraction improvements. Keep application policy outside Hister. Pin tested releases/commits rather than tracking latest container tags. Do not bulk merge stale forks.

| Candidate from supplied research | Proposed priority | Review / acceptance before port |
| --- | --- | --- |
| kestermcullough: formatted HTML/image stripping, Defuddle, Docs/Sheets, import scheduling/config/memory | Selective P2 | Compare current implementation; preserve original bytes, sanitize rendered output; Docs credentials isolated; measure memory and restart behavior |
| AlexanderReaper7: pre-ranking filters, date/type context, rerank, separate credentials, vector cleanup/retry | P1 filters, P2 rest | Tenant/job filters before top-k; embedding-version reindex; deterministic rerank fallback; no indexing credential sent to query endpoint |
| Octavity/hister-serp experimental extractors | P3 | Pin branch/commit, fixtures and licensing; search-assistant text is attributed secondary material, not independent evidence |
| MunGell Wails | Reference only | Inspect macOS packaging ideas; preserve Windows Rust/WebView2 investment after inspection |
| vanhalt contacts/gallery | P3 | Limit PII extraction and media budgets; feature opt-in |
| boromil concurrency/shutdown | P1 candidate | Cancel/restart fixture, queue recovery and bounded memory; check if upstream already provides it |
| HiemSword EML | P2 opt-in | MIME/attachment limits, PII isolation, preserve headers and exact bytes |
| luketych asynchronous YouTube | P3 opt-in | No automatic large media downloads; quotas, cancellation, transcript provenance |

For each upstream update: fetch into review branch; compare departure ledger; merge upstream by default to preserve published commit history. Rebase only unpublished patch branches. Run frozen frontend install/build, Go tests plus race tests on Linux/Windows CI, MCP envelope fixtures, tenant-filter tests, ingest replay/delete/resume tests and disposable PostgreSQL/pgvector integration. Exercise archive/index restore with the prior production snapshot before rollout. A conflict in schema, document ID, auth, crawler completion or MCP is a release blocker.

Hister is AGPL-3.0-or-later. Preserve notices and upstream license; inventory third-party licenses before ports. For a modified network service, provide interacting users a prominent offer/access to Corresponding Source for the running version, including modifications and necessary build/install scripts. Retain reproducible source tags/artifacts per release and include notices/source access in desktop distribution where applicable. Have the actual hub/adapter/desktop combination reviewed for derivative-work and distribution obligations; process boundaries alone are not a license exemption. Respect CONTRIBUTING's AI disclosure and human-written upstream communication rules; no upstream PR or message was sent.
