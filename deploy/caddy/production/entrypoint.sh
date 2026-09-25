#!/bin/sh
# Writes the TLS material from the task environment to tmpfs, assembles the
# edge listeners named in EDGES, then starts Caddy without the private key in
# its environment. MAINTENANCE=1 selects the maintenance-mode route table
# instead of the normal one; both import the same edge listeners.
#
# EDGES is a comma-separated list of cloudflare (443, origin-pull mTLS,
# CF-Connecting-IP from CLOUDFLARE_RANGES) and cloudfront (8443, origin mTLS
# from CLOUDFRONT_CLIENT_CA, CloudFront-Viewer-Address). Unset means
# cloudflare; an empty list is an error. See docs/design/cloudfront-edge.md.
#
#   aboutme-caddy         run Caddy
#   aboutme-caddy adapt   assemble the edges and check that both route
#                         tables parse, without TLS material
set -eu
umask 077
edges=${EDGES-cloudflare}
case ",$edges," in
  *[!a-z,]* | *,,*) echo "aboutme-caddy: malformed EDGES '$edges'" >&2; exit 1 ;;
esac
mkdir -p /run/caddy
: >/run/caddy/edges.global
: >/run/caddy/edges.sites
seen=,
for edge in $(printf '%s' "$edges" | tr ',' ' '); do
  case $edge in
    cloudflare) : "${CLOUDFLARE_RANGES:?}" ;;
    cloudfront) ;;
    *) echo "aboutme-caddy: unknown edge '$edge'" >&2; exit 1 ;;
  esac
  case $seen in
    *,"$edge",*) echo "aboutme-caddy: edge '$edge' listed twice" >&2; exit 1 ;;
  esac
  seen=$seen$edge,
  cat "/etc/caddy/edges/$edge.global" >>/run/caddy/edges.global
  cat "/etc/caddy/edges/$edge.sites" >>/run/caddy/edges.sites
done

if [ "${1:-}" = adapt ]; then
  caddy adapt --config /etc/caddy/Caddyfile >/dev/null
  caddy adapt --config /etc/caddy/Caddyfile.maintenance >/dev/null
  exit 0
fi

: "${ORIGIN_CERT:?}" "${ORIGIN_KEY:?}"
printf '%s\n' "$ORIGIN_CERT" >/run/caddy/origin.pem
printf '%s\n' "$ORIGIN_KEY" >/run/caddy/origin-key.pem
unset ORIGIN_KEY
case $seen in
  *,cloudflare,*)
    : "${ORIGIN_PULL_CA:?}"
    printf '%s\n' "$ORIGIN_PULL_CA" >/run/caddy/origin-pull-ca.pem ;;
esac
case $seen in
  *,cloudfront,*)
    : "${CLOUDFRONT_CLIENT_CA:?}"
    printf '%s\n' "$CLOUDFRONT_CLIENT_CA" >/run/caddy/cloudfront-ca.pem ;;
esac
config=/etc/caddy/Caddyfile
[ "${MAINTENANCE:-0}" = 1 ] && config=/etc/caddy/Caddyfile.maintenance
exec caddy run --config "$config" --adapter caddyfile
