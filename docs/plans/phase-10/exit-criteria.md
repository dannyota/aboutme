# Phase 10 exit criteria

## Before the first deploy

- [x] The single-replica direction plan's verification and fresh review pass.
- [x] `shared_rate_buckets` has an index supporting both ordered candidate
      scans, `(policy_id, last_seen, key_digest)`, proved with `EXPLAIN` at
      twenty thousand rows. The partition column was measured and removed: it
      served the allocation scan only, leaving maintenance cleanup sorting the
      whole policy under the exclusive clock. Keeping both shapes cost 13
      percent more write-ahead log on the hottest write for no further gain.
- [x] Hosted provisioning works without superuser, `set-login` sends only SCRAM
      verifiers, and the server image verifies RDS TLS. Each has passing tests.
- [x] The first-task checks on a real Bottlerocket host pass: host-mode task
      roles, the bridge-gateway listener, and the IMDS hop-limit block.
- [x] The v0.3.30 release-image workflow builds and smokes all three ARM64
      images.
- [x] `tofu validate` and a reviewed `tofu plan` pass.
- [x] One fresh review, local `make ci`, and connected `make scan` pass at the
      candidate. The review confirms client-IP trust, origin lockdown, secret
      handling, IAM scope, and migration order by name.
- [x] `apps/server/migrations/.uat-baseline` is committed.

## In production

- [ ] The first deploy completes, including the first-deploy database steps.
- [x] v0.3.30 runs with one healthy app task and one healthy web task, and
      public `/`, `/healthz`, and `/readyz` return 200.
- [x] A direct request to the origin address fails.
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

## After launch

- [ ] Rate candidate selection no longer evaluates the eligibility predicate on
      every row of a policy while holding the exclusive clock. Indexing fixed
      the ordering, not the predicate: below the cleanup page size, and for an
      allocation scan whose partition holds nothing expirable, every index shape
      including none costs 150 to 300 milliseconds at twenty thousand rows.
      Either make the idle branch a real index condition, or move candidate
      discovery outside the clock lock, which is safe because both call sites
      revalidate each candidate under lock afterwards. Moved here on 2026-09-17:
      the fix is a later forward migration with `CREATE OR REPLACE FUNCTION`, so
      it need not land before the baseline.

## Notes

- 2026-09-20: v0.3.30 deployed from `925b8bbc` after CI run `35456754168` and
  release-image run `35457085851` passed. The reviewed OpenTofu plan added four
  maintenance resources and changed or destroyed none. App revision 37 and web
  revision 33 are healthy; maintenance revision 2 runs zero tasks at rest. All
  five schedules are enabled. Public `/`, `/healthz`, and `/readyz` return 200,
  and the deploy script's direct-origin rejection check passed. A signed-out
  browser observed the marked maintenance 503 and automatic recovery to 200. The
  final port handoff included about one minute of 521 responses. These checks do
  not prove owner flows, job results, alarm delivery, restore, or the
  first-deploy database steps.

- 2026-09-19: production serves v0.3.29. App revision 36 and web revision 32 use
  the v0.3.29 image digests, each service has one healthy running task, and all
  five schedules are enabled. The site-down alarm is OK with actions enabled.
  Public `/`, `/healthz`, and `/readyz` return 200. These checks do not satisfy
  the open product, job-result, alarm-delivery, restore, or traceability
  criteria.

- 2026-09-17: the fresh review found two blockers (origin pulls enabled too
  late; the instance role could read every production secret), two should-fix
  items (deploy recovery; the Chromium sandbox on Bottlerocket) and four minor
  items. All are fixed and the same reviewer confirmed each fix. Local gates ran
  in chunks: `make check`, web lint, typecheck, test and build, the database,
  migration, resume API and S3 suites, the route table test, and connected
  `make scan`.
