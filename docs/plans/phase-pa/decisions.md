# Phase PA implementation decisions

These decisions close implementation seams in the approved password design. Task
00 promotes the applicable rules into Approved v4, budgets, and ADR 0025 before
code work.

## D1 — Canonical account email and registration name

`CanonicalizeAccountEmail` accepts 5–254 ASCII bytes and returns lowercase
ASCII. It rejects surrounding space, controls, non-ASCII, quoted local parts,
comments, domain literals, empty labels, underscore in a domain label, a
leading/trailing dot, consecutive local dots, a leading/trailing domain hyphen,
and labels over 63 bytes. The local part is 1–64 bytes from this exact set:

```text
A-Z a-z 0-9 ! # $ % & ' * + - / = ? ^ _ ` { | } ~ .
```

The domain is 1–253 bytes, contains at least one dot, and each label contains
only ASCII letters, digits, and internal hyphens. This deliberate
deliverable-address subset does not support SMTPUTF8 or quoted spelling.

```go
package accountemail

const MaxBytes = 254
func Canonicalize(raw string) (string, error)
```

Password registration names accept 1–100 Unicode code points after NFC, reject
controls, and reject more than 400 raw UTF-8 bytes. They are not trimmed,
collapsed, or case-folded. Provider signup keeps its existing provider name
rules; Task 00 documents that this phase does not rewrite existing names.

The migration preflight asserts every stored user email equals
`lower(email::text)`, is ASCII, is at most 254 bytes, and passes the same SQL
shape constraints. Task 03's Go test reads every live email through the exact Go
parser. Failure blocks migration; there is no automatic rewrite.

## D2 — Password, breach, hash, and token budgets

All password routes have an exact 4,096-byte body cap. Strict JSON admits one
object, rejects duplicate/unknown fields and trailing bytes, and checks scalar
types before policy or storage work. Exact `Content-Type` is `application/json`;
parameters and repeated values are rejected with 415.

Password input rejects more than 1,024 UTF-8 bytes before NFC. After NFC it
requires 15–128 Unicode code points. It preserves all code points, spaces, and
case. The API never receives confirmation.

The bundled blocklist is exactly SecLists commit
`eedc5117b3f506d874d033c18786a218e7cec34c`, file
`Passwords/Common-Credentials/100k-most-used-passwords-NCSC.txt`, SHA-256
`c2e5696882c603b76bb67a47ee970897e5a76fc4c3f5547abe3d0ca340c576e0`, 99,840
lines, under the repository's MIT license. A deterministic generator normalizes
each candidate with NFC and stores sorted unique SHA-256 digests of the exact
UTF-8 bytes. Runtime lookup compares digests, not plaintext.

HIBP uses only `GET https://api.pwnedpasswords.com/range/<five-uppercase-hex>`
with `Add-Padding: true`, a five-second request deadline, no redirects, a 128
KiB response cap, strict ASCII suffix/count parsing, and no query or
user-specific header. The cache holds at most 256 prefixes and 16 MiB of parsed
entry bytes for 24 hours. It stores sorted fixed `[20]byte` SHA-1 digests and
charges exactly 20 bytes per digest; counts are validated but not retained.
Deterministic least-recently-used eviction removes expired entries first, then
evicts until both caps hold. A valid cached response is usable during upstream
failure. A cache miss plus failure returns `ErrBreachUnavailable` and setting a
password fails closed.

Argon2id is version 19, 65,536 KiB, 3 iterations, parallelism 1, 16 random salt
bytes, and 32 result bytes. The closed PHC encoding is at most 192 ASCII bytes.
Parsing rejects duplicate, missing, unknown, over-budget, or noncanonical
parameters before allocation. Each process runs at most two hash/verify jobs and
queues at most 16. The seventeenth waiter fails immediately with
`ErrHashAdmission`. Unknown and no-credential login use one startup-generated
dummy encoding through the same admission path. Successful verification reports
`NeedsRehash` without changing the login result.

Bearer tokens are exactly 32 random bytes encoded as 43 unpadded base64url
characters. Only SHA-256 digests are stored. Decode rejects any other length,
padding, alphabet, or noncanonical spelling before constant-time digest
comparison.

```go
package password

type BreachChecker interface {
  Breached(context.Context, string) (bool, error)
}
type Policy struct {
  blocklist *Blocklist
  breach BreachChecker
}
type CheckResult struct { Normalized string }
func (p *Policy) CheckNew(ctx context.Context, raw string) (CheckResult, error)

type VerifyResult struct { Match bool; NeedsRehash bool }
type Hasher struct {
  policy HashPolicy
  entropy io.Reader
  admission *Admission
}
func (h *Hasher) Hash(ctx context.Context, normalized string) (string, error)
func (h *Hasher) Verify(ctx context.Context, encoded, normalized string) (VerifyResult, error)
func (h *Hasher) VerifyDummy(ctx context.Context, normalized string) error

type Token struct { Raw string; Digest [32]byte }
func NewToken(r io.Reader) (Token, error)
func DigestToken(raw string) ([32]byte, error)
```

## D3 — Storage schema and outbox state

Migration `00008_add_password_auth.sql` adds:

- `password_credentials`: `user_id` primary/cascading FK, `encoded_hash` 1–192
  bytes, `created_at`, `changed_at`, and `changed_at >= created_at`.
- `password_registrations`: UUIDv7 ID, unique canonical `email` citext, `name`
  1–400 bytes, `encoded_hash` 1–192 bytes, unique 32-byte `token_digest`,
  `created_at`, and exact `expires_at = created_at + 24 hours`.
- `password_reset_tokens`: UUIDv7 ID, unique cascading `user_id`, unique 32-byte
  `token_digest`, `created_at`, and exact
  `expires_at = created_at + 30 minutes`.
- `auth_email_jobs`: UUIDv7 ID; kind `verify|reset|password_changed`; state
  `pending|leased|sent|terminal`; exactly one nullable scope FK
  (`registration_id`, `reset_token_id`, or `user_id`) matching the kind;
  optional 32-byte token digest required only for verify/reset; attempts 0–8;
  `expires_at` no later than 24 hours after creation; and the exact state matrix
  below.

| State      | `key_id`/`nonce`/`ciphertext` | `next_attempt_at` | `lease_owner`/`lease_expires_at` | Outcome timestamp  |
| ---------- | ----------------------------- | ----------------- | -------------------------------- | ------------------ |
| `pending`  | All non-null and bounded      | Non-null          | Both null                        | Both null          |
| `leased`   | All non-null and bounded      | Null              | Both non-null                    | Both null          |
| `sent`     | All null                      | Null              | Both null                        | `sent_at` only     |
| `terminal` | All null                      | Null              | Both null                        | `terminal_at` only |

When present, key ID is 1–64 printable ASCII, nonce is exactly 12 bytes, and
ciphertext is 1–4,112 bytes. A claim increments attempts before entering
`leased`; leased/sent attempts are 1–8, pending is 0–7, and terminal is 0–8
because stale/expired jobs may terminate without a send.

Deleting/replacing a registration or reset token cascades its unsent jobs. A
password notification job belongs to its user and cascades on account deletion.
Claim order is `(next_attempt_at, created_at, id)` with
`FOR UPDATE SKIP LOCKED`. A claim batch is at most 10 and a worker owns at most
two concurrent sends. Lease length is 30 seconds. Stale leases return to pending
only through a bounded query. Cleanup deletes at most 200 expired registrations,
expired reset tokens, and sent/terminal jobs older than seven days per tick.

The outbox plaintext is closed JSON capped at 4,096 bytes. AES-256-GCM uses a
12-byte random nonce and AAD
`aboutme.auth-email.v1\x00<job-id-bytes>\x00<kind>\x00<key-id>`. Ciphertext is
at most 4,112 bytes. The key ring has exactly one active key and at most one
previous 32-byte key; duplicate/unknown IDs and more than two keys fail config.
Startup reads distinct key IDs referenced by pending/leased jobs and fails
readiness unless the ring contains all of them. Production may remove a previous
key only after no live job references it; a second rotation is rejected while
the older key remains referenced.

## D4 — Transaction and session fencing

All account/session creation uses `store.Pool` transactions. Provider login and
password login both call one session primitive only after `GetUserForUpdate`.
The primitive inserts the existing opaque session format and never commits its
own transaction.

```go
type SessionIssue struct { RawToken string; Session store.Session }
func (m *SessionManager) IssueTx(
  context.Context, *store.Queries, store.User, string, string,
) (SessionIssue, error)
```

`Issue` remains only as a compatibility wrapper that opens a transaction, locks
the user, calls `IssueTx`, and commits. No provider callback bypasses it. ADR
0015's `BeginSessionRotation` admission update stays separate as required; the
successor insert then runs in a short transaction that locks the user, re-reads
the predecessor as live, calls the transaction-scoped insert, and commits. If
reset won first, the revoked predecessor cannot mint a successor.

Password reset locks user → credential → reset token → sessions, writes the new
credential, consumes the token, revokes every session, enqueues the
notification, and commits. A provider or password login cannot insert a session
past that fence without acquiring the same user lock.

Password add/change locks user → credential → current session → remaining
sessions. It creates fresh random session and CSRF material with
`rotated_from = NULL`, preserves the old current session's absolute expiry, user
agent, IP, and new `reauthenticated_at`, revokes every old session including the
current row, enqueues notification, and commits. This is forced credential
replacement, not ADR 0015 delivery rotation. A lost HTTP response leaves the old
cookie revoked and the new token unreachable; it never preserves a pre-change
credential or session.

## D5 — Provider identity resolution

Each callback first validates/consumes the OAuth transaction and resolves
`(provider, subject)` in a transaction. If the identity exists, it loads and
locks the owning user and issues the session without fetching or accepting an
email claim for account identity.

For a new subject, the provider adapter obtains a required verified email,
passes it through D1, and calls one transaction that reads canonical-email
ownership, creates the user plus identity atomically, then issues a session.
PostgreSQL's unique email and provider-subject constraints arbitrate absent-row
races. After either unique violation, the callback rolls back the complete
attempted user transaction and re-reads `(provider, subject)` first. If that
subject now exists, it follows the returning-login path and issues a session for
the owning user. Otherwise it re-reads canonical-email ownership and returns the
current closed `email_already_registered` redirect. A failed identity insert
cannot orphan a user. Only authenticated linking can return a cross-user subject
conflict.

Authenticated link resolves only the stable subject, locks the current user, and
inserts the identity. Provider email is not requested or passed across this
boundary. A linked subject cannot move between accounts.

## D6 — Password service outcomes and rate limits

The exact route paths and wire schemas are owned by Task 02. Internal errors are
closed sentinel values. Handlers map only those sentinels; raw dependency errors
never cross the boundary.

Every password route also passes the existing outer 300/minute per-IP limiter.
Route policies use ADR 0018 bounded stores with 10,000 active keys and one
overflow bucket:

| Route class        | Key                    | Limit                  |
| ------------------ | ---------------------- | ---------------------- |
| Login admission    | client IP              | 30/minute              |
| Login failures     | HMAC(canonical email)  | 10 failures/15 minutes |
| Register/forgot    | HMAC(canonical email)  | 5/hour                 |
| Register/forgot    | client IP              | 20/hour                |
| Verify/reset token | client IP              | 10/hour                |
| Add/change/reauth  | `(user ID, client IP)` | 10/hour                |

Email keys are
`HMAC-SHA-256(rate-secret, "aboutme.password-rate.v1\x00" + canonical-email)`
and only the 32-byte digest enters a bucket. The runtime secret is exactly 32
bytes and distinct from mail keys.

```go
type FailureState struct { Exhausted bool; RetryAfterSeconds int }
type PasswordFailureLimiter interface {
  State(now time.Time, emailKey [32]byte) FailureState
  RecordFailure(now time.Time, emailKey [32]byte) FailureState
  ClearSuccess(emailKey [32]byte)
}

type RateDecision struct {
  Allowed bool
  RetryAfterSeconds int
}
type admissionLimiter interface {
  Admit(time.Time, string) RateDecision
}
type PasswordRatePolicies struct {
  emailHMACKey [32]byte
  loginIP admissionLimiter
  registerOrForgotIP admissionLimiter
  registerOrForgotEmail admissionLimiter
  verifyOrResetIP admissionLimiter
  accountMutation admissionLimiter
  failures PasswordFailureLimiter
}
func NewPasswordRatePolicies(
  emailHMACKey [32]byte,
) (*PasswordRatePolicies, error)
func (p *PasswordRatePolicies) AdmitLoginIP(
  now time.Time, clientIP netip.Addr,
) RateDecision
func (p *PasswordRatePolicies) AdmitRegisterOrForgotIP(
  now time.Time, clientIP netip.Addr,
) RateDecision
func (p *PasswordRatePolicies) AdmitRegisterOrForgotEmail(
  now time.Time, canonicalEmail string,
) RateDecision
func (p *PasswordRatePolicies) AdmitVerifyOrResetIP(
  now time.Time, clientIP netip.Addr,
) RateDecision
func (p *PasswordRatePolicies) AdmitAccountMutation(
  now time.Time, userID uuid.UUID, clientIP netip.Addr,
) RateDecision
func (p *PasswordRatePolicies) LoginFailureState(
  now time.Time, canonicalEmail string,
) FailureState
func (p *PasswordRatePolicies) RecordLoginFailure(
  now time.Time, canonicalEmail string,
) FailureState
func (p *PasswordRatePolicies) ClearLoginSuccess(canonicalEmail string)
```

The constructor creates five shared admission stores plus the failure store;
each is capped at 10,000 active keys plus one overflow bucket. Register and
forgot share both their IP and email budget. Verify and reset share their IP
budget. Reauth and set/change share their account+IP budget. A zero HMAC key is
rejected. Email methods canonicalize through D1, HMAC immediately, and pass only
the 32-byte key to a limiter. `Allowed=true` requires retry zero;
`Allowed=false` requires a positive ceiling-rounded retry value. IP methods
accept only the already validated canonical `api.ClientIP` result.

Failure state never blocks verification of an otherwise admitted attempt. After
the hash result, a correct password clears debt and may succeed. A wrong
password records a failure; the tenth and later failures return 429 with exact
`Retry-After`, while failures one through nine return the identical 401. Unknown
and provider-only accounts follow the same dummy-hash and failure path. The
first failure opens one fixed 15-minute window; later failures do not extend it.
After expiry, the next recorded failure starts a new window. Unexhausted state
has retry zero; exhausted state has the ceiling-rounded positive time remaining
in that fixed window.

## D7 — Email delivery and local capture

The worker polls once per second, claims at most 10 jobs, and sends at most two
at once. Every send has a 10-second deadline. Temporary failures use injected
full jitter over `min(30s * 2^(attempt-1), 1h)` and never schedule beyond job
expiry. Permanent failure, expiry, or failed attempt 8 marks terminal. SES
acceptance marks sent. Both outcomes clear nonce, ciphertext, key ID, and lease.
An ambiguous send is classified temporary and may duplicate delivery; token
authority and single use make the duplicate harmless.

For each send, the worker begins one bounded authorization transaction. It locks
the scoped registration/reset token/user first, then the exact leased job,
decrypts, rehashes any raw token, and confirms current authority and lease
ownership. It holds both locks through the at-most-10-second sender call and
finalizes sent/terminal/temporary state before commit. Replacement uses the same
scope-before-job order, so it commits before send and cancels the job, or waits
until the handoff finishes; it cannot commit in a check/send gap. A stale job
becomes terminal without send. Decryption and authority failures are closed
signals; plaintext never reaches logs.

Production uses SES v2 in exact region `ap-southeast-1`, a configured verified
From address, and a required configuration-set name. SDK retries are disabled;
the worker owns retry timing. No production credential field exists.

Native capture uses `127.0.0.1:20091`; HTTPS overlay capture uses
`127.0.0.1:20444`. It retains at most 50 messages and 256 KiB total, rejects an
individual message over 16 KiB, and evicts oldest accepted messages.
`POST /capture` requires a 32-byte bearer secret from the mode-0600 harness
state. `GET /`, `GET /api/messages`, and `DELETE /api/messages` bind loopback
only and require the same secret; HTML escapes all values. Production config
rejects the capture sender and never starts this command.

The Playwright container already uses host networking. Task 12 mounts the
capture secret with the Caddy root as read-only mode 0600 input and uses Node
control traffic, not a page/browser request, to poll capture. Browser network
policy remains restricted to `https://localhost:20443`.

## D8 — Web and fragment handling

Nuxt roots are `/register`, `/verify-email`, `/forgot-password`, and
`/reset-password`. Login keeps all provider actions and adds email/password. All
pages use the integrated Nova/Zinc/Emerald tokens and existing light/dark theme;
Phase PA does not redesign the shell.

Verification/reset pages synchronously read `location.hash`, accept exactly one
`token` key, call `history.replaceState` before any network call, and keep the
token only in component-local memory until POST completes. They render
`Referrer-Policy: no-referrer`, load no third-party resource, never put the
token in query/history/log/analytics, and replace it with an empty string after
submission. Success copy is uniform: verification says “Email verified. Sign
in.” even when a provider signup won; reset says “Password reset. Sign in.”

Settings extend `/app/settings/sessions` with password status and add/change
controls. Provider-only users complete an existing provider reauth round trip;
password users may use password reauth. Password and linked-provider email are
never displayed as a linkage decision.

## D9 — OpenAPI and response bytes

All seven operations have explicit 4,096-byte route limits, strict JSON, exact
status rows, `Cache-Control: no-store, no-transform`, and no response schema
that contains email, token, provider, hash, or dependency detail. Registration
and forgot return byte-identical:

```text
{"data":{"accepted":true}}\n
```

with 202 and `application/json; charset=utf-8`. Login failure is exact
`{"error":{"code":"authentication_failed","message":"authentication failed"}}\n`;
reauth failure changes only code/message. Successful mutations have 204 and no
body. Token invalid is 400 with `credential_token_invalid`. Password policy 422
details contain only a closed issue code from `length|common|breached`, never
the rejected value. Authenticated-route absence uses the existing 401
`authentication_required`. Exact Origin/Referer or synchronizer-token failure
uses 403 `csrf_rejected`; an expired recent-reauthentication window uses 403
`reauth_required`. Task 02 freezes the per-operation status/code matrix.

## D10 — UAT fixture and evidence

Task 12 owns one deterministic database fixture with three accounts: provider
only, password plus provider, and a second password account used for collision
proof. Provider mock exposes different verified emails for link tests. Local
capture is reset before the run.

One Playwright process proves registration → captured verification → no session
→ login; existing provider-only login → reauth → add password; link a provider
whose email differs; reset → every prior session rejected → new password login;
old password and token replays rejected. Browser requests stay on the trusted
HTTPS origin. Node-only capture polling uses the read-only secret and neither
token nor email enters the bounded evidence file.
