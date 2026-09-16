# Phase 10 exit criteria

## Before the first deploy

- [ ] The single-replica direction plan's verification and fresh review pass.
- [x] `shared_rate_buckets` has an index supporting both ordered candidate
      scans, `(policy_id, last_seen, key_digest)`, proved with `EXPLAIN` at
      twenty thousand rows. The partition column was measured and removed: it
      served the allocation scan only, leaving maintenance cleanup sorting the
      whole policy under the exclusive clock. Keeping both shapes cost 13
      percent more write-ahead log on the hottest write for no further gain.
- [ ] Rate candidate selection no longer evaluates the eligibility predicate on
      every row of a policy while holding the exclusive clock. Indexing fixed
      the ordering, not the predicate: below the cleanup page size, and for an
      allocation scan whose partition holds nothing expirable, every index shape
      including none costs 150 to 300 milliseconds at twenty thousand rows.
      Either make the idle branch a real index condition, or move candidate
      discovery outside the clock lock, which is safe because both call sites
      revalidate each candidate under lock afterwards.
- [ ] Hosted provisioning works without superuser, `set-login` sends only SCRAM
      verifiers, and the server image verifies RDS TLS. Each has passing tests.
- [ ] The first-task checks on a real Bottlerocket host pass: host-mode task
      roles, the bridge-gateway listener, and the IMDS hop-limit block.
- [ ] Image builds and ARM64 smoke pass; `tofu validate` and a reviewed
      `tofu plan` pass.
- [ ] One fresh review, local `make ci`, and connected `make scan` pass at the
      candidate. The review confirms client-IP trust, origin lockdown, secret
      handling, IAM scope, and migration order by name.
- [ ] `apps/server/migrations/.uat-baseline` is committed.

## In production

- [ ] The first deploy completes, including the three first-deploy database
      steps, and its smoke checks pass. A direct request to the origin address
      fails.
- [ ] The owner tests registration, sign-in, editing, publishing, exports,
      realtime, MCP, account export and deletion.
- [ ] Every alarm is triggered once and its email arrives.
- [ ] Scheduled jobs run and report success.
- [ ] Traceability rows name their production evidence; none is claimed by
      configuration alone.

## Before the public announcement

- [ ] A snapshot restores to a temporary instance, the data verifies, and the
      instance is deleted.
- [ ] SES production access is granted.
- [ ] Privacy, terms, and name reviews are complete.
