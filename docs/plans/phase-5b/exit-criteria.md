# P5B exit criteria

All items run against one unchanged candidate commit. Record the candidate SHA
and exact command outcomes in the phase exit report before deleting this
directory. A failure invalidates later results until the fix is committed and
the affected sequence is rerun.

## Acceptance and behavior

- [ ] `AC-PUB-006` through `AC-PUB-010` are `PROVEN` with exact unit or native
      HTTPS evidence; `AC-PUB-003` and `AC-PUB-005` remain open for P7B.
- [ ] The dialog exposes separate Public resume, PDF download, and SEO and GEO
      controls; SEO and GEO default off for a never-published resume.
- [ ] The accepted canonical response supplies the shown public link.
- [ ] Save-first, CSRF, CAS, idempotency, stale, password/provider factor
      selection, reauth, validation, rate, busy, malformed-response, network,
      duplicate-submit, and cancellation cases pass their T01/T02 matrices.
- [ ] The native HTTPS proof publishes, verifies discovery off and on,
      unpublishes, observes public `404` within five seconds, and tears down its
      disposable resume without secret-bearing evidence.
- [ ] The publish dialog passes keyboard, focus-return, error-announcement, and
      serious-accessibility checks at wide and narrow layouts.
- [ ] `AC-MCP-007` still exposes no publish, unpublish, or public-read
      operation.

## Documentation and review

- [ ] Architecture, local UAT runbook, roadmap, and traceability agree with the
      code, OpenAPI, and deployment harness.
- [ ] A fresh non-author Terra reviewer reports no unresolved finding and
      confirms every invariant named in T04.
- [ ] The integrated diff contains no generated-file hand edit, unrelated
      change, secret, credential, personal data, or committed local evidence.

## Unchanged-candidate gates

- [ ] `make web-lint web-typecheck web-test web-build`
- [ ] `make dev-https-publish-check`
- [ ] `make docs-fmt` leaves the tree unchanged.
- [ ] `make ci`
- [ ] Connected `make scan` with `SEMGREP_APP_TOKEN` available at runtime; no
      secret value is printed or persisted.
- [ ] `git status --short --branch` and `git diff HEAD` confirm the candidate
      stayed unchanged throughout the gate sequence.

## Exit

- [ ] Update the implementation roadmap to P5B complete and pushed.
- [ ] Delete `docs/plans/phase-5b/`; Git history retains the active plan.
- [ ] Inspect `git diff --cached --name-only`, run the repository's per-commit
      gitleaks check, create the explicit-path exit commit, and push `main`.
