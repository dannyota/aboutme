# Task 3: Media storage substrate + pinned local S3-compatible service

Implements [D10](decisions.md). The
[deployment design](../../design/deployment.md#media) reserves `internal/media/`
and puts production media in private S3. The repository has no object storage
today. This task builds the storage layer and local substitute. It ships **no
HTTP surface or image normalizer** — Task 11 owns both.

**Tier:** High risk (handles credentials and private user objects).

**Files:** create
`apps/server/internal/media/{media.go,fs.go,s3.go,conformance_test.go,fs_test.go,s3_test.go}`;
modify `apps/server/internal/config/config.go` + `config_test.go`,
`deploy/compose.yml`, `deploy/README.md`, `.env.example`, and
`apps/server/go.mod` (**exclusive dependency-source window**). The integration
owner applies the reserved `apps/server/go.sum` result. Reports the `Makefile`
and CI changes to the integration owner — does not apply them.

## Interfaces

```go
package media

// ErrNotFound is returned by Get and Delete for an absent key. Both
// backends return exactly this error, never a backend-specific one.
var ErrNotFound = errors.New("media: object not found")
var ErrAlreadyExists = errors.New("media: object already exists")

// Backend is the whole storage contract. Object keys are canonical,
// forward-slash-separated nonempty segments produced only by this package's
// callers (D11); no implementation may interpret a key as a filesystem path
// before validating every segment.
type Backend interface {
    // Put is create-only. It returns ErrAlreadyExists without changing bytes
    // when key already exists.
    Put(ctx context.Context, key, contentType string, body io.Reader, size int64) error
    Get(ctx context.Context, key string) (io.ReadCloser, string, error) // body, contentType
    Delete(ctx context.Context, key string) error
    ListPage(ctx context.Context, prefix, cursor string, limit int) (
        objects []Object, nextCursor string, err error)
}

type Object struct {
    Key       string
    UpdatedAt time.Time
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
startup failure, never a lazy runtime error. For `s3`, bucket and region are
always required. A non-empty custom endpoint requires path-style addressing and
a complete access-key/secret pair; a partial pair or malformed/non-absolute
endpoint fails. An empty endpoint is AWS mode: path-style and both static key
variables must be absent, and the SDK uses the ECS task-role default credential
chain. Credentials are never logged, echoed in an error, or committed. Tests
cover every valid mode and cross-mode/partial combination.

Both backends apply the same key parser before I/O. An object key has no empty,
`.` or `..` segment, repeated separator, leading or trailing separator,
backslash, or NUL. `ListPage` accepts the same canonical prefix with one
optional trailing `/`; it rejects every other alias. Filesystem re-rooting is a
second defense, not the canonicalization rule. S3 receives the validated bytes
unchanged, so FS and S3 cannot name the same object through different strings.

The standalone harness uses only `TEST_S3_ENDPOINT`, `TEST_S3_REGION`,
`TEST_S3_BUCKET`, `TEST_S3_ACCESS_KEY_ID`, `TEST_S3_SECRET_ACCESS_KEY`, and
`TEST_S3_FORCE_PATH_STYLE`. `make test-s3-up` writes their generated disposable
values to `.dev/test-s3.env`; tests never fall back to deployment `MEDIA_*`
credentials. [D10](decisions.md#d10--media-storage-in-a-local-first-repo)
defines the target and port contract.

## Steps

- [ ] **Step 1: failing conformance suite first.** One table-driven suite in
      `conformance_test.go` parameterized over backends, run for `fs` always and
      for `s3` when `TEST_S3_ENDPOINT` is set. It must cover: round-trip put/get
      including the content type; `Get` and `Delete` of an absent key →
      `ErrNotFound` from **both** backends; a second `Put` at the same key →
      `ErrAlreadyExists` and leaves the original bytes/content type unchanged;
      `ListPage` returns stable prefix-scoped pages at the exact limit, advances
      a cursor without duplicates, exposes update time for the age gate, and
      never lists neighbours; empty keys and keys containing an empty, `.`, or
      `..` segment, repeated `/`, a leading or trailing `/`, a backslash, or a
      NUL are rejected by both backends **before** any I/O. Prefix tests cover
      the one allowed trailing delimiter and the same alias cases. `Put` accepts
      only a body whose EOF is exactly at `size` and leaves no partial object
      when the body is shorter or longer; a cancelled context aborts without
      writing. Inject unique sentinel access-key and secret values into
      configuration, backend errors, and a failing SDK response. Assert neither
      sentinel appears in returned errors or captured logs. Startup errors may
      name a missing variable but never its value.
- [ ] Both implementations enforce create-only atomically: filesystem uses
      exclusive create in the target directory followed by safe cleanup of only
      its own partial file; S3 uses conditional `If-None-Match: *` and maps only
      the documented collision response to `ErrAlreadyExists`. A concurrent
      same-key test proves a loser cannot overwrite or delete the winner.
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
      P2A's D1 requires for a new module. In the same exclusive dependency
      window, pin the latest stable `golang.org/x/image` for Task 11's WebP
      decoder and Catmull–Rom scaler. It must not be older than `v0.43.0`, which
      fixes [GO-2026-5061](https://pkg.go.dev/vuln/GO-2026-5061). At this plan's
      2026-08-12 review, the latest stable release is `v0.45.0`; recheck at
      scaffold time under the repository's dependency rule. Recheck and promote
      the already-transitive `golang.org/x/text/language` dependency to a direct
      exact pin for Task 6's BCP 47 parse/canonicalize path.
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
- [ ] **Step 5: report the owner changes.** Supply exact Makefile recipes for
      `test-s3-up`, `test-s3-down`, `server-test-s3`, and `server-test-p2b-s3`
      that implement D10's container, credential-file, environment, fail-closed,
      and package-command contract. The integration owner applies and verifies
      those shared targets before this task's gate.
- [ ] **Step 6: gate.** Run `make server-build server-vet server-test`,
      `make test-s3-up`, `make server-test-s3`, and
      `gitleaks dir --no-banner --redact .`. Record the test tally. A missing
      service or `.dev/test-s3.env` must fail `server-test-s3`.
- [ ] **Step 7: handoff.** Report the owned paths, failing-test evidence,
      dependency graph delta, pinned image and module versions, exact checks,
      and shared-target recipes to the integration owner. Do not stage or
      commit.
- [ ] **Step 8: independent defect review**, with the reviewer asked
      specifically about credential handling, key-escape rejection, and whether
      the two backends can diverge without the conformance suite noticing.

## Acceptance mapping

| Row          | What this task contributes                                                  |
| ------------ | --------------------------------------------------------------------------- |
| AC-MEDIA-004 | The one contract, both backends, and the fail-closed S3 run — the whole row |
| AC-MEDIA-002 | The key-escape and traversal rejection both backends enforce                |
| AC-MEDIA-003 | Exact-key deletion and bounded listing support safe lifecycle cleanup       |
| AC-MEDIA-007 | Stable pages and object age make the orphan sweep bounded and race-safe     |
| AC-MEDIA-009 | Context cancellation and exact-size writes support bounded intake           |
