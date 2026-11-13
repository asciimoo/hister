# Environment validation and limits

Current-instance evidence from 11 November 2026. This document reports actual execution, separate from the proposed architecture and acceptance matrix.

## Verified

- Existing checkout `/workspace/hister_fork`, revision `3414902b2ff0066d7a387d0812f1b1c367593413`; origin read succeeded using platform Git authentication. Initial worktree clean.
- Go 1.26.11 installed under `/workspace/toolchains/go`, downloaded from go.dev and verified against its published SHA-256 `9849f4e04ee35303714e34fbc3ac8ab44ea3e448a95aa182f91e3b9b79c710ec`. `/usr/bin/go` is an unrelated program, so PATH activation is necessary. Node 24.22.1, npm 11.9.0, GCC 14.2 available.
- Frozen `npm ci`, app production build, Go module download and `go mod verify`, and Go binary build completed. Reusable install script rerun successfully; tracked dependency declarations and lockfiles unchanged.
- `go test -p 4 -json ./...` completed with exit 0: 3,411 passing test/subtest events, six skipped events, zero failed events; 41 passing packages and 16 packages reported no tests. Counts include subtests, not 3,411 independent top-level tests.
- Browser extension test runner completed with exit 0: 44 passed, no failures/skips.
- Local Hister started with disposable data at `/workspace/hister-dev/data`, loopback only. Config request succeeded. Supplied text at `https://example.invalid/onboarding-fixture` returned HTTP 201 and search for `quartz` returned that URL. Server was stopped and restarted; same indexed fixture was returned. No NAS instance was contacted.

Browser suite with system Chromium and two workers completed with exit 1: **315 passed, 11 skipped, four failed** out of 330. Failures were desktop token-login return routing, issue preview content, highlighter-unavailable fallback, and locked extractor settings. A targeted rerun of precisely these four tests with one worker and a stable backend completed with exit 0: **four passed**. Initial failures did not reproduce; they remain intermittent/unexplained, not confirmed repository defects. The initial full run overlapped other setup/build work, and the recheck used warm caches; no exact root cause was established. No assertions or application files were changed. This is not a clean all-green full-suite result.

The suite uses existing system Chromium through the repository-supported executable-path override. It primarily mocks APIs and is separate from the actual backend ingest/search smoke check. Initial missing-backend logo requests logged connection errors; the local server subsequently served that asset. Browser traces in the ignored test output correspond to the latest rerun, while the initial full-run summary is preserved in `/tmp/hister-browser-tests.log`.

## Reproduction files and persistence blocker

[Install script](setup/install.sh) and [startup instructions](setup/STARTUP.md) are retained here and in `/workspace/hister-dev`. The complete install script was run, including module verification and rebuild; startup/readiness/persistence commands were exercised. Startup uses retained files; live processes do not survive publication or a new task.

The onboarding draft read tool returned `INVALID_ARGUMENT` twice: “The current environment draft could not be verified.” A subsequent save of `install_script` and `start_skill` returned `INVALID_ARGUMENT`: “Setup could not be completed; saving the draft could not be confirmed.” No draft ID or confirmed save was returned. **Configuration persistence is unconfirmed.** No publication or new-task restoration was tested. The current development workflow works, but onboarding cannot be called fully configured until those fields are saved through a functioning environment configuration flow. Review/save the retained install and startup contents in environment settings, then publish the prepared environment when the product permits. This is a platform draft-binding failure, not a request for a GitHub token or paid credential.

The install script uses direct Git module retrieval because five large Go proxy module archives redirected to forbidden `storage.googleapis.com`; the allowed alternate proxy also returned 403. Direct retrieval of the same pinned modules succeeded with checksum verification retained. No network policy changes were made. Playwright's browser installer first encountered an unwritable default cache and then `Domain forbidden` for `cdn.playwright.dev`; the installed Chromium worked, avoiding a browser download requirement. A future requirement for Playwright's exact bundled browser would need approved download domains, including any redirect hosts, instead of weakening TLS verification.

## Functional fixture

With the local server running as documented:

```bash
curl -fsS -H 'Origin: http://127.0.0.1:4433' \
  -H 'Content-Type: application/json' \
  --data '{"url":"https://example.invalid/onboarding-fixture","title":"Onboarding fixture","text":"Theophysics onboarding sentinel quartz photon validation.","favicon":"data:image/png;base64,iVBORw0KGgo="}' \
  http://127.0.0.1:4433/api/add
curl -fsS -H 'Accept: application/json' \
  'http://127.0.0.1:4433/search?q=quartz&format=json'
```

Matching Origin is required for this local write. An initial missing-Origin write was rejected by CSRF checks; adding the expected header succeeded. For CLI search use explicit JSON format/Accept to avoid the browser-oriented Origin requirement. The fixture supplies text and favicon data; it does not require fetching the invalid-domain URL.

## Unrun checks and boundaries

Live extractor tests, PostgreSQL/pgvector integration, Windows execution, race instrumentation, website/extension production packaging, hot-reload air launcher, NAS/Hub/desktop integration and all future research-engine acceptance tests were not run. PostgreSQL setup is a later architecture implementation target, not required for the verified existing Hister SQLite development workflow. No live migration, crawler campaign, external paid API, deployment or publication occurred. Only new design/setup documentation was added to the checkout; application source, tests, manifests and lockfiles remain unchanged. The locally built binary, frontend output and test artifacts are ignored generated outputs.

## Documentation update — 12 November 2026

Updated the product contract, first-slice sequencing, category/duplicate schema, archive/restore and API requirements from David's desktop-first specification. Checked relative Markdown links and rebuilt the combined handoff and ZIP archive; archive integrity passed. No application tests were rerun for this documentation-only update. Earlier runtime evidence above remains historical; no current-runtime or newly published configuration readiness is claimed. Environment configuration was not changed or republished.
