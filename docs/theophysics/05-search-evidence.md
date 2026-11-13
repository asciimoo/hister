# Search, evidence and quality contract

## Strict score boundaries

Preserve native Hister score, lexical query mode and semantic similarity independently. Ranking is a discovery aid; similarity, citations and publisher reputation are not evidence. A source's assertions, machine-normalized claims, evidential assessments, formal inference, theological interpretation and canon placement must be separate objects and display fields. General search is the baseline for all sources, not Academic Lite.

Proposed retrieval: authorize tenant/workspace first; resolve job/collection/hierarchy/claim filters into permitted document versions; apply field filters in Hister lexical and vector candidate retrieval before top-k. Fuse lexical and optional semantic lists with reciprocal rank fusion `RRF(d)=sum(1/(60+rank_i(d)))`. Keep components and pipeline version. Apply profile-specific discovery features only on eligible candidates. Initial deterministic ranking uses normalized lexical/semantic fusion only; enable qualitative feature weights after rubric validation. Never fabricate missing feature values or treat missing assessment as zero credibility. Report feature coverage and ranking mode.

Optional qualitative features are relevance, inspectable supporting passages, internal coherence, explanatory coverage, novelty, firsthand work, reproducibility and independently established convergence. Store feature value, method/version, assessor, rationale, input references and uncertainty. Missing features are null and excluded with disclosed weight renormalization. Do not compare numerical scores across profile versions or query normalizations. Relevance score is not a calibrated probability.

A proposed later General discovery blend uses retrieval 0.60, inspectability 0.10, coherence 0.05, explanatory coverage 0.05, novelty 0.05, firsthand work 0.05, reproducibility 0.05 and independence-aware convergence 0.05. These are editable discovery weights requiring pilot calibration, not epistemic arithmetic. Domain diversity is a reranking constraint (default at most three same-domain entries in top 20 when alternatives exist), with an explainable override for narrow queries. Never downrank Medium/Substack/personal sites just for venue. Publisher prestige is a displayed metadata dimension, default ranking weight zero; manual source preference remains profile policy, not truth.

Personal relevance is a separate opt-in final ordering component bounded initially to a 10% rank influence; return its component and consent state. Opening, dwell, saving and usefulness judgments modify only the current principal's preferences. They must never update claim assessment, formal validity, source evidence strength, canon, other users or global profile weights. Short dwell does not establish low quality. Users can disable/reset personalization and replay impersonal ranking.

| Profile | Explicit deviation from General | Guardrail |
| --- | --- | --- |
| General | source-diverse neutral discovery baseline | no venue hard filter; prestige zero; show missing evidence |
| Academic | boost methods/data/citation traceability; optional academic document-type filter | no mandatory institutional publisher; independent reproducible work eligible |
| Theology | boost theological topic and interpretation/primary-text matches | tradition tags editable; interpretation stays interpretation |
| Physics | boost physical domain, mathematical assumptions and reproducible calculations | mathematical validity and empirical fit separate |
| History | boost provenance, dates, primary sources and independent corroboration | chronology uncertainty and testimony limitations explicit |
| Cross-domain | boost explicitly mapped structural correspondences | show analogy assumptions and known source dependence; convergence is not proof |
| Canonization | surface review-ready material and defeaters for human decisions | no automatic canon; scope/authority/approver required |

Every preset derives from a pinned General version; persist only explicit differences and a resolved immutable definition/hash. User edits create a new version. Search receipts record exact profile/pipeline versions, filters, index generation and optional provider requests. A profile filter called approved is a placement filter, not a synonym for true.

## Evidence interpretation

A claim is an attributed assertion by a source. Atomic normalized text must preserve modal terms, conditions, quantifiers, scope, dates, negation and uncertainty. Split compound claims while retaining shared premise links. Never silently turn “may” into “is,” “some” into “all,” or reported testimony into direct observation. Claim type examples: observation, historical assertion, mathematical proposition, philosophical inference, theological interpretation, methodological claim and analogy.

An edge has a source and target, explicit direction and review state. `supports` says an assessor found a supporting relation; it does not declare entailment. `derives_from` requires stated premises and method. `repeats` and derivative lineage prevent counting copies as independent convergence. `contradicts` preserves both claims and context; do not erase minority or critical positions. `unresolved` and gaps are valid outputs, not extraction failures to be repaired with guesses.

Assessments are immutable, attributed and method-specific, with confidence, rationale and possible defeater. Multiple statuses can coexist: empirical support under a model and disputed philosophical interpretation, for example. Human-approved hierarchy/canon is a separate decision, never a maximum of scores. The Creator premise may motivate cross-domain research but must be recorded as an assumption or interpretive context in the applicable job; it is not a database constraint or hidden ranking prior proving Christianity.

“Independent convergence” requires a reviewed provenance family: identify shared citations, copying, translations and common datasets. Distinct domains and distinct bytes are insufficient. If independence is unknown, label unknown and do not award independence credit. Exact dedup may share a content blob while retaining source occurrence, author/date, retrieval and reasoning provenance. Near-duplicate detection creates candidate equivalence links for review rather than deleting independently important wording.

## Markdown projection contract

Packet layout: `exports/<job_uuid>/<snapshot_hash>/JOB.md`, `SOURCES.md`, `CLAIMS.md`, `CLASSIFICATION.md`, `REPORT.md`, `receipts.jsonl`, `manifest.json`, and `sources/<document_uuid>/<version_uuid>.md`. Optional attachments use relative hash references, not local NAS paths. Export authorization is checked at snapshot and download; private content stays private.

Front matter includes packet format version, job ID/revision, document/version/claim IDs as applicable, capture and export timestamps, raw/text hashes, profile and pipeline versions, extraction/model versions, review state, ACL classification and source attribution. Each claim links exact quote and source span/hash; each edge identifies endpoints, relation and assessor. REPORT separates source claims, evidence, inference, disagreements, reconstructed assumptions and theological interpretation. Include coverage gaps and abstentions before conclusions. A report is a projection of the pinned database snapshot, not an independent editable truth store.

Manifest lists every file's SHA-256 and size plus database snapshot watermark and input version list. Generate deterministically with stable sorting, explicit timestamp fields and escaping of source-supplied Markdown/HTML; do not execute code or render active HTML. Same input snapshot and export version yields same bytes. Large packets can stream under a repeatable-read snapshot or frozen explicit version list; never mix live edits halfway through export.

Never export over an existing path. If a packet exists, verify manifest: identical is reused, changed creates a new packet version with a diff. Hand-edited Markdown is detected by manifest mismatch; preserve it, record a derivative import or produce a three-way merge proposal for review. Do not parse manual changes back into approved graph state without explicit review. “Lossless summary” includes passage-coverage map, exclusions, unresolved references and original text link; it is not accepted as full source evidence.

## Pilot and expansion gate

Use 36 sites as the default target within David's 25–50 range, selected and approved before live crawling: six academic/institutional, six independent scientific or philosophical, six denominational (spread traditions), six long-form/personal including Medium/Substack, six critical/skeptical and six interdisciplinary/history sources. Categories can overlap; record editorial rationale, viewpoint, provenance family and language. This is a sampling plan, not a verified list of sites. Avoid claiming representativeness of Christianity, science or the web.

Capture roughly three substantive pages per site initially, capped by approved robots/quotas. Include equations, quoted criticism, ambiguous dates, copied arguments, OCR/PDF, compound claims and disagreements. Freeze held-out adjudication samples before tuning. Two reviewers independently label at least 200 atomic claim candidates and 100 edges across at least 20 domains, with adjudication and reviewer agreement recorded. Use separate precision/recall/error denominators; include abstentions and failed extraction. Do not cherry-pick only successful pages.

| Measure | Proposed gate for expansion | Measurement |
| --- | --- | --- |
| Exact-span fidelity | 100% mechanically verified stored quotes; no altered quote accepted | byte-offset equality/hash for every accepted claim; OCR provenance separately reported |
| Compound splitting | >=95% correct on adjudicated sampled compounds | qualifiers/premises retained; report n/N and interval |
| Relationship fidelity | >=90% correct relation and direction; zero knowingly inverted contradiction | held-out reviewed edge sample, per-relation breakdown |
| Provenance completeness | 100% accepted versions have receipt/raw+text hashes and model/run provenance where applicable | catalog joins plus artifact checks; absent originals labeled and excluded from preservation claims |
| Dedup and convergence | zero merges erasing source occurrence/lineage in pilot; >=95% correct sampled equivalence proposals | adversarial copied/independent-source fixtures plus adjudication |
| Disagreement preservation | 100% labeled challenges/contradictions retained in graph/export sample | compare accepted review set to export and search |
| Abstention | >=95% of deliberately unresolvable cases abstain; report coverage alongside precision | ambiguous/unsupported/untestable set, false certainty counted as critical error |
| Neutral discovery | diverse venues remain eligible; report recall@20 and ranking by source category | fixed query set, no venue exclusion unless visible profile rule |
| Recovery and isolation | all deterministic acceptance/security gates pass | plan matrix; no cross-tenant visibility or lost accepted source |

Thresholds are proposed engineering gates for David's review, not measured results. Small sample intervals may be wide; expansion requires visible n/N, disagreement analysis and no critical provenance/isolation failures, not only rounded averages. Pilot maps what sources claim and why. It does not prove Christianity. Publish measurements privately for review before scaling intake.
