# 0017: Optional second factors: passkeys, authenticator apps, and recovery codes

Status: Accepted (2026-09-20, 2026-09-23). The owner approved the passkey
choices and, on 2026-09-22 and 2026-09-23, the authenticator-app product choices
and failure-budget rules after a fresh review.

## Context

Password and provider authentication issue the same opaque browser session.
Neither protects an account after its primary credential is stolen. The owner
selected optional passkeys first, authenticator-app codes (TOTP) next, and
single-use recovery codes. The service must enforce an enrolled factor for
browser and connected-agent authority without turning password reset, provider
relinking, email, support, or an operator into a weaker recovery path.

WebAuthn parsing, COSE key handling, authenticator-data validation, attestation
decoding, and signature verification are security-critical protocol work. TOTP
is widely supported but not phishing-resistant, and its server must retain a
secret that can generate codes, so digest-only storage is impossible. An older
image would issue a full session after primary authentication alone, so each
release needs a rollback boundary.

## Decision

### Shared boundary

**Factors are optional second factors.** A password or linked provider remains
the primary credential. An enrolled account receives a bounded pending
authentication after primary verification. Only an active passkey, TOTP code, or
recovery code may complete it. Passkeys are not password replacements, and there
is no usernameless authentication.

**One authentication epoch fences all authority.** Sessions, connected-agent
grants, and authorization codes bind the user's current epoch. Factor
completion, replacement, and removal advance it, revoke other sessions and
grants, and rotate the deliberate current session, under the same recent-proof,
CSRF, Origin, rate, and notification rules. Every issuer and sensitive mutation
uses the common lock order (user, current session when present, policy,
credential, enrollment, then pending authentication) and rechecks the concrete
caller session under the user lock. Ordinary session rotation copies the epoch
and nullable factor-proof time unchanged.

**Recovery codes are the only lost-factor path.** First enrollment of any factor
on an unenrolled account creates ten single-use 128-bit codes and returns them
once. PostgreSQL stores only domain-separated digests. Adding or replacing a
factor keeps an existing set; removing the final active factor, counted across
passkeys and TOTP, deletes it. Password reset and provider authentication keep
factor enforcement. Password, provider, email, support, and operators do not
recover a lost factor; losing every factor and code permanently loses
application access. The owner accepts the related first-enrollment denial risk.

**Enrollment uses a durable release fence.** The
[release-fence contract](../design/passkey-release-fence.md) owns the monotonic
DynamoDB minimum release, serialized operation lock, roles, and failure order.
Each factor release deploys with enrollment off; after flag-off proof, the
serialized production operation raises the floor (v0.4.2 for passkeys, v0.4.7
for TOTP); only then may enrollment turn on. Disabling enrollment later never
lowers the floor or turns off verification. Direct AWS administration remains a
named privileged bypass and must prove factor compatibility first.

### Passkeys

WebAuthn uses one exact relying-party policy: the canonical origin host as RP
ID, exact origin, required user verification, resident credentials, no
attestation, ES256 then RS256, 32-byte challenges, and five-minute ceremonies.
Cross-origin and top-origin ceremonies fail. The server stores explicit
credential fields and verifies account, ceremony, purpose, epoch, user handle,
signature, and counter binding in one transaction.

The server pins `github.com/go-webauthn/webauthn` v0.18.2 (BSD-3-Clause), which
supports multi-factor ceremonies, exact origin binding, explicit SQL storage,
and unsolicited-extension rejection, and fixes session-challenge validation at
ceremony completion. The server uses the documented `webauthn` and `protocol`
APIs but owns HTTP decoding, error mapping, ceremony persistence, transactions,
and typed SQL storage. It never stores a serialized library credential or
session object. `RPAllowCrossOrigin` stays false, no extension is requested, and
client extension output must be empty. The dependency is pre-v1; an upgrade
needs release-note review, hostile origin and signature tests, stored-credential
compatibility proof, and dependency review. Self-written WebAuthn cryptography
and a browser-only verifier are rejected.

[The passkey contract](../design/passkey-second-factor-contract.md) owns pending
login and reauthentication, exact WebAuthn JSON, response and error shapes,
recovery download bytes, transactional mail events, and migration loss.

### Authenticator app (TOTP)

**Profile.** RFC 6238 with HMAC-SHA-1, a 20-byte random secret, six digits, a
30-second period, and the previous, current, and next time steps. A matched step
is accepted only once. The settings UI describes the phishing risk and
recommends passkeys when available.

**One active credential.** Starting setup creates one ten-minute, session-bound
encrypted enrollment and supersedes any earlier incomplete setup. The active
credential stays usable until successful proof atomically installs or replaces
it. Enrollment proof becomes the first used step.

**Shared policy identity.** When no factor policy exists, first TOTP completion
generates a random 32-byte unique WebAuthn user handle. It tries at most three
candidates, persists the winner on the policy, and later passkey enrollment
reuses that exact value.

**Sealed secrets with a separate key ring.** AES-256-GCM binds ciphertext to its
account, row, record kind, format, and key identifier. Each key ID is derived
from its key value. Runtime holds one active key and at most one previous key,
chosen from two production parameter slots by nonsecret OpenTofu variables. New
writes use the active key. Successful use rotates lazily, and a bounded one-shot
task in its own family rotates dormant rows before the previous key is removed.
Only the app ECS execution role reads the exact TOTP parameter ARNs; scheduled
jobs receive no TOTP key access. An unknown key ID or authenticated-decryption
failure fails closed for that TOTP row only, keeps `/readyz` green, and raises a
dedicated alarm. A malformed ring fails startup.

**Guessing bound.** Each TOTP credential carries a consecutive-failure count and
a cool-down that grows from 15 minutes to 24 hours, independent of client IP,
and never blocks passkeys or recovery codes. A password reset does not clear it;
a valid code clears it only after 24 hours without a failure. Attempt mail is
capped at one per account per hour. A password holder gets at most 35 guesses in
the first day and 5 a day after that, from any number of client IPs.

**Local provisioning.** The server returns one canonical `otpauth` URI and a
grouped Base32 secret once. The issuer is the canonical origin hostname and the
label includes the canonical account email. The browser renders the QR code with
a bundled encoder; no external QR or provisioning service sees either value.

The [authenticator-app contract](../design/totp-second-factor-contract.md) owns
the exact API, storage, mail, mixed-version, bounds, and release rules. The
[key-management design](../design/totp-key-management.md) owns sealing, the key
ring, rotation, and key failures. The shared rules are in
[second-factor authentication](../design/second-factor-authentication.md).

## Alternatives

- HMAC-SHA-256 or eight digits would strengthen TOTP but reduce authenticator
  compatibility. Zero skew rejects ordinary phone clock drift; two steps widen
  replay. One-step skew with single-use step storage follows common defaults.
- Multiple named TOTP credentials would add naming, listing, per-device removal,
  and recovery ambiguity.
- Hashing the TOTP secret would prevent verification. Database-native encryption
  would put key use and plaintext handling outside the application's typed
  bounds; application-layer encryption keeps a database backup alone
  insufficient.
- Server-rendered QR images would add a secret-bearing endpoint and cache
  surface.

## Compatibility, migration, and loss

The passkey migration adds epoch and passkey state with defaults that keep
unenrolled behavior, and deleted outstanding 60-second authorization codes
before adding their grant and epoch bindings. The TOTP migration adds credential
and enrollment tables and extends the closed auth-mail kind and scope
constraints for three TOTP events. Neither deletes a durable account, session,
grant, token, resume, or mail job; superseded, expired, and consumed enrollments
are the only automatic loss. No resume schema, renderer, public page, PDF, or
MCP tool schema changes.

Older web clients receive no session for an enrolled account and fail closed;
they may need a refresh to use a TOTP-only account. Mixed old and new tasks are
safe only while the new factor's enrollment is off and no such credential
exists. After production enrollment is possible, supported deploy, rollback, and
restore paths reject images below the floor; rollback uses the floor release or
later, or a forward fix.

## Consequences

- Every numeric limit is enforced from the design's budget table.
- Turning enrollment off stops new credentials but keeps verification, removal,
  state reads, and recovery.
- A database snapshot without the runtime key cannot reveal a TOTP secret; an
  application compromise with a live key can. Passkeys remain the recommended
  phishing-resistant method.
- Every factor mutation sends bilingual, secret-free security mail in the same
  transaction.
- TOTP state never enters account export, public surfaces, MCP, logs, metrics,
  traces, CI evidence, or production proof evidence.
- Key rotation needs a bounded re-encryption run before old-key removal.

## History

- Former ADR 0048 (2026-09-20): passkey second factor, recovery codes, the
  authentication epoch, the wire contract, and the release fence (v0.4.2).
- Former ADR 0049 (2026-09-23): authenticator-app second factor with sealed
  secrets and a derived-ID key ring, shared recovery, and floor v0.4.7.
