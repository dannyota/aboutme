#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
export QUOTE_LOG=$work/args QUOTE_FIXTURE=$work/plan.json
cat >"$work/bin/tofu" <<'STUB'
#!/usr/bin/env bash
[[ $* == 'show -json saved.tfplan' ]] || exit 2
cat "$QUOTE_FIXTURE"
STUB
cat >"$work/bin/vngcloud" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$QUOTE_LOG"
printf '{"OptimumPrice":100}\n'
STUB
chmod +x "$work/bin/"*
export PATH=$work/bin:$PATH
jq -n '{resource_changes:[
 {type:"vngcloud_vserver_server",change:{actions:["create"],after:{zone_id:"zone-server",flavor_id:"large",image_id:"image",root_disk_size:80,root_disk_type_id:"root-type",encryption_volume:true,root_disk_encryption_type:"root-encryption",attach_floating:true}}},
 {type:"vngcloud_vserver_volume",change:{actions:["create"],after:{zone_id:"zone-volume",size:90,volume_type_id:"data-type",encryption_type:"data-encryption"}}}]}' >"$QUOTE_FIXTURE"
run_quote() {
  (cd "$root/deploy/vn/prod"; FLAVOR_ID=cheap ROOT_DISK_GB=1 bash ../scripts/quote.sh --plan saved.tfplan --max-price "$1")
}
run_quote 1000 >"$work/out" 2>&1
grep -qF -- '--zone-id zone-server --flavor-id large --image-id image --root-disk-size 80 --root-disk-type-id root-type --root-disk-encryption-type-id root-encryption' "$QUOTE_LOG"
grep -qF -- '--zone-id zone-volume --size 90 --volume-type-id data-type --encryption-type-id data-encryption' "$QUOTE_LOG"
if run_quote 250 >"$work/out" 2>&1; then exit 1; fi
for filter in '.resource_changes=[]' '.resource_changes[0].change.actions=["no-op"]' '.resource_changes[1].change.actions=["no-op"]' '.resource_changes[0].change.after.flavor_id=null'; do
  jq "$filter" "$QUOTE_FIXTURE" >"$work/invalid"
  cp "$QUOTE_FIXTURE" "$work/valid"
  mv "$work/invalid" "$QUOTE_FIXTURE"
  : >"$QUOTE_LOG"
  if run_quote 1000 >"$work/out" 2>&1; then exit 1; fi
  [[ ! -s $QUOTE_LOG ]]
  mv "$work/valid" "$QUOTE_FIXTURE"
done
echo 'saved plan quote tests passed'
