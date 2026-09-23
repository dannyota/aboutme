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
  editor-fixtures.ts
  network-policy.ts
  harness-lib.ts
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
  second-factor) printf 'caddy-root.crt\nmail-capture-token\nmcp-client-name' ;;
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
  second-factor)
    printf 'CA input must contain the Caddy root, the capture token, and the run client name'
    ;;
  *) printf 'CA input must contain one root' ;;
  esac
}

# validate_mode_input_files checks the extra per-mode input files after the
# entry set already matched.
validate_mode_input_files() {
  local mode=$1 dir=$2 uid=$3
  case $mode in
  password-auth | sample-start | second-factor)
    validate_capture_token_file "$dir/mail-capture-token" "$uid"
    ;;
  esac
  case $mode in
  mcp | privacy | second-factor)
    validate_mcp_client_name_file "$dir/mcp-client-name" "$uid"
    ;;
  esac
}

validate_mcp_credential_file() {
  # <path> <expected-owner-uid>; structural only, never opens or reads it.
  local path=$1 uid=$2
  [ -f "$path" ] && [ ! -L "$path" ] ||
    fail 'MCP credential file is not a regular file'
  [ "$(stat -c %u "$path")" = "$uid" ] || fail 'MCP credential file owner mismatch'
  [ "$(stat -c %a "$path")" = 600 ] || fail 'MCP credential file mode must be 0600'
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

inside_container() {
  [ "$#" -le 2 ] || fail 'container entrypoint accepts at most a mode and a workflow mode'
  local mode=${1:-auth} workflow_mode=${2:-}
  case $mode in
  auth | transport | editor | public | password-auth | mcp | entry | publish | exports | privacy | sample-start | second-factor | second-factor-disabled | mcp-sdk) ;;
  *) fail 'mode must be auth, transport, editor, public, password-auth, mcp, entry, publish, exports, privacy, sample-start, second-factor, second-factor-disabled, or mcp-sdk' ;;
  esac
  if [ "$mode" = mcp-sdk ]; then
    [[ $workflow_mode = local || $workflow_mode = production ]] ||
      fail 'mcp-sdk mode requires workflow mode local or production'
  else
    [ -z "$workflow_mode" ] || fail 'workflow mode is only accepted for mcp-sdk mode'
  fi
  [ "$(id -u)" -ne 0 ] || fail 'browser must run as non-root'

  local root_target root_options input_target input_options
  local evidence_target evidence_options uid input_entries evidence_entries
  uid=$(id -u)
  root_target=$(findmnt -n -o TARGET --target /) ||
    fail 'cannot inspect the root filesystem'
  root_options=$(findmnt -n -o OPTIONS --target /) ||
    fail 'cannot inspect the root filesystem options'
  [ "$root_target" = / ] || fail 'unexpected root filesystem target'
  mount_has_option "$root_options" ro || fail 'root filesystem is not read-only'

  input_target=$(findmnt -n -o TARGET --target /uat-input) ||
    fail 'CA input is not mounted'
  input_options=$(findmnt -n -o OPTIONS --target /uat-input) ||
    fail 'cannot inspect CA input options'
  [ "$input_target" = /uat-input ] || fail 'CA input is not a dedicated mount'
  mount_has_option "$input_options" ro || fail 'CA input is not read-only'
  mount_has_option "$input_options" rw && fail 'CA input is writable'

  local spec_target spec_options
  spec_target=$(findmnt -n -o TARGET --target /uat-spec) ||
    fail 'spec input is not mounted'
  spec_options=$(findmnt -n -o OPTIONS --target /uat-spec) ||
    fail 'cannot inspect spec input options'
  [ "$spec_target" = /uat-spec ] || fail 'spec input is not a dedicated mount'
  mount_has_option "$spec_options" ro || fail 'spec input is not read-only'
  mount_has_option "$spec_options" rw && fail 'spec input is writable'

  [ -d /uat-input ] && [ ! -L /uat-input ] || fail 'invalid CA input directory'
  [ "$(stat -c %u /uat-input)" = "$uid" ] || fail 'CA input owner mismatch'
  [ "$(stat -c %a /uat-input)" = 700 ] || fail 'CA input mode must be 0700'

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

  input_entries=$(find /uat-input -mindepth 1 -maxdepth 1 -printf '%f\n' | sort)
  [ "$input_entries" = "$(mode_input_entries "$mode")" ] ||
    fail "$(mode_input_diagnostic "$mode")"
  [ -f /uat-input/caddy-root.crt ] && [ ! -L /uat-input/caddy-root.crt ] ||
    fail 'Caddy root is not a regular file'
  [ "$(stat -c %u /uat-input/caddy-root.crt)" = "$uid" ] ||
    fail 'Caddy root owner mismatch'
  [ "$(stat -c %a /uat-input/caddy-root.crt)" = 600 ] ||
    fail 'Caddy root mode must be 0600'
  validate_mode_input_files "$mode" /uat-input "$uid"

  validate_spec_dir /uat-spec "$uid"

  export HOME=/tmp/home
  export XDG_CACHE_HOME=$HOME/.cache
  export XDG_CONFIG_HOME=$HOME/.config
  install -d -m 0700 "$HOME" "$HOME/.pki" "$HOME/.pki/nssdb" \
    "$XDG_CACHE_HOME" "$XDG_CONFIG_HOME"
  certutil -N --empty-password -d "sql:$HOME/.pki/nssdb" >/dev/null ||
    fail 'cannot initialize the isolated NSS database'
  certutil -A -d "sql:$HOME/.pki/nssdb" -n aboutme-local-caddy-root \
    -t 'C,,' -i /uat-input/caddy-root.crt || fail 'cannot import the Caddy root'
  certutil -L -d "sql:$HOME/.pki/nssdb" -n aboutme-local-caddy-root >/dev/null ||
    fail 'cannot verify the imported Caddy root'

  local evidence_name evidence_limit proof_name spec
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

  local log_file=/tmp/playwright-uat.log status=0
  cd /tmp/spec
  local -a mode_env=(ABOUTME_BROWSER_MODE="$mode")
  if [ "$mode" = mcp-sdk ]; then
    mode_env+=(ABOUTME_MCP_BROWSER_DIR=/mcp-browser ABOUTME_MCP_WORKFLOW_MODE="$workflow_mode")
  fi
  env "${mode_env[@]}" \
    /opt/aboutme-auth/node_modules/.bin/playwright test \
    --config playwright.config.ts "$spec" \
    >"$log_file" 2>&1 || status=$?
  if [ "$status" -ne 0 ]; then
    if [ "$mode" = public ] || [ "$mode" = editor ] || [ "$mode" = mcp ] || [ "$mode" = publish ] ||
      [ "$mode" = entry ] || [ "$mode" = exports ] || [ "$mode" = privacy ] ||
      [ "$mode" = sample-start ] || [ "$mode" = second-factor ] ||
      [ "$mode" = second-factor-disabled ] || [ "$mode" = mcp-sdk ]; then
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

  if [ "$mode" = mcp-sdk ]; then
    printf 'dev-https-browser %s proof: PASS\n' "$proof_name"
    return 0
  fi

  evidence_entries=$(find /evidence -mindepth 1 -maxdepth 1 -printf '%f\n')
  [ "$evidence_entries" = "$evidence_name" ] ||
    fail 'browser produced unexpected evidence'
  local evidence_path=/evidence/$evidence_name
  [ -f "$evidence_path" ] && [ ! -L "$evidence_path" ] ||
    fail 'browser evidence is not a regular file'
  [ "$(stat -c %u "$evidence_path")" = "$uid" ] ||
    fail 'browser evidence owner mismatch'
  [ "$(stat -c %a "$evidence_path")" = 600 ] ||
    fail 'browser evidence mode must be 0600'
  [ "$(stat -c %s "$evidence_path")" -le "$evidence_limit" ] ||
    fail 'browser evidence exceeds its bound'

  if ! node /opt/aboutme-auth/verify-evidence.mjs "$mode" "$evidence_path"; then
    fail 'browser evidence has invalid schema'
  fi

  printf 'dev-https-browser %s proof: PASS\n' "$proof_name"
}

host_run() {
  [ "$#" -ge 4 ] && [ "$#" -le 9 ] ||
    fail 'usage: run.sh <image-ID> <CA-input-directory> <spec-input-directory> <evidence-directory> [mode] [workflow-mode] [MCP-browser-directory] [MCP-credential-file] [MCP-container-name]'
  local image=$1 input=$2 spec_input=$3 evidence=$4 mode=${5:-auth}
  local workflow_mode=${6:-} browser_dir=${7:-} credential=${8:-} container_name=${9:-}
  case $mode in
  auth | transport | editor | public | password-auth | mcp | entry | publish | exports | privacy | sample-start | second-factor | second-factor-disabled | mcp-sdk) ;;
  *) fail 'mode must be auth, transport, editor, public, password-auth, mcp, entry, publish, exports, privacy, sample-start, second-factor, second-factor-disabled, or mcp-sdk' ;;
  esac
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
  local uid gid input_entries evidence_entries i j
  local inspect inspected_id image_user entrypoint contract base playwright nss extra
  local -a dir_paths=("$input" "$spec_input" "$evidence")
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
  input=$(realpath -e -- "$input") || fail 'cannot resolve CA input directory'
  spec_input=$(realpath -e -- "$spec_input") ||
    fail 'cannot resolve spec input directory'
  evidence=$(realpath -e -- "$evidence") || fail 'cannot resolve evidence directory'
  dir_paths=("$input" "$spec_input" "$evidence")
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
  [ "$(stat -c %u "$input")" = "$uid" ] || fail 'CA input owner mismatch'
  [ "$(stat -c %a "$input")" = 700 ] || fail 'CA input mode must be 0700'
  [ "$(stat -c %u "$evidence")" = "$uid" ] || fail 'evidence owner mismatch'
  [ "$(stat -c %a "$evidence")" = 700 ] || fail 'evidence mode must be 0700'
  [ -w "$evidence" ] || fail 'evidence output is not writable'
  if [ "$mode" = mcp-sdk ]; then
    validate_mcp_browser_dir "$browser_dir" "$uid"
    validate_mcp_credential_file "$credential" "$uid"
  fi

  input_entries=$(find "$input" -mindepth 1 -maxdepth 1 -printf '%f\n' | sort)
  [ "$input_entries" = "$(mode_input_entries "$mode")" ] ||
    fail "$(mode_input_diagnostic "$mode")"
  [ -f "$input/caddy-root.crt" ] && [ ! -L "$input/caddy-root.crt" ] ||
    fail 'Caddy root is not a regular file'
  [ "$(stat -c %u "$input/caddy-root.crt")" = "$uid" ] ||
    fail 'Caddy root owner mismatch'
  [ "$(stat -c %a "$input/caddy-root.crt")" = 600 ] ||
    fail 'Caddy root mode must be 0600'
  validate_mode_input_files "$mode" "$input" "$uid"
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
    "--mount=type=bind,src=$input,dst=/uat-input,ro=true"
    "--mount=type=bind,src=$spec_input,dst=/uat-spec,ro=true"
    "--mount=type=bind,src=$evidence,dst=/evidence,rw=true"
  )
  if [ "$mode" = mcp-sdk ]; then
    mount_args+=(
      "--mount=type=bind,src=$credential,dst=/mcp-credentials/login.env,ro=true"
      "--mount=type=bind,src=$browser_dir,dst=/mcp-browser,rw=true"
    )
  fi
  local -a name_args=()
  [ "$mode" = mcp-sdk ] && name_args=("--name=$container_name")
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
    --memory=2g \
    --memory-swap=2g \
    --cpus=2 \
    --tmpfs=/tmp:rw,nosuid,nodev,mode=1777,size=268435456 \
    "${name_args[@]}" \
    "${mount_args[@]}" \
    "$image" "${mode_args[@]}"
}

if [ "${1-}" = --inside ]; then
  shift
  inside_container "$@"
else
  host_run "$@"
fi
