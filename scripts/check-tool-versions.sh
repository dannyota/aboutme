#!/usr/bin/env bash

# Fail when local gates would run with a different toolchain from hosted CI.
# This script never installs tools. Update .tool-versions and every validated
# consumer together through an explicit dependency/tooling change.
set -Eeuo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
ROOT=$PWD
MANIFEST=$ROOT/.tool-versions

declare -A expected=()
readonly -a known=(
  nodejs golang sqlc semgrep gitleaks golangci-lint govulncheck caddy
)

die() {
  printf 'tool versions: %s\n' "$*" >&2
  exit 1
}

is_known() {
  local candidate=$1 name
  for name in "${known[@]}"; do
    [ "$candidate" = "$name" ] && return 0
  done
  return 1
}

load_manifest() {
  [ -f "$MANIFEST" ] || die "$MANIFEST is missing"

  local name version extra
  while read -r name version extra; do
    [ -n "${name:-}" ] || continue
    [[ $name == \#* ]] && continue
    is_known "$name" || die "$MANIFEST: unknown tool '$name'"
    [ -n "${version:-}" ] && [ -z "${extra:-}" ] ||
      die "$MANIFEST: '$name' must have one exact version"
    [ -z "${expected[$name]+set}" ] || die "$MANIFEST: duplicate tool '$name'"
    expected[$name]=$version
  done <"$MANIFEST"

  for name in "${known[@]}"; do
    [ -n "${expected[$name]+set}" ] || die "$MANIFEST: missing tool '$name'"
  done
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "$1: command not found"
}

first_line() {
  local value=$1
  printf '%s\n' "${value%%$'\n'*}"
}

go_module_version() {
  local command_name=$1 module=$2 info version
  require_command "$command_name"
  require_command go
  info=$(go version -m "$(command -v "$command_name")") ||
    die "$command_name: cannot read Go build metadata"
  version=$(awk -v module="$module" '$1 == "mod" && $2 == module { print $3 }' <<<"$info")
  [ -n "$version" ] || die "$command_name: module $module is absent from build metadata"
  printf '%s\n' "$version"
}

assert_version() {
  local label=$1 wanted=$2 actual=$3
  if [ "$actual" != "$wanted" ]; then
    printf '%s: want %s, got %s\n' "$label" "$wanted" "${actual:-<empty>}" >&2
    return 1
  fi
  printf '%s %s\n' "$label" "$actual"
}

check_nodejs() {
  local actual
  require_command node
  actual=$(node --version)
  assert_version node "v${expected[nodejs]}" "$(first_line "$actual")"
}

check_golang() {
  local actual
  require_command go
  actual=$(go env GOVERSION)
  assert_version go "go${expected[golang]}" "$(first_line "$actual")"
}

check_sqlc() {
  local actual
  require_command sqlc
  actual=$(sqlc version)
  assert_version sqlc "v${expected[sqlc]}" "$(first_line "$actual")"
}

check_semgrep() {
  local actual
  require_command semgrep
  actual=$(semgrep --version)
  assert_version semgrep "${expected[semgrep]}" "$(first_line "$actual")"
}

check_gitleaks() {
  local actual
  actual=$(go_module_version gitleaks github.com/zricethezav/gitleaks/v8)
  assert_version gitleaks "v${expected[gitleaks]}" "$actual"
}

check_golangci_lint() {
  local actual
  actual=$(go_module_version golangci-lint github.com/golangci/golangci-lint/v2)
  assert_version golangci-lint "v${expected[golangci-lint]}" "$actual"
}

check_govulncheck() {
  local actual
  actual=$(go_module_version govulncheck golang.org/x/vuln)
  assert_version govulncheck "v${expected[govulncheck]}" "$actual"
}

check_caddy() {
  local actual
  require_command caddy
  actual=$(caddy version)
  actual=$(first_line "$actual")
  assert_version caddy "v${expected[caddy]}" "${actual%% *}"
}

assert_file_line() {
  local label=$1 path=$2 wanted=$3
  grep -Fqx "$wanted" "$path" || die "$label: $path must contain '$wanted'"
}

assert_file_text() {
  local label=$1 path=$2 wanted=$3
  grep -Fq "$wanted" "$path" || die "$label: $path must contain '$wanted'"
}

assert_all_yaml_values() {
  local label=$1 path=$2 key=$3 wanted=$4 actual
  local -a values=()
  mapfile -t values < <(
    awk -v key="$key:" '$1 == key {
      value = $2
      gsub(/^"|"$/, "", value)
      print value
    }' "$path"
  )
  [ "${#values[@]}" -gt 0 ] || die "$label: $path has no '$key' values"
  for actual in "${values[@]}"; do
    [ "$actual" = "$wanted" ] || die "$label: $path has $actual, want $wanted"
  done
}

assert_all_image_versions() {
  local label=$1 path=$2 marker=$3 wanted=$4 actual
  local -a values=()
  mapfile -t values < <(
    awk -v marker="$marker" '
      index($0, marker) {
        value = substr($0, index($0, marker) + length(marker))
        split(value, parts, "-")
        print parts[1]
      }
    ' "$path"
  )
  [ "${#values[@]}" -gt 0 ] || die "$label: $path has no '$marker' image"
  for actual in "${values[@]}"; do
    [ "$actual" = "$wanted" ] || die "$label: $path has $actual, want $wanted"
  done
}

check_repository_contract() {
  assert_file_line node apps/web/.nvmrc "${expected[nodejs]}"
  assert_file_line go go.work "go ${expected[golang]}"
  assert_file_line go apps/server/go.mod "go ${expected[golang]}"
  assert_file_line go packages/schema/gen/go/go.mod "go ${expected[golang]}"
  assert_all_image_versions node deploy/web.Dockerfile \
    docker.io/library/node: "${expected[nodejs]}"
  assert_all_image_versions go deploy/server.Dockerfile \
    docker.io/library/golang: "${expected[golang]}"
  assert_all_image_versions caddy deploy/compose.yml \
    docker.io/library/caddy: "${expected[caddy]}"

  assert_all_yaml_values node .github/workflows/ci.yml node-version \
    "${expected[nodejs]}"
  assert_all_yaml_values go .github/workflows/ci.yml go-version \
    "${expected[golang]}"
  assert_file_text golangci-lint .github/workflows/ci.yml \
    "version: v${expected[golangci-lint]}"
  assert_file_text govulncheck .github/workflows/ci.yml \
    "govulncheck@v${expected[govulncheck]}"
  assert_file_text semgrep .github/workflows/ci.yml \
    "semgrep==${expected[semgrep]}"
  assert_file_text sqlc .github/workflows/ci.yml "sqlc@v${expected[sqlc]}"
  assert_file_text caddy .github/workflows/ci.yml "caddy@v${expected[caddy]}"
}

select_tools() {
  case ${1:-all} in
  all) printf '%s\n' nodejs golang sqlc semgrep gitleaks golangci-lint govulncheck caddy ;;
  ci) printf '%s\n' nodejs golang sqlc golangci-lint govulncheck caddy ;;
  scan) printf '%s\n' semgrep gitleaks ;;
  dev) printf '%s\n' nodejs golang caddy ;;
  *) printf '%s\n' "$@" ;;
  esac
}

load_manifest
check_repository_contract

mapfile -t selected < <(select_tools "$@")
for name in "${selected[@]}"; do
  is_known "$name" || die "unknown requested tool '$name'"
  "check_${name//-/_}"
done
