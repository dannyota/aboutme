# Task 07 — Mutation transitions, publish, delete, and recovery

**Owner:** Transition author in W3.

**Acceptance:** AC-PUB-001/002/004 integration. This task implements coverage
for Task 02's rows 1, 5, 8, 10, and 13–15; Task 02 owns the names.

**Authorities:** All Phase 5A approved docs, ADR 0016, ADR 0019, ADR 0022, and
Phase 2B integration handoffs.

**Files:** The Task 07 row in `file-structure.md`. Task 06 owns its new policy
files; do not edit fences, idempotency, migrations/sqlc, or public reads.

**Interfaces:** Consumes Task 01 SQL, Task 02 transition API, Task 03 `Recheck`,
and Task 06 policy names exactly. Produces transition-aware `resumeapi.Service`,
publish registration, and a recovery resolver.

Extend `resumeapi.Options` with the exact composition seam below. Task 11 reads
the durable discovery generation, creates one coordinator, and injects the same
pointer into mutation, public-read, and readiness consumers. Recovery uses the
pool to take a new connection after an ambiguous commit. A missing dependency
fails closed before transition or database work; code never guesses a
generation.

```go
type Options struct {
  // Existing fields remain unchanged.
  Coordinator *publicstate.Coordinator
  RecoveryPool *store.Pool
}
```

## Step 1 — RED every revision mutation and transition outcome

- [ ] Make a route inventory assertion cover every existing metadata, entry,
      section, structure, personal-details, customization, photo, publish, and
      delete revision mutation. Add deterministic blockers at preflight,
      transition ownership, drain, recheck, transaction, and commit outcome.
- [ ] Compile directly against this verbatim Task 02 consumer contract; do not
      alias or wrap it:

  ```go
  type Representation string
  const (
    RepresentationJSON Representation = "json"
    RepresentationPhoto Representation = "photo"
    RepresentationHTML Representation = "html"
    RepresentationMarkdown Representation = "markdown"
    RepresentationSitemap Representation = "sitemap"
    RepresentationRobots Representation = "robots"
    RepresentationLLMS Representation = "llms"
  )
  type TransitionClass uint8
  const ( NonDraining TransitionClass = iota; Revoking )
  type ResumeTarget struct { ID uuid.UUID; ExpectedRevision int64; Class TransitionClass }
  type Plan struct { DiscoveryGeneration *int64; Resumes []ResumeTarget }
  type CommittedState struct { DiscoveryGeneration *int64; ResumeRevisions map[uuid.UUID]int64; RetiredResumes []uuid.UUID }
  type RecoveryDisposition uint8
  const ( RecoveryCommitted RecoveryDisposition = iota + 1; RecoveryNotCommitted )
  type RecoveryProof struct { Disposition RecoveryDisposition; State CommittedState }
  type RecoveryResolver interface { Resolve(context.Context) (RecoveryProof, error) }
  var ErrAdmissionClosed = errors.New("publicstate: admission closed")
  type GenerationMismatchError struct { Expected int64; Actual int64 }
  func (e *GenerationMismatchError) Error() string
  type DrainTimeoutError struct { Deadline time.Time }
  func (e *DrainTimeoutError) Error() string
  type RecoveryUnresolvedError struct { Cause error }
  func (e *RecoveryUnresolvedError) Error() string
  func (e *RecoveryUnresolvedError) Unwrap() error
  type CoordinatorConfig struct { DiscoveryGeneration int64; Now func() time.Time }
  func NewCoordinator(CoordinatorConfig) (*Coordinator, error)
  func (c *Coordinator) Ready() error
  func (c *Coordinator) AcquireResume(
    ctx context.Context, id uuid.UUID, expected int64, rep Representation,
  ) (*Lease, error)
  func (c *Coordinator) AcquireDiscovery(
    ctx context.Context, expected int64, rep Representation,
  ) (*Lease, error)
  func (c *Coordinator) Begin(context.Context, Plan) (*Transition, error)
  func (l *Lease) Context() context.Context
  func (l *Lease) OnCancel(func()) error
  func (l *Lease) Release()
  func (t *Transition) Close(context.Context, time.Time) error
  func (t *Transition) Commit(CommittedState) error
  func (t *Transition) Rollback() error
  func (t *Transition) Recover(context.Context, RecoveryResolver) error
  ```

- [ ] Compile directly against this verbatim Task 03 consumer contract:

  ```go
  type StoredResponse struct {
    Status int
    Body json.RawMessage
    Headers map[string]string
  }
  type CommitOutcome uint8
  const (
    CommitNotAttempted CommitOutcome = iota
    CommitDefinitelyRolledBack
    CommitCommitted
    CommitUnknown
  )
  type ExecuteResult struct {
    Response StoredResponse
    Replayed bool
    Outcome CommitOutcome
  }
  type RecheckDecision uint8

  const (
    RecheckFresh RecheckDecision = iota
    RecheckReplay
    RecheckReuse
  )

  type RecheckResult struct {
    Decision RecheckDecision
    Response StoredResponse
  }

  func (s *IdempotencyStore) Recheck(
    ctx context.Context,
    userID uuid.UUID,
    operation string,
    key uuid.UUID,
    requestHash [32]byte,
  ) (RecheckResult, error)
  func (s *IdempotencyStore) Execute(
    ctx context.Context,
    userID uuid.UUID,
    operation string,
    key uuid.UUID,
    requestHash [32]byte,
    mutate func(qtx *store.Queries) (StoredResponse, error),
  ) (ExecuteResult, error)
  ```

  Compile `var _ store.PublicMutationQueries = (*store.Queries)(nil)` and call
  Task 01's exact `LockPublicState`, `AdvanceDiscoveryGeneration`,
  `LockSlugClaim`, claim/tombstone, `PublishResumeCAS`, and
  `DeleteResumePublicCAS` signatures through the `*store.Queries` passed to the
  existing `Execute` callback. Do not add a second transaction wrapper.

  Consume Task 06's exact same-package contract verbatim:

  ```go
  type optionalSlug struct { Present bool; Value string }
  type publishInput struct {
    Slug optionalSlug
    Live bool
    DownloadEnabled bool
    SEOGeoEnabled bool
  }
  type currentPublish struct {
    Slug *string
    Live bool
    DownloadEnabled bool
    SEOGeoEnabled bool
    Revision int64
  }
  type publishPrepared struct {
    Effective currentPublish
    ChangedSlug bool
    Issues []publishIssue
  }
  type publishIssue struct { Path string; Code string; Message string }
  type publishShapeError struct { Field string }
  func (e *publishShapeError) Error() string
  func decodePublish(io.Reader) (publishInput, error)
  func validatePublish(schema.Resume, currentPublish, publishInput) publishPrepared
  type slugAttemptLimiter interface {
    AllowChangedSlug(accountID uuid.UUID, now time.Time) bool
  }
  ```

- [ ] Test exact wire order: auth/CSRF/Origin/syntax/fingerprint; retained
      replay before delete reauth/fence; cheap preflight; transition ownership;
      fresh recheck; one shared close deadline; final `Execute` transaction.
- [ ] Test non-draining for every existing revision mutation. Test revoking for
      rename, unpublish, route disable, and slug-bearing delete; discovery-
      changing operations also drain every global generation. Drain timeout
      begins no transaction and restores unchanged readability.
- [ ] Test bytewise old/new slug locks, tombstones, claim collisions, fresh 412
      after contender serialization, exact 30/hour limiter order, exact publish
      response, bodyless delete, and exact replay.
- [ ] Test definite commit/rollback and unknown outcome. Recovery reads retained
      idempotency plus database proof, never reruns mutation, opens only exact
      committed/not-committed state, and leaves mixed/unavailable state closed
      and readiness-failing.
- [ ] Run RED:

  ```sh
  (cd apps/server && go test ./internal/resumeapi/... -race -count=1 -run 'Test(AllRevisionMutation|PublishTransition|Delete|TransactionalRecheck|DrainTimeout|ContentContenders|Recovery)')
  ```

  Expected: route inventory is not transition-complete and publish is absent.

## Step 2 — GREEN one transition template

- [ ] Implement one internal helper used by every revision mutation:

  ```go
  type mutationIdentity struct {
    UserID uuid.UUID
    Operation string
    Key uuid.UUID
    RequestHash [32]byte
  }
  type mutationPlan struct {
    Fence publicstate.Plan
    Mutate func(
      context.Context, *store.Queries,
    ) (resume.StoredResponse, publicstate.CommittedState, error)
    ReplayState func(
      context.Context, resume.StoredResponse,
    ) (publicstate.CommittedState, error)
    Recover publicstate.RecoveryResolver
  }
  func (s *Service) runMutation(
    context.Context, mutationIdentity, mutationPlan,
  ) (resume.ExecuteResult, error)
  ```

  `runMutation` calls `IdempotencyStore.Recheck` from `mutationIdentity`, then
  closes the transition. It calls existing `Execute` with a wrapper that passes
  its `*store.Queries` into `Mutate` and retains the returned committed state.
  `Replayed` resolves state through `ReplayState`; `CommitCommitted` commits the
  retained state; `CommitNotAttempted`/`CommitDefinitelyRolledBack` roll back;
  `CommitUnknown` invokes `Recover`. No route duplicates or bypasses that order.

- [ ] Integrate Task 06 publish policy, Task 01 lock/claim/tombstone/proof SQL,
      existing idempotency authority, revision CAS, and photo-job transaction.
      Register publish and preserve all existing API errors/headers/auth rules.
- [ ] Attach every backend/render/viewer cancel hook through Task 02 and join by
      lease release before mutation. Never use cache invalidation as authority.
- [ ] Run GREEN:

  ```sh
  (cd apps/server && go test ./internal/resumeapi/... -race -count=1)
  make server-build server-vet server-test
  ```

## Executable RED → GREEN checkpoints

- [ ] Inventory RED: implement Task 02 catalog row 1 with an explicit table of
      every registered revision-writing route and expected
      `NonDraining`/`Revoking` plan; run
      `go test ./internal/resumeapi -race -count=1 -run 'Test.*RevisionMutationRoutes'`
      from `apps/server` and observe existing routes bypass `runMutation`.
      GREEN: add the shown helper and route every table row through
      `Recheck → Begin → Close → Execute`; rerun the command.
- [ ] Publish RED: add `TestPublishTransition` for unchanged, rename, unpublish,
      route-disable, lock collision, 180-day boundary, and 30/hour; run the
      package test with
      `-run 'Test(PublishTransition|TransactionalRecheck|ContentContenders)'`
      and observe no route/transaction. GREEN: register publish, map Task 06
      typed policy results, and call Task 01 lock/tombstone/CAS queries only
      inside the existing `Execute` callback; rerun that command.
- [ ] Delete RED: implement Task 02 catalog rows 13 and 14; run the package test
      with `-run 'TestDelete(Replay|Rollback)'`. GREEN: classify slug-bearing
      delete as revoking, return a bodyless
      `StoredResponse{Status: http.StatusNoContent}`, and atomically persist
      tombstone/media job/generation/idempotency proof; rerun that command.
- [ ] Recovery RED: make commit return `CommitUnknown` while durable proof is
      committed, absent, mixed, then unavailable; run the package test with
      `-run 'Test(DrainTimeout|Recovery)'`. GREEN: map committed/not-committed
      proof into `Transition.Recover`, return `RecoveryUnresolvedError` for
      mixed/unavailable proof, and never rerun `Mutate`; rerun that command,
      then `make server-build server-vet server-test`.

## Completion

- [ ] Return route inventory, transition class per route, recovery predicates,
      and exact report.
- [ ] Suggest commit: `feat(publish): integrate mutation transition protocol`.
