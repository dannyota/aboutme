# Task 4: Blind adversarial sanitizer + render-bounds suites (independent author)

The master plan's independence rule names the sanitizer and render bounds as
high-risk. A **second, fresh Sonnet 5 instance** authors these suites. Its
inputs are **only**: spec §5 (sanitizer contract + renderer detail),
`validation/sanitizer-allowlist.v1.json`, `validation/hostile-corpus.json`,
`resume.schema.json` (bounds), acceptance IDs AC-SEC-001/AC-SEC-003/AC-SEC-004
(NEW-M7), and **this plan's interface signatures** (`sanitize.RichText`,
`sanitizeRichText`, `ResumeDocument` props). It must **not** read Task 2/3/6
implementation diffs — **or the author-side `sanitizetest` predicate helpers**
(B4) — before its tests are authored and committed. The implementing authors may
not weaken these tests without Opus 5 review.

**Files:** `apps/server/internal/sanitize/adversarial_test.go`,
`apps/web/test/sanitizer/adversarial.test.ts`,
`apps/web/test/renderer/bounds.adversarial.test.ts`.

- [ ] **Step 0 (blind): author an independent neutralization predicate** on each
      side, derived **only** from spec §5 and the allowlist JSON — never by
      importing or transcribing Task 2/3's helper. Two independently derived
      predicates disagreeing about the same output is exactly the kind of
      finding this task exists to surface. Each blind predicate gets its own
      **negative control**: it must reject raw corpus payloads and hand-built
      violations, and the suite fails if it accepts any — a predicate that
      always returns true must fail the suite (B4: this is the difference
      between proving neutralization and proving nothing).
- [ ] **Step 1 (blind): derive payloads beyond the corpus** from the spec's
      forbidden list — at minimum: nested/mutation cases (mXSS-style
      `<noscript>`/`<template>`/foreign-content pivots), scheme obfuscation not
      in the corpus (URL-encoded colon, mixed entity+case), attribute smuggling
      (`formaction`, `srcdoc`, `xlink:href`, `style` attribute), namespace
      confusion (`<math>`, `<svg>` wrappers), and rel/target forgeries. Every
      payload must satisfy the **blind** predicate on **both** sides. With one
      parser boundary removed by D3 (no jsdom in the SSR path), the remaining
      cross-parser seam is x/net/html → Blink — the mutation payloads target it
      directly.
- [ ] **Step 2 (blind): property tests.** Both sides: for arbitrary strings
      (seeded PRNG, fixed seed — deterministic), output always satisfies the
      blind predicate; idempotence; output of one side fed to the other never
      reintroduces a violation.
- [ ] **Step 3 (blind): render bounds.** `bounds.adversarial.test.ts` opens with
      a file-level `// @vitest-environment node` pragma (B7). From schema bounds
      alone: a doc with 24 sections × 64 entries × 16 KB rich text (within the
      512 KB cap — the author computes a consistent max shape) renders via
      `renderToString` without error; every rich-text field in the output passed
      sanitization (predicate over the full document HTML); output size is
      finite and recorded (no numeric budget exists for renderer output —
      deliberately not invented; the recorded number goes to the integration
      owner).
- [ ] **Step 4: hand findings to the implementers** (never fix in-suite); Opus 5
      adjudicates disputes.
