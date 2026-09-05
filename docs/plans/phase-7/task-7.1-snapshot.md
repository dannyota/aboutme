# Task 7.1.2: Frozen print snapshots

Build the [private print payload](print-contract.md#frozen-payload) from an
authorized owner resume or an admitted public snapshot. Authority: ADR 0023,
security design, public projection rules, and task 7.1. Acceptance:
`AC-PDF-002`, `AC-PDF-004`, and `AC-SEC-001` print sanitization.

Owner: one Sol author. Exclusive paths:

- `apps/server/internal/printsnapshot/*.go`.
- `apps/server/internal/publicresume/projection.go` and a new
  `apps/server/internal/publicresume/print_projection_test.go`.

Do not change other files, manifests, generated files, Git, or database state.

## Interfaces

Export
`publicresume.ProjectDocument(source schema.Resume, photoURL string) PublicResumeDocument`.
It performs the existing visible-only projection and Go rich-text sanitization.
An empty photo URL removes the photo; a supplied URL replaces its private key
while preserving crop. Refactor existing `Project` to call this helper without
changing public JSON behavior or bytes.

`printsnapshot` exports `Envelope` with the exact JSON fields in the contract,
and these functions:

```go
func FromOwner(source resume.Resume, photo []byte, contentType string) (Envelope, error)
func FromPublic(source publicresume.Snapshot, photo []byte, contentType string) (Envelope, error)
func Marshal(source Envelope) ([]byte, error)
```

The public source was already projected and sanitized by `Reader.ReadResume`.
Copy it before replacing its photo URL. Owner input uses `ProjectDocument`.
Enforce all stated byte, identifier, revision, schema, language, photo and crop
bounds. Reject unsupported or mismatched image types and unexpected photo bytes.
Never read object storage, authorize an account, or load a resume here.

## Test cycle

- [ ] Write and run a failing owner snapshot test with hidden contact/entry,
      hostile rich text, normalized photo, and a private photo key.
- [ ] Implement the projection and frozen envelope.
- [ ] Assert owner and public output contain only visible sanitized fields and
      data photos. An edit to input after preparation cannot change the
      envelope.
- [ ] Cover missing/mismatched/oversize photo, invalid crop, unknown schema,
      missing/noncanonical ID, nonpositive revision, invalid legacy language
      fallback, public-generation mismatch, maximum payload, and one byte above
      every bound.
- [ ] Verify existing public projection bytes and source optional-state
      semantics are unchanged. Marshal emits only the six closed fields with
      public generation null for owner and the revision string for public.
- [ ] Run from the repository root:

```sh
flock --close .dev/phase-7/heavy.lock sh -c \
  'cd apps/server && go test -race -count=1 ./internal/printsnapshot ./internal/publicresume'
```

Report failing-first evidence, files, exact passed checks, and open issues. The
integration owner inspects edits and reruns the key check. No Git operations.
