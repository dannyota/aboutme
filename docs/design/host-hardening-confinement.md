# Host hardening: confinement

Path abbreviations and `S:line` evidence citations follow
[Current state and evidence](host-hardening.md#current-state-and-evidence) in
the [host hardening](host-hardening.md) proposal, which owns status, rollout,
and owner questions.

## Container confinement and Chromium

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

## Host services and timers

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

[Quadlet]: https://docs.podman.io/en/v4.9.3/markdown/podman-systemd.unit.5.html
[Podman run]: https://docs.podman.io/en/v4.9.3/markdown/podman-run.1.html
[Ubuntu AppArmor]:
  https://documentation.ubuntu.com/security/security-features/privilege-restriction/apparmor/
[Systemd exec]:
  https://manpages.ubuntu.com/manpages/noble/man5/systemd.exec.5.html
