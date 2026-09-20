# Passkey security mail brief

Role: backend. Model: `gpt-5.6-terra`.

## Objective and authority

Add the bilingual, secret-free security notification vocabulary that passkey and
recovery transactions enqueue atomically. Read `AGENTS.md`,
`docs/design/second-factor-authentication.md`,
`docs/design/passkey-second-factor-contract.md`, the auth-mail design, ADR 0025,
and ADR 0048 before editing.

## Owned paths

- Modify `apps/server/internal/authmail/payload.go`.
- Modify `apps/server/internal/authmail/payload_test.go`.
- Modify `apps/server/internal/authmail/templates.go`.
- Create `apps/server/internal/authmail/second_factor_templates_test.go`.
- Modify `apps/server/internal/authmail/outbox.go`.
- Modify `apps/server/internal/authmail/outbox_test.go`.
- Modify `apps/server/internal/authmail/worker.go`.
- Modify `apps/server/internal/authmail/worker_test.go`.

Do not edit factor services, password services, mail crypto or key rings,
migrations, OpenAPI, web files, or infrastructure.

## Required behavior

- Add only the v0.4.2 notification events named by the accepted contract.
- Render Vietnamese and English action, UTC time, recovery guidance, and
  remaining recovery count where required.
- Include no code, credential ID, public key, challenge, client data, signature,
  raw IP, email, account ID, cookie, CSRF token, or pending token.
- Preserve existing encrypted-outbox size, key, retry, expiry, and terminal
  behavior.
- Admit every new event kind through outbox validation and the worker's
  user-scoped delivery lock without changing existing kind behavior.
- Reject malformed or oversize payloads before enqueue.

## Test-first cycle and checks

Start with `git status --short`. Add failing payload, template,
outbox-admission, worker-lock, delivery, retry, and terminal cases before
implementation. Do not run local tests, builds, lint, installs, database writes,
browsers, or development stacks. Report these checks as unrun pending GitHub CI
on the exact candidate:

```bash
(cd apps/server && go test ./internal/authmail)
(cd apps/server && golangci-lint run ./internal/authmail/...)
```

Definition of done: every accepted passkey and recovery event has both locales,
stays within bounds, cannot carry factor material, passes outbox validation, and
uses the worker's user-scoped lock through delivery. Report exact files, checks
and results, skipped checks with command and reason, and open items. Do not
perform Git operations. Use short plain text with no em dash.
