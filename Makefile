# aboutme — repo-level targets. App-specific targets arrive with the apps.
# bash, not sh: test-db-up's host-side port probe uses /dev/tcp, a bash
# builtin that under dash never succeeds and turns the readiness loop into a
# guaranteed 30s failure.
SHELL := /bin/bash
.PHONY: help ci check scan tools-check operational-test hooks-install docs-lint docs-fmt generate schema-gen schema-check api-gen api-check server-build server-vet server-test server-test-db server-test-s3 server-test-p2b server-test-p2b-s3 web-build web-lint web-typecheck web-test web-e2e web-e2e-update dev dev-down test-db-up test-db-down test-s3-up test-s3-down server-test-integration semgrep semgrep-ci sqlc-gen sqlc-check migrate migrate-check server-migration-test public-roots-check route-table-test dev-native dev-native-down dev-native-status dev-native-logs dev-https dev-https-down dev-https-status dev-https-logs dev-https-browser-image dev-https-auth-check dev-https-transport-check

WEB_E2E_COMMIT := $(shell git rev-parse --verify 'HEAD^{commit}')
WEB_E2E_IMAGE := mcr.microsoft.com/playwright:v1.62.1-noble@sha256:c091b21d9fae78c76e85cd4356431e9b018402f172a214fc7d7a5e9a7e29d8ac
WEB_E2E_MANIFEST := scripts/web-e2e-source.manifest
DEV_HTTPS_BROWSER_CONTEXT := deploy/dev-https-browser
DEV_HTTPS_BROWSER_TAG := localhost/aboutme-dev-https-browser:local
DEV_HTTPS_BROWSER_MANIFEST := .dev/native-https/browser-image.manifest
DEV_HTTPS_BROWSER_SOURCES := \
	deploy/dev-https-browser/Dockerfile \
	deploy/dev-https-browser/package.json \
	deploy/dev-https-browser/package-lock.json \
	deploy/dev-https-browser/playwright.config.ts \
	deploy/dev-https-browser/auth.spec.ts \
	deploy/dev-https-browser/transport.spec.ts \
	deploy/dev-https-browser/network-policy.ts \
	deploy/dev-https-browser/run.sh

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "%-16s %s\n", $$1, $$2}'

ci: ## Full local non-security gate, including DB-backed suites; integration owner runs it before integration
	bash scripts/ci.sh

check: ## Fast gate — the same checks minus the web build and DB-backed suites; for the inner development loop
	bash scripts/ci.sh --fast

scan: ## Batched security scan for a phase gate: Semgrep (SAST + Supply Chain SCA + secrets) then gitleaks over full history
	scripts/test/semgrep-sca-inputs-test.sh
	bash scripts/scan.sh

tools-check: ## Verify local gate tools match .tool-versions (limit with ARGS="ci", "scan", "dev", or tool names)
	bash scripts/check-tool-versions.sh $(ARGS)

operational-test: ## Test local CI, scan, toolchain, Compose guard, and native-status contracts without real services
	bash -n scripts/check-tool-versions.sh scripts/check-migrations-append-only.sh scripts/ci.sh scripts/scan.sh scripts/dev-native.sh scripts/dev-https.sh scripts/dev-https-test.sh scripts/test-s3.sh scripts/web-e2e-source.sh scripts/web-e2e-source.test.sh deploy/dev-https-browser/run.sh deploy/dev-https-browser/static-test.sh scripts/test/render-topology-test.sh scripts/test/ci-failure-propagation-test.sh scripts/test/ci-lifecycle-test.sh scripts/test/ci-scan-adversarial-test.sh scripts/test/live-db-transcript-secrecy-test.sh scripts/test/makefile-safety-test.sh scripts/test/migration-append-only-test.sh scripts/test/scan-engine-error-test.sh scripts/test/scan-products-contract-test.sh scripts/test/semgrep-sca-inputs-test.sh scripts/test/toolchain-contract-test.sh scripts/test/workflow-safety-test.sh
	bash scripts/test/render-topology-test.sh
	bash scripts/dev-https-test.sh --static
	bash deploy/dev-https-browser/static-test.sh
	scripts/test/ci-failure-propagation-test.sh
	scripts/test/ci-lifecycle-test.sh
	scripts/test/ci-scan-adversarial-test.sh
	scripts/test/live-db-transcript-secrecy-test.sh
	scripts/test/makefile-safety-test.sh
	scripts/test/migration-append-only-test.sh
	scripts/test/scan-engine-error-test.sh
	scripts/test/scan-products-contract-test.sh
	scripts/test/toolchain-contract-test.sh
	scripts/test/workflow-safety-test.sh
	scripts/web-e2e-source.test.sh

hooks-install: ## Point git at .githooks so pre-commit runs gitleaks on staged content
	git config core.hooksPath .githooks
	@echo "core.hooksPath = .githooks (pre-commit runs gitleaks protect --staged)"

docs-lint: ## Check markdown formatting + lint
	npm run docs:lint

docs-fmt: ## Format + autofix markdown, then re-lint (catches non-convergence)
	npm run docs:fmt
	$(MAKE) docs-lint

generate: schema-gen api-gen sqlc-gen ## Regenerate all generated artifacts (schema types, API client, then sqlc)

schema-gen: ## Regenerate schema types (Go/TS)
	cd packages/schema && npm run generate

schema-check: ## Fail if generated types drift from the schema
	cd packages/schema && npm ci && npm test

api-gen: ## Regenerate the committed TypeScript API client from docs/api/openapi.yaml
	cd apps/web && npm run api:gen

api-check: ## Lint and test the OpenAPI contract, and fail on generated-client drift
	npx @redocly/cli lint docs/api/openapi.yaml --config docs/api/redocly.yaml
	npx vitest run --dir docs/api/test
	bash apps/web/scripts/api-drift-check.sh

server-build: ## Build the Go API server
	cd apps/server && go build ./...

server-vet: ## Vet the Go API server
	cd apps/server && go vet ./...

server-test: ## Test the Go API server
	cd apps/server && go test ./...

server-test-db: ## Run the auth/store/user/resume DB-backed test suite against a live Postgres (needs test-db-up or TEST_DATABASE_URL); REQUIRE_TEST_DB=1 turns a missing TEST_DATABASE_URL into a failure instead of a silent skip, so a gate run can never pass vacuously
	@printf '%s\n' 'server-test-db: go test live auth/store/user/resume packages'
	@cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable} \
	  go test ./internal/auth/... ./internal/store/... ./internal/user/... ./internal/resume/... -race -count=1 -v

server-test-s3: ## Run the fail-closed media conformance suite against aboutme-test-s3 (needs test-s3-up)
	bash scripts/test-s3.sh run bash -c 'cd apps/server && go test ./internal/media/... -race -count=1 -v -skip "^TestNormalizationBudget$$"'

server-test-p2b: ## Run the fail-closed Phase 2B resume API suite with filesystem media (needs test-db-up)
	@cd apps/server && REQUIRE_TEST_DB=1 TEST_MEDIA_BACKEND=fs \
	  TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable} \
	  go test ./internal/resumeapi/... -race -count=1 -v

server-test-p2b-s3: ## Run the fail-closed Phase 2B resume API suite with S3 media (needs test-db-up and test-s3-up)
	bash scripts/test-s3.sh run bash -c 'cd apps/server && REQUIRE_TEST_DB=1 TEST_MEDIA_BACKEND=s3 TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable} go test ./internal/resumeapi/... -race -count=1 -v'

web-build: ## Build the Nuxt web app
	cd apps/web && npm run build
	cd apps/web && npx vitest run test/fonts.test.ts -t 'retains every license byte-for-byte after nuxt build'

web-lint: ## Lint the Nuxt web app
	cd apps/web && npm run lint

web-typecheck: ## Typecheck the Nuxt web app
	cd apps/web && npm run typecheck

web-test: ## Test the Nuxt web app
	cd apps/web && npm run test

web-e2e: ## Compare renderer baselines in the pinned AMD64 browser
	@set -Eeuo pipefail; \
	if [[ -n "$${UPDATE_GOLDEN+x}" ]]; then echo 'UPDATE_GOLDEN must be absent' >&2; exit 64; fi; \
	if [[ -n "$${PLAYWRIGHT_UPDATE_SNAPSHOTS+x}" ]]; then echo 'PLAYWRIGHT_UPDATE_SNAPSHOTS must be absent' >&2; exit 64; fi; \
	run_id=$${WEB_E2E_RUN_ID-}; \
	if [[ ! $$run_id =~ ^[A-Za-z0-9_-]+$$ ]]; then echo 'WEB_E2E_RUN_ID must match [A-Za-z0-9_-]+' >&2; exit 64; fi; \
	test "$$(uname -m)" = x86_64 || { echo 'web-e2e requires a native x86_64 host' >&2; exit 1; }; \
	commit='$(WEB_E2E_COMMIT)'; \
	test "$$(git rev-parse --verify 'HEAD^{commit}')" = "$$commit"; \
	test -z "$$(git status --porcelain=v1 --untracked-files=all)" || { echo 'web-e2e requires a clean worktree and index' >&2; exit 1; }; \
	result_root=".dev/web-e2e-results/$$commit/$$run_id"; \
	mode_dir="$$result_root/compare"; \
	source_tar=".dev/web-e2e-source/$$commit/$$run_id.tar"; \
	if [[ -e $$mode_dir || -L $$mode_dir || -e $$source_tar || -L $$source_tar ]]; then echo 'web-e2e result or source tar already exists' >&2; exit 1; fi; \
	install -d -m 0700 "$$result_root" "$$(dirname "$$source_tar")"; \
	mkdir -m 0700 "$$mode_dir"; \
	manifest_sha=$$(sha256sum '$(WEB_E2E_MANIFEST)' | cut -d ' ' -f 1); \
	scripts/web-e2e-source.sh "$$commit" '$(WEB_E2E_MANIFEST)' "$$source_tar" >/dev/null; \
	test "$$(sha256sum '$(WEB_E2E_MANIFEST)' | cut -d ' ' -f 1)" = "$$manifest_sha"; \
	tar_sha=$$(sha256sum "$$source_tar" | cut -d ' ' -f 1); \
	umask 077; printf 'commit=%s\nmanifest_sha256=%s\ntar_sha256=%s\n' "$$commit" "$$manifest_sha" "$$tar_sha" >"$$mode_dir/source-metadata.txt"; \
	test "$$(git rev-parse --verify 'HEAD^{commit}')" = "$$commit"; \
	test -z "$$(git status --porcelain=v1 --untracked-files=all)"; \
	podman run --rm --platform linux/amd64 --network=host --security-opt label=disable \
	  -v "$$PWD/$$source_tar:/candidate.tar:ro" \
	  -v "$$PWD/$$mode_dir:/results:rw" \
	  -e TZ=UTC -e LANG=C.UTF-8 -e LC_ALL=C.UTF-8 \
	  -e PLAYWRIGHT_RESULTS_DIR=/results -w /tmp '$(WEB_E2E_IMAGE)' \
	  sh -eu -c 'test "$$(uname -m)" = x86_64; test "$$TZ" = UTC; test "$$LANG" = C.UTF-8; test "$$LC_ALL" = C.UTF-8; locale -a | grep -qx C.utf8; test "$$(locale charmap)" = UTF-8; chrome_version="$$(/ms-playwright/chromium-1234/chrome-linux64/chrome --version)"; chrome_version="$$(printf "%s" "$$chrome_version" | sed "s/[[:space:]]*$$//")"; test "$$chrome_version" = "Google Chrome for Testing 151.0.7922.34"; mkdir /tmp/aboutme; tar -xf /candidate.tar -C /tmp/aboutme; test ! -e /tmp/aboutme/.git; test ! -e /tmp/aboutme/.env; test ! -e /tmp/aboutme/.dev; test ! -e /tmp/aboutme/.superpowers; cd /tmp/aboutme/apps/web; npm ci --ignore-scripts; status=0; PLAYWRIGHT_SURFACE=harness npx --no-install playwright test --config e2e/playwright.config.ts screenshot.spec.ts fonts-offline.spec.ts corpus.spec.ts print.spec.ts || status=$$?; if [ "$$status" -eq 0 ]; then PLAYWRIGHT_SURFACE=normal npx --no-install playwright test --config e2e/playwright.config.ts normal-csp.spec.ts || status=$$?; fi; post=0; for path in playwright-report test-results blob-report; do if [ -e "/tmp/aboutme/apps/web/$$path" ]; then echo "unexpected default Playwright output: $$path" >&2; post=1; fi; done; if [ -e /results/candidate-baselines ]; then echo "compare run wrote candidate baselines" >&2; post=1; fi; test "$$post" -eq 0 || exit 1; exit "$$status"'; \
	test "$$(git rev-parse --verify 'HEAD^{commit}')" = "$$commit"; \
	test -z "$$(git status --porcelain=v1 --untracked-files=all)"

web-e2e-update: ## Generate review-only renderer baseline candidates in the pinned AMD64 browser
	@set -Eeuo pipefail; \
	if [[ -n "$${UPDATE_GOLDEN+x}" ]]; then echo 'UPDATE_GOLDEN must be absent' >&2; exit 64; fi; \
	if [[ -n "$${PLAYWRIGHT_UPDATE_SNAPSHOTS+x}" ]]; then echo 'PLAYWRIGHT_UPDATE_SNAPSHOTS must be absent' >&2; exit 64; fi; \
	run_id=$${WEB_E2E_RUN_ID-}; \
	if [[ ! $$run_id =~ ^[A-Za-z0-9_-]+$$ ]]; then echo 'WEB_E2E_RUN_ID must match [A-Za-z0-9_-]+' >&2; exit 64; fi; \
	test "$$(uname -m)" = x86_64 || { echo 'web-e2e-update requires a native x86_64 host' >&2; exit 1; }; \
	commit='$(WEB_E2E_COMMIT)'; \
	test "$$(git rev-parse --verify 'HEAD^{commit}')" = "$$commit"; \
	test -z "$$(git status --porcelain=v1 --untracked-files=all)" || { echo 'web-e2e-update requires a clean worktree and index' >&2; exit 1; }; \
	result_root=".dev/web-e2e-results/$$commit/$$run_id"; \
	mode_dir="$$result_root/update"; \
	source_tar=".dev/web-e2e-source/$$commit/$$run_id.tar"; \
	if [[ -e $$mode_dir || -L $$mode_dir || -e $$source_tar || -L $$source_tar ]]; then echo 'web-e2e-update result or source tar already exists' >&2; exit 1; fi; \
	install -d -m 0700 "$$result_root" "$$(dirname "$$source_tar")"; \
	mkdir -m 0700 "$$mode_dir"; \
	manifest_sha=$$(sha256sum '$(WEB_E2E_MANIFEST)' | cut -d ' ' -f 1); \
	scripts/web-e2e-source.sh "$$commit" '$(WEB_E2E_MANIFEST)' "$$source_tar" >/dev/null; \
	test "$$(sha256sum '$(WEB_E2E_MANIFEST)' | cut -d ' ' -f 1)" = "$$manifest_sha"; \
	tar_sha=$$(sha256sum "$$source_tar" | cut -d ' ' -f 1); \
	umask 077; printf 'commit=%s\nmanifest_sha256=%s\ntar_sha256=%s\n' "$$commit" "$$manifest_sha" "$$tar_sha" >"$$mode_dir/source-metadata.txt"; \
	test "$$(git rev-parse --verify 'HEAD^{commit}')" = "$$commit"; \
	test -z "$$(git status --porcelain=v1 --untracked-files=all)"; \
	podman run --rm --platform linux/amd64 --network=host --security-opt label=disable \
	  -v "$$PWD/$$source_tar:/candidate.tar:ro" \
	  -v "$$PWD/$$mode_dir:/results:rw" \
	  -e TZ=UTC -e LANG=C.UTF-8 -e LC_ALL=C.UTF-8 \
	  -e PLAYWRIGHT_RESULTS_DIR=/results -w /tmp '$(WEB_E2E_IMAGE)' \
	  sh -eu -c 'test "$$(uname -m)" = x86_64; test "$$TZ" = UTC; test "$$LANG" = C.UTF-8; test "$$LC_ALL" = C.UTF-8; locale -a | grep -qx C.utf8; test "$$(locale charmap)" = UTF-8; chrome_version="$$(/ms-playwright/chromium-1234/chrome-linux64/chrome --version)"; chrome_version="$$(printf "%s" "$$chrome_version" | sed "s/[[:space:]]*$$//")"; test "$$chrome_version" = "Google Chrome for Testing 151.0.7922.34"; mkdir /tmp/aboutme; tar -xf /candidate.tar -C /tmp/aboutme; test ! -e /tmp/aboutme/.git; test ! -e /tmp/aboutme/.env; test ! -e /tmp/aboutme/.dev; test ! -e /tmp/aboutme/.superpowers; cd /tmp/aboutme/apps/web; npm ci --ignore-scripts; status=0; PLAYWRIGHT_SURFACE=harness npx --no-install playwright test --config e2e/playwright.config.ts screenshot.spec.ts fonts-offline.spec.ts corpus.spec.ts print.spec.ts --update-snapshots || status=$$?; if [ "$$status" -eq 0 ]; then PLAYWRIGHT_SURFACE=normal npx --no-install playwright test --config e2e/playwright.config.ts normal-csp.spec.ts --update-snapshots || status=$$?; fi; post=0; for path in playwright-report test-results blob-report; do if [ -e "/tmp/aboutme/apps/web/$$path" ]; then echo "unexpected default Playwright output: $$path" >&2; post=1; fi; done; if [ "$$status" -eq 0 ]; then expected=/tmp/expected-baselines; actual=/tmp/actual-baselines; printf "%s\n" baselines/classic-serif--vn-full--paged.png baselines/engineer-compact--vn-full--paged.png baselines/modern-sidebar--vn-full--paged.png baselines/executive-band--vn-full--paged.png baselines/consulting-formal--vn-full--paged.png baselines/academic-dense--vn-full--paged.png baselines/modern-sidebar--full--continuous.png print-baselines/print-main-overflow-p1.png print-baselines/print-main-overflow-p2.png print-baselines/print-sidebar-overflow-p1.png print-baselines/print-sidebar-overflow-p2.png | LC_ALL=C sort >"$$expected"; test -d /results/candidate-baselines || { echo "candidate baseline directory is missing" >&2; post=1; }; if find /results/candidate-baselines -type l -print -quit | grep -q .; then echo "candidate baseline contains a symlink" >&2; post=1; fi; if find /results/candidate-baselines ! -type d ! -type f -print -quit | grep -q .; then echo "candidate baseline contains a special file" >&2; post=1; fi; find /results/candidate-baselines -type f -printf "%P\n" | LC_ALL=C sort >"$$actual"; diff -u "$$expected" "$$actual" || post=1; if [ "$$post" -eq 0 ]; then (cd /results/candidate-baselines && sha256sum $$(cat "$$expected")) > /results/candidate-baselines/SHA256SUMS; fi; fi; test "$$post" -eq 0 || exit 1; exit "$$status"'; \
	test "$$(git rev-parse --verify 'HEAD^{commit}')" = "$$commit"; \
	test -z "$$(git status --porcelain=v1 --untracked-files=all)"

dev: ## HTTP image/network smoke and self-hosting stack; not for daily development
	@if ! running_containers="$$(podman ps --format '{{.Names}}')"; then \
	  echo "make dev: cannot inspect running containers; refusing to start Compose." >&2; \
	  exit 1; \
	fi; \
	if grep -qx aboutme-test-db <<<"$$running_containers"; then \
	  echo "make dev: aboutme-test-db is running; only the integration owner may stop it after every live-DB worker is idle." >&2; \
	  exit 1; \
	fi; \
	digest_output="$$(node scripts/generate-public-roots.mjs --check)" || exit 1; \
	app_digest=$$(awk -F= '$$1 == "APP_BUILD_DIGEST" { print $$2 }' <<<"$$digest_output"); \
	renderer_digest=$$(awk -F= '$$1 == "PUBLIC_RENDERER_BUILD_DIGEST" { print $$2 }' <<<"$$digest_output"); \
	[[ $$app_digest =~ ^sha256:[0-9a-f]{64}$$ ]] || { echo 'make dev: invalid APP_BUILD_DIGEST' >&2; exit 1; }; \
	[[ $$renderer_digest =~ ^sha256:[0-9a-f]{64}$$ ]] || { echo 'make dev: invalid PUBLIC_RENDERER_BUILD_DIGEST' >&2; exit 1; }; \
	APP_BUILD_DIGEST="$$app_digest" PUBLIC_RENDERER_BUILD_DIGEST="$$renderer_digest" \
	  podman compose --env-file .env -f deploy/compose.yml up -d --build

dev-down: ## Stop the compose stack and remove containers (keeps the postgres volume)
	@APP_BUILD_DIGEST=sha256:0000000000000000000000000000000000000000000000000000000000000000 \
	  PUBLIC_RENDERER_BUILD_DIGEST=sha256:0000000000000000000000000000000000000000000000000000000000000000 \
	  podman compose --env-file .env -f deploy/compose.yml down

dev-native: ## Daily development — native server, web, and Caddy on http://localhost:20080 against the shared database
	bash scripts/dev-native.sh up

dev-native-down: ## Stop the native dev stack (leaves the shared aboutme-test-db container running)
	bash scripts/dev-native.sh down

dev-native-status: ## Native dev stack liveness and ports; non-zero exit if anything is down or crashed
	bash scripts/dev-native.sh status

dev-native-logs: ## Tail native dev stack logs (make dev-native-logs ARGS="-f web")
	bash scripts/dev-native.sh logs $(ARGS)

dev-https: ## Native HTTPS auth harness at https://localhost:20443 against aboutme_dev
	bash scripts/dev-https.sh up

dev-https-down: ## Stop the native HTTPS harness and keep aboutme-test-db running
	bash scripts/dev-https.sh down

dev-https-status: ## Verify native HTTPS harness ownership, configuration, processes, and ports
	bash scripts/dev-https.sh status

dev-https-logs: ## Show redacted native HTTPS logs (make dev-https-logs ARGS="-f server")
	bash scripts/dev-https.sh logs $(ARGS)

dev-https-browser-image: dev-https-status ## Build and record the pinned trusted-browser image by immutable ID
	@set -Eeuo pipefail; \
	repo=$$(pwd -P); \
	state="$$repo/.dev/native-https"; \
	manifest="$$repo/$(DEV_HTTPS_BROWSER_MANIFEST)"; \
	uid=$$(id -u); \
	[ -d "$$state" ] && [ ! -L "$$state" ] || { echo 'dev-https-browser-image: invalid state directory' >&2; exit 1; }; \
	[ "$$(realpath -e -- "$$state")" = "$$state" ] || { echo 'dev-https-browser-image: non-canonical state directory' >&2; exit 1; }; \
	[ "$$(stat -c %u "$$state")" = "$$uid" ] && [ "$$(stat -c %a "$$state")" = 700 ] || { echo 'dev-https-browser-image: state directory ownership or mode mismatch' >&2; exit 1; }; \
	if [[ -e "$$manifest" || -L "$$manifest" ]]; then \
	  [ -f "$$manifest" ] && [ ! -L "$$manifest" ] && [ "$$(stat -c %u "$$manifest")" = "$$uid" ] && [ "$$(stat -c %a "$$manifest")" = 600 ] || { echo 'dev-https-browser-image: invalid existing image manifest' >&2; exit 1; }; \
	  rm -- "$$manifest"; \
	fi; \
	source_hash() { \
	  { for path in $(DEV_HTTPS_BROWSER_SOURCES); do \
	      [ -f "$$path" ] && [ ! -L "$$path" ] || return 1; \
	      printf '%s\0' "$$path"; sha256sum -- "$$path"; \
	    done; } | sha256sum | awk '{print $$1}'; \
	}; \
	source_before=$$(source_hash) || { echo 'dev-https-browser-image: cannot hash browser sources' >&2; exit 1; }; \
	iid_file=$$(mktemp "$$state/.browser-image-iid.XXXXXX"); \
	manifest_tmp=; \
	trap 'rm -f -- "$$iid_file" "$${manifest_tmp:-}"' EXIT; \
	podman build --tag '$(DEV_HTTPS_BROWSER_TAG)' --iidfile "$$iid_file" '$(DEV_HTTPS_BROWSER_CONTEXT)'; \
	mapfile -t iid_lines <"$$iid_file"; \
	[ "$${#iid_lines[@]}" -eq 1 ] || { echo 'dev-https-browser-image: image build returned an invalid IID record' >&2; exit 1; }; \
	image_id=$${iid_lines[0]}; \
	[[ $$image_id =~ ^sha256:[0-9a-f]{64}$$ ]] || { echo 'dev-https-browser-image: image build returned a mutable or malformed ID' >&2; exit 1; }; \
	mapfile -t inspect_lines < <(podman image inspect --format '{{.Id}}' "$$image_id"); \
	[ "$${#inspect_lines[@]}" -eq 1 ] || { echo 'dev-https-browser-image: local image identity mismatch' >&2; exit 1; }; \
	inspected_id=$${inspect_lines[0]}; \
	if [[ $$inspected_id =~ ^[0-9a-f]{64}$$ ]]; then inspected_id="sha256:$$inspected_id"; fi; \
	[[ $$inspected_id =~ ^sha256:[0-9a-f]{64}$$ ]] && [ "$$inspected_id" = "$$image_id" ] || { echo 'dev-https-browser-image: local image identity mismatch' >&2; exit 1; }; \
	source_after=$$(source_hash) || { echo 'dev-https-browser-image: cannot rehash browser sources' >&2; exit 1; }; \
	[ "$$source_before" = "$$source_after" ] || { echo 'dev-https-browser-image: browser sources changed during build' >&2; exit 1; }; \
	manifest_tmp=$$(mktemp "$$state/.browser-image-manifest.XXXXXX"); \
	chmod 0600 "$$manifest_tmp"; \
	printf 'image_id=%s\nsource_sha256=%s\n' "$$image_id" "$$source_after" >"$$manifest_tmp"; \
	mv -- "$$manifest_tmp" "$$manifest"; \
	manifest_tmp=; \
	printf 'dev-https-browser-image: %s\n' "$$image_id"

dev-https-auth-check: dev-https-status ## Run the trusted local Google auth proof and retain only bounded local evidence

dev-https-transport-check: dev-https-status ## Run the trusted authenticated transport proof and retain only bounded local evidence

dev-https-auth-check dev-https-transport-check:
	@set -Eeuo pipefail; \
	repo=$$(pwd -P); \
	state="$$repo/.dev/native-https"; \
	manifest="$$repo/$(DEV_HTTPS_BROWSER_MANIFEST)"; \
	input="$$state/input"; \
	evidence_root="$$state/evidence"; \
	target='$@'; \
	case "$$target" in \
	dev-https-auth-check) mode=auth; evidence_prefix=google-auth ;; \
	dev-https-transport-check) mode=transport; evidence_prefix=transport ;; \
	*) echo 'dev-https-browser-check: invalid target' >&2; exit 1 ;; \
	esac; \
	uid=$$(id -u); \
	[ -d "$$state" ] && [ ! -L "$$state" ] && [ "$$(realpath -e -- "$$state")" = "$$state" ] || { echo "$$target: invalid state directory" >&2; exit 1; }; \
	[ "$$(stat -c %u "$$state")" = "$$uid" ] && [ "$$(stat -c %a "$$state")" = 700 ] || { echo "$$target: state directory ownership or mode mismatch" >&2; exit 1; }; \
	[ -f "$$manifest" ] && [ ! -L "$$manifest" ] && [ "$$(stat -c %u "$$manifest")" = "$$uid" ] && [ "$$(stat -c %a "$$manifest")" = 600 ] || { echo "$$target: invalid browser image manifest" >&2; exit 1; }; \
	mapfile -t manifest_lines <"$$manifest"; \
	[ "$${#manifest_lines[@]}" -eq 2 ] || { echo "$$target: malformed browser image manifest" >&2; exit 1; }; \
	[[ $${manifest_lines[0]} =~ ^image_id=(sha256:[0-9a-f]{64})$$ ]] || { echo "$$target: malformed browser image ID" >&2; exit 1; }; \
	image_id=$${BASH_REMATCH[1]}; \
	[[ $${manifest_lines[1]} =~ ^source_sha256=([0-9a-f]{64})$$ ]] || { echo "$$target: malformed browser source hash" >&2; exit 1; }; \
	recorded_source=$${BASH_REMATCH[1]}; \
	current_source=$$({ for path in $(DEV_HTTPS_BROWSER_SOURCES); do \
	  [ -f "$$path" ] && [ ! -L "$$path" ] || exit 1; \
	  printf '%s\0' "$$path"; sha256sum -- "$$path"; \
	done; } | sha256sum | awk '{print $$1}') || { echo "$$target: cannot hash browser sources" >&2; exit 1; }; \
	[ "$$current_source" = "$$recorded_source" ] || { echo "$$target: browser sources changed after image build" >&2; exit 1; }; \
	[ -d "$$input" ] && [ ! -L "$$input" ] && [ "$$(realpath -e -- "$$input")" = "$$input" ] || { echo "$$target: invalid CA input directory" >&2; exit 1; }; \
	[ "$$(stat -c %u "$$input")" = "$$uid" ] && [ "$$(stat -c %a "$$input")" = 700 ] || { echo "$$target: CA input ownership or mode mismatch" >&2; exit 1; }; \
	mapfile -t input_entries < <(find "$$input" -mindepth 1 -maxdepth 1 -printf '%f\n'); \
	[ "$${#input_entries[@]}" -eq 1 ] && [ "$${input_entries[0]}" = caddy-root.crt ] || { echo "$$target: CA input must contain one root" >&2; exit 1; }; \
	root="$$input/caddy-root.crt"; \
	[ -f "$$root" ] && [ ! -L "$$root" ] && [ "$$(stat -c %u "$$root")" = "$$uid" ] && [ "$$(stat -c %a "$$root")" = 600 ] || { echo "$$target: invalid Caddy root" >&2; exit 1; }; \
	if [[ -e "$$evidence_root" || -L "$$evidence_root" ]]; then \
	  [ -d "$$evidence_root" ] && [ ! -L "$$evidence_root" ] && [ "$$(realpath -e -- "$$evidence_root")" = "$$evidence_root" ] || { echo "$$target: invalid evidence root" >&2; exit 1; }; \
	  [ "$$(stat -c %u "$$evidence_root")" = "$$uid" ] && [ "$$(stat -c %a "$$evidence_root")" = 700 ] || { echo "$$target: evidence root ownership or mode mismatch" >&2; exit 1; }; \
	else \
	  install -d -m 0700 "$$evidence_root"; \
	fi; \
	evidence=$$(mktemp -d "$$evidence_root/$$evidence_prefix.XXXXXX"); \
	[ "$$(stat -c %u "$$evidence")" = "$$uid" ] && [ "$$(stat -c %a "$$evidence")" = 700 ] || { echo "$$target: evidence directory ownership or mode mismatch" >&2; exit 1; }; \
	if [ "$$mode" = auth ]; then \
	  deploy/dev-https-browser/run.sh "$$image_id" "$$input" "$$evidence"; \
	else \
	  deploy/dev-https-browser/run.sh "$$image_id" "$$input" "$$evidence" transport; \
	fi; \
	current_source_after=$$({ for path in $(DEV_HTTPS_BROWSER_SOURCES); do \
	  [ -f "$$path" ] && [ ! -L "$$path" ] || exit 1; \
	  printf '%s\0' "$$path"; sha256sum -- "$$path"; \
	done; } | sha256sum | awk '{print $$1}') || { echo "$$target: cannot rehash browser sources" >&2; exit 1; }; \
	[ "$$current_source_after" = "$$recorded_source" ] || { echo "$$target: browser sources changed during check" >&2; exit 1; }; \
	printf '%s evidence: %s\n' "$$target" "$$evidence"

test-db-up: ## Start THE one aboutme Postgres container (idempotent; serves the `aboutme` test DB and the `aboutme_dev` native-dev DB; 512 MB cap). One DB container total is the rule — never start a second
	@if ! running_containers="$$(podman ps --format '{{.Names}}|{{.Label "com.docker.compose.project"}}|{{.Label "com.docker.compose.service"}}')"; then \
	  echo "test-db-up: cannot inspect running containers; refusing to start Postgres." >&2; \
	  exit 1; \
	fi; \
	if awk -F '|' '$$1 != "aboutme-test-db" && $$2 == "aboutme" && $$3 == "postgres" { found=1 } END { exit !found }' <<<"$$running_containers"; then \
	  echo "test-db-up: the Compose Postgres service is running; stop it before starting the shared database." >&2; \
	  exit 1; \
	fi; \
	if awk -F '|' '$$1 == "aboutme-test-db" { found=1 } END { exit !found }' <<<"$$running_containers"; then \
	  echo "aboutme-test-db already running; reusing it."; \
	else \
	  podman run -d --rm --name aboutme-test-db --memory 512m -p 127.0.0.1:20432:5432 \
	    -e POSTGRES_USER=aboutme -e POSTGRES_PASSWORD=aboutme_dev -e POSTGRES_DB=aboutme \
	    docker.io/library/postgres:18.4-alpine; \
	fi
	@echo "Waiting for aboutme-test-db to accept connections..."
	@i=0; \
	until podman exec aboutme-test-db pg_isready -h 127.0.0.1 -p 5432 -U aboutme -d aboutme >/dev/null 2>&1 \
	  && (echo > /dev/tcp/127.0.0.1/20432) >/dev/null 2>&1; do \
	  i=$$((i + 1)); \
	  if [ $$i -ge 30 ]; then \
	    echo "test-db-up: aboutme-test-db did not become ready within 30s; see 'podman logs aboutme-test-db'" >&2; \
	    exit 1; \
	  fi; \
	  sleep 1; \
	done; \
	podman exec aboutme-test-db psql -U aboutme -d aboutme -tc \
	  "SELECT 1 FROM pg_database WHERE datname='aboutme_dev'" | grep -q 1 || \
	  podman exec aboutme-test-db psql -U aboutme -d aboutme -c "CREATE DATABASE aboutme_dev"; \
	echo "aboutme-test-db is ready (databases: aboutme, aboutme_dev)."

test-db-down: ## Stop the aboutme Postgres container (check no worker is mid-suite first)
	podman rm -f aboutme-test-db

test-s3-up: ## Start or reuse the one disposable MinIO conformance-test container on 127.0.0.1:20091
	bash scripts/test-s3.sh up

test-s3-down: ## Remove only aboutme-test-s3 and its disposable credential file (check no media suite is active first)
	bash scripts/test-s3.sh down

server-test-integration: ## Run server integration tests (needs test-db-up or TEST_DATABASE_URL)
	@printf '%s\n' 'server-test-integration: go test store integration package'
	@cd apps/server && TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable} \
	  go test ./internal/store/... -run Integration -count=1 -v

semgrep: ## Offline SAST scan with registry packs + project rules (no account needed; quick local check)
	semgrep --config p/golang --config p/gosec --config p/typescript --config p/javascript \
	  --config p/owasp-top-ten --config p/secrets --config p/dockerfile --config p/docker-compose \
	  --config .semgrep.yml --error .

semgrep-ci: ## Connected Semgrep — Code (Pro rules) + Supply Chain (SCA) + Secrets; free for public repos. Needs SEMGREP_APP_TOKEN in the environment. This is what CI runs.
	semgrep ci --code --supply-chain --secrets --no-suppress-errors

sqlc-gen: ## Regenerate the typed data layer (sqlc reads migrations/ for the schema and sql/queries.sql for the queries)
	cd apps/server && sqlc generate

sqlc-check: ## Fail if the generated data layer drifts from migrations/ (uses --porcelain so a NEW untracked generated file is caught too; plain `git diff` misses those)
	cd apps/server && sqlc generate && \
	  { [ -z "$$(git status --porcelain -- internal/store)" ] || \
	    { echo "generated data layer drifts from migrations/ — run 'make sqlc-gen' and commit:"; \
	      git status --porcelain -- internal/store; exit 1; }; }

migrate: ## Apply pending migrations
	cd apps/server && go run ./cmd/migrate

migrate-check: ## Report pending migrations without applying them
	cd apps/server && go run ./cmd/migrate -check

server-migration-test: ## Run the migration harness + migrate CLI (needs test-db-up or TEST_DATABASE_URL)
	@printf '%s\n' 'server-migration-test: go test migration harness and CLI packages'
	@cd apps/server && TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable} \
	  go test ./migrations/... ./cmd/migrate/... -count=1 -v

public-roots-check: ## Verify the closed public-root registry, generated consumers, and source manifests
	node --test packages/publicroots/public-roots.test.mjs
	node scripts/generate-public-roots.mjs --check
	cd apps/server && go test ./internal/publicroots -count=1
	cd apps/web && npx vitest run test/public-roots.generated.test.ts

route-table-test: public-roots-check ## Run the Caddy route-table integration test (needs a caddy binary; set CADDY_BIN or have caddy on PATH)
	cd apps/server && CADDY_BIN=$${CADDY_BIN:-caddy} go test ./internal/routetable/... -run RouteTable -count=1 -v
