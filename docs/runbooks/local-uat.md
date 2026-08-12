# Local UAT

Status: **blocked for acceptance**. The P9 contract requires the complete
image-based deployment at an HTTPS origin on port 443. The current Compose
Caddyfile exposes HTTP only, so it cannot produce valid authentication or
end-to-end UAT evidence.

Do not mark a P9 criterion `PASS` against the current HTTP origin. Do not weaken
Secure cookies or the HTTPS requirement to bypass the blocker.

## Current Compose smoke check

The current deployment can still prove image construction, migration ordering,
container health, network routing, and teardown. This is a smoke check, not UAT.

From the repository root:

```sh
cp .env.example .env
# Set POSTGRES_PASSWORD to a new local value.
# Set CADDY_HTTP_PORT=8080 when rootless Podman cannot bind port 80.
make dev
podman compose --env-file .env -f deploy/compose.yml ps
curl --fail http://localhost:8080/healthz
curl --fail http://localhost:8080/readyz
make dev-down
```

Use port 80 when `CADDY_HTTP_PORT` is unset. Confirm that the `migrate` service
exited successfully and that server, web, Caddy, and PostgreSQL are healthy.
Always run `make dev-down` when the smoke session ends; the Compose stack is not
the daily development environment.

## Acceptance entry conditions

P9 can start only when all of these are true:

- the UAT deployment serves `https://localhost` on port 443 through the real
  route table;
- `make uat-up`, `make uat-reset`, and `make uat-down` manage only the isolated
  `aboutme-uat` project and its disposable data;
- rootless Podman can bind 443, and the isolated browser profile trusts the
  recorded UAT Caddy root without certificate-error bypass flags;
- the complete product slice and frozen P9 scenarios are present;
- `make ci` passes at the exact candidate commit;
- image IDs, migration head, configuration names without values, and tool
  versions can be recorded;
- the project-scoped Playwright service can reach the HTTPS origin;
- no AWS, Cloudflare, certificate, DNS, registry, or staging mutation is
  required.

The active [P9 plan](../plans/phase-9/README.md) owns the frozen scenarios and
evidence schema. Update that plan and the deployment together when the HTTPS
harness lands.

## Required execution shape

At the accepted candidate commit, the main session will:

1. Build and start the complete Compose deployment on HTTPS port 443.
2. Record the commit, image IDs, migration head, commands, timestamps, tool
   versions, configuration names, and every state change.
3. Run each frozen scenario through Playwright and attach accessibility,
   screenshot, console, network, request-ID, server-log, and database evidence
   as required by that criterion.
4. Record expected and observed results with `PASS`, `FAIL`, or `BLOCKED`.
   Missing evidence and `BLOCKED` both fail the gate.
5. Tear down the isolated deployment with `make uat-down`.
6. Send the pinned evidence to a fresh independent reviewer.

A later product-code change makes evidence stale for every changed path. Rerun
those scenarios at the commit being shipped.

Only after local UAT and its evidence review pass may the owner be asked to
authorize external resource creation. Production launch needs separate later
approval.
