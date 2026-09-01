# Task 06 — Token endpoint, refresh rotation, revocation

**Acceptance:** AC-MCP-003, AC-MCP-004.

**Depends on:** T01 contract; T03 primitives; T05 code semantics.

**Owned paths:** T06 paths in `file-structure.md`.

## Contract

Implement M2/M3/M4 exchange semantics:

- `HandleToken` (POST): exact `application/x-www-form-urlencoded`, ≤ 4,096
  bytes, closed parameter set, duplicates rejected. Two grant types:
  - `authorization_code`: parse code, verify PKCE (`VerifyS256`), consume the
    code under its row lock checking client, exact `redirect_uri`, expiry, and
    unconsumed state; issue one access and one refresh token in a new family
    inside the same transaction. A consumed-code replay revokes every token
    issued from that code before returning `invalid_grant`.
  - `refresh_token`: parse token; a live refresh token rotates (new refresh
    inserted, old marked superseded, new access issued, same family, same
    transaction); a superseded one revokes the whole family; expired or revoked
    returns `invalid_grant`. Family absolute expiry (30 d) is checked against
    the injected clock.
- `HandleRevoke` (POST): RFC 7009 form; a valid access or refresh token revokes
  its grant and every family under it in one transaction; unknown or foreign
  tokens return 200 with no state change (no oracle).
- Success bodies:
  `{ access_token, token_type: "Bearer", expires_in, refresh_token, scope }`
  with `Cache-Control: no-store`. Every failure uses the M4 closed error JSON.
  No cookie is read on either route.

## TDD cycle

- [x] Write form-strictness REDs: media type, size, closed/duplicate params,
      unknown grant types, byte-compared error bodies.
- [x] Write exchange REDs: happy path, wrong verifier, wrong client, wrong
      redirect URI, expired code (60 s ± 1 s), consumed-code replay revoking
      issued tokens.
- [x] Write rotation REDs with injected clock: rotate chain of three, superseded
      reuse revokes family, access expiry 3,600 s boundary, family expiry 30 d
      boundary, cross-family `rotated_from` impossible.
- [x] Write live-DB race REDs: two concurrent exchanges of one code yield one
      success and one `invalid_grant`; concurrent rotate and revoke leave zero
      live tokens.
- [x] Write revocation REDs: grant + families die together; unknown token 200
      no-op; `/mcp`-visible effect is T07's test, cite the shared fixture.
- [x] Run the expected RED:

  ```sh
  cd apps/server && go test ./internal/oauthsrv -race -count=1 -run 'Token|Revoke'
  ```

- [x] Implement; rerun to GREEN, then `make server-build server-vet`.

## Adversarial checklist

- Timing: unknown client, unknown code, and bad verifier take the same code path
  shape (hash-then-lookup); no early return leaks existence.
- Token/code material and verifiers never appear in errors, logs, or test
  transcripts (sentinel grep).
- A revoked grant cannot be resurrected by an in-flight rotation that commits
  after the revocation (lock-order test).

## Handoff

Report exchange/rotation matrices, race evidence, RED/GREEN outputs. Suggested
commit: `feat(auth): add oauth token exchange and revocation`.
