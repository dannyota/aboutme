# Task 01 — OAuth storage: migration 00009, constraints, sqlc contract

**Acceptance:** AC-MCP-001, AC-MCP-003, AC-MCP-004 (storage clauses).

**Depends on:** T00 (budgets and roots recorded).

**Owned paths:** T01 paths in `file-structure.md`. Integration owner alone
(migration/sqlc window).

## Contract

Additive goose migration `00009_add_oauth_agent_access.sql` creating:

| Table                       | Required state                                                                                                                                           |
| --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `oauth_clients`             | UUID pk, unique public client id, name ≤ 64 code points (byte-bound check ≤ 256), redirect URIs `jsonb` (1–5, ≤ 512 bytes each; count check), timestamps |
| `oauth_authorization_codes` | UUID pk, unique 32-byte code digest, client/user fks, scopes closed check, challenge text, exact redirect URI, 60 s expiry check, consumed time          |
| `oauth_grants`              | UUID pk, one live row per (user, client) partial unique, scopes closed check, created/revoked times                                                      |
| `oauth_tokens`              | UUID pk, unique 32-byte token digest, closed kind check, family UUID, rotated-from self-fk, client/user/grant fks, expiry ordering, revoked/last-used    |

Digest columns are `BYTEA` with `octet_length = 32` checks. Scope columns admit
only `resumes:read`/`resumes:write` combinations. Account deletion cascades user
→ grants/codes/tokens; client deletion cascades its rows. Expired codes and
revoked/expired tokens are removed in bounded batches (≤ 200) by the contract's
cleanup queries.

`internal/store/oauth_contract.go` exports the `OAuthQueries` interface per
`integration-handoffs.md`, with `FOR UPDATE` on code consumption and family
revocation paths.

## TDD cycle

- [x] Write migration REDs: fresh up/down, every constraint boundary (name
      bytes, URI count, digest length, closed kinds/scopes, expiry ordering),
      cascade matrix, and partial-unique live-grant behavior under two
      concurrent inserts.
- [x] Write store REDs: code consume is single-success under two concurrent
      transactions; family revocation revokes every member; cleanup queries
      respect batch bounds; joined token lookup returns grant and user in one
      round trip.
- [x] Run the expected RED:

  ```sh
  make sqlc-check
  make server-test-db server-test-integration server-migration-test
  ```

- [x] Land migration + queries, regenerate sqlc, implement the contract; rerun
      to GREEN with `-count=1`.

## Adversarial checklist

- A second live grant for the same (user, client) is impossible at the database
  level, not only in code.
- `rotated_from` cannot form a cross-family link (family equality enforced in
  the supersede query and tested).
- No column ever stores raw token or code material; tests grep fixtures for the
  `amat_`/`amrt_` prefixes appearing only in memory-side test values.

## Handoff

Report the exact interface, constraint list, GREEN outputs, and any goose
preflight notes. Suggested commit: `feat(auth): add oauth agent access storage`.
