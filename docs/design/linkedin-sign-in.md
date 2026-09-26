# LinkedIn sign-in

People can sign up, sign in, link, and reauthenticate with LinkedIn, the same
way they do with Google. The provider code, mock hooks, and tests already exist
behind the per-provider flag. What is left is to follow LinkedIn's documented
web flow exactly, wire production credentials, update the privacy notice, add
browser proofs, and turn the flag on.
[ADR 0058](../adr/0058-linkedin-sign-in-in-production.md) records the decision.

Status: proposed. Facts below were checked against LinkedIn's documentation and
its live discovery document on 2026-09-26. **Verify** marks a fact that only the
first production sign-in can confirm.

## What exists

| Part          | State                                                                                                                                                                                                                         |
| ------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Flag          | `PROVIDER_LOGIN_ENABLED` takes a comma list; `linkedin` registers the start and callback routes ([ADR 0039](../adr/0039-per-provider-login-enablement.md)). Production can set only `""` or `"google"` today.                 |
| Server        | `apps/server/internal/auth/linkedin.go`: OIDC discovery of `https://www.linkedin.com/oauth`, scopes `openid profile email`, PKCE S256, nonce, state, ID token checks, nullable `email_verified`.                              |
| Registration  | A new subject needs an email that is present, `email_verified` true, and canonical. An existing subject signs in without an email check. Link and reauth ignore email.                                                        |
| Linking       | Shared with Google: identity is `(provider, subject)`; an email already owned by any account returns the generic `email_already_registered` and writes nothing; linking starts only from a signed-in account.                 |
| Config        | Prod and staging need `LINKEDIN_CLIENT_ID` and `LINKEDIN_CLIENT_SECRET` only when LinkedIn is enabled. `LINKEDIN_OIDC_ISSUER_URL` is allowed only with `ENV=dev` and only on loopback path `/linkedin`.                       |
| Web           | Capabilities `providers` drives the buttons ("Tiếp tục với LinkedIn" / "Continue with LinkedIn"), settings link, and reauth. The authorize URL allowlist holds `https://www.linkedin.com/oauth/v2/authorization`.             |
| Go tests      | `linkedin_test.go` and `linkedin_adversarial_test.go`: email rule, redirect URI, issuer, audience, signature, nonce, expiry, state, missing cookie, `access_denied`, enrolled second factor, no-oracle failures; link matrix. |
| Mock provider | `internal/uatmock` and `cmd/mock-oauth` serve Google only. The native HTTPS harness enables all three providers, so its LinkedIn and GitHub buttons lead nowhere.                                                             |
| Production    | `deploy/aws/modules/tasks` accepts only `""` or `"google"` and wires only Google's two SSM parameters. The execution role reads only Google's.                                                                                |

## LinkedIn facts

| Topic           | Fact                                                                                                                                                                                                                                | Source   |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------- |
| Product         | "Sign In with LinkedIn using OpenID Connect" is an open permission: any developer adds it from the app's Products tab, with no partner review.                                                                                      | [1], [2] |
| Scopes          | `openid` (required for OIDC), `profile` (id, name, picture), `email` (primary email). The access table also says `profile` gives the headline, but no claim or userinfo field carries it.                                           | [1], [2] |
| ID token claims | `iss`, `aud`, `iat`, `exp`, `sub`; with `profile`: `name`, `given_name`, `family_name`, `picture`, `locale`; with `email`: `email`, `email_verified`. Signed RS256.                                                                 | [1], [3] |
| Email           | `email` and `email_verified` are optional and "may not be included in all responses".                                                                                                                                               | [1]      |
| Subject         | `subject_types_supported` is `pairwise`: `sub` differs per LinkedIn app. A new app gives every member a new subject.                                                                                                                | [3]      |
| Issuer          | The live discovery document says `https://www.linkedin.com/oauth`. The documentation sample shows `https://www.linkedin.com`, which is stale; the code uses the live value.                                                         | [1], [3] |
| Endpoints       | Authorize `https://www.linkedin.com/oauth/v2/authorization`; token `https://www.linkedin.com/oauth/v2/accessToken`; userinfo `https://api.linkedin.com/v2/userinfo`; JWKS `https://www.linkedin.com/oauth/openid/jwks`.             | [3]      |
| Token request   | Form-encoded POST with `grant_type`, `code`, `client_id`, `client_secret`, `redirect_uri`. The web flow lists no PKCE parameter.                                                                                                    | [4]      |
| PKCE            | A report dated 2026-09-21 says the token endpoint answers `401 invalid_client` when a confidential client sends `code_verifier`. LinkedIn does not document this. **Verify.**                                                       | [5]      |
| Redirect URLs   | Registered on the Auth tab; absolute; query parameters are ignored; no `#`; a request must match a registered URL or LinkedIn returns 401.                                                                                          | [4]      |
| Lifetimes       | Authorization code 30 minutes; access token 60 days (the service discards it).                                                                                                                                                      | [4]      |
| Cancel          | The callback gets `error=user_cancelled_login` or `error=user_cancelled_authorize` with `state`, not `access_denied`.                                                                                                               | [4]      |
| Identity        | "Sign In with LinkedIn using OpenID Connect does not verify user identities and should not be marketed as such."                                                                                                                    | [1]      |
| App setup       | An app needs a name, a LinkedIn Page, a privacy policy URL, and a logo. A Page super admin can verify the app's Page association through a URL valid 30 days. The docs require that step for restricted products, not for this one. | [6], [7] |
| Controller      | For members outside the EU, EEA, and Switzerland, LinkedIn Corporation (United States) controls their LinkedIn data.                                                                                                                | [8]      |

## Design

### Protocol

LinkedIn follows its documented confidential web flow:

- The authorize URL carries `response_type=code`, `client_id`, `redirect_uri`,
  `state`, `scope=openid profile email`, and `nonce`. It carries no
  `code_challenge`.
- The token request sends `client_id` and `client_secret` in the form body
  (`oauth2.AuthStyleInParams`) and no `code_verifier`. Auto-detection is not
  used, because its first attempt sends HTTP Basic credentials that LinkedIn
  does not document.
- The ID token nonce is the code-injection defense. It is random per
  transaction, stored with the transaction, and bound to the browser by the
  `__Host-oauth-tx` handle. The callback discards every token until the nonce
  matches. RFC 9700 section 2.1.1 allows this for confidential OpenID Connect
  clients [9].
- The transaction row keeps its PKCE verifier column, so storage does not
  change; LinkedIn simply never receives it. Google and GitHub keep PKCE S256.
- `error=user_cancelled_login`, `error=user_cancelled_authorize`, and
  `error=access_denied` all return the cancelled result, after the state check
  as today. Any other `error` value returns the generic failure.
- The UI never says LinkedIn verified a person. The only use of the email claim
  is the existing registration rule.

`security.md` changes its OAuth sentence to: "Google and GitHub use
authorization code with PKCE S256. LinkedIn uses its documented confidential
flow without PKCE; the OIDC nonce defends against code injection."

### Account linking

LinkedIn follows the Google rules with no new exception:

1. A known `(linkedin, sub)` signs in to its account, whatever the email says
   now.
2. An unknown subject with a present, true `email_verified` and a canonical
   email that no account holds creates a new account with no password.
3. An unknown subject whose email an account already holds, whether it signs in
   with a password or Google, gets the generic `email_already_registered`
   result. Nothing is written, and the message never names the existing method.
4. To attach LinkedIn to an existing account, the person signs in the usual way
   and chooses Link in Settings, which needs recent reauthentication. Link
   ignores the LinkedIn email, so it also works when the addresses differ.
5. An unknown subject without a verified email gets `email_not_verified`, and
   the existing message asks the person to verify the email with the provider.
6. Accounts are never merged by email. A pending password registration does not
   hold the email; the provider sign-up wins, as it does for Google.

Because `sub` is pairwise per app, production keeps one LinkedIn app for good.
Replacing it would strand every linked identity. Secret rotation happens inside
the same app.

### Web

- The collision message changes so that it helps a person who used a password,
  not only another provider, and names the provider just used (**Owner
  approval** L3):
  - vi: "Email này đã có tài khoản. Hãy đăng nhập như bạn vẫn làm, rồi liên kết
    {provider} trong Cài đặt."
  - en: "An account with this email already exists. Sign in the way you usually
    do, then link {provider} in Settings."
- The post-registration and expired-link notices offer every listed provider,
  not only Google. `web.md` changes its "offers Google when listed" sentence to
  match.
- Button order stays the capabilities order: Google, then LinkedIn.

### Privacy notice

`apps/web/app/i18n/legal.ts` changes in both languages (**Owner approval** L4;
the owner reviews the Vietnamese). LinkedIn is named where Google is. For this
purpose LinkedIn acts as an independent controller that verifies the person to
aboutme, not as a processor under contract with aboutme, so the text says
"verifies your account", as it does for Google.

| Place         | English                                                                                                        | Vietnamese                                                                                                      |
| ------------- | -------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| What we keep  | "If you sign in with Google or LinkedIn: your account ID with that provider, your email, and your name."       | "Nếu bạn đăng nhập bằng Google hoặc LinkedIn: ID tài khoản của bạn tại nhà cung cấp đó, email và tên."          |
| Optional list | "Google or LinkedIn sign-in"                                                                                   | "đăng nhập bằng Google hoặc LinkedIn"                                                                           |
| Cookies       | "to complete Google or LinkedIn sign-in"                                                                       | "hoàn tất đăng nhập bằng Google hoặc LinkedIn"                                                                  |
| Where stored  | Add after the Google sentence: "If you sign in with LinkedIn, LinkedIn (United States) verifies your account." | Add after the Google sentence: "Nếu bạn đăng nhập bằng LinkedIn, LinkedIn (Hoa Kỳ) xác thực tài khoản của bạn." |
| Transfer      | "...mainly to Singapore, and in part to Google and LinkedIn."                                                  | "...chủ yếu đến Singapore, và một phần đến Google và LinkedIn."                                                 |

The notice ships in the release, before the flag turns on.

### Production

- `deploy/aws/modules/tasks`: `provider_login_enabled` accepts `""`, `"google"`,
  or `"google,linkedin"`. LinkedIn's two secrets are referenced only when the
  value includes `linkedin`, from `/aboutme/prod/oauth/linkedin-client-id` and
  `/aboutme/prod/oauth/linkedin-client-secret`. The server gets the value as
  given, or `"false"` for `""`.
- `deploy/aws/modules/identity`: the app execution role's parameter list adds
  the two LinkedIn parameters and nothing else.
- `prod.tfvars.example` and the variable description list the new value. The
  runbook gains a "LinkedIn sign-in" section with the app setup below.
- `deploy.sh` already refuses a deploy while any task secret is missing.

### Security

- No new route, cookie, table, or rate limit. LinkedIn uses the existing start,
  callback, link, reauth, and unlink routes and their limits.
- A disabled LinkedIn still returns the uniform not-found response.
- Provider tokens are used for the exchange only, then discarded. No refresh
  token, picture, or locale is stored. Logs carry the provider name and a reason
  code, never the email, subject, code, or token.
- The client secret lives only in SSM SecureString and the running task
  environment. It is never printed, logged, or committed.

### Older clients and rollback

No schema, API, OpenAPI, or database change. Every live release accepts the
`google,linkedin` list, so rollback needs no flag change for the server. Rolling
back to a release before this one while LinkedIn is on would bring back the PKCE
flow and the old notice, so turn LinkedIn off (`"google"`, apply, deploy) before
such a rollback. An account whose only method is LinkedIn cannot sign in while
LinkedIn is off, as ADR 0039 already states.

### Size

No new dependency. Mock and test code grow; the web bundle changes only by copy.

## Tests

Tests cite this design and ADR 0058.

- Go, `internal/auth`: the authorize URL has `nonce` and no `code_challenge`;
  the token request has `client_id` and `client_secret` in the body, no
  `Authorization` header, and no `code_verifier`; both cancel errors and
  `access_denied` give cancelled; any other `error` gives the generic failure; a
  wrong nonce still fails after a successful exchange. Google keeps its PKCE
  assertions.
- Go, discovery: a committed copy of LinkedIn's public discovery document
  (`internal/auth/testdata/linkedin-discovery.json`, fetched 2026-09-26) served
  by a test server is accepted with the issuer constant. The test fails if the
  constant and the document disagree.
- Go, config: `google,linkedin` in prod without either LinkedIn value stops
  startup; with both, it starts.
- Mock provider: `uatmock` gains a LinkedIn mode on `/linkedin` with LinkedIn's
  behavior: pairwise-looking subjects, optional email claims, 401
  `invalid_client` when the token request carries `code_verifier` or Basic
  credentials, and cancel buttons that send the two LinkedIn error codes.
  Accounts: verified email; no email claim; `email_verified` false; an email
  that a password account holds; one for linking. The harness sets
  `PROVIDER_LOGIN_ENABLED=google,linkedin`, the LinkedIn client values, the
  issuer URL, and the Caddy authorize route, and redacts the secret in logs.
- Browser proof, `deploy/dev-https-browser/linkedin.spec.ts`: sign-up with a
  verified email lands on the resume list; sign-out and sign-in again reach the
  same account; missing and false `email_verified` show the verify message and
  create nothing; the collision shows the generic message and creates nothing; a
  password account links LinkedIn after reauth, then signs in with it; unlink
  works and the last-method guard holds; each cancel shows "Sign-in was
  cancelled". The entry proof expects exactly Google and LinkedIn buttons.
- Web: privacy notice text in both languages; collision copy; notices list every
  provider in `providers`.
- Infrastructure: the variable validation accepts exactly the three values; the
  plan adds two secrets and two role resources and destroys nothing.

## LinkedIn app setup

The owner does these steps once. Never paste a secret into chat, a command
argument, or a repository file.

1. **LinkedIn Page.** LinkedIn requires one. If aboutme has none, create a
   company Page: name "aboutme", website `https://aboutme.vn`, the logo mark.
   The Page is public (**Owner approval** L1).
2. **Create the app** at <https://www.linkedin.com/developers/apps> → Create
   app:
   - App name: `aboutme`
   - LinkedIn Page: the aboutme Page
   - Privacy policy URL: `https://aboutme.vn/privacy`
   - App logo: the square logo mark
   - Accept the legal agreement, then Create app.
3. **Verify the Page link.** Settings tab → Verify → Generate URL. Open the URL
   while signed in as the Page super admin and approve it. It is valid for 30
   days.
4. **Add the product.** Products tab → "Sign In with LinkedIn using OpenID
   Connect" → Request access, and accept its terms. Add no other product.
5. **Redirect URL.** Auth tab → OAuth 2.0 settings → Authorized redirect URLs →
   add exactly `https://aboutme.vn/api/v1/auth/linkedin/callback`. Add no other
   URL. Check that the scopes list shows `openid`, `profile`, and `email`.
6. **Store the credentials.** From the Auth tab, copy the Client ID, then the
   Primary Client Secret, each through a private tmpfs file, as the runbook's
   Google steps do:

   ```sh
   f=$(mktemp -p "$XDG_RUNTIME_DIR")
   wl-paste >"$f" && wl-copy --clear # after copying the client ID
   aws ssm put-parameter --region ap-southeast-1 --type SecureString \
     --name /aboutme/prod/oauth/linkedin-client-id --value "file://$f"
   wl-paste >"$f" && wl-copy --clear # after copying the client secret
   aws ssm put-parameter --region ap-southeast-1 --type SecureString \
     --name /aboutme/prod/oauth/linkedin-client-secret --value "file://$f"
   rm -f "$f"
   ```

7. **Keep the app.** Never delete or replace it; subjects are per app. Keep the
   owner as its only admin.

## Release and enable

1. Release 0.6.2 with the code, notice, infrastructure, and proofs; flag
   unchanged. Wait for it to be live and healthy.
2. The owner finishes the app setup.
3. Set `provider_login_enabled = "google,linkedin"` in `prod.tfvars` and copy it
   to the private infrastructure repository. Run `tofu apply`; the plan changes
   the task definition and the role policy only. Redeploy the live tag.
4. Check: capabilities report `"providers":["google","linkedin"]`;
   `/api/v1/auth/linkedin/start` redirects to LinkedIn's authorize URL with the
   exact redirect URI, the three scopes, a nonce, and no `code_challenge`;
   `/api/v1/auth/github/start` returns 404.
5. The owner links LinkedIn to his own account in Settings, signs out, and signs
   in with LinkedIn. This is the **Verify** step for the token exchange. If it
   fails, set `"google"`, apply, redeploy, and bring the logged reason code back
   for diagnosis.

## Owner approval

| ID  | Choice                                                                                          | Recommendation                                                        |
| --- | ----------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| L1  | Turn on LinkedIn sign-in in production, and create a public "aboutme" LinkedIn Page for the app | Yes                                                                   |
| L2  | LinkedIn uses its documented flow without PKCE; the nonce defends against code injection        | Yes; LinkedIn documents no PKCE for web apps and one report shows 401 |
| L3  | New collision message, above                                                                    | Yes                                                                   |
| L4  | Privacy notice text, above; the owner reviews the Vietnamese                                    | Yes                                                                   |

## Sources

Retrieved 2026-09-26.

1. [Sign In with LinkedIn using OpenID Connect](https://learn.microsoft.com/en-us/linkedin/consumer/integrations/self-serve/sign-in-with-linkedin-v2)
2. [Getting Access to LinkedIn APIs](https://learn.microsoft.com/en-us/linkedin/shared/authentication/getting-access)
3. [LinkedIn OpenID discovery document](https://www.linkedin.com/oauth/.well-known/openid-configuration)
   and [JWKS](https://www.linkedin.com/oauth/openid/jwks)
4. [LinkedIn 3-Legged OAuth Flow](https://learn.microsoft.com/en-us/linkedin/shared/authentication/authorization-code-flow)
5. [LinkedIn confidential OAuth fails when token request includes code_verifier](https://github.com/UsefulSoftwareCo/executor/issues/2087)
   (third-party report)
6. [Create a new app for a LinkedIn Page](https://www.linkedin.com/help/linkedin/answer/a1667239)
7. [Verify the LinkedIn Page association of an app](https://www.linkedin.com/help/linkedin/answer/a1665329)
8. [LinkedIn Privacy Policy](https://www.linkedin.com/legal/privacy-policy),
   effective 2025-11-03
9. [RFC 9700, OAuth 2.0 Security Best Current Practice, section 2.1.1](https://www.rfc-editor.org/rfc/rfc9700.html#section-2.1.1)
