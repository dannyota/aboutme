# Production deploy

This runbook holds the deploy, failed-deploy, rollback, and release-fence steps
of the [production runbook](production.md), which owns access, host operations,
backups, alarms, and healthy state.

## Deploy

Use the [CrowdSec PostgreSQL runbook](crowdsec-postgresql.md) for host
installation, migration, verification, failure recovery, and cluster restore.

Use the current reviewed checkout's deployer. Publication and deployment share
`scripts/release-eligibility.sh`: the tag must be strict `vMAJOR.MINOR.PATCH`,
resolve to a commit on `main` fetched from `dannyota/aboutme`, and have a
completed, successful latest matching `ci.yml` push run on `main` for that exact
commit. A branch or manual run never counts. A newer pending, cancelled, or
failed matching run blocks an older success. Missing evidence or a fetch/API
error also blocks.

The accepted
[tooling and display changes](../design/local-tooling-simplification.md) leave
this exact-commit release rule unchanged. After integration, full CI also
requires `server-runtime-smoke` startup/idle evidence. It does not replace
release image checks, production smoke checks, or host memory observation.
Native macOS partial proofs cannot authorize tagging or deployment.

The image workflow runs the helper from a separate checkout of
`dannyota/aboutme` at `refs/heads/main` and binds the tag to the workflow's
event commit. A read-only job checks eligibility before publishing jobs start.
Every write-capable job repeats the check before other work, so a partial rerun
must pass fresh eligibility. Candidate checkouts supply build inputs only. The
workflow publishes arm64 and amd64 images; the Vietnam deploy requires amd64 and
pins image digests. GitHub holds no cloud credential other than the vMonitor
quota-renewal service account
([quota renewal design](../design/vmonitor-quota-renewal.md)).

Before SSH or registry access, the deployer also requires its checkout to be on
`main`, clean (including untracked files), and equal to freshly fetched
`origin/main`. Use that checkout for rollbacks and activation too; the target
tag may be older. Ignored local configuration does not make the checkout dirty.

```sh
DEPLOY_HOST=<host> bash deploy/vn/scripts/deploy.sh <tag>
```

The deployer checks tag-bound build provenance and requires the installed
`fence.sh`, `deploy-host.sh`, and `deploy-safety.sh` to match the checkout. If
they differ, install the reviewed host bundle before retrying.

The deployer then:

1. Takes the host operation lock and rejects a tag below the release floor.
2. Pulls images while the old release serves and checks every referenced secret
   by name.
3. Takes a pgBackRest incremental backup annotated with the tag.
4. Stops job timers and active jobs. Starts maintenance beside Caddy with shared
   ports and certificate storage, then stops serving Caddy and Go.
5. Requires the marked 503 maintenance page from outside the host.
6. Renders the new units, runs migrations, and requires exit 0. `--first-deploy`
   runs `db-setup` before migrations.
7. Restarts Nuxt, starts Go, and waits for `/readyz` at `127.0.0.1:8080` on the
   host. Starts Caddy, then stops maintenance and proves it stopped.
8. Starts the four job timers. Checks public health, readiness, TLS, HSTS,
   `nosniff`, and a CrowdSec 403 for the operator's test ban. Deletes the ban,
   polls `/` until HTTP 200 (sleeps of two ticks plus 5 s, rounded up to 5 s
   steps, and up to 10 s a request: about 85 s), then releases the lock.

vMonitor and the uptime backstop keep running during maintenance. Host alarms
and backup timers also remain active. A deploy can therefore send a site-down
notification.

Public-page stylesheets and hydration scripts use a build-time content hash.
Rename a changed fixed-name font under `/_nuxt/fonts/` so browsers fetch it.

## Failed deploy

Before migrations, failure restores the previous release and job timers through
maintenance; first deploy failures keep maintenance up. After a possible
migration, fix forward or restore with the app and jobs stopped. Failed recovery
retains the operation lock; inspect units and the host journal before retrying.

A CrowdSec retention failure stops both edge modes. Follow
[CrowdSec incident recovery](crowdsec-postgresql.md#interrupted-deploy).

## Rollback

```sh
DEPLOY_HOST=<host> bash deploy/vn/scripts/deploy.sh --rollback <previous-tag>
```

Rollback uses the same lock and maintenance path, with no backup or migration.
It keeps the current maintenance image. Rollback is safe only when the failed
release applied no migration, the tag meets the release floor, and the older
server can read every stored document version. No tool lowers newer document
rows for an older server. Fix forward when those conditions do not hold.

## Passkey release fence

`/var/lib/aboutme/fence.json` holds the monotonic minimum release and a
nonexpiring operation lock outside PostgreSQL. `fence.sh` writes it under
`flock`, with an atomic rename. A crash leaves the lock closed. Every supported
deploy, rollback, and activation takes the lock; host image starts check the
floor before Podman runs.

Before taking fence ownership, take `/srv/data/host-operations/.bundle.lock`,
then the fence lock, and release the bundle lock after recording ownership.

Activation requires the exact candidate server to be running:

```sh
DEPLOY_HOST=<host> bash deploy/vn/scripts/deploy.sh --activate <tag>
```

Deploy a capable release with its feature flag off, prove it healthy, activate
the floor, then enable the flag in `/etc/aboutme/flags.env` and redeploy the
same tag. The floors are v0.4.2 for passkeys, v0.4.7 for TOTP, and v0.6.26 for
sign in to view. A raised floor stays raised if a later step fails.

Before clearing a stale lock, prove that no laptop deploy, host deploy process,
database command, or media writer from that operation can still run. Inspect
release state, unit state, the fence, and any mutation marker. On the host:

```sh
sudo /usr/local/lib/aboutme/fence.sh clear '<reason>'
```

The clear records its reason and never lowers the floor. Account-deletion
release and clear also refuse an unresolved request, active unit/container, or
matching database backend. Missing data mounts and key-change markers block
image starts. Recover through the owning request; neither manual clear nor an
EXIT trap bypasses these checks. A retained operation or mutation marker can
block serving and job starts across reboot. Inspect and recover the exact
mutation before clearing its lock. Do not delete fence files to restart the app.
