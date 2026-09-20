# Integrated passkey release review brief

Role: reviewer. Model: `gpt-5.6-sol`.

## Objective and authority

Review the complete v0.4.2 release once, after the manager verifies every author
report and before the candidate leaves the local repository. Read `AGENTS.md`,
`docs/design/second-factor-authentication.md`,
`docs/design/passkey-second-factor-contract.md`,
`docs/design/passkey-release-fence.md`, ADR 0048, affected design and budget
sources, OpenAPI, traceability rows, production runbook, and every task report.

You are read-only. Own no files. Do not edit, stage, commit, push, tag, deploy,
change the fence or flag, or retry a failed check into a pass. Review the exact
integrated diff from the recorded base through the candidate and exclude other
working-tree changes.

## Required review

- Trace password, every provider, pending login, pending reauthentication,
  passkey, recovery, password reset, sensitive actions, sessions, OAuth grants,
  authorization codes, refresh, bearer authentication, and account deletion
  across the authentication epoch.
- Confirm the accepted lock order and one-winner behavior for enrollment,
  assertion, recovery, epoch change, session rotation, consent, code exchange,
  token refresh, factor removal, and account deletion.
- Confirm exact Origin, relying-party ID, ceremony type, presence, verification,
  cross-origin, user handle, signature, counter, challenge, binding, canonical
  base64url, body, field, count, lifetime, rate, and cleanup enforcement.
- Confirm cookies, CSRF, strict JSON, no-oracle errors, cache headers, logs,
  metrics, traces, exports, evidence, and mail reveal no factor material.
- Confirm enrollment-off behavior, older-client safety, unenrolled
  compatibility, no TOTP implementation, no resume or MCP tool schema change,
  and no public or renderer change.
- Adversarially review the DynamoDB item, conditional update, consistent read,
  nonexpiring operation lock, explicit release, deletion protection, role trust
  and permission boundary, old-script denial, normal deploy, rollback,
  failed-deploy restoration, activation, and flag order.
- Inspect both locales, recovery plaintext cleanup, browser evidence, all
  acceptance evidence, dependency version review, and every claimed command.
- Confirm the docs job only compiles and lists the passkey browser mode. Confirm
  the separate `passkey-browser-proof` job contract executes the candidate with
  fictional data, bounded secret-free evidence, test cleanup, and unconditional
  stack and runner-local database teardown. Hosted results are a later manager
  gate on the unchanged reviewed commit.

## Report contract

Rank findings by severity. For each finding, state the failure scenario, exact
path and line, violated authority, and owning author brief. Name every invariant
confirmed. List reports and checks inspected, checks not run with reason, exact
candidate SHA, and open items. Do not launch a local test, build, linter,
browser, database, or stack. Findings return to the original path owner. The
same reviewer confirms each fix against the corrected integrated diff. Use short
plain text with no em dash.
