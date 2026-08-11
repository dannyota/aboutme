# Task 2: Network module — VPC, prefix-list ingress, private DB subnets, EIP auto-reassociation

**Files:** `deploy/aws/modules/network/**` (+ `tests/*.tftest.hcl`), env-root
wiring, `docs/runbooks/eip-recovery.md` seed.

**Steps:**

- [ ] Failing tests first (`terraform test` with `mock_provider "aws"` and
      **explicit `override_data` values** for the prefix-list and AMI data
      sources, so wiring assertions check real derived values, not
      mock-generated placeholders): assert (a) the SG has **exactly one**
      inbound rule — TCP 443 whose source is the managed prefix list
      `com.amazonaws.global.cloudfront.origin-facing` (via
      `aws_ec2_managed_prefix_list` data source); (b) no rule admits port 80 or
      22 (also the SG-quota-safe shape — D14); (c) the EIP is tagged for the
      user-data association script; (d) the ASG has min=max=desired=1 and the
      launch template sets `instance_type = var.instance_type`; (e) **private
      subnets exist (2 AZs) with no route to an IGW and no NAT** — they exist
      solely for the DB subnet group (Task 3).
- [ ] Implement: VPC (public subnets across 2 AZs — the single node lives in
      one; **plus 2 private subnets for RDS**), IGW, SG per the tests, EIP,
      ASG(1) + launch template (arm64 ECS-optimized AMI pinned per the
      guardrail; user data associates the EIP by allocation ID and emits a
      CloudWatch metric on failure; SSM Session Manager access via instance role
      — no SSH key pairs).
- [ ] Seed `docs/runbooks/eip-recovery.md`: what auto-reassociation does, manual
      recovery commands, and the P9A drill hook ("terminate the instance; verify
      the replacement re-associates within N minutes").
      `make docs-fmt && make docs-lint`.

**Verification:** `terraform test` (mocked, in CI — no credentials); `validate`;
parity check still green. Real-AWS behavior (does the user-data script actually
associate?) is **deliberately deferred to Task 14's staging bring-up** — the
cheapest safe check before that is a shellcheck + `bash -n` pass on the rendered
user-data script plus the mocked assertions.
