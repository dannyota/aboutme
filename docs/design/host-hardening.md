# Vietnam host hardening

## Summary

Status: proposed. Updates complete; narrow access, then confine services.
Chromium needs a separate proof. No host or provider change is authorized here.

[ADR 0026](../adr/0026-vietnam-hosted-production.md) owns the topology and
fence. Access/host policy changes need owner acceptance and an ADR History
entry. Keep the [memory budget](vietnam-host-memory.md),
[print authority](../adr/0011-print-capability-and-pdf-output.md), and
[production recovery rules](../runbooks/production.md) unchanged.

## Current state and evidence

Evidence: private `host-snapshot.txt`, 2026-11-08 (`S:line`), and public bundle.
`V/` = `deploy/vn/`; `H/` = `V/host/`; `E/` = `H/etc/`; `Q/` = `H/quadlet/`.
Repository lines cite the audited revision, not live state.

S:2,3,276 reports Ubuntu 24.04.5, kernel 6.8, Podman 4.9.3; S:277 reports
seccomp enabled and Podman AppArmor disabled; S:331 lists the AppArmor LSM.
Confirm loaded profiles, systemd version, BPF JIT, mount flags, and SG. Missing
`aa-status` (S:92) and S:300 scores prove neither disablement nor exploits.

The owner confirmed the SSH prerequisite on 2026-11-08: manual Ubuntu security
updates, OpenSSH `1:9.6p1-3ubuntu13.20`, and reboot onto kernel `6.8.0-149`. The
provider-image `apt-mark hold` on `openssh-client`, `openssh-server`, and
`openssh-sftp-server` was released; the repository did not set it.

The [workflow attacker list](../../instructions/workflow.md#reviews) applies:

- **Visitor / signed-in user / stolen session or MCP token:** reach Caddy and
  application authorization. Host controls limit damage after exploitation; they
  do not replace account, media, or public-revocation checks.
- **Hostile resume content:** reaches Chromium through authorized rendering. Its
  sandbox, blocked proxy, and one-use capability remain mandatory.
- **Network position between visitor and edge:** TLS and Caddy address handling
  remain the boundary. SSH policy separately protects the operator connection.
- **Compromised container:** target of kernel, filesystem, namespace,
  capability, metadata, and service restrictions below. A server compromise
  still exposes that server's application keys and authorized database access.
- **Database writer without application keys / leaked backup:** HBA, separate
  roles, key custody, core suppression, and encrypted backup custody help limit
  escalation. They cannot undo stolen plaintext or a root/provider compromise.

No API, schema, or web/MCP client changes. Update operator clients first. No
deletion, reformat, restore, automatic key rotation, or history rewrite. Root,
the laptop, and the provider can bypass these controls.

## Area proposals

### SSH, security group, and private values

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

### Kernel and network

Home: `E/sysctl.d/90-aboutme.conf`, installed at `H/install.sh:565`. Bundled
hardening at lines 22-25 is limited. Fail required checks on absent knobs; probe
after boot and Podman network creation. Target: compromised containers unless
named otherwise.

- **Ptrace:** S:55 has `kernel.yama.ptrace_scope=1`; retain and pin 1. It blocks
  unrelated same-UID tracing while retaining parent-child debugging. Defer 2
  until Chromium crash handling and operator diagnostics are proved; 3 cannot be
  undone until reboot. Verify cross-process denial and rendering with the pinned
  server image. [Yama]
- **BPF:** S:56 has `kernel.unprivileged_bpf_disabled=2`; retain 2. Both 1 and 2
  deny unprivileged BPF, but 1 cannot be reverted until reboot. JIT state is
  absent: propose `net.core.bpf_jit_harden=2` and `net.core.bpf_jit_kallsyms=0`
  only if those knobs exist. Measure CPU and networking impact; keep privileged
  nftables/CrowdSec working. Do not disable all BPF or its JIT as a substitute.
  Verify unprivileged denial, runtime knobs, bans, and bridge traffic. [Kernel]
  [BPF]
- **kexec:** S:57 has `kernel.kexec_load_disabled=0`. Propose 1 only after
  confirming no crash-dump or reboot workflow needs new kexec loads. S:341
  includes a crashkernel parameter, which does not prove an active dump path. It
  limits kernel replacement after privileged container escape, not arbitrary
  root compromise. Owner approval required: rollback needs a reboot with the
  setting removed. Do not change `kernel.modules_disabled`. [Kernel]
- **Files:** S:59-65 already has hardlink/symlink protection 1, regular-file
  protection 2, `fs.suid_dumpable=0`, and restricted kernel pointers/logs. Pin
  these; raise `fs.protected_fifos` from 1 to 2 to cover group-writable sticky
  directories. Verify controlled FIFO/link refusal and backup/job temp use.
  Risk: shared temporary-file workflows fail; never work around this with
  broader directory permissions. [Filesystem sysctls]
- **Routes:** S:70-83 has inconsistent default IPv4 redirects, send redirects,
  and enabled IPv6 redirects. Set IPv4 `accept_redirects`, `secure_redirects`,
  `send_redirects`, and `accept_source_route` to 0 for `all`, `default`, and
  existing interfaces. Set IPv6 `accept_redirects=0` and
  `accept_source_route=-1` for the same scopes. A network-position attacker gets
  fewer ways to alter routing. Preserve forwarding, current reverse-path
  filtering, IPv6 RA, and the Caddy/print bind settings until topology-specific
  evidence supports a change. Verify new interfaces too: changing forwarding can
  reset IPv4 defaults. Test DNS, package egress, Nuxt print redemption, observer
  isolation, both Caddy modes, and backup uploads. [IP sysctls]

### Container confinement and Chromium

S:279-282 shows non-root images, writable roots except observer, and a server
SYS_ADMIN setting. `Q/aboutme-server.container.in:34-39` explains that the
capability selects default seccomp namespace allowances; the snapshot does not
establish effective process capabilities. `Q/aboutme-observer.container.in:17`
already has read-only root, dropped capabilities, NNP, and bounded tmpfs.

**Ordinary containers.** Propose `DropCapability=all`, `NoNewPrivileges=true`,
and `ReadOnly=true` first for web, Caddy, maintenance, migrate, db-setup, and
jobs. Home: their `Q/*.container.in` files and `H/job-run.sh:41`, which launches
jobs without Quadlet. Keep the Podman default seccomp policy. The read-only root
needs explicit writable mounts and bounded temporary storage discovered from the
pinned image, not unrestricted tmpfs. Caddy needs `/data`, its log mount, and
verified config-generation scratch; web and jobs need their actual temporary
paths. New tmpfs consumption counts inside existing memory ceilings; do not add
capacity without budget approval. Verify generated units, actual capability
sets, NNP, failed root writes, job completion, migration, Caddy ACME/storage
sharing, and maintenance overlap. This limits persistence and privilege gain
after a container exploit. [Quadlet]

**Server-only namespace exception.** `E/sysctl.d/90-aboutme.conf:7-11` globally
relaxes Ubuntu user namespaces; `V/probe/probe.sh:163-200` currently requires
that relaxation and only tests Alpine unshare. Replace it only after a pinned
Chromium proof. Target: AppArmor enforcing, global
`kernel.apparmor_restrict_unprivileged_userns=1`, namespace permission granted
to the named server profile only, and a server seccomp profile allowing only the
sandbox syscalls actually needed without SYS_ADMIN. Keep
`user.max_user_namespaces=16384` and unprivileged namespace support available
for Chromium. Home: new `E/apparmor.d/` and `E/containers/` profiles, server
Quadlet, installer ordering, and revised probe. [Ubuntu AppArmor]

Load profiles first; use `PodmanArgs` for Podman 4.9's
`--security-opt apparmor=<profile>` and `seccomp=<file>`. No sandbox bypass.
AppArmor profile inheritance/transition, seccomp syscall flags, NNP with
Chromium, and removal of SYS_ADMIN are **unverified on this image/kernel**.
Require successful sandboxed PDF/image renders, sandbox-status evidence,
negative namespace creation from web/ordinary host processes, actual process
labels/capabilities, and unchanged blocked egress. Do not infer confinement from
an empty `podman inspect` capability list. [Podman run]

Defer rootful `UserNS=auto`/UID remapping, distinct from Chromium namespaces.
Prove PostgreSQL peer identity, socket mounts, secret access, Caddy shared
UID/`SO_REUSEPORT`, and persistent ownership before proposing Quadlet maps and
ownership migration. Never recursively chown live data for a test. Failed proof
retains existing policy and records residual risk. [Podman run]

### Host services and timers

`E/systemd/system/aboutme-alert@.service:7`, `aboutme-backup@.service:9`,
`aboutme-watch.service:6`, and retention/vacuum/job/operation services mostly
omit sandboxing. Alert/backup have no address-family restrictions. The reporter
sets NNP and resource/core limits at
`aboutme-deployment-document.service:10-15`. Inspect effective units/drop-ins;
S:300-311 scores do not prove confinement.

Propose `NoNewPrivileges=yes`, `ProtectSystem=full`, `ProtectHome=yes`,
`PrivateTmp=yes`, `ProtectKernelTunables=yes`, `ProtectKernelModules=yes`,
`ProtectControlGroups=yes`, and `RestrictSUIDSGID=yes` for alert and backup
services first. Set empty `CapabilityBoundingSet=` and `AmbientCapabilities=` on
both; retain backup `User=postgres`. Removing CAP_SYS_ADMIN prevents a
compromised root helper undoing mount restrictions. Prove empty process bounds,
failed mount/remount attempts, secret/config reads, pgBackRest lock/spool
writes, WAL shipping, and alert delivery before activation. Private temp/home
controls can break staging or credential reads. Authorized writes remain
possible. Home: service drop-ins under `E/systemd/system/`. [Systemd exec]

On those two services propose `SystemCallArchitectures=native` and
`RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`. These preserve local sockets
and DNS/HTTPS/SMTP while reducing a compromised helper's socket attack surface;
they do not restrict destinations or inherited sockets. Prove DNS, IPv4/IPv6
transport, journal, PG socket, backup, and alert paths plus AF_PACKET denial. If
required resolver behavior fails, stop and revise the family list from evidence.
Rollback restores the previous reviewed drop-ins and reruns both services. Keep
launcher/bouncer exceptions below. [Systemd exec]

For watcher, retention, vacuum, operation, reporter, and Podman job launchers,
start with core limits and per-unit review. Keep fence, bundle, workload,
retention locks and systemd control available. Do not apply `PrivateUsers`,
`PrivateNetwork`, `ProtectControlGroups`, empty capability bounds, syscall
filters, or `MemoryDenyWriteExecute` globally: Podman needs mounts/namespaces,
the bouncer needs host nftables, helpers change UID, and Chromium/V8 uses JIT.
Defer stronger `ProtectSystem=strict` and exact `ReadWritePaths` until each
helper's closure is enumerated. Verify failure-triggered alerts, retained fence
ownership, privacy-gate shutdown, deploy recovery, and timer catch-up. A better
`systemd-analyze security` score alone does not pass acceptance. [Systemd exec]

### Database, logs, and cores

**PostgreSQL.** S:313-321 agrees with `E/postgresql/18/main/pg_hba.conf:4-7`:
peer for postgres and mapped CrowdSec, SCRAM for explicit app roles.
`conf.d/90-aboutme.conf:7-9` disables TCP. Retain these and the 0777 socket
mode: authentication is in HBA; restricting socket mode without shared UID/GID
design breaks containers and pgBackRest. Mount the socket directory read-only in
clients only after proving connection still works. Check `pg_hba_file_rules` for
errors, deny an unrelated local role, prove app/migrator/setup/CrowdSec and
backup access, and verify no TCP listener. Home: PostgreSQL config and client
mounts. This limits a database writer or compromised container's lateral access,
not the application role's legitimate authority. [PostgreSQL HBA] [PG sockets]

**Audit.** Propose `LogLevel VERBOSE` for SSH, preserving the narrow journal
feed at `V/edge/crowdsec/acquis.d/sshd.yaml:3-7`. Verify accepted-key and failed
login attribution and CrowdSec parsing/banning with synthetic events. S:32 is
INFO; S:227-241 does not prove effective sudo logging. Confirm sudo command
events reach the persistent journal without stdin, environment, or I/O capture.
Secret imports remain stdin-only. Preserve the journal's existing 30-day/2 GB
caps at `E/systemd/journald.conf.d/90-aboutme.conf:6-9`. A visitor attack or
stolen operator key becomes easier to investigate; root can still erase logs.
Home: SSH policy and a minimal `E/sudoers.d/` policy only if needed. [OpenSSH]
[CrowdSec journal] [Sudo]

Defer auditd: S:273-274 is not an installed-package inventory proving absence.
Broad exec/I/O audit risks capturing secrets, personal operation arguments, and
filling the data volume. A later proposal must specify narrow file-change/login
events, local encrypted storage, retention, overflow behavior, and memory/disk
budget; no audit log export or immutable lock before recovery proof. Verify
events and bounded overflow in isolation. Home: `E/audit/`, if approved. Keep
personal operation audit in its existing separate store. [Sudo] [ADR 0026]

**Cores.** S:257-259 reports `core`, shell limit 0, and no discovered dump unit;
that does not prove every service or container is covered. Set `LimitCORE=0` on
sensitive services and `Ulimit=core:0:0` on all Quadlets, with equivalent
job-run arguments. Set PAM hard/soft core 0 for interactive sessions; verify PAM
applies it. If systemd-coredump is present, use `Storage=none` and
`ProcessSizeMax=0`; check Apport/crash handlers separately. Retain
`fs.suid_dumpable=0`. Home: service/Quadlet files, `E/security/limits.d/`, and
`E/systemd/coredump.conf.d/`. Verify effective process limits and a synthetic
crash on a disposable guest. This prevents key/plaintext spill to disk or a
leaked backup, at the cost of crash diagnostics. [Core dumps] [Quadlet]

### Mounts, updates, time, and provider custody

**Mounts.** S:244-255 and `H/data-volume.sh:87,131` show permissive fstab
entries, not verified effective flags. Propose `nodev,nosuid` for the data
filesystem and each bind target. Defer `noexec` until executable mappings,
plugins, helpers, and restore paths are inventoried. No root remount. Keep
`/tmp` disk-backed initially; a new tmpfs competes with the 4 GiB budget. Trial
`nodev,nosuid,noexec` on `/tmp` only after apt, installer staging, Podman,
Chromium and pgBackRest proofs; use per-service private temp first. These impede
payload execution after container escape but interpreters can bypass `noexec`.
Replace owned fstab entries, never append duplicates or format data. Verify each
mount with `findmnt` and after reboot; bind flags need explicit handling.
Preserve missing-data-volume refusal. [Mount]

**Updates.** Retain daily Ubuntu security updates and manual reboots at
`E/apt/apt.conf.d/52aboutme-unattended-upgrades:3-15`. Keep reviewed third-party
pins at `H/install.sh:24-28,146-153`; do not silently unhold PostgreSQL,
pgBackRest, or CrowdSec. Propose weekly advisory review and expedited reviewed
security-pin updates when applicable. Home: installer, watcher alerts for
update/reboot backlog, and production runbook. Verify versions, update logs,
fresh SSH, PG backup, CrowdSec, and readiness. Updates limit known exploits;
unattended restarts can interrupt service. [Ubuntu updates]

Monthly attended reboots remain the baseline; urgent active security fixes may
need an earlier owner-approved window. Follow the existing provider API reboot
runbook, with console recovery and fence inspection. No blind reset or automatic
reboot while an operation is unresolved. Do not downgrade a database package or
restore old data to undo a hardening change. [ADR 0026]

**Time.** S:261-264 reports UTC and synchronization. Retain one time daemon; do
not add chrony beside timesyncd. Confirm the active implementation and approved
time sources privately. Preserve the sync dependency at
`E/systemd/system/aboutme-crowdsec-retention-startup.service:3-6` and existing
clock-failure gates. Add loss-of-sync metadata alerting in `H/bin/watch.sh`.
Verify boot sync and refusal paths without moving production time. A network
attacker corrupting time can affect TLS, TOTP, and retention; ordinary SNTP does
not authenticate time. Authenticated time needs a separate provider/source
decision. [Time sync]

**Cloud-init and control plane.** S:266-267 says cloud-init is done and enabled;
`H/cloud-init.yaml.tftpl:27-60` writes boot diagnostics, and
`V/prod/main.tf:89-94` ignores later user-data changes. Keep first-boot support;
stop publishing raw sshd/listener diagnostics to serial logs. Do not disable
cloud-init or delete its state until network/metadata/console recovery has a
reboot proof. Home: cloud-init template and installer; disabling is deferred
because boot stages can configure networking. [Cloud-init boot]

ADR 0026 and `V/README.md:50-64` place administrative API/state keys on the
laptop; `H/cloud-init.yaml.tftpl:8` gives the SSH admin passwordless sudo. Treat
the SSH key as root authority. Propose a separate operator SSH key from commit
signing, encrypted local custody, pinned host keys, no agent forwarding, and
console MFA with offline recovery if the account supports it. Add/test the new
key before removing the old authorized key; removal does not kill existing
sessions. Revoke sessions and provider API credentials after laptop compromise,
then rebuild/rotate affected runtime keys through existing recovery contracts.
Home: private `aboutme-infra`, laptop configuration, and access runbook.
[OpenSSH] [ADR 0026]

GreenNode console MFA enforcement/recovery, API scope granularity, and metadata
endpoint behavior are **unverified**; public provider docs could not be fetched.
Require private account evidence before implementation. Do not put API keys in
cloud-init. Propose denying metadata access from application containers after
confirming required guest services. Host-network containers need a tested host
OUTPUT/cgroup rule, bridge containers a FORWARD rule; a bridge-only rule misses
Go and Caddy. Home: `E/aboutme/` firewall policy and its service. Verify denial
using a controlled endpoint without reading metadata responses or secrets, plus
DHCP, DNS, time, backups, and reboot. Never infer an AWS-style metadata token
mechanism. A compromised container may still reach provider services with keys
it already holds; provider-root compromise remains outside this boundary.

## Ordered small PRs and host rollout

High-benefit changes, ranked by risk; one concern per PR, with each area's
proofs. Primary runs `make pre-push`; hosted VN checks/exact-main CI gate
releases. Install the reviewed bundle before Quadlets; preserve operations,
floors, secrets, and privacy gates. Missing proof stops only that control.

1. **Low host risk:** private-input migration and access recovery docs. Verify
   synthetic transport fixtures; coordinator rollback requires private inputs.
2. **Moderate lockout risk:** narrow SG, then a separate SSH policy PR/install.
   Use the two-login sequence and access recovery rules above.
3. **Moderate restart risk:** separate core and reversible-sysctl PRs. Prove
   reboot readiness; rollback restores reversible knobs/limits.
4. **Moderate application risk:** per-container read-only/capability controls,
   then alert/backup sandboxes in separate PRs. Rollback restores reviewed
   templates/drop-ins and recreates affected units under the fence.
5. **High compatibility risk:** prove server AppArmor/seccomp before narrowing
   namespaces. Rollback restores profiles and sysctl together to the reviewed
   policy, never disabling the sandbox.

Deferred: mount flags, kexec, metadata filtering, SSH key separation, auditd,
read-only PG sockets, time/update alerts, and cloud-init diagnostics. Each needs
a reviewed design with ownership, activation order, positive/negative proofs,
interruption recovery, and rollback. Rollback preserves data, fences, and
bundles; uncertain writes need attended recovery. Older v0 images have no
support promise.

## Alternatives and owner questions

Prefer staged controls to broad access or blanket profiles that risk rendering
and recovery; no benchmark compliance claim. Owner approval: narrow sources with
console recovery; scoped drill forwarding; SSH limits/MACs; key separation;
kexec after proof; staged server confinement; journald without auditd.

[Noble SSH]: https://manpages.ubuntu.com/manpages/noble/man5/sshd_config.5.html
[OpenSSH]: https://man.openbsd.org/sshd_config
[Provider SG]:
  https://github.com/vngcloud/terraform-provider-vngcloud/blob/v1.3.21/docs/resources/vserver_secgrouprule.md
[Yama]: https://docs.kernel.org/admin-guide/LSM/Yama.html
[Kernel]: https://docs.kernel.org/admin-guide/sysctl/kernel.html
[BPF]: https://docs.kernel.org/admin-guide/sysctl/net.html
[Filesystem sysctls]: https://docs.kernel.org/admin-guide/sysctl/fs.html
[IP sysctls]: https://docs.kernel.org/networking/ip-sysctl.html
[Quadlet]: https://docs.podman.io/en/v4.9.3/markdown/podman-systemd.unit.5.html
[Podman run]: https://docs.podman.io/en/v4.9.3/markdown/podman-run.1.html
[Ubuntu AppArmor]:
  https://documentation.ubuntu.com/security/security-features/privilege-restriction/apparmor/
[Systemd exec]:
  https://manpages.ubuntu.com/manpages/noble/man5/systemd.exec.5.html
[PostgreSQL HBA]: https://www.postgresql.org/docs/18/auth-pg-hba-conf.html
[PG sockets]: https://www.postgresql.org/docs/18/runtime-config-connection.html
[CrowdSec journal]:
  https://docs.crowdsec.net/docs/log_processor/data_sources/journald/
[Sudo]: https://manpages.ubuntu.com/manpages/noble/man5/sudoers.5.html
[Core dumps]:
  https://manpages.ubuntu.com/manpages/noble/man5/coredump.conf.5.html
[Mount]: https://manpages.ubuntu.com/manpages/noble/man8/mount.8.html
[Ubuntu updates]:
  https://ubuntu.com/server/docs/how-to/software/automatic-updates/
[Time sync]:
  https://manpages.ubuntu.com/manpages/noble/man8/systemd-timesyncd.service.8.html
[Cloud-init boot]: https://docs.cloud-init.io/en/latest/explanation/boot.html
[ADR 0026]: ../adr/0026-vietnam-hosted-production.md
