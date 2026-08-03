# Self-hosting

aboutme is AGPL-3.0 and ships a runnable **podman compose** stack for local
development and self-hosting. Caddy provides one origin in front of the Go API
and Nuxt; PostgreSQL is reachable only by the migration and server containers.

## Start the stack

From the repository root:

```sh
cp .env.example .env
# Set POSTGRES_PASSWORD in .env before continuing.
make dev
```

Rootless podman usually cannot bind port 80. Set `CADDY_HTTP_PORT=8080` in
`.env`; the stack is then available at `http://localhost:8080`.

Verify it:

```sh
curl --fail http://localhost:8080/healthz
curl --fail http://localhost:8080/readyz
```

Use the configured port instead of `8080` if it differs. Stop the stack without
removing its PostgreSQL volume:

```sh
make dev-down
```

The one-shot `migrate` service must finish successfully before the Go service
starts. A migration failure therefore prevents an application/database version
mismatch instead of booting partially.

## Current limitations

The runnable stack currently provides the foundation and authentication/session
slice. Resume APIs, the editor/renderer, publishing, realtime, and PDF output
are not available yet. Production AWS deployment is a later phase; do not
promote the development Caddyfile as a production edge configuration.

Authentication has an important TLS boundary: the `__Host-session` cookie is
always `Secure`. Plain HTTP on a LAN hostname therefore cannot sustain a login,
and Safari also rejects the cookie on `http://localhost`. Use a browser that
supports secure localhost for local OAuth testing, or put the stack behind
external TLS for usable self-hosted authentication. In either case, set
`PUBLIC_ORIGIN` to the exact browser origin and register its exact callback URLs
with each OAuth provider.

Configuration variables and security notes live in
[`.env.example`](../.env.example) and [`deploy/README.md`](../deploy/README.md).
