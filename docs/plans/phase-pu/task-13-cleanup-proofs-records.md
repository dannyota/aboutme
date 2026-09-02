# Task 13 — Cleanup, proofs, records

**Acceptance:** AC-UI-001…006 `PROVEN`.

**Depends on:** T01–T12 reports accepted. Owner task; runs alone.

**Owned paths:** T13 paths in `file-structure.md`.

## Contract

- The four legacy stylesheets and their `tailwind.css` import lines are deleted,
  along with `OptionalField.vue`. `tailwind.css` then matches decisions U2 minus
  the legacy lines; the toolkit test still passes.
- `test/ui/surface-boundary.test.ts` proves no raw control or hand-written
  dialog exists in scope.
- Every screen is reviewed in light and dark at 1440×1000 and 1024×768; the
  screenshots live under `.dev/design-qa/pu/`.
- The browser proofs pass; records are updated; the exit checklist, `make ci`,
  and `make scan` run on one unchanged candidate.

## Steps

- [ ] **Boundary RED.** Create `test/ui/surface-boundary.test.ts`:

  ```ts
  import { readdirSync, readFileSync } from "node:fs";
  import { join } from "node:path";

  import { describe, expect, it } from "vitest";

  const ROOTS = [
    "app/pages",
    "app/components/editor",
    "app/components/auth",
    "app/components/settings",
    "app/components/app",
  ];
  const EXEMPT = new Set([
    "app/pages/_harness/render.vue",
    "app/components/editor/photo/PhotoPanel.vue", // native file inputs
    "app/components/editor/richtext/RichTextEditor.vue", // ProseMirror root
  ]);
  const RAW = /<(button|input|select|textarea)\b/g;
  const DIALOG = /role="(dialog|alertdialog)"/g;

  function vueFiles(dir: string): string[] {
    return readdirSync(dir, { withFileTypes: true }).flatMap((entry) =>
      entry.isDirectory()
        ? vueFiles(join(dir, entry.name))
        : entry.name.endsWith(".vue")
          ? [join(dir, entry.name)]
          : [],
    );
  }

  describe("surfaces compose the shared layers (AC-UI-002)", () => {
    const files = ROOTS.flatMap(vueFiles).filter((file) => !EXEMPT.has(file));

    it.each(files)("%s renders no raw control", (file) => {
      const source = readFileSync(file, "utf8");
      const template = source.slice(source.indexOf("<template>"));
      expect(template.match(RAW) ?? [], file).toEqual([]);
    });

    it.each(files)("%s hand-writes no dialog", (file) => {
      const source = readFileSync(file, "utf8");
      expect(source.match(DIALOG) ?? [], file).toEqual([]);
    });

    it("exempts PhotoPanel only for the file inputs", () => {
      const source = readFileSync(
        "app/components/editor/photo/PhotoPanel.vue",
        "utf8",
      );
      const raw = source.slice(source.indexOf("<template>")).match(RAW) ?? [];
      expect(raw.every((tag) => tag === "<input")).toBe(true);
    });
  });
  ```

  `components/app` is included because composites must use primitives too; if a
  composite legitimately needs a raw element (none is expected), add it to
  `EXEMPT` with a comment and record it.

- [ ] Run it; expected: it fails only on files a surface task missed. Return
      each failure to that task's author; do not fix them here.

- [ ] Delete `app/assets/css/{app,editor,auth,landing}.css`, their import lines
      in `tailwind.css`, and `app/components/editor/forms/OptionalField.vue`.
      Confirm
      `grep -rn "editor.css\|auth.css\|landing.css\|app.css\|OptionalField" app test`
      prints nothing.

- [ ] **Web gates:**

  ```sh
  make web-lint web-typecheck web-test web-build
  cd apps/web && npx vitest run test/renderer
  make web-e2e
  ```

- [ ] **Visual review.** With the native stack up (`make dev-native-status`),
      drive every screen with the Playwright MCP server in both themes and both
      widths, and save screenshots under
      `.dev/design-qa/pu/<screen>-<theme>-<width>.png`: landing, login,
      register, forgot password, reset password (token invalid state), verify
      email (invalid state), consent (mock query), resume list (empty and with
      rows, each dialog open), settings (each card, revoke dialog), editor (each
      rail panel, session-lost dialog, narrow mode with both tabs, delete
      dialogs, photo panel with and without a photo). Record P0–P2 findings in
      the report and return them to the owning author before continuing.

- [ ] **Browser proofs** (each alone):

  ```sh
  make dev-https-auth-check
  make dev-https-editor-check
  make dev-https-mcp-check
  make dev-https-entry-check
  make dev-https-public-check
  ```

  A selector failure that maps to a listed hook change is fixed in the spec
  under `deploy/dev-https-browser/` by the owner and noted in the report; any
  other failure returns to the task author.

- [ ] **Records.** Update `apps/web/README.md` with a "UI conventions" section
      (the three layers, `scripts/ui-add.sh`, the field commit rule, the
      test-hook policy, `data-slot` as the primitive marker);
      `docs/architecture.md` "Implemented authenticated editor" and the entry
      narrative for the toolkit and the preview fallback; `ac-ui.md` rows to
      `PROVEN` with evidence; the master plan PU row to `Complete`. Commit the
      records with `git add -- <paths>`.

- [ ] **Review and gates.** Dispatch the fresh non-author review named in the
      README; fold fixes; then on the unchanged candidate run the exit
      checklist, `make ci` (foreground chunks if the single command exceeds the
      shell limit), and connected `make scan`. Delete `docs/plans/phase-pu/` in
      the exit commit.

## Handoff

Report every gate output, the visual-review findings with their resolution, and
the final commit list. Suggested commits:
`chore(web): remove the legacy stylesheets` and `docs: record phase PU exit`.
