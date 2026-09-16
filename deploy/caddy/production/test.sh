#!/usr/bin/env bash
# Builds the production Caddy image and checks route rendering, origin-pull
# mTLS, and that the origin key does not stay in the process environment.
set -euo pipefail
root=$(git rev-parse --show-toplevel)
work=$(mktemp -d)
name=aboutme-caddy-test
port=20451
trap 'podman rm -f --ignore --depend "$name" >/dev/null 2>&1 || true; rm -rf "$work"' EXIT

render=$root/deploy/caddy/production/render.sh
routes=$(bash "$render" "$root/deploy/caddy/public-roots.generated.caddy")
grep -q 'reverse_proxy 127.0.0.1:8080' <<<"$routes"
grep -q 'reverse_proxy 127.0.0.1:3000' <<<"$routes"
grep -q 'header_up X-Real-IP {client_ip}' <<<"$routes"
if grep -qE 'server:8080|web:3000|remote\.host' <<<"$routes"; then
  echo "render left a Compose token" >&2
  exit 1
fi
printf 'reverse_proxy server:8080 {\n' >"$work/bad.caddy"
if bash "$render" "$work/bad.caddy" >/dev/null 2>&1; then
  echo "render accepted a wrong token count" >&2
  exit 1
fi

podman build -q -f "$root/deploy/caddy/production/Dockerfile" -t localhost/aboutme/caddy:test "$root" >/dev/null

ec=(-newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes)
openssl req -x509 "${ec[@]}" -days 1 -subj /CN=aboutme.vn \
  -addext subjectAltName=DNS:aboutme.vn,DNS:www.aboutme.vn \
  -keyout "$work/origin.key" -out "$work/origin.pem" 2>/dev/null
openssl req -x509 "${ec[@]}" -days 1 -subj /CN=pull-ca \
  -keyout "$work/ca.key" -out "$work/ca.pem" 2>/dev/null
openssl req "${ec[@]}" -subj /CN=pull -keyout "$work/pull.key" -out "$work/pull.csr" 2>/dev/null
openssl x509 -req -in "$work/pull.csr" -CA "$work/ca.pem" -CAkey "$work/ca.key" \
  -CAcreateserial -days 1 -out "$work/pull.pem" 2>/dev/null
openssl req -x509 "${ec[@]}" -days 1 -subj /CN=other \
  -keyout "$work/other.key" -out "$work/other.pem" 2>/dev/null

start_stack() { # trusted ranges
  podman rm -f --ignore --depend "$name" >/dev/null 2>&1 || true
  podman run -d --name "$name" -p "127.0.0.1:$port:443" --tmpfs /run/caddy \
    -e ORIGIN_CERT="$(cat "$work/origin.pem")" -e ORIGIN_KEY="$(cat "$work/origin.key")" \
    -e ORIGIN_PULL_CA="$(cat "$work/ca.pem")" -e CLOUDFLARE_RANGES="$1" \
    localhost/aboutme/caddy:test >/dev/null
  # A stand-in for Go on 127.0.0.1:8080 that echoes the forwarded client IP.
  podman run -d --name "$name-echo" --network "container:$name" --entrypoint sh \
    localhost/aboutme/caddy:test -c \
    'printf "{\n\tadmin off\n}\n:8080 {\n\trespond \"ip={http.request.header.X-Real-IP}\"\n}\n" >/tmp/echo && exec caddy run --config /tmp/echo --adapter caddyfile' \
    >/dev/null
  for _ in $(seq 1 20); do
    podman logs "$name" 2>&1 | grep -q 'serving initial configuration' &&
      podman logs "$name-echo" 2>&1 | grep -q 'serving initial configuration' && return
    sleep 0.5
  done
  echo "stack did not start" >&2
  exit 1
}
start_stack "192.0.2.0/24 198.51.100.0/24"

request() {
  curl -s -o /dev/null -w '%{http_code}' --resolve "aboutme.vn:$port:127.0.0.1" \
    --resolve "www.aboutme.vn:$port:127.0.0.1" --cacert "$work/origin.pem" "$@"
}
if request "https://aboutme.vn:$port/healthz" >/dev/null 2>&1; then
  echo "request without a client certificate succeeded" >&2
  exit 1
fi
if request --cert "$work/other.pem" --key "$work/other.key" "https://aboutme.vn:$port/healthz" >/dev/null 2>&1; then
  echo "request with an untrusted client certificate succeeded" >&2
  exit 1
fi
pull=(--cert "$work/pull.pem" --key "$work/pull.key")
code=$(request "${pull[@]}" "https://aboutme.vn:$port/healthz" || true)
[[ $code == 200 ]] || { echo "healthz: want 200 from the stand-in, got $code" >&2; exit 1; }
code=$(request "${pull[@]}" "https://aboutme.vn:$port/print/x" || true)
[[ $code == 404 ]] || { echo "print: want 404, got $code" >&2; exit 1; }
code=$(request "${pull[@]}" "https://www.aboutme.vn:$port/a" || true)
[[ $code == 301 ]] || { echo "www: want 301, got $code" >&2; exit 1; }
if podman exec "$name" sh -c 'tr "\0" "\n" </proc/1/environ | grep -q "^ORIGIN_KEY="'; then
  echo "ORIGIN_KEY remains in the Caddy process environment" >&2
  exit 1
fi
podman exec "$name" sh -c 'test "$(stat -c %a /run/caddy/origin-key.pem)" = 600'
forwarded() {
  curl -s --resolve "aboutme.vn:$port:127.0.0.1" --cacert "$work/origin.pem" "${pull[@]}" \
    -H 'CF-Connecting-IP: 203.0.113.7' -H 'X-Real-IP: 6.6.6.6' -H 'X-Forwarded-For: 5.5.5.5' \
    "https://aboutme.vn:$port/api/v1/probe"
}
got=$(forwarded)
if [[ $got != ip=* || $got == *203.0.113.7* || $got == *6.6.6.6* || $got == *5.5.5.5* || $got == ip= ]]; then
  echo "untrusted peer: Go received '$got', want the peer address" >&2
  exit 1
fi
# Trust the local peer, which is on a private network, but not the visitor
# address. Strict mode returns the first untrusted address in the header.
start_stack "10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 127.0.0.0/8 ::1/128 fc00::/7"
got=$(forwarded)
[[ $got == ip=203.0.113.7 ]] || { echo "trusted peer: Go received '$got', want ip=203.0.113.7" >&2; exit 1; }
echo "caddy-prod-test: ok"
