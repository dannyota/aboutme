# Task 13 — MCP caller-controlled idempotency parity

**Acceptance:** AC-MCP-007. Every mutating tool provides the caller-controlled
safe-retry contract required by ADR 0026 and Approved v4 resume write safety.

**Depends on:** T08 tool/facade implementation; T12 proof harness.

**Author:** One MCP author owns the implementation paths below. The integration
owner owns the design, phase-plan, traceability, and live-harness windows. No
other task edits these paths concurrently.

## Files

- Modify: `apps/server/internal/resumeapi/validate.go`.
- Test: `apps/server/internal/resumeapi/agent_test.go`.
- Modify:
  `apps/server/internal/mcpapi/{instructions.go,tools_lifecycle.go,tools_content.go,tools_photo.go,tools_read.go}`.
- Test: `apps/server/internal/mcpapi/{tools_test.go,server_test.go}`.
- Modify and live-test: `deploy/dev-https-browser/mcp.spec.ts`.
- Integration-owner records:
  `docs/{design/api.md,plans/mcp-agent-access-design.md}`,
  `docs/plans/phase-pm/{README.md,decisions.md,exit-criteria.md,file-structure.md,task-08-mcp-server-tools.md}`,
  and `docs/plans/traceability/ac-mcp.md`.

## Interfaces

`resumeapi.AgentCall` gains the caller key:

```go
type AgentCall struct {
    Operation      AgentOperation
    IdempotencyKey string
    ResumeID       string
    Revision       string
    SectionKey     string
    EntryID        string
    Payload        json.RawMessage
    File           []byte
}
```

Every one of the twelve mutating MCP tool inputs adds this required field. The
three read tools (`list_resumes`, `get_resume`, and `get_photo`) do not:

```go
IdempotencyKey string `json:"idempotency_key" jsonschema:"caller-generated UUID reused only for an exact retry"`
```

`ExecuteAgent` forwards `AgentCall.IdempotencyKey` unchanged as the internal
`Idempotency-Key` header. It never generates a replacement. The existing REST
parser and idempotency transaction remain the only validator and persistence
path. The key is MCP transport metadata and must not enter the semantic REST
JSON payload; in particular, `create_resume` marshals only `title`, `lng`, and
`document`. Read tools accept no idempotency key.

Error mapping stays inside the frozen M6 vocabulary: missing, malformed, or
changed-fingerprint keys map to `validation_failed`; the existing
idempotency-capacity response remains `rate_limited`. Exact replay returns the
retained successful mutation response without executing the mutation again.

## TDD cycle

- [x] **Step 1: write MCP schema and error-mapping REDs**

  Add `TestServer_MutatingToolSchemasRequireIdempotencyKey` to assert that all
  twelve mutating schemas require `idempotency_key` and the three read schemas
  omit it. Add `TestServer_MutatingToolsForwardIdempotencyKey` as a table over
  the twelve tool calls and assert each recorded `AgentCall` carries the exact
  input key. The create case also asserts that the forwarded JSON payload omits
  `idempotency_key`. Extend `TestToolErrorMapIsClosed` so
  `409 idempotency_key_reuse` expects `validation_failed`; retain the existing
  `429 rate_limited` mapping for idempotency-capacity exhaustion.

- [x] **Step 2: run the MCP REDs**

  ```sh
  cd apps/server && go test ./internal/mcpapi -race -count=1 \
    -run 'MutatingToolSchemasRequireIdempotencyKey|MutatingToolsForwardIdempotencyKey|ToolErrorMapIsClosed'
  ```

  Expected: FAIL because mutation schemas omit the field, calls cannot forward
  it, and idempotency-key reuse maps to `agent_access_unavailable`.

- [x] **Step 3: write resume-facade REDs**

  Start the shared database with `make test-db-up`; leave it running for the
  integration owner.

  Add these live-database tests in `agent_test.go`:

  - `TestExecuteAgent_RequiresCallerIdempotencyKey`: a missing key returns the
    existing idempotency-key validation error and writes no resume or record.
  - `TestExecuteAgent_ReplaysCallerIdempotencyKey`: two identical creates with
    one key return the same created resume and leave one resume and one retained
    idempotency record.
  - `TestExecuteAgent_ReplaysExistingMutation`: two identical metadata updates
    with one key return the same response and advance the revision only once.
  - `TestExecuteAgent_RejectsChangedIdempotencyFingerprint`: a second call with
    the same operation/key and changed semantic input returns
    `idempotency_key_reuse` and leaves both resume and retained response
    unchanged.

- [x] **Step 4: run the facade REDs**

  ```sh
  cd apps/server && REQUIRE_TEST_DB=1 \
    TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' \
    go test ./internal/resumeapi -race -count=1 \
      -run 'ExecuteAgent_(RequiresCallerIdempotencyKey|ReplaysCallerIdempotencyKey|ReplaysExistingMutation|RejectsChangedIdempotencyFingerprint)'
  ```

  Expected: FAIL because `ExecuteAgent` replaces the missing caller key with a
  new UUID on every mutation.

- [x] **Step 5: implement the smallest parity change**

  Add the required field to each mutation input, pass it into `AgentCall`,
  forward it unchanged in `ExecuteAgent`, add the closed reuse-error mapping,
  and update server instructions to tell clients to reuse a key only for an
  exact retry and choose a new key for changed intent. Do not add another
  idempotency store, table, error code, or OpenAPI operation.

- [x] **Step 6: run the focused GREEN gates**

  ```sh
  cd apps/server && go test ./internal/mcpapi -race -count=1
  REQUIRE_TEST_DB=1 \
    TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' \
    go test ./internal/resumeapi -race -count=1
  cd ../.. && make server-build server-vet server-test server-test-p2b
  ```

  Expected: PASS. The twelve mutation schemas require the key, reads do not,
  exact create and update replays mutate once, and changed-fingerprint reuse
  writes nothing.

- [x] **Step 7: extend the live proof**

  Give every logical mutation in `mcp.spec.ts` its own UUID key and retain the
  key for a retry. Retry `create_resume` immediately with the same key and
  identical arguments; assert the same resume ID and revision return and only
  one resume exists. Keep the M9 evidence schema unchanged.

  Run alone:

  ```sh
  make dev-native-down
  make dev-https
  make dev-https-status
  make dev-https-mcp-check
  make dev-https-down
  make dev-native
  ```

  Expected: all M9 steps remain true, all error counters remain zero, the replay
  returns the first create result, and fixture cleanup removes the one resume
  and every OAuth row.

- [x] **Step 8: update records and hand off**

  Update AC-MCP-007 from PLANNED to PROVEN only after the focused and live
  checks pass. Report the twelve mutating tool schema names, facade replay/reuse
  results, exact GREEN commands, M9 proof size/mode/counters, and any unrun
  check. Suggested commit:
  `fix(mcp): preserve idempotency across agent retries`.
