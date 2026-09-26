# 0057: Deployment transparency through an independent observer

Status: Proposed (2026-09-26). The choices marked **Owner approval** in the
[deployment transparency design](../design/deployment-transparency/README.md)
stay open until the owner approves them.

## Context

aboutme is open source, and its users should be able to see exactly what runs in
production. Release images are already built in public CI and carry
Sigstore-signed build provenance bound to their digests, and `deploy.sh` deploys
by digest. Nothing yet tells a user which digests are running, and a tag is not
evidence: it can move.

Evidence is weak if the application reports its own version, because a
compromised application can report anything. It is stronger if a separate
program with read-only access asks the platform what runs and checks each digest
against the signed build record.

## Decision

- An observer, separate from the application and read-only on the platform,
  publishes `https://aboutme.vn/.well-known/deployment.json`: every running
  digest per serving component with its replica count, and the version, commit,
  signature status, SBOM status, and links taken from the signed provenance for
  that digest.
- On AWS the observer is a Lambda function running the observer container image,
  started every minute, with a role that may call only `ecs:ListTasks` and
  `ecs:DescribeTasks` on the production cluster, write one S3 object, and write
  its logs. CloudFront serves the object from S3 and caches it for 30 seconds.
  On Kubernetes the same image runs in its own namespace with a Role limited to
  `get` and `list` on pods.
- The document is built from an allowlist and validated against a closed JSON
  Schema before it is written; a test fails if any infrastructure identifier
  reaches it.
- `release-images.yml` adds a signed SBOM attestation per image. No separate
  cosign signature is added.
- A bilingual page at `/verify`, linked from the landing footer, shows the chain
  from source to running service, never shows "verified" for stale or unchecked
  data, and gives the command to verify independently.

## Consequences

- CloudFront caches one more path. This amends the cache rule of
  [ADR 0054](0054-cloudfront-edge-for-single-host-production.md), which cached
  only `/_nuxt/*`. [ADR 0022](0022-public-artifact-revocation.md) is unaffected:
  the document holds no publication state.
- Production gains a Lambda function, an ECR repository, an S3 bucket, a
  schedule, and an alarm, for under USD 0.50 a month. The deploy role can update
  the observer.
- The evidence stops at the platform's report. The ECS agent on the host reports
  digests, and host-network containers can reach the instance role that makes
  those reports, so a compromised host or `app` task could forge them. The page
  states that it does not prove host honesty.
- The owner's administrator login can still change the observer. The design
  defends against a compromised application and silent drift, not against the
  operator.
- `verify` becomes a reserved slug in public-root registry version 9.
- The Vietnam design's Podman host has no read-only platform API; the observer
  there needs its own design before the move.

## Alternatives

- **Application self-report** (a version endpoint in Go): rejected as weaker
  evidence.
- **Sidecar or scheduled task on the host:** rejected; credentials would be
  reachable from the `app` task, and it shares the observed failure domain.
- **Fargate:** rejected on cost and a public IPv4 address per run.
- **Signature check in the browser:** rejected; a verifier served by aboutme
  proves nothing a compromised aboutme could not fake.
