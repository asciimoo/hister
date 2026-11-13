# Research engine threat model

Scope: inspected Hister revision `3414902b2ff0066d7a387d0812f1b1c367593413` plus the explicitly proposed research system in this package. This is design risk analysis, not a completed vulnerability scan or validated findings list. Hub/desktop/NAS/Firecrawl source and deployed policy were not available. The Codex Security threat-model skill's linked reference resources could not be resolved by the skill service; no shared security cache or scan artifact was read or written. This user-requested local document does not claim completion of that scan workflow.

## Current architecture and assets

Hister accepts documents via HTTP/CLI/browser/importers, extracts and indexes them with Bleve, stores relational users/crawl/history/version state in SQLite or PostgreSQL and optionally vectors in sqlite-vec/pgvector. Its filesystem content store holds previews/favicon bytes by SHA-256. MCP exposes retrieved source fields inside an explicit untrusted envelope. Current auth, browser/fetcher validation and tenant-sensitive behavior have tests, but their existence does not establish production security. The example compose maps a server port to the host; it does not prove the NAS has the desired private gateway boundary.

Assets: corpus originals, immutable extracted text and spans, private browser/file data, database/jobs, account identities, source permissions, claim/assessment provenance, approved placements/canon, deletion manifests, model prompts, credentials, provider budgets, NAS filesystem and backup keys. Integrity of the evidence chain matters as much as confidentiality. Search and model output must not confer permission to act.

Attackers include a hostile website/publisher, malicious uploaded file, adversarial extracted passage, low-privilege user in another tenant, compromised desktop/WebView, untrusted model/extractor, stolen gateway token and compromised worker dependency. A malicious NAS/database administrator can exceed application protections; backups/signatures and least privilege reduce damage but are not absolute defenses. No unobserved external attacker access is asserted.

## Trust boundaries and required controls

| Boundary / abuse case | Impact | Required control and verification |
| --- | --- | --- |
| Internet URL → HTTP/browser/Firecrawl | SSRF into NAS, PostgreSQL, Ollama, metadata or loopback; redirect/DNS rebinding | shared policy validating scheme, DNS/IP before connection and every redirect; deny private/link-local/loopback/metadata IPv4+IPv6 and non-HTTP schemes except explicitly approved local-root intake; browser subrequests/downloads/WebSockets same egress restriction; test all backends |
| Untrusted upload → parser/archive/PDF/OCR | traversal, parser exploit, decompression bomb, resource exhaustion | isolated bounded extraction, noexec scratch, recursion/time/byte limits, file signature checks, supported type allowlist, no corpus/network credentials; hostile fixtures |
| Website/Markdown/MCP source → model | prompt injection leading to data leak, paid call, deletion or canon | keep fields as data, retain current untrusted envelope, schema-validate outputs, capability broker outside model, read-only model tasks; adversarial quoted tool instructions must not execute |
| Source HTML → UI/WebView/export | XSS/session theft/native bridge execution | sanitize display, strict CSP, raw originals attachment/separate sandbox origin, no active HTML in exported prose, minimal origin-bound native bridge; test hostile links and scripts |
| Client principal → gateway → Hister | confused deputy, forged proxy identity or user ID | authenticate at gateway, reject external identity headers, scoped service identity mapping, Hister private listener, tenant ACL on lexical/vector/facets/previews/history; deny direct bypass |
| Search filter/cache/event → another tenant | snippets, counts, memberships or hash existence leaks | authorize before candidate retrieval and serialization; cache/cursor keys bind principal+ACL+filters, invalidate on permission change; test all result channels and receipts |
| Desktop root/UNC → NAS file intake | arbitrary file read, symlink/junction escape, credentials in network share | explicit approved root IDs, safe traversal/file IDs, manifest hashes, read-only agent roots, uploads rather than raw paths; no arbitrary shell bridge |
| Model extraction → claim/edge/assessment | fabricated quotes, wrong direction, suppressed objections, analogy promoted to proof | immutable span/hash validation; proposals with provenance; method/assessor/defeater; independent review; disagreeing assessments coexist; graph endpoint/type tests |
| Research outbox → Hister index | lost source, duplicate indexing, wrong owner/version, stale resurrection | transaction/outbox, fenced leases, stable logical keys, explicit ack/reconciliation, tombstones checked before replay; kill/replay tests |
| Bulk selector → deletion bytes | race expands scope, shared-source loss, unrecoverable purge | sealed exact manifest, expiry/actor/revision, dependency locks and reference counts, separate permanent confirmation, quarantine/undo limits, item receipts; mutation-between-preview tests |
| Local AI/paid provider → external network | exfiltration, hidden bill, background resource starvation | local-only default, task/destination budgets and authorization each request, idle detection fail closed, cancellation, usage receipts; no paid fallback on local failure |
| Container/runtime → NAS | breakout or broad writable mount destroys corpus | unprivileged containers, narrow RO/RW mounts, cap drop, no Docker socket, isolated browser network, immutable pinned images, tested restore; assess DSM capability before rollout |
| Dependency/upstream patch → build/release | supply-chain compromise or regression | frozen locks, checksums/TLS, pinned source/images, license review, cohesive patch ledger, CI and restore rehearsals; no stale fork bulk merge |
| Logs/receipts/exports → unintended readers | credentials/private source leakage or tampered provenance | redact tokens/cookies/query secrets, per-resource ACL, encrypted backup, retention policy, immutable/hash-linked receipts, external checkpoints; audit export authorization |
| Backup restore → formerly purged sources | deleted/private data resurrected | replay deletion/ACL ledger before serving; disclose backup retention and distinguish byte purge from index removal |

The proposed fetch restrictions need implementation across Hister and companions; their presence in this document is not evidence they exist today. Any requirement to crawl an internal approved site needs a narrow administrator-approved policy distinct from default public-web crawler access, never a blanket private-network allow rule.

## Security acceptance and remaining questions

Release blockers: any cross-tenant result; source-controlled code/native bridge execution; model-authorized destructive/paid action; permanent purge outside sealed manifest; replay resurrecting tombstoned content; forged or altered accepted quote; source content escaping its raw-data boundary. These tests belong in the acceptance matrix and must run against both gateway and adapters.

Unresolved: current Hub auth and CSRF conventions, production Hister exposure, DSM container controls and architecture, Firecrawl version/dependency/egress model, local model cancellation, Cloudflare proxy header trust, backup key custody and retention. Confirm these before production design sign-off. A later repository security review should inspect actual routes, parser paths and adapters; this design alone cannot establish that deployed services meet the controls.

## Desktop-first product refinement — 12 November 2026

New boundaries to test: spreadsheet cells/macros/external links are inert intake data; native large-folder enumeration cannot escape approved roots; private-local search never implies upload consent; confidence policy cannot set canon/evidence status or trigger file moves/deletes; human-pinned categories survive model reruns; archive cross-filesystem recovery preserves a verified authoritative pointer; shared artifact/archive references cannot disappear through a move; restore cannot overwrite a changed destination; search suppression survives outbox reconciliation. Substantial private-text MCP approvals must bind agent/document versions/budget/expiry and aggregate across pagination. These are proposed acceptance requirements, not tests executed in this documentation update.
