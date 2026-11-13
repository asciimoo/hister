# Theophysics Research Engine — implementation handoff

Status: proposed architecture, updated 12 November 2026. AI disclosure: prepared by OpenAI Codex from David's supplied handoff and the attached checkout. No implementation, NAS access, deployment, live migration, paid API request, or external message was performed. Human review is required before implementation. This package is technical design, not a theological publication.

## Read in order

0. [Desktop-first product contract](00-product-contract.md): latest product scope, first slice, classification policy and archive behavior.

1. [Current state](01-current-state.md): inspected evidence, reuse and missing repositories.
2. [Architecture and contracts](02-architecture.md): deployment, workflows, Firecrawl and desktop.
3. [Data model](03-data-model.md): relational contract, constraints and state transitions.
4. [API](04-api.md): asynchronous operations, search and deletion contracts.
5. [Search and evidence](05-search-evidence.md): profile defaults, evidence boundaries and pilot.
6. [Operations and implementation](06-operations-plan.md): storage, phases, acceptance, rollback and decisions.
7. [Threat model](threatmodel.md): current versus proposed boundaries and abuse cases.
8. [Environment validation](environment-validation.md): actual execution evidence and setup limitations.

All `/v1` endpoints, `research` tables, gateway modules and desktop features below are proposals unless explicitly called existing. The inspected revision is `3414902b2ff0066d7a387d0812f1b1c367593413` in `YellowKidokc/hister_fork`. The requested Synology API Hub and `D:\GitHub\hister-desktop` were not attached. This package lives in Hister's new docs directory pending relocation or linking into the Hub docs after that checkout is supplied. No substitute Hub implementation or NAS layout is invented.

## Selected direction

Use one NAS hub as canonical service, a modular research application with durable PostgreSQL outbox and leased work, and a narrow internal adapter to the Hister fork. Retain Hister's persistent crawl frontier and retrieval. Keep the research application alongside Hister as a single modular monolith; Hister is a separate upstream-compatible dependency, not a fleet of new research microservices. Integrate it into the existing Hub only after inspecting that repository's language, auth, migrations and operations conventions.

PostgreSQL is authoritative for research state, receipts and the artifact catalog. Hister maintains its own database and Bleve indexes. Immutable corpus bytes live outside containers on approved NAS storage. Windows is a client of the same hub, with optional explicitly separate private local mode. Cloudflare exposes only an authenticated gateway. Local AI proposes categories; calibrated, explicitly enabled policy may apply reversible ordinary assignments, with medium-confidence items flagged for review. Canon, evidence judgments and destructive operations retain separate controls.

The system records an open evidence web: exact source passages, atomic claims, evidence, objections, explicit assumptions and reasoning links. It can record cross-domain convergence under the Creator premise without treating repeated patterns as proof of God or Christianity. General search admits independent sites, personal research, Medium and Substack on equal evidentiary terms. Neither publication venue nor user engagement changes evidentiary status.

## Next-session entry point

Read this directory, confirm repository revision and dirty status, then execute phase 0 and phase 1 in [the plan](06-operations-plan.md). Use the existing isolated checkout; do not create a worktree unless David asks. Do not infer authorization to deploy, probe the NAS, delete data, publish, spend money or migrate live services from this design pass. First obtain the missing Hub and desktop repositories for read-only compatibility inspection. Phase 1 can otherwise be built entirely with disposable fixtures. Build the first slice as job → file/folder/URL intake → hashes/receipts → duplicate review → category preview/commit → PostgreSQL/outbox → Hister → Windows search → selected Markdown export. The desktop-first product contract defines the accepted scope. Add claim extraction and unattended classification only after the core transaction recovers from crashes.
