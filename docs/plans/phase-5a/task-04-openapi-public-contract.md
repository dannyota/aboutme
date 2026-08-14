# Task 04 — Publish and public OpenAPI wire contract

**Owner:** Integration owner in serialized window W0c.

**Acceptance:** AC-PUB-001/002/003 wire authority. This task has no revocation-
row test ownership.

**Authorities:** `public-contract.md`, `design.md`, `docs/design/api.md`, ADR
0004, ADR 0005, ADR 0016, and ADR 0019.

**Files:** The Task 04 row in `file-structure.md`. Do not edit Go handlers, Nuxt
renderer implementation, migrations/sqlc, or root manifests.

**Interfaces:** Produces OpenAPI `PublishResumeRequest`, publish result/issues,
closed `PublicResume` leaves, and generated TypeScript. Consumes existing error,
auth, revision, language, and resume-schema components unchanged.

## Step 1 — RED the closed wire schemas

- [ ] Extend contract tests for required booleans and an optional, non-null,
      nonempty string `slug`; closed issues; publish response; public JSON
      route; photo route, public `405`, and owner/public DTO separation.
- [ ] Declare projected personal `details` optional but, when present, a
      non-null array that permits zero items. Contract goldens distinguish
      source-absent (member omitted), explicit source-empty (`"details":[]`),
      and source-present but fully privacy-filtered (`"details":[]`).
- [ ] Assert `PublicResume` is closed and contains only `slug`, decimal-string
      `revision`, canonical `lng`, `downloadEnabled`, and `document`.
      `PublicResumeDocument` contains only `schemaVersion`, projected
      `personalDetails`, projected `content`, and `customization`. Every public
      contact/section/entry/photo leaf is closed and omits `isHidden`, storage
      key, account/resume IDs, title, flags, and timestamps.
- [ ] Assert HTML/Markdown/sitemap/robots/llms are documented prose routes, not
      generated JSON API operations. Preserve standard error and auth headers.
- [ ] Run RED:

  ```sh
  npx @redocly/cli lint docs/api/openapi.yaml
  (cd docs/api && npx vitest run test/openapi.test.ts test/resumes.contract.test.ts test/generate-wiring.test.ts)
  ```

  Expected: publish/public schemas and paths are absent.

## Step 2 — GREEN the wire authority and generated client

- [ ] Add exact `PublishResumeRequest`, `PublishValidationIssue`, publish
      response, `PublicResume`, every public document leaf, public JSON and
      photo path. Mark all objects closed and preserve documented order/bounds.
- [ ] Define `slug` as optional `type: string` with `minLength: 1`, no nullable
      branch, no default, and no OpenAPI grammar/length constraint that would
      turn approved semantic `422` cases into transport `400` cases.
- [ ] Use route parameters with the approved slug grammar and length. Encode
      revisions as decimal strings. Document exact GET, HEAD, conditional,
      success, and error behavior without adding a second error family.
- [ ] Regenerate `apps/web/app/api/generated/openapi.ts`; never hand-edit it.
      Task 05's Go DTO and Task 09's TypeScript request consume this schema as
      the sole public wire authority.
- [ ] Run GREEN:

  ```sh
  make api-check web-typecheck
  ```

## Executable RED → GREEN checkpoints

- [ ] Publish RED: add a contract case that requires `live` and both flags,
      accepts omitted `slug`, and rejects `null`/empty `slug`; run
      `(cd docs/api && npx vitest run test/resumes.contract.test.ts -t 'publish request')`
      and observe `PublishResumeRequest` is missing. GREEN: add
      `slug: { type: string, minLength: 1 }` outside `required`, the three
      required booleans, closed issues/result, and the owner operation; rerun
      that command.
- [ ] Public DTO RED: add a generator test that indexes
      `components["schemas"]["PublicResume"]` and fails on any owner-only key;
      run
      `(cd docs/api && npx vitest run test/openapi.test.ts -t 'closed public resume')`.
      GREEN: add the exact closed leaf graph and JSON/photo paths, then rerun
      the command and `npx @redocly/cli lint docs/api/openapi.yaml`.
- [ ] Generation RED: run
      `(cd docs/api && npx vitest run test/generate-wiring.test.ts)` and observe
      generated drift. GREEN: run the repository OpenAPI generator, inspect the
      generated `PublicResume` and `PublishResumeRequest`, then run
      `make api-check web-typecheck`.

## Completion

- [ ] Return exact schema/operation names and generated diff in the handoff.
- [ ] Suggest commit: `feat(api): define publish and public resume contract`.
