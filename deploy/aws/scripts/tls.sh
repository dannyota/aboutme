#!/usr/bin/env bash
# Generates the origin key and CSR, and the origin-pull client certificate.
# The origin key goes straight to SSM. The origin-pull files wait in the
# per-user tmpfs until the owner uploads them in the Cloudflare dashboard;
# `tls.sh --forget-pull` then deletes them. No private key is printed.
set -euo pipefail
region=ap-southeast-1
root=$(git rev-parse --show-toplevel)
pull_dir=${XDG_RUNTIME_DIR:?XDG_RUNTIME_DIR must point at a per-user tmpfs}/aboutme-origin-pull
umask 077

if [[ ${1:-} == --forget-pull ]]; then
  rm -rf "$pull_dir"
  echo "origin-pull files deleted"
  exit 0
fi
if [[ -e $pull_dir ]]; then
  echo "tls: $pull_dir exists; upload or delete it first (--forget-pull)" >&2
  exit 1
fi

work=$(mktemp -d -p "$XDG_RUNTIME_DIR")
trap 'rm -rf "$work"' EXIT
ec=(-newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes)

openssl req -new "${ec[@]}" -subj /CN=aboutme.vn -keyout "$work/origin.key" \
  -out "$root/deploy/aws/prod/origin.csr" 2>/dev/null
jq -Rs '{Name: "/aboutme/prod/tls/origin-key", Type: "SecureString", Value: ., Overwrite: true}' \
  "$work/origin.key" >"$work/request.json"
aws ssm put-parameter --region "$region" --cli-input-json "file://$work/request.json" >/dev/null

mkdir -p "$pull_dir"
openssl req -x509 "${ec[@]}" -days 3650 -subj /CN=aboutme-origin-pull-ca \
  -keyout "$work/ca.key" -out "$work/ca.pem" 2>/dev/null
openssl req -new "${ec[@]}" -subj /CN=aboutme-origin-pull \
  -keyout "$pull_dir/client.key" -out "$work/pull.csr" 2>/dev/null
openssl x509 -req -in "$work/pull.csr" -CA "$work/ca.pem" -CAkey "$work/ca.key" \
  -CAcreateserial -days 3650 -out "$pull_dir/client.pem" 2>/dev/null
jq -Rs '{Name: "/aboutme/prod/tls/origin-pull-ca", Type: "String", Value: ., Overwrite: true}' \
  "$work/ca.pem" >"$work/request.json"
aws ssm put-parameter --region "$region" --cli-input-json "file://$work/request.json" >/dev/null

echo "origin CSR written to deploy/aws/prod/origin.csr; origin key stored in SSM"
echo "upload $pull_dir/client.pem and $pull_dir/client.key in the Cloudflare dashboard,"
echo "then run: bash deploy/aws/scripts/tls.sh --forget-pull"
