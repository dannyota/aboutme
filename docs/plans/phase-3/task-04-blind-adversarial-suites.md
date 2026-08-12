# Task 4: Blind sanitizer, renderer, and pagination suites

This test-only task supplies the independent tests required by ADR 0011. Fresh
authors own only the files below. They read the approved design, template
contract, generated policy data, current schema, acceptance rows, and the
relevant interface signatures in Tasks 2, 3, 6, and 7. An author must not read
the implementation diff it tests or author-side predicate helpers before
freezing its file. Implementation authors may not edit or weaken these files.

The withheld author helpers are `apps/server/internal/sanitize/sanitizetest/**`
and `apps/web/test/sanitizer/neutralization.ts`. This task never reads or
imports either path.

**Files:**

- `packages/schema/gen/go/sanitizer_adversarial_test.go`
- `packages/schema/test/sanitizer-codegen.adversarial.test.ts`
- `apps/server/internal/sanitize/adversarial_test.go`
- `apps/web/test/sanitizer/adversarial.test.ts`
- `apps/web/test/renderer/bounds.adversarial.test.ts`
- `apps/web/test/renderer/plain-fields.adversarial.test.ts`
- `apps/web/test/renderer/paginate.adversarial.test.ts`
- `apps/web/test/renderer/apply-template.adversarial.test.ts`

The task has five freeze points. Freeze the two codegen-faithfulness tests
before Task 1. Freeze the runtime sanitizer files after Task 1 and before Task 2
or 3. Freeze bounds and plain-fields in one test-only diff after Task 5B and
before Task 6. After Task 6 passes the renderer-only gate below, a different
fresh author freezes pagination in a second, pagination-only diff before Task 7.
After Task 5B, another fresh author freezes the `applyTemplate` suite in its own
test-only diff before Task 8. A missing implementation may make a focused test
fail to compile; record that expected failure and the applicable test-only diff.

The runtime sanitizer workers were unavailable before Tasks 2 and 3, so those
two implementations landed first. The recovered authors later derived their
suites without reading the implementation, author tests, helpers, or diffs. The
exact frozen files fail at codegen-only commit `897d69c` because the public
implementations are absent, and pass at their integration commit. This preserves
independent derivation but does not claim the originally planned chronology. The
Phase 3 defect review must assess this disclosed exception. Renderer and
pagination suites still follow their original pre-implementation freeze order.

- [x] **Step 0a (blind): freeze codegen faithfulness before Task 1.** From only
      the validation JSON, schema version constant, and generated-interface
      contract, author independent TS and Go tests for exact tags, attributes,
      schemes, forbidden sets, rel value, corpus IDs/payloads, and version.
      Record their expected failure while the generated artifacts are absent.
      Task 1's author may not read an implementation diff until these files are
      frozen and may never edit them. Run
      `(cd packages/schema && npx vitest run test/sanitizer-codegen.adversarial.test.ts)`
      and `(cd packages/schema/gen/go && go test ./... -count=1)`.
- [x] **Step 0b (blind): author an independent neutralization predicate** on
      each side, derived **only** from the
      [rich-text contract](../../design/security.md#untrusted-document-content)
      and the allowlist JSON — never by importing or transcribing Task 2/3's
      helper. Two independently derived predicates disagreeing about the same
      output is exactly the kind of finding this task exists to surface. Each
      blind predicate gets its own **negative control**: it must reject raw
      corpus entries whose parsed tree violates the predicate plus hand-built
      violations, and the suite fails if it accepts any — a predicate that
      always returns true must fail the suite (B4: this is the difference
      between proving neutralization and proving nothing). Assert separately
      that dangerous-looking bare text remains a safe text node.
- [x] **Step 1 (blind): derive payloads beyond the corpus** from the spec's
      forbidden list — at minimum: nested/mutation cases (mXSS-style
      `<noscript>`/`<template>`/foreign-content pivots), scheme obfuscation not
      in the corpus (URL-encoded colon, mixed entity+case), attribute smuggling
      (`formaction`, `srcdoc`, `xlink:href`, `style` attribute), namespace
      confusion (`<math>`, `<svg>` wrappers), and rel/target forgeries. Every
      payload must satisfy the **blind** predicate on **both** sides. With one
      parser boundary removed by D3 (no jsdom in the SSR path), the remaining
      cross-parser seam is x/net/html → Blink — the mutation payloads target it
      directly.
- [x] **Step 2 (blind): property tests.** Both sides: for arbitrary strings
      (seeded PRNG, fixed seed — deterministic), output always satisfies the
      blind predicate; idempotence; output of one side fed to the other never
      reintroduces a violation.
- [x] **Step 3: freeze the sanitizer suite.** The recovered suites are commit
      `30291d4`; Go uses seed `0x5033474f5a17` for 2,048 cases and web uses seed
      `0x51a17e3d` for 512 cases. Both fail for the missing public interface at
      codegen-only commit `897d69c`. Run
      `(cd apps/server && go test ./internal/sanitize)` and
      `(cd apps/web && npx vitest run test/sanitizer/adversarial.test.ts)`.
- [ ] **Step 4 (blind): plain fields stay text.** Put markup-looking strings,
      entity spellings, and event-handler text in every renderable plain-text
      field. Render through plain `vue/server-renderer`. Parse the result and
      assert that each value survives as text content, creates no element or
      attribute, and gains no active URL. The renderer escapes these fields; it
      does not reject, sanitize, or rewrite their text.
- [ ] **Step 5 (blind): render bounds.** `bounds.adversarial.test.ts` opens with
      a file-level `// @vitest-environment node` pragma (B7). Derive separate
      fixtures for independent limits that cannot all be maximized in one 512
      KiB document: maximum section and entry counts with small fields; a 16 KiB
      rich-text field; and a near-512 KiB valid aggregate. Each renders via
      `renderToString` without losing content or order, and every rich-text
      field satisfies the independent predicate. Record output size and wall
      time as evidence; no new numeric gate is inferred from one machine.
- [ ] **Step 6 (blind): pagination properties.** Generate valid positive finite
      block heights, semantic gaps, header geometry, and both layout modes with
      a fixed seed. In two-column mode, flattening pages must reproduce every
      block exactly once and in per-column order; shorter columns gain empty
      slices without duplication. In one-column mode, main then sidebar must
      become one ordered full-width flow, and a non-normalized sidebar flow must
      fail. Assert header capacity on page 1, no header repeat, oversized-header
      expansion, conditional header/body gap, exact-fit semantic gaps, and
      suppression of a leading gap after a break. A heading is never stranded;
      heading plus first entry moves as one group and gets one marked expanded
      page when the pair alone is too tall. No other page exceeds capacity;
      increasing capacity never increases page count; identical inputs are
      deterministic. Add a Node `renderToString` case that provides the DOM-free
      async `PaginationMeasureKey`, proves no element/global DOM read, and gets
      complete paged output; missing SSR provider fails closed. Derive these
      tests before reading the Task 7 diff.
- [ ] **Step 6a (blind): template-apply properties.** From accepted ADRs 0008
      and 0021, the template contract, current schema, and Task 8's public
      signature, derive cases without reading a Task 8 implementation diff.
      Prove `keep` preserves valid arrays byte-for-byte; `byType` ranks selected
      keys by selector order and then current visual order; unselected and
      custom keys stay in `main` in current visual order; and object insertion
      order never changes either result. Missing, duplicate, and unknown current
      placement keys, duplicate selectors, `custom`, and selectors on `keep`
      fail with the exact typed code. Property cases prove exactly-once output,
      wholesale replacement outside `layout.sections`, content preservation, and
      input immutability.
- [ ] **Step 7: freeze renderer suites before Task 6.** Record the test-only
      diff containing only `bounds.adversarial.test.ts` and
      `plain-fields.adversarial.test.ts`, plus the expected focused-test
      failure. Run
      `(cd apps/web && npx vitest run test/renderer/bounds.adversarial.test.ts test/renderer/plain-fields.adversarial.test.ts)`.
- [ ] **Step 7a: freeze pagination after Task 6 and before Task 7.** A fresh
      pagination-test author derives Step 6 without reading a Task 7 diff, adds
      only `paginate.adversarial.test.ts`, and records the expected compile or
      contract failure in a separate test-only diff. Task 6's renderer-only gate
      must have passed before this file exists, so its absent T7 imports cannot
      break Task 6. Run
      `(cd apps/web && npx vitest run test/renderer/paginate.adversarial.test.ts)`.
- [ ] **Step 7b: freeze template apply after Task 5B and before Task 8.** A
      fresh author adds only `apply-template.adversarial.test.ts`, records its
      expected compile or contract failure, and freezes it before the Task 8
      author reads the file. Run
      `(cd apps/web && npx vitest run test/renderer/apply-template.adversarial.test.ts)`.
- [ ] **Step 8: hand findings to the implementers** without fixing them in the
      independent suites; the phase reviewer adjudicates disputes.
- [ ] **Step 9: final gate.** Run the focused Go sanitizer tests and
      `make web-lint web-typecheck web-test web-build`. Record the fixed random
      seeds and exact commands.

## Acceptance mapping

- AC-SEC-001: independent hostile and property tests cover Go, client, SSR, and
  browser handoff inputs.
- AC-SEC-003: sanitizer agreement remains safe across parser boundaries.
- AC-SEC-004: contact URL rechecks and hostile document values fail closed.
- AC-REN-002: pagination preserves every block exactly once and in order.
- AC-REN-006: the pure renderer handles valid boundary documents without a DOM
  or network dependency; plain fields remain escaped text nodes.
- AC-REN-004: preset application is a pure total function for valid documents
  and fails closed with the exact typed error for invalid placement inputs.
