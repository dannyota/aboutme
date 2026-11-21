# Resume history contract

Status: Proposed (2026-11-21). The contract for release v0.10.0 of the
[change review](change-review.md) design, sections "Checkpoints" and "Release
order" item 1. Product rules stay in that design; this page fixes storage,
write-path hooks, routes, export, retention, limits and copy.

## Storage

Migration `00022_resume_checkpoints.sql`, with `SET LOCAL lock_timeout = '5s'`
in Up and Down, explicit grants to `aboutme_app`
([ADR 0005](../adr/0005-database-migrations.md)), and an entry in
`grants_test.go`.

`resume_checkpoints`:

| Column             | Type          | Rule                                                                      |
| ------------------ | ------------- | ------------------------------------------------------------------------- |
| `id`               | `uuid`        | Primary key, random                                                       |
| `resume_id`        | `uuid`        | `REFERENCES resumes (id) ON DELETE CASCADE`                               |
| `started_at`       | `timestamptz` | Session start; the order key                                              |
| `cause`            | `text`        | `edit`, `assistant` or `restore`; v0.10.1 adds `accept`                   |
| `actor_grant_id`   | `uuid` null   | `REFERENCES oauth_grants (id) ON DELETE SET NULL`; null for the owner     |
| `actor_name`       | `text` null   | The client's display name copied at insert; null for the owner            |
| `title`, `lng`     | `text`        | As on `resumes`                                                           |
| `schema_version`   | `integer`     | Version of the stored document                                            |
| `personal_details` | `jsonb`       | Without `photo`                                                           |
| `content`          | `jsonb`       |                                                                           |
| `customization`    | `jsonb`       |                                                                           |
| `byte_size`        | `integer`     | UTF-8 bytes of the checkpoint's export JSON, computed in Go before insert |

Index `(resume_id, started_at DESC)`, and an index leading with `actor_grant_id`
for the foreign key (`foreign_key_indexes_test.go`).

`resumes` gains three nullable columns: `session_actor` (`owner` or
`grant:<uuid>`, checked by pattern), `session_started_at` and
`session_last_write_at`. They change only in writes that already update the row,
so they add no notification.

## Session rule

`resume.CheckpointTx(ctx, q, resumeID, actor, before, now)` runs inside the
write transaction, after the resume row is locked and before its update. `actor`
is the owner, or a grant with its id and client name. `before` is the stored
state as the caller already holds it, converted to the current schema version.

1. Compare `before` with the state the write produces on title, `lng`, personal
   details without `photo`, content and customization, as canonical JSON. Equal:
   do nothing, and leave the session columns unchanged.
2. Start a new session when `session_actor` is null or differs from `actor`,
   `session_last_write_at` is more than 10 minutes before `now`, or
   `session_started_at` is more than an hour before `now`. On a new session,
   insert a checkpoint of `before` and set `session_started_at`.
3. Set `session_actor` and `session_last_write_at`.
4. After an insert, delete the resume's oldest checkpoints while there are more
   than 50 or their `byte_size` sum passes 2 MiB.

Callers:

- `SaveDocumentTx` and `SaveMetadataAndDocumentTx` take the actor and `before`,
  and call the rule. That covers every REST and MCP resume write, photo writes
  included (step 1 ignores a photo-only change), and `removeProjectTx`.
- `ProjectRemoval` carries the acting owner or grant, so a library delete's
  rewrite counts as that actor.
- `CreateTx` starts no session. `BackfillOne` converts schema versions without a
  restorable change and does not call the rule.
- Restore calls the rule with a forced new session and cause `restore`.

The agent authority query (`GetOAuthTokenAuthorityByDigest`) also returns
`oauth_clients.client_name`, so the actor's name is known in the transaction.

## Routes

Owner session only: `sessionCookie` and `csrfToken`, tag `resumes`, the resume
write limiter, and no MCP tool. An agent principal reaching these handlers is
refused like any other session-only route.

| Method and path                                         | operationId               | Headers                       | Success |
| ------------------------------------------------------- | ------------------------- | ----------------------------- | ------- |
| `GET /resumes/{id}/checkpoints`                         | `listResumeCheckpoints`   |                               | 200     |
| `GET /resumes/{id}/checkpoints/{checkpointId}`          | `getResumeCheckpoint`     |                               | 200     |
| `POST /resumes/{id}/checkpoints/{checkpointId}/restore` | `restoreResumeCheckpoint` | `If-Match`, `Idempotency-Key` | 200     |
| `DELETE /resumes/{id}/checkpoints/{checkpointId}`       | `deleteResumeCheckpoint`  | `Idempotency-Key`             | 204     |
| `DELETE /resumes/{id}/checkpoints`                      | `clearResumeCheckpoints`  | `Idempotency-Key`             | 204     |

- The list is an `Envelope` whose `data` holds up to 50 items, newest first,
  with no paging: `id`, `startedAt`, `cause`, `actor` (`{kind: "owner"}` or
  `{kind: "assistant", name}`), `title` and `byteSize`.
- The read returns the same item plus `lng`, `schemaVersion` and `document`
  (`personalDetails` without `photo`, `content`, `customization`), converted to
  the current schema version. The client previews it with the resume's current
  photo, which a restore keeps.
- Restore answers like the other resume mutations, with the saved resume and its
  new revision. Errors use the shared responses: `ResumeNotFound` (also for an
  unknown or foreign checkpoint), `ResumeRevisionMismatch`,
  `ResumePreconditionRequired`, `ResumeIdempotencyConflict`,
  `ResumeDocumentInvalid` and `ResumeRateLimited`.
- Delete and clear touch no resume column and advance no revision.
- `TestRouteInventoryEqualsOpenAPI` and the mutation header contract test cover
  the five routes, and `apps/web/app/api/generated/` is regenerated.

### Restore transaction

It follows the [data](data.md) lock order, then:

1. reads the checkpoint scoped to the resume and the owner's account;
2. converts it to the current schema version and validates it;
3. keeps the current photo, slug, publish, download, search, sign in to view and
   showcase settings, public title and favicon emoji;
4. drops `projectId` from project entries whose library project is missing, as
   `removeProjectTx` does;
5. checkpoints the current state (forced session, cause `restore`) and saves
   through `SaveMetadataAndDocumentTx` with the `If-Match` revision, so the
   revision advances and the usual notification fires;
6. commits with the idempotency record.

## Retention

The daily privacy retention sweep (`privacy_retention.sql`, `Worker.Retain`)
gains a `resume_checkpoints` category: delete checkpoints with `started_at` more
than 30 days old, or with a `schema_version` below the oldest accepted version
in `packages/schema/released-versions.json`, 1,000 per page and 10,000 per run,
with its backlog query and a database test.

## Export

`exportVersion` becomes 3, in Go, the OpenAPI `AccountExport` schema
(`const: 3`), `docs/api/test/privacy.test.ts` and the Go tests. Each
`AccountExportResume` gains `checkpoints`: the read shape above, newest first,
read in the same snapshot. The cap becomes 25,165,824 bytes (24 MiB) in
`exportMaxBytes`, the OpenAPI description and [budgets](budgets.md), with its
test at the new cap. [Operations](operations.md) and the
[projects contract](projects-contract.md) describe the new version.

## Limits

[Budgets](budgets.md) gains these rows, enforced by the session rule and the
sweep:

| Budget                 | Limit                                |
| ---------------------- | ------------------------------------ |
| Checkpoints per resume | 50, and 2 MiB of `byte_size`         |
| Checkpoint age         | 30 days; daily sweep, 1,000/page     |
| Checkpoint session     | Ends after 10 minutes idle or 1 hour |
| Account JSON export    | ≤ 25,165,824 bytes                   |

## Copy

`apps/web/app/i18n/legal-en.ts` and `legal-vi.ts`, with `privacyUpdated` and
`termsUpdated` set to the release date. In "Your controls", after the sentence
about keeping resumes until deletion:

- EN: "Each resume also keeps a history of its earlier text, layout and title
  for up to 30 days, so you can restore them. Text you remove from a resume
  stays in that history until the copy is 30 days old or you delete the history
  in the editor. Deleting a resume deletes its history."
- VI: "Mỗi CV cũng giữ lịch sử nội dung, bố cục và tiêu đề trước đó trong tối đa
  30 ngày để bạn có thể khôi phục. Nội dung bạn xóa khỏi CV vẫn nằm trong lịch
  sử đó cho đến khi bản sao đủ 30 ngày hoặc bạn xóa lịch sử trong trình chỉnh
  sửa. Xóa CV sẽ xóa lịch sử của CV đó."

The export sentence names "resume content and history" (VI: "nội dung và lịch sử
CV"). In "Your content", the permission ends:

- EN: "except for resume history copies until they are 30 days old or you delete
  the history, and backup copies until they expire."
- VI: "trừ các bản trong lịch sử CV cho đến khi chúng đủ 30 ngày hoặc bạn xóa
  lịch sử, và các bản sao lưu cho đến khi chúng hết hạn."

The History panel's layout and copy come from its UI spec.

## Tests

- Store: the session rule for each boundary (no session, actor change, 10
  minutes idle, one hour), a photo-only write, pruning by count and by bytes,
  and a checkpoint for every caller above, MCP included.
- Restore: kept fields, a dropped missing project link, an older schema version,
  a stale `If-Match`, an idempotent replay, an invalid result, and a foreign or
  unknown checkpoint answering 404.
- Delete and clear, the retention category, export version 3 and its cap,
  migration Up-Down-Up, grants and foreign-key index tests, OpenAPI parity, and
  that no MCP tool reaches a checkpoint route.
