# Vietnam host operations proofs

This page holds the external proof contracts of the
[host operations](vietnam-host-operations.md) design and its
[delivery companion](vietnam-host-operations-delivery.md).

New `scripts/host-operations-proof.sh <id>` reads the saved action over SSH.
Account deletion uses bounded HTTPS requests only, with a 10-second request
limit and five-minute total limit; no login or browser dependency. It checks
known slugs and discovery/showcase membership, discards bodies, and forwards
only fixed outcomes. No screenshots or content artifacts are saved. The host
publishes a `serving` challenge for this check after resolving deletion.

Rotation uses the pinned production Playwright image and a named fictional
account/resume supplied by the primary. The wrapper requires the
[local memory watchdog](../../instructions/resources.md), the shared local-check
lock, no active local stack, the browser container limits, and a 60-minute
timeout. It stages only allowlisted sources; no host dependency install. Modes
are `hostops-prod-totp` and `hostops-prod-view-pass`. The origin is exactly
`https://aboutme.vn`. These proofs are run by the primary, not a worker or
public CI.

Owner-only 0600 `request.json` contains `requestId`, `action`, `digest`,
`slugs`, and view-pass `witnessSlug`. Rotation `account.env` contains only the
named fictional account's inputs: TOTP `ABOUTME_TEST_EMAIL` and
`ABOUTME_TEST_PASSWORD`; view-pass `PROVIDER` (`google|linkedin`),
`PROVIDER_USERNAME`, `PROVIDER_PASSWORD`. The wrapper checks metadata and mounts
these read-only; only the browser process reads values. Missing inputs or a
provider challenge blocks. Never create accounts/resumes or use the owner's
account. Precheck the witness has sign-in-to-view, photo and PDF enabled.

The browser runs in laptop tmpfs with a 0700 `control/` directory mounted
read-only and `reply/` writable. Wrapper `control/phase.json` and browser
`reply/result.json` are atomically renamed, at most 1 KiB each. Closed fields
are `requestId`, `digest`, `phase`, `nonce` (32-byte random base64url); replies
add `result` (`pass|fail`). Reject symlinks, unknown fields, wrong tuple, stale
nonce and duplicate advance. Poll once per second, one outstanding challenge.
Forward valid replies through the private host `proof` verb. Host challenges
expire after five minutes; timeout/exit/failure never advances an effect. The
host unit can exit at its durable waiting phase; resume challenges again.

Disable traces, screenshots, video, downloads, raw browser output, and assertion
value dumps. No SSH agent/key, Podman socket, host directory or recovery archive
is mounted. Browser memory holds cookies, TOTP setup seed, recovery codes, and
provider session only. Its finalizer removes only its own proof factor, logs
out, and deletes profiles/channel files. Failed cleanup is explicit, not
success. Existing unrelated factors must never be removed.

| Challenge            | What passes                                                                                                                                               |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `prepared` TOTP      | Refuse an existing TOTP factor; enroll a temporary one under the old active key and prove sign-in.                                                        |
| `prepared` view-pass | Obtain a pass, prove it admits every protected route and no-pass requests are gated; retain exact cookie plus decoded resume ID, epoch, expiry in memory. |
| `maintenance`        | TLS verified; public origin returns 503 with `aboutme:maintenance` after Go and serving Caddy stop.                                                       |
| `serving` TOTP       | Public readiness/reads and sign-in with the prepared factor succeed after re-encryption.                                                                  |
| `serving` view-pass  | Old-cookie/fresh-cookie comparison below succeeds.                                                                                                        |
| `retired` TOTP       | Sign-in succeeds with only new active mapped; public readiness/reads succeed.                                                                             |

A lost TOTP browser can permanently lose the fictional account if every factor
and recovery code was lost
([ADR 0017](../adr/0017-second-factor-authentication.md)). It never authorizes
an operator reset. Resume can use another named factorless fictional account
under the current ring. Keep prior switch evidence: before activation repeat
`prepared`; after activation repeat `serving`/`retired` without rewinding keys.
A replacement witness alone does not prove old rows decrypt; the successful
re-encryption result and both zero snapshots remain required.

For view-pass, obtain a fresh provider pass in a separate browser context after
reload. Require the same resume ID and epoch, unchanged live/gated state and
slug, and old expiry at least 60 seconds later than server HTTP `Date`
throughout the comparison. Replay the exact old cookie in an isolated context
without the fresh cookie. HTML must gate; JSON, photo, PDF, live stream and view
start must return the uniform 404. The fresh cookie must admit the same routes.
Check
[discovery and preview exceptions](viewer-analytics/sign-in-to-view.md#gated-routes),
readiness and ordinary public reads. Do not toggle publish state to simplify the
proof. Lost/expired witness, changed epoch/ID, provider challenge or ambiguous
state follows the main design's `unverified` outcome, never a false pass.
