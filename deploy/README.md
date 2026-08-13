# Deployment artifacts

`deploy/` contains the current image-based local deployment. The intended
environment and trust boundaries live in the
[deployment design](../docs/design/deployment.md).

| Path                 | Purpose                                               |
| -------------------- | ----------------------------------------------------- |
| `compose.yml`        | Podman Compose services and isolated networks         |
| `server.Dockerfile`  | Go server and embedded goose migration binaries       |
| `web.Dockerfile`     | Nuxt production image                                 |
| `caddy/Caddyfile`    | Current one-origin route table and client-IP boundary |
| `dev-https-browser/` | Pinned disposable browser for local HTTPS auth proof  |

AWS infrastructure has not landed.

## Which stack to use

Daily work uses `make dev-native` at `http://localhost:20080`. It starts native
Go, Nuxt, and Caddy processes against the one shared PostgreSQL container. See
the [native development runbook](../docs/runbooks/native-development.md).

`make dev-https` runs the local authenticated-development stack at
`https://localhost:20443`. It uses the shared `aboutme_dev` database, a
deterministic local Google provider, and a project-local Caddy root. The
`dev-https-browser/` image is pinned by digest and package versions. Its runner
accepts only the verified local image ID, imports the root into a fresh NSS
database, mounts only the root read-only and an empty evidence directory
read-write, and runs with a read-only container root.

`make dev` builds and starts the Compose deployment. Reserve it for local
deployment smoke checks and self-hosting evaluation because it is heavier. The
current Compose Caddyfile is HTTP-only. It cannot produce P9 UAT evidence. The
isolated port-443 overlay and its `uat-*` targets are still planned. See the
[local UAT runbook](../docs/runbooks/local-uat.md).

## Start the Compose deployment

From the repository root:

```sh
cp .env.example .env
# Set POSTGRES_PASSWORD, MEDIA_ACCESS_KEY_ID, and MEDIA_SECRET_ACCESS_KEY.
make test-db-down
make dev
```

Generate independent media credentials and copy them into `MEDIA_ACCESS_KEY_ID`
and `MEDIA_SECRET_ACCESS_KEY` in `.env`. For example, use `openssl rand -hex 16`
for the access key and `openssl rand -hex 32` for the secret key. Do not commit
`.env`, paste its contents into commands, or put its values in logs.

Compose uses the private `aboutme-media` bucket in `us-east-1` by default. Set
`MEDIA_BUCKET` or `MEDIA_REGION` in `.env` only to change those values. Bucket
creation is idempotent: a one-shot initializer creates it before Go starts.

`make dev` fails its preflight while the shared `aboutme-test-db` container is
running. Only the integration owner may wait for every live-database worker to
be idle and run `make test-db-down`. Workers never stop the shared database.

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
make test-db-up
```

The integration owner restores the shared database only after Compose teardown,
and only when later work needs it.

## Runtime boundaries

Compose runs PostgreSQL, MinIO, Go, Nuxt, and Caddy as long-lived services. Two
one-shot services run first: `migrate` applies embedded goose migrations, and
the media initializer creates the private bucket. A failed migration or bucket
initialization prevents the server from starting.

PostgreSQL and the MinIO API are not published to the host. Only Caddy publishes
a port. MinIO, its initializer, and Go are the only members of the isolated
`media` network. Caddy and Nuxt cannot reach object storage. Separate `db`,
`edge`, and `frontend` networks prevent Nuxt and PostgreSQL from becoming
trusted Go proxies. Caddy strips viewer-supplied forwarding headers and sends
one canonical client address to Go.

The bucket has no public route. Go remains the authorization boundary for all
owner and public photo reads. A leaked object key does not bypass the current
resume-reference and live-state checks. Replacement and deletion revoke the
database reference and schedule exact-key cleanup; object storage is not a
second source of ownership.

## Media configuration

The server validates media configuration at startup. It accepts these closed
modes; values from another mode are errors rather than ignored settings.

| Mode               | Required settings                                                                                                                     | Settings that must be absent                                                                                 |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| Filesystem         | `MEDIA_BACKEND=fs`, `MEDIA_FS_DIR`                                                                                                    | All six S3-only settings                                                                                     |
| Custom-endpoint S3 | `MEDIA_BACKEND=s3`, `MEDIA_BUCKET`, `MEDIA_REGION`, absolute `MEDIA_ENDPOINT`, both static credentials, `MEDIA_FORCE_PATH_STYLE=true` | `MEDIA_FS_DIR`                                                                                               |
| AWS task-role S3   | `MEDIA_BACKEND=s3`, `MEDIA_BUCKET`, `MEDIA_REGION`                                                                                    | `MEDIA_FS_DIR`, `MEDIA_ENDPOINT`, `MEDIA_ACCESS_KEY_ID`, `MEDIA_SECRET_ACCESS_KEY`, `MEDIA_FORCE_PATH_STYLE` |

The S3-only settings are `MEDIA_BUCKET`, `MEDIA_REGION`, `MEDIA_ENDPOINT`,
`MEDIA_ACCESS_KEY_ID`, `MEDIA_SECRET_ACCESS_KEY`, and `MEDIA_FORCE_PATH_STYLE`.
A custom endpoint must be an absolute HTTP or HTTPS origin with no credentials,
path, query, or fragment. Static credentials must be supplied as a complete pair
and are valid only with a custom endpoint. An empty endpoint selects AWS mode
and the AWS SDK default credential chain.

Compose selects custom-endpoint S3 with `http://media:9000`, path-style access,
and credentials from `.env`. MinIO is development and self-hosting tooling;
production uses private S3 without an endpoint or static credentials.

The database password is supplied through `PGPASSWORD`, not inserted into the
database URL. This preserves passwords containing URI delimiters.

The current Caddyfile is a development trust boundary. Do not expose it to the
Internet or place it unchanged behind another proxy. Production must validate
the edge path and derive the viewer address as specified by the deployment
design.

The [self-hosting guide](../docs/guides/self-hosting.md) states the current
operator scope and TLS limits.
