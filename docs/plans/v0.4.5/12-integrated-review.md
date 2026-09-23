# Integrated TOTP release review brief

Role: reviewer. Model: Opus (Codex: `gpt-5.6-sol`).

## Objective and authority

Review the complete v0.4.5 release once after the manager verifies every author
report and before the candidate leaves the local repository. Read `AGENTS.md`,
the accepted TOTP and passkey contracts, the key-management design,
release-fence contract, ADRs 0048 and 0049, affected design and budget sources,
OpenAPI, traceability, runbook, this release plan, and every task report.

You are read-only. Own no files. Do not edit, stage, commit, push, tag, deploy,
change keys, run rotation, change the fence or flag, or retry a failed check.
Review only the exact integrated diff from recorded base through candidate.

## Required review

- Trace password, every provider, pending login and reauthentication, passkey,
  TOTP, recovery, password reset, sensitive actions, sessions, grants,
  authorization codes, refresh, bearer authentication, and account deletion
  across the authentication epoch.
- Confirm lock order and one-winner behavior for setup, supersession,
  completion, code use, recovery use, replacement, removal, rotation, session
  issue, consent, token exchange and refresh, and account deletion. Confirm the
  sole order is user, current session when present, policy, credential,
  enrollment, then pending authentication when applicable.
- Confirm exact profile, code syntax, skew, step replay, clock, entropy, issuer,
  label, URI, strict JSON, cookies, CSRF, Origin, rate, body, lifetime, count,
  cleanup, error, and cache behavior.
- Adversarially review ciphertext bounds, associated data, nonce use, active and
  previous keys, derived key IDs, row-ID sealing, lazy and proactive rotation,
  bounded key-ID query, per-row key failure with green `/readyz`, the alarm
  signal, bounded count, slot rotation order across restarts, secret paths, app
  execution role access, the `totp_reencrypt` operation, and local-stack key
  redaction.
- Confirm the per-account failure budget, cool-down bounds, attempt-mail cap,
  and that no cool-down blocks passkey or recovery completion.
- Confirm the final active factor counts every type and that passkey routes and
  handlers are unchanged.
- Confirm first-policy handle entropy, three-candidate collision bound,
  persistence, later passkey reuse, and all-or-nothing failure.
- Confirm migration 00005 replaces and tests both mail constraints while factor
  mutation and mail insertion stay in one transaction.
- Confirm every v0.4.2 and TOTP route keeps exact
  `Cache-Control: no-store, no-transform`.
- Confirm flags, mixed versions, older-client failure, floor 4005, lower-target
  denial, activation and restoration, privileged bypass, and forward-fix rule.
- Inspect mail, export, logs, metrics, traces, evidence, QR rendering, client
  state, dependency review, both locales, viewports, focus, keyboard behavior,
  and cleanup for secret material.
- Confirm CI owns all test, build, lint, database, migration, browser,
  infrastructure, Semgrep, and gitleaks gates. Confirm the hosted job executes
  the candidate with fictional data and unconditional cleanup.

## Report contract

Rank findings by severity. For each finding, state failure scenario, exact path
and line, violated authority, and owning brief. Name every confirmed invariant.
List reports and hosted evidence inspected, checks not run with reason, exact
candidate SHA, and open items. Findings return to the original owner. The same
reviewer confirms fixes against the corrected diff. Do not launch a local test,
build, linter, browser, database, stack, or network command. Use short plain
text with no em dash. Code, tests, comments, and living docs never cite plans,
tasks, phases, or review findings; cite the design, ADR, or `AC-*` ID instead.
