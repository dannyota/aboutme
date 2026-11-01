# Production pull deployment attendance

See the [design overview](README.md), [protocol](protocol.md), and
[execution](execution.md).

## Administrative commands and external proofs

Use pinned administrative SSH and the independently reviewed checker, with
canonical JSON over stdin. There is no public deployment API. Commands are at
most 16 KiB; receipts and challenge replies are at most 4 KiB. SSH supplies
authority; caller-supplied identity does not. Record the administrative key
fingerprint privately.

```text
Check = "maintenance"|"health"|"ready"|"headers"|"ban"|"unban"
Attendance = {
  session_id:UUID, epoch:UUID, generation:ID, request_hash:H,
  egress_ipv4:IPv4, created_at:T, admit_until:T
}
Challenge = {
  schema:1, binding:Binding, session_id:UUID, check:Check,
  nonce:H, issued_at:T, expires_at:T
}
ChallengeReply = { schema:1, state:"unavailable" } | {
  schema:1, state:"pending"|"ready", session_id:UUID, binding:Binding?, challenge:Challenge?
}
Receipt = {
  schema:1, challenge:Challenge, observed_at:T, http_status:U,
  tls_valid:boolean, maintenance_marker:boolean?, hsts:boolean?, nosniff:boolean?
}
AdminCommand =
  { verb:"status" }
  | { verb:"pause"|"resume_polling", epoch:UUID, revision:U }
  | { verb:"attend", epoch:UUID, generation:ID, request_hash:H, egress_ipv4:IPv4 }
  | { verb:"submit", request:Request }
  | { verb:"challenge", session_id:UUID }
  | { verb:"proof", receipt:Receipt }
  | { verb:"config_begin", epoch:UUID, revision:U, recovery_binding:Binding? }
  | { verb:"config_finish", epoch:UUID, revision:U, change_id:UUID, recovery_binding:Binding? }
  | { verb:"recover", binding:Binding, revision:U, recovery:Recovery }
```

`attend` creates and returns `Attendance`, privately bound to the authenticated
key. It arms admission for ten minutes. Admission binds the session to the exact
operation; challenge reads and proofs require the same currently authorized key
and session. Allow one session per request/operation. Egress cannot change after
admission; an address change requires stopping the operation and attended
recovery with a fresh session. Recovery may attach a fresh session to its
retained binding without consuming another generation.

The checker declares its public HTTP egress IPv4, which may differ from its VPN
SSH source. Store attendance, key binding, address, challenges, and ban
identifiers only in root-private `/run/aboutme-deploy/`, with 0700 directories
and 0600 files. Exclude them from requests, public status, durable journals,
logs, and backups. Delete unadmitted sessions at expiry; revoke and erase
admitted sessions during cleanup, at eight hours, or on reboot. Losing a session
never releases ownership.

The checker sends `challenge` through the same authenticated SSH command
interface, at most once every two seconds. Return `pending` with null challenge
while awaiting admission or the next proof check. Its binding is null only
before admission; afterward it equals the session’s exact retained binding.
Return `ready` with non-null binding and challenge only when the coordinator has
opened that check’s proof window.

Keep at most one outstanding challenge per session. Repeated reads return
identical bytes until consumption or expiry. `unavailable` covers unknown,
wrong-key, revoked, expired, or closed sessions without exposing their identity
or binding. A session whose operation has failed or entered cleanup cannot
receive another challenge.

Challenges contain fresh 256-bit random nonces. Their expiry is the earliest of
issuance plus 120 seconds, the existing proof-phase deadline, and session
expiry. Polling, disconnecting, and reconnecting never extend these times. An
expired outstanding challenge fails the proof phase; it cannot be replaced to
restart the deadline.

A reconnect using the same authorized key and session retrieves the same
unconsumed challenge while valid. A missing or erased session cannot be
reconstructed from public status or receipts. After admission, replacing it
requires terminated execution and attended recovery; there is no automatic
takeover.

Issue checks only against `https://aboutme.vn`. Require TLS certificate
validation, cache bypass, no redirects, and ten-second HTTP deadlines.
Maintenance requires 503 and `aboutme:maintenance`; health and readiness require
200; headers require 200, HSTS, and `nosniff`. Irrelevant nullable receipt
fields must be null. Accept a receipt only during the challenge window, with
observation time within that window and exact challenge, session, key, binding,
and current check. Consume its nonce once; a failed check fails the operation.

For the declared address, apply one operation-owned, three-minute HTTP-only test
ban; require 403, remove that exact ban, then require 200. Cleanup always
attempts removal and verifies removal or expiry before releasing ownership.
Never remove unrelated bans. The TTL bounds failed removal. Attendance loss
after admission is handled by proof deadlines.

`pause` sets pause and increments revision atomically, invalidating pending
requests without interrupting admitted work. `resume_polling` requires idle,
reconciled, healthy state, no configuration transaction, matching
applied/current configuration revisions, and a release at or above `F`; it
increments revision. Neither changes the floor. SSH submission requires pause,
consumes a generation, and leaves polling paused. Old helpers fail the
protocol/version check.
