# Runbooks

Runbooks contain exact commands, checks, stop conditions, and cleanup for
operations the repository supports now.

| Runbook                                     | State       | Purpose                                                      |
| ------------------------------------------- | ----------- | ------------------------------------------------------------ |
| [Native development](native-development.md) | Runnable    | Start, inspect, and stop the daily local stack               |
| [Local HTTPS checks](local-uat.md)          | Runnable    | Verify native features before the production deploy          |
| [Resume exports](exports.md)                | Runnable    | Check PDF/image rendering, public gates, and resource limits |
| [Realtime](realtime.md)                     | Runnable    | Check stream bounds, recovery, refresh, and revocation       |
| [Authentication email](email.md)            | Production  | Verify and operate Google Workspace and AWS SES              |
| [Production](production.md)                 | Operational | Deploy, rollback, access, edge settings, and secrets         |

The production runbook covers only shipped scripts and current operator steps.
Planned behavior belongs in the [deployment design](../design/deployment.md),
not in speculative runbooks.

Use [`../guides/`](../guides/README.md) for setup and explanatory workflows.
