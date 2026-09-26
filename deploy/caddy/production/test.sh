#!/usr/bin/env bash
# Builds the production Caddy image and checks route rendering, the
# CloudFront listener's client certificate trust and client address rule
# (EDGES), that the origin key does not stay in the process environment, and
# maintenance mode's page, headers, and CSP hashes, and that Caddy runs as its
# non-root user with no capability and binds only 8443. See
# docs/design/cloudfront-edge.md.
set -euo pipefail
root=$(git rev-parse --show-toplevel)
work=$(mktemp -d)
name=aboutme-caddy-test
port=20452
closed_port=20451
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
# Production runs the image's own user; the task definitions set none.
user=$(podman image inspect --format '{{.Config.User}}' localhost/aboutme/caddy:test)
[[ $user == 10001:10001 ]] || { echo "image user is '$user', want 10001:10001" >&2; exit 1; }

ec=(-newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes)
openssl req -x509 "${ec[@]}" -days 1 -subj /CN=aboutme.vn \
  -addext subjectAltName=DNS:aboutme.vn,DNS:www.aboutme.vn \
  -keyout "$work/origin.key" -out "$work/origin.pem" 2>/dev/null
# The CloudFront origin mTLS CA and its client certificate.
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
cf_ca_env=(-e CLOUDFRONT_CLIENT_CA="$(cat "$work/cf-ca.pem")")

# Production runs Caddy with host networking, where the host's
# ip_unprivileged_port_start (default 1024) applies. Podman lowers it inside a
# container's network namespace, so every Caddy here runs with it set back to
# 1024: a Caddy that binds a privileged port fails to start, as it would in
# production.
#
# ECS runs containers with runc, which gives a tmpfs the mode of the image's
# directory (see the Dockerfile), so Caddy runs under runc here too when it is
# installed; CI requires it.
unprivileged=(--sysctl net.ipv4.ip_unprivileged_port_start=1024)
if runc=$(command -v runc); then
  unprivileged+=(--runtime "$runc")
elif [[ ${CI:-} == true ]]; then
  echo "runc is not installed" >&2
  exit 1
fi

# Caddy (PID 1, exec'd by the entrypoint) runs as 10001 with no capability,
# on the tmpfs and under the default privileged-port boundary production
# has, and listens on exactly the given ports (hex, sorted, from
# /proc/net/tcp and tcp6).
check_unprivileged() { # container want-ports
  local status ports
  status=$(podman exec "$1" cat /proc/1/status)
  [[ $(podman exec "$1" cat /proc/1/comm) == caddy ]] || { echo "$1: PID 1 is not caddy" >&2; exit 1; }
  grep -qP '^Uid:\t10001\t10001\t10001\t10001$' <<<"$status" ||
    { echo "$1: Caddy does not run as uid 10001:" >&2; grep '^Uid:' <<<"$status" >&2; exit 1; }
  grep -qP '^Gid:\t10001\t10001\t10001\t10001$' <<<"$status" ||
    { echo "$1: Caddy does not run as gid 10001:" >&2; grep '^Gid:' <<<"$status" >&2; exit 1; }
  for cap in CapInh CapPrm CapEff CapAmb; do
    grep -qP "^$cap:\t0{16}$" <<<"$status" ||
      { echo "$1: Caddy holds a capability:" >&2; grep "^$cap:" <<<"$status" >&2; exit 1; }
  done
  [[ $(podman exec "$1" stat -f -c %T /run/caddy) == tmpfs ]] || { echo "$1: /run/caddy is not a tmpfs" >&2; exit 1; }
  [[ $(podman exec "$1" stat -c %a:%u /run/caddy) == 1777:0 ]] ||
    { echo "$1: /run/caddy is not root-owned 1777" >&2; exit 1; }
  [[ $(podman exec "$1" cat /proc/sys/net/ipv4/ip_unprivileged_port_start) == 1024 ]] ||
    { echo "$1: ip_unprivileged_port_start is not 1024" >&2; exit 1; }
  ports=$(podman exec "$1" cat /proc/net/tcp /proc/net/tcp6 |
    awk '$4 == "0A" { split($2, a, ":"); print a[2] }' | sort -u | paste -sd ' ')
  [[ $ports == "$2" ]] || { echo "$1: listening ports '$ports', want '$2'" >&2; exit 1; }
}

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
  podman run -d --name "$name" -p "127.0.0.1:$port:8443" -p "127.0.0.1:$closed_port:443" \
    --tmpfs /run/caddy "${unprivileged[@]}" "${tls_env[@]}" "$@" "$image" >/dev/null
  # Caddy must be up before the stand-in joins its network namespace, and a
  # Caddy that exits must show its logs.
  wait_started "$name"
  # A stand-in for Go on 127.0.0.1:8080 that echoes the forwarded client
  # address and any CloudFront-Viewer-Address that reached it.
  podman run -d --name "$name-echo" --network "container:$name" --entrypoint sh "$image" -c \
    'printf "{\n\tadmin off\n}\n:8080 {\n\trespond \"ip={http.request.header.X-Real-IP} cfva={http.request.header.CloudFront-Viewer-Address}\"\n}\n" >/tmp/echo && exec caddy run --config /tmp/echo --adapter caddyfile' \
    >/dev/null
  wait_started "$name-echo"
}

request() {
  curl -s -o /dev/null -w '%{http_code}' --resolve "aboutme.vn:$port:127.0.0.1" \
    --resolve "www.aboutme.vn:$port:127.0.0.1" --cacert "$work/origin.pem" "$@"
}

cf=(--cert "$work/cf.pem" --key "$work/cf.key")

# ---- EDGES unset: the CloudFront listener starts, container port 443 answers nothing ----
start_stack "${cf_ca_env[@]}"

if request "https://aboutme.vn:$port/healthz" >/dev/null 2>&1; then
  echo "request without a client certificate succeeded" >&2
  exit 1
fi
if request --cert "$work/other.pem" --key "$work/other.key" "https://aboutme.vn:$port/healthz" >/dev/null 2>&1; then
  echo "request with an untrusted client certificate succeeded" >&2
  exit 1
fi
code=$(request "${cf[@]}" "https://aboutme.vn:$port/healthz" || true)
[[ $code == 200 ]] || { echo "EDGES unset: healthz: want 200 from the stand-in, got $code" >&2; exit 1; }
if curl -s -m 3 -o /dev/null "http://127.0.0.1:$closed_port/" 2>/dev/null; then
  echo "container port 443 answered" >&2
  exit 1
fi
if podman exec "$name" sh -c 'tr "\0" "\n" </proc/1/environ | grep -q "^ORIGIN_KEY="'; then
  echo "ORIGIN_KEY remains in the Caddy process environment" >&2
  exit 1
fi
podman exec "$name" sh -c 'test "$(stat -c %a:%u /run/caddy/origin-key.pem)" = 600:10001'
# 20FB is Caddy's 8443 and 1F90 the stand-in's 8080; nothing listens on 80.
check_unprivileged "$name" "1F90 20FB"

# ---- CloudFront listener: routing, client certificate trust, response headers ----
start_stack "${cf_ca_env[@]}" -e EDGES=cloudfront
code=$(request "${cf[@]}" "https://aboutme.vn:$port/healthz" || true)
[[ $code == 200 ]] || { echo "healthz: want 200, got $code" >&2; exit 1; }
code=$(request "${cf[@]}" "https://aboutme.vn:$port/print/x" || true)
[[ $code == 404 ]] || { echo "print: want 404, got $code" >&2; exit 1; }
curl -s -o /dev/null -D "$work/www.headers" --resolve "www.aboutme.vn:$port:127.0.0.1" \
  --cacert "$work/origin.pem" "${cf[@]}" "https://www.aboutme.vn:$port/a?b=1"
grep -q '^HTTP/[0-9.]* 301' "$work/www.headers" || { echo "www: want 301" >&2; exit 1; }
grep -qi '^location: https://aboutme.vn/a?b=1' "$work/www.headers" ||
  { echo "www: want a redirect to the apex, same path and query" >&2; exit 1; }

# Response headers: HSTS and nosniff added when the upstream sent none,
# Server removed (the stand-in upstream sends one), and no Via naming Caddy
# (reverse_proxy adds "Via: 1.1 Caddy" to every proxied response).
curl -s -o /dev/null -D "$work/resp.headers" --resolve "aboutme.vn:$port:127.0.0.1" \
  --cacert "$work/origin.pem" "${cf[@]}" "https://aboutme.vn:$port/healthz"
grep -qi '^strict-transport-security: max-age=31536000'$'\r''$' "$work/resp.headers" ||
  { echo "want HSTS max-age=31536000" >&2; exit 1; }
grep -qi '^x-content-type-options: nosniff' "$work/resp.headers" ||
  { echo "want X-Content-Type-Options nosniff" >&2; exit 1; }
if grep -qi '^server:' "$work/resp.headers"; then
  echo "Server header present" >&2
  exit 1
fi
if grep -qi '^via:' "$work/resp.headers"; then
  echo "Via header present" >&2
  exit 1
fi

viewer() { # CloudFront-Viewer-Address values... -> what Go receives
  local args=() v
  for v in "$@"; do args+=(-H "CloudFront-Viewer-Address: $v"); done
  curl -s --resolve "aboutme.vn:$port:127.0.0.1" --cacert "$work/origin.pem" "${cf[@]}" \
    -H 'CF-Connecting-IP: 203.0.113.50' -H 'X-Real-IP: 6.6.6.6' -H 'X-Forwarded-For: 5.5.5.5' \
    -H 'Forwarded: for=4.4.4.4' -H 'X-Forwarded-Host: evil.example' \
    "${args[@]}" "https://aboutme.vn:$port/api/v1/probe"
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
# CF-Connecting-IP means nothing on the CloudFront listener.
got=$(viewer)
[[ $got != *203.0.113.50* ]] || { echo "listener trusted CF-Connecting-IP: '$got'" >&2; exit 1; }

# Nothing listens on the web upstream here, so reverse_proxy logs the failed
# request. Caddy's logs must keep the method and URI but never the visitor's
# address or request headers.
curl -s -o /dev/null --resolve "aboutme.vn:$port:127.0.0.1" --cacert "$work/origin.pem" "${cf[@]}" \
  -H 'CloudFront-Viewer-Address: 203.0.113.8:5555' -H 'User-Agent: privacy-probe-agent' \
  -H 'Referer: https://referrer.example/privacy-probe' "https://aboutme.vn:$port/nested/privacy-probe" || true
logs=$(podman logs "$name" 2>&1)
grep -q '"uri":"/nested/privacy-probe"' <<<"$logs" || { echo "no proxy log line for the probe request" >&2; exit 1; }
grep -q '"method":"GET"' <<<"$logs" || { echo "proxy log lost the method" >&2; exit 1; }
for leak in client_ip remote_ip remote_port '"headers"' 203.0.113.8 privacy-probe-agent \
  referrer.example Cf-Connecting-Ip Cloudfront-Viewer-Address; do
  if grep -qF -- "$leak" <<<"$logs"; then
    echo "Caddy log contains $leak" >&2
    exit 1
  fi
done
# The listener reaches the origin over TCP only, so no HTTP/3 listener starts.
if grep -i 'enabling HTTP/3 listener' <<<"$logs" >&2; then
  echo "Caddy enabled HTTP/3" >&2
  exit 1
fi

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
refuse "an empty EDGES" "${cf_ca_env[@]}" -e EDGES=
refuse "an unknown edge" "${cf_ca_env[@]}" -e EDGES=cloudfront,bogus
refuse "a repeated edge" "${cf_ca_env[@]}" -e EDGES=cloudfront,cloudfront
refuse "a trailing comma" "${cf_ca_env[@]}" -e EDGES=cloudfront,
refuse "a space" "${cf_ca_env[@]}" -e 'EDGES=cloudfront, cloudfront'
refuse "an uppercase edge" "${cf_ca_env[@]}" -e EDGES=CloudFront
refuse "an unknown edge value cloudflare" "${cf_ca_env[@]}" -e EDGES=cloudflare
refuse "an unknown edge value cloudflare alongside cloudfront" "${cf_ca_env[@]}" -e EDGES=cloudflare,cloudfront
refuse "cloudfront without its CA" -e EDGES=cloudfront

# The release smoke test runs the same adapt mode without TLS material.
podman run --rm "$image" adapt || { echo "adapt failed for EDGES unset" >&2; exit 1; }
podman run --rm -e EDGES=cloudfront "$image" adapt || { echo "adapt failed for EDGES=cloudfront" >&2; exit 1; }

# ---- Maintenance mode: same image, MAINTENANCE=1 selects Caddyfile.maintenance ----
podman rm -f --ignore --depend "$name" >/dev/null 2>&1 || true
podman run -d --name "$name" -p "127.0.0.1:$port:8443" --tmpfs /run/caddy \
  "${unprivileged[@]}" "${tls_env[@]}" "${cf_ca_env[@]}" -e EDGES=cloudfront -e MAINTENANCE=1 \
  "$image" >/dev/null
wait_started "$name"
check_unprivileged "$name" "20FB"

# The origin still rejects a direct request with no client certificate, and
# one with a certificate another CA signed.
if request "https://aboutme.vn:$port/healthz" >/dev/null 2>&1; then
  echo "maintenance: request without a client certificate succeeded" >&2
  exit 1
fi
if request --cert "$work/other.pem" --key "$work/other.key" "https://aboutme.vn:$port/healthz" >/dev/null 2>&1; then
  echo "maintenance: request with an untrusted client certificate succeeded" >&2
  exit 1
fi

maint() { # url [client certificate options] -> writes $work/maint.headers and $work/maint.body, a GET
  local url=$1
  shift
  (($#)) || set -- "${cf[@]}"
  curl -s -m 10 -o "$work/maint.body" -D "$work/maint.headers" \
    --resolve "aboutme.vn:$port:127.0.0.1" --resolve "www.aboutme.vn:$port:127.0.0.1" \
    --cacert "$work/origin.pem" "$@" "$url"
}
maint_method() { # method url -> writes $work/maint.headers and $work/maint.body
  curl -s -m 10 -X "$1" -o "$work/maint.body" -D "$work/maint.headers" \
    --resolve "aboutme.vn:$port:127.0.0.1" --resolve "www.aboutme.vn:$port:127.0.0.1" \
    --cacert "$work/origin.pem" "${cf[@]}" "$2"
}
maint_head() { # url -> writes $work/maint.headers, $work/maint.size
  # -X HEAD only changes the request line; curl still expects a body up to
  # Content-Length and can hang against a real file_server response. --head
  # tells curl itself not to read one, but then curl's only output *is* the
  # headers, so -o must discard it rather than double up with -D.
  curl -s -m 10 -o /dev/null -D "$work/maint.headers" -w '%{size_download}' --head \
    --resolve "aboutme.vn:$port:127.0.0.1" --resolve "www.aboutme.vn:$port:127.0.0.1" \
    --cacert "$work/origin.pem" "${cf[@]}" "$1" >"$work/maint.size"
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
  if grep -qi '^server:' "$work/maint.headers"; then
    echo "$1: Server header present" >&2
    exit 1
  fi
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
