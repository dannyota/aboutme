#!/usr/bin/env bash
# Bounded HTTPS account revocation proof. Bodies stay in private temporary files.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
(($# == 1)) && [[ $1 =~ ^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$ ]] || { echo invalid_request >&2; exit 2; }
id=$1
status=$(bash "$root/deploy/vn/scripts/operations.sh" verify "$id")
# Verify starts a read attempt. Resolve it through resume before publishing proof.
stop_at=$((SECONDS+60))
while ((SECONDS<stop_at)); do
  status=$(bash "$root/deploy/vn/scripts/operations.sh" resume "$id" 2>/dev/null) || { sleep 1; continue; }
  [[ $(jq -r .challenge.nonce <<<"$status") != null ]] && break
  sleep 1
done
[[ $(jq -r .challenge.phase <<<"$status") == serving && $(jq -r .action.userId <<<"$status") != null ]] || { echo refused >&2; exit 1; }
nonce=$(jq -r .challenge.nonce <<<"$status")
work=$(mktemp -d)
chmod 0700 "$work"
trap 'rm -rf "$work"' EXIT
umask 077
stop_at=$((SECONDS+300))
result=pass
read_url() { # absolute path, expected status
  local code
  ((SECONDS<stop_at)) || return 1
  code=$(curl --silent --show-error --max-time 10 --max-filesize 1048576 \
    --proto '=https' --tlsv1.2 --output "$work/body" --write-out '%{http_code}' "https://aboutme.vn$1" 2>/dev/null) || return 1
  [[ $code == "$2" ]]
}
read_url /readyz 200 || result=fail
read_url /healthz 200 || result=fail
slugs=$(jq -c '(.action.shownSlugs+(.action.returnedSlugs//[]))|unique' <<<"$status")
while IFS= read -r slug; do
  [[ $slug =~ ^[a-z0-9]+(-[a-z0-9]+)*$ ]] || { result=fail; break; }
  for path in "/$slug" "/api/v1/public/resumes/$slug" "/api/v1/public/resumes/$slug/photo" "/api/v1/public/resumes/$slug/pdf" "/$slug.md" "/api/v1/public/resumes/$slug/og.png" "/api/v1/live/$slug"; do
    read_url "$path" 404 || result=fail
  done
  for path in /sitemap.xml /llms.txt; do
    if read_url "$path" 200; then
      if ! python3 - "$path" "$slug" "$work/body" 2>/dev/null <<'PYTHON'
import pathlib, re, sys, xml.etree.ElementTree as ET
path, slug, body = sys.argv[1:]
text = pathlib.Path(body).read_text(encoding="utf-8")
if path == "/sitemap.xml":
    root = ET.fromstring(text)
    ns = "{http://www.sitemaps.org/schemas/sitemap/0.9}"
    if root.tag != ns + "urlset":
        sys.exit(1)
    urls = [loc.text for loc in root.findall(ns + "url/" + ns + "loc")]
    target = "https://aboutme.vn/" + slug
else:
    urls = re.findall(r"\[[^\]\n]*\]\(([^)\s]+)\)", text)
    target = "https://aboutme.vn/" + slug + ".md"
sys.exit(1 if target in urls else 0)
PYTHON
      then result=fail; fi
    else result=fail; fi
  done
done < <(jq -r '.[]' <<<"$slugs")
# Traverse every page within the total bound. The host records card coverage
# and lost committed slugs as limited even when this traversal passes.
page=1
while ((SECONDS<stop_at)); do
  read_url "/api/v1/public/showcase?page=$page" 200 || { result=fail; break; }
  if ! jq -e --argjson slugs "$slugs" 'all(.items[]; .slug as $s | $slugs|index($s)|not)' "$work/body" >/dev/null 2>&1; then result=fail; break; fi
  pages=$(jq -er '.pageCount | select(type=="number" and .>=0 and .==floor)' "$work/body") || { result=fail; break; }
  ((page<pages)) || break
  ((page+=1))
done
((SECONDS<stop_at)) || result=fail
# Fixed stdin receipt only. SSH settings match the operations coordinator.
# shellcheck source=../deploy/vn/scripts/ssh-lib.sh
source "$root/deploy/vn/scripts/ssh-lib.sh"
jq -cn --arg id "$id" --arg nonce "$nonce" --arg result "$result" \
  '{verb:"proof",requestId:$id,phase:"serving",nonce:$nonce,result:$result}' |
  ssh -p "$SSH_PORT" -o BatchMode=yes -o StrictHostKeyChecking=yes -o ForwardAgent=no \
    "aboutme-admin@${DEPLOY_HOST:?}" 'sudo /usr/local/lib/aboutme/operation-run.sh request' 2>/dev/null
