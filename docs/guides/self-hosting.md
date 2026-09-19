# Self-hosting

The repository currently ships a Podman Compose deployment for local evaluation.
It runs PostgreSQL, private MinIO object storage, the Go API, Nuxt, Caddy, a
one-shot database role and grant setup, a goose migration service, and a
private-bucket initializer.

This artifact is not ready for direct Internet exposure. Its Caddy listener is
HTTP-only and its client-IP rule assumes the viewer connects directly to Caddy.
Use the native HTTPS harness for authenticated local checks. The hosted service
uses the separate AWS and Cloudflare deployment.

## Prerequisites

- Podman with `podman compose` support.
- Enough memory to build the Go and Nuxt images and run five long-lived
  containers.
- A repository checkout and a private `.env` file.

Daily source development should use the lighter
[native stack](../runbooks/native-development.md). Use Compose when the image
and network boundaries are what you need to inspect. The Compose stack cannot
run beside the shared `aboutme-test-db` container.

## Configure

From the repository root:

```sh
cp .env.example .env
```

Set `POSTGRES_PASSWORD`, `MEDIA_ACCESS_KEY_ID`, and `MEDIA_SECRET_ACCESS_KEY` to
independent new values. Do not commit `.env`. Generate the media values with
`openssl rand -hex 16` and `openssl rand -hex 32`, respectively. Other Compose
variables have development defaults; [`.env.example`](../../.env.example)
documents each name.

The current listener defaults to HTTP port 80. On a rootless Podman host that
cannot bind privileged ports, set this for local evaluation:

```dotenv
CADDY_HTTP_PORT=8080
```

`PUBLIC_ORIGIN` must equal the exact browser origin when set. OAuth provider
callback URLs are:

- `<PUBLIC_ORIGIN>/api/v1/auth/google/callback`
- `<PUBLIC_ORIGIN>/api/v1/auth/github/callback`
- `<PUBLIC_ORIGIN>/api/v1/auth/linkedin/callback`

The current HTTP stack is not an authentication acceptance environment. Do not
weaken Secure cookie settings to make it one.

## Start and verify

Within a shared development session, only the integration owner may schedule
Compose. The owner waits until every live-database worker is idle, stops the
shared database, runs the smoke stack, tears it down, then restores the shared
database if later work needs it. `make dev` fails its preflight while
`aboutme-test-db` is running. Workers never stop the shared database.

```sh
make test-db-down
make dev
podman compose --env-file .env -f deploy/compose.yml ps
curl --fail http://localhost:8080/healthz
curl --fail http://localhost:8080/readyz
```

Use `http://localhost` when `CADDY_HTTP_PORT` is unset. Database setup must pass
before migrations; migrations and bucket initialization must pass before the
server starts. Setup creates missing fixed roles without passwords, applies
their grants, and verifies existing permissions. It stops on privilege drift
without changing existing roles or credentials.

Roles live at the cluster level, not inside one database. A PostgreSQL volume
from before [ADR 0038](../adr/0038-single-baseline-and-plain-migrator.md) can
still hold a role the release baseline retired, which setup correctly refuses.
Recreate the volume to clear it: stop the stack, remove its `postgres-data`
volume (`podman volume ls` shows the exact name), then start the stack again.

Only Caddy publishes a host port. PostgreSQL and MinIO stay inside separate
Compose networks. Only MinIO, its initializer, and Go join the `media` network;
Caddy and Nuxt cannot reach object storage. The server, database setup and
migration processes receive the database password through `PGPASSWORD`; it is
not inserted into a URI. Current Compose still uses the initialization login for
application access; hosted deployment uses the separate `aboutme_app` role.

Inspect logs without printing `.env`:

```sh
podman compose --env-file .env -f deploy/compose.yml logs server web caddy
```

## Stop and update

Stop containers while retaining the PostgreSQL volume:

```sh
make dev-down
make test-db-up
```

Restore the shared database only after `make dev-down`, and only when later work
needs it.

An updated checkout rebuilds images and applies pending embedded goose
migrations on the next `make dev`. Released migrations are append-only. Back up
operator data before an update; a tested restore procedure is not yet shipped.

Do not delete the Compose volume as a routine stop or update step.

## Current limits

- The Compose listener is HTTP-only, so it cannot prove Secure-cookie
  authentication or the production proxy-validation path.
- Backup, restore, rollback, and secret-rotation runbooks will be added only
  when Compose gains those operator workflows.
- Production infrastructure and operations are separate from this self-hosted
  artifact.

See [deployment artifacts](../../deploy/README.md) for the network shape and
[current-state architecture](../architecture.md) for the implemented product
slice.

aboutme is licensed under AGPL-3.0. If you offer a modified version as a network
service, provide its corresponding source to that service's users.
