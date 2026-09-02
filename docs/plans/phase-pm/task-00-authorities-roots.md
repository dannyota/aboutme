# Task 00 — Authorities, budgets, traceability, and v6 roots

**Acceptance:** AC-MCP-001…010 rows created as PLANNED; no behavior lands.

**Depends on:** Approved spec `../mcp-agent-access-design.md`; PA complete.

**Owned paths:** T00 paths in `file-structure.md`. Integration owner alone.

## Contract

Reconcile the authorities so no operative text contradicts the spec:

- Write and accept `docs/adr/0026-mcp-agent-access.md`: context (v1 ships
  bring-your-own-agent access), decision (remote Streamable HTTP MCP with a
  first-party OAuth 2.1 authorization server in the Go binary; editor parity
  minus publish; account-wide scopes), consequences (new public roots, four
  tables, bearer world beside the cookie world), alternatives rejected
  (PAT-only, separate service, per-resume grants).
- Amend Approved v4: `product.md` (agent access is a v1 capability; publishing
  stays human-only), `api.md` (bearer world endpoints and the OpenAPI ruling
  from `decisions.md`), `security.md` (OAuth token model, cookie isolation,
  scope enforcement), `data.md` (four bounded tables, digest-only storage),
  `web.md` (consent page, connected agents), `decisions.md` design index.
- Add the M5 rows to `docs/design/budgets.md` with `PM` enforcement column
  values.
- Create `docs/plans/traceability/ac-mcp.md` with AC-MCP-001…010 in state
  PLANNED and add the prefix to the traceability README index and phase table.
- Regenerate public roots v5 → v6 adding exactly four registry rows, one per
  top-level segment (the registry rejects duplicate roots, so the finer paths
  dispatch inside the Go routers, following the `/api` pattern): `.well-known`
  (Go), `oauth` (Go), `mcp` (Go), and `authorize` (Nuxt). Update every generated
  consumer and the runtime v6 references named in `file-structure.md`.

## TDD cycle

- [x] Extend the public-roots generator/registry tests RED for the four new root
      rows, v6 filename, and unchanged existing dispatch.
- [x] Run the expected RED:

  ```sh
  node scripts/generate-public-roots.mjs --check
  cd apps/server && go test ./internal/publicroots ./internal/routetable
  ```

- [x] Land registry v6 + regenerate consumers; rerun to GREEN.
- [x] Write the ADR, design amendments, budgets rows, and traceability rows.
- [x] Verify formatting and route contracts:

  ```sh
  make docs-fmt
  make route-table-test
  bash scripts/dev-https-test.sh --static
  ```

## Adversarial checklist

- No root overlaps or shadows an existing public root; `/authorize` does not
  collide with `/oauth/authorize` dispatch.
- Grep proves no remaining operative design text says agents or MCP are out of
  scope, and none says agents may publish.
- The budgets table and `decisions.md` M5 numbers are byte-consistent.

## Handoff

Report the ADR number, amended files, registry diff summary, GREEN outputs, and
the traceability row list. Suggested commits:
`feat(publicroots): add oauth, mcp, and consent roots (v6)` and
`docs: adopt MCP agent access authorities`.
