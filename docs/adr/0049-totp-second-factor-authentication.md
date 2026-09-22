# 0049: Authenticator-app second-factor authentication

Status: Proposed. The owner approved its product choices on 2026-09-22; a fresh
review is pending.

## Context

V0.4.2 adds optional passkeys, pending authentication, recovery codes, an
authentication epoch, and a durable minimum-release fence. The owner selected
authenticator-app codes as the next optional method. TOTP must reuse that
boundary without making a symmetric secret, clock tolerance, replacement flow,
or rollback into a bypass.

TOTP is widely supported but is not phishing-resistant. Its server must retain a
secret that can generate codes, so digest-only storage is impossible. An old
image can enforce passkeys but cannot serve a TOTP-only account.

## Proposed decision

**TOTP is an optional second factor.** The service uses RFC 6238 with
HMAC-SHA-1, a 20-byte random secret, six digits, a 30-second period, and the
previous, current, and next time steps. It accepts a matched step only once. The
settings UI describes the phishing risk and recommends passkeys when available.

**An account has one active TOTP credential.** Starting setup creates one
ten-minute, session-bound encrypted enrollment and supersedes any earlier
incomplete setup. The active credential remains usable until successful proof
atomically installs or replaces it. Enrollment proof becomes the first used
step.

**TOTP reuses the passkey authority boundary.** Password and provider primary
proof still create a pending authentication for an enrolled account. Any listed
passkey, TOTP code, or recovery code may complete it. Factor completion,
replacement, and removal use the same epoch, session rotation, grant revocation,
recent-proof, CSRF, Origin, rate, and notification rules. Their lock order is
user, current session when present, policy, credential, enrollment, then pending
authentication when applicable.

**First TOTP creates the shared policy identity.** When no factor policy exists,
completion generates a random 32-byte unique WebAuthn user handle. It tries at
most three candidates, persists the winner on the policy, and later passkey
enrollment reuses that exact value.

**Recovery stays shared and explicit.** First TOTP enrollment on an unenrolled
account creates the existing ten single-use recovery codes. Adding or replacing
TOTP preserves an existing set. Removing the final active factor, counted across
passkeys and TOTP, deletes it. Password, provider, email, support, and operators
do not recover a lost factor.

**Secrets use a separate authenticated key ring.** AES-256-GCM binds ciphertext
to its account, row, record kind, format, and key identifier. Each key ID is
derived from its key value. Runtime holds one active key and at most one
previous key, chosen from two production parameter slots by nonsecret OpenTofu
variables. New writes use active. Successful use rotates lazily, and a bounded
one-shot task in its own family rotates dormant rows before the previous key is
removed. Only the app ECS execution role reads the exact TOTP parameter ARNs.
Scheduled jobs receive no TOTP key access. An unknown key ID or
authenticated-decryption failure fails closed for that TOTP row only, keeps
`/readyz` green, and raises a dedicated alarm.

**Guessing has a per-account bound.** Each TOTP credential carries a
consecutive-failure count and a cool-down that grows from 15 minutes to 24
hours. It is independent of client IP and never blocks passkeys or recovery
codes. Attempt mail is capped at one per account per hour.

**The browser renders provisioning locally.** The server returns one canonical
`otpauth` URI and a grouped Base32 secret once. The issuer is the canonical
origin hostname and the label includes the canonical account email. No external
QR or provisioning service receives either value.

**The release floor advances before enrollment.** V0.4.3 deploys with enrollment
off and valid keys. After flag-off proof, the serialized production operation
raises the existing durable floor from v0.4.2 to v0.4.3. Only then may the flag
turn on. Disabling enrollment later does not lower the floor or verification.

The exact API, storage, mail, mixed-version, bounds, and release rules live in
the [authenticator-app contract](../design/totp-second-factor-contract.md). The
[key-management design](../design/totp-key-management.md) owns sealing, the key
ring, rotation, and key failures.

## Alternatives

HMAC-SHA-256 or eight digits would strengthen the primitive but reduce
authenticator compatibility. A zero-skew policy would reject ordinary phone
clock drift. A two-step skew window would increase replay opportunity. The
proposed profile follows common authenticator defaults and couples one-step skew
with single-use step storage.

Multiple named TOTP credentials would support several devices but would add
credential naming, listing, per-device removal, and more recovery ambiguity.
V0.4.3 keeps one active credential and safe replacement.

Hashing the secret would prevent verification. Application-layer authenticated
encryption keeps database backups alone insufficient and supports controlled key
rotation. Database-native encryption would put key use and plaintext handling
outside the application's typed bounds.

Server-rendered QR images would create another secret-bearing endpoint and cache
surface. A bundled browser encoder keeps the provisioning value inside the
authenticated page.

## Compatibility, migration, and loss

Migration `00005_totp_second_factor.sql` adds credential and enrollment tables
and extends both closed auth-mail kind and scope constraints for the three TOTP
events. Existing accounts, passkeys, recovery codes, pending rows, sessions,
grants, resumes, and mail stay unchanged. The release deletes no durable data.
Superseded, expired, and consumed enrollments are the only automatic loss.

Mixed v0.4.2 and v0.4.3 tasks are safe only while TOTP enrollment is false and
no TOTP credential exists. New clients treat absent new fields as false. Older
clients cannot bypass pending authentication, but they may require a refresh to
use a TOTP-only account.

After the floor reaches numeric release 4003, supported deploy, rollback, and
restoration paths reject lower images. A post-enablement rollback uses v0.4.3 or
later, or a forward fix.

## Security and operational consequences

- A database snapshot without the runtime key cannot reveal a TOTP secret.
- An application compromise with a live key can reveal TOTP secrets. Passkeys
  remain the recommended phishing-resistant method.
- Losing every factor and recovery code still causes permanent application-level
  account loss.
- A malformed ring fails startup. An unknown key ID or authenticated-decryption
  failure fails closed for the affected TOTP row, raises a dedicated alarm, and
  leaves readiness, passkeys, and recovery available.
- Every TOTP mutation sends bilingual, secret-free security mail in the same
  transaction.
- TOTP state never enters account export, public surfaces, MCP, logs, metrics,
  traces, CI evidence, or production proof evidence.
- Key rotation needs a bounded re-encryption run before old-key removal.
- A password holder gets at most 35 TOTP guesses in the first day and 5 a day
  after that, from any number of client IPs.

## Approval

The owner approved all seven product choices in the contract on 2026-09-22. This
ADR becomes Accepted after the fresh design review closes. Implementation starts
only after that acceptance.
