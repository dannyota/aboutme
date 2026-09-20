# Passkey API and composition brief

Role: backend. Model: `gpt-5.6-terra`.

## Objective and authority

Expose the verified v0.4.2 passkey behavior through configuration, capabilities,
route composition, OpenAPI, and generated web types. Read `AGENTS.md`,
`docs/design/second-factor-authentication.md`,
`docs/design/passkey-second-factor-contract.md`, `docs/design/api.md`,
`docs/design/budgets.md`, ADR 0048, and every verified backend report before
editing.

## Owned paths

- Modify `apps/server/internal/config/config.go`.
- Create `apps/server/internal/config/passkey.go`.
- Create `apps/server/internal/config/passkey_test.go`.
- Modify `apps/server/internal/api/capabilities.go`.
- Modify `apps/server/internal/api/capabilities_test.go`.
- Modify `apps/server/cmd/server/main.go`.
- Modify `apps/server/cmd/server/main_test.go`.
- Create `apps/server/cmd/server/second_factor.go`.
- Create `apps/server/cmd/server/second_factor_test.go`.
- Modify `docs/api/openapi.yaml`.
- Modify `docs/api/test/openapi.test.ts`.
- Create `docs/api/test/second-factor.test.ts`.
- Modify `apps/web/app/api/generated/openapi.ts` through `make api-gen`.

Do not edit behavior packages, web pages, web tests or composables, migrations,
source manifest, infrastructure, or design sources. Never hand-edit the
generated API client.

## Required behavior

- Parse `PASSKEY_ENROLLMENT_ENABLED` with default false and the accepted strict
  environment syntax.
- Reject startup with enabled enrollment when `PUBLIC_ORIGIN` cannot produce the
  accepted HTTPS or localhost relying-party configuration.
- Register every v0.4.2 state, pending, passkey, recovery, and removal route.
  Register no TOTP implementation route.
- Capabilities expose only the accepted v0.4.2 fields and never account state.
- OpenAPI pins request and response bounds, cookies, CSRF, cache headers, status
  codes, error codes, formats, nullable fields, and one-time secret behavior.
- Compose entropy, clocks, storage, sessions, mail, limiters, and WebAuthn
  configuration through injectable dependencies. Do not log secret inputs.

## Test-first cycle and checks

Start with `git status --short`. Add failing config, capability, route,
composition, focused second-factor, and API contract tests. Regenerate the
client only from OpenAPI. The top manager must grant normal build and package
lanes before these commands run:

```bash
make api-check
(cd apps/server && go test ./cmd/server ./internal/api ./internal/config)
make server-build server-vet server-test
(cd apps/server && golangci-lint run ./cmd/server/... ./internal/api/... ./internal/config/...)
```

Definition of done: the generated client and server expose the same accepted
v0.4.2 contract, enrollment defaults off, and startup fails closed for an
invalid enabled WebAuthn origin. Report exact files, generated diff, checks and
results, skipped checks with command and reason, and open items. Do not perform
Git operations. Use short plain text with no em dash.
