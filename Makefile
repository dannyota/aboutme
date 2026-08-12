# aboutme — repo-level targets. App-specific targets arrive with the apps.
# bash, not sh: test-db-up's host-side port probe uses /dev/tcp, a bash
# builtin that under dash never succeeds and turns the readiness loop into a
# guaranteed 30s failure.
SHELL := /bin/bash
.PHONY: help ci check scan tools-check operational-test hooks-install docs-lint docs-fmt generate schema-gen schema-check api-gen api-check server-build server-vet server-test server-test-db web-build web-lint web-typecheck web-test dev dev-down test-db-up test-db-down server-test-integration semgrep semgrep-ci sqlc-gen sqlc-check migrate migrate-check server-migration-test route-table-test dev-native dev-native-down dev-native-status dev-native-logs

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "%-16s %s\n", $$1, $$2}'

ci: ## Full local non-security gate, including DB-backed suites; integration owner runs it before integration
	bash scripts/ci.sh

check: ## Fast gate — the same checks minus the web build and DB-backed suites; for the inner development loop
	bash scripts/ci.sh --fast

scan: ## Batched security scan for a phase gate: Semgrep (SAST + Supply Chain SCA + secrets) then gitleaks over full history
	bash scripts/scan.sh

tools-check: ## Verify local gate tools match .tool-versions (limit with ARGS="ci", "scan", "dev", or tool names)
	bash scripts/check-tool-versions.sh $(ARGS)

operational-test: ## Test local CI, scan, toolchain, Compose guard, and native-status contracts without real services
	bash -n scripts/check-tool-versions.sh scripts/check-migrations-append-only.sh scripts/ci.sh scripts/scan.sh scripts/dev-native.sh scripts/test/ci-failure-propagation-test.sh scripts/test/ci-lifecycle-test.sh scripts/test/ci-scan-adversarial-test.sh scripts/test/makefile-safety-test.sh scripts/test/migration-append-only-test.sh scripts/test/scan-engine-error-test.sh scripts/test/toolchain-contract-test.sh scripts/test/workflow-safety-test.sh
	scripts/test/ci-failure-propagation-test.sh
	scripts/test/ci-lifecycle-test.sh
	scripts/test/ci-scan-adversarial-test.sh
	scripts/test/makefile-safety-test.sh
	scripts/test/migration-append-only-test.sh
	scripts/test/scan-engine-error-test.sh
	scripts/test/toolchain-contract-test.sh
	scripts/test/workflow-safety-test.sh

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
	cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable} \
	  go test ./internal/auth/... ./internal/store/... ./internal/user/... ./internal/resume/... -race -count=1 -v

web-build: ## Build the Nuxt web app
	cd apps/web && npm run build

web-lint: ## Lint the Nuxt web app
	cd apps/web && npm run lint

web-typecheck: ## Typecheck the Nuxt web app
	cd apps/web && npm run typecheck

web-test: ## Test the Nuxt web app
	cd apps/web && npm run test

dev: ## HTTP image/network smoke and self-hosting stack; not for daily development
	@if ! running_containers="$$(podman ps --format '{{.Names}}')"; then \
	  echo "make dev: cannot inspect running containers; refusing to start Compose." >&2; \
	  exit 1; \
	fi; \
	if grep -qx aboutme-test-db <<<"$$running_containers"; then \
	  echo "make dev: aboutme-test-db is running; only the integration owner may stop it after every live-DB worker is idle." >&2; \
	  exit 1; \
	fi; \
	podman compose --env-file .env -f deploy/compose.yml up -d --build

dev-down: ## Stop the compose stack and remove containers (keeps the postgres volume)
	podman compose --env-file .env -f deploy/compose.yml down

dev-native: ## Daily development — native server, web, and Caddy on http://localhost:20080 against the shared database
	bash scripts/dev-native.sh up

dev-native-down: ## Stop the native dev stack (leaves the shared aboutme-test-db container running)
	bash scripts/dev-native.sh down

dev-native-status: ## Native dev stack liveness and ports; non-zero exit if anything is down or crashed
	bash scripts/dev-native.sh status

dev-native-logs: ## Tail native dev stack logs (make dev-native-logs ARGS="-f web")
	bash scripts/dev-native.sh logs $(ARGS)

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

server-test-integration: ## Run server integration tests (needs test-db-up or TEST_DATABASE_URL)
	cd apps/server && TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable} \
	  go test ./internal/store/... -run Integration -count=1 -v

semgrep: ## Offline SAST scan with registry packs + project rules (no account needed; quick local check)
	semgrep --config p/golang --config p/gosec --config p/typescript --config p/javascript \
	  --config p/owasp-top-ten --config p/secrets --config p/dockerfile --config p/docker-compose \
	  --config .semgrep.yml --error .

semgrep-ci: ## Connected Semgrep — Code (Pro rules) + Supply Chain (SCA) + Secrets; free for public repos. Needs SEMGREP_APP_TOKEN in the environment. This is what CI runs.
	semgrep ci

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
	cd apps/server && TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable} \
	  go test ./migrations/... ./cmd/migrate/... -count=1 -v

route-table-test: ## Run the Caddy route-table integration test (needs a caddy binary; set CADDY_BIN or have caddy on PATH)
	cd apps/server && CADDY_BIN=$${CADDY_BIN:-caddy} go test ./internal/routetable/... -run RouteTable -count=1 -v
