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

  Inspect `go.mod`, `go.sum`, and `go.work.sum`; run `go mod tidy` and commit
  only the canonical result.

- `mcpapi.NewServer(deps)` wires the go-sdk Streamable HTTP handler in stateless
  JSON mode (single JSON-RPC message per POST, no SSE, no sessions) and
  registers exactly the fifteen M6 tools with schemas generated from Go structs.
  Server instructions text follows M6.
- Extract the per-operation request validation currently inlined in `resumeapi`
  HTTP handlers into package-internal functions (`validate.go`), then call the
  same functions from both the handlers and the tools. REST behavior must remain
  byte-identical — the existing `resumeapi` suites are the proof and may not be
  edited except where a helper name moves.
- Each tool: check scope, apply per-tool input bounds, run the shared
  validation + sanitizer + service call, and map errors onto the closed M6
  vocabulary. Mutating tools take `revision` and return `{ revision, state }`
  with the canonical stored document section. `upload_photo` decodes base64
  first and enforces the existing media ceilings on decoded bytes. No publish,
  unpublish, or public-read code path is reachable.

## TDD cycle

- [ ] Write tool-parity table REDs: for each of the fifteen tools, drive the
      tool and its REST counterpart with the same fixture and assert identical
      stored state, revision behavior, sanitizer output, and bound rejections
      (hostile markup, oversized payload, invalid ids).
- [ ] Write CAS REDs: stale revision through a tool returns `revision_conflict`;
      the returned revision from a mutation chains into the next call.
- [ ] Write scope and cookie-isolation REDs at the `/mcp` handler level (read
      token vs write tool; session cookie ignored).
- [ ] Write a go-sdk client integration RED: a real `go-sdk` client connects to
      an `httptest` server, initializes, lists exactly fifteen tools, and
      completes create → upsert → get with CAS.
- [ ] Write negative REDs: unknown tool, malformed JSON-RPC, batch messages
      (rejected in stateless mode), and `payload_too_large` at the 4 MiB
      boundary.
- [ ] Run the expected RED:

  ```sh
  cd apps/server && go test ./internal/mcpapi -race -count=1
  go test ./internal/resumeapi -race -count=1
  ```

- [ ] Implement extraction first (REST suites stay GREEN), then the server and
      tools; rerun both suites to GREEN, then
      `make server-build server-vet server-test`.

## Adversarial checklist

- Grep-level proof that `publish`, `unpublish`, and public-read service symbols
  are not imported by `mcpapi`.
- A tool cannot address another user's resume by forged UUID (ownership checks
  live in the shared service chain; cross-user matrix included).
- Sanitizer version and output through MCP equal the REST path for the hostile
  corpus sample.
- Tool responses never include internal error text, SQL, or file paths.

## Handoff

Report the tool/REST parity matrix, extraction diff summary, SDK pin diff,
RED/GREEN outputs. Suggested commits:
`build(server): pin modelcontextprotocol go-sdk v1.7.0` and
`feat(mcp): add resume agent tools over streamable http`.
