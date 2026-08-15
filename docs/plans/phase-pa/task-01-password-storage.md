# Task 01 — Add password, token, and email-job storage

**Acceptance:** AC-AUTH-009, AC-AUTH-013, AC-AUTH-014.

**Depends on:** T00 D1–D7 authority.

**Owned paths:** T01 paths in `file-structure.md`. Migration, query source, and
generated sqlc files are one serialized integration-owner window.

## Contract

Migration 00008 implements D3 exactly. It also adds the preflight that stops on
any noncanonical existing email before table creation. Fresh up/down restores
the exact prior schema. This is before `.uat-baseline`, but correction still
uses the normal migration tests and never rewrites an earlier migration.

The generated store contract is exact:

```go
package store

type PasswordQueries interface {
  GetUserForUpdate(context.Context, uuid.UUID) (User, error)
  GetUserByCanonicalEmail(context.Context, string) (User, error)
  GetPasswordCredential(context.Context, uuid.UUID) (PasswordCredential, error)
  GetPasswordCredentialForUpdate(context.Context, uuid.UUID) (PasswordCredential, error)
  UpsertPasswordCredential(context.Context, UpsertPasswordCredentialParams) (PasswordCredential, error)
  GetPasswordRegistrationByEmailForUpdate(context.Context, string) (PasswordRegistration, error)
  GetPasswordRegistrationByDigest(context.Context, []byte) (PasswordRegistration, error)
  GetPasswordRegistrationForUpdate(context.Context, uuid.UUID) (PasswordRegistration, error)
  CreatePasswordRegistration(context.Context, CreatePasswordRegistrationParams) (PasswordRegistration, error)
  DeletePasswordRegistration(context.Context, uuid.UUID) (int64, error)
  GetPasswordResetTokenByUserForUpdate(context.Context, uuid.UUID) (PasswordResetToken, error)
  GetPasswordResetTokenByDigest(context.Context, []byte) (PasswordResetToken, error)
  GetPasswordResetTokenForUpdate(context.Context, uuid.UUID) (PasswordResetToken, error)
  CreatePasswordResetToken(context.Context, CreatePasswordResetTokenParams) (PasswordResetToken, error)
  DeletePasswordResetToken(context.Context, uuid.UUID) (int64, error)
  GetSessionByIDForUpdate(context.Context, uuid.UUID) (Session, error)
  CreateSession(context.Context, CreateSessionParams) (Session, error)
  RevokeAllSessions(context.Context, RevokeAllSessionsParams) (int64, error)
  CreateAuthEmailJob(context.Context, CreateAuthEmailJobParams) (AuthEmailJob, error)
  ListLiveAuthEmailJobKeyIDs(context.Context, time.Time) ([]string, error)
  ClaimAuthEmailJobs(context.Context, ClaimAuthEmailJobsParams) ([]AuthEmailJob, error)
  GetLeasedAuthEmailJobForUpdate(context.Context, GetLeasedAuthEmailJobForUpdateParams) (AuthEmailJob, error)
  MarkAuthEmailJobSent(context.Context, MarkAuthEmailJobSentParams) (int64, error)
  MarkAuthEmailJobTerminal(context.Context, MarkAuthEmailJobTerminalParams) (int64, error)
  RequeueAuthEmailJob(context.Context, RequeueAuthEmailJobParams) (int64, error)
  RequeueExpiredAuthEmailLeases(context.Context, RequeueExpiredAuthEmailLeasesParams) (int64, error)
  CleanupExpiredPasswordRegistrations(context.Context, CleanupExpiredPasswordRegistrationsParams) (int64, error)
  CleanupExpiredPasswordResetTokens(context.Context, CleanupExpiredPasswordResetTokensParams) (int64, error)
  CleanupFinishedAuthEmailJobs(context.Context, CleanupFinishedAuthEmailJobsParams) (int64, error)
}
```

`password_contract.go` contains this interface and
`var _ PasswordQueries = (*Queries)(nil)`. Existing generated methods used by
provider identity/session code remain on `*Queries`; T04–T09 use no raw SQL. An
absent canonical email has no row to lock. New-account creation reads ownership,
attempts user+identity in one transaction, and lets the existing unique email
and provider-subject constraints linearize insert races. A failed constraint
rolls back the whole attempted user before the caller classifies or reloads the
winner.

## TDD cycle

- [ ] Write migration tests first for all table/column/FK/unique/check/index
      clauses, exact expiry arithmetic, scope/state matrix, ciphertext clearing,
      lease pair, exact nullable encryption/outcome fields in each state,
      attempt/state cap, and cleanup indexes.
- [ ] Write a preflight RED with one uppercase, one non-ASCII, one malformed,
      and one overlong existing email. Assert migration fails before any new
      table exists and changes no row.
- [ ] Write live DB RED tests for registration/reset replacement cascading only
      their old unsent jobs, account deletion, exact active-token uniqueness,
      `SKIP LOCKED` disjoint claims, stale-lease requeue, ordered batch 10, and
      cleanup batch 200.
- [ ] Run the focused RED:

  ```sh
  cd apps/server && REQUIRE_TEST_DB=1 \
    TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' \
    go test ./migrations ./internal/store -race -count=1 \
    -run 'TestPasswordAuth(Migration|Preflight|Constraints|Queries|Leases|Cleanup)'
  ```

- [ ] Add the migration and sqlc annotations. Use generated transactional
      queries only; no package-local SQL or string-built query.
- [ ] Regenerate with the repository command. Do not hand-edit generated Go.
- [ ] Add the compile-time query contract and exact row-count/result types.
- [ ] Rerun the focused test, then:

  ```sh
  make sqlc-check server-test-db server-test-integration server-migration-test
  ```

- [ ] Inspect `git diff --check` and confirm the `public.citext` sqlc override
      is unchanged.

## Adversarial checklist

- Every byte/check boundary is tested at limit and limit+1.
- Every invalid outbox kind/state/scope/lease/timestamp combination is rejected.
- Two claimers never own the same job; expired lease recovery never steals a
  live lease.
- Concurrent registration/reset replacements leave one token, one current job,
  and no orphan ciphertext.
- Rollback after each write point leaves credential, token, session, and job
  state unchanged.
- No raw token, plaintext payload, or password enters a database parameter.

## Handoff

Report migration hash, generated symbols, compile contract, live DB RED/GREEN,
fresh up/down result, and rollback/concurrency evidence. Suggested commit:
`feat(auth): add password credential storage`.
