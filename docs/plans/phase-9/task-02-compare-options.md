# Task 9.2 — Compare cost and operating effort

**Owner:** integration owner or one delegated researcher. **Owned output:**
`docs/research/aws-cost/comparison.md`. **Predecessor:** task 9.1.
**Authority:** [phase index](README.md) and accepted deployment boundaries.

- [ ] Compare the ECS-on-EC2/RDS/S3 baseline with managed AWS alternatives,
      prioritizing ECS/Fargate and suitable managed database, storage, email,
      and scheduling options. Evaluate at least two viable configurations or
      document disqualifications. Verify Singapore availability, Chromium
      resource limits, networking, SSE behavior, and OpenTofu provider support.
- [ ] Prefer ARM64/Graviton where compatible. Check every runtime image and
      native dependency, especially Chromium and fonts. Compare priced capacity
      against workload measurements; do not translate advertised
      price/performance into an assumed application speedup. Flag any ARM64
      incompatibility before the Phase 10 build contract is implemented.
- [ ] For each option calculate setup, active UAT, idle UAT, retained-resource,
      and monthly production costs using task 9.1. Include temporary restore
      resources and image/log retention.
- [ ] Compare memory/render bounds, SSE, private networking/media, downtime,
      backup/restore, patching, portability, and operator effort. Identify
      proposed design changes and the ADR each would require.
- [ ] Prefer managed services when the trade-off is acceptable. A self-managed
      component needs a concrete cost or capability reason, including patching,
      recovery, and ongoing operator effort.
- [ ] Test sensitivity to runtime hours, traffic/egress, storage, render jobs,
      and logs. Compare scheduled shutdown/teardown savings with restore time
      and retained charges. Commitments are research options, not purchases.

**Check:** reproduce scenario totals from task 9.1; verify service claims with
official sources; run Prettier and markdownlint on the output.

**Done:** options have comparable totals, assumptions, operating trade-offs, and
explicit unmet requirements. Low cost does not excuse a security, reliability,
or resource requirement.
