#!/usr/bin/env bash

# One entry point for the trusted-browser proofs (auth, transport, editor,
# public, password-auth, MCP, and entry). Stages an immutable per-run copy of
# the spec
# sources and mounts it into the pinned browser image, so editing a spec
# never requires an image rebuild; the image manifest gates only the
# image-side sources (Dockerfile, run.sh, package manifests).
set -Eeuo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
REPO=$PWD
STATE=$REPO/.dev/native-https
MANIFEST=$STATE/browser-image.manifest
INPUT=$STATE/input
EVIDENCE_ROOT=$STATE/evidence
CONTEXT=$REPO/deploy/dev-https-browser
NATIVE_DSN='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable'

# Image-side sources: baked into the image, recorded in the manifest, and
# re-verified here so image drift still blocks a check. Order and digest
# construction must match the Makefile dev-https-browser-image recipe.
readonly -a IMAGE_SOURCES=(
  deploy/dev-https-browser/Dockerfile
  deploy/dev-https-browser/package.json
  deploy/dev-https-browser/package-lock.json
  deploy/dev-https-browser/run.sh
)

# Spec-side sources: staged per run, never gated on the image manifest.
readonly -a SPEC_SOURCES=(
  playwright.config.ts
  auth.spec.ts
  transport.spec.ts
  editor.spec.ts
  public.spec.ts
  password-auth.spec.ts
  mcp.spec.ts
  entry.spec.ts
  editor-fixtures.ts
  network-policy.ts
  harness-lib.ts
)

EVIDENCE_KEEP=10

MODE=${1-}
TARGET="dev-https-${MODE}-check"

fail() {
  printf '%s: %s\n' "$TARGET" "$*" >&2
  exit 1
}

case $MODE in
auth) evidence_prefix=google-auth ;;
transport) evidence_prefix=transport ;;
editor) evidence_prefix=editor ;;
public) evidence_prefix=public ;;
password-auth)
  evidence_prefix=password
  TARGET=dev-https-password-check
  ;;
mcp) evidence_prefix=mcp ;;
entry) evidence_prefix=entry ;;
*)
  TARGET=dev-https-check
  fail 'usage: dev-https-check.sh auth|transport|editor|public|password-auth|mcp|entry'
  ;;
esac

UID_NOW=$(id -u)

image_source_hash() {
  # Byte-identical to the Makefile dev-https-browser-image digest: repo-root
  # relative paths with full sha256sum output lines.
  local path
  {
    for path in "${IMAGE_SOURCES[@]}"; do
      [ -f "$path" ] && [ ! -L "$path" ] || return 1
      printf '%s\0' "$path"
      sha256sum -- "$path"
    done
  } | sha256sum | awk '{print $1}'
}

spec_source_hash() {
  # Content-only digest of the staged specs; staging paths are random per
  # run, so path strings must not enter the digest.
  local base=$1 path
  {
    for path in "${SPEC_SOURCES[@]}"; do
      [ -f "$base/$path" ] && [ ! -L "$base/$path" ] || return 1
      printf '%s\0' "$path"
      sha256sum -- "$base/$path" | awk '{print $1}'
    done
  } | sha256sum | awk '{print $1}'
}

require_secure_dir() {
  local path=$1 label=$2
  [ -d "$path" ] && [ ! -L "$path" ] &&
    [ "$(realpath -e -- "$path")" = "$path" ] || fail "invalid $label"
  [ "$(stat -c %u "$path")" = "$UID_NOW" ] &&
    [ "$(stat -c %a "$path")" = 700 ] ||
    fail "$label ownership or mode mismatch"
}

require_secure_dir "$STATE" 'state directory'

[ -f "$MANIFEST" ] && [ ! -L "$MANIFEST" ] &&
  [ "$(stat -c %u "$MANIFEST")" = "$UID_NOW" ] &&
  [ "$(stat -c %a "$MANIFEST")" = 600 ] || fail 'invalid browser image manifest'
mapfile -t manifest_lines <"$MANIFEST"
[ "${#manifest_lines[@]}" -eq 2 ] || fail 'malformed browser image manifest'
[[ ${manifest_lines[0]} =~ ^image_id=(sha256:[0-9a-f]{64})$ ]] ||
  fail 'malformed browser image ID'
image_id=${BASH_REMATCH[1]}
[[ ${manifest_lines[1]} =~ ^source_sha256=([0-9a-f]{64})$ ]] ||
  fail 'malformed browser source hash'
recorded_source=${BASH_REMATCH[1]}

current_source=$(image_source_hash) ||
  fail 'cannot hash browser image sources'
[ "$current_source" = "$recorded_source" ] ||
  fail 'browser image sources changed after image build; rerun make dev-https-browser-image'

require_secure_dir "$INPUT" 'CA input directory'
mapfile -t input_entries < <(find "$INPUT" -mindepth 1 -maxdepth 1 -printf '%f\n')
[ "${#input_entries[@]}" -eq 1 ] && [ "${input_entries[0]}" = caddy-root.crt ] ||
  fail 'CA input must contain one root'
[ -f "$INPUT/caddy-root.crt" ] && [ ! -L "$INPUT/caddy-root.crt" ] &&
  [ "$(stat -c %u "$INPUT/caddy-root.crt")" = "$UID_NOW" ] &&
  [ "$(stat -c %a "$INPUT/caddy-root.crt")" = 600 ] || fail 'invalid Caddy root'

if [[ -e "$EVIDENCE_ROOT" || -L "$EVIDENCE_ROOT" ]]; then
  require_secure_dir "$EVIDENCE_ROOT" 'evidence root'
else
  install -d -m 0700 "$EVIDENCE_ROOT"
fi

staging=
password_input=
mcp_input=
mcp_fixture=
mcp_client_name=
mcp_seeded=0
cleanup() {
  [ -z "$staging" ] || rm -rf -- "$staging"
  [ -z "$password_input" ] || rm -rf -- "$password_input"
  [ -z "$mcp_input" ] || rm -rf -- "$mcp_input"
  if [ "$mcp_seeded" -eq 1 ] && [ -n "$mcp_fixture" ]; then
    "$mcp_fixture" cleanup --database-url "$NATIVE_DSN" \
      --client-name "$mcp_client_name" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

staging=$(mktemp -d "$STATE/spec-input.XXXXXX")
chmod 0700 "$staging"
for path in "${SPEC_SOURCES[@]}"; do
  [ -f "$CONTEXT/$path" ] && [ ! -L "$CONTEXT/$path" ] ||
    fail "spec source $path is missing or not a regular file"
  cp -- "$CONTEXT/$path" "$staging/$path"
  chmod 0600 "$staging/$path"
done
spec_sha=$(spec_source_hash "$staging") ||
  fail 'cannot hash staged spec sources'

run_input=$INPUT
if [ "$MODE" = password-auth ]; then
  capture_secret=$STATE/secrets/auth-email-capture-bearer
  [ -f "$capture_secret" ] && [ ! -L "$capture_secret" ] &&
    [ "$(stat -c %u "$capture_secret")" = "$UID_NOW" ] ||
    fail 'invalid capture secret'
  password_input=$STATE/password-input
  rm -rf -- "$password_input"
  install -d -m 0700 "$password_input"
  cp -- "$INPUT/caddy-root.crt" "$password_input/caddy-root.crt"
  chmod 0600 "$password_input/caddy-root.crt"
  capture_token=$(base64 -w0 -- "$capture_secret" | tr '+/' '-_' | tr -d '=')
  printf '%s' "$capture_token" >"$password_input/mail-capture-token"
  chmod 0600 "$password_input/mail-capture-token"
  run_input=$password_input

  install -d -m 0700 "$REPO/.dev/bin"
  (cd "$REPO/apps/server" &&
    go build -o "$REPO/.dev/bin/password-auth-fixture" \
      ./cmd/password-auth-fixture) || fail 'fixture build failed'
  "$REPO/.dev/bin/password-auth-fixture" cleanup --database-url "$NATIVE_DSN"
  "$REPO/.dev/bin/password-auth-fixture" seed --database-url "$NATIVE_DSN"
  curl -fsS -X DELETE -H "Authorization: Bearer $capture_token" \
    "http://127.0.0.1:20444/api/messages" >/dev/null
elif [ "$MODE" = mcp ]; then
  mcp_run_id=$(</proc/sys/kernel/random/uuid)
  [[ $mcp_run_id =~ ^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ ]] ||
    fail 'cannot create an MCP run identifier'
  mcp_client_name="aboutme MCP UAT $mcp_run_id"
  mcp_input=$(mktemp -d "$STATE/mcp-input.XXXXXX")
  chmod 0700 "$mcp_input"
  cp -- "$INPUT/caddy-root.crt" "$mcp_input/caddy-root.crt"
  printf '%s\n' "$mcp_client_name" >"$mcp_input/mcp-client-name"
  chmod 0600 "$mcp_input/caddy-root.crt" "$mcp_input/mcp-client-name"
  run_input=$mcp_input

  install -d -m 0700 "$REPO/.dev/bin"
  mcp_fixture=$REPO/.dev/bin/mcp-uat-fixture
  (cd "$REPO/apps/server" &&
    go build -o "$mcp_fixture" ./cmd/mcp-uat-fixture) ||
    fail 'MCP fixture build failed'
  "$mcp_fixture" cleanup --database-url "$NATIVE_DSN" \
    --client-name "$mcp_client_name"
  mcp_seeded=1
  "$mcp_fixture" seed --database-url "$NATIVE_DSN" \
    --client-name "$mcp_client_name"
elif [ "$MODE" = entry ]; then
  install -d -m 0700 "$REPO/.dev/bin"
  (cd "$REPO/apps/server" &&
    go build -o "$REPO/.dev/bin/dev-seed" ./cmd/dev-seed) ||
    fail 'dev-seed build failed'
  "$REPO/.dev/bin/dev-seed" seed --database-url "$NATIVE_DSN"
fi

evidence=$(mktemp -d "$EVIDENCE_ROOT/$evidence_prefix.XXXXXX")
[ "$(stat -c %u "$evidence")" = "$UID_NOW" ] &&
  [ "$(stat -c %a "$evidence")" = 700 ] ||
  fail 'evidence directory ownership or mode mismatch'

status=0
"$CONTEXT/run.sh" "$image_id" "$run_input" "$staging" "$evidence" "$MODE" ||
  status=$?

if [ "$MODE" = password-auth ]; then
  "$REPO/.dev/bin/password-auth-fixture" cleanup --database-url "$NATIVE_DSN"
elif [ "$MODE" = mcp ]; then
  if "$mcp_fixture" cleanup --database-url "$NATIVE_DSN" \
    --client-name "$mcp_client_name"; then
    mcp_seeded=0
  else
    status=1
  fi
fi
[ "$status" -eq 0 ] || fail 'browser proof failed'

# Keep only the newest EVIDENCE_KEEP runs for this mode.
mapfile -t old_evidence < <(
  find "$EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d \
    -name "$evidence_prefix.*" -printf '%T@ %p\n' |
    sort -rn | awk '{print $2}' | tail -n "+$((EVIDENCE_KEEP + 1))"
)
for old in "${old_evidence[@]}"; do
  case $old in
  "$EVIDENCE_ROOT"/*) rm -rf -- "$old" ;;
  *) fail 'refusing to prune outside the evidence root' ;;
  esac
done

printf '%s evidence: %s (spec sha256 %s)\n' "$TARGET" "$evidence" "$spec_sha"
