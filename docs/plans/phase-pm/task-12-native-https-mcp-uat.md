# Task 12 — Native HTTPS MCP UAT

**Acceptance:** AC-MCP-010; end-to-end confirmation of 001–009.

**Depends on:** T00–T11 complete and focused-GREEN.

**Owned paths:** T12 paths in `file-structure.md`. Integration owner
(Makefile/harness/contract-test window).

## Contract

One Playwright process proves the M9 scenario over the trusted HTTPS harness at
`https://localhost:20443`:

1. Node-side: POST `/oauth/register` (M1 minimal client with a loopback redirect
   URI) → `clientRegistered`.
2. Browser: sign in with the seeded UAT identity, visit `/oauth/authorize?...`
   with a generated S256 challenge → `authorizeRedirected`; approve on
   `/authorize` → `consentApproved` (capture the code from the intercepted
   loopback redirect without running a listener — read it from the navigation
   URL).
3. Node-side: exchange the code with the verifier → `tokenExchanged`; JSON-RPC
   `initialize` + `tools/list` over `/mcp` → `toolsListed` (exactly fifteen
   tools); `create_resume` + `upsert_entry` → `resumeCreated`, `entryUpserted`.
   Every `/mcp` fetch sends `Accept: application/json, text/event-stream` — the
   go-sdk stateless handler 400s without both values.
4. Browser: open the editor and assert the agent-created content renders →
   `editorVisible`.
5. Browser: revoke the grant in Connected agents → `grantRevoked`. Node-side:
   the next `/mcp` call fails with the closed 401 → `revokedRejected`.
6. Teardown deletes the created resume through the still-valid session and the
   fixture cleanup removes every OAuth row the proof created.

Harness integration follows the mounted-spec model: add `mcp.spec.ts` to
`SPEC_SOURCES` in `scripts/dev-https-check.sh` and `run.sh`, the `mcp` mode with
`evidence_prefix=mcp` and `mcp-proof.json` (M9 schema, 4,096 bytes) to both, the
`dev-https-mcp-check` Makefile wrapper, contract-test updates (`static-test.sh`,
`makefile-safety-test.sh`), and the AGENTS.md check-table row. All agent-side
fetches stay on the trusted origin inside the existing network firewall; use the
Playwright MCP server only as an authoring aid.

The HTTPS lifecycle must set `MCP_ENABLED=true` for its isolated server and pin
that setting in `dev-https-test.sh`; no global or daily native default is
changed. Phase PF later added `PROVIDER_LOGIN_ENABLED`; the MCP proof also sets
that flag to `true` because its seeded sign-in uses the local provider. The
current-candidate proof must retain both settings. `playwright.config.ts` must
accept the `mcp` mode. The existing auth spec and shared provider-login helper
must expect `/app/resumes`, matching the approved provider-login default
introduced before T10, before the required auth regression can pass.

## TDD cycle

- [x] Extend `static-test.sh` and `makefile-safety-test.sh` RED for the new
      mode, spec file, and evidence schema; run both to observe the exact
      failures.
- [x] Write `mcp.spec.ts` against the live harness using `harness-lib`
      (diagnostics, firewall, login, hydration waits); no fixed sleeps.
- [x] Land the check-script/run.sh/Makefile wiring; rerun both contract tests to
      GREEN.
- [x] Live run:

  ```sh
  make dev-native-down && make dev-https
  make dev-https-browser-image   # run.sh changed: one rebuild
  make dev-https-mcp-check
  ```

  All M9 steps true, all error counters zero, evidence mode 0600.

- [x] Rerun `make dev-https-auth-check` to prove the existing proofs still pass
      beside the new mode, then restore the native stack.

## Adversarial checklist

- The proof asserts the revoked token fails with the byte-exact closed 401 and
  that no external request left the firewall.
- Evidence contains step booleans and counters only — no token, code, URL query,
  or resume content.
- Cleanup verified: zero `oauth_*` rows and no leftover resume for the fixture
  identity after the run.

## Handoff

Report the ten step booleans, error counters, evidence path/size, and
contract-test GREEN lines. Suggested commit:
`test(mcp): prove agent access over native HTTPS`.
