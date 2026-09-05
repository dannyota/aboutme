# Task 10.10: Scheduled jobs, restore verification, TLS, and drift

AC-INF-006; drift-detector half of D6.

**Task gate:** One author writes the failing checks first and runs the affected
checks. The fresh Phase 10 review covers retention authority, IAM, database
restore isolation, and overlap handling.

**Files:** `deploy/aws/modules/jobs/**` (+ tests),
`deploy/aws/scripts/{restore-verify,cidr-drift-check,tls-expiry-check}.sh`,
script tests, and `docs/runbooks/restore-drill.md` seed.

## Schedule contract

Task 10.10 dispatches only after Phase 8 privacy has shipped and its exact
server CLI is available. The module pins these enabled schedules at activation:

| Job                   | Command                    | Cadence   | Lock and heartbeat                                                      |
| --------------------- | -------------------------- | --------- | ----------------------------------------------------------------------- |
| Idempotency expiry    | `idempotency-expiry-sweep` | hourly    | PostgreSQL advisory lock; hourly success/backlog/oldest-age metrics     |
| Media orphan cleanup  | `media-orphan-sweep`       | weekly    | PostgreSQL advisory lock; weekly success/backlog/delete-failure metrics |
| Session/audit privacy | `privacy-retention-sweep`  | daily     | PostgreSQL advisory lock; daily success/backlog metrics                 |
| Restore verification  | `restore-verify.sh`        | nightly   | tagged deterministic target; daily heartbeat                            |
| Origin TLS expiry     | `tls-expiry-check.sh`      | daily     | scheduler exclusion; expiry/failure metric                              |
| CloudFront CIDR drift | `cidr-drift-check.sh`      | every 6 h | scheduler exclusion; six-hour heartbeat                                 |

The first three use the server image and its task role. The other three use the
ops image and the job task role. Flexible windows are off and Scheduler retry
attempts are zero: a missed or failed run must alarm, not hide behind retries.
Local OpenTofu authoring may use services disabled, but Task 10.15 cannot close
until all six schedules are enabled and their task commands resolve in the
candidate images.

## Steps

- [ ] Failing mocked tests first: one schedule and task definition per table
      row; exact command, cadence, image digest, network settings, enabled
      state, zero retry, and heartbeat alarm. The Scheduler role may call
      `ecs:RunTask` only on those task-definition families and `iam:PassRole`
      only on their exact task and execution roles with
      `iam:PassedToService=ecs-tasks.amazonaws.com`.
- [ ] Pin the ops task role. It may describe the environment's RDS instances and
      automated snapshots; restore only from the environment source ARN to the
      deterministic verification identifier with required environment and run-ID
      request tags; delete only a target carrying those tags; read the
      CloudFront managed prefix list; and publish only the named AboutMe job
      metrics. It has no SSM read, wildcard mutation, media, production, or
      bootstrap permission. Mock negative tests compare exact actions,
      resources, conditions, trust principal, SourceAccount, and regional/
      account ECS SourceArn wildcard.
- [ ] `restore-verify.sh`: choose the latest available automated snapshot and
      restore `aboutme-<env>-restore-verify` into the private DB subnet group,
      compute-only DB security group, `publicly-accessible=false`, the declared
      restore class, deletion protection off, and no final snapshot on deletion.
      The restored engine/storage encryption remain those of the snapshot. Run
      the goose-head and newest-migration sanity queries with the injected
      read-only credential, emit the heartbeat, then delete and wait for
      deletion without printing credentials.
- [ ] The restore target is an overlap mutex, not a cleanup wildcard. Each run
      creates a random run ID and tags the target. If a target already exists
      and is younger than the four-hour job budget, the loser exits and never
      mutates or deletes it. Older or missing ownership tags fail closed, page,
      and require the runbook's manual adjudication. A trap deletes only when
      this invocation completed the create and the live ownership tag still
      matches its run ID. A two-process fake-AWS test proves the loser and
      stale- debris path cannot delete the winner.
- [ ] `cidr-drift-check.sh`: compare set equality between the live
      `com.amazonaws.global.cloudfront.origin-facing` entries and the injected
      OpenTofu baseline, emit heartbeat on equality, drift plus nonzero exit on
      mismatch. It never calls SSM.
- [ ] `tls-expiry-check.sh`: connect to `127.0.0.1:443` with SNI and hostname
      equal to the configured origin FQDN, five-second connect/read limits,
      validate the chain/hostname, calculate whole days remaining, and publish
      the expiry metric. It never opens origin ingress or probes the EIP.
- [ ] Every script supports `--plan`, is shellcheck-clean, and has deterministic
      fake-command tests for arguments, timeouts, exit paths, metrics, overlap,
      cleanup ownership, and secret-free stdout/stderr. Seed
      `docs/runbooks/restore-drill.md` with manual Phase 10 operational
      rehearsal timing and stale-target adjudication.

**Verification:** `tofu test`, `tofu validate`, shellcheck, script tests,
parity, and docs gates. Real restore timing remains Phase 10 operational
rehearsal AC-OPS-018; Task 14 proves the enabled schedules, task definitions,
roles, and heartbeat sources resolve.
