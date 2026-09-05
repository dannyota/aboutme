# Task 9.1 — Workload and price inventory

**Owner:** integration owner or one delegated researcher. **Owned output:**
`docs/research/aws-cost/workload.md` and `pricing.csv`. **Authority:**
[phase index](README.md), deployment design, and resource budgets.

- [ ] Inventory Go/Nuxt/Caddy, PostgreSQL, media, Chromium, SSE, mail, jobs,
      backups, observability, and build/deploy resources from the final runtime.
- [ ] Record measured memory/CPU bounds and unknown traffic inputs. Define low,
      expected, and stress scenarios as assumptions, with active hours,
      requests, concurrent SSE connections, render jobs, storage growth, and
      transfer.
- [ ] Retrieve official Singapore prices and billing units for compute,
      database, storage, backups, transfer, public IPv4, NAT/endpoints, edge,
      DNS, email, logs/metrics/alarms, secrets, registry, and jobs. Include
      applicable items in each option and account for idle charges.
- [ ] Price native GitHub Actions `ubuntu-24.04-arm` builds separately from AWS
      runtime. Public app CI uses standard public-repository runners; the
      planned private `aboutme-infra` workflows consume the owner's included
      minutes and then paid usage. Model build frequency/duration, cache and
      artifact storage/retention, and ECR storage/transfer. Verify the account
      plan supports the required private-repository environment protections.
      Record account-plan assumptions without private billing data. Use dated
      [runner specifications](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)
      and
      [Actions billing](https://docs.github.com/en/billing/concepts/product-billing/github-actions)
      sources; do not assume deployment builds are free.
- [ ] Save each price's source URL, retrieval date, region, unit, currency, and
      calculation basis. Show steady cost without promotional credits; itemize
      allowances and discounts separately with their eligibility.

**Check:** recalculate inventory units and totals from saved inputs; run
Prettier and markdownlint on the owned Markdown. No cloud mutation is part of
this task.

**Done:** another reader can reproduce assumptions and dated price inputs
without account access. Report remaining unknowns and exact checks run.
