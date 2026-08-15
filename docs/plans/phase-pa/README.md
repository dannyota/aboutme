# Phase PA — Password authentication implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add email-and-password registration, verification, login,
reauthentication, add/change, and reset while preserving provider identities,
opaque sessions, exact CSRF/Origin rules, and no-oracle behavior.

**Architecture:** `users` stays the account authority, `identities` stays the
provider-subject authority, and a user has zero or one password credential.
Password work is memory-bounded and happens outside short database transactions.
Every session issuer and credential mutation serializes on the same user lock.
Verification/reset tokens are random bearer values stored only as digests.
Transactional email jobs store only AES-256-GCM ciphertext and a bounded worker
delivers them through SES or a local capture sender.

**Tech Stack:** Go 1.26.6, PostgreSQL 18, sqlc, Argon2id, `golang.org/x/crypto`
0.55.0, Unicode NFC, AWS SDK for Go v2 SES `service/sesv2` 1.66.6, OpenAPI 3.1,
Nuxt 4, Vue 3, TypeScript 6.0.3, Caddy, Playwright 1.62.1, and Podman.

## Global Constraints

- The approved input is
  [`../password-authentication-design.md`](../password-authentication-design.md).
  Task 00 first reconciles Approved v4 and accepts ADR 0025. No password code
  lands while the provider-only authorities still disagree.
- Phase PA starts only after Phase 4 and Phase 5A are integrated. It does not
  edit their dirty UI, generated client, migration, route registry, Caddy, or
  native-harness windows in parallel.
- A returning provider subject resolves before any email fetch. Email is used
  only to create a new provider account. Provider linking never reads, stores,
  compares, or displays provider email.
- Canonical account email is lowercase ASCII addr-spec with exact D1 bounds.
  Accounts are never merged automatically by email. A provider identity stays
  unique to one account.
- Every provider/password login and ADR 0015 rotation successor is inserted
  while holding the user lock. A session linearized before reset is revoked by
  reset; one linearized after it is a distinct post-reset login/use.
- Every user-bound password transaction locks user, credential, reset token when
  applicable, then sessions. Password hashing, breach lookup, and mail
  encryption happen before the transaction.
- Password add/change creates a new unrelated current session, revokes the old
  current session and all other sessions in the same transaction, and sets the
  new cookie only after commit. ADR 0015 predecessor grace is not used.
- Password reset revokes all sessions and never creates a session. Verification
  creates no session. Both successful pages use uniform copy that reveals no
  account/provider race outcome.
- Registration and forgot-password account states have exact identical 202 bytes
  and cookie behavior. Unknown, provider-only, and wrong-password login states
  have exact identical 401 bytes and cookie behavior and perform one admitted
  Argon2id verification.
- Passwords, canonical emails, raw or full-digest tokens, hashes, provider
  claims, session values, encryption material, and decrypted mail never enter
  logs, metrics labels, traces, errors, panic text, evidence, or analytics.
- Mail enqueue/encryption failure is a pre-commit dependency failure and rolls
  back the account mutation. Only delivery failure after commit is retried and
  never rolls back a credential change.
- The local capture process is loopback-only, bounded, secret-authenticated, and
  absent from production composition and public routing. Real SES, DNS, and AWS
  configuration require separate human approval after local UAT.
- A worker holds the authoritative scope and job locks through the bounded mail
  sender handoff. Token replacement therefore orders before send and cancels it,
  or orders after a completed handoff; there is no stale-check/send gap.
- Each task has one author who writes RED first and owns its adversarial cases.
  There is no per-task reviewer. One non-author performs the ADR 0024 phase
  review after completion records are committed.
- Workers edit only exclusive paths and never use Git. The integration owner
  serializes Approved v4/ADR/budgets/registry, migration/sqlc, OpenAPI/generated
  client, dependencies/config/composition, lifecycle/Make, and final records.
- At most three heavy checks run at once. Full `make ci` and connected
  `make scan` run alone on one unchanged candidate commit.

## Plan documents

- [Decisions](decisions.md) freezes numeric bounds, storage state, lock order,
  interfaces, routes, mail delivery, and browser evidence.
- [File structure](file-structure.md) assigns every implementation path once.
- [Integration handoffs](integration-handoffs.md) freezes producer/consumer
  interfaces and shared owner windows.
- [Exit criteria](exit-criteria.md) is the unchanged-candidate phase gate.

## Task index

| Task                                      | Deliverable                                                   | Acceptance                     | Owner                   |
| ----------------------------------------- | ------------------------------------------------------------- | ------------------------------ | ----------------------- |
| [00](task-00-authority-routes-budgets.md) | Approved v4/ADR/budgets/traceability and v5 public roots      | AUTH-008/015, OPS-020          | Integration owner       |
| [01](task-01-password-storage.md)         | Migration, exact constraints, lock/lease queries, sqlc        | AUTH-009/013/014               | Integration owner       |
| [02](task-02-openapi-client.md)           | Seven password operations, `/me.hasPassword`, generated TS    | AUTH-009/011/012/013/015       | Integration owner       |
| [03](task-03-password-primitives.md)      | Canonical email, policy, blocklist/HIBP, Argon2id, tokens     | AUTH-008/010, SEC-005          | Password-core author    |
| [04](task-04-encrypted-outbox.md)         | AEAD key ring, typed payloads, transaction enqueue/authority  | AUTH-014, SEC-005              | Outbox author           |
| [05](task-05-provider-session-fencing.md) | Subject-first provider resolver and user-fenced session issue | AUTH-008/011/012/013           | Provider/session author |
| [06](task-06-mail-delivery-capture.md)    | Leased worker, SES sender, bounded local capture core         | AUTH-014, OPS-020, SEC-005     | Mail author             |
| [07](task-07-password-rate-policies.md)   | Route limits and outcome-aware login failure limiter          | AUTH-015                       | Rate-policy author      |
| [08](task-08-password-http-service.md)    | Password service/routes and all transactional race evidence   | AUTH-009–015, SEC-002/005      | Password-service author |
| [09](task-09-local-lifecycle.md)          | Config/composition, native and HTTPS mail-capture lifecycle   | AUTH-014, OPS-020              | Integration owner       |
| [10](task-10-password-pages.md)           | Login/register/verify/forgot/reset Nuxt flows                 | AUTH-009/011/013/016           | Web-auth author         |
| [11](task-11-password-settings.md)        | Password status, add/change, password reauth settings UI      | AUTH-012/016                   | Web-settings author     |
| [12](task-12-native-https-uat.md)         | One-process HTTPS password/provider/session proof             | AUTH-008–016, OPS-020, SEC-005 | Integration owner       |

## Frozen waves

Phase PA starts from the integrated Phase 4 and Phase 5A candidate. Shared owner
windows never overlap another task that reads or writes the same surface.

| Wave | Tasks                 | Start condition                        | Heavy limit                                              |
| ---- | --------------------- | -------------------------------------- | -------------------------------------------------------- |
| W0a  | 00                    | P4 and P5A complete; plan is committed | Owner alone; authorities and route registry              |
| W0b  | 01                    | T00 lands                              | Owner alone; migration/sqlc and one database             |
| W0c  | 02                    | T01 lands                              | Owner alone; OpenAPI/generated client                    |
| W1   | 03, 04                | T01/T02 interfaces are frozen          | Two disjoint Go checks                                   |
| W2   | 05, 06, 07            | T03/T04 dependencies land              | At most three disjoint Go checks; at most two live DB    |
| W3   | 08                    | T03–T07 land                           | Password service alone; live races                       |
| W4   | 09, 10                | T08 and P4/P5A final UI land           | Two heavy checks; owner lifecycle and web pages disjoint |
| W5   | 11                    | T10 shared web primitives land         | Web settings alone                                       |
| W6   | 12                    | T00–T11 focused gates pass             | One Playwright process; no other browser/build           |
| W7   | Records, review, exit | T00–T12 reports accepted               | Records, one fresh review, then candidate gates          |

## Dispatch and completion

The integration owner commits this approved plan and marks Phase PA queued
behind P4/P5A. After both predecessor phases close, the owner changes Phase PA
to active and dispatches T00. Each task brief includes its task file, integrated
base commit, authorities, acceptance IDs, owned paths, exact check, and report
format.

After T12, the owner updates phase state, master plan, traceability,
architecture, and runbook evidence and commits those records locally. One fresh
non-author then reviews the complete candidate and names password, token,
session fencing, CSRF, Origin, enumeration, rate limiting, provider identity,
encrypted outbox, SES, capture isolation, and concurrency invariants. Findings
return to the owning author and the same reviewer confirms fixes. The owner then
runs the exit checklist, `make ci`, and connected `make scan` on one unchanged
candidate before push.
