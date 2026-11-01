# Deployment rules

Read this file for Podman, Caddy, container images, host access, or deployment work.

- Use Podman and `podman compose` locally. Hosted release-image workflows use Docker.
- Caddy is the client-IP trust boundary. Go accepts the canonical header only from configured trusted proxies and never parses `X-Forwarded-For`.
- `deploy/web.Dockerfile` copies named paths only. When the web app starts importing a new directory, add it there too; CI builds from the full checkout and cannot catch the gap, only the release image build does.
- Avoid `podman ps | grep -q` under `pipefail`; capture output first.
- If host DB TCP fails while in-container `pg_isready` passes, recreate the container; rootless pasta can lose its forward.
- Before a laptop deploy, turn off VPNs such as Cloudflare WARP; the deploy smoke assumes SSH and HTTPS leave through the same address.
- Production runs PostgreSQL on the Vietnam host; operator access uses SSH and the Unix socket. Never print row data or secrets.
- Follow the [production runbook](../docs/runbooks/production.md) and prefer infrastructure as code. Production infrastructure and release paths beyond one-line changes need adversarial review before merge. The primary applies and deploys already reviewed commits under standing approval without a new review, and stops on unexpected destroys. Check that secrets exist; never read their values.

Deployment procedures live in the [production runbook](../docs/runbooks/production.md). Release procedures and eligibility live in [releases](releases.md). Local execution follows [resources](resources.md).
