# TOTP API and composition brief

Role: backend. Model: `gpt-5.6-terra`.

## Objective and authority

Expose the verified TOTP lifecycle through configuration, capabilities, route
composition, OpenAPI, generated web types, the TOTP key-health signal, and the
bounded key rotation command. Read `AGENTS.md`, the accepted TOTP contract and
key-management design, accepted ADR 0049, the API and budget designs, and every
verified backend report.

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
- Require `TOTP_ACTIVE_KEY` and accept an absent or present `TOTP_PREVIOUS_KEY`.
  Decode each as exactly 32 bytes from canonical unpadded base64url without
  exposing it in errors. Fail startup when both derive the same key ID. Accept
  no key-ID variables.
- Register exact verification, start, completion, and removal routes. Add
  required capability, state, and pending method fields without changing passkey
  routes.
- Enforce strict JSON, exact media, body, field, cookie, CSRF, Origin, cache,
  status, error, and one-time plaintext shapes in OpenAPI and handlers.
- Keep exact `Cache-Control: no-store` on capabilities, password login and
  reauthentication, every v0.4.2 factor route, TOTP verification, removal, and
  state. Use exact `no-store, no-transform` only on TOTP enrollment start and
  completion.
- Wire the lifecycle key-health check at startup and every five minutes. Do not
  edit `apps/server/internal/publicstate/readiness.go` or add a TOTP input to
  `/readyz`. Add a `cmd/server` test that `/readyz` stays ready while a stored
  key ID is unknown and TOTP verification returns 503.
- Add `/usr/local/bin/server totp-key-reencrypt` for manager-run bounded
  re-encryption and count. It reports only counts and internal row IDs and obeys
  the 200-row, 10,000-row, and 30-minute limits.
- Prove flag-off completion consumes matching enrollment and stores nothing.
  Verification, removal, state, recovery, and existing passkeys remain active.

## Hosted checks and report

Write failing config, exact-cache, capability, route, composition, key-health,
green-readiness, command, and API contract tests first. Regenerate the client
only from OpenAPI. Do not run local tests, builds, lint, installs, database
writes, browsers, or stacks. Report these as unrun pending exact-candidate
GitHub CI:

```bash
make api-check
(cd apps/server && go test ./cmd/server ./internal/api ./internal/config)
make server-build server-vet server-test
(cd apps/server && golangci-lint run ./cmd/server/... ./internal/api/... ./internal/config/...)
```

Definition of done: server, OpenAPI, and generated client expose one matching
strict contract, a malformed ring fails startup, a key failure stays confined to
TOTP, and enrollment defaults off. Report exact files, generated diff, checks
and results, skipped commands and reason, and open items. Do not perform Git
operations. Use short plain text with no em dash. Code, tests, comments, and
living docs never cite plans, tasks, phases, or review findings; cite the
design, ADR, or `AC-*` ID instead.
