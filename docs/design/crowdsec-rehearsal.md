# CrowdSec host rehearsal

This is the executable protocol for the
[pre-cutover memory gate](vietnam-host-memory.md#rehearsal-gate). It uses one
temporary synthetic host before production changes. The existing numeric
budgets, start guards, notice authority, and cutover contract remain mandatory.
The values below define the previously unspecified workload and sampling, not
permission to raise a service ceiling or admit a forbidden cutover workload.

## Fixed command surfaces

Add `deploy/vn/scripts/crowdsec-rehearsal.sh` with only these operations:

| Operation              | Contract                                                                                                                                                                        |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `plan <candidate-tag>` | Validate exact reviewed release/images, produce a create-only infrastructure plan, quote it, and record the nonsecret run manifest. No cloud writes.                            |
| `run <candidate-tag>`  | Consume that matching plan, create one isolated host/resources, bootstrap, cut over, seed, measure, and clean up. No arbitrary host, command, URL, image, or database argument. |
| `cleanup`              | Read this operation's recorded exact resource IDs, reconcile uncertain creates, and remove only its resources. Never rerun workloads or adopt another operation.                |

The fixed host helper `deploy/vn/probe/crowdsec-rehearsal-host.sh` exposes only
`bootstrap`, `seed`, `sample`, `measure`, and `verify`; the coordinator supplies
validated private run context over a bounded inherited pipe. No callback,
shell-text command, caller-selected path, existing host adoption, or environment
override replaces that context. Deployment and cutover still use their reviewed
fixed commands and retain their own locks. The helper never writes fence,
notice, permit, mode, lease, or release success evidence itself.

The run manifest records schema version 1, a random 16-lowercase-hex run ID,
candidate commit/tag and AMD64 image digests, the pinned bootstrap identity,
provider project/zone/flavor/image IDs, resource IDs/names, origin, quote,
creation deadline, fixture hashes, and measurement protocol version 1. It has no
credential values. Store it mode 0600 outside the repository under the primary's
scratch directory. Keep a single active-run marker there; any live or unresolved
prior rehearsal refuses a new run. Every effect rechecks the manifest's exact
host identity, isolated names, origin, and bundle hashes.

`run` has a four-hour whole-operation deadline from the first resource create,
including measurement and normal cleanup. It traps interruption into bounded
cleanup. Timeout/failure never means acceptance. Cleanup failure retains the
manifest and blocks another host until the exact leftovers are reconciled. There
is no `--keep`, retry-until-green, or automatic takeover.

## Resources, quote, and isolation

Use one fixed rehearsal OpenTofu root under `deploy/vn/rehearsal/`, with the
same pinned providers and resource shapes as `prod/` and `dns/`. It owns only
one server, encrypted volume/attachment, dedicated security group/rules, and one
DNS record. Reference the existing operator-selected project/VPC/subnet; do not
import, edit, or destroy production resources. Use separate local encrypted
state/plan files in the scratch directory, never production state. This bounded
root is not a reusable VM framework or standing environment.

Resource names are `aboutme-cs-rehearsal-<run-id>` with `-data`, `-media`, or
`-backups` where required. Refuse any pre-existing name or ambiguous create;
reconcile the exact attempted name/ID before another effect. Require the
production-matching AMD64 Ubuntu 24.04 image and verified 2 vCPU/4 GiB flavor,
30 GB encrypted root, and 20 GB encrypted data volume. Preserve the encryption
type, filesystem, bind mounts, and service settings from the host contract. No
second rehearsal host may overlap this one, including failed leftovers.

Reuse `quote.sh --plan <saved-plan> --max-price 1047880` for the server, disks,
encryption, and floating IP. This is a monthly-equivalent ceiling in VND,
including the existing quote's VAT treatment, not an asserted actual charge. Any
missing/zero price or higher total refuses creation. Use the existing vStorage
project only after proving at least 2 GiB provider-reported unused allowance;
create no new paid storage project and change no quota/renewal setting. Before
apply, verify every planned write names only this run's new resources.
Revalidate the quote if plan inputs change. The primary reviews the concrete
plan normally.

For that allowance, the primary reads the existing HCM04 project's console
Information panel: **Total quota** and **Total usage**, not bucket-size sums.
Record a private `storage-allowance.json` in the run's fixed scratch directory:
version 1, run ID, exact project/region, observation UTC time, displayed numeric
values/units/precision, conservative quota-lower and usage-upper bytes, and the
digest of the private cropped evidence. Capture no keys or account details. The
primary verifies the transcription; this is evidence input, not a bypass flag or
caller-selected API endpoint. Bind it to the plan and refresh before `run`; it
must be at most five minutes old before the first resource creation.

Use exact integer byte arithmetic: `quota_lower_bytes - usage_upper_bytes` must
be at least 2,147,483,648. Exact byte counters need no rounding allowance. For
rounded displays subtract one displayed quantum from quota and add one to usage
before converting. For GB without a stated decimal/binary convention, use
1,000,000,000 bytes for the quota lower bound and 1,073,741,824 for the usage
upper bound. Reject negative/nonfinite values, absent/ambiguous units, identity
mismatch, stale observation, missing evidence, or an insufficient bound before
any create. Omitted usage is never zero. The current SDK's quota-only Project
type cannot supply this proof; invent no `used` field.

This gate measures reported project capacity at observation time. Provider
documentation does not specify Total usage refresh lag or multipart accounting;
the evidence must state both limits. It is not a reservation or a guarantee
about unreported in-flight parts or later writes. No bucket-sum inference,
project-wide object scan, new paid project, quota change, or additional
freshness claim replaces this operational observation. A storage-full response
during the rehearsal fails it and enters cleanup; never expand capacity
automatically.

Origin is exactly `https://cs-rehearsal-<run-id>.aboutme.vn`. Create only that
DNS-only A record in the existing zone, TTL 300, pointing at this host's
floating IPv4. Reuse the existing Cloudflare provider shape, with separate
state; never import the zone's existing records. Leave parent CAA/DNSSEC alone.
Set `DEPLOY_SITE` and `PUBLIC_ORIGIN` to this origin, `DIRECT_HOST` to its host,
and `DIRECT_WWW_HOST` to the empty string, which the current Caddy entrypoint
supports. Keep public certificate verification and HTTP-01 issuance enabled. No
insecure TLS, production hostname, host-header spoofing, or hosts-file alias.

Allow TCP 80 for ACME and TCP 443/SSH only from the declared
operator/load-client addresses. Pin the generated SSH host key before the first
connection using the existing temporary-host cloud-init pattern. SSH never goes
through the production host. The load generator runs outside the measured
server, from one declared address. Host egress supports packages, registry,
ACME, DNS/time, and the existing regional storage endpoint; no production
database/SSH access or external SMTP delivery is needed. Close the initial SSH
connection if identity or origin validation changes rather than selecting a
replacement host.

Create two new private SSE-S3 encrypted buckets and dedicated service accounts
using the exact `buckets.sh` policy and attached-key generation patterns. Grant
each account object work only on its own bucket. Keep the media and backup
accounts separate. Verify encryption/policies before uploads. Attached keys
still expose the provider's documented bucket metadata capabilities; this is
object isolation, not a new storage-project isolation claim.

Generate every application key/password and the pgBackRest cipher pass on this
host through the existing secret helper. Create temporary storage keys directly
into mode-0600 tmpfs files and pipe them into `secrets-host.sh import-s3` for
the exact target; remove source files after confirmed import. Never call the
production `buckets.sh` entry point with its fixed production names, copy
production secret files, or use `move-from-ssm`, `age-copy`, or restore-drill.

Before any app/backup start, install and verify isolated values:

| Setting                          | Required value                                                                                             |
| -------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `MEDIA_BUCKET`                   | This run's `-media` bucket                                                                                 |
| Media endpoint/region/path style | Existing HCM04 vStorage settings                                                                           |
| `repo1-s3-bucket` / `repo1-path` | This run's `-backups` bucket / `/rehearsal`                                                                |
| `ENV` / mail mode                | `prod` / `smtp`                                                                                            |
| SMTP                             | `localhost`, port 465, implicit TLS, fresh unused credentials; no listener and no external delivery        |
| Provider login and enrollment    | Disabled; no external identity credentials                                                                 |
| Password registration            | Disabled through notice activation; enabled only for the fixed synthetic registration, then disabled again |

Set mail sender to a synthetic address under this run's child domain. Generate
required but unused secret slots locally. No mail capture/dev mode or SMTP
credential from production is allowed. The fixed registration helper removes its
synthetic verification job transactionally before measurements. An SMTP failure
during setup is not a successful mail proof and may not enter evidence with
recipient/token values. Alarm delivery is not assessed by this rehearsal.

## Reach the candidate's ordinary state

1. Pin the reviewed `v0.8.9` host bundle and its exact released AMD64 images in
   the run manifest. Verify their release provenance. On the new empty host, run
   its full installer, data-volume preparation, generated-secret setup, isolated
   repository stanza creation, and first-deploy host steps. This baseline
   supplies the supported fresh full-host package/bootstrap path.
2. The rehearsal controller uses the fixed baseline host-step sequence from
   `deploy.sh` through the matching installed `deploy-host.sh`: acquire the
   normal deploy owner, validate/pull images, backup, stop job timers, start
   maintenance, stop serving, render, database setup, migrate, start app, stop
   maintenance, resume timers, smoke, release. Preserve step guards and private
   owner handling. Do not run the current laptop deployer against mismatched
   legacy helper hashes or disable its checkout/hash checks.
3. Before creating any user, use the baseline's reviewed notice commands:
   `reconcile-initial`, `prepare`, `status`, and `activate`. Require an actual
   sealed empty cohort and activated count zero. No send permission/email is
   needed for an empty cohort. Validate the actual root activation receipt
   through its normal helper. Never fabricate receipt or epoch files, and never
   read the production campaign. Resume stopped services through the ordinary
   reviewed deployment path when the notice command requires it.
4. Stage the exact candidate bundle and run its reviewed CrowdSec cutover on
   this isolated host through real safety passes, permits, both watchdog states,
   and durable completion. Only then seed and measure workloads under ordinary
   guards. Baseline SQLite may exist only before this candidate transition; no
   candidate measurement includes SQLite or a cutover exception.
5. Verify the candidate release, empty operation state, absent mutation/mode/
   permits, normal lease, active safety timers, database gate, app readiness,
   isolation settings, and exact resource ceilings. A failed transition ends the
   run; it is never repaired by writing success evidence.

The first candidate transition on this temporary host establishes the state
being measured; it cannot depend on its own still-pending production rehearsal
result. Only the external capacity-evidence prerequisite is inapplicable to this
isolated synthetic setup. Every cutover security/retention prerequisite and
actual safety pass remains mandatory. Production cutover still requires the
completed rehearsal result before it starts.

## Synthetic corpus and API setup

Reuse `apps/server/cmd/render-budget/fixture.go` without changing its corpus.
Add a fixed offline `render-budget --export-fixtures <directory>` mode that only
exports its three canonical documents, normalized full-fixture PNG, and
SHA-256/count manifest. It must exit before any database, browser, network, or
benchmark action. Build it in hosted CI; do not build on the measured host.

- Minimal: `packages/schema/fixtures/minimal.json`.
- Typical: `packages/schema/fixtures/full.json`, with the existing normalized
  `apps/server/internal/media/testdata/alpha-max-pixels.png` photo.
- Maximum: the existing `buildMaximumDocument` algorithm, 24 custom sections, 64
  visible entries per section, descriptions filled in order with `x` up to
  16,384 bytes each, and canonical document size exactly 524,288 bytes.

Use the existing deterministic namespace `01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f`
and `aboutme/render-budget/` names. Do not shrink the maximum, hide entries,
substitute an empty photo, or choose a best-performing template. Record
candidate schema version and exported hashes. Exported fixture content is
synthetic; passwords, sessions, and IDs assigned by the running app are
runtime-only, never part of the export or public report.

Create one account named `CrowdSec rehearsal`, using the fixed email
`fixture@cs-rehearsal-<run-id>.aboutme.vn` and a runtime-generated password. Use
`POST /api/v1/auth/password/register` with the real HTTPS Origin; require 202
and exactly one matching pending registration. A fixed root helper may complete
only that synthetic registration in one database transaction: lock the matching
row, insert one user and its already-generated password hash, remove that
registration and its verification outbox job, and require exact row counts. It
accepts no SQL, account ID, email, password hash, or DSN input. This avoids an
external mailbox and does not claim to test email verification.

Before that mutation prove this run's host/resource manifest, exact isolated
origin and buckets, no production endpoint/key paths, zero existing users, and
the genuinely activated zero-member cohort. A matching email or root UID alone
is insufficient. Subsequent operations require exactly that single synthetic
user and no unexpected users. Keep the transaction output to fixed outcomes;
never select the stored hash into a client result. This helper exists only in
the rehearsal bundle, is absent from release images, and refuses production.

Login through `POST /api/v1/auth/password/login`; require 204 and retain the
cookie jar in private tmpfs. Read `GET /api/v1/me` for the CSRF token. Use the
normal schema header, Origin, CSRF, idempotency keys, and current `If-Match`
revisions on existing resume APIs. Create exactly three resumes and publish
their synthetic slugs. No application database inserts seed resume content.

The create/mutation body limit is 256 KiB. Load the maximum through bounded
structure/section/entry mutations rather than a 512 KiB create request. Follow
the exported section/entry order; each entry write stays below the body cap.
Pace setup at no more than two mutations/second. Upload the full photo through
the existing photo API. Read back each document and verify canonical content,
counts, visibility, and maximum size, allowing only server-owned photo metadata
and revisions to differ. A mismatch fails setup, not a fixture substitution.

For the job scenario use `aboutme-job@idempotency-expiry-sweep.service` only.
The fixed synthetic helper may seed exactly 10,000 expired idempotency records
for this same account before each repeat, using current schema constraints and
deterministic noncolliding keys. It accepts no row/count/query override. Use
existing retention age rules, validate expiry, then verify the real job removes
them without changing fixture documents. Do not seed during measured intervals.

## Workload schedule

Two repeats run sequentially on the same candidate host. Minimal, typical, and
maximum are measured separately for the first three scenarios. Warm-up is 30
seconds per scenario/corpus; resource safety still applies during warm-up, but
latency samples are discarded. Then measure 120 seconds at 1 Hz, requiring at
least 120 complete resource samples. At phase boundaries take an extra sample.
Wait 60 seconds between scenarios; do not drop caches or reset service limits.

| Scenario                    | Fixed work                                                                                                                                                                                                                                                                                                                                                                                    |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Serving                     | Four client workers, each at most one outstanding request and one request every two seconds. Stagger starts by 500 ms. Alternate authenticated resume GET and public resume HTML GET for the current corpus. Maximum aggregate arrival rate is two requests/second; require at least 100 successes of each kind in 120 seconds.                                                               |
| Print with serving          | Keep those four workers. Add exactly one owner PDF worker, with at least 7.5 seconds between request starts and no overlapping PDF requests. Require at least 12 successful uncached owner PDF responses in the interval; use the existing 20-second request deadline. Never bypass the 10/minute owner/IP limit or public artifact cache to generate load.                                   |
| Job and backup with serving | Keep four serving workers. At sampling start launch exactly one fixed scheduled job and one `pgbackrest --stanza=aboutme --type=full backup` against the isolated repository. Keep sampling for at least 120 seconds and until both finish, with a 30-minute scenario deadline. Prove their process lifetimes overlap serving and each other; a skipped/failed job or backup fails the phase. |
| Maintenance overlap         | Measure an ordinary same-candidate deploy using the existing maintenance-up/app-down order. Begin sampling before maintenance starts and keep external requests running to prove serving answered during the overlap; then accept only the existing marked maintenance 503 until app recovery.                                                                                                |
| Setup and migration         | In that same normal deploy, after serving stops and jobs drain, run the candidate's idempotent `db-setup` followed by `migrate`, then normal warm-up/recovery. Preserve operation/workload locks and the one-job/setup/migrate rule. Both commands must actually execute successfully; report a no-op migration as such.                                                                      |

Use 10 Hz resource sampling around job/backup start and through the two deploy
transients, from five seconds before the effect until ten seconds after its
foreground completion. Require at least one complete simultaneous overlap sample
for serving/job/backup and serving/maintenance. Too-short or missing coverage is
an unproved phase, not permission to add artificial sleeps inside the production
commands. Include cold-start peaks separately. Run one ordinary candidate
`--first-deploy` sequence per repeat to exercise its idempotent setup step; it
changes no tag/floor and never runs inside cutover ownership.

Do not execute application jobs through a cutover permit, disable CrowdSec,
disable rate limits, increase connection pools, or edit a lease to make load
pass. Unexpected 429, 5xx outside marked maintenance, invalid PDF, timeout,
connection failure, or missing sample fails the run. Record latency with queue
time included and exact nearest-rank p95 (`ceil(0.95*N)` after sorting). Report
each corpus/scenario/repeat separately against applicable existing budgets.

## Measurements and acceptance

Use monotonic sample timestamps and UTC run boundaries. A lightweight host
collector reads fixed numeric files/queries only; its own memory/CPU is included
in host totals. Stream bounded numeric records to the external controller, with
no process arguments, environment, raw journals, request paths, or rows.

Record host `/proc/meminfo`, `/proc/pressure/memory` some/full totals and
averages, `/proc/vmstat` swap counters, physical zram allocation, log-tmpfs used
bytes, active DB connection count, and every relevant cgroup's `memory.current`,
`memory.peak`, and `memory.events` deltas. Include PostgreSQL, both Caddy modes,
server/Chromium, web, CrowdSec, bouncer, job/setup/migrate, and native
pgBackRest. Retain cgroup identity across start/stop and collect peaks before it
disappears. Reduce kernel OOM events locally to a count; never export raw kernel
lines.

Combined used memory is `MemTotal - MemAvailable`; unused headroom is
`MemAvailable`, converted from KiB to bytes. This kernel headroom estimate
includes physical zram cost already; record zram physical bytes but do not
subtract them twice or add logical swap capacity. Record `MemFree` separately.
The combined peak is the largest simultaneous used-memory sample, not the sum of
per-service peaks. Missing telemetry or unsupported counters fails proof. A gap
exceeding twice the sampling interval fails that phase.

For this protocol, sustained memory pressure means either memory PSI `some`
stall time at least 10% or `full` at least 1% in each of ten consecutive
one-second intervals, calculated from cumulative microsecond counters. Evaluate
the same rolling one-second intervals during 10 Hz collection. This is the
chosen operational criterion, not a Linux recommendation. Keep avg10/60/300 and
raw numeric deltas so the finding is reproducible. Counter reset/wrap or clock
discontinuity invalidates the phase.

Every phase and repeat requires at least 268,435,456 bytes unused at every
sample, zero kernel/cgroup OOM events, no sustained pressure as defined above,
no tmpfs exhaustion, no connection-pool exhaustion, and successful setup/
migration. Existing service ceilings, connection limits, PDF/body bounds, and
other hard budgets remain unchanged. Record all transient peaks and failures; do
not average away a failure or accept only the better repeat. A failed rehearsal
blocks production cutover pending a reviewed fix and new complete run.

## Cleanup and evidence

Cleanup runs only in the existing coordinator, including after a lost host. It
adds no host-helper operation, secret export, remote callback, or replacement
host. Serialize cleanup for this run and durably record each intended effect and
result in its private manifest before proceeding. Stop external load, delete the
exact workload S3 keys while still attached, and verify their absence. Destroy
this root's recorded DNS record, server, floating IP, volume/attachment, and
security rules/group; confirm absence before creating cleanup credentials. This
terminates host writers even when SSH is unavailable. Preserve shared
network/project resources and all production resources.

Keep each run-owned bucket policy and service account until its bucket is empty.
Require versioning disabled before first use. For each bucket in turn, verify
its exact project, name, recorded account ID/principal, and unchanged
single-bucket policy. Use the existing management provisioner to create one
fresh attached S3 key on that existing account with
`create-s3-key --service-account-id <recorded-id> --secret-file <fresh-path>`.
The path is a new mode-0600 file in a private tmpfs directory. Confirm its
`UserKeyID` and attachment from key inventory before object access. No account,
bucket, policy grant, or paid resource is added. Never detach a key or recover
an old key's secret. Original handoff files remain deleted after import.

The coordinator's fixed object-cleanup adapter reads this fresh file locally; it
never prints credentials or sends them to a host. Use fixed SigV4 requests for
`ListMultipartUploads`, `AbortMultipartUpload`, `ListParts`, `ListObjectsV2`,
and `DeleteObjects`, or their AWS CLI `s3api` equivalents, with explicit HCM04
region/HTTPS endpoint and path-style addressing. No AWS SDK is required. Load
only the isolated credential file; disable ambient credential fallback. Every
bucket argument comes from the validated manifest; object keys, upload IDs, and
continuation markers come only from that bucket's private list responses. Accept
no caller-selected endpoint, bucket, command, or key. Keep TLS verification
enabled and output private, emitting only fixed outcomes and counts. Each
request has a 30-second outer timeout; each list page or delete batch has at
most 100 entries. Abort outstanding uploads, delete listed objects, and repeat
bounded passes until both listings are empty. Pagination cycles, unexpected
responses, per-object delete errors, or deadline expiry fail cleanup. Do not
treat permission errors as absence or broaden a policy to make it pass.
Multipart pagination carries both returned next-key and next-upload-ID markers;
object listing carries its opaque continuation token. Paginate `ListParts` with
its returned next-part-number marker. Parse successful bodies, including
per-object delete errors, rather than trusting HTTP status alone. After abort,
verify known upload parts are empty or return exact `NoSuchUpload`; other
404/403 responses are not proof. Recheck listings within the deadline, since
in-flight parts may need another abort. No AWS-only extension is assumed to work
on HCM04; incompatible responses fail cleanup.

After confirming emptiness, remove that bucket's policy, delete the fresh key
while attached, and verify both absent. Remove only the account's known IAM
policy attachments, delete the account and empty bucket, and confirm absence. On
any error or interruption, attempt fresh-key deletion within the remaining
cleanup deadline and remove its local secret file. Record unresolved key IDs;
never retain the secret for a later attempt. A later explicit `cleanup` resumes
from the manifest and may create another fresh key only after prior attempted
keys are reconciled and retired. Record before/after key inventories around
creation; an uncertain create must be attributed to this exact owned account and
serialized attempt, never to an unfamiliar project key. Ambiguous ownership or a
missing account/policy while objects remain stops for exact operator recovery;
it does not authorize a replacement identity or wider grant. After a process
crash, revoke the recorded or reconciled attempted key before removing any
leftover private credential file and creating a new key. Key lifetime is bounded
by cleanup and explicit revocation, not a provider TTL.

Verify all created resources absent before removing encrypted state, host-key
private material, and the active marker. A failed or interrupted rehearsal stays
failed even if a later cleanup succeeds; cleanup cannot rerun measurement.

Only cleanup may continue after the four-hour deadline. Each explicit cleanup
attempt has a 30-minute bound covering reconciliation, requests, and key
retirement; it never extends the workload deadline or retries automatically.
Unresolved resources retain the manifest and block another run. Never delete by
a broad prefix or infer deletion from a failed listing. No production backup,
row, key, or restore is involved.

Evidence contains public candidate digests, corpus hashes, protocol version, UTC
times, sample counts, latency/resource aggregates, fixed phase outcomes, and
cleanup status. Keep synthetic-only raw numeric measurements locally. Exclude
credentials, cookies, tokens, account/resume/resource IDs, addresses, paths, and
provider responses from public logs. Report setup, measurement, and cleanup
separately; only all three passing yields rehearsal acceptance.

## Implementation scope and verification

Devops owns the fixed coordinator/host helper, dedicated rehearsal Tofu root,
isolated resource/config adapters, and secretless lifecycle/protocol tests.
Backend owns only the offline fixture export mode in `cmd/render-budget` and its
corpus-equivalence checks. No product API, development DB guard, production
bucket names, existing restore controller, or shared deployment guard changes.

Hosted tests prove production-target refusal, manifest/host mismatch, unsafe
paths, quote refusal, wrong-resource cleanup, lost-host cleanup, uncertain key
creates, interrupted key retirement, exhausted cleanup deadlines, secret-free
outputs, empty-cohort-before-user ordering, synthetic SQL bounds, API body/rate
limits, missing samples, pressure arithmetic, and failure propagation. Verify
the fixed bootstrap/candidate sequence and exact provider shapes before a paid
run. The primary reviews infrastructure plans and runs resource-bounded tooling;
independent adversarial review and green CI precede the actual rehearsal.

## Sources

- [Linux pressure metrics](https://docs.kernel.org/accounting/psi.html).
- [Linux memory counters](https://docs.kernel.org/filesystems/proc.html).
- [HCM04 project information](https://docs.greennode.ai/vstorage/object-storage/object-storage-hcm04/cac-tinh-nang-cua-object-storage/lam-viec-voi-project.md).
- [S3 multipart listing](https://docs.aws.amazon.com/AmazonS3/latest/API/API_ListMultipartUploads.html)
  and
  [abort semantics](https://docs.aws.amazon.com/AmazonS3/latest/API/API_AbortMultipartUpload.html).
- [vngcloud workload credentials and retirement](https://github.com/dannyota/vngcloud/blob/master/docs/wiki/Storage-Workload-Access.md).
- [vngcloud object client boundaries](https://github.com/dannyota/vngcloud/blob/master/docs/wiki/Storage-Objects.md)
  and
  [recorded HCM04 S3 behavior](https://github.com/dannyota/vngcloud/blob/master/docs/wiki/Storage.md).
- Repository authorities: [benchmark protocol](budgets.md#benchmark-protocol),
  [host limits](vietnam-host-memory.md), [cutover](crowdsec-cutover.md),
  [HTTP contract](../api/openapi.yaml), and
  [resource patterns](../../deploy/vn/README.md).

The kernel sources define counters; this document selects measurement criteria.
The vngcloud source was read from the local first-party checkout. No
secondary-source statement supplies an API or acceptance rule.
