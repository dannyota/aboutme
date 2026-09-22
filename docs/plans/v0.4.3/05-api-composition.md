# TOTP API and composition brief

Role: backend. Model: `gpt-5.6-terra`.

## Objective and authority

Expose the verified TOTP lifecycle through configuration, capabilities, route
composition, OpenAPI, generated web types, readiness, and the bounded key
rotation command. Read `AGENTS.md`, the accepted TOTP contract, accepted ADR
0049, the API and budget designs, and every verified backend report.

## Owned paths

- Modify `apps/server/internal/config/config.go` and
  `apps/server/internal/config/config_test.go`.
- Create `apps/server/internal/config/totp.go` and
  `apps/server/internal/config/totp_test.go`.
- Modify `apps/server/internal/api/capabilities.go` and
  `apps/server/internal/api/capabilities_test.go`.
- Modify `apps/server/cmd/server/main.go` and
  `apps/server/cmd/server/main_test.go`.
- Modify `apps/server/cmd/server/second_factor.go` and
  `apps/server/cmd/server/second_factor_test.go`.
- Create `apps/server/cmd/server/totp_keys.go` and
  `apps/server/cmd/server/totp_keys_test.go`.
- Modify `docs/api/openapi.yaml`, `docs/api/test/openapi.test.ts`, and
  `docs/api/test/second-factor.test.ts`.
- Modify generated `apps/web/app/api/generated/openapi.ts` through
  `make api-gen`.

Do not edit behavior services, SQL, migrations, mail, web pages and tests,
manifests, infrastructure, workflows, or design. Never hand-edit the generated
client.

## Required behavior

- Parse `TOTP_ENROLLMENT_ENABLED` with default false and closed Boolean syntax.
- Require exact active key configuration and accept either no previous fields or
  a complete distinct previous pair. Decode each value as exactly 32 bytes from
  canonical unpadded base64url without exposing it in errors.
- Register exact verification, start, completion, and removal routes. Add
  required capability, state, and pending method fields without changing passkey
  routes.
- Enforce strict JSON, exact media, body, field, cookie, CSRF, Origin, cache,
  status, error, and one-time plaintext shapes in OpenAPI and handlers.
- Keep exact `Cache-Control: no-store` on capabilities, password login and
  reauthentication, every v0.4.2 factor route, TOTP verification, removal, and
  state. Use exact `no-store, no-transform` only on TOTP enrollment start and
  completion.
- At startup, read at most three distinct stored key IDs without decrypting all
  rows. Make readiness report the fixed unavailable result while the lifecycle
  latch is set. Wire bounded validate-and-clear behavior through composition.
- Add `/usr/local/bin/server totp-key-reencrypt` for manager-run bounded
  re-encryption and count. It reports only counts and internal row IDs and obeys
  the 200-row, 10,000-row, and 30-minute limits.
- Prove flag-off completion consumes matching enrollment and stores nothing.
  Verification, removal, state, recovery, and existing passkeys remain active.

## Hosted checks and report

Write failing config, exact-cache, capability, route, composition, readiness
lifecycle, command, and API contract tests first. Regenerate the client only
from OpenAPI. Do not run local tests, builds, lint, installs, database writes,
browsers, or stacks. Report these as unrun pending exact-candidate GitHub CI:

```bash
make api-check
(cd apps/server && go test ./cmd/server ./internal/api ./internal/config)
make server-build server-vet server-test
(cd apps/server && golangci-lint run ./cmd/server/... ./internal/api/... ./internal/config/...)
```

Definition of done: server, OpenAPI, and generated client expose one matching
strict contract, startup and readiness fail closed, and enrollment defaults off.
Report exact files, generated diff, checks and results, skipped commands and
reason, and open items. Do not perform Git operations. Use short plain text with
no em dash.
