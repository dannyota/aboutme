# Task 10: Editor→renderer one-way import lint rule + purity lint

Satisfies **AC-REN-005**.

**Files:** modify `apps/web/eslint.config.mjs` (D19 override block); create
`apps/web/test/import-rule.test.ts`.

- [ ] **Step 1: Failing rule test.** Using the `eslint` API (`ESLint` / `Linter`
      with the project flat config — already a devDep), lint **virtual** file
      contents at paths under `app/components/resume/` containing:
      `import { useAppStore } from '~/stores/app'`;
      `import { useApi } from '~/composables/useApi'`; `from 'pinia'`;
      `from '#app'`; `from '~/components/editor/Toolbar.vue'`; plus
      `Date.now()`, `Math.random()`, `x.toLocaleDateString()`,
      `new     Intl.DateTimeFormat()` — assert each reports an error. Lint a
      clean renderer-style snippet (imports from `vue`, `@aboutme/schema`,
      sibling `./primitives/…`) — assert zero errors. Also lint the same bad
      imports at a **non**-renderer path and assert they are _not_ flagged (the
      rule is scoped, not global). Run → FAIL.
- [ ] **Step 2: Implement the override** in `eslint.config.mjs`; pass. Then run
      `make web-lint` over the real tree — the renderer built in Tasks 6–7 must
      already satisfy the rule (if not, that's a Task 6/7 defect to fix, not a
      rule to weaken).
- [ ] **Step 3: Gate + commit.**
