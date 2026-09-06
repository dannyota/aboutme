# Task 10.2: Network module — ALB ingress, public application nodes, private RDS

**Dependency:** Task 10.18's approved trust, placement, readiness, and scheduled
UAT contracts. One author writes failing checks first. The fresh Phase 10 review
covers metadata, security groups, forwarding trust, and stopped-state safety.

**Files:** `deploy/aws/modules/network/**` and tests, environment-root wiring,
and the recovery runbook named by Task 10.18.

**Steps:**

- [ ] Write mocked `tofu test` cases for two public application subnets and two
      private RDS subnets across two Availability Zones. Private subnets have no
      internet gateway or NAT route. Nodes associate public IPv4 for outbound
      access without NAT.
- [ ] Assert an internet-facing application load balancer (ALB) spans both
      public subnets when the environment is active. Its listener accepts only
      the CloudFront origin path chosen by Task 10.18. Node ingress accepts only
      the ALB security group on the selected target port; it admits neither
      direct Internet traffic nor ports 22 or 80. The origin-secret check
      remains mandatory at Caddy.
- [ ] Assert the ALB, target groups, listeners, and related hourly resources are
      absent when scheduled UAT is off. Production always retains them. The UAT
      state transition cannot leave a public target or listener behind.
- [ ] Implement the Auto Scaling group (ASG) and capacity-provider inputs from
      Task 10.18. Production uses minimum one and initial maximum two nodes. UAT
      uses explicit active and stopped settings. Tests reconcile ECS desired
      count with ASG minimum, desired, and maximum so host replacement cannot
      start a stopped environment.
- [ ] Pin the selected ECS-optimized Amazon Linux 2023 arm64 image during
      authorized activation. Require IMDSv2, hop limit one, encrypted gp3 root
      volumes, task-role credentials, and host/container metadata firewall
      rules. Dedicated application UIDs cannot reach instance metadata.
      Root-owned ECS and SSM agents keep only their required instance-profile
      paths.
- [ ] Test ALB target registration, deregistration, readiness, replacement, and
      one-complete-replica-per-node placement using the exact output interface
      consumed by Task 10.5. No stable elastic IP or EIP reassociation script is
      part of the target topology.
- [ ] Seed the recovery runbook with node replacement, ALB target health,
      stopped-UAT reconciliation, and direct-node bypass checks. Production
      recovery never scales below one.

**Verification:** mocked `tofu test`, `tofu validate`, environment parity,
shellcheck for rendered user data, and deterministic lifecycle tests. Task 10.15
owns real AWS checks.
