# 0025 — Password authentication and provider identity linking

Status: Accepted (2026-08-16)

Supersedes the "no password database" scope in the Approved v4 product and
security design.

## Context

Approved v4 made authentication provider-only: Google, GitHub, and LinkedIn with
opaque server-side sessions. The service stated it stores no passwords. That
excluded people who do not hold, or do not want to use, one of the three
providers.

Adding a password must not break the provider model. A provider identity stays
bound to exactly one account by its stable subject, and an account may link any
number of providers. The question is how a second authenticator coexists with
that without making email the identity key, splitting session authority, or
introducing an account-recovery surface.

## Decision

**Email-and-password is additive.** An account may hold zero or one password
credential and any number of linked provider identities. Existing opaque
sessions remain the only browser session mechanism. Provider signup keeps
creating an account from its verified email with no password; a signed-in
provider user may add one later.

**The application owns authentication authority.** Password storage and
verification live in the application next to provider identities and sessions.
Cognito-backed password authentication would split account and session authority
across two systems; moving every provider and session to Cognito would be a
larger, provider-locking migration.

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
login, and the ADR 0015 rotation successor insert sessions only while holding
the user lock. Password reset locks the same row, revokes every session, and
never logs in. Add/change creates one fresh non-lineage current session and
revokes all others atomically.

**No removal or email change.** Password removal and account-email change stay
out of scope to avoid last-authenticator and account-recovery rules this phase
does not design.

## Rejected alternatives

- **Cognito-backed passwords.** Splits account and session authority and locks
  identity to one provider.
- **Username sign-in.** Adds a second, non-email identifier the product does not
  otherwise expose.
- **Email-keyed provider linking or merge.** Makes a provider-reported email an
  identity authority and enables account takeover or accidental merge.

## Consequences

- Passwords, tokens, and mail payloads are stored only as Argon2id hashes,
  SHA-256 digests, and AES-256-GCM ciphertext; plaintext never persists.
- Password routes add exact Origin, CSRF, session, and rate-limit admission and
  byte-identical enumeration responses.
- Migration 00008 is additive; existing accounts begin with no credential and
  continue to sign in through linked providers.
- Cloud SES activation remains gated on local UAT and explicit owner
  authorization.
