Use the existing isolated checkout /workspace/hister_fork. Do not create a Git worktree unless the user explicitly requests one. Read repository agent instructions and preserve user changes. Source/application/test/lock files must not be altered for setup.

Activate the retained Go toolchain before every Go command:
export PATH=/workspace/toolchains/go/bin:$PATH
export GOCACHE=/workspace/.cache/go-build GOMODCACHE=/workspace/.cache/go-mod GOMAXPROCS=4 GOTOOLCHAIN=local GOPROXY=direct

The system /usr/bin/go is a different program. The verified Go version is 1.26.11, Node 24.22.1 and npm 11.9.0. CGO uses installed GCC. If dependencies need refreshing, run bash /workspace/hister-dev/install.sh; its contents are also saved as install_script where configuration persistence succeeds. Native app and Go build output are retained; processes are not.

From /workspace/hister_fork start a disposable development server, never the live NAS:
HISTER__APP__DIRECTORY=/workspace/hister-dev/data HISTER__SERVER__BASE_URL=http://127.0.0.1:4433 ./hister listen --address 127.0.0.1:4433
Keep it in a managed terminal/session. Check an existing port/process first; do not kill unrelated services. For readiness GET http://127.0.0.1:4433/api/config, then GET /search?q=quartz&format=json with Accept: application/json. The onboarding fixture https://example.invalid/onboarding-fixture contains the word quartz and must appear. For a fresh empty data directory, submit the documented supplied text fixture with matching Origin header before the search. Do not fetch real sites or enable remote embeddings/paid providers for this check. No public preview links are supported.

Backend verification: go test -p 4 ./... . Frontend: npm run build -w @hister/app . Browser suite uses installed Chromium: PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium npm run test:browser -w @hister/app -- --workers=2 . Extension: npm test -w @hister/ext . Browser tests mostly mock API responses and do not replace backend integration. Optional live extractor, PostgreSQL/pgvector and Windows/race suites need their own explicit disposable test setup.

For frontend hot reload while the compiled backend is running, npm run dev -w @hister/app -- --host 127.0.0.1 . Rebuild SPA via npm run build -w @hister/app and copy build contents into ignored server/static/app before Go builds. Prefer this tested path over manage.sh build/go generate, which run npm install and may change a lockfile. The provided serve script installs air@latest; it was not needed or validated during onboarding.

The architecture package in docs/theophysics is proposed design only. Hub and Windows desktop checkouts, NAS/storage inspection and all proposed integration acceptance tests are outstanding. Never interpret this setup as authorization to deploy, migrate live data, publish, call paid APIs or modify the NAS.
