# 0054: CloudFront edge for single-host production

Status: Accepted (2026-09-25) for the move, by the human owner's direction, on
condition that the result is as secure as today. The choices marked for owner
approval in the [CloudFront edge design](../design/cloudfront-edge.md) stay open
until the owner approves them.

## Context

[ADR 0037](0037-single-host-production-without-hosted-uat.md) put production
behind Cloudflare's free plan, proxied, with no CloudFront distribution and no
load balancer. Measurements on 2026-09-25 show intermittent 522 and 525 errors
and first-byte times of seconds only from Cloudflare locations far from
Singapore, during Asian daytime. The public internet path from those locations
to the AWS Singapore origin is the cause. Cloudflare Argo would route around it
for a monthly fee and keep Cloudflare as a processor. CloudFront fetches from
AWS origins over the AWS network and has edges in Vietnam.

[ADR 0051](0051-vietnam-hosted-production.md) moves production to Vietnamese
providers later. Any edge chosen now is interim.

## Decision

Production uses one Amazon CloudFront distribution, pay as you go, in front of
the existing EC2 host. There is still no load balancer.

- The origin stays the host's Elastic IP. A security group admits only the
  CloudFront origin-facing prefix list, on a dedicated port 8443. CloudFront
  authenticates with origin mTLS, using a client certificate from our own CA,
  and Caddy requires it. A secret origin header is only the fallback. VPC
  origins are not used: they need a private subnet, hence a NAT gateway, and
  they exclude origin mTLS.
- The origin serves an ACM exportable public certificate for `aboutme.vn` and
  `www.aboutme.vn`, exported to SSM on each renewal. Viewers get a separate,
  non-exportable ACM certificate.
- CloudFront forwards the viewer `Host`, `Authorization`, cookies, and every
  other viewer header, caches only `/_nuxt/*`, and passes everything else
  through uncached.
- Caddy runs one listener per edge. The CloudFront listener derives the client
  address from `CloudFront-Viewer-Address` and sends Go one `X-Real-IP`, as
  today. Go does not change.
- Caddy, not the edge, adds HSTS and `nosniff` and removes `Server`.
- DNS stays at Cloudflare, DNS-only, subject to owner approval.
- AWS WAF with a rate-based rule and two managed groups replaces Cloudflare's
  application-layer protection, subject to owner approval.

The design states the settings, the migration, the rollback, and the tests.

## Consequences

- Cloudflare no longer sees page traffic, IP addresses, or decrypted content.
  AWS, already the hosting processor, terminates TLS at edges worldwide. The
  privacy notice changes at cutover.
- Monthly cost rises by about USD 11 with the WAF (USD 8) and the exportable
  origin certificate (about USD 3), or about USD 3 without the WAF. CloudFront
  stays inside its free tier at current traffic.
- The origin certificate renews every 198 days and must be exported and deployed
  each time. A deploy refuses to run when it has fewer than 21 days left.
- A rollback to a release whose Caddy lacks the CloudFront listener would take
  the site down, so the release fence rises to that release at cutover.
- Deploy checks treat CloudFront 502 and 504 as the origin-down signals in place
  of Cloudflare's 521, 522, and 525.

## Compatibility and review

This record supersedes, until the ADR 0051 cutover, the Cloudflare edge parts of
ADR 0037: Cloudflare as the production proxy, the Cloudflare Origin CA
certificate, Authenticated Origin Pulls, and the security group built from
Cloudflare's ranges. ADR 0037's single host, production without hosted UAT,
laptop-run deploys, and public infrastructure code stand.

ADR 0051 still governs the move to Vietnam. Its cutover replaces this
distribution with vCDN and removes CloudFront with the rest of AWS production.
Keeping DNS at Cloudflare leaves its cutover steps unchanged.

[ADR 0022](0022-public-artifact-revocation.md) stands. The default behavior does
not cache at all, which is stricter than its 60-second revalidated maximum.
Public HTTP, SSE, and MCP contracts, the client address contract between Caddy
and Go, and every data invariant are unchanged.
