# Task 15: Traceability closure, new AC rows, docs, and handoffs

The master plan rejects a phase plan whose own rows are absent or unresolved,
and names **media/avatar upload** as an ownership gap whose rows "must be minted
during each plan's refresh, before dispatch". This task mints them, fills every
row P2B owns with real test references, and hands the remaining shared-file
edits to the integration owner.

**Tier:** Normal (documentation and matrix maintenance; it ships no code).

**Files:** create `docs/plans/traceability/ac-media.md`; modify
`docs/plans/traceability/{README.md,ac-save.md,ac-sec.md,ac-doc.md}`,
`docs/architecture.md`, and this plan's status index.

## Steps

- [ ] **Step 1: mint the media rows.** Create `ac-media.md` in the established
      one-file-per-prefix shape (header sentence with the row count, a link back
      to `README.md`, then the table with
      `ID | Spec clause | Statement |     Phase/task | Test / UAT reference`)
      carrying AC-MEDIA-001…005 exactly as stated in this plan's README, each
      with its owning task and its real test reference — file path plus test
      name, in the style AC-DOC-003 and AC-SEC-004 already use. AC-MEDIA-005 is
      a **documented boundary, not a built feature**: it records that the
      account avatar has no upload surface in v1 and points at the P1 OAuth
      profile fetch that populates `users.avatar_key`.
- [ ] **Step 2: mint AC-SAVE-005.** Append the customization-allowlist row to
      `ac-save.md` (P2A's D14 flagged this boundary as having no row either
      way), owned by Task 10, with its parity and denial test references.
- [ ] **Step 3: fill the rows P2B owns.** Replace `(pending)` in AC-SAVE-001,
      AC-SAVE-002, and AC-SAVE-004 with the real references from Tasks 4, 9, 13.
      In `ac-sec.md`, resolve AC-SEC-003's "P2B (wiring `sanitize.RichText` into
      every rich-text write path)" clause to Task 5's tests and Task 14's
      independent evidence, and correct the row's closing prose, which today
      says the wiring "remains P3/AC-SEC-001".
- [ ] **Step 4: add HTTP evidence to borrowed rows without re-owning them.**
      AC-DOC-001, AC-DOC-004, AC-DOC-007, AC-DOC-008, AC-DOC-009, AC-DOC-010,
      AC-DOC-011, AC-DOC-012, and AC-SEC-004 stay owned by their phases; append
      the P2B test reference that exercises each at the HTTP boundary, marked as
      such. Do not change any of those rows' `Phase/task` column.
- [ ] **Step 5: fix the index.** `traceability/README.md` gains the `AC-MEDIA`
      prefix row with its count and file link, and its prefix count and total
      are recomputed (today: 12 prefixes, 70 rows). Assert the arithmetic by
      counting rows in the files, not by editing the number in place.
- [ ] **Step 6: update the living documents.** `docs/architecture.md` gains the
      resume HTTP surface and the media storage layer with its two backends.
      This plan's status index records each task's real state. Report — do not
      apply — the `../implementation-plan.md` edits: the phase-status row, and
      striking media/avatar upload from the named traceability gaps.
- [ ] **Step 7: close or reassign the handoffs.** Walk
      [integration-handoffs.md](integration-handoffs.md) row by row; each is
      either applied or has a named owner and a downstream gate. Anything still
      open at the phase gate is listed in the gate report, not silently carried.
- [ ] **Step 8: gate.** `make docs-fmt && make docs-lint` green; every internal
      link in the touched files resolves.
- [ ] **Step 9: commit** —
      `git commit -m "docs(traceability): close the resume HTTP and media acceptance rows" -- docs`

## Acceptance mapping

Every row this phase owns or mints closes here: AC-SAVE-001, AC-SAVE-002,
AC-SAVE-004, AC-SAVE-005, AC-SEC-003 (P2B half), AC-MEDIA-001…005.
