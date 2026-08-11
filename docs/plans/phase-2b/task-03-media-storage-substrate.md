# Task 3: Media storage substrate + pinned local S3-compatible service

Implements [D10](decisions.md). The spec reserves `internal/media/` and says
media lives in S3 behind CloudFront in production; the repo has no object
storage of any kind today, and the owner's rule is that everything runs locally
until the product is done. This task builds the storage layer and the local
substitute. It ships **no HTTP surface** — Task 11 owns that.

**Tier:** High risk (handles credentials; enforces resource bounds on untrusted
input).

**Files:** create
`apps/server/internal/media/{media.go,fs.go,s3.go,conformance_test.go,fs_test.go,s3_test.go}`;
modify `apps/server/internal/config/config.go` + `config_test.go`,
`deploy/compose.yml`, `deploy/README.md`, `.env.example`,
`apps/server/go.mod`/`go.sum` (**exclusive dependency window**). Reports the
`Makefile` and CI changes to the integration owner — does not apply them.

## Interfaces

```go
package media

// ErrNotFound is returned by Get and Delete for an absent key. Both
// backends return exactly this error, never a backend-specific one.
var ErrNotFound = errors.New("media: object not found")

// Backend is the whole storage contract. Keys are opaque, forward-slash
// separated, and produced only by this package's callers (D11); no
// implementation may interpret a key as a filesystem path without first
// rejecting traversal.
type Backend interface {
    Put(ctx context.Context, key, contentType string, body io.Reader, size int64) error
    Get(ctx context.Context, key string) (io.ReadCloser, string, error) // body, contentType
    Delete(ctx context.Context, key string) error
    DeletePrefix(ctx context.Context, prefix string) (deleted int, err error)
}

// NewFS returns the filesystem backend rooted at dir: native development
// and every unit test. It refuses any key that escapes dir after cleaning.
func NewFS(dir string) (Backend, error)

// NewS3 returns the S3-compatible backend. Endpoint is empty for real AWS
// and set for the local service; PathStyle is required for the latter.
func NewS3(ctx context.Context, cfg S3Config) (Backend, error)
```

Configuration (`internal/config`, validated at startup, fail-closed):
`MEDIA_BACKEND` (`fs` | `s3`), `MEDIA_FS_DIR`, `MEDIA_BUCKET`, `MEDIA_REGION`,
`MEDIA_ENDPOINT`, `MEDIA_ACCESS_KEY_ID`, `MEDIA_SECRET_ACCESS_KEY`,
`MEDIA_FORCE_PATH_STYLE`. A missing required value for the selected backend is a
startup failure, never a lazy runtime error. Credentials are never logged, never
echoed in an error, and never committed.

## Steps

- [ ] **Step 1: failing conformance suite first.** One table-driven suite in
      `conformance_test.go` parameterized over backends, run for `fs` always and
      for `s3` when `TEST_S3_ENDPOINT` is set. It must cover: round-trip put/get
      including the content type; `Get` and `Delete` of an absent key →
      `ErrNotFound` from **both** backends; overwrite replaces content;
      `DeletePrefix` removes every object under the prefix and reports the
      count, leaves neighbours untouched, and is a no-op returning 0 for an
      unused prefix; a key containing `..`, a leading `/`, a backslash, or a NUL
      is rejected by both backends **before** any I/O; `Put` refuses a body
      longer than `size`; a cancelled context aborts without writing.
- [ ] **Step 2: skip-or-fail-closed harness.** Add `RequireTestS3(t)` in the
      same shape as `testutil.RequireMigratedTestDatabaseURL`: skip when
      `TEST_S3_ENDPOINT` is unset, **fail** when `REQUIRE_TEST_S3=1` is set and
      it is missing, so a gate run cannot pass vacuously. Prove the fail-closed
      arm with its own test.
- [ ] **Step 3: implement both backends; green.** `fs` cleans and re-roots every
      key and refuses escapes; `s3` uses AWS SDK for Go v2 pinned at latest
      stable, with a custom endpoint resolver and path-style addressing when
      `MEDIA_ENDPOINT` is set, and maps `NoSuchKey`/404 to `ErrNotFound`. Record
      the transitive dependency delta from `go mod graph` in the task report, as
      P2A's D1 requires for a new module.
- [ ] **Step 4: the local service.** Add a pinned, fully qualified
      `docker.io/minio/minio:<exact tag>` service to `deploy/compose.yml` on a
      **new `media` network shared only with `server`** — never `edge`, never
      `frontend`, so a compromise of Caddy or Nuxt cannot reach it — plus a
      one-shot `docker.io/minio/mc:<exact tag>` container that creates the
      bucket and exits (the `migrate` service is the established one-shot
      precedent, including `healthcheck: disable: true`). Credentials come from
      `.env` with **no baked-in default**, exactly as `POSTGRES_PASSWORD` does,
      because `compose.yml` is also the self-hosting artifact. Document both in
      `deploy/README.md` and `.env.example`.
- [ ] **Step 5: report the owner changes.** `make test-s3-up` / `test-s3-down`
      (standalone pinned MinIO + bucket creation, mirroring `test-db-up`, so the
      S3 suite runs without the heavyweight compose stack), and
      `./internal/media/...` added to `server-test-db` with `REQUIRE_TEST_S3=1`.
      Report the exact target text; the integration owner applies it.
- [ ] **Step 6: gate.** `make server-build server-vet server-test`; then, with
      the local service up,
      `REQUIRE_TEST_S3=1 TEST_S3_ENDPOINT=… go test     ./internal/media/... -race -count=1 -v`
      and record the tally. Confirm `gitleaks` is clean over the diff.
- [ ] **Step 7: commit** —
      `git commit -m "feat(media): add object storage backends and the local service" -- apps/server/internal/media apps/server/internal/config apps/server/go.mod apps/server/go.sum deploy .env.example`
- [ ] **Step 8: independent defect review**, with the reviewer asked
      specifically about credential handling, key-escape rejection, and whether
      the two backends can diverge without the conformance suite noticing.

## Acceptance mapping

| Row          | What this task contributes                                                     |
| ------------ | ------------------------------------------------------------------------------ |
| AC-MEDIA-004 | The one contract, both backends, and the fail-closed S3 run — the whole row    |
| AC-MEDIA-002 | The key-escape and traversal rejection both backends enforce                   |
| AC-MEDIA-003 | `DeletePrefix`, the primitive replace/delete/resume-delete cleanup is built on |
