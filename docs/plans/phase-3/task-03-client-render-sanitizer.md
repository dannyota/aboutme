# Task 3: Client-side render sanitizer (DOMPurify, browser-only) + cross-implementation agreement

Satisfies the DOMPurify half of **AC-SEC-001**/**AC-SEC-003** and D2's agreement
contract, under the D3 ruling: DOMPurify guards **client-side** renders of user
HTML (P4 ProseMirror preview, P6B SSE-refetch re-render); SSR passes
Go-sanitized content through unchanged.

**Files:** create `apps/web/app/utils/sanitizeRichText.ts`,
`apps/web/test/sanitizer/{neutralization.ts,sanitize.test.ts,cross-agreement.test.ts,ssr-passthrough.test.ts}`.
`dompurify` (dependency) and `jsdom` (devDependency only — the vitest DOM
environment for these tests, never a production import, D3) are already
installed by Task 0 (B8); this task does not touch `package.json`/lock.

Task 4's client adversarial suite must already be frozen. This author records
its expected failure before implementation and never edits that file.

**Interfaces (produced):**

```ts
// app/utils/sanitizeRichText.ts — CLIENT-ONLY sanitization (D3 ruling).
// Client (import.meta.client): DOMPurify over window; config built from
// @aboutme/schema/sanitizer constants — never a literal list. Hooks
// enforce D4 (rel overwritten to EXTERNAL_REL; target stripped unless
// "_blank"; per-tag attribute scoping, since DOMPurify's ALLOWED_ATTR is
// global). Server: returns the input UNCHANGED — Go is the sanitization
// authority for everything SSR renders (bluemonday on write, P2B; public-read
// defence in depth, P5A; internal-print read defence in depth, P7A). A jsdom
// import anywhere under
// app/ is a defect (Task 10's lint scope + Step 4's build assertion).
export function sanitizeRichText(html: string): string;
```

- [x] **Step 1: Failing corpus test (client leg).** `sanitize.test.ts` (run
      under the `jsdom` vitest environment via a file-level
      `// @vitest-environment jsdom` pragma — DOMPurify's supported test DOM;
      happy-dom stays the default elsewhere): iterate `HOSTILE_CORPUS`, assert
      the neutralization predicate (same D2(a) rules as Task 2) implemented over
      `DOMParser` in `neutralization.ts`. That helper exports serializable rule
      data and DOM assertions reused by the author-side Tasks 9 and 11; the Task
      4 blind author cannot read or import it. Run → **FAIL**. Include the same
      **negative control** as Task 2: the TS predicate must reject raw corpus
      entries whose parsed DOM violates the predicate and guaranteed hand-built
      violations. Dangerous-looking bare text such as `javascript:alert(1)`
      remains safe text and must pass the structural predicate. A vacuous
      predicate fails the suite (B4).
- [x] **Step 2: Implement** with generated constants + hooks; make Step 1 pass.
      Also assert idempotence across the corpus. The real-browser (non-jsdom)
      execution of this exact code path is proven in Task 11 Step 4 — the test
      env here is a development proxy, not the AC-SEC-001 browser evidence.
- [x] **Step 3: Cross-implementation agreement.** `cross-agreement.test.ts`
      reads `apps/server/internal/sanitize/testdata/corpus-output.golden.json`
      (repo-relative path, read-only — the same cross-package pattern
      `schema-contract.test.ts` already uses) and asserts, per payload:
      `sanitizeRichText(bluemondayOut)` is **DOM-canonically equal** to
      `bluemondayOut` per D2's precise definition (sorted attributes,
      whitespace-normalized `rel` token comparison, comments and whitespace-only
      text nodes ignored, everything else byte-exact) — the client pass must
      never visibly alter Go-sanitized content when P6B refetches or P4 previews
      it. A mismatch here is a **blocking cross-side defect**, resolved by
      changing one side's normalization, never by loosening the test.
- [x] **Step 4: SSR passthrough contract.** `ssr-passthrough.test.ts` opens with
      a file-level `// @vitest-environment node` pragma (B7 — plain Node env, no
      DOM, proving nothing here depends on a browser shim): with the server
      branch active, `sanitizeRichText` returns its input **byte-identical**,
      and a minimal component using it with `v-html` rendered through
      `renderToString` (plain `vue/server-renderer`) emits an already-sanitized
      fragment byte-intact — no re-encoding, no mutation (Task 9 Step 3 extends
      this to whole documents). Additionally assert the built client bundle is
      the only place DOMPurify lands: `nuxt build` output's server bundle
      contains no `dompurify`/`jsdom` module (string scan of `.output/server` —
      cheap and direct evidence for D3's "not in the SSR path" claim).
- [x] **Step 5: Gate.** Run `make web-lint web-typecheck web-test web-build`
      first, then run the focused bundle-scan test against the just-built
      `.output/server`. A missing or older output is a test failure. Report the
      owned-path diff and exact output to the integration owner.
