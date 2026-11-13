# Proposed gateway API v1

These routes do not yet exist. Keep native Hister `/search`, `/api/add`, configuration and `/mcp` private behind adapters. Before implementation, translate this contract into the Hub's existing OpenAPI/route conventions after repository inspection, then generate clients and contract tests. Base URL is the protected gateway; do not embed NAS hostname or historical port in clients.

## Common wire rules

HTTPS JSON, UTF-8, UUID strings, RFC3339 timestamps with UTC offsets, byte counts as nonnegative integers, SHA-256 lowercase hex. Authenticated identity resolves tenant and principal; never accept a body `tenant_id` as authority. Cookie clients use CSRF protection; native clients use scoped bearer tokens. Scopes: `read`, `intake`, `manage_jobs`, `review`, `delete`, `admin`, with resource ACL checks on every route. Discovery leaks no topology or credentials.

Create/mutate calls require `Idempotency-Key`, scoped to principal+route for seven days. Body mismatch is 409. Updates and action commands carry `If-Match` with aggregate revision; missing is 428, stale 412. Lists return `{items,next_cursor,snapshot_at}` with opaque signed cursor bound to principal, filters and revision, default limit 50, max 200. Expired snapshots return 410; authorization changes always override snapshot membership. Unknown fields rejected in command DTOs; versioned metadata objects explicitly allow extensions.

Errors use `{error:{code,message,retryable,request_id,details}}`; scrub SQL, secrets and internal paths. 400 syntax, 401 identity, 403 capability, 404 absent/inaccessible resource, 409 conflict/policy state, 412 stale revision, 413 quota, 422 semantic validation, 429 throttled with Retry-After, 503 dependent service unavailable. All responses include request ID. Async commands return 202 with `Location: /v1/operations/{id}` and `{operation_id,state:'queued',receipt_id,resource_id}`. Accepted never means completed. Operation polling returns progress with timestamp, counts, phase, retry info, errors and receipt links. SSE `/events` supports Last-Event-ID; on retention gap return resync_required; polling is mandatory fallback. Never leak another tenant through events.

## Endpoint and DTO catalog

| Route | Scope and input | Result / semantics |
| --- | --- | --- |
| `GET /.well-known/theophysics` | authenticated discovery | instance_id, api_version, public gateway base, supported capabilities; no internal addresses |
| `GET /v1/health/live` | no sensitive info | process live boolean; no database queries |
| `GET /v1/health/ready` | authenticated | research DB, artifact write/read, outbox and Hister readiness; dependency failure yields 503/degraded details as appropriate |
| `GET /v1/orientation` | read | instance, principal permissions, available jobs/collections, schema/profile versions, feature flags and default scope |
| `GET, POST /v1/jobs` | read/manage_jobs; create `{title,purpose,source_scope,processing_scope,publication_scope,profile_ref}` | job with UUID/revision; draft until scope validated |
| `GET, PATCH /v1/jobs/{id}` | authorized read/write; patch permitted fields only | immutable scope/profile changes create new version; return revision/ETag |
| `POST /v1/jobs/{id}/actions` | manage_jobs; `{action:validate/start/pause/resume/cancel/archive}` | async; rejects invalid transitions; cancel preserves accepted sources |
| `GET /v1/jobs/{id}/stages` | read | dependency DAG, versions, per-stage state and counts |
| `POST /v1/jobs/{id}/stages` | manage_jobs; `{kind,config,dependencies,input_snapshot}` | create stage version, cycle rejection |
| `POST /v1/stages/{id}/actions` | manage_jobs; `{action:retry/cancel/approve,approval_id?}` | async retry/cancel; review capability for approval; no automatic canon |
| `POST /v1/intake/previews` | intake; `{job_id,kind,locators,root_id?,manifest?,rules,storage_policy}` | no fetch by default; authorized resolved scope, invalid paths, limits, preview hash and expiry |
| `POST /v1/jobs/{id}/sources` | intake; `{preview_id,preview_hash,confirmed:true}` or supplied text/artifact DTO | async acceptance; per-source status and eventual receipt/mapping IDs |
| `GET /v1/jobs/{id}/sources` | read | canonical documents, locations, versions and intake/index/analysis state separately |
| `GET /v1/categories` | read; facet/taxonomy version filters | stable category IDs, labels and protected automation flags |
| `GET, POST /v1/classification-proposals` | read/manage_jobs; create `{document_version_ids,category_scope,policy_ref,mode:preview}` | proposals with rationale/calibrated band, Unknown abstention; no assignment in preview |
| `POST /v1/classification-previews/{id}/commit` | manage_jobs; `{manifest_hash,expected_revisions,decisions}` | async reversible assignments; protected canon/lifecycle actions rejected |
| `GET, PUT /v1/classification-policies/{id}` | manage_jobs; versioned thresholds/allowed categories/calibration evidence/enabled | disabled until calibrated and explicitly enabled; human approvals cannot be forged |
| `POST /v1/category-assignments/{id}/actions` | manage_jobs/review; `{action:accept/reject/revert/pin,reason}` | new receipt and review state; category change never moves files |
| `GET /v1/duplicate-groups` | read; exact/probable/job filters | group evidence, locations, source occurrence and review status |
| `POST /v1/duplicate-groups/{id}/decisions` | review; `{decision:confirm/reject/select_representative,document_id?,reason}` | grouping/representative only; deletion requires separate sealed manifest |
| `POST /v1/spreadsheet-previews` | intake; `{artifact_id,format,url_column,sheet?,header_row?}` | accepted/rejected row preview and provenance; no formulas/macros/external links executed |
| `POST /v1/spreadsheet-previews/{id}/commit` | intake; `{preview_hash,job_id,selected_rows,limits}` | bounded URL intake; not whole-site crawl authorization |
| `POST /v1/uploads` | intake; `{job_id,name,size,sha256,media_type}` | upload_id, chunk size, expiry and local authorized upload path; reserve quota |
| `PUT /v1/uploads/{id}/chunks/{ordinal}` | intake; binary + digest, bounded Content-Length | idempotent chunk acknowledgment; conflicting digest 409 |
| `POST /v1/uploads/{id}/complete` | intake; `{sha256,chunk_count}` | verify aggregate checksum/size; async source acceptance; no arbitrary server path |
| `POST /v1/crawl-previews` | intake; `{job_id,seeds:{kind,ids/query_snapshot/domain/urls},limits,allow,exclude,robots,backend,storage_policy}` | frozen seed/rule hash, proposed cost/bytes bounds, domain sample, blocked items; paid disabled |
| `POST /v1/crawls` | manage_jobs+intake; `{preview_id,scope_hash,confirmed:true}` | async crawl_execution linking Hister frontier; reject expired/changed preview |
| `GET /v1/crawls/{id}` | read | execution state, wave, all counts, limits, stop reason and saturation deltas |
| `POST /v1/crawls/{id}/actions` | manage_jobs; `{action:pause/resume/cancel/retry_failed}` | async checkpoint/control; requires revision; retry retains attempt receipts |
| `GET /v1/crawls/{id}/frontier` | read; status/depth/domain filters | authorized Hister frontier projection with cursor |
| `GET /v1/crawls/{id}/errors` | read | error codes, attempt timestamps, sanitized message, retry eligibility |
| `GET /v1/documents/{id}` | read | metadata, locations, job memberships allowed to caller, current immutable version and provenance |
| `GET /v1/documents/{id}/versions` | read | capture and extraction history, hashes and receipts |
| `GET /v1/versions/{id}/content` | read; format text/sanitized_html/markdown/original | authorized streamed attachment by default; raw HTML sandboxed/separate origin, no scripts |
| `GET, POST /v1/claims` | read/review; filters or `{version_id,spans,normalized_text,type,run_id?}` | claim proposal; reject span/quote mismatch; model provenance required for machine output |
| `GET /v1/claims/{id}` | read | exact quote, version/hash, normalized claim, evidence/edges, assessments and disagreement |
| `POST /v1/evidence-items` | review; typed evidence and version/span or artifact refs | evidence record with provenance; inputs/code required for calculation |
| `GET, POST /v1/reasoning-edges` | read/review; `{from_id,to_id,relation,rationale,source_claim_id?,run_id?}` | typed graph with bounded traversal; machine edges proposed |
| `POST /v1/assumptions` | review; `{text,origin,version_id?,spans?,run_id?}` | explicit/reconstructed distinction enforced |
| `GET, POST /v1/assessments` | read/review; `{target_id,status,method,method_version,confidence,rationale,possible_defeater,context_job_id?,supersedes_id?}` | immutable attributed assessment; never rewrite disagreements |
| `GET, POST /v1/gaps` | read/review; `{job_id,target_id?,kind,question,required_evidence}` | missing evidence, contradiction and abstention queue |
| `PATCH /v1/gaps/{id}` | review; state and resolution receipt | resolve/defer with revision and explanation |
| `GET /v1/review-queue` | review; job/type filters | proposed claims, taxonomy, edges, unresolved spans and conflicts |
| `POST /v1/reviews/{resource_id}/decisions` | review; `{decision,rationale,proposal_revision,canon_status?,approval_id?}` | receipt and approved successor; canon changes need explicit human capability, no machine approval |
| `GET, POST /v1/collections` | read/manage_jobs; `{title,purpose,visibility}` | collection distinct from job; publication ACL remains independent |
| `POST /v1/collections/{id}/memberships` | manage_jobs; `{document_ids,action:add/remove}` | reversible membership change; no deletion or canon effect |
| `POST /v1/collection-import-previews` | intake; manifest artifact | parse/duplicate/ACL preview; execution through source/membership commands |
| `GET, POST /v1/rules` | read/manage_jobs; `{kind,matcher,exceptions,scope,priority,enabled}` | versioned hide/exclude/assign policy; cannot encode purge as rule |
| `PATCH /v1/rules/{id}` | manage_jobs; permitted rule fields | revisioned update; receipt, no retroactive deletion |
| `POST /v1/rules/previews` | manage_jobs; proposed rule plus selector | sample matches, false positives, affected jobs/domains/counts; exceptions applied first |
| `POST /v1/search` | read; SearchRequest below | local retrieval; live only explicitly selected; no intake side effect |
| `GET, POST /v1/ranking-profiles` | read/manage_jobs; `{name,base_profile_ref,definition}` | editable named versions; expose differences from General; author/version required |
| `POST /v1/feedback` | read; `{document_id,event:open/dwell/save/useful,value?,consent_version}` | isolated personal relevance receipt; own user only, opt-in dwell |
| `DELETE /v1/feedback` | read; own personal history selector | clear personal feedback; no source purge |
| `POST /v1/archive-previews` | manage_jobs; `{selector,exceptions,mode:move/copy,target_root_id}` | exact source/target/hash/dependency manifest, shared bytes and restore conflict policy; target is approved 999_ARCHIVE |
| `POST /v1/archive-plans/{id}/execute` | manage_jobs; `{manifest_hash,dependencies_hash,confirmed:true,approval_id}` | async journaled move/copy; copy-only does not silently change authority |
| `GET /v1/archive-plans/{id}` | resource read | per-item journal/progress/receipts; no automatic expiry of archived content |
| `POST /v1/archive-plans/{id}/restore-previews` | manage_jobs; `{item_ids?,destination_root_id?}` | validate retained hashes and destination conflicts; never overwrite |
| `POST /v1/archive-restores/{id}/execute` | manage_jobs; `{manifest_hash,confirmed:true,approval_id}` | async authorized restore and reindex policy; retained UUID/categories/provenance |
| `POST /v1/deletion-previews` | delete; DeletionPreview below | sealed dependency/byte manifest and expiry, no mutation |
| `POST /v1/deletion-plans/{id}/execute` | delete; sealed hash + confirmation + approval | async, revalidates exact resource revisions and ACL; permanent flag cannot be inherited from prior command |
| `POST /v1/deletion-plans/{id}/undo` | delete; `{item_ids?,reason}` | async where retention permits; return 409 irreversible/expired with item reasons |
| `GET /v1/deletion-plans/{id}` | delete | per-item outcomes, receipts, remaining work and reclaim metrics |
| `POST /v1/jobs/{id}/exports` | read plus export capability; `{format:markdown_packet,job_revision,include_private:true,destination:new_version}` | async immutable packet; authorization checked per source; export never implies publication |
| `GET /v1/exports/{id}` | read | snapshot/hash/manifest, state, warnings, authorized download route |
| `GET /v1/exports/{id}/download` | read | archive attachment; stable IDs; signed short-lived link only if gateway supports private artifacts |
| `GET /v1/idle-worker` | own user read | heartbeat/activity freshness, approved tasks, active lease, resource/cost budget and cancellation status |
| `PUT /v1/idle-worker/policy` | own user; `{enabled,allowed_tasks,budgets}` | bounded opt-in; reject destructive/paid task kinds |
| `POST /v1/idle-worker/actions` | own user; `{action:pause/resume/cancel_active}` | cancellation receipt; no broader authorization |
| `GET /v1/storage` | read/admin depending detail | per-job logical bytes vs unique physical bytes, retention, quarantine and capacity |
| `POST /v1/cache/previews` | admin; `{kind,selector}` | reclaimable derived-cache manifest; never delete original corpus through cache control |
| `POST /v1/cache/actions` | admin; `{preview_id,action:evict,confirmation}` | scoped receipt; reindex availability warning; no original purge |
| `GET /v1/system-health` | admin | queue age, dead letters, Hister generation, DB/storage checks, model availability; no secrets |
| `GET /v1/operations/{id}` | resource-authorized read | state/progress/error/receipts; polling works after reconnect |
| `GET /v1/events` | read | scoped SSE, heartbeat and cursor resume |
| `GET /v1/receipts` and `GET /v1/receipts/{id}` | resource-authorized read | immutable, sanitized operation evidence; filter by job/source/request |

## Search request and response

```json
{
  "query": "information conservation",
  "scope": {"mode": "selected_jobs", "job_ids": ["<uuid>"]},
  "filters": {"claim_status": ["unresolved"], "domain_exclude": [], "hierarchy_ids": []},
  "profile_ref": {"name": "General", "version": 1},
  "semantic": false,
  "live": {"enabled": false, "providers": [], "authorization_id": null},
  "personal_relevance": false,
  "refine_snapshot_id": null,
  "limit": 50,
  "cursor": null
}
```

Scope modes: `this_job`, `selected_jobs`, `everything`, `local_plus_live`, `refine`. Local-plus-live means authorized saved corpus plus explicit live provider results; local private client index is a separately marked federated instance and does not silently upload content. Empty selected-job lists are invalid, not “everything.” Refine intersects a frozen authorized result snapshot, retaining parent query/profile. Missing indexed documents return a separate pending count, never synthetic matches. Source-diverse General is the default.

Response includes `query_id`, `snapshot_id`, `profile_ref`, `pipeline_versions`, `index_generation`, `partial`, `warnings`, `provider_usage`, `next_cursor`, and `results[]`. Each result: `document_id`, `version_id`, `canonical_locator`, `allowed_job_memberships`, `provenance`, `native_hister:{id,score,query_mode}`, `score_components`, `display_rank`, `match_reasons`, `assessment_refs`, `publication_status`, `freshness`, `origin:saved/live/private_local`. Live results may have no canonical saved ID and must say so. A score is never labeled truth probability. Claim/ACL filters apply before ranking and pagination; overfetch/recheck is a temporary bounded fallback with partial flag, never a claim of complete filtered retrieval.

## Deletion preview and execution example

```json
{
  "selector": {"result_snapshot_id": "<uuid>"},
  "mode": "quarantine",
  "exceptions": {"document_ids": ["<uuid>"], "job_ids": []},
  "reason": "reviewed noisy video-game material"
}
```

Modes: `hide_in_profile` (prefer rule route), `exclude_future_ingest` (rule), `assign_collection` (membership), `remove_index`, `quarantine`, `purge`. Archive uses separate archive-preview/execute/restore routes and never expires merely because deletion quarantine does. UI presents them as distinct actions. Removal from one collection/job is not removal from all search. Preview returns exact IDs/versions, domains and sample matches, paths through authorized root aliases, job/collection dependencies, claim/edge/assessment dependencies, shared artifact references/holds, logical bytes, uniquely reclaimable bytes, excluded exceptions, warnings, manifest SHA-256, dependency hash, scope revision and expires_at. Do not count shared blobs as reclaimable.

Execution body: `{manifest_sha256,dependencies_hash,expected_revision,confirmed:true,confirmation_phrase,reason,permanent:false,approval_id}`. The server issues an actor-bound, one-use approval from an authenticated explicit confirmation interaction; machine extraction cannot mint it. Purge requires `permanent:true` and separate permanent-purge capability/confirmation. Any changed membership/reference/ACL invalidates the plan with 409 and requires a fresh preview; do not expand scope at execution. Recheck dependencies under locks before each artifact purge. Maintain minimal deleted-ID/hash/reason manifest, with restricted access and no retained source payload when purge was requested.

For remove_index persist search suppression and consult it during reconciliation; only explicit re-enable may rebuild a removed projection. For deletion first tombstone/hide the exact versions in gateway queries; then remove Hister entries, vectors and previews; then quarantine/archive cleanup; finally purge unreferenced approved bytes. Hister direct retrieval must be blocked from gateway bypass throughout. A partial error remains partial and retryable, with per-item receipt. Undo restores permitted retained artifacts, relationships and index projections from manifest; it never promises to recover purged bytes. Proposed quarantine is 30 days. Backups may retain deleted bytes until their documented expiry; restore applies deletion tombstone ledger before serving traffic. UI must disclose this scope instead of claiming universal immediate erasure.

## Product contract additions

Bulk selector DTO supports `topic`, `domain`, `file_types`, `root_ids`, `folder_prefixes`, `category_ids`, `job_ids`, `date_range:{field,from,to}`, `duplicate_group_ids`, `assessment_refs`, `crawl_status`, and `classification_state`. Date field is explicit (captured/modified/added); unclassified means no active non-fallback category, not no document. Low-quality filtering references an attributed assessment; it is not truth or deletion authority. Filter combinations and exceptions are frozen into exact IDs/versions at preview.

Selected Markdown export additionally accepts `{result_snapshot_id,document_version_ids?,category_ids?,include_archive:false}` and pins exact versions/ACL. Combined desktop/private-local search is assembled by the native client with instance-labelled result DTOs; `/v1/search` does not reach arbitrary private desktop endpoints. If selected local-private content is needed in a NAS job or export, explicit upload/receipt precedes export. See [the product contract](00-product-contract.md) for taxonomy, thresholds, archive durability and substantial-private-text MCP disclosure budgets.
