# Runbooks

Runbooks contain exact commands, checks, stop conditions, and cleanup for
operations the repository supports now.

| Runbook                                     | State                  | Purpose                                                  |
| ------------------------------------------- | ---------------------- | -------------------------------------------------------- |
| [Native development](native-development.md) | Runnable               | Start, inspect, and stop the daily local stack           |
| [Local UAT](local-uat.md)                   | Blocked for acceptance | Smoke-check Compose and state the missing HTTPS/443 gate |

Cloud deploy, rollback, restore, EIP recovery, and secret rotation procedures
will be written when their infrastructure exists. Planned behavior belongs in
the [deployment design](../design/deployment.md) and
[infrastructure plan](../plans/phase-pi/README.md), not in speculative runbooks.

Use [`../guides/`](../guides/README.md) for setup and explanatory workflows.
