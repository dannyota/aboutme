# 0063: LinkedIn sign-in without a nonce claim

Status: Accepted (2026-09-26). It supersedes decision 2 of
[ADR 0058](0058-linkedin-sign-in-in-production.md) in part: LinkedIn keeps its
flow, but the nonce is no longer its code-injection defense. The owner accepted
the remaining risk below, and LinkedIn is on again from v0.6.3.

## Context

ADR 0058 made the OIDC nonce LinkedIn's defense against authorization code
injection, because LinkedIn's token endpoint is reported to refuse PKCE from a
confidential client. The first production sign-ins on 2026-09-26 all failed at
that check with reason `nonce_mismatch`, after a successful token exchange and a
valid ID token signature, issuer, audience, and expiry.

LinkedIn does not return the nonce:

- LinkedIn's OIDC guide and its 3-legged OAuth guide never mention `nonce` [1],
  [2]. Its discovery document lists the supported claims, and `nonce` is not
  among them [3].
- Keycloak's built-in LinkedIn provider turns the nonce off with the comment
  "linkedin does not manage nonce correctly", and its class comment says "The
  nonce in the authentication request is not returned back in the ID Token" [4].
  The Keycloak developer who wrote it reported the same on Stack Overflow in
  2023 [5].
- python-social-auth's LinkedIn backend skips nonce validation "as it does not
  provide any nonce" [6].

The code sends the same 43-character nonce it stores, and the same comparison
works for Google in production, so the failure is LinkedIn's missing claim, not
an encoding difference.

## Decision

1. The LinkedIn callback accepts an ID token without a `nonce` claim. A present
   `nonce` claim must equal the transaction's nonce, or the callback rejects the
   token before using any claim. The authorize request still sends a nonce, so
   the check applies at once if LinkedIn starts to return it.
2. Google keeps the strict rule: its ID token must carry the transaction's
   nonce, and PKCE S256 stays.
3. The mock LinkedIn provider leaves the nonce claim out, as LinkedIn does, so
   the browser proofs exercise the production shape.

## Remaining risk

RFC 9700 section 2.1.1 says clients must prevent authorization code injection,
with PKCE or, for confidential OpenID Connect clients, the nonce [7]. LinkedIn
supports neither for this client, so aboutme cannot bind a LinkedIn code to the
browser that started the flow. Section 4.5 describes the attack [7]: someone who
steals a victim's unused authorization code starts their own flow and presents
the victim's code with their own state and transaction cookie. The callback
redeems it. The effect depends on the purpose:

- Login, identity already linked: the attacker is signed in to the victim's
  account.
- Login, identity unknown: the attacker creates and holds an account under the
  victim's verified email, and the victim's own later sign-up gets
  `email_already_registered`.
- Link: the victim's LinkedIn identity attaches to the attacker's account, so
  the victim's later LinkedIn sign-ins land in the attacker's account.
- Reauth: not exposed, because reauth requires the identity to belong to the
  signed-in caller.

State, the one-time `__Host-oauth-tx` transaction, and the client secret do not
stop that attack, because the attacker supplies their own valid state and
transaction. What limits it:

- LinkedIn only issues the code to the exact registered redirect URI
  `https://aboutme.vn/api/v1/auth/linkedin/callback`, over TLS [2].
- The code lives at most 30 minutes [2] and is single use [8]. The victim's own
  browser normally redeems it within a second, so the attacker must obtain it
  and also stop the victim's browser from delivering it.
- Only the confidential client can redeem the code, so a stolen code is useless
  outside aboutme's callback.
- The callback redirects at once, and API responses send
  `Referrer-Policy: no-referrer`. The Go request log records the path without
  the query, and production Caddy writes no request log.

The residual exposure is a code taken from the victim's device or network path
before delivery, for example by a malicious browser extension, which can often
take the resulting session cookie as well.

## Rejected alternatives

- **Keep requiring the nonce.** LinkedIn sign-in cannot work.
- **Stop sending the nonce.** Gains nothing and drops the check if LinkedIn
  starts returning the claim.
- **Send PKCE again.** A public report says LinkedIn answers
  `401 invalid_client`; ADR 0058 already rejected probing it in production.
  LinkedIn's token error table does mention a "code verifier" that must match
  the code [2], so a probe against a separate development app could settle it.
  If LinkedIn accepts PKCE there, PKCE should replace this decision.
- **Bind the code with a per-transaction query value on `redirect_uri`.**
  LinkedIn documents that it ignores query parameters on redirect URLs [2], so
  the value would not come back.
- **Keep LinkedIn off.** Valid if the owner does not accept the risk above; the
  flag stays `"google"`.

## Consequences

- LinkedIn sign-in, link, and reauth work; `nonce_mismatch` now means LinkedIn
  returned a nonce that differs from the one sent.
- LinkedIn's code-injection protection is weaker than Google's. The design and
  the security page say so.

## Sources

Retrieved 2026-09-26.

1. [Sign In with LinkedIn using OpenID Connect](https://learn.microsoft.com/en-us/linkedin/consumer/integrations/self-serve/sign-in-with-linkedin-v2)
2. [LinkedIn 3-Legged OAuth Flow](https://learn.microsoft.com/en-us/linkedin/shared/authentication/authorization-code-flow)
3. [LinkedIn OpenID discovery document](https://www.linkedin.com/oauth/.well-known/openid-configuration)
4. [Keycloak `LinkedInOIDCIdentityProviderFactory`](https://github.com/keycloak/keycloak/blob/main/services/src/main/java/org/keycloak/social/linkedin/LinkedInOIDCIdentityProviderFactory.java)
5. [Issues with Sign In with LinkedIn using OpenID Connect](https://stackoverflow.com/questions/76889585),
   Stack Overflow, 2023-08-12
6. [python-social-auth `LinkedinOpenIdConnect`](https://github.com/python-social-auth/social-core/blob/master/social_core/backends/linkedin.py)
7. [RFC 9700, OAuth 2.0 Security Best Current Practice, sections 2.1.1 and 4.5](https://www.rfc-editor.org/rfc/rfc9700.html#section-2.1.1)
8. [RFC 6749, section 4.1.2](https://www.rfc-editor.org/rfc/rfc6749.html#section-4.1.2):
   "If an authorization code is used more than once, the authorization server
   MUST deny the request"; LinkedIn's own enforcement is not verified
