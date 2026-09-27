# 0015: Password accounts, identity linking, and session rotation

Status: Accepted (2026-08-12, 2026-08-16).

## Context

Authentication began provider-only (Google, GitHub, LinkedIn) with opaque
server-side sessions, which excluded people who do not hold or do not want to
use a provider. A password must coexist with providers without making email the
identity key, splitting session authority, or adding an account-recovery
surface.

Replacing a session cookie on the first request after the rotation age has two
failure points: concurrent requests can race to rotate, and the response that
carries the successor cookie can be lost. Revoking the predecessor at insert
time can strand the user; leaving it valid without a bound keeps a superseded
credential alive too long.

## Decision

### Password and identity

**Email and password are additive.** An account may hold zero or one password
credential and any number of linked provider identities. Opaque sessions remain
the only browser session mechanism. Provider signup creates an account from its
verified email with no password; a signed-in provider user may add one later.

**The application owns authentication authority.** Password storage and
verification live in the application next to provider identities and sessions.

**Email is an account address, not an identity key.** One canonical parser
shares the exact ASCII addr-spec grammar across provider account creation,
password registration, login lookup, database writes, and rate-limit keys. A
provider identity is resolved by its `(provider, subject)` only. Provider email
creates a new account and is otherwise never read, stored, compared, displayed,
or synchronized. Accounts are never merged by email.

**Registration verifies before it creates.** A password registration stores a
pending credential and an encrypted verification-mail job; no user or session
exists until the single-use token is consumed. The unique user-email constraint
arbitrates a concurrent provider signup.

**Every session issuer is fenced on the user lock.** Provider login, password
login, and the rotation successor insert sessions only while holding the user
lock. Password reset locks the same row, revokes every session, and never logs
in. Adding or changing a password creates one fresh non-lineage current session
and revokes all others atomically.

**No password removal or email change.** Both stay out of scope to avoid
last-authenticator and account-recovery rules that are not designed.

### Session rotation

A conditional update admits at most one rotation winner for a predecessor. A
unique lineage key permits at most one successor row. The successor inherits the
predecessor's user, absolute expiry, recent-reauth time, user agent, and IP;
rotation does not extend any of them.

The winner initially parks the predecessor's deadline at
`min(now + 24 hours, absolute expiry)`. The successor's first authenticated use
proves that its cookie reached a client and changes the predecessor deadline to
`min(existing predecessor deadline, now + 60 seconds)`. This transition can only
shorten the deadline; first use never revives or extends a predecessor whose
parked or absolute deadline is earlier. Concurrent losers continue with the
predecessor while it is live and never mint another successor.

The admission update and successor insert are separate statements. If the insert
fails, the predecessor remains usable only to its parked deadline. If the
response is lost after insert, the unreachable successor expires normally and
the predecessor remains usable to its parked deadline. These outcomes are
bounded and may require a new login; they never create a second successor.

Revoking either member of a rotation pair also revokes its live lineage partner,
so a device action cannot leave the paired credential active.

## Rejected alternatives

- **Cognito-backed passwords.** Splits account and session authority across two
  systems; moving every provider and session to Cognito locks identity to one
  vendor.
- **Username sign-in.** Adds a second, non-email identifier the product does not
  otherwise expose.
- **Email-keyed provider linking or merge.** Makes a provider-reported email an
  identity authority and enables account takeover or accidental merge.

## Consequences

- Passwords, tokens, and mail payloads are stored only as Argon2id hashes,
  SHA-256 digests, and AES-256-GCM ciphertext; plaintext never persists.
- Password routes add exact Origin, CSRF, session, and rate-limit admission and
  byte-identical enumeration responses.
- A stolen predecessor can remain usable until the parked deadline when
  successor delivery is never proved. Monitoring must expose this state.
- Rotation convergence depends on first use, not on assuming that `Set-Cookie`
  was delivered.
- Tests cover concurrent winners, lost insert, lost response, first-use grace,
  absolute expiry, and lineage revocation. First-use boundary cases pin an
  existing predecessor deadline before, exactly at, and after
  `now + 60 seconds`; concurrent successor uses never move any of those
  deadlines outward.
- Second factors (ADR 0017) sit behind primary authentication and add the
  authentication epoch to every session.

## History

- Former ADR 0015 (2026-08-12): session rotation waits for successor delivery.
  Unchanged.
- Former ADR 0025 (2026-08-16): password authentication and provider identity
  linking. Unchanged; its migration and mail-activation notes were one-off
  launch steps.
