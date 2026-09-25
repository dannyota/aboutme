# CloudFront edge migration

Status: open (owner decision 2026-09-25: move if as secure as today). Moves the production edge from Cloudflare to CloudFront on the same host; interim until the Vietnam move. Design: [cloudfront-edge.md](../design/cloudfront-edge.md). Decision: [ADR 0054](../adr/0054-cloudfront-edge-for-single-host-production.md). Design wins over this plan.

Code, comments, tests, and living docs cite the design or ADR 0054, never this plan or its slice letters. One feature per release per [How we work](../../AGENTS.md#how-we-work); the manager assigns version numbers.

## Owner approvals (recommendation first)

- DNS stays at Cloudflare, DNS-only, apex flattened CNAME. Recommend yes; Route 53 only if the latency check shows distant edges.
- AWS WAF: rate rule per IP, IP reputation, known bad inputs; count one week, then block; no core rule set. Recommend yes (about USD 8/month plus USD 0.60/M requests); without it L7 protection is weaker than today.
- Pre-cutover tests pin the real hostnames to CloudFront instead of a temporary hostname. Recommend yes; a temporary hostname fails CSRF origin, cookie, and passkey checks.
- Raise the release fence at cutover to the first release with the CloudFront Caddy listener. Recommend yes; older Caddy images leave CloudFront with no origin on rollback.
- Privacy notice text (vi and en) naming CloudFront and Cloudflare DNS only. Recommend the design's wording.
- CAA `0 issue "amazon.com"` after Cloudflare leaves the path. Recommend yes, not earlier.

## Owner actions

1. Approve or change each item above.
2. Add the two ACM DNS validation CNAMEs (apex, `www`) in Cloudflare when devops supplies them; they serve both certificates.
3. At cutover, approve the DNS switch at Cloudflare; after the soak, approve Cloudflare removal.

## Slices

|Slice|Owner role|Done when|
|-|-|-|
|A Origin certificate|devops|ACM exportable cert (ap-southeast-1, apex and www, ECDSA P-256) issued; `tls.sh` export mode writes key, leaf, chain to SSM without printing; EventBridge rule on `ACM Certificate Available` RENEWAL to SNS; `deploy.sh` refuses when the deployed origin cert has < 21 days; deployed; Cloudflare Full (strict) still green; adversarial review before merge.|
|B Caddy edge listeners|devops|`EDGES` selects cloudflare (443), cloudfront (8443), or both in Caddyfile and Caddyfile.maintenance; 8443 requires the CloudFront client CA; strips forwarding headers; maps one valid `CloudFront-Viewer-Address` to one `X-Real-IP`, none otherwise; HSTS, nosniff if absent, `Server` removed; Caddy tests cover forged headers per listener, IPv4, unbracketed IPv6, duplicate, missing, malformed; released with `EDGES=cloudflare` behavior unchanged; adversarial review.|
|C Build the edge|devops|`tls.sh` CloudFront client-CA mode (CA cert to SSM, client cert to ACM us-east-1); OpenTofu: viewer cert us-east-1, SG on 8443 from the origin-facing prefix list attached in place, custom cache and origin request policies, distribution per design, web ACL if approved, EventBridge on the client cert's approaching expiry, task defs with `EDGES=cloudflare,cloudfront` and the trust pool; `deploy.sh` direct-origin check on 443 and 8443, 502/504 retries, distribution preflight; tofu plan shows no destroy or replace; adversarial review before apply.|
|D Pre-cutover proof|qa, devops|Design facts 1 to 9 recorded; design test plan 1 to 8 pass with pinned hostnames; evidence in `.dev/prod-checks/cloudfront/`.|
|E Privacy notice|frontend|`apps/web/app/i18n/legal.ts` vi and en per approved text; tests updated; first deployed at cutover.|
|F Cutover|manager, devops, owner|Fence raised; E deployed; apex and www switched to DNS-only CNAMEs at Cloudflare, TTL 60; test plan 2, 6, 7, 9 pass on public DNS; `Via` check enabled in `deploy.sh`; a normal deploy passes test 8.|
|G Soak|devops, qa|Seven days with no unexplained 5xx, site-down alarm quiet, WAF counts reviewed and rules switched to block.|
|H Remove Cloudflare|devops, architect|`EDGES=cloudfront`; Cloudflare SG rules, `CLOUDFLARE_RANGES`, the `http` data source, range check, and origin-pull CA parameter removed; zone origin-pull cert removed and Origin CA cert revoked; CAA if approved; living docs updated in the same change (single-host-production, deployment, security, system, design README diagram, budgets SSE row, architecture, production runbook, vietnam-production "Today" column); adversarial review.|

## Rollback

Before H: restore the proxied apex `A` and `www` CNAME at Cloudflare. After H: revert H, apply, deploy, then restore the records.

## Gates

Every slice touching `deploy/`, OpenTofu, IAM, DNS, or TLS gets a reviewer's adversarial pass before merge. Tag and deploy only after green GitHub CI on the exact commit. Delete this plan when H ships.
