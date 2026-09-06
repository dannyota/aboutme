# Task 8.6 — Privacy settings

**Owner:** Terra author. **Acceptance:** AC-PRIV-001/002. **Predecessor:** 8.1
HTTP contract. Read the phase index, product deletion disclosures, existing
settings and password reauth components.

**Owned paths:** `apps/web/app/components/settings/PrivacySettings.vue`,
`apps/web/app/composables/privacySettings.ts`,
`apps/web/app/pages/app/settings/sessions.vue`, focused privacy component tests
and affected settings tests. Root owns generated types and browser scripts.

Add export and account deletion to existing settings with shared UI components.
Export downloads `aboutme-export.json` and revokes its object URL after use. Use
a bounded binary read with content-type/size checks and closed error copy.

Deletion has a deliberate confirmation dialog with exact disclosure: access ends
immediately, private-media removal targets 24 hours, and backup copies expire on
the 30-day schedule. Confirming sends one bodiless DELETE through the existing
CSRF mutation helper. Disable duplicate submission. A reauth_required response
shows the existing password/provider reauth flow, then requires the user to
confirm deletion again. Never automatically delete after reauth or a callback.
Cancel makes no request.

On 204, clear local authenticated state and navigate to login. Use a full
same-origin navigation if needed to avoid retaining account data in memory. A
failed deletion keeps account data and displays fixed retry copy. Keyboard,
focus, accessible name, busy and error behavior follow existing dialogs.

- [ ] Write component/action tests and observe failure.
- [ ] Implement the smallest settings slice.
- [ ] Test cancel, duplicate click, reauth-required, failed reauth, explicit
      second confirmation, 401, 409, 429, 503, export abort/oversize/error, no
      secret diagnostics and object-URL cleanup.
- [ ] Run focused Vitest, then `make web-lint web-typecheck` from the root.
      Reserve a heavy slot; never run full CI.

Report exact checks and selectors for the later browser author.
