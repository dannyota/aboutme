# 0016: Sign-in providers: start methods, per-provider enablement, and LinkedIn

Status: Accepted (2026-08-12, 2026-09-02, 2026-09-18, 2026-09-26). The owner
accepted the LinkedIn code-injection risk stated below; LinkedIn sign-in is on
from v0.6.3.

## Context

Login, provider linking, and recent reauthentication all create OAuth
transactions but carry different authority. Login is public; linking and
reauthentication act on the current account. A top-level link cannot carry the
synchronizer token used by the CSRF boundary.

Google, GitHub, and LinkedIn login are built and tested, but each provider needs
production credentials, and a registered provider route is a reachable redirect
surface. The owner enables providers one at a time.

LinkedIn's documented web flow differs from the generic one. Its token request
lists `client_id` and `client_secret` as form parameters and no PKCE parameter;
a third-party report says its token endpoint returns `401 invalid_client` when a
confidential client sends `code_verifier`. A cancelled consent returns
`user_cancelled_login` or `user_cancelled_authorize`, not `access_denied`. The
first production sign-ins on 2026-09-26 all failed with `nonce_mismatch` after a
valid token exchange: LinkedIn does not return the OIDC nonce. Its guides never
mention `nonce` [1], [2]; its discovery document does not list the claim [3];
Keycloak [4], [5] and python-social-auth [6] skip the check for the same reason.
LinkedIn subjects are pairwise per app.

## Decision

### Start methods

`GET /api/v1/auth/{provider}/start` starts login only. A query that requests
`link` or `reauth` returns `405` and creates no transaction. Linking and recent
reauthentication use authenticated
`POST /api/v1/auth/{provider}/start?purpose=link|reauth`. The bodiless request
passes the normal origin and CSRF checks and returns an authorize URL in the
normal data envelope; the browser opens it as a top-level navigation. OAuth
callbacks remain GET, because the provider initiates that navigation and the
stored one-use transaction is the callback authority.

### Enablement

`PROVIDER_LOGIN_ENABLED` accepts blank, `false`, `true`, or a comma list of
distinct providers from `google`, `github`, and `linkedin`. Blank and `false`
enable none; `true` enables all three. Any other value, an unknown provider, or
a repeated provider stops the server at startup.

Go registers the start, callback, link, and reauthentication routes only for
enabled providers. A disabled provider's paths return the uniform not-found
response of any unregistered route. The provider code, tests, mock provider, and
OpenAPI operations stay in the tree. In prod and staging, only an enabled
provider requires its client ID and secret.

The unauthenticated capabilities read carries `providerLogin`, true when at
least one provider is enabled, and `providers`: the enabled names in the fixed
order `google`, `github`, `linkedin`. The web renders a provider control only
for a name in that list and never mirrors the flag into Nuxt runtime config.

Production may set `provider_login_enabled` to `""`, `"google"`, or
`"google,linkedin"`. Each enabled provider's client ID and secret come from SSM
SecureString parameters that the task references only while that provider is on.
Enabling a provider is a separate step after the release that supports it is
live, never part of that release.

### LinkedIn flow

- LinkedIn uses its documented confidential flow: no `code_challenge` or
  `code_verifier`, client credentials in the form body, and state plus an OIDC
  nonce bound to the transaction. Google and GitHub keep PKCE S256.
- The LinkedIn callback accepts an ID token without a `nonce` claim. A present
  `nonce` claim must equal the transaction's nonce, or the callback rejects the
  token before using any claim. The authorize request still sends a nonce, so
  the check applies at once if LinkedIn starts to return it.
- Google keeps the strict rule: its ID token must carry the transaction's nonce.
- Both LinkedIn cancel errors and `access_denied` give the cancelled result.
- Account linking follows the Google rules: identity is the subject, an owned
  email gives the generic collision result with no write, and linking starts
  only from a signed-in account.
- The mock LinkedIn provider leaves the nonce claim out, as LinkedIn does, so
  browser proofs exercise the production shape.

## Remaining LinkedIn risk

RFC 9700 section 2.1.1 says clients must prevent authorization code injection,
with PKCE or, for confidential OpenID Connect clients, the nonce [7]. LinkedIn
supports neither for this client, so aboutme cannot bind a LinkedIn code to the
browser that started the flow. Section 4.5 describes the attack [7]: someone who
steals a victim's unused authorization code starts their own flow and presents
the victim's code with their own state and transaction cookie. The effect
depends on the purpose:

- Login, identity already linked: the attacker is signed in to the victim's
  account.
- Login, identity unknown: the attacker creates and holds an account under the
  victim's verified email, and the victim's own later sign-up gets
  `email_already_registered`.
- Link: the victim's LinkedIn identity attaches to the attacker's account.
- Reauth: not exposed, because reauth requires the identity to belong to the
  signed-in caller.

State, the one-time `__Host-oauth-tx` transaction, and the client secret do not
stop that attack. What limits it:

- LinkedIn issues the code only to the exact registered redirect URI
  `https://aboutme.vn/api/v1/auth/linkedin/callback`, over TLS [2].
- The code lives at most 30 minutes [2] and is single use [8]. The victim's
  browser normally redeems it within a second, so the attacker must obtain it
  and also stop the victim's browser from delivering it.
- Only the confidential client can redeem the code.
- The callback redirects at once, and API responses send
  `Referrer-Policy: no-referrer`. The Go request log records the path without
  the query, and production Caddy writes no request log.

The residual exposure is a code taken from the victim's device or network path
before delivery, for example by a malicious browser extension, which can often
take the resulting session cookie as well.

## Rejected alternatives

- **Hide the buttons only.** Leaves redirect surfaces reachable and lets the web
  and server disagree.
- **One boolean per provider.** Three variables that must agree with the
  capabilities read, where one list states the whole choice.
- **Delete unused provider code.** Throws away proven work and the local Google
  mock the HTTPS harness depends on.
- **Send PKCE to LinkedIn, or send it and drop it after a failure.** Depends on
  undocumented behavior a public report says fails, and a retry path for a
  security control is hard to test. LinkedIn's token error table does mention a
  "code verifier" [2], so a probe against a separate development app could
  settle it; if LinkedIn accepts PKCE there, PKCE should replace the nonce rule.
- **Keep requiring the LinkedIn nonce.** LinkedIn sign-in cannot work.
- **Bind the code with a query value on `redirect_uri`.** LinkedIn ignores query
  parameters on redirect URLs [2].
- **Link accounts by matching email.** ADR 0015 rejects email as an identity
  key.
- **Use the userinfo endpoint instead of the ID token.** Adds a request and
  gives no claim the ID token lacks.

## Consequences

- UI controls for linking and reauthentication call POST; an anchor to a
  privileged GET route is a defect. OpenAPI examples must not imply that GET can
  start a privileged purpose.
- An account whose only credential is a disabled provider cannot sign in.
- The native HTTPS harness enables providers because its proofs sign in through
  the local mocks; the native HTTP stack and Compose leave the flag unset.
- A release older than the list parser accepts only blank, `false`, and `true`.
  A release older than the LinkedIn flow would send PKCE. Before rolling back to
  either, set `provider_login_enabled` to a value that release supports and
  apply.
- LinkedIn's code-injection protection is weaker than Google's. The design and
  the security page say so, and `nonce_mismatch` means LinkedIn returned a nonce
  that differs from the one sent.
- The privacy notice names every enabled provider.
- Replacing the LinkedIn app would change every member's subject and strand
  their linked identities.
- The [LinkedIn sign-in design](../design/linkedin-sign-in.md) and the
  [security design](../design/security.md) hold the details.

## History

- Former ADR 0014 (2026-08-12): privileged OAuth starts use authenticated POST.
- Former ADR 0027 (2026-09-02): provider login behind one boolean flag.
- Former ADR 0039 (2026-09-18): the per-provider list, so Google could ship
  alone.
- Former ADR 0058 (2026-09-26): LinkedIn in production with no PKCE and the
  nonce as its code-injection defense.
- Former ADR 0063 (2026-09-26): LinkedIn returns no nonce claim, so the nonce
  check applies only when present, and the owner accepted the remaining risk.

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
