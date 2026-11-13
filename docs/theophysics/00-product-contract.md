# Desktop-first product contract

Accepted product direction from David, 12 November 2026. This refines the original design and takes precedence where earlier phase descriptions differ. It is a repository preparation update, not a claim that these features are implemented or authorization to deploy to the NAS.

## One product, one authoritative record

Windows is the control center; Synology runs persistent services and stores authoritative records. Users work with research jobs, documents, categories, review and exports rather than raw Hister configuration. Hister remains underneath for full-text/Bleve retrieval, previews, extraction and persistent crawl execution. PostgreSQL owns identity, workflow, categories, relationships, provenance and operation receipts. Its role does not replace Bleve.

The first end-to-end build is:

```text
Create named job
→ upload files / folder manifest / URL list
→ hash bytes and preserve source receipts
→ detect exact and probable duplicates
→ choose categories or preview classification proposals
→ commit authoritative metadata in PostgreSQL
→ index accepted content in Hister
→ search from the Windows control center
→ export selected material as versioned Markdown
```

Persist source receipt and job membership as soon as bytes are durably accepted, before classification; classification decisions and index outbox events then commit transactionally. “Store metadata” in this user flow does not mean holding provenance in volatile memory until AI finishes. An unavailable classifier must not prevent safe ingestion, search or export: retain Inbox/Workbench and show pending classification.

Desktop controls: resumable files/folders/large drag-and-drop batches; URL and spreadsheet intake; job creation; categories and classification preview; exact/probable duplicate review; combined desktop+NAS search; selected-result Markdown exports; bulk remove-from-search, archive/restore and permanent-delete previews. Synology hosts Hister, PostgreSQL, orchestration API, crawl/classification workers, backup/restore jobs, audit records and the public/private MCP boundary. Workers are modules of the same research deployment where possible, not a service per feature.

## Intake and combined desktop search

Native drag/drop produces a frozen manifest with root ID, relative path, file identity, size, modified time and SHA-256. Enumerate incrementally, show progress and exclusions, resume chunks, bound memory, and re-stat/hash files changed during transfer. Preserve hierarchy as metadata without duplicating source bytes for every category. Never silently delete or move the original Windows file after upload; transferring authoritative management to NAS is explicit and leaves a provenance receipt.

Spreadsheet intake initially supports CSV/TSV with an explicit URL-column mapping and preview; XLSX is an optional parser adapter in the same first-slice intake milestone, with clear capability reporting rather than accepting an unsupported format. Preserve workbook/sheet/row attribution, rejected rows and original sheet artifact. Treat cells as data: do not evaluate formulas, macros, external links or spreadsheet commands. Reject invalid URLs per row without discarding valid rows. Apply ordinary fetch/domain/robots/byte limits to accepted URL rows. A URL's presence in a sheet does not authorize a whole-site crawl.

One query can search the canonical NAS corpus and an explicitly selected desktop-private index through the same desktop UI. Keep each result's instance, document ID, capture freshness and privacy state visible. NAS catalog is authoritative for managed copies. A private-local item is not uploaded just by searching it; choosing it for a NAS job/export requires explicit intake. Dedupe UI occurrences via known mapping/hash without hiding location provenance. Show partial results and offline status if either instance is unavailable. Never send private source text or embeddings to an external provider implicitly. Native desktop federates private-local queries; the NAS gateway must not open arbitrary desktop listeners.

## Categories and framework classifications

Versioned taxonomy supports independent facets, not one exclusive destination folder. Seed the following display terms with stable category UUIDs and machine slugs; name changes retain IDs:

| Facet | Initial terms / records |
| --- | --- |
| Topic / framework | Master Equation; Axioms; Theophysics; Theology; Physics; Information; Consciousness; Moral Order; Grace and salvation |
| Workflow / artifact role | Master Equation Namer; Lossless Summary; Stories; Evidence; Objections; Formalization |
| Membership | Series membership; road membership, each with stable series/road IDs |
| Review / lifecycle | Canon status (human-controlled); Inbox; Workbench; Archive |

“Master Equation Namer” and “Lossless Summary” have separate versioned output records, linked to source document/version and producing run; attaching a category does not claim those tasks have been completed. Evidence/Objections category membership describes intended use; it does not create or validate an evidence graph edge. Formalization category membership does not prove a theorem. Canon status remains a human decision. Archive is a lifecycle/storage command, not an automatically applied model tag. AI may suggest sensitive terms, but cannot execute canon changes, archive/move/purge or publication through classification.

A document can belong to many categories, series, roads and jobs while retaining one canonical document identity and one managed authoritative source artifact per version. Shared byte storage across exact duplicates does not collapse independent author/source occurrences. Category projections can contain references/Markdown links; do not create new authoritative copies for each folder placement. Distinguish a document UUID from a paper UUID: a paper record can relate multiple source revisions and contextual projections without guessing that two similar documents are the same paper.

## Classification policy and conservative thresholds

The first slice implements deterministic user-selected categories plus proposal preview/commit. AI proposal adapter can run local classification for review, but unattended policy application stays disabled until thresholds are calibrated against reviewed examples. This preserves David's staged requirement: confidence automation follows a verified first slice. Users can deliberately enable a versioned policy for approved ordinary taxonomy facets after calibration; each application remains reversible and logged.

Proposed configurable bands, with per-category calibrated probabilities rather than an unvalidated model's self-reported certainty:

| Band | Initial numerical policy | Behavior when approved automation is enabled |
| --- | --- | --- |
| High | p >= 0.95, category-specific precision gate passed | apply reversible category assignment; log policy/model/input/version and confidence |
| Medium | 0.80 <= p < 0.95, precision gate passed | apply provisional assignment and add visible review item; do not label human-approved |
| Low | known category proposal below 0.80 | hold proposal in review queue; keep Inbox/Workbench fallback |
| Unknown | missing/uncalibrated score, unsupported category, ambiguous/conflicting output or distribution drift | Inbox/Workbench with explicit abstention; no guessed category |

Scores at boundaries use the table exactly. A high raw language-model confidence without calibration is Unknown for automatic application. Freeze input/document version, taxonomy and policy version with every proposal. Store rationale, model/prompt version and producing-run receipt. Human corrections override machine assignments; a rerun cannot silently replace pinned manual choices. Conflicting assignments queue for review. Selecting categories never moves source bytes. Policy changes affect future decisions unless an explicit reclassification preview is accepted.

Separate `assignment_state` (proposed/applied/provisional/rejected/superseded) from `review_state` (unreviewed/needs_review/human_approved/human_rejected) and `actor_kind` (human/policy/model). Model output alone cannot set human approval. Automatic category assignments do not alter evidence quality, formal validity, personal relevance, source permissions, canon or publication. Review can remove a provisional assignment with an inverse receipt.

## Duplicate contract

Exact duplicates require equal verified content SHA-256 and size. Group known occurrences and preserve per-source/job/location receipts. Probable duplicates use versioned text similarity/fingerprints or optional embeddings and show match reasons; they remain proposals. No probable match automatically merges canonical identities, moves bytes or deletes a file. Exact byte equality also does not prove independent publications are the same provenance source. Group membership is reversible; representative selection is separate from purge authorization. Only a confirmed duplicate-cleanup manifest can authorize removal, after reference checks.

## Three mass actions

| User action | Required behavior | Restoration |
| --- | --- | --- |
| Remove from search | hide or remove selected index projections; retain source bytes/identity/provenance; persist suppression so reconciliation does not reindex automatically | explicit re-enable/reindex with receipt |
| Archive | user-previewed move or recoverable archive copy into approved `999_ARCHIVE`; retain UUID, hash, categories, provenance and original locator | restore verifies bytes and resolves destination conflicts without overwrite |
| Permanent deletion | exact affected source/version/files/dependencies/unique reclaimable bytes; explicit actor-bound confirmation and item-level receipts | irreversible after bytes are purged; backup-retention scope disclosed |

Archive is distinct from 30-day deletion quarantine. `999_ARCHIVE` has no automatic expiry/purge. Default normal search excludes archived items, with an explicit include-archive filter; archived content remains available to authorized retrieval and restore. After archive, the archive artifact is the authoritative managed location; a redundant old managed copy is removed only if the accepted move plan permits it and every shared reference is accounted for. A copy-only archive plan keeps the current authority until a separate promotion, and reports redundant bytes honestly. Original external Windows/UNC files remain unchanged unless separately selected in an explicit native file-management plan.

Archive paths are server-generated under an approved root: `999_ARCHIVE/<tenant_uuid>/<document_uuid>/<version_uuid>/<artifact_hash>/<safe_name>`. These are logical paths pending NAS inspection, not directories created on the NAS. Same-filesystem move uses atomic rename plus durable journal; cross-filesystem move copies, fsyncs, verifies hash, commits location authority, then removes the old managed location only if authorized. Do not claim atomicity across DB and filesystem. Outbox/reconciliation journals recover crashes at each boundary. Shared artifacts cannot be moved away from live references: use an archive reference/catalog projection to the same bytes, or preview a verified archive copy, keeping one authoritative artifact pointer and explicit replicas. Restore creates a conflict if the original path has changed, offering a new destination instead of overwrite.

Bulk selectors support video-game topic, domain, file type, folder/root, category, job, date range, duplicate group, assessed low-quality source, failed crawl and unclassified documents. Resolve them into an authorized immutable manifest before action. A model-supplied low-quality label is a reviewable selector, never permanent-delete authority. Preview affected jobs/categories/evidence dependencies, sample matches, exclusions, file locations, shared refs and byte estimates. Revalidate after changes; no silent expansion. Failed-crawl rows may have no source bytes and must not inflate file/deletion totals.

## Public/private MCP

Expose metadata/snippets by default only within caller ACLs; substantial private text needs explicit, bounded disclosure approval for the agent and selected document versions. Approval binds agent, corpus/root, purpose, byte/token budget and expiry; log retrieval and enforce aggregate limits across pagination so many small calls cannot bypass it. No provider keys, tokens or internal paths in responses. Hister's current untrusted-content envelope remains intact behind the gateway. Private desktop content is never made public by default search federation.

## First-slice acceptance and deferred work

First slice must prove job/file/folder/URL intake, interrupted upload resume, hashes/receipts, exact and probable duplicate review, manual/proposed categories, PostgreSQL commit/outbox recovery, desktop search and selected Markdown export. Exercise simultaneous desktop/NAS result identity and partial/offline results with fixtures. UI exposes proposals before commit; no irreversible automatic classification effect. One managed document appears in multiple categories/jobs without authoritative file copies.

Bulk remove/archive/restore/purge are required product controls and follow as the next fixture-only milestone after the core ingest/search/export slice. Gate them with sealed previews, restore/hash/conflict tests and crash recovery before use on real collections. Do not delay durable receipts, ACLs, backup design or the private MCP boundary until after scaling.

Live providers, crawling at scale, behavioral reranking, Mattermost, Obsidian projections, canon workflows and unattended classification beyond conservative reviewed thresholds remain deferred until this slice passes. The existing Hub and desktop repositories must be inspected for native compatibility; no raw Hister UI substitution counts as desktop product completion.
