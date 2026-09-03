# P5B T01 — publish transport and controller

## Contract

Add a dedicated explicit-action publish lane. Do not add publish to the autosave
command queue. The controller first flushes the editor coordinator and continues
only when the resume has no pending command, dispatch, conflict, session loss,
unresolved issue, partial template, opaque photo outcome, or required complete
read. It freezes the accepted revision, current schema version, command body,
and a UUID idempotency key before dispatch.

Use these public types, with readonly fields and discriminated outcomes:

```ts
interface PublishCommand {
  slug?: string;
  live: boolean;
  downloadEnabled: boolean;
  seoGeoEnabled: boolean;
}

interface FrozenPublishAttempt {
  resumeId: string;
  ownerId: string;
  revision: Revision;
  schemaVersion: number;
  idempotencyKey: string;
  command: Readonly<PublishCommand>;
}

type PublishResult =
  | { kind: "accepted"; resume: AcceptedResume }
  | { kind: "invalid"; issues: readonly ServerValidationIssue[] }
  | { kind: "reauth-required" }
  | { kind: "slug-taken" }
  | { kind: "stale"; winner: ValidatedStaleWinner }
  | { kind: "rate-limited"; retryAfterMs: number | null }
  | { kind: "public-state-busy"; retryAfterMs: number | null }
  | { kind: "session-lost" }
  | { kind: "failed"; code: PublishFailureCode }
  | { kind: "unknown"; reason: "transport" | "server" };
```

Plan correction (2026-09-03): `revision` is the editor's branded decimal
`Revision` string, not a JavaScript `number`; the API revision is int64 and must
not cross a lossy numeric conversion. `ownerId` binds every retained retry to
the authenticated account that created it.

The exact type names may follow nearby editor conventions, but those states must
remain explicit. The transport sends `POST /api/v1/resumes/{id}/publish` with
exact `If-Match`, `Idempotency-Key`, `X-CSRF-Token`, `X-Resume-Schema-Version`,
JSON content type, credentials, and no-store cache. It validates the complete
success envelope, ETag, and schema header before the store adopts it.

On `csrf_rejected`, refresh `/api/v1/me` and retry the frozen attempt once with
the refreshed token. On a network error or `500`, retain that same frozen
attempt for an explicit user retry. Never create a second idempotency key for
that retry. On `412`, adopt the validated winner, discard the publish attempt,
and require a fresh user decision; never auto-republish. Reject malformed or
unknown response shapes as closed failures. Preserve issue path and code only;
do not show raw server text.

Expose controller operations for submit, retry-uncertain, password reauth,
provider-reauth start, retry-after-provider-reauth, and cancel. Password reauth
calls `POST /api/v1/auth/password/reauth`; success replays the same frozen
publish attempt. Incorrect password remains in the reauth state.

The v1 flag defaults provider login off, so every v1 account uses password
reauth. Preserve the retained provider-capable design when that flag is on: an
account without a password uses the stable first identity returned by `GET /me`,
starts `POST /api/v1/auth/{provider}/start?purpose=reauth` with CSRF, validates
the returned authorize URL against the existing exact provider or loopback
allowlist, and returns only that validated URL to the dialog. The editor tab
keeps the frozen intent in memory and offers one explicit retry after the user
completes the provider round trip. Do not call `window.open` after the
asynchronous POST, poll, persist the attempt, auto-retry, or select a later
identity. Extract and reuse the existing authorize-URL validator so account
settings and publish cannot drift.

## TDD cases

Write `publish-api.test.ts` first. Observe failures for exact request headers
and body, success validation, every documented status family, malformed error
bodies, one CSRF refresh, and same-key uncertain replay. Then write
`publish-controller.test.ts` and observe failures for save-first blocking,
accepted store adoption, stale no-replay, password reauth replay, stable-first
provider selection, provider start URL validation, explicit post-provider retry,
disabled-provider rejection, cancellation, and duplicate-submit suppression. Add
focused tests for the shared authorize-URL validator and keep the existing
settings tests green.

## Ownership and checks

Owned paths:

- `apps/web/app/editor/publishApi.ts`
- `apps/web/app/editor/publishController.ts`
- the smallest necessary exports in `apps/web/app/editor/resumeApi.ts`
- `apps/web/app/composables/useResumeEditor.ts`
- `apps/web/app/composables/providerAuthorization.ts`
- `apps/web/app/pages/app/settings/sessions.vue`
- `apps/web/test/editor/publish-api.test.ts`
- `apps/web/test/editor/publish-controller.test.ts`
- `apps/web/test/provider-authorization.test.ts`
- `apps/web/test/sessions.test.ts`
- `apps/web/test/sessions-privileged-start-adversarial.test.ts`

Acceptance: `AC-PUB-007`, `AC-PUB-008`, `AC-PUB-009`. Budgets: the server owns
the 30 changed-slug attempts per account per hour limit; the client honors
`Retry-After` and does not create a retry loop.

Run:

```sh
cd apps/web
npx vitest run test/editor/publish-api.test.ts test/editor/publish-controller.test.ts
npx eslint app/editor/publishApi.ts app/editor/publishController.ts \
  app/composables/useResumeEditor.ts app/composables/providerAuthorization.ts \
  app/pages/app/settings/sessions.vue test/editor/publish-api.test.ts \
  test/editor/publish-controller.test.ts test/provider-authorization.test.ts \
  test/sessions.test.ts test/sessions-privileged-start-adversarial.test.ts
npx vitest run test/sessions.test.ts \
  test/sessions-privileged-start-adversarial.test.ts
npx vue-tsc --noEmit
```

Do not edit Git state, UI components, CSS, browser harnesses, generated OpenAPI,
or plan records. Report the first observed failing test, changed files, exact
commands and results, and any contract gap.
