# Task 10.17 — Operational rehearsal and evidence

**Owner:** integration owner. A fresh Sol reviewer verifies the integrated phase
and confirms fixes without authoring implementation.

## Live operational checks

Use the runbooks and exact commands delivered by the infrastructure tasks:

- AC-OPS-002 and AC-OPS-015: direct-origin rejection and the live edge matrix,
  including authenticated/MCP forwarding, public revalidation, SSE, HSTS,
  noindex, and absence of a direct public media path.
- AC-OPS-016: origin-secret rotation with current/next overlap, no downtime, and
  rejection of the retired value.
- AC-OPS-017: two concurrent migration dispatches with exactly-once application;
  rollback proves the prior digest against the migrated schema.
- AC-OPS-018: timed isolated database restore, data verification, and cleanup.
- AC-OPS-019: deliberately trigger every critical alarm and verify receipt,
  including missing-job heartbeat, cost alerts, and SES failure notifications.
- Re-run the existing media-normalization corpus and new render benchmarks under
  the selected production architecture and task limits. If UAT is smaller,
  include the production-shape rehearsal and temporary cost in Phase 9's model.
- Prove the Task 10.18 fleet contracts during an actual 1 → 2 → 1 transition:
  shared publication drains, account/private-media revocation, render and
  one-use redemption, SSE repair, aggregate limits, pgx budget, abrupt failure,
  and graceful scale-in.

Changing topology requires the corresponding design/acceptance update first; it
does not silently waive a recovery or security invariant.

## Evidence

Keep bounded evidence under ignored `.dev/uat/<commit>/<run-id>/`. Never commit
browser traces, mail links, tokens, cookies, credentials, account data, or raw
cloud logs. Prefer redacted structured results; inspect artifacts before
sharing.

Record the candidate and image digests, migration head, commands, tool versions,
timestamps, fixture scope, expected/observed results, failures/reruns, and
configuration names without values. Link results to the stable traceability IDs.
Only a redacted summary and safe test references enter the public repository.

ADR 0024 applies: one fresh phase review and one corrected exit checklist. No
blind test author, frozen catalog, sealed exporter, or mandatory two-stage
acceptance ceremony is introduced.

## Cost and cleanup

Compare observed UAT usage/cost with Phase 9's estimate. Record stopped/deleted
resources, retained data/backups, remaining daily cost, and expiry/cleanup
owner. Do not delete the owner's shared SES resources, unrelated DNS, or
production resources. Follow the recorded UAT retention policy for test data.
Record each booked window's ALB lifetime, the off-state absence of application
nodes and the ALB, RDS stop/start guard execution, required retention-job
completion, persistent RDS storage, keys, state, ECR images, and required logs.
Treat the modeled full-month 30 GB root disk and public IPv4 as conservative
cost reserves until live inventory proves they can decrease. Clean up any
orphaned volume or address. Forecast at USD 30 shortens optional work; it cannot
defer privacy or deletion obligations.

Successful Phase 10 is the evidence input to Phase 11. It does not authorize
production promotion by itself.
