# Host fact probe

`probe.sh` checks, on the production host, the facts the deploy relies on. It
prints one `PASS` or `FAIL` line per fact and exits 1 if any fails. Read-only
facts are checked in place. Two networking facts start a throwaway container on
the `aboutme` network. Every such container is named `aboutme-probe-<x>`, runs
with `--rm`, and is removed by a trap on exit. The probe prints no secret.

## Run it

Run it as root after `host/install.sh` and before the first deploy:

```sh
ssh aboutme-admin@HOST sudo bash -s < deploy/vn/probe/probe.sh
```

The probe pulls one public image, `alpine:3.24`, pinned by digest, so the host
needs outbound access to Docker Hub. Fix every `FAIL` and run it again until it
ends with `probe: all facts PASS`.

## What it checks

The design is [Vietnam production](../../../docs/design/vietnam-production.md).

- Podman is rootful and the Quadlet generator exists. Every unit is a Quadlet
  file under rootful Podman (Host).
- `/srv/data` is labeled `aboutme-data` and each bind target is mounted.
  Secrets, PostgreSQL, CrowdSec, and Caddy state belong on the encrypted volume,
  not the root disk (Host, Secrets and host identity).
- The root disk has 8 GB free: three server images during a deploy plus Podman
  overhead (Host).
- `zram0` is active swap, since memory is sized for zram, not a swap file
  (Host).
- journald `SystemMaxUse` is 500M, which the root disk budget assumes (Host).
- Four kernel settings hold. Caddy binds 80 and 443 unprivileged, the print
  listener may bind before its address exists, and Chromium needs user
  namespaces (Host, trust boundaries).
- A user namespace works in a non-root container with `SYS_ADMIN`, which is what
  Chromium's sandbox needs. The probe runs `unshare -U -r true` as uid 1000 with
  the busybox applet already in the image.
- A bridge container reaches `10.89.10.1`, where Go binds the print listener for
  Nuxt (Host, trust boundaries).
- The host reaches a container port published on `127.0.0.1`, as Caddy and Go
  reach Nuxt on `127.0.0.1:3000` (Host).
- No listener sits on a non-loopback address except 22, 80, 443, and
  `10.89.10.1:8081`. This is the host-side view of the security group.
- PostgreSQL has no TCP listener and its socket exists, because
  `listen_addresses = ''` and `DATABASE_URL` uses `/run/postgresql`
  (PostgreSQL).
- Outbound HTTPS reaches vStorage HCM04 and `ghcr.io`, for pgBackRest, media,
  and image pulls.
- Outbound TCP 10092 reaches `hcm03-loghub01.vngcloud.vn`, where Filebeat pushes
  logs to vMonitor (Logs, metrics, and alarms).
- The CrowdSec local API answers on `127.0.0.1:8095` (Go owns 8080) and the host
  has no registered central API, so sharing is off (Edge).
- `fence.sh read` works, so the release fence file exists and parses (Release
  fence).

The listener check passes on a host that has not started the server yet, since a
missing listener is not an unexpected one. Check the GreenNode security group
rules by hand as well: the probe sees only the host side.

## When to rerun it

Rerun the probe after:

- an Ubuntu release upgrade, which can change the AppArmor user namespace
  default and the kernel
- a Podman upgrade, which can change Quadlet, netavark, and rootful defaults
- a host rebuild, including a replacement server from `deploy/vn/prod`

The monthly maintenance reboot does not need a rerun unless a check was edited.
