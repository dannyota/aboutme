# Deployment artifacts

## Component map

|Path|Role|
|-|-|
|`vn/`|[Live Vietnam host and infrastructure](vn/AGENTS.md)|
|`caddy/`|[Route and client-IP boundary](caddy/AGENTS.md)|
|`observer/`|[Independent deployment transparency program](observer/AGENTS.md)|
|`aws/`|[Undeployed AWS reference](aws/AGENTS.md)|
|`dev-https-browser/`|[QA proofs and devops browser runner](dev-https-browser/AGENTS.md)|
|`compose.yml`|Local image smoke and self-hosting services|
|`server.Dockerfile`, `web.Dockerfile`|Shared server commands and Nuxt production images|

Production uses `vn/`, not the AWS roots. Local native and HTTPS tools live in `scripts/`; [deployment README](README.md) explains which stack to use.

## Sources and outputs

The Dockerfiles compile `apps/server/` and `apps/web/` into release images. The web image copies named source paths; new imports need its copy map updated under [deployment rules](../instructions/deploy.md). Caddy and observer have their own image sources in their component guides. `vn/host/quadlet/` templates become installed host units through the Vietnam deployment scripts.

## Authorities and checks

Read [deployment rules](../instructions/deploy.md), [release rules](../instructions/releases.md), [Vietnam production design](../docs/design/vietnam-production.md), and [production runbook](../docs/runbooks/production.md). [ADR 0026](../docs/adr/0026-vietnam-hosted-production.md) owns the live hosting choice.

Component guides name static suites. [Verification](../instructions/verification.md) owns CI evidence; [release rules](../instructions/releases.md) own image build checks and publication eligibility.
