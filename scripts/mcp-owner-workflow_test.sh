#!/usr/bin/env bash

# Tests scripts/mcp-owner-workflow.sh with fake Git, Go, curl, runner,
# fixture, and browser-container commands in a private temporary tree. It
# starts no stack, container, browser, or network request.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
SCRIPT_SOURCE=$ROOT/scripts/mcp-owner-workflow.sh
T=$(mktemp -d "${TMPDIR:-/tmp}/mcp-owner-workflow-test.XXXXXX")
chmod 0700 "$T"
T=$(cd "$T" && pwd -P)
MAIN=$T/main
REPO=$T/repo
FAKE=$T/bin
CTL=$T/ctl
SCRIPT=$REPO/scripts/mcp-owner-workflow.sh
BROWSER=$REPO/deploy/dev-https-browser
STATE=$REPO/.dev/native-https
CONTROL=$MAIN/.dev/mcp-workflow
ATTESTATION=$CONTROL/sdk-proof-attestation.json
OWNER_CREDENTIAL=$MAIN/.dev/credentials/owner-test.env
OWNER_MARKER=owner-secret-marker-5d1c
CAPTURE_SECRET=capture-secret-marker-91ab
CAPTURE_BEARER=$(printf '%s' "$CAPTURE_SECRET" | base64 -w0 | tr '+/' '-_' | tr -d '=')
COMMIT=0123456789abcdef0123456789abcdef01234567
TAG=v9.9.9
APP=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
WEB=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
IMAGE=sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
FIXED_LINE='^mcp-owner-workflow: [a-z][A-Za-z0-9 .:;,_-]*$'

failures=0
cleanup_test() {
  local pid
  for pid in $(jobs -p); do kill "$pid" 2>/dev/null || true; done
  chmod -R u+rwx "$T" 2>/dev/null || true
  rm -rf -- "$T"
}
trap cleanup_test EXIT

pass() { printf 'ok - %s\n' "$1"; }
bad() {
  printf 'not ok - %s\n' "$1" >&2
  failures=$((failures + 1))
}
check() {
  local label=$1
  shift
  if "$@"; then pass "$label"; else bad "$label"; fi
}
has() { grep -Fq -- "$1" "$2"; }
no_line() { ! grep -Fxq -- "$1" "$2"; }
jqe() { jq -e "$@" >/dev/null; }
lacks() { ! grep -Fq -- "$1" "$2"; }
called() { grep -Eq "$1" "$CTL/calls"; }
not_called() { ! grep -Eq "$1" "$CTL/calls"; }
mode_of() { stat -c %a -- "$1"; }
sha() { sha256sum -- "$1" | cut -c1-64; }
dead() { [ ! -s "$1" ] || ! kill -0 "$(cat "$1")" 2>/dev/null; }
# container_removed proves cleanup force-removed the exact named container.
container_removed() {
  local name
  name=$(grep -oE 'CONTAINER_NAME=aboutme-mcp-sdk-[0-9a-f]{32}' "$CTL/trace" | head -n 1 | cut -d= -f2)
  [ -n "$name" ] && [ "$(grep -c '^podman ' "$CTL/calls")" -eq 1 ] &&
    called "^podman rm -f --ignore $name\$" &&
    { [ ! -s "$CTL/container.args" ] || [ "$(sed -n 9p "$CTL/container.args")" = "$name" ]; }
}

write_fake() {
  sed "s|__CTL__|$CTL|g" >"$1"
  chmod 0755 "$1"
}

make_fakes() {
  mkdir -p "$FAKE" "$CTL/state"
  write_fake "$FAKE/git" <<'EOF'
#!/usr/bin/env bash
ctl=__CTL__
[ "$1" = -C ] && shift 2
case "$*" in
'rev-parse --path-format=absolute --git-common-dir') cat "$ctl/git.common" ;;
'rev-parse --verify HEAD^{commit}') cat "$ctl/git.commit" ;;
'status --porcelain --untracked-files=normal') cat "$ctl/git.status" ;;
'tag --points-at HEAD') cat "$ctl/git.tags" ;;
'rev-parse --verify refs/tags/'*) cat "$ctl/git.tagcommit" ;;
*) exit 99 ;;
esac
EOF
  write_fake "$FAKE/podman" <<'EOF'
#!/usr/bin/env bash
printf 'podman %s\n' "$*" >>__CTL__/calls
EOF
  write_fake "$FAKE/go" <<'EOF'
#!/usr/bin/env bash
ctl=__CTL__
out= target=
while [ "$#" -gt 0 ]; do
  case $1 in
  -o) out=$2; shift 2 ;;
  build | -trimpath | -buildvcs=false) shift ;;
  *) target=$1; shift ;;
  esac
done
printf 'go build %s cgo=%s\n' "$target" "${CGO_ENABLED-unset}" >>"$ctl/calls"
case $target in
./cmd/mcp-workflow) cp -- "$ctl/fake-runner" "$out" ;;
./cmd/password-auth-fixture) cp -- "$ctl/fake-fixture" "$out" ;;
*) exit 99 ;;
esac
chmod 0700 "$out"
EOF
  write_fake "$CTL/fake-fixture" <<'EOF'
#!/usr/bin/env bash
printf 'fixture %s\n' "$1" >>__CTL__/calls
EOF
  write_fake "$CTL/fake-runner" <<'EOF'
#!/usr/bin/env bash
set -u
ctl=__CTL__
terminated() { echo runner-terminated >>"$ctl/calls"; exit 143; }
trap terminated TERM
printf 'runner cwd=%s\n' "$PWD" >>"$ctl/calls"
printf '%s\n' "$$" >"$ctl/runner.pid"
printf '%s\n' "$@" >"$ctl/runner.args"
printf 'SSL_CERT_FILE=%s\nSSL_CERT_DIR=%s\n' "${SSL_CERT_FILE-unset}" "${SSL_CERT_DIR-unset}" >"$ctl/runner.env"
[ "$#" -eq 3 ] || exit 2
mode=$1 run=$2 browser=$3
(cd "$run" && LC_ALL=C ls -A | tr '\n' ' ') >"$ctl/runner.entries"
stat -c %a "$run" "$browser" >"$ctl/runner.modes"
if [ -e "$run/synthetic-login.env" ]; then
  stat -c '%F %a' "$run/synthetic-login.env" >>"$ctl/runner.modes"
  cp -- "$run/synthetic-login.env" "$ctl/synthetic.copy"
fi
wait_for() {
  local i=0
  while [ ! -e "$1" ]; do
    i=$((i + 1))
    [ "$i" -lt 400 ] || return 1
    sleep 0.05
  done
}
publish() {
  local tmp
  tmp=$(mktemp "$1.XXXXXX")
  printf '%s' "$2" >"$tmp"
  chmod 0600 "$tmp"
  mv -f -- "$tmp" "$1"
}
behavior=$(cat "$ctl/runner.behavior")
case $behavior in
fail)
  [ ! -s "$ctl/runner.stderr" ] || cat "$ctl/runner.stderr" >&2
  exit 1
  ;;
fail-with-evidence)
  [ ! -s "$ctl/runner.stderr" ] || cat "$ctl/runner.stderr" >&2
  publish "$run/evidence.json" '{"version":1,"mode":"production","stage":"owner_workflow","sdk_version":"go-sdk v1.7.0","transport":"streamable-http","tool_count":15,"language":"vi","source_unchanged":true,"target_private":true,"count_delta":1,"create_reconciliation":"created","revocation":"revocation_unconfirmed","post_revocation_401":false}'
  exit 1
  ;;
fail-with-source)
  publish "$run/source.json" '{"owner":"content"}'
  publish "$run/candidate.json" '{"owner":"content"}'
  publish "$run/candidate-review.json" '{}'
  exit 1
  ;;
hang) while :; do sleep 0.05; done ;;
sentinel) exit 0 ;;
recovery)
  publish "$run/evidence.json" '{"version":1,"mode":"production","stage":"revocation_recovery","sdk_version":"","transport":"","tool_count":0,"language":"","source_unchanged":false,"target_private":false,"count_delta":0,"create_reconciliation":"","revocation":"revoked","post_revocation_401":false}'
  exit 0
  ;;
esac
wait_for "$browser/browser-ready" || exit 1
[ "$(cat "$browser/browser-ready")" = ready ] || exit 1
publish "$browser/browser-request.json" '{"version":1,"authorization_url":"https://localhost:20443/oauth/authorize?state=request-secret-marker","mode":"local","expected_origin":"https://localhost:20443","expected_login_path":"/login","credential_file":"/mcp-credentials/login.env","result_file":"/mcp-browser/browser-result.json"}'
wait_for "$browser/browser-result.json" || exit 1
[ "$(cat "$browser/browser-result.json")" = completed ] || exit 1
rm -f -- "$browser/browser-ready" "$browser/browser-result.json"
if [ "$mode" = local ]; then
  publish "$run/source.json" '{"personalDetails":{"fullName":"Lan Fixture","headline":"Fictional platform engineer"},"content":{"work":{"displayName":"Experience"}}}'
  wait_for "$run/candidate-review.json" || exit 1
  [ -e "$run/candidate.json" ] || exit 1
  src=$(sha256sum <"$run/source.json" | cut -c1-64)
  cand=$(sha256sum <"$run/candidate.json" | cut -c1-64)
  jq -e --arg s "$src" --arg c "$cand" \
    '. == {source_digest: $s, candidate_digest: $c, facts_preserved: true}' \
    "$run/candidate-review.json" >/dev/null || exit 1
  stat -c %a "$run/candidate.json" "$run/candidate-review.json" >"$ctl/candidate.modes"
  cp -- "$run/candidate.json" "$ctl/candidate.copy"
  rm -f -- "$run/source.json" "$run/candidate.json" "$run/candidate-review.json"
fi
if [ "$behavior" = bad-evidence ]; then
  publish "$run/evidence.json" "{\"version\":1,\"mode\":\"$mode\",\"stage\":\"owner_workflow\",\"extra\":true}"
elif [ "$mode" = local ]; then
  publish "$run/evidence.json" '{"version":1,"mode":"local","stage":"owner_workflow","sdk_version":"go-sdk v1.7.0","transport":"streamable-http","tool_count":15,"language":"vi","source_unchanged":true,"target_private":true,"count_delta":1,"create_reconciliation":"replayed","revocation":"revoked","post_revocation_401":true}'
else
  publish "$run/evidence.json" '{"version":1,"mode":"production","stage":"owner_workflow","sdk_version":"go-sdk v1.7.0","transport":"streamable-http","tool_count":15,"language":"vi","source_unchanged":true,"target_private":true,"count_delta":1,"create_reconciliation":"created","revocation":"revoked","post_revocation_401":true}'
fi
exit 0
EOF
  write_fake "$FAKE/curl" <<'EOF'
#!/usr/bin/env bash
ctl=__CTL__
state=$ctl/state
printf '%s\n' "$*" >>"$ctl/curl.argv"
out= fmt= method= url= data= form= head=0
while [ "$#" -gt 0 ]; do
  case $1 in
  -o) out=$2; shift 2 ;;
  -w) fmt=$2; shift 2 ;;
  -X) method=$2; shift 2 ;;
  --data-binary) data=${2#@}; shift 2 ;;
  -F) form=$2; shift 2 ;;
  -H | -b | -c | --cacert | --max-time | --proto) shift 2 ;;
  -fsSI) head=1; shift ;;
  -*) shift ;;
  *) url=$1; shift ;;
  esac
done
[ -n "$method" ] || { [ -n "$form" ] && method=POST || method=GET; }
path=/${url#*://*/}
printf '%s %s\n' "$method" "$path" >>"$ctl/curl.calls"
respond() {
  [ -z "$out" ] || printf '%s' "$2" >"$out"
  [ -z "$fmt" ] || printf '%s' "$1"
  [ -n "$out" ] || [ -n "$fmt" ] || printf '%s' "$2"
  exit 0
}
behavior=$(cat "$ctl/curl.behavior")
case "$method $url" in
'GET https://ghcr.io/token?scope=repository:dannyota/aboutme-'*':pull&service=ghcr.io')
  respond 200 '{"token":"ghcr-anon-token"}'
  ;;
'GET https://ghcr.io/v2/dannyota/aboutme-'*)
  name=${url#https://ghcr.io/v2/dannyota/aboutme-}
  name=${name%%/*}
  [ "$head" -eq 1 ] && [ "${url##*/}" = "$(cat "$ctl/git.tags")" ] && [ -s "$ctl/ghcr.$name" ] || exit 22
  printf 'HTTP/2 200\r\ndocker-content-digest: %s\r\n\r\n' "$(cat "$ctl/ghcr.$name")"
  exit 0
  ;;
'DELETE http://127.0.0.1:20444/api/messages') exit 0 ;;
'GET http://127.0.0.1:20444/api/messages')
  respond 200 "{\"messages\":[{\"kind\":\"verify\",\"to\":\"$(cat "$state/email")\",\"text_body\":\"Open https://aboutme.vn/verify-email#token=tok123\"}]}"
  ;;
esac
case "$method $path" in
'POST /api/v1/auth/password/register')
  jq -r .email "$data" >"$state/email"
  jq -r .password "$data" >"$state/password"
  respond 202 ''
  ;;
'POST /api/v1/auth/password/verify')
  [ "$(jq -r .token "$data")" = tok123 ] && respond 204 ''
  respond 400 ''
  ;;
'POST /api/v1/auth/password/login')
  if [ "$(jq -r .email "$data")" = "$(cat "$state/email")" ] &&
    [ "$(jq -r .password "$data")" = "$(cat "$state/password")" ]; then
    : >"$state/session"
    respond 204 ''
  fi
  respond 401 ''
  ;;
'GET /api/v1/me') [ -e "$state/session" ] && respond 200 '{"data":{"csrfToken":"csrf-secret-marker"}}' ;;
'POST /api/v1/resumes') respond 201 '{"data":{"id":"src-id","revision":"1"}}' ;;
'POST /api/v1/resumes/src-id/photo')
  printf '%s\n' "$form" >"$ctl/photo.form"
  respond 200 '{"data":{"revision":"2"}}'
  ;;
'POST /api/v1/auth/logout') rm -f -- "$state/session"; respond 204 '' ;;
'GET /api/v1/resumes')
  live=false
  [ "$behavior" != public-target ] || live=true
  respond 200 "{\"data\":[{\"id\":\"src-id\",\"lng\":\"en\",\"title\":\"Fictional English resume\",\"revision\":\"2\",\"live\":false,\"slug\":null},{\"id\":\"tgt-id\",\"lng\":\"vi\",\"title\":\"CV tiếng Việt\",\"revision\":\"4\",\"live\":$live,\"slug\":null}]}"
  ;;
'GET /api/v1/resumes/tgt-id')
  respond 200 '{"data":{"live":false,"slug":null,"document":{"personalDetails":{"headline":"Kỹ sư nền tảng hư cấu","photo":{"crop":null}}}}}'
  ;;
'GET /api/v1/resumes/tgt-id/photo') respond 200 'png' ;;
esac
respond 404 ''
EOF
}

# The fake browser container records its exact arguments and what it can
# see, then plays the helper side of the handoff.
write_container() {
  write_fake "$BROWSER/run.sh" <<'EOF'
#!/usr/bin/env bash
set -u
readonly -a SPEC_SOURCES=(
  playwright.config.ts
  mcp.spec.ts
  mcp-sdk.spec.ts
  harness-lib.ts
  network-policy.ts
)
ctl=__CTL__
terminated() { echo container-terminated >>"$ctl/calls"; exit 143; }
trap terminated TERM
printf '%s\n' "$$" >"$ctl/container.pid"
printf '%s\n' "$@" >"$ctl/container.args"
echo container >>"$ctl/calls"
staging=$3 browser=$7 credential=$8
stat -c %a "$staging" >"$ctl/container.staged"
find "$staging" -mindepth 1 -printf '%f %m\n' | LC_ALL=C sort >>"$ctl/container.staged"
LC_ALL=C ls -A "$browser" | wc -l >"$ctl/container.browser"
stat -c '%a' "$browser" >>"$ctl/container.browser"
stat -c '%F %a' "$credential" >"$ctl/container.credential"
publish() {
  local tmp
  tmp=$(mktemp "$browser/.$1.XXXXXX")
  printf '%s' "$2" >"$tmp"
  chmod 0600 "$tmp"
  mv -f -- "$tmp" "$browser/$1"
}
case $(cat "$ctl/container.behavior") in
result-login-failed)
  publish browser-ready ready
  i=0
  while [ ! -e "$browser/browser-request.json" ]; do
    i=$((i + 1))
    [ "$i" -lt 400 ] || exit 1
    sleep 0.05
  done
  rm -f -- "$browser/browser-request.json"
  publish browser-result.json login_failed
  exit 1
  ;;
result-login-failed-late)
  publish browser-ready ready
  i=0
  while [ ! -e "$browser/browser-request.json" ]; do
    i=$((i + 1))
    [ "$i" -lt 400 ] || exit 1
    sleep 0.05
  done
  rm -f -- "$browser/browser-request.json"
  publish browser-result.json login_failed
  sleep 1
  echo 'dev-https-browser: mcp-sdk-stage:handoff-failed-login-failed-at-session-consent-none-on-login'
  exit 1
  ;;
fail)
  echo 'dev-https-browser: mcp-sdk-stage:login'
  echo 'dev-https-browser: https://localhost:20443/login?next=leak-marker'
  exit 1
  ;;
esac
publish browser-ready ready
i=0
while [ ! -e "$browser/browser-request.json" ]; do
  i=$((i + 1))
  [ "$i" -lt 400 ] || exit 1
  sleep 0.05
done
rm -f -- "$browser/browser-request.json"
publish browser-result.json completed
exit 0
EOF
}

refresh_image_manifest() {
  local id=${1:-$IMAGE} path hash
  hash=$({
    for path in deploy/dev-https-browser/Dockerfile deploy/dev-https-browser/package.json \
      deploy/dev-https-browser/package-lock.json deploy/dev-https-browser/run.sh; do
      printf '%s\0' "$path"
      (cd "$REPO" && sha256sum -- "$path")
    done
  } | sha256sum | cut -c1-64)
  printf 'image_id=%s\nsource_sha256=%s\n' "$id" "$hash" >"$STATE/browser-image.manifest"
  chmod 0600 "$STATE/browser-image.manifest"
}

# refresh_effective_config records the HTTPS stack server hash the way
# scripts/dev-https.sh does, among other effective-config lines.
refresh_effective_config() {
  printf 'web_port=20030\nserver_binary_sha256=%s\nmigrate_binary_sha256=%s\n' \
    "$(sha "$STATE/bin/server")" "$(printf migrate | sha256sum | cut -c1-64)" >"$STATE/effective-config"
  chmod 0600 "$STATE/effective-config"
}

container_args() {
  awk -v s="$STATE" 'NR == 3 && index($0, s "/spec-input.") == 1 { print "STAGING"; next }
    NR == 4 && index($0, s "/mcp-sdk-browser.") == 1 { print "EVIDENCE"; next }
    NR == 9 && /^aboutme-mcp-sdk-[0-9a-f]{32}$/ { print "NAME"; next } { print }' \
    "$CTL/container.args" 2>/dev/null || true
}

setup_tree() {
  chmod -R u+rwx "$MAIN" "$REPO" 2>/dev/null || true
  rm -rf -- "$MAIN" "$REPO" "$CTL"
  make_fakes
  mkdir -p "$MAIN/.git" "$MAIN/.dev/credentials" "$REPO/scripts" "$BROWSER" \
    "$REPO/apps/server/cmd/native-http-fixture/testdata" "$REPO/.dev/bin" \
    "$STATE/input" "$STATE/secrets"
  chmod 0700 "$MAIN/.dev/credentials" "$REPO/.dev" "$REPO/.dev/bin" "$STATE" "$STATE/input" "$STATE/secrets"
  printf 'ABOUTME_TEST_EMAIL=%s@example.invalid\nABOUTME_TEST_PASSWORD=%s\n' \
    "$OWNER_MARKER" "$OWNER_MARKER" >"$OWNER_CREDENTIAL"
  chmod 0600 "$OWNER_CREDENTIAL"
  cp -- "$SCRIPT_SOURCE" "$SCRIPT"
  chmod 0755 "$SCRIPT"
  local name
  for name in Dockerfile package.json package-lock.json playwright.config.ts mcp.spec.ts \
    mcp-sdk.spec.ts harness-lib.ts network-policy.ts; do
    printf '// %s fixture\n' "$name" >"$BROWSER/$name"
  done
  write_container
  printf 'png' >"$REPO/apps/server/cmd/native-http-fixture/testdata/photo.png"
  printf 'dev-native server binary\n' >"$REPO/.dev/bin/server"
  mkdir -p "$STATE/bin"
  chmod 0700 "$STATE/bin"
  printf 'native HTTPS server binary\n' >"$STATE/bin/server"
  chmod 0755 "$STATE/bin/server"
  refresh_effective_config
  printf 'root\n' >"$STATE/input/caddy-root.crt"
  printf '%s' "$CAPTURE_SECRET" >"$STATE/secrets/auth-email-capture-bearer"
  chmod 0600 "$STATE/input/caddy-root.crt" "$STATE/secrets/auth-email-capture-bearer"
  refresh_image_manifest
  printf '%s\n' "$MAIN/.git" >"$CTL/git.common"
  printf '%s\n' "$APP" >"$CTL/ghcr.server"
  printf '%s\n' "$WEB" >"$CTL/ghcr.web"
  printf '%s\n' "$COMMIT" >"$CTL/git.commit"
  printf '%s\n' "$COMMIT" >"$CTL/git.tagcommit"
  : >"$CTL/git.status"
  : >"$CTL/git.tags"
  echo ok >"$CTL/runner.behavior"
  echo ok >"$CTL/container.behavior"
  echo ok >"$CTL/curl.behavior"
}

# run_workflow runs the script with an xtrace on a private descriptor, so a
# test can prove which paths the shell touched.
run_workflow() {
  local mode=$1
  shift
  rm -f -- "$CTL"/runner.* "$CTL"/container.args "$CTL"/container.pid "$CTL/synthetic.copy"
  echo "${RUNNER_BEHAVIOR:-ok}" >"$CTL/runner.behavior"
  echo "${CONTAINER_BEHAVIOR:-ok}" >"$CTL/container.behavior"
  echo "${CURL_BEHAVIOR:-ok}" >"$CTL/curl.behavior"
  : >"$CTL/calls"
  : >"$CTL/curl.argv"
  : >"$CTL/curl.calls"
  printf '%s' "${RUNNER_STDERR-}" >"$CTL/runner.stderr"
  local started=$SECONDS
  STATUS=0
  env -u ABOUTME_RELEASE_APP_IMAGE -u ABOUTME_RELEASE_WEB_IMAGE PATH="$FAKE:$PATH" "$@" \
    timeout 90 bash -c 'exec 7>"$1"; BASH_XTRACEFD=7; set -x; shift; source "$0"' \
    "$SCRIPT" "$CTL/trace" "$mode" >"$CTL/out" 2>&1 || STATUS=$?
  ELAPSED=$((SECONDS - started))
}

fixed_output() {
  ! grep -Evq "$FIXED_LINE" "$CTL/out"
}

no_secret_in() {
  local file=$1 value
  for value in "$OWNER_MARKER" "$CAPTURE_SECRET" "$CAPTURE_BEARER" csrf-secret-marker tok123 \
    request-secret-marker leak-marker; do
    lacks "$value" "$file" || return 1
  done
  if [ -s "$CTL/synthetic.copy" ]; then
    value=$(sed -n 's/^ABOUTME_TEST_PASSWORD=//p' "$CTL/synthetic.copy")
    [ -n "$value" ] && lacks "$value" "$file" || return 1
  fi
}

local_run_root() {
  sed -n 2p "$CTL/runner.args" 2>/dev/null || true
}

source_digest() {
  local name
  {
    for name in $(printf '%s\n' "$@" | LC_ALL=C sort); do
      printf '%s\0%s\0%s\n' "$name" 600 "$(sha "$BROWSER/$name")"
    done
  } | sha256sum | cut -c1-64
}

set_release() {
  printf '%s\n' "$TAG" >"$CTL/git.tags"
}

# 1. Closed modes.
setup_tree
for args in "" "staging" "local extra" "production local"; do
  STATUS=0
  # shellcheck disable=SC2086
  PATH="$FAKE:$PATH" bash "$SCRIPT" $args >"$CTL/out" 2>&1 || STATUS=$?
  check "rejects mode '$args'" [ "$STATUS" -eq 2 ]
done
check 'usage runs nothing' [ ! -s "$CTL/calls" ]

# 2. Local synthetic proof on an untagged checkout.
setup_tree
chmod 000 "$MAIN/.dev/credentials"
run_workflow local
chmod 0700 "$MAIN/.dev/credentials"
RUN=$(local_run_root)
check 'local succeeds' [ "$STATUS" -eq 0 ]
check 'local ends with the completed stage' [ "$(tail -n 1 "$CTL/out")" = 'mcp-owner-workflow: stage completed' ]
check 'local output is fixed stage lines only' fixed_output
check 'local skips the attestation off a release tag' has 'stage attestation-skipped' "$CTL/out"
check 'local writes no attestation' [ ! -e "$ATTESTATION" ]
check 'local never names the owner credential' lacks 'owner-test.env' "$CTL/trace"
check 'local never names the credential directory' lacks '.dev/credentials' "$CTL/trace"
check 'local uses a non-production control root' \
  [ "$(dirname "$RUN")" = "$REPO/.dev/mcp-workflow-local/control" ]
check 'runner runs on the host from the checkout' called "^runner cwd=$REPO\$"
check 'runner receives only mode, run root, and browser root' \
  [ "$(cat "$CTL/runner.args")" = "$(printf 'local\n%s\n%s/browser' "$RUN" "$RUN")" ]
check 'runner is built without cgo' called '^go build ./cmd/mcp-workflow cgo=0$'
check 'run root holds only the browser root and synthetic login' \
  [ "$(cat "$CTL/runner.entries")" = 'browser synthetic-login.env ' ]
check 'run, browser, and login modes are private' \
  [ "$(tr '\n' ' ' <"$CTL/runner.modes")" = '700 700 regular file 600 ' ]
expected_args=$(printf '%s\n' "$IMAGE" "$STATE/input" STAGING EVIDENCE mcp-sdk local \
  "$RUN/browser" "$RUN/synthetic-login.env" NAME)
check 'container receives the exact local mounts' [ "$(container_args)" = "$expected_args" ]
check 'container never receives the run root itself' no_line "$RUN" "$CTL/container.args"
check 'staged browser sources are the closed manifest at mode 0600' [ "$(cat "$CTL/container.staged")" = "$(printf '%s\n' 700 \
  'harness-lib.ts 600' 'mcp-sdk.spec.ts 600' 'mcp.spec.ts 600' 'network-policy.ts 600' 'playwright.config.ts 600')" ]
check 'browser root starts empty and private' [ "$(tr '\n' ' ' <"$CTL/container.browser")" = '0 700 ' ]
check 'synthetic login is a private regular file' [ "$(cat "$CTL/container.credential")" = 'regular file 600' ]
check 'synthetic login uses the reserved fixture prefix' \
  grep -Eq '^ABOUTME_TEST_EMAIL=pa-test-mcp-[0-9a-f]{16}@example\.invalid$' "$CTL/synthetic.copy"
check 'candidate and review are private' [ "$(tr '\n' ' ' <"$CTL/candidate.modes")" = '600 600 ' ]
check 'candidate translates only text' jqe '. == {personalDetails: {fullName: "Lan Fixture",
  headline: "Kỹ sư nền tảng hư cấu"}, content: {work: {displayName: "Kinh nghiệm"}}}' "$CTL/candidate.copy"
check 'fixture flow registers, creates, uploads, signs out, and inspects' [ "$(tr '\n' ' ' <"$CTL/curl.calls")" = \
  "DELETE /api/messages POST /api/v1/auth/password/register GET /api/messages POST /api/v1/auth/password/verify POST /api/v1/auth/password/login GET /api/v1/me POST /api/v1/resumes GET /api/v1/me POST /api/v1/resumes/src-id/photo GET /api/v1/me POST /api/v1/auth/logout POST /api/v1/auth/password/login GET /api/v1/resumes GET /api/v1/resumes/tgt-id GET /api/v1/resumes/tgt-id/photo GET /api/v1/me POST /api/v1/auth/logout " ]
check 'fixture photo comes from the tracked fixture' \
  has "file=@$REPO/apps/server/cmd/native-http-fixture/testdata/photo.png;type=image/png" "$CTL/photo.form"
check 'fixture account is cleaned before and after' [ "$(grep -c '^fixture cleanup$' "$CTL/calls")" -eq 2 ]
check 'no secret reaches output' no_secret_in "$CTL/out"
check 'the local runner trusts only the exported Caddy root' \
  [ "$(cat "$CTL/runner.env")" = "$(printf 'SSL_CERT_FILE=%s\nSSL_CERT_DIR=%s' "$STATE/input/caddy-root.crt" "$STATE/input")" ]
check 'an untagged proof never contacts the registry' lacks '/token?scope=' "$CTL/curl.calls"
check 'the proof hashes the HTTPS stack server, not dev-native' lacks "$REPO/.dev/bin/server" "$CTL/trace"
check 'no secret reaches curl arguments' no_secret_in "$CTL/curl.argv"
check 'no secret reaches container arguments' no_secret_in "$CTL/container.args"
check 'no secret reaches runner arguments' no_secret_in "$CTL/runner.args"
check 'local retains only allowlisted evidence' [ "$(LC_ALL=C ls -A "$RUN")" = evidence.json ]
check 'retained evidence is private' [ "$(mode_of "$RUN/evidence.json")" = 600 ]
check 'fixture scratch is removed' [ ! -e "$REPO/.dev/mcp-workflow-local/fixture" ]
check 'staging and browser output are removed' \
  [ -z "$(find "$STATE" -mindepth 1 -maxdepth 1 -name 'spec-input.*' -o -name 'mcp-sdk-browser*')" ]
check 'launcher lock is released' flock -n "$MAIN/.dev/mcp-workflow-launcher.lock" true
check 'local success removes the named container' container_removed

# 3. Local proof on a clean release tag writes the closed attestation.
set_release
run_workflow local env ABOUTME_RELEASE_APP_IMAGE="sha256:$(printf 'f%.0s' {1..64})"
check 'release local proof succeeds' [ "$STATUS" -eq 0 ]
check 'release local proof writes the attestation' has 'stage attestation-written' "$CTL/out"
check 'attestation is private' [ "$(mode_of "$ATTESTATION") $(mode_of "$CONTROL")" = '600 700' ]
expected_source=$(source_digest playwright.config.ts mcp.spec.ts mcp-sdk.spec.ts harness-lib.ts network-policy.ts)
check 'attestation binds every identity' jqe --arg commit "$COMMIT" --arg tag "$TAG" \
  --arg runner "$(sha "$CTL/fake-runner")" --arg server "$(sha "$STATE/bin/server")" \
  --arg launcher "$(sha "$SCRIPT")" --arg browser_runner "$(sha "$BROWSER/run.sh")" \
  --arg source "$expected_source" --arg app "$APP" --arg web "$WEB" --arg image "$IMAGE" '
  (keys == ["app_image","browser_image","browser_runner_sha256","browser_source_sha256",
    "completed_at","launcher_sha256","release_commit","release_tag","result",
    "runner_sha256","scenario","server_sha256","version","web_image"])
  and .version == 1 and .scenario == "mcp-owner-workflow-local-v1" and .result == "passed"
  and .release_tag == $tag and .release_commit == $commit and .runner_sha256 == $runner
  and .server_sha256 == $server and .launcher_sha256 == $launcher
  and .browser_runner_sha256 == $browser_runner and .browser_source_sha256 == $source
  and .app_image == $app and .web_image == $web and .browser_image == $image
  and ((now - (.completed_at | fromdateiso8601)) | . >= 0 and . < 120)' "$ATTESTATION"
if [ ! -f "$ATTESTATION" ]; then
  bad 'no attestation to test production against'
  exit 1
fi
cp -- "$ATTESTATION" "$T/attestation.good"
rm -rf -- "$T/repo.good"
cp -a -- "$REPO" "$T/repo.good"
cp -- "$CTL/fake-runner" "$T/runner.good"
check 'release digests come from the registry by tag' [ "$(grep -c "/manifests/$TAG\$" "$CTL/curl.calls")" -eq 2 ]
check 'caller image input is ignored' lacks 'ffffffff' "$ATTESTATION"
check 'the registry token stays out of curl arguments' lacks ghcr-anon-token "$CTL/curl.argv"
check 'the registry token stays out of output' lacks ghcr-anon-token "$CTL/out"

rm -f -- "$CTL/ghcr.web"
run_workflow local
check 'unpublished release images skip the attestation' has 'stage attestation-skipped' "$CTL/out"
check 'unpublished release images keep the proof passing' [ "$STATUS" -eq 0 ]
printf '%s\n' "$WEB" >"$CTL/ghcr.web"

restore_release() {
  chmod -R u+rwx "$REPO" 2>/dev/null || true
  rm -rf -- "$REPO"
  cp -a -- "$T/repo.good" "$REPO"
  cp -- "$T/runner.good" "$CTL/fake-runner"
  install -m 0600 "$T/attestation.good" "$ATTESTATION"
  set_release
  : >"$CTL/git.status"
  printf '%s\n' "$COMMIT" >"$CTL/git.commit"
  printf '%s\n' "$COMMIT" >"$CTL/git.tagcommit"
  chmod 0600 "$OWNER_CREDENTIAL"
  printf '%s\n' "$APP" >"$CTL/ghcr.server"
  printf '%s\n' "$WEB" >"$CTL/ghcr.web"
}

run_production() {
  run_workflow production "$@"
}

# 4. Production with the matching attestation.
restore_release
run_production env SSL_CERT_FILE=/caller/override.pem SSL_CERT_DIR=/caller
PROD_RUN=$(local_run_root)
check 'production succeeds with a matching attestation' [ "$STATUS" -eq 0 ]
check 'production ends with the completed stage' [ "$(tail -n 1 "$CTL/out")" = 'mcp-owner-workflow: stage completed' ]
check 'production output is fixed stage lines only' fixed_output
check 'production uses the main checkout control root' [ "$(dirname "$PROD_RUN")" = "$CONTROL" ]
check 'container receives the exact production mounts' [ "$(container_args)" = "$(printf '%s\n' \
  "$IMAGE" "" STAGING EVIDENCE mcp-sdk production "$PROD_RUN/browser" "$OWNER_CREDENTIAL" NAME)" ]
check 'production never receives a CA input directory' \
  [ "$(sed -n 2p "$CTL/container.args")" = "" ]
check 'production never reads the owner credential into the shell' no_secret_in "$CTL/trace"
check 'production keeps secrets out of output and arguments' no_secret_in "$CTL/out"
check 'production builds no fixture' not_called 'password-auth-fixture'
check 'production keeps the system trust store and drops a caller override' \
  [ "$(cat "$CTL/runner.env")" = "$(printf 'SSL_CERT_FILE=unset\nSSL_CERT_DIR=unset')" ]
check 'production retains only evidence' [ "$(LC_ALL=C ls -A "$PROD_RUN")" = evidence.json ]
check 'production success removes the named container' container_removed

restore_release
owner_sha=$(sha "$OWNER_CREDENTIAL")
RUNNER_BEHAVIOR=fail-with-source run_production
PROD_RUN=$(local_run_root)
check 'production runner failure fails the workflow' [ "$STATUS" -eq 1 ]
check 'production runner failure is reported' has 'mcp-owner-workflow: runner failed' "$CTL/out"
check 'production runner failure stops the browser container' dead "$CTL/container.pid"
check 'production failure removes owner content and the browser root' [ -z "$(LC_ALL=C ls -A "$PROD_RUN")" ]
check 'production failure keeps the owner credential intact' [ "$(sha "$OWNER_CREDENTIAL")" = "$owner_sha" ]
check 'production failure removes staging and browser output' \
  [ -z "$(find "$STATE" -mindepth 1 -maxdepth 1 -name 'spec-input.*' -o -name 'mcp-sdk-browser*')" ]
check 'production failure releases the launcher lock' flock -n "$MAIN/.dev/mcp-workflow-launcher.lock" true
check 'production failure keeps secrets out of output' no_secret_in "$CTL/out"
check 'production failure output is fixed' fixed_output
check 'production failure removes the named container' container_removed

restore_release
RUNNER_BEHAVIOR=sentinel run_production
check 'a completion sentinel stops production successfully' [ "$STATUS" -eq 0 ]
check 'sentinel stop reports already-complete' [ "$(tail -n 1 "$CTL/out")" = 'mcp-owner-workflow: stage already-complete' ]
check 'sentinel stop stops the idle helper' dead "$CTL/container.pid"
check 'sentinel stop removes the named container' container_removed

restore_release
RUNNER_BEHAVIOR=recovery run_production
check 'revocation-only recovery succeeds' [ "$(tail -n 1 "$CTL/out")" = 'mcp-owner-workflow: stage recovered' ]

restore_release
RUNNER_BEHAVIOR=fail-with-evidence RUNNER_STDERR='mcp-workflow: revocation
' run_production
check 'unresolved revocation evidence is reported' \
  has 'mcp-owner-workflow: runner evidence owner_workflow revocation_unconfirmed' "$CTL/out"
check 'unresolved revocation output is fixed' fixed_output
check 'evidence wins over a runner reason' lacks 'runner reason' "$CTL/out"

restore_release
RUNNER_BEHAVIOR=bad-evidence run_production
check 'evidence outside the allowlist fails' [ "$STATUS" -eq 1 ]
check 'evidence rejection is reported' has 'evidence rejected' "$CTL/out"

# 5. Attestation rejections happen before any credential access or launch.
expect_rejection() {
  local label=$1 message=$2
  check "$label: fails" [ "$STATUS" -eq 1 ]
  check "$label: reports $message" has "$message" "$CTL/out"
  check "$label: before credential access" lacks 'owner-test.env' "$CTL/trace"
  check "$label: before launch" not_called '^(runner|container|podman)'
  check "$label: fixed output" fixed_output
}

edit_attestation() {
  jq -c "$1" "$T/attestation.good" >"$ATTESTATION"
  chmod 0600 "$ATTESTATION"
}

restore_release
rm -f -- "$ATTESTATION"
run_production
expect_rejection 'missing attestation' 'attestation rejected: missing'

restore_release
printf 'not json' >"$ATTESTATION"
run_production
expect_rejection 'malformed attestation' 'attestation rejected: malformed'

for filter in '. + {extra: true}' '.version = 2' 'del(.web_image)' '.runner_sha256 = "abc"' \
  '.completed_at = "yesterday"'; do
  restore_release
  edit_attestation "$filter"
  run_production
  expect_rejection "attestation edit $filter" 'attestation rejected: malformed'
done

restore_release
edit_attestation '.scenario = "mcp-owner-workflow-local-v2"'
run_production
expect_rejection 'wrong scenario' 'attestation rejected: scenario'

restore_release
edit_attestation '.result = "failed"'
run_production
expect_rejection 'failed result' 'attestation rejected: scenario'

restore_release
chmod 0644 "$ATTESTATION"
run_production
expect_rejection 'shared attestation mode' 'attestation rejected: unsafe file'

restore_release
mv -- "$ATTESTATION" "$CONTROL/elsewhere.json"
ln -s "$CONTROL/elsewhere.json" "$ATTESTATION"
run_production
expect_rejection 'symbolic attestation' 'attestation rejected: unsafe file'
rm -f -- "$ATTESTATION" "$CONTROL/elsewhere.json"

restore_release
edit_attestation ".completed_at = \"$(date -u -d '+1 hour' +%Y-%m-%dT%H:%M:%SZ)\""
run_production
expect_rejection 'future attestation' 'attestation rejected: time reversed'

restore_release
edit_attestation ".completed_at = \"$(date -u -d '-25 hours' +%Y-%m-%dT%H:%M:%SZ)\""
run_production
expect_rejection 'stale attestation' 'attestation rejected: stale'

restore_release
edit_attestation '.release_tag = "v0.0.1"'
run_production
expect_rejection 'other release tag' 'attestation rejected: release mismatch'

restore_release
edit_attestation '.release_commit = "fedcba9876543210fedcba9876543210fedcba98"'
run_production
expect_rejection 'other release commit' 'attestation rejected: release mismatch'

restore_release
printf ' M apps/server/cmd/mcp-workflow/main.go\n' >"$CTL/git.status"
run_production
expect_rejection 'dirty checkout' 'attestation rejected: checkout is not a clean release tag'

restore_release
: >"$CTL/git.tags"
run_production
expect_rejection 'untagged checkout' 'attestation rejected: checkout is not a clean release tag'

restore_release
printf '# changed\n' >>"$CTL/fake-runner"
run_production
expect_rejection 'changed runner' 'attestation rejected: runner mismatch'

restore_release
printf 'changed\n' >>"$STATE/bin/server"
refresh_effective_config
run_production
expect_rejection 'changed local server' 'attestation rejected: server mismatch'

restore_release
printf '# changed\n' >>"$SCRIPT"
run_production
expect_rejection 'changed launcher' 'attestation rejected: launcher mismatch'

restore_release
printf '# changed\n' >>"$BROWSER/run.sh"
refresh_image_manifest
run_production
expect_rejection 'changed browser runner' 'attestation rejected: browser runner mismatch'

for changed in playwright.config.ts mcp-sdk.spec.ts harness-lib.ts network-policy.ts; do
  restore_release
  printf '// changed\n' >>"$BROWSER/$changed"
  run_production
  expect_rejection "changed staged source $changed" 'attestation rejected: browser source mismatch'
done

restore_release
sed -i 's/^  network-policy.ts$/  network-policy.ts\n  extra-helper.ts/' "$BROWSER/run.sh"
printf '// extra\n' >"$BROWSER/extra-helper.ts"
refresh_image_manifest
edit_attestation ".browser_runner_sha256 = \"$(sha "$BROWSER/run.sh")\""
run_production
expect_rejection 'added manifest member' 'attestation rejected: browser source mismatch'

restore_release
printf '%s\n' sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd >"$CTL/ghcr.server"
run_production env ABOUTME_RELEASE_APP_IMAGE="$APP"
expect_rejection 'other registry app image' 'attestation rejected: release image mismatch'

restore_release
printf '%s\n' sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd >"$CTL/ghcr.web"
run_production
expect_rejection 'other registry web image' 'attestation rejected: release image mismatch'

restore_release
rm -f -- "$CTL/ghcr.server"
run_production
expect_rejection 'unpublished release image' 'attestation rejected: release images are not published'

restore_release
refresh_image_manifest sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
run_production
expect_rejection 'other browser image' 'attestation rejected: browser image mismatch'

# 6. The owner credential is checked only after the attestation passes.
restore_release
chmod 0644 "$OWNER_CREDENTIAL"
run_production
check 'shared owner credential fails' [ "$STATUS" -eq 1 ]
check 'shared owner credential is reported' has 'owner credential must be a regular mode-0600 file' "$CTL/out"
check 'shared owner credential never launches' not_called '^(runner|container)'

restore_release
mv -- "$OWNER_CREDENTIAL" "$MAIN/.dev/credentials/real.env"
ln -s "$MAIN/.dev/credentials/real.env" "$OWNER_CREDENTIAL"
run_production
check 'symbolic owner credential fails' has 'owner credential must be a regular mode-0600 file' "$CTL/out"
rm -f -- "$OWNER_CREDENTIAL"
mv -- "$MAIN/.dev/credentials/real.env" "$OWNER_CREDENTIAL"

# 7. Joined failure: either side failing stops and reaps the other.
restore_release
: >"$CTL/git.tags"
RUNNER_BEHAVIOR=fail run_workflow local
check 'runner failure fails the workflow' [ "$STATUS" -eq 1 ]
check 'runner failure is reported' has 'mcp-owner-workflow: runner failed' "$CTL/out"
check 'runner failure stops the browser container' dead "$CTL/container.pid"
check 'runner failure returns promptly' [ "$ELAPSED" -lt 15 ]
check 'runner failure still cleans the fixture' [ "$(grep -c '^fixture cleanup$' "$CTL/calls")" -eq 2 ]
check 'runner failure removes the synthetic login' [ -z "$(find "$REPO/.dev/mcp-workflow-local" -name synthetic-login.env)" ]
check 'runner failure output is fixed' fixed_output
check 'runner failure removes the named container' container_removed
check 'runner failure names the exit status' has 'mcp-owner-workflow: runner exit 1' "$CTL/out"
check 'runner failure names the missing evidence' has 'mcp-owner-workflow: runner evidence none' "$CTL/out"
check 'runner failure names the missing browser result' has 'mcp-owner-workflow: browser result none' "$CTL/out"
check 'runner failure names the stopped helper' \
  has 'mcp-owner-workflow: browser helper stopped after runner failure' "$CTL/out"
check 'a silent runner reports no reason' lacks 'runner reason' "$CTL/out"

RUNNER_BEHAVIOR=fail RUNNER_STDERR='mcp-workflow: tls
' run_workflow local
check 'a closed runner reason is forwarded' has 'mcp-owner-workflow: runner reason tls' "$CTL/out"
check 'a forwarded reason keeps the output fixed' fixed_output

for noise in 'mcp-workflow: https://localhost:20443/oauth/authorize?code=leak-marker
' 'mcp-workflow: Bearer leak-marker
' 'mcp-workflow: /home/runner/work/aboutme/.dev/mcp-workflow-local
' 'mcp-workflow: not-a-listed-word
' 'mcp-workflow: tls
mcp-workflow: oauth
' 'panic: runtime error
mcp-workflow: tls
' 'mcp-workflow: TLS
'; do
  RUNNER_BEHAVIOR=fail RUNNER_STDERR=$noise run_workflow local
  check 'runner output outside the closed vocabulary is dropped' lacks 'runner reason' "$CTL/out"
  check 'dropped runner output never reaches the launcher output' no_secret_in "$CTL/out"
  check 'dropped runner output keeps the output fixed' fixed_output
done

CONTAINER_BEHAVIOR=result-login-failed run_workflow local
check 'a failed login fails the workflow' [ "$STATUS" -eq 1 ]
check 'the browser result code is reported' has 'mcp-owner-workflow: browser result login_failed' "$CTL/out"
check 'a failed login reports the helper exit' has 'mcp-owner-workflow: browser helper failed with exit 1' "$CTL/out"
check 'a failed login output is fixed' fixed_output

CONTAINER_BEHAVIOR=result-login-failed-late run_workflow local
check 'a helper with a result is not stopped when the runner fails first' \
  has 'mcp-owner-workflow: browser helper failed with exit 1' "$CTL/out"
check 'the helper names the failed step after the runner fails' \
  has 'mcp-owner-workflow: browser mcp-sdk-stage:handoff-failed-login-failed-at-session-consent-none-on-login' "$CTL/out"
check 'a late helper stage keeps the output fixed' fixed_output

RUNNER_BEHAVIOR=hang CONTAINER_BEHAVIOR=fail run_workflow local
check 'browser failure fails the workflow' [ "$STATUS" -eq 1 ]
check 'browser failure is reported' has 'mcp-owner-workflow: browser helper failed' "$CTL/out"
check 'browser failure forwards only the bounded stage' has 'mcp-owner-workflow: browser mcp-sdk-stage:login' "$CTL/out"
check 'browser failure withholds volatile output' no_secret_in "$CTL/out"
check 'browser failure stops the runner' called '^runner-terminated$'
check 'browser failure reaps the runner' dead "$CTL/runner.pid"
check 'browser failure removes the browser root' [ -z "$(find "$REPO/.dev/mcp-workflow-local" -name browser)" ]
check 'browser failure output is fixed' fixed_output
check 'browser failure removes the named container' container_removed

CURL_BEHAVIOR=public-target run_workflow local
check 'a public target fails inspection' has 'target inspection failed' "$CTL/out"
check 'inspection failure still cleans the fixture' [ "$(grep -c '^fixture cleanup$' "$CTL/calls")" -eq 2 ]

# 8. The proof binds the HTTPS stack server that its effective config names.
restore_release
: >"$CTL/git.tags"
rm -f -- "$STATE/bin/server"
run_workflow local
check 'only a dev-native server fails the proof' has 'cannot hash the HTTPS stack server binary' "$CTL/out"
check 'only a dev-native server launches nothing' not_called '^(runner|container|fixture)'

restore_release
: >"$CTL/git.tags"
printf 'rebuilt\n' >>"$STATE/bin/server"
run_workflow local
check 'a server that differs from the effective config fails' \
  has 'the HTTPS stack server does not match its effective config' "$CTL/out"
check 'a mismatched server launches nothing' not_called '^(runner|container|fixture)'
restore_release
: >"$CTL/git.tags"

# 9. One workflow at a time, and never beside another local check.
(
  exec 8>>"$MAIN/.dev/mcp-workflow-launcher.lock"
  flock 8
  : >"$CTL/lock.held"
  exec sleep 30
) &
holder=$!
for _ in $(seq 100); do [ -e "$CTL/lock.held" ] && break; sleep 0.05; done
run_workflow local
check 'a held lock refuses a second run' has 'another workflow run holds the lock' "$CTL/out"
check 'a held lock runs nothing' [ ! -s "$CTL/calls" ]
kill "$holder" 2>/dev/null || true
wait "$holder" 2>/dev/null || true
rm -f -- "$CTL/lock.held"

shared_lock=$MAIN/.git/aboutme-local-check.lock
(
  exec 8>>"$shared_lock"
  flock 8
  : >"$CTL/lock.held"
  exec sleep 30
) &
holder=$!
for _ in $(seq 100); do [ -e "$CTL/lock.held" ] && break; sleep 0.05; done
run_workflow local
check 'another local check holding the shared lock refuses the proof' \
  has 'another local check holds the shared lock' "$CTL/out"
check 'a held shared lock runs no fixture or build' [ ! -s "$CTL/calls" ]
kill "$holder" 2>/dev/null || true
wait "$holder" 2>/dev/null || true
rm -f -- "$CTL/lock.held"

run_workflow local flock "$shared_lock"
check 'a caller holding the shared lock runs the proof' [ "$STATUS" -eq 0 ]
check 'the inherited shared lock is reused' has 'stage completed' "$CTL/out"

# 10. The browser-source digest covers content, mode, and membership.
digest_dir=$T/digest
mkdir -p "$digest_dir"
printf 'a\n' >"$digest_dir/a.ts"
printf 'b\n' >"$digest_dir/b.ts"
printf 'c\n' >"$digest_dir/c.ts"
chmod 0600 "$digest_dir"/*.ts
digest() { (source "$SCRIPT" && browser_source_digest "$digest_dir" "$@"); }
base_digest=$(digest a.ts b.ts)
check 'digest matches the canonical definition' [ "$base_digest" = "$(
  printf '%s\0%s\0%s\n' a.ts 600 "$(sha "$digest_dir/a.ts")" b.ts 600 "$(sha "$digest_dir/b.ts")" |
    sha256sum | cut -c1-64
)" ]
check 'an added member changes the digest' [ "$(digest a.ts b.ts c.ts)" != "$base_digest" ]
chmod 0644 "$digest_dir/b.ts"
check 'a mode change changes the digest' [ "$(digest a.ts b.ts)" != "$base_digest" ]
chmod 0600 "$digest_dir/b.ts"
printf 'b2\n' >"$digest_dir/b.ts"
check 'a content change changes the digest' [ "$(digest a.ts b.ts)" != "$base_digest" ]
manifest=$( (source "$SCRIPT" && spec_manifest "$BROWSER/run.sh") | tr '\n' ' ')
check 'manifest is read from the launcher and sorted' \
  [ "$manifest" = 'harness-lib.ts mcp-sdk.spec.ts mcp.spec.ts network-policy.ts playwright.config.ts ' ]
printf 'readonly -a SPEC_SOURCES=(\n  b.spec.ts\n  a-shards.mjs\n)\n' >"$T/mjs-run.sh"
check 'manifest accepts an ES module source' \
  [ "$( (source "$SCRIPT" && spec_manifest "$T/mjs-run.sh") | tr '\n' ' ')" = 'a-shards.mjs b.spec.ts ' ]
printf 'readonly -a SPEC_SOURCES=(\n  b.spec.ts\n  a-shards.js\n)\n' >"$T/js-run.sh"
check 'manifest rejects a source that is neither TypeScript nor an ES module' \
  [ -z "$( (source "$SCRIPT" && spec_manifest "$T/js-run.sh") || true)" ]
printf 'readonly -a SPEC_SOURCES=(\n  ../escape.ts\n)\n' >"$T/bad-run.sh"
check 'manifest rejects a path outside the staging root' \
  [ -z "$( (source "$SCRIPT" && spec_manifest "$T/bad-run.sh") || true)" ]

if [ "$failures" -ne 0 ]; then
  printf 'mcp-owner-workflow test: %d failure(s)\n' "$failures" >&2
  exit 1
fi
printf 'mcp-owner-workflow test: all checks passed\n'
