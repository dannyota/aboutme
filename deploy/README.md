# deploy

- `compose.yml` — podman compose for dev and self-hosting (postgres, a one-shot
  `migrate` service, server, web, caddy).
- `server.Dockerfile`, `web.Dockerfile` — multi-stage builds for the two apps.
  The server image also carries the `migrate` binary the `migrate` service runs.
- `caddy/` — Caddyfile implementing the full spec §2 route table.
- `aws/` — **planned, not present yet**. The deferred PI phase will add
  Terraform for ap-southeast-1 (ECS on Graviton, RDS, and CloudFront).

See spec §6.

## Running the stack

`compose.yml` doubles as the self-hosting artifact, so it ships **no** default
database credential. Before the first run:

```sh
cp .env.example .env   # repo root, if you haven't already
# edit .env and set POSTGRES_PASSWORD, e.g.:
#   POSTGRES_PASSWORD=$(openssl rand -base64 24)
```

Then, from the repo root (so the root `.env` is picked up):

```sh
podman compose --env-file .env -f deploy/compose.yml up -d --build
podman compose --env-file .env -f deploy/compose.yml down
```

`make dev` / `make dev-down` wrap the same commands from the root Makefile.

On `up`, the one-shot `migrate` service runs first: it applies the embedded
goose migrations against PostgreSQL and exits 0. The `server` only starts once
it has, via compose's `depends_on: condition: service_completed_successfully`.
If a migration fails, the `migrate` service exits non-zero and the server never
starts. A bad migration therefore fails the whole stack closed, rather than
booting a server against an unmigrated database. The service has no healthcheck:
a run-to-completion container has no steady state to probe. It reuses the server
image with the `migrate` entrypoint.

The database password is supplied to both `migrate` and `server` as
`PGPASSWORD`, never spliced into `DATABASE_URL` — a `/`, `@`, `:` or `=` in a
generated password would otherwise corrupt the connection URI. The three compose
networks are isolated: only Caddy shares the `edge` network with the server. As
a result, the SSR web container cannot reach the Go server directly, and only
Caddy's address is a trusted proxy.

Serves on `http://localhost` (port 80). Rootless podman without a
`sysctl net.ipv4.ip_unprivileged_port_start` adjustment can't bind port 80; set
`CADDY_HTTP_PORT` (e.g. `8080`) in `.env` in that case.
