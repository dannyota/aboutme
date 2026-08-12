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
It cannot run beside the shared `aboutme-test-db` container. Only the
integration owner may schedule this handoff after every live-database worker is
idle. `make dev` must fail its preflight while that container is running.

From the repository root:

```sh
cp .env.example .env
# Set POSTGRES_PASSWORD to a new local value.
# Set CADDY_HTTP_PORT=8080 when rootless Podman cannot bind port 80.
make test-db-down
make dev
podman compose --env-file .env -f deploy/compose.yml ps
curl --fail http://localhost:8080/healthz
curl --fail http://localhost:8080/readyz
make dev-down
make test-db-up
```

Use port 80 when `CADDY_HTTP_PORT` is unset. Confirm that the `migrate` service
exited successfully and that server, web, Caddy, and PostgreSQL are healthy.
Always run `make dev-down` when the smoke session ends; the Compose stack is not
the daily development environment. Restore the shared database only after
Compose teardown, and only when later work needs it. A worker never performs
this handoff or stops the shared database.

## Acceptance entry conditions

P9 can start only when all of these are true:

- the UAT deployment serves `https://localhost` on port 443 through the real
  route table;
- the future `make uat-up`, `make uat-reset`, and `make uat-down` targets exist
  and manage only the isolated `aboutme-uat` project and its disposable data;
- rootless Podman can bind 443, and the isolated browser profile trusts the
  recorded UAT Caddy root without certificate-error bypass flags;
- the complete product slice and frozen P9 scenarios are present;
- `make ci` passes at the exact candidate commit;
- the integration owner has every live-database worker's idle acknowledgment,
  has stopped the native stack and `aboutme-test-db`, and has verified that no
  other aboutme PostgreSQL container is running;
- image IDs, migration head, configuration names without values, and tool
  versions can be recorded;
- the project-scoped Playwright service can reach the HTTPS origin;
- no AWS, Cloudflare, certificate, DNS, registry, or staging mutation is
  required.

These `uat-*` targets do not exist yet. The active
[P9 plan](../plans/phase-9/README.md) owns their implementation order, frozen
scenarios, and evidence schema. Update that plan and the deployment together
when the HTTPS harness lands.

## Required execution shape

At the accepted candidate commit, the main session will:

1. After `make ci`, wait for live-worker acknowledgments, stop the native stack
   and shared test database, verify the one-database handoff, then build and
   start the complete Compose deployment on HTTPS port 443. The future
   `make uat-up` target must fail closed if another aboutme PostgreSQL container
   is running.
2. Record the commit, image IDs, migration head, commands, timestamps, tool
   versions, configuration names, and every state change.
3. Run each frozen scenario through Playwright and attach accessibility,
   screenshot, console, network, request-ID, server-log, and database evidence
   as required by that criterion.
4. Record expected and observed results with `PASS`, `FAIL`, or `BLOCKED`.
   Missing evidence and `BLOCKED` both fail the gate.
5. Send the pinned evidence to a fresh independent reviewer for a live,
   read-only probe while the deployment still runs.
6. After that live review passes, tear down the isolated deployment with the
   future `make uat-down` target.
7. Have the reviewer verify cleanup and issue the final cleanup verdict.
8. Verify the final manifest and reviewer digest at the candidate commit.
9. Restore `aboutme-test-db` only after UAT teardown and the cleanup verdict,
   and only when later work needs it.

A later product-code change makes evidence stale for every changed path. Rerun
those scenarios at the commit being shipped.

Only after local UAT and its evidence review pass may the owner be asked to
authorize external resource creation. Production launch needs separate later
approval.
