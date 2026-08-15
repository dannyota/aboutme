# Task 03 — Implement canonical email and password primitives

**Acceptance:** AC-AUTH-008, AC-AUTH-010, AC-SEC-005.

**Depends on:** T00 D1/D2 budgets; T01 preflight contract; T02 wire bounds.

**Owned paths:** T03 paths in `file-structure.md`. The integration owner alone
performs the direct-dependency and root-sum subwindow.

## Contract

Implement D1 and D2 with these closed errors:

```go
var (
  ErrEmailInvalid       = errors.New("account email invalid")
  ErrPasswordLength     = errors.New("password length invalid")
  ErrPasswordCommon     = errors.New("password is common")
  ErrPasswordBreached   = errors.New("password is breached")
  ErrBreachUnavailable  = errors.New("breach service unavailable")
  ErrHashAdmission      = errors.New("password hash admission unavailable")
  ErrHashInvalid        = errors.New("password hash invalid")
  ErrTokenInvalid       = errors.New("password token invalid")
)
```

Errors contain no input or dependency text. `Policy.CheckNew` checks raw byte
cap → NFC/code points → blocklist → HIBP. Login normalizes/bounds but does not
call blocklist/HIBP. Hash parsing completes bounds and canonical syntax checks
before Argon2 allocation.

## TDD cycle

- [ ] Write table REDs for every D1 valid/invalid email grammar boundary,
      lowercase output, ASCII byte count, and a corpus shared with migration
      preflight fixtures.
- [ ] Add blocklist manifest/generator tests first: source commit/path/license,
      SHA-256, 99,840 lines, deterministic sorted unique digest output, no
      plaintext runtime artifact, and mutation detection.
- [ ] Freeze `digests.bin` as ASCII magic `ABMEBL01`, big-endian uint32 count
      99,839, then that many strictly increasing 32-byte SHA-256 digests. The
      generator reads only the vendored pinned source, applies NFC to each line,
      rejects invalid UTF-8/NUL, skips the source's empty line, deduplicates
      normalized values, and supports `--check` without network access or source
      mutation.
- [ ] Write password REDs for 1,024/1,025 raw bytes, NFC expansion/composition,
      14/15/128/129 code points, astral code points, spaces/case preservation,
      controls allowed as password data, and exact common/breached taxonomy.
- [ ] Write an HIBP server test that records only request method/path/headers
      and proves five-character prefix, Add-Padding, no redirect, timeout, 128
      KiB response cap, parsing, 256-prefix/16 MiB LRU/TTL, cache-on-outage, and
      miss fail-closed.
- [ ] Write PHC REDs for exact parameters, independent Argon2 result, malformed
      and malicious encodings, rehash, dummy path, entropy failure, and no
      allocation before bounds.
- [ ] Write deterministic admission REDs: two run, 16 wait, waiter 17 fails,
      cancellation releases one slot, and every result/failure releases once.
- [ ] Write token REDs for exact 32-byte entropy, 43-character spelling,
      canonical decode, digest, malformed inputs, and constant-time comparator
      use.
- [ ] Run the expected RED:

  ```sh
  cd apps/server && node --test \
    internal/password/blocklist/generate.test.mjs
  node internal/password/blocklist/generate.mjs --check
  go test ./internal/accountemail ./internal/password -race -count=1
  ```

- [ ] Owner pins dependencies exactly:

  ```sh
  cd apps/server && go get golang.org/x/crypto@v0.55.0
  ```

  Inspect all `go.mod`, `go.sum`, and `go.work.sum` changes. Do not run tidy
  unless generation proves it is required.

- [ ] Implement the smallest closed parser/policy/cache/hasher/admission/token
      code. Inject HTTP client, clock, entropy, and worker hook; no ambient
      network/time/random in tests.
- [ ] Rerun the focused suite and:

  ```sh
  make server-build server-vet
  ```

- [ ] Run the resource benchmark alone and record wall time/peak RSS without
      changing D2 values to make the test pass.

## Adversarial checklist

- Unicode/ASCII/byte/code-point and PHC integer-overflow matrices are complete.
- HIBP never receives full digest/password and rejects redirects, bad
  content-type, malformed lines, controls, oversized/truncated bodies, and
  duplicate conflicting suffixes.
- Queue overflow/account cancellation cannot leak whether a credential exists.
- Dummy and real verification share admission and Argon2 parameters.
- Logs/errors/panics/metrics contain none of the supplied password, email,
  digest, PHC string, salt, result, or HIBP body sentinels.

## Handoff

Report exact exported signatures/errors, dependency diff, blocklist provenance
and generated hash, focused RED/GREEN, resource evidence, and any benchmark
uncertainty. Suggested commit: `feat(auth): add password security primitives`.
