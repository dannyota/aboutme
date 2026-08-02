# Phase 1 — UAT acceptance catalog

> **Authored by the integration owner before the run. Immutable during a run.**
> The UAT worker executes this catalog and reports results; it must not edit
> this file, product code, tests, snapshots, seeds, or acceptance criteria.
> `BLOCKED` counts as `FAIL`. See `../../CLAUDE.md` ("Design and delivery
> workflow") and `implementation-plan.md` ("UAT report contract").

Phase 1 ships authentication: OAuth login with three providers, server-side
sessions with rotation, CSRF protection, session device management, and explicit
account linking that refuses to merge accounts by email.

## What this catalog can and cannot prove here

**Deliberately out of scope, deferred to the P9A staging smoke:** a browser
round trip against the **real** Google/GitHub/LinkedIn endpoints. No real
provider credentials exist in this environment, the master plan reserves
dedicated provider accounts for staging, and the phase's own rule forbids any
test reaching a real provider. A criterion demanding it would be unsatisfiable
and would therefore have to be recorded `BLOCKED` — so it is not written.

The provider round trip **is** covered, against in-process mock providers that
run go-oidc's real signature/issuer/audience verification and a real GitHub REST
stub. UAT-P1-01 verifies those suites genuinely executed rather than skipping —
which is the specific failure this phase already found once.

Everything else below runs against the real stack over HTTP.

## Run preconditions

Record before the first scenario and include in the report header: commit SHA
(`git rev-parse HEAD`), `git status --short --branch` (must be clean), Go/Node/
podman versions, container image tags, migration head, and the `.env`
fingerprint (names only — **never values**). Every row's evidence must come from
the same commit; if product code changes mid-run, the run is void and restarts
at the new commit.

Start from a genuinely empty database:
`podman compose --env-file .env -f deploy/compose.yml down -v` before UAT-P1-02.
The stack publishes on `${CADDY_HTTP_PORT}` (8080 in rootless environments).

## Acceptance scenarios

| ID        | Scenario                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | Acceptance IDs           |
| --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------ |
| UAT-P1-01 | **The acceptance suites actually ran.** `make test-db-up && make server-test-db`. Record the exact counts of `--- PASS`, `--- SKIP`, `--- FAIL`. **Any `SKIP` fails this row** — a skipped DB-gated case means the phase's acceptance tests did not execute. Separately confirm `REQUIRE_TEST_DB=1` with no DSN **fails** rather than skipping.                                                                                                                                                                                                                                                                                      | all                      |
| UAT-P1-02 | **Stack boots from an empty volume** with the Phase 1 schema: `make dev` after `down -v`; `/healthz` and `/readyz` green; migration head is `00003_add_sessions_rotated_from`; the four auth tables and the `rotated_from` FK + partial unique index exist in the live database.                                                                                                                                                                                                                                                                                                                                                     | —                        |
| UAT-P1-03 | **Unauthenticated surface is closed.** Against the running stack: `GET /api/v1/me`, `GET /api/v1/sessions`, `DELETE /api/v1/sessions`, `DELETE /api/v1/sessions/{uuid}`, `POST /api/v1/auth/logout` each return `401` with error code `session_required`, and each clears rather than sets a session cookie.                                                                                                                                                                                                                                                                                                                         | AC-AUTH-005              |
| UAT-P1-04 | **Provider start endpoints redirect correctly.** `GET /api/v1/auth/{google,github,linkedin}/start` each `302` to that provider's authorize URL carrying `state`, `code_challenge` + `code_challenge_method=S256`, a `redirect_uri` equal to `PUBLIC_ORIGIN` + that provider's callback path, and set a `__Host-oauth-tx` cookie with `Secure; HttpOnly; SameSite=Lax; Path=/; Max-Age=600`. Google and LinkedIn carry a `nonce`; **GitHub must not.**                                                                                                                                                                                | AC-AUTH-003              |
| UAT-P1-05 | **Method discipline.** Every `/api/v1/auth/*/start` and `/callback` returns `405` for `POST` and for `HEAD`, and a `HEAD` creates no `oauth_transactions` row (verify by row count before/after). **Issue HEAD with `curl -I` (`--head`), never `curl -X HEAD`** — the latter sends the method but keeps GET's response parser, so it blocks waiting for a body that HTTP forbids a HEAD response to carry, and appears to hang until the proxy's idle timeout. A hang under `-X HEAD` is a client artifact, not a server defect; if you observe one, re-test with `-I` and with `-H 'Connection: close'` before recording anything. | —                        |
| UAT-P1-06 | **Same-site gate on link/reauth start (DD-C16).** `GET /api/v1/auth/google/start?purpose=link` with `Sec-Fetch-Site: cross-site` → `403 csrf_rejected`; with no `Sec-Fetch-Site`, no `Origin` and no `Referer` → `403` (fail closed); with `Sec-Fetch-Site: same-site` → `403`. In all three cases no `oauth_transactions` row is created. `purpose=login` with the same cross-site headers still succeeds.                                                                                                                                                                                                                          | AC-AUTH-001              |
| UAT-P1-07 | **Link/reauth start rejections are redirects, not JSON (DD-C17).** With no session, `GET /api/v1/auth/google/start?purpose=link` (same-origin headers) `302`s to `PUBLIC_ORIGIN/login` and carries no error code in the query.                                                                                                                                                                                                                                                                                                                                                                                                       | —                        |
| UAT-P1-08 | **CSRF is fail-closed on the real stack (AC-SEC-002).** Using a session established directly in the database (seed a user + session row and present its token), a mutating request with a valid token but a foreign `Origin` → `403 csrf_rejected`; with no `Origin` and no `Referer` → `403`; with a valid token and same-origin → succeeds. The rejection body and headers are byte-identical across the failure classes.                                                                                                                                                                                                          | AC-SEC-002               |
| UAT-P1-09 | **`/me` returns the user and the CSRF token, and the token appears nowhere else.** With a seeded session: `200` with `{data:{user:{id,email,name,avatarKey}, csrfToken, identities:[…]}}`; the token value appears in **no** response header and in **no** `Set-Cookie`.                                                                                                                                                                                                                                                                                                                                                             | AC-AUTH-005              |
| UAT-P1-10 | **Device list, revoke, and logout-everywhere over HTTP.** With two seeded sessions for one user and one for another: `GET /api/v1/sessions` lists only the caller's, flags exactly one `current: true`; `DELETE /api/v1/sessions/{other user's id}` → `404` with a body byte-identical to that of an unknown UUID and of an already-revoked own session; `DELETE /api/v1/sessions` (with recent reauth) → `204` + `Clear-Site-Data: "cookies", "storage"` + cleared cookie, and the other user's session still authenticates afterward.                                                                                              | AC-AUTH-005, AC-AUTH-001 |
| UAT-P1-11 | **Reauth gating bites before any row changes.** With a session whose `reauthenticated_at` is older than 15 minutes: `DELETE /api/v1/sessions/{own id}` and `DELETE /api/v1/sessions` each return `403 reauth_required`, and **zero** session rows change `revoked_at` (verify by querying before and after).                                                                                                                                                                                                                                                                                                                         | AC-AUTH-005              |
| UAT-P1-12 | **Web pages render and point at the real endpoints.** `make web-build` succeeds; the login page's three provider links have `href` exactly `/api/v1/auth/{google,github,linkedin}/start` and are plain anchors; the settings page renders. Record the built output paths as evidence.                                                                                                                                                                                                                                                                                                                                                | —                        |
| UAT-P1-13 | **Contract conformance.** `make api-check` passes; every path this phase added is present in `docs/api/openapi.yaml`; `make schema-check`, `make sqlc-check`, `make data-drift`, `make server-migration-test` all pass.                                                                                                                                                                                                                                                                                                                                                                                                              | —                        |
| UAT-P1-14 | **Full quality gate.** `make server-build server-vet server-test`, `golangci-lint run ./...`, `govulncheck ./...`, `make semgrep`, `make web-lint web-typecheck web-test`, `make docs-lint` — all pass with the exact output recorded.                                                                                                                                                                                                                                                                                                                                                                                               | —                        |
| UAT-P1-15 | **Secrets hygiene.** No provider client secret, session token, CSRF secret, or OAuth handle appears in any committed file, in the server's log output during the run, or in **any evidence or scratch artifact this run produces** — name every path you wrote secret-bearing scratch files to, so this is checkable without forensics. `.env` is not staged, tracked, or quoted. If `*_CLIENT_ID`/`*_CLIENT_SECRET` are empty in this environment, say so: that half of the row is then vacuous, not proven.                                                                                                                        | —                        |
| UAT-P1-16 | **Migration immutability and append-only.** `git diff --name-status <base>...HEAD -- apps/server/migrations` shows only `A` entries for `*.sql` — nothing modified, renamed, or deleted; `00001_extensions.sql` is byte-identical to its state at the base commit; each migration added during this phase appears in exactly one commit and is never edited afterward (`git log --follow` per file); `atlas.sum` verifies. (Corrected 2026-08-02 — the original wording assumed `00002` predated the phase; it did not.)                                                                                                             | —                        |

## Reporting

One row per ID: expected / observed / `PASS` | `FAIL` | `BLOCKED`, each linked
to its evidence (command, exact output, request/response dumps, DB verification
query and result, server log excerpt). Missing evidence, an undisclosed retry,
or any unexplained error output fails that row. **Any** state-changing action
taken during the run is disclosable — seeding, truncation, container restarts, a
discarded attempt — not only a re-issued scenario command. Report the
environment honestly — if a scenario cannot run here, mark it `BLOCKED` with the
reason rather than substituting a weaker check. `BLOCKED` counts as `FAIL`.

Seeding note: rows that need an authenticated session may create users and
session rows directly in the database (the token is the sha256 preimage the
worker chooses). That is a deliberate substitute for a real provider round trip,
which is out of scope here per the section above; state in each such row that
the session was seeded rather than obtained through a login.

## Corrections log

Corrections apply to **future runs only**; the verdicts of completed runs are
never rewritten. If a criterion proves unsatisfiable, the run records `BLOCKED`
and the criterion may be corrected for the next run, with the rationale recorded
here and adjudicated by an Opus 5 reviewer.

### 2026-08-02 — after run 1 (verdict FAIL; evidence pinned at `4f5ef3e`)

Both corrections were adjudicated by the Opus evidence verification of run 1,
which upheld that run with corrections. Run 1's recorded verdicts stand
unchanged.

- **UAT-P1-16 — the criterion was unsatisfiable (integration owner's authoring
  error).** It asked that `00002_add_auth_tables.sql` be byte-identical to its
  state at the phase base commit, but `00002` was created _during_ this phase
  and does not exist at the base — confirmed by `git ls-tree`. Run 1 correctly
  recorded `BLOCKED` and correctly did not edit the criterion. Reworded to test
  the property actually intended, which is what the repo's own append-only CI
  job enforces: no migration file modified, renamed or deleted since the base;
  `00001` byte-identical; each migration added this phase touched by exactly one
  commit; `atlas.sum` verifies.
- **UAT-P1-05 — the criterion did not pin the client invocation.** Run 1
  recorded a `FAIL` reporting that the proxy hung for minutes on every `HEAD`.
  Verification showed this was an artifact of `curl -X HEAD`, which sends the
  method but keeps GET's response parser and blocks awaiting a body that HTTP
  forbids: the same invocation hangs identically with the proxy bypassed
  entirely, and `Connection: close` shows the status line arriving in ~1 ms with
  the byte deficit reported. With `curl -I` every route answers in 1–3 ms. The
  row's substance passed; verification's corrected tally for run 1 is 14 PASS /
  1 FAIL / 1 BLOCKED — still an overall `FAIL`, on UAT-P1-01 alone. The
  criterion now names the correct invocation and tells a future worker how to
  tell a client artifact from a server defect before recording one.

### 2026-08-02 — after run 2 (verdict PASS; evidence pinned at `2d17f77`)

Adjudicated by the Opus evidence verification of run 2, which upheld the run
with corrections. Run 2's recorded verdicts stand unchanged; both items below
apply to **run 3 onward**.

- **UAT-P1-15 — the criterion binds the worker's own artifacts, and run 2 did
  not check them.** The row already says "any evidence artifact this run
  produces", but run 2 checked only committed files, container logs, and its
  report — while its scratch files held session tokens, CSRF secrets and a live
  `csrfToken` in plaintext. Verification re-grepped 18 secret values across the
  captured and live logs and found none leaked, so the verdict held. The row is
  restated to make the artifact scope explicit and to require the worker to name
  where it wrote secret-bearing scratch files, so a verifier can check them
  without forensics. Note also: the provider-secret half of this row is
  **vacuously true** in any environment where `*_CLIENT_ID`/`*_CLIENT_SECRET`
  are empty — say so rather than reporting a clean grep as though it proved
  something.
- **Reporting — a setup-step retry is a disclosable retry.** Run 2 disclosed no
  retries "at the scenario-command level", which was literally true while
  omitting a mid-run `TRUNCATE` of all four auth tables after a seeding script
  aborted. Verification established by catalog forensics that it happened and
  that no verdict depended on pre-truncate state. The Reporting section now
  states that **any** state-changing action taken during the run is disclosable,
  not only a re-issued scenario command.
