# Phase 5B — publish UX

Status: **Revision 1, active** (2026-09-03).

P5B gives a signed-in owner the complete human publish workflow over the
already-landed publish API and public renderer. It does not add a new public
surface, agent capability, PDF renderer, image renderer, or realtime stream.

## Authorities and boundaries

- Product controls, slug lifecycle, disclosures, and human-only publishing:
  [`../../design/product.md`](../../design/product.md).
- Publish completeness: [`../../design/data.md`](../../design/data.md).
- Request, response, error, CAS, CSRF, and idempotency contract:
  [`../../api/openapi.yaml`](../../api/openapi.yaml).
- Session and recent-reauth policy:
  [`../../design/security.md`](../../design/security.md).
- Public delivery behavior: [`../../design/web.md`](../../design/web.md).
- Numeric limits: [`../../design/budgets.md`](../../design/budgets.md).
- Single-pass delivery:
  [ADR 0024](../../adr/0024-single-pass-delivery-gates.md).

P5B owns `AC-PUB-006` through `AC-PUB-010`. It consumes the P5A public behavior
in `AC-PUB-001` and `AC-PUB-004` and preserves the human/agent boundary proved
by `AC-MCP-007`. P6 owns SSE. P7B owns public PDF and image generation.

## Task index

| Task | Work                                             | Owner                                                     | Predecessor | Narrow check                                                                                           |
| ---- | ------------------------------------------------ | --------------------------------------------------------- | ----------- | ------------------------------------------------------------------------------------------------------ |
| T00  | Authorities, traceability, and dispatch package  | Integration owner                                         | None        | Markdown formatting and lint                                                                           |
| T01  | Publish transport and state controller           | Luna implementer                                          | T00         | `cd apps/web && npx vitest run test/editor/publish-api.test.ts test/editor/publish-controller.test.ts` |
| T02  | Accessible publish dialog and editor integration | Luna implementer                                          | T01         | `cd apps/web && npx vitest run test/editor/publish-dialog.test.ts test/editor/editor-shell.test.ts`    |
| T03  | Native HTTPS publish proof                       | Luna implementer plus integration-owner shared-file edits | T02         | Harness static tests, then `make dev-https-publish-check`                                              |
| T04  | Records, fresh phase review, and exit            | Integration owner; fresh Terra reviewer                   | T03         | [`exit-criteria.md`](exit-criteria.md)                                                                 |

The implementation tasks run in order because T02 consumes T01's controller
contract and T03 proves the integrated T02 flow. Each implementer writes the
named failing test first and owns the adversarial cases assigned in
[`adversarial-coverage.md`](adversarial-coverage.md). There is no per-task
review. The integration owner inspects each result, reruns its key check, and
commits coherent work. One fresh non-author reviews the integrated phase diff.

## Delivery result

The phase is complete only when an owner can save a valid resume, publish it,
open its public link, change its three independent settings, satisfy recent
reauthentication for a live-slug rename, and unpublish it through the native
HTTPS browser proof. Unit and browser evidence together must show closed error
handling without exposing raw server bodies or adding an MCP publish tool.
