# Phase 10 infrastructure exit criteria

This file separates the local code checkpoint from the handoff that permits
hosted UAT activation. Hosted UAT is not a prerequisite for infrastructure
activation.

## Local code checkpoint

- [ ] The [email ownership strategy](contracts.md#existing-email-ownership)
      names exactly one manager per resource and provides retention, import,
      rollback, and no-change-plan checks before any overlapping resource apply.
- [ ] Task 10.14's harness and the workflow/operational specs and commands are
      locally tested and included in the reviewed candidate before activation.

- [ ] Phase 9 records quantified AWS cost, a budget decision, and selected UAT
      sizing. No sizing default in this baseline is approved until that result.
- [ ] The pre-dispatch refresh records the final Phase 6.1/6.2, Phase 7.1/7.2,
      and Phase 8 interfaces; `PUBLIC_RENDER_ORIGIN` replaces any stale
      `NUXT_RENDER_ORIGIN` use where required; password and MCP settings, mail
      runtime, SES handoff, provider-login-disabled startup credentials,
      internal MCP routes, and UAT Basic Authorization behavior are resolved.
- [ ] The UAT access mechanism and secret runtime contract are explicit outputs
      of the refresh. Task contracts do not choose either silently. Secret
      handling follows the current mandatory secret-skill instructions and no
      secret values enter this plan.
- [ ] Local-only infrastructure preparation passes its affected checks with fake
      AMI data and OpenTofu mocks. No AWS, Cloudflare, DNS, registry, or
      hosted-UAT mutation is used for this checkpoint.
- [ ] Each task has one author and its affected checks. One fresh Phase 10
      review reads the integrated candidate. The integration owner runs
      `make ci` and `make scan` once at that unchanged candidate commit.

## Activation handoff into hosted UAT

- [ ] The local code checkpoint and Phase 9 cost decision are recorded at the
      same candidate commit.
- [ ] The existing user authorization record names `uat.aboutme.vn`,
      `ap-southeast-1`, the UAT resource scope, and Cloudflare DNS. It does not
      authorize production or replace the Phase 9 budget decision.
- [ ] Task 10.15 may now resolve the real AWS AMI, apply the approved UAT
      resources, complete certificate/DNS and secret handoffs, and run hosted
      UAT at `uat.aboutme.vn`. It records redacted evidence and leaves the
      operational rehearsal to the remaining Phase 10 exit criteria.

## Hosted UAT handoff

- [ ] Task 10.8's native `ubuntu-24.04-arm` build/smoke evidence and versioned
      manifest identify the app/infrastructure commits and all four Linux/arm64
      ECR digests. Task 10.12 validates that successful build and deploys the
      same images; AMD64 browser baselines remain on their pinned architecture.
- [ ] Task 10.15 has a zero-drift post-activation plan, passing health and
      readiness checks through `uat.aboutme.vn`, and the UAT evidence ledger.
- [ ] Phase 10 operational rehearsal receives the live interfaces for its
      behavior matrix, rotation, migration, restore, alarm, and performance
      checks. Phase 11 receives the UAT-proven digest manifest and production
      promotion handoff.
