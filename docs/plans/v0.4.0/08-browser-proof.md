# Bilingual browser proof brief

Role: qa. Model: `gpt-5.6-terra`. Start after the integrated web gate passes.

## Objective and authority

Prove the complete Vietnamese and English resume journey at phone and desktop
widths, including state preservation during locale changes. Read `AGENTS.md`,
the accepted localization design and ADR, the local UAT runbook,
`AC-EDITOR-018`, and `AC-UI-014`.

## Owned paths

- Modify `deploy/dev-https-browser/entry.spec.ts`.
- Modify `deploy/dev-https-browser/editor.spec.ts`.
- Modify `deploy/dev-https-browser/publish.spec.ts`.
- Modify `deploy/dev-https-browser/exports.spec.ts`.
- Modify `deploy/dev-https-browser/sample-start.spec.ts`.
- Modify `deploy/dev-https-browser/public.spec.ts` for current editor hooks and
  bounded failure stages.
- Modify `deploy/dev-https-browser/editor-fixtures.ts` only if the specs require
  a shared fictional fixture.
- Save bounded ignored evidence under `.dev/native-https/evidence/`.

Do not edit product source, scripts, package files, images, design, manifests,
or baselines. Do not fix defects. Report steps and the owning frontend path.

The devops role separately owns the `public` addition to the bounded-stage
allowlist in `deploy/dev-https-browser/run.sh`. The manager rebuilds the browser
image and verifies that raw output remains withheld.

## Required proof

- No cookie and invalid cookie open the list in Vietnamese with `html lang=vi`.
  Both language choices are keyboard reachable and persist through sign-in,
  list, creation, editor, and reload.
- Phone width `390x844` and desktop width `1440x900` each complete list, create,
  edit, publish, copy public link, and owner PDF in Vietnamese. Repeat the path
  in English.
- Sample-start runs once per interface locale. After the sample is loaded,
  toggle locale and assert sample ID, suggested title, content, resume language,
  and submitted payload remain equal.
- In the editor, hold a dirty field, focus and selection, visible validation,
  open dialog, and pending or conflict state. Toggle locale and assert no write,
  revision change, remount, focus loss, or data change.
- Open an English resume under Vietnamese chrome. Assert preview `lang`, dates,
  and resume labels stay English. Explicit resume-language edit keeps authored
  text unchanged.
- Exercise publish and PDF pending, blocked, rate-limited, session-ended,
  uncertain, retry, and success copy without changing command or PDF bytes.
- Check Vietnamese diacritics, clipping, wrapping, focus order, error focus,
  live announcements, and accessible names.

## Exact checks

Run `git status --short` first. Announce any native-stack restart before taking
it. Never stop the shared database. Run these browser-heavy commands one at a
time and do not retry failures:

```bash
make dev-https-auth-check
make dev-https-entry-check
make dev-https-editor-check
make dev-https-mcp-check
make dev-https-publish-check
make dev-https-exports-check
make dev-https-sample-start-check
make dev-https-public-check
make native-http-check
```

The expected result is PASS from every command, with bounded evidence retained
by the harness. Public and native checks prove shared shell changes did not
alter public content or security headers.

## Definition of done and report

Report exact changed spec files, commands and results, evidence paths, viewport
coverage, defects with steps and owning role, skipped checks with exact reason,
and open items. Do not perform Git operations. Use short plain text with no em
dash. Do not put plan or task IDs in tests or comments.
