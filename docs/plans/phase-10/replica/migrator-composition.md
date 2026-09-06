# B3 - Dedicated migrator and protected history

Status: B1 is accepted at `a5c47fa` and the version-13 manifest is frozen after
root's live catalog proof. The same fresh reviewer cleared design integration
for bounded local implementation. B2 live runner proof is a separate store task.
No 00014 lands before this composition is proved.

Authorities:
[ADR 0035](../../../adr/0035-replica-coordination-and-uat-lifecycle.md),
[transaction entry](../../../design/scaling/transaction-entry.md),
[dedicated migrator](../../../design/scaling/migrator.md),
[connection retirement](../../../design/scaling/migrator-retirement.md),
[provisioning and adoption](../../../design/scaling/migration-provisioning.md),
[fixed manifest](migrator-manifest.md), and pinned Goose v3.27.3 source.
Acceptance mapping: AC-INF-009 and Task 10.18 write-barrier/migration proof.

## Scope and ownership

One implementation author who designed none of this slice writes failing tests,
implements the complete contract and runs narrow checks. Root assigns exact
paths before dispatch and owns integration, Git and shared-file changes. The
same fresh Sol phase reviewer checks the integrated phase; no per-task review is
added. Stop and report a contract conflict instead of inventing a bypass.

Author-owned proposal:

- apps/server/migrations/migrations.go
- apps/server/migrations/migrator_session.go
- apps/server/migrations/migrator_session_test.go
- apps/server/migrations/migrator_manifest.go and focused tests
- apps/server/migrations/harness_test.go and status_test.go
- apps/server/cmd/migrate/main.go and focused tests

Root/shared paths:

- apps/server/migrations/00014_migrator_enforcement.sql
- apps/server/migrations migration framing/schema tests
- apps/server/internal/testutil/db.go and dbroles.go
- apps/server/migrations/testdb_test.go; cmd/migrate/testdb_test.go
- Makefile, deploy/compose.yml, server Dockerfile, .github/workflows/ci.yml,
  scripts/dev-native.sh, .env.example, hosted task/OpenTofu composition, docs.

Root may explicitly delegate the serialized 00014 migration and its framing
checks to the same author. No other runtime migration can be assigned then. B1
source is stable and not owned by B3; report any required change to root. Shared
helper/caller changes must use the fixed identity API. Test-only raw Goose
callers may remain only when they do not claim production composition.

## Implementation order

1. Freeze and test the exact version-13 manifest after B1 acceptance. Confirm
   current PostgreSQL catalog names, ownership and identity-sequence dependency.
2. Implement fixed identity, pinned connection cleanup, runtime/Goose session
   locker, and read-only Status with failing tests.
3. Implement pre-foundation provisioning and explicit local/test adoption.
   Preserve existing data. Fresh migrator-owned installs need no adoption stop.
4. Add 00014 and same-transaction history assertion. Prove fresh/adopted owner
   convergence and later canonical migration framing through synthetic sources.
5. Root wires commands, local/CI fixtures and manifests with exact identities,
   regenerates sqlc and reruns affected checks. Hosted wiring waits for its
   existing local-proof gate.

Each stage preserves one-backend identity, runtime-before-Goose lock order,
bounded cleanup and no retry. Any contamination, unknown commit/cleanup, reset
failure or backend change destroys the physical connection. Go never reads
owner-only temporary marker rows. Schema CREATE remains available through
owner-role function DDL and is revoked after RESET ROLE, before finish.

Goose cleanup cannot observe the primary migration result. Every migration
backend therefore retires through the pinned pgx driver before Goose returns,
including successful migrations. Bootstrap and protected versions use separate
ApplyVersion lifecycles; no production path uses unlocked Goose discovery.
Capture the exact close-only socket capability before SQL in SessionLock or a
manual pinned operation. It survives ErrBadConn making Raw unavailable. Prove
CleanupDone or exact TCP closure; leave Goose's logical Close to Goose. Every
admitted early return owns retirement. Status and unambiguous read-only adoption
reconciliation may cleanly release their connections under the same PID,
identity, transaction and binding checks. Retain the capability through logical
Close. Status uses fixed SQL transaction commands on its pinned Conn so rollback
receives a detached five-second context. Provision and adoption use the same
bounded server rollback mechanism. Adoption alone may reconcile through one new
read-only connection after confirmed retirement, using the exact evidence in
migration-provisioning.md. Its AdoptionResult distinguishes Applied,
AlreadyConverged and ReconciledConverged; AmbiguousCause retains the original
cause. Convergence never claims which adopter committed. Joined cleanup errors
cannot count as peer convergence.

## Failing-first acceptance matrix

- TestRuntimeLockerSameBackendOrder; TestRuntimeLockerCleanExit;
  TestRuntimeLockerContentionCleanup; TestRuntimeLockerAmbiguityPoisonsBackend;
  TestRuntimeLockerErrBadConnNeverReconnects; TestRuntimeLockerExitTimeoutFails.
- TestMigrationSuccessPhysicallyRetiresBackendAndKeepsDBOpen;
  TestMigrationCommitResponseLostRetiresExactPIDBeforeReturn;
  TestMigrationCommitErrorPreservesPrimaryAndCleanupErrors;
  TestMigrationUnsupportedDriverFailsBeforeMigration;
  TestProtectedApplyVersionUsesFreshPIDAndRechecksEachVersion.
- Prove async cleanup with a gated CancelRequest and a real TCP proxy: the
  original socket stays open after driver.ErrBadConn and Raw ErrConnDone until
  production retirement closes it. Cover all admitted early returns, TLS,
  unsupported transports and clean logical Close without fabricated errors.
- TestBootstrapFreshApplyVersionLocksAndSetsIdentityBeforeHistoryCreate;
  TestBootstrapNeverCallsUnlockedProviderDiscovery;
  TestBootstrapEachVersionUsesFreshRetiredBackend;
  TestBootstrapConcurrentPeerAlreadyAppliedConverges;
  TestBootstrapAlreadyAppliedWithGapOrMismatchFails;
  TestBootstrapAt13DoesNotConstructBootstrapProvider;
  TestBootstrapFailureReturnsPartialAndRetiresBeforeReturn;
  TestBootstrapCommitAmbiguityStopsWithoutRetry.
- TestBootstrapPeerFoundationHandoffRequiresExactCleanErrorTree;
  TestBootstrapAlreadyAppliedCleanupErrorStops;
  TestAdoptAmbiguousCommitRetiresBeforeReadOnlyReconciliation;
  TestAdoptReconciledConvergedPreservesOriginalCause;
  TestAdoptConcurrentLoserAlreadyConvergedDoesNotAdvanceGeneration;
  TestAdoptReconcileCatalogACLHistorySnapshotIsAtomic;
  TestAdoptReconcileFailureRetiresSecondBackend;
  TestAdoptCommitErrorNeverRollsBackOrRetries;
  TestProvisionCanceledUsesDetachedBoundedRollback;
  TestProvisionAmbiguousCommitReturnsCauseWithoutReconnect.
- TestApplyFreshStopsAt13ThenReentersFor14;
  TestApplyFreshMigratorOwnerNeedsNoAdoption;
  TestApplyExistingAboutmePre13StaysAdminOnlyThrough13;
  TestAdoptHistoryOwnerPreservesDataAndRevokesSchemaCreate;
  TestAdoptHistoryOwnerAmbiguousCommitResolvedByReread;
  TestManifestDigestAndExactVersion13Inventory;
  TestManifestMissingExtraMixedOwnerFailsBeforeMutation;
  TestFreshAndAdoptedManifestConvergeRuntimeOwner;
  TestManifestTransferPreservesOIDsRowsDefinitionsAndACLs;
  TestApplyConcurrentBootstrapRechecksAfterGooseLock;
  TestApplyInert13ToProtected14; TestApplyProtectedToNextVersion;
  TestApplyEveryMismatchFailsBeforeWrite; TestApplyNoInternalRetry.
- TestMigration14OwnerTriggerGrantsAtomic; TestMigration14ExactPostFinishInsert;
  TestMigration14WrongVersionDenied; TestMigration14OtherDMLDenied;
  TestMigrationPureDDLAdvancesOnce; TestMigrationRollbackLeavesEnforcementZero.
- TestStatusFreshIsReadOnly; TestStatusPre13IsReadOnly;
  TestStatusInert13DoesNotRequireTrigger; TestStatusProtectedRequiresTrigger;
  TestStatusRepeatableReadCatalogRace; TestStatusCorruptionNeverRepairs.
- TestStatusDirectRejectsAdmin; TestStatusLocalUsesSameBackendAndResets;
  TestStatusCancellationRollsBackAndResets;
  TestStatusResetFailureDiscardsPhysicalConnection;
  TestStatusRollbackFailureRetiresBackendWithoutGoroutine;
  TestStatusReadOnlyCommitErrorRetiresBackend.
- TestDirectIdentityRejectsAdmin; TestLocalIdentityRejectsNonAboutme;
  TestLocalIdentityResetsSession; TestHostedManifestForbidsLocalAdminIdentity;
  TestMigratorMetadataAccessorIsBoundedAndStateTableDenied;
  TestMigrateOutputContainsNoDSNOrDriverDetail.
- TestProtectedMigrationNeverUsesAdminSessionUser;
- Static tests reject 00015+ without exact framing, NO TRANSACTION, Go
  migration, production direct NewProvider, arbitrary role input, and fixture
  migration.

## Verification

The author runs, under apps/server with the public local TEST_DATABASE_URL and
REQUIRE_TEST_DB=1:

```sh
go test -race -count=1 ./migrations ./cmd/migrate
```

Root inspects every owned diff, reruns key proof, then runs
`make server-migration-test server-test-db server-test-integration sqlc-check`.
Root regenerates actual output and owns full `make ci`, connected `make scan`
and the unchanged candidate phase exit. None of these B3 checks has run yet.
Only the existing single capped PostgreSQL container may be used. Disposable
databases are removed by their creator; workers never stop the container.

## Later boundary

R7d follows the separate
[maintenance session contract](../../../design/scaling/maintenance-entry.md).
Its barrier, fixed command lock and writes share one leased backend. B3 does not
add a general session-lock API.
