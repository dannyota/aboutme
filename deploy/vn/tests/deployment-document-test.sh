#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/observation" "$work/public"
printf 'secret-canary-not-a-release\n' >"$work/release.json"
export CALLS=$work/calls PODMAN_CASE=running
cat >"$work/bin/podman" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$CALLS"
[[ $1 == container ]]
case $2 in
  exists)
    case "$PODMAN_CASE:${3}" in
      error:*) exit 125 ;;
      running:aboutme-maintenance | missing:aboutme-web) exit 1 ;;
    esac ;;
  inspect)
    # No full inspect may read or print environment secrets.
    [[ $3 == --format && $4 == '{{.State.Running}} {{.ImageDigest}} {{.State.StartedAt.UTC.Format "2006-01-02T15:04:05.999999999Z07:00"}}' ]]
    if [[ $PODMAN_CASE == inspect-error ]]; then
      echo 'secret-canary-from-podman-error' >&2; exit 125
    fi
    running=true
    [[ $PODMAN_CASE != stopped || $5 != aboutme-web ]] || running=false
    digest=sha256:$(printf '%064d' 7)
    [[ $PODMAN_CASE != malformed ]] || digest=sha256:bad
    stamp=2026-10-11T01:14:05.123456789Z
    [[ $PODMAN_CASE != bad-time ]] || stamp=secret-canary
    [[ $PODMAN_CASE != extra-field ]] || stamp+=' secret-canary'
    printf '%s %s %s\n' "$running" "$digest" "$stamp" ;;
  *) exit 99 ;;
esac
STUB
chmod +x "$work/bin/podman"
export PATH="$work/bin:$PATH"
generator=$root/deploy/vn/host/deployment-document.sh
generate() { bash "$generator" "$work/observation" HCM03 >"$work/out" 2>"$work/err"; }
fail() { echo "deployment-document-test: $*" >&2; exit 1; }

generate
doc=$work/observation/observation.json
[[ ! -e $work/public/deployment.json ]] || fail 'reporter wrote a public document'
grep -qx 'directory=${1:-/var/lib/aboutme/observation}' "$generator" || fail 'wrong default observation directory'
jq -e '
  .schema_version == 1 and .platform == {provider:"greennode",orchestrator:"podman",region:"HCM03"}
  and .summary == "unverified" and .release == null
  and (.components | map(.name)) == ["server","web","caddy"]
  and all(.components[].running_images[];
    .digest == ("sha256:" + ("0" * 63) + "7") and .replicas == 1
    and .running_since == "2026-10-11T01:14:05Z"
    and .version == null and .commit == null
    and .signature.status == "unchecked" and .sbom.status == "unchecked"
    and .links.commit == null and .links.build == null and .links.sbom == null)
  and ((.stale_after | fromdateiso8601) - (.observed_at | fromdateiso8601)) == 180
' "$doc" >/dev/null || fail 'running state or evidence is wrong'
[[ $(stat -c %a "$doc") == 644 ]] || fail 'document is not public-readable'
[[ ! -s $work/out && ! -s $work/err ]] || fail 'successful generator printed output'
# The running digest differs from the intended release; release.json must not
# be read. A canary elsewhere in the directory must never enter the document.
cp "$doc" "$work/previous"
for PODMAN_CASE in error inspect-error malformed bad-time extra-field; do
  export PODMAN_CASE
  if generate; then fail "accepted $PODMAN_CASE"; fi
  cmp -s "$doc" "$work/previous" || fail "replaced good document on $PODMAN_CASE"
  if grep -q -- 'secret-canary' "$doc" "$work/out" "$work/err"; then fail 'leaked raw data'; fi
  [[ $(find "$work/observation" -name '.observation.*' | wc -l) == 0 ]] || fail 'left a temporary file'
done
if flock -o "$work/observation/.writer.lock" bash "$generator" "$work/observation" HCM03 \
  >"$work/out" 2>"$work/err"; then fail 'accepted a concurrent writer'; fi
cmp -s "$doc" "$work/previous" || fail 'changed document while locked'
for PODMAN_CASE in missing stopped; do
  export PODMAN_CASE
  generate
  jq -e '.summary == "mismatch" and .components[1].running_images == []' "$doc" >/dev/null
done
PODMAN_CASE=maintenance generate
jq -e '.components[3].name == "maintenance" and .components[3].image == "ghcr.io/dannyota/aboutme-caddy"' \
  "$doc" >/dev/null
if bash "$generator" "$work/observation" 'HCM03 secret-canary' >"$work/out" 2>"$work/err"; then
  fail 'accepted invalid region'
fi
if grep -q -- 'secret-canary' "$work/out" "$work/err"; then fail 'printed invalid input'; fi
for unit in aboutme-caddy aboutme-maintenance; do
  grep -qx -- 'Volume=/var/lib/aboutme/deployment:/srv/deployment:ro' \
    "$root/deploy/vn/host/quadlet/$unit.container.in"
done
observer=$root/deploy/vn/host/quadlet/aboutme-observer.container.in
for setting in 'User=65532:65532' 'Network=aboutme-observer' 'ReadOnly=true' 'DropCapability=all' 'NoNewPrivileges=true' \
  'DNS=8.8.8.8' 'DNS=1.1.1.1' \
  'Requires=aboutme-observer-network.service aboutme-observer-firewall.service' \
  'After=aboutme-observer-network.service aboutme-observer-firewall.service' \
  'Exec=run --interval 60s' 'Environment=OBSERVER_PLATFORM=podman' 'Environment=OBSERVER_REGION=HCM03' \
  'Volume=/var/lib/aboutme/observation:/observation:ro' 'Volume=/var/lib/aboutme/deployment:/public:rw'; do
  grep -qxF -- "$setting" "$observer" || fail "observer lacks $setting"
done
[[ $(grep -c '^Volume=' "$observer") == 2 ]] || fail 'observer has unexpected host mounts'
grep -q -- '--memory=128m' "$observer" || fail 'observer memory is not capped'
grep -qxF -- 'Tmpfs=/tmp/sigstore-tuf:rw,nosuid,nodev,noexec,size=16m,uid=65532,gid=65532,mode=0700' \
  "$observer" || fail 'observer TUF cache is not bounded tmpfs'
if grep -Ei -- 'podman.sock|Secret=|EnvironmentFile=|AddCapability=|PublishPort=|Network=host' "$observer"; then
  fail 'observer exposes host access, ports, or secrets'
fi
network=$root/deploy/vn/host/quadlet/aboutme-observer.network
for setting in 'NetworkName=aboutme-observer' 'Driver=bridge' 'Subnet=172.18.0.0/24' \
  'Gateway=172.18.0.1' 'DisableDNS=true' 'IPv6=false'; do
  grep -qxF -- "$setting" "$network" || fail "observer network lacks $setting"
done
for setting in '--sysctl=net.ipv6.conf.all.disable_ipv6=1' \
  '--sysctl=net.ipv6.conf.default.disable_ipv6=1'; do
  grep -qF -- "$setting" "$observer" || fail "observer lacks $setting"
done
firewall=$root/deploy/vn/host/etc/aboutme/observer-firewall.nft
grep -qxF 'add table inet aboutme_observer' "$firewall" || fail 'firewall cannot load before its table exists'
grep -qxF 'delete table inet aboutme_observer' "$firewall" || fail 'firewall does not replace its own table'
grep -qxF 'table inet aboutme_observer {' "$firewall" || fail 'firewall lacks its own table'
if grep -E -- 'flush ruleset|table (ip|ip6) |delete table .* (nat|filter|crowdsec|crowdsec6)' "$firewall"; then
  fail 'observer firewall changes another table'
fi
input=$(sed -n '/^    chain input {/,/^    }/p' "$firewall")
for rule in 'type filter hook input priority filter - 1; policy accept;' \
  'ip saddr 172.18.0.0/24 ct state established,related counter accept' \
  'ip saddr 172.18.0.0/24 counter drop'; do
  grep -qF -- "$rule" <<<"$input" || fail "observer input lacks $rule"
done
for subnet in 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 100.64.0.0/10 \
  169.254.0.0/16 127.0.0.0/8 224.0.0.0/4 0.0.0.0/8 240.0.0.0/4; do
  grep -qF -- "$subnet" "$firewall" || fail "observer firewall lacks $subnet"
done
forward=$(sed -n '/^    chain forward {/,/^    }/p' "$firewall")
for rule in 'type filter hook forward priority filter - 1; policy accept;' \
  'ip saddr 172.18.0.0/24 ct state established,related counter accept' \
  'ip saddr 172.18.0.0/24 ip daddr { 8.8.8.8, 1.1.1.1 } udp dport 53 counter accept' \
  'ip saddr 172.18.0.0/24 ip daddr { 8.8.8.8, 1.1.1.1 } tcp dport 53 counter accept' \
  'ip saddr 172.18.0.0/24 ip daddr @blocked_ipv4 counter drop' \
  'ip saddr 172.18.0.0/24 tcp dport 443 counter accept' \
  'ip saddr 172.18.0.0/24 counter drop' \
  'ip daddr 172.18.0.0/24 ct state established,related counter accept' \
  'ip daddr 172.18.0.0/24 counter drop'; do
  grep -qF -- "$rule" <<<"$forward" || fail "observer forwarding lacks $rule"
done
private_drop=$(grep -nF 'ip daddr @blocked_ipv4 counter drop' <<<"$forward" | cut -d: -f1)
https_accept=$(grep -nF 'tcp dport 443 counter accept' <<<"$forward" | cut -d: -f1)
((private_drop < https_accept)) || fail 'observer accepts private HTTPS'
unit=$root/deploy/vn/host/etc/systemd/system/aboutme-observer-firewall.service
for setting in 'Type=oneshot' 'RemainAfterExit=yes' 'Before=aboutme-observer.service' \
  'ExecStart=/usr/sbin/nft -f /etc/aboutme/observer-firewall.nft' 'WantedBy=multi-user.target'; do
  grep -qxF -- "$setting" "$unit" || fail "observer firewall unit lacks $setting"
done
installer=$root/deploy/vn/host/install.sh
grep -qF 'install -m 0644 "$bundle/host/quadlet/aboutme-observer.network"' "$installer" ||
  fail 'installer omits the observer network'
grep -qxF 'systemctl enable aboutme-observer-firewall.service >/dev/null' "$installer" ||
  fail 'installer does not enable the observer firewall'
grep -qxF 'systemctl restart aboutme-observer-firewall.service' "$installer" ||
  fail 'installer does not load the observer firewall'
grep -qxF 'd /var/lib/aboutme/observation 0755 root root -' "$root/deploy/vn/host/etc/tmpfiles.d/aboutme.conf"
grep -qxF 'd /var/lib/aboutme/deployment 0755 65532 65532 -' "$root/deploy/vn/host/etc/tmpfiles.d/aboutme.conf"
echo 'deployment document tests passed'
