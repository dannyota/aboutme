# Phase PA integration handoffs

## Serialized integration-owner windows

Phase PA starts only after P4 and P5A close. The integration owner completes and
verifies each shared window before releasing consumers.

| Window       | Owned change                                                                               | Release gate                                                                     |
| ------------ | ------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------- |
| W0a T00      | Approved v4, ADR 0025, budgets, traceability, v5 root registry and all generated consumers | Focused docs checks, generator check, `make public-roots-check route-table-test` |
| W0b T01      | Migration 00008, SQL queries, generated sqlc/store contract                                | `make sqlc-check server-test-db server-test-integration server-migration-test`   |
| W0c T02      | OpenAPI source, generated TypeScript, contract fixtures                                    | `make api-check web-typecheck`                                                   |
| W1a T03 deps | Exact x/crypto pin and blocklist artifact/provenance                                       | Focused password tests, `make server-build server-vet`                           |
| W2a T06 deps | Exact SES v2 pin and generated sums                                                        | Focused authmail tests, `make server-build server-vet`                           |
| W4 T09       | Config/main, native/HTTPS lifecycle, env example, root Make                                | Static lifecycle tests and server gates                                          |
| W6 T12       | Fixture, browser runner/spec/evidence and root UAT target                                  | One isolated Playwright process and readback                                     |
| W7 records   | Master/index, architecture, runbook, trace evidence                                        | Commit records; docs checks; reread; fresh review                                |

No worker edits a migration, generated store/client/root consumer, dependency
lockfile, route registry output, `.env.example`, root Makefile, lifecycle
script, or master record outside its released owner window.

## Exact handoffs

| Producer | Consumers  | Interface                                                                                                     |
| -------- | ---------- | ------------------------------------------------------------------------------------------------------------- |
| 00       | 01–12      | D1–D10 authority, password budgets, AC-AUTH-008–016, AC-SEC-005, AC-OPS-020, v5 roots                         |
| 01       | 04–09      | Generated password/outbox/lock/lease queries and `store.PasswordQueries` compile contract                     |
| 02       | 10–12      | Seven operation types, `hasPassword`, fixed success/error schemas                                             |
| 03       | 05, 07, 08 | `accountemail.Canonicalize`; `password.Policy`, `Hasher`, `Token`, and closed errors from D2                  |
| 04       | 06, 08     | `authmail.KeyRing`, `Seal/Open`, `EnqueueTx`, typed payloads, and token-authority contract                    |
| 05       | 08         | `SessionManager.IssueTx`, user-lock session rule, transactional provider resolver, `/me.hasPassword` producer |
| 06       | 09         | `authmail.Sender`, `Worker`, SES factory, `mailcapture.Server`, fixed ports and capture API                   |
| 07       | 08         | `RateDecision`, `NewPasswordRatePolicies`, exact route-admission methods, and login outcome methods from D6   |
| 08       | 09–12      | Registered password routes, closed outcomes, cookie/session behavior, config dependency struct                |
| 09       | 12         | Running capture/server/web/Caddy topology, mode-0600 capture secret, status/log controls                      |
| 10       | 11, 12     | `usePasswordAuth`, fragment parser, shared password field and page locators                                   |
| 11       | 12         | Password settings/reauth locators and `useAuth.hasPassword` behavior                                          |

An interface mismatch returns to its producer. A consumer does not redeclare,
rename, alias around, or weaken a frozen interface.

## Task report format

```text
Phase/task: PA TNN — exact task title
Owned paths: complete path list
Acceptance: AC IDs and adversarial/race rows
RED: exact command, failing assertion, expected reason
GREEN: exact command and result
Adversarial cases: named cases proved
Shared edits requested: exact reserved path/edit, or "none"
Unrun checks: exact command, reason, remaining uncertainty, or "none"
Risks/notes: remaining fact, or "none"
Suggested commit: Conventional Commit subject
```

The owner inspects each owned diff and reruns its key GREEN command before
staging exact paths. Workers do not run Git, `make ci`, or `make scan`.

## Record and review order

1. Accept T00–T12 reports and focused checks.
2. Update and locally commit the master plan/index, architecture, runbook, phase
   state, and exact trace evidence.
3. Run focused document checks and verify the candidate contains the record
   commit.
4. Dispatch one fresh non-author reviewer.
5. Return findings to the owning author; the same reviewer confirms fixes.
6. Run the full exit checklist, `make ci`, and connected `make scan` on one
   unchanged candidate.

The reviewer names provider subject authority, canonical email, password
normalization/hash admission, dummy verification, token storage/single use,
session issuance/reset fencing, forced session replacement, CSRF/Origin,
enumeration equality, rate limiting, outbox encryption/lease authority, SES
retry classification, local capture isolation, secret-free diagnostics, web
fragment removal, and every deterministic race.
