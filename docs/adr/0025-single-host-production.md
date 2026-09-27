# 0025: Single-host AWS production behind CloudFront and Route 53

Status: Accepted (2026-09-05 to 2026-09-26), by the human owner's direction. The
owner approved every choice in the
[CloudFront edge design](../design/cloudfront-edge.md).

## Context

The product is a personal project with few users. A hosted UAT environment, a
load balancer, a second infrastructure repository, and a cross-repository image
handoff add work and monthly cost before anyone can use the site, and they
protect users who do not exist yet.

The first edge, Cloudflare's free proxy, showed intermittent 522 and 525 errors
and multi-second first bytes from locations far from Singapore, caused by the
public internet path to the AWS Singapore origin. CloudFront fetches from AWS
origins over the AWS network and has edges in Vietnam. With DNS at Cloudflare,
the flattened apex CNAME made CloudFront pick an edge near Cloudflare's resolver
(Singapore or Marseille from Vietnam), while the plain `www` CNAME landed in
Hanoi. A Route 53 alias queried with a Vietnamese client subnet answered with
`HAN50`; the AWS documentation does not state this behavior, so the measurement
is the evidence.

## Decision

### Environment and host

- Production at `https://aboutme.vn` is tested in production. There is no hosted
  UAT environment until the product has about 500 users.
- Production runs in `ap-southeast-1` on one `t4g.small` EC2 instance with the
  Bottlerocket ECS image, one `db.t4g.micro` RDS PostgreSQL instance, and the
  private S3 media bucket. There is no load balancer. One serving replica runs
  (ADR 0026).
- OpenTofu is the infrastructure tool, and its modules live in this public
  repository under `deploy/aws/`. Prefer managed AWS services when they meet the
  workload and cost requirements, weighing operating effort, limits, and
  compatibility, never price alone. Global-service dependencies, such as
  certificates in `us-east-1`, are named in the resource inventory and cost
  model; they do not relocate application data.
- State, account identifiers, and environment values stay out of Git. The owner
  applies infrastructure and runs deploys from the laptop. GitHub holds no cloud
  credentials, and infrastructure code contains no account identifier, secret,
  or private hostname.
- Cost reductions never silently weaken authentication, private media,
  revocation, backup, or recovery requirements.

### Images

Release images build and smoke-test on public GitHub Actions `ubuntu-24.04-arm`
runners for `linux/arm64`, from the exact reviewed commit, and publish to public
GitHub Container Registry packages with signed provenance. Public build archives
and logs contain only public inputs and synthetic evidence. The host pulls
images by digest.

### Edge

- One Amazon CloudFront distribution, pay as you go, fronts the host.
- The origin is the host's Elastic IP. A security group admits only the
  CloudFront origin-facing prefix list, on a dedicated port 8443. CloudFront
  authenticates with origin mTLS using a client certificate from our own CA, and
  Caddy requires it. A secret origin header is only the fallback. VPC origins
  are not used: they need a private subnet, hence a NAT gateway, and exclude
  origin mTLS.
- The origin serves an ACM exportable public certificate for `aboutme.vn` and
  `www.aboutme.vn`, exported to SSM on each renewal. Viewers get a separate,
  non-exportable ACM certificate.
- CloudFront forwards the viewer `Host`, `Authorization`, cookies, and every
  other viewer header, caches only `/_nuxt/*`, and passes everything else
  through uncached.
- Caddy runs one listener per edge. The CloudFront listener derives the client
  address from `CloudFront-Viewer-Address` and sends Go one `X-Real-IP`. Caddy,
  not the edge, adds HSTS and `nosniff` and removes `Server`.
- AWS WAF with a rate-based rule and two managed groups provides
  application-layer protection, blocking since 2026-09-26.

### DNS

- A public Route 53 hosted zone for `aboutme.vn`, built by OpenTofu in
  `deploy/aws/modules/dns`, serves every record. The apex and `www` are alias A,
  AAAA, and HTTPS records to the distribution. Mail and certificate validation
  records keep their values.
- Route 53 signs the zone with DNSSEC. The key-signing key (key tag 2015) uses a
  KMS key in `us-east-1`. Two CloudWatch alarms mail the owner when Route 53
  reports a signing problem.
- The move from Cloudflare follows the [DNS runbook](../runbooks/dns.md): remove
  Cloudflare's DS record, change name servers after the DS TTL, add the Route 53
  DS record after Cloudflare's NS TTL, then remove the Cloudflare zone.

## Consequences

- Deploys and monthly OS updates interrupt service for a few minutes. A host
  failure takes the site down until EC2 auto-recovery or a manual replacement.
- Test accounts and real accounts share one database.
- AWS, the hosting processor, terminates TLS at edges and answers DNS. Once the
  Cloudflare zone is removed, Cloudflare processes no aboutme data, and the
  privacy notice names only AWS in both languages.
- Cost is about USD 45 to 55 a month for the host, plus about USD 11 for the WAF
  and exportable origin certificate and about USD 1.50 for the hosted zone and
  KMS key. CloudFront stays inside its free tier at current traffic, and alias
  queries to CloudFront are free.
- The origin certificate renews every 198 days and must be exported and deployed
  each time. A deploy refuses to run when it has fewer than 21 days left.
- The release fence rose at cutover to the first release whose Caddy has the
  CloudFront listener, because an older release would take the site down.
- Deploy checks treat CloudFront 502 and 504 as the origin-down signals.
- A fault in the DNSSEC KMS key or its policy can make validating resolvers fail
  the whole domain. Rollback before the new DS is a name-server change back to
  Cloudflare; after it, the DS comes out first.
- The [single-host production design](../design/single-host-production.md)
  states the resulting rules. ADR 0027 later moves production to Vietnam, which
  replaces the host, CloudFront, and Route 53.

## History

- Former ADR 0031 (2026-09-05): AWS cost research before hosted UAT; OpenTofu,
  managed services, Singapore, and a private `aboutme-infra` repository. Its
  hosted UAT, private repository, and SES handoff were replaced by 0037.
- Former ADR 0033 (2026-09-06): public ARM64 image builds with private ECR
  publication. Its public builds stand; private publication was replaced by
  public GHCR in 0037.
- Former ADR 0034 (2026-09-06): scheduled UAT and an ALB with one to two
  autoscaled replicas. Replaced by 0036 and 0037; its Singapore, RDS, private
  S3, and EC2 Graviton choices stand.
- Former ADR 0037 (2026-09-16): single-host production without hosted UAT,
  behind Cloudflare. This record is the production approval. Its Cloudflare edge
  was replaced by 0054.
- Former ADR 0054 (2026-09-25): CloudFront edge with origin mTLS. Its "DNS stays
  at Cloudflare" clause was replaced by 0056.
- Former ADR 0056 (2026-09-26): Route 53 DNS with DNSSEC, so CloudFront picks
  edges near the viewer.
