# shellcheck shell=bash
# Sourced by deploy.sh. The deploy-time origin-certificate guard
# (docs/design/cloudfront-edge.md, "Origin certificate"; ADR 0054: "A deploy
# refuses to run when it has fewer than 21 days left"). Needs $region, $work,
# and say() already set.

# /aboutme/prod/tls/origin-cert holds only the public certificate chain and
# /aboutme/prod/tls/origin-key-sha256 only the SHA-256 of the origin key's
# public key (tls.sh writes both); neither is secret, and the private key is
# never read. They are read with the base caller's own credentials, as
# origin_ip does: ssm:GetParameter is outside the deploy role's closed IAM
# list. A certificate that does not match the stored key would stop every
# Caddy start, so the deploy refuses first.
edge_origin_cert_check() {
  if ! aws --region "$region" ssm get-parameter --name /aboutme/prod/tls/origin-cert \
      --query Parameter.Value --output text >"$work/origin-cert.pem" 2>/dev/null; then
    say "could not read the origin certificate"
    return 1
  fi
  # A separate parse check first, so a missing or malformed parameter reports
  # as unreadable rather than as a false "expiring soon".
  openssl x509 -in "$work/origin-cert.pem" -noout -enddate >/dev/null 2>&1 || {
    say "could not read the origin certificate"
    return 1
  }
  openssl x509 -in "$work/origin-cert.pem" -noout -checkend $((21 * 86400)) >/dev/null 2>&1 || {
    say "the origin certificate expires in fewer than 21 days; run tls.sh export, then deploy"
    return 1
  }
  local want got
  want=$(aws --region "$region" ssm get-parameters --names /aboutme/prod/tls/origin-key-sha256 \
    --query 'Parameters[0].Value' --output text 2>/dev/null) || {
    say "could not read the origin key fingerprint"
    return 1
  }
  # Absent until the first tls.sh export or tls.sh origin run writes it.
  if [[ $want == None ]]; then
    say "no origin key fingerprint stored yet; skipping the key match check"
    return 0
  fi
  got=$(openssl x509 -in "$work/origin-cert.pem" -noout -pubkey | openssl pkey -pubin -outform DER |
    openssl dgst -sha256 -r | cut -d' ' -f1) || {
    say "could not read the origin certificate"
    return 1
  }
  [[ $got == "$want" ]] || {
    say "the origin certificate does not match the stored origin key; run tls.sh export, then deploy"
    return 1
  }
}
