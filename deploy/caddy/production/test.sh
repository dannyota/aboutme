#!/usr/bin/env bash
# Builds the production Caddy image and checks route rendering, each edge
# listener's client certificate trust and client address rule (EDGES), that
# the origin key does not stay in the process environment, and maintenance
# mode's page, headers, and CSP hashes. See docs/design/cloudfront-edge.md.
set -euo pipefail
root=$(git rev-parse --show-toplevel)
work=$(mktemp -d)
name=aboutme-caddy-test
port=20451
cf_port=20452
trap 'podman rm -f --ignore --depend "$name" >/dev/null 2>&1 || true; rm -rf "$work"' EXIT

render=$root/deploy/caddy/production/render.sh
routes=$(bash "$render" "$root/deploy/caddy/public-roots.generated.caddy")
grep -q 'reverse_proxy 127.0.0.1:8080' <<<"$routes"
grep -q 'reverse_proxy 127.0.0.1:3000' <<<"$routes"
grep -q 'header_up X-Real-IP {vars.client_address}' <<<"$routes"
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

# Each of the following starts from the real page with exactly one line
# changed, so a passing build stays honest about what the real page contains.
reject() { # label -> assert maintenance-render rejects $work/bad.html
  if bash "$maintenance_render" "$work/bad.html" >/dev/null 2>&1; then
    echo "maintenance-render accepted a page with $1" >&2
    exit 1
  fi
}
sed '/aboutme:maintenance/d' "$maintenance_html" >"$work/bad.html"
reject "no aboutme:maintenance marker"
sed '0,/<style>/s//<link rel="stylesheet" href="x.css"><style>/' "$maintenance_html" >"$work/bad.html"
reject "a <link> tag"
sed '0,/<body>/s//<body style="color:red">/' "$maintenance_html" >"$work/bad.html"
reject "a style attribute"
sed 's#<title>#<!-- https://evil.example --><title>#' "$maintenance_html" >"$work/bad.html"
reject "an absolute URL"
sed '0,/aria-label="aboutme"/s//aria-label="aboutme" href="logo.svg"/' "$maintenance_html" >"$work/bad.html"
reject "a non-fragment href"
sed '0,/fill="url(#lf)"/s//fill="url(logo.png)"/' "$maintenance_html" >"$work/bad.html"
reject "a non-fragment css url()"
bash "$maintenance_render" "$maintenance_html" >/dev/null ||
  { echo "maintenance-render rejected the real maintenance page" >&2; exit 1; }

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
# The CloudFront origin mTLS CA and its client certificate, a separate trust
# pool from the Cloudflare origin-pull CA above.
openssl req -x509 "${ec[@]}" -days 1 -subj /CN=cloudfront-ca \
  -keyout "$work/cf-ca.key" -out "$work/cf-ca.pem" 2>/dev/null
openssl req "${ec[@]}" -subj /CN=cloudfront -keyout "$work/cf.key" -out "$work/cf.csr" 2>/dev/null
printf '%s\n' extendedKeyUsage=clientAuth >"$work/cf.ext"
openssl x509 -req -in "$work/cf.csr" -CA "$work/cf-ca.pem" -CAkey "$work/cf-ca.key" \
  -CAcreateserial -days 1 -extfile "$work/cf.ext" -out "$work/cf.pem" 2>/dev/null
openssl req -x509 "${ec[@]}" -days 1 -subj /CN=other \
  -keyout "$work/other.key" -out "$work/other.pem" 2>/dev/null

image=localhost/aboutme/caddy:test
tls_env=(-e ORIGIN_CERT="$(cat "$work/origin.pem")" -e ORIGIN_KEY="$(cat "$work/origin.key")")
pull_ca_env=(-e ORIGIN_PULL_CA="$(cat "$work/ca.pem")")
cf_ca_env=(-e CLOUDFRONT_CLIENT_CA="$(cat "$work/cf-ca.pem")")

wait_started() { # container... -> waits for each to serve its configuration
  local c logs
  for c in "$@"; do
    for _ in $(seq 1 20); do
      logs=$(podman logs "$c" 2>&1 || true)
      grep -q 'serving initial configuration' <<<"$logs" && continue 2
      sleep 0.5
    done
    echo "$c did not start" >&2
    printf '%s\n' "$logs" >&2
    exit 1
  done
}

start_stack() { # podman run options...
  podman rm -f --ignore --depend "$name" >/dev/null 2>&1 || true
  podman run -d --name "$name" -p "127.0.0.1:$port:443" -p "127.0.0.1:$cf_port:8443" \
    --tmpfs /run/caddy "${tls_env[@]}" "$@" "$image" >/dev/null
  # A stand-in for Go on 127.0.0.1:8080 that echoes the forwarded client
  # address and any CloudFront-Viewer-Address that reached it.
  podman run -d --name "$name-echo" --network "container:$name" --entrypoint sh "$image" -c \
    'printf "{\n\tadmin off\n}\n:8080 {\n\trespond \"ip={http.request.header.X-Real-IP} cfva={http.request.header.CloudFront-Viewer-Address}\"\n}\n" >/tmp/echo && exec caddy run --config /tmp/echo --adapter caddyfile' \
    >/dev/null
  wait_started "$name" "$name-echo"
}

# ---- Default edges: EDGES unset serves only the Cloudflare listener ----
start_stack "${pull_ca_env[@]}" -e CLOUDFLARE_RANGES="192.0.2.0/24 198.51.100.0/24"

request() {
  curl -s -o /dev/null -w '%{http_code}' --resolve "aboutme.vn:$port:127.0.0.1" \
    --resolve "www.aboutme.vn:$port:127.0.0.1" --cacert "$work/origin.pem" "$@"
}
cf_request() {
  curl -s -o /dev/null -w '%{http_code}' --resolve "aboutme.vn:$cf_port:127.0.0.1" \
    --resolve "www.aboutme.vn:$cf_port:127.0.0.1" --cacert "$work/origin.pem" "$@"
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
cf=(--cert "$work/cf.pem" --key "$work/cf.key")
code=$(request "${pull[@]}" "https://aboutme.vn:$port/healthz" || true)
[[ $code == 200 ]] || { echo "healthz: want 200 from the stand-in, got $code" >&2; exit 1; }
code=$(request "${pull[@]}" "https://aboutme.vn:$port/print/x" || true)
[[ $code == 404 ]] || { echo "print: want 404, got $code" >&2; exit 1; }
code=$(request "${pull[@]}" "https://www.aboutme.vn:$port/a" || true)
[[ $code == 301 ]] || { echo "www: want 301, got $code" >&2; exit 1; }
if cf_request "${cf[@]}" "https://aboutme.vn:$cf_port/healthz" >/dev/null 2>&1; then
  echo "EDGES unset: the CloudFront listener answered" >&2
  exit 1
fi
# The Cloudflare listener keeps its responses as they were: Caddy adds no
# HSTS there and keeps its Server header.
curl -s -o /dev/null -D "$work/cfl.headers" --resolve "aboutme.vn:$port:127.0.0.1" \
  --cacert "$work/origin.pem" "${pull[@]}" "https://aboutme.vn:$port/healthz"
if grep -qi '^strict-transport-security:' "$work/cfl.headers"; then
  echo "Cloudflare listener: Caddy added HSTS" >&2
  exit 1
fi
grep -qi '^server: caddy' "$work/cfl.headers" || { echo "Cloudflare listener: Server header changed" >&2; exit 1; }
if podman exec "$name" sh -c 'tr "\0" "\n" </proc/1/environ | grep -q "^ORIGIN_KEY="'; then
  echo "ORIGIN_KEY remains in the Caddy process environment" >&2
  exit 1
fi
podman exec "$name" sh -c 'test "$(stat -c %a /run/caddy/origin-key.pem)" = 600'
forwarded() {
  curl -s --resolve "aboutme.vn:$port:127.0.0.1" --cacert "$work/origin.pem" "${pull[@]}" \
    -H 'CF-Connecting-IP: 203.0.113.7' -H 'X-Real-IP: 6.6.6.6' -H 'X-Forwarded-For: 5.5.5.5' \
    -H 'CloudFront-Viewer-Address: 198.51.100.9:4000' "https://aboutme.vn:$port/api/v1/probe"
}
got=$(forwarded)
if [[ $got != ip=* || $got == *203.0.113.7* || $got == *6.6.6.6* || $got == *5.5.5.5* || $got == 'ip= '* ||
  $got == ip=198.51.100.9* ]]; then
  echo "untrusted peer: Go received '$got', want the peer address" >&2
  exit 1
fi

# EDGES unset with the local peer trusted: CF-Connecting-IP reaches Go, as
# it does in production before any edge change.
start_stack "${pull_ca_env[@]}" \
  -e CLOUDFLARE_RANGES="10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 127.0.0.0/8 ::1/128 fc00::/7"
got=$(forwarded)
[[ $got == ip=203.0.113.7\ * ]] || { echo "EDGES unset, trusted peer: Go received '$got'" >&2; exit 1; }

# ---- Both edges; the Cloudflare listener trusts the local peer ----
# Trust the local peer, which is on a private network, but not the visitor
# address. Strict mode returns the first untrusted address in the header.
start_stack "${pull_ca_env[@]}" "${cf_ca_env[@]}" -e EDGES=cloudflare,cloudfront \
  -e CLOUDFLARE_RANGES="10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 127.0.0.0/8 ::1/128 fc00::/7"
got=$(forwarded)
[[ $got == ip=203.0.113.7\ * ]] || { echo "trusted peer: Go received '$got', want ip=203.0.113.7" >&2; exit 1; }

# Each listener trusts only its own edge's client certificate.
if request "${cf[@]}" "https://aboutme.vn:$port/healthz" >/dev/null 2>&1; then
  echo "Cloudflare listener accepted the CloudFront client certificate" >&2
  exit 1
fi
for bad in "" "--cert $work/pull.pem --key $work/pull.key" "--cert $work/other.pem --key $work/other.key"; do
  # shellcheck disable=SC2086 # $bad is deliberately split into curl options.
  if cf_request $bad "https://aboutme.vn:$cf_port/healthz" >/dev/null 2>&1; then
    echo "CloudFront listener accepted a request with client certificate options '$bad'" >&2
    exit 1
  fi
done
code=$(cf_request "${cf[@]}" "https://aboutme.vn:$cf_port/healthz" || true)
[[ $code == 200 ]] || { echo "CloudFront healthz: want 200, got $code" >&2; exit 1; }
code=$(cf_request "${cf[@]}" "https://aboutme.vn:$cf_port/print/x" || true)
[[ $code == 404 ]] || { echo "CloudFront print: want 404, got $code" >&2; exit 1; }
curl -s -o /dev/null -D "$work/www.headers" --resolve "www.aboutme.vn:$cf_port:127.0.0.1" \
  --cacert "$work/origin.pem" "${cf[@]}" "https://www.aboutme.vn:$cf_port/a?b=1"
grep -q '^HTTP/[0-9.]* 301' "$work/www.headers" || { echo "CloudFront www: want 301" >&2; exit 1; }
grep -qi '^location: https://aboutme.vn/a?b=1' "$work/www.headers" ||
  { echo "CloudFront www: want a redirect to the apex, same path and query" >&2; exit 1; }

# Response headers on the CloudFront listener: HSTS and nosniff added when
# the upstream sent none, Server removed (the stand-in upstream sends one).
curl -s -o /dev/null -D "$work/cf.headers" --resolve "aboutme.vn:$cf_port:127.0.0.1" \
  --cacert "$work/origin.pem" "${cf[@]}" "https://aboutme.vn:$cf_port/healthz"
grep -qi '^strict-transport-security: max-age=31536000'$'\r''$' "$work/cf.headers" ||
  { echo "CloudFront listener: want HSTS max-age=31536000" >&2; exit 1; }
grep -qi '^x-content-type-options: nosniff' "$work/cf.headers" ||
  { echo "CloudFront listener: want X-Content-Type-Options nosniff" >&2; exit 1; }
if grep -qi '^server:' "$work/cf.headers"; then
  echo "CloudFront listener: Server header present" >&2
  exit 1
fi

viewer() { # CloudFront-Viewer-Address values... -> what Go receives
  local args=() v
  for v in "$@"; do args+=(-H "CloudFront-Viewer-Address: $v"); done
  curl -s --resolve "aboutme.vn:$cf_port:127.0.0.1" --cacert "$work/origin.pem" "${cf[@]}" \
    -H 'CF-Connecting-IP: 203.0.113.50' -H 'X-Real-IP: 6.6.6.6' -H 'X-Forwarded-For: 5.5.5.5' \
    -H 'Forwarded: for=4.4.4.4' -H 'X-Forwarded-Host: evil.example' \
    "${args[@]}" "https://aboutme.vn:$cf_port/api/v1/probe"
}
want_viewer() { # want-ip values...
  local want=$1 got
  shift
  got=$(viewer "$@")
  [[ $got == "ip=$want cfva=" ]] ||
    { echo "CloudFront-Viewer-Address '$*': Go received '$got', want 'ip=$want cfva='" >&2; exit 1; }
}
want_viewer 203.0.113.7 203.0.113.7:46532
want_viewer 2001:db8:0:0:0:0:0:1 2001:db8:0:0:0:0:0:1:60776
want_viewer 2001:db8::1 2001:db8::1:443
# Missing, repeated, or malformed: no address reaches Go, and no forged
# forwarding header takes its place.
want_viewer ''
want_viewer '' 203.0.113.7:1 198.51.100.9:2
want_viewer '' '203.0.113.7:1, 198.51.100.9:2'
want_viewer '' 203.0.113.7
want_viewer '' '[2001:db8::1]:443'
want_viewer '' 203.0.113.7:123456
want_viewer '' 'evil.example:80'
want_viewer '' '{env.ORIGIN_KEY}:1'
want_viewer '' ''
# Cloudflare's header means nothing on the CloudFront listener.
got=$(viewer)
[[ $got != *203.0.113.50* ]] || { echo "CloudFront listener trusted CF-Connecting-IP: '$got'" >&2; exit 1; }

# Nothing listens on the web upstream here, so reverse_proxy logs the failed
# request. Caddy's logs must keep the method and URI but never the visitor's
# address or request headers, on either listener.
curl -s -o /dev/null --resolve "aboutme.vn:$port:127.0.0.1" --cacert "$work/origin.pem" "${pull[@]}" \
  -H 'CF-Connecting-IP: 203.0.113.7' -H 'User-Agent: privacy-probe-agent' \
  -H 'Referer: https://referrer.example/privacy-probe' "https://aboutme.vn:$port/nested/privacy-probe" || true
curl -s -o /dev/null --resolve "aboutme.vn:$cf_port:127.0.0.1" --cacert "$work/origin.pem" "${cf[@]}" \
  -H 'CloudFront-Viewer-Address: 203.0.113.8:5555' -H 'User-Agent: privacy-probe-agent' \
  -H 'Referer: https://referrer.example/privacy-probe' "https://aboutme.vn:$cf_port/nested/privacy-probe-cf" || true
logs=$(podman logs "$name" 2>&1)
grep -q '"uri":"/nested/privacy-probe"' <<<"$logs" || { echo "no proxy log line for the probe request" >&2; exit 1; }
grep -q '"uri":"/nested/privacy-probe-cf"' <<<"$logs" ||
  { echo "no proxy log line for the CloudFront probe request" >&2; exit 1; }
grep -q '"method":"GET"' <<<"$logs" || { echo "proxy log lost the method" >&2; exit 1; }
for leak in client_ip remote_ip remote_port '"headers"' 203.0.113.7 203.0.113.8 privacy-probe-agent \
  referrer.example Cf-Connecting-Ip Cloudfront-Viewer-Address; do
  if grep -qF -- "$leak" <<<"$logs"; then
    echo "Caddy log contains $leak" >&2
    exit 1
  fi
done
# Both edges reach the origin over TCP only, so no HTTP/3 listener starts.
if grep -i 'enabling HTTP/3 listener' <<<"$logs" >&2; then
  echo "Caddy enabled HTTP/3" >&2
  exit 1
fi

# ---- CloudFront only: no Cloudflare listener, no Cloudflare settings needed ----
start_stack "${cf_ca_env[@]}" -e EDGES=cloudfront
if request "${pull[@]}" "https://aboutme.vn:$port/healthz" >/dev/null 2>&1; then
  echo "EDGES=cloudfront: the Cloudflare listener answered" >&2
  exit 1
fi
want_viewer 203.0.113.7 203.0.113.7:46532

# ---- EDGES and its settings are validated before Caddy starts ----
refuse() { # label podman run options...
  local label=$1 status
  shift
  set +e
  timeout 30 podman run --rm --name "$name-refuse" --tmpfs /run/caddy "${tls_env[@]}" "$@" "$image" \
    >/dev/null 2>"$work/refuse.err"
  status=$?
  set -e
  podman rm -f --ignore "$name-refuse" >/dev/null 2>&1 || true
  # 124 is timeout's own status: Caddy started, so the input was accepted.
  if ((status == 0 || status == 124)); then
    echo "entrypoint accepted $label (status $status)" >&2
    exit 1
  fi
  # The entrypoint's own check must be what refused, not a later Caddy error.
  grep -qE '^aboutme-caddy: |parameter (not set|null)' "$work/refuse.err" ||
    { echo "entrypoint did not name the problem for $label:" >&2; cat "$work/refuse.err" >&2; exit 1; }
}
both=("${pull_ca_env[@]}" "${cf_ca_env[@]}" -e CLOUDFLARE_RANGES=192.0.2.0/24)
refuse "an empty EDGES" "${both[@]}" -e EDGES=
refuse "an unknown edge" "${both[@]}" -e EDGES=cloudflare,bogus
refuse "a repeated edge" "${both[@]}" -e EDGES=cloudfront,cloudfront
refuse "a trailing comma" "${both[@]}" -e EDGES=cloudflare,
refuse "a space" "${both[@]}" -e 'EDGES=cloudflare, cloudfront'
refuse "an uppercase edge" "${both[@]}" -e EDGES=CloudFront
refuse "cloudfront without its CA" "${pull_ca_env[@]}" -e CLOUDFLARE_RANGES=192.0.2.0/24 -e EDGES=cloudflare,cloudfront
refuse "cloudflare without its CA" "${cf_ca_env[@]}" -e CLOUDFLARE_RANGES=192.0.2.0/24 -e EDGES=cloudflare,cloudfront
refuse "cloudflare without its ranges" "${pull_ca_env[@]}" "${cf_ca_env[@]}" -e EDGES=cloudflare

# The release smoke test runs the same adapt mode without TLS material.
for edges in cloudflare cloudfront cloudflare,cloudfront; do
  podman run --rm -e EDGES="$edges" -e CLOUDFLARE_RANGES=192.0.2.0/24 "$image" adapt ||
    { echo "adapt failed for EDGES=$edges" >&2; exit 1; }
done

# ---- Maintenance mode: same image, MAINTENANCE=1 selects Caddyfile.maintenance ----
podman rm -f --ignore --depend "$name" >/dev/null 2>&1 || true
podman run -d --name "$name" -p "127.0.0.1:$port:443" -p "127.0.0.1:$cf_port:8443" --tmpfs /run/caddy \
  "${tls_env[@]}" "${pull_ca_env[@]}" "${cf_ca_env[@]}" -e EDGES=cloudflare,cloudfront \
  -e CLOUDFLARE_RANGES="192.0.2.0/24 198.51.100.0/24" -e MAINTENANCE=1 \
  "$image" >/dev/null
wait_started "$name"

# The origin still rejects a direct request with no client certificate, and
# one with a certificate another CA signed, on each listener.
if request "https://aboutme.vn:$port/healthz" >/dev/null 2>&1; then
  echo "maintenance: request without a client certificate succeeded" >&2
  exit 1
fi
if request --cert "$work/other.pem" --key "$work/other.key" "https://aboutme.vn:$port/healthz" >/dev/null 2>&1; then
  echo "maintenance: request with an untrusted client certificate succeeded" >&2
  exit 1
fi
for bad in "" "--cert $work/pull.pem --key $work/pull.key"; do
  # shellcheck disable=SC2086 # $bad is deliberately split into curl options.
  if cf_request $bad "https://aboutme.vn:$cf_port/healthz" >/dev/null 2>&1; then
    echo "maintenance: CloudFront listener accepted client certificate options '$bad'" >&2
    exit 1
  fi
done

maint() { # url [client certificate options] -> writes $work/maint.headers and $work/maint.body, a GET
  local url=$1
  shift
  (($#)) || set -- "${pull[@]}"
  curl -s -m 10 -o "$work/maint.body" -D "$work/maint.headers" \
    --resolve "aboutme.vn:$port:127.0.0.1" --resolve "www.aboutme.vn:$port:127.0.0.1" \
    --resolve "aboutme.vn:$cf_port:127.0.0.1" \
    --cacert "$work/origin.pem" "$@" "$url"
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
  [[ $(grep -ci '^strict-transport-security:' "$work/maint.headers") == 1 ]] ||
    { echo "$1: more than one Strict-Transport-Security header" >&2; exit 1; }
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

# The CloudFront listener serves the same page, keeps the page's own HSTS,
# and removes Server.
maint "https://aboutme.vn:$cf_port/readyz" "${cf[@]}"
check_maint_response "maintenance CloudFront /readyz"
cmp -s "$work/maint.body" "$maintenance_html" ||
  { echo "maintenance CloudFront: served body does not match maintenance.html byte for byte" >&2; exit 1; }
if grep -qi '^server:' "$work/maint.headers"; then
  echo "maintenance CloudFront: Server header present" >&2
  exit 1
fi

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

# Maintenance mode with EDGES unset, as production runs it before any edge
# change: the Cloudflare listener serves the page and 8443 stays closed.
podman rm -f --ignore --depend "$name" >/dev/null 2>&1 || true
podman run -d --name "$name" -p "127.0.0.1:$port:443" -p "127.0.0.1:$cf_port:8443" --tmpfs /run/caddy \
  "${tls_env[@]}" "${pull_ca_env[@]}" -e CLOUDFLARE_RANGES="192.0.2.0/24 198.51.100.0/24" -e MAINTENANCE=1 \
  "$image" >/dev/null
wait_started "$name"
maint "https://aboutme.vn:$port/readyz"
check_maint_response "maintenance, EDGES unset"
if cf_request "${cf[@]}" "https://aboutme.vn:$cf_port/healthz" >/dev/null 2>&1; then
  echo "maintenance, EDGES unset: the CloudFront listener answered" >&2
  exit 1
fi

echo "caddy-prod-test: ok"
