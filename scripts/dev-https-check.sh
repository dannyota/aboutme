#!/usr/bin/env bash

# One entry point for the trusted-browser proofs (auth, transport, editor,
# public, password-auth, MCP, entry, publish, exports, privacy, sample-start,
# passkey, and totp). Stages an immutable per-run copy of the spec sources
# and mounts it into the pinned browser image, so editing a spec
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
  deploy/dev-https-browser/verify-evidence.mjs
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
  mcp-sdk.spec.ts
  entry.spec.ts
  publish.spec.ts
  exports.spec.ts
  privacy.spec.ts
  sample-start.spec.ts
  linkedin.spec.ts
  second-factor.spec.ts
  totp.spec.ts
  totp-fixture.ts
  totp-production.spec.ts
  production.config.ts
  editor-fixtures.ts
  network-policy.ts
  harness-lib.ts
  second-factor-lib.ts
  second-factor-pages.ts
  proof-shards.mjs
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
publish) evidence_prefix=publish ;;
exports) evidence_prefix=exports ;;
privacy) evidence_prefix=privacy ;;
sample-start) evidence_prefix=sample-start ;;
linkedin) evidence_prefix=linkedin ;;
passkey)
  # Two bounded phases, one per server enrollment flag. Both evidence
  # directories start with "passkey-" so the hosted job uploads exactly them.
  evidence_prefix=passkey-enabled
  TARGET=dev-https-passkey-check
  ;;
totp)
  # Two bounded phases, one per server enrollment flag. Both evidence
  # directories start with "totp-" so the hosted job uploads exactly them.
  evidence_prefix=totp-enabled
  TARGET=dev-https-totp-check
  ;;
*)
  TARGET=dev-https-check
  fail 'usage: dev-https-check.sh auth|transport|editor|public|password-auth|mcp|entry|publish|exports|privacy|sample-start|linkedin|passkey|totp'
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
passkey_input=
totp_input=
linkedin_input=
mcp_fixture=
mcp_client_name=
mcp_seeded=0
cleanup() {
  [ -z "$staging" ] || rm -rf -- "$staging"
  [ -z "$password_input" ] || rm -rf -- "$password_input"
  [ -z "$mcp_input" ] || rm -rf -- "$mcp_input"
  [ -z "$passkey_input" ] || rm -rf -- "$passkey_input"
  [ -z "$totp_input" ] || rm -rf -- "$totp_input"
  [ -z "$linkedin_input" ] || rm -rf -- "$linkedin_input"
  if [ "$mcp_seeded" -eq 1 ] && [ -n "$mcp_fixture" ]; then
    "$mcp_fixture" cleanup --database-url "$NATIVE_DSN" \
      --client-name "$mcp_client_name" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

# new_client_name prints a fresh, uniquely named local OAuth client for a run
# that registers a connected agent.
new_client_name() {
  local run_id
  run_id=$(</proc/sys/kernel/random/uuid)
  [[ $run_id =~ ^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ ]] ||
    return 1
  printf 'aboutme MCP UAT %s' "$run_id"
}

# assert_server_flag proves the running server environment carries the exact
# named enrollment flag this phase needs.
assert_server_flag() {
  local name=$1 want=$2 env_file=$STATE/run/server.env
  [ -f "$env_file" ] && [ ! -L "$env_file" ] ||
    fail 'server environment file is missing'
  grep -Fqx "$name=$want" "$env_file" ||
    fail "server environment does not carry $name=$want"
}

# prune_evidence keeps only the newest EVIDENCE_KEEP runs of one prefix.
prune_evidence() {
  local prefix=$1 old
  local -a stale=()
  mapfile -t stale < <(
    find "$EVIDENCE_ROOT" -mindepth 1 -maxdepth 1 -type d \
      -name "$prefix.*" -printf '%T@ %p\n' |
      sort -rn | awk '{print $2}' | tail -n "+$((EVIDENCE_KEEP + 1))"
  )
  for old in "${stale[@]}"; do
    case $old in
    "$EVIDENCE_ROOT"/*) rm -rf -- "$old" ;;
    *) fail 'refusing to prune outside the evidence root' ;;
    esac
  done
}

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

if [ "$MODE" = passkey ]; then
  # The enabled phase registers a connected agent and reads security mail, so
  # its input carries the capture token and this run's client name beside the
  # Caddy root. The disabled phase needs only the root.
  capture_secret=$STATE/secrets/auth-email-capture-bearer
  [ -f "$capture_secret" ] && [ ! -L "$capture_secret" ] &&
    [ "$(stat -c %u "$capture_secret")" = "$UID_NOW" ] ||
    fail 'invalid capture secret'
  mcp_client_name=$(new_client_name) || fail 'cannot create a run client name'
  passkey_input=$(mktemp -d "$STATE/passkey-input.XXXXXX")
  chmod 0700 "$passkey_input"
  cp -- "$INPUT/caddy-root.crt" "$passkey_input/caddy-root.crt"
  capture_token=$(base64 -w0 -- "$capture_secret" | tr '+/' '-_' | tr -d '=')
  printf '%s' "$capture_token" >"$passkey_input/mail-capture-token"
  printf '%s\n' "$mcp_client_name" >"$passkey_input/mcp-client-name"
  chmod 0600 "$passkey_input/caddy-root.crt" \
    "$passkey_input/mail-capture-token" "$passkey_input/mcp-client-name"

  install -d -m 0700 "$REPO/.dev/bin"
  mcp_fixture=$REPO/.dev/bin/mcp-uat-fixture
  (cd "$REPO/apps/server" &&
    go build -o "$mcp_fixture" ./cmd/mcp-uat-fixture) ||
    fail 'MCP fixture build failed'
  "$mcp_fixture" cleanup --database-url "$NATIVE_DSN" \
    --client-name "$mcp_client_name"
  mcp_seeded=1
  curl -fsS -X DELETE -H "Authorization: Bearer $capture_token" \
    "http://127.0.0.1:20444/api/messages" >/dev/null

  status=0
  assert_server_flag PASSKEY_ENROLLMENT_ENABLED true
  enabled_evidence=$(mktemp -d "$EVIDENCE_ROOT/passkey-enabled.XXXXXX")
  [ "$(stat -c %u "$enabled_evidence")" = "$UID_NOW" ] &&
    [ "$(stat -c %a "$enabled_evidence")" = 700 ] ||
    fail 'evidence directory ownership or mode mismatch'
  "$CONTEXT/run.sh" "$image_id" "$passkey_input" "$staging" \
    "$enabled_evidence" second-factor || status=$?

  disabled_evidence=none
  # The disabled-enrollment phase does not depend on which roles the enabled
  # phase sharded (second-factor.spec.ts "Enabled-proof sharding"); only the
  # primary-disabled shard, or an unsharded run, proves it, so the
  # recovery-attempts shard does not repeat it for no added coverage.
  run_disabled_phase=1
  case ${ABOUTME_PASSKEY_SHARD-} in
  recovery-attempts) run_disabled_phase=0 ;;
  esac
  if [ "$status" -eq 0 ] && [ "$run_disabled_phase" -eq 1 ]; then
    # The runner-local database keeps its rows across this restart; only the
    # server enrollment flag changes.
    bash "$REPO/scripts/dev-https.sh" down ||
      fail 'cannot stop the harness between passkey phases'
    DEV_HTTPS_PASSKEY_ENROLLMENT=false bash "$REPO/scripts/dev-https.sh" up ||
      fail 'cannot restart the harness with passkey enrollment disabled'
    assert_server_flag PASSKEY_ENROLLMENT_ENABLED false
    disabled_evidence=$(mktemp -d "$EVIDENCE_ROOT/passkey-disabled.XXXXXX")
    [ "$(stat -c %u "$disabled_evidence")" = "$UID_NOW" ] &&
      [ "$(stat -c %a "$disabled_evidence")" = 700 ] ||
      fail 'evidence directory ownership or mode mismatch'
    "$CONTEXT/run.sh" "$image_id" "$INPUT" "$staging" \
      "$disabled_evidence" second-factor-disabled || status=$?
    # Leave no stack running at an unexpected flag; the operator or the hosted
    # job starts a fresh one.
    bash "$REPO/scripts/dev-https.sh" down || status=1
  fi

  if "$mcp_fixture" cleanup --database-url "$NATIVE_DSN" \
    --client-name "$mcp_client_name"; then
    mcp_seeded=0
  else
    status=1
  fi
  prune_evidence passkey-enabled
  prune_evidence passkey-disabled
  [ "$status" -eq 0 ] || fail 'browser proof failed'
  printf '%s evidence: %s and %s (spec sha256 %s)\n' \
    "$TARGET" "$enabled_evidence" "$disabled_evidence" "$spec_sha"
  exit 0
fi

if [ "$MODE" = totp ]; then
  # Two bounded phases, one per server TOTP enrollment flag. The enabled
  # phase registers a connected agent, links a provider, and reads security
  # mail, so its input carries the capture token and this run's client name
  # beside the Caddy root, exactly like the passkey mode above. The disabled
  # phase needs only the root
  # (docs/design/totp-second-factor-contract.md "Migration, mixed versions,
  # and loss").
  capture_secret=$STATE/secrets/auth-email-capture-bearer
  [ -f "$capture_secret" ] && [ ! -L "$capture_secret" ] &&
    [ "$(stat -c %u "$capture_secret")" = "$UID_NOW" ] ||
    fail 'invalid capture secret'
  mcp_client_name=$(new_client_name) || fail 'cannot create a run client name'
  totp_input=$(mktemp -d "$STATE/totp-input.XXXXXX")
  chmod 0700 "$totp_input"
  cp -- "$INPUT/caddy-root.crt" "$totp_input/caddy-root.crt"
  capture_token=$(base64 -w0 -- "$capture_secret" | tr '+/' '-_' | tr -d '=')
  printf '%s' "$capture_token" >"$totp_input/mail-capture-token"
  printf '%s\n' "$mcp_client_name" >"$totp_input/mcp-client-name"
  chmod 0600 "$totp_input/caddy-root.crt" \
    "$totp_input/mail-capture-token" "$totp_input/mcp-client-name"

  install -d -m 0700 "$REPO/.dev/bin"
  mcp_fixture=$REPO/.dev/bin/mcp-uat-fixture
  (cd "$REPO/apps/server" &&
    go build -o "$mcp_fixture" ./cmd/mcp-uat-fixture) ||
    fail 'MCP fixture build failed'
  "$mcp_fixture" cleanup --database-url "$NATIVE_DSN" \
    --client-name "$mcp_client_name"
  mcp_seeded=1
  curl -fsS -X DELETE -H "Authorization: Bearer $capture_token" \
    "http://127.0.0.1:20444/api/messages" >/dev/null

  status=0
  assert_server_flag TOTP_ENROLLMENT_ENABLED true
  enabled_evidence=$(mktemp -d "$EVIDENCE_ROOT/totp-enabled.XXXXXX")
  [ "$(stat -c %u "$enabled_evidence")" = "$UID_NOW" ] &&
    [ "$(stat -c %a "$enabled_evidence")" = 700 ] ||
    fail 'evidence directory ownership or mode mismatch'
  "$CONTEXT/run.sh" "$image_id" "$totp_input" "$staging" \
    "$enabled_evidence" totp || status=$?

  disabled_evidence=none
  # The disabled-enrollment phase does not depend on which roles the enabled
  # phase sharded (totp.spec.ts "Enabled-proof sharding"); only a run whose
  # comma-separated shard list includes epoch-disabled, or an unsharded run,
  # proves it, so the other shards do not repeat it for no added coverage.
  # It restarts the harness, so it starts only after every listed shard's
  # parallel enabled test has finished.
  run_disabled_phase=1
  case ,${ABOUTME_TOTP_SHARD-}, in
  ,, | *,epoch-disabled,*) ;;
  *) run_disabled_phase=0 ;;
  esac
  if [ "$status" -eq 0 ] && [ "$run_disabled_phase" -eq 1 ]; then
    # The runner-local database keeps its rows across this restart; only the
    # server enrollment flag changes.
    bash "$REPO/scripts/dev-https.sh" down ||
      fail 'cannot stop the harness between TOTP phases'
    DEV_HTTPS_TOTP_ENROLLMENT=false bash "$REPO/scripts/dev-https.sh" up ||
      fail 'cannot restart the harness with TOTP enrollment disabled'
    assert_server_flag TOTP_ENROLLMENT_ENABLED false
    disabled_evidence=$(mktemp -d "$EVIDENCE_ROOT/totp-disabled.XXXXXX")
    [ "$(stat -c %u "$disabled_evidence")" = "$UID_NOW" ] &&
      [ "$(stat -c %a "$disabled_evidence")" = 700 ] ||
      fail 'evidence directory ownership or mode mismatch'
    "$CONTEXT/run.sh" "$image_id" "$INPUT" "$staging" \
      "$disabled_evidence" totp-disabled || status=$?
    # Leave no stack running at an unexpected flag; the operator or the
    # hosted job starts a fresh one.
    bash "$REPO/scripts/dev-https.sh" down || status=1
  fi

  if "$mcp_fixture" cleanup --database-url "$NATIVE_DSN" \
    --client-name "$mcp_client_name"; then
    mcp_seeded=0
  else
    status=1
  fi
  prune_evidence totp-enabled
  prune_evidence totp-disabled
  [ "$status" -eq 0 ] || fail 'browser proof failed'
  printf '%s evidence: %s and %s (spec sha256 %s)\n' \
    "$TARGET" "$enabled_evidence" "$disabled_evidence" "$spec_sha"
  exit 0
fi

run_input=$INPUT
if [ "$MODE" = password-auth ] || [ "$MODE" = sample-start ]; then
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
elif [ "$MODE" = linkedin ]; then
  # The proof registers and verifies its own password accounts through the
  # capture mailbox. The fixture removes every row an earlier run left, so
  # each run starts from the same state (docs/design/linkedin-sign-in.md
  # "Tests").
  capture_secret=$STATE/secrets/auth-email-capture-bearer
  [ -f "$capture_secret" ] && [ ! -L "$capture_secret" ] &&
    [ "$(stat -c %u "$capture_secret")" = "$UID_NOW" ] ||
    fail 'invalid capture secret'
  linkedin_input=$(mktemp -d "$STATE/linkedin-input.XXXXXX")
  chmod 0700 "$linkedin_input"
  cp -- "$INPUT/caddy-root.crt" "$linkedin_input/caddy-root.crt"
  chmod 0600 "$linkedin_input/caddy-root.crt"
  capture_token=$(base64 -w0 -- "$capture_secret" | tr '+/' '-_' | tr -d '=')
  printf '%s' "$capture_token" >"$linkedin_input/mail-capture-token"
  chmod 0600 "$linkedin_input/mail-capture-token"
  run_input=$linkedin_input

  install -d -m 0700 "$REPO/.dev/bin"
  (cd "$REPO/apps/server" &&
    go build -o "$REPO/.dev/bin/password-auth-fixture" \
      ./cmd/password-auth-fixture) || fail 'fixture build failed'
  "$REPO/.dev/bin/password-auth-fixture" linkedin-cleanup --database-url "$NATIVE_DSN"
  curl -fsS -X DELETE -H "Authorization: Bearer $capture_token" \
    "http://127.0.0.1:20444/api/messages" >/dev/null
elif [ "$MODE" = mcp ] || [ "$MODE" = privacy ]; then
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
  if [ "$MODE" = mcp ]; then
    "$mcp_fixture" seed --database-url "$NATIVE_DSN" \
      --client-name "$mcp_client_name"
  fi
elif [ "$MODE" = entry ]; then
  install -d -m 0700 "$REPO/.dev/bin"
  (cd "$REPO/apps/server" &&
    go build -o "$REPO/.dev/bin/dev-seed" ./cmd/dev-seed) ||
    fail 'dev-seed build failed'
  "$REPO/.dev/bin/dev-seed" seed --database-url "$NATIVE_DSN"
elif [ "$MODE" = publish ]; then
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

if [ "$MODE" = password-auth ] || [ "$MODE" = sample-start ]; then
  "$REPO/.dev/bin/password-auth-fixture" cleanup --database-url "$NATIVE_DSN"
elif [ "$MODE" = linkedin ]; then
  "$REPO/.dev/bin/password-auth-fixture" linkedin-cleanup --database-url "$NATIVE_DSN" ||
    status=1
elif [ "$MODE" = mcp ] || [ "$MODE" = privacy ]; then
  if "$mcp_fixture" cleanup --database-url "$NATIVE_DSN" \
    --client-name "$mcp_client_name"; then
    mcp_seeded=0
  else
    status=1
  fi
fi
# Keep only the newest EVIDENCE_KEEP runs for this mode.
prune_evidence "$evidence_prefix"

[ "$status" -eq 0 ] || fail 'browser proof failed'

printf '%s evidence: %s (spec sha256 %s)\n' "$TARGET" "$evidence" "$spec_sha"
