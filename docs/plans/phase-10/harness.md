# Task 10.14 — Hosted UAT harness

Author and test this task before task 10.15 activation. Include all workflow
specs and operational commands used by tasks 10.16–10.17 in the locally reviewed
candidate. After deployment, run the same harness's live TLS/route preflight; do
not introduce new code into the candidate during acceptance.

**Owner:** assigned harness author; root Makefile/workflows remain
integration-owned. **Planned paths:** `deploy/uat-browser/`,
`scripts/uat-check.sh`, and a `docs/runbooks/aws-uat.md` guide. Create exact
task contracts and command names from the Phase 9 decision before
implementation; these paths do not yet exist.

## Required behavior

- Reuse the established scripted headless Playwright approach, with a pinned
  browser and explicit `https://uat.aboutme.vn` base URL. Keep native HTTPS
  checks bound to localhost; do not repoint their seed/reset commands to AWS.
- Verify normal certificate trust, HTTPS redirects, Secure `__Host-` cookies,
  CSRF/Origin handling, UAT noindex, and the resolved edge access policy. No TLS
  bypass, personal browser profile, or production hostname is allowed.
- Use synthetic accounts/resumes and owner-approved test mailboxes.
  Registration, verification, reset, and notification tests consume the owner's
  SES setup. In sandbox, use approved verified recipients for these workflows;
  simulator smoke is separate and cannot prove receipt and use of a
  verification/reset link. Never copy a local development database or account
  into AWS.
- Bound fixture counts, browser concurrency, timeouts, and local evidence size.
  Cleanup touches only fixtures tagged to the run; there is no blanket live
  database reset. Infrastructure teardown follows the Phase 9 retention policy.
- Allow only the documented UAT origin and any explicitly required test client
  callback. Do not broadly disable the existing local network protections.
- Add negative checks proving that production origins, unexpected redirects,
  wrong environment identity, and unsafe cleanup are rejected before mutation.

## Verification and handoff

The author first tests configuration validation and cleanup against local
fixtures, then checks TLS and routes through the real UAT origin. Record exact
commands, browser version, candidate/image digests, and fixture scope in the
runbook. A browser on this laptop runs alone, with no parallel heavy gate.

Task 10.16 receives a healthy candidate, bounded fixture setup/cleanup, exact
commands, and the mail-testing procedure. A missing SES handoff blocks mail
acceptance, not local harness authoring.
