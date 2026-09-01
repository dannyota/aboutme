#!/usr/bin/env bash
set -Eeuo pipefail

readonly IMAGE_CONTRACT=2
readonly IMAGE_BASE='mcr.microsoft.com/playwright:v1.62.1-noble@sha256:c091b21d9fae78c76e85cd4356431e9b018402f172a214fc7d7a5e9a7e29d8ac'
readonly IMAGE_PLAYWRIGHT=1.62.1
readonly IMAGE_NSS='2:3.98-1ubuntu0.2'
readonly IMAGE_ENTRYPOINT='["/opt/aboutme-auth/run.sh","--inside"]'

# Spec sources are mounted read-only per run, never baked into the image.
# Both sides validate this exact set; scripts/dev-https-check.sh stages it.
readonly -a SPEC_SOURCES=(
  playwright.config.ts
  auth.spec.ts
  transport.spec.ts
  editor.spec.ts
  public.spec.ts
  password-auth.spec.ts
  mcp.spec.ts
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

mount_has_option() {
  local options=$1 expected=$2
  case ",$options," in
  *",$expected,"*) return 0 ;;
  *) return 1 ;;
  esac
}

inside_container() {
  [ "$#" -le 1 ] || fail 'container entrypoint accepts at most one mode'
  local mode=${1:-auth}
  case $mode in
  auth | transport | editor | public | password-auth | mcp) ;;
  *) fail 'mode must be auth, transport, editor, public, password-auth, or mcp' ;;
  esac
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

  evidence_target=$(findmnt -n -o TARGET --target /evidence) ||
    fail 'evidence output is not mounted'
  evidence_options=$(findmnt -n -o OPTIONS --target /evidence) ||
    fail 'cannot inspect evidence output options'
  [ "$evidence_target" = /evidence ] ||
    fail 'evidence output is not a dedicated mount'
  mount_has_option "$evidence_options" rw || fail 'evidence output is not writable'
  mount_has_option "$evidence_options" ro && fail 'evidence output is read-only'

  [ -d /uat-input ] && [ ! -L /uat-input ] || fail 'invalid CA input directory'
  [ -d /evidence ] && [ ! -L /evidence ] || fail 'invalid evidence directory'
  [ "$(stat -c %u /uat-input)" = "$uid" ] || fail 'CA input owner mismatch'
  [ "$(stat -c %a /uat-input)" = 700 ] || fail 'CA input mode must be 0700'
  [ "$(stat -c %u /evidence)" = "$uid" ] || fail 'evidence owner mismatch'
  [ "$(stat -c %a /evidence)" = 700 ] || fail 'evidence mode must be 0700'
  [ -w /evidence ] || fail 'evidence output is not writable'

  input_entries=$(find /uat-input -mindepth 1 -maxdepth 1 -printf '%f\n' | sort)
  if [ "$mode" = password-auth ]; then
    [ "$input_entries" = $'caddy-root.crt\nmail-capture-token' ] ||
      fail 'CA input must contain the Caddy root and the capture token'
  else
    [ "$input_entries" = caddy-root.crt ] || fail 'CA input must contain one root'
  fi
  [ -f /uat-input/caddy-root.crt ] && [ ! -L /uat-input/caddy-root.crt ] ||
    fail 'Caddy root is not a regular file'
  [ "$(stat -c %u /uat-input/caddy-root.crt)" = "$uid" ] ||
    fail 'Caddy root owner mismatch'
  [ "$(stat -c %a /uat-input/caddy-root.crt)" = 600 ] ||
    fail 'Caddy root mode must be 0600'
  if [ "$mode" = password-auth ]; then
    [ -f /uat-input/mail-capture-token ] && [ ! -L /uat-input/mail-capture-token ] ||
      fail 'capture token is not a regular file'
    [ "$(stat -c %u /uat-input/mail-capture-token)" = "$uid" ] ||
      fail 'capture token owner mismatch'
    [ "$(stat -c %a /uat-input/mail-capture-token)" = 600 ] ||
      fail 'capture token mode must be 0600'
  fi
  evidence_entries=$(find /evidence -mindepth 1 -maxdepth 1 -print -quit)
  [ -z "$evidence_entries" ] || fail 'evidence output must start empty'

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
  ABOUTME_BROWSER_MODE=$mode \
    /opt/aboutme-auth/node_modules/.bin/playwright test \
    --config playwright.config.ts "$spec" \
    >"$log_file" 2>&1 || status=$?
  if [ "$status" -ne 0 ]; then
    if [ "$mode" = editor ] || [ "$mode" = mcp ]; then
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

  if ! node --input-type=module - "$mode" "$evidence_path" <<'VERIFY_EVIDENCE'
import { readFile } from 'node:fs/promises';

const mode = process.argv[2];
const path = process.argv[3];
const actual = JSON.parse(await readFile(path, 'utf8'));
const common = {
  errors: { certificate: 0, console: 0, externalRequest: 0, page: 0 },
  origin: 'https://localhost:20443',
};
const expected = mode === 'auth' ? {
  ...common,
  scenario: 'google-authentication',
  schemaVersion: 1,
  steps: Object.fromEntries(
    Array.from({ length: 10 }, (_, index) => [String(index + 1), true]),
  ),
} : mode === 'transport' ? {
  ...common,
  scenario: 'authenticated-transport',
  schemaVersion: 1,
  steps: { auth: true, cache: true, etag: true, ifMatch: true, teardown: true },
} : mode === 'public' ? {
  schemaVersion: 1,
  scenario: 'public-resume-hydration',
  origin: 'https://localhost:20443',
  errors: { console: 0, externalRequest: 0, page: 0 },
  steps: { published: true, ssr: true, hydrated: true },
} : mode === 'password-auth' ? {
  ...common,
  scenario: 'password-authentication',
  schemaVersion: 1,
  steps: {
    differentEmailLink: true,
    newPasswordLogin: true,
    oldPasswordRejected: true,
    oldSessionsRevoked: true,
    passwordAdded: true,
    passwordLogin: true,
    providerOnlyLogin: true,
    registerAccepted: true,
    reset: true,
    resetReplayRejected: true,
    verifiedWithoutSession: true,
  },
} : mode === 'mcp' ? {
  ...common,
  scenario: 'mcp-agent-access',
  schemaVersion: 1,
  steps: {
    clientRegistered: true,
    authorizeRedirected: true,
    consentApproved: true,
    tokenExchanged: true,
    toolsListed: true,
    resumeCreated: true,
    entryUpserted: true,
    editorVisible: true,
    grantRevoked: true,
    revokedRejected: true,
  },
} : {
  schemaVersion: 1,
  scenario: 'authenticated-editor',
  origin: 'https://localhost:20443',
  errors: { certificate: 0, console: 0, externalRequest: 0, page: 0 },
  steps: {
    auth: true,
    cache: true,
    etag: true,
    ifMatch: true,
    autosave: true,
    conflict: true,
    template: true,
    photo: true,
    session: true,
    persistence: true,
    accessibility: true,
    teardown: true,
  },
};
if (JSON.stringify(actual) !== JSON.stringify(expected)) process.exit(1);
VERIFY_EVIDENCE
  then
    fail 'browser evidence has invalid schema'
  fi

  printf 'dev-https-browser %s proof: PASS\n' "$proof_name"
}

host_run() {
  [ "$#" -ge 4 ] && [ "$#" -le 5 ] ||
    fail 'usage: run.sh <image-ID> <CA-input-directory> <spec-input-directory> <empty-evidence-directory> [auth|transport|editor|public|password-auth|mcp]'
  local image=$1 input=$2 spec_input=$3 evidence=$4 mode=${5:-auth}
  case $mode in
  auth | transport | editor | public | password-auth | mcp) ;;
  *) fail 'mode must be auth, transport, editor, public, password-auth, or mcp' ;;
  esac
  local uid gid input_entries evidence_entries
  local inspect inspected_id image_user entrypoint contract base playwright nss extra
  [[ $image =~ ^sha256:[0-9a-f]{64}$ ]] ||
    fail 'image must be an immutable sha256 ID'
  for path in "$input" "$spec_input" "$evidence"; do
    [[ $path = /* ]] || fail 'mount paths must be absolute'
    [[ $path != *$'\n'* && $path != *$'\r'* && $path != *$'\t'* ]] ||
      fail 'mount paths contain control characters'
    [ -d "$path" ] && [ ! -L "$path" ] || fail 'mount path is not a real directory'
  done
  input=$(realpath -e -- "$input") || fail 'cannot resolve CA input directory'
  spec_input=$(realpath -e -- "$spec_input") ||
    fail 'cannot resolve spec input directory'
  evidence=$(realpath -e -- "$evidence") || fail 'cannot resolve evidence directory'
  [ "$input" != "$evidence" ] && [ "$spec_input" != "$evidence" ] &&
    [ "$spec_input" != "$input" ] || fail 'mount directories must differ'

  uid=$(id -u)
  gid=$(id -g)
  [ "$uid" -ne 0 ] && [ "$gid" -ne 0 ] || fail 'host runner must be non-root'
  [ "$(stat -c %u "$input")" = "$uid" ] || fail 'CA input owner mismatch'
  [ "$(stat -c %a "$input")" = 700 ] || fail 'CA input mode must be 0700'
  [ "$(stat -c %u "$evidence")" = "$uid" ] || fail 'evidence owner mismatch'
  [ "$(stat -c %a "$evidence")" = 700 ] || fail 'evidence mode must be 0700'
  [ -w "$evidence" ] || fail 'evidence output is not writable'

  input_entries=$(find "$input" -mindepth 1 -maxdepth 1 -printf '%f\n' | sort)
  if [ "$mode" = password-auth ]; then
    [ "$input_entries" = $'caddy-root.crt\nmail-capture-token' ] ||
      fail 'CA input must contain the Caddy root and the capture token'
  else
    [ "$input_entries" = caddy-root.crt ] || fail 'CA input must contain one root'
  fi
  [ -f "$input/caddy-root.crt" ] && [ ! -L "$input/caddy-root.crt" ] ||
    fail 'Caddy root is not a regular file'
  [ "$(stat -c %u "$input/caddy-root.crt")" = "$uid" ] ||
    fail 'Caddy root owner mismatch'
  [ "$(stat -c %a "$input/caddy-root.crt")" = 600 ] ||
    fail 'Caddy root mode must be 0600'
  if [ "$mode" = password-auth ]; then
    [ -f "$input/mail-capture-token" ] && [ ! -L "$input/mail-capture-token" ] ||
      fail 'capture token is not a regular file'
    [ "$(stat -c %u "$input/mail-capture-token")" = "$uid" ] ||
      fail 'capture token owner mismatch'
    [ "$(stat -c %a "$input/mail-capture-token")" = 600 ] ||
      fail 'capture token mode must be 0600'
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
  exec podman run \
    --rm \
    --pull=never \
    --network=host \
    --read-only \
    --userns=keep-id \
    --user="$uid:$gid" \
    --security-opt=label=disable \
    --security-opt=no-new-privileges \
    --cap-drop=all \
    --cap-add=SYS_CHROOT \
    --tmpfs=/tmp:rw,nosuid,nodev,mode=1777,size=268435456 \
    --mount="type=bind,src=$input,dst=/uat-input,ro=true" \
    --mount="type=bind,src=$spec_input,dst=/uat-spec,ro=true" \
    --mount="type=bind,src=$evidence,dst=/evidence,rw=true" \
    "$image" "${mode_args[@]}"
}

if [ "${1-}" = --inside ]; then
  shift
  inside_container "$@"
else
  host_run "$@"
fi
