# Task 10: Jobs module — retention interface, restore-verification, drift check

AC-INF-006; drift-detector half of D6.

**Files:** `deploy/aws/modules/jobs/**` (+ tests),
`deploy/aws/scripts/restore-verify.sh`,
`deploy/aws/scripts/cidr-drift-check.sh`, `docs/runbooks/restore-drill.md` seed.

**Steps:**

- [ ] Failing `terraform test` (mocked) first: an EventBridge Scheduler schedule
      per job (`retention` — disabled-by-default until P8-priv ships the
      subcommand; `restore-verify` — nightly; `tls-expiry-check` — daily;
      **`cidr-drift-check` — every 6 h**), each targeting `ecs:RunTask` on the
      right task definition with a flexible-window **off** (deterministic
      start), scheduler role scoped to exactly those task definitions, and
      `retry_policy` attempts = 0 (a failed drill must alarm, not silently
      retry).
- [ ] `restore-verify.sh` (ops image): restore latest automated snapshot to
      instance id `aboutme-<env>-restore-verify` (the deterministic id is the
      overlap mutex — the script **fails fast** if the instance already exists,
      and that failure alarms via the task-state rule, D20); wait; run a
      verification query (`SELECT count(*) FROM goose_db_version` + newest
      migration timestamp sanity); emit the heartbeat metric; tear down the
      instance **in a trap so teardown also runs on failure**; never print
      credentials. Shellcheck + `bash -n` + a dry-run mode (`--plan`) that
      prints intended AWS calls without executing — the dry-run is the
      CI-runnable failing-first check.
- [ ] `cidr-drift-check.sh` (ops image): fetch the live
      `com.amazonaws.global.cloudfront.origin-facing` prefix-list entries,
      compare (set equality) against the Terraform-written baseline parameter
      `/aboutme/<env>/edge/cloudfront-origin-cidrs` (Task 4); equal → emit
      heartbeat metric; different → emit drift metric + exit nonzero (→
      task-state alarm). This is the **real, alarmed control** behind D6 — a
      stale trusted-CIDR set degrades every viewer behind a new edge into one
      shared bucket, so drift must page, not wait for a runbook reader. Same
      `--plan` dry-run pattern for CI.
- [ ] Retention interface (for P8-priv, stated here so PI leaves a contract, not
      a drill): the schedule invokes the **server image** with command
      `["retention-sweep"]`; P8-priv implements that subcommand with its pg
      advisory lock. PI ships the schedule disabled + the task definition
      wiring; enabling it is a P8-priv one-variable change.
- [ ] Seed `docs/runbooks/restore-drill.md` (what the job does, how P9A runs the
      **real** timed restore drill manually — AC-OPS-018, evidence
      expectations).

**Verification:** `terraform test`, shellcheck, both scripts' `--plan` dry-run
output asserted by a grep-based script test, docs gates. A real snapshot restore
is **explicitly real-AWS and belongs to P9A (AC-OPS-018)**; PI's staging
bring-up (Task 14) only verifies the schedules exist and the task definitions
resolve.
