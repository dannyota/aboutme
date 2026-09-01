#!/usr/bin/env bash

# One entry point for the trusted-browser proofs (auth, transport, editor,
# public, password-auth). Stages an immutable per-run copy of the spec
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
*)
  TARGET=dev-https-check
  fail 'usage: dev-https-check.sh auth|transport|editor|public|password-auth'
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
cleanup() {
  [ -z "$staging" ] || rm -rf -- "$staging"
  [ -z "$password_input" ] || rm -rf -- "$password_input"
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
