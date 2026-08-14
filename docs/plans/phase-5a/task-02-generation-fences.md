# Task 02 — Generation fences, leases, and recovery state

**Owner:** Fence author in W1.

**Acceptance:** AC-PUB-004 and AC-SEC-001 primitives. This file is the sole
named-test owner catalog for all 22 ADR 0022 race rows; no other task owns or
renames these test cases.

**Authorities:** `revocation.md` in full, ADR 0022, `design.md`,
`public-contract.md`, ADR 0017, and `docs/plans/budgets.md`.

**Files:** Task 02's five new publicstate implementation files and their
same-basename tests from `file-structure.md`; Task 11 owns readiness files.

**Interfaces:** Produces the exact Step 1 fence/lease/transition/recovery API.
Consumes the initial durable discovery generation and injected clock only.

## Step 1 — RED the complete publicstate contract

- [ ] Add deterministic state-machine tests for open/closing/closed/retired,
      lazy exact-revision initialization, global startup state, mismatch, single
      ownership, cancellation, idempotent lease release, old-generation
      retention, recovery, and invariant/readiness failures. Use injected
      clocks/channels; no sleeps or retries.
- [ ] Compile this exact producer surface; Tasks 05, 07, and 11 repeat it
      verbatim as their consumer contract:

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

- [ ] Run RED:

  ```sh
  (cd apps/server && go test ./internal/publicstate/... -race -count=1)
  ```

  Expected: the package and contract do not exist.

## Step 2 — GREEN the coordinator

- [ ] Implement exact admission, generation identity, synchronous cancel hooks,
      and release-based join. Missing resume fences accept only a database-read
      revision. No error/metric contains slug, content, key, or body.
- [ ] `Begin` acquires global first, then UUID bytes. Non-draining closes no
      leases. Revoking/global closes cancel all active retained generations.
      `Close` spends one caller-supplied absolute five-second deadline; timeout
      runs no database callback and reopens the exact unchanged state.
- [ ] Definite commit/rollback opens only supplied proof. Ambiguous state stays
      closed; `Recover` can only read proof and never rerun mutation. Mixed or
      unavailable evidence stays unready.
- [ ] Register response abort by `ResponseController.SetWriteDeadline(now)` and
      wait for handler `Release`.
- [ ] Run GREEN:

  ```sh
  (cd apps/server && go test ./internal/publicstate/... -race -count=1)
  (cd apps/server && go test ./internal/publicstate/... -race -count=20 -run 'Test(Coordinator|Lease|Transition|Recovery)')
  ```

## Executable RED → GREEN checkpoints

Do not batch the two sections above.

- [ ] Lease RED: add `TestCoordinatorAcquireExactRevision` that calls
      `AcquireResume(ctx, id, 7, RepresentationJSON)`, expects a lease, and
      expects `GenerationMismatchError{Expected: 6, Actual: 7}` for revision 6.
      Run
      `(cd apps/server && go test ./internal/publicstate -race -count=1 -run TestCoordinatorAcquireExactRevision)`
      and observe the missing package. GREEN: implement `NewCoordinator`,
      `AcquireResume`, and idempotent `Lease.Release`; rerun the same command.
- [ ] Drain RED: add `TestOneDeadlineCoversAllFencesAndHandlers` with injected
      channels and one fixed deadline; run the same package command with
      `-run 'Test(OneDeadline|NonDraining|LaterRevocation|DiscoveryChange)'` and
      observe active leases are not joined. GREEN: implement `Begin`, `Close`,
      cancel hooks, global-first/UUID-byte order, and unchanged-state reopen;
      rerun the same command with `-race -count=20`.
- [ ] Recovery RED: add `TestAmbiguousEvidenceControlsAdmissionAndReadiness`,
      run the package command with `-run 'Test(Ambiguous|DefiniteOutcome)'`, and
      observe unknown state reopens. GREEN: implement `Commit`, `Rollback`, and
      `Recover` so unresolved proof returns `RecoveryUnresolvedError` and stays
      closed; rerun the same command.

## Sole 22-row named-test catalog

| Row | Exact test name                                                  | Implementing task |
| --- | ---------------------------------------------------------------- | ----------------- |
| 1   | `TestAllRevisionMutationRoutesCloseAdmission`                    | 07                |
| 2   | `TestPublicReadMismatchRetriesOnceThenUnavailable`               | 05                |
| 3   | `TestNonDrainingCommitLetsOldLeaseFinishAndRejectsNewOldLease`   | 02                |
| 4   | `TestLaterRevocationDrainsRetainedGenerationSets`                | 02                |
| 5   | `TestRevokingPublishAndDeleteCancelEveryRepresentation`          | 07                |
| 6   | `TestDiscoveryChangeDrainsAllGlobalGenerations`                  | 02                |
| 7   | `TestCheapPreflightFailuresDoNotTouchFences`                     | 06                |
| 8   | `TestTransactionalRecheckRaceReopensExactOldGenerations`         | 07                |
| 9   | `TestOneDeadlineCoversAllFencesAndHandlers`                      | 02                |
| 10  | `TestDrainTimeoutBeginsNoTransactionAndReopens`                  | 07                |
| 11  | `TestDefiniteOutcomeOpensOnlyExactState`                         | 02                |
| 12  | `TestAmbiguousEvidenceControlsAdmissionAndReadiness`             | 02                |
| 13  | `TestDeleteReplayPrecedesExpiredReauthAndFence`                  | 07                |
| 14  | `TestDeleteRollbackAndCommitProofAreAtomic`                      | 07                |
| 15  | `TestContentContendersSerializeThenLoserGetsFresh412`            | 07                |
| 16  | `TestConcurrentReclaimWinnerRollbackAndLaterReleaseTime`         | 01                |
| 17  | `TestCrossRenameLocksSlugBytesWithoutOverwrite`                  | 01                |
| 18  | `TestGlobalThenUUIDOrderSupportsThreeResumeCaller`               | 02                |
| 19  | `TestCacheAndConditionalHitAcquireLeaseFirst`                    | 05                |
| 20  | `TestRestartGenerationAndInvalidationFailureCannotRestoreAccess` | 11                |
| 21  | `TestOriginAdmissionBoundaryAroundCompletedEdgeValidation`       | 11                |
| 22  | `TestSameKeyContenderRechecksBeforeCAS`                          | 03                |

## Completion

- [ ] Return the exact handoff report and all 22 test locations/results.
- [ ] Suggest commit: `feat(public): add generation fence coordinator`.
