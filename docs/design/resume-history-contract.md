# Resume history contract

Status: Accepted (2026-11-21) after an independent review; the owner approved
its legal copy. The contract for release v0.10.0 of the
[change review](change-review.md) design, sections "Checkpoints" and "Release
order" item 1. Product rules stay in that design and the
[History panel spec](ui/resume-history.md); this page fixes storage, write-path
hooks, routes, export, retention and copy.

## Storage

Migration `00022_resume_checkpoints.sql`, with `SET LOCAL lock_timeout = '5s'`
in Up and Down, explicit grants to `aboutme_app`
([ADR 0005](../adr/0005-database-migrations.md)), and an entry in
`grants_test.go`.

`resume_checkpoints`:

| Column             | Type          | Rule                                                                      |
| ------------------ | ------------- | ------------------------------------------------------------------------- |
| `id`               | `uuid`        | Primary key, `DEFAULT uuidv7()`                                           |
| `resume_id`        | `uuid`        | `REFERENCES resumes (id) ON DELETE CASCADE`                               |
| `started_at`       | `timestamptz` | Session start; the order and retention key                                |
| `cause`            | `text`        | `CHECK` in `edit`, `assistant`, `restore`, `accept`; v0.10.0 writes three |
| `actor_grant_id`   | `uuid` null   | `REFERENCES oauth_grants (id) ON DELETE SET NULL`                         |
| `actor_name`       | `text` null   | Client display name copied at insert; may be empty                        |
| `title`, `lng`     | `text`        | The same `CHECK` limits as `resumes`                                      |
| `schema_version`   | `integer`     | Version of the stored document                                            |
| `personal_details` | `jsonb`       | Without `photo`                                                           |
| `content`          | `jsonb`       |                                                                           |
| `customization`    | `jsonb`       |                                                                           |
| `byte_size`        | `integer`     | `CHECK (byte_size > 0)`; see below                                        |

- `CHECK ((cause = 'assistant') = (actor_name IS NOT NULL))`: an assistant row
  keeps its name after its grant is deleted; owner rows hold neither.
- Indexes: `(resume_id, started_at DESC)` for reads and pruning, `(started_at)`
  for the sweep, and one leading with `actor_grant_id` for the foreign key
  (`foreign_key_indexes_test.go`).
- `byte_size` is the UTF-8 length of the JSON of `title`, `lng`, `schemaVersion`
  and `document` as stored, encoded by `encoding/json` with
  `SetEscapeHTML(false)`. It excludes ids, times, actor fields and itself.

`resumes` gains `session_actor` (`owner` or `grant:<uuid>`, checked by pattern),
`session_started_at` and `session_last_write_at`, all three null or all three
set.

## Session rule

`resume.CheckpointTx(ctx, q, input)` runs inside the write transaction, under
the owner row lock (`LockUserForResumeWrite`), before the resume update. `input`
holds the resume id, the actor (the owner, or a grant id), the cause, `force`,
the stored state before the write (`before`, as the caller holds it, converted
to the current schema version), the state the write produces, the current
session columns and `now`. It returns the session values to write.

1. Compare `before` with the result on title, `lng`, personal details without
   `photo`, content and customization, as canonical JSON. Equal and not `force`:
   return the session columns unchanged and insert nothing.
2. Start a new session when `force` is set, `session_actor` is null or differs
   from the actor, `session_last_write_at` is more than 10 minutes before `now`,
   or `session_started_at` is more than an hour before `now`. On a new session,
   insert a checkpoint of `before` and set `session_started_at`. For a grant,
   look up the client's display name through the grant at insert.
3. Return `session_actor` and `session_last_write_at = now`.
4. After an insert, delete the resume's oldest checkpoints while there are more
   than 50 or their `byte_size` sum passes 2 MiB.

`UpdateResumeDocumentCAS` and `UpdateResumeMetadataAndDocumentCAS` set the three
session columns from the returned values, so the rule adds no `UPDATE` and no
notification of its own.

Callers:

- `SaveDocumentTx` and `SaveMetadataAndDocumentTx` take a checkpoint argument
  (actor, cause, `force`) and call the rule once. That covers every REST and MCP
  resume write, photo writes included (step 1 skips a photo-only change), and
  `removeProjectTx`.
- `ProjectRemoval` carries the acting owner or grant id, so a library delete's
  rewrite counts as that actor.
- `CreateTx` starts no session. `BackfillOne` converts schema versions without a
  restorable change and does not call the rule.
- `Store.SaveTitle`, `saveTitleTx` and `UpdateResumeTitleCAS`, used only by
  tests, are deleted.

## Routes

All five are session routes (`sessionCookie`), and the three writes also need
`csrfToken`, as `getResume` and the resume writes do. They take tag `resumes`,
path parameters `ResumeID` and `CheckpointID`, the read limiter on `GET` and the
resume write limiter otherwise. They are not agent operations, and each handler
also answers `ResumeNotFound` to an agent principal; a test calls each with one.

| Method and path                                         | operationId               | Headers                       | Success |
| ------------------------------------------------------- | ------------------------- | ----------------------------- | ------- |
| `GET /resumes/{id}/checkpoints`                         | `listResumeCheckpoints`   |                               | 200     |
| `GET /resumes/{id}/checkpoints/{checkpointId}`          | `getResumeCheckpoint`     |                               | 200     |
| `POST /resumes/{id}/checkpoints/{checkpointId}/restore` | `restoreResumeCheckpoint` | `If-Match`, `Idempotency-Key` | 200     |
| `DELETE /resumes/{id}/checkpoints/{checkpointId}`       | `deleteResumeCheckpoint`  | `Idempotency-Key`             | 204     |
| `DELETE /resumes/{id}/checkpoints`                      | `clearResumeCheckpoints`  | `Idempotency-Key`             | 204     |

- Every checkpoint query joins `resumes` on both `id` and `user_id`. A
  checkpoint of another resume or account, an expired one, or one at a schema
  version the release does not accept answers `ResumeNotFound`.
- The list is an `Envelope` whose `data` holds up to 50 items, newest first,
  with no paging: `id`, `startedAt`, `cause`, `actor` (`{kind: "owner"}` or
  `{kind: "assistant", name}`), `title` and `byteSize`. It shows only
  checkpoints from the last 30 days at an accepted schema version.
- The read returns the same item plus `lng`, `schemaVersion` and `document`
  (`personalDetails` without `photo`, `content`, `customization`), converted to
  the current schema version. The client previews it with the current photo.
- Responses. List and read: `200`, `400`, `401`, `404`, `405`, `429` and `500`,
  with no `ETag` or `SchemaVersionHeader`, since the body is always at the
  current version. Restore follows `updateResumePersonalDetails`:
  `SchemaVersionHeader`, `400`, `401`, `403`, `404`, `405`, `409`, `412`, `422`,
  `428`, `429`, `500` and `PublicStateBusy`. Delete and clear: `204`, `400`,
  `401`, `403`, `404`, `405`, `409`, `429` and `500`.
- Delete and clear touch no resume column, advance no revision and take no
  `If-Match`. [API](api.md) records them as exceptions to the `If-Match` rule,
  and the mutation header contract test lists them.
- `TestRouteInventoryEqualsOpenAPI` covers the five routes, and
  `apps/web/app/api/generated/` is regenerated.

### Restore

Restore is a resumeapi `mutationSpec` with `nonDrainingTransition`, so it takes
the public-state coordinator like every other resume write. Its operation:

1. reads the checkpoint through the scoped query above;
2. converts it to the current schema version;
3. keeps the current photo, slug, publish, download, search, sign in to view and
   showcase settings, public title and favicon emoji;
4. drops `projectId` from project entries whose library project is missing, as
   `removeProjectTx` does;
5. passes the document through `prepareDocumentForPersistence`, which sanitizes
   and validates it;
6. saves through `SaveMetadataAndDocumentTx` with cause `restore` and `force`,
   at the `If-Match` revision, so the state before is checkpointed once, the
   revision advances, and the usual notification fires;
7. commits with the idempotency record.

## Retention

The daily privacy retention sweep (`privacy_retention.sql`, `Worker.Retain`)
gains a `resume_checkpoints` category: delete checkpoints with `started_at` more
than 28 days old, so a daily run removes each before it is 30 days old even
after a late or long run, or with a `schema_version` outside `acceptedVersions`
in `packages/schema/released-versions.json`; 1,000 per page and 10,000 per run,
with its backlog query and a database test. Reads and export filter at 30 days
and at accepted versions, so a backlog never shows an older checkpoint.

## Export

`exportVersion` becomes 3, in Go, the OpenAPI `AccountExport` schema
(`const: 3`), `docs/api/test/privacy.test.ts` and the Go tests. Each
`AccountExportResume` gains `checkpoints`: the read shape above, newest first,
read in the same snapshot, filtered as reads are. The cap becomes 25,165,824
bytes in `exportMaxBytes`, the OpenAPI description and [budgets](budgets.md),
with its test at the new cap. [Operations](operations.md) and the
[projects contract](projects-contract.md) describe the new version.

[Budgets](budgets.md) gains rows for checkpoints per resume, checkpoint age and
the checkpoint session with the values in the [design](change-review.md#limits),
and its account export row becomes "≤ 25,165,824 bytes; 5/min per account and
IP".

## Copy

`apps/web/app/i18n/legal-en.ts` and `legal-vi.ts`, with `privacyUpdated` and
`termsUpdated` set to the release date. In "Your controls", these sentences
replace "We keep your account, resumes, and photos until you delete them or your
account. The export includes your profile, resume content, and resume photos."
and their Vietnamese counterparts:

- EN: "We keep your account, resumes, and photos until you delete them or your
  account. Each resume also keeps earlier versions of its text, contact details,
  layout, title, and language for up to 30 days, so you can restore them. Text
  you remove from a resume stays in those versions until they are 30 days old or
  you delete the resume's history in the editor. Deleting a resume or your
  account deletes its history. The export includes your profile, your resumes
  with their history, and resume photos."
- VI: "Chúng tôi giữ tài khoản, CV và ảnh của bạn cho đến khi bạn xóa chúng hoặc
  xóa tài khoản. Mỗi CV còn giữ các phiên bản trước của nội dung, thông tin liên
  hệ, bố cục, tiêu đề và ngôn ngữ trong tối đa 30 ngày để bạn có thể khôi phục.
  Nội dung bạn xóa khỏi CV vẫn nằm trong các phiên bản đó cho đến khi chúng đủ
  30 ngày hoặc bạn xóa lịch sử của CV trong trình chỉnh sửa. Xóa CV hoặc tài
  khoản sẽ xóa lịch sử của CV. Bản xuất gồm hồ sơ, các CV cùng lịch sử của
  chúng, và ảnh trong CV của bạn."

In "Your content", the sentence beginning "This permission ends" becomes:

- EN: "This permission ends when you delete the content, except for earlier
  versions in a resume's history until they are 30 days old or you delete that
  history, and backup copies until they expire."
- VI: "Quyền này chấm dứt khi bạn xóa nội dung, trừ các phiên bản trước trong
  lịch sử CV cho đến khi chúng đủ 30 ngày hoặc bạn xóa lịch sử đó, và các bản
  sao lưu cho đến khi chúng hết hạn."

## Tests

- Session rule: each boundary (no session, actor change, 10 minutes idle, one
  hour, `force`), a photo-only write, pruning by count and by bytes, a
  checkpoint for every caller above with MCP included, an owner and an assistant
  racing on one resume, and no extra notification.
- Restore: kept fields, a dropped missing project link, an older schema version,
  markup the sanitizer strips, a stale `If-Match`, an idempotent replay, an
  invalid result, and `PublicStateBusy`.
- Scope: every route with another resume's and another account's checkpoint id
  answers 404; every route refuses an agent principal.
- Lifecycle: delete and clear; resume and account deletion cascades
  (`assertAccountCascadeCounts`); a deleted grant leaving `actor_name`; the
  retention category; a checkpoint at an unaccepted version skipped by list,
  read and export.
- Export version 3 and its cap; migration Up-Down-Up; grants and foreign-key
  index tests; OpenAPI parity and the header contract.
