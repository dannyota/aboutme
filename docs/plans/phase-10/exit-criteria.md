# Phase 10 exit criteria

## Before activation

- [ ] Task 10.14's harness, workflow specs, operational scripts, and runbooks
      are authored, tested locally, and included in the reviewed candidate.
      After task 10.15 deploys it, only live preflight and acceptance execution
      remain; no new test code is required to begin the hosted run.

- [ ] Phase 6–8 behavior and Phase 9's cost/configuration decision are complete.
- [ ] Infrastructure contracts match the runtime and cost decision, including
      mail/MCP settings, disabled-provider startup, edge routes, and UAT access.
- [ ] The [infrastructure local checkpoint](infrastructure/exit-criteria.md)
      passes. One fresh review, local `make ci`, and connected `make scan` pass
      at the candidate before deployment; heavy checks run serially.
- [ ] The migration baseline marker is committed before the first UAT migration.
- [ ] The resource/DNS inventory, spending ceiling, UAT lifetime, cleanup scope,
      and any global-service region exceptions are recorded for the authorized
      Singapore environment and `uat.aboutme.vn`.
- [ ] The email runbook's existing SES stack is inventoried. OpenTofu ownership
      and runtime IAM are settled without replacing resources or Google DNS.
      Sandbox-compatible workflow recipients and missing integration have
      owners.

## Hosted acceptance

- [ ] Task 10.15 deploys the candidate digests and passes the activation
      handoff.
- [ ] Tasks 10.14–10.16 pass all required workflows through real HTTPS and SES.
- [ ] Task 10.17 passes security, performance, restore, rotation, migration,
      rollback, edge, alarm, and cost checks with private supporting evidence.
- [ ] Affected traceability rows have accurate evidence; no required row is
      blocked or claimed proven by configuration alone.
- [ ] The same fresh reviewer confirms fixes and the final evidence. Any
      candidate change reruns required gates and invalidated UAT results.
- [ ] Cleanup/retention and residual cost are recorded; unrelated resources and
      the owner's shared mail setup are preserved.
- [ ] The integration owner completes this checklist and records local gates and
      hosted evidence against one unchanged final candidate before closure.

Production is Phase 11 and needs separate owner approval. Correct wrong criteria
in this phase and note the change, per ADR 0024.
