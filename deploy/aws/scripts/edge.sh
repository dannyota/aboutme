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
  # Absent until the first tls.sh export run writes it.
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

# Runs on every deploy: production's only edge is CloudFront
# (docs/design/cloudfront-edge.md; ADR 0054). Checks the live distribution
# with the base caller's own credentials, as origin_ip does: cloudfront:List*
# and ec2:DescribeAddresses are outside the deploy role's closed list.
edge_distribution_check() {
  local ip domain dists count origin https_port protocol mtls_arn origin_domain
  ip=$(aws --region "$region" ec2 describe-addresses --filters Name=tag:Name,Values=aboutme-prod \
    --query 'Addresses[0].PublicIp' --output text) &&
    [[ $ip =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]] || {
    say "could not resolve the origin address"
    return 1
  }
  domain="ec2-${ip//./-}.ap-southeast-1.compute.amazonaws.com"

  dists=$(aws cloudfront list-distributions --output json) || {
    say "could not list CloudFront distributions"
    return 1
  }
  count=$(jq '[.DistributionList.Items[]? | select(.Aliases.Items[]? == "aboutme.vn")] | length' <<<"$dists")
  ((count == 1)) || {
    say "expected exactly one CloudFront distribution for aboutme.vn; found $count; run tofu apply first"
    return 1
  }
  origin=$(jq -c '[.DistributionList.Items[] | select(.Aliases.Items[]? == "aboutme.vn")][0].Origins.Items[0]' <<<"$dists")
  https_port=$(jq -r '.CustomOriginConfig.HTTPSPort // empty' <<<"$origin")
  protocol=$(jq -r '.CustomOriginConfig.OriginProtocolPolicy // empty' <<<"$origin")
  # CloudFront API 2020-05-31: CustomOriginConfig.OriginMtlsConfig.
  mtls_arn=$(jq -r '.CustomOriginConfig.OriginMtlsConfig.ClientCertificateArn // empty' <<<"$origin")
  origin_domain=$(jq -r '.DomainName // empty' <<<"$origin")

  [[ $https_port == 8443 ]] || {
    say "the distribution's origin HTTPS port is ${https_port:-unset}, not 8443; run tofu apply first"
    return 1
  }
  [[ $protocol == https-only ]] || {
    say "the distribution's origin protocol policy is ${protocol:-unset}, not https-only; run tofu apply first"
    return 1
  }
  [[ -n $mtls_arn ]] || {
    say "the distribution's origin has no mTLS client certificate; run tofu apply first"
    return 1
  }
  [[ $origin_domain == "$domain" ]] || {
    say "the distribution's origin is ${origin_domain:-unset}, not $domain; run tofu apply first"
    return 1
  }
}

# The direct-origin check covers 443 and 8443 (docs/design/cloudfront-edge.md,
# "Deploys and monitoring"). The security groups drop these packets, so only a
# timeout (curl exit 28) passes: a refused connection or a TLS rejection means
# the port is reachable, and mTLS alone is guarding it. It never retries into
# a pass.
edge_origin_closed() { # ip port
  local status=0
  curl -sk -m "${DEPLOY_SMOKE_TIMEOUT:-5}" -o /dev/null "https://$1:$2/" || status=$?
  ((status == 28)) || {
    say "smoke: the origin is reachable directly on port $2 (curl exit $status); check the security groups"
    return 1
  }
}
