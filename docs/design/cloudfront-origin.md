# CloudFront origin

This page is part of the
[undeployed CloudFront edge reference](cloudfront-edge.md).

## Origin access

The origin stays a public Elastic IP. VPC origins take an EC2 instance only in a
private subnet ([VPC origins][cf-vpc]), which would need a NAT gateway for image
pulls and AWS and outside APIs (USD 0.045 per hour in the published US East
example, [VPC pricing][vpc-price]), and they exclude origin mTLS ([enable
mTLS][cf-mtls-enable]).

Two layers replace Cloudflare's ranges and Authenticated Origin Pulls:

1. **Network.** A new security group admits TCP 8443 only from the AWS-managed
   prefix list `com.amazonaws.global.cloudfront.origin-facing` ([prefix
   list][cf-prefix]). The list weighs 55 rules against the default quota of 60
   per group ([weights][vpc-pl-weight]), so it gets its own group, attached to
   the host beside the existing one. The list admits every CloudFront customer,
   as Cloudflare's ranges admit every Cloudflare customer.
2. **Origin mTLS.** CloudFront presents a client certificate from our own
   private CA, and Caddy requires and verifies it on 8443 ([origin
   mTLS][cf-mtls]). This is the counterpart of Cloudflare's zone-level origin
   pull certificate. The certificate is ECDSA P-256 with the TLS client
   authentication extended key usage, imported into ACM in `us-east-1`
   ([requirements][cf-mtls-cert]). `tls.sh client-ca` generates it: the CA key
   is discarded, the CA certificate goes to SSM, and `tls.sh client-import`
   moves the client certificate and key from the per-user tmpfs to ACM, then
   deletes them. Another distribution cannot present it. Origin mTLS is
   available on pay-as-you-go, Business, and Premium, not on the Free plan
   ([enable mTLS][cf-mtls-enable]).

A secret origin header is not used. It is weaker than mTLS: the value travels in
every request and sits in the distribution config and OpenTofu state. It stays
the fallback if origin mTLS fails on the account.

Caddy runs one listener per edge, each with its own client certificate trust
pool and client address rule: only the CloudFront listener on 8443 runs today,
and 443 is closed. A future edge, such as vCDN, gets its own listener, so a
header one edge sets is never trusted on another's. The origin domain name is
the Elastic IP's public DNS name (`ec2-…ap-southeast-1.compute.amazonaws.com`),
read from OpenTofu, so no DNS record points at the origin ([origin
domain][cf-origin]).

## Origin certificate

CloudFront accepts only a certificate from a CA in the Mozilla list, with the
full chain, whose names include the origin domain or, when the viewer `Host`
header is forwarded, that host ([origin TLS][cf-origin-tls]). The Cloudflare
Origin CA certificate does not qualify. The origin gets an **ACM exportable
public certificate** for `aboutme.vn` and `www.aboutme.vn` in `ap-southeast-1`:

- USD 7 per name at issuance and each renewal ([ACM pricing][acm-price]), valid
  198 days and renewed 45 days before expiry ([exportable][acm-export]): about
  USD 33 a year for two names. It shares the viewer certificate's DNS validation
  records, which work in any region ([DNS validation][acm-dns]).
- `tls.sh` gains an export mode. It exports with a one-time passphrase inside a
  private tmpfs directory, removes the passphrase, and writes the key to the
  existing SecureString `/aboutme/prod/tls/origin-key` and the leaf and chain to
  String parameters. No task definition or entrypoint change is needed beyond
  appending the chain. **Verify:** leaf plus chain may exceed the 4 KB limit of
  a standard parameter ([tiers][ssm-tiers]); if so, the chain gets its own
  parameter.
- An EventBridge rule on `ACM Certificate Available` with action `RENEWAL` for
  this certificate emails the owner through the existing SNS topic
  ([events][acm-events]). The owner runs the export and redeploys the live tag.
  `deploy.sh` refuses to deploy when the deployed origin certificate expires in
  fewer than 21 days, so a missed email cannot silently run out the 45 days.
- Full (strict) accepts a publicly trusted certificate ([Full
  strict][cfl-strict]), so it serves both edges during the migration.

Let's Encrypt in Caddy was rejected: TLS-ALPN-01 cannot pass CloudFront, and
HTTP-01 or DNS-01 needs ACME storage shared by both Caddy tasks and kept across
deploys, or several deploys a day hit the limit of five certificates per name
set per week ([rate limits][le-limits]); DNS-01 also puts a DNS credential on
the host. The viewer certificate stays separate so its key never leaves ACM.

## Client address

`CloudFront-Viewer-Address` carries the viewer address and source port
([headers][cf-headers]); CloudFront adds it when the origin request policy lists
it ([API][cf-orp-api]). For IPv6 the address comes without brackets, as in
`2001:db8:0:0:0:0:0:1:60776` ([nginx ticket][nginx-2524]; **Verify**), which
Caddy's `client_ip_headers` cannot parse reliably. The CloudFront listener
therefore sets no `trusted_proxies` and applies this rule:

1. Remove `X-Real-IP`, `X-Forwarded-For`, `Forwarded`, `X-Forwarded-Host`, and
   `CF-Connecting-IP`. CloudFront already removes viewer `X-Real-IP` and
   `X-Forwarded-Proto` ([custom origins][cf-custom]).
2. Accept `CloudFront-Viewer-Address` only with exactly one value matching
   `^([0-9A-Fa-f:.]+):([0-9]{1,5})$`; the address is everything before the last
   colon, since the port is always present. Remove the header.
3. Send the address as the single `X-Real-IP`. Missing, repeated, or malformed
   input sends none, and Go fails closed as today.

Go is unchanged: it trusts `X-Real-IP` only from loopback and parses it with
`netip`. A spoofed value needs a request that reaches 8443 without our
distribution, which the security group and mTLS block. **Verify:** a
viewer-supplied `CloudFront-Viewer-Address` is replaced, not appended; step 2
rejects a duplicate either way.

[acm-dns]: https://docs.aws.amazon.com/acm/latest/userguide/dns-validation.html
[acm-events]:
  https://docs.aws.amazon.com/acm/latest/userguide/supported-events.html
[acm-export]:
  https://docs.aws.amazon.com/acm/latest/userguide/acm-exportable-certificates.html
[acm-price]: https://aws.amazon.com/certificate-manager/pricing/
[cf-custom]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/RequestAndResponseBehaviorCustomOrigin.html
[cf-headers]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/adding-cloudfront-headers.html
[cf-mtls]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/origin-mtls-authentication.html
[cf-mtls-cert]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/origin-certificate-management-certificate-manager.html
[cf-mtls-enable]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/origin-enable-mtls-distributions.html
[cf-orp-api]:
  https://docs.aws.amazon.com/cloudfront/latest/APIReference/API_OriginRequestPolicyHeadersConfig.html
[cf-origin]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/DownloadDistValuesOrigin.html
[cf-origin-tls]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-https-cloudfront-to-custom-origin.html
[cf-prefix]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/LocationsOfEdgeServers.html
[cf-vpc]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/private-content-vpc-origins.html
[cfl-strict]:
  https://developers.cloudflare.com/ssl/origin-configuration/ssl-modes/full-strict/
[le-limits]: https://letsencrypt.org/docs/rate-limits/
[nginx-2524]: https://trac.nginx.org/nginx/ticket/2524
[ssm-tiers]:
  https://docs.aws.amazon.com/systems-manager/latest/userguide/parameter-store-advanced-parameters.html
[vpc-pl-weight]:
  https://docs.aws.amazon.com/vpc/latest/userguide/working-with-aws-managed-prefix-lists.html#aws-managed-prefix-list-weights
[vpc-price]: https://aws.amazon.com/vpc/pricing/
