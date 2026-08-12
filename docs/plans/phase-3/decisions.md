# Phase 3 decisions

These decisions make the draft phase executable. Accepted ADRs remain the
architecture authority.

## Sanitizing and CSP

1. **D1 — Generated sanitizer policy.** The schema generator emits typed Go and
   TypeScript artifacts from the versioned allowlist and hostile corpus. Runtime
   code never carries a second hand-written tag or scheme list.
2. **D2 — Cross-implementation agreement.** Both sanitizers satisfy one
   structural predicate, are idempotent, and treat the committed Go output as a
   DOM-canonical fixed point. Attribute order and `rel` token order normalize
   only for comparison; emitted security attributes remain exact.
3. **D3 — SSR authority.** Go sanitizes stored writes and every document read
   that feeds SSR, including public HTML and internal print. DOMPurify protects
   client `innerHTML` assignments only. SSR passes the Go-sanitized string and
   ships no server-side DOM package. ADR 0012 controls.
4. **D4 — Anchors.** Every emitted anchor has exact `rel="noopener noreferrer"`.
   `target` survives only when it is `_blank`.
5. **D5 — Renderer CSP.** P3 exports this exact byte string:
   `default-src 'none'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; manifest-src 'self'; media-src 'none'; worker-src 'none'`.
   `data:` is limited to images for the fixed inline photo fixture; derived CSS
   variables require inline style attributes, while inline script remains
   forbidden. P5A and P8-sec apply the same value to public pages and test the
   production SSR build.

## Rendering and pagination

1. **D6 — Pure pagination.** Pagination consumes measured blocks and page
   height. Browser measurement is an injected adapter; unit tests use a
   committed synthetic measurer.
2. **D7 — Geometry.** A4 is 794×1123 CSS pixels and Letter is 816×1056 at 96
   dpi. Page content height derives from token-driven millimetre margins. In
   two-column mode each column paginates independently and page count is the
   larger count. One-column mode first flattens main then sidebar into one
   full-width flow.
3. **D8 — Font catalog.** Task 5 vendors the versioned manifest and assets under
   the license-only admission gate. Task 5B releases schema v2 with stable
   catalog IDs and explicit v1 fallbacks. Coverage and faces are labels, not
   rejection rules. See `../../design/fonts.md`.
4. **D9 — Font readiness.** Screenshot and print consumers explicitly request
   the selected face and fallback, await them, then await
   `document.fonts.ready`.
5. **D10 — Template apply.** A preset stores a placement rule. `applyTemplate`
   computes section keys against the current document and replaces the rest of
   customization without touching content. Current visual order is `main`
   followed by `sidebar`. `keep` preserves both arrays after exact-once
   validation. `byType` orders selected keys by selector rank and then current
   visual order; unselected and custom keys are placed in `main` in current
   order. Invalid placement, duplicate selectors, and a `custom` selector fail
   with a typed error. ADR 0008 controls the base rule; proposed ADR 0021 fixes
   these candidate tie-breaks and validation rules.
6. **D11 — Dates.** `Mon YYYY` uses a fixed English abbreviation table.
   Locale-aware month names remain a later explicit i18n contract.
7. **D12 — Contacts.** Details render in array order. Only validated website,
   LinkedIn, GitHub, and Twitter values link; other values are plain text. ADR
   0013 controls.
8. **D13 — Icons.** A closed `iconKey` map imports individual Lucide icons.
   Unknown keys fail safely and cannot trigger a dynamic package lookup.
9. **D14 — Photos.** The renderer never turns an object key into a URL. Its
   explicit render context carries an already-authorized owner, public, or
   local-preview photo URL. Crop values remain document input and use CSS.
10. **D15 — Goldens.** Committed HTML files cover populated one-column and
    two-column starting states cloned from the `full` fixture, across every
    preset and both display modes, then compare byte-for-byte. Focused tests
    cover other document states. Regeneration is an explicit command never used
    in verification.
11. **D16 — Browser baseline.** The official Playwright image is pinned by
    digest on the locally runnable Linux AMD64 platform. Chromium, viewport,
    timezone, locale, scale, color scheme, color profile, font rendering flags,
    and assets are fixed. Screenshot tolerance and retry count are zero. P9A
    repeats the named visual set on the production ARM64 image as a launch gate;
    `--platform` is never treated as emulation.
12. **D17 — Harness isolation.** The arbitrary renderer route exists only when
    `NUXT_HARNESS=1`. A normal production build test proves its absence.
13. **D18 — Draft emptiness.** Missing or cleared fields create no placeholder.
    Hidden entries are absent from the DOM. Empty or all-hidden sections render
    no heading, rule, or gap.
14. **D19 — Import boundary.** Renderer files cannot import editor, store, API,
    Nuxt runtime, clock, random, locale, or network dependencies. Negative lint
    fixtures prove the rules.
15. **D20 — Sanitizer handoff.** P3 delivers the Go package. P2B calls it at
    every write boundary. P5A calls it on public reads. P7A calls it on the
    document read that feeds internal print SSR. Those phases own their
    integration tests.
16. **D21 — Current document only.** The renderer checks the document's
    `schemaVersion` against the generated current-version constant and fails
    closed before rendering a stale or unknown shape. Server projection remains
    the normal path; the runtime guard covers cached or miswired callers.
