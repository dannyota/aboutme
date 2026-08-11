# Task 3: Database + storage modules — RDS gp3 (private subnets), S3 media

**Files:** `deploy/aws/modules/database/**`, `deploy/aws/modules/storage/**` (+
tests), env-root wiring.

**Steps:**

- [ ] Failing `terraform test` (mocked) first: RDS engine `postgres`, engine
      version = pinned latest stable major (resolve at scaffold; record),
      `storage_type = "gp3"`, `instance_class = var.db_instance_class`,
      `backup_retention_period = var.db_backup_retention_days` (staging 7,
      production 30 per spec §9), PITR on, storage encrypted,
      `publicly_accessible = false`, deletion protection on in production vars,
      **subnet group composed of the network module's private subnets only**,
      `password_wo` present **with its required companion
      `password_wo_version = var.db_master_password_version`** and **no**
      `password` attribute; parameter group keeps `max_connections` ≥ 100
      (budget wiring). S3: bucket private, all public access blocked, policy
      grants read to the CloudFront OAC principal only, SSE enabled, versioning
      on.
- [ ] Implement both modules; DB security group admits 5432 **only from the
      compute node SG** (module input), nothing else.
- [ ] Output the pieces `DATABASE_URL` assembly needs (host, port, dbname) —
      never a URL containing credentials (compose convention).

**Verification:** `terraform test`, `validate`, parity. RDS actually accepting
connections is Task 14 + P9A territory (real AWS, stated).
