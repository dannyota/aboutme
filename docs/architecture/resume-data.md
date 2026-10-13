# Resume data and storage

This page describes implemented behavior. See the
[running-system overview](../architecture.md) for deployment and dependencies.

## Implemented resume data layer

The relational domain and transaction primitives back the complete private
resume HTTP surface. The implemented boundary provides:

- released resume schemas, retained generated types, explicit adjacent
  converters, and released/accepted/emitted version registries derived from the
  [release manifest](../../packages/schema/released-versions.json), under the
  [pre-v1 policy](../adr/0004-resume-document-contract.md#versions-and-converters);
- hand-written Goose migrations as the sole relational schema source, editable
  before the application v1 baseline under
  [ADR 0005](../adr/0005-database-migrations.md);
- sqlc-generated data access from those migrations and `sql/` query sources;
- schema-derived bounds, aggregate validation, and a bounded codec;
- owner-scoped CRUD primitives, a three-resume cap, and revision
  compare-and-swap (CAS) that persists the complete document aggregate;
- a transactional idempotency primitive keyed by user, concrete operation, UUID
  key, and canonical request fingerprint; it stores the exact response with the
  mutation and enforces a bounded candidate deadline;
- pure document projection, explicit adjacent-converter machinery, a one-row CAS
  backfill primitive, independent write, migration, and bounds suites, and a
  bounds completeness guard.

Every mutation uses strict singleton headers, bounded and duplicate-key-safe
decoding, owner-scoped lookup, revision CAS, and one aggregate sanitizer and
validator boundary. A request in an accepted document version is upgraded,
changed, persisted as a complete current-version aggregate, and projected back
to the requested version. The fixed customization allowlist is derived from the
embedded current schema. The hourly global idempotency sweep uses the same
user-first lock order and releases exact retained response bytes in each bounded
transaction.

Released schemas retain their original font identifiers. Adjacent converters
bridge display names and stable catalog IDs; see the
[font catalog design](../design/fonts.md).

The versioned sanitizer allowlist and hostile corpus generate Go and TypeScript
artifacts. Go and TypeScript validators consume the same fixture corpus;
divergent behavior fails both suites. `internal/sanitize.RichText` builds its Go
policy from that artifact; the client wrapper builds DOMPurify policy from the
same data and is a byte- preserving passthrough on server-side rendering. Author
and independent suites cover the corpus, parser boundaries, exact anchor
hardening, idempotence, and deterministic arbitrary input. The Go sanitizer runs
before every resume write and on the public-read and private-print projections.

## Implemented private media boundary

Resume photo objects are never public. Uploads pass session, CSRF, origin,
route-rate, mutation-header, declared-size, and one-task admission checks before
the body is read. The server streams one bounded raw multipart file, validates
the container, fully decodes a static JPEG, PNG, or WebP, applies Exif
orientation, strips metadata, and writes a deterministic bounded JPEG or PNG.

Only the media package creates or parses a photo key. Every read, cleanup
enqueue, and compensation validates that key against the expected resume before
backend I/O. Filesystem and S3 implement the same create-only, bounded-page
contract. Replacement and deletion revoke the database reference and enqueue the
exact old key in one transaction. Definite failures compensate a proved-created
candidate; unknown object-write or database outcomes remain private for later
reconciliation. Scheduled workers process queued deletions and reconcile
orphaned objects; the [privacy runbook](../runbooks/privacy.md) defines their
budgets and failure handling.

## Implemented realtime

One pooled PostgreSQL LISTEN connection fans committed resume notifications into
an in-process hub. Owner streams recheck sessions; public streams expose only
revision metadata and hold a revocable lease. Unpublish, rename, and delete wait
for the public writer to close before returning. Listener loss closes streams
and rejects new admission until LISTEN recovers.

Browsers refetch on every connection, reconnect, and newer revision. Repeated
errors or silence activate conditional polling. Editor refresh reconciles with
pending work; public refresh updates the mounted view and preserves scroll.
Session loss stops owner retries, and browser Back restoration restarts public
updates. See the [realtime runbook](../runbooks/realtime.md) for bounds and
local measurement evidence.

## Account privacy

Settings downloads a bounded JSON copy of the account, complete current-schema
resumes, and normalized photos from one database snapshot. Credentials and
backend keys are excluded. Account deletion requires recent reauthentication and
a fresh typed confirmation. It drains discovery and all affected public resume
generations, then atomically removes account state, creates slug tombstones,
queues exact media keys, and records a durable audit event.

Pending media jobs never expire; completed jobs and lifecycle audits remain for
180 days. Session IP and user agent metadata is redacted after 90 days. See the
[privacy runbook](../runbooks/privacy.md) for command bounds and failure
handling.

## Database roles

The database uses two fixed roles, `aboutme_migrator` and `aboutme_app`:
`db-setup` creates and grants them, and every migration runs as
`aboutme_migrator` behind a PostgreSQL session advisory lock
([ADR 0005](../adr/0005-database-migrations.md)). `aboutme_app` holds exactly
SELECT/INSERT/UPDATE/DELETE on each business table and nothing on
`goose_db_version`. Live tests prove the exact grant set, migrator ownership of
every object, and a store smoke flow under `aboutme_app`.

## Scheduled jobs

The server exposes five one-shot lifecycle commands before HTTP or Chromium
initialization. Database commands load only their required database settings;
media commands also load the private media backend. `release-snapshot-sweep`
uses AWS RDS snapshot metadata and needs no database or media configuration.
Vietnam production schedules only idempotency expiry, queued media deletion,
orphan reconciliation, and privacy retention through four systemd timers.
pgBackRest expires production backups. The
[privacy runbook](../runbooks/privacy.md#commands-and-schedules) owns command
configuration, schedules, deadlines, overlap locks, fixed results, cancellation,
and failure handling.
