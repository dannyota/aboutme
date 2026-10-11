# 0028: Deployment transparency through an independent observer

Status: Accepted (2026-09-27). The owner approved the choices marked **Owner
approval** (A1 to A5) in the
[deployment transparency design](../design/deployment-transparency/README.md).

## Context

aboutme is open source, and its users should be able to see exactly what runs in
production. Release images are built in public CI and carry Sigstore-signed
build provenance bound to their digests, and `deploy.sh` deploys by digest. A
release tag alone cannot tell a user which digests run: tags can move.

Evidence is weak if the application reports its own version, because a
compromised application can report anything. It is stronger if a separate
program with read-only access asks the platform what runs and checks each digest
against the signed build record.

## Decision

- On ECS and Kubernetes, an observer separate from the application and read-only
  on the platform publishes `https://aboutme.vn/.well-known/deployment.json`:
  every running digest per serving component with its replica count, and the
  version, commit, signature status, SBOM status, and links taken from the
  signed provenance for that digest.
- On AWS the observer is a Lambda function running the observer container image,
  started every minute, with a role that may call only `ecs:ListTasks` and
  `ecs:DescribeTasks` on the production cluster, write one S3 object, and write
  its logs. CloudFront serves the object from S3 and caches it for 30 seconds.
  On Kubernetes the same image runs in its own namespace with a Role limited to
  `get` and `list` on pods.
- The ECS and Kubernetes document is built from an allowlist and validated
  against a closed JSON Schema before it is written; a test fails if any
  infrastructure identifier reaches it.
- `release-images.yml` publishes a signed SBOM attestation per image. No
  separate cosign signature is added.
- A bilingual page at `/verify`, linked from the landing footer, shows the chain
  from source to running service, never shows "verified" for stale or unchecked
  data, and gives the command to verify independently.

## Consequences

- CloudFront caches one more path, beside `/_nuxt/*` (ADR 0025). ADR 0010 is
  unaffected: the document holds no publication state.
- An ECS deployment uses a Lambda function, an ECR repository, an S3 bucket, a
  schedule, and an alarm, for under USD 0.50 a month. The deploy role can update
  the observer.
- The evidence stops at the platform's report. The ECS agent on the host reports
  digests, and host-network containers can reach the instance role that makes
  those reports, so a compromised host or `app` task could forge them. The page
  states that it does not prove host honesty.
- The owner's administrator login can still change the observer. The design
  defends against a compromised application and silent drift, not against the
  operator.
- `verify` is a reserved slug in the public-root registry.
- The Vietnam Podman host uses the weaker host report described in the dated
  amendment below.

## Amendment: Vietnam host reporter (2026-10-11)

Production on one GreenNode vServer uses a host timer to inspect running Podman
containers and publish the same deployment document through the direct Caddy
edge. The [host design](../design/deployment-transparency/host.md) defines the
source, atomic publication, headers, and failure rules. The existing platform
fields identify Podman; version 1 adds that enum value without a new field.

The host reports itself, not an independent platform API. Its evidence is weaker
than ECS's off-host observer. The report does not prove host honesty or runtime
integrity. The host publishes unchecked signature and SBOM statuses; it does not
treat deploy-time checks as runtime verification. GitHub attestation and
Sigstore checks retain their policy and independent commands. AWS ECS and
Kubernetes retain their observer implementations.

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

Originally proposed as ADR 0057.
