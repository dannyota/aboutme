# Deployment observer

The reporter publishes `https://aboutme.vn/.well-known/deployment.json`: the
running image digests and their signature check statuses. The
[design](../design/deployment-transparency/README.md) explains the evidence.
Production on the Vietnam host uses the root reporter and observer container
below. The [AWS observer](aws-observer.md) is an undeployed ECS reference.

## Vietnam host

Run `deploy/vn/host/install.sh <bundle-dir>` on the host with the reviewed
`deploy/vn` bundle. It installs the root reporter and observer template, creates
the observation and public directories, installs the dedicated observer network,
and enables and starts its firewall. It also enables
`aboutme-deployment-document.timer`. Deploy a release with the single-platform
`aboutme-observer:<tag>-amd64` image. Deploy verifies its provenance, pulls its
digest, renders `aboutme-observer.container`, and starts or restarts the unit. A
tag without the amd64 observer image keeps the installed observer unit and
reports that choice, including during rollback.

The timer observes Podman every minute, including container restarts outside a
deploy. To refresh once and inspect service state on the host:

```sh
systemctl start aboutme-deployment-document.service
systemctl status aboutme-deployment-document.timer
systemctl status aboutme-observer.service
journalctl -u aboutme-deployment-document.service --since '-10 minutes'
journalctl -u aboutme-observer.service --since '-10 minutes'
```

The root reporter writes `/var/lib/aboutme/observation/observation.json`
atomically. The observer reads that directory read-only, checks the running
digests through Sigstore, and atomically writes
`/var/lib/aboutme/deployment/deployment.json`. The public directory belongs to
uid `65532`; the observation directory belongs to root. Both have mode `0755`,
and both files have mode `0644`. Caddy mounts only the public directory
read-only and serves the exact well-known path with the
[document headers](../design/deployment-transparency/host.md#source-and-publication).
It does not serve the rest of the directory. The `www` host redirects to the
apex.

After installation and deploy, fetch the document and open `/verify` in both
languages:

```sh
curl -fsS -D /tmp/aboutme-deployment.headers \
  https://aboutme.vn/.well-known/deployment.json \
  | jq '{platform, observed_at, stale_after, summary, components}'
```

Expect `greennode` / `podman`, current timestamps, and the digests Podman
reports. When the signature checks pass, expect `verified` with the release
version and commit. An unfinished signature check gives `unverified`. The page
must show the digests and the host trust limit, with no AWS claim. The host
reports its own runtime state, and a container on that host checks signatures.
Use the page's GitHub CLI commands to check each image independently. Verified
Podman images link to the release's `aboutme-<name>-amd64.spdx.json` assets,
including `aboutme-caddy-amd64.spdx.json` for maintenance.

A reporter failure preserves the observation and invokes `aboutme-alert@`. The
observer refuses observations older than 180 seconds or more than 60 seconds in
the future, so failed reads leave the public document to become stale. Inspect
the reporter's fixed error message, Podman state, and the observer journal.
Check the host clock if freshness errors recur.

The observer needs outbound HTTPS to GitHub, the registry fallback, and Sigstore
TUF. It has no Podman socket, secret, or bucket. Its TUF cache is tmpfs, and
verification results stay in process memory with a daily recheck. An observer
restart clears both caches. The observer stays up during maintenance.

A missing required container produces `mismatch`. A missing public document
produces 404. Stopping the timer or observer leaves the document to become
stale; removing the file stops publication until the observer writes again.
Never substitute `release.json` for a failed Podman read. A publication error
before rename preserves the previous document. A directory fsync error after
rename is logged even though the complete new document may be visible.

### Network isolation check

The observer joins only `aboutme-observer`, an IPv4 bridge on `172.18.0.0/24`.
Network DNS is disabled; the container uses `8.8.8.8` and `1.1.1.1`. IPv6 is
disabled inside the container. The `aboutme_observer` nftables table blocks
every host listener, app networks, private destinations, and cloud metadata. It
permits public TCP 443 and UDP and TCP DNS to those two resolvers. It drops new
forwarded connections to the observer subnet. CNI and CrowdSec retain their own
tables.

Run these checks as root on the host after installation and deploy:

```sh
systemctl status aboutme-observer-network.service \
  aboutme-observer-firewall.service aboutme-observer.service
systemctl is-enabled aboutme-observer-firewall.service
systemctl show aboutme-observer.service -p Requires -p After
podman info --format '{{.Host.NetworkBackend}}'
podman network inspect aboutme-observer
podman inspect --format '{{json .NetworkSettings.Networks}}' aboutme-observer
podman inspect --format '{{json .HostConfig.Dns}}' aboutme-observer
nft -c -f /etc/aboutme/observer-firewall.nft
nft list table inet aboutme_observer
```

Expect the Container Network Interface (CNI) backend, subnet `172.18.0.0/24`,
gateway `172.18.0.1`, DNS disabled on the network, and both public resolvers on
the container. The observer's generated unit must require and follow the
firewall unit. Both nft chains must have priority `filter - 1`. The input drop
covers the gateway, the host's eth0 address, and `172.17.0.1:8081`. The forward
destination drop must precede the TCP 443 accept.

With `curl` and `dig` installed on the host, test from the running observer's
network namespace. The observer image has no shell. These commands use host
tools without adding tools or mounts to the container:

```sh
observer_pid=$(podman inspect --format '{{.State.Pid}}' aboutme-observer)
nsenter -t "$observer_pid" -n sysctl net.ipv6.conf.all.disable_ipv6 \
  net.ipv6.conf.default.disable_ipv6
for resolver in 8.8.8.8 1.1.1.1; do
  nsenter -t "$observer_pid" -n dig "@$resolver" ghcr.io A +time=2 +tries=1
  nsenter -t "$observer_pid" -n dig "@$resolver" ghcr.io A +tcp +time=2 +tries=1
done
registry_ip=$(getent ahostsv4 ghcr.io | awk '$2 == "STREAM" { print $1; exit }')
nsenter -t "$observer_pid" -n curl --ipv4 --noproxy '*' --max-time 10 \
  --resolve "ghcr.io:443:$registry_ip" -sS -o /dev/null -w '%{http_code}\n' \
  https://ghcr.io/v2/
web_ip=$(podman inspect --format \
  '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' aboutme-web)
for target in 172.18.0.1:80 172.18.0.1:443 172.18.0.1:22922 \
  172.17.0.1:8081 10.76.1.3:443 "$web_ip:3000" \
  169.254.169.254:80 169.254.169.254:443; do
  if nsenter -t "$observer_pid" -n timeout 3 bash -c \
    'exec 3<>"/dev/tcp/$1/$2"' _ "${target%:*}" "${target##*:}"; then
    printf 'FAIL reached %s\n' "$target"
  else
    printf 'connection failed %s\n' "$target"
  fi
done
nsenter -t "$observer_pid" -n dig @8.8.4.4 ghcr.io A +time=2 +tries=1
observer_ip=$(podman inspect --format \
  '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' aboutme-observer)
web_pid=$(podman inspect --format '{{.State.Pid}}' aboutme-web)
if nsenter -t "$web_pid" -n timeout 3 bash -c \
  'exec 3<>"/dev/tcp/$1/443"' _ "$observer_ip"; then
  printf 'FAIL app reached observer\n'
else
  printf 'app connection to observer failed\n'
fi
nft list table inet aboutme_observer
```

Expect both IPv6 sysctls to report `1`, successful DNS over both transports, and
HTTP 401 from the registry. Every listed TCP connection must fail, and DNS to
`8.8.4.4` must time out. Check that the input, private-destination, and final
forward drop counters increased. The app connection must fail and increase the
destination drop counter. A failed connection alone does not prove a firewall
drop. Repeat after a host reboot.

## AWS ECS setup

See [the AWS reference](aws-observer.md#aws-ecs-setup).

## AWS ECS update

See [the AWS reference](aws-observer.md#aws-ecs-update).

## AWS ECS production check

See [the AWS reference](aws-observer.md#aws-ecs-production-check).

## AWS ECS failures

See [the AWS reference](aws-observer.md#aws-ecs-failures).

## AWS ECS stop publishing

See [the AWS reference](aws-observer.md#aws-ecs-stop-publishing).
