#!/bin/sh
# Writes the TLS material from the task environment to tmpfs, then starts
# Caddy without the private key in its environment.
set -eu
umask 077
: "${ORIGIN_CERT:?}" "${ORIGIN_KEY:?}" "${ORIGIN_PULL_CA:?}" "${CLOUDFLARE_RANGES:?}"
printf '%s\n' "$ORIGIN_CERT" >/run/caddy/origin.pem
printf '%s\n' "$ORIGIN_KEY" >/run/caddy/origin-key.pem
printf '%s\n' "$ORIGIN_PULL_CA" >/run/caddy/origin-pull-ca.pem
unset ORIGIN_KEY
exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
