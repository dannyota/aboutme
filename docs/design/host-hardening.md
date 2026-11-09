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

Access and confinement proposals have their own pages:

- [Access](host-hardening-access.md): SSH, security group, and private values.
- [Confinement](host-hardening-confinement.md): container confinement and
  Chromium, host services and timers.

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
   Use the two-login sequence and access recovery rules in
   [access](host-hardening-access.md).
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

[OpenSSH]: https://man.openbsd.org/sshd_config
[Yama]: https://docs.kernel.org/admin-guide/LSM/Yama.html
[Kernel]: https://docs.kernel.org/admin-guide/sysctl/kernel.html
[BPF]: https://docs.kernel.org/admin-guide/sysctl/net.html
[Filesystem sysctls]: https://docs.kernel.org/admin-guide/sysctl/fs.html
[IP sysctls]: https://docs.kernel.org/networking/ip-sysctl.html
[Quadlet]: https://docs.podman.io/en/v4.9.3/markdown/podman-systemd.unit.5.html
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
