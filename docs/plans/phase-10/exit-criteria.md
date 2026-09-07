# Phase 10 exit criteria

## Before activation

- [ ] Task 10.18's detailed design, bounded implementation tasks, runtime
      implementation, and local multi-process/lifecycle proofs are complete.
      Dependent infrastructure and hosted work consume those outputs.
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
- [x] `shared_rate_buckets` has an index supporting both ordered candidate
      scans, `(policy_id, last_seen, key_digest)`, proved with `EXPLAIN` at
      twenty thousand rows. The partition column was measured and removed: it
      served the allocation scan only, leaving maintenance cleanup sorting the
      whole policy under the exclusive clock. Keeping both shapes cost 13
      percent more write-ahead log on the hottest write for no further gain.
- [ ] Rate candidate selection no longer evaluates the eligibility predicate on
      every row of a policy while holding the exclusive clock. Indexing fixed
      the ordering, not the predicate: below the cleanup page size, and for an
      allocation scan whose partition holds nothing expirable, every index shape
      including none costs 150 to 300 milliseconds at twenty thousand rows.
      Either make the idle branch a real index condition, or move candidate
      discovery outside the clock lock, which is safe because both call sites
      revalidate each candidate under lock afterwards.
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
- [ ] Production-shaped UAT proves actual 1 → 2 → 1 capacity during writes,
      revocation, render, and SSE, including abrupt failure and graceful drain,
      without multiplying limits or exceeding the pgx connection budget.
- [ ] Task 10.17 passes security, performance, restore, rotation, migration,
      rollback, edge, alarm, and cost checks with private supporting evidence.
- [ ] Affected traceability rows have accurate evidence; no required row is
      blocked or claimed proven by configuration alone.
- [ ] The same fresh reviewer confirms fixes and the final evidence. Any
      candidate change reruns required gates and invalidated UAT results.
- [ ] Scheduled UAT stop/start, RDS seven-day guard, required-job deadlines,
      temporary ALB removal, cleanup/retention, and residual cost are recorded;
      unrelated resources and the owner's shared mail setup are preserved.
- [ ] The integration owner completes this checklist and records local gates and
      hosted evidence against one unchanged final candidate before closure.

Production is Phase 11 and needs separate owner approval. Correct wrong criteria
in this phase and note the change, per ADR 0024.
