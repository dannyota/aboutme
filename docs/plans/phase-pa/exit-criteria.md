# Phase PA exit criteria

The integration owner checks every item at one unchanged candidate commit.
Failed or unsatisfiable items are corrected and rerun under ADR 0024.

## Authorities, routes, storage, and dependencies

- [ ] Approved v4, ADR 0025, budgets, OpenAPI, traceability, and the phase plan
      agree on password/provider/session/mail behavior and no provider-only
      authority text remains operative.
- [ ] The v5 public-root source regenerates byte-identical Go/Nuxt/Caddy/test
      consumers and adds exactly register, verify-email, forgot-password, and
      reset-password without changing existing dispatch.
- [ ] x/crypto 0.55.0 and SES v2 1.66.6 are direct exact dependencies; no
      unreviewed dependency or toolchain drift exists.
- [ ] The pinned blocklist source, commit, license, byte hash, line count,
      generator, generated digest artifact, and runtime lookup all agree.
- [ ] Migration 00008 passes fresh up/down, constraint, preflight, cleanup,
      lock-order, lease, rollback, and concurrent sqlc tests with `-count=1`.
- [ ] OpenAPI and generated TypeScript contain exactly seven password
      operations, `hasPassword`, strict 4 KiB bodies, fixed statuses, and no
      secret-bearing response field.

## Identity, password, token, and session invariants

- [ ] Canonical email independently covers every grammar/byte/case boundary; all
      existing rows pass preflight; provider creation, password lookup, storage,
      and rate keys use the one parser.
- [ ] Returning provider subjects resolve before email retrieval. New subjects
      create user+identity atomically. Existing email never auto-links; link
      ignores different provider email; one subject cannot move accounts.
- [ ] Every provider/password login and ADR 0015 rotation successor holds the
      user lock. Reset versus every issuer proves a pre-reset session is revoked
      and an issue ordered after reset is a distinct post-reset login/use, with
      no cross-boundary insert and unchanged rotation-grace semantics.
- [ ] Password NFC, raw bytes, code points, spaces/case, blocklist, HIBP
      prefix/padding/cache/failure, and Argon2id PHC parsing/rehash pass exact
      at-limit and limit+1 tests.
- [ ] At most two hashes run and 16 wait. Cancellation releases capacity. The
      seventeenth waiter fails closed. Unknown/no-credential/wrong paths each
      perform one indistinguishable admitted verification.
- [ ] Tokens have exact entropy/encoding, digest-only storage, constant-time
      checks, 24h/30m boundaries, replacement, expiry, single use, and replay
      rejection. A raw token exists only in transient memory or encrypted mail;
      its full digest exists only in bounded database rows and constant-time
      comparisons, never diagnostics or evidence.
- [ ] Login snapshot/recheck, reset/change, password reauth, duplicate reset,
      duplicate verification, provider signup/verification, subject collision,
      and session issuance/reset races pass with no stale session, deadlock,
      partial write, or duplicate account.
- [ ] Add/change creates one fresh non-lineage current session, revokes all old
      sessions in the transaction, and preserves only the specified session
      metadata. Reset revokes all and creates none. Lost-response cases remain
      secure.

## HTTP, rate, outbox, and delivery invariants

- [ ] Every password route passes method, exact media type, 4 KiB cap, strict
      JSON, Origin, applicable CSRF/session/reauth, and rate admission before
      expensive work or state; every rejection proves no write and no secret.
- [ ] Register/forgot account states produce byte-identical 202 bytes/cookies;
      unknown/provider-only/wrong login states produce byte-identical 401
      bytes/cookies/work class. Closed errors contain no dependency detail.
- [ ] Outer and route policies independently pin every numeric budget, bounded
      key store, overflow behavior, HMAC email keys, exact Retry-After, correct
      password success after failure debt, and success debt clearing.
- [ ] AEAD payload/AAD/key-ring/version/bounds pass tamper, wrong-key, rotation,
      duplicate-ID, unknown-ID, and plaintext-leak tests. Enqueue failure rolls
      back its credential/account mutation.
- [ ] Worker claims/leases with SKIP LOCKED, rechecks current token authority,
      holds scope/job locks through the bounded send handoff, caps concurrency/
      batches/time, classifies SES errors, applies bounded jitter/retries,
      clears every encryption field on sent/terminal, and drains/joins on
      shutdown. Replacement cannot commit in an authorization/send gap.
- [ ] Startup contains every key referenced by a live job. Native/HTTPS restart
      reuses its persisted keyring; production cannot remove a referenced key or
      perform a second rotation that would strand one.
- [ ] Logs, metrics, errors, panics, browser output, and evidence remain free of
      emails, passwords, tokens/digests, hashes, provider claims, session
      values, encryption keys/plaintext, SES request IDs, and raw dependency
      errors.
- [ ] Native/HTTPS capture binds loopback, enforces its secret, caps
      count/bytes, escapes HTML, resets predictably, and is absent from
      production config, Caddy/public roots, and deployed composition.

## Web, live evidence, records, and final gate

- [ ] Login retains all providers and adds accessible password login. Register,
      verify, forgot, reset, and settings flows pass keyboard, password manager,
      paste, issue-focus, light/dark, and Nova/Zinc/Emerald tests without shell
      redesign.
- [ ] Verify/reset strip fragments before network activity, use no-referrer,
      load no third-party resources, clear local token state, and use uniform
      success/failure copy without account/provider disclosure.
- [ ] One Playwright process through the trusted native HTTPS overlay proves
      registration, captured verification without login, password login,
      provider-only add, different-email link, reset, old-session revocation,
      new-password login, and replay rejection. Capture polling is Node-only;
      browser network stays on the trusted origin.
- [ ] The Argon2id policy benchmark records deployment-CPU wall time and peak
      RSS under the two-running/16-waiting admission bound. Any policy change is
      an authority change, not test tuning.
- [ ] No SES, DNS, AWS, staging, or production mutation occurred. Remaining
      cloud activation is recorded as human-authorized future work.
- [ ] Every T00–T12 report matches the handoff format; shared edits and unrun
      checks are resolved or block exit.
- [ ] The owner updates and commits the master plan/index, architecture,
      runbook, and trace evidence before review; focused record checks pass.
- [ ] One fresh non-author reviews the full candidate and confirms every named
      security/concurrency/privacy invariant; the same reviewer confirms fixes.
- [ ] `make ci` passes alone, then connected `SEMGREP_APP_TOKEN` `make scan`
      passes alone on the same unchanged candidate.
