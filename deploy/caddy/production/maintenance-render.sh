#!/usr/bin/env bash
# Computes the maintenance page's Content-Security-Policy hashes from its own
# script and style content, and emits the Caddy header directives for the
# maintenance site block to import. This is the one place the hashes are
# computed. The page's bytes are served straight from disk by file_server
# (see Caddyfile.maintenance), never echoed back through this script or any
# Caddy directive, so nothing here can expand a Caddy placeholder ({path},
# {env.*}, {file.*}, ...) that the page's own braces might otherwise match.
set -euo pipefail
html=$1

one() { # tag -> exit 1 unless the file has exactly one open and close tag
  local open="<$1>" close="</$1>"
  # -c counts matching lines, not occurrences, so two tags on one line would
  # pass; -o counts every occurrence.
  [[ $(grep -o -F -- "$open" "$html" | wc -l) == 1 && $(grep -o -F -- "$close" "$html" | wc -l) == 1 ]] ||
    { echo "maintenance-render: want exactly one unattributed $open block" >&2; exit 1; }
}
one script
one style

fail() { echo "maintenance-render: $1" >&2; exit 1; } # message

# deploy.sh greps the served body for this marker to confirm the maintenance
# page, not some other 503 response, is up before it runs a database step.
grep -qF -- '<!-- aboutme:maintenance -->' "$html" ||
  fail "missing the aboutme:maintenance marker"

# These tags and the style attribute either fetch something (link, @import,
# iframe, object, embed, img) or reopen an inline-style hole this page does
# not need (style="). The CSP already blocks a live request; this fails the
# build before one ships.
if grep -qiE '<link|@import|<iframe|<object|<embed|<img|style="' "$html"; then
  fail "page uses a forbidden tag or a style attribute"
fi

# No absolute URL: the page must never name a remote host.
if grep -qE 'https?://' "$html"; then
  fail "page contains an absolute URL"
fi

# Every src= or href= value must be a same-document fragment (an inline SVG
# use, such as href="#id"), never a path or URL that fetches something.
while IFS= read -r value; do
  [[ -z $value || $value == \#* ]] || fail "src/href value '$value' is not a fragment"
done < <(grep -oE '\b(src|href)="[^"]*"' "$html" | sed -E 's/^(src|href)="//; s/"$//')

# Same rule for CSS url(...): only a fragment (url(#id), for an inline SVG
# paint reference) or a data: URI, never a fetched path.
while IFS= read -r arg; do
  [[ -z $arg || $arg == \#* || $arg == data:* ]] || fail "css url($arg) is not a fragment or data: URI"
done < <(grep -oE 'url\([^)]*\)' "$html" | sed -E 's/^url\(//; s/\)$//; s/^["'"'"']//; s/["'"'"']$//')

extract() { # tag -> exact bytes between <tag> and </tag>, no NUL terminator
  grep -Pzo "(?s)(?<=<$1>).*?(?=</$1>)" "$html" | tr -d '\0'
}
hash() { # content on stdin -> sha256-BASE64, the CSP hash-source format
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
