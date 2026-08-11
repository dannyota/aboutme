# Task 9: Observability module — alarms (with default thresholds), dashboards, SNS

AC-INF-005.

**Alarm inventory** (from spec §9 _Monitoring_ + P0 review additions; this list
is the review artifact). Thresholds are concrete staging defaults — each is a
variable; rows marked _(owner-set)_ have no defensible default and must be set
by the owner before production use:

| Alarm                                            | Source metric                                                         | Default threshold (staging)             |
| ------------------------------------------------ | --------------------------------------------------------------------- | --------------------------------------- |
| RDS free storage low                             | `RDS/FreeStorageSpace`                                                | < 20 % of allocated, 2 × 5 min          |
| RDS CPU high                                     | `RDS/CPUUtilization`                                                  | > 80 %, 3 × 5 min                       |
| RDS connections near `max_connections`           | `RDS/DatabaseConnections`                                             | > 80 (of ≥ 100), 2 × 5 min              |
| RDS snapshot/backup failure                      | RDS events → EventBridge rule                                         | any event                               |
| Restore-drill failure or **absence** (heartbeat) | Custom metric from Task 10's job (missing-data ⇒ ALARM — fail closed) | no heartbeat in 26 h                    |
| Retention-job failure                            | EventBridge ECS task-state rule (nonzero exit)                        | any nonzero exit                        |
| **Prefix-list drift (D6)**                       | Custom metric from Task 10's drift job (missing-data ⇒ ALARM)         | any drift, or no heartbeat in 13 h      |
| Server task readiness flapping / restart loop    | ECS service `RunningTaskCount` + deployment events                    | < 1 for 5 min, or > 3 restarts / 30 min |
| Render queue depth / OOM kills                   | Custom app metric (interface reserved — emitted from P7A)             | _(owner-set with P7A baselining)_       |
| EIP association failure                          | Custom metric from Task 2's user-data script                          | any failure event                       |
| TLS certificate expiry (origin cert)             | Custom metric from a scheduled ops check (Task 10)                    | < 21 days remaining                     |
| CloudFront 5xx rate                              | `CloudFront/5xxErrorRate` (us-east-1 metrics)                         | > 1 %, 2 × 5 min                        |
| ECS deployment circuit-breaker rollback          | ECS deployment state EventBridge rule                                 | any rollback                            |

**Steps:**

- [ ] Failing `terraform test` (mocked) first: every row above materializes as
      an `aws_cloudwatch_metric_alarm` (or EventBridge rule → SNS) wired to the
      single SNS topic; heartbeat-style alarms treat missing data as
      `breaching`; the SNS topic has exactly the `var.oncall_email`
      subscription; dashboard resources exist for API/ECS/RDS/CloudFront;
      threshold inputs default to the table values.
- [ ] Implement; thresholds are variables (staging defaults deliberately tight —
      staging is the rehearsal instrument P9A uses for AC-OPS-019, "alarm fires
      and is received").
- [ ] Document each alarm's **deliberate trigger method** in a table inside the
      module README — this is the interface P9A's AC-OPS-019 drill consumes.

**Verification:** `terraform test`, `validate`, parity. Live
alarm-fires-and-received proof is **P9A's AC-OPS-019**, not PI's.
