# shellcheck shell=bash
# Direct-maintenance regression helpers sourced by ../test.sh.
# The harness defines the shared container, port, and work-directory variables.
# shellcheck disable=SC2154

test_direct_maintenance_log_isolation() {
  local serving_sum serving_feed_sum mdport mhport maintenance_ca code maintenance_matches
  local maintenance_feed leak maintenance_logs

  # Maintenance Caddy runs beside serving Caddy during a deploy. Both share the
  # host log mount, but each process owns and rotates a distinct diagnostic file.
  serving_sum=$(podman exec "$name" sha256sum /var/log/caddy/waf/serving-match.log)
  serving_feed_sum=$(podman exec "$name" sha256sum /var/log/caddy/crowdsec/serving.json)
  podman exec "$name" test ! -e /var/log/caddy/waf/maintenance-match.log
  mdport=20455
  mhport=20456
  podman run -d --name "$name-maint" -p "127.0.0.1:$mdport:443" -p "127.0.0.1:$mhport:80" \
    --tmpfs /run/caddy "${unprivileged[@]}" --cap-add NET_BIND_SERVICE \
    -v "$waf_volume:/var/log/caddy" -e EDGES=direct -e DIRECT_TLS=internal -e MAINTENANCE=1 \
    "$image" >/dev/null
  wait_started "$name-maint"
  check_unprivileged "$name-maint" "0050 01BB" 0000000000000400
  for _ in $(seq 1 20); do
    maintenance_ca=$(podman exec "$name-maint" cat /data/caddy/pki/authorities/local/root.crt 2>/dev/null || true)
    [[ -n $maintenance_ca ]] && break
    sleep 0.5
  done
  printf '%s\n' "$maintenance_ca" >"$work/maintenance-direct-ca.pem"
  code=$(curl -s -o /dev/null -w '%{http_code}' --resolve "aboutme.vn:$mdport:127.0.0.1" \
    --cacert "$work/maintenance-direct-ca.pem" \
    "https://aboutme.vn:$mdport/api/v1/probe?q=%3Cscript%3Emaintmarker6e4d%3C/script%3E")
  [[ $code == 503 ]] || { echo "direct maintenance: WAF probe got $code, want 503" >&2; exit 1; }
  sleep 1
  maintenance_matches=$(podman exec "$name-maint" cat /var/log/caddy/waf/maintenance-match.log)
  grep -q '"msg":"waf_rule_match"' <<<"$maintenance_matches" ||
    { echo "direct maintenance: no metadata-only rule match logged" >&2; exit 1; }
  grep -q '"rule_id":941100' <<<"$maintenance_matches" ||
    { echo "direct maintenance: no XSS rule ID logged" >&2; exit 1; }
  if grep -q 'waf_rule_match_unparsed' <<<"$maintenance_matches"; then
    echo "direct maintenance: an upstream rule message did not match the safe metadata shape" >&2
    exit 1
  fi
  for leak in maintmarker6e4d 127.0.0.1 'XSS Attack' 'Matched Data' '@owasp_crs'; do
    if grep -qF "$leak" <<<"$maintenance_matches"; then
      echo "direct maintenance: WAF diagnostics contain $leak" >&2
      exit 1
    fi
  done
  [[ $(podman exec "$name-maint" stat -c %a:%u /var/log/caddy/waf/maintenance-match.log) == 600:10001 ]] ||
    { echo "direct maintenance: diagnostic log is not private to uid 10001" >&2; exit 1; }
  [[ $(podman exec "$name" sha256sum /var/log/caddy/waf/serving-match.log) == "$serving_sum" ]] ||
    { echo "direct maintenance: serving diagnostic file changed" >&2; exit 1; }
  maintenance_feed=$(podman exec "$name-maint" cat /var/log/caddy/crowdsec/maintenance.json)
  grep -q '"route_class":"maintenance"' <<<"$maintenance_feed" ||
    { echo "direct maintenance: no maintenance feed record" >&2; exit 1; }
  if grep -qE 'maintmarker6e4d|"(uri|path|query|headers|body|host|port|tls|duration|size)"' <<<"$maintenance_feed"; then
    echo "direct maintenance: request data reached the maintenance feed" >&2
    exit 1
  fi
  [[ $(podman exec "$name-maint" stat -c %a:%u /var/log/caddy/crowdsec/maintenance.json) == 600:10001 ]] ||
    { echo "direct maintenance: feed is not private to uid 10001" >&2; exit 1; }
  [[ $(podman exec "$name" sha256sum /var/log/caddy/crowdsec/serving.json) == "$serving_feed_sum" ]] ||
    { echo "direct maintenance: serving feed changed" >&2; exit 1; }
  podman exec "$name-maint" test ! -e /var/log/caddy/waf/match.log
  maintenance_logs=$(podman logs "$name-maint" 2>&1)
  if grep -qE 'maintmarker6e4d|941100|"client_ip"' <<<"$maintenance_logs"; then
    echo "direct maintenance: WAF request data reached stderr" >&2
    exit 1
  fi
  podman rm -f --ignore "$name-maint" >/dev/null
}
