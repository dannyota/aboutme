# Local tooling simplification

Status: owner decisions accepted, 2026-11-01; implementation pending. Use **A:
one session per HTTPS stack** on macOS and Linux, with native Playwright
functional proofs on the Mac. The owner accepted every choice below on
2026-11-01, including profile-only CA trust in a disposable Chromium profile, no
native WebKit for HTTPS proofs, and whole-journey Mac skips for server-rendering
tests. [Display assertions](display-assertions.md) replaces all pixel
comparisons. Remove per-process `/proc` identity, laptop RSS/nice enforcement,
numeric job ceilings and dependency borrowing. Add a capped production startup
smoke.

The owner has a 36 GiB Apple silicon MacBook and two Linux ThinkPads. The
[Linux runner](local-linux-runner.md) stays withdrawn. No execution is
authorized.

## Authority and evidence

[ADR 0024](../adr/0024-delivery-gates.md#amendment-tooling-and-display-checks-2026-11-01)
records the owner-approved tooling and native-proof changes. The amendments to
ADRs 0019 and 0020 adopt display assertions. Implementation remains pending;
existing commands and gates remain until their replacements land. Retain trusted
commits, independent review, secret scans, full main CI, and exact-commit
release gates. [ADR 0026](../adr/0026-vietnam-hosted-production.md) and
[host budgets](vietnam-host-memory.md) remain unchanged.

## HTTPS options and local scope

| Option                | Counted change and cases                                                                                                                                                                                         | macOS                                                      | Residual risk and cost                                                                                   |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| A: one session        | Remove 420 identity lines; add an estimated 120–180 session lines, net 240–300 fewer before caller edits. Replace 12 cases with 5; retain 24, total 29.                                                          | Selected for native stack and native browser proofs.       | Unmeasured check-to-signal reuse race; missing anchor needs manual cleanup. No new image.                |
| B: labeled containers | Remove the same 420 lines; estimate 250–400 lines for container lifecycle, image definitions and wiring, net 20–170 fewer before caller edits. Retain 24 behaviors; replace 12 with 5 container cases, total 29. | Podman VM services would still need native browser wiring. | Wrong labels or writable mounts can target another checkout. Image maintenance and VM networking remain. |
| C: keep identity      | Remove/add 0 ownership lines; retain all 36 cases and 1,621 suite lines. Add 0 lock lines.                                                                                                                       | Linux `/proc` only.                                        | Better stale-PID rejection, but still a non-atomic check before signaling; highest maintenance cost.     |

**Accepted A:** replace five launch protocols in
`dev-https-lifecycle.sh:78-104`. `platform.sh:35-40` supplies `setsid --fork`
with Python `os.setsid()` fallback.

HTTPS is optional on both laptop platforms. Linux keeps container browsers;
macOS uses native builds. Keep both hosted static shards and every proof.
Ownership estimates exclude native runner additions; no total saving is claimed.

## Session contract

Launch mock OAuth, mail capture, Go, Nuxt and Caddy under one long-lived
supervisor/session/group with shell job control off. Persist a versioned record:
canonical checkout, PID, PGID and fresh 128-bit random public launch token. Put
path/token in fixed supervisor argv; secrets stay in private environment files.
Record before launching; failure starts nothing. Exec services without
daemonizing. Retain/wait on all five direct children; never restart them.

`up`, `status`, reuse and proof admission require a validated live anchor, five
live children in its group/session, matching source/config hashes and no latched
exit. Before/after readiness probes, verify every listener belongs to its
service's live child tree and recorded group/session. Inspect liveness and
listeners every second thereafter; latch any loss or inspection failure and
abort active proofs. Unknown/foreign ownership fails even with HTTP 200.
Readiness cannot clear a latch; completion rechecks health. Sampling may miss
shorter listener substitutions. Partial startup uses the same cleanup.

`scripts/lib/platform.sh` owns these helpers beside `detach` and stat/hash:

| Helper                      | Contract                                                                                                                                                                          |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `platform_process_snapshot` | Normalize wide `ps` to PID, PPID, PGID, UID and command; Python `os.getsid(pid)` supplies numeric sessions on both OSes. Disappearance fails inspection.                          |
| `platform_group_members`    | BSD `ps -g PGID` may select a group; GNU uses a full snapshot filtered by numeric PGID. Cross-check membership.                                                                   |
| `platform_listener_pids`    | macOS uses `lsof -nP -iTCP:PORT -sTCP:LISTEN -Fpn`; Linux may use it or parsed `ss -ltnp`. Check every IPv4/IPv6 listener/PID. Ownerless `netstat` evidence cannot admit a stack. |
| `platform_signal_group`     | Validate the anchor before TERM/KILL; signal with Bash `kill -TERM -- -PGID` or `kill -KILL -- -PGID`. Reject zero, one, negative input and caller's own group.                   |

Preflight selects platform tools, not mandatory Linux `ss`/`setsid`. Missing
tools, hidden owners, malformed/truncated output or parse errors fail closed.
Anchor validation requires PID=PGID, current UID and the entire expected
supervisor command, canonical path and final token. Parse numeric columns only;
compare the command remainder without splitting spaces. Names/path substrings
are insufficient. Never read `/proc/*/{stat,cmdline,environ,exe}` here.

The supervisor traps TERM and anchors the group while children drain for up to
10 seconds. Revalidate, then KILL the remaining group including the supervisor.
Confirm absence within 5 seconds before deleting only the validated record.
Inspection/drain failure cannot report clean stop. Missing anchor with remaining
members preserves state for manual recovery; never infer ownership from names.
If the group is absent, remove only its validated record. Keep keys, logs, media
and DB; no automatic stale-state retry or name-based kill.

A live anchor prevents PGID reuse. All members exiting between validation and
signal leaves an unmeasured reuse race; tokens cannot stop it or same-UID
impersonation. Escaped children may survive; keep production renderer teardown.
No HTTPS lifecycle/shared lock: one operator controls one fixed-port stack.
Listener and HTTP/Compose exclusions reject occupied services, but ports are not
race-proof locks; concurrent launches are unsupported. Publish/delete state only
for its matching token; a failed launch cannot remove another's record. Same-UID
state tampering is not contained. Stop before switching checkouts.

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
runner integration. In `apps/server/cmd/server/print_runtime.go`, Darwin closes
the render queue; `printrender/renderer.go` requires nonroot Linux user
namespaces. Keep these guards. Skip whole mixed journeys, with no split in this
change. Paths below are relative to `deploy/dev-https-browser/`; lines identify
the reviewed source.

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
| `make web-e2e`, `make web-e2e-fast`                                                                                                  | Native screen and harness-PDF assertions under [display assertions](display-assertions.md).              | Same assertion contract, both hosted shards.                                |
| HTTPS lifecycle static cases                                                                                                         | Port portable session/listener cases; exercise them natively.                                            | Keep both static shards and completeness.                                   |
| `server-runtime-smoke`, `renderprocess` Linux `/proc` tests, cgroup budget probes, `vn-infra`                                        | Explicit Linux-only skip.                                                                                | Linux kernel/container checks; smoke runs hosted amd64.                     |

Display assertions owns removal of `web-e2e-update` and `Baselines`, product
asset generation and diagnostic images. Harness PDFs use native Chromium and do
not call the Linux server render queue. Only the explicit HTTPS trust/rendering
exclusions apply; Mac evidence cannot certify Linux fonts, rendering, cgroups or
production teardown. Keep `ci-local` separate from opt-in functional proofs;
hosted CI remains mandatory for release. Extend `macos-tooling` with portable
helper/runner contracts; real native browser acceptance needs a separately
authorized bounded Mac proof.

## Guard disposition and remaining cost

Keep one DB/stack, coordinated fixture writes and idle windows; stack exit never
stops the DB. Retain rollback, source/config/binary hashes, DB/media/Compose
collisions, pins, scans and drift checks. HTTP ownership needs portable identity
validation beyond PID=PGID. Production proof authorization/custody/locks stay.

Replace 12 identity cases with five groups: stale/foreign/tampered records;
drain/escalation; startup/record failure; missing anchor/recovery; failed or
truncated inspection. Adapt the other 24. Cleanup requires all five services and
group absent. Reject server death after `up` and foreign listeners in status,
reuse or proofs. Keep absent-state idempotence, cancellation/timeouts, foreign
PID/shared-DB survival, symlink/dirty-tree/install-failure tests. Update
inventory, selectors and both shards together; preserve path, secret, drift,
enrollment, staging, diagnostic and completeness assertions. Final suite size is
uncertain.

Diagnostics must admit `dev-https-session.sh` source locations and its rejection
functions, read the versioned session record instead of per-service `.launch`/
`.identity`, and use portable snapshots/listeners. Capture before rollback; emit
only bounded fixed fields and redacted logs, never argv/environment or keys. The
session job owns both diagnostic scripts and fixtures for absent, malformed and
rejected session state on macOS/Linux.

## Ordinary tooling and dependencies

Rename the wrapper/helper to `run-owned.sh` and `lib/run-owned.py`. Keep
timeout, TERM/KILL, child joining, identity and failure status; remove memory
discovery, RSS sampling/limits/reporting and nice. Ordinary commands need no
mandatory wrapper or lock. Keep the existing managed lock for pre-push, ci-local
and dependency preparation, through cancellation. Preserve MCP's lock and
isolated writing checkouts; remove numeric ceilings, coordinate fixtures, add no
locks.

Without RSS enforcement, runaway builds may exhaust RAM, swap, trigger OS kills
and lose unsaved work. The owner accepted this on 2026-11-01. Keep `GOGC=50` in
`scripts/ci.sh` and pre-push heap/concurrency hints after recorded OOMs.
Optional `run-owned.sh --timeout <seconds> -- <command>` bounds time, not
memory; managed deadlines stay 600 seconds for pre-push and 3600 for ci-local.
Add no memory-warning sampler.

Prepare checkout-local dependencies with `npm ci` and `npm --prefix apps/web ci`
under the managed lock. Reject linked dependencies, generated directories and
ancestors before install; refuse installs/builds in an active stack checkout.
Pre-push never installs, borrows, copies, links, repairs or deletes
dependencies. Keep REFUSED/exit 2 for missing tools, changed manifests or
selected-tool drift, naming CI coverage. No install receipt: selected tools
cannot certify the full graph. Local CI installs every npm root afresh after
link checks. Keep required Bash 4/GNU tools and Mac Linux-only skips.

## Production runtime coverage

Keep [runtime bounds](budgets.md), container/realtime/media/queue limits,
`apps/server/internal/renderprocess/` supervision, Linux `/proc`/cgroup evidence
and host job/deploy ordering. There is no committed runtime `GOMEMLIMIT`;
preserve configured values without reading secrets and media normalization's
temporary Go limit. Do not choose a heap number without measuring Chromium's
shared allowance. `cmd/render-budget/cgroup_linux.go` is not invoked by CI; its
fixtures/`--probe`, ordinary tests and opt-in benchmarks do not prove capped API
cold start.

Add `server-runtime-smoke` (`Server runtime smoke`) after `web` in
`.github/workflows/ci.yml`, `ubuntu-24.04`, needing `[preflight, server, web]`,
with the existing full/prose condition. Add it to `ci.needs`, expected keys and
the permitted PR-prose skip set (nine omissions). Complete keys: `preflight`,
`contracts`, `macos-tooling`, `vn-infra`, `server`, `server-integration`,
`server-resumeapi`, `web`, `web-e2e`, `dev-https-proofs`,
`server-runtime-smoke`. Update selection design/contracts together; missing
evidence, cancellation or unexplained skips fail.

Use same-run/commit `runtime-server` and `runtime-web` artifacts, retained one
day, with verified commit/SHA-256 manifests. Export `server`,
`render-browser-supervisor`, `migrate`, `db-setup` from `server` with
`CGO_ENABLED=0`, `-trimpath`, `-ldflags="-s -w"`, linux/amd64. Export complete
production `make web-build` `.output/` from `web`. No smoke builds/installs,
development server or startup mocks. Mount artifacts read-only in pinned
Playwright/Node bases from `deploy/{server,web}.Dockerfile`; use the PostgreSQL
pin in `deploy/compose.yml`, matching build digests and real supervisor/browser
paths. This tests binaries, not release images.

| Phase          | Deadline and required evidence                                                                                                                                                                                                                                                                                                                                                                                                                       |
| -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Entire job     | 10 minutes including checkout, downloads and cleanup.                                                                                                                                                                                                                                                                                                                                                                                                |
| Preparation    | 6 minutes from entry: fetch images/artifacts, private synthetic config, empty DB, setup/migrations and production Nuxt. DB 1 GiB, Nuxt 256 MiB, one-shot setup/migration 256 MiB; stop one-shots before Go. Isolated network, ephemeral volumes, local media; no user rows, production credentials, Caddy, browser suite, render request or load.                                                                                                    |
| Measurement    | Fresh Go/Chromium cgroup after preparation. Before exec verify cgroup v2, `memory.max=536870912`, `memory.swap.max=0`, and `cpu.max=max` (no CPU quota, matching the production server container: `--memory=512m` without a CPU cap on a 2 vCPU host; owner direction 2026-11-01). Within 120 seconds: `/readyz` 200 within 60 seconds, 30 successes at one-second intervals with one-second request timeouts, then TERM shutdown within 10 seconds. |
| Health         | Check owned containers/processes throughout; Nuxt `/` 200 and PostgreSQL `pg_isready` at preparation end and each sample. Capture counters before removal. Any OOM/oom_kill increase, early exit, failed sample, missing limits or forced shutdown fails.                                                                                                                                                                                            |
| Cleanup/upload | Always attempt cleanup within 30 seconds and upload within one minute; failures fail the job. Cancellation/hard timeout may prevent upload and never passes. Remove only this run's containers/network/volumes.                                                                                                                                                                                                                                      |

Artifact interface (primary decision, 2026-11-01):
`scripts/server-runtime-smoke.sh manifest DIR` prints a GNU `sha256sum` manifest
of every regular file under `DIR/server/` and `DIR/web/.output/`, sorted by
relative path, refusing links and special files.
`scripts/server-runtime-smoke.sh run --artifacts DIR --manifest FILE --server-base REF --web-base REF`
verifies that manifest byte for byte before use, refuses any base reference
without an `@sha256:` digest, and records the manifest's SHA-256 and the commit
in evidence. The `server` and `web` jobs create their halves of the manifest;
integration owns that wiring.

Upload `server-runtime-smoke/evidence.json` as `server-runtime-smoke`, retained
14 days, at most 16 KiB. Allow only commit/artifact hashes, fixed phase/results,
configured/observed limits, service peaks/OOM counters, sample counts, exit
codes, cleanup and elapsed times. Apply the allowlist to console output;
suppress raw logs, rows, bodies, URLs, environment and credentials. Preparation
failure still emits fixed evidence; missing evidence fails. No retries,
quarantine or raised limits to obtain green: diagnose, fix forward, prove
intermittent fixes twice. Only evidenced provider failure before execution
permits a same-commit rerun. This proves cold start/idle health, not throughput
or combined host headroom; keep
[production observation](vietnam-host-memory.md#memory-gate).

## Security, migration and release

Apply the native admission contract above. Reject linked state before writes and
keep local keys in mode-0600 files under private directories. Only the operator
controls starts and stops. No production credential enters HTTPS launch
arguments or fixtures; production proof custody, authorization, revocation and
recovery stay with existing tools. Session tokens grant no product access.
Ambiguous ownership fails without signaling.

Public visitors and cross-account users gain no new production route. Stolen
sessions/MCP tokens retain existing authorization, expiry and revocation checks.
Hostile fixture content reaches native browser processes, so sandboxing and
origin filters remain required. A network attacker must still fail TLS
validation, including wrong-root/host/expiry cases. Compromised production
containers, database writers and leaked backups retain existing key custody,
encryption and recovery boundaries. No production key enters this runner. Local
stack tooling trusts same-UID code; session metadata cannot defend against
impersonation. Keep loopback listeners and refusal of a nonlocal HTTPS database.
No shared data is deleted; native profile cleanup removes run secrets.

Migrate in an idle window: stop old stacks with old helpers, then reject old PID
state. Remove only confirmed dependency links, preserving targets. Never reset
databases, keys, media or generated files automatically. Older helper state has
no compatibility shim; no application schema, API or older-client change occurs.
Rollback requires drained stacks before reverting tooling. Losses are laptop RSS
enforcement/reporting, per-PID HTTPS birth evidence and guaranteed automatic
cleanup after anchor loss. Keep Mac tooling and production-supervisor CI tests.

## Owner choices and combined jobs

This is the only choice and path-ownership list for both proposals. The owner
accepted every choice on 2026-11-01, including session A, RSS/nice and
numeric-ceiling removal, managed locks, checkout-local installs, and runtime
smoke. No local run is authorized.

| Choice                              | Accepted decision and status                                                                                            | Reason or cost                                                                                                                                               |
| ----------------------------------- | ----------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Owner-account native trust          | **Accepted, 2026-11-01.** Apply the `ci-local` trusted-commit boundary to both native runners.                          | Keeps the existing Podman machine and shared database usable; admission and residual risk are defined above.                                                 |
| Profile CA and HTTPS engines        | **Accepted, 2026-11-01.** Attended disposable Chromium profile import; skip native HTTPS WebKit, retain Linux WebKit.   | Strict TLS without host trust changes; bootstrap is manual and must be proved. No SPKI/context bypass.                                                       |
| Server-rendering exclusions         | **Accepted, 2026-11-01.** Skip the exact whole HTTPS journeys listed above, with partial evidence and full CI coverage. | Loses their portable steps locally too; avoids splitting coupled journeys or changing the production render boundary. Display harness PDFs still run on Mac. |
| Display replacement and authorities | **Accepted, 2026-11-01.** Display assertions; ADRs 0019, 0020, 0024 amended; implementation pending.                    | Removes pixel gates while retaining layout, paint, PDF, security and release checks; subtle optical defects remain a manual-review limit.                    |
| Product images                      | **Accepted, 2026-11-01.** Keep 26 product PNGs and a separate explicit generator.                                       | Preserves Library/PDF-tab assets and URLs without test baselines.                                                                                            |
| Review artifacts                    | **Accepted, 2026-11-01.** Allow the eight optional sanitized display images for 14 days.                                | Gives manual evidence without a comparison gate; capture failure is non-blocking.                                                                            |
| Git history                         | **Accepted, 2026-11-01: keep history.** Delete test baselines only after proof; no purge.                               | Avoids rewriting tags, hashes and evidence; old objects retain their size.                                                                                   |

Execute the following jobs in order. Brace lists expand to exact paths; PNG
globs mean only the existing inventory in the display proposal. Each path has
one owner across the pair. Shared wiring belongs only to integration. A missing
path or dependency needs an explicit scope addendum before editing.

| Order and owner               | Exact owned paths                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | Completion                                                                                                                                                                                                                                                                                                                                               |
| ----------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. Authority/documentation    | `docs/adr/{0019-application-ui-toolkit,0020-application-visual-identity,0024-delivery-gates}.md`; `AGENTS.md`; `CLAUDE.md`; `apps/web/AGENTS.md`; `deploy/dev-https-browser/AGENTS.md`; `apps/web/app/components/resume/AGENTS.md`; `instructions/{resources,workflow,testing,verification,releases,web,gotchas}.md`; `.claude/agents/manager.md`; `.codex/agents/{implementer,qa,explorer,troubleshooter,code_reviewer,design_reviewer}.toml`; `docs/design/{README,web,deployment,public-page-theme,link-previews,budgets,ci-path-selection,local-tooling-simplification,display-assertions}.md`; `docs/design/templates/{contract,colors,print}.md`; `docs/design/ui/library-filters.md`; `docs/runbooks/{native-development,local-uat,privacy,production,totp-keys}.md`; `docs/architecture.md`; `docs/architecture/rendering-and-editor.md`; `docs/plans/backlog.md`; `apps/web/test/import/linkedin/fixtures/README.md` | Record approved ADR History first; activate instructions with implementation. Primary applies agent-file edits. Preserve exact-commit release CI.                                                                                                                                                                                                        |
| 2. Session/diagnostics        | `scripts/{dev-https,dev-https-test,dev-https-test-diagnostics,ci-browser-proofs}.sh`; `scripts/lib/dev-https-{identity,state,caddy,preflight,lifecycle,session}.sh`; `scripts/lib/platform.sh`; `scripts/test/{ci-browser-proofs,platform-helpers,dev-https-diagnostics}-test.sh`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | Portable session/listener/signal helpers; both static shards; diagnostic source/record migration; foreign-process/shared-DB survival. Hand caller contracts to native HTTPS and integration.                                                                                                                                                             |
| 3. Dependencies/cleanup/HTTP  | Old/new `scripts/{run-capped,run-owned}.sh`; `scripts/lib/{run-capped,run-owned}.py`; `scripts/test/{run-capped,run-owned}-test.sh`; `scripts/test/{run-capped-lifecycle-test,test_run_owned_lifecycle}.py`; `scripts/{pre-push,ci-local,dev-native,totp-production-proof,deployment-document-check,ci}.sh`; `scripts/test/{pre-push,ci-local,totp-production-proof,deployment-document-check}-test.sh`; `scripts/test/dev-native-stop-test.py`                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | Preserve timeouts, managed locks, failure status and foreign targets; consume portable helpers. Remove RSS/nice and dependency borrowing.                                                                                                                                                                                                                |
| 4. Native HTTPS runner        | `deploy/dev-https-browser/{run-native.sh,run.sh,static-test.sh,playwright.config.ts,harness-paths.ts,harness-lib.ts,spec-sources.txt,verify-evidence.mjs}`; `deploy/dev-https-browser/{auth,password-auth,transport,entry,editor,public,publish,exports,privacy,sample-start,linkedin,linkedin-import,second-factor,totp,mcp,mcp-sdk,mcp-ts-sdk}.spec.ts`; `deploy/dev-https-browser/{editor-proof-state,second-factor-lib,second-factor-pages}.ts`; `scripts/{dev-https-check,mcp-owner-workflow,mcp-owner-workflow-test}.sh`; `scripts/lib/{dev-https-browser-stage,mcp-workflow-fixture}.sh`; `scripts/test/dev-https-native-browser-test.sh`                                                                                                                                                                                                                                                                              | Strict CA trust, admission/environment/staging, session admission, exact skips, separate native evidence and joined cleanup. No journey split. Preserve Linux completeness; test native production refusal with synthetic inputs.                                                                                                                        |
| 5. Display/assets             | `apps/web/e2e/{support,display-assertions,pdf-display}.ts`; `apps/web/e2e/{screenshot,chrome,gallery,gallery-baselines,showcase-baselines,verify,card,sign-in-gate,print,sample-pages}.spec.ts`; `apps/web/scripts/generate-template-pages.mts`; `apps/web/public/templates/pages/manifest.json`; `apps/web/public/templates/pages/*.png`; `apps/web/app/templates/pageImages.ts`; `apps/web/test/templates/pageImages.test.ts`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | Replace all 133 cells, including level geometry/paint; make showcase cards ephemeral; separate product generation. Keep old comparisons until acceptance, then remove helpers in these same files.                                                                                                                                                       |
| 6. Runtime smoke              | `scripts/server-runtime-smoke.sh`; `scripts/test/server-runtime-smoke-test.sh`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | Cover limits, artifacts, health, OOM/failure/deadlines, bounded evidence and owned cleanup with fixtures.                                                                                                                                                                                                                                                |
| 7. Shared integration/cutover | `Makefile`; `.github/workflows/{ci,template-pages,baselines}.yml`; `apps/web/e2e/playwright.config.ts`; `scripts/collect-browser-diagnostics.py`; `scripts/test/{makefile-safety-test.sh,workflow-contract.test.mjs,workflow-safety-test.sh}`; `scripts/{web-e2e-source.sh,generate-web-e2e-source-manifest.sh,web-e2e-source.manifest}`; `apps/web/e2e/baselines/*.png`; `apps/web/e2e/print-baselines/*.png`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | Wire wrapper/session/native install and HTTPS commands, native display execution, portable contracts, smoke artifacts/aggregate and product generator. Validate manifests, private builds, environment, cleanup and diagnostic bounds. After acceptance, delete test PNGs, Baselines and update mode. Preserve both shards and `web-e2e`/`web-e2e-fast`. |
| 8. QA evidence                | New `docs/plans/traceability/display-assertions.md` only; scratch reports outside Git                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | Record cells/mutations, font/browser hashes, geometry deltas, HTTPS trust/skip/cleanup and smoke results from primary/CI execution. Starts after integration wiring; finishes before cutover.                                                                                                                                                            |

The order is authority, leaf implementations, integration wiring, acceptance,
then cutover by the same path owners. Land each coherent feature with its
authority and wiring changes; do not release intermediate broken callers. Every
release requires affected operational tests and exact-commit green CI. No local
formatting, tests or native proof ran for this revision. Profile trust,
cross-platform assertions, cleanup and smoke timing remain runtime acceptance
work; first-party source support is not execution evidence.

[revisions]:
  https://raw.githubusercontent.com/microsoft/playwright/v1.64.0/packages/playwright-core/browsers.json
