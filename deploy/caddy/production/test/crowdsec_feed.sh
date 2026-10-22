#!/usr/bin/env bash
# Direct request-feed regression helpers sourced by ../test.sh.
# The harness defines the shared container, request, and work variables.
# shellcheck disable=SC2154

feed_line_count() { # container path
  podman exec "$1" sh -c "wc -l < '$2'"
}

wait_for_feed_lines() { # container path minimum
  local got=0 minimum=$3
  for _ in $(seq 1 40); do
    got=$(feed_line_count "$1" "$2")
    ((got >= minimum)) && return 0
    sleep 0.25
  done
  echo "$2 has $got lines, want at least $3" >&2
  exit 1
}

assert_private_log_has_no_markers() { # text
  local text=$1 marker
  for marker in pathmarker7e2a querymarker4c9d headermarker8f3b bodymarker1d6c \
    hostmarker5a7e agentmarker2b4f 198.51.100.77 203.0.113.78; do
    if grep -qF "$marker" <<<"$text"; then
      echo "private-output-forbidden-material" >&2
      exit 1
    fi
  done
}

test_direct_crowdsec_feed() {
  local feed=/var/log/caddy/crowdsec/serving.json before want feed_text logs adapted

  [[ $(podman exec "$name" stat -c %a:%u "$feed") == 600:10001 ]] ||
    { echo "CrowdSec feed is not private to uid 10001" >&2; exit 1; }
  adapted=$(podman exec "$name" caddy adapt --config /etc/caddy/Caddyfile 2>/dev/null)
  grep -q '"crowdsec_serving"' <<<"$adapted" || { echo "adapted config lacks the serving feed logger" >&2; exit 1; }
  grep -q '"filename":"/var/log/caddy/crowdsec/serving.json"' <<<"$adapted" ||
    { echo "adapted config has the wrong serving feed path" >&2; exit 1; }
  grep -q '"roll_size_mb":1' <<<"$adapted" || { echo "adapted config lacks the 1 MiB feed cap" >&2; exit 1; }
  grep -q '"roll_keep":1' <<<"$adapted" || { echo "adapted config lacks the one-roll cap" >&2; exit 1; }
  grep -q '"roll_gzip":false' <<<"$adapted" || { echo "adapted config compresses feed rolls" >&2; exit 1; }
  grep -q '"http.log.access.crowdsec_serving"' <<<"$adapted" ||
    { echo "adapted default logger does not exclude the serving feed" >&2; exit 1; }
  grep -q '"http.log.error.crowdsec_serving"' <<<"$adapted" ||
    { echo "adapted config does not isolate direct HTTP errors" >&2; exit 1; }
  grep -q '"direct_unmatched_serving"' <<<"$adapted" ||
    { echo "adapted config lacks the safe unmatched-host logger" >&2; exit 1; }
  grep -q '"direct_tls_serving"' <<<"$adapted" ||
    { echo "adapted config lacks the safe TLS logger" >&2; exit 1; }

  before=$(feed_line_count "$name" "$feed")
  # The web upstream returns a real denial so only this unknown path may be
  # sampled. Its separate listener leaves the existing Go stand-in intact.
  podman exec "$name-echo" sh -c \
    'printf "{\n admin off\n}\n:3000 {\n respond 404\n}\n" >/tmp/feed-upstream'
  podman exec -d "$name-echo" caddy run --config /tmp/feed-upstream --adapter caddyfile
  for _ in $(seq 1 40); do
    if podman exec "$name-echo" wget -q -O /dev/null http://127.0.0.1:3000/ 2>/dev/null; then
      break
    fi
    # wget returns failure for the expected denial; prove the listener exists.
    if podman exec "$name-echo" sh -c 'grep -q ":0BB8 " /proc/net/tcp /proc/net/tcp6'; then
      break
    fi
    sleep 0.25
  done
  dcurl -o /dev/null -H 'User-Agent: agentmarker2b4f' -H 'X-Probe: headermarker8f3b' \
    -H 'X-Forwarded-For: 198.51.100.77' -H 'X-Real-IP: 203.0.113.78' \
    -H 'CloudFront-Viewer-Address: 198.51.100.77:4444' -H 'Forwarded: for=203.0.113.78' \
    -H 'Content-Type: text/plain' --data 'bodymarker1d6c' \
    "https://aboutme.vn:$dport/api/pathmarker7e2a?token=querymarker4c9d" >/dev/null
  for path in /_nuxt/pathmarker7e2a.js /app/pathmarker7e2a /mcp/pathmarker7e2a \
    /resume-pathmarker \
    /.well-known/acme-challenge/pathmarker7e2a; do
    dcurl -o /dev/null "https://aboutme.vn:$dport$path?token=querymarker4c9d" || true
  done
  [[ $(dcurl --path-as-is -o /dev/null -w '%{http_code}' \
    "https://aboutme.vn:$dport/unknown/eligible%2fmarker?token=querymarker4c9d") == 404 ]] ||
    { echo "eligible-target-status" >&2; exit 1; }

  # The earlier IPv4 proof added this namespace-local name. Move that name to
  # IPv6 so wget sends aboutme.vn as both TLS SNI and the HTTP Host while Caddy
  # sees the socket peer as ::1.
  podman exec --user 0 "$name-echo" sh -c \
    'sed "s/^127\.0\.0\.1 aboutme\.vn$/::1 aboutme.vn/" /etc/hosts >/tmp/hosts && cat /tmp/hosts >/etc/hosts'
  podman exec "$name-echo" wget -q -O /dev/null --no-check-certificate \
    https://aboutme.vn/healthz
  timeout 5 openssl s_client -connect "127.0.0.1:$dport" \
    -servername hostmarker5a7e.example -alpn acme-tls/1 </dev/null >/dev/null 2>&1 || true

  want=$((before + 8))
  wait_for_feed_lines "$name" "$feed" "$want"
  feed_text=$(podman exec "$name" cat "$feed")
  for class in api static auth mcp public unknown system; do
    grep -q '"route_class":"'"$class"'"' <<<"$feed_text" ||
      { echo "CrowdSec feed lacks route class $class" >&2; exit 1; }
  done
  grep -q '"source_ip":"::1"' <<<"$feed_text" || { echo "CrowdSec feed lacks canonical IPv6" >&2; exit 1; }
  if grep -qE '"(uri|path|query|headers|body|host|port|tls|duration|size|bytes_read|user_id)"' <<<"$feed_text"; then
    echo "CrowdSec feed contains a forbidden field" >&2
    exit 1
  fi
  if grep -Evq '^\{"ts":"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z","source_ip":"[0-9a-f:.]+","method":"(GET|HEAD|POST|PUT|PATCH|DELETE|OPTIONS|OTHER)","status":[1-5][0-9]{2},"route_class":"(system|static|auth|api|mcp|public|unknown|maintenance)","probe_class":"(none|invalid|oversized|traversal|secret-config|repository|web-shell|framework|other)","target_path":"[^"[:cntrl:]]*"\}$' <<<"$feed_text"; then
    echo "feed-seven-field-interface" >&2
    exit 1
  fi
  if grep -q '"roll_minutes"' <<<"$adapted"; then
    echo "feed-timed-rotation-enabled" >&2
    exit 1
  fi
  [[ $(grep -cF '"target_path":"/unknown/eligible%2fmarker"' <<<"$feed_text") == 1 ]] ||
    { echo "feed-exact-eligible-target" >&2; exit 1; }
  if grep -F 'eligible%2fmarker' <<<"$feed_text" | grep -vqF \
    '"route_class":"unknown","probe_class":"other","target_path":"/unknown/eligible%2fmarker"'; then
    echo "feed-eligible-target-combination" >&2
    exit 1
  fi
  assert_private_log_has_no_markers "$feed_text"
  test_crowdsec_feed_size_roll

  # A wrong Host and a stopped upstream force Caddy error paths while every
  # request surface carries a distinct marker.
  dcurl -o /dev/null -H 'Host: hostmarker5a7e.example' \
    "https://aboutme.vn:$dport/pathmarker7e2a?token=querymarker4c9d" || true
  podman stop "$name-echo" >/dev/null
  dcurl -o /dev/null -H 'User-Agent: agentmarker2b4f' -H 'X-Probe: headermarker8f3b' \
    -H 'Content-Type: text/plain' --data 'bodymarker1d6c' \
    "https://aboutme.vn:$dport/api/pathmarker7e2a?token=querymarker4c9d" || true
  sleep 1
  logs=$(podman logs "$name" 2>&1)
  grep -q '"msg":"http_error"' <<<"$logs" || { echo "direct error did not use safe metadata" >&2; exit 1; }
  grep -q '"msg":"tls_event"' <<<"$logs" || { echo "ACME TLS error did not use safe metadata" >&2; exit 1; }
  assert_private_log_has_no_markers "$logs"
  if grep -qE 'eligible%2[fF]marker|"(uri|request_uri|target_path)"' <<<"$logs"; then
    echo "runtime-error-request-material" >&2
    exit 1
  fi
}

test_crowdsec_feed_size_roll() {
  local target rolls=0 active_size roll_size statuses=$work/feed-roll-status
  target=/unknown/$(printf '%0499d' 0)'[0001-3600]'
  # Two size rotations prove the writer retains only one uncompressed roll.
  # One client avoids per-request helper processes. The four-digit expansion
  # keeps every target at 512 bytes; each expanded response goes to /dev/null.
  timeout 120s curl --disable --silent --fail-early --max-time 10 \
    --noproxy '*' --resolve "aboutme.vn:$dport:127.0.0.1" --cacert "$work/direct-ca.pem" \
    --output /dev/null --write-out '%{http_code}\n' "https://aboutme.vn:$dport$target" \
    >"$statuses" 2>/dev/null || { echo "feed-size-roll-requests" >&2; exit 1; }
  [[ $(wc -l <"$statuses") == 3600 ]] || { echo "feed-size-roll-response-count" >&2; exit 1; }
  if grep -qvx '404' "$statuses"; then
    echo "feed-size-roll-response-status" >&2
    exit 1
  fi
  for _ in $(seq 1 40); do
    rolls=$(podman exec "$name" sh -c '
      count=0
      for file in /var/log/caddy/crowdsec/serving-*-size.json; do
        [ ! -f "$file" ] || count=$((count + 1))
      done
      printf "%s" "$count"
    ')
    [[ $rolls == 1 ]] && break
    sleep 0.25
  done
  [[ $rolls == 1 ]] || { echo "feed-size-roll-count" >&2; exit 1; }
  active_size=$(podman exec "$name" stat -c %s /var/log/caddy/crowdsec/serving.json)
  roll_size=$(podman exec "$name" sh -c 'stat -c %s /var/log/caddy/crowdsec/serving-*-size.json')
  ((active_size > 0 && active_size <= 1048576 && roll_size > 0 && roll_size <= 1048576)) ||
    { echo "feed-size-roll-bound" >&2; exit 1; }
  if podman exec "$name" sh -c 'test -n "$(find /var/log/caddy/crowdsec -name "serving-*.gz" -print -quit)"'; then
    echo "feed-size-roll-compression" >&2
    exit 1
  fi
}

test_crowdsec_bouncer_error_log() {
  local bouncer_log=/var/log/caddy/crowdsec/serving-bouncer.json contents logs

  for _ in $(seq 1 40); do
    contents=$(podman exec "$name" cat "$bouncer_log" 2>/dev/null || true)
    [[ -n $contents ]] && break
    sleep 0.5
  done
  [[ -n $contents ]] || { echo "forced CrowdSec error produced no private log" >&2; exit 1; }
  grep -q '"level":"error","msg":"crowdsec_event"' <<<"$contents" ||
    { echo "forced CrowdSec error did not use safe metadata" >&2; exit 1; }
  [[ $(podman exec "$name" stat -c %a:%u "$bouncer_log") == 600:10001 ]] ||
    { echo "CrowdSec bouncer log is not private to uid 10001" >&2; exit 1; }
  logs=$(podman logs "$name" 2>&1)
  assert_private_log_has_no_markers "$contents$logs"
  if grep -qE '198\.51\.100\.77|203\.0\.113\.78|127\.0\.0\.1:8099' <<<"$contents$logs"; then
    echo "CrowdSec error leaked an address" >&2
    exit 1
  fi
}
