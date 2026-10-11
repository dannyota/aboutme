# Nitro render services

## Source map and boundaries

|Path|Role|
|-|-|
|`routes/print/[id].get.ts`|Private print route, one-use Go capability redemption|
|`utils/print/`, `workers/print/`|Redemption transport, envelope validation, deadline, isolated Vue rendering|
|`routes/internal-render/public.post.ts`|Private transport for public HTML rendering|
|`utils/public-render/`, `workers/public-render/`|Closed public request, deadline, isolated render and sanitizer|
|`plugins/`|Worker build integration and response security headers|

Go owns authorization and supplies sanitized snapshots. Public rendering receives the privacy-safe projection; the public-render transport is not a public data API. These routes grant no access from a resume ID alone. Nitro has no database or object-storage credential access and does not fetch owner data with browser cookies.

`utils/print/protocol.ts` and `utils/public-render/envelope.ts` own protocol bounds. Their handlers and runners enforce deadlines, request and output limits, cancellation, and joined worker teardown. Keep the Go and Nitro protocol consistent; do not bypass the bounded worker with inline rendering or general network access.

## Sources and outputs

`utils/print/worker-build.ts` compiles the print worker. `utils/public-render/worker-build.ts` generates public resume and print document validators from OpenAPI. `apps/web/nuxt.config.ts` compiles the public-render worker and emits both workers into Nitro output. Workers consume the shared pure renderer under `apps/web/app/components/resume/`; do not hand-edit build output.

## Authorities and checks

Read [web rules](../../../instructions/web.md), [web design](../../../docs/design/web.md), [security design](../../../docs/design/security.md#internal-print-authority), [budgets](../../../docs/design/budgets.md), [print design](../../../docs/design/templates/print.md), and [process teardown](../../../docs/design/templates/process-teardown.md). [ADR 0011](../../../docs/adr/0011-print-capability-and-pdf-output.md) owns print capability and PDF output.

[Testing](../../../instructions/testing.md) names web and renderer gates. `apps/web/test/print/` and `apps/web/test/public-render/` cover protocol, bounds, deadlines, hostile input, and teardown; browser print checks cover final output.
