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
      — this becomes a P9A/P10 pre-flight) and two apply stages; `cf` CLI v0.5+
      per D19; never a Cloudflare Terraform provider. `--apply-foundation`
      writes only the DNS-only origin A record and ACM validation CNAMEs.
      `--apply-aliases` writes apex/staging and canonical redirect aliases only
      after Terraform outputs a deployed distribution domain. Grey-cloud is
      enforced on every record.
- [ ] Document and test the two Terraform stages in the script header. The
      foundation saved plan has `services_enabled=false` and
      `distribution_enabled=false`, but creates the EIP, persistent data plane,
      and ACM certificate and outputs validation records. Apply it, run
      `--apply-foundation`, and wait until ACM is `ISSUED`. Only then create and
      approve a **new** full saved plan with `distribution_enabled=true`; after
      apply, run `--apply-aliases`. CloudFront is never planned against a
      pending certificate, and no alias points at a missing distribution. Task
      14 and the production promotion use this same staged contract.

**Verification:** script harness green in CI (no network); shellcheck.
Real-AWS/Cloudflare execution happens in Task 14 (stated).
