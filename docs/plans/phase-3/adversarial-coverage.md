# Phase 3 adversarial coverage

Required test coverage for the renderer lane. Under
[ADR 0024](../../adr/0024-single-pass-delivery-gates.md) the implementing author
writes these cases in the task that owns the behavior. The cases are the
requirement; the choreography that used to surround them is gone.

## Landed: sanitizer suites

These files exist and were derived independently of the implementation:

- `packages/schema/gen/go/sanitizer_adversarial_test.go`
- `packages/schema/test/sanitizer-codegen.adversarial.test.ts`
- `apps/server/internal/sanitize/adversarial_test.go`
- `apps/web/test/sanitizer/adversarial.test.ts`

Go uses seed `0x5033474f5a17` for 2,048 cases; web uses seed `0x51a17e3d` for
512 cases. Both fail at codegen-only commit `897d69c` and pass at integration.
Do not weaken them. They cover the neutralization predicate with negative
controls, payloads beyond the corpus (mXSS pivots through
`<noscript>`/`<template>`/foreign content, URL-encoded and entity-obfuscated
schemes, `formaction`/`srcdoc`/`xlink:href`/`style` smuggling, `<math>` and
`<svg>` namespace confusion, rel and target forgery), idempotence, and
cross-implementation agreement.

## Task 6 — renderer: plain fields stay text

Put markup-looking strings, entity spellings, and event-handler text in every
renderable plain-text field. Render through plain `vue/server-renderer`, parse
the result, and assert each value survives as text content, creates no element
or attribute, and gains no active URL. The renderer escapes these fields; it
does not reject, sanitize, or rewrite them.

## Task 6 — renderer: bounds

File-level `// @vitest-environment node`. Separate fixtures for limits that
cannot all be maximized in one 512 KiB document: maximum section and entry
counts with small fields; a 16 KiB rich-text field; a near-512 KiB valid
aggregate. Each renders through `renderToString` without losing content or
order, and every rich-text field satisfies the neutralization predicate. Record
output size and wall time as evidence; infer no new numeric gate from one
machine.

## Task 7 — pagination properties

Fixed-seed generation of valid positive finite block heights, semantic gaps,
header geometry, and both layout modes.

- Two-column: flattening pages reproduces every block exactly once in per-column
  order; shorter columns gain empty slices without duplication.
- One-column: main then sidebar becomes one ordered full-width flow; a
  non-normalized sidebar flow fails.
- Header capacity on page 1, no header repeat, oversized-header expansion,
  conditional header/body gap, exact-fit semantic gaps, and suppression of a
  leading gap after a break.
- A heading is never stranded: heading plus first entry moves as one group and
  gets one marked expanded page when the pair alone is too tall.
- No other page exceeds capacity; increasing capacity never increases page
  count; identical inputs are deterministic.
- A Node `renderToString` case provides the DOM-free async
  `PaginationMeasureKey`, proves no element or global DOM read, and produces
  complete paged output. A missing SSR provider fails closed.

## Task 8 — template apply properties

From ADRs 0008 and 0021 and the template contract: `keep` preserves valid arrays
byte-for-byte; `byType` ranks selected keys by selector order then current
visual order; unselected and custom keys stay in `main` in current visual order;
object insertion order never changes either result. Missing, duplicate, and
unknown current placement keys, duplicate selectors, `custom`, and selectors on
`keep` each fail with the exact typed code. Property cases prove exactly-once
output, wholesale replacement outside `layout.sections`, content preservation,
and input immutability.

## Acceptance mapping

- AC-SEC-001, AC-SEC-003, AC-SEC-004 — sanitizer suites above.
- AC-REN-002 — pagination preserves every block exactly once and in order.
- AC-REN-004 — preset application is a pure total function and fails closed with
  the exact typed error.
- AC-REN-006 — the pure renderer handles valid boundary documents with no DOM or
  network dependency; plain fields remain escaped text nodes.
