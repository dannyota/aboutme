# 6. Deployment and trust boundaries

The environments share application code and route policy but use different
process and infrastructure shapes. Current runnable behavior remains documented
in [`../architecture.md`](../architecture.md) and operator guides.

## Environment model

| Environment         | Shape                                                                                       | User origin                       |
| ------------------- | ------------------------------------------------------------------------------------------- | --------------------------------- |
| Tests               | One capped PostgreSQL container; native test processes; optional isolated service harnesses | Kernel-assigned or `20090+` ports |
| Native development  | Shared test DB container plus native Go, Nuxt, and Caddy processes                          | `http://localhost:20080`          |
| Native HTTPS checks | Shared DB, native processes, and disposable browser                                         | `https://localhost:20443`         |
| Self-hosted         | Podman Compose with operator-supplied credentials and TLS configuration                     | Operator-defined HTTPS origin     |
| Production          | Cloudflare proxy, one Bottlerocket ECS host (Caddy, Go, Nuxt), RDS PostgreSQL, private S3   | `https://aboutme.vn`              |

Native development uses ports `20432` (PostgreSQL), `20081` (Go), `20030`
(Nuxt), and `20080` (Caddy). The test and development databases are separate
logical databases in the one container. Production never receives a copy of the
native development account or database. The native script idempotently seeds
`aboutme_dev` with one development account and one private sample resume. The
command refuses any other database and is never run by Compose or cloud
environments.

The native HTTPS harness sets `PROVIDER_LOGIN_ENABLED=true` for provider proofs.
Native HTTP, Compose, and self-hosted configurations leave it unset for a
password-only surface. Production sets it and `PASSWORD_REGISTRATION_ENABLED`
from OpenTofu variables; [security](security.md#provider-identity) defines both
flags.

Browser authentication requires HTTPS because session and OAuth transaction
cookies are always `Secure`. Native HTTP remains useful for unauthenticated UI
and API work. Auth feature checks run at the native HTTPS origin. Complete
product acceptance runs in production, per
[ADR 0037](../adr/0037-single-host-production-without-hosted-uat.md).

## Production topology

A version tag builds ARM64 server, web, and Caddy images on GitHub-hosted ARM64
runners, smoke-tests them, and publishes them with build provenance to public
GitHub Container Registry packages. The AMD64 Playwright baseline job stays the
pixel authority; ARM64 smoke tests cover runtime compatibility, including
Chromium and fonts. OpenTofu lives in `deploy/aws/`, state and environment
values stay out of Git, and GitHub holds no cloud credentials. The owner applies
infrastructure and deploys from the laptop by image digest, never a moving tag.

Production is Cloudflare in front of one EC2 host running Caddy, Go, and Nuxt,
with private single-AZ RDS PostgreSQL and private S3
([ADR 0037](../adr/0037-single-host-production-without-hosted-uat.md)). The
[single-host design](single-host-production.md) owns its details. One serving
replica runs under
[ADR 0036](../adr/0036-single-replica-launch-and-pipeline-migrations.md), and
the growth path is a larger instance first. Raising the replica count alone is
unsafe: publication fences, render jobs and capabilities, and SSE state are
process-local until the [scaling contract](scaling/README.md) lands.

## Client-IP boundary

Cloudflare sets `CF-Connecting-IP`. Caddy trusts it only from Cloudflare's
published ranges, rejects duplicate or invalid values, strips every other
forwarding header, and emits one `X-Real-IP`. Go accepts that canonical header
only from the colocated Caddy on loopback, normalizes it with `netip`, and fails
closed in production when its trusted-proxy set is empty. Go never parses
`X-Forwarded-For`. Tests cover forged viewer headers and direct requests to the
origin address.

## Edge behavior

Cloudflare terminates viewer TLS; the
[single-host design](single-host-production.md#edge) lists its settings. Apex is
the sole application origin and `www` redirects before any authentication route.
The origin accepts only Cloudflare's ranges with its origin-pull client
certificate. Every path except hashed `/_nuxt/*` assets bypasses the edge cache,
so edge storage is never publication authority: each public request reaches the
origin, which checks slug, live state, route flag, and public generation before
a strong ETag can validate retained bytes
([ADR 0022](../adr/0022-public-artifact-revocation.md)).

Go reaches Nuxt on the host's private network, and at the native Nuxt address in
development. Caddy denies `/internal-render` and `/internal-render/*` before its
default web proxy, and denies `/print/**` to viewers. The render browser reaches
Nuxt only on the private network and has no account cookie or general outbound
access. The [system design](system.md#renderer-boundary) and
[security design](security.md#internal-print-authority) own both internal
routes. Route-parity tests pin the Caddy denials. The render queue, redeemed
capabilities, and controller handle are process-local, which is correct only
while one replica serves.

## Media

Object storage is private, and Go authorizes every owner and public media read,
including the live-gated public photo. There is no direct object-store origin,
because a leaked key must not keep an unpublished photo public
([ADR 0019](../adr/0019-private-media-delivery.md)).

The bucket is unversioned, so a delete removes the bytes instead of leaving a
noncurrent version outside the orphan sweep. Object keys are random and
immutable, and writes are create-only: a collision loser never overwrites or
deletes a winner's bytes.

An upload writes a new candidate object, then updates the resume in one
transaction. Only a proved-created object reaches that mutation. A candidate
that a definite database result does not reference (a replay, loser, conflict,
stale precondition, or rollback) is deleted best-effort. An ambiguous commit
keeps the candidate, and an object write with an unknown outcome stops before
the database and is never deleted, since its key may name a collision winner.
Weekly orphan reconciliation owns what crashes and failed cleanup leave behind.

Replacement, photo deletion, resume deletion, and account deletion revoke the
reference and enqueue an exact-key deletion job in one transaction. The API
succeeds after that commit because every read is reference-gated. A worker
retries idempotent deletion toward a 24-hour target, and overdue work is audited
and alerted until it finishes.

Photo normalization and Chromium rendering share one task-wide heavy-work
permit, so their memory peaks never overlap.

## Authentication email

Verification, reset, and security-notification email is committed through a
bounded encrypted outbox and delivered by a worker. Production uses the AWS SDK
for Go v2 SES client in `ap-southeast-1` with a verified sending domain, a
configuration set, and delivery/bounce/complaint alarms. AWS credentials use the
standard runtime credential chain and never enter repository files.

Native development starts a loopback-only mail-capture command that retains a
bounded number of messages and never initializes AWS. The
[email runbook](../runbooks/email.md) records the existing Singapore SES sandbox
setup. Preserve its Google Workspace root DNS and SES resources. The production
task role adds send permission without taking over those resources. Mail to real
users requires SES production access; a simulator smoke does not prove a user's
verification or reset flow.

## Database and releases

RDS PostgreSQL uses Graviton-compatible instances, gp3 storage, automated
backups, and point-in-time recovery. It starts single-AZ; Multi-AZ is a later
availability decision. Backup retention is 30 days. Restore evidence matters
more than backup configuration: one isolated restore with data verification
precedes the public announcement.

Migrations run as a separate deploy step, never at server startup
([ADR 0036](../adr/0036-single-replica-launch-and-pipeline-migrations.md)); the
[single-host design](single-host-production.md#release-and-deploy) lists the
order. A nonzero migration exit blocks the update. The runner uses goose's
Provider with a PostgreSQL session advisory locker, which takes the lock before
reading the pending set, so a second concurrent runner is safe. Every migration
runs as the fixed `aboutme_migrator` role that `db-setup` creates.
`apps/server/migrations/.uat-baseline` keeps existing migrations immutable
([ADR 0020](../adr/0020-uat-migration-baseline.md),
[ADR 0038](../adr/0038-single-baseline-and-plain-migrator.md)).

The [release fence](passkey-release-fence.md) keeps every supported deploy,
rollback, and restoration at or above the second-factor floor. Every supported
path runs the current deployer, not a script from the target release. OpenTofu
and AWS administration remain privileged bypasses that need a compatibility
proof.

A breaking schema change uses expand, backfill, and contract across releases, so
the previous server keeps working against the migrated schema. The deploy script
does not test that, so redeploying an earlier image is a supported rollback only
when the failed release applied no migration. Otherwise recovery is a forward
fix or a point-in-time restore.

Application secrets use AWS Systems Manager Parameter Store `SecureString`
values in `ap-southeast-1`, injected at runtime. They never enter images,
source, command lines, logs, or OpenTofu state. Secret names and rotation
procedures are tracked; values are never evidence.

The TOTP key ring uses two such parameters, chosen by nonsecret OpenTofu slot
variables; the [key-management design](totp-key-management.md) owns rotation and
key failures.

A later second replica uses the pinned admission and password-email key versions
from [key identity](scaling/admission.md#key-identity). A key change then
requires closed admission, joined or fenced work, and proved zero claim, rate,
and pending debt.
