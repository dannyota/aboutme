# Go server

## Source map and boundaries

|Path|Role|
|-|-|
|`cmd/server/`|Command wiring, dependency construction, listeners, and shutdown|
|`cmd/migrate/`, `cmd/db-setup/`|Deployment database commands, separate from serving startup|
|`internal/api/`, `internal/resumeapi/`, `internal/accountapi/`, `internal/publicapi/`, `internal/mcpapi/`|Transport adapters, HTTP envelopes, and authorization entry points|
|`internal/auth/`, `internal/password/`, `internal/secondfactor/`, `internal/oauthsrv/`|Session, credential, second-factor, and agent authorization domains|
|`internal/resume/`, `internal/store/`|Resume mutations and database persistence|
|`internal/media/`, `internal/mediacleanup/`|Authorized media intake, delivery, and exact-key cleanup|
|`internal/publicresume/`, `internal/publicstate/`, `internal/publicpage/`, `internal/publiccache/`|Privacy-safe projection, live-state gates, HTML delivery, and public artifact caching|
|`internal/printapi/`, `internal/printrender/`, `internal/renderjob/`|Private redemption, bounded rendering, and job lifecycle|

Transport packages translate requests into domain calls. Keep command wiring out of domain packages. Authenticated resume and media reads require ownership; public reads use the live projection and revocation gate, never owner data or storage keys.

## Sources and outputs

`migrations/` is the goose and sqlc schema source; `sql/` contains query sources. `sqlc.yaml` maps both to generated files in `internal/store/`, beside handwritten persistence code. The migration marker and grants follow [ADR 0005](../../docs/adr/0005-database-migrations.md).

The [schema guide](../../packages/schema/AGENTS.md) owns shared document types and the generated `internal/showcase/presets.generated.go`. The [API guide](../../docs/api/AGENTS.md) owns `docs/api/openapi.yaml` and frontend client output; contract briefs must name those outputs explicitly.

## Authorities and checks

Read [Go rules](../../instructions/go.md), [API design](../../docs/design/api.md), [data design](../../docs/design/data.md), and [security design](../../docs/design/security.md). Public and private rendering follow [ADR 0010](../../docs/adr/0010-public-artifact-revocation.md) and [ADR 0011](../../docs/adr/0011-print-capability-and-pdf-output.md).

[Testing](../../instructions/testing.md) names the server build, vet, unit, database, integration, migration, and sqlc gates. Tests live beside Go sources; migration tests also protect frozen history and grants. [Verification](../../instructions/verification.md) owns generated drift and CI evidence.
