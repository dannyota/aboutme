# The HTTP write path this phase builds

Every mutation in this phase takes exactly this path. The store half (everything
below `resume.Store`) is P2A's and is shown only where P2B binds to it — see
[`../phase-2a/write-path.md`](../phase-2a/write-path.md).

```mermaid
flowchart TD
    A[request] --> B[RequestID / SecurityHeaders / Logging]
    B --> C[NoStoreCache then RateLimit then BodyLimit]
    C --> D[RequireSession then RequireCSRF]
    D --> E{Idempotency-Key present and a UUID?}
    E -->|no| E1[400 idempotency_key_required / _invalid]
    E -->|yes| F{If-Match required and well formed?}
    F -->|missing| F1[428 precondition_required]
    F -->|malformed| F2[400 precondition_malformed]
    F -->|ok| G[strict JSON decode - P2A D16 ingress guard]
    G -->|unknown field / bad JSON| G1[400 request_invalid]
    G --> H{X-Resume-Schema-Version accepted?}
    H -->|no| H1[400 unsupported_schema_version]
    H -->|yes| I[IdempotencyStore.Execute: hash raw body]
    I -->|replay| I1[stored response, byte-identical]
    I -->|different body| I2[409 idempotency_key_reuse]
    I -->|leader| J[tx: read resume by id + user_id]
    J -->|absent or wrong owner| J1[404 resume_not_found]
    J --> K[EmitWire down to caller version - D4]
    K --> L[apply the delta]
    L --> M[AcceptWire up to current - D4]
    M --> N[sanitize rich text - D5]
    N --> O[validate + bounds at current version]
    O -->|invalid| O1[422 document_invalid with details.issues]
    O --> P[CAS write of the FULL document at If-Match revision]
    P -->|0 rows| P1[412 revision_mismatch with details.revision + document]
    P -->|1 row| Q[store idempotency record in the same tx]
    Q --> R[200/201 + EmitWire response at caller version]
```

Three properties this diagram is drawn to make checkable:

- **Nothing is written before every bound is checked.** Validation, sanitization
  and the size bounds all run inside the transaction and before the CAS
  statement, so a rejected request leaves the row byte-identical. The blind
  bounds suite asserts exactly that (row equality before and after).
- **The idempotency record and the mutation share one transaction.** A replay
  returns the stored response without re-running the mutation; a rolled-back
  mutation leaves no record to replay. This is P2A's primitive, unchanged — P2B
  supplies the route string and the SHA-256 of the **raw** request body.
- **The caller's wire version bounds both ends.** The delta is applied at the
  caller's declared version and the response is emitted at it, while storage
  only ever holds a complete current-version document.

## The read path

```mermaid
flowchart LR
    A[GET] --> B[RequireSession]
    B --> C[Store.Get / List - projection only, never writes]
    C --> D[EmitWire to the caller's declared version]
    D --> E[200 with data + ETag: r-revision + X-Resume-Schema-Version]
```

Reads never write: projection is pure (P2A D18), and the response's `ETag` is
the revision in the same `"r<n>"` form the client sends back as `If-Match`, so a
client never has to construct that string from a JSON field itself.

## What "granular" means here

Spec §5's autosave model is: keystroke → store → debounce ~1 s → **one coalesced
PATCH**. The endpoints are granular so that a coalesced save touches one entry,
one section, the personal-details object, or a customization delta list — not
the whole document. Storage is still whole-document (D15), so "granular" is a
property of the request, never of the write.

Structural mutations are the exception the spec calls out: creating, deleting,
moving, or reordering a **section** changes `content` and
`customization.layout.sections` together, and the exactly-once placement
invariant must never be observably broken. Those go through
`PATCH /resumes/{id}/structure` alone, which applies the whole command or none
of it.
