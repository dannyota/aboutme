# P9 HTTPS harness

## Deployment contract

`make uat-up`, `make uat-reset`, and `make uat-down` target only Compose project
`aboutme-uat`. Reset and teardown verify exact project and resource labels. An
absent, extra, or mismatched target fails closed.

The resolved base-plus-overlay configuration contains:

- PostgreSQL, migration, Go, Nuxt SSR, Caddy, and `mock-oauth`;
- one pinned MinIO service and one pinned one-shot bucket initializer;
- isolated PostgreSQL and unversioned media volumes;
- a private `aboutme-uat` bucket with disposable credentials;
- `MEDIA_BACKEND=s3`, the UAT bucket and region, path-style access, and the
  internal MinIO endpoint on the Go service only.

The initializer creates the bucket and a bucket-limited application identity.
Root credentials reach only MinIO and the initializer; application credentials
reach only the initializer and Go. Values stay mode `0600` under `/dev/shm` and
are not evidence. The stack never reuses test-S3 configuration or state.

Only Caddy publishes `127.0.0.1:443:443`; other services publish no port. Its
internal certificate covers `localhost`. Nothing binds `[::1]`; probes and
scripts resolve `localhost` to `127.0.0.1`.

The browser uses `--network=host`; Chrome maps `localhost` to `127.0.0.1`. This
preserves the certificate name and removes host-dependent address selection.
Tests reject IPv6, wildcard mapping, a second port, or other networking.

The stack has project-scoped database, edge, frontend, media, and OAuth
networks. Only Go, Caddy, and `mock-oauth` join OAuth. Go and Caddy reach the
unpublished mock at alias `mock-oauth`; Caddy routes `/__uat/oauth/*` there
before Nuxt. Only MinIO, its initializer, and Go join media. Readiness proves
both OAuth paths, the database path, and a private media put/get/delete.

`uat-reset` recreates only UAT database, media, and mock state. It loads
deterministic fake accounts, identities, resumes, and media. `uat-down` removes
only allowed UAT runtime resources and leaves the ignored evidence directory.

## Trusted browser

The browser image, platform, Playwright MCP, Chrome, and package lock are
pinned. Its entrypoint imports the Caddy root into a disposable trust store. It
never changes host trust, uses a personal profile, or bypasses certificate
errors.

The wrapper mounts only:

- a generated input directory containing the Caddy root and non-secret run
  metadata, read-only at `/uat-input`;
- a separate scenario-specific volatile output directory, read-write at
  `/evidence`.

It does not mount the repository, home directory, `.env`, container socket, or
final evidence directory. The container has a read-only root filesystem and
disposable tmpfs paths. It uses `--security-opt label=disable` so SELinux does
not relabel host paths; the input stays read-only and the dedicated output path
is the only writable bind mount.

The ignored `.mcp.json` uses `--isolated`, exact Chrome, `https://localhost` as
the sole origin, no output eviction, arbitrary code, or host-file access, and
the volatile scenario directory as output.

## Mock OAuth contract

The mock is deployment-only test support, not an OpenAPI route or a production
server feature.

- Google and LinkedIn expose browser authorization, OpenID Connect discovery,
  JSON Web Key Set, and token endpoints. GitHub exposes browser authorization,
  token, `/user`, and `/user/emails` endpoints.
- The mock exact-matches callback URLs, preserves `state`, validates OpenID
  Connect `nonce` and Proof Key for Code Exchange S256, and consumes opaque
  codes once. Its accessible page selects a named fake account and outcome.
- Reset restores verified, unverified, missing-email, duplicate-email,
  already-linked, expired, replay, denial, and provider-error outcomes.
- It refuses any environment except `uat`, any public origin except
  `https://localhost`, and any callback outside its allowlist. It has no
  management endpoint. Client credentials and signing fixtures are visibly fake.

Configuration adds `uat` to the closed `ENV` set. In `ENV=uat`, all five
endpoint variables below are required. Browser URLs stay on the public origin;
backchannel URLs use the private mock alias. Setting one in `dev`, `staging`, or
`prod` fails startup. Production retains built-in provider endpoints. The
overlay passes the same required values to `server` and the one-shot `migrate`
service while both use the shared configuration loader.

```text
GOOGLE_OIDC_ISSUER_URL=http://mock-oauth:8080/google
LINKEDIN_OIDC_ISSUER_URL=http://mock-oauth:8080/linkedin
GITHUB_OAUTH_AUTHORIZE_URL=https://localhost/__uat/oauth/github/authorize
GITHUB_OAUTH_TOKEN_URL=http://mock-oauth:8080/github/token
GITHUB_API_BASE_URL=http://mock-oauth:8080/github
```

## U1–U4: provider boundary

### U1 — Blind endpoint tests

Tier: **High risk, independent test author**.

Own only `apps/server/internal/config/uat_adversarial_test.go` and
`apps/server/internal/auth/uat_endpoints_adversarial_test.go`. Derive tests for
the closed environment set, missing or cross-environment variables,
public/internal authority confusion, callback escape, and production defaults.
Run and record the expected failure:

```sh
(cd apps/server && go test ./internal/config ./internal/auth -run UAT -count=1)
```

Freeze the test-only diff before U2 reads it.

### U2 — Server endpoint configuration

Tier: **High risk, implementation author**.

Own `.env.example`, `apps/server/internal/config/{config.go,config_test.go}`,
`apps/server/internal/auth/{handlers.go,google.go,linkedin.go,github.go}`, and
new `apps/server/internal/auth/uat_endpoints_test.go`. First add author tests
for each configuration and dispatch branch, run the U1 command, and record the
expected failure before changing implementation. Implement without weakening U1,
then run:

```sh
make server-build server-vet server-test
make semgrep
```

### U3 — Blind mock-provider tests

Tier: **High risk, independent test author**.

Own only `apps/server/internal/uatmock/adversarial_test.go`. Derive black-box
tests for callback equality, state, nonce, PKCE S256, one-use codes,
issuer/audience/signature checks, named failures, startup guards, and bounded
inputs. Run and record the expected failure:

```sh
(cd apps/server && go test ./internal/uatmock ./cmd/mock-oauth -count=1)
```

Freeze the test-only diff before U4 reads it.

### U4 — Mock OAuth service

Tier: **High risk, implementation author**.

Own
`apps/server/internal/uatmock/{service.go,protocol.go,fixtures.go,service_test.go,protocol_test.go}`,
`apps/server/cmd/mock-oauth/{main.go,main_test.go}`, and
`deploy/mock-oauth.Dockerfile`. First write the two author test files, run the
U3 command, and record the expected failure before writing implementation. Use
only pinned Go dependencies. The normal server image and base Compose file stay
unchanged. Then run:

```sh
(cd apps/server && go test ./internal/uatmock ./cmd/mock-oauth -count=1)
make server-build server-vet server-test
make semgrep
```

## U5–U7: isolation and trust

### U5 — Blind deployment and trust tests

Tier: **High risk, independent test author**.

Own only `apps/server/internal/routetable/uat_route_table_adversarial_test.go`,
`scripts/uat-{isolation-test,identities-adversarial-test}.sh`, and
`scripts/uat-browser-trust-test.mjs`. Derive checks for exact destructive
targets; one database and one object store; the service, network, media, volume,
sentinel-schema, and sentinel-coverage contracts; IPv4-only port 443; route and
private-media readiness; reset containment; root matching; trusted browser
reach; mount separation; and rejection of bypass flags, personal paths, real
credentials, external origins, or test-service reuse. Identity cases prove
`paths` accepts a task-owned dirty diff while `images`, `seal`, and `final`
reject any dirty tree.

Before U6 or U7 starts, run every command and record the expected contract
failures caused by missing implementation:

```sh
(cd apps/server && go test ./internal/routetable -run '^TestRouteTableUAT' -count=1 -v)
bash -n scripts/uat-isolation-test.sh
bash scripts/uat-identities-adversarial-test.sh
node --check scripts/uat-browser-trust-test.mjs
bash scripts/uat-isolation-test.sh --static
node scripts/uat-browser-trust-test.mjs --static
```

Freeze the test-only diff.

### U6 — HTTPS overlay and lifecycle

Tier: **High risk, implementation author**.

Own `deploy/compose.uat.yml`, `deploy/caddy/Caddyfile.uat`,
`deploy/uat/{fixtures/**,sentinels.schema.json,sentinel-coverage.json}`,
`scripts/uat.sh`, `scripts/uat-lifecycle-test.sh`, and
`scripts/uat-identities.{sh,test.sh}`. First write the lifecycle and identity
author tests for that exact clean/dirty mode split, run the frozen U5 suites,
and record the expected failures before implementation. Then run:

```sh
bash -n scripts/uat.sh scripts/uat-lifecycle-test.sh scripts/uat-identities.sh scripts/uat-identities-test.sh
bash scripts/uat-identities-test.sh
bash scripts/uat-identities-adversarial-test.sh
bash scripts/uat-lifecycle-test.sh --static
bash scripts/uat-isolation-test.sh --static
(cd apps/server && go test ./internal/routetable -run '^TestRouteTableUAT' -count=1 -v)
make route-table-test
```

Non-secret stack state and the exported root live under
`.superpowers/uat/p9-runtime/`. Runtime credentials and run sentinels live only
in the mode-`0700` run root under `/dev/shm`; teardown proves that root is gone.

### U7 — Isolated trusted browser

Tier: **High risk, implementation author**.

Own `deploy/uat-browser/{Dockerfile,package.json,.containerignore}`,
`scripts/uat-mcp.sh`, `scripts/uat-mcp-test.sh`, and local ignored `.mcp.json`.
The integration owner generates `deploy/uat-browser/package-lock.json` in a
serialized lockfile window. First write the static wrapper test and record its
expected failure. Resolve every image digest and executable before dispatch.
Build and test exactly, with U6 running:

```sh
bash -n scripts/uat-mcp.sh
bash -n scripts/uat-mcp-test.sh
bash scripts/uat-mcp-test.sh --static
. scripts/uat-identities.sh paths
podman build --pull=never -f deploy/uat-browser/Dockerfile \
  --iidfile "$P9_BROWSER_IID_FILE" deploy/uat-browser
IFS= read -r P9_BROWSER_IMAGE_ID < "$P9_BROWSER_IID_FILE"
bash scripts/uat-mcp-test.sh --image-id "$P9_BROWSER_IMAGE_ID"
bash scripts/uat-mcp.sh --check --image-id "$P9_BROWSER_IMAGE_ID"
node scripts/uat-browser-trust-test.mjs --image-id "$P9_BROWSER_IMAGE_ID"
```

The build context is exactly `deploy/uat-browser/`. Its `.containerignore`
starts deny-all and admits only the Dockerfile and two package manifests. The
Dockerfile uses named `COPY` sources, never `COPY .`. Static and image-inventory
tests place an unexpected context file and prove it, `.env`, `.git`,
`.superpowers`, `.dev`, and test output cannot enter the context or image.

The wrapper, local MCP configuration, and U8 target accept only the immutable ID
read from the IID file; they reject a tag, missing ID, or contract mismatch. The
check reaches `https://localhost/healthz` through host networking with IPv4
pinned and no TLS bypass. It records the image ID, executable, root, resolved
address, raw formats, trace allowlist, and capture flags in
`.superpowers/uat/p9-tooling/<commit>/browser-output-contract.json`. That closed
file and its digest are the U7-to-U10 handoff.

## U8–U9: integration gate

### U8 — Integration targets and living docs

Tier: **Normal, integration owner only**.

Own the root `Makefile`, `deploy/README.md`, and `docs/runbooks/local-uat.md`.
Consume the
[shared identity contract](README.md#candidate-identity-initialization) and add
`uat-browser-image`, `uat-up`, `uat-reset`, `uat-browser-check`, and `uat-down`.

After `make ci` and the scheduled resource handoff in
[the phase index](README.md#host-and-shared-resource-gate), run:

```sh
. scripts/uat-identities.sh images
make uat-up
make uat-reset
make uat-browser-check P9_BROWSER_IMAGE_ID="$P9_BROWSER_IMAGE_ID"
make uat-down
```

### U9 — Harness defect review and gate

A fresh reviewer who authored none of U1–U8 or U10 reviews the full diff, design
fit, interface stability, destructive targeting, provider and media isolation,
TLS trust, IPv4 policy, evidence-tool interface, test independence, and
traceability. Authors fix blockers; an independent reviewer rechecks them.

At one unchanged candidate commit, the integration owner runs, in order:

1. `make ci` and `make scan` while the shared test services are available.
2. The worker acknowledgements and resource handoff in the phase index.
3. `. scripts/uat-identities.sh paths`, then
   `make uat-browser-image P9_BROWSER_IID_FILE="$P9_BROWSER_IID_FILE"` and
   `make uat-evidence-test P9_EVIDENCE_IID_FILE="$P9_EVIDENCE_IID_FILE"`. Source
   `images` after both builds.
4. The exact U8 lifecycle without retry. Its browser check writes the contract
   for the rebuilt browser image.
5. Run `scripts/uat-identities.sh seal`, then source `final`. This records both
   immutable images, the fresh browser contract, and coverage map at the exact
   final commit.
