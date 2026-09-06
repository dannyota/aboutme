# 0033 — Public image builds and private AWS deployment

Status: Accepted (2026-09-06), by the owner's approval of public image builds.

## Context

The application repository is public. ADR 0031 placed image builds with private
infrastructure workflows, so the first cost model charged private runner minutes
for them. Standard GitHub-hosted runners are free in public repositories under
the
[Actions billing rules](https://docs.github.com/en/billing/concepts/product-billing/github-actions),
checked on 2026-09-06. The owner accepted keeping image builds in the public app
repository. AWS hosting costs remain separate.

## Decision

- Public `dannyota/aboutme` owns all four image build inputs: server, web,
  Caddy, and ops. Generic configuration templates, ops scripts, native smoke
  harnesses, and synthetic fixtures stay public. Account settings, state,
  environment values, credentials, and private release records stay private.
- A manual public workflow builds and smoke-tests the exact reviewed app commit
  on standard `ubuntu-24.04-arm` runners with Podman. It targets `linux/arm64`
  and produces versioned OCI archives and provenance evidence. It needs neither
  the private repository nor AWS credentials. Existing AMD64 browser baselines
  keep their pinned architecture.
- Private `aboutme-infra` owns OpenTofu, environment configuration, ECR
  publication, and deployment. Publication is a separate manual protected job.
  It validates the canonical public workflow, approved source commit, successful
  run and attempt, artifact identity, checksums, native smoke, and image
  identities before obtaining AWS access. It pushes the tested images without
  rebuilding them.
- AWS OIDC roles trust only the private repository's protected environment.
  Public build and fork identities cannot assume publication, plan, or deploy
  roles. The publisher treats public artifacts as data and never executes
  downloaded scripts or images while holding publication credentials.
- A private release record binds the public build evidence to the reviewed
  infrastructure commit, successful publication run, and four ECR digests. Keep
  that record and referenced images through UAT, production promotion, and the
  rollback window. Expired public artifacts do not permit reconstruction from
  mutable tags. Production promotes the UAT-proven digests without a rebuild.
- Phase 9 separates free public build usage from private publication and
  deployment usage. Private minutes and metadata storage use available account
  allowances first. Start with no paid Actions usage; any required overage or
  subscription needs a priced decision. Public caches remain within the included
  10 GiB per-repository limit. No larger runners are selected.

## Supersession and consequences

This supersedes ADR 0031 only for image-build placement and its private-build
cost assumption. Private AWS ownership, native ARM64, UAT scope, budget
decisions, and local delivery gates remain in force. The public workflow's
archives and logs must contain only public inputs and synthetic evidence.

Phase 10 implements and tests the cross-repository artifact handoff. Public
builds do not resolve the private deployment approval feature: verify the
private account's eligibility or review an equivalent approval contract before
enabling AWS workflow access. This decision approves neither AWS spending nor a
GitHub plan purchase.
