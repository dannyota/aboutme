#!/usr/bin/env bash
# Rewrites the generated Compose route table for the production host. Each
# token must appear exactly as often as expected, so a generator change fails
# the build instead of silently skipping the rewrite.
set -euo pipefail
content=$(<"$1")
replace() {
  local from=$1 to=$2 want=$3 got
  got=$(grep -oF -- "$from" <<<"$content" | wc -l)
  if ((got != want)); then
    echo "render: expected $want of '$from', found $got" >&2
    exit 1
  fi
  content=${content//"$from"/"$to"}
}
replace 'reverse_proxy server:8080' 'reverse_proxy 127.0.0.1:8080' 2
replace 'reverse_proxy web:3000' 'reverse_proxy 127.0.0.1:3000' 1
replace '{http.request.remote.host}' '{vars.client_address}' 2
printf '%s\n' "$content"
