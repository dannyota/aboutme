# Task 0: Batch web dependency additions (single owner — B8)

Serializes every new `apps/web` package and lockfile edit into one exclusive
window so T3, T5, T6, and T11 never race on the same files. The integration
owner executes this task alone because the lockfile is a reserved shared file.
No test-first step applies because this task installs packages without adding
behavior.

**Files:** modify `apps/web/package.json`, `apps/web/package-lock.json`.

- [ ] **Step 1: Production dependencies.**
      `(cd apps/web && npm install dompurify@latest lucide-vue-next@latest --save-exact)`
      (consumed by Task 3 — D3 — and Task 6 — D13).
- [ ] **Step 2: Dev dependencies.**
      `(cd apps/web && npm install -D jsdom@latest fontkit@latest @playwright/test@latest pdfjs-dist@latest @napi-rs/canvas@latest tsx@latest --save-exact)`
      (consumed by Task 3's `jsdom` vitest environment — D3, test-only, never
      production — Task 5's `fontkit` cmap coverage — D8 — and Task 11's harness
      plus pinned PDF raster goldens — D16).
- [ ] **Step 3: Verify.** Run `make web-lint web-typecheck web-test web-build`.
      Report the exact installed versions and the two-file diff in the session
      ledger.
