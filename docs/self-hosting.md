# Self-hosting

> Living document — expands as the apps land.

aboutme is AGPL-3.0 and designed to be self-hostable with **podman compose**
(one origin: Caddy → Go API + Nuxt, plus PostgreSQL). The compose file will live
at `deploy/compose.yml` and is the same stack used for local development.

Until the first runnable slice exists, see
[`specs/aboutme-design.md`](specs/aboutme-design.md) §6 for the deployment
design.
