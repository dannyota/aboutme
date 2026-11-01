# Web rules

Read this file for TypeScript, Nuxt, renderer, source-manifest, or display-check work.

- Follow Google TypeScript style with the configured ESLint.
- In `apps/web`, TypeScript is pinned to 6.0.3 until `vue-tsc` supports TS 7.
- `npm audit` runs through `scripts/npm-audit.sh`: any high or critical advisory fails unless `.github/npm-audit-exceptions.json` lists its GHSA ID with a `reason` and an `expires` date. An expired entry fails every run; remove it or renew it with a new reason.
- Renderer CSS and template tokens are frontend implementation; the designer supplies visual direction and UI specs.
- Adding a web file needs the source manifest regenerated through the primary. Renderer isolation and golden HTML remain required under ADRs 0019 and 0020.
- [Display assertions](../docs/design/display-assertions.md) is the accepted replacement for every pixel comparison. Keep existing comparisons until the replacement inventory, mutation proofs, and Linux/macOS acceptance pass. Keep `web-e2e`, `web-e2e-fast`, and both shard selections; remove update mode only at cutover.
- Until cutover, report every moved baseline and use hosted `Baselines` (`.github/workflows/baselines.yml`) at the exact candidate. The primary downloads `candidate-baselines`, QA inspects the changed PNGs, and the primary commits the selected files. The workflow runs `make web-e2e-update` from a clean checkout and never commits or pushes. This interim procedure ends when the replacement lands.
- Keep the 26 product PNGs in `public/templates/pages/`. After generator integration, use the separate `make web-template-pages` / `template-pages.yml` path and inspect the exact-commit candidate artifact before copying assets and manifest. Source/output hashes detect stale assets without pixel comparison. These commands remain pending; product images must not be deleted with test baselines.
- After cutover, optional sanitized review images are non-blocking and retained for 14 days under the display design. Preserve source/browser metadata and bounded failure diagnostics; never upload raw traces, HTML, console, or PDFs.
- Native macOS proofs require the trusted-commit admission, disposable builds/profiles, and explicit test-only environment in [local tooling](../docs/design/local-tooling-simplification.md#native-admission-and-account). Wait for runner integration and acceptance. HTTPS exclusions never apply to display harness PDFs.
- For generated UI files, follow the [generated-file rule](verification.md) and the [prose exceptions](../docs/standards/engineering.md#code-comments). For Nuxt tooling during browser proofs, follow [resource rules](resources.md).
- For release image imports, read the [container-build gotcha](deploy.md).

Verification gates live in [testing](testing.md). CI lifecycle and generated-file rules live in [verification](verification.md). Local execution follows [resources](resources.md). Exact-commit hosted release CI is unchanged.
