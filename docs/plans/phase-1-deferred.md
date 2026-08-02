# Phase 1 — deferred items and follow-up scope

Recorded at the phase gate so these survive the working ledger, which is
git-ignored and dies with the phase worktree. Each item was raised by a task
review, the whole-branch review, or the adversarial review, triaged, and
deliberately not fixed in Phase 1.

## P1.1 — a bounded follow-up, before P2A takes the migration head

Scheduled as one unit because the pieces are cheap only while the auth router
and the phase's migration head are still open, and because three of them are one
gap rather than three deferrals.

| #   | Item                                                                                                                                                                                                                                                                                                                                                                                     | Why it is not "later"                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | **Auth-route rate limits + `oauth_transactions` reaper + `/start` rejection logging.** `GET /api/v1/auth/{provider}/start` is unauthenticated, writes one transaction row per request, is bounded only by the global 300/min per-IP default, is never reaped, and its rejections emit no log record. `?error=email_already_registered` is also an account-existence oracle at that rate. | The master plan makes each phase the owner of its own routes' policies; P1 is the owning phase for auth routes. The `RateLimit` middleware already supports composite account+IP keys.                                                                                                                                                                                                                                                                                                                                |
| 2   | **Make link/reauth start a CSRF-protected `POST`** returning the authorize URL, instead of DD-C16's same-site check on a `GET`.                                                                                                                                                                                                                                                          | DD-C16 is verified fail-closed, but it is a second authorization primitive parallel to the CSRF machinery, and it depends on `Sec-Fetch-Site` or a same-origin `Referer` surviving the edge. P8-sec's job is security headers, and a global `Referrer-Policy: no-referrer` is standard hardening — it would break linking silently for browsers without Fetch Metadata. A POST rides the existing chain, needs no DD-C17 companion, and P11's bearer client gets it free. Cost rises once P5B owns the settings page. |
| 3   | **Typed reason constants for funnel logs** instead of free-text `reason` strings.                                                                                                                                                                                                                                                                                                        | P9A is the first contact with a real IdP. A systematic misconfiguration (clock skew, issuer mismatch, redirect_uri) presents to every user as `auth_failed`, and the only operator signal is free text in a Warn log.                                                                                                                                                                                                                                                                                                 |
| 4   | **Session rotation's single-delivery orphan.** A successor's raw token is delivered on exactly one response and is never stored (only its sha256), so a lost response orphans the session: the predecessor dies 60s later and the live successor is unreachable. Over ~90 rotations in a 90-day lifetime, a 0.1% per-response loss rate orphans ~9% of sessions.                         | Not a security defect — a wrong failure shape for a product whose thesis is "don't make people log in again". The remedy compatible with the current schema is to defer the predecessor's death until the successor is first used.                                                                                                                                                                                                                                                                                    |

## Forward-binding decisions

**DD-C9 is load-bearing for session survival, not just hydration.**
Authenticated `/api/v1` fetches are `server: false`. If one runs during SSR, it
rotates the session and delivers the successor cookie into Nitro, which discards
it — killing the user's session 60 seconds later, deterministically, on every
page load past the 24h rotation age. **P4's `useApi` must hard-code
`server: false`** and should be the only authenticated fetch path, ideally
enforced by an ESLint restriction while there are still few call sites.

**Mobile (P11) constraints, decided now so P11 extends this model instead of
forking it.** (a) DD-C16 rejects native clients by construction — a Custom Tab
sends `Sec-Fetch-Site: none`; P11 adds a _parallel bearer-authenticated_ start
and never relaxes the web one. (b) CSRF must be structured as "auth mode decided
once at `RequireSession`; CSRF required iff mode == cookie" — never "skip CSRF
if an `Authorization` header is present", which is the bypass shape. (c)
Rotation delivers credentials only via `Set-Cookie` today; the transport needs
an abstraction, the lifecycle does not. (d) Mobile sessions land in the same
`sessions` table with a `kind` column, or `GET /sessions` — the user-facing
device list, and the recovery control DD-C14 exists to make trustworthy — will
lie by omission.

**The `sessions_rotated_from_key` unique index stays.** Dropping it would
preserve the option of minting a second successor per predecessor, but that
remedy multiplies live credentials from one lineage _and_ reopens the
`:one`-with-a-non-unique-predicate ambiguity that caused a real defect during
this phase. The better remedy for the orphan (defer predecessor death until the
successor is first used) is fully compatible with keeping the index. Recorded so
the decision is not re-argued from scratch.

**DD-C6's Content-Type gate is not the load-bearing CSRF control** —
exact-Origin plus the synchronizer token are. When P2B adds media upload,
`multipart/form-data` is permitted for that route on those grounds, rather than
base64-in-JSON or an ad-hoc carve-out.

## The unstated assumption, now stated

**The entire auth model is a pure function of one exact origin string.**
`__Host-` cookies carry no `Domain`, CSRF compares `Origin` by exact equality,
DD-C16 and DD-C17 derive from it, and the `redirect_uri` registered with three
providers is built from it. Consequences that were never written down:

- **`www` and the apex are different cookie jars.** A visitor who signs in on
  one and is later served the other is logged out, and only one is registered
  with the providers. The deployment must redirect one to the other before
  launch.
- **Plain HTTP cannot authenticate at all.** `Secure` is unconditional. A
  self-hoster on a LAN hostname over HTTP cannot sign in — a real product
  decision that was never made explicitly.
- **Safari rejects `Secure` cookies over `http://localhost`**, so local
  development in Safari cannot sign in, and the failure funnels into the
  deliberately opaque `auth_failed`.

This fails closed everywhere at once, and every failure mode is opaque by
design. It belongs in the spec and as a PI/P9A gate item.

## Correctly deferred to a named phase

- `oauth_transactions` and dead-session GC; dead rows are invisible to
  `GET /sessions` _and_ unrevokable through the API, so a sweep is the only
  thing that will ever reclaim them → **P8-priv**.
- Session-lifecycle audit logging → **P8-priv** (which already owns the 180-day
  retention for a log that does not yet exist; its emission points are all in
  `internal/auth`). Needs a traceability row naming the owner.
- The `users` → `sessions` cascade firing the self-FK's `SET NULL` against rows
  in the same delete set is untested → **the phase that lands account
  deletion**.
- A `session_revoked` NOTIFY so long-lived SSE streams close on revocation,
  logout-everywhere, and account deletion; `last_seen_at` is only touched by
  `Authenticate`, so a session whose sole activity is one held-open stream
  idle-expires while in use → **P6A**, which already builds the LISTEN/NOTIFY
  hub.
- The `csrf_rejected` retry must reuse the same `Idempotency-Key`; `mutate()`
  does this correctly today by rebuilding from the same options, but by accident
  rather than by contract → **P2B/P4**.
- `oidctest` harness fidelity (text/plain token errors, single-`aud`,
  `/authorize` 404, wall-clock signing) — test-only, no production surface.
- Web polish: a11y labels, `useHead` titles, sticky link banner, raw ISO
  timestamps → **P5B**.

## Housekeeping, unowned

- **`internal/user` has zero production importers.** It survives for its
  schema-drift assertions. Either delete it and move those assertions into
  `internal/store`, or route user creation through it — before P8-priv's
  `DELETE /me` has to choose.
- **AC-AUTH-003's no-OIDC guard is file-shaped.** It parses exactly
  `internal/auth/github.go`, but GitHub's callback path now also runs through
  the shared funnel and `provider_http.go`. Neither imports go-oidc today, so
  nothing is broken — but the guard no longer covers what it claims. Widen it to
  the package minus `google.go`/`linkedin.go`.
- Untested branches on paths P2B and P8-priv will hit first: `RevokeForUser`'s
  already-revoked and unknown-id arms, the I2/I3 unique-violation recovery arms,
  the exact-instant grace boundary, nil-`AvatarKey` round-trip, citext
  case-insensitivity.
- Stale doc comments in a codebase where long doc comments _are_ the design
  record: `handlers.go`'s "GitHub follows in a later task"; the
  closed-vocabulary comment missing `reauth_required`;
  `redirectStartCSRFRejected`'s name (it does not redirect); "same-transaction
  re-read" (it is a pool, not a transaction); the `math/rand` suppression citing
  reasoning that is the opposite of its own.
- go-oidc's discovery and JWKS fetches are time-bounded but not size-bounded;
  truncated and malformed bodies are indistinguishable in operator logs.
