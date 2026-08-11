# Task 11: DNS + certificate glue — `cf` apply script from Terraform outputs

**Files:** `deploy/aws/scripts/dns-apply.sh`, `.env.example` diff
(`CLOUDFLARE_API_TOKEN=` name-only; token scope Zone:DNS:Edit on `aboutme.vn`
only — D13) for the integration owner (owner-serialized).

**Steps:**

- [ ] Failing-first: script test harness (pure-bash, no network) feeding a
      fixture `terraform output -json` document and asserting the rendered `cf`
      commands for: `origin-staging` A → EIP (grey-cloud/DNS-only), ACM
      validation CNAMEs, `staging` CNAME/alias → CloudFront domain. Production
      names render from the same code path with production outputs (parity).
- [ ] Implement with `--check` (diff live DNS vs outputs, exit nonzero on drift
      — this becomes a P9A/P10 pre-flight) and `--apply` modes; `cf` CLI v0.5+
      per CLAUDE.md; never a Cloudflare Terraform provider (D19). Grey-cloud
      enforced on every record the script manages.
- [ ] Document ordering in the script header: secrets-bootstrap → terraform
      apply (cert pending validation) → `dns-apply.sh --apply` (validation
      records) → ACM issues → CloudFront deploys. This ordering note **is** the
      executable answer to the cert-vs-DNS chicken-and-egg.

**Verification:** script harness green in CI (no network); shellcheck.
Real-AWS/Cloudflare execution happens in Task 14 (stated).
