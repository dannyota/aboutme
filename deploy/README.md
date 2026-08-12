# Deployment artifacts

`deploy/` contains the current image-based local deployment. The intended
environment and trust boundaries live in the
[deployment design](../docs/design/deployment.md).

| Path                | Purpose                                               |
| ------------------- | ----------------------------------------------------- |
| `compose.yml`       | Podman Compose services and isolated networks         |
| `server.Dockerfile` | Go server and embedded goose migration binaries       |
| `web.Dockerfile`    | Nuxt production image                                 |
| `caddy/Caddyfile`   | Current one-origin route table and client-IP boundary |

AWS infrastructure has not landed.

## Which stack to use

Daily work uses `make dev-native` at `http://localhost:20080`. It starts native
Go, Nuxt, and Caddy processes against the one shared PostgreSQL container. See
the [native development runbook](../docs/runbooks/native-development.md).

`make dev` builds and starts the Compose deployment. Reserve it for local
deployment smoke checks and UAT sessions because it is heavier. The current
Caddyfile is HTTP-only. It does not yet meet the P9 requirement for HTTPS on
port 443, so it cannot produce valid authentication UAT evidence. See the
[local UAT runbook](../docs/runbooks/local-uat.md).

## Start the Compose deployment

From the repository root:

```sh
cp .env.example .env
# Set POSTGRES_PASSWORD to a new value in .env.
make dev
```

The default published origin is `http://localhost` on port 80. Rootless Podman
often cannot bind that port. Set `CADDY_HTTP_PORT=8080` in `.env` for a local
smoke check, then use `http://localhost:8080`.

Verify the running services:

```sh
podman compose --env-file .env -f deploy/compose.yml ps
curl --fail http://localhost:8080/healthz
curl --fail http://localhost:8080/readyz
```

Use port 80 in the URLs when `CADDY_HTTP_PORT` is unset. Stop the deployment
without deleting its PostgreSQL volume:

```sh
make dev-down
```

## Runtime boundaries

Compose runs PostgreSQL, Go, Nuxt, and Caddy as long-lived services. A one-shot
`migrate` service applies embedded goose migrations before Go starts. A failed
migration prevents the server from starting.

PostgreSQL is not published to the host. Only Caddy publishes a port. Separate
`db`, `edge`, and `frontend` networks prevent Nuxt and PostgreSQL from becoming
trusted Go proxies. Caddy strips viewer-supplied forwarding headers and sends
one canonical client address to Go.

The database password is supplied through `PGPASSWORD`, not inserted into the
database URL. This preserves passwords containing URI delimiters.

The current Caddyfile is a development trust boundary. Do not expose it to the
Internet or place it unchanged behind another proxy. Production must validate
the edge path and derive the viewer address as specified by the deployment
design.

The [self-hosting guide](../docs/guides/self-hosting.md) states the current
operator scope and TLS limits.
