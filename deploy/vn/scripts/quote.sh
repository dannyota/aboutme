#!/usr/bin/env bash
# The price gate to run before `tofu apply` in deploy/vn/prod. OpenTofu has no
# price ceiling, so this quotes what the plan creates and refuses above a
# maximum. Read-only: it only calls vngcloud quote and pricing reads
# (docs/design/vietnam-production.md, "Infrastructure code and state").
#
# Run from deploy/vn/prod:
#   ../scripts/quote.sh --plan <saved.tfplan> --max-price <vnd a month>
# Quotes exactly one server and data volume to create in the saved plan.
# Unknown values or missing creates refuse before any pricing call.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=vng-lib.sh
. "$here/vng-lib.sh"

usage() {
  say "usage: quote.sh --plan <file> --max-price <vnd>" >&2
  exit 2
}

max_price='' plan=''
while (($#)); do
  case $1 in
    --plan)
      (($# >= 2)) || usage
      plan=$2
      shift 2
      ;;
    --max-price)
      (($# >= 2)) || usage
      max_price=$2
      shift 2
      ;;
    *) usage ;;
  esac
done
[[ -n $max_price ]] || usage
[[ $max_price =~ ^[0-9]+$ ]] || usage

[[ -n $plan ]] || usage
need_tools
command -v tofu >/dev/null || die "missing tool on PATH: tofu"
planned=$(tofu show -json "$plan" | jq -ce '
  def creates($t): [.resource_changes[]? | select(.type == $t)
    | select(.change.actions == ["create"]) | .change.after]
    | if length == 1 then .[0] else error("expected one create for " + $t) end;
  def text: type == "string" and length > 0;
  def size: type == "number" and . > 0 and . == floor;
  {server: creates("vngcloud_vserver_server"), volume: creates("vngcloud_vserver_volume")}
  | if (.server | (.zone_id | text) and (.flavor_id | text) and (.image_id | text)
      and (.root_disk_size | size) and (.root_disk_type_id | text)
      and (.encryption_volume | type == "boolean")
      and (if .encryption_volume then (.root_disk_encryption_type | text) else true end)
      and (.attach_floating | type == "boolean"))
    and (.volume | (.zone_id | text) and (.size | size) and (.volume_type_id | text)
      and (.encryption_type | type == "string"))
    then . else error("missing or unknown planned quote values") end') ||
  die "cannot quote the saved plan"
server_encryption=()
if [[ $(jq -r .server.encryption_volume <<<"$planned") == true ]]; then
  server_encryption=(--root-disk-encryption-type-id "$(jq -r .server.root_disk_encryption_type <<<"$planned")")
fi
volume_encryption=()
if [[ -n $(jq -r .volume.encryption_type <<<"$planned") ]]; then
  volume_encryption=(--encryption-type-id "$(jq -r .volume.encryption_type <<<"$planned")")
fi
root_size=$(jq -r .server.root_disk_size <<<"$planned")
data_size=$(jq -r .volume.size <<<"$planned")

# price_of reads a quote's JSON on stdin and prints OptimumPrice.
price_of() {
  jq -r '.OptimumPrice // empty'
}

server=$(vng_ro compute quote-create-server \
  --zone-id "$(jq -r .server.zone_id <<<"$planned")" \
  --flavor-id "$(jq -r .server.flavor_id <<<"$planned")" \
  --image-id "$(jq -r .server.image_id <<<"$planned")" \
  --root-disk-size "$root_size" \
  --root-disk-type-id "$(jq -r .server.root_disk_type_id <<<"$planned")" \
  "${server_encryption[@]}" | price_of)
volume=$(vng_ro volume quote-create-volume \
  --zone-id "$(jq -r .volume.zone_id <<<"$planned")" --size "$data_size" \
  --volume-type-id "$(jq -r .volume.volume_type_id <<<"$planned")" \
  "${volume_encryption[@]}" | price_of)
eip=0
if [[ $(jq -r .server.attach_floating <<<"$planned") == true ]]; then
  eip=$(vng_ro pricing get-quote --resource-type elastic-ip | price_of)
  require_price "elastic IP" "$eip" "$max_price"
fi
require_price "server and ${root_size} GB root disk" "$server" "$max_price"
require_price "${data_size} GB data volume" "$volume" "$max_price"

total=$(awk -v a="$server" -v b="$volume" -v c="$eip" 'BEGIN { printf "%d", a + b + c }')
require_price "total" "$total" "$max_price"
