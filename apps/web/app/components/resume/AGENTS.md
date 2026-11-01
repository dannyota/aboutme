# Pure resume renderer

## Source map and boundary

`ResumeDocument.vue` composes the header, layout, `sections/`, and `primitives/`. `resolveRenderModel.ts` resolves document and context input; `useResumeStyles.ts` projects CSS tokens. Renderer CSS belongs here, separate from application chrome.

The input is the current document plus explicit render context: pagination mode, normalized resume language, authorized photo URL when required, and the supported name-heading override. Output must be deterministic. Controllers authorize photos; a storage key never grants access or becomes a URL here.

Imports stay within the renderer and the pure dependency set enforced by `apps/web/eslint.config.mjs`: schema, Vue, approved icons, and the allowlisted font and sanitizer utilities. Do not import stores, API clients, application UI, server code, or ambient clock, randomness, locale, or network capabilities.

## Sources, outputs, and authorities

[Web design](../../../../../docs/design/web.md#pure-renderer) owns the input boundary. [Template contract](../../../../../docs/design/templates/contract.md), [tokens](../../../../../docs/design/templates/tokens.md), and [print design](../../../../../docs/design/templates/print.md) own presentation and output agreement. [ADR 0012](../../../../../docs/adr/0012-template-placement.md) owns placement; [ADR 0011](../../../../../docs/adr/0011-print-capability-and-pdf-output.md) owns print authority.

`packages/schema/templates/` feeds `packages/schema/scripts/generate.mjs` and `packages/schema/gen/ts/templates.ts`; templates are data, not separate component trees. Bundled font sources and coverage follow [font design](../../../../../docs/design/fonts.md).

## Checks

[Web rules](../../../../../instructions/web.md) and [testing](../../../../../instructions/testing.md) name renderer-boundary lint, web gates, and display checks. `apps/web/test/renderer/golden/` keeps HTML goldens. The accepted [display assertion contract](../../../../../docs/design/display-assertions.md) replaces screen/print pixel comparisons only after full inventory and Linux/macOS mutation acceptance. Until cutover, report moved PNGs under `apps/web/e2e/baselines/` and `print-baselines/` and obtain replacements through hosted Baselines. Product page PNGs remain separate assets, with their generator pending integration. Preserve same-environment output determinism and production browser pins.
