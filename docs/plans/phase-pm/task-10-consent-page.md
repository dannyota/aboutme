# Task 10 — Nuxt consent page and login-redirect preservation

**Acceptance:** AC-MCP-002, AC-MCP-009 (web clauses).

**Depends on:** T02 generated client; T05 service semantics.

**Owned paths:** T10 paths in `file-structure.md`.

## Contract

- `app/pages/authorize.vue`: reads the M8 query, calls `getOAuthConsent` through
  the generated client, renders client name and scopes as text (never markup),
  and offers Approve/Deny. Both actions call `postOAuthConsentDecision` through
  the existing CSRF-protected mutation path and navigate the browser to the
  returned `redirectTo` value with a full-page navigation.
- Unauthenticated visits rely on the existing auth middleware redirect to
  `/login?next=…`; this task verifies the round trip preserves the full query
  and reports (does not silently fix) any bounded gap in the login `next`
  handling.
- Error states are closed: invalid request → fixed error copy with no parameter
  echo; expired session mid-decision → login redirect preserving the query.
  Loading, keyboard operation, focus management, light/dark themes, and the
  Nova/Zinc/Emerald tokens follow the existing auth pages.
- `useOAuthConsent.ts` wraps the two operations with the closed error mapping;
  no window/global state beyond the composable.

## TDD cycle

- [ ] Write component REDs: renders name/scopes as text for a hostile client
      name fixture (markup shows escaped), approve posts exact body, deny posts
      exact body, navigation uses `redirectTo` verbatim, closed error mapping
      for each API failure.
- [ ] Write composable REDs: request shaping, error taxonomy, no retry on 4xx.
- [ ] Write a login-round-trip RED at the router level: visiting
      `/authorize?x=1` unauthenticated lands on login with `next` containing the
      full query; completing login returns to it.
- [ ] Run the expected RED:

  ```sh
  cd apps/web && npx vitest run test/authorize.test.ts test/useOAuthConsent.test.ts
  ```

- [ ] Implement page + composable; rerun to GREEN, then
      `make web-lint web-typecheck web-test`.

## Adversarial checklist

- A crafted `redirectTo` cannot be reached: the page navigates only to the
  server-returned value, and the server only returns registered URIs — the test
  asserts the page performs no client-side URL construction.
- Query parameters are never written into `innerHTML` or interpolated
  attributes.
- Double-submit (rapid approve clicks) posts once (in-flight guard).

## Handoff

Report component/composable API, RED/GREEN outputs, and any login-`next`
finding. Suggested commit: `feat(web): add agent consent page`.
