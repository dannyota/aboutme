# Host hardening: SSH, security group, and private values

Path abbreviations and `S:line` evidence citations follow
[Current state and evidence](host-hardening.md#current-state-and-evidence) in
the [host hardening](host-hardening.md) proposal, which owns status, rollout,
and owner questions.

**Access.** S:8-40 has key-only login, root login denied, PAM enabled, X11 and
user environment disabled, six authentication tries, a 120-second grace period,
no client-alive probes, forwarding enabled, and INFO logging.
`H/cloud-init.yaml.tftpl:33-52` owns the initial config; `H/install.sh:419` does
not yet install an SSH policy. Add a managed drop-in under
`E/ssh/sshd_config.d/` and explicit installer validation/reload. Keep
`AllowUsers aboutme-admin`, `PermitRootLogin no`, `PasswordAuthentication no`,
`KbdInteractiveAuthentication no`, `UsePAM yes`, `X11Forwarding no`, and
`PermitUserEnvironment no`. Propose `MaxAuthTries 3`, `LoginGraceTime 30`,
`ClientAliveInterval 300`, `ClientAliveCountMax 2`, and
`AllowAgentForwarding no`. Alive probes remove unresponsive connections, not
idle but responsive sessions. Agent users need `IdentitiesOnly=yes` to avoid
spending three tries on unrelated keys. Risk: operator lockout or interrupted
transport, not automatic cancellation of durable host operations. [Noble SSH]

**Forwarding.** Propose `AllowTcpForwarding local`, `PermitOpen none`,
`AllowStreamLocalForwarding no`, and `PermitTunnel no`. A privately rendered,
attended drill exception may replace `PermitOpen` with only the drill host and
SSH port, then restore `none`. `V/scripts/restore-drill.sh:503` uses `ssh -W`;
blanket forwarding denial breaks that recovery path. Validate exception removal
after success and failure. This limits convenient pivots by a stolen operator
key; an administrator with a shell and sudo can bypass it. The workflow's
compromised-container attacker benefits only if it steals operator authority.
Home: SSH drop-in and the drill coordinator, not a public destination list.
[OpenSSH]

**Cryptography.** S:26-35 offers modern AEAD/CTR ciphers and hybrid/curve/DH
exchange, plus SHA-1 and 64-bit MAC variants. Keep Noble's patched cipher, KEX,
and host-key defaults. Propose only `MACs -hmac-sha1*,umac-64*` as a
conservative reduction, not a claim these offers prove an exploitable host.
Retain `PubkeyAcceptedAlgorithms ssh-ed25519` from cloud-init. Never delete host
keys to narrow negotiation. Compare the installed Noble `sshd -T` and laptop
negotiation after updates; do not copy latest upstream default lists. Risk: old
operator clients may fail. Home: the same drop-in. [Noble SSH]

**Verification and lockout recovery.** Before any SSH change, prove attended
console access can restore a root-owned configuration, not merely display a
console log. Keep a current SSH session. Stage the candidate, run `sshd -t` and
`sshd -T -C user=aboutme-admin,host=<host>,addr=<operator-address>` privately,
and account for first-value-wins includes and Match blocks. Reload the active
SSH service only after success; socket activation needs its own listener
handling. Open a second pinned-host-key login and prove sudo, rsync, deployment
transport, rejected forwarding, and the scoped drill path before closing the
first. Syntax or login failure restores the saved config. [OpenSSH]

**Security group.** `V/prod/variables.tf:65-88` supplies public defaults;
`V/prod/main.tf:51-62` creates IPv4 SSH rules. The accepted design describes
broad SSH ingress; the snapshot does not prove the provider's effective SG.
Require nonempty explicit `ssh_allowlist` of reviewed operator IPv4 `/32`
addresses initially; reject unrestricted prefixes and missing values. Keep
public HTTP/HTTPS rules. This removes visitor scanning before sshd and reduces
key-attack exposure, but does not constrain a compromised local container. Risk:
travel or changing ISP addresses lock out the owner. [Provider SG]

Home: `V/prod/`, private `aboutme-infra/prod.tfvars`, and the production
runbook. Add and test the new narrow rules before removing broad rules. Read
back all attached groups, including unmanaged rules; verify login from an
allowed address and denial from an unallowed one. IPv6 floating-IP support and
effective IPv6 ingress remain unverified; do not assume IPv4 rules cover IPv6.
Use the console/API to add a narrow recovery address if access fails. Do not
change the SSH port in the same rollout. A later port change must admit the new
port first, verify both listeners, migrate clients, then remove the old one.

**Private-values migration.** Require `ssh_port` in private Tofu input and
`SSH_PORT` in the laptop environment; require explicit probe input on the host.
Validate integer range 1-65535 and matching private inputs; errors stop before
SSH, provider writes, or installation. Public examples use `<ssh-port>` and
`<operator-ipv4>/32`; tests use explicit synthetic ports and documentation
addresses. Avoid shell tracing and printing rendered private config or plans.
Port secrecy is not authentication; history retains earlier disclosures.

Literal/default search found these locations; values are intentionally omitted:

- `V/prod/variables.tf:68,76,82,83,86,87`; `V/prod/main.tf:52,135,136`;
  `V/prod/prod.tfvars.example:4,5`;
  `V/prod/tests/drill-network.tftest.hcl:64,74`.
- `V/README.md:68,73,74`; `V/scripts/ssh-lib.sh:3`; `V/probe/probe.sh:7,8,22`;
  `V/probe/README.md:14,15,46`.
- `V/tests/cutover-ssh-failure-test.sh:42`;
  `V/tests/deploy-failure-test.sh:168`; `V/tests/host-operations-test.sh:124`.
- `docs/design/vietnam-production.md:49,153`;
  `docs/design/restore-drill-networking.md:33`;
  `docs/runbooks/production.md:11,16`; `docs/runbooks/account-delete.md:21`;
  `docs/runbooks/observer.md:129`; `instructions/releases.md:16`.

Update consumers/fixtures, including `ssh-lib.sh` callers, together. Provision
private values before removing defaults; update laptop scripts before rollout.
Repeat the private search with path/line-only output; encrypt plans/state.

[Noble SSH]: https://manpages.ubuntu.com/manpages/noble/man5/sshd_config.5.html
[OpenSSH]: https://man.openbsd.org/sshd_config
[Provider SG]:
  https://github.com/vngcloud/terraform-provider-vngcloud/blob/v1.3.21/docs/resources/vserver_secgrouprule.md
