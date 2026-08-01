# 0007 — `/healthz` and `/readyz` unversioned, outside `/api/v1`

Status: Accepted (2026-08-01)

## Context

Health checks are infrastructure, not product API: they exist for the ECS
container agent and the CloudFront→Caddy→app synthetic check, not for clients of
the resume API. An eventual `/api/v2` must never break an orchestrator or
synthetic check that has no reason to know a version exists. The Caddy route
table already sends other non-product paths — `/sitemap.xml`, `/robots.txt`,
`/llms.txt` — to the Go server outside `/api/v1`; health checks follow the same
pattern.

## Decision

`/healthz` and `/readyz` are served at the site root
(`https://aboutme.vn/healthz`, `/readyz`), not under `/api/v1`.

## Consequences

- `/healthz` is liveness only and **never touches the database** — a database
  outage must not cause the container health check to fail and restart-loop the
  task.
- `/readyz` is readiness and **does** check the database (and render queue
  saturation), returning `503` with an error envelope when not ready; this is
  what gates traffic admission, not liveness.
- The OpenAPI document needs a path-level `servers` override for these two
  operations (root `https://aboutme.vn`, not the `/api/v1` server) so the
  contract accurately describes where they live.
