# Native development

Use the native stack for daily work. It runs Go, Nuxt, and Caddy as local
processes against two logical databases in the one capped `aboutme-test-db`
container.

## Ports and state

| Port    | Service                      |
| ------- | ---------------------------- |
| `20432` | Shared PostgreSQL container  |
| `20080` | Browser origin through Caddy |
| `20081` | Go server                    |
| `20030` | Nuxt development server      |

Tests use the `aboutme` database. Native development uses `aboutme_dev`, so a
test suite cannot truncate development data. Logs, process IDs, binaries, and a
generated Caddyfile stay under the ignored `.dev/` directory.

## Prerequisites

Install the versions in [`.tool-versions`](../../.tool-versions), plus Podman,
`curl`, `ss`, and `setsid`. Verify the native stack's tools and install web
dependencies once:

```sh
make tools-check ARGS=dev
(cd apps/web && npm ci)
```

Provider credentials are optional in development. Export them before startup if
an OAuth path is under test; the script does not read `.env` automatically.

## Start

From the repository root:

```sh
make dev-native
```

The command is idempotent. It starts or reuses `aboutme-test-db`, creates the
`aboutme_dev` database if needed, applies goose migrations, builds the Go
binary, then starts Go, Nuxt, and Caddy.

Open only `http://localhost:20080` in a browser. Direct upstream ports are for
diagnostics.

## Verify

```sh
make dev-native-status
curl --fail http://localhost:20080/healthz
curl --fail http://localhost:20080/readyz
```

`dev-native-status` exits nonzero when any process is stopped, crashed, or not
listening. The readiness probe also proves the application can reach the
development database.

## Inspect logs

```sh
make dev-native-logs
make dev-native-logs ARGS="server"
make dev-native-logs ARGS="-f web"
```

If the real Caddyfile changes while the stack is running, stop and restart so
the generated native copy is refreshed.

## Stop

```sh
make dev-native-down
```

This stops only the native Go, Nuxt, and Caddy process groups. It leaves
`aboutme-test-db` running for tests and other workers. Do not stop or recreate
the shared database while another test suite is active.

## Failure checks

- If startup reports a busy port, inspect the named `20000`–`21000` port. The
  script refuses to kill a process it did not start.
- If the database container is healthy but host connections are refused, run
  `make test-db-up`. Its host-side probe detects a failed Podman port forward.
  Recreate the database container only in an announced idle window.
- If Nuxt startup says dependencies are missing, run `npm ci` in `apps/web`.
- Use `.dev/server.log`, `.dev/web.log`, and `.dev/caddy.log` for full startup
  errors. Do not commit `.dev/`.

Authentication acceptance uses the HTTPS/443 UAT deployment because cookies are
Secure. Native HTTP remains the daily API and UI development origin.
