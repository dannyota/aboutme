# Task 10 — Nuxt consent page and login-redirect preservation

**Acceptance:** AC-MCP-002, AC-MCP-009 (web clauses).

**Depends on:** T01 mutable pre-UAT migration; T02 generated client; T05 service
semantics.

**Owned paths:** T10 paths in `file-structure.md`.

## Contract

- `app/pages/authorize.vue`: reads the M8 query, calls `getOAuthConsent` through
  the generated client, renders client name and scopes as text (never markup),
  and offers Approve/Deny. Both actions call `postOAuthConsentDecision` through
  the existing CSRF-protected mutation path and navigate the browser to the
  returned `redirectTo` value with a full-page navigation.
- No auth middleware or login `next` support exists today (every current guard
  is a bare `navigateTo('/login')`, and `login.vue` hardcodes `/app/resumes`
  after login). This task builds both bounded pieces: the consent page's own
  session guard redirects unauthenticated visitors to
  `/login?next=<url-encoded full path+query>`, and `login.vue` honors a
  validated `next` after any successful login (provider or password) —
  same-origin relative path only per M8, fallback `/app/resumes`. Other bare
  `navigateTo('/login')` call sites are out of scope and unchanged.
- Provider login accepts the same bounded `next` on each public GET start. Go
  validates it before creating the provider transaction, stores it in
  `oauth_transactions.return_path`, and uses the stored value after a successful
  callback. Link and reauthentication callbacks remain fixed on the sessions
  settings page. Migration, sqlc, OpenAPI, and provider callback paths are an
  integration-owner correction because the original web-only file list could not
  complete the required provider round trip.
- Error states are closed: invalid request → fixed error copy with no parameter
  echo; expired session mid-decision → login redirect preserving the query.
  Loading, keyboard operation, focus management, light/dark themes, and the
  Nova/Zinc/Emerald tokens follow the existing auth pages.
- `useOAuthConsent.ts` wraps the two operations with the closed error mapping;
  no window/global state beyond the composable.

## TDD cycle

- [x] Write provider-flow REDs: the OpenAPI start contract exposes bounded
      `next`; the database enforces the relative-path byte boundary; a real
      provider callback preserves a valid path; invalid and oversized values
      bind `/app/resumes`. Recreate the pre-UAT test database after amending
      migration 00009 and rerun to GREEN.
- [x] Write component REDs: renders name/scopes as text for a hostile client
      name fixture (markup shows escaped), approve posts exact body, deny posts
      exact body, navigation uses `redirectTo` verbatim, closed error mapping
      for each API failure.
- [x] Write composable REDs: request shaping, error taxonomy, no retry on 4xx.
- [x] Write login `next` REDs against `login.vue`: absent → `/app/resumes`;
      valid relative path with query → honored verbatim; `//evil`,
      `https://evil`, `javascript:`, over-length, and non-leading-slash values →
      fallback. Provider and password login paths both honored.
- [x] Write a login-round-trip RED at the router level: visiting
      `/authorize?x=1` unauthenticated lands on login with `next` containing the
      full query; completing login returns to it.
- [x] Run the expected RED:

  ```sh
  cd apps/web && npx vitest run test/authorize.test.ts test/useOAuthConsent.test.ts
  ```

- [x] Implement page + composable; rerun to GREEN, then
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
