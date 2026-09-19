#!/usr/bin/env bash
# Builds the production Caddy image and checks route rendering, origin-pull
# mTLS, that the origin key does not stay in the process environment, and
# maintenance mode's page, headers, and CSP hashes.
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

maintenance_render=$root/deploy/caddy/production/maintenance-render.sh
maintenance_html=$root/deploy/caddy/production/maintenance.html
printf '<script>a</script><script>b</script><style>c</style>\n' >"$work/bad.html"
if bash "$maintenance_render" "$work/bad.html" >/dev/null 2>&1; then
  echo "maintenance-render accepted a page with two script blocks" >&2
  exit 1
fi
printf '<script>a</script><style>c</style><style>d</style>\n' >"$work/bad.html"
if bash "$maintenance_render" "$work/bad.html" >/dev/null 2>&1; then
  echo "maintenance-render accepted a page with two style blocks" >&2
  exit 1
fi
# Recomputed fresh from the page as it stands on disk right now (a second run
# of the same generator, not the one baked into the image below), so a stale
# image or a hand-edited hash fails this check even though it shares code
# with the build.
want_csp=$(bash "$maintenance_render" "$maintenance_html" |
  grep '^header Content-Security-Policy' | sed -E 's/^header Content-Security-Policy "(.*)"$/\1/')

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

# Nothing listens on the web upstream here, so reverse_proxy logs the failed
# request. Caddy's logs must keep the method and URI but never the visitor's
# address or request headers.
curl -s -o /dev/null --resolve "aboutme.vn:$port:127.0.0.1" --cacert "$work/origin.pem" "${pull[@]}" \
  -H 'CF-Connecting-IP: 203.0.113.7' -H 'User-Agent: privacy-probe-agent' \
  -H 'Referer: https://referrer.example/privacy-probe' "https://aboutme.vn:$port/nested/privacy-probe" || true
logs=$(podman logs "$name" 2>&1)
grep -q '"uri":"/nested/privacy-probe"' <<<"$logs" || { echo "no proxy log line for the probe request" >&2; exit 1; }
grep -q '"method":"GET"' <<<"$logs" || { echo "proxy log lost the method" >&2; exit 1; }
for leak in client_ip remote_ip remote_port '"headers"' 203.0.113.7 privacy-probe-agent referrer.example Cf-Connecting-Ip; do
  if grep -qF -- "$leak" <<<"$logs"; then
    echo "Caddy log contains $leak" >&2
    exit 1
  fi
done
# Cloudflare reaches the origin over TCP only, so no HTTP/3 listener starts.
if grep -i 'enabling HTTP/3 listener' <<<"$logs" >&2; then
  echo "Caddy enabled HTTP/3" >&2
  exit 1
fi

# ---- Maintenance mode: same image, MAINTENANCE=1 selects Caddyfile.maintenance ----
podman rm -f --ignore --depend "$name" >/dev/null 2>&1 || true
podman run -d --name "$name" -p "127.0.0.1:$port:443" --tmpfs /run/caddy \
  -e ORIGIN_CERT="$(cat "$work/origin.pem")" -e ORIGIN_KEY="$(cat "$work/origin.key")" \
  -e ORIGIN_PULL_CA="$(cat "$work/ca.pem")" -e CLOUDFLARE_RANGES="192.0.2.0/24 198.51.100.0/24" \
  -e MAINTENANCE=1 \
  localhost/aboutme/caddy:test >/dev/null
for _ in $(seq 1 20); do
  podman logs "$name" 2>&1 | grep -q 'serving initial configuration' && break
  sleep 0.5
done
podman logs "$name" 2>&1 | grep -q 'serving initial configuration' ||
  { echo "maintenance stack did not start" >&2; exit 1; }

# The origin still rejects a direct request with no client certificate, and
# one with a certificate the origin-pull CA did not sign.
if request "https://aboutme.vn:$port/healthz" >/dev/null 2>&1; then
  echo "maintenance: request without a client certificate succeeded" >&2
  exit 1
fi
if request --cert "$work/other.pem" --key "$work/other.key" "https://aboutme.vn:$port/healthz" >/dev/null 2>&1; then
  echo "maintenance: request with an untrusted client certificate succeeded" >&2
  exit 1
fi

maint() { # url -> writes $work/maint.headers and $work/maint.body, a GET
  curl -s -m 10 -o "$work/maint.body" -D "$work/maint.headers" \
    --resolve "aboutme.vn:$port:127.0.0.1" --resolve "www.aboutme.vn:$port:127.0.0.1" \
    --cacert "$work/origin.pem" "${pull[@]}" "$1"
}
maint_method() { # method url -> writes $work/maint.headers and $work/maint.body
  curl -s -m 10 -X "$1" -o "$work/maint.body" -D "$work/maint.headers" \
    --resolve "aboutme.vn:$port:127.0.0.1" --resolve "www.aboutme.vn:$port:127.0.0.1" \
    --cacert "$work/origin.pem" "${pull[@]}" "$2"
}
maint_head() { # url -> writes $work/maint.headers, $work/maint.size
  # -X HEAD only changes the request line; curl still expects a body up to
  # Content-Length and can hang against a real file_server response. --head
  # tells curl itself not to read one, but then curl's only output *is* the
  # headers, so -o must discard it rather than double up with -D.
  curl -s -m 10 -o /dev/null -D "$work/maint.headers" -w '%{size_download}' --head \
    --resolve "aboutme.vn:$port:127.0.0.1" --resolve "www.aboutme.vn:$port:127.0.0.1" \
    --cacert "$work/origin.pem" "${pull[@]}" "$1" >"$work/maint.size"
}
check_maint_response() { # label
  grep -q '^HTTP/[0-9.]* 503' "$work/maint.headers" || { echo "$1: want 503" >&2; exit 1; }
  grep -qi '^retry-after: 60' "$work/maint.headers" || { echo "$1: missing Retry-After" >&2; exit 1; }
  grep -qi '^cache-control: no-store' "$work/maint.headers" || { echo "$1: missing Cache-Control" >&2; exit 1; }
  grep -qi '^content-type: text/html; charset=utf-8' "$work/maint.headers" || { echo "$1: missing Content-Type" >&2; exit 1; }
  grep -qi '^x-content-type-options: nosniff' "$work/maint.headers" || { echo "$1: missing X-Content-Type-Options" >&2; exit 1; }
  grep -qi '^referrer-policy: no-referrer' "$work/maint.headers" || { echo "$1: missing Referrer-Policy" >&2; exit 1; }
  grep -qi '^strict-transport-security: max-age=63072000; includeSubDomains' "$work/maint.headers" ||
    { echo "$1: missing Strict-Transport-Security" >&2; exit 1; }
  got_csp=$(grep -i '^content-security-policy:' "$work/maint.headers" |
    sed -E 's/^[Cc]ontent-[Ss]ecurity-[Pp]olicy: //' | tr -d '\r')
  [[ $got_csp == "$want_csp" ]] || { echo "$1: CSP does not match the page's recomputed hashes" >&2; exit 1; }
}
for path in / /readyz /api/v1/anything; do
  maint "https://aboutme.vn:$port$path"
  check_maint_response "maintenance $path"
  # file_server serves the file verbatim: the body must match it byte for
  # byte, not just contain a marker. This also proves no Caddy placeholder in
  # the page ({path}, {env.*}, {file.*}, ...) got expanded.
  cmp -s "$work/maint.body" "$maintenance_html" ||
    { echo "maintenance $path: served body does not match maintenance.html byte for byte" >&2; exit 1; }
done

for method in POST PUT PATCH DELETE OPTIONS; do
  maint_method "$method" "https://aboutme.vn:$port/api/v1/anything"
  check_maint_response "maintenance $method"
  cmp -s "$work/maint.body" "$maintenance_html" ||
    { echo "maintenance $method: served body does not match maintenance.html byte for byte" >&2; exit 1; }
done

maint_head "https://aboutme.vn:$port/"
check_maint_response "maintenance HEAD /"
[[ $(cat "$work/maint.size") == 0 ]] ||
  { echo "maintenance HEAD /: unexpected body ($(cat "$work/maint.size") bytes)" >&2; exit 1; }

# www still redirects to the apex in maintenance mode, so a visitor's tab
# keeps polling the same origin across the deploy.
maint "https://www.aboutme.vn:$port/some/path"
grep -q '^HTTP/[0-9.]* 301' "$work/maint.headers" || { echo "maintenance www: want 301" >&2; exit 1; }
grep -qi '^location: https://aboutme.vn/some/path' "$work/maint.headers" ||
  { echo "maintenance www: want a permanent redirect to the apex, same path" >&2; exit 1; }

echo "caddy-prod-test: ok"
