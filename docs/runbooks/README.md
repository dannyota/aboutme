# Runbooks

Runbooks describe supported commands, checks, stop conditions, and cleanup.
**Supported** means the named operation has a documented command path. **Open
host procedure** means the host operation lacks a reviewed procedure and must
not run. **Reference** means an undeployed path, not live operations. A runbook
can support setup while leaving key rotation open.

| Runbook                                               | State                               | Purpose                                                                    |
| ----------------------------------------------------- | ----------------------------------- | -------------------------------------------------------------------------- |
| [Native development](native-development.md)           | Supported                           | Start, inspect, and stop the daily local stack                             |
| [Local HTTPS checks](local-uat.md)                    | Supported                           | Verify native features before the production deploy                        |
| [Resume exports](exports.md)                          | Supported                           | Check PDF/image rendering, public gates, and resource limits               |
| [Realtime](realtime.md)                               | Supported                           | Check stream bounds, recovery, refresh, and revocation                     |
| [Authentication email](email.md)                      | Supported                           | Operate Bizfly SMTP and the Google Workspace mailbox                       |
| [CrowdSec privacy notice](crowdsec-privacy-notice.md) | Supported                           | First adoption, initial reconciliation, notification, and activation       |
| [Production](production.md)                           | Supported                           | Deploy, rollback, access, edge settings, and secrets                       |
| [DNS](dns.md)                                         | Supported                           | Cloudflare DNS-only records and DNSSEC                                     |
| [CloudFront](cloudfront.md)                           | Reference                           | Certificates and edge checks for the undeployed AWS path                   |
| [Deployment observer](observer.md)                    | Supported                           | Operate the Vietnam host reporter and observer                             |
| [AWS deployment observer](aws-observer.md)            | Reference                           | Setup, update, checks, and failures for the undeployed AWS observer        |
| [Privacy and media cleanup](privacy.md)               | Supported; AWS reference            | Four host lifecycle jobs; retained AWS snapshot sweep                      |
| [Account delete](account-delete.md)                   | Open host procedure                 | Required checks for operator deletion after an abuse report                |
| [Authenticator-app keys](totp-keys.md)                | Supported setup; open host rotation | Bootstrap and enrollment; rotation safeguards pending a reviewed procedure |
| [Sign in to view](sign-in-to-view.md)                 | Supported setup; open host rotation | Bootstrap and enablement; key rotation pending a reviewed procedure        |

The production runbook covers shipped scripts and current operator steps. Open
host procedures state required safeguards without authorizing an operation.
Planned behavior belongs in the [deployment design](../design/deployment.md).

Use [`../guides/`](../guides/README.md) for setup and explanatory workflows.
