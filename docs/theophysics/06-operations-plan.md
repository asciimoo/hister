# Storage, implementation and operational acceptance

## Storage plan without assumed NAS topology

Use logical mount names until read-only NAS inspection: `CORPUS_ROOT` (authoritative HDD Volume 1), `HISTER_DATA_ROOT` (Bleve and required native data), `PGDATA_ROOT`, `WORK_ROOT`, `EXPORT_ROOT` (may be corpus subdirectory), and `BACKUP_ROOT` (independent fault domain). These are design names, not required new secrets or paths that were created. Mount bytes outside container writable layers. Apply explicit UID/GID and least privilege; source intake mounts read-only, corpus writer narrowly scoped, browsers no corpus mount, exporters read selected artifacts and write new packet directories.

Content layout `corpus/tenants/<tenant_uuid>/sha256/<aa>/<bb>/<fullhash>/<artifact-kind>` with safe server-generated segments. Store byte hash before optional compression; separately record physical encoding/hash if compressed/encrypted. Artifacts are immutable. Dedup within a tenant prevents hash-existence leakage; cross-tenant physical dedup is deferred. Logical job size counts referenced content; physical size counts unique stored bytes; reclaimable size counts only zero-reference, no-hold artifacts. Never couple Docker image lifecycle to corpus lifetime.

RAM holds PostgreSQL shared buffers/OS cache, not sole durable job state. Start with PostgreSQL shared_buffers near 20–25% of RAM *allocated to PostgreSQL*, not all NAS memory; cap work_mem by concurrent query budget and measure before tuning. Default conservative worker concurrency: two ingestion tasks, one browser, one local AI; disk/memory pressure stops new work. NVMe may hold Bleve, active vector/HNSW indexes, temporary processing and PostgreSQL only when actual pools and durability support it. HDD Volume 1 holds originals, HTML, frozen text/Markdown, WARC when enabled, uploaded files, completed packets and receipts.

Do not assume Hister has a supported independent preview mount; its native datastore is under its data directory. Initially preserve native layout. Authoritative archive independently lives on HDD; duplicated previews can remain with Hister. Move/split native storage only through a tested supported path/adapter in a later coding task, not symlink surgery on a running NAS. A derived preview cache can be evicted only when retrievable or safely rebuildable; source archives are not cache.

Default no PostgreSQL tablespaces. After inspecting SSD pool and failure semantics, consider a dedicated tablespace for rebuildable indexes, not unprotected operational tables or WAL. Tablespaces complicate backup/restore and must be included in physical backups. DSM SSD cache is optional and model-dependent; do not repurpose NVMe, create pools, alter RAID or enable write cache here. Keep WAL/PGDATA on reliable storage; an NVMe device without redundancy/power-loss behavior review is not automatically suitable.

Initial cache quotas are configurable 10 GiB derived preview cache and 5 GiB temporary work per worker, not promises of available NAS space. Reject/reduce jobs when free space falls below max(10% filesystem capacity, active-job worst-case reservation). Sweep only expired temporary files with no active lease. Quarantine retains bytes for 30 days by default. Never automatically purge durable originals on storage pressure; pause intake and show options.

## Required read-only NAS/Hub inspection questions

- Synology model, CPU architecture (amd64/arm64), DSM and Container Manager versions, RAM, available CPU and local AI host?
- Volume 1 filesystem, RAID/storage-pool layout, free space, snapshot support, encryption, permissions and UPS behavior?
- NVMe devices, existing use/pools, redundancy, health, supported DSM cache versus volume behavior? No drive changes implied.
- Current PostgreSQL version/extension policy, database owners, migration role, backup/WAL/PITR, tablespaces and available restore target?
- Current Hister binary/image SHA, config and data paths, users, Bleve generations, existing corpus ownership and actual listeners? Historical ports 4433/4484 need later authorized probing.
- Hub repository/runtime/auth/session/CSRF/ACL conventions, gateway route names, worker/migration framework, receipts and existing service health?
- Cloudflare tunnel/access policies, trusted proxy boundaries, domains, public endpoints and direct NAS exposure?
- Windows shell repository revision, update signing, current MCP adapter, native bridge, local source mapping and existing caches?
- Backup independent destination, retention and restore objectives, export ownership, privacy/copyright rules for retained sources and sharing?

No answer is inferred from the cloud checkout. These block production placement/integration, not fixture implementation.

## Delivery phases and checkpoints

### 0 — compatibility inventory (no deployment)

Obtain Hub and desktop checkouts, preserve dirty status, read their agent instructions and manifests. Record immutable revisions and actual API/MCP contracts. Reconcile this package into Hub docs. Review fork upstream baseline/patch provenance. Inspect NAS only under a later explicit implementation authorization. Produce an adapter compatibility table and proposed development configuration; do not run Hister startup against a live DB because it auto-migrates.

### 1 — desktop-first fixture-only vertical slice

Create research modules using Hub's native language/conventions; if absent, isolate a Go research companion without inventing Hub APIs. Add disposable PostgreSQL and versioned migrations. Build named jobs; resumable native file/folder drag/drop manifests; bounded URL and CSV/TSV link intake; immutable hashes/receipts; exact/probable duplicate groups and preview; seeded categories and manual/proposal preview/commit; durable metadata/outbox; Hister acknowledgment; search from the inspected Windows client; and selected-result Markdown export. Keep XLSX as an explicitly reported optional parser adapter, never silently accept an unsupported workbook. The product contract defines taxonomy, calibrated confidence bands and private-local search federation.

Persist acceptance receipts before classification and index only committed authorized versions. When AI is unavailable or uncalibrated, use manual categories/Inbox and mark proposals pending; do not block safe intake/search/export. No live providers, scaled crawling, paid API, canon workflow, behavioral reranking or unattended classification in this slice. Test with controlled local HTTP fixtures and disposable storage, not a NAS deployment. Run crash injection after byte rename, receipt/DB commit, classification commit, Hister write and acknowledgment. Require upload resume, duplicate provenance retention, classification preview/reversal, combined-instance result identity, offline/partial results and stable selected-export hashes before exit.

### 1b — reversible mass management

Implement durable remove-from-search suppression, `999_ARCHIVE` move/copy plans, archive restore, and separately confirmed permanent deletion using exact dependency-aware manifests. Archive has no automatic expiration; deletion quarantine remains a distinct 30-day proposal. Test cross-filesystem copy/verify/promote recovery, shared artifact references, restore conflicts, partial retries and permanent-delete confirmations on disposable fixtures. No model classification authorizes a move or deletion. Complete these controls before managing large real collections.

### 2 — canonical crawl and all intake

Attach research processing callback to Hister persistent crawler; add crawl preview/control/status adapter and leased research execution mapping. Extend first-slice URL lists/root manifests with sitemaps, RSS and native crawler intake. Route normal CLI/extension/desktop intake through accepted-source seam or documented importer; test direct-write reconciliation and skip acknowledgments. Add rules/exceptions, quota/robots/SSRF limits and exact dedup occurrence retention. Test 2,000-link fixture queue with pause/crash/resume and repeat submissions; no live crawl needed for acceptance.

### 3 — graph, review and calibrated search

Implement exact-span validation, typed graph, independent-source families, immutable assessments, hierarchy proposals and human decision receipts. Add editable profile versions and search filters before top-k. Add bounded local microtasks and immediate idle cancellation only after backend capability checks. Keep qualitative ranking disabled until pilot rubric passes. Add contradictions/gaps and reviewed lossless-summary coverage.

### 4 — extended client parity and controlled extraction

Extend the phase-1 Rust/WebView2 control center and reuse web UI components where supported; do not defer native desktop ingest/search/export to this phase. Exercise all required screens and intake paths against same canonical instance. Add isolated Chromium and self-hosted Firecrawl single-page fallback with versioned adapter, hard quotas, no egress to internal services. Review preservation-grade Browsertrix/WARC separately. Add offline cache and explicit local/private mode only with conflict/privacy tests.

### 5 — pilot, operations and later rollout

Approve source list and limits; run 36-site pilot with visible measurements from the evidence contract. Revalidate phase-1b archive/delete/purge controls; complete production-scale backup/restore rehearsals, source availability notices and signed release artifacts. Roll out only after production-specific approvals and tested backup, with existing services preserved until parity is verified. No step here authorizes deployment.

## Acceptance matrix — tests to implement, not results of this pass

| Area | Required acceptance | Failure response |
| --- | --- | --- |
| Source transaction | kill at every durable boundary; blob orphan reclaimed after grace, accepted source never lost | block phase 1; repair outbox/ack before adding AI |
| Replay/idempotency | same key/body returns same result; conflicting body 409; duplicate delivery yields one version and mapping | block rollout; unique constraints and adapter ack |
| Lease recovery | two workers cannot complete one logical stage; stale token cannot commit; restart reclaims expired lease | block orchestration |
| Hister projection | skip acknowledged distinctly; version/user matches; reconcile drift; full-text works without embeddings | no searchable claim until verified |
| Tenant/ACL | lexical, vector, facets, snippets, MCP, exports, receipts, SSE and caches cannot cross scopes | release blocker; deny/fix affected path |
| Crawl | seed insertion atomic; 2,000 URLs resumable; robots/delay/redirect scope enforced on each backend | pause execution, preserve error receipts |
| Fetch sandbox | SSRF private/link-local/metadata IPv4+IPv6, DNS rebinding, redirect bypass and browser subrequests blocked | release blocker; deny egress |
| Files | traversal, UNC, junction/symlink escape, renamed files, case and partial uploads tested | reject unsupported root mapping |
| Extraction | exact quotes match frozen bytes; malformed encodings and OCR labeled; compound claims preserve qualifiers | abstain/quarantine; never repair quote by guessing |
| Evidence | direction/types valid; repeats not independent; critical views survive search/export | block graph expansion |
| Ranking | job/claim filters before top-k; no academic-only General; engagement changes only personal rank | disable faulty ranking component |
| Deletion | preview stale on new ref/ACL; explicit confirmation; shared bytes retained; partial retry/undo recorded | invalidate plan or remain partial, no broad cascade |
| Export | reproducible manifest; hand edit creates version/merge; no private link or active HTML leakage | fail export atomically |
| Costs/idle | paid provider disabled by default, authorization/budget every request; activity cancels worker, unknown activity pauses | deny request, receipt no spend |
| Categories | exact band boundaries, Unknown abstention, pinned human choices, multiple memberships without source copies; no canon/evidence/delete effect | block classification automation |
| Archive | archive is separate from quarantine; shared references, cross-filesystem crash recovery and restore conflicts preserve hashes/identity | remain partial and retry, no overwrite |
| Spreadsheet | URL-column preview, row attribution, formula/macro non-execution and per-row failures | reject unsupported formats explicitly |
| Desktop | all screens share instance; upload/hash and reconnect conflicts; hostile page cannot call native bridge | block client release |
| Restore | restore PostgreSQL+corpus+Hister config/mapping; rebuild Bleve/vector; apply tombstones before query | keep isolated; do not promote incomplete restore |
| Upstream | frozen build, Go/race/browser, MCP envelope, crawler resume and PG integration on supported OS | hold fork update |

## Rollback and migration policy

Before any later migration inventory binary, config, database schema, index generation and artifact manifest. Back up PostgreSQL with tested WAL/PITR where supported and copy immutable corpus independently. Bleve needs an app-quiesced snapshot or rebuild from canonical accepted versions; ordinary file copying of a live index is not a verified backup. Preserve Hister-native history/crawl metadata that research cannot regenerate. Record encryption key recovery separately and securely.

Use expand/contract research migrations: additive nullable fields/new tables, backfill in bounded idempotent batches with receipts, validate constraints, switch feature flag, defer destructive cleanup. Version DTOs and retain old readers during cutover. Hister's automatic migrations are handled separately against a restored disposable DB before upgrading production. No cross-schema migration ownership.

Rollback switches application feature flag and previous tested binary/config only when backward-compatible. Do not blindly downgrade schema or reverse destructive migration. When a migration is not backward-compatible, restore into an isolated replacement DB plus matching corpus/index generation, verify receipts and replay permitted outbox events, then explicitly cut over. Replay deletion ledger before making restored data searchable. Purged data and expired backups are irreversible; no undo promise. Export/import to a fresh version is safer than editing historical receipts.

Proposed initial operational goals: RPO 24h for corpus, RPO 15m for PostgreSQL if WAL archiving available, RTO 8h at pilot scale; measure restore time/size and get David's approval before adopting. Monitoring: queue oldest age, dead letters, lease expiry, accepted-versus-searchable lag, disk reservations, artifact hash failures, Hister/PG health, crawl 429/errors, local model cancellation, provider cost ledger, quarantine and backup age. Alert internally in UI first; external messages require separate authorization.

## Receipts

Every operation records request/actor/tenant/resource IDs, input hash, pinned policy/profile/model/prompt/extractor versions, start/end time, outcome/error, output hashes, cost/duration, authorization reference and predecessor/successor IDs. Fetch receipts add redirects, robots and storage hashes. Bulk actions add sealed manifest hash, item result and reasons. Store append-only DB records and immutable JSONL snapshots; signed or hash-chained checkpoints plus backups expose tampering but do not make compromised administrators harmless. Scrub tokens/cookies, signed URL queries and private payloads from logs. Retain full sensitive provenance only under corpus ACL.

## Decisions needing David's approval

1. Production NAS placement, mount paths, UID/GID, storage pools and backup/restore targets after inspection; default no drive/cache/tablespace changes.
2. Pilot source list and crawl/storage budgets; default 36 diverse sites, robots on, paid/live providers off.
3. Human authority for canon/publication, shared workspace visibility, source licensing and external sharing; default private, machine proposals only.
4. Retention, permanent purge policy and recovery objectives; defaults 30-day quarantine, backup expiry explicitly disclosed, proposed RPO/RTO above.
5. Any future paid provider or publication/deployment action; default disabled. AGPL/source access plan requires release review.

All other design defaults in this package are proposals implementers can use for disposable development without another preference round. Missing repository inspection remains a compatibility prerequisite, not a reason to invent Hub details.
