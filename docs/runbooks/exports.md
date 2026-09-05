# Resume exports

Owner PDF and public PDF/image exports use the same Vue renderer and a bounded
Chromium queue. They run locally; hosted resource acceptance remains Phase 10.

## Runtime

Use `make dev-native` or `make dev-https`. Startup resolves the
repository-pinned Playwright Chromium executable. `ABOUTME_CHROMIUM_PATH` may
name that exact installed version. Go receives `CHROMIUM_PATH`; it rejects root
execution, an unsupported sandbox, or a different browser version. Browser
launches and version probes receive only `TZ=UTC`, `LANG=C.UTF-8`, and
`LC_ALL=C.UTF-8`. The render launcher clears the inherited environment before
executing Chromium, so server credentials do not enter the browser environment.

`PRINT_LISTEN_ADDR` is private redemption traffic only. Native development uses
`127.0.0.1:20082`, native HTTPS uses `127.0.0.1:20445`, and the Compose render
network uses `10.91.0.2:8081`. Nuxt receives the corresponding
`NUXT_PRINT_ORIGIN`. Production and staging permit only `127.0.0.1:8081` for the
shared task network. Caddy never routes this listener. The public `/print/<id>`
path accepts no ID-only, cookie, or browser-owner authority.

The queue allows one active render and eight queued calls. Its configured
20-second cancellation deadline starts at admission. Saturation returns 503 and
makes readiness unhealthy. Capabilities are one-use and expire within 60
seconds. Every terminal path joins render work before releasing its permit.

PDF creation and modification metadata use a fixed epoch for repeatable bytes.
PDF output is at most 16 MiB; the opaque 1200×630 PNG is at most 4 MiB. The
private payload and HTML limits are 3,407,872 and 6,291,456 bytes. Public
artifact cache entries expire after 60 seconds and share a 128-entry, 32 MiB
bound.

## Functional checks

Run affected checks under the repository's heavy-command limit:

```sh
make dev-https-exports-check
make dev-https-public-check
make p5a-native-http-check
```

The export proof checks save-before-download, owner authorization, PDF and PNG
headers, conditional requests, download/discovery flags, and revocation. It
retains bounded synthetic evidence under `.dev/native-https/evidence/`.

The real Chromium suite is opt-in:

```sh
ABOUTME_RUN_BROWSER_TEST=1 \
ABOUTME_CHROMIUM_PATH="$(node scripts/chromium-path.mjs)" \
  sh -c 'cd apps/server && go test -race -count=1 ./internal/printrender'
```

It checks actual PDF/PNG capture, font and image readiness, resource denial,
repeatability, and cancellation cleanup. A denied resource fails the render. The
browser proxy and request controls restrict normal browser traffic; they do not
claim kernel confinement after a Chromium sandbox escape.

## Resource benchmark

Use a Linux cgroup v2 host with the pinned Chromium and production Nuxt output.
Stop native HTTP and HTTPS before building or measuring; development startup
replaces generated Nuxt output even when its ports differ. Keep the database
container running. The benchmark never reads a database or `.env`.

Build outside the measurement cgroup:

```sh
(cd apps/web && NUXT_BUILD_TEST=1 npm run build)
(cd apps/server && go build -o ../../.dev/bin/render-budget ./cmd/render-budget)
```

Run Nuxt in a separate terminal:

```sh
HOST=127.0.0.1 PORT=20030 NUXT_PRINT_ORIGIN=http://127.0.0.1:20082 \
  node apps/web/.output/normal-test/server/index.mjs
```

Run the Go queue and Chromium together with no swap and half a CPU. Choose a new
output directory for every run; an existing nonempty directory is refused.

```sh
systemd-run --user --scope --collect \
  -p MemoryMax=512M -p MemorySwapMax=0 -p CPUQuota=50% \
  .dev/bin/render-budget \
  --repository-root "$PWD" \
  --chromium-executable "$(node scripts/chromium-path.mjs)" \
  --output-directory "$PWD/.dev/phase-7/resource-run"
```

The corpus contains minimal and full documents plus a valid 524,288-byte
document with 24 visible sections and 64 entries per section. Each format and
fixture runs two series of one cold call, two discarded warmups, and ten
measured calls. A separate wave admits nine maximum-document PDF calls together.
Deadline-shed tail calls remain explicit failures in evidence.

For a quick integration check, add `--probe`. It runs one cold call per fixture
and format. Its evidence declares `mode=probe`, and the command intentionally
exits nonzero with `measurement_incomplete`; it cannot satisfy this resource
gate.

`evidence.json` records raw timings, byte sizes, hashes, nearest-rank p95, and
cgroup limits, peak memory, and out-of-memory events. Deadline failures retain
their return times and teardown overshoot. Serial failures, unexpected queue
failures, successful calls above 20 seconds, an 8-second p95 miss, or an
out-of-memory event fail the command. A p95 series requires its full planned
count of successful samples. First valid PDF/PNG artifacts support local text,
font, page-size, and visual checks.

This is a local Go-plus-Chromium baseline. Nuxt runs in its own process outside
the measured cgroup. Phase 10 repeats it on the selected host and exercises the
full server workload before hosted activation.

### Local baseline, 2026-09-06

The final repeated run passed all 156 serial calls under the 512 MiB, no-swap,
half-CPU limits. Seven of nine queued calls completed; two reached the
configured 20-second deadline and joined cleanup within 17 ms. Those timeout
outcomes remain explicit in the evidence. Peak cgroup memory was 330,780,672
bytes (315.5 MiB), with no out-of-memory events. Each fixture/format pair had
identical bytes across 26 calls, matching the earlier inspected baseline.

| Fixture | PDF p95 | PNG p95 | PDF bytes | PNG bytes |
| ------- | ------- | ------- | --------- | --------- |
| Minimal | 1.911 s | 1.811 s | 14,426    | 7,227     |
| Full    | 1.963 s | 1.924 s | 183,975   | 72,080    |
| Maximum | 3.002 s | 2.389 s | 220,424   | 36,898    |

Each p95 is the larger of the two measured series. The full PDF has two Letter
pages, its normalized photo, and two columns. The maximum PDF has 128 A4 pages;
text extraction finds all 1,536 unique entry labels. Both PDFs use epoch dates.
Visual inspection confirms wrapped long text and the fixed share-image crop. Raw
evidence and artifacts remain under `.dev/phase-7/resource-20260906-4/`; the
runbook commands reproduce the protocol.

## Failure handling

A 503 can mean saturation, timeout, revocation in progress, unavailable
Chromium, or rejected print input. Inspect readiness and bounded server
diagnostics, then run the narrow queue or browser suite. Never log capabilities,
cookies, private snapshots, or browser request headers. Restart the owned native
process groups through their `-down` and startup targets after runtime sources
change.
