# Caddy edge

## Source map and boundaries

`Caddyfile` owns local one-origin routing. `production/Caddyfile`, `production/Caddyfile.maintenance`, and `production/edges/` own production serving, maintenance, direct Vietnam, and retained CloudFront listener configuration.

Caddy owns the client-IP trust boundary. Direct production uses the socket address, strips supplied forwarding headers, and sends the canonical address to Go. Read the exact trust rule in [deployment rules](../../instructions/deploy.md). Internal print and public-render transports must not become public routes.

## Sources and outputs

`production/build/` is an independent Go module containing production Caddy registration and privacy-safe metadata modules. `production/Dockerfile` builds it into the image; `production/render.sh` and `production/maintenance-render.sh` render edge configuration from templates.

`packages/publicroots/public-roots.v11.json` feeds `scripts/generate-public-roots.mjs`, producing `public-roots.generated.caddy` and `testdata/public-roots.generated.json`, plus Go and frontend consumers. Briefs must name every affected generated path across lanes; see the [scripts guide](../../scripts/AGENTS.md).

## Authorities and checks

Read [Vietnam production design](../../docs/design/vietnam-production.md), [CrowdSec privacy design](../../docs/design/crowdsec-http-privacy.md), [production runbook](../../docs/runbooks/production.md), and [ADR 0027](../../docs/adr/0027-vietnam-hosted-production.md).

`production/test.sh` and `production/test/` check routing, edge trust, maintenance, metadata privacy, and deployment document delivery. `production/build/` carries Go unit tests. `apps/server/internal/routetable/` checks local routing; `packages/publicroots/public-roots.test.mjs` checks registry consumers. [Verification](../../instructions/verification.md) and [release rules](../../instructions/releases.md) own hosted gates and image checks.
