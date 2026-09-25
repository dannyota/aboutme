#!/usr/bin/env bash
set -Eeuo pipefail

readonly IMAGE_CONTRACT=2
readonly IMAGE_BASE='mcr.microsoft.com/playwright:v1.62.1-noble@sha256:c091b21d9fae78c76e85cd4356431e9b018402f172a214fc7d7a5e9a7e29d8ac'
readonly IMAGE_PLAYWRIGHT=1.62.1
readonly IMAGE_NSS='2:3.98-1ubuntu0.2'
readonly IMAGE_ENTRYPOINT='["/opt/aboutme-auth/run.sh","--inside"]'
readonly MCP_CLIENT_NAME_RE='^aboutme MCP UAT [0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'

# Mounted read-only per run; both sides validate this exact set.
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

validate_spec_dir() {
  # validate_spec_dir <directory> <expected-owner-uid>
  local dir=$1 uid=$2 entries expected path
  [ -d "$dir" ] && [ ! -L "$dir" ] || fail 'spec input is not a real directory'
  [ "$(stat -c %u "$dir")" = "$uid" ] || fail 'spec input owner mismatch'
  [ "$(stat -c %a "$dir")" = 700 ] || fail 'spec input mode must be 0700'
  entries=$(find "$dir" -mindepth 1 -maxdepth 1 -printf '%f\n' | sort)
  expected=$(printf '%s\n' "${SPEC_SOURCES[@]}" | sort)
  [ "$entries" = "$expected" ] || fail 'spec input must contain exactly the spec sources'
  for path in "${SPEC_SOURCES[@]}"; do
    [ -f "$dir/$path" ] && [ ! -L "$dir/$path" ] ||
      fail 'spec source is not a regular file'
    [ "$(stat -c %u "$dir/$path")" = "$uid" ] || fail 'spec source owner mismatch'
    [ "$(stat -c %a "$dir/$path")" = 600 ] || fail 'spec source mode must be 0600'
  done
}

fail() {
  printf 'dev-https-browser: %s\n' "$*" >&2
  exit 1
}

validate_mcp_client_name_file() {
  local path=$1 uid=$2
  local -a lines=()
  [ -f "$path" ] && [ ! -L "$path" ] ||
    fail 'MCP client name is not a regular file'
  [ "$(stat -c %u "$path")" = "$uid" ] || fail 'MCP client name owner mismatch'
  [ "$(stat -c %a "$path")" = 600 ] || fail 'MCP client name mode must be 0600'
  mapfile -t lines <"$path"
  [ "${#lines[@]}" -eq 1 ] && [[ ${lines[0]} =~ $MCP_CLIENT_NAME_RE ]] ||
    fail 'MCP client name must contain a lowercase UUIDv4'
}

validate_capture_token_file() {
  local path=$1 uid=$2
  [ -f "$path" ] && [ ! -L "$path" ] ||
    fail 'capture token is not a regular file'
  [ "$(stat -c %u "$path")" = "$uid" ] || fail 'capture token owner mismatch'
  [ "$(stat -c %a "$path")" = 600 ] || fail 'capture token mode must be 0600'
}

# mode_input_entries prints the sorted CA input filenames a mode requires.
mode_input_entries() {
  case $1 in
  password-auth | sample-start) printf 'caddy-root.crt\nmail-capture-token' ;;
  mcp | privacy) printf 'caddy-root.crt\nmcp-client-name' ;;
  second-factor | totp)
    printf 'caddy-root.crt\nmail-capture-token\nmcp-client-name'
    ;;
  totp-prod-flag-off | totp-prod-enabled | totp-prod-cleanup)
    printf 'account.env'
    ;;
  *) printf 'caddy-root.crt' ;;
  esac
}

# mode_input_diagnostic prints the rejection text for a wrong CA input set.
mode_input_diagnostic() {
  case $1 in
  password-auth | sample-start)
    printf 'CA input must contain the Caddy root and the capture token'
    ;;
  mcp | privacy)
    printf 'MCP input must contain the Caddy root and the run client name'
    ;;
  second-factor | totp)
    printf 'CA input must contain the Caddy root, the capture token, and the run client name'
    ;;
  totp-prod-flag-off | totp-prod-enabled | totp-prod-cleanup)
    printf 'production input must contain exactly the fictional account file'
    ;;
  *) printf 'CA input must contain one root' ;;
  esac
}

# mode_is_totp_production reports whether mode is one of the three production
# TOTP proof modes, which carry no CA root and validate against the image's
# own system trust store instead (docs/runbooks/totp-keys.md "Production proofs").
mode_is_totp_production() {
  case $1 in
  totp-prod-flag-off | totp-prod-enabled | totp-prod-cleanup) return 0 ;;
  *) return 1 ;;
  esac
}

# add_shard_env <array-name> <flag> <mode>: for a sharded mode, validates its shard variable against the mode's shard names and appends KEY=value (--env KEY=value when <flag> is set) to the named array.
add_shard_env() {
  local -n arr=$1; local flag=$2 key value names
  case $3 in
  totp) key=ABOUTME_TOTP_SHARD value=${ABOUTME_TOTP_SHARD-} names='primary skew replay-concurrent replace-recovery locale-attempts epoch-disabled' ;;
  second-factor) key=ABOUTME_PASSKEY_SHARD value=${ABOUTME_PASSKEY_SHARD-} names='primary-disabled recovery-attempts' ;;
  *) return 0 ;;
  esac
  [ -z "$value" ] && return 0
  [[ " $names " == *" $value "* ]] || fail "$key must be one of: $names"
  arr+=(${flag:+--env} "$key=$value")
}

# validate_mode_input_files checks the extra per-mode input files after the
# entry set already matched.
validate_mode_input_files() {
  local mode=$1 dir=$2 uid=$3
  case $mode in
  password-auth | sample-start | second-factor | totp)
    validate_capture_token_file "$dir/mail-capture-token" "$uid"
    ;;
  esac
  case $mode in
  mcp | privacy | second-factor | totp)
    validate_mcp_client_name_file "$dir/mcp-client-name" "$uid"
    ;;
  esac
  if mode_is_totp_production "$mode"; then
    validate_account_file "$dir/account.env" "$uid"
  fi
}

validate_account_file() {
  # <path> <expected-owner-uid>; structural only, never opens or reads it.
  local path=$1 uid=$2
  [ -f "$path" ] && [ ! -L "$path" ] ||
    fail 'production account file is not a regular file'
  [ "$(stat -c %u "$path")" = "$uid" ] || fail 'production account file owner mismatch'
  [ "$(stat -c %a "$path")" = 600 ] || fail 'production account file mode must be 0600'
}

validate_mcp_credential_file() {
  # <path> <expected-owner-uid>; structural only, never opens or reads it.
  local path=$1 uid=$2
  [ -f "$path" ] && [ ! -L "$path" ] ||
    fail 'MCP credential file is not a regular file'
  [ "$(stat -c %u "$path")" = "$uid" ] || fail 'MCP credential file owner mismatch'
  [ "$(stat -c %a "$path")" = 600 ] || fail 'MCP credential file mode must be 0600'
}

validate_evidence_file() { # <path> <uid> <size-limit> <fail-message-label>
  local path=$1 uid=$2 limit=$3 label=$4
  [ -f "$path" ] && [ ! -L "$path" ] || fail "$label is not a regular file"
  [ "$(stat -c %u "$path")" = "$uid" ] || fail "$label owner mismatch"
  [ "$(stat -c %a "$path")" = 600 ] || fail "$label mode must be 0600"
  [ "$(stat -c %s "$path")" -le "$limit" ] || fail "$label exceeds its bound"
}

validate_mcp_browser_dir() {
  # validate_mcp_browser_dir <directory> <expected-owner-uid>
  local dir=$1 uid=$2 entries
  [ -d "$dir" ] && [ ! -L "$dir" ] ||
    fail 'MCP browser directory is not a real directory'
  [ "$(stat -c %u "$dir")" = "$uid" ] || fail 'MCP browser directory owner mismatch'
  [ "$(stat -c %a "$dir")" = 700 ] || fail 'MCP browser directory mode must be 0700'
  entries=$(find "$dir" -mindepth 1 -maxdepth 1 -print -quit)
  [ -z "$entries" ] || fail 'MCP browser directory must start empty'
}

mount_has_option() {
  local options=$1 expected=$2
  case ",$options," in
  *",$expected,"*) return 0 ;;
  *) return 1 ;;
  esac
}

require_valid_mode() {
  case $1 in
  auth | transport | editor | public | password-auth | mcp | entry | publish | exports | privacy | sample-start | second-factor | second-factor-disabled | totp | totp-disabled | totp-prod-flag-off | totp-prod-enabled | totp-prod-cleanup | mcp-sdk) ;;
  *) fail 'mode must be auth, transport, editor, public, password-auth, mcp, entry, publish, exports, privacy, sample-start, second-factor, second-factor-disabled, totp, totp-disabled, totp-prod-flag-off, totp-prod-enabled, totp-prod-cleanup, or mcp-sdk' ;;
  esac
}

inside_container() {
  [ "$#" -le 2 ] || fail 'container entrypoint accepts at most a mode and a workflow mode'
  local mode=${1:-auth} workflow_mode=${2:-}; require_valid_mode "$mode"
  if [ "$mode" = mcp-sdk ]; then
    [[ $workflow_mode = local || $workflow_mode = production ]] ||
      fail 'mcp-sdk mode requires workflow mode local or production'
  else
    [ -z "$workflow_mode" ] || fail 'workflow mode is only accepted for mcp-sdk mode'
  fi
  [ "$(id -u)" -ne 0 ] || fail 'browser must run as non-root'
  # Production browses the real public origin, which the image's own system
  # trust store already validates. Trusting the local harness's Caddy root
  # there would let the browser accept a self-signed certificate for a
  # session that must rely on nothing but the public CA chain, so production
  # mounts no CA input and the container never imports one.
  local production_sdk=0
  [ "$mode" = mcp-sdk ] && [ "$workflow_mode" = production ] && production_sdk=1
  # The three production TOTP modes keep the /uat-input mount (it carries
  # only the fictional account file) but never import a CA root, for the
  # same reason as production_sdk above.
  local skip_ca_import=$production_sdk
  mode_is_totp_production "$mode" && skip_ca_import=1

  local root_target root_options input_target input_options
  local evidence_target evidence_options uid input_entries evidence_entries
  uid=$(id -u)
  root_target=$(findmnt -n -o TARGET --target /) ||
    fail 'cannot inspect the root filesystem'
  root_options=$(findmnt -n -o OPTIONS --target /) ||
    fail 'cannot inspect the root filesystem options'
  [ "$root_target" = / ] || fail 'unexpected root filesystem target'
  mount_has_option "$root_options" ro || fail 'root filesystem is not read-only'

  if [ "$production_sdk" -eq 1 ]; then
    ! findmnt -n --target /uat-input >/dev/null 2>&1 ||
      fail 'production mcp-sdk mode must not mount a CA input'
  else
    input_target=$(findmnt -n -o TARGET --target /uat-input) ||
      fail 'CA input is not mounted'
    input_options=$(findmnt -n -o OPTIONS --target /uat-input) ||
      fail 'cannot inspect CA input options'
    [ "$input_target" = /uat-input ] || fail 'CA input is not a dedicated mount'
    mount_has_option "$input_options" ro || fail 'CA input is not read-only'
    mount_has_option "$input_options" rw && fail 'CA input is writable'
  fi

  local spec_target spec_options
  spec_target=$(findmnt -n -o TARGET --target /uat-spec) ||
    fail 'spec input is not mounted'
  spec_options=$(findmnt -n -o OPTIONS --target /uat-spec) ||
    fail 'cannot inspect spec input options'
  [ "$spec_target" = /uat-spec ] || fail 'spec input is not a dedicated mount'
  mount_has_option "$spec_options" ro || fail 'spec input is not read-only'
  mount_has_option "$spec_options" rw && fail 'spec input is writable'

  if [ "$production_sdk" -ne 1 ]; then
    [ -d /uat-input ] && [ ! -L /uat-input ] || fail 'invalid CA input directory'
    [ "$(stat -c %u /uat-input)" = "$uid" ] || fail 'CA input owner mismatch'
    [ "$(stat -c %a /uat-input)" = 700 ] || fail 'CA input mode must be 0700'
  fi

  evidence_target=$(findmnt -n -o TARGET --target /evidence) ||
    fail 'evidence output is not mounted'
  evidence_options=$(findmnt -n -o OPTIONS --target /evidence) ||
    fail 'cannot inspect evidence output options'
  [ "$evidence_target" = /evidence ] ||
    fail 'evidence output is not a dedicated mount'
  mount_has_option "$evidence_options" rw || fail 'evidence output is not writable'
  mount_has_option "$evidence_options" ro && fail 'evidence output is read-only'
  [ -d /evidence ] && [ ! -L /evidence ] || fail 'invalid evidence directory'
  [ "$(stat -c %u /evidence)" = "$uid" ] || fail 'evidence owner mismatch'
  [ "$(stat -c %a /evidence)" = 700 ] || fail 'evidence mode must be 0700'
  [ -w /evidence ] || fail 'evidence output is not writable'
  evidence_entries=$(find /evidence -mindepth 1 -maxdepth 1 -print -quit)
  [ -z "$evidence_entries" ] || fail 'evidence output must start empty'

  if [ "$mode" = mcp-sdk ]; then
    local credential_target credential_options browser_target browser_options
    credential_target=$(findmnt -n -o TARGET --target /mcp-credentials/login.env) ||
      fail 'MCP credential file is not mounted'
    credential_options=$(findmnt -n -o OPTIONS --target /mcp-credentials/login.env) ||
      fail 'cannot inspect MCP credential file options'
    [ "$credential_target" = /mcp-credentials/login.env ] ||
      fail 'MCP credential file is not a dedicated mount'
    mount_has_option "$credential_options" ro || fail 'MCP credential file is not read-only'
    mount_has_option "$credential_options" rw && fail 'MCP credential file is writable'
    validate_mcp_credential_file /mcp-credentials/login.env "$uid"

    browser_target=$(findmnt -n -o TARGET --target /mcp-browser) ||
      fail 'MCP browser directory is not mounted'
    browser_options=$(findmnt -n -o OPTIONS --target /mcp-browser) ||
      fail 'cannot inspect MCP browser directory options'
    [ "$browser_target" = /mcp-browser ] ||
      fail 'MCP browser directory is not a dedicated mount'
    mount_has_option "$browser_options" rw || fail 'MCP browser directory is not writable'
    mount_has_option "$browser_options" ro && fail 'MCP browser directory is read-only'
    validate_mcp_browser_dir /mcp-browser "$uid"
  fi

  if [ "$production_sdk" -ne 1 ]; then
    input_entries=$(find /uat-input -mindepth 1 -maxdepth 1 -printf '%f\n' | sort)
    [ "$input_entries" = "$(mode_input_entries "$mode")" ] ||
      fail "$(mode_input_diagnostic "$mode")"
    if mode_is_totp_production "$mode"; then
      validate_mode_input_files "$mode" /uat-input "$uid"
    else
      [ -f /uat-input/caddy-root.crt ] && [ ! -L /uat-input/caddy-root.crt ] ||
        fail 'Caddy root is not a regular file'
      [ "$(stat -c %u /uat-input/caddy-root.crt)" = "$uid" ] ||
        fail 'Caddy root owner mismatch'
      [ "$(stat -c %a /uat-input/caddy-root.crt)" = 600 ] ||
        fail 'Caddy root mode must be 0600'
      validate_mode_input_files "$mode" /uat-input "$uid"
    fi
  fi

  validate_spec_dir /uat-spec "$uid"

  export HOME=/tmp/home
  export XDG_CACHE_HOME=$HOME/.cache
  export XDG_CONFIG_HOME=$HOME/.config
  install -d -m 0700 "$HOME" "$HOME/.pki" "$HOME/.pki/nssdb" \
    "$XDG_CACHE_HOME" "$XDG_CONFIG_HOME"
  certutil -N --empty-password -d "sql:$HOME/.pki/nssdb" >/dev/null ||
    fail 'cannot initialize the isolated NSS database'
  if [ "$skip_ca_import" -ne 1 ]; then
    certutil -A -d "sql:$HOME/.pki/nssdb" -n aboutme-local-caddy-root \
      -t 'C,,' -i /uat-input/caddy-root.crt || fail 'cannot import the Caddy root'
    certutil -L -d "sql:$HOME/.pki/nssdb" -n aboutme-local-caddy-root >/dev/null ||
      fail 'cannot verify the imported Caddy root'
  fi

  local evidence_name evidence_limit proof_name spec evidence_extra_name= evidence_extra_limit=
  case $mode in
  auth)
    evidence_name=auth-proof.json
    evidence_limit=4096
    proof_name=authentication
    spec=auth.spec.ts
    ;;
  transport)
    evidence_name=transport-proof.json
    evidence_limit=4096
    proof_name=transport
    spec=transport.spec.ts
    ;;
  editor)
    evidence_name=editor-proof.json
    evidence_limit=8192
    proof_name=editor
    spec=editor.spec.ts
    ;;
  public)
    evidence_name=public-proof.json
    evidence_limit=4096
    proof_name=public
    spec=public.spec.ts
    ;;
  password-auth)
    evidence_name=password-proof.json
    evidence_limit=4096
    proof_name=password-authentication
    spec=password-auth.spec.ts
    ;;
  mcp)
    evidence_name=mcp-proof.json
    evidence_limit=4096
    proof_name='MCP agent access'
    spec=mcp.spec.ts
    ;;
  entry)
    evidence_name=entry-proof.json
    evidence_limit=4096
    proof_name='entry flow'
    spec=entry.spec.ts
    ;;
  publish)
    evidence_name=publish-proof.json
    evidence_limit=8192
    proof_name='native HTTPS publish, discovery, and revocation'
    spec=publish.spec.ts
    ;;
  exports)
    evidence_name=exports-proof.json
    evidence_limit=8192
    proof_name=exports
    spec=exports.spec.ts
    ;;
  privacy)
    evidence_name=privacy-proof.json
    evidence_limit=8192
    proof_name='account privacy'
    spec=privacy.spec.ts
    ;;
  sample-start)
    evidence_name=sample-start-proof.json
    evidence_limit=4096
    proof_name='register and start a resume from a sample'
    spec=sample-start.spec.ts
    ;;
  second-factor)
    evidence_name=passkey-second-factor-proof.json
    evidence_limit=8192
    proof_name='passkey second factor'
    spec=second-factor.spec.ts
    ;;
  second-factor-disabled)
    evidence_name=passkey-enrollment-disabled-proof.json
    evidence_limit=8192
    proof_name='disabled passkey enrollment'
    spec=second-factor.spec.ts
    ;;
  totp)
    evidence_name=totp-second-factor-proof.json
    evidence_limit=8192
    evidence_extra_name=totp-timing.json
    evidence_extra_limit=4096
    proof_name='authenticator-app second factor'
    spec=totp.spec.ts
    ;;
  totp-disabled)
    evidence_name=totp-enrollment-disabled-proof.json
    evidence_limit=8192
    proof_name='disabled authenticator-app enrollment'
    spec=totp.spec.ts
    ;;
  totp-prod-flag-off)
    # No fixed /evidence schema: only fixed step names and outcomes reach
    # the runner's own output (docs/runbooks/totp-keys.md "Production
    # proofs"); the mounted evidence directory stays empty and unused.
    proof_name='production TOTP flag-off proof'
    spec=totp-production.spec.ts
    ;;
  totp-prod-enabled)
    proof_name='production TOTP enabled proof'
    spec=totp-production.spec.ts
    ;;
  totp-prod-cleanup)
    proof_name='production TOTP cleanup'
    spec=totp-production.spec.ts
    ;;
  mcp-sdk)
    # No fixed /evidence schema: /mcp-browser carries the result instead.
    proof_name='MCP SDK owner workflow browser handoff'
    spec=mcp-sdk.spec.ts
    ;;
  esac
  # Stage the mounted specs beside a node_modules symlink so module
  # resolution finds the image's pinned dependencies. The image package.json
  # supplies "type": "module"; without it Node loads the specs as CJS and
  # import.meta fails.
  install -d -m 0700 /tmp/spec
  local spec_file
  for spec_file in "${SPEC_SOURCES[@]}"; do
    cp -- "/uat-spec/$spec_file" "/tmp/spec/$spec_file"
    chmod 0400 "/tmp/spec/$spec_file"
  done
  cp -- /opt/aboutme-auth/package.json /tmp/spec/package.json
  chmod 0400 /tmp/spec/package.json
  ln -s /opt/aboutme-auth/node_modules /tmp/spec/node_modules

  local config=playwright.config.ts
  mode_is_totp_production "$mode" && config=production.config.ts

  local log_file=/tmp/playwright-uat.log status=0
  cd /tmp/spec
  local -a mode_env=(ABOUTME_BROWSER_MODE="$mode")
  if [ "$mode" = mcp-sdk ]; then
    mode_env+=(ABOUTME_MCP_BROWSER_DIR=/mcp-browser ABOUTME_MCP_WORKFLOW_MODE="$workflow_mode")
  fi
  add_shard_env mode_env '' "$mode"
  env "${mode_env[@]}" \
    /opt/aboutme-auth/node_modules/.bin/playwright test \
    --config "$config" "$spec" \
    >"$log_file" 2>&1 || status=$?
  if [ "$status" -ne 0 ]; then
    if [ "$mode" = public ] || [ "$mode" = editor ] || [ "$mode" = mcp ] || [ "$mode" = publish ] ||
      [ "$mode" = entry ] || [ "$mode" = exports ] || [ "$mode" = privacy ] ||
      [ "$mode" = sample-start ] || [ "$mode" = second-factor ] ||
      [ "$mode" = second-factor-disabled ] || [ "$mode" = totp ] ||
      [ "$mode" = totp-disabled ] || mode_is_totp_production "$mode" ||
      [ "$mode" = mcp-sdk ]; then
      local -a bounded_stages=()
      mapfile -t bounded_stages < <(
        grep -E "^${mode}-stage:[a-z0-9-]+$" "$log_file" || true
      )
      if [ "${#bounded_stages[@]}" -gt 0 ]; then
        printf 'dev-https-browser: %s\n' \
          "${bounded_stages[${#bounded_stages[@]} - 1]}" >&2
      fi
    fi
    fail "$proof_name proof failed; volatile browser output was withheld"
  fi

  if [ "$mode" = mcp-sdk ] || mode_is_totp_production "$mode"; then
    printf 'dev-https-browser %s proof: PASS\n' "$proof_name"
    return 0
  fi

  local -a expected_evidence=("$evidence_name" ${evidence_extra_name:+"$evidence_extra_name"})
  evidence_entries=$(find /evidence -mindepth 1 -maxdepth 1 -printf '%f\n' | sort)
  [ "$evidence_entries" = "$(printf '%s\n' "${expected_evidence[@]}" | sort)" ] ||
    fail 'browser produced unexpected evidence'
  local evidence_path=/evidence/$evidence_name
  validate_evidence_file "$evidence_path" "$uid" "$evidence_limit" 'browser evidence'

  if ! node /opt/aboutme-auth/verify-evidence.mjs "$mode" "$evidence_path"; then
    fail 'browser evidence has invalid schema'
  fi

  [ -z "$evidence_extra_name" ] ||
    validate_evidence_file "/evidence/$evidence_extra_name" "$uid" "$evidence_extra_limit" 'browser timing evidence'

  printf 'dev-https-browser %s proof: PASS\n' "$proof_name"
}

host_run() {
  [ "$#" -ge 4 ] && [ "$#" -le 9 ] ||
    fail 'usage: run.sh <image-ID> <CA-input-directory> <spec-input-directory> <evidence-directory> [mode] [workflow-mode] [MCP-browser-directory] [MCP-credential-file] [MCP-container-name]'
  local image=$1 input=$2 spec_input=$3 evidence=$4 mode=${5:-auth}
  local workflow_mode=${6:-} browser_dir=${7:-} credential=${8:-} container_name=${9:-}
  require_valid_mode "$mode"
  if [ "$mode" = mcp-sdk ]; then
    [[ $workflow_mode = local || $workflow_mode = production ]] ||
      fail 'mcp-sdk mode requires workflow mode local or production'
    [ -n "$browser_dir" ] || fail 'mcp-sdk mode requires an MCP browser directory'
    [ -n "$credential" ] || fail 'mcp-sdk mode requires an MCP credential file'
    [[ $container_name =~ ^aboutme-mcp-sdk-[0-9a-f]{32}$ ]] ||
      fail 'mcp-sdk mode requires a container name matching aboutme-mcp-sdk-<32 lowercase hex characters>'
  else
    [ -z "$workflow_mode" ] && [ -z "$browser_dir" ] && [ -z "$credential" ] &&
      [ -z "$container_name" ] ||
      fail 'workflow mode, MCP browser directory, MCP credential file, and MCP container name are only accepted for mcp-sdk mode'
  fi
  # See inside_container for why production mode takes no CA input.
  local production_sdk=0
  [ "$mode" = mcp-sdk ] && [ "$workflow_mode" = production ] && production_sdk=1
  if [ "$production_sdk" -eq 1 ]; then
    [ -z "$input" ] || fail 'production mcp-sdk mode does not take a CA input directory'
  else
    [ -n "$input" ] || fail 'CA input directory is required'
  fi
  local uid gid input_entries evidence_entries i j
  local inspect inspected_id image_user entrypoint contract base playwright nss extra
  local -a dir_paths=("$spec_input" "$evidence")
  [ "$production_sdk" -eq 1 ] || dir_paths+=("$input")
  [ "$mode" = mcp-sdk ] && dir_paths+=("$browser_dir")
  [[ $image =~ ^sha256:[0-9a-f]{64}$ ]] ||
    fail 'image must be an immutable sha256 ID'
  for path in "${dir_paths[@]}"; do
    [[ $path = /* ]] || fail 'mount paths must be absolute'
    [[ $path != *$'\n'* && $path != *$'\r'* && $path != *$'\t'* ]] ||
      fail 'mount paths contain control characters'
    [ -d "$path" ] && [ ! -L "$path" ] || fail 'mount path is not a real directory'
  done
  if [ "$mode" = mcp-sdk ]; then
    [[ $credential = /* ]] || fail 'mount paths must be absolute'
    [[ $credential != *$'\n'* && $credential != *$'\r'* && $credential != *$'\t'* ]] ||
      fail 'mount paths contain control characters'
  fi
  [ "$production_sdk" -eq 1 ] ||
    input=$(realpath -e -- "$input") || fail 'cannot resolve CA input directory'
  spec_input=$(realpath -e -- "$spec_input") ||
    fail 'cannot resolve spec input directory'
  evidence=$(realpath -e -- "$evidence") || fail 'cannot resolve evidence directory'
  dir_paths=("$spec_input" "$evidence")
  [ "$production_sdk" -eq 1 ] || dir_paths+=("$input")
  if [ "$mode" = mcp-sdk ]; then
    browser_dir=$(realpath -e -- "$browser_dir") ||
      fail 'cannot resolve MCP browser directory'
    credential=$(realpath -e -- "$credential") ||
      fail 'cannot resolve MCP credential file'
    dir_paths+=("$browser_dir" "$credential")
  fi
  for ((i = 0; i < ${#dir_paths[@]}; i++)); do
    for ((j = i + 1; j < ${#dir_paths[@]}; j++)); do
      [ "${dir_paths[i]}" != "${dir_paths[j]}" ] || fail 'mount directories must differ'
    done
  done

  uid=$(id -u)
  gid=$(id -g)
  [ "$uid" -ne 0 ] && [ "$gid" -ne 0 ] || fail 'host runner must be non-root'
  if [ "$production_sdk" -ne 1 ]; then
    [ "$(stat -c %u "$input")" = "$uid" ] || fail 'CA input owner mismatch'
    [ "$(stat -c %a "$input")" = 700 ] || fail 'CA input mode must be 0700'
  fi
  [ "$(stat -c %u "$evidence")" = "$uid" ] || fail 'evidence owner mismatch'
  [ "$(stat -c %a "$evidence")" = 700 ] || fail 'evidence mode must be 0700'
  [ -w "$evidence" ] || fail 'evidence output is not writable'
  if [ "$mode" = mcp-sdk ]; then
    validate_mcp_browser_dir "$browser_dir" "$uid"
    validate_mcp_credential_file "$credential" "$uid"
  fi

  if [ "$production_sdk" -ne 1 ]; then
    input_entries=$(find "$input" -mindepth 1 -maxdepth 1 -printf '%f\n' | sort)
    [ "$input_entries" = "$(mode_input_entries "$mode")" ] ||
      fail "$(mode_input_diagnostic "$mode")"
    if mode_is_totp_production "$mode"; then
      validate_mode_input_files "$mode" "$input" "$uid"
    else
      [ -f "$input/caddy-root.crt" ] && [ ! -L "$input/caddy-root.crt" ] ||
        fail 'Caddy root is not a regular file'
      [ "$(stat -c %u "$input/caddy-root.crt")" = "$uid" ] ||
        fail 'Caddy root owner mismatch'
      [ "$(stat -c %a "$input/caddy-root.crt")" = 600 ] ||
        fail 'Caddy root mode must be 0600'
      validate_mode_input_files "$mode" "$input" "$uid"
    fi
  fi
  evidence_entries=$(find "$evidence" -mindepth 1 -maxdepth 1 -print -quit)
  [ -z "$evidence_entries" ] || fail 'evidence output must start empty'
  validate_spec_dir "$spec_input" "$uid"
  command -v podman >/dev/null || fail 'podman is required'

  inspect=$(podman image inspect --format \
    '{{.Id}}|{{.Config.User}}|{{json .Config.Entrypoint}}|{{index .Config.Labels "io.aboutme.dev-https-browser.contract"}}|{{index .Config.Labels "io.aboutme.dev-https-browser.base"}}|{{index .Config.Labels "io.aboutme.dev-https-browser.playwright"}}|{{index .Config.Labels "io.aboutme.dev-https-browser.libnss3-tools"}}' \
    "$image") || fail 'cannot inspect the local browser image'
  [[ $inspect != *$'\n'* && $inspect != *$'\r'* ]] ||
    fail 'browser image inspection returned multiple records'
  IFS='|' read -r inspected_id image_user entrypoint contract base playwright nss extra \
    <<<"$inspect"
  if [[ $inspected_id =~ ^[0-9a-f]{64}$ ]]; then
    inspected_id="sha256:$inspected_id"
  elif [[ ! $inspected_id =~ ^sha256:[0-9a-f]{64}$ ]]; then
    fail 'browser image inspection returned a malformed ID'
  fi
  [ "$inspected_id" = "$image" ] || fail 'inspected browser image ID does not match'
  [ -z "$extra" ] && [ "$image_user" = pwuser ] &&
    [ "$entrypoint" = "$IMAGE_ENTRYPOINT" ] &&
    [ "$contract" = "$IMAGE_CONTRACT" ] && [ "$base" = "$IMAGE_BASE" ] &&
    [ "$playwright" = "$IMAGE_PLAYWRIGHT" ] && [ "$nss" = "$IMAGE_NSS" ] ||
    fail 'browser image contract mismatch'

  local -a mode_args=()
  [ "$mode" = auth ] || mode_args=("$mode")
  [ "$mode" = mcp-sdk ] && mode_args+=("$workflow_mode")
  local -a mount_args=(
    "--mount=type=bind,src=$spec_input,dst=/uat-spec,ro=true"
    "--mount=type=bind,src=$evidence,dst=/evidence,rw=true"
  )
  [ "$production_sdk" -eq 1 ] ||
    mount_args+=("--mount=type=bind,src=$input,dst=/uat-input,ro=true")
  if [ "$mode" = mcp-sdk ]; then
    mount_args+=(
      "--mount=type=bind,src=$credential,dst=/mcp-credentials/login.env,ro=true"
      "--mount=type=bind,src=$browser_dir,dst=/mcp-browser,rw=true"
    )
  fi
  local -a name_args=()
  [ "$mode" = mcp-sdk ] && name_args=("--name=$container_name")
  # mcp-sdk (it shares the runner with scripts/mcp-owner-workflow.sh) and the
  # production TOTP modes (docs/runbooks/totp-keys.md "Production proofs") are
  # capped; a 2 GiB ceiling would starve other modes' long journeys (such as
  # second-factor) on a runner busy with the stack. Chromium keeps its shared
  # memory in /tmp (Playwright passes --disable-dev-shm-usage): at 256 MiB, dev
  # web app module fetches failed with net::ERR_INSUFFICIENT_RESOURCES.
  local -a resource_args=()
  local tmp_bytes=1073741824
  if [ "$mode" = mcp-sdk ] || mode_is_totp_production "$mode"; then
    resource_args=(--memory=2g --memory-swap=2g --cpus=2)
    tmp_bytes=268435456
  fi
  local -a env_args=()
  add_shard_env env_args --env "$mode"
  exec podman run \
    --rm \
    --init \
    --pull=never \
    --network=host \
    --read-only \
    --userns=keep-id \
    --user="$uid:$gid" \
    --security-opt=label=disable \
    --security-opt=no-new-privileges \
    --cap-drop=all \
    --cap-add=SYS_CHROOT \
    "${resource_args[@]}" \
    --tmpfs="/tmp:rw,nosuid,nodev,mode=1777,size=$tmp_bytes" \
    "${name_args[@]}" \
    "${mount_args[@]}" \
    "${env_args[@]}" \
    "$image" "${mode_args[@]}"
}

if [ "${1-}" = --inside ]; then
  shift
  inside_container "$@"
else
  host_run "$@"
fi
