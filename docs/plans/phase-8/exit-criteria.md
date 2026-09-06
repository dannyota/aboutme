# Phase 8 exit criteria

- [ ] AC-PRIV-001–005 and the remaining Phase 8 portions of AC-MEDIA-003/006/007
      are PROVEN with concrete tests.
- [ ] Account deletion commits every cascade, tombstone, media job, audit and
      discovery change atomically after the shared public drain.
- [ ] Every old public representation is absent after success; rollback restores
      the unchanged state and ambiguous commit never guesses.
- [ ] Export includes complete portable drafts and normalized photos beneath its
      hard cap without credential or backend-key leakage.
- [ ] Hourly deletion/idempotency, weekly orphan and daily retention commands
      enforce bounds, locks, retries, dry run, cursor and fixed output.
- [ ] Deletion, overdue and completion audit records survive account removal;
      180-day expiry preserves pending jobs.
- [ ] Migration, generated SQL/OpenAPI, contract, Go, web, native HTTPS and
      public HTTP checks pass with bounded synthetic evidence.
- [ ] Phase 10 consumes exact commands, schedules and log-retention handoff.
      Hosted activation/alert delivery/backups remain explicitly unrun.
- [ ] One fresh Sol review clears the integrated diff and named invariants.
- [ ] The integration owner runs full `make ci` alone and connected `make scan`
      at one unchanged candidate commit.
- [ ] Living docs and traceability record actual behavior and evidence. The
      committed phase plan is deleted, the phase is pushed, and its PR
      identifies the tested candidate.
