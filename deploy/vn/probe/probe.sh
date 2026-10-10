#!/usr/bin/env bash
# Host fact probe for the production host (docs/design/vietnam-production.md,
# "Host", "PostgreSQL", and "Migration", step 3; the networking facts follow
# docs/design/single-host-production.md, "Host and networking"). Run it as root
# on the host after install.sh and before the first deploy:
#
#   ssh aboutme-admin@HOST sudo bash -s < deploy/vn/probe/probe.sh
#
# It prints one PASS or FAIL line per fact and exits 1 if any fails. It changes
# nothing that lasts: every throwaway container is named aboutme-probe-<x> and
# runs with --rm, and a trap removes leftovers and the temporary listener.
# Nothing here reads or prints a secret.
set -euo pipefail

# A small public image, pinned by digest. Alpine's busybox provides the
# `unshare` applet used by the user namespace check, so nothing is installed
# inside the container.
IMAGE=docker.io/library/alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
PREFIX=aboutme-probe-
GATEWAY=10.89.10.1
# Free ports for the throwaway listeners; the server uses 8080, 8081 and 3000.
HOST_PORT=18081
PUB_PORT=18082

# The bind targets host/data-volume.sh mounts from /srv/data.
TARGETS=(
  /etc/aboutme/secrets
  /var/lib/containers/storage/secrets
  /var/lib/postgresql
  /etc/pgbackrest/conf.d
  /var/lib/crowdsec
  /etc/crowdsec
  /var/lib/aboutme-caddy
  /var/lib/aboutme
  /var/log/journal
)

failed=0
work=

cleanup() {
  local ids
  if [[ -n $work && -f $work/http.pid ]]; then
    kill "$(cat "$work/http.pid")" 2>/dev/null || true
  fi
  ids=$(podman ps -a --filter "name=$PREFIX" -q 2>/dev/null || true)
  if [[ -n $ids ]]; then
    # shellcheck disable=SC2086 # one ID per word
    podman rm -f $ids >/dev/null 2>&1 || true
  fi
  [[ -z $work ]] || rm -rf "$work"
}
trap cleanup EXIT

[[ $(id -u) -eq 0 ]] || {
  echo "probe: run as root" >&2
  exit 2
}
work=$(mktemp -d)

# check <fact> <function>: the function prints a short detail and returns 0 on
# success. Its output becomes the text after PASS or FAIL.
check() {
  local fact=$1 fn=$2 out rc=0
  out=$("$fn" 2>&1 </dev/null) || rc=$?
  out=${out//$'\n'/; }
  if ((rc == 0)); then
    printf 'PASS %s%s\n' "$fact" "${out:+: $out}"
  else
    printf 'FAIL %s%s\n' "$fact" "${out:+: $out}"
    failed=1
  fi
}

# ---- Podman and Quadlet --------------------------------------------------

c_podman() {
  local rootless
  command -v podman >/dev/null || {
    echo "podman is not installed"
    return 1
  }
  rootless=$(podman info --format '{{.Host.Security.Rootless}}')
  [[ $rootless == false ]] || {
    echo "podman runs rootless as root"
    return 1
  }
  echo "rootful, $(podman --version)"
}

c_quadlet() {
  local gen=/usr/lib/systemd/system-generators/podman-system-generator
  [[ -x $gen ]] || {
    echo "$gen is missing"
    return 1
  }
  echo "$gen"
}

# ---- Disks and memory ----------------------------------------------------

c_data_volume() {
  local src label t
  mountpoint -q /srv/data || {
    echo "/srv/data is not a mountpoint"
    return 1
  }
  src=$(findmnt -no SOURCE /srv/data)
  label=$(blkid -o value -s LABEL "$src" 2>/dev/null || true)
  [[ $label == aboutme-data ]] || {
    echo "/srv/data is $src with label '${label:-none}'"
    return 1
  }
  for t in "${TARGETS[@]}"; do
    mountpoint -q "$t" || {
      echo "$t is not a mountpoint"
      return 1
    }
  done
  echo "$src labeled $label, ${#TARGETS[@]} bind targets mounted"
}

c_root_free() {
  local avail
  avail=$(df -B1 --output=avail / | tail -n1 | tr -d ' ')
  if ((avail < 8 * 1024 * 1024 * 1024)); then
    echo "$((avail / 1048576)) MiB free on /, need 8 GB"
    return 1
  fi
  echo "$((avail / 1073741824)) GiB free on /"
}

c_zram() {
  local row
  row=$(swapon --noheadings --show=NAME,SIZE | grep '^/dev/zram0 ' || true)
  [[ -n $row ]] || {
    echo "zram0 is not active swap"
    return 1
  }
  echo "$row"
}

c_journald() {
  local v
  v=$(systemd-analyze cat-config systemd/journald.conf |
    sed -n 's/^SystemMaxUse=//p' | tail -n1)
  [[ $v == 2G ]] || {
    echo "SystemMaxUse is '${v:-unset}', want 2G"
    return 1
  }
  echo "SystemMaxUse=$v"
}

# ---- Kernel settings -----------------------------------------------------

c_sysctls() {
  local bad=0 key want got
  for pair in net.ipv4.ip_unprivileged_port_start=80 \
    net.ipv4.ip_nonlocal_bind=1 \
    kernel.apparmor_restrict_unprivileged_userns=0; do
    key=${pair%%=*}
    want=${pair#*=}
    got=$(sysctl -n "$key" 2>/dev/null || echo missing)
    if [[ $got != "$want" ]]; then
      echo "$key is $got, want $want"
      bad=1
    fi
  done
  got=$(sysctl -n user.max_user_namespaces 2>/dev/null || echo 0)
  if ((got <= 0)); then
    echo "user.max_user_namespaces is $got, want above 0"
    bad=1
  fi
  ((bad == 0)) || return 1
  echo "all four hold"
}

ensure_image() {
  podman image exists "$IMAGE" || podman pull -q "$IMAGE" >/dev/null
}

# The Chromium sandbox in the server container needs an unprivileged user
# namespace inside a container that runs as a non-root user and holds
# SYS_ADMIN (design "Host", last bullet of the trust boundaries).
c_userns() {
  ensure_image || return 1
  podman run --rm --name "${PREFIX}userns" --user 1000:1000 \
    --cap-add SYS_ADMIN "$IMAGE" unshare -U -r true ||
    {
      echo "unshare -U -r failed in a non-root container with SYS_ADMIN"
      return 1
    }
  echo "busybox unshare -U -r works as uid 1000"
}

# ---- Listeners -----------------------------------------------------------

# Nothing may listen on a non-loopback address except 22, 80, 443, and the
# print listener 10.89.10.1:8081. Run before the probe starts its own listener.
c_listeners() {
  local bad out addr port
  bad=()
  while read -r local_addr; do
    addr=${local_addr%:*}
    port=${local_addr##*:}
    addr=${addr#[}
    addr=${addr%]}
    addr=${addr%%%*}
    case $addr in 127.* | ::1) continue ;; esac
    case $port in 22 | 80 | 443) continue ;; esac
    [[ $addr == "$GATEWAY" && $port == 8081 ]] && continue
    bad+=("$local_addr")
  done < <(ss -Hltn | awk '{print $4}')
  if ((${#bad[@]})); then
    out=${bad[*]}
    echo "unexpected listeners: $out"
    return 1
  fi
  echo "only loopback, 22, 80, 443 and $GATEWAY:8081"
}

c_postgres_socket() {
  local tcp
  tcp=$(ss -Hltnp | grep '"postgres"' || true)
  [[ -z $tcp ]] || {
    echo "postgres listens on TCP: $tcp"
    return 1
  }
  [[ -z $(ss -Hltn 'sport = :5432') ]] || {
    echo "something listens on port 5432"
    return 1
  }
  [[ -S /run/postgresql/.s.PGSQL.5432 ]] || {
    echo "/run/postgresql/.s.PGSQL.5432 does not exist"
    return 1
  }
  echo "no TCP listener, socket exists"
}

# ---- Container networking ------------------------------------------------

ensure_network() {
  podman network exists aboutme && return 0
  systemctl start aboutme-network.service
  podman network exists aboutme
}

# A bridge container must reach a host listener on the bridge gateway, where
# the server binds its print listener. The gateway address exists while a
# container is attached, and ip_nonlocal_bind lets the host bind it before.
c_bridge_to_host() {
  local body=
  ensure_image || return 1
  ensure_network || {
    echo "network aboutme does not exist and aboutme-network.service failed"
    return 1
  }
  echo ok >"$work/probe"
  python3 -m http.server --bind "$GATEWAY" --directory "$work" "$HOST_PORT" \
    >/dev/null 2>&1 &
  echo $! >"$work/http.pid"
  for _ in 1 2 3 4 5; do
    ss -Hltn "sport = :$HOST_PORT" | grep -q . && break
    sleep 1
  done
  body=$(podman run --rm --name "${PREFIX}bridge" --network aboutme "$IMAGE" \
    wget -q -T 5 -O - "http://$GATEWAY:$HOST_PORT/probe" 2>&1) || true
  kill "$(cat "$work/http.pid")" 2>/dev/null || true
  rm -f "$work/http.pid"
  [[ $body == ok ]] || {
    echo "container did not reach $GATEWAY:$HOST_PORT (got '${body:-nothing}')"
    return 1
  }
  echo "container on aboutme reached $GATEWAY:$HOST_PORT"
}

# The host (Caddy, Go) must reach Nuxt, published on loopback only.
c_host_to_bridge() {
  local body=
  ensure_image || return 1
  ensure_network || return 1
  podman run -d --rm --name "${PREFIX}pub" --network aboutme \
    -p "127.0.0.1:$PUB_PORT:8000" "$IMAGE" \
    sh -c 'mkdir -p /www && echo ok >/www/index.html && exec httpd -f -p 8000 -h /www' \
    >/dev/null
  for _ in 1 2 3 4 5; do
    body=$(curl -fsS --max-time 3 "http://127.0.0.1:$PUB_PORT/index.html" 2>/dev/null) && break
    sleep 1
  done
  podman rm -f "${PREFIX}pub" >/dev/null 2>&1 || true
  [[ $body == ok ]] || {
    echo "host did not reach the published port 127.0.0.1:$PUB_PORT"
    return 1
  }
  echo "host reached a container port published on 127.0.0.1"
}

# ---- Outbound reachability -----------------------------------------------

c_https() {
  local host=$1
  curl -sS -o /dev/null --max-time 10 "https://$host/" 2>&1 || {
    echo "no HTTPS answer from $host"
    return 1
  }
  echo "$host answers on 443"
}
c_vstorage() { c_https hcm04.vstorage.vngcloud.vn; }
c_ghcr() { c_https ghcr.io; }

# The journal is the log store and lives on the data volume, and alert.sh
# needs msmtp (design "Logs, metrics, and alarms").
c_journal_alerts() {
  mountpoint -q /var/log/journal || {
    echo "/var/log/journal is not bound from the data volume"
    return 1
  }
  command -v msmtp >/dev/null || {
    echo "msmtp is not installed"
    return 1
  }
  systemctl is-enabled --quiet aboutme-watch.timer || {
    echo "aboutme-watch.timer is not enabled"
    return 1
  }
  echo "journal on the data volume, msmtp present, watch timer enabled"
}

# ---- CrowdSec and the fence ----------------------------------------------

c_crowdsec_lapi() {
  local code
  code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 \
    http://127.0.0.1:8095/health || true)
  [[ $code == 200 ]] || {
    echo "127.0.0.1:8095/health answered '$code'"
    return 1
  }
  echo "local API answers on 127.0.0.1:8095"
}

# Prove the effective local files cannot configure the central API. A network
# status check cannot distinguish an opt-out from a registered offline host.
c_crowdsec_capi() {
  local helper=/usr/local/lib/aboutme/crowdsec-offline.sh
  if [[ ! -r $helper || -L $helper ]]; then
    echo "$helper is missing or is a symlink"
    return 1
  fi
  # shellcheck source=../host/crowdsec-offline.sh
  source "$helper"
  crowdsec_assert_offline /etc/crowdsec/config.yaml /etc/crowdsec/config.yaml.local \
    /etc/crowdsec/online_api_credentials.yaml || return 1
  echo "central API config and credentials are absent"
}

c_fence() {
  /usr/local/lib/aboutme/fence.sh read >/dev/null 2>&1 || {
    echo "fence.sh read failed"
    return 1
  }
  echo "fence.sh read works"
}

check "Podman is rootful and runs" c_podman
check "Quadlet generator exists" c_quadlet
check "data volume and bind targets are mounted" c_data_volume
check "root disk has 8 GB free" c_root_free
check "zram0 is active swap" c_zram
check "journald SystemMaxUse is 2G" c_journald
check "kernel settings hold" c_sysctls
check "no unexpected listener on a non-loopback address" c_listeners
check "PostgreSQL has no TCP port and its socket exists" c_postgres_socket
check "user namespace works for a non-root container with SYS_ADMIN" c_userns
check "bridge container reaches the host at $GATEWAY" c_bridge_to_host
check "host reaches a port published on 127.0.0.1" c_host_to_bridge
check "outbound HTTPS to hcm04.vstorage.vngcloud.vn" c_vstorage
check "outbound HTTPS to ghcr.io" c_ghcr
check "journal on the data volume and alerting in place" c_journal_alerts
check "CrowdSec local API answers" c_crowdsec_lapi
check "CrowdSec central API sharing is off" c_crowdsec_capi
check "fence.sh read works" c_fence

if ((failed)); then
  echo "probe: one or more facts FAILED"
  exit 1
fi
echo "probe: all facts PASS"
