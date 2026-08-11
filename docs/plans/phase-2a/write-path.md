# The write path this phase builds

```mermaid
flowchart TD
    A[P2B handler - future] -->|schema.Resume + expected revision| B[resume.Store]
    B --> C[canonical marshal - D16]
    C --> D[JSON-Schema validate vs embedded resume.schema.json - D1]
    D --> E[total doc &le; 512 KB - D10]
    E --> F[schema.ValidateDocument: rich-text bytes, layout, dates, URL schemes + entry-id uniqueness - D3]
    F --> G{valid?}
    G -->|no| H[ErrDocumentInvalid with issues]
    G -->|yes| I[tx: CAS UPDATE ... WHERE id AND user_id AND revision = expected]
    I -->|1 row| J[revision + 1 returned]
    I -->|0 rows| K[re-read: ErrNotFound or ErrRevisionMismatch with current doc + revision]
```

Backfill vs autosave (the D12 race, proven in Tasks 8/10):

```mermaid
sequenceDiagram
    participant BF as Backfill job
    participant DB as resumes row (v_old, rev 7)
    participant AS as Autosave (CAS rev 7)
    BF->>DB: read schema_version=v_old, revision=7
    AS->>DB: UPDATE ... SET doc(v_cur), revision=8 WHERE revision=7
    DB-->>AS: 1 row - autosave wins, doc now current version
    BF->>DB: UPDATE ... WHERE schema_version=v_old AND revision=7
    DB-->>BF: 0 rows - BackfillLostRace: retryable (observation stale; re-observe and retry)
```
