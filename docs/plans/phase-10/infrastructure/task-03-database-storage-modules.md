# Task 10.3: Database + storage modules — RDS gp3 (private subnets), S3 media

**Files:** `deploy/aws/modules/database/**`, `deploy/aws/modules/storage/**` (+
tests), env-root wiring.

The media bucket follows the
[deployment design](../../../design/deployment.md#media) and
[ADR 0019](../../../adr/0019-private-media-delivery.md). It is not a CloudFront
origin. Task 10.4 grants the Go server task the only application access.

**Steps:**

- [ ] Failing `tofu test` (mocked) first: RDS engine `postgres`, engine version
      = pinned latest stable major (resolve at scaffold; record),
      `storage_type = "gp3"`, `instance_class = var.db_instance_class`,
      `backup_retention_period = var.db_backup_retention_days` (staging 7,
      production 30 per the
      [privacy lifecycle](../../../design/operations.md#privacy-lifecycle)),
      PITR on, storage encrypted, `publicly_accessible = false`, deletion
      protection on in production vars, **subnet group composed of the network
      module's private subnets only**, `password_wo` present **with its required
      companion `password_wo_version = var.db_master_password_version`** and
      **no** `password` attribute; parameter group keeps `max_connections` ≥ 100
      (budget wiring). S3: bucket private; all four public-access-block settings
      true; Object Ownership set to `BucketOwnerEnforced`; S3-managed AES-256
      encryption; versioning explicitly disabled; bucket policy denies transport
      without TLS and grants no principal. No website configuration, ACL,
      CloudFront principal, or OAC exists.
- [ ] Pin destroy behavior by environment. Staging is disposable only after an
      operator records the data-loss scope: RDS uses
      `skip_final_snapshot = true` and a deletion-protection variable set false,
      and a scoped pre-destroy command empties only the named staging media
      bucket before OpenTofu destroys it. Production has deletion protection,
      `skip_final_snapshot = false`, a unique timestamped final-snapshot
      identifier, and no force-destroy bucket path. Mock tests prove both tfvar
      modes and fail if production can delete stateful data without a snapshot.
- [ ] Implement both modules; DB security group admits 5432 **only from the
      compute node SG** (module input), nothing else.
- [ ] Output the pieces `DATABASE_URL` assembly needs (host, port, dbname) —
      never a URL containing credentials (compose convention).
- [ ] Output the media bucket name, bucket ARN, and fixed `resumes/` object
      prefix for Task 10.4. Do not output an object URL or public hostname.

**Verification:** `tofu test`, `validate`, parity. The tests must reject a
bucket policy that grants access, any public-access-block flag set false, a
website or ACL, enabled or suspended versioning, an OAC, and any media output
other than bucket identity and the fixed prefix. RDS actually accepting
connections is Task 10.15 + Phase 10 operational rehearsal territory (real AWS,
stated).
