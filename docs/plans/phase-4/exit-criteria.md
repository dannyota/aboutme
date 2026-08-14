# Phase 4 exit criteria

Every item passes at one unchanged candidate commit. A wrong or unsatisfiable
criterion is corrected in this phase, with the change noted.

## Product and state

- [ ] `/app/resumes` lists only summaries after browser authentication, handles
      an empty list, creates the server's blank current-v2 document, opens a
      resume, and routes rename/delete through a complete read plus the common
      mutation queue. Delete confirms the owner-visible title.
- [ ] `/app/resumes/{id}` performs no authenticated SSR fetch, validates a
      complete current-v2 response and exact body-revision/ETag/schema-header
      agreement, and gives missing and wrong-owner IDs one unavailable state.
- [ ] Accepted document, accepted revision, metadata freshness, optimistic
      current state, pending, in-flight, conflicts, issues, and photo-read state
      remain distinct. A child `204` never fabricates a complete response or
      timestamp; accepted revision never decreases. An older stored success
      returns its current winner, is reconciled without another write, and
      cannot drop intent silently.
- [ ] Every user action captures immutable target/base/intended/context data
      before optimistic replay. Only adjacent unsent same-target value commands
      coalesce, preserving the first base. Named local dependencies alone may
      advance captured projections. Presence is unconstrained while projection
      values and context keys form one explicit closed union. Atomic/template
      IDs use one injected runtime at the `useResumeEditor` boundary.
- [ ] Each resume has one queue and at most one in-flight write; different
      resumes save independently. One second of inactivity triggers dispatch.
      Later work stops behind unknown, failed, partial, conflicted, or
      session-lost heads and is never called saved.
- [ ] Every exact retry preserves idempotency key, precondition, schema version,
      operation, target, and semantic bytes. CSRF refresh changes only the token
      and retries once. Unknown outcomes have at most one automatic replay,
      never cross the 23-hour cutoff, and after cutoff offer only create
      Refresh/Abandon or photo Keep observed/Replace with new IDs for new work.
- [ ] Valid `412` details use the winning revision/document for shared
      intended/base/context reconciliation. A safe rebase uses one new attempt
      and key; a second `412`, malformed details, context change, or same-target
      change stops visibly. Conflict actions obey field, entry, reorder,
      structure, crop/photo, and destructive reconfirmation rules.

## Editing and rendering

- [ ] Personal details, contacts, section metadata, and every generated field
      for profile, work, education, skill, language, certificate, project, and
      custom entries are editable. Absence, clear, unset, zero, and array order
      remain distinct; no publish completeness rule runs.
- [ ] Structure controls are keyboard operable and use only the structure API
      for section create/delete/move/reorder. Entry reorder uses section
      metadata. Column count is a customization leaf and preserves placement.
- [ ] Every generated customization leaf is exposed with its enum/bound and an
      explicit unset control where optional. Fallbacks are shown without being
      written. No customization delta reaches `layout.sections`.
- [ ] ProseMirror exposes only the closed sanitizer-v1 nodes, marks, link
      attributes, and toolbar actions. Hostile HTML is sanitized before parse;
      unsafe links, unsupported markup, images, and file paste/drop cannot enter
      the model. Empty content serializes as `""`.
- [ ] Template apply uses `applyTemplate` against optimistic state, preserves
      content, emits the deterministic placement/delta diff, waits for named
      dependencies, and becomes saved only from one complete final revision.
      No-change, warnings, partial recovery, and guarded undo match the group
      contract.
- [ ] Photo upload sends source bytes unchanged and never previews or decodes
      them. Owner JPEG/PNG reads are conditional and bind an in-memory data URL
      to the exact accepted photo key. Replacement clears crop; read failure or
      binding mismatch suspends preview without removing metadata or forms. A
      stale read generation returns without any store mutation.
- [ ] Preview imports the Phase 3 pure renderer, updates immediately from
      optimistic state, uses paged mode and total language, renders the exact
      visible label **Estimated pages**, and counts only a two-frame-settled
      visible contiguous `data-page-index` set excluding measurement pages. It
      never derives a photo URL from an object key. Renderer failure cannot
      change accepted state.
- [ ] Errors and validation issues use safe stable text, preserve unmapped
      paths, discard raw server messages at transport, and focus the summary
      when the handled action fails. Labels, live regions, dialogs, panels,
      reorder, crop, templates, and preview mode meet the keyboard/focus
      contract; automated accessibility checks find no serious or critical
      violation in the main flow.
- [ ] Session loss retains pending/in-flight/failed/partial/conflicted work,
      stops dispatch, and resumes after authentication in another tab. Discard
      is explicit. Route/unload prompts appear exactly while unsafe-to-leave
      work exists, and no local browser storage contains resume data.
- [ ] Component and native tests instrument localStorage, sessionStorage,
      IndexedDB, history/URL query/hash, and sendBeacon before and after edit
      and session loss. They prove no editor persistence, query/hash encoding,
      unload beacon, or direct non-transport request occurs.

## Authenticated transport and browser proof

- [ ] Every authenticated response uses exact
      `Cache-Control: no-store, no-transform`. With `Accept-Encoding` through
      native Caddy, an owner `GET` returns exact strong `"rN"`; the next
      mutation sends that unchanged value as `If-Match` and Go receives it.
- [ ] `make dev-https-transport-check` and `make dev-https-editor-check` each
      pass once without retry at `https://localhost:20443`, using the
      deterministic Google account, Secure host-only `__Host-` cookies, trusted
      project CA, fixed origin-only network policy, and shared development
      database.
- [ ] Browser scenarios prove create/load/logout; immediate preview, one-second
      save, and reload; safe stale rebase and same-target conflict; dedicated
      entry/reorder/photo conflict actions; photo lifecycle and suspended
      preview; template final-state/undo/forced partial; session-loss retention
      and reauthentication; keyboard flow and automated accessibility.
- [ ] Browser teardown deletes only resume IDs created by that run, leaves the
      shared database running, emits no certificate/page/unexpected console or
      external-network error, and writes only bounded secret-free verdict
      evidence under the ignored native HTTPS state root. A fixed-account
      resume-cap error fails safely and never deletes an unrecorded resume.

## Checks and review

- [ ] `make schema-check api-check server-build server-vet server-test` passes.
- [ ] `make web-lint web-typecheck web-test web-build` passes.
- [ ] Every Task 00–15 final gate passes. Native harness static tests,
      `bash scripts/test/makefile-safety-test.sh`,
      `make route-table-test operational-test`, and both live Phase 4 targets
      pass.
- [ ] A fresh non-author reviews the integrated diff for behavior, design fit,
      interface stability, and traceability, and confirms authentication,
      sessions, CSRF, sanitizing, CAS, idempotency, unknown outcomes, template
      partial state, media privacy, renderer purity, persistence boundary,
      accessibility, and secret-free evidence by name. The same reviewer
      confirms every fix.
- [ ] Planned indexes landed before dispatch. Architecture, runbook, AC
      evidence, exit status, implementation-plan completion, and traceability
      status are in the record commit reviewed by the sole fresh reviewer.
      AC-EDITOR-001…017 are `PROVEN`; full P9 and excluded scope remain pending.
- [ ] The integration owner runs `make ci` alone and connected `make scan` at
      the same unchanged final record commit after review. No commit or record
      edit follows those gates before push. Per-commit gitleaks passes for every
      local commit. No cloud, DNS, registry, staging, or port-443 mutation runs.
