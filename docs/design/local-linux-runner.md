# Local Linux runner for native HTTPS contracts

Withdrawn by the owner's decision of 2026-11-01. Keep the dev-https static
contracts in Linux CI, or run them on a ThinkPad when needed; macOS skips them.
Do not implement this runner. See the
[tooling proposal](local-tooling-simplification.md).

Status: proposed. Extend the landed [`ci-local.sh`](../../scripts/ci-local.sh)
with Linux native HTTPS contracts on Darwin. [ADR 0024][delivery] permits the
owner's capped local landing gate on reviewed commits; hosted CI gates main
pushes, tags, and deploys. This proposal authorizes no execution.

## Suite and coverage

Run exactly `bash scripts/dev-https-test.sh --static`, including diagnostics and
the completeness guard. Keep the direct Linux Make recipe and hosted
`DEV_HTTPS_TEST_SHARD=0/2` and `1/2` index-modulo selections unchanged. Darwin
runs once, unsharded, with `DEV_HTTPS_TEST_SHARD` and `DEV_HTTPS_TEST_CASE`
absent. Reject inherited nonempty selectors. Accept no arbitrary command or
extra Podman flags.

The sole inventory is `CHECKS` in
[`dev-https-test.sh`](../../scripts/dev-https-test.sh), with 36 cases at design
time. Add fixed-version start and successful-end records containing case name,
index, shard, and status, including the diagnostics prelude. Emit the inventory
before execution and completion only after every selected call succeeds.
Preserve assertions, fixture tools, process groups, and failure exits. Compare
Darwin's completed ordered list with the inventory and the disjoint union of
successful Linux shard lists at the same commit, ordered by index. Reject
missing, duplicate, unknown, unordered, failed, or unterminated records. The
prelude runs once per invocation. Record commit, bundle hash, architecture,
kernel, tools, and image ID. PASS alone cannot prove selection or kernel parity.

## Image and tool custody

Reuse the multi-architecture Playwright Noble reference in
[`deploy/server.Dockerfile`](../../deploy/server.Dockerfile). Its index digest
is `sha256:` followed by
`06a9939e57531807f8d5fd76ce44b53165ffb7d7501d87ab10e285c20b1e971f`. Commit it in
`deploy/local-linux-runner/image.lock.json`; assert its Dockerfile mirror and
verify both child digests, including arm64 support.

Build a tools image without application sources or browser execution. Install
exact versions of `bash`, `python3`, `coreutils`, `findutils`, `grep`, `sed`,
`gawk`, `procps`, `iproute2`, `util-linux`, `make`, `tar`, and `ca-certificates`
from a dated signed Ubuntu snapshot. Record transitive packages per
architecture. Install the official Linux Node archive at the `.tool-versions`
pin, verified against committed per-architecture SHA-256 checksums. Expose tools
in `/usr/bin:/bin`, including `ss`, `setsid`, and `flock`. Do not substitute
BusyBox or musl binaries.

The lock owns snapshot date, packages, checksums, digests, and architecture.
Missing pins block preparation. Forbid mutable tags, runtime installs, and
unpinned upgrades. Use native arm64 on the Mac VM and amd64 in CI, no emulation.
Preparation runs `bash scripts/local-linux-runner-image.sh prepare` under the
shared lock and host watchdog for 1,260 seconds; its VM deadline is 1,200
seconds. Use the VM containment contract below for installs and all build
helpers. Failure publishes no manifest. A private cache outside the checkout
records recipe/lock hashes, package inventory, base identity, and immutable
image ID. Accept only matching prepared images with `--pull=never`. This local
build record uses no registry credential or signing key.

Capture actual Linux shard tool paths and versions before `actions/setup-node`.
Compare them with the image inventory using committed expected values and
reviewed differences in the lock. Unexpected drift fails; never rewrite pins
silently or equate ambient Node with `.tool-versions`. Keep Linux tool selection
unchanged. Follow monthly
[pin maintenance](../../instructions/verification.md#pin-maintenance). Image or
lock changes require native arm64 and amd64 checks at the candidate:
`local-linux-runner-check.yml`, `contents: read`, 20-minute job deadlines, no
publishing, signing, or secrets. Keep `ci.yml`'s job set and collect shard
evidence in `contracts`. Image evidence cannot authorize release.

## Source staging and writes

Resolve `HEAD^{commit}` with replacement objects disabled. Capture the closed
`scripts/local-linux-runner.sources` inventory bytes and ordered path, Git mode,
and blob hash entries, plus runner and image inputs. Read blobs from that commit
only, never worktree copies, archive attributes, or recursive traversal. Reject
staged or unstaged input changes; untracked files are never inputs. Before
success, require final HEAD to equal starting HEAD, the inventory bytes and
every captured entry to equal their starting values, and inputs to remain clean.
Mirror the browser recipe's before/after `HEAD^{commit}` equality checks in both
Linux-runner Make entry recipes. Print the captured commit; any mismatch
invalidates the result even when the final tree is clean.

Initial closure: `scripts/dev-https.sh`, `scripts/dev-https-test.sh`,
`scripts/dev-https-test-diagnostics.sh`,
`scripts/test/dev-https-diagnostics-test.sh`, every committed
`scripts/lib/dev-https-*.sh`, `scripts/generate-public-roots.mjs`,
`scripts/chromium-path.mjs`, `deploy/caddy/Caddyfile`,
`deploy/caddy/public-roots.generated.caddy`,
`packages/publicroots/public-roots.v11.json`, and
`deploy/dev-https-browser/spec-sources.txt`. Expand the library pattern to exact
inventory paths and test completeness when libraries change.

Allow only Git modes 100644 and 100755, preserved as 0644 and 0755. Reject
links, submodules, absolute/traversal paths, duplicates, control bytes, and case
collisions. Deny `.git`, `.env`, `.env.*`, `.dev`, credential directories, and
private keys, including inventory additions. Read no secrets. Limit staging to
32 MiB and 1,024 files in a private 0700 directory. Generate a fixed Dockerfile
from the prepared image ID; copy validated `source/` to `/src` with
`COPY --chown=0:0`, preserving regular file modes and using 0755 directories.
Never interpolate paths into Dockerfile or shell syntax. Build with
`--network=none --pull=never --layers=false` and an IID file, with no source
instructions. Verify source hash and resulting image ID. Stream the bounded
context to the owned VM job, without Mac bind mounts.

Source immutability comes from root ownership and the read-only root filesystem.
The suite's `umask 077` leaves fixture copies owner-writable. Its write map is:
`new_fixture`, standalone helper cases, and diagnostics use `mktemp -d` under
`TMPDIR=/tmp`; copied repositories, fake tools, audit files, outside sentinels,
browser staging, and diagnostics artifacts stay beneath those directories.
Fixture `.dev/native-https` holds generated binaries, configs, synthetic
secrets, Caddy data, logs, and process records. Caddyfile, generated fragment,
and binary drift writes target fixture copies. Stdout/stderr use bounded pipes;
`/dev/null` is the discard sink. No case needs to write `/src`. Acceptance must
verify this map for the full unchanged suite under nonroot UID and read-only
root, including Caddyfile append and diagnostics artifact cases.

## Isolation and VM limits

Before VM work, persist a 0600 intent receipt with random token, exact job and
object names, hashes, and phase on the host and VM. Persist IDs atomically;
label images and containers with the token. Use a private CID file. Run with
`--read-only --user=1000:1000 --cap-drop=ALL`,
`--security-opt=no-new-privileges --network=none --pid=private`,
`--ipc=private --uts=private`, and default seccomp. Disable healthchecks and
default writable tmpfs mounts. Forbid privileged mode, external mounts, sockets,
devices, host namespaces, secrets, and published ports. Inspect before start.
Only `/tmp` is writable storage: 512 MiB tmpfs,
`rw,exec,nosuid,nodev,mode=1777`. Executable fixtures require `exec`. Use an
empty environment except fixed PATH, `HOME=/tmp`, `TMPDIR=/tmp`, `LANG=C.UTF-8`,
`LC_ALL=C.UTF-8`, and `TZ=UTC`. Run from `/src`. Strip image/caller environment,
including BASH_ENV and diagnostic destinations; the suite supplies fixture
settings. Database and Podman calls remain stubs. Use
`--memory=1g --memory-swap=1g --cpus=2 --pids-limit=256`, `--log-driver=none`,
and a five-second stop grace. Bound output to 8 MiB; overflow fails and tears
down the job.

Every VM-side helper, including transfer/extraction, inspection, source-image
assembly, Podman/conmon processes, suite container, and teardown, belongs to a
verified owned cgroup. Start a transient delegated user service through
`podman machine ssh`, with `MemoryMax=1G`, `MemorySwapMax=0`, `CPUQuota=200%`,
and `TasksMax=256`. Run Podman locally inside that service; do not submit builds
to the shared remote API service. Place all child containers/scopes beneath its
cgroup and inspect effective memory, swap, CPU, PID limits and ancestry before
processing inputs. Refuse unsupported placement or enforcement. Bound the
aggregate, not only Dockerfile RUN instructions. Never constrain the shared
Podman service or DB. Tmpfs counts within this cap.

Use a VM-enforced 600-second run deadline covering staging, assembly, and
execution; source assembly gets at most 120 seconds of that budget. Set the
container `--timeout` to the remaining whole seconds; refuse if none remain. Use
control-group termination, TERM then KILL after five seconds. Cleanup gets 30
seconds under the same aggregate resource limits; never overlap independently
capped jobs. Preparation uses its deadline above. VM deadlines survive Mac
disconnect or KILL; no client heartbeat extends them. Persist unit/cgroup
identity before work and retain receipts until verified empty and owned objects
removed. Reject unavailable or wrong-architecture VMs. Require VM MemAvailable
of at least 1.5 GiB before work; this is a refusal threshold, not a reservation.
Never change machine settings or control the DB.

## Locking, cancellation, and integration

The landed `ci-local.sh` acquires the common Git directory's
`aboutme-local-check.lock` through `scripts/lib/local-check.py`. That Python
parent closes the descriptor in children; do not infer lock ownership from an
inherited descriptor. Keep it alive throughout cleanup. Standalone
`make operational-test-dev-https-linux` calls the public runner, which takes
this lock and uses `run-capped.sh --timeout 660`. For the gate,
`ci-local.sh --capped` explicitly calls `ci.sh --local-gate`; only that call
selects the private Make target `operational-test-dev-https-linux-locked`, whose
recipe calls the runner `--body` without reacquiring the lock. These internal
command arguments are trusted call structure, not an environment-variable bypass
or public API. Ignore inherited mode variables; ordinary `ci.sh` uses the public
target. The gate retains its 3,600-second watchdog; the private recipe wraps the
body in a 660-second watchdog without another lock.

Linux `ci.sh` retains `make operational-test`. Darwin runs fast, deploy,
browser, and isolated targets within `operational contracts`, preserving any
failure. Unknown systems, missing Podman, or missing image fail, never skip. The
host supervisor in `scripts/lib/local-linux-runner.py` owns nested Make and
runner children. Standalone mode supervises the body and cancels on parent Make
death. INT/TERM at `ci-local.sh`, `ci.sh`, Make, or the runner forwards through
the live supervisor to the exact VM job and container; allow five seconds before
KILL. Use explicit waits and traps, including the tee pipeline; a shell trap
that exits without waiting is insufficient.

The locked parent allocates private receipts outside the capped process tree.
Atomic results record token, commit, input hash, status, signal, watchdog code,
and cleanup state. Treat child signal death or exits 130/143 as cancellation
even if Make returns 2. `ci.sh` validates the receipt, prints
`--- operational contracts CANCELLED (Ns)`, stops later groups, and exits
130/143. Ordinary failure prints FAILED and ends the gate with 1. Extend
`ci-local.sh`'s summary parser for CANCELLED and preserve 130/143 in its outer
status mapping, with `CANCELLED: local CI (INT|TERM)`.

Change `ci-local.sh --locked` to wait for its watchdog rather than exec it. On
watchdog exits 123/124/126, reconcile the exact VM job under the still-held lock
using a fresh capped 30-second cleanup command. The supervisor records the inner
watchdog's code before Make normalizes it; `ci.sh` stops later groups. The outer
summary reports watchdog memory limit, timeout, or sampling failure with FAIL
and exit 1, overriding cancellation or success for either watchdog. Exit 125
remains refusal with exit 2. Reconcile after cancellation too; report incomplete
cleanup, retain receipts, and never convert it to a pass. The watchdog's
five-second escalation can kill host traps; VM deadlines are the backstop.

Remove verified containers, source images, staging, then CID files; never prune
or remove cached tools images. After a crash or lost connection, the next
invocation under the lock reconciles at most 16 exact-token objects and the
recorded VM unit, validating IDs, names, labels, and input hashes. Stop owned
cgroups before removal. Bound recovery to 30 seconds under the same VM limits;
ambiguous ownership or unprovable intermediates require manual recovery and
block new work. Missing or invalid terminal evidence fails.

## Security, costs, and compatibility

Hostile committed tests or altered images are the added attackers. Commit-only
allowlisting excludes host secrets; root ownership, nonroot UID, private
namespaces, dropped capabilities, and no socket isolate signals and control.
Network isolation denies DB and external access. VM resource limits contain
faulty image processing. Kernel/Podman compromise and a malicious host account
with Podman authority remain outside the boundary; run reviewed source only.
Public visitors, signed-in users, hostile resume content, stolen session/MCP
tokens, network-to-edge attackers, compromised production containers, database
writers, and leaked backups gain no endpoint, production access, data, or keys.
Synthetic tokens die with tmpfs. Revoke images by rejecting their lock; rebuild
reviewed pins to recover. Validation failure denies a pass.

Real Linux /proc, identity, race, rollback, and process-group coverage costs
preparation, VM disk, source layers, and arm64 timing differences. Measure and
cap the prepared image at 5 GiB. A separate VM duplicates management; Linux
laptop runs leave the Mac incomplete. No product data, schemas, migrations, or
clients change. Older tooling and rollback fail setup, never pass by omission.
The owner approves limits and integration before code starts.

## Verification and delivery

Contracts cover staging modes, dirty inputs, clean commit changes during a run,
inventory/mode/blob drift, selectors, mounts, environment, limits, and failures
at every lifecycle step. Prove nested Make completes while the outer lock stays
held and an independent contender is refused. Signal every layer; assert
130/143, CANCELLED summaries, no later groups, and watchdog failure precedence.
Kill the Mac client during assembly and execution; verify VM deadlines, cgroup
emptiness, exact-object recovery, foreign-object survival, and no DB operations.
Test receipt crash gaps and the complete write map above. Keep Linux recipes and
shard assertions; deliberately fail a case and prove the outer summary.

A separately authorized Mac proof uses the standalone target at the green Linux
commit under the lock, cap, and 660-second bound. Compare case/tool records,
inspect real cgroups/namespaces, and prove a disposable external PID sentinel
survives signals. Check DB ID/running state only, without data access. Prove
success, intentional failure, and interrupt cleanup in separate bounded
invocations. Stub tests cannot establish VM properties; execution is unproved.

Implement in order, serializing shared paths:

1. Image: `deploy/local-linux-runner/{Dockerfile,image.lock.json}`,
   `scripts/local-linux-runner-image.sh`, and
   `scripts/test/local-linux-runner-image-test.sh`.
2. Runner: `scripts/local-linux-runner.sh`, `scripts/lib/local-linux-runner.py`,
   `scripts/local-linux-runner.sources`, and
   `scripts/test/local-linux-runner-test.sh`.
3. Coverage: `scripts/dev-https-test.sh`,
   `scripts/local-linux-runner-evidence.py`,
   `scripts/test/local-linux-runner-evidence-test.sh`,
   `.github/workflows/{ci,local-linux-runner-check}.yml`, and
   `scripts/test/workflow-contract.test.mjs`.
4. Integration: `Makefile`, `scripts/ci.sh`, `scripts/ci-local.sh`,
   `scripts/test/ci-local-test.sh`,
   `scripts/test/{ci-failure-propagation,ci-lifecycle}-test.sh`,
   `docs/design/README.md`, and `docs/runbooks/native-development.md`. Own lock
   selection, cancellation, summaries, final HEAD checks, and wiring.

Confirm independent review before implementation. Deliver one tooling feature
after hosted checks and the Mac proof. [CI selection](ci-path-selection.md)
requires the full gate. No production deployment or release-image change.

## First-party references to verify during implementation

Network was forbidden; pins and platform semantics remain unverified:

- [Podman run](https://docs.podman.io/en/v5.0.0/markdown/podman-run.1.html)
- [Podman build](https://docs.podman.io/en/v5.0.0/markdown/podman-build.1.html)
- systemd [resource controls][resources] and [service deadlines][service]
- [GNU copy modes][copy-modes]
- [Ubuntu snapshots](https://snapshot.ubuntu.com/)
- [Node checksums](https://nodejs.org/dist/)
- [Playwright images](https://playwright.dev/docs/docker)
- [Linux PID namespaces][pid-namespaces]

[delivery]: ../adr/0024-delivery-gates.md
[resources]:
  https://www.freedesktop.org/software/systemd/man/systemd.resource-control.html
[service]: https://www.freedesktop.org/software/systemd/man/systemd.service.html
[copy-modes]:
  https://www.gnu.org/software/coreutils/manual/html_node/cp-invocation.html
[pid-namespaces]: https://man7.org/linux/man-pages/man7/pid_namespaces.7.html
