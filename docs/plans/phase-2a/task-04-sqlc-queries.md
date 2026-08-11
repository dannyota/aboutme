# Task 4: sqlc queries + regenerated store layer

Structural prerequisite; no acceptance ID.

**Files:** append `apps/server/sql/queries.sql`; regenerate
`apps/server/internal/store/` (committed, never hand-edited); create
`apps/server/internal/resume/resume_shapes_test.go` (compile-time shape
assertions, the P1 Task 1 pattern).

**Queries produced** (names verbatim — later tasks import them):

```sql
-- name: LockUserForResumeWrite :one
SELECT id FROM users WHERE id = $1 FOR UPDATE;

-- name: CreateResume :one
INSERT INTO resumes (user_id, title, schema_version, lng,
                     personal_details, content, customization)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *;

-- name: GetResumeForUser :one
SELECT * FROM resumes WHERE id = $1 AND user_id = $2;

-- name: ListResumesForUser :many
SELECT * FROM resumes WHERE user_id = $1 ORDER BY created_at, id;

-- name: CountResumesForUser :one
SELECT count(*) FROM resumes WHERE user_id = $1;

-- name: DeleteResumeForUser :execrows
DELETE FROM resumes WHERE id = $1 AND user_id = $2;

-- name: UpdateResumeDocumentCAS :one
UPDATE resumes
SET personal_details = $4, content = $5, customization = $6,
    schema_version = $7, revision = revision + 1, updated_at = now()
WHERE id = $1 AND user_id = $2 AND revision = $3
RETURNING revision;

-- name: UpdateResumeTitleCAS :one
UPDATE resumes
SET title = $4, revision = revision + 1, updated_at = now()
WHERE id = $1 AND user_id = $2 AND revision = $3
RETURNING revision;

-- name: BackfillResumeDocumentCAS :execrows
-- D12, all three omissions deliberate — do NOT "fix" them: this does not
-- bump `revision` (a backfill rewrites storage to something byte-identical
-- to what every reader was already served, so nothing observable changes),
-- does not bump `updated_at` (which tracks user-visible change), and is not
-- user-scoped (it is a system job).
-- Fully named params (owner decision 2026-08-03): the from/to schema
-- versions are both int32 and sqlc's positional naming would emit
-- `SchemaVersion` and `SchemaVersion_2`, neither carrying its direction.
-- A caller swapping them silently rewrites current rows back to the old
-- version. Named args make the pair unswappable at the call site.
UPDATE resumes
SET personal_details = sqlc.arg(personal_details),
    content = sqlc.arg(content),
    customization = sqlc.arg(customization),
    schema_version = sqlc.arg(to_schema_version)
WHERE id = sqlc.arg(id)
    AND schema_version = sqlc.arg(from_schema_version)
    AND revision = sqlc.arg(revision);

-- name: ListResumeIDsBelowSchemaVersion :many
SELECT id FROM resumes WHERE schema_version < $1 ORDER BY id LIMIT $2;

-- name: CreateIdempotencyRecord :exec
INSERT INTO idempotency_records
    (user_id, route, idempotency_key, request_hash,
     response_status, response_body, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetIdempotencyRecord :one
SELECT * FROM idempotency_records
WHERE user_id = $1 AND route = $2 AND idempotency_key = $3;

-- name: DeleteIdempotencyRecordIfExpired :execrows
DELETE FROM idempotency_records
WHERE user_id = $1 AND route = $2 AND idempotency_key = $3
    AND expires_at <= $4;

-- name: DeleteExpiredIdempotencyRecordsForUser :execrows
-- D11 opportunistic reaping: every Execute commits this per-user cleanup
-- before its mutation transaction, so expiry enforcement survives a rejected
-- key reuse or mutation error instead of depending on a future global job.
DELETE FROM idempotency_records
WHERE user_id = $1 AND expires_at <= $2;
```

Note `BackfillResumeDocumentCAS` deliberately does **not** touch `revision` or
`updated_at` (D12; `updated_at` tracks user-visible changes for the same reason)
and is not user-scoped (a system job). No `slug_tombstones` queries (D15 — P5A
defines that contract).

- [x] **Step 1: failing compile-time shape test** pinning the generated contract
      later tasks build on:
      `store.Resume{ID, UserID uuid.UUID, Title string, Slug *string, Live,     DownloadEnabled, SeoGeoEnabled bool, SchemaVersion int32, Revision     int64, Lng *string, PersonalDetails, Content, Customization     json.RawMessage, CreatedAt, UpdatedAt time.Time}`
      and `store.IdempotencyRecord{…}` (per the committed sqlc.yaml overrides:
      pointers for nullables, `json.RawMessage` for jsonb). Run
      `cd apps/server && go test ./internal/resume/...` → **FAIL**. **Owner
      decision (2026-08-03), replacing this step's open question:** sqlc emits
      `SeoGeoEnabled`, which breaks the Go initialism rule and would disagree
      with Task 6's domain field `SEOGeoEnabled` — two names for one concept
      differing only in casing, meeting at the codec. Add
      `seo_geo_enabled: "SEOGeoEnabled"` to `sqlc.yaml`'s existing `rename:`
      block, which already carries exactly this correction for `ua`, `ip`,
      `csrf_secret`, `pkce_verifier`, `redirect_uri` and `oauth_transaction`.
      This regenerates `internal/store/models.go` a second time (Task 3 landed
      it first under owner correction 1); `sqlc.yaml` and the regenerated output
      are therefore in **this** task's scope and commit.
- [x] **Step 2: append queries, `make sqlc-gen`, commit generated output; Step 1
      compiles green.**
- [x] **Step 3: gate.** `make sqlc-check` (regenerate → no diff),
      `make server-build server-vet server-test`, `make data-drift`.
- [x] **Step 4: commit** —
      `git commit -m "feat(resume): add resume and idempotency sqlc queries" -- apps/server/sql/queries.sql apps/server/sqlc.yaml apps/server/internal/store apps/server/internal/resume`
      (`sqlc.yaml` added 2026-08-03: the `SEOGeoEnabled` rename above lives
      there, and omitting it from the commit would leave `make sqlc-check`
      drifting on the next run.)
