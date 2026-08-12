# Task 5: Rich-text sanitization at the HTTP write boundary

Closes the **P2B half of AC-SEC-003**: the traceability row assigns P3 the
allowlist, the hostile corpus, and both sanitizer implementations, and assigns
P2B "wiring `sanitize.RichText` into every rich-text write path". P3's own Task
2 says the same from the other side. Implements [D5](decisions.md): one choke
point, not one call per handler.

**Tier:** High risk (rich-text sanitization).

**Hard prerequisite:** `apps/server/internal/sanitize` from P3 Task 2 is this
task's direct code dependency and must be landed and green. P3's remaining work
and phase gate do not block this task.

**Files:** create
`apps/server/internal/resumeapi/{sanitize_doc.go,sanitize_doc_test.go}`. Task 4
owns the call site in `persist.go`; this task owns the function.

> **Wave 2 lands as one unit** with Task 4: `persist.go` calls
> `sanitizeDocument`, which only this file defines.

## Interfaces

```go
package resumeapi

// sanitizeDocument returns doc with every rich-text field replaced by
// sanitize.RichText of its value. It is called by the kernel's persist
// helper on the assembled current-version document, after conversion and
// before validation, so a field can never reach storage unsanitized and a
// sanitized field is what the bounds are measured against.
//
// The set of rich-text fields is derived from the schema, not hardcoded:
// see richTextPaths and its completeness test.
func sanitizeDocument(doc schema.Resume) schema.Resume
```

## Steps

- [ ] **Step 1: failing completeness test first.** Walk `packages/schema`'s
      embedded raw schema for every property whose subschema is the `richText`
      definition, and assert `richTextPaths` contains exactly that set — no
      more, no fewer. Adding a rich-text field to the schema must fail this test
      until the walk covers it. This is the guard that makes "every rich-text
      write path" a checkable claim rather than a promise; today that set is
      `profile.text`, `work.description`, `education.description`,
      `skill.infoHtml`, `certificate.description`, `project.description`, and
      `custom.description` — derived, not transcribed.
- [ ] **Step 2: failing behavior tests.** A hostile payload from P3's shared
      corpus in an entry's rich text is neutralized in the persisted document; a
      benign fragment is unchanged; sanitization is **idempotent**
      (`sanitizeDocument(sanitizeDocument(d)) == sanitizeDocument(d)`); a
      `nil`/absent field stays absent and an empty string stays empty (the
      [resume aggregate](../../design/data.md#resume-aggregate) gives those
      states different storage meaning, so sanitization must not fabricate or
      drop either); a hidden entry is sanitized like any other.
- [ ] **Step 3: failing order test.** Sanitization runs **before** validation
      and the size bounds: construct rich text that is under the 16 KB byte
      bound only after sanitization strips a hostile wrapper, and assert it is
      accepted; construct one that is over the bound after sanitization and
      assert `422 document_invalid`. Measuring bounds on unsanitized input would
      let a payload pass a check it does not satisfy in storage.
- [ ] **Step 4: failing discriminating test.** Removing the `sanitizeDocument`
      call from the kernel's persist helper must make a test fail. Assert this
      at the HTTP boundary — write hostile rich text through a real request and
      read it back through a real `GET` — so the guard covers the wiring, not
      just the function. Without this the invariant is structural but unguarded,
      the blind spot P2A's Task 8 Step 3c documents.
- [ ] **Step 5: implement; green.**
- [ ] **Step 6: gate.** Run `make test-db-up`,
      `make server-build server-vet server-test`,
      `(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable}" go test ./internal/resumeapi/... -race -count=1)`,
      and the focused sanitizer package tests. Connected `make scan` runs once
      at the unchanged phase candidate.
- [ ] **Step 7: handoff.** Report the owned paths, failing-test evidence, exact
      checks, and rich-text completeness set to the integration owner. Do not
      stage or commit.

**Phase-review focus:** At W4, the one fresh phase reviewer checks that every
schema-declared rich-text field reaches the sanitizer exactly once before
validation and storage. The same reviewer confirms fixes.

## Acceptance mapping

| Row        | What this task contributes                                                     |
| ---------- | ------------------------------------------------------------------------------ |
| AC-SEC-003 | The entire P2B half: `sanitize.RichText` wired into every rich-text write path |
| AC-SEC-001 | Adds the HTTP-boundary surface to P3's four-surface evidence set               |
| AC-DOC-007 | Bounds are measured on the sanitized document, closing the order gap           |
