# shellcheck shell=bash
# The direct edge serves the host's deployment document read-only, also in
# maintenance (docs/design/deployment-transparency/host.md). Sourced by
# test.sh; uses its work, name, dport, and dcurl.
check_deployment_security_headers() {
  for header in 'strict-transport-security: max-age=31536000' \
    'x-content-type-options: nosniff'; do
    grep -qixF "$header"$'\r' "$work/document.headers"
  done
  if grep -qiE '^(server|via):' "$work/document.headers"; then
    echo 'deployment document exposes an edge header' >&2; exit 1
  fi
}

test_direct_deployment_document() {
  # The direct edge serves only the public document, even in maintenance. The
  # directory mount must keep seeing atomic replacements of deployment.json.
  printf '%s\n' '{"marker":"first"}' >"$work/deployment/deployment.json"
  printf 'private\n' >"$work/deployment/private.json"
  chmod 0644 "$work/deployment/"*.json
  if podman exec "$name" touch /srv/deployment/write-test >/dev/null 2>&1; then
    echo 'deployment document mount permits writes' >&2; exit 1
  fi
  dcurl -D "$work/document.headers" -o "$work/document.body" \
    "https://aboutme.vn:$dport/.well-known/deployment.json"
  grep -q '^HTTP/[0-9.]* 200' "$work/document.headers"
  [[ $(cat "$work/document.body") == '{"marker":"first"}' ]]
  for header in 'content-type: application/json' 'cache-control: public, max-age=30' \
    'access-control-allow-origin: *' 'x-content-type-options: nosniff' \
    'strict-transport-security: max-age=31536000'; do
    grep -qixF "$header"$'\r' "$work/document.headers"
  done
  if grep -qiE '^(server|via):' "$work/document.headers"; then
    echo 'deployment document exposes an edge header' >&2; exit 1
  fi
  dcurl --head -D "$work/document.headers" -o /dev/null \
    "https://aboutme.vn:$dport/.well-known/deployment.json"
  grep -q '^HTTP/[0-9.]* 200' "$work/document.headers"
  code=$(dcurl -X POST -D "$work/document.headers" -o /dev/null -w '%{http_code}' \
    "https://aboutme.vn:$dport/.well-known/deployment.json")
  [[ $code == 405 ]]
  check_deployment_security_headers
  grep -qixF 'allow: GET, HEAD'$'\r' "$work/document.headers"
  got=$(dcurl "https://aboutme.vn:$dport/.well-known/private.json")
  [[ $got != private ]]
  printf '%s\n' '{"marker":"second"}' >"$work/deployment/new"
  chmod 0644 "$work/deployment/new"
  mv "$work/deployment/new" "$work/deployment/deployment.json"
  [[ $(dcurl "https://aboutme.vn:$dport/.well-known/deployment.json") == '{"marker":"second"}' ]]
  rm "$work/deployment/deployment.json"
  code=$(dcurl -D "$work/document.headers" -o /dev/null -w '%{http_code}' \
    "https://aboutme.vn:$dport/.well-known/deployment.json")
  [[ $code == 404 ]]
  check_deployment_security_headers
}
