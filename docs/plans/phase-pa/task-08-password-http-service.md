# Task 08 — Implement password service, routes, and transactional races

**Acceptance:** AC-AUTH-009–015, AC-SEC-002, AC-SEC-005.

**Depends on:** T01–T07.

**Owned paths:** T08 paths in `file-structure.md`. T08 starts after T05 releases
the shared `handlers.go` surface.

## Contract

```go
type PasswordServiceOptions struct {
  Pool *store.Pool
  Queries *store.Queries
  Sessions *SessionManager
  Policy *password.Policy
  Hasher *password.Hasher
  Outbox *authmail.Outbox
  Limits *PasswordRatePolicies
  PublicOrigin string
  Clock func() time.Time
  Entropy io.Reader
  Logger *slog.Logger
  TrustedProxies api.TrustedProxies
}
func NewPasswordService(PasswordServiceOptions) (*PasswordService, error)
func (s *PasswordService) RegisterRoutes(*http.ServeMux)
```

The constructor fails on any nil/invalid dependency. T09 calls the independent
`PasswordService.RegisterRoutes`; T08 does not edit OAuth
`Service.RegisterRoutes`. Route constants match T02. All handlers use a
password-only strict JSON chain so existing OAuth/resume media-type mappings
remain unchanged.

### Operation order

1. **Register:** method/cache/IP rate → media/body/strict JSON → exact Origin →
   canonical name/email/password → email rate → policy/HIBP/hash → token/job ID
   and encrypted payload → transaction; owned email discards prepared private
   material and returns generic 202, unowned replaces registration+job. No user
   or session.
2. **Verify:** decode/hash token → IP rate → read preflight → transaction lock
   registration; insert user+credential atomically or consume without password
   if unique email is now owned. Always 204 for a live token and no session.
3. **Login:** route checks → canonical email/password bounds → IP rate →
   credential snapshot or dummy → one admitted verify → wrong failure accounting
   or short transaction user/credential recheck + `IssueTx`; changed snapshot
   retries verify once; success clears debt after commit. Optional rehash is
   prepared outside and committed only against the verified snapshot.
4. **Forgot:** route checks → canonical email/email rate → prepare token and
   encrypted payload before ownership lookup → transaction; only an existing
   credential gets replacement token+job. Every valid account state returns the
   identical 202.
5. **Reset:** route checks → token decode/digest/IP rate → live-token preflight
   before password/HIBP/hash → transaction in D4 lock order; replace credential,
   consume token, revoke all sessions, enqueue notification. No session/cookie.
6. **Password reauth:** live session → CSRF → Origin → media/body/JSON → account
   rate → credential snapshot/verify → user/credential/session lock and exact
   recheck → touch only current `reauthenticated_at`.
7. **PUT password:** live session → CSRF → Origin → media/body/JSON → account
   rate → policy/HIBP/hash + notification seal → D4 transaction recheck; add or
   replace credential, create fresh current session, revoke all old sessions,
   enqueue notification; commit then set cookie.

Every prepared secret buffer is short-lived and zeroed where Go semantics make
that meaningful; no security claim relies on guaranteed garbage-collector
erasure.

## TDD cycle

- [ ] Add exact handler-inventory REDs for seven routes and T02 operation
      parity.
- [ ] Add a generated route matrix for wrong method, repeated/missing/wrong
      media type, empty/4,096/4,097 body, malformed/trailing/duplicate/unknown
      JSON, null/wrong scalar, Origin/Referer/repeated header, session, CSRF,
      reauth, and every route limiter. Assert no DB/hash/HIBP/outbox call before
      its authorized stage.
- [ ] Add byte-equality REDs across registration owned/provider-only/unknown;
      forgot credential/provider-only/unknown; login unknown/provider-only/
      wrong. Compare status, ordered headers, exact body bytes, cookies, hash
      call count/parameters, and state snapshots.
- [ ] Add happy-path live DB REDs for every operation, exact cookie flags,
      `/me.hasPassword`, no verification/reset auto-login, add/change fresh
      session metadata, all-old-session rejection, and notification enqueue.
- [ ] Add deterministic race REDs named exactly:

  ```text
  TestPasswordLogin_ResetFence
  TestPasswordLogin_ChangeSnapshotRetry
  TestPasswordReset_ChangeLockOrder
  TestPasswordReset_DuplicateToken
  TestPasswordVerify_DuplicateToken
  TestPasswordVerify_ProviderSignupRace
  TestPasswordProvider_SubjectCollision
  TestPasswordProviderIssue_ResetFence
  TestPasswordChange_LostResponseRevokesOldSession
  TestPasswordOutboxFailure_RollsBackMutation
  ```

- [ ] Add token expiry/replacement boundary, rehash CAS, current-session revoke,
      unknown hash encoding, hash/HIBP/admission failure, entropy/encryption/DB/
      commit ambiguity, no-write, and secret-leak REDs.
- [ ] Run the expected focused RED:

  ```sh
  cd apps/server && REQUIRE_TEST_DB=1 \
    TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' \
    go test ./internal/auth -race -count=1 \
    -run 'TestPassword(Register|Verify|Login|Forgot|Reset|Reauth|Change|Provider|Outbox|Route)'
  ```

- [ ] Implement strict decoding, closed mapping, service operations,
      lock/recheck loops, and route registration. Never hash or call HIBP in a
      transaction.
- [ ] Run the minimal GREEN, then the high-contention race regex 20 times:

  ```sh
  cd apps/server && REQUIRE_TEST_DB=1 \
    TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' \
    go test ./internal/auth -race -count=20 \
    -run 'TestPassword(Login_ResetFence|Reset_ChangeLockOrder|Verify_ProviderSignupRace|ProviderIssue_ResetFence)'
  ```

- [ ] Run:

  ```sh
  make server-build server-vet server-test
  ```

## Adversarial checklist

- Each handler order is proved with call counters and state snapshots.
- No public response/cookie/timing-class work chosen before expensive work
  reveals owned/unknown/provider-only/wrong state.
- Every race has two explicit orderings and a deterministic pause, not sleep.
- A former password cannot create a session after reset/change commit. Reset
  cannot leave a session issued across its user fence.
- Enqueue failure rolls back; post-commit delivery failure does not affect the
  credential. Commit ambiguity fails closed and never repeats mutation.
- Response/log/header/metric/panic sentinels cover password, email, raw/full
  digest token, PHC, session/CSRF, provider claim, plaintext, key, and raw
  dependency error.

## Handoff

Report route inventory, constructor/signatures, exact response hashes,
operation-order proof, all named races, 20-run result, full server checks, and
remaining deployment work. Suggested commit:
`feat(auth): add password authentication routes`.
