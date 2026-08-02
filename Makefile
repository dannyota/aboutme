# aboutme — repo-level targets. App-specific targets arrive with the apps.
.PHONY: help docs-lint docs-fmt generate schema-gen schema-check api-check server-build server-vet server-test server-test-db web-build web-lint web-typecheck web-test dev dev-down test-db-up test-db-down server-test-integration semgrep semgrep-ci sqlc-gen sqlc-check migrate migrate-check migrate-gen server-migration-test data-drift route-table-test

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "%-12s %s\n", $$1, $$2}'

docs-lint: ## Check markdown formatting + lint
	npm run docs:lint

docs-fmt: ## Format + autofix markdown, then re-lint (catches non-convergence)
	npm run docs:fmt
	$(MAKE) docs-lint

generate: schema-gen sqlc-gen ## Regenerate all generated artifacts (schema types, then sqlc)

schema-gen: ## Regenerate schema types (Go/TS)
	cd packages/schema && npm run generate

schema-check: ## Fail if generated types drift from the schema
	cd packages/schema && npm ci && npm test

api-check: ## Lint and test the OpenAPI contract
	npx @redocly/cli lint docs/api/openapi.yaml --config docs/api/redocly.yaml
	npx vitest run --dir docs/api/test

server-build: ## Build the Go API server
	cd apps/server && go build ./...

server-vet: ## Vet the Go API server
	cd apps/server && go vet ./...

server-test: ## Test the Go API server
	cd apps/server && go test ./...

server-test-db: ## Run the auth/store/user DB-backed test suite against a live Postgres (needs test-db-up or TEST_DATABASE_URL); REQUIRE_TEST_DB=1 turns a missing TEST_DATABASE_URL into a failure instead of a silent skip, so a gate run can never pass vacuously
	cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:5432/aboutme?sslmode=disable} \
	  go test ./internal/auth/... ./internal/store/... ./internal/user/... -race -count=1 -v

web-build: ## Build the Nuxt web app
	cd apps/web && npm run build

web-lint: ## Lint the Nuxt web app
	cd apps/web && npm run lint

web-typecheck: ## Typecheck the Nuxt web app
	cd apps/web && npm run typecheck

web-test: ## Test the Nuxt web app
	cd apps/web && npm run test

dev: ## Start the dev stack (podman compose): postgres + server + web + caddy
	podman compose --env-file .env -f deploy/compose.yml up -d --build

dev-down: ## Stop the dev stack and remove containers (keeps the postgres volume)
	podman compose --env-file .env -f deploy/compose.yml down

test-db-up: ## Start a throwaway Postgres for integration tests (publishes 5432)
	podman run -d --rm --name aboutme-test-db -p 127.0.0.1:5432:5432 \
	  -e POSTGRES_USER=aboutme -e POSTGRES_PASSWORD=aboutme_dev -e POSTGRES_DB=aboutme \
	  docker.io/library/postgres:18.4-alpine

test-db-down: ## Stop the throwaway integration-test Postgres
	podman rm -f aboutme-test-db

server-test-integration: ## Run server integration tests (needs test-db-up or TEST_DATABASE_URL)
	cd apps/server && TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:5432/aboutme?sslmode=disable} \
	  go test ./internal/store/... -run Integration -count=1 -v

semgrep: ## Offline SAST scan with registry packs + project rules (no account needed; quick local check)
	semgrep --config p/golang --config p/gosec --config p/typescript --config p/javascript \
	  --config p/owasp-top-ten --config p/secrets --config p/dockerfile --config p/docker-compose \
	  --config .semgrep.yml --error .

semgrep-ci: ## Connected Semgrep — Code (Pro rules) + Supply Chain (SCA) + Secrets; free for public repos. Needs SEMGREP_APP_TOKEN in the environment. This is what CI runs.
	semgrep ci

sqlc-gen: ## Regenerate the typed data layer from sql/ (sqlc)
	cd apps/server && sqlc generate

sqlc-check: ## Fail if the generated data layer drifts from sql/
	cd apps/server && sqlc generate && git diff --exit-code -- internal/store

migrate-gen: ## Author a migration from sql/schema.sql (contributors only; needs Atlas)
	cd apps/server && go run ./cmd/migrate/gen

migrate: ## Apply pending migrations
	cd apps/server && go run ./cmd/migrate

migrate-check: ## Report pending migrations without applying them
	cd apps/server && go run ./cmd/migrate -check

server-migration-test: ## Run the migration harness + migrate CLI (needs test-db-up or TEST_DATABASE_URL; the gen suite self-skips without Atlas)
	cd apps/server && TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:5432/aboutme?sslmode=disable} \
	  go test ./migrations/... ./cmd/migrate/... -count=1 -v

data-drift: ## Fail if sqlc output or migrations drift from sql/schema.sql (needs the pinned Atlas, sqlc, and a disposable DB)
	DATABASE_URL=$${DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:5432/aboutme?sslmode=disable} \
	  bash scripts/check-data-drift.sh

route-table-test: ## Run the Caddy route-table integration test (needs a caddy binary; set CADDY_BIN or have caddy on PATH)
	cd apps/server && CADDY_BIN=$${CADDY_BIN:-caddy} go test ./internal/routetable/... -run RouteTable -count=1 -v
