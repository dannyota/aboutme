#!/usr/bin/env bash

# Runs the MCP owner workflow: the host Go runner joined with the browser
# helper. `local` proves the SDK path on the native HTTPS stack with synthetic
# data and attests a clean release tag; `production` requires that attestation
# before it touches the owner credential. It takes no origin, credential,
# resume, or token input. See docs/design/mcp-owner-workflow.md.
set -Eeuo pipefail
umask 077

readonly NAME=mcp-owner-workflow
readonly LOCAL_ORIGIN=https://localhost:20443
readonly SCENARIO=mcp-owner-workflow-local-v1
readonly ATTESTATION_NAME=sdk-proof-attestation.json
readonly ATTESTATION_MAX_AGE=86400
readonly RUN_LIMIT_SECONDS=3000
readonly STOP_GRACE_SECONDS=30
readonly NATIVE_DSN='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable'
readonly CAPTURE_URL=http://127.0.0.1:20444/api/messages
readonly TARGET_TITLE='CV tiếng Việt'
readonly TARGET_HEADLINE='Kỹ sư nền tảng hư cấu'
readonly TARGET_SECTION='Kinh nghiệm'
readonly IMAGE_RE='^sha256:[0-9a-f]{64}$'
# The release-images workflow publishes here, and deploy/aws/scripts/deploy.sh
# deploys the digest this registry returns for the release tag.
readonly IMAGE_REPOSITORY=dannyota/aboutme
# The runner prints at most one closed failure word on standard error, and
# only while it has written no evidence. Anything else there stays unprinted.
RUNNER_REASONS=' arguments control-root run-root entry-set artifact-mode tls network discovery'
RUNNER_REASONS+=' oauth browser-handoff grant source-selection source-changed candidate create-intent'
RUNNER_REASONS+=' create-rejected mutation-cap tool-output photo recovery revocation evidence'
RUNNER_REASONS+=' workflow-state contract local-proof internal '
readonly RUNNER_REASONS
readonly -a IMAGE_SOURCES=(
  deploy/dev-https-browser/Dockerfile
  deploy/dev-https-browser/package.json
  deploy/dev-https-browser/package-lock.json
  deploy/dev-https-browser/run.sh
  deploy/dev-https-browser/verify-evidence.mjs
)

say() { printf '%s: %s\n' "$NAME" "$1"; }
stage() { say "stage $1"; }
# fail writes to ERR_FD, the saved stderr. main silences descriptor 2 so tool
# diagnostics and shell job notices never reach the output. Every descriptor
# the script opens is dynamic, so none replaces one a caller passed down.
fail() { printf '%s: %s\n' "$NAME" "$1" >&"$ERR_FD"; exit 1; }
secure_dir() { [ -d "$1" ] && [ ! -L "$1" ] && [ "$(stat -c '%u %a' -- "$1")" = "$UID_NOW 700" ]; }
secure_file() { [ -f "$1" ] && [ ! -L "$1" ] && [ "$(stat -c '%u %a' -- "$1")" = "$UID_NOW 600" ]; }
file_sha256() { [ -f "$1" ] && [ ! -L "$1" ] && sha256sum -- "$1" | awk '{print $1}'; }

# spec_manifest prints the closed staged browser source set run.sh enforces,
# sorted bytewise. A change in membership changes the digest.
spec_manifest() {
  local launcher=$1 name
  local -a names=()
  mapfile -t names < <(awk '
    /^readonly -a SPEC_SOURCES=\($/ { inside = 1; next }
    inside && /^\)$/ { exit }
    inside { gsub(/^[[:space:]]+|[[:space:]]+$/, ""); print }
  ' "$launcher")
  [ "${#names[@]}" -gt 0 ] || return 1
  for name in "${names[@]}"; do
    [[ $name =~ ^[a-z0-9][a-z0-9.-]*\.ts$ ]] || return 1
  done
  printf '%s\n' "${names[@]}" | LC_ALL=C sort -u
}

# browser_source_digest hashes ordered path, mode, and content entries for
# every staged source in the closed manifest.
browser_source_digest() {
  local dir=$1 name mode hash
  shift
  {
    for name in "$@"; do
      [ -f "$dir/$name" ] && [ ! -L "$dir/$name" ] || return 1
      mode=$(stat -c %a -- "$dir/$name") || return 1
      hash=$(file_sha256 "$dir/$name") || return 1
      printf '%s\0%s\0%s\n' "$name" "$mode" "$hash"
    done
  } | sha256sum | awk '{print $1}'
}

image_source_hash() {
  local path
  {
    for path in "${IMAGE_SOURCES[@]}"; do
      [ -f "$REPO/$path" ] && [ ! -L "$REPO/$path" ] || return 1
      printf '%s\0' "$path"
      (cd "$REPO" && sha256sum -- "$path")
    done
  } | sha256sum | awk '{print $1}'
}

resolve_paths() {
  REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
  LAUNCHER=$REPO/scripts/mcp-owner-workflow.sh
  CONTEXT=$REPO/deploy/dev-https-browser
  STATE=$REPO/.dev/native-https
  BIN=$REPO/.dev/bin
  GIT_COMMON=$(git -C "$REPO" rev-parse --path-format=absolute --git-common-dir) ||
    fail 'cannot find the Git common directory'
  [[ $GIT_COMMON = /*/.git ]] || fail 'the main checkout has an unexpected Git layout'
  MAIN=${GIT_COMMON%/.git}
  PRODUCTION_CONTROL=$MAIN/.dev/mcp-workflow
  [ -d "$MAIN/.dev" ] && [ ! -L "$MAIN/.dev" ] || fail 'the main checkout has no .dev directory'
}

take_lock() {
  local lock=$MAIN/.dev/mcp-workflow-launcher.lock
  [ ! -L "$lock" ] || fail 'the workflow lock is a symbolic link'
  exec {LOCK_FD}>>"$lock"
  flock -n "$LOCK_FD" || fail 'another workflow run holds the lock'
}

# take_local_check_lock takes the shared local-check lock (AGENTS.md Resource
# rules), so the fixture cleanup of reserved-prefix accounts and captured mail
# never overlaps the password proof. An inherited descriptor for it is reused.
take_local_check_lock() {
  local lock=$GIT_COMMON/aboutme-local-check.lock fd target
  [ ! -L "$lock" ] || fail 'the shared local-check lock is a symbolic link'
  for fd in /proc/$$/fd/*; do
    target=$(readlink -- "$fd") || continue
    if [ "$target" = "$lock" ] && flock -n "${fd##*/}"; then
      return 0
    fi
  done
  exec {CHECK_FD}>>"$lock"
  flock -n "$CHECK_FD" || fail 'another local check holds the shared lock'
}

validate_browser_state() {
  local manifest=$STATE/browser-image.manifest
  local -a lines=() entries=()
  secure_dir "$STATE" || fail 'invalid native HTTPS state directory'
  secure_file "$manifest" || fail 'invalid browser image manifest'
  mapfile -t lines <"$manifest"
  [ "${#lines[@]}" -eq 2 ] && [[ ${lines[0]} =~ ^image_id=(sha256:[0-9a-f]{64})$ ]] ||
    fail 'malformed browser image manifest'
  BROWSER_IMAGE=${BASH_REMATCH[1]}
  [[ ${lines[1]} =~ ^source_sha256=([0-9a-f]{64})$ ]] || fail 'malformed browser image manifest'
  [ "$(image_source_hash)" = "${BASH_REMATCH[1]}" ] ||
    fail 'browser image sources changed after image build; rerun make dev-https-browser-image'
  CA_INPUT=$STATE/input
  secure_dir "$CA_INPUT" || fail 'invalid CA input directory'
  mapfile -t entries < <(find "$CA_INPUT" -mindepth 1 -maxdepth 1 -printf '%f\n')
  [ "${#entries[@]}" -eq 1 ] && [ "${entries[0]}" = caddy-root.crt ] &&
    secure_file "$CA_INPUT/caddy-root.crt" || fail 'CA input must contain one root'
}

build_tools() {
  install -d -m 0700 "$BIN"
  (cd "$REPO/apps/server" && CGO_ENABLED=0 GOFLAGS= go build -trimpath -buildvcs=false \
    -o "$BIN/mcp-workflow" ./cmd/mcp-workflow) >/dev/null 2>&1 || fail 'runner build failed'
  RUNNER=$BIN/mcp-workflow
  if [ "$MODE" = local ]; then
    (cd "$REPO/apps/server" && go build -o "$BIN/password-auth-fixture" \
      ./cmd/password-auth-fixture) >/dev/null 2>&1 || fail 'fixture build failed'
  fi
}

stage_browser_sources() {
  local name helper=0
  mapfile -t MANIFEST_NAMES < <(spec_manifest "$CONTEXT/run.sh")
  [ "${#MANIFEST_NAMES[@]}" -gt 0 ] || fail 'cannot read the browser source manifest'
  STAGING=$(mktemp -d "$STATE/spec-input.XXXXXX") && chmod 0700 "$STAGING"
  for name in "${MANIFEST_NAMES[@]}"; do
    [ "$name" != mcp-sdk.spec.ts ] || helper=1
    [ -f "$CONTEXT/$name" ] && [ ! -L "$CONTEXT/$name" ] || fail 'a browser source is missing'
    cp -- "$CONTEXT/$name" "$STAGING/$name" && chmod 0600 "$STAGING/$name"
  done
  [ "$helper" -eq 1 ] || fail 'the browser source manifest has no workflow helper'
  SOURCE_DIGEST=$(browser_source_digest "$STAGING" "${MANIFEST_NAMES[@]}") ||
    fail 'cannot hash the staged browser sources'
}

current_identity() {
  RUNNER_SHA=$(file_sha256 "$RUNNER") || fail 'cannot hash the runner'
  https_server_identity
  LAUNCHER_SHA=$(file_sha256 "$LAUNCHER") || fail 'cannot hash the launcher'
  BROWSER_RUNNER_SHA=$(file_sha256 "$CONTEXT/run.sh") || fail 'cannot hash the browser runner'
}

# https_server_identity hashes the server binary the native HTTPS stack runs,
# not the dev-native one, and requires the hash `dev-https status` checks.
https_server_identity() {
  local config=$STATE/effective-config
  local -a lines=()
  SERVER_SHA=$(file_sha256 "$STATE/bin/server") || fail 'cannot hash the HTTPS stack server binary'
  secure_file "$config" || fail 'invalid HTTPS stack effective config'
  mapfile -t lines < <(grep -E '^server_binary_sha256=' "$config" || true)
  [ "${#lines[@]}" -eq 1 ] && [ "${lines[0]}" = "server_binary_sha256=$SERVER_SHA" ] ||
    fail 'the HTTPS stack server does not match its effective config'
}

# release_identity sets RELEASE_TAG and RELEASE_COMMIT only for a clean
# checkout whose HEAD is exactly one release tag.
release_identity() {
  local tags tagged
  local dirty
  RELEASE_TAG= RELEASE_COMMIT=
  RELEASE_COMMIT=$(git -C "$REPO" rev-parse --verify 'HEAD^{commit}') || return 1
  [[ $RELEASE_COMMIT =~ ^[0-9a-f]{40}$ ]] || return 1
  dirty=$(git -C "$REPO" status --porcelain --untracked-files=normal) || return 1
  [ -z "$dirty" ] || return 1
  tags=$(git -C "$REPO" tag --points-at HEAD) || return 1
  tags=$(printf '%s\n' "$tags" | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' || true)
  [ -n "$tags" ] && [ "$(printf '%s\n' "$tags" | wc -l)" -eq 1 ] || return 1
  tagged=$(git -C "$REPO" rev-parse --verify "refs/tags/$tags^{commit}") || return 1
  [ "$tagged" = "$RELEASE_COMMIT" ] || return 1
  RELEASE_TAG=$tags
}

# registry_digest sets the variable named $2 to the immutable digest the
# public registry holds for the release tag, with the deploy script's lookup.
registry_digest() {
  local name=$1 token digest=
  token=$(curl -fsS --proto '=https' --max-time 20 \
    "https://ghcr.io/token?scope=repository:$IMAGE_REPOSITORY-$name:pull&service=ghcr.io" |
    jq -er '.token | select(type == "string" and length > 0)') || return 1
  REGISTRY_HEADERS=$(mktemp "$STATE/.registry-headers.XXXXXX")
  printf 'Authorization: Bearer %s\nAccept: %s\n' "$token" \
    'application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json' \
    >"$REGISTRY_HEADERS"
  digest=$(curl -fsSI --proto '=https' --max-time 20 -H "@$REGISTRY_HEADERS" \
    "https://ghcr.io/v2/$IMAGE_REPOSITORY-$name/manifests/$RELEASE_TAG" |
    tr -d '\r' | awk -F': ' 'tolower($1) == "docker-content-digest" { print $2 }') || digest=
  rm -f -- "$REGISTRY_HEADERS"
  REGISTRY_HEADERS=
  [[ $digest =~ $IMAGE_RE ]] || return 1
  printf -v "$2" '%s' "$digest"
}

# release_images derives the app (server) and web digests of RELEASE_TAG from
# the registry; no caller input sets them. It cannot prove what production
# runs: the manager checks the deployed digests before the production run.
release_images() {
  APP_IMAGE= WEB_IMAGE=
  registry_digest server APP_IMAGE && registry_digest web WEB_IMAGE
}

# validate_attestation rejects a missing, malformed, time-reversed, stale, or
# mismatched proof before production resolves the owner credential path.
validate_attestation() {
  local path=$PRODUCTION_CONTROL/$ATTESTATION_NAME reason
  secure_dir "$PRODUCTION_CONTROL" || fail 'attestation rejected: missing'
  [ -e "$path" ] || [ -L "$path" ] || fail 'attestation rejected: missing'
  secure_file "$path" && [ "$(stat -c %s -- "$path")" -le 4096 ] || fail 'attestation rejected: unsafe file'
  reason=$(jq -er --arg scenario "$SCENARIO" --argjson age "$ATTESTATION_MAX_AGE" '
    def hex: type == "string" and test("^[0-9a-f]{64}$");
    def image: type == "string" and test("^sha256:[0-9a-f]{64}$");
    if (type == "object"
      and (keys == ["app_image","browser_image","browser_runner_sha256","browser_source_sha256",
        "completed_at","launcher_sha256","release_commit","release_tag","result",
        "runner_sha256","scenario","server_sha256","version","web_image"])
      and .version == 1 and (.scenario | type == "string") and (.result | type == "string")
      and (.completed_at | type == "string"
        and test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$"))
      and (.release_tag | type == "string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
      and (.release_commit | type == "string" and test("^[0-9a-f]{40}$"))
      and all(.runner_sha256, .server_sha256, .launcher_sha256, .browser_runner_sha256,
        .browser_source_sha256; hex) and all(.app_image, .web_image, .browser_image; image) | not)
      then "malformed"
    elif .scenario != $scenario or .result != "passed" then "scenario"
    elif (.completed_at | fromdateiso8601) > now then "time reversed"
    elif now - (.completed_at | fromdateiso8601) >= $age then "stale"
    else "ok" end' "$path" 2>/dev/null) || reason=malformed
  [ "$reason" = ok ] || fail "attestation rejected: $reason"
  release_identity || fail 'attestation rejected: checkout is not a clean release tag'
  release_images || fail 'attestation rejected: release images are not published'
  reason=$(jq -er --arg tag "$RELEASE_TAG" --arg commit "$RELEASE_COMMIT" --arg runner "$RUNNER_SHA" \
    --arg server "$SERVER_SHA" --arg launcher "$LAUNCHER_SHA" --arg app "$APP_IMAGE" \
    --arg browser_runner "$BROWSER_RUNNER_SHA" --arg source "$SOURCE_DIGEST" \
    --arg web "$WEB_IMAGE" --arg image "$BROWSER_IMAGE" '
    if .release_tag != $tag or .release_commit != $commit then "release mismatch"
    elif .runner_sha256 != $runner then "runner mismatch"
    elif .server_sha256 != $server then "server mismatch"
    elif .launcher_sha256 != $launcher then "launcher mismatch"
    elif .browser_runner_sha256 != $browser_runner then "browser runner mismatch"
    elif .browser_source_sha256 != $source then "browser source mismatch"
    elif .app_image != $app or .web_image != $web then "release image mismatch"
    elif .browser_image != $image then "browser image mismatch"
    else "ok" end' "$path" 2>/dev/null) || reason=malformed
  [ "$reason" = ok ] || fail "attestation rejected: $reason"
}

write_attestation() {
  local temporary
  if ! release_identity || ! release_images; then stage attestation-skipped && return 0; fi
  [ -e "$PRODUCTION_CONTROL" ] || [ -L "$PRODUCTION_CONTROL" ] || install -d -m 0700 "$PRODUCTION_CONTROL"
  secure_dir "$PRODUCTION_CONTROL" || fail 'invalid production control directory'
  temporary=$(mktemp "$PRODUCTION_CONTROL/.attestation.XXXXXX")
  chmod 0600 "$temporary"
  # Every value below is a constant, a fixed timestamp, or a value already
  # matched against a hex, tag, commit, or digest pattern, so none needs JSON
  # escaping.
  printf '{"version":1,"scenario":"%s","result":"passed","completed_at":"%s","release_tag":"%s",' \
    "$SCENARIO" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$RELEASE_TAG" >"$temporary"
  printf '"release_commit":"%s","runner_sha256":"%s","server_sha256":"%s","launcher_sha256":"%s",' \
    "$RELEASE_COMMIT" "$RUNNER_SHA" "$SERVER_SHA" "$LAUNCHER_SHA" >>"$temporary"
  printf '"browser_runner_sha256":"%s","browser_source_sha256":"%s","app_image":"%s",' \
    "$BROWSER_RUNNER_SHA" "$SOURCE_DIGEST" "$APP_IMAGE" >>"$temporary"
  printf '"web_image":"%s","browser_image":"%s"}' "$WEB_IMAGE" "$BROWSER_IMAGE" >>"$temporary"
  mv -f -- "$temporary" "$PRODUCTION_CONTROL/$ATTESTATION_NAME"
  stage attestation-written
}

# validate_owner_credential runs only in production, after the attestation
# passes. It checks metadata and never reads the file.
validate_owner_credential() {
  local credential=$MAIN/.dev/credentials/owner-test.env
  [ -f "$credential" ] && [ ! -L "$credential" ] && [ "$(realpath -e -- "$credential")" = "$credential" ] &&
    [ "$(stat -c '%u %a' -- "$credential")" = "$UID_NOW 600" ] ||
    fail 'owner credential must be a regular mode-0600 file owned by this user'
  CREDENTIAL=$credential
}

prepare_run_root() {
  local base
  if [ "$MODE" = local ]; then
    base=$REPO/.dev/mcp-workflow-local
    if [ -e "$base" ] || [ -L "$base" ]; then
      secure_dir "$base" || fail 'invalid local workflow directory'
      rm -rf -- "$base"
    fi

    install -d -m 0700 "$base" "$base/control" "$base/fixture"
    printf 'Content-Type: application/json\n' >"$base/fixture/json-header"
    CONTROL=$base/control
    SCRATCH=$base/fixture
  else
    [ -e "$PRODUCTION_CONTROL" ] || [ -L "$PRODUCTION_CONTROL" ] || install -d -m 0700 "$PRODUCTION_CONTROL"
    secure_dir "$PRODUCTION_CONTROL" || fail 'invalid production control directory'
    CONTROL=$PRODUCTION_CONTROL
  fi
  RUN=$(mktemp -d "$CONTROL/run.XXXXXX") && chmod 0700 "$RUN"
  install -d -m 0700 "$RUN/browser"
  BROWSER_EVIDENCE=$(mktemp -d "$STATE/mcp-sdk-browser.XXXXXX") && chmod 0700 "$BROWSER_EVIDENCE"
  BROWSER_LOG=$(mktemp "$STATE/mcp-sdk-browser-log.XXXXXX")
  RUNNER_LOG=$(mktemp "$STATE/mcp-sdk-runner-log.XXXXXX")
  CONTAINER_NAME=aboutme-mcp-sdk-$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
  [[ $CONTAINER_NAME =~ ^aboutme-mcp-sdk-[0-9a-f]{32}$ ]] || fail 'cannot name the browser container'
}

# write_synthetic_login generates run-private fixture values. They pass only
# through builtins and private files, never through arguments or output.
write_synthetic_login() {
  local suffix password
  suffix=$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')
  password=$(head -c 24 /dev/urandom | base64 -w0 | tr '+/' '-_' | tr -d '=')
  [[ $suffix =~ ^[0-9a-f]{16}$ && ${#password} -eq 32 ]] || fail 'cannot generate fixture values'
  FIXTURE_EMAIL="pa-test-mcp-$suffix@example.invalid" FIXTURE_PASSWORD=$password
  CREDENTIAL=$RUN/synthetic-login.env
  printf 'ABOUTME_TEST_EMAIL=%s\nABOUTME_TEST_PASSWORD=%s\n' "$FIXTURE_EMAIL" "$FIXTURE_PASSWORD" >"$CREDENTIAL"
  chmod 0600 "$CREDENTIAL"
}

# api sends one same-origin request to the local stack and requires the exact
# status. Secrets travel in private header and body files, never in curl
# arguments.
api() {
  local want=$1 label=$2 method=$3 path=$4 body=${5-} headers=${6-} form=${7-} status
  local -a args=(-sS --proto '=https' --cacert "$CA_INPUT/caddy-root.crt" --max-time 30
    -o "$SCRATCH/response" -w '%{http_code}' -b "$SCRATCH/cookies" -c "$SCRATCH/cookies"
    -H "Origin: $LOCAL_ORIGIN" -X "$method")
  [ -z "$headers" ] || args+=(-H "@$headers")
  [ -z "$body" ] || args+=(--data-binary "@$body")
  [ -z "$form" ] || args+=(-F "$form")
  status=$(curl "${args[@]}" "$LOCAL_ORIGIN$path" 2>/dev/null) || status=000
  [ "$status" = "$want" ] || fail "fixture request failed at $label"
}

session_headers() {
  local csrf
  api 200 session GET /api/v1/me
  csrf=$(jq -er '.data.csrfToken | select(type == "string" and test("^[A-Za-z0-9_-]+$"))' \
    "$SCRATCH/response") || fail 'fixture request failed at session'
  printf 'X-CSRF-Token: %s\nIdempotency-Key: %s\n' "$csrf" "$(</proc/sys/kernel/random/uuid)" \
    >"$SCRATCH/headers"
  [ -z "${1-}" ] || printf '%s\n' "$1" >>"$SCRATCH/headers"
}

sign_in() {
  rm -f -- "$SCRATCH/cookies"
  printf '{"email":"%s","password":"%s"}' "$FIXTURE_EMAIL" "$FIXTURE_PASSWORD" >"$SCRATCH/body"
  api 204 sign-in POST /api/v1/auth/password/login "$SCRATCH/body" "$SCRATCH/json-header"
}

sign_out() {
  session_headers
  api 204 sign-out POST /api/v1/auth/logout "" "$SCRATCH/headers"
  rm -f -- "$SCRATCH/cookies" "$SCRATCH/headers" "$SCRATCH/response"
}

source_document() {
  printf '%s' '{"schemaVersion":4,"personalDetails":{"fullName":"Lan Fixture","headline":"Fictional platform engineer","details":[]},"content":{"work":{"sectionType":"work","displayName":"Experience","iconKey":"briefcase","entries":[]}},'
  printf '%s' '"customization":{"font":{"family":"inter","baseSizePx":14},"colors":{"primary":"#1a1a1a","text":"#1a1a1a","background":"#ffffff"},"spacing":{"sectionGap":16,"entryGap":8,"lineHeight":1.4},"heading":{"style":"normal","showRule":false},'
  printf '%s' '"layout":{"columns":1,"sections":{"main":["work"],"sidebar":[]}},"sectionDisplay":{"skill":{"style":"text"},"language":{"style":"text"}},"pageFormat":"a4","dateFormat":"MM/YYYY"}}'
}

fixture_cleanup() {
  "$BIN/password-auth-fixture" cleanup --database-url "$NATIVE_DSN" >/dev/null 2>&1
}

# seed_fixture registers and verifies the reserved-prefix password account,
# creates one fictional English resume with a photo, and signs out.
seed_fixture() {
  local capture=$STATE/secrets/auth-email-capture-bearer token= deadline revision message
  [ -f "$capture" ] && [ ! -L "$capture" ] && [ "$(stat -c %u -- "$capture")" = "$UID_NOW" ] ||
    fail 'invalid capture secret'
  fixture_cleanup || fail 'fixture cleanup failed'
  FIXTURE_SEEDED=1
  printf 'Authorization: Bearer %s\n' "$(base64 -w0 -- "$capture" | tr '+/' '-_' | tr -d '=')" >"$SCRATCH/capture-header"
  curl -fsS --max-time 10 -X DELETE -H "@$SCRATCH/capture-header" "$CAPTURE_URL" >/dev/null 2>&1 ||
    fail 'fixture request failed at capture reset'
  printf '{"name":"Fictional Workflow Owner","email":"%s","password":"%s"}' \
    "$FIXTURE_EMAIL" "$FIXTURE_PASSWORD" >"$SCRATCH/body"
  api 202 register POST /api/v1/auth/password/register "$SCRATCH/body" "$SCRATCH/json-header"

  deadline=$((SECONDS + 30))
  while [ -z "$token" ] && ((SECONDS < deadline)); do
    if curl -fsS --max-time 10 -H "@$SCRATCH/capture-header" -o "$SCRATCH/messages" \
      "$CAPTURE_URL" 2>/dev/null; then
      message=$(jq -r --arg to "$FIXTURE_EMAIL" \
        'first(.messages[] | select(.kind == "verify" and .to == $to) | .text_body) // ""' "$SCRATCH/messages") || message=
      if [[ $message =~ \#token=([A-Za-z0-9_-]+) ]]; then token=${BASH_REMATCH[1]}; fi
    fi
    [ -n "$token" ] || sleep 0.5
  done
  rm -f -- "$SCRATCH/messages" "$SCRATCH/capture-header"
  [ -n "$token" ] || fail 'fixture request failed at verification mail'
  printf '{"token":"%s"}' "$token" >"$SCRATCH/body"
  api 204 verify POST /api/v1/auth/password/verify "$SCRATCH/body" "$SCRATCH/json-header"
  sign_in
  source_document | jq -c --arg title 'Fictional English resume' '{title: $title, lng: "en", document: .}' \
    >"$SCRATCH/body"
  session_headers 'Content-Type: application/json'
  api 201 create POST /api/v1/resumes "$SCRATCH/body" "$SCRATCH/headers"
  SOURCE_ID=$(jq -er '.data.id' "$SCRATCH/response") &&
    revision=$(jq -er '.data.revision' "$SCRATCH/response") || fail 'fixture request failed at create'
  session_headers "If-Match: \"r$revision\""
  api 200 photo POST "/api/v1/resumes/$SOURCE_ID/photo" "" "$SCRATCH/headers" \
    "file=@$REPO/apps/server/cmd/native-http-fixture/testdata/photo.png;type=image/png"
  SOURCE_REVISION=$(jq -er '.data.revision' "$SCRATCH/response") || fail 'fixture request failed at photo'
  sign_out && rm -f -- "$SCRATCH/body"
}

# supply_candidate writes the fictional Vietnamese candidate and its
# digest-bound review for the local proof. It changes only translatable text.
supply_candidate() {
  local source=$RUN/source.json candidate review source_sha candidate_sha
  secure_file "$source" || fail 'invalid source handoff'
  candidate=$(mktemp "$RUN/.candidate.XXXXXX")
  review=$(mktemp "$RUN/.review.XXXXXX")
  jq -c --arg headline "$TARGET_HEADLINE" --arg section "$TARGET_SECTION" '
    (if (.personalDetails.headline? | type) == "string" then .personalDetails.headline = $headline else . end)
    | (if (.content.work.displayName? | type) == "string" then .content.work.displayName = $section else . end)' \
    "$source" >"$candidate" || fail 'cannot write the candidate'
  source_sha=$(file_sha256 "$source") && candidate_sha=$(file_sha256 "$candidate") ||
    fail 'cannot hash the candidate'
  printf '{"source_digest":"%s","candidate_digest":"%s","facts_preserved":true}' \
    "$source_sha" "$candidate_sha" >"$review"
  chmod 0600 "$candidate" "$review"
  mv -f -- "$candidate" "$RUN/candidate.json"
  mv -f -- "$review" "$RUN/candidate-review.json"
}

stop_process() {
  local pid=$1 waited=0
  [ -n "$pid" ] || return 0
  kill -TERM "$pid" 2>/dev/null || return 0
  while kill -0 "$pid" 2>/dev/null && ((waited < 20)); do
    sleep 0.5
    waited=$((waited + 1))
  done
  kill -KILL "$pid" 2>/dev/null || true
}

# run_joined starts the host runner and the browser container together and
# waits for both. A failure of either stops the other; both are reaped. When
# the runner succeeds without a browser handoff (a production sentinel stop or
# revocation-only recovery), the idle helper is stopped as expected.
run_joined() {
  local supplied=0 deadline=$((SECONDS + RUN_LIMIT_SECONDS)) runner_done_at=
  RUNNER_STATUS= BROWSER_STATUS= BROWSER_STOPPED=0 BROWSER_SIGNALLED=0 OUTCOME=
  stage run
  # Caddy serves the local origin from its own CA and never installs it in a
  # system store, so the local runner trusts exactly the exported root.
  # Production keeps the system store and drops any inherited override.
  local -a trust=(env -u SSL_CERT_FILE -u SSL_CERT_DIR)
  [ "$MODE" != local ] || trust=(env "SSL_CERT_FILE=$CA_INPUT/caddy-root.crt" "SSL_CERT_DIR=$CA_INPUT")
  (cd "$REPO" && exec "${trust[@]}" "$RUNNER" "$MODE" "$RUN" "$RUN/browser") \
    </dev/null >/dev/null 2>"$RUNNER_LOG" {ERR_FD}>&- &
  RUNNER_PID=$!
  "$CONTEXT/run.sh" "$BROWSER_IMAGE" "$CA_INPUT" "$STAGING" "$BROWSER_EVIDENCE" mcp-sdk \
    "$MODE" "$RUN/browser" "$CREDENTIAL" "$CONTAINER_NAME" </dev/null >"$BROWSER_LOG" 2>&1 \
    {ERR_FD}>&- {LOCK_FD}>&- &
  BROWSER_PID=$!
  while [ -z "$RUNNER_STATUS" ] || [ -z "$BROWSER_STATUS" ]; do
    if [ -z "$RUNNER_STATUS" ] && ! kill -0 "$RUNNER_PID" 2>/dev/null; then
      if wait "$RUNNER_PID"; then RUNNER_STATUS=0; else RUNNER_STATUS=$?; fi
      RUNNER_PID= runner_done_at=$SECONDS
      if [ "$RUNNER_STATUS" -ne 0 ]; then
        [ -z "$BROWSER_PID" ] || BROWSER_SIGNALLED=1
        stop_process "$BROWSER_PID"
      else
        # A sentinel stop or revocation recovery needs no browser handoff.
        OUTCOME=$(check_evidence) || OUTCOME=rejected
        if [ -n "$BROWSER_PID" ] && [ "$OUTCOME" != completed ] && [ "$OUTCOME" != rejected ]; then
          BROWSER_STOPPED=1 && stop_process "$BROWSER_PID"
        fi
      fi
    fi
    if [ -z "$BROWSER_STATUS" ] && ! kill -0 "$BROWSER_PID" 2>/dev/null; then
      if wait "$BROWSER_PID"; then BROWSER_STATUS=0; else BROWSER_STATUS=$?; fi
      BROWSER_PID=
      [ "$BROWSER_STATUS" -eq 0 ] || [ "$BROWSER_STOPPED" -eq 1 ] || stop_process "$RUNNER_PID"
    fi
    if [ "$MODE" = local ] && [ "$supplied" -eq 0 ] && [ -z "$RUNNER_STATUS" ] &&
      [ -e "$RUN/source.json" ]; then
      stage candidate && supply_candidate && supplied=1
    fi
    if ((SECONDS >= deadline)) ||
      { [ -n "$runner_done_at" ] && ((SECONDS - runner_done_at >= STOP_GRACE_SECONDS)); }; then
      stop_process "$RUNNER_PID" && stop_process "$BROWSER_PID"
    fi
    [ -n "$RUNNER_STATUS" ] && [ -n "$BROWSER_STATUS" ] || sleep 0.2
  done
}

report_browser_stage() {
  local line
  line=$(grep -E '^dev-https-browser: mcp-sdk-stage:[a-z0-9-]+$' "$BROWSER_LOG" | tail -n 1 || true)
  [ -z "$line" ] || say "browser ${line#dev-https-browser: }"
}

# report_failure names the failure in closed words only: the runner exit
# status and reason, the stage and revocation status of any evidence, the
# helper's result code, and whether the launcher stopped the helper.
# runner_reason prints the runner's one closed failure word, and nothing at
# all for any other content on that stream.
runner_reason() {
  local -a lines=()
  mapfile -t lines <"$RUNNER_LOG" || return 0
  [ "${#lines[@]}" -eq 1 ] && [[ ${lines[0]} =~ ^mcp-workflow:\ ([a-z-]+)$ ]] &&
    [[ $RUNNER_REASONS == *" ${BASH_REMATCH[1]} "* ]] || return 0
  printf '%s' "${BASH_REMATCH[1]}"
}

report_failure() {
  local evidence result outcome reason
  evidence=$(jq -er 'select((.stage | IN("owner_workflow", "revocation_recovery"))
    and (.revocation | IN("revoked", "revocation_unconfirmed"))) | "\(.stage) \(.revocation)"' \
    "$RUN/evidence.json" 2>/dev/null) || evidence=none
  result=$(cat "$RUN/browser/browser-result.json" 2>/dev/null) || result=
  case $result in
  completed | login_failed | origin_rejected | consent_failed | timeout) ;;
  *) result=none ;;
  esac
  say "runner exit $RUNNER_STATUS"
  say "runner evidence $evidence"
  if [ "$evidence" = none ]; then
    reason=$(runner_reason)
    [ -z "$reason" ] || say "runner reason $reason"
  fi
  say "browser result $result"
  report_browser_stage
  [ "$RUNNER_STATUS" -eq 0 ] || say 'runner failed'
  if [ "$BROWSER_STATUS" -ne 0 ]; then
    outcome="failed with exit $BROWSER_STATUS"
    if [ "$BROWSER_SIGNALLED" -eq 1 ] && [ "$BROWSER_STATUS" -gt 128 ]; then
      outcome='stopped after runner failure'
    fi
    say "browser helper $outcome"
  fi
}

# check_evidence accepts only the runner's closed allowlisted evidence shape
# and prints completed, recovered, or already-complete. Local proof must show
# the replayed create.
check_evidence() {
  local path=$RUN/evidence.json
  if [ ! -e "$path" ] && [ ! -L "$path" ]; then
    [ "$MODE" = production ] && echo already-complete
    return
  fi
  secure_file "$path" && [ "$(stat -c %s -- "$path")" -le 4096 ] || return 1
  jq -er --arg mode "$MODE" '
    {version: 1, mode: $mode, stage: "owner_workflow", sdk_version: "go-sdk v1.7.0",
      transport: "streamable-http", tool_count: 15, language: "vi", source_unchanged: true,
      target_private: true, count_delta: 1, revocation: "revoked", post_revocation_401: true} as $done
    | if $mode == "production" and . == ($done + {stage: "revocation_recovery", sdk_version: "",
        transport: "", tool_count: 0, language: "", source_unchanged: false, target_private: false,
        count_delta: 0, create_reconciliation: "", post_revocation_401: false}) then "recovered"
      elif . == ($done + {create_reconciliation: "replayed"}) then "completed"
      elif $mode == "production" and (.create_reconciliation | IN("created", "reconciled"))
        and . == ($done + {create_reconciliation: .create_reconciliation}) then "completed"
      else error("rejected") end' "$path" 2>/dev/null
}

# inspect_target reads the private target through the owner API, with no
# screenshot, trace, video, console attachment, or retained report or media.
inspect_target() {
  local target
  stage inspect
  sign_in
  api 200 inspect GET /api/v1/resumes
  target=$(jq -er --arg source "$SOURCE_ID" --arg revision "$SOURCE_REVISION" --arg title "$TARGET_TITLE" '
    .data | select(length == 2)
    | select(any(.[]; .id == $source and .lng == "en" and .revision == $revision and .live == false))
    | map(select(.id != $source)) | select(length == 1) | .[0]
    | select(.lng == "vi" and .title == $title and .live == false and .slug == null) | .id
  ' "$SCRATCH/response") || fail 'target inspection failed'
  api 200 inspect GET "/api/v1/resumes/$target"
  jq -e --arg headline "$TARGET_HEADLINE" '.data.live == false and .data.slug == null
    and .data.document.personalDetails.headline == $headline
    and (.data.document.personalDetails.photo | type) == "object"' "$SCRATCH/response" >/dev/null ||
    fail 'target inspection failed'
  api 200 inspect GET "/api/v1/resumes/$target/photo"
  rm -f -- "$SCRATCH/response" && sign_out
}

# cleanup runs on every exit. Known local debris outlives it: each local run
# leaves one client registration row named "aboutme MCP owner workflow" in
# aboutme_dev.oauth_clients, and deleting the fixture user can leave its photo
# objects in the local media store. Both are disposable local data.
cleanup() {
  local status=$? path
  trap - EXIT
  stop_process "${RUNNER_PID-}"
  stop_process "${BROWSER_PID-}"
  if [ -n "${CONTAINER_NAME-}" ] && ! podman rm -f --ignore "$CONTAINER_NAME" >/dev/null 2>&1; then
    printf '%s: %s\n' "$NAME" 'browser container cleanup failed' >&"$ERR_FD"
    status=1
  fi
  for path in "${STAGING-}" "${BROWSER_EVIDENCE-}" "${SCRATCH-}" "${BROWSER_LOG-}" "${RUNNER_LOG-}" \
    "${REGISTRY_HEADERS-}"; do
    [ -z "$path" ] || rm -rf -- "$path"
  done
  if [ -n "${RUN-}" ] && [ -d "$RUN" ]; then
    rm -rf -- "$RUN/browser"
    rm -f -- "$RUN/synthetic-login.env" "$RUN/source.json" "$RUN/candidate.json" \
      "$RUN/candidate-review.json" "$RUN"/.candidate.* "$RUN"/.review.*
  fi
  if [ "${FIXTURE_SEEDED-0}" -eq 1 ] && ! fixture_cleanup; then
    printf '%s: %s\n' "$NAME" 'fixture cleanup failed' >&"$ERR_FD"
    status=1
  fi
  exit "$status"
}

main() {
  [ "$#" -eq 1 ] && { [ "$1" = local ] || [ "$1" = production ]; } || {
    printf 'usage: %s local|production\n' "$NAME" >&2
    exit 2
  }
  MODE=$1
  exec {ERR_FD}>&2 2>/dev/null
  UID_NOW=$(id -u)
  [ "$UID_NOW" -ne 0 ] || fail 'the workflow must not run as root'
  RUNNER_PID= BROWSER_PID= FIXTURE_SEEDED=0 STAGING= BROWSER_EVIDENCE= BROWSER_LOG= RUN= SCRATCH=
  REGISTRY_HEADERS= CONTAINER_NAME= RUNNER_LOG=
  trap cleanup EXIT
  stage preflight
  resolve_paths
  take_lock
  [ "$MODE" != local ] || take_local_check_lock
  validate_browser_state
  stage build && build_tools
  stage browser-sources && stage_browser_sources
  current_identity
  if [ "$MODE" = production ]; then
    stage attestation && validate_attestation
    stage credential && validate_owner_credential
    prepare_run_root
  else
    prepare_run_root && write_synthetic_login
    stage fixture && seed_fixture
  fi
  run_joined
  if [ "$RUNNER_STATUS" -ne 0 ] || { [ "$BROWSER_STATUS" -ne 0 ] && [ "$BROWSER_STOPPED" -eq 0 ]; }; then
    report_failure
    fail 'workflow failed'
  fi
  stage evidence
  [ "$OUTCOME" != rejected ] || fail 'evidence rejected'
  if [ "$MODE" = local ]; then
    inspect_target
    stage cleanup
    fixture_cleanup || fail 'fixture cleanup failed'
    FIXTURE_SEEDED=0 && write_attestation
  fi
  stage "$OUTCOME"
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  main "$@"
fi
