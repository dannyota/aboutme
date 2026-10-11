# HTTP contract

## Source map and boundaries

`openapi.yaml` owns the implemented HTTP contract. `redocly.yaml` owns scoped lint settings; `test/` contains contract tests. Keep operations, request and response bounds, envelopes, write safety, and examples consistent with Go transport behavior.

## Sources and outputs

`openapi.yaml` feeds `apps/web/scripts/openapi-gen.sh`, which writes `apps/web/app/api/generated/`. `apps/web/scripts/api-drift-check.sh` compares fresh output without mutating the checkout. `apps/web/app/api/client.ts` is the handwritten typed-client factory.

The contract source belongs to the backend lane; generated client output belongs to the frontend lane. A contract brief must explicitly include both paths and their affected tests. Coordinate the output owner before changing the contract.

## Authorities and checks

Read [API design](../design/api.md), [security design](../design/security.md), [Go rules](../../instructions/go.md), [web rules](../../instructions/web.md), and [verification](../../instructions/verification.md). [API README](README.md) maps generation and drift wiring.

[Testing](../../instructions/testing.md) names the API gate. `test/openapi.test.ts` and the focused contract tests check operations and invariants; `test/generate-wiring.test.ts` checks generation wiring. Generated frontend output also needs the applicable web gates.
