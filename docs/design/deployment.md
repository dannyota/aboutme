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
| Production          | One GreenNode host (Caddy, Go, Nuxt, PostgreSQL), private vStorage                          | `https://aboutme.vn`              |

Native development uses ports `20432` (PostgreSQL), `20081` (Go), `20030`
(Nuxt), and `20080` (Caddy). The test and development databases are separate
logical databases in the one container. Production never receives a copy of the
native development account or database. The native script idempotently seeds
`aboutme_dev` with one development account and one private sample resume. The
command refuses any other database and is never run by Compose or cloud
environments.

The native HTTPS harness sets `PROVIDER_LOGIN_ENABLED=google,linkedin` for
provider proofs. Native HTTP, Compose, and self-hosted configurations leave it
unset for a password-only surface. The owner sets production provider and
`PASSWORD_REGISTRATION_ENABLED` flags in `/etc/aboutme/flags.env`;
[security](security.md#provider-identity) defines both flags.

Browser authentication requires HTTPS because session and OAuth transaction
cookies are always `Secure`. Native HTTP remains useful for unauthenticated UI
and API work. Auth feature checks run at the native HTTPS origin. Complete
product acceptance runs in production, per
[ADR 0026](../adr/0026-vietnam-hosted-production.md).

## Production topology

A version tag builds ARM64 and AMD64 server, web, and Caddy images on
GitHub-hosted runners, smoke-tests them, and publishes them with build
provenance to public GitHub Container Registry packages. The AMD64 Playwright
baseline job is the pixel authority. The Vietnam host requires AMD64. OpenTofu
lives in `deploy/vn/prod`; state and environment values stay out of Git. GitHub
holds no cloud credentials. The owner applies infrastructure and deploys from
the laptop by image digest.

Production runs on one GreenNode host in Vietnam with Caddy, Go, Nuxt,
PostgreSQL 18, private vStorage media, and pgBackRest backups in vStorage
([ADR 0026](../adr/0026-vietnam-hosted-production.md)). Cloudflare provides
DNS-only records. The [Vietnam production design](vietnam-production.md) owns
the topology. The AWS deployment was removed on 2026-10-11; `deploy/aws/`
remains undeployed for a possible AWS ECS or Kubernetes path.

One serving replica runs under [ADR 0025](../adr/0025-replica-scaling.md). Grow
the host first. Raising the replica count alone is unsafe: publication fences,
render jobs and capabilities, and SSE state are process-local until the
[scaling contract](scaling/README.md) is implemented.

## Client-IP boundary

Caddy's direct listener takes the socket address and strips every forwarding
header before emitting one `X-Real-IP`. Go accepts that canonical header only
from colocated Caddy on loopback, normalizes it with `netip`, and fails closed
in production when its trusted-proxy set is empty. Go never parses
`X-Forwarded-For`. Tests cover forged forwarding headers on the direct listener.

## Edge behavior

Clients connect directly to the floating IP. Caddy terminates TLS with Let's
Encrypt and runs Coraza and the CrowdSec bouncer. Apex is the sole application
origin; `www` redirects before any authentication route. Cloudflare provides DNS
with proxying off. There is no edge cache. Caddy serves hashed `/_nuxt/*` assets
with Nuxt's immutable cache headers. Every public resume request reaches Go's
slug, live-state, route-flag, and public-generation gate before a strong ETag
can validate retained bytes
([ADR 0010](../adr/0010-public-artifact-revocation.md)).

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
([ADR 0009](../adr/0009-private-media-delivery.md)).

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
bounded encrypted outbox and delivered by a worker. Production sends through
Bizfly Email Transaction over SMTP as
`Danny from aboutme.vn <danny@aboutme.vn>`. Replies reach the Google Workspace
mailbox. The [email runbook](../runbooks/email.md) owns SMTP and delivery
checks.

Native development starts a loopback-only mail capture that retains a bounded
number of messages. No production credentials enter that capture.

## Database and releases

PostgreSQL 18 runs on the host's encrypted data volume and listens only on its
Unix socket. pgBackRest archives WAL to GreenNode vStorage and retains backups
for 30 days. Quarterly restore drills check the latest backup and a
point-in-time target on an isolated temporary server. The drill's outbound
networking remains open. See the
[Vietnam database design](vietnam-production.md#postgresql).

Migrations run as a separate deploy step, never at server startup
([ADR 0025](../adr/0025-replica-scaling.md)); the
[Vietnam production design](vietnam-production.md#deploy) lists the order. A
nonzero migration exit blocks the update. The runner uses goose's Provider with
a PostgreSQL session advisory locker, which takes the lock before reading the
pending set, so a second concurrent runner is safe. Every migration runs as the
fixed `aboutme_migrator` role that `db-setup` creates. Before application
`v1.0.0`, migration history may be edited, deleted, or collapsed without
backwards compatibility. Repository SQL edits do not change applied database
history or stored state; deployment reconciles that state explicitly. The first
v1 candidate adds root `.v1-baseline`; trusted comparison bases containing it
freeze existing migrations ([ADR 0005](../adr/0005-database-migrations.md)).

The [release fence](passkey-release-fence.md) keeps every supported deploy,
rollback, and restoration at or above the second-factor floor. Every supported
path runs the current deployer, not a script from the target release. OpenTofu
and host administration remain privileged bypasses that need a compatibility
proof.

After the application v1 baseline, a breaking schema change uses expand,
backfill, and contract across releases, so the previous server keeps working
against the migrated schema. The deploy script does not test that, so
redeploying an earlier image is a supported rollback only when the failed
release applied no migration. Otherwise recovery is a forward fix or a
point-in-time restore.

Application secrets are root-owned 0400 host files on the encrypted data volume,
supplied as named Podman secrets only to the units that need them. An
age-encrypted copy stays in `aboutme-infra`. Values never enter images, source,
command lines, logs, or OpenTofu state.

The TOTP key ring uses two host secret slots, selected in
`/etc/aboutme/flags.env`; the [key-management design](totp-key-management.md)
owns the application key-ring contract.

A later second replica uses the pinned admission and password-email key versions
from [key identity](scaling/admission.md#key-identity). A key change then
requires closed admission, joined or fenced work, and proved zero claim, rate,
and pending debt.
