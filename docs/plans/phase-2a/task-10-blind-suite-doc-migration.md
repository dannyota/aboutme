# Task 10: Blind adversarial suite B — doc-migration purity and CAS-vs-autosave races

Same independence protocol as Task 9, **different fresh instance** (not Task 9's
author, not Tasks 5–8's author). Inputs: spec §3 doc-migrations bullet +
wire-version row, D12/D13/D18 as written contracts, Task 8's Interfaces block.
Withheld: all `internal/resume` implementation and author tests. This is the
master plan's named "**CAS-vs-autosave race tests**" obligation.

**Files:** create `apps/server/internal/resume/docmigrate_adversarial_test.go`.

Minimum matrix:

| Test                                             | Assert                                                                                                                                                    |
| ------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TestGet_NeverWrites`                            | hammer `Get`/`List` on an old-version row concurrently; row bytes, `revision`, `updated_at` unchanged throughout (projection-only, under concurrency)     |
| `TestBackfill_LosesToConcurrentAutosave`         | interleave: backfill reads (vOld, rev R) → autosave commits rev R+1 → backfill CAS → 0 rows, `BackfillLostRace`, autosave's doc intact at current version |
| `TestAutosave_AfterBackfill_NoSpurious412`       | backfill applies (revision unchanged, D12) → autosave with pre-backfill revision R still succeeds — the exact user-visible property D12 exists to protect |
| `TestBackfill_ConcurrentWithItself_AppliesOnce`  | N concurrent `BackfillOne` on one row → one `BackfillApplied`, rest skipped/lost; final state valid, revision unchanged                                   |
| `TestBackfill_NeverPersistsInvalidProjection`    | synthetic converter emitting an invalid doc → error, row untouched                                                                                        |
| `TestProjection_UnknownStoredVersionFailsClosed` | stored version with no converter path → error from `Get`, never a silently un-projected doc                                                               |
| `TestList_OneBadProjectionFailsAtomically`       | one unprojectable row among valid rows → `nil, err`; no partial list or silent omission                                                                   |
| `TestWireConverters_BothDirectionsFailClosed`    | independently exercise synthetic v1⇄v2, old-client preparation, down-emission, source/target validation, and every missing-path arm                       |

- [ ] **Step 1 (blind author): write from the contracts; run; findings to the
      implementer.**
- [ ] **Step 2: gate.** Live-DB command, `-race -count=20` on the race cases.
- [ ] **Step 3: commit** —
      `git commit -m "test(resume): add adversarial doc-migration and backfill race suite" -- apps/server/internal/resume/docmigrate_adversarial_test.go`
