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
| `20091` | Authentication mail capture  |

Tests use the `aboutme` database. Native development uses `aboutme_dev`, so a
test suite cannot truncate development data. Logs, process IDs, binaries, a
generated Caddyfile, and the mode-0600 password-mail secrets stay under the
ignored `.dev/` directory.

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

The stack seeds one account, `dev@aboutme.invalid` with password
`aboutme-dev-password-1`, and one private sample resume. `make dev-seed` repeats
the seed on its own; it never overwrites edits you made to the sample resume.

Isolated capture harnesses that seed their own fixture database set
`ABOUTME_DEV_AUTO_SEED=0` for startup. Daily development defaults to `1`;
`make dev-seed` remains an explicit request to seed `aboutme_dev`.

The command is idempotent. It starts or reuses `aboutme-test-db`, creates the
`aboutme_dev` database if needed, applies goose migrations, builds the Go
binary, then starts the authentication-mail-capture server, Go, Nuxt, and Caddy.
The mail-capture bearer, rate-HMAC, and mail-encryption secrets are created once
under `.dev/secrets/` and reused across restarts; they are never printed.

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

## Automated checks

The development browser harness runs on a separate trusted stack at
`https://localhost:20443` with a deterministic local Google account and a pinned
headless Chromium: `make dev-https-auth-check`, `dev-https-transport-check`,
`dev-https-editor-check`, `dev-https-public-check`, and
`dev-https-password-check`. The native public HTTP capture is
`make p5a-native-http-check`. These are proof targets, not daily drivers; run
them only when their surface changes. The password proof additionally seeds and
cleans three deterministic accounts and reads authentication mail from the
loopback capture server.

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
