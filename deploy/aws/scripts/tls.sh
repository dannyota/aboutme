#!/usr/bin/env bash
# Production TLS material. No private key is printed.
#
#   tls.sh origin       new origin key (to SSM) and deploy/aws/prod/origin.csr
#   tls.sh pull         new origin-pull CA (certificate to SSM, key discarded)
#                       and client certificate and key in the per-user tmpfs,
#                       ready for the Cloudflare dashboard upload
#   tls.sh forget-pull  delete the client files after the upload
#   tls.sh export       export the ACM origin certificate for aboutme.vn and
#                       www.aboutme.vn (docs/design/cloudfront-edge.md,
#                       "Origin certificate"; ADR 0054) to SSM, and print the
#                       next step
#
# A new origin key needs a new Origin CA certificate from the CSR.
set -euo pipefail
region=ap-southeast-1
root=$(git rev-parse --show-toplevel)
runtime=${XDG_RUNTIME_DIR:?XDG_RUNTIME_DIR must point at a per-user tmpfs}
pull_dir=$runtime/aboutme-origin-pull
umask 077

store() { # parameter type file [tier]
  jq -Rs --arg n "$1" --arg t "$2" --arg tier "${4:-Standard}" \
    '{Name: $n, Type: $t, Value: ., Overwrite: true, Tier: $tier}' "$3" >"$work/request.json"
  aws ssm put-parameter --region "$region" --cli-input-json "file://$work/request.json" >/dev/null
}

# Writes the SHA-256 of a PEM private key's public key, in hex, to a file.
# It is public data; deploy.sh compares the one stored at origin-key-sha256
# with the stored certificate's, so a key and certificate that do not match
# stop a deploy instead of a Caddy start.
pubkey_sha256() { # key-file out-file
  openssl pkey -in "$1" -pubout -outform DER | openssl dgst -sha256 -r | cut -d' ' -f1 | tr -d '\n' >"$2"
}

work=$(mktemp -d -p "$runtime")
trap 'rm -rf "$work"' EXIT
ec=(-newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes)

case "${1:-}" in
  origin)
    openssl req -new "${ec[@]}" -subj /CN=aboutme.vn -keyout "$work/origin.key" \
      -out "$root/deploy/aws/prod/origin.csr" 2>/dev/null
    # The fingerprint goes first, so any stored certificate now fails the
    # deploy check until one for this key is stored.
    pubkey_sha256 "$work/origin.key" "$work/origin-key.sha256"
    store /aboutme/prod/tls/origin-key-sha256 String "$work/origin-key.sha256"
    store /aboutme/prod/tls/origin-key SecureString "$work/origin.key"
    echo "origin key stored in SSM; request a new Origin CA certificate from deploy/aws/prod/origin.csr"
    echo "deploy.sh refuses to deploy until /aboutme/prod/tls/origin-cert holds a certificate for this key"
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
  export)
    # Exactly one certificate must match: an exportable, issued ACM
    # certificate for aboutme.vn.
    # ListCertificates returns only RSA certificates unless keyTypes names
    # others.
    # shellcheck disable=SC2016 # The backticks are JMESPath literals.
    certs=$(aws acm list-certificates --region "$region" --certificate-statuses ISSUED \
      --includes keyTypes=EC_prime256v1,exportOption=ENABLED \
      --query 'CertificateSummaryList[?DomainName==`aboutme.vn`].CertificateArn' --output text)
    read -ra cert_arns <<<"$certs"
    ((${#cert_arns[@]} == 1)) ||
      { echo "tls: expected exactly one exportable, issued certificate for aboutme.vn in $region; found ${#cert_arns[@]}" >&2; exit 1; }
    cert_arn=${cert_arns[0]}

    # No trailing newline: ACM receives the file's exact bytes, while openssl
    # -passin file: drops a newline, so the two would disagree.
    openssl rand -hex 32 | tr -d '\n' >"$work/passphrase"
    aws acm export-certificate --region "$region" --certificate-arn "$cert_arn" \
      --passphrase "fileb://$work/passphrase" --output json >"$work/export.json"
    jq -r .PrivateKey <"$work/export.json" >"$work/key.enc.pem"
    openssl pkey -passin file:"$work/passphrase" -in "$work/key.enc.pem" -out "$work/key.pem" 2>/dev/null ||
      { echo "tls: could not decrypt the exported key" >&2; exit 1; }
    rm -f "$work/passphrase"

    jq -r .Certificate <"$work/export.json" >"$work/leaf.pem"
    jq -r .CertificateChain <"$work/export.json" >"$work/chain.pem"
    cat "$work/leaf.pem" "$work/chain.pem" >"$work/fullchain.pem"

    # Every check below reads only public material, never the key itself.
    key_pub=$(openssl pkey -in "$work/key.pem" -pubout 2>/dev/null)
    leaf_pub=$(openssl x509 -in "$work/leaf.pem" -pubkey -noout 2>/dev/null)
    [[ -n $key_pub && $key_pub == "$leaf_pub" ]] ||
      { echo "tls: the exported key does not match the certificate" >&2; exit 1; }
    sans=$(openssl x509 -in "$work/leaf.pem" -noout -ext subjectAltName 2>/dev/null)
    { grep -qE '(^|[ ,])DNS:aboutme\.vn(,|$)' <<<"$sans" && grep -qE '(^|[ ,])DNS:www\.aboutme\.vn(,|$)' <<<"$sans"; } ||
      { echo "tls: the certificate does not cover both aboutme.vn and www.aboutme.vn" >&2; exit 1; }
    openssl x509 -in "$work/leaf.pem" -noout -checkend $((21 * 86400)) >/dev/null ||
      { echo "tls: the certificate has fewer than 21 days left" >&2; exit 1; }
    # Cloudflare and CloudFront both need the chain to reach a public root.
    openssl verify -untrusted "$work/chain.pem" "$work/leaf.pem" >/dev/null 2>&1 ||
      { echo "tls: the exported chain does not verify to a trusted root" >&2; exit 1; }

    # Three writes: the key's fingerprint first, then the key, then the chain.
    # Until the last one succeeds, the stored fingerprint no longer matches
    # the stored certificate, so deploy.sh refuses instead of starting Caddy
    # with a key and certificate that do not match.
    pubkey_sha256 "$work/key.pem" "$work/origin-key.sha256"
    store /aboutme/prod/tls/origin-key-sha256 String "$work/origin-key.sha256"
    store /aboutme/prod/tls/origin-key SecureString "$work/key.pem"
    store /aboutme/prod/tls/origin-cert String "$work/fullchain.pem" Intelligent-Tiering ||
      { echo "tls: the key is stored but the certificate is not; rerun tls.sh export before any deploy" >&2; exit 1; }

    expiry=$(openssl x509 -in "$work/leaf.pem" -noout -enddate | cut -d= -f2)
    bytes=$(wc -c <"$work/fullchain.pem")
    tier=$(aws ssm describe-parameters --region "$region" \
      --parameter-filters Key=Name,Values=/aboutme/prod/tls/origin-cert --query 'Parameters[0].Tier' --output text) ||
      tier=unknown
    cert_id=${cert_arn##*/}
    echo "origin certificate $cert_id exported; expires $expiry"
    echo "chain stored: $bytes bytes, parameter tier $tier"
    echo "next: redeploy the live tag with deploy.sh <tag>"
    ;;
  *)
    echo "usage: tls.sh origin|pull|forget-pull|export" >&2
    exit 2
    ;;
esac
