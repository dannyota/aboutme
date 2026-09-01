# Task 03 — OAuth primitives: tokens, codes, PKCE, redirect, scopes

**Acceptance:** AC-MCP-001, AC-MCP-003, AC-MCP-004 (primitive clauses).

**Depends on:** T00 M1–M4 decisions; T01 digest widths.

**Owned paths:** T03 paths in `file-structure.md`.

## Contract

Implement M2/M3/M4 primitives with the frozen signatures in
`integration-handoffs.md` and these closed errors:

```go
var (
  ErrTokenInvalid      = errors.New("oauth token invalid")
  ErrCodeInvalid       = errors.New("oauth code invalid")
  ErrRedirectInvalid   = errors.New("oauth redirect uri invalid")
  ErrClientNameInvalid = errors.New("oauth client name invalid")
  ErrScopeInvalid      = errors.New("oauth scope invalid")
)
```

- `NewToken`/`NewCode` read exactly 32 bytes from the injected entropy reader
  and fail closed on short reads. Spellings: `amat_`/`amrt_` prefix plus
  43-character unpadded base64url; codes are bare 43 characters.
- `ParseToken`/`ParseCode` do exact shape decoding (length, alphabet, prefix)
  before hashing; digest comparison call sites use `subtle.ConstantTimeCompare`
  on the 32-byte digests.
- `VerifyS256` base64url-decodes nothing: it hashes the verifier and compares
  against the challenge string's decoded digest in constant time; verifier
  admits 43–128 RFC 7636 alphabet characters only.
- `ValidateRedirectURI` and `ValidateClientName` enforce the full M1 grammar;
  `ParseScopes` admits only the closed set and canonical ordering.

## TDD cycle

- [x] Write token/code REDs: entropy length, exact spelling, prefix/kind round
      trip, malformed inputs (length ±1, padding, wrong alphabet, wrong prefix,
      empty), digest stability, and short-entropy failure.
- [x] Write PKCE REDs: RFC 7636 appendix vectors, verifier 42/43/128/129
      lengths, alphabet violations, challenge non-base64url, and constant-time
      comparator use.
- [x] Write redirect REDs: every M1 scheme/host/port/path/fragment/userinfo and
      byte-bound boundary, IPv6 loopback, uppercase scheme/host handling, and
      5/6 URI counts at the caller.
- [x] Write name/scope REDs: NFC boundaries at 64/65 code points, control
      rejection, scope set/order/duplicate handling.
- [x] Run the expected RED:

  ```sh
  cd apps/server && go test ./internal/oauthsrv -race -count=1
  ```

- [x] Implement the smallest closed primitives; inject entropy and clock.
- [x] GREEN: rerun the suite, then `make server-build server-vet`.

## Adversarial checklist

- No allocation-heavy work before shape checks; malformed input costs O(1).
- Errors, panics, and test logs never contain raw token, code, verifier, or
  challenge values (sentinel grep in tests).
- Base64url decoding rejects padding and non-canonical encodings; two spellings
  of one value cannot both parse.

## Handoff

Report exported signatures/errors, RED/GREEN outputs, and any vector-source
notes. Suggested commit: `feat(auth): add oauth token and pkce primitives`.
