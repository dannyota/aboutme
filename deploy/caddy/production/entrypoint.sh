#!/bin/sh
# Writes the TLS material from the task environment to tmpfs, assembles the
# edge listeners named in EDGES, then starts Caddy without a private key or
# API key in its environment. MAINTENANCE=1 selects the maintenance-mode route
# table instead of the normal one; both import the same edge listeners.
#
# EDGES is a comma-separated list of:
#   cloudfront  8443, origin mTLS from CLOUDFRONT_CLIENT_CA, client address
#               from CloudFront-Viewer-Address (docs/design/cloudfront-edge.md).
#               Needs ORIGIN_CERT and ORIGIN_KEY.
#   direct      80 and 443, Let's Encrypt certificates, client address from the
#               socket, Coraza, and the CrowdSec bouncer
#               (docs/design/vietnam-production.md, "Edge").
# Unset means cloudfront; an empty list is an error.
#
# The direct listener reads:
#   DIRECT_HOST        apex it serves; default aboutme.vn
#   DIRECT_WWW_HOST    host that redirects to the apex; default www.DIRECT_HOST,
#                      empty for none
#   DIRECT_TLS         acme (default, Let's Encrypt) or internal (tests only)
#   CROWDSEC_API_URL   CrowdSec local API, such as http://127.0.0.1:8081/
#   CROWDSEC_API_KEY_FILE or CROWDSEC_API_KEY
#                      the bouncer key, as a file (a mounted Podman secret) or
#                      in the environment (an env Podman secret), not both
# With neither CROWDSEC_API_URL nor a key, the bouncer is off and the listener
# serves as before; giving only one of them is an error. With both edges, the
# direct hosts must differ from the CloudFront listener's (a rehearsal host).
#
#   aboutme-caddy         run Caddy
#   aboutme-caddy adapt   assemble the edges and check that both route
#                         tables parse, without TLS material
set -eu
umask 077
fail() { echo "aboutme-caddy: $*" >&2; exit 1; }
edges=${EDGES-cloudfront}
case ",$edges," in
  *[!a-z,]* | *,,*) fail "malformed EDGES '$edges'" ;;
esac
mkdir -p /run/caddy
: >/run/caddy/edges.global
: >/run/caddy/edges.sites
seen=,
for edge in $(printf '%s' "$edges" | tr ',' ' '); do
  case $edge in
    cloudfront | direct) ;;
    *) fail "unknown edge '$edge'" ;;
  esac
  case $seen in
    *,"$edge",*) fail "edge '$edge' listed twice" ;;
  esac
  seen=$seen$edge,
  cat "/etc/caddy/edges/$edge.global" >>/run/caddy/edges.global
  cat "/etc/caddy/edges/$edge.sites" >>/run/caddy/edges.sites
done

# A lowercase DNS name with at least two labels; Caddy reads it as a site
# address, so nothing else may appear in it.
hostname_ok() {
  printf '%s\n' "$1" | grep -Eqx '([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?'
}

crowdsec_url=${CROWDSEC_API_URL-}
crowdsec_key_file=${CROWDSEC_API_KEY_FILE-}
crowdsec_key_set=${CROWDSEC_API_KEY+set}
case $seen in
  *,direct,*)
    export DIRECT_HOST="${DIRECT_HOST-aboutme.vn}"
    hostname_ok "$DIRECT_HOST" || fail "malformed DIRECT_HOST '$DIRECT_HOST'"
    www=${DIRECT_WWW_HOST-www.$DIRECT_HOST}
    : >/run/caddy/direct-www.sites
    if [ -n "$www" ]; then
      hostname_ok "$www" || fail "malformed DIRECT_WWW_HOST '$www'"
      [ "$www" != "$DIRECT_HOST" ] || fail "DIRECT_WWW_HOST equals DIRECT_HOST"
      printf 'http://%s {\n\tredir https://%s{uri} permanent\n}\n' "$www" "$DIRECT_HOST" \
        >/run/caddy/direct-www.sites
      printf 'https://%s {\n\timport direct_tls\n\timport direct_response_headers\n\tredir https://%s{uri} permanent\n}\n' \
        "$www" "$DIRECT_HOST" >>/run/caddy/direct-www.sites
    fi
    # Caddy picks a certificate by name, so with both edges the direct
    # listener could present the CloudFront origin certificate.
    case $seen in
      *,cloudfront,*)
        case " $DIRECT_HOST $www " in
          *" aboutme.vn "* | *" www.aboutme.vn "*) fail "with the cloudfront edge, the direct hosts must not be aboutme.vn or www.aboutme.vn" ;;
        esac ;;
    esac
    case ${DIRECT_TLS-acme} in
      acme) printf 'tls {\n\tissuer acme\n}\n' >/run/caddy/direct-tls.caddy ;;
      internal) printf 'tls internal\n' >/run/caddy/direct-tls.caddy ;;
      *) fail "unknown DIRECT_TLS '$DIRECT_TLS'" ;;
    esac

    : >/run/caddy/crowdsec.global
    : >/run/caddy/crowdsec.handler
    if [ -n "$crowdsec_url" ] || [ -n "$crowdsec_key_file" ] || [ -n "$crowdsec_key_set" ]; then
      [ -n "$crowdsec_url" ] || fail "a CrowdSec key without CROWDSEC_API_URL"
      printf '%s\n' "$crowdsec_url" | grep -Eqx 'https?://[A-Za-z0-9.-]+(:[0-9]{1,5})?/?' ||
        fail "malformed CROWDSEC_API_URL"
      if [ -n "$crowdsec_key_file" ]; then
        [ -z "$crowdsec_key_set" ] || fail "both CROWDSEC_API_KEY_FILE and CROWDSEC_API_KEY are set"
        printf '%s\n' "$crowdsec_key_file" | grep -Eqx '/[A-Za-z0-9._/-]+' ||
          fail "malformed CROWDSEC_API_KEY_FILE"
        key_file=$crowdsec_key_file
      elif [ -n "$crowdsec_key_set" ]; then
        key_file=/run/caddy/crowdsec-key
        printf '%s\n' "$CROWDSEC_API_KEY" >"$key_file"
      else
        fail "CROWDSEC_API_URL without a CrowdSec key"
      fi
      # Caddy reads the key from the file when the app starts ({file.*}), so
      # the key is never part of the configuration. The bouncer pulls the
      # decision stream every 10 seconds; when the local API is down it
      # keeps serving and retries (no enable_hard_fails).
      printf 'crowdsec {\n\tapi_url %s\n\tapi_key {file.%s}\n\tticker_interval 10s\n}\n' \
        "$crowdsec_url" "$key_file" >/run/caddy/crowdsec.global
      printf 'crowdsec\n' >/run/caddy/crowdsec.handler
    fi
    ;;
  *)
    if [ -n "$crowdsec_url" ] || [ -n "$crowdsec_key_file" ] || [ -n "$crowdsec_key_set" ]; then
      fail "CrowdSec settings need the direct edge"
    fi
    ;;
esac
unset CROWDSEC_API_KEY

if [ "${1:-}" = adapt ]; then
  caddy adapt --config /etc/caddy/Caddyfile >/dev/null
  caddy adapt --config /etc/caddy/Caddyfile.maintenance >/dev/null
  exit 0
fi

case $seen in
  *,cloudfront,*)
    : "${ORIGIN_CERT:?}" "${ORIGIN_KEY:?}" "${CLOUDFRONT_CLIENT_CA:?}"
    printf '%s\n' "$ORIGIN_CERT" >/run/caddy/origin.pem
    printf '%s\n' "$ORIGIN_KEY" >/run/caddy/origin-key.pem
    printf '%s\n' "$CLOUDFRONT_CLIENT_CA" >/run/caddy/cloudfront-ca.pem ;;
esac
unset ORIGIN_KEY
config=/etc/caddy/Caddyfile
[ "${MAINTENANCE:-0}" = 1 ] && config=/etc/caddy/Caddyfile.maintenance
exec caddy run --config "$config" --adapter caddyfile
