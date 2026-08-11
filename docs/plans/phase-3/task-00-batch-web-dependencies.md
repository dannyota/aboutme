# Task 0: Batch web dependency additions (single owner — B8)

Serializes every new `apps/web` package.json/lockfile edit into one commit so
T3, T5, T6, and T11 never race on the same file. A single fresh implementer
executes this task alone, before any of T3/T5/T6/T11 begins. No test-first step:
this task installs packages and verifies the install, it does not add behavior.

**Files:** modify `apps/web/package.json`, `apps/web/package-lock.json`.

- [ ] **Step 1: Production dependencies.**
      `npm install dompurify@latest lucide-vue-next@latest --save-exact`
      (consumed by Task 3 — D3 — and Task 6 — D13).
- [ ] **Step 2: Dev dependencies.**
      `npm install -D jsdom@latest fontkit@latest @playwright/test@latest --save-exact`
      (consumed by Task 3's `jsdom` vitest environment — D3, test-only, never
      production — Task 5's `fontkit` cmap coverage — D8 — and Task 11's harness
      — D16).
- [ ] **Step 3: Verify + commit.** `make web-lint web-typecheck web-build` (a
      no-op source-wise pass that confirms the install alone doesn't break
      anything). Commit `apps/web/package.json` and `apps/web/package-lock.json`
      only.
