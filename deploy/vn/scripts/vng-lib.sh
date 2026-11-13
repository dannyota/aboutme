# shellcheck shell=bash

VNG_PROFILE=${VNG_PROFILE:-}
VNG_BIN=${VNG_BIN:-vngcloud}
VNG_REGION=hcm-3

say() { printf '%s\n' "$*"; }
die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

need_tools() {
  local tool
  for tool in "$VNG_BIN" jq awk; do
    command -v "$tool" >/dev/null 2>&1 || die "missing tool on PATH: $tool"
  done
}

vng() {
  local profile=()
  [[ -z $VNG_PROFILE ]] || profile=(--profile "$VNG_PROFILE")
  "$VNG_BIN" "${profile[@]}" --region "$VNG_REGION" --output json "$@"
}

vng_ro() {
  vng --read-only "$@"
}

require_price() {
  local what=$1 quoted=${2:-} max=${3:-}
  local num='^[0-9]+([.][0-9]+)?$'
  [[ $quoted =~ $num ]] || die "$what: quote is not a number: '$quoted'"
  [[ $max =~ $num ]] || die "$what: maximum is not a number: '$max'"
  if awk -v q="$quoted" 'BEGIN { exit !(q == 0) }'; then
    die "$what: quoted 0 VND, so it is unpriced; refusing"
  fi
  if awk -v q="$quoted" -v m="$max" 'BEGIN { exit !(q > m) }'; then
    die "$what: $quoted VND a month is above the maximum $max VND; refusing"
  fi
  say "$what: $quoted VND a month (max $max)"
}

json_id_by_name() {
  jq -r --arg n "$1" '
    [.Items[]?
     | select((.Name // .name // .ProjectName // .projectName) == $n)
     | (.ID // .id // .UUID // .uuid)] | .[0] // empty'
}
