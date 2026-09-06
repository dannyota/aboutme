# Task 8.2 — Transactional account deletion

**Owner:** Sol author. **Acceptance:** AC-PRIV-001/004, AC-MEDIA-003/006,
AC-PUB-001/002/003, AC-SEC-002. **Predecessor:** 8.1 schema. Read the phase
index, operations/security/data design and ADRs 0019/0022, plus existing
resumeapi transition/recovery code.

**Owned paths:** `internal/accountapi/{service,delete,recovery}*.go`,
`internal/auth/account_routes.go`, `internal/auth/handlers.go`, affected auth
route tests, `internal/auth/password_service.go`,
`internal/auth/provider_identity.go`, their focused race tests, and
`sql/account_deletion.sql` under `apps/server/`. No generated files, migrations,
root wiring, Git, or other worker paths.

## Behavior

DELETE `/api/v1/me` is cookie-only, bodiless and queryless. Reject
preconditions, schema and idempotency headers; it is an account operation, not a
resume mutation. Enforce session, exact Origin/CSRF, route limit, and recent
reauthentication. Recheck the concrete live session and reauth inside the
transaction after locking the account.

Read at most three owned resumes and discovery state. Reserve the global
discovery fence, then all resume fences in UUID order, and drain once within
five seconds before starting the deletion transaction. Preserve existing slug,
public-state, user and session SQL lock order. Under the user lock, re-read and
compare the complete resume set and revisions. A concurrent create or stale plan
rolls back and reopens unchanged fences; retry preflight at most three times,
then return `409 account_changed`.

Validate every exact photo key, insert slug tombstones, enqueue media jobs,
advance discovery once, insert the account-deleted audit event, and delete the
user in one transaction. Add a canonical-email advisory lock shared by password
registration/verification, provider account creation and deletion. Take it
before registration and user row locks. Purge the same-email pending
registration before deleting the user. Cascades remove sessions, identities,
password state, agent grants/codes/tokens, resumes and idempotency state.
Tombstone owner becomes null; cleanup state survives. No object I/O occurs in
the transaction.

Classify commit outcomes. Definite rollback reopens unchanged state. Ambiguous
commit keeps admission closed while an independent bounded read proves the
account, tombstones, queue and discovery outcome. Unresolved proof fails
readiness closed. A proven commit retires every resume fence.

Return exact bodyless 204 only after commit. Clear the session and OAuth
transaction cookies and send `Clear-Site-Data: "cookies", "storage"`. Bodiless
errors are closed JSON codes: 400 request_invalid, 401 session_required, 403
csrf_rejected/reauth_required, 409 account_changed, 429 rate_limited, 503
account_unavailable; unexpected failures are opaque 500.

## Author checks

- [ ] Write the auth/CSRF/body/header matrix and observe failure.
- [ ] Implement the smallest route/service slice.
- [ ] Test three resumes including private, live and discoverable; exact jobs,
      tombstones, all cascade tables, foreign-account preservation and audit.
- [ ] Test stale session/reauth while waiting, reset/rotation/grant/create
      races, malformed and cross-resume photo keys, queue/audit insertion
      failure, drain timeout, definite rollback,
      committed/rolled-back/unresolved ambiguous commit, canceled request and
      global lock ordering.
- [ ] Confirm cached HTML, JSON, photo, PDF, share image, discovery and public
      SSE cannot serve old state after success.
- [ ] Run from `apps/server`:
      `go test -race -count=1 ./internal/accountapi ./internal/auth`.

Report failing-test evidence, changed paths, exact checks, and shared edits
needed. The owner regenerates SQL and reruns the key checks.
