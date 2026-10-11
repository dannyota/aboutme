# Documentation

## Index routes and lifecycle

|Path|Entry point and purpose|
|-|-|
|`design/README.md`|Intended behavior and domain designs|
|`adr/README.md`|One decision per topic and its status|
|`architecture.md`, `architecture/`|Current running components and implemented behavior|
|`runbooks/README.md`|Operator procedures|
|`guides/README.md`|Stable contributor and self-hosting guidance|
|`api/`|[Implemented HTTP contract](api/AGENTS.md)|
|`plans/README.md`, `plans/backlog.md`, `plans/traceability/`|Remaining work, follow-ups, and acceptance evidence|
|`research/README.md`|Historical evidence and research index|
|`standards/engineering.md`|Human documentation and comment rules|

The root [authority map](../AGENTS.md#map) owns precedence. Accepted ADRs override conflicting design; code, deployment configuration, and OpenAPI describe existing behavior and must agree. Choose each artifact through [documentation instructions](../instructions/documentation.md), which own plan deletion and evidence handling. Keep each detailed contract in its owning document and link from indexes.

## Sources, outputs, and checks

`api/openapi.yaml` generates the frontend client through the [API guide](api/AGENTS.md). `brand/src/` feeds `apps/web/scripts/render-brand.mjs`; `brand/README.md` maps brand sources and exported assets.

[Testing](../instructions/testing.md) and [documentation instructions](../instructions/documentation.md) own human-document formatting and agent-only exclusions. `scripts/check-doc-links.mjs` checks local links; `scripts/check-lengths.sh` checks file size. `scripts/test/codex-agent-test.sh` checks launcher wiring and settings validation using fixture roles.
