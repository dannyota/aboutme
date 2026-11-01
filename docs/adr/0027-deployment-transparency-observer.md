# 0027: Deployment transparency through an independent observer

Status: Accepted (2026-09-27). The owner approved the choices marked **Owner
approval** (A1 to A5) in the
[deployment transparency design](../design/deployment-transparency/README.md).

## Decision

Production on one GreenNode vServer uses a root host timer to inspect running
Podman containers. It writes an unchecked observation to a root-owned directory.
An observer container without Podman socket access checks the running digests
with the existing `sigstore-go` verifier and atomically publishes the deployment
document to a directory Caddy mounts read-only and serves through the direct
edge. The [host design](../design/deployment-transparency/host.md) owns the
source, publication, headers, and failure rules. The document identifies Podman
in its version 1 platform field.

The observer rejects observations older than 180 seconds or more than 60 seconds
in the future. Its verification cache stays in memory; no bucket or secret is
needed. It runs independently of application maintenance with a read-only root
filesystem and bounded memory. Deploy verifies the native amd64 observer image's
provenance before use. That image uses the `<tag>-amd64` tag; a release without
it leaves the installed observer unchanged. The plain version tag remains an
arm64 image for Lambda.

The bilingual `/verify` page, linked from the landing footer, shows the chain
from source to running service and gives an independent verification command. It
shows Verified only for fresh, checked evidence. A stale or unchecked report is
never verified. The host reports itself, and an observer on that host cannot
prove host or operator honesty or runtime integrity. Deploy-time checks are not
runtime verification.

ECS and Kubernetes retain an observer separate from the application with
read-only platform access. It publishes `/.well-known/deployment.json` with
every running digest per serving component and replica count, plus version,
commit, signature and SBOM status, and links from signed provenance. On AWS, a
Lambda function runs every minute with permission only to list and describe ECS
tasks, write one S3 object, and write logs; CloudFront caches the object for 30
seconds. On Kubernetes the image runs in its own namespace with a Role limited
to `get` and `list` on pods. Those documents use an allowlist and a closed JSON
Schema; a test rejects infrastructure identifiers.

`release-images.yml` publishes a signed SBOM attestation per image. No separate
cosign signature is added. Signature policy and independent commands are shared
across platforms.

## Context

aboutme is open source, and its users should be able to see exactly what runs in
production. Release images are built in public CI and carry Sigstore-signed
build provenance bound to their digests, and `deploy.sh` deploys by digest. A
release tag alone cannot tell a user which digests run: tags can move.

Evidence is weak if the application reports its own version, because a
compromised application can report anything. It is stronger if a separate
program with read-only access asks the platform what runs and checks each digest
against the signed build record.

## Consequences

- ADR 0010 is unaffected: the deployment document holds no publication state.
- The AWS ECS alternative uses a Lambda function, ECR repository, S3 bucket,
  schedule, and alarm, for under USD 0.50 a month. Its deploy role can update
  the observer; CloudFront caches the document alongside `/_nuxt/*`
  ([historical AWS production decision](history/aws-production.md)).
- ECS evidence stops at the platform's report. The ECS agent on the host reports
  digests, and host-network containers can reach the instance role, so a
  compromised host or `app` task could forge them.
- The owner's administrator login can change the observer. The design defends
  against a compromised application and silent drift, not against the operator.
- `verify` is a reserved slug in the public-root registry.

## Alternatives

- **Application self-report** (a version endpoint in Go): rejected as weaker
  evidence.
- **Sidecar or scheduled task on the AWS host:** rejected; credentials would be
  reachable from the `app` task, and it shares the observed failure domain.
- **Fargate:** rejected on cost and a public IPv4 address per run.
- **Signature check in the browser:** rejected; a verifier served by aboutme
  proves nothing a compromised aboutme could not fake.

## History

2026-10-11: Added host reporting for the Vietnam vServer, with its weaker trust
claim and unchanged attestation verification policy.

2026-10-11: Added a host observer container to verify runtime digests with the
existing verifier while keeping Podman access in the root reporter.

2026-11-01: Consolidated the host reporting and verification amendments to
reflect Vietnam production under ADR 0027, preserving the observer's trust
limits.

Originally proposed as ADR 0057.
