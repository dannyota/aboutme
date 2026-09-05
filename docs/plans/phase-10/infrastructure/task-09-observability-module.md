# Task 10.9: Observability module — alarms (with default thresholds), dashboards, SNS

AC-INF-005.

**Alarm inventory** (from the
[monitoring design](../../../design/operations.md#monitoring) plus P0 review
additions; this list is the review artifact). Thresholds are concrete staging
defaults — each is a variable; rows marked _(owner-set)_ have no defensible
default and must be set by the owner before production use:

| Alarm                                              | Source metric                                                                                                         | Default threshold (staging)                        |
| -------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------- |
| RDS free storage low                               | `AWS/RDS FreeStorageSpace`, Minimum, bytes, DB instance dimension                                                     | < `allocated_storage_gib × 2^30 × 0.20`, 2 × 5 min |
| RDS CPU high                                       | `RDS/CPUUtilization`                                                                                                  | > 80 %, 3 × 5 min                                  |
| RDS connections near `max_connections`             | `RDS/DatabaseConnections`                                                                                             | > 80 (of ≥ 100), 2 × 5 min                         |
| RDS snapshot/backup failure                        | RDS events → EventBridge rule                                                                                         | any event                                          |
| Restore-drill failure or **absence** (heartbeat)   | Custom metric from Task 10.10's job (missing-data ⇒ ALARM — fail closed)                                              | no heartbeat in 26 h                               |
| Idempotency/media/privacy sweep failure or absence | ECS task-state failure plus one custom heartbeat per Task 10.10 schedule                                              | any nonzero exit or one missed cadence             |
| **Prefix-list drift (D6)**                         | Custom metric from Task 10.10's drift job (missing-data ⇒ ALARM)                                                      | any drift, or no heartbeat in 13 h                 |
| Server task readiness flapping / restart loop      | Container Insights `RunningTaskCount`; EventBridge STOPPED events → dedicated log metric `ECSServiceTaskStoppedCount` | < 1 for 5 min, or Sum > 3 in 30 min                |
| Render queue depth / OOM kills                     | Custom app metric (interface reserved — emitted from Phase 7.1)                                                       | _(owner-set with Phase 7.1 baselining)_            |
| EIP association failure                            | Custom metric from Task 10.2's user-data script                                                                       | any failure event                                  |
| TLS certificate expiry (origin cert)               | Custom metric from a scheduled ops check (Task 10.10)                                                                 | < 21 days remaining                                |
| CloudFront 5xx rate                                | `CloudFront/5xxErrorRate` (us-east-1 metrics)                                                                         | > 1 %, 2 × 5 min                                   |
| ECS deployment circuit-breaker rollback            | ECS deployment state EventBridge rule                                                                                 | any rollback                                       |

**Steps:**

- [ ] Failing `tofu test` (mocked) first: every row above materializes as an
      `aws_cloudwatch_metric_alarm` (or EventBridge rule → SNS) wired to the
      single SNS topic; heartbeat-style alarms treat missing data as
      `breaching`; the SNS topic has exactly the `var.oncall_email`
      subscription; dashboard resources exist for API/ECS/RDS/CloudFront;
      threshold inputs default to the table values. Tests pin namespace,
      dimensions, statistic, period/evaluation count, comparison, units, and
      missing-data treatment for every metric. RDS storage uses a byte threshold
      derived from the allocated GiB variable, never a percentage applied to a
      byte metric. The restart source is an EventBridge rule for STOPPED service
      tasks into a dedicated CloudWatch Logs group plus a metric filter; it is
      not described as a nonexistent standard ECS restart metric. Every
      authoritative retention schedule has its own heartbeat alarm with missing
      data breaching after its fixed cadence.
- [ ] Implement; thresholds are variables (staging defaults deliberately tight —
      staging is the rehearsal instrument Phase 10 operational rehearsal uses
      for AC-OPS-019, "alarm fires and is received").
- [ ] Document each alarm's **deliberate trigger method** in a table inside the
      module README — this is the interface Phase 10 operational rehearsal's
      AC-OPS-019 drill consumes.

**Verification:** `tofu test`, `validate`, parity. Live alarm-fires-and-received
proof is **Phase 10 operational rehearsal's AC-OPS-019**, not Phase 10
infrastructure's.
