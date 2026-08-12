# Task 2: Network module — VPC, prefix-list ingress, private DB subnets, EIP auto-reassociation

**Tier:** High risk for the instance profile, metadata boundary, host firewall,
and origin TLS-key storage.

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
      solely for the DB subnet group (Task 3); (f) the launch ENI has
      `associate_public_ip_address = true`, which supplies bounded bootstrap
      egress before the EIP is associated; (g) metadata options require IMDSv2,
      disable v1, and use hop limit 1; (h) the 30 GiB gp3 root volume is
      encrypted with `alias/aws/ebs`, delete-on-termination, and not magnetic.
- [ ] Implement: VPC (public subnets across 2 AZs — the single node lives in
      one; **plus 2 private subnets for RDS**), IGW, SG per the tests, EIP,
      ASG(1) + launch template. Resolve the latest stable ECS-optimized Amazon
      Linux 2023 arm64 AMI at scaffold time, then pin and record its exact image
      ID; do not float an SSM alias at apply. The instance role has only ECS
      registration, SSM Session Manager, the one EIP association, and the one
      CloudWatch metric namespace; no application SSM or media access. User data
      first verifies the pinned `aws` CLI is present and records its version,
      then associates the EIP by allocation ID with at most 30 attempts ten
      seconds apart. It emits one failure metric without secrets and exits
      nonzero after five minutes. Initial public IPv4 egress is removed from the
      interface only by the successful EIP replacement path.
- [ ] Before ECS starts tasks, a persistent systemd unit installs and verifies
      two metadata rules: `DOCKER-USER` rejects bridge-container traffic to
      `169.254.169.254/32`, while leaving ECS task credentials at
      `169.254.170.2` reachable; host `OUTPUT` rejects IMDS for the dedicated
      Caddy, server, migrate, web, and ops UIDs fixed by Task 5. Root-owned ECS
      and SSM agents retain the minimum instance-profile path. A rendered-user-
      data test asserts rule order, persistence across service restart, fail-
      closed startup, IMDSv2 settings, CLI probe, and bounded retry. Task 14
      probes denial from bridge and host-mode application containers while a
      server task still obtains its task-role identity.
- [ ] The same pre-ECS unit creates `/var/lib/caddy` and `/var/log/caddy` owned
      by UID/GID 10001 with mode 0700, then verifies both before starting ECS.
      No other task UID can read the persisted origin TLS material or access
      logs.
- [ ] Seed `docs/runbooks/eip-recovery.md`: what auto-reassociation does, manual
      recovery commands, and the P9A drill hook ("terminate the instance; verify
      the replacement re-associates within five minutes after EC2 reports the
      replacement running"). `make docs-fmt && make docs-lint`.

**Verification:** `terraform test` (mocked, in CI — no credentials); `validate`;
parity check still green. Real-AWS behavior (does the user-data script actually
associate?) is **deliberately deferred to Task 14's staging bring-up** — the
cheapest safe check before that is a shellcheck + `bash -n` pass on the rendered
user-data script plus the mocked assertions.
