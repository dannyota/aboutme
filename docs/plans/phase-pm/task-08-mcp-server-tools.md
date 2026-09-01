# Task 08 — MCP server, fifteen tools, shared-validation refactor

**Acceptance:** AC-MCP-006, AC-MCP-007.

**Depends on:** T07 bearer boundary; existing `resumeapi` service chain.

**Owned paths:** T08 paths in `file-structure.md`. Integration owner (the go-sdk
pin and the `resumeapi` extraction are owner windows).

## Contract

- Owner opens W4 with the dependency pin:

  ```sh
  cd apps/server && go get github.com/modelcontextprotocol/go-sdk@v1.7.0
  ```

  Inspect `go.mod`, `go.sum`, and `go.work.sum`; run `go mod tidy` afterwards
  (the hosted CI tidy-is-a-no-op gate requires canonical files) and inspect that
  diff too before committing.

- `mcpapi.NewServer(deps)` wires the go-sdk Streamable HTTP handler in stateless
  JSON mode (single JSON-RPC message per POST, no SSE, no sessions) and
  registers exactly the fifteen M6 tools with schemas generated from Go structs.
  Server instructions text follows M6.
- Add a closed in-process `resumeapi` agent facade in `validate.go`. It builds
  an internal request for one of the fifteen allowed operations and dispatches
  directly to the existing handler, so REST and MCP execute the same validators,
  sanitizer, bounds, and store callbacks without a second validation path. REST
  behavior must remain byte-identical; the existing `resumeapi` suites are the
  proof and may not be edited except where a helper name moves.
- Generalize the write-safety principal: `resumeapi/writesafety.go` today
  sources identity from `auth.SessionFromContext` and re-fetches the session row
  inside the write transaction (`transactionMutation` → `GetSessionByID`) before
  any mutation commits. Introduce a closed principal type carrying either a
  session (REST) or a token row (MCP); the mid-transaction recheck re-fetches
  the matching authority row — the session for REST, the token joined to its
  grant for MCP — and fails closed (`auth.ErrSessionInvalid` / `mcpapi`
  `agent_access_unavailable` mapping) when it is revoked, expired, or
  superseded. REST semantics are unchanged and the existing suites prove it; the
  token path gets its own mid-transaction revocation race test.
- Each tool: check scope, apply per-tool input bounds, run the shared
  validation + sanitizer + service call, and map errors onto the closed M6
  vocabulary. `create_resume` omits revision; every existing-resume mutation
  takes the decimal-string `revision`. Successful create/update operations
  return `{ revision, state }` with the complete canonical stored resume;
  `delete_resume` returns the matched revision and `{ id, deleted: true }`.
  `upload_photo` decodes base64 first and enforces the existing media ceilings
  on decoded bytes. A write grant may delete a private or published resume. A
  published delete uses the existing public revocation fence and cleanup path,
  but no standalone publish, unpublish, or public-read tool is reachable.

## TDD cycle

- [x] Write tool-parity table REDs: for each of the fifteen tools, drive the
      tool and its REST counterpart with the same fixture and assert identical
      stored state, revision behavior, sanitizer output, and bound rejections
      (hostile markup, oversized payload, invalid ids).
- [x] Write CAS REDs: stale revision through a tool returns `revision_conflict`;
      the returned revision from a mutation chains into the next call.
- [x] Write scope and cookie-isolation REDs at the `/mcp` handler level (read
      token vs write tool; session cookie ignored).
- [x] Write a write-safety principal RED: a token revoked after admission but
      before the transaction body commits no write (the MCP analog of the
      session recheck), and the REST session recheck behavior is unchanged.
- [x] Write a go-sdk client integration RED: a real `go-sdk` client connects to
      an `httptest` server, initializes, lists exactly fifteen tools, and
      completes create → upsert → get with CAS. Note: the SDK's stateless
      handler 400s unless the request `Accept` header contains both
      `application/json` and `text/event-stream`; the client sends it, and a
      negative test pins the 400 for a JSON-only `Accept`.
- [x] Write negative REDs: unknown tool, malformed JSON-RPC, batch messages
      (rejected in stateless mode), and `payload_too_large` at the 4 MiB
      boundary.
- [x] Run the expected RED:

  ```sh
  cd apps/server && go test ./internal/mcpapi -race -count=1
  go test ./internal/resumeapi -race -count=1
  ```

- [x] Implement the shared facade first (REST suites stay GREEN), then the
      server and tools; rerun both suites to GREEN, then
      `make server-build server-vet server-test`.

## Adversarial checklist

- Grep-level proof that `publish`, `unpublish`, and public-read service symbols
  are not imported by `mcpapi`.
- A tool cannot address another user's resume by forged UUID (ownership checks
  live in the shared service chain; cross-user matrix included).
- Sanitizer version and output through MCP equal the REST path for the hostile
  corpus sample.
- A revoked, expired, or superseded token cannot commit a mutation whose
  transaction was already open when the revocation landed.
- Tool responses never include internal error text, SQL, or file paths.

## Handoff

Report the tool/REST parity matrix, extraction diff summary, SDK pin diff,
RED/GREEN outputs. Suggested commits:
`build(server): pin modelcontextprotocol go-sdk v1.7.0` and
`feat(mcp): add resume agent tools over streamable http`.
