#!/bin/sh
# Writes the TLS material from the task environment to tmpfs, then starts
# Caddy without the private key in its environment. MAINTENANCE=1 selects the
# maintenance-mode route table instead of the normal one; both trust the same
# Cloudflare ranges and the same origin-pull certificate.
set -eu
umask 077
: "${ORIGIN_CERT:?}" "${ORIGIN_KEY:?}" "${ORIGIN_PULL_CA:?}" "${CLOUDFLARE_RANGES:?}"
printf '%s\n' "$ORIGIN_CERT" >/run/caddy/origin.pem
printf '%s\n' "$ORIGIN_KEY" >/run/caddy/origin-key.pem
printf '%s\n' "$ORIGIN_PULL_CA" >/run/caddy/origin-pull-ca.pem
unset ORIGIN_KEY
config=/etc/caddy/Caddyfile
[ "${MAINTENANCE:-0}" = 1 ] && config=/etc/caddy/Caddyfile.maintenance
exec caddy run --config "$config" --adapter caddyfile
