# Task 05 — Authorize flow, consent service, code issue

**Acceptance:** AC-MCP-002, AC-MCP-003 (issue clauses).

**Depends on:** T01 contract; T02 operation shapes; T03 primitives.

**Owned paths:** T05 paths in `file-structure.md`.

## Contract

Implement M8 exactly:

- `HandleAuthorize` (GET): validate `client_id` exists, `redirect_uri`
  exact-matches a registered value, `response_type=code`, scopes parse,
  `code_challenge` present with `code_challenge_method=S256`. Validation failure
  that cannot trust the redirect URI renders a closed 400 page; failure with a
  trusted URI redirects with the M4 error code. With no session: 302 to
  `/login?next=<url-encoded authorize path+query>`. With a session and a live
  equal-or-narrower grant: issue a code immediately and 302 to the redirect URI.
  Otherwise: 302 to `/authorize?<validated query>`.
- `ConsentContext`: re-validate the query against the client row; return client
  name and scope list only.
- `ConsentDecision`: re-validate; `deny` → `redirectTo` carrying
  `error=access_denied` and `state`; `approve` → upsert the grant (respect the
  M5 10-live-grant cap with a closed error), insert the code row (digest,
  challenge, exact URI, 60 s expiry), and return `redirectTo` with `code` and
  `state`.
- Code issue and grant upsert share one transaction. `state` is echoed verbatim
  but bounded (≤ 512 bytes) and never logged.

## TDD cycle

- [x] Write authorize matrix REDs: every validation branch, trusted vs untrusted
      redirect failure shape, login redirect `next` encoding, grant-skip (equal,
      narrower, wider → consent), and session detection.
- [x] Write consent REDs: context returns only name+scopes; decision
      re-validation catches a client row changed between fetch and post; deny
      shape; approve issues exactly one code bound to all five values; 11th live
      grant refused closed.
- [x] Write race REDs on a live database: two concurrent approvals for one
      (user, client) yield one live grant and two valid codes; concurrent
      approve and revoke leave no live token path.
- [x] Run the expected RED:

  ```sh
  cd apps/server && go test ./internal/oauthsrv -race -count=1 -run 'Authorize|Consent'
  ```

- [x] Implement service; rerun to GREEN, then `make server-build server-vet`.

## Adversarial checklist

- Open-redirect attempts (substring, suffix, case, userinfo, encoded slashes)
  fail closed before any user interaction.
- A forged consent POST for an unregistered redirect URI cannot mint a code even
  with a valid session (server-side re-validation proves it).
- `state` handling cannot reflect markup into any rendered page (server returns
  it only inside `redirectTo`).

## Handoff

Report branch table, race evidence, RED/GREEN outputs. Suggested commit:
`feat(auth): add oauth authorize and consent service`.
