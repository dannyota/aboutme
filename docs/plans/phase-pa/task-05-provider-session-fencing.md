# Task 05 — Make provider resolution and session issuance transactional

**Acceptance:** AC-AUTH-008, AC-AUTH-011, AC-AUTH-012, AC-AUTH-013.

**Depends on:** T01 lock/session queries; T03 canonical email.

**Owned paths:** T05 paths in `file-structure.md`.

## Contract

Implement D4/D5. `SessionManager.IssueTx` is the only primitive that constructs
a fresh login session. Its caller must already hold the exact user row lock. The
compatibility `Issue` wrapper begins a transaction, locks the user, calls
`IssueTx`, and commits. ADR 0015 rotation keeps its separate admission update,
then inserts its successor only in a transaction that locks the user and
rechecks the predecessor as live. Tests inject a lock probe; production never
trusts a caller assertion without the qtx lock query in the same transaction.

Split provider resolution into stable-subject and new-account paths:

```go
type ProviderSubject struct { Provider Provider; Subject string }
type NewProviderAccount struct {
  Subject ProviderSubject
  VerifiedEmail string
  Name string
  AvatarKey *string
}

func (s *Service) resolveReturningProviderTx(
  context.Context, *store.Queries, ProviderSubject,
) (store.User, bool, error)
func (s *Service) createProviderAccountTx(
  context.Context, *store.Queries, NewProviderAccount,
) (store.User, error)
```

The provider adapter fetches registration email only after the returning lookup
reports absent. Google requires a nonempty email with `email_verified=true`;
LinkedIn requires the optional `email_verified` claim to be present and true;
GitHub selects a `primary && verified` `/user/emails` row. All three pass the
selected value through D1. Link uses only `ProviderSubject` and never constructs
`NewProviderAccount`. `/me` reads `hasPassword` through one EXISTS query and
retains deterministic identity order.

## TDD cycle

- [ ] Add session REDs proving `IssueTx` needs a locked existing user, creates
      exact existing token/CSRF/session fields, uses `rotated_from = NULL`, and
      rolls back all rows on commit/entropy/insert failure.
- [ ] Add deterministic pause REDs: provider issue pauses after user lock; reset
      contender cannot acquire the lock; after provider commit reset revokes
      that session. Reverse order proves issue sees reset-completed user state
      and creates only a post-reset session.
- [ ] Add `TestSessionRotation_ResetFence` with both orderings: rotation insert
      before reset is revoked by reset; reset before the successor transaction
      makes the predecessor recheck fail and creates no successor. Preserve the
      separate admission update and parked deadline on insert failure.
- [ ] Add provider REDs that count email endpoint/claim access. Returning
      Google/GitHub/LinkedIn subjects must access email zero times. New subjects
      require verified canonical email. Link with a different/unverified email
      accesses email zero times and succeeds by subject.
- [ ] Add live transaction REDs for new email collision, same-subject collision,
      user insert then identity failure, and concurrent same provider subject.
      Assert no orphan user and no cross-account subject movement.
- [ ] Add `TestProviderConcurrentFirstLogin_ReReadsSubject`. Require both
      same-subject/same-email callbacks to succeed after the loser rolls back
      and re-reads the subject: one user, one identity, and two independently
      issued sessions. A login-only unique race never returns the
      authenticated-link conflict.
- [ ] Add `/me` REDs for `hasPassword` false/true while preserving identity
      order and not exposing provider email.
- [ ] Run expected RED:

  ```sh
  cd apps/server && REQUIRE_TEST_DB=1 \
    TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' \
    go test ./internal/auth -race -count=1 \
    -run 'Test(ProviderSubjectFirst|ProviderAccountTransaction|ProviderConcurrentFirstLogin_ReReadsSubject|SessionIssueFence|SessionRotation_ResetFence|ProviderIssueResetRace|MeHasPassword)'
  ```

- [ ] Implement the subject-first adapter flow and qtx resolver. Preserve all
      OAuth state/PKCE/nonce/issuer/audience/callback/cookie checks.
- [ ] Implement `IssueTx` and route every provider fresh-session call through
      the user lock.
- [ ] Route ADR 0015 successor insertion through the user-lock transaction and
      predecessor liveness recheck without combining or rolling back its prior
      admission update.
- [ ] Add `hasPassword` with the T02 generated contract.
- [ ] Run the minimal GREEN: focused tests, the existing auth adversarial suite,
      then:

  ```sh
  make server-build server-vet server-test
  ```

## Adversarial checklist

- Returning subject never depends on changed/missing/malformed provider email.
- New subject cannot create against an owned email and cannot orphan a user.
- Link ignores email and rejects only stable-subject ownership conflict.
- Reset/session-issue interleavings end with either a pre-reset session that
  reset revoked or a post-reset session created after the reset fence—never a
  session inserted across reset commit.
- Existing OAuth redirect/error bytes, session cookies, reauth, rate, and
  no-oracle tests remain unchanged except the documented new-account rule. ADR
  0015 delivery grace and one-successor behavior remain exact.

## Handoff

Report exported session/resolver signatures, provider email-access counters, all
race outcomes, `/me` producer shape, and focused/full checks. Suggested commit:
`refactor(auth): fence provider identity and session creation`.
