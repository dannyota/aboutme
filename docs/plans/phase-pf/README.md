# Phase PF — v1 entry experience implementation plan

Status: **Planned** (2026-09-02). Dispatchable once the integration owner
commits this plan.

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** A visitor sees what aboutme is and can sign in or register; v1 offers
password sign-in only; a developer gets one seeded account; the public
application has no operator surface.

**Architecture:** One Go environment flag keeps provider login off and leaves
its routes unregistered. One unauthenticated `GET /api/v1/capabilities` read
tells the web which optional surfaces exist, fetched client-side only. The
landing page is static Nuxt SSR. A fixture-pattern Go command seeds the native
development database.

**Tech Stack:** Go 1.27.0, PostgreSQL 18, OpenAPI 3.1 with `openapi-fetch`, Nuxt
4, Vue 3, TypeScript 6.0.3, vitest with `@nuxt/test-utils`, Playwright 1.62.1 in
the pinned trusted-browser image, Podman.

**Spec:** [`design.md`](design.md) (approved 2026-09-02). Every task argues from
it.

## Global constraints

- `PROVIDER_LOGIN_ENABLED` accepts exactly `true`, `false`, or blank; blank is
  `false`; any other value fails startup without echoing the value.
- `GET /api/v1/capabilities` returns
  `{"data":{"providerLogin":<bool>,"agentAccess":<bool>}}`, both required,
  `security: []`, `Cache-Control: no-store`.
- Nuxt never fetches Go during server-side rendering. Capabilities are read
  client-side after hydration; a failed read means both flags are false.
- The seed user is `5d000000-0000-4000-8000-000000000001`,
  `dev@aboutme.invalid`, name `Dev User`, password `aboutme-dev-password-1`. The
  seed resume is `5d000000-0000-4000-8000-000000000002`, title `Sample resume`,
  the current-version `full` fixture, private.
- The seed refuses any database not named `aboutme_dev` and never runs in
  Compose or cloud.
- Landing copy is exactly the spec's D5 text. No copy names an unshipped feature
  (PDF download, realtime refresh).
- The native HTTPS harness sets `PROVIDER_LOGIN_ENABLED=true`; the native HTTP
  stack and Compose leave it unset.
- ADR 0027 is the provider-login flag; ADR 0028 is no operator surface.
- Workers never touch Git. Root `Makefile`, scripts, OpenAPI, generated client,
  `.env.example`, and design/ADR/traceability pages are integration-owner paths;
  a worker reports the needed shared edit.
- Commit messages use Conventional Commits and never mention agents.

## Task index

| Task                                   | Deliverable                                                      | Acceptance                    | Owner             |
| -------------------------------------- | ---------------------------------------------------------------- | ----------------------------- | ----------------- |
| [00](task-00-authorities.md)           | ADRs 0027/0028, design amendments, traceability rows, env name   | Rows PLANNED                  | Integration owner |
| [01](task-01-provider-login-flag.md)   | `PROVIDER_LOGIN_ENABLED` config and provider route gating        | AC-AUTH-017 (Go)              | Go author         |
| [02](task-02-capabilities-endpoint.md) | OpenAPI operation, generated client, Go handler, composition     | AC-AUTH-018                   | Integration owner |
| [03](task-03-dev-seed.md)              | `dev-seed` command, native script and Makefile wiring, runbook   | AC-OPS-021                    | Go author         |
| [04](task-04-app-shell-landing.md)     | `AppChrome` by auth state, landing page, placeholder removal     | AC-OPS-022 (web)              | Web author        |
| [05](task-05-capabilities-gating.md)   | `useCapabilities`, login and settings gating, 404 fix            | AC-AUTH-017 (web); AC-SEC-006 | Web author        |
| [06](task-06-harness-entry-proof.md)   | Harness flag, `entry.spec.ts`, `make dev-https-entry-check`      | AC-OPS-022                    | Integration owner |
| [07](task-07-records-exit.md)          | Traceability PROVEN, architecture, roadmap, review, exit, delete | All                           | Integration owner |

## Waves

| Wave | Tasks      | Start condition          | Heavy limit                               |
| ---- | ---------- | ------------------------ | ----------------------------------------- |
| W0   | 00         | Plan committed           | Owner alone; docs only                    |
| W1   | 01, 03, 04 | T00 lands                | Two Go checks and one web check           |
| W2   | 02         | T01 lands                | Owner alone; OpenAPI and generated client |
| W3   | 05         | T02 and T04 land         | One web check                             |
| W4   | 06         | T01, T03, T04, T05 land  | One Playwright process, no other browser  |
| W5   | 07         | T00–T06 reports accepted | Records, one fresh review, gates          |

T01, T03, and T04 touch disjoint paths: `internal/config` and `internal/auth`;
`cmd/dev-seed`; `apps/web/app` shell and landing. T02 needs T01's config field.
T05 needs T02's generated `Capabilities` type and T04's `AppChrome` (the login
page and settings page tests mount with the new shell). T06 needs everything.

## Dispatch and completion

The integration owner commits this plan and dispatches T00. Each brief names the
task file, the integrated base commit (verify it with `git log -1`),
authorities, acceptance IDs, owned paths, the exact check, and the report
format: files changed, RED and GREEN outputs, unrun checks with reasons, and any
shared-file edit the owner must make.

After T06, the owner runs T07: records, one fresh non-author review that names
the route-absence, capabilities-no-store, seed-database-guard, no-operator-
surface, and no-SSR-fetch invariants, the exit checklist, `make ci`, and
connected `make scan` on one unchanged candidate, then deletes this directory in
the exit commit.
