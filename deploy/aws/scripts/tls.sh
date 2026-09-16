#!/usr/bin/env bash
# Production TLS material. No private key is printed.
#
#   tls.sh origin       new origin key (to SSM) and deploy/aws/prod/origin.csr
#   tls.sh pull         new origin-pull CA (certificate to SSM, key discarded)
#                       and client certificate and key in the per-user tmpfs,
#                       ready for the Cloudflare dashboard upload
#   tls.sh forget-pull  delete the client files after the upload
#
# A new origin key needs a new Origin CA certificate from the CSR.
set -euo pipefail
region=ap-southeast-1
root=$(git rev-parse --show-toplevel)
runtime=${XDG_RUNTIME_DIR:?XDG_RUNTIME_DIR must point at a per-user tmpfs}
pull_dir=$runtime/aboutme-origin-pull
umask 077

store() { # parameter type file
  jq -Rs --arg n "$1" --arg t "$2" '{Name: $n, Type: $t, Value: ., Overwrite: true}' "$3" >"$work/request.json"
  aws ssm put-parameter --region "$region" --cli-input-json "file://$work/request.json" >/dev/null
}

work=$(mktemp -d -p "$runtime")
trap 'rm -rf "$work"' EXIT
ec=(-newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes)

case "${1:-}" in
  origin)
    openssl req -new "${ec[@]}" -subj /CN=aboutme.vn -keyout "$work/origin.key" \
      -out "$root/deploy/aws/prod/origin.csr" 2>/dev/null
    store /aboutme/prod/tls/origin-key SecureString "$work/origin.key"
    echo "origin key stored in SSM; request a new Origin CA certificate from deploy/aws/prod/origin.csr"
    ;;
  pull)
    if [[ -e $pull_dir ]]; then
      echo "tls: $pull_dir exists; upload it, then run: tls.sh forget-pull" >&2
      exit 1
    fi
    mkdir -p "$pull_dir"
    openssl req -x509 "${ec[@]}" -days 3650 -subj /CN=aboutme-origin-pull-ca \
      -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign \
      -keyout "$work/ca.key" -out "$work/ca.pem" 2>/dev/null
    openssl req -new "${ec[@]}" -subj /CN=aboutme-origin-pull \
      -keyout "$pull_dir/client.key" -out "$work/client.csr" 2>/dev/null
    printf '%s\n' basicConstraints=critical,CA:FALSE keyUsage=critical,digitalSignature \
      extendedKeyUsage=clientAuth >"$work/client.ext"
    openssl x509 -req -in "$work/client.csr" -CA "$work/ca.pem" -CAkey "$work/ca.key" \
      -CAcreateserial -days 3650 -sha256 -extfile "$work/client.ext" -out "$pull_dir/client.pem" 2>/dev/null
    store /aboutme/prod/tls/origin-pull-ca String "$work/ca.pem"
    echo "origin-pull CA stored in SSM; its key is discarded"
    echo "upload $pull_dir/client.pem and client.key in the Cloudflare dashboard, then run: tls.sh forget-pull"
    ;;
  forget-pull)
    rm -rf "$pull_dir"
    echo "origin-pull files deleted"
    ;;
  *)
    echo "usage: tls.sh origin|pull|forget-pull" >&2
    exit 2
    ;;
esac
