# Task 12 — Prove password authentication through native HTTPS

**Acceptance:** AC-AUTH-008–016, AC-OPS-020, AC-SEC-005.

**Depends on:** T00–T11 focused gates; running Task 09 native HTTPS stack; one
idle browser/build window.

**Owned paths:** T12 paths in `file-structure.md`. Browser runner, fixture, root
Makefile, and evidence are a serialized integration-owner window.

## Contract

Add runner mode `password-auth` and one `password-auth.spec.ts`. Keep
`workers: 1`, `fullyParallel: false`, Chromium sandbox, isolated NSS trust,
read-only root/input, writable bounded evidence, service workers blocked,
traces/video/screenshots off, and the existing request/WebSocket firewall.

The read-only input directory contains exactly mode-0600 `caddy-root.crt` and
`mail-capture-token`. The latter is a random 32-byte base64url secret generated
by the lifecycle. Playwright Node control code may call
`http://127.0.0.1:20444/api/messages`; no page/context request may leave
`https://localhost:20443`.

Evidence is at most 4 KiB, mode 0600, and exact closed JSON:

```json
{
  "errors": { "certificate": 0, "console": 0, "externalRequest": 0, "page": 0 },
  "origin": "https://localhost:20443",
  "scenario": "password-authentication",
  "schemaVersion": 1,
  "steps": {
    "differentEmailLink": true,
    "newPasswordLogin": true,
    "oldPasswordRejected": true,
    "oldSessionsRevoked": true,
    "passwordAdded": true,
    "passwordLogin": true,
    "providerOnlyLogin": true,
    "registerAccepted": true,
    "reset": true,
    "resetReplayRejected": true,
    "verifiedWithoutSession": true
  }
}
```

It contains no email, password, provider subject, token/digest, session/CSRF,
capture secret, mail body, or request/response bytes.

## TDD cycle

- [ ] Extend static runner tests first for the new mode/spec/evidence, exact two
      input files, modes/owners, no extra mounts, no token on argv/env/output,
      and rejection of tag/unverified image/update/trace/video/sandbox bypass.
- [ ] Add fixture REDs for deterministic provider subjects, different verified
      emails, exact three-account baseline/reset, and no real provider/AWS URL.
- [ ] Write browser scenario RED with runtime-random example-domain emails and
      passwords held only in test memory: 1. reset capture and fixture; 2.
      register and prove fixed accepted copy; 3. poll capture through Node, open
      verification fragment, prove fragment stripped and `/me` remains 401; 4.
      password login and prove authenticated `/me`/cookie; 5. provider-only
      login, provider reauth, add password; 6. link a second provider whose
      verified email differs and prove both identities remain on one account; 7.
      create two live sessions for the registered account; 8. forgot, capture
      reset, reset without auto-login; 9. prove both old contexts `/me` 401, old
      password 401, reset token replay invalid, and new password login succeeds.
- [ ] Attach console/pageerror/requestfailed observers to the fixture page
      before first navigation and to every later page/popup. Install context
      HTTP and WebSocket firewall before creating/navigating pages.
- [ ] Run expected static RED:

  ```sh
  bash -n deploy/dev-https-browser/run.sh \
    deploy/dev-https-browser/static-test.sh
  bash deploy/dev-https-browser/static-test.sh
  ```

- [ ] Implement runner/fixture/Make changes while preserving `auth` and
      `transport` modes byte-for-byte.
- [ ] Run the live GREEN: run server/web focused gates, start the Task 09 HTTPS
      stack, build/inspect the pinned browser image, and run exactly one browser
      process through the new root target. Do not run another Playwright
      agent/process concurrently.
- [ ] Read back and validate evidence, then stop only the stack created for this
      run. Verify no process/listener/capture message or test account remains.

## Adversarial checklist

- Browser certificate validation is real; no ignoreHTTPS/HTTP fallback.
- Browser requests remain exact-origin; capture polling is Node-only and bearer
  authenticated. Capture data never reaches the page/DOM/evidence.
- URL fragment is absent before the first API request and never appears in
  request URL, Referer, console, screenshot, trace, or evidence.
- Cookie flags, CSRF refresh after password-change cookie replacement, Origin,
  provider linking, different email, no auto merge, and all-session reset are
  materially observed.
- Test cleanup targets only recorded IDs/state and is safe under partial
  failure.
- The run makes no AWS, SES, DNS, registry, staging, or external provider call.

## Handoff

Report immutable browser image ID, candidate commit, lifecycle state hash,
fixture reset result, exact command, evidence SHA-256 and readback booleans, and
no-AWS/external-request count. Never report secret values. Suggested commit:
`test(auth): prove password flows over native HTTPS`.
