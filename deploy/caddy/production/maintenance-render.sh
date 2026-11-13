#!/usr/bin/env bash
set -euo pipefail
html=$1

one() {
  local open="<$1>" close="</$1>"
  # grep -c counts matching lines, not occurrences (grep(1)).
  [[ $(grep -o -F -- "$open" "$html" | wc -l) == 1 && $(grep -o -F -- "$close" "$html" | wc -l) == 1 ]] ||
    { echo "maintenance-render: want exactly one unattributed $open block" >&2; exit 1; }
}
one script
one style

fail() { echo "maintenance-render: $1" >&2; exit 1; }

grep -qF -- '<!-- aboutme:maintenance -->' "$html" ||
  fail "missing the aboutme:maintenance marker"

if grep -qiE '<link|@import|<iframe|<object|<embed|<img|style="' "$html"; then
  fail "page uses a forbidden tag or a style attribute"
fi

if grep -qE 'https?://' "$html"; then
  fail "page contains an absolute URL"
fi

while IFS= read -r value; do
  [[ -z $value || $value == \#* ]] || fail "src/href value '$value' is not a fragment"
done < <(grep -oE '\b(src|href)="[^"]*"' "$html" | sed -E 's/^(src|href)="//; s/"$//')

while IFS= read -r arg; do
  [[ -z $arg || $arg == \#* || $arg == data:* ]] || fail "css url($arg) is not a fragment or data: URI"
done < <(grep -oE 'url\([^)]*\)' "$html" | sed -E 's/^url\(//; s/\)$//; s/^["'"'"']//; s/["'"'"']$//')

extract() {
  grep -Pzo "(?s)(?<=<$1>).*?(?=</$1>)" "$html" | tr -d '\0'
}
hash() {
  printf 'sha256-%s' "$(openssl dgst -sha256 -binary | openssl base64 -A)"
}
script_hash=$(extract script | hash)
style_hash=$(extract style | hash)
csp="default-src 'none'; connect-src 'self'; script-src '$script_hash'; style-src '$style_hash'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

printf 'header Content-Security-Policy "%s"\n' "$csp"
printf 'header Retry-After 60\n'
printf 'header Cache-Control "no-store"\n'
printf 'header Content-Type "text/html; charset=utf-8"\n'
printf 'header X-Content-Type-Options nosniff\n'
printf 'header Referrer-Policy no-referrer\n'
printf 'header Strict-Transport-Security "max-age=63072000; includeSubDomains"\n'
