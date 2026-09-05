# Task 7.1.8: Render budget harness

Make the print pipeline's resource gate reproducible with synthetic documents.

## Authorities and ownership

Read AGENTS.md, `docs/design/budgets.md`, `docs/design/templates/print.md`, ADR
0023, ADR 0032, and the Phase 7 print/browser contracts. Acceptance: AC-PDF-001,
AC-PDF-004, AC-PDF-006.

One author owns `apps/server/cmd/render-budget/` only. Root owns Makefile,
scripts, generated files, manifests, runtime setup, and the measured gate. The
command is local test tooling. It never accesses a database, account, secret, or
external network. Do not modify the browser controller package.

## Command and fixtures

Create a small CLI with explicit required flags for repository root, pinned
Chromium executable, and an output directory below ignored `.dev/phase-7/`. It
uses fixed Nuxt `http://127.0.0.1:20030` and private redemption
`127.0.0.1:20082`; root starts Nuxt with that print origin. No caller-supplied
network destination is accepted. Refuse an existing nonempty output directory.
Root supplies the runtime cgroup; do not invoke containers or systemd.

Use the real render queue, print snapshot builder, redemption HTTP adapter, and
Chromium controller. A private server joins before exit. No mock renderer or
stub HTML is accepted as measured output. Build deterministic owner snapshots
from these three valid documents:

1. `packages/schema/fixtures/minimal.json`, preserving its empty draft content.
2. `packages/schema/fixtures/full.json`, replacing its photo with a
   deterministic normalized PNG from the existing media corpus if required.
3. A generated document at exactly 524,288 canonical bytes with 24 sections, 64
   distinct entries per section, and rich-text fields reaching 16,384 UTF-8
   bytes while the entire document remains valid. Use synthetic repeated text
   and deterministic UUIDs, meaningful custom section names, and layout entries
   for every section. Validate through `resume.ValidateForStore` and canonical
   encoding before any run. No hidden content may pad the measured render.

If those limits cannot coexist under the schema, demonstrate the conflict and
report it to root; do not silently reduce the corpus. Record fixture SHA-256,
canonical size, section count, entry count, largest rich-text bytes, page
format, font family, inline photo size, and tool/browser versions in JSON
evidence. Tests prove the fixture contract without needing Chromium.

## Measurement protocol

For each fixture and format (PDF and PNG), run one cold sample, two discarded
warmups, and ten measured samples. Repeat that full measured series twice in one
invocation. Every sample is one queue call with a fresh browser; record
monotonic admission-to-result duration, output size and digest, and whether a
failure occurred. Do not retry failures. Report p95 by nearest-rank over the
measured series, cold time separately, and every raw sample.

Measure a separate queued wave with nine calls admitted together after their
snapshots are prepared. Record all outcomes and queue-inclusive duration. The
configured 20-second cancellation deadline may shed tail jobs; record
cancellation explicitly instead of treating shedding as a successful render. Do
not expand queue/deadline limits.

Capture Linux cgroup v2 `memory.max`, `memory.swap.max`, `cpu.max`,
`memory.peak`, `memory.events`, and the process's cgroup path when available.
Require memory.max 536870912 and swap.max 0 in measured mode. The command must
fail if any serial sample fails, a successful call exceeds 20 seconds, a queued
call fails for a reason other than its deadline, an OOM occurs, or the cgroup is
wrong. Record return time and teardown overshoot for each deadline cancellation.
Record p95 against the 8-second objective; a series meets it only with the full
planned count of successful measured samples. Report an SLO failure explicitly.

The first controlled run on 2026-09-06 recorded seven queued deadline failures
returning 66–84 ms after the configured deadline, after joined teardown. This
corrects the original admission-to-return hard-bound criterion, which conflicted
with mandatory cleanup. The queue timer and capacity rules remain unchanged.

Save the first valid PDF and PNG per fixture under the output directory so root
can inspect page size, text, fonts, image geometry, and visual output. Repeated
samples need only metadata. No private authority, IDs from real accounts,
headers, resume payloads, browser stderr, or raw dependency errors enter logs.
Use fixed safe error codes. Evidence may contain synthetic fixture IDs/hashes.

## Checks and report

Write fixture/argument/evidence tests first. Run the narrow package race gate
under the shared heavy lock. Do not run the measured benchmark; root runs it in
the controlled cgroup with real Nuxt after inspecting your changes.

```sh
flock --close .dev/phase-7/heavy.lock sh -c \
  'cd apps/server && go test -race -count=1 ./cmd/render-budget'
```

Report changed paths, RED evidence, exact checks, CLI usage, and any unsatisfied
contract. No Git, manifests, generated files, secrets, containers, or full CI.
