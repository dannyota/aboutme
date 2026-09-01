# Task 02 — OpenAPI consent and grant operations, generated client

**Acceptance:** AC-MCP-002, AC-MCP-009 (contract clauses).

**Depends on:** T00 roots; T01 storage names.

**Owned paths:** T02 paths in `file-structure.md`. Integration owner alone
(OpenAPI/generated-client window).

## Contract

Add exactly four operations per `integration-handoffs.md`:

- `getOAuthConsent` (GET `/api/v1/oauth/consent`): query parameters `client_id`,
  `redirect_uri`, `response_type`, `scope`, `state`, `code_challenge`,
  `code_challenge_method`; 200 `{ clientName, scopes }`; closed 400/401/404
  errors. Session required; no CSRF (read).
- `postOAuthConsentDecision` (POST `/api/v1/oauth/consent`): strict JSON body of
  the same fields plus `decision` (`approve`/`deny`); 200 `{ redirectTo }`;
  session + CSRF + exact Origin; ≤ 4,096 bytes.
- `listAgentGrants` (GET `/api/v1/me/agents`): 200
  `{ grants: [{ id, clientName, scopes, createdAt, lastUsedAt }] }`.
- `revokeAgentGrant` (DELETE `/api/v1/me/agents/{grantId}`): 204; session +
  CSRF + exact Origin; unknown or foreign grant returns the same closed 404.

No response field ever carries token or code material. The raw OAuth protocol
endpoints and `/mcp` stay out of OpenAPI per the `decisions.md` ruling; note
that ruling in the spec description text.

## TDD cycle

- [ ] Write `docs/api/test/oauth-consent.test.ts` REDs: operation presence,
      closed request/response schemas, required session/CSRF markers, no
      additionalProperties, and the four-operation exact count for the new tag.
- [ ] Run the expected RED: `make api-check`.
- [ ] Land the OpenAPI source, regenerate `openapi.ts`, and update the
      client-parity fixture.
- [ ] GREEN: `make api-check` and `make web-typecheck`.

## Adversarial checklist

- `scope` parsing in the contract is a closed enum list, not free text.
- `redirectTo` is documented as same-registered-redirect only; the schema
  forbids additional properties everywhere.
- Error components reuse the existing closed error envelope; no new error shape
  leaks dependency detail.

## Handoff

Report operation ids, schema names, GREEN outputs, and generated-client diff
size. Suggested commit:
`feat(api): add oauth consent and agent grant operations`.
