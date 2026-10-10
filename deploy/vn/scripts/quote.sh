#!/usr/bin/env bash
# The price gate to run before `tofu apply` in deploy/vn/prod. OpenTofu has no
# price ceiling, so this quotes what the plan creates and refuses above a
# maximum. Read-only: it only calls vngcloud quote and pricing reads
# (docs/design/vietnam-production.md, "Infrastructure code and state").
#
#   quote.sh --max-price <vnd a month>
#
# The IDs and sizes default to the variables in deploy/vn/prod/variables.tf and
# are overridden by environment variables with the same names in capitals:
# ZONE_ID, FLAVOR_ID, IMAGE_ID, VOLUME_TYPE_ID, ROOT_DISK_GB, DATA_DISK_GB,
# DATA_VOLUME_ENCRYPTION_TYPE. The server is quoted with a plain root disk and
# no data disk, as the root creates it: encrypting a disk at server create adds
# a surcharge (wiki Compute-Servers.md, "Encrypted disks"), while a separately
# created encrypted volume prices the same as a plain one. The data volume is
# quoted with its encryption type when the installed CLI takes one (v0.59.0
# and later), and plain otherwise. The log project is not part of the
# plan. Exit 1 when the total is above the maximum.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=vng-lib.sh
. "$here/vng-lib.sh"

usage() {
  say "usage: quote.sh --max-price <vnd>" >&2
  exit 2
}

max_price=
while (($#)); do
  case $1 in
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

: "${ZONE_ID:=HCM03-1A}"
: "${FLAVOR_ID:=flav-cbb11ae8-f4b7-4e25-96a6-c30bcfcccece}"
: "${IMAGE_ID:=img-34440a82-92fb-40bc-b79c-b1a2b49b93de}"
: "${VOLUME_TYPE_ID:=vtype-61c3fc5b-f4e9-45b4-8957-8aa7b6029018}"
: "${ROOT_DISK_GB:=30}"
: "${DATA_DISK_GB:=20}"
: "${DATA_VOLUME_ENCRYPTION_TYPE:=aes-xts-plain64_256}"

need_tools

# price_of reads a quote's JSON on stdin and prints OptimumPrice.
price_of() {
  jq -r '.OptimumPrice // empty'
}

server=$(vng_ro compute quote-create-server \
  --zone-id "$ZONE_ID" --flavor-id "$FLAVOR_ID" --image-id "$IMAGE_ID" \
  --root-disk-size "$ROOT_DISK_GB" --root-disk-type-id "$VOLUME_TYPE_ID" |
  price_of)
encryption_args=()
if vng_ro volume quote-create-volume --help 2>&1 | grep -q -- '--encryption-type-id'; then
  encryption_args=(--encryption-type-id "$DATA_VOLUME_ENCRYPTION_TYPE")
else
  say "this CLI cannot quote an encrypted volume; quoting it plain (same price)"
fi
volume=$(vng_ro volume quote-create-volume \
  --zone-id "$ZONE_ID" --size "$DATA_DISK_GB" \
  --volume-type-id "$VOLUME_TYPE_ID" "${encryption_args[@]}" | price_of)
eip=$(vng_ro pricing get-quote --resource-type elastic-ip | price_of)

require_price "server and ${ROOT_DISK_GB} GB root disk" "$server" "$max_price"
require_price "${DATA_DISK_GB} GB data volume" "$volume" "$max_price"
require_price "elastic IP" "$eip" "$max_price"

total=$(awk -v a="$server" -v b="$volume" -v c="$eip" 'BEGIN { printf "%d", a + b + c }')
require_price "total" "$total" "$max_price"
