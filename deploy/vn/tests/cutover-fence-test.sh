#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
fence=$repo_root/deploy/vn/scripts/fence.sh
safety=$repo_root/deploy/vn/scripts/deploy-safety.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/fence"

cat >"$work/bin/id" <<'STUB'
#!/usr/bin/env bash
[[ ${1-} == -u ]] && echo 0
STUB
cat >"$work/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
if [[ ${1-} == is-active && ${3-} == aboutme-maintenance ]]; then exit 0; fi
exit 1
STUB
cat >"$work/bin/pgrep" <<'STUB'
#!/usr/bin/env bash
exit 1
STUB
cat >"$work/bin/logger" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
chmod +x "$work/bin/"*
export PATH=$work/bin:$PATH FENCE_DIR=$work/fence FENCE_SAFETY_LIB=$safety

for template in aboutme-server aboutme-web aboutme-caddy aboutme-migrate aboutme-db-setup; do
  grep -Fq "ExecStartPre=/usr/local/lib/aboutme/fence.sh check @RELEASE_NUMBER@ $template" \
    "$repo_root/deploy/vn/host/quadlet/$template.container.in" || {
    echo "$template does not call the start-time fence" >&2
    exit 1
  }
done
grep -Fq '"$lib/fence.sh" check "$number" "aboutme-job@$name"' \
  "$repo_root/deploy/vn/host/job-run.sh" || {
  echo "scheduled jobs do not call the start-time fence" >&2
  exit 1
}

"$fence" init
op=$("$fence" lock cutover v0.4.7)
"$fence" mutation-start "$op" v0.4.7 reset-db
for unit in aboutme-server aboutme-web aboutme-caddy \
  aboutme-job@media-deletion-sweep aboutme-migrate; do
  if "$fence" check 4007 "$unit" 2>/dev/null; then
    echo "$unit started while a cutover mutation was active" >&2
    exit 1
  fi
done
"$fence" check 4007 aboutme-db-setup
cp "$work/fence/cutover-mutation.json" "$work/reset-db-marker.json"
jq '.operation_id = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' \
  "$work/reset-db-marker.json" >"$work/fence/cutover-mutation.json"
if "$fence" check 4007 aboutme-db-setup 2>/dev/null; then
  echo "db-setup started with a marker owned by another operation" >&2
  exit 1
fi
cp "$work/reset-db-marker.json" "$work/fence/cutover-mutation.json"
if "$fence" lock deploy v0.4.7 2>/dev/null; then
  echo "a deploy acquired the cutover lock" >&2
  exit 1
fi
if "$fence" release "$op" 2>/dev/null; then
  echo "the cutover lock released with an active mutation" >&2
  exit 1
fi
"$fence" mutation-clear "$op" reset-db
if "$fence" check 4007 aboutme-server 2>/dev/null; then
  echo "the server started while the cutover lock remained" >&2
  exit 1
fi
if "$fence" check 4007 aboutme-db-setup 2>/dev/null; then
  echo "db-setup started without its reset-db marker" >&2
  exit 1
fi
"$fence" mutation-start "$op" v0.4.7 media
if "$fence" check 4007 aboutme-db-setup 2>/dev/null; then
  echo "db-setup started for a media mutation" >&2
  exit 1
fi
if "$fence" recover-cutover-media "$op" wrong-phrase inspect 2>/dev/null; then
  echo "media recovery accepted no operator attestation" >&2
  exit 1
fi
"$fence" recover-cutover-media "$op" media-writer-stopped-and-result-inspected inspect
"$fence" release "$op"

deploy_op=$("$fence" lock deploy v0.4.7)
for unit in aboutme-server aboutme-web aboutme-caddy \
  aboutme-job@media-deletion-sweep aboutme-db-setup aboutme-migrate; do
  "$fence" check 4007 "$unit"
done
"$fence" release "$deploy_op"

printf '{"state":"active"}\n' >"$work/fence/cutover-mutation.json"
if "$fence" check 4007 aboutme-server 2>/dev/null; then
  echo "the server started with a malformed cutover marker" >&2
  exit 1
fi
if "$fence" check 4007 aboutme-db-setup 2>/dev/null; then
  echo "db-setup started with a malformed cutover marker" >&2
  exit 1
fi
rm -f "$work/fence/cutover-mutation.json"
ln -s "$work/reset-db-marker.json" "$work/fence/cutover-mutation.json"
if "$fence" check 4007 aboutme-db-setup 2>/dev/null; then
  echo "db-setup started with a symlinked cutover marker" >&2
  exit 1
fi

echo "cutover fence tests passed"
