# Task 07 — Implement password route and failure rate policies

**Acceptance:** AC-AUTH-015.

**Depends on:** T00 budgets; T03 canonical email; ADR 0018 existing limiter.

**Owned paths:** T07 paths in `file-structure.md`.

## Contract

Implement D6 independently from handler constants. Tests read the exact budget
rows and assert production parity. HMAC construction uses the injected 32-byte
rate secret and canonical email. No API returns or logs the key.

T07 exports the exact D6 `RateDecision` and `PasswordRatePolicies` constructor
and methods. T08 consumes `*PasswordRatePolicies`; it does not assemble or name
individual limiter stores. Register/forgot, verify/reset, and account-mutation
pairs share their respective class budget exactly as D6 states.

`PasswordFailureLimiter.State` observes without mutation. `RecordFailure`
atomically increments/creates the bucket and returns exhausted on failure 10 and
later with ceiling-rounded positive `RetryAfterSeconds`. The first failure sets
the fixed 15-minute expiry and later failures do not extend it. `ClearSuccess`
removes only that email key. Expired entries are reclaimed; active entries are
never evicted; overflow uses one shared failure bucket.

## TDD cycle

- [ ] Write budget-derived REDs for all D6 IP/email/account combinations at
      limit and limit+1, independent key isolation, rolling expiry, exact
      Retry-After, 10,000 active entries, expired reclamation, and overflow.
- [ ] Add HMAC REDs for domain separation, distinct secrets/emails, canonical
      case equivalence, fixed 32-byte output, invalid secret rejection, and no
      raw-email string representation.
- [ ] Add outcome REDs: failures 1–9 report unexhausted, 10+ exhausted; state
      observation changes nothing; success clears; another email remains; later
      failures do not extend the first-failure expiry; concurrent record
      increments are exact; clock rollback fails closed.
- [ ] Add an integration fake showing an exhausted wrong attempt still invokes
      verification once, while a correct attempt verifies once, clears, and
      succeeds. Per-IP rejection may stop before body/hash.
- [ ] Run expected RED:

  ```sh
  cd apps/server && go test ./internal/auth ./internal/api -race -count=1 \
    -run 'TestPassword(Rate|Failure|EmailHMAC)'
  ```

- [ ] Compose existing ADR 0018 primitives or add the smallest generic bounded
      outcome primitive. Do not alter existing OAuth/resume limiter semantics.
- [ ] Run the minimal GREEN focused tests and:

  ```sh
  make server-build server-vet
  ```

## Adversarial checklist

- Raw/mixed-case email never enters keys/logs/metrics.
- Active-key flood stays bounded without evicting a protected key.
- Concurrent success/failure has a deterministic linearization point; a
  committed success clears prior debt but cannot erase a later failure.
- Correct-password success remains possible after failure exhaustion; wrong
  attempts remain CPU/IP bounded and return exact 429 at threshold.
- Existing OAuth/resume rate tests stay byte- and policy-identical.

## Handoff

Report `RateDecision`, `NewPasswordRatePolicies`, every route-admission and
login-outcome method, exact numeric-source proof, overflow behavior, concurrency
linearization, and focused checks. Suggested commit:
`feat(auth): add password rate policies`.
