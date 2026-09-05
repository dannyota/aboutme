# Task 9.3 — Recommend the UAT configuration

**Owner:** integration owner. **Owned output:**
`docs/research/aws-cost/recommendation.md`; matching ADR, design, Phase 10 plan,
and traceability changes where a decision changes. **Predecessor:** task 9.2.

- [ ] Recommend UAT resource sizes, storage/backup settings, test duration, and
      separate production assumptions. Explain how they fit the measured
      workload and the comparison's trade-offs.
- [ ] Give expected and upper-bound scenario costs, a proposed spending ceiling,
      alert thresholds, cleanup steps, and remaining idle charges. Distinguish
      budget alerts from mechanisms that actually stop spending.
- [ ] Record the owner's budget decision if an amount remains unresolved. Reuse
      the authorization for Singapore, UAT, and Cloudflare DNS.
- [ ] Inventory the owner's SES handoff and missing deployment inputs without
      storing secret values. Carry missing mail setup to Phase 10's activation
      checklist; do not claim SES readiness without evidence.
- [ ] Account for OpenTofu adoption of the existing `aboutme-email` stack,
      missing runtime IAM, sandbox recipient limits, and the unconsumed feedback
      queue described in `docs/runbooks/email.md`. Do not duplicate its
      resources.
- [ ] Update Phase 10 contracts, including any architecture ADR, regional/global
      resources, routing, drill costs, and retention. Document UAT/production
      differences and how production-shape risks will be tested before launch.
- [ ] Resolve the fresh plan review and run the phase exit checklist.

**Check:** a fresh Sol reviewer reproduces representative totals and checks
design fit; the integration owner runs owned-path documentation checks and the
[phase exit criteria](exit-criteria.md).

**Done:** the Phase 10 author has a priced, accepted configuration and clear
scope. No resource has been provisioned by this research task.
