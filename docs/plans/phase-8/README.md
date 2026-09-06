# Phase 8 — Privacy lifecycle

Status: **Active** (2026-09-06). Base: `6383dca`.

Use the executing-plans and subagent-driven-development skills under the
repository's two-pass delivery rules. The approved design already authorizes
this phase; ordinary implementation choices below need no new approval.

**Goal:** export portable account data, delete accounts with immediate access
revocation, and run bounded retention and private-media cleanup.

**Architecture:** cookie-authenticated account routes share the existing
session, CSRF, resume projection, and public revocation boundaries. PostgreSQL
owns deletion jobs, lifecycle audit records, and reconciliation progress.
One-shot server commands run the same job code locally and in Phase 10.

**Stack:** Go, pgx/sqlc, PostgreSQL, existing media backends, Nuxt/Vue.

**Authority:** [operations](../../design/operations.md),
[API](../../design/api.md), [security](../../design/security.md),
[data](../../design/data.md), [budgets](../../design/budgets.md), ADRs
[0016](../../adr/0016-transactional-idempotency.md),
[0019](../../adr/0019-private-media-delivery.md), and
[0022](../../adr/0022-public-artifact-revocation.md).

## Tasks and ownership

| Task                               | Deliverable                                           | Owner             | Predecessor                |
| ---------------------------------- | ----------------------------------------------------- | ----------------- | -------------------------- |
| [8.1](task-01-contracts.md)        | Contracts, lifecycle schema, generated data layer     | Integration owner | None                       |
| [8.2](task-02-account-deletion.md) | Account deletion and public drain                     | Sol author        | 8.1 schema                 |
| [8.3](task-03-export.md)           | Portable JSON export                                  | Terra author      | 8.1; 8.2 service interface |
| [8.4](task-04-media-cleanup.md)    | Media deletion and orphan reconciliation              | Sol author        | 8.1 schema                 |
| [8.5](task-05-retention.md)        | Idempotency, session, audit retention                 | Sol author        | 8.1 schema                 |
| [8.6](task-06-settings.md)         | Export and deletion settings                          | Terra author      | 8.1 HTTP contract          |
| [8.7](task-07-integration.md)      | Commands, browser proof, documentation and phase exit | Integration owner | 8.2–8.6                    |

Workers own only the paths named in their task. Shared sources, generated files,
migrations, manifests, Makefile, runtime wiring, and Git belong to the
integration owner. SQL query sources are split into task-owned files under
`apps/server/sql/`; only the owner runs sqlc generation.

## Shared contracts

- `accountapi.New(Dependencies) (*Service, error)` takes Pool, Sessions,
  Coordinator, Media, Projector, Logger, TrustedProxies, PublicOrigin, and Now.
  `RegisterRoutes(*http.ServeMux)` registers GET `/api/v1/me/export`.
  `DeleteHandler() http.Handler` returns the protected DELETE handler. Auth
  retains GET `/me` and accepts the deletion handler through
  `SetAccountDeleteHandler(http.Handler)`.
- `mediacleanup.New(Config) (*Worker, error)` takes Pool, Media, Logger, and
  Now. `DeleteDue(context.Context) (Result, error)` and
  `Reconcile(context.Context, bool) (Result, error)` run one bounded job.
- `privacyretention.New(Config) (*Worker, error)` takes Pool, Logger, Now.
  `ExpireIdempotency(context.Context) (Result, error)` and
  `Retain(context.Context) (Result, error)` run one bounded job.
- Job results expose fixed numeric counters and durations only. Each worker
  holds a dedicated PostgreSQL advisory lock for the entire run, releases it on
  every exit, and joins all object work before returning. No raw database,
  backend, account, key, or credential value enters diagnostics.
- Runtime commands are `idempotency-expiry-sweep`, `media-deletion-sweep`,
  `media-orphan-sweep [--dry-run]`, and `privacy-retention-sweep`. They do not
  start HTTP or Chromium. Hosted schedule activation and alert delivery remain
  Phase 10.

## Delivery rules

- Write and observe a meaningful failing test before implementation.
- At most three heavy processes; use the owner's reserved slots. Full `make ci`
  runs alone. Do not stop or recreate the shared database.
- Root inspects each diff and reruns the key check before a local commit.
- One fresh Sol reviewer authors none of the phase and confirms the named
  invariants in [adversarial coverage](adversarial-coverage.md).
- Exit through [the checklist](exit-criteria.md), full CI and connected scan at
  one unchanged candidate. Delete this plan directory only after its contracts
  have been committed and its evidence moved to living docs.
