# Local tooling native runner

This page holds the native macOS browser runner and proof portability contracts
of the [local tooling simplification](local-tooling-simplification.md) design,
which owns authority, the session contract, owner choices, and combined jobs.

## Native macOS browser runner

The owner accepted the contract below on 2026-11-01; implementation and runtime
acceptance remain pending. Keep `make dev-https` and `dev-https-*-check`; after
integration, Darwin selects native execution and Linux keeps containers. Never
fall back after failure. Before startup, add `make dev-https-browser-install`
under the managed lock: run
`npm --prefix deploy/dev-https-browser ci --ignore-scripts`, then its local
`node_modules/.bin/playwright install chromium webkit`. Use a private
checkout-local `PLAYWRIGHT_BROWSERS_PATH`. Match package-lock's Playwright
`1.64.0`, its [revision manifest][revisions] and Node pin mirrors; refuse drift,
missing builds, system browsers, proof-time downloads, Rosetta and amd64 images.
Keep the separate `apps/web` renderer pin; installation cannot enable rendering.

### Browser trust

Caddy keeps its private keys and `skip_install_trust`; export only its public
root through the validated local path. No system/login-keychain trust changes,
inherited profiles or blanket bypass. Node uses explicit `ca` or child-only
`NODE_EXTRA_CA_CERTS`, retaining hostname verification.

| Candidate                          | Finding and disposition                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| ---------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Chromium profile certificate store | **Accepted: full bundled Chromium with a fresh profile.** The pinned [factory](https://raw.githubusercontent.com/chromium/chromium/156.0.8078.4/chrome/browser/net/server_certificate_database_service_factory.cc) stores certificates under the profile; [import UI](https://raw.githubusercontent.com/chromium/chromium/156.0.8078.4/chrome/browser/ui/webui/certificate_manager/user_cert_sources.cc) and [verifier wiring](https://raw.githubusercontent.com/chromium/chromium/156.0.8078.4/chrome/browser/net/profile_network_context_service.cc) add real trust anchors. Use `channel: 'chromium'`, not the default headless shell.                                                                                                                                   |
| Chromium NSS or policy             | [macOS trust](https://raw.githubusercontent.com/chromium/chromium/156.0.8078.4/net/cert/internal/system_trust_store.cc) uses Apple trust integration, not Linux's NSS database. [`CACertificatesWithConstraints`](https://raw.githubusercontent.com/chromium/chromium/156.0.8078.4/components/policy/resources/templates/policy_definitions/CertificateManagement/CACertificatesWithConstraints.yaml) supports desktop profiles, but policy delivery is management configuration, not a Playwright context option. Do not change host/user policies or hand-edit internal databases.                                                                                                                                                                                        |
| Chromium SPKI exception            | [`--ignore-certificate-errors-spki-list`](https://raw.githubusercontent.com/chromium/chromium/156.0.8078.4/services/network/ignore_errors_cert_verifier.cc) takes base64 SHA-256 SPKI hashes and requires a user-data directory. It returns success before verification if any presented certificate matches. A leaf list is narrower than a CA list, but either public pinned certificate can be appended to an unrelated chain; this is not cryptographic peer pinning. Both skip hostname, expiry and chain checks. Reject both for TLS proofs; only explicitly approved functional checks could use this weaker mode.                                                                                                                                                   |
| Playwright context bypass          | [`ignoreHTTPSErrors`](https://playwright.dev/docs/api/class-browser#browser-new-context-option-ignore-https-errors) defaults false. One context limits lifetime/state, not trusted hosts or keys: all its HTTPS errors are ignored. An origin filter does not authenticate the peer. Weaker than a leaf pin; not selected.                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| Native WebKit                      | [Cocoa networking](https://raw.githubusercontent.com/WebKit/WebKit/main/Source/WebKit/NetworkProcess/cocoa/NetworkSessionCocoa.mm) uses Apple server-trust challenges; Playwright exposes [context-wide ignore](https://raw.githubusercontent.com/microsoft/playwright/v1.64.0/packages/playwright-core/src/server/webkit/wkBrowser.ts), not a custom CA API. [Apple custom anchors](https://developer.apple.com/library/archive/documentation/NetworkingInternet/Conceptual/NetworkingTopics/Articles/OverridingSSLChainValidationCorrectly.html) apply to a caller's trust object, not another browser process. No supported per-run CA injection was found for the pinned build. Use the accepted explicit native WebKit skip and retain Linux WebKit CI; no TLS bypass. |

**Attended bootstrap per run:** import only the exported root through custom
trusted certificates in `chrome://certificate-manager` in a fresh private
profile. Close and relaunch it with `launchPersistentContext`, headless full
Chromium; test contexts must inherit that profile's trust. Keep Playwright's
mock-keychain/password-store arguments. Mac bootstrap/inheritance and unattended
import are untested. Admission requires valid localhost success, wrong-root/
wrong-host/expired leaf rejection in actual test contexts, and local-root
rejection in a second empty profile. Failure prints
`Native browser CA trust unavailable`; never retry with bypass. Delete profiles
after child exit; CA rotation invalidates profiles/metadata. On CA loss or
compromise, stop the stack, rotate local keys and recreate profiles.

### Native admission and account

Use the owner's macOS account for both native HTTPS and display proofs, under
the trusted-commit boundary of ADR 0024 and `instructions/workflow.md`. Run only
commits the owner wrote or reviewed; contributor heads go to hosted CI. Review
runner, configuration, spec, workflow and dependency changes before admission.
Require a clean candidate and match its staged source hash to the admitted
commit. A reviewed but buggy commit runs with the owner's privileges, as
`make ci-local` already does. This is the accepted residual risk, not a new
account boundary.

The owner retains his Podman connection and existing `aboutme-test-db`; only
stack startup invokes the existing idempotent `test-db-up`. Browser children
receive no engine socket/configuration. Preserve `aboutme` and `aboutme_dev`,
collision checks and the one-database rule. No provisioning, engine sharing with
another UID, database recreation or reset is part of native admission.

Build from validated archives in disposable 0700 run directories; use private
HOME/TMPDIR, browser profiles and caches, with 0600 single-link inputs and no
linked ancestors. Explicitly allowlist PATH, tool/browser paths,
locale/timezone, local origins and test configuration instead of spreading
`process.env`.
[Playwright inherits environment by default](https://playwright.dev/docs/test-webserver).
Supply only test credentials, no production secret, credential agent socket or
CA private key. Set `chromiumSandbox: true`, origin filters and blocked service
workers. These measures control inputs and cleanup, not owner filesystem access.
After failure, join owned children, remove run directories/profiles, revoke
fixture sessions/tokens and rotate affected local keys. Preserve shared data.
Container browsers remain the Linux alternative; no Mac VM tunnel is proposed.

### Server rendering

**Accepted: explicit Mac skips**, with full Linux CI coverage after native
runner integration. In `apps/server/internal/app/print.go`, Darwin closes the
render queue; `printrender/renderer.go` requires nonroot Linux user namespaces.
Keep these guards. Skip whole mixed journeys, with no split in this change.
Paths below are relative to `deploy/dev-https-browser/`; lines identify the
reviewed source.

| Mac disposition | Exact test and lost local coverage                                                                                                                                                                                                                       |
| --------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Skip            | `exports.spec.ts:310`, all owner/public export gates.                                                                                                                                                                                                    |
| Skip            | `privacy.spec.ts:562–917`, account export, cancellation, reauthentication, deletion, session/MCP revocation, public revocation and tombstone. PDF/PNG success at 642–650 precedes JSON export at 657–675. None of this journey counts as locally proved. |
| Skip            | `public.spec.ts:356–619`, sign-in-to-view gate, pass holders and switch-off; PDF success at 565 and 595 requires Linux.                                                                                                                                  |
| Skip            | `public.spec.ts:621–1062`, publication, SSR/hydration, styling, metadata, artifacts and skip link; PNG/PDF checks at 887–965 require Linux.                                                                                                              |
| Run             | `public.spec.ts:1067–1083` bilingual MCP guide, 1088–1092 guide-root 404, 1097–1123 unauthenticated MCP challenge; all other independent portable specs, including Chromium client-side LinkedIn PDF parsing.                                            |

Linux CI keeps every whole journey and its all-true evidence schema. The native
HTTPS runner job owns explicit selection and native evidence; never skip an
assertion inside a test that reports full success. Print
`SKIP: server PDF/PNG rendering requires Linux; covered by CI`. Native WebKit
prints `SKIP: native WebKit CA trust unavailable; covered by CI`. Record fixed
skip reasons and partial coverage; neither skip satisfies the existing
aggregate. CI retains every engine, assertion and completeness check.

Native evidence uses a separate closed version-1 envelope: `mode: native-macos`,
`coverage: partial`, and fixed test IDs mapped to `passed` or `skipped`, with
only `linux-server-rendering` and `native-webkit-ca` skip reasons. Public guide
results have three separate IDs; no hydration evidence is fabricated. Export and
privacy contain skip records only. Require every selected runnable test, shard
and phase to pass, every expected exclusion to appear, zero unexpected skips and
successful health/cleanup checks. Missing/extra IDs fail. Print `PARTIAL` on
successful native completion; never feed it to the Linux aggregate.

| Rendering alternative        | Why deferred                                                                                                                                                                                                                                                                                                                                                  |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| arm64 Linux render container | Go's queue/controller is in-process and direct origins are allowlisted. A worker needs a designed bridge or the Go render runtime moved into Linux, with private redemption, one-use capabilities, revocation and joined teardown under [ADR 0011](../adr/0011-print-capability-and-pdf-output.md) and [ADR 0010](../adr/0010-public-artifact-revocation.md). |
| Darwin rendering             | Chromium can print, but Linux user namespaces and supervisor guarantees do not transfer. An equivalent sandbox, supervision and resource design needs separate proofs. Keep Linux guards; neither alternative belongs in this simplification.                                                                                                                 |

### Sources and evidence

`run-native.sh` uses the existing specs, manifest, shard map and evidence
validator through a shared path adapter for `/uat-input`, `/evidence` and
temporary output; preserve container defaults and production custody. Both
runners consume one SHA-256 archive with sorted closed entries and fixed
metadata. Verify before extraction; reject links, traversal and extra files.
Hash staged sources before/after execution; changed live source/config fails
admission or completion. Match archive/manifest/dependency hashes across OSes.
Refuse dirty native candidates; no inherited CI evidence, Mac spec copies or
goldens. Linux working-copy diagnostics remain marked dirty.

Retain schema/byte limits, fixed diagnostics and newest 10 runs per prefix in
`.dev/native-https/evidence/`. Add at most 4 KiB metadata: source/archive/
dependency hashes, OS/architecture, browser revisions, mode/shards, trust and
isolation modes, fixed results/skips; no arbitrary strings or substitute image
ID. Disable traces, videos, screenshots, HTML reports and page-content errors.
Capture raw output privately, emit validated diagnostics, delete raw output and
profiles on every exit after bounded child joining; cleanup failure fails. Keep
fixture cleanup, factor phases, timeouts and stack health checks.

## Proof portability

Accepted coverage is available only after integration and native acceptance.
Specs live in `deploy/dev-https-browser/`; the rendering/trust sections define
exact skips.

| Proofs                                                                                                                               | macOS native                                                                                             | Linux and hosted amd64                                                      |
| ------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| `auth`, `password-auth`, `transport`, `entry`, `editor`, `public`, `publish`, `exports`, `privacy`, `sample-start`, `linkedin` specs | Strict Chromium; render-dependent public/privacy cases and exports skip.                                 | Same specs in pinned container.                                             |
| `second-factor` and `totp` specs                                                                                                     | Chromium, all shards and enabled/disabled phases; passkeys use CDP virtual authenticators, not Touch ID. | Same shards and coverage validators.                                        |
| `mcp`, `mcp-ts-sdk` specs                                                                                                            | Chromium plus native Node SDK requests.                                                                  | Same specs.                                                                 |
| `linkedin-import.spec.ts`                                                                                                            | Strict Chromium including hostile PDF parsing; WebKit skips.                                             | Container Chromium and WebKit.                                              |
| `mcp-sdk.spec.ts`                                                                                                                    | Local synthetic Go SDK workflow after its path/custody adapter; production mode refuses.                 | Existing local and explicitly authorized production wrappers.               |
| `totp-production.spec.ts`, `production.config.ts`                                                                                    | Outside local runner; refuse production modes.                                                           | Existing explicitly authorized container path; not ordinary CI credentials. |
| `make web-e2e`, `make web-e2e-fast`                                                                                                  | Native screen and harness-PDF assertions under [display assertions](display-assertions.md).              | Same assertion contract, every hosted shard.                                |
| HTTPS lifecycle static cases                                                                                                         | Port portable session/listener cases; exercise them natively.                                            | Keep every static shard and completeness.                                   |
| `server-runtime-smoke`, `renderprocess` Linux `/proc` tests, cgroup budget probes, `vn-infra`                                        | Explicit Linux-only skip.                                                                                | Linux kernel/container checks; smoke runs hosted amd64.                     |

Display assertions owns removal of `web-e2e-update` and `Baselines`, product
asset generation and diagnostic images. Harness PDFs use native Chromium and do
not call the Linux server render queue. Only the explicit HTTPS trust/rendering
exclusions apply; Mac evidence cannot certify Linux fonts, rendering, cgroups or
production teardown. Keep `ci-local` separate from opt-in functional proofs;
hosted CI remains mandatory for release. Extend `macos-tooling` with portable
helper/runner contracts; real native browser acceptance needs a separately
authorized bounded Mac proof.

[revisions]:
  https://raw.githubusercontent.com/microsoft/playwright/v1.64.0/packages/playwright-core/browsers.json
