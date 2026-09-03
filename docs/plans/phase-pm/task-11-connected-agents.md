# Task 11 — Connected agents settings block

**Acceptance:** AC-MCP-009.

**Depends on:** T02 generated client; T10 shared error-mapping pattern.

**Owned paths:** T11 paths in `file-structure.md`.

## Contract

- `ConnectedAgents.vue` renders inside the existing settings page
  (`sessions.vue` integration point): a list of grants from `listAgentGrants`
  showing client name (text only), scopes, created and last-used times, and a
  Revoke action per row.
- Phase PF later made this mount conditional on the configuration-backed
  `agentAccess` capability. When disabled, the settings page neither renders the
  block nor requests the grant list. AC-AUTH-018 and AC-SEC-006 own that later
  gate; T14 reviews it for PM compatibility.
- Revoke calls `revokeAgentGrant` through the CSRF chain with a confirmation
  dialog patterned on the sessions list, then refreshes the grant list. The
  empty state explains that agents connect through MCP and links nothing
  external.
- `agentGrants.ts` wraps the two operations with the closed error mapping and
  exposes `{ grants, refresh, revoke }`.
- Reauthentication is not required for revocation (revoking access is always
  allowed on a live session); a 401 maps to the login redirect.

## TDD cycle

- [x] Write component REDs: list rendering incl. hostile-name escaping, empty
      state, revoke confirmation flow, exact DELETE call, list refresh after
      revoke, closed error mapping, keyboard operation.
- [x] Write composable REDs: shaping, refresh semantics, no retry on 4xx.
- [x] Write a settings-integration RED: the block appears on the settings page
      without disturbing the session-list or password-settings specs (their
      existing tests stay GREEN).
- [x] Run the expected RED:

  ```sh
  cd apps/web && npx vitest run test/connected-agents.test.ts test/agentGrants.test.ts
  ```

- [x] Implement; rerun to GREEN plus the neighboring settings suites, then
      `make web-lint web-typecheck web-test`.

## Adversarial checklist

- A foreign grant ID in the DELETE path is not constructible from the UI (ids
  come only from the fetched list), and the closed 404 maps to a generic
  refresh.
- Times render from server values without locale-dependent snapshot breakage
  (fixed TZ in tests).

## Handoff

Report component/composable API, RED/GREEN outputs. Suggested commit:
`feat(web): add connected agents settings`.
