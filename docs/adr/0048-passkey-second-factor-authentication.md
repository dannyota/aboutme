# 0048: Passkey second-factor authentication

Status: Accepted (2026-09-20)

## Context

Password and provider authentication issue the same opaque browser session.
Neither protects an account after its primary credential is stolen. The owner
selected optional passkeys first, TOTP later, and single-use recovery codes. The
service must enforce an enrolled factor for browser and connected-agent
authority without turning password reset, provider relinking, email, support, or
an operator into a weaker recovery path.

WebAuthn parsing, COSE key handling, authenticator-data validation, attestation
decoding, and signature verification are security-critical protocol work. The
server has no WebAuthn dependency today. The release also needs a rollback
boundary because an older image would issue a full session after primary
authentication alone.

## Decision

**Passkeys are optional second factors.** A password or linked provider remains
the primary credential. An enrolled account receives a bounded pending
authentication after primary verification. Only an active passkey or recovery
code may complete it. Passkeys are not password replacements, and the service
does not offer usernameless authentication.

**One authentication epoch fences all authority.** Sessions, connected-agent
grants, and authorization codes bind the user's current epoch. Factor changes
advance it, revoke other sessions and grants, and rotate the deliberate current
session. Every issuer and sensitive mutation uses the common lock order and
rechecks the concrete caller session under the user lock. Ordinary session
rotation copies the epoch and nullable factor-proof time unchanged.

**Recovery codes are the only lost-factor path.** First enrollment creates ten
single-use 128-bit codes and returns them once. PostgreSQL stores only their
domain-separated digests. Password reset and provider authentication preserve
factor enforcement. Losing every factor and code permanently loses application
access. The owner accepts the related first-enrollment denial risk.

**WebAuthn has one exact relying-party policy.** Registration and assertion use
the canonical origin host as RP ID, exact origin, required user verification,
resident credentials, no attestation, ES256 then RS256, 32-byte challenges, and
five-minute ceremonies. Cross-origin and top-origin ceremonies fail. The server
stores explicit credential fields and verifies account, ceremony, purpose,
epoch, user handle, signature, and counter binding in one transaction.

**The server pins `github.com/go-webauthn/webauthn` v0.18.2.** The upstream
[v0.18.2 release](https://github.com/go-webauthn/webauthn/releases/tag/v0.18.2)
is the latest tagged release on the decision date. Its
[package documentation](https://pkg.go.dev/github.com/go-webauthn/webauthn@v0.18.2/webauthn)
supports Go 1.27, multi-factor ceremonies, exact origin binding, explicit SQL
storage, and unsolicited-extension rejection. Its BSD-3-Clause license permits
distribution. V0.18.2 fixes session-challenge validation at ceremony completion
and authorization for user-verification initialization, both on this security
boundary. The server uses the documented `webauthn` and `protocol` APIs but owns
HTTP decoding, error mapping, ceremony persistence, transactions, and typed SQL
storage. It never stores a serialized library credential or session object.
`RPAllowCrossOrigin` remains false, no extension is requested, and client
extension output must be empty.

The dependency is pre-v1 and may make breaking changes. V0.4.2 pins the exact
version in `go.mod` and `go.sum`. An upgrade needs release-note review, hostile
origin and signature tests, stored-credential compatibility proof, and normal
dependency review. Self-written WebAuthn cryptography and a browser-only
verifier are rejected.

**V0.4.2 has a closed wire contract.**
[The passkey contract](../design/passkey-second-factor-contract.md) owns pending
login and reauthentication, exact WebAuthn JSON, response and error shapes,
recovery download bytes, transactional mail events, migration loss, and the
explicit absence of TOTP routes and fields. OpenAPI and generated clients must
match that contract in the implementation release.

**Enrollment uses a durable release fence.** The
[release-fence contract](../design/passkey-release-fence.md) owns its monotonic
DynamoDB minimum, serialized operation lock, roles, and failure order. Direct
AWS administration remains a named privileged bypass and must prove factor
compatibility first.

## Compatibility, migration, and loss

Migration `00004_passkey_second_factor.sql` adds epoch and passkey state with
defaults that preserve unenrolled behavior. It deletes outstanding 60-second
authorization codes before adding their exact grant and epoch bindings. No
grant, token, account, session, resume, or mail job is deleted.

Enrollment defaults off. Mixed old and new application code is safe only before
the flag becomes reachable. Older web clients receive no session for an enrolled
account and therefore fail closed. The release changes no resume schema,
renderer, public page, PDF, or MCP tool schema.

## Consequences

- Passkey, recovery, mail, session, grant, OAuth, deployment, and bilingual web
  support ship together in v0.4.2.
- Every numeric limit is enforced from the design's budget table.
- Turning enrollment off stops new credentials but preserves verification,
  removal, state reads, and recovery.
- After production enrollment is possible, rollback below v0.4.2 is invalid.
- The authenticator-app release may add TOTP without changing stored passkeys or
  the pending boundary.
