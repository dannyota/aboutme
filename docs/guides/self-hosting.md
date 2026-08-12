# Self-hosting

The repository currently ships a Podman Compose deployment for local evaluation.
It runs PostgreSQL, the Go API, Nuxt, Caddy, and a one-shot goose migration
service.

This artifact is not ready for direct Internet exposure. Its Caddy listener is
HTTP-only and its client-IP rule assumes the viewer connects directly to Caddy.
Authentication UAT and public service require the planned HTTPS/443 edge path.

## Prerequisites

- Podman with `podman compose` support.
- Enough memory to build the Go and Nuxt images and run four long-lived
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

Set `POSTGRES_PASSWORD` to a new value. Do not commit `.env`. Other variables
have development defaults; [`.env.example`](../../.env.example) documents each
name.

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

Use `http://localhost` when `CADDY_HTTP_PORT` is unset. The one-shot migration
service must exit successfully before the server starts. A migration failure
keeps the application down.

Only Caddy publishes a host port. PostgreSQL stays inside the Compose networks.
The server and migration process receive the database password through
`PGPASSWORD`; it is not inserted into a URI.

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

- Resume HTTP, editor, rendering, publishing, realtime, and PDF output are not
  implemented.
- HTTPS termination and the production proxy-validation path are not present.
- Backup, restore, rollback, and secret-rotation runbooks will be added only
  when their supporting deployment exists.
- AWS infrastructure is not part of the current repository.

See [deployment artifacts](../../deploy/README.md) for the network shape and
[current-state architecture](../architecture.md) for the implemented product
slice.

aboutme is licensed under AGPL-3.0. If you offer a modified version as a network
service, provide its corresponding source to that service's users.
