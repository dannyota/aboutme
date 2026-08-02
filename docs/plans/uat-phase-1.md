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

| ID        | Scenario                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | Acceptance IDs           |
| --------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------ |
| UAT-P1-01 | **The acceptance suites actually ran.** `make test-db-up && make server-test-db`. Record the exact counts of `--- PASS`, `--- SKIP`, `--- FAIL`. **Any `SKIP` fails this row** — a skipped DB-gated case means the phase's acceptance tests did not execute. Separately confirm `REQUIRE_TEST_DB=1` with no DSN **fails** rather than skipping.                                                                                                                                                                                         | all                      |
| UAT-P1-02 | **Stack boots from an empty volume** with the Phase 1 schema: `make dev` after `down -v`; `/healthz` and `/readyz` green; migration head is `00003_add_sessions_rotated_from`; the four auth tables and the `rotated_from` FK + partial unique index exist in the live database.                                                                                                                                                                                                                                                        | —                        |
| UAT-P1-03 | **Unauthenticated surface is closed.** Against the running stack: `GET /api/v1/me`, `GET /api/v1/sessions`, `DELETE /api/v1/sessions`, `DELETE /api/v1/sessions/{uuid}`, `POST /api/v1/auth/logout` each return `401` with error code `session_required`, and each clears rather than sets a session cookie.                                                                                                                                                                                                                            | AC-AUTH-005              |
| UAT-P1-04 | **Provider start endpoints redirect correctly.** `GET /api/v1/auth/{google,github,linkedin}/start` each `302` to that provider's authorize URL carrying `state`, `code_challenge` + `code_challenge_method=S256`, a `redirect_uri` equal to `PUBLIC_ORIGIN` + that provider's callback path, and set a `__Host-oauth-tx` cookie with `Secure; HttpOnly; SameSite=Lax; Path=/; Max-Age=600`. Google and LinkedIn carry a `nonce`; **GitHub must not.**                                                                                   | AC-AUTH-003              |
| UAT-P1-05 | **Method discipline.** Every `/api/v1/auth/*/start` and `/callback` returns `405` for `POST` and for `HEAD`, and a `HEAD` creates no `oauth_transactions` row (verify by row count before/after).                                                                                                                                                                                                                                                                                                                                       | —                        |
| UAT-P1-06 | **Same-site gate on link/reauth start (DD-C16).** `GET /api/v1/auth/google/start?purpose=link` with `Sec-Fetch-Site: cross-site` → `403 csrf_rejected`; with no `Sec-Fetch-Site`, no `Origin` and no `Referer` → `403` (fail closed); with `Sec-Fetch-Site: same-site` → `403`. In all three cases no `oauth_transactions` row is created. `purpose=login` with the same cross-site headers still succeeds.                                                                                                                             | AC-AUTH-001              |
| UAT-P1-07 | **Link/reauth start rejections are redirects, not JSON (DD-C17).** With no session, `GET /api/v1/auth/google/start?purpose=link` (same-origin headers) `302`s to `PUBLIC_ORIGIN/login` and carries no error code in the query.                                                                                                                                                                                                                                                                                                          | —                        |
| UAT-P1-08 | **CSRF is fail-closed on the real stack (AC-SEC-002).** Using a session established directly in the database (seed a user + session row and present its token), a mutating request with a valid token but a foreign `Origin` → `403 csrf_rejected`; with no `Origin` and no `Referer` → `403`; with a valid token and same-origin → succeeds. The rejection body and headers are byte-identical across the failure classes.                                                                                                             | AC-SEC-002               |
| UAT-P1-09 | **`/me` returns the user and the CSRF token, and the token appears nowhere else.** With a seeded session: `200` with `{data:{user:{id,email,name,avatarKey}, csrfToken, identities:[…]}}`; the token value appears in **no** response header and in **no** `Set-Cookie`.                                                                                                                                                                                                                                                                | AC-AUTH-005              |
| UAT-P1-10 | **Device list, revoke, and logout-everywhere over HTTP.** With two seeded sessions for one user and one for another: `GET /api/v1/sessions` lists only the caller's, flags exactly one `current: true`; `DELETE /api/v1/sessions/{other user's id}` → `404` with a body byte-identical to that of an unknown UUID and of an already-revoked own session; `DELETE /api/v1/sessions` (with recent reauth) → `204` + `Clear-Site-Data: "cookies", "storage"` + cleared cookie, and the other user's session still authenticates afterward. | AC-AUTH-005, AC-AUTH-001 |
| UAT-P1-11 | **Reauth gating bites before any row changes.** With a session whose `reauthenticated_at` is older than 15 minutes: `DELETE /api/v1/sessions/{own id}` and `DELETE /api/v1/sessions` each return `403 reauth_required`, and **zero** session rows change `revoked_at` (verify by querying before and after).                                                                                                                                                                                                                            | AC-AUTH-005              |
| UAT-P1-12 | **Web pages render and point at the real endpoints.** `make web-build` succeeds; the login page's three provider links have `href` exactly `/api/v1/auth/{google,github,linkedin}/start` and are plain anchors; the settings page renders. Record the built output paths as evidence.                                                                                                                                                                                                                                                   | —                        |
| UAT-P1-13 | **Contract conformance.** `make api-check` passes; every path this phase added is present in `docs/api/openapi.yaml`; `make schema-check`, `make sqlc-check`, `make data-drift`, `make server-migration-test` all pass.                                                                                                                                                                                                                                                                                                                 | —                        |
| UAT-P1-14 | **Full quality gate.** `make server-build server-vet server-test`, `golangci-lint run ./...`, `govulncheck ./...`, `make semgrep`, `make web-lint web-typecheck web-test`, `make docs-lint` — all pass with the exact output recorded.                                                                                                                                                                                                                                                                                                  | —                        |
| UAT-P1-15 | **Secrets hygiene.** No provider client secret, session token, CSRF secret, or OAuth handle appears in any committed file, in the server's log output during the run, or in any evidence artifact this run produces. `.env` is not staged, tracked, or quoted.                                                                                                                                                                                                                                                                          | —                        |
| UAT-P1-16 | **Migration immutability and append-only.** `00001` and `00002` are byte-identical to their state at the phase base commit; `00003` is the only addition; `atlas.sum` verifies.                                                                                                                                                                                                                                                                                                                                                         | —                        |

## Reporting

One row per ID: expected / observed / `PASS` | `FAIL` | `BLOCKED`, each linked
to its evidence (command, exact output, request/response dumps, DB verification
query and result, server log excerpt). Missing evidence, an undisclosed retry,
or any unexplained error output fails that row. Report the environment honestly
— if a scenario cannot run here, mark it `BLOCKED` with the reason rather than
substituting a weaker check. `BLOCKED` counts as `FAIL`.

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
