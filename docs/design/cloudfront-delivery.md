# CloudFront delivery

This page is part of the
[undeployed CloudFront edge reference](cloudfront-edge.md).

## Viewer TLS, hostnames, and DNS

- A free ACM certificate in `us-east-1` ([certificates][cf-cert]) covers the
  aliases `aboutme.vn` and `www.aboutme.vn`. HTTP redirects to HTTPS; security
  policy `TLSv1.2_2021`; HTTP/2 and HTTP/3; IPv6 on, matching today's `AAAA`
  answers; price class All.
- The viewer `Host` is forwarded, so Caddy sees the same hosts as behind
  Cloudflare, and `www` keeps redirecting at the origin.
- **DNS is Route 53** ([ADR 0025](../adr/0025-single-host-production.md)). The
  apex and `www` are alias A, AAAA, and HTTPS records to the distribution in a
  DNSSEC-signed zone ([DNS runbook](../runbooks/dns.md)). An alias answer
  follows the resolver's client subnet: a Vietnamese subnet got `HAN50`.
  Cloudflare's flattened apex CNAME ([flattening][cfl-flatten]) got edges near
  Cloudflare's resolver instead: Singapore through `1.1.1.1` and Marseille
  (first byte 1.0 s) through `8.8.8.8`. Alias queries to CloudFront are free
  ([Route 53 pricing][r53-price]).

## Cache and forwarding

ADR 0010 stands: edge storage is never publication authority. CloudFront caches
only hashed assets and the deployment document, and passes the rest uncached,
stricter than the 60-second revalidated maximum ADR 0010 allows.

| Behavior                       | Cache policy                                                   | Origin request policy                                                                    | Methods   |
| ------------------------------ | -------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | --------- |
| `/_nuxt/*`                     | Custom: TTL 0 / 0 / 1 year, key = `Host` and all query strings | None                                                                                     | GET, HEAD |
| `/.well-known/deployment.json` | Custom: TTL 0 / 30 / 30, nothing in the key                    | None; the transparency bucket through origin access control                              | GET, HEAD |
| Default (`*`)                  | Managed `CachingDisabled` ([cache policies][cf-cache])         | Custom: all viewer headers, cookies, and query strings, plus `CloudFront-Viewer-Address` | All seven |

- The `/_nuxt/*` key includes query strings because the renderer stylesheets are
  fixed names versioned by `?v=`, which the managed `CachingOptimized` policy
  would drop. The origin's `Cache-Control` decides what stays; `Host` in the key
  keeps a `www` redirect from being served for the apex.
- The deployment document comes from the private transparency bucket, with the
  headers policy in its [design](deployment-transparency/README.md).
- The default origin request policy uses header behavior
  `allViewerAndWhitelistCloudFront` ([API][cf-orp-api]), so `Authorization`
  reaches the origin on every method, as MCP and bearer calls need; CloudFront
  otherwise strips it from GET and HEAD ([forwarding][cf-auth]). Cookies,
  `Origin`, `User-Agent`, and `If-None-Match` pass as behind Cloudflare.
- Error caching minimum TTL is 0 for 400, 403, 404, 405, 414, 416, and 500 to
  504, so a maintenance 503, or a 404 for a hash the old release lacks, is never
  replayed.
- Origin: HTTPS only, TLS 1.2 minimum, port 8443, IPv4, connection timeout 10
  seconds with 3 attempts, read timeout 60 seconds, keep-alive 60 seconds, and
  no response completion timeout, so a stream has no total cap ([origin
  settings][cf-origin]). The read timeout bounds the gap between packets; SSE
  heartbeats every 25 seconds stay inside it, and a render ends within 20
  seconds ([budgets](budgets.md)). MCP is stateless with JSON responses, so SSE
  is the only long response.
- Limits: request body 64 GB, request line and headers 32,768 bytes, URL 8,192
  bytes ([quotas][cf-quotas]), so 2 MiB photos and 4 MiB `/mcp` bodies fit. A
  GET with a body gets 403. CloudFront speaks HTTP/1.1 to the origin and streams
  chunked responses as they arrive ([custom origins][cf-custom]).

## Response headers

Origin headers pass through: both CSPs, `Clear-Site-Data`, `__Host-` cookies,
`ETag`, `Cache-Control`, and `Retry-After`. CloudFront sets its own `Via` and
adds `X-Cache`, `X-Amz-Cf-Pop`, and `X-Amz-Cf-Id` ([custom origins][cf-custom]).
Caddy sets `Strict-Transport-Security: max-age=31536000` (no subdomains, no
preload) and `X-Content-Type-Options: nosniff` when the upstream did not, and
removes `Server` and its own `Via: 1.1 Caddy`, so no response names the origin
software and the rule survives the move to vCDN.

## DDoS and WAF

Shield Standard covers network and transport layer attacks on CloudFront at no
charge ([Shield][shield]), but not the application layer (L7). Cloudflare's free
plan has unmetered L7 DDoS protection ([DDoS][cfl-ddos]) and the Free Managed
Ruleset ([rules][cfl-waf]), so parity needs AWS WAF.

**Owner approval:** launch with one web ACL holding a rate-based rule per IP,
the Amazon IP reputation list, and known bad inputs: USD 5 per web ACL plus USD
1 per rule or managed group, USD 8 a month, plus USD 0.60 per million requests
([WAF pricing][waf-price]). Rules count for a week, then block; the rate limit
starts at 2,000 requests per five minutes. The core rule set is left out: it
blocks bodies over 8 KB, which photo uploads, autosaves, and `/mcp` calls
exceed, and its cross-site scripting body rule would inspect rich-text resume
bodies ([baseline groups][waf-crs]). WAF logging stays off. Viewer analytics
adds label-only rules on one path
([counting](viewer-analytics/counting.md#layers-2-and-3-edge-labels)).

## Deploys and monitoring

- The `SO_REUSEPORT` handoff is unchanged. The maintenance Caddyfile carries the
  same listeners, trust, and client address rule.
- CloudFront returns 502 on a bad origin certificate, chain, or mTLS handshake
  and 504 when the origin does not answer in time ([origin TLS][cf-origin-tls],
  [origin settings][cf-origin]); they replace 521, 522, and 525. Its own errors
  carry `X-Cache: Error from cloudfront`. It retries a stalled GET or HEAD but
  not a POST, PUT, PATCH, or DELETE, so a write on a connection the outgoing
  Caddy is closing can still fail, as behind Cloudflare.
- `deploy.sh`: smoke, warm-up, and the maintenance check keep using
  `https://aboutme.vn` and, after cutover, require `Via` to name CloudFront. The
  direct-origin check covers 443 and 8443. Retries treat 502 and 504 as
  transient. The Cloudflare range check goes with Cloudflare. A preflight checks
  the distribution's origin domain and at least 21 days on the origin
  certificate.
- The Route 53 health check on `/readyz` now passes through CloudFront and fails
  on 502 or 504 as on any 5xx today. An EventBridge rule on
  `ACM Certificate Approaching Expiration` for the imported client certificate
  emails the owner 45 days ahead ([events][acm-events]).
- **Owner approval:** once CloudFront serves production, raise the release fence
  to the first release whose Caddy has the CloudFront listener, because
  `--rollback` redeploys the older Caddy image, which has no 8443 listener.

## Privacy

Cloudflare handles no page traffic, IP addresses, or decrypted content, and
after the DNS move it answers no queries either. AWS, already the hosting
processor, terminates TLS at CloudFront edges worldwide, including in Vietnam,
so data still leaves Vietnam until the ADR 0027 move. CloudFront access logs and
WAF logs stay off, as the privacy policy promises no IP address in request logs.

The notice in `apps/web/app/i18n/legal.ts` changes in both languages at cutover:
CloudFront (Amazon Web Services, global edge network) delivers the site and
processes IP addresses; public pages are not stored at the edge; Cloudflare only
provides DNS; transfer abroad goes mainly to Singapore and in part to Google.
When the Cloudflare zone is removed, the Cloudflare DNS sentence goes (**Owner
approval:** the exact text).

[acm-events]:
  https://docs.aws.amazon.com/acm/latest/userguide/supported-events.html
[cf-auth]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/add-origin-custom-headers.html#add-origin-custom-headers-forward-authorization
[cf-cache]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-managed-cache-policies.html
[cf-cert]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/cnames-and-https-requirements.html
[cf-custom]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/RequestAndResponseBehaviorCustomOrigin.html
[cf-orp-api]:
  https://docs.aws.amazon.com/cloudfront/latest/APIReference/API_OriginRequestPolicyHeadersConfig.html
[cf-origin]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/DownloadDistValuesOrigin.html
[cf-origin-tls]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-https-cloudfront-to-custom-origin.html
[cf-quotas]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/cloudfront-limits.html
[cfl-ddos]: https://developers.cloudflare.com/ddos-protection/
[cfl-flatten]:
  https://developers.cloudflare.com/dns/cname-flattening/set-up-cname-flattening/
[cfl-waf]: https://developers.cloudflare.com/waf/managed-rules/
[r53-price]: https://aws.amazon.com/route53/pricing/
[shield]:
  https://docs.aws.amazon.com/waf/latest/developerguide/ddos-standard-summary.html
[waf-crs]:
  https://docs.aws.amazon.com/waf/latest/developerguide/aws-managed-rule-groups-baseline.html
[waf-price]: https://aws.amazon.com/waf/pricing/
