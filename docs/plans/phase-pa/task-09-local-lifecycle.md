# Task 09 — Compose password mail dependencies into local lifecycles

**Acceptance:** AC-AUTH-014, AC-OPS-020.

**Depends on:** T05/T06/T08; integrated Phase 4/P5A lifecycle state.

**Owned paths:** T09 paths in `file-structure.md`. This is a serialized
integration-owner window.

## Contract

Add exact validated configuration:

```text
PASSWORD_RATE_HMAC_KEY       base64url, exactly 32 decoded bytes
AUTH_EMAIL_ACTIVE_KEY_ID     1–64 printable ASCII
AUTH_EMAIL_ACTIVE_KEY        base64url, exactly 32 decoded bytes
AUTH_EMAIL_PREVIOUS_KEY_ID   both previous fields absent or valid together
AUTH_EMAIL_PREVIOUS_KEY      base64url, exactly 32 decoded bytes
AUTH_EMAIL_MODE              ses|capture
AUTH_EMAIL_CAPTURE_URL       loopback HTTP only; required only in capture mode
AUTH_EMAIL_CAPTURE_BEARER    base64url, exactly 32 decoded bytes; capture only
SES_FROM_ADDRESS             canonical D1 email; SES mode only
SES_CONFIGURATION_SET        1–64 AWS-safe ASCII; SES mode only
AWS_REGION                   exactly ap-southeast-1 in SES mode
```

Prod/staging require SES mode and reject capture fields. Dev accepts capture
mode and never constructs an AWS config/client. Both modes construct the key
ring, password rate policy, one startup dummy hash, password service, and mail
worker. Server shutdown cancels the worker and waits at most the existing
graceful-shutdown deadline; readiness fails if required composition is missing
or the worker exits unexpectedly.

`make dev-native` starts mail capture at `127.0.0.1:20091` before server.
`scripts/dev-https.sh` starts it at `127.0.0.1:20444` before server and extends
the proved PID/group/manifest model. Each lifecycle creates mode-0600 random
rate/mail/capture secrets once when its validated state root has none, then
reuses the keyring across down/up while the development database persists. It
never rotates implicitly. Values never print. Stop order is Caddy → web → server
→ mail-capture → provider mock. Startup failure rolls back only processes
created by that invocation.

Server startup queries every key ID referenced by pending/leased jobs and fails
readiness before worker or route start if one is absent. Production rotation
likewise retains every referenced key and rejects a second rotation until the
older key has no live job.

The HTTPS mock config enables Google, GitHub, and LinkedIn local endpoints and
distinct verified registration emails so T12 can prove different-email link
without any external request.

## TDD cycle

- [ ] Add config REDs for every missing, malformed, repeated, wrong-mode,
      duplicate-key-ID, over-count key-ring, wrong region, noncanonical sender,
      non-loopback capture URL, and prod/staging capture attempt.
- [ ] Add composition REDs proving capture mode constructs no AWS loader/client,
      SES mode uses the standard AWS credential chain without credential env
      fields, dummy hash is created once, every live job key ID exists, and
      worker error changes readiness.
- [ ] Extend lifecycle static tests first. Require exact ports/service order,
      state modes/ownership, manifest drift, foreign listeners, failed capture,
      failed server after capture, worker logs redaction, idempotent status/up,
      and complete down/group drain.
- [ ] Add restart/rotation REDs: down/up with a pending job reuses the exact
      keyring and can deliver it; a missing referenced key fails before route/
      worker start; active+previous succeeds; a third-key rotation is rejected
      until no live job references the older key.
- [ ] Add tests that production config/source contains no capture route or
      Caddy/public-root mapping.
- [ ] Run expected RED:

  ```sh
  cd apps/server && go test ./internal/config ./cmd/server -race -count=1 \
    -run 'Test(Password|AuthEmail|MailWorker)'
  bash scripts/dev-https-test.sh --static
  ```

- [ ] Add config and server composition. Start the worker only after database,
      key ring, sender, and service construction succeeds. Join it on every
      shutdown/error path.
- [ ] Extend native/HTTPS scripts with the existing path-integrity,
      process-identity, group-drain, redaction, and fail-closed listener probes.
      Do not weaken or duplicate that machinery.
- [ ] Add empty variable names to `.env.example`; never add values.
- [ ] Add narrow Make targets for capture static/lifecycle checks. Preserve all
      existing dev-native/dev-https targets and behavior.
- [ ] Run the minimal GREEN focused checks and:

  ```sh
  bash -n scripts/dev-native.sh scripts/dev-https.sh scripts/dev-https-test.sh
  bash scripts/dev-https-test.sh --static
  make server-build server-vet server-test
  ```

## Adversarial checklist

- No inherited env can redirect database, capture, SES region, canonical origin,
  or provider mock outside the exact local/prod allowlist.
- Symlink/hardlink/mode/owner/listener/PID-reuse/partial-start and command-drift
  protections still cover the new service.
- `status`, `logs`, and failures reveal no secret, email, token, hash, payload,
  capture content, or AWS detail.
- Capture/SES mode is exclusive; local commands perform no AWS network call.
- Startup and shutdown leave no untracked worker/process/listener.

## Handoff

Report config fields/validation, composition/shutdown order, lifecycle manifest
changes, exact generated-state modes, focused checks, and explicit no-AWS
evidence. Suggested commit: `feat(dev): run local authentication mail capture`.
