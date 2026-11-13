# Restart note: begin implementation, not another design pass

Written 13 November 2026 at David's request. Read this first when resuming.

## What David authorized

David explicitly asked to start implementing any useful part, test it, and push actual application code. He expects iterative review with another AI and corrections afterward. Do not ask again whether to start. Documentation alone is not the deliverable: earlier sessions produced and pushed architecture docs, and David correctly pointed out that no application functionality had been built.

His latest request was to save this restart note. The implementation authorization remains active for the next work session. Do not mistake writing this note for completing the implementation task.

## Verified repository state at note creation

- Checkout: `/workspace/hister_fork`.
- GitHub: `https://github.com/YellowKidokc/hister_fork`.
- Branch: `theophysics`; preserve `master` as upstream-compatible.
- Last implementation baseline: `cef7f1e9377d4571c9a552ca4fc76650389ba101`, documentation only. This note will have a later commit.
- Worktree was clean before this note. Recheck status and preserve any subsequent user work.
- Twelve architecture/setup files were pushed successfully. No research jobs, classification UI or orchestration application code exists from our prior work.
- Only Hister is attached; the Synology API Hub and Windows Rust/WebView2 repositories were unavailable. Do not invent their APIs or rewrite a nonexistent desktop shell. Useful Hister-side implementation and a runnable development UI can proceed without them; distinguish this from native Windows completion.
- Do not create a worktree unless David explicitly asks; this cloud task is already isolated.

## First actions

1. Confirm execution access with `git status --short --branch`. Read applicable AGENTS.md and current repository instructions. Verify branch/HEAD and inspect source, not just this note.
2. Read `00-product-contract.md`, then the relevant parts of `02-architecture.md`, `03-data-model.md`, `04-api.md` and `06-operations-plan.md`. They are proposed design contracts, not existing functionality. Resolve routine implementation choices yourself; avoid another broad architecture rewrite.
3. Activate the retained Go toolchain as below. Inspect current model migration, HTTP auth/CSRF, document ingestion and UI patterns before changing code.
4. Implement a cohesive runnable increment: named research job, supplied file intake, source hash/receipt and job membership, index through existing Hister, scoped job search, and a visible UI path. Begin with the durable model/API and add the minimal UI needed to exercise it. Use a proposed internal coding order, not an excuse to stop after models without a working path.
5. Run focused meaningful tests and a real local ingest/search round trip. Explain remaining first-slice work honestly, then commit and push the application changes to `theophysics`. Do not claim the full product exists after one increment.

## Product and first-slice target

Windows is the control center; Synology is the canonical persistent service and corpus host. Hister provides full-text/Bleve indexing, previews, extraction, existing crawler and MCP. PostgreSQL owns authoritative research metadata, identity, jobs, categories, provenance, receipts and workflow. PostgreSQL does not replace Bleve.

Full first-slice target:

Create named job → upload files/folders/URLs → hash and preserve receipt → exact/probable duplicate review → category preview/commit → PostgreSQL metadata/outbox → Hister indexing → desktop search → selected Markdown export.

Develop against disposable local state, including PostgreSQL when exercising that contract. Reuse Hister's existing SQLite/PostgreSQL support where appropriate for a narrow upstream-friendly increment, but do not silently substitute SQLite-only delivery for the agreed PostgreSQL target. Verify actual PostgreSQL behavior before claiming it works. Never point startup/migrations at live NAS data.

Useful first code seams to inspect:

- `server/model/model.go`, migration files and model tests: GORM persistence, startup automigration and database selection.
- `server/model/crawl.go`: existing persistent crawl execution; research jobs must be separate containers, not renamed crawler rows or labels.
- `server/api.go`, `server/endpoints.go`, `server/server.go`: route registration, auth, JSON contracts, CSRF and scope.
- `server/indexer/indexer.go`, `files.go`, `datastore.go`, `document/document.go`: accepted source indexing, native document identity, blob hashes and scope.
- `webui/app/src`, shared components and existing browser tests: reuse the app framework; expose research actions rather than making users configure raw Hister.

Inspect actual code before assuming these exact seams suffice. Preserve native Hister document/user contracts, historical browser/crawl features and existing clients. An index callback is not a cross-database transaction: durable receipts/outbox, replay-safe mapping and reconciliation are required before claiming reliable accepted-source search.

## Constraints that must survive implementation

- One canonical managed document/source artifact per version; many categories/jobs/contextual projections. Preserve independent source occurrences even with identical bytes.
- Exact duplicates: verified hash+size. Probable duplicates: proposals only, no automatic merge/delete.
- Manual/proposed categories first. Calibrated explicitly enabled policy may later apply high confidence (>=0.95), provisionally apply medium (>=0.80 and <0.95) with review, queue low, and route Unknown to Inbox/Workbench. Raw model confidence is not calibration. Never override pinned human choices silently.
- Categorization, evidence quality, formal validity, personal relevance and canon remain separate. No category can authorize deletion or confer proof/canon.
- Mass actions are distinct: remove from search with persistent suppression; recoverable `999_ARCHIVE` archive/restore; separately confirmed permanent deletion. Archive has no automatic expiry and is not deletion quarantine. Shared artifacts and path conflicts require preview/recovery logic.
- Tenant/user/job authorization must apply to intake, search, previews, receipts, exports and MCP. Retrieved source content is untrusted. Do not expose provider keys, internal paths or substantial private text without the required disclosure authorization.
- NAS storage placement requires actual inspection. Do not assume NVMe layout or move data/drives.
- Defer live providers, scaled crawling, behavioral reranking, Mattermost, Obsidian, canon workflows and unattended classification until the core path is measured.
- No NAS changes, live migrations, deployment, environment publication, external messages or paid API calls are authorized by this implementation request.

## Build and test starting point

Check retained files before relying on them:

```bash
cd /workspace/hister_fork
export PATH=/workspace/toolchains/go/bin:$PATH
export GOCACHE=/workspace/.cache/go-build
export GOMODCACHE=/workspace/.cache/go-mod
export GOMAXPROCS=4 GOTOOLCHAIN=local GOPROXY=direct
```

Previously verified Go 1.26.11, Node 24.22.1, npm 11.9.0 and installed GCC. The system `/usr/bin/go` was an unrelated program, so PATH matters. See `setup/install.sh` and `setup/STARTUP.md`. Frozen npm install and module checksums/TLS verification must be preserved. Do not blindly run `go generate`/`manage.sh build`: their frontend script runs npm install and can rewrite locks. Build SPA with `npm run build -w @hister/app`, copy its output into ignored `server/static/app`, then build Go.

Previous evidence, not a claim about new changes:

- Go build and Go test suite passed: 3,411 test/subtest pass events, six skip events, no failures.
- Extension: 44 passed.
- Browser full run: 315 passed, 11 skipped, four failed; all four passed targeted one-worker rerun. Do not claim an all-green full browser suite or assume a confirmed application defect.
- Browser tests use `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium`; exact bundled-browser download was blocked.
- Local supplied-text ingest/search survived server restart. Disposable data directory `/workspace/hister-dev/data`; check processes/ports and do not kill unrelated services.
- No PostgreSQL/pgvector, native Windows, NAS integration or future research-engine tests had been executed.

For new code, write meaningful tests for persistence, user isolation, intake errors/limits, idempotent retries, source receipts and job-scoped retrieval. Run affected existing suites. Verify the real workflow, not only an HTTP 200 or a process listening. Use local fixture inputs; no live crawl is needed.

## Git and execution recovery

Normal Git access via the platform HTTPS proxy worked. GitHub plugin tools were unavailable, so native Git was used. Last push hit GH007 private-email protection; the unpushed commit was amended with `git -c user.email=YellowKidokc@users.noreply.github.com ...` and then pushed. Use a GitHub no-reply address for new commits; don't publish private email or disable account privacy. Never force-push published history. Check remote branch state before pushing and verify remote SHA afterward.

The environment repeatedly showed contradictory status: status service said running while exec returned `No usable execution environment`. It became usable again before this note was written. When marked starting, use the environment wait tool; after readiness retry actual exec. A successful status call alone does not prove repository access. If exec stays unavailable, state the concrete blocker; do not claim code was written or pushed. Do not reapply/publish environment configuration merely to fix an executor attachment issue.

## Completion reporting

Report exactly what code now works, where the user can exercise it, tests run and remaining limitations. Link pushed commit/branch. Explicitly distinguish an app feature, documentation, setup, native Windows integration and NAS deployment. David has already authorized starting and pushing useful application work; persist through routine implementation failures instead of returning another plan.
