# TOTP security mail brief

Role: backend. Model: Sonnet (Codex: `gpt-5.6-terra`).

## Objective and authority

Add the bilingual, secret-free notification vocabulary for TOTP mutations. Read
`AGENTS.md`, the accepted TOTP contract and key-management design, accepted ADR
0049, the auth-mail design, and the v0.4.2 second-factor mail code on main
(`payload.go`, `templates.go`, `outbox.go`, `worker.go`, and
`second_factor_templates_test.go` in `apps/server/internal/authmail`).

## Owned paths

- Modify `apps/server/internal/authmail/payload.go` and
  `apps/server/internal/authmail/payload_test.go`.
- Modify `apps/server/internal/authmail/templates.go` and
  `apps/server/internal/authmail/second_factor_templates_test.go`.
- Modify `apps/server/internal/authmail/outbox.go` and
  `apps/server/internal/authmail/outbox_test.go` only if the closed-kind
  admission table lives there.
- Modify `apps/server/internal/authmail/worker.go` and
  `apps/server/internal/authmail/worker_test.go` only if the closed-kind worker
  table lives there.

Do not edit factor services, cryptography, migrations, OpenAPI, web,
infrastructure, or mail key-ring behavior.

## Required behavior

- Add exactly `totp_added`, `totp_replaced`, and `totp_removed` with existing
  version 2 payload shape and 24-hour expiry.
- Render fixed Vietnamese-first and English-second subject and body copy with
  action, UTC time, and recovery guidance.
- Carry no code, secret, URI, QR data, credential or enrollment ID, key ID,
  nonce, ciphertext, IP, user agent, account ID, or link.
- Preserve every outbox size, encryption, user lock, retry, lease, expiry, and
  terminal rule. Reject malformed or oversized payloads before enqueue.
- Storage owns both database constraint replacements. Lifecycle owns inserting
  each notification in the same transaction as its factor mutation. Test this
  package's closed Go kind vocabulary without editing the migration or service.

## Hosted checks and report

Write failing kind, payload, template, outbox, worker, retry, and terminal tests
first. Do not run local tests, builds, lint, installs, database writes,
browsers, or stacks. Report these as unrun pending exact-candidate GitHub CI:

```bash
(cd apps/server && go test ./internal/authmail)
(cd apps/server && golangci-lint run ./internal/authmail/...)
```

Definition of done: every TOTP event has both locales, stays bounded, carries no
factor material, and follows existing transactional delivery behavior. Report
exact files and hunks, checks and results, skipped commands and reason, and open
items. Do not perform Git operations. Use short plain text with no em dash.
Code, tests, comments, and living docs never cite plans, tasks, phases, or
review findings; cite the design, ADR, or `AC-*` ID instead.
