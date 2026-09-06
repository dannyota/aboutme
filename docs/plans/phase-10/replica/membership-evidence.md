# R1.11 membership evidence implementation plan

> For workers: use superpowers:subagent-driven-development or
> superpowers:executing-plans within AGENTS.md ownership. ADR 0024 keeps one
> author per task and one fresh phase review; it supersedes per-task review
> defaults.

**Goal:** Record graceful leave and exact EC2 termination with immutable replay
and atomic release of the terminated replica's claims.

**Architecture:** Migration 23 follows accepted lifecycle operations 22. It adds
the two missing proof fields before exposing three role-bound functions. Leave
binds a historical prepare step; proof releases all owned live claims in one
transaction. Store methods return a result only after WriteTxRunner succeeds.

**Tech stack:** PostgreSQL 18, Goose, Go, pgx and sqlc; repository pins apply.

**Spec:** [Evidence operations](../../../design/scaling/membership-evidence.md),
[transport](../../../design/scaling/membership-evidence-transport.md),
[membership](../../../design/scaling/replica-membership.md),
[cases](../../../design/scaling/membership-cases.md),
[schema](../../../design/scaling/runtime-schema.md),
[claims](../../../design/scaling/claim-operations.md),
[lifecycle](../../../design/scaling/lifecycle-controller.md) and
[write entry](../../../design/scaling/transaction-entry.md).

## Ownership and acceptance

This slice contributes to AC-INF-009 and Task 10.18's membership, fencing and
claim-release rows. R8 retains local join proof, trusted identity, real EC2
DescribeInstances evidence and the permitted same-evidence ambiguity retry.

Root assigns one Sol author after migration 22 is accepted and committed:

- `apps/server/migrations/00023_runtime_membership_evidence.sql`;
- `apps/server/migrations/runtime_membership_evidence_test.go`;
- `apps/server/migrations/runtime_membership_evidence_helpers_test.go`;
- `apps/server/sql/runtime_membership_evidence.sql`;
- `apps/server/internal/store/runtime_membership_evidence.go`;
- `apps/server/internal/store/runtime_membership_evidence_test.go`.

The author must not have designed or reviewed this slice. Root owns earlier
migrations, generated files, existing query/store sources, shared harnesses,
plans, configuration and Git. Report shared edits needed for earlier tests that
insert legacy proof rows. Do not change existing files outside ownership.

Use isolated harness databases in the one shared container and the public
fixture DSN. One heavy command at a time; bound setup, waits and cleanup and
cancel/join every goroutine. No container reset, native/shared migration, cloud
call, secret read, dependency change or caller composition.

## Fixed interfaces

Implement runtime_finish_serving_graceful_leave,
runtime_finish_maintenance_graceful_leave and runtime_record_ec2_termination
with the exact signatures and composites in the transport contract. Their
respective direct roles are app, maintenance and fencing-proof. All input and
result fields are required; add no optional retry/authority parameter.

Define RuntimeMembershipEvidenceTransport,
NewRuntimeMembershipEvidenceTransport, RuntimeLeaveResult and RuntimeFenceResult
exactly as specified. There is no separate public proof SELECT, resolver,
cleanup method or retry loop. A single idempotent proof call supplies the
readback boundary.

Install the owner-only runtime_sample_membership_time sampler and
runtime_release_fenced_replica_claims(replica_id uuid, effective_at timestamptz)
helper. The latter returns the changed parent count, hardcodes fenced and is
called only by the proof function. No login executes either helper.

## Task 1: Prove missing fields and operations

- [ ] Before migration 23 exists, observe a bounded test report both absent
      proof fields and all three exact missing function signatures:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeMembershipEvidenceSurfaceExists$'
```

- [ ] Add failing identity, replay, role and cleanup cases before implementing
      each rule. Keep fixtures otherwise valid and require the intended
      SQLSTATE; unrelated constraint failures are not proof.

## Task 2: Upgrade proof storage without inventing history

- [ ] Enter runtime_begin_migration_write('migration-00023') first and finish
      last. Down is inert. Keep migration 00015 unchanged.
- [ ] Require runtime_fencing_proofs empty before changing its schema. No proof
      writer exists before this slice. Reject a nonempty legacy table with a
      fixed diagnostic and preserve the entire transaction; never delete proof
      or fabricate old request IDs/reclaimed counts.
- [ ] Add request_id text NOT NULL, printable ASCII 1..128 bytes and UNIQUE. Add
      reclaimed_claim_count integer NOT NULL with a nonnegative CHECK. Retain
      proof immutability and all existing identity/time constraints.
- [ ] Test a populated version 22 upgrade with ordinary business, membership,
      transition, claim and rate rows preserved. Separately insert one valid
      legacy proof at version 22 and prove upgrade rejection preserves it, other
      rows, version and write generation. Inject a later migration error to
      prove new columns/functions/grants and generation roll back together.

## Task 3: Implement graceful leave

- [ ] Use fixed serving/maintenance wrappers over an owner-only helper. Validate
      direct session_user and scalar bounds before mutable reads. Lock
      public_state, capacity, exact replica, target evidence and live claim
      parents in UUID order. Use immutable prepare rows without a reverse
      replica-to-ledger or transition-parent lock edge.
- [ ] Require the exact tuple/kind and prepared draining target, no termination
      intent, no conflicting receipt, zero waiting/running claims and a
      nonlocking predicate finding zero owned closing/unresolved transitions.
      SQL never substitutes for local callbacks/processes joining.
- [ ] Bind operation_id and expected controller generation to the matching
      prepare step's historical result generation. Serving uses
      prepare-scale-in; maintenance uses prepare-maintenance-drain. Later
      current controller generation may be greater. A current generation below
      the retained prepare result is AM001.
- [ ] Insert one immutable zero-count receipt, change draining to left and
      update capacity generation once. Store clamped database time and the exact
      wrapper name in capacity.updated_by. Preserve controller generation,
      desired capacity, admission, lifecycle and partition/debt state.
- [ ] Exact receipt replay returns the retained result without new live
      predicates or mutation. Changed identity/operation/generation is AM002;
      malformed retained evidence is AM001. No leave release, fence, readiness
      reopening or public-transition mutation is added.

## Task 4: Implement exact proof and atomic claim release

- [ ] Require direct fencing-proof role, exact identity, observed_state exactly
      terminated and valid external time order. Store external times raw; SQL
      does not call AWS or infer death from a deadline, heartbeat, ECS, ALB or
      database loss.
- [ ] Lock public_state, capacity, target replica, matching intent/proof, then
      target live claims by UUID and catalog/summaries canonically. Proof must
      neither lock nor predicate on any transition parent. A dead initiator's
      closing/unresolved transition remains unchanged.
- [ ] Store request_id with or without an intent and enforce uniqueness among
      proofs. If that ID names an intent, it must belong to the exact target; if
      the target has an intent, the request must match it. Reject changed
      no-intent replay or cross-replica request/evidence reuse with AM002.
- [ ] A left target stays left and gains audit proof with reclaimed count zero.
      Joining/active/draining/terminating becomes fenced. Release only that
      replica's waiting/running claim parents and every ordered child scope,
      decrement each summary once, retain identities/ordinals and store the
      changed parent count on the proof. Existing released receipts remain
      unchanged. No claim age authorizes release.
- [ ] Process all live owned claims in one transaction, with ordered locks and a
      scalar result. There is no fixed row/work bound. Use the bounded caller
      context, no unbounded Go result/SQL array, no paging, partial commit,
      global cap or cleanup ledger. Timeout/error grants no authority; confirmed
      rollback preserves rows. Ambiguous completion retires the connection and
      is resolved only through the permitted same-evidence call.
- [ ] After needed locks, sample runtime_sample_membership_time once and clamp
      to prior evidence. Use that time for new replica/proof/claim/summary
      timestamps. Fresh proof, including left audit proof, advances capacity
      generation once and leaves controller generation and partition state
      unchanged. Detect generation/count overflow before partial completion.
- [ ] Exact proof replay compares retained input identity and returns stored
      state/count/time after later claim GC. Never recompute the historical
      reclaimed count. A same-evidence call after ambiguous commit either
      replays the committed proof or performs the sole absent insertion. Store
      transport does not retry, replace an evidence ID or infer success.

## Task 5: Prove state, role, replay and lock boundaries

- [ ] Cover each leave predicate, historical generation equal/greater, wrong
      target/kind/operation/generation, missing prepare, live
      claims/transitions, intent, exact replay and every changed receipt field.
- [ ] Cover every proof source state, left audit-only behavior, exact intent, no
      intent, every changed proof field, request/evidence conflicts, stored
      reclaimed count and replay after released-claim GC. Both new fields remain
      immutable. Prove no other replica's claim or evidence changes.
- [ ] Exercise multiple claim policies/scopes and waiting/running/released
      states. Prove exact decrements, retained ordinals, corruption rollback and
      parent rather than child count. A canceled/failed cleanup leaves no
      partial proof, fence, receipt, summary or generation change.
- [ ] Use independent pinned pools with observed lock waits for claim acquire
      versus fence, release versus leave/fence and competing proof identity.
      Hold a transition parent while proof succeeds and leaves it unchanged;
      leave may reject its visible blocker but never wait on that parent.
      Fenced-transition recovery remains separate and unimplemented here.
- [ ] Prove all real named role success/denial, exact owners/search paths/ACLs,
      no helper or direct table access, no supplied values in fixed errors and
      hostile search-path resistance.
- [ ] Test future-time clamp and generation bounds with the isolated sampler
      replacement only. No shared/native clock change. Bound every race, cancel
      unfinished queries and join all goroutines before fixture cleanup.

## Task 6: Generate and implement store transport

- [ ] Add three exact one-call MATERIALIZED queries and explicit scalar casts.
      Reject every required NULL through proven direct scans or value/presence
      projections. Never silently substitute zero/false/empty for missing data.
- [ ] Request root sqlc generation after SQL/query sources are ready. Inspect
      generated row/parameter types before dependent store implementation. Root
      owns generated models, query methods and interface changes.
- [ ] Write failing store tests for method mapping, every required result field,
      invalid input, query/decode errors and finish/commit ambiguity. Use one
      WriteTxRunner and one generated query per call. Copy inside its callback
      and return exact zero result on every error without an internal retry.
- [ ] Prove real one-call evaluation, NULL rejection for each result attribute,
      output lifetime after connection reuse, rollback versus committed state,
      same-evidence replay after ambiguous completion and physical retirement.
      Keep all driver handles and callbacks private.

## Task 7: Verify and release

- [ ] Run from apps/server with the public fixture DSN:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeMembershipEvidence'
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./internal/store -run '^TestRuntimeMembershipEvidence'
GOGC=50 golangci-lint run ./migrations ./internal/store --tests
```

- [ ] Report exact red/green evidence, role/replay/lock/cleanup proof, changed
      paths, shared-file needs and any boundary in
      `.dev/phase-10/runtime-membership-evidence-author-report.txt`. Release all
      six files. Root inspects/reruns key checks and owns generated/broad gates,
      docs, staging, gitleaks and commit.

Do not add wake, lifecycle actions, fenced-transition recovery, final stop,
cloud calls or caller composition. Report any concrete contract conflict.
