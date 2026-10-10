# shellcheck shell=bash
# Shared helpers for the laptop-side vngcloud scripts in deploy/vn/. Source it;
# do not run it. Design: docs/design/vietnam-production.md, "Infrastructure
# code and state" (the CLI covers service accounts, buckets, and the budget).
#
# VNG_PROFILE names a writable operator profile in ~/.vngcloud that the owner
# created. These scripts never write credentials. Nothing here prints a secret.

VNG_PROFILE=${VNG_PROFILE:-aboutme}
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

# vng runs the CLI as the operator profile with JSON output.
vng() {
  "$VNG_BIN" --profile "$VNG_PROFILE" --region "$VNG_REGION" --output json "$@"
}

# vng_ro is vng with the CLI's read-only guard, so it refuses any write.
vng_ro() {
  vng --read-only "$@"
}

# require_price <what> <quoted-vnd> <max-vnd> fails when the quote is 0 (an
# unpriced order), is not a number, or is above the maximum.
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

# json_id_by_name <name> reads a list command's JSON on stdin and prints the ID
# of the item whose name is exactly <name>, or nothing. The CLI prints Go field
# names; the lowercase alternatives guard against a casing change.
json_id_by_name() {
  jq -r --arg n "$1" '
    [.Items[]?
     | select((.Name // .name // .ProjectName // .projectName) == $n)
     | (.ID // .id // .UUID // .uuid)] | .[0] // empty'
}
