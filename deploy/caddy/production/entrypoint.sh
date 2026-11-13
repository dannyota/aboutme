#!/bin/sh
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

hostname_ok() {
  printf '%s\n' "$1" | grep -Eqx '([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?'
}

# Caddy's file log writer rotates a file from one process only (Caddy log file output docs).
write_waf_log_output() {
  case $1 in
    serving | maintenance) ;;
    *) fail "unknown WAF log owner '$1'" ;;
  esac
  printf 'output file /var/log/caddy/waf/%s-match.log {\n\tmode 0600\n\troll_size 5MiB\n\troll_keep 2\n\troll_keep_for 24h\n}\n' \
    "$1" >/run/caddy/waf-log-output.caddy
}

write_crowdsec_feed_log() {
  case $1 in
    serving | maintenance) ;;
    *) fail "unknown CrowdSec feed owner '$1'" ;;
  esac
  cat > /run/caddy/crowdsec-feed.caddy <<EOF
log crowdsec_$1 {
	output file /var/log/caddy/crowdsec/$1.json {
		mode 0600
		roll_size 1MiB
		roll_uncompressed
		roll_keep 1
		roll_keep_for 0
	}
	format crowdsec_feed $1
}
EOF
  cat > /run/caddy/direct-private-logs.global <<EOF
log crowdsec_plugin_$1 {
	include crowdsec http.handlers.crowdsec
	output file /var/log/caddy/crowdsec/$1-bouncer.json {
		mode 0600
		roll_size 1MiB
		roll_uncompressed
		roll_keep 1
		roll_keep_for 0
	}
	format safe_metadata crowdsec_event
}
log direct_unmatched_$1 {
	include http.log.access
	exclude http.log.access.crowdsec_serving http.log.access.crowdsec_maintenance
	output file /var/log/caddy/crowdsec/$1.json {
		mode 0600
		roll_size 1MiB
		roll_uncompressed
		roll_keep 1
		roll_keep_for 0
	}
	format crowdsec_feed $1
}
log direct_errors_$1 {
	include http.log.error.crowdsec_$1 http.stdlib
	output stderr
	format safe_metadata http_error
}
log direct_tls_$1 {
	include tls
	output stderr
	format safe_metadata tls_event
}
EOF
}

write_default_log_excludes() {
  case $seen in
    *,direct,*)
      printf 'exclude http.handlers.waf crowdsec http.handlers.crowdsec http.log.error.crowdsec_serving http.log.error.crowdsec_maintenance http.stdlib http.log.access tls\n' \
        > /run/caddy/default-log-excludes.caddy ;;
    *)
      printf 'exclude http.handlers.waf\n' > /run/caddy/default-log-excludes.caddy ;;
  esac
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
    # Caddy picks a TLS certificate by server name (Caddy automatic HTTPS docs).
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
      printf '%s\n' "$crowdsec_url" | grep -Eqx 'http://127\.0\.0\.1:[0-9]{1,5}/?' ||
        fail "CROWDSEC_API_URL must use the loopback HTTP API"
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
write_default_log_excludes

if [ "${1:-}" = adapt ]; then
  write_waf_log_output serving
  write_crowdsec_feed_log serving
  caddy adapt --config /etc/caddy/Caddyfile >/dev/null
  write_waf_log_output maintenance
  write_crowdsec_feed_log maintenance
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
log_owner=serving
if [ "${MAINTENANCE:-0}" = 1 ]; then
  config=/etc/caddy/Caddyfile.maintenance
  log_owner=maintenance
fi
write_waf_log_output "$log_owner"
write_crowdsec_feed_log "$log_owner"
exec caddy run --config "$config" --adapter caddyfile
