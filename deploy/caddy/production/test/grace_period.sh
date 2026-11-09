# shellcheck shell=bash

test_direct_grace_period() {
  local adapted
  wait_direct 200
  adapted=$(podman exec "$name" caddy adapt --config /etc/caddy/Caddyfile)
  python3 -c '
import json, sys
http = json.load(sys.stdin)["apps"]["http"]
assert http.get("grace_period") == 5_000_000_000, "direct: want a 5s shutdown grace period"
assert http.get("shutdown_delay", 0) == 0, "direct: want no shutdown delay"
' <<<"$adapted"
}
