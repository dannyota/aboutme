# P9 acceptance catalog contract

The future `acceptance-catalog-r1.md` and its machine index define expected
results before UAT. Neither contains observed results or a run verdict.

## Ownership and freeze

A fresh acceptance-criteria author owns only
`docs/plans/phase-9/acceptance-catalog-r1.{md,json}`. The Markdown is the
reviewable procedure; the JSON is its closed machine index. The author must not
have implemented P0–P8 product behavior, U1–U10 harness behavior, fixtures, or
evidence tooling. The future executor and reviewer cannot author or edit either.

The integration owner checks the catalog against the approved design, OpenAPI,
accepted ADRs, all applicable traceability rows, and the shipping UI. The owner
does not dispatch the author until the
[U10 evidence-tooling gate](evidence-tooling.md) passes. The owner validates the
JSON against U10's `catalog.schema.json`, verifies exact Markdown/JSON parity,
and records both SHA-256 digests before the first `make uat-up`. After that
freeze, both files are immutable history.

If a criterion is wrong or cannot be satisfied, the run records `BLOCKED`. A
corrected catalog gets a new revision for a later run with its reason recorded;
no one edits r1 or its prior evidence.

## Required row shape

Each numbered row contains:

- one stable `P9-R1-###` ID and every acceptance ID it probes;
- exact deterministic initial fixture and reset state;
- ordered user actions through the public browser origin;
- stable expected-result, observed-result, permitted-state-change, and command
  IDs that fit U10's closed record schema;
- expected visible, accessibility, HTTP, database, cache, and log effects that
  apply;
- permitted state changes and a cleanup rule;
- exact required evidence types and safe fields;
- the U10-supported exporter mode for every required evidence type;
- whether a later product-code change makes the row stale.

The matching JSON row repeats only stable machine fields: criterion and
acceptance IDs; fixture ID; expected-assertion, command, permitted-state-change,
reason, and read-only probe IDs; required raw types and safe export modes; and
the stale-path set. It has no prose, observed value, verdict, credential, URL,
or environment value. Array order follows the Markdown. U10 rejects any runtime
ID or evidence type absent from this frozen row.

The catalog must account for every traceability row owned by the completed P0–P8
web release. A row proved only by an earlier automated gate is linked to that
pinned evidence and gets an explicit P9 non-regression probe when one is
possible. A row cannot disappear because it lacks a browser action. P9A-only
infrastructure drills and post-v1 Flutter rows are named as out of P9 scope
instead of being silently omitted.

## Scenario groups

The author expands these groups into atomic rows:

1. Authentication: all mock providers, linking, reauthentication, denial,
   callback expiry and replay, logout, revoke, and logout-everywhere.
2. Resume lifecycle: empty state, create limit, each section and customization,
   validation and size bounds, delete, and defined recovery.
3. Autosave: coalescing, offline and reconnect, two-tab `412`, idempotent
   replay, and changed-payload key reuse.
4. Rendering and accessibility: `classic-serif`, `engineer-compact`,
   `modern-sidebar`, `executive-band`, `consulting-formal`, and
   `academic-dense`; both display modes; Vietnamese font choices; desktop and
   mobile; keyboard, focus, semantics, axe, contrast; and no third-party font
   request. Phase 3 string goldens cover the other presets.
5. Publishing: slug create, rename, tombstone, disclosure and visibility
   combinations, public SSR and discovery formats, public photo gating, cache
   invalidation, and unpublish absence.
6. Realtime: second-tab refresh, heartbeat, reconnect, polling fallback, and
   stream closure on unpublish.
7. Artifacts: preview/PDF agreement, download gates, owner and public PDF, Open
   Graph image, and template thumbnails.
8. Privacy: export, recent-reauth account deletion, public disappearance,
   private-media removal, and documented backup-retention delay.
9. Security: CSRF, hostile rich text, content security policy and headers, proxy
   spoofing, rate limits, bounded media intake, and no secrets in UI, safe
   evidence, network data, or logs.
10. Recovery: unavailable PostgreSQL, media, renderer, and provider paths;
    history navigation; clear errors; and no unexplained browser or server
    errors.

## Freeze gate

Before freeze, a fresh reviewer checks row independence, acceptance-ID coverage,
fixture availability, deterministic actions, observable expected results,
evidence feasibility against the frozen U10 support matrix, and cleanup. A row
cannot require a raw type, exporter mode, optical-character-recognition path, or
manifest field that U10 did not verify. Missing coverage or an expected result
that relies on an unstated implementation detail blocks the freeze.

The machine-readable support matrix is `scripts/uat-evidence/formats.json`. Its
SHA-256 and both catalog-file digests are part of the freeze record.
