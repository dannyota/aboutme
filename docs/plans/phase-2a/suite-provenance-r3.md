# Phase 2A revision 3 suite provenance

Verdict: **acceptance independence remains supportable**. Three isolated
original work units produced Suites A, B, and C. On 2026-08-12, three named
fresh work units independently rederived and froze the required matrices before
inspecting the suites or production code. Later fixes followed those frozen
findings and received independent review.

This is process evidence, not cryptographic proof of what an author saw. Git
records commits and file contents under the repository owner's identity. It does
not identify the agent behind a commit or prove withheld context. The original
agent display names and exact derivation times were not preserved. This ledger
therefore identifies the recoverable task/worktree names and fails closed on
claims the record cannot support.

## Original isolated derivation

The preserved external ledger is
`.superpowers/sdd/phase-2a-resume-store/suite-provenance.md`. The briefs remain
beside it as `task-9-brief.md`, `task-10-brief.md`, and `task-11-brief.md`.
These ignored records attest that the work units were fresh, distinct, and
isolated from the Tasks 5–8 implementation author and from one another.

| Order | Suite | Worktree/task       | Allowed inputs                                                                                            | Withheld inputs                                                            | Delivered commit                           | Delivered SHA-256                                                  |
| ----- | ----- | ------------------- | --------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- | ------------------------------------------ | ------------------------------------------------------------------ |
| 1     | A     | `task9-writesafety` | Written data/write-safety contracts, budgets, named traceability rows, and Tasks 6–7 interface signatures | `internal/resume/*.go`, author tests, `sql/queries.sql`                    | `a3deaa8c36700127a67a9950cdb9b77a8926bab8` | `8bbc5726f63a0e4b6dc410fa01619a73459f9b3577dec143159f75fedacec035` |
| 2     | B     | `task10-docmigrate` | Written document-migration and wire contracts plus Task 8 interfaces                                      | All `internal/resume` implementation and author tests                      | `b94f3e6493943a616c6263db77363770f134f342` | `e28971b2dd70dd03e8d545071efccab187f89e26565932c921ce2d14afc137ee` |
| 3     | C     | `task11-bounds`     | Budgets, design section 3, frozen resume schema, and Task 5 interface signatures                          | `bounds_test.go`, `validate.go`, and all other Task 5 implementation files | `b6f1ee0d1f15da7133ff887ba8ecec8559b78fd4` | `c3d473e903ff9a9666018d051d4b8b6d4c49dbf52ca371dff4f40f8e9cb30829` |

The commit timestamps are 2026-08-03 in the listed order. They are delivery
times, not derivation times. The exact pre-inspection derivation times are not
recoverable. The original commits are retained on local refs but are not
ancestors of the current main-line squash. The original ledger records their
integration through `f1556f1` and `d40894e`, followed by mechanical suite lint
edits in `2f6d730`.

The `2f6d730` edit grouped imports, removed an unused Suite A helper, renamed
shadowing locals, checked rollback/close assertions, and changed Suite C's type
probe to `errors.As`. It did not deliberately change a matrix row. The later
exact-type correction to Suite C is disclosed below.

Suite B's original review corrections were incorporated before `b94f3e6` and
strengthened document equality, projected output, and converter source
validation. Suite C's original author could not be resumed. Before integration,
the integration owner applied reviewer-directed corrections for direct frozen
schema reading, removal of a disallowed decode seam, exact layout error paths,
and required top-level test names. The Git commits do not identify those agent
authors, so this ledger does not assign personal authorship.

## Fresh rederivation on 2026-08-12

The preserved orchestration messages record the sequence below. No wall-clock
time survives. Each Stage 1 froze its matrix before Stage 2 gained broader read
access. A frozen derived row did not necessarily become a separate top-level
test; Stage 2 mapped the rows to the current suite and reported gaps.

### Suite A — `p2a_suite_a_rederive_final`

Stage 1 read only `AGENTS.md` and the current Task 9. It did not read
implementation, tests, SQL, or Git history. It froze these 13 rows:

1. `TestCreate_SequentialCap_NoWriteOnFourth`
2. `TestCreate_Concurrent_ExactlyThreeSucceed`
3. `TestCreate_RawSQLBypass_StillCapped`
4. `TestCreate_ConcurrentRawSQLBypass_StillCapped`
5. `TestSaveDocument_ConcurrentSameRevision_OneWinner`
6. `TestSaveDocument_MismatchCarriesWinningDoc`
7. `TestIdempotency_ConcurrentSameKey_OneMutationCommits`
8. `TestIdempotency_ConcurrentDifferentKeys_SerializePerUser`
9. `TestIdempotency_MutationErrorRollsBack`
10. `TestIdempotency_DifferentBodyNeverExecutes`
11. `TestValidation_RejectionWritesNothing`
12. `TestValidation_SaveTitle_RejectionWritesNothing`
13. `TestNoExistenceOracle_WrongUserSameAsNotFound`

Stage 2 inspected only the authorized Suite A and production paths. Its exact
count-1 gate and five `-race -count=20` stress cases passed. It still reported
partial comparison of the complete CAS `Current` value, forbidden atomic
callback side effects, and incomplete no-oracle return-shape comparison.

Commit `2fa55202eed5c77b9656efb000b7b110f0f99062` then strengthened Suite A.
`p2a_final_defect_review` found that the different-body callback could execute
and roll back invisibly. Commit `8f02b2436238151d69481d766fcbbe61050a369d` made
invocation observable through a sentinel result. A later final phase review
reported that blocker closed. Git attributes both commits to the repository
owner; the orchestration author task for the first fix is not recoverable, so no
stronger authorship claim is made.

### Suite B — `p2a_suite_b_rederive_final`

Stage 1 read only `AGENTS.md` and the current Task 10. It read no other path,
Git history, or test. It froze these 26 rows:

1. `Get_NeverWrites`
2. `Project_PureAndDeterministic`
3. `Projection_UnknownStoredVersionFailsClosed`
4. `Projection_InvalidStoredSourceFailsClosed`
5. `List_OneBadProjectionFailsAtomically`
6. `Backfill_LosesToConcurrentAutosave`
7. `Backfill_LosesToConcurrentTitleChange`
8. `Autosave_AfterBackfill_NoSpurious412`
9. `Backfill_ConcurrentWithItself_AppliesOnce`
10. `Backfill_AlreadyCurrentSkips`
11. `Backfill_NeverPersistsInvalidProjection`
12. `Backfill_UnknownStoredVersionFailsClosed`
13. `Backfill_CASScopesID`
14. declaration immutability and no inference
15. adjacent-pair completeness
16. upward conversion success
17. downward conversion success
18. identity conversion
19. missing path or direction
20. missing schema
21. unknown or undeclared versions
22. invalid source or result
23. old-client preparation through `AcceptWire`
24. supported down-emission through `EmitWire`
25. lossy emission
26. concurrent projector use

Stage 2 ran the pure and live Suite B gates successfully. It found that identity
`Convert` contradicted the validation contract, accepted and emitted sets were
not tested independently, and schema-valid optional-field loss was neither
tested nor rejected.

`p2a_suite_b_fix` changed production and both author/adversarial tests together
in `890463e2ccbca55fe544926799e8798e39bcc332`. It made identity `Convert`
validate, separated accepted/emitted-set assertions, and added reverse semantic
loss detection. Independence rests on the matrix frozen before this mixed edit,
not on fixer blindness. `p2a_suite_b_rereview` then found that JSON numbers were
compared by token spelling. `p2a_suite_b_number_fix` added a red/green exact
`big.Rat` semantic-number test and production fix in
`e760afc8c44651bdb625e84d25a13ab9ec3f55eb`; it did not edit the adversarial
Suite B files. The resumed `p2a_suite_b_rereview`, which did not write the fix,
returned final PASS.

### Suite C — `p2a_suite_c_rederive_final`

Stage 1 read only `AGENTS.md` and the current Task 11. It froze a direct schema
walk of every `maxLength`, `maxItems`, and `maxProperties`, plus these
independent groups: total document bytes, sections, entries, personal details,
strings and rich-text UTF-8 bytes, completeness against Task 5, and exact
concrete `*ValidationError` at limit+1 with nil at the limit.

Stage 2 inspected only the seven authorized paths. It independently counted 38
schema declarations: 26 `maxLength`, 11 `maxItems`, and one `maxProperties`. It
matched the Task 5 inventory and checked exact errors, rich-text UTF-8 bytes,
and the canonical 512 KiB pair. Build, vet, focused, and full tests passed. It
made no edits.

The current file was restored by suite-only commit
`c67e47d2b16761c43d3e157e007969775c8738ff`. The prior `task11_review` had found
an extra blank line and that a `2f6d730` `errors.As` lint change weakened the
exact-type assertion. The restored file keeps `errors.As` and also compares
`reflect.TypeOf` to require an unwrapped concrete `*ValidationError`.
`p2a_suite_c_rederive_final` approved that current behavior.

## Current-line edit ledger

This table covers every commit that changes a current Suite A/B/C path on the
current main line, plus production-only follow-up required by a suite review.

| Date       | Commit                                     | Suite effect                                                                                    | Production and suite together?                                                                                              |
| ---------- | ------------------------------------------ | ----------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| 2026-08-11 | `f7ce41b7852f131fc5862e9b925cc793b857de03` | Added current Suite A and the split live/pure Suite B files from the independently derived work | **Yes.** The squash also added projection, backfill, store, codec, and author tests.                                        |
| 2026-08-12 | `b230e9650bd81061e99768843a89cde35d587005` | Mechanically migrated Suite A/B comments away from retired design paths and decision labels     | **Yes.** This repository-wide documentation-path migration touched production comments but did not change suite assertions. |
| 2026-08-12 | `c67e47d2b16761c43d3e157e007969775c8738ff` | Restored Suite C and its exact concrete error-type check                                        | No; suite only.                                                                                                             |
| 2026-08-12 | `2fa55202eed5c77b9656efb000b7b110f0f99062` | Strengthened complete CAS payload, rollback, idempotency, and no-oracle assertions              | No; Suite A only.                                                                                                           |
| 2026-08-12 | `890463e2ccbca55fe544926799e8798e39bcc332` | Corrected Suite B identity and lossy-emission assertions                                        | **Yes.** The production converter and author tests changed in the same commit.                                              |
| 2026-08-12 | `e760afc8c44651bdb625e84d25a13ab9ec3f55eb` | No adversarial-suite byte changed; added the reviewer-required exact numeric author test        | Production and author test only.                                                                                            |
| 2026-08-12 | `8f02b2436238151d69481d766fcbbe61050a369d` | Made rejected callback execution observable                                                     | No; Suite A only.                                                                                                           |

The `f7ce41b` squash does not retain the original per-file commit ancestry and
its Suite A/B bytes are not identical to the original delivery commits. The
fresh pre-inspection rederivations are therefore required evidence; the old
commit names alone cannot establish the current suites' independence.

`git log --all` also shows `2df6f1d73fd22977a36b4dbd27d6a92a2aa3d4a2` editing
the old combined Suite B file on a side branch. It is an ancestor of neither the
prior accepted `ba03f64` candidate nor current HEAD, so none of its bytes enter
the current suite identity.

## Current identity and acceptance verification

The reconstruction base is `2f19340927c941bb9d8062eae852dc2154907d99`. The suite
paths had no working-tree diff at reconstruction. Their SHA-256 identities were:

| Suite   | Current path                                                             | SHA-256                                                            |
| ------- | ------------------------------------------------------------------------ | ------------------------------------------------------------------ |
| A       | `apps/server/internal/resume/writesafety_adversarial_test.go`            | `60ddd06a5c827a39fbbaaef669d5ea502a39dbbe5ff87374afb66c1e83be637a` |
| B, live | `apps/server/internal/resume/docmigrate_adversarial_suiteb_test.go`      | `de1459a85f8f7269e0fe9522cb0df196a59542ab0b66843fd079dadd8c1acba3` |
| B, pure | `apps/server/internal/resume/docmigrate/suiteb_wire_adversarial_test.go` | `ccb25182f4276f5c81a20737e9fa7b18220a9bf9628171f976ef2710b210ab6c` |
| C       | `apps/server/internal/resume/bounds_adversarial_test.go`                 | `800ea28ba769e4d32b8297469cc83f3d658e8aee8f7798941d8e423132005125` |

At acceptance, run these commands at the clean candidate. A hash mismatch,
unexplained history entry, missing retained ref, or conflict with this ledger is
`BLOCKED`, not an inferred pass.

```sh
git rev-parse HEAD
git status --short --branch
sha256sum \
  apps/server/internal/resume/writesafety_adversarial_test.go \
  apps/server/internal/resume/docmigrate_adversarial_suiteb_test.go \
  apps/server/internal/resume/docmigrate/suiteb_wire_adversarial_test.go \
  apps/server/internal/resume/bounds_adversarial_test.go
git log --format='%H%x09%ad%x09%s' --date=iso-strict -- \
  apps/server/internal/resume/writesafety_adversarial_test.go \
  apps/server/internal/resume/docmigrate_adversarial_suiteb_test.go \
  apps/server/internal/resume/docmigrate/suiteb_wire_adversarial_test.go \
  apps/server/internal/resume/bounds_adversarial_test.go
git log --all --follow --format='%H%x09%ad%x09%s' --date=iso-strict -- \
  apps/server/internal/resume/writesafety_adversarial_test.go
git log --all --follow --format='%H%x09%ad%x09%s' --date=iso-strict -- \
  apps/server/internal/resume/docmigrate_adversarial_test.go
git log --all --follow --format='%H%x09%ad%x09%s' --date=iso-strict -- \
  apps/server/internal/resume/bounds_adversarial_test.go
git merge-base --is-ancestor a3deaa8c36700127a67a9950cdb9b77a8926bab8 ba03f64402123c57bdf389aeb685788f5cc67d36
git merge-base --is-ancestor b94f3e6493943a616c6263db77363770f134f342 ba03f64402123c57bdf389aeb685788f5cc67d36
git merge-base --is-ancestor b6f1ee0d1f15da7133ff887ba8ecec8559b78fd4 ba03f64402123c57bdf389aeb685788f5cc67d36
```

Residual uncertainty is limited but real: the original author identities,
withheld context, and Stage 1 wall-clock times are process attestations. The
fresh pre-inspection rederivations and independent fix reviews preserve the
intended adversarial separation despite the disclosed mixed commits. They do not
turn that attestation into cryptographic evidence.
