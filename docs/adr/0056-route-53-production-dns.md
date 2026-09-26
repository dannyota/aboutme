# 0056: Route 53 serves production DNS

Status: Accepted (2026-09-26), by the owner's decision. This record supersedes
one decision of [ADR 0054](0054-cloudfront-edge-for-single-host-production.md):
"DNS stays at Cloudflare, DNS-only". The rest of ADR 0054 stands.

## Context

ADR 0054 kept DNS at Cloudflare. The apex became a CNAME to the CloudFront
distribution, which Cloudflare flattens, and the design left one fact to verify:
whether Vietnamese viewers still reach a nearby edge.

They do not. Cloudflare resolves the flattened apex itself, so CloudFront picks
an edge near Cloudflare's resolver, not near the viewer. From Vietnam, the apex
landed on Singapore through `1.1.1.1` and on Marseille (`MRS52`, first byte 1.0
s) through `8.8.8.8`. `www`, a plain CNAME, landed on Hanoi.

A Route 53 alias record answers with the distribution's addresses for the client
subnet the resolver sends, or for the resolver's own location.
`deploy/aws/scripts/dns-check.sh` queried the Route 53 zone with a Vietnamese
client subnet and got `HAN50`. The AWS documentation does not state this
behavior for aliases, so the measurement is the evidence.

## Decision

- A public Route 53 hosted zone for `aboutme.vn`, built by OpenTofu in
  `deploy/aws/modules/dns`, serves every record. The apex and `www` are alias A,
  AAAA, and HTTPS records to the distribution. Mail and certificate validation
  records keep their values.
- Route 53 signs the zone with DNSSEC. The key-signing key (key tag 2015) uses a
  KMS key in `us-east-1`.
- The move follows the [DNS runbook](../runbooks/dns.md). The owner removed
  Cloudflare's DS record at the `.vn` registrar on 2026-09-26, changes the name
  servers to Route 53 after 12 hours (the DS TTL), and adds the Route 53 DS
  record after 24 more hours (Cloudflare's NS TTL).
- Cloudflare keeps answering until resolvers have left it. Then the zone is
  removed from Cloudflare.

## Consequences

- Viewers in Vietnam reach an edge in Vietnam on the apex, as they already do on
  `www`.
- Between the two DS changes, about 36 hours, validating resolvers treat the
  zone as unsigned, so answers lack DNSSEC protection. Removing the old DS first
  keeps validating resolvers from failing the domain during the name server
  change.
- A fault in the KMS key or its policy can make validating resolvers fail the
  whole domain. Two CloudWatch alarms mail the owner when Route 53 reports a
  signing problem.
- Cost rises by about USD 1.50 a month: USD 0.50 for the zone and USD 1 for the
  KMS key, plus per-query and per-signing charges. Alias queries to CloudFront
  are free.
- Once the Cloudflare zone is removed, Cloudflare processes no aboutme data.
  AWS, already the hosting processor, answers DNS. The privacy notice says
  "Cloudflare only provides DNS", so it changes in both languages at that point.
  **Owner approval:** the exact text.
- ADR 0054 kept Cloudflare so that the domain would change name servers once, at
  the [ADR 0051](0051-vietnam-hosted-production.md) cutover. That cutover now
  moves the name servers and the DS record from Route 53 to vDNS, a second move.
  Nearby edges for every viewer are worth it.
- Rollback before the new DS is a name server change back to Cloudflare, as the
  runbook's rollback section states. After the new DS, the DS comes out first.
