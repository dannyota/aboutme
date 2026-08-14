# Phase 4 implementation decisions

These decisions make the approved Phase 4 contracts executable. Accepted ADRs
and the four approved Phase 4 documents remain authoritative.

## State and validation

### D1 — One current-version runtime validator

The web dependency window exports the existing current schema JSON and aggregate
validator from `@aboutme/schema`. `parseCurrentDocument(value)` runs Ajv 2020-12
with formats, requires `CURRENT_VERSION`, then runs the aggregate validator. No
editor module carries a second handwritten document schema.

### D2 — Branded revision values

`Revision` and `ParentETag` are branded strings created only by `parseRevision`
and `parseParentETag`. Validation uses canonical decimal text and a string
comparison with signed-64-bit max; no `number` or lossy parse participates in
CAS.

### D3 — Validated owner responses

A complete read/write is adoptable only when response ETag, body revision, body
`schemaVersion`, emitted schema header, and validated document agree. A
malformed response produces `response_invalid`, stops the queue, and requests a
complete owner read.

Generated validation issues narrow once at the transport boundary to
`{ path, code }`. Raw envelope and issue messages are discarded there; the store
and UI never receive them.

### D4 — One keyed Pinia store

`useResumeStore` owns a reactive record per resume ID. Each record contains
accepted state, optimistic `current`, one queue, conflicts, issues,
template-group state, and photo-read state. Teardown explicitly removes the
record and releases photo data.

`adoptComplete` returns `adopted` or `older`. An older stored success never
mutates the current winner or drops intent implicitly. The coordinator
reconciles that intent against the winner and exposes supersession instead of
sending another write.

## Commands and mutation

### D5 — Data-only commands

Commands are immutable discriminated data. Pure reducers and projection
functions switch exhaustively on `kind`; a command stores no closure, network
object, Pinia reference, clock, or UUID generator. UUIDs and sequence values are
injected once at the user-action boundary.

`useResumeEditor` is that sole action boundary. Atomic edits and template apply
receive the same injected `EditorRuntime`; group and child IDs are captured
before enqueue, and panels cannot enqueue partial transport objects.

### D6 — Structural equality

Equality is recursive own-property equality: absence differs from `undefined`,
`null`, and `""`; array order matters; numeric crop values use exact JSON-number
equality. Rich text compares the accepted sanitized string.

### D7 — Frozen wire descriptors

The coordinator does not pass mutation objects to an auto-serializer on retry.
`FrozenAttempt` stores exact UTF-8 JSON text, an explicit empty body, or the
accepted `File` bytes plus all semantic headers. Each dispatch creates a new
`Request` from that descriptor without changing its semantic bytes or
idempotency key.

### D8 — Central retry cutoff

`EditorRuntime` injects `nowEpochMs`, `uuid`, and a bounded delay. A first
dispatch fixes `retryCutoff = firstDispatch + 23h`. One automatic same-key
replay is allowed before it; explicit retry may reuse the same attempt before
it. No background loop or retry across the cutoff is permitted.

After cutoff, create remains an explicit Refresh list/Abandon outcome and photo
upload remains an explicit Keep observed photo/Replace photo outcome. Abandon or
Replace discards the expired descriptor; a later intent gets new command and
idempotency IDs.

### D9 — One reconciliation function

Normal dispatch admission, unknown outcomes, `412`, Apply mine, and template
children call the same target then context comparison. Only an accepted named
local dependency may advance a captured projection. A second `412` never
auto-rebases.

### D10 — Bodyless adoption is explicit

Entry/photo child `204` applies only the acknowledged reducer to accepted
document, adopts its parent ETag, marks summary timestamps stale, and schedules
a drain-time complete read. Resume `204` clears the record only after the
outcome is definitive.

## Editor and preview

### D11 — Client-only loading

`/app/resumes/**` is configured as a client-rendered route. Components start
authenticated reads only after `useAuth.authState` resolves. A server-render
test proves no resume request occurs during SSR.

### D12 — Form intent is explicit

Shared controls emit `set`, `clear`, or `unset` based on the generated property
shape. Focus/blur without a value change emits nothing. Entry IDs and
custom-section IDs are generated once before command capture and are never
editable fields.

### D13 — Closed rich-text schema

`richTextSchema` is constructed from the generated sanitizer v1 allowlist and
exposes only the contract's nodes, marks, and link attributes. HTML paste first
calls `sanitizeRichText`, then ProseMirror parses it. Image/file paste and drop
are prevented. Empty content serializes to `""`.

### D14 — Template groups are queue items

A template apply captures the pure `applyTemplate` result, deterministic
placement/delta children, content key/type context, and named dependencies
before preview changes. The queue expands at most two adjacent non-coalescible
attempts and calls a group complete only from one adopted complete revision.

### D15 — Photo bytes stay private and transient

Upload source bytes are held only as the selected `File` for the frozen attempt;
they are not decoded or previewed. Owner-read JPEG/PNG bytes become an in-memory
data URL bound to the accepted photo key and object ETag. Replacement, deletion,
and unmount clear the old string.

Each owner read carries a monotonic generation and accepted-key binding. A late
generation returns without store mutation. Binding mismatch may suspend only
when the same generation is still the store's current loading state.

### D16 — Preview has one adapter

`EditorPreview` catches typed renderer and pagination errors, but passes
`ResumeDocument` only the optimistic current document, total language,
`mode: 'paged'`, and a matching authorized photo URL. It labels page count
`Estimated pages` and never writes it.

The editor-owned observer counts visible `.resume-page[data-page-index]`
elements only when indexes are contiguous from zero and unchanged for two
animation frames. It excludes pagination measurement and hidden/inert pages; any
observed mutation cancels and restarts the frame pair.

### D17 — Stable safe messages

UI text is keyed by error code. Raw server messages, request bodies, object
keys, filenames, dependency errors, tokens, and stack data never render or enter
browser evidence. Unmapped validation paths remain visible in the focused error
summary.

## Integration and scope

### D18 — Cache and Caddy are a hard prerequisite

Before editor commands land, authenticated Go responses and OpenAPI must use
exact `no-store, no-transform`. The existing Caddy `encode` path must prove,
with `Accept-Encoding`, that a strong `"rN"` survives unchanged and returns to
Go unchanged as `If-Match`. A failing parity test is fixed by the integration
owner in Caddy and its route tests before W1.

### D19 — HTTPS UAT extends, not replaces, auth proof

A focused transport spec proves the W0 cache/ETag prerequisite. A later editor
spec proves the whole UI. Both reuse `https://localhost:20443`, the fixed Google
account, trusted CA, closed mounts, and network policy. Auth, transport, and
editor proofs remain independently runnable. This does not claim P9 U1–U5 or
port 443.

### D20 — Optional customization groups require explicit enable

Absent `spacing.pageMargin` displays 15/15 mm and absent `header` displays
left/inline/outline without enqueuing a command. An explicit Enable action
materializes the whole schema-valid object with those visible values before leaf
edits are admitted. Unset removes the whole object through its allowlisted
parent path. Single optional leaves accent, surface, and surfaceTarget use their
own set/unset deltas.

### D21 — No extra product surface

Phase 4 introduces no API route, migration, public page, publish control, SSE
client, print/export behavior, local persistence, cloud resource, or template
identity.

### D22 — Completion records precede the sole fresh review

The owner adds planned Phase 4/AC-EDITOR index entries before dispatch. After
all tasks, the owner integrates architecture, runbook, evidence, acceptance, and
phase-status records and runs preliminary full gates before creating the record
commit. The sole fresh phase review reads that commit. After fixes, definitive
`make ci` and connected `make scan` rerun at the unchanged final candidate. A
failure corrects completion records before review/gates repeat; no pushed commit
silently follows the gated candidate.
