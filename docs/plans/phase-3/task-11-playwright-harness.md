# Task 11: Playwright harness — visual regression, offline fonts, browser corpus + CSP

Satisfies **AC-SEC-001**'s real-browser + CSP legs, AC-REN-003's offline proof,
and the master plan's "Visual regression" row.

**Tier:** High for the candidate-source boundary; normal for the isolated
browser assertions. The source tar becomes executable build input inside a
network-capable container, so the high-risk author/reviewer split below is
mandatory.

**Files:** create
`apps/web/e2e/{playwright.config.ts,screenshot.spec.ts,fonts-offline.spec.ts,corpus.spec.ts,normal-csp.spec.ts,print.spec.ts,baselines/*.png,print-baselines/*.png}`,
`apps/web/app/pages/_harness/{render.vue,print-fixtures.ts}` (D17 gating in
`nuxt.config.ts`), `apps/web/app/utils/csp.ts`,
`apps/web/test/harness-absent.test.ts`. Also modify `nuxt.config.ts` to assign
distinct harness and normal build/output directories, and reuse
`apps/web/app/pages/_harness/photo-fixture.ts` from Task 9. The integration
owner alone generates and applies both baseline directories. `@playwright/test`
is already installed by Task 0 (B8); this task does not touch `package.json`.

**Requested from the integration owner (Makefile + CI are owner-owned; recipe
template to resolve before handover):** `scripts/web-e2e-source.manifest` and
`scripts/web-e2e-source.sh` are integration-owner-only shared files.

<!-- markdownlint-disable MD010 -->

```make
WEB_E2E_COMMIT := $(shell git rev-parse --verify 'HEAD^{commit}')
WEB_E2E_RESULT_ROOT := .dev/web-e2e-results/$(WEB_E2E_COMMIT)/$(WEB_E2E_RUN_ID)
WEB_E2E_SOURCE_TAR := .dev/web-e2e-source/$(WEB_E2E_COMMIT)/$(WEB_E2E_RUN_ID).tar

web-e2e: ## Renderer visual regression + browser corpus (pinned container, AMD64 baseline)
		test -z "$${UPDATE_GOLDEN+x}" && test -z "$${PLAYWRIGHT_UPDATE_SNAPSHOTS+x}"
		test -z "$$(git status --porcelain=v1 --untracked-files=all)"
		test "$$(git rev-parse --verify 'HEAD^{commit}')" = "$(WEB_E2E_COMMIT)"
		case "$(WEB_E2E_RUN_ID)" in ''|.|..|*[!A-Za-z0-9_-]*) exit 64;; esac
		test ! -e $(WEB_E2E_RESULT_ROOT)/compare && test ! -e $(WEB_E2E_SOURCE_TAR)
		install -d -m 0700 $(WEB_E2E_RESULT_ROOT)/compare $(dir $(WEB_E2E_SOURCE_TAR))
		scripts/web-e2e-source.sh $(WEB_E2E_COMMIT) scripts/web-e2e-source.manifest $(WEB_E2E_SOURCE_TAR)
		test -z "$$(git status --porcelain=v1 --untracked-files=all)"
		podman run --rm --platform linux/amd64 --network=host \
	   --security-opt label=disable -v $(PWD)/$(WEB_E2E_SOURCE_TAR):/candidate.tar:ro \
	   -v $(PWD)/$(WEB_E2E_RESULT_ROOT)/compare:/results:rw \
	   -e TZ=UTC -e LANG=en_US.UTF-8 -e LC_ALL=en_US.UTF-8 \
	   -e PLAYWRIGHT_RESULTS_DIR=/results \
   -w /tmp/aboutme/apps/web \
   mcr.microsoft.com/playwright:<pinned-version>@sha256:<digest> \
   sh -eu -c 'mkdir -p /tmp/aboutme; tar -xf /candidate.tar -C /tmp/aboutme; \
     test ! -e /tmp/aboutme/.git && test ! -e /tmp/aboutme/.env && \
     test ! -e /tmp/aboutme/.dev && test ! -e /tmp/aboutme/.superpowers; \
     npm ci --ignore-scripts; PLAYWRIGHT_SURFACE=harness npx --no-install playwright test \
       --config e2e/playwright.config.ts screenshot.spec.ts fonts-offline.spec.ts corpus.spec.ts print.spec.ts; \
     PLAYWRIGHT_SURFACE=normal npx --no-install playwright test \
       --config e2e/playwright.config.ts normal-csp.spec.ts'

web-e2e-update: ## Regenerate committed screenshot baselines (same container)
 # identical, plus --update-snapshots
```

<!-- markdownlint-enable MD010 -->

plus a CI `web-e2e` job running
`WEB_E2E_RUN_ID="ci-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}" make web-e2e` after
`web`, with the job definition itself asserting `UPDATE_GOLDEN` and
`--update-snapshots`/`PLAYWRIGHT_UPDATE_SNAPSHOTS` are unset before the step
runs (B6 — a stray CI env var left set would silently rewrite baselines instead
of comparing against them). The `<pinned version>@sha256:<digest>` is resolved
at implementation time from the pinned `@playwright/test` version, is an **AMD64
image digest** (this project's locally runnable baseline platform — D16/B6), and
is recorded in `playwright.config.ts`'s header comment; local runs use the
identical `--platform linux/amd64` container. P9A repeats the named set with the
production ARM64 image as a separate launch gate.

- [ ] **Step 0: Resolve the browser image.** Match the pinned `@playwright/test`
      version, resolve and verify its AMD64 image digest and Chrome executable,
      then replace every placeholder above. Record `uname -m` on the host and
      inside the container and require `x86_64` in both; a platform flag without
      a native architecture is a failure. Inside the image, require
      `en_US.UTF-8` in `locale -a` and prove `TZ`, `LANG`, and `LC_ALL` have the
      exact values passed by the recipe; an unavailable locale is a blocking
      image finding, not permission to fall back. Expand `web-e2e-update` into a
      complete recipe identical to `web-e2e` except for the explicit
      `--update-snapshots` flag and its distinct `<commit>/<run-id>/update`
      mount. Hand the integration owner both complete recipes; no placeholder or
      abbreviated command may remain. Each recipe mounts only the reviewed tar
      read-only plus its dedicated ignored result directory, installs only
      lockfile-resolved packages in the disposable copy, and forbids `npx`
      downloads. `playwright.config.ts` requires `PLAYWRIGHT_RESULTS_DIR`; the
      result contract below keeps every artifact and report outside repository
      source. The update recipe writes generated baselines only to its distinct
      ignored output mount; the integration owner reviews and applies them to
      the reserved snapshot path. Each recipe fails if its source tar or mode
      directory already exists. It never deletes or reuses a prior run. Both
      recipes reject either update environment variable when it is present,
      including when it is set to an empty string; neither passes those
      variables into the container. `--security-opt label=disable` is deliberate
      on the SELinux-enforcing host and avoids relabelling the shared workspace;
      no `:z` or `:Z` mount may change repository labels.

Every run requires a caller-supplied `WEB_E2E_RUN_ID` in the closed
`[A-Za-z0-9_-]+` grammar. Its immutable result root is
`.dev/web-e2e-results/<commit>/<run-id>/<compare|update>`; the recipe records
the commit, source-manifest SHA-256, and tar SHA-256 there before starting the
container. `playwright.config.ts` places `outputDir`, HTML and JSON reporter
files, traces, screenshots, diffs, and PDF rasters below
`PLAYWRIGHT_RESULTS_DIR/<surface>/`; it configures no default
`playwright-report`, `test-results`, or blob-report path. A post-run assertion
fails if such output appears under `/tmp/aboutme`. In update mode, the recipe
copies exactly the seven screenshot candidates and every expected print page to
`/results/candidate-baselines/`, writes a sorted digest manifest, and fails on a
missing or extra file. No candidate may remain only under `/tmp/aboutme`.

## Candidate source boundary — high risk

`scripts/web-e2e-source.manifest` is the reviewed input boundary. It contains
one canonical repo-relative regular-file path per line, sorted bytewise, with no
blank, duplicate, directory, or glob entry. Entries are limited to the root
`package.json` and `package-lock.json`, `apps/web/**`, and `packages/schema/**`.
The source script accepts an exact commit, that manifest, and one new output
path below `.dev/`. It verifies the commit exists, equals `HEAD`, and that the
worktree and index are clean before and after archive creation. Every entry must
be a regular blob in that commit. Archive bytes come from those commit blobs,
never from the index, worktree, an ignored file, or an untracked file. It reads
no unlisted repository path. It rejects absolute paths, `..`, symlinks, special
files, and closed secret-like names even when Git tracks them. The
case-insensitive denylist covers any `.env` variant; `.git`, `.dev`,
`.superpowers`, `node_modules`, `.nuxt`, `.output`, `dist`, `coverage`,
`test-results`, or `playwright-report` components; `credentials*` or `secrets*`
basenames; `id_rsa*` or `id_ed25519*`; and `.key` or `.pem` suffixes. The script
then creates a deterministic tar with epoch timestamps and numeric owner and
group zero.

The security gate has three separate roles:

1. `web-e2e-source.test.sh` is written from this contract, before the source
   script exists. In isolated temporary Git repositories, the test proves listed
   valid tracked files enter the tar with bytes from the requested commit. A
   listed ordinary untracked path must fail, not enter the archive. Separate
   tracked and untracked controls exercise every secret-like class and must make
   the script fail without leaving an archive. A dirty tracked file, dirty
   index, ignored path, traversal, duplicate, symlink, special file, invalid
   commit, non-HEAD commit, or commit that changes during the run is also a
   negative control. Record the expected failure from
   `bash scripts/web-e2e-source.test.sh` before implementation.
2. The integration-owner implementer writes only the explicit manifest and
   source script, cannot weaken the test, and runs
   `bash scripts/web-e2e-source.test.sh` to pass.
3. A fresh reviewer that authored neither side reviews the manifest path by path
   and the complete boundary diff, then independently reruns
   `bash scripts/web-e2e-source.test.sh`. A finding returns to the implementer;
   the same fresh reviewer rechecks the fix.

The owner creates the tar in one serialized window before either container
invocation. A manifest change invalidates prior review and requires this gate
again.

`playwright.config.ts` (D16/B6) defines exactly one `webServer`, selected by the
closed `PLAYWRIGHT_SURFACE=harness|normal` value, never `nuxt dev`. The Make
recipe runs the two surface invocations sequentially; Playwright never starts
both servers together. The harness invocation builds with `NUXT_HARNESS=1` into
its own output directory and serves on `127.0.0.1:20090`. The normal invocation
builds with the flag absent into a different output directory and serves on
`127.0.0.1:20092`; port 20091 is reserved by P2B's retained test-S3 service.
`nuxt.config.ts` owns the exact distinct build and Nitro output paths. Neither
server may reuse another build's output. The container environment fixes
`TZ=UTC`, `LANG=en_US.UTF-8`, and `LC_ALL=en_US.UTF-8` for installation, build,
both servers, Playwright, PDF generation, and rasterization. The config fixes
`retries: 0`, `workers: 1`, `fullyParallel: false`, and
`webServer.reuseExistingServer: false`. It also defines a fixed `use:` context
(`timezoneId: 'UTC'` — the same value `print.md` §7 pins as `TZ=UTC` for the
print container, so the two rendering paths cannot disagree about which is
authoritative (D16); `locale: 'en-US'`, A4 viewport 794 × 1123 by default,
`deviceScaleFactor: 1`, `colorScheme: 'light'`, `reducedMotion: 'reduce'`); and
Chromium launch args `--force-color-profile=srgb`, `--font-render-hinting=none`,
`--disable-lcd-text`, `--disable-gpu`, and `--hide-scrollbars`. A global setup
step asserts `process.env.UPDATE_GOLDEN` and
`process.env.PLAYWRIGHT_UPDATE_SNAPSHOTS` are both unset and throws if either is
set, so a local `web-e2e` run gets the same protection as the requested CI job
even before that job exists (B6).

## Screenshot subset — 7 baselines

Pixel baselines are the scarce artifact: string goldens cover all 20 presets
(Task 9), screenshots cover a named six-preset subset plus the continuous-mode
case (`colors.md` §4.2). The subset is **named, not derived at run time**, and
each entry earns its place by a property no other entry covers:

| Preset              | Cell                    | Property it pins                                                                                                                 |
| ------------------- | ----------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `classic-serif`     | `vn-full` × paged       | one column, `placement: "keep"`, untinted, serif — the plain baseline                                                            |
| `engineer-compact`  | `vn-full` × paged       | two columns, `placement: "byType"`, untinted — a column regression cannot hide behind a fill                                     |
| `modern-sidebar`    | `vn-full` × paged       | two columns with `surfaceTarget: "sidebar"` — per-surface clamp, and a tint that must repaint across the flow (`print.md` §5)    |
| `executive-band`    | `vn-full` × paged       | `surfaceTarget: "header"` at `#16273d`, the darkest surface in the set — the clamp's direction flip to light text on a dark band |
| `consulting-formal` | `vn-full` × paged       | `pageFormat: "letter"` — the only page geometry A4 cannot show                                                                   |
| `academic-dense`    | `vn-full` × paged       | base 13 px, `lineHeight` 1.3, `entryGap` 6 — dense small type, where line-height and gap rounding shows first                    |
| `modern-sidebar`    | `full` × **continuous** | the continuous-mode case; the other six run paged                                                                                |

The other fourteen presets get no pixel baseline; Task 9's string goldens cover
them. P9's browser UAT exercises this same six-preset subset
(`../phase-9/README.md`), so the three coverage surfaces cannot drift apart —
changing the subset changes all three at once.

## Chromium print fragmentation — 2 cases

The harness-only `print-fixtures.ts` derives two schema-valid, two-column
documents from `vn-full`: `print-sidebar-overflow`, whose sidebar alone
overflows one page, and `print-main-overflow`, whose main flow alone overflows.
The opposite flow stays short. `render.vue` resolves these two IDs from that
module before its normal package/schema fixture lookup; a normal build contains
neither. Both use `modern-sidebar`, and their exact URLs set
`template=modern-sidebar&mode=continuous`; they never invoke the editor
paginator. `print.spec.ts` loads each through the build-only harness, waits for
fonts and images, asserts exactly one `[data-render-mode="continuous"]` root and
no paged-wrapper marker `[data-pagination-settled]`, calls `page.pdf()` with
`print.md`'s exact parameters, and uses the pinned `pdfjs-dist` plus
`@napi-rs/canvas` to raster every PDF page at 96 dpi. It asserts at least two
pages, exact page count, content/order sentinels on the expected pages, and
zero-tolerance equality with the integration-owner
`print-baselines/<case>-p<n>.png` files. A divergent Chromium result is kept in
the ignored output directory and blocks P3; it never updates the accepted
baseline. This tests print-media fragmentation, not the editor paginator. P7A
still owns the production `/print` route and worker.

The PDF call is fixed to `preferCSSPageSize: true`, `printBackground: true`,
`displayHeaderFooter: false`, `scale: 1`, and zero top/right/bottom/left job
margins so CSS `@page` wins. Each case runs alone with a 20-second hard timeout,
`TZ=UTC`, `LANG=en_US.UTF-8`, `LC_ALL=en_US.UTF-8`, device scale 1, reduced
motion, and the full print launch-argument set. Before printing, it awaits the
selected face, fallback, `document.fonts.ready`, and successful decode of every
image. Timeout kills the browser process group and fails without writing a
baseline. Raster generation is deterministic and remains inside the disposable
container; only the update target's ignored output mount can receive candidate
pages.

Harness page contract:
`/_harness/render?fixture=<id>&template=<id>&mode=<m>&font=<catalog-id>` renders
`ResumeDocument` from the named fixture (served from `packages/schema/fixtures`,
except for the two closed print-only IDs above). `font` is optional, validated
against the released catalog, and overrides only the in-memory harness document
so the offline-font probe can exercise every family without changing a preset.
The harness root emits the exact validated mode as `data-render-mode`; it never
infers that marker from rendered children. When the fixture has photo metadata,
the page sets `context.photoUrl` to Task 9's inline PNG only after verifying its
recorded SHA-256. It maps `full` to `context.lng: "en"` and `vn-full` to
`context.lng: "vi"`; `mode` comes from the validated query. Screenshot tests set
the viewport before the first navigation from the document's closed page-format
map: A4 is 794 × 1123 and Letter is 816 × 1056.

The page also has a client-only `?payload=<corpus-id>` mode. Its SSR response
contains only an inert mount point and no corpus payload or raw-markup
assignment. After mount, it resolves the closed corpus ID, calls
`sanitizeRichText`, assigns that result, and sets
`data-corpus-ready="sanitized"`. `?raw=1` uses a separate harness-only client
branch to assign the same payload without sanitizing and sets
`data-corpus-ready="raw"` for the CSP-backstop probe. It carries a visible
warning, but never serializes raw corpus markup into SSR. Both branches exist
only under the build flag. All `_harness` responses carry `HTML_CSP` via route
rules. `HTML_CSP` is byte-for-byte D5's fixed directive string; tests do not
reconstruct or reorder it.

- [ ] **Step 1: Failing gating test.** `harness-absent.test.ts`: a normal
      `nuxt build` output contains no `_harness` route; a `NUXT_HARNESS=1` build
      does. Implement the page + gating; pass.
- [ ] **Step 2: Offline fonts.** `fonts-offline.spec.ts`: route-block every
      non-`localhost` request and **fail the test if any is attempted**; load
      the full Vietnamese harness page once per catalog family; await
      `fontsReady(catalogId)` for the selected face and fallback; assert the
      manifest's declared loaded faces are ready and zero external requests were
      attempted — the self-hosted/offline proof (AC-REN-003).
- [ ] **Step 3: Screenshot baselines.** `screenshot.spec.ts` renders the seven
      cells of the named subset above. Before navigation it sets the exact A4 or
      Letter viewport derived from the fixture's `pageFormat`, asserts the
      rendered paper box uses the same dimensions, then waits for
      `fontsReady(catalogId)`, `data-pagination-settled="true"` in paged mode,
      and every requested image to complete and decode with `naturalWidth > 0`;
      any image error fails the cell. It then captures the full page and
      compares with **zero** tolerance against committed baselines. First run
      generates via the update target; committed; CI compares. Vietnamese
      diacritic fidelity is judged in baseline review (tofu/misplaced marks in a
      baseline = task failure, not a later discovery).
- [ ] **Step 3a: Print fragmentation goldens.** Run both cases above through
      actual `page.pdf()`, raster every page, and compare at zero tolerance.
      Prove sidebar-only and main-only overflow independently, repeated tint,
      correct page order, and no clipped or missing entry. Generate only through
      the owner update target and preserve any divergent output as evidence.
- [ ] **Step 4: Browser corpus + CSP conformance.** `corpus.spec.ts`, per corpus
      payload: first fetch the SSR response and prove it contains neither the
      payload bytes nor an active corpus node. Then: (a) sanitized page
      (`?payload=`) — wait for `data-corpus-ready="sanitized"`; collect
      `page.on('dialog')`, `pageerror`, console errors, and
      `securitypolicyviolation` events; assert **zero** of each; assert the
      author-side rules from `apps/web/test/sanitizer/neutralization.ts` via
      `page.evaluate` over the live DOM (the real-browser leg of AC-SEC-001);
      (b) raw backstop (`?raw=1`) — wait for `data-corpus-ready="raw"`; the CSP
      alone must still prevent script execution: zero dialogs/pageerrors even
      though client-assigned markup is unsanitized (violation events are
      _expected_ here and recorded, not asserted zero). Assert the response
      actually carried `HTML_CSP` byte-exact (a silently missing header must
      fail, not vacuously pass). (c) **CSP baseline compatibility on a normal
      build (B9).** In the later sequential `PLAYWRIGHT_SURFACE=normal`
      invocation, `normal-csp.spec.ts` navigates to the existing `/login` page
      and injects the `HTML_CSP` header via `page.route()` response fulfillment
      — real route-rule wiring for production pages is P5A/P8-sec's job (D5),
      this step only proves the exported **value** is compatible with genuinely
      Nuxt-emitted output on the separate port 20092, not just the harness's
      purpose-built markup. Collect `securitypolicyviolation` events across the
      page's full load and hydration and assert **zero** — the regression this
      catches is a future Nuxt-emitted resource (e.g. a payload script needing a
      nonce) that `script-src 'self'` would silently break. `csp.ts`'s doc
      comment states this constant is the renderer-surface baseline and names
      P5A/P8-sec as its production-enforcement owner.
- [ ] **Step 5: Gate.** Run `make web-lint web-typecheck web-test web-build`,
      then `WEB_E2E_RUN_ID="local-$(date -u +%Y%m%dT%H%M%SZ)" make web-e2e`.
      Record both outputs verbatim; never claim the browser gate ran without
      output.
