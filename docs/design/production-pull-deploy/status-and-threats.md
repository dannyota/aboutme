# Production pull deployment status and threats

See the [design overview](README.md), [protocol](protocol.md), and
[evidence](evidence.md).

## Status, custody, and threats

Serve `/.well-known/deploy-status.json` through serving and maintenance Caddy
with `Cache-Control: no-store`, mounting only a generated allowlisted public
file:

```text
Status = {
  schema:1, observed_at:T, expected_state:H, expectation:Expectation,
  operation:Binding?, desired_tag:Tag?, phase:Phase?, outcome:Outcome?,
  cleanup:"pending"|"running"|"done"|"unknown"?, workflow_run_id:ID?, config_change:UUID?
}
```

Limit status to 4 KiB. Null operation fields mean no delivery operation;
`config_change` separately identifies a configuration barrier. Historical
attended deployment below `F` may remove this route; use SSH status and keep
polling paused until a supported release returns. Status reports host claims,
not independent attestation.

Retain at most 100 completed journals for at most 180 days. Never expire
unresolved state or epoch/generation records. Disable Ansible diffs and verbose
tracing. Logs and alerts contain fixed outcomes and public release identifiers,
never raw errors, credentials, IPs, or environment values.

| Attacker or failure                                                        | Boundary and limit                                                                                                                                   |
| -------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| Compromised maintainer account                                             | Can approve eligible requests, but cannot enroll source keys or silently substitute approved bytes; either maintainer is deliberately trusted        |
| Compromised preparation Action                                             | Independent inspection and explicit tag/hash approval prevent logs from defining approval                                                            |
| Compromised build Action                                                   | Provenance proves origin, not harmlessness; trusted malicious builds remain a supply-chain risk                                                      |
| Fork or same-repository PR                                                 | Wrong ref/event, missing approval, and independent source trust prevent admission; no production secrets or host runner                              |
| Visitors, signed-in attackers, hostile resumes, stolen sessions/MCP tokens | No deployment API; application content never becomes inventory, commands, or control state                                                           |
| Compromised container or database writer                                   | No deployment-state, administrative-key, or Podman-socket mount; database writes confer no deployment authority                                      |
| Network attacker                                                           | TLS, pinned SSH, source signatures, hashes, and authenticated proofs resist substitution; blocking traffic can deny deployment                       |
| GitHub outage or takeover                                                  | Incomplete evidence blocks admission; admitted execution uses verified cached inputs; takeover without source trust cannot introduce executable code |
| Replay                                                                     | Epoch, consumed generation, revision-bound expected state, expiry, run/attempt, and attendance reject reuse                                          |
| Leaked backup or disk                                                      | Database backups contain no deployment authority; readable full-host snapshots expose secrets and require revocation and fresh enrollment            |
| Compromised laptop or host root                                            | Administrative authority can defeat these controls; use independent console recovery and rotate affected credentials                                 |

“No CI network path” means no administrative or private route; public HTTPS
remains reachable. Restrict SSH at provider and guest firewalls to proved
owner-controlled VPN egress, preserve remote laptop and console recovery, and
give CI no VPN membership.

Keep vMonitor renewal manual with existing write limits and uncertainty/hold
rules. Disable credentialed renewal automation, terminate its runs, reconcile
outstanding holds, and revoke its GitHub credential before strict activation. Do
not relocate renewal to the host. Preserve vMonitor and the public GitHub uptime
backstop.
