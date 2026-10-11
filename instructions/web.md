# Web rules

Read this file for TypeScript, Nuxt, renderer, source-manifest, or baseline work.

- Follow Google TypeScript style with the configured ESLint.
- In `apps/web`, TypeScript is pinned to 6.0.3 until `vue-tsc` supports TS 7.
- `npm audit` runs through `scripts/npm-audit.sh`: any high or critical advisory fails unless `.github/npm-audit-exceptions.json` lists its GHSA ID with a `reason` and an `expires` date. An expired entry fails every run; remove it or renew it with a new reason.
- Renderer CSS and template tokens are frontend implementation; the designer supplies visual direction and UI specs.
- Adding a web file needs the source manifest regenerated through the primary. Report every pixel baseline a renderer change moves; generate it through hosted Baselines.
- Update pixel baselines by dispatching the `Baselines` workflow (`.github/workflows/baselines.yml`, manual dispatch only) on the branch, downloading its `candidate-baselines` artifact, copying the files into place, and committing. It runs `make web-e2e-update` on a clean hosted checkout (the target itself still refuses a dirty tree) and never commits or pushes.
- For generated UI files, follow the [generated-file rule](verification.md) and the [prose exceptions](../docs/standards/engineering.md#code-comments). For Nuxt tooling during browser proofs, follow [resource rules](resources.md).
- For release image imports, read the [container-build gotcha](deploy.md).

Verification gates live in [testing](testing.md). CI lifecycle and generated-file rules live in [verification](verification.md). Local execution follows [resources](resources.md).
