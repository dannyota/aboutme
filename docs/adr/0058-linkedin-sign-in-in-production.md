# 0058: LinkedIn sign-in in production

Status: Accepted (2026-09-26).

Amends [ADR 0039](0039-per-provider-login-enablement.md) and the OAuth
transaction rule in the [security design](../design/security.md). The detailed
design is [LinkedIn sign-in](../design/linkedin-sign-in.md).

## Context

ADR 0039 lets production enable only Google. LinkedIn login is built and tested
against a local OIDC server, but it has never talked to LinkedIn. LinkedIn's
documented web flow differs from what the code sends in three ways:

- The token request lists `client_id` and `client_secret` as form parameters and
  no PKCE parameter. The code sends a `code_verifier`, and its token client
  first tries HTTP Basic credentials.
- A third-party report dated 2026-09-21 says LinkedIn's token endpoint returns
  `401 invalid_client` when a confidential client sends `code_verifier`.
- A cancelled consent returns `user_cancelled_login` or
  `user_cancelled_authorize`, not `access_denied`.

LinkedIn subjects are pairwise per app, so the production app must never be
replaced.

## Decision

1. Production may set `provider_login_enabled` to `""`, `"google"`, or
   `"google,linkedin"`. LinkedIn's client ID and secret come from two SSM
   SecureString parameters that the task references only while LinkedIn is on.
2. LinkedIn uses its documented confidential flow: no `code_challenge` or
   `code_verifier`, client credentials in the form body, and state plus an OIDC
   nonce bound to the transaction. The nonce is the code-injection defense, as
   RFC 9700 section 2.1.1 allows for confidential OpenID Connect clients. Google
   and GitHub keep PKCE S256.
3. Both LinkedIn cancel errors and `access_denied` give the cancelled result.
4. Account linking follows the Google rules unchanged: identity is the subject,
   an owned email gives the generic collision result with no write, and linking
   starts only from a signed-in account.
5. Enabling LinkedIn is a separate step after the release that carries this
   change is live, as ADR 0039 requires for any provider.

## Rejected alternatives

- **Keep PKCE and see whether LinkedIn accepts it.** Depends on undocumented
  behavior that a public report says fails; a failure appears only in
  production.
- **Send PKCE and drop it after a failure.** A retry path for a security control
  is hard to reason about and to test.
- **Link accounts by matching email.** ADR 0025 rejects email as an identity
  key.
- **Use the userinfo endpoint instead of the ID token.** Adds a request and
  gives no claim the ID token lacks.

## Consequences

- LinkedIn's code-injection defense rests on the nonce check, which already runs
  before any token is used. A reviewer confirms it by name.
- An older release, if rolled back to while LinkedIn is on, would send PKCE
  again. Turn LinkedIn off before such a rollback.
- The privacy notice names LinkedIn next to Google before the flag turns on.
- The first real token exchange happens in production, with the owner's own
  account; turning the provider off is one apply and one deploy.
- Replacing the LinkedIn app would change every member's subject and strand
  their linked identities.
