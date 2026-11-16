# GitHub evidence

Status: Accepted by the owner (2026-11-11); not yet implemented.
[ADR 0030](../adr/0030-github-evidence.md) records the decision. This design
covers roadmap features "Connect GitHub", "Evidence picker" and "Evidence on the
page" ([roadmap](../plans/roadmap.md#v09-show-the-work)). It builds on the
[project library](projects.md) and its [contract](projects-contract.md).

## What a person does

1. In a library project, chooses "Add evidence from GitHub".
2. The first time, connects a GitHub account: GitHub asks for no permission
   beyond public information.
3. Picks from their own public repositories, their merged pull requests and
   their releases, or pastes a GitHub link.
4. Each picked item is checked and attached. The project page then shows it with
   what was confirmed and when.

## Enablement

| Configuration                                 | Evidence routes           | GitHub sign-in         |
| --------------------------------------------- | ------------------------- | ---------------------- |
| Evidence client id and secret both unset      | 404                       | As ADR 0016 configures |
| Evidence client id and secret both set        | On                        | As ADR 0016 configures |
| Only one of the evidence client id and secret | The server does not start | Unchanged              |

On the production host, the deploy's secrets check rejects a half-set pair
before any service changes.

The evidence application is a GitHub OAuth application of its own, with its own
client id and secret. Its callback URL is the public origin plus the fixed path
`/api/v1/github-evidence/callback`, as for sign-in providers. The capabilities
response tells the web app whether evidence is available. Sign-in start methods,
purposes and scopes ([ADR 0016](../adr/0016-sign-in-providers.md)) are
untouched.

Connecting collects GitHub identity data. In production the credentials are set,
and the feature turns on, only after the deployed bilingual privacy notice lists
the GitHub connection: the GitHub user id, login and connected time, and that
the token is never stored. The owner approved that text on 2026-11-16 and waived
the advance email for the two accounts that existed then.

## Connection

| Field          | Rule                                                             |
| -------------- | ---------------------------------------------------------------- |
| `githubUserId` | Numeric id from GitHub; the only matching key                    |
| `login`        | As last seen; display only; never used to match                  |
| `generation`   | Advanced by every connect, replace and disconnect; never repeats |
| `connectedAt`  | Set when the connection is made                                  |

- One connection row per aboutme account. Disconnecting clears its GitHub fields
  and keeps the row and its generation, so a generation is never reused; the row
  is deleted only with the account.
- One connection per aboutme account. A GitHub account may be connected to
  several aboutme accounts; nothing is unique across accounts.
- `POST /api/v1/github-evidence/start` is session-authenticated and
  CSRF-protected and returns an authorize URL, as the privileged sign-in starts
  do. `GET` on it answers 405. It creates a one-use OAuth transaction with
  purpose `evidence`, state, PKCE and the exact callback URL, bound to the user
  and the connection generation at that moment.
- `GET /api/v1/github-evidence/callback` consumes the transaction once and
  requires a live session of the same user. After the token exchange and the
  profile fetch, one transaction under the owner lock compares the generation
  recorded on the OAuth transaction with the stored one and, only if they are
  equal, installs the connection and advances the generation. Any mismatch
  discards the token and fails through the existing no-oracle path. It never
  resolves or links an identity, never updates reauthentication proof, and never
  starts a session of the account.
- The account boundary orders the callback's install against every ending,
  whichever takes the account lock first. The install sees an ending that
  committed before it. A disconnect or a replacement has changed the generation,
  and revoking the caller's own session or an epoch change has invalidated it,
  so the install is refused and its token revoked. "Done", expiry and revoking
  another session of the account end only the session live at that moment, so a
  later install proceeds. Once shutdown has begun, no install proceeds. An
  ending that comes after the install ends the new session.
- The one exception is a commit that fails after the install. The callback then
  ends exactly the session it installed, after the lock is released.
- The token response must contain a `scope` field that is present and empty. Any
  other value, or a missing field, discards the token and fails the connection.
- Connecting a different GitHub account replaces the connection and removes the
  confirmed mark from every item checked against the old account, as one
  owner-locked library mutation per affected project.
- Settings (`/app/settings/sessions`) shows a GitHub evidence section only when
  the capability is on. It shows the connected login and connected time, whether
  a session is live and its expiry, and offers Connect (Continue once
  connected), Done and Disconnect. It shows no other GitHub data.
- The callback ends with a redirect to that section carrying
  `evidence=connected` or `evidence=failed`. No other value is produced, a
  failure gives no reason, and the page ignores any other value.

## Evidence session

The token exists only in server memory. The production application has token
expiration on, so GitHub issues the access token for eight hours together with a
refresh token. The server never stores or uses the refresh token and drops it
with the token response. An unused refresh token stays valid at GitHub for six
months, which is why it is dropped at once.

- A session starts at the callback, lasts at most 30 minutes, and is held in the
  memory of the server process, keyed by user. It records the user's
  authentication epoch and the connection generation. One session per account; a
  new one replaces the old.
- It ends on "Done", expiry, sign-out, an epoch change, disconnect, replacement
  and process exit. Ending discards the token through token cleanup.
- Sign-out means any revocation of the account's sessions: logout, revoking one
  session or all of them, a password change or reset, and account deletion. An
  epoch change ends the session inside the transaction that changes the epoch.
- On shutdown, evidence cleanup starts at the signal, alongside the HTTP drains.
  In-flight code exchanges are cancelled, and every held token gets its one
  revocation attempt in parallel. Cleanup ends within 6 seconds of the signal
  (the 5-second request bound plus 1 second), so it fits inside Podman's
  10-second container stop. A callback that arrives after the signal does no
  exchange and redirects with `evidence=failed`.
- One account boundary orders every change: the account's user row lock in a
  database transaction. The evidence routes lock the user, the caller's session,
  then the GitHub connection row. Session-authenticated operations revalidate
  the caller's session inside it (live, same user, current epoch, same CSRF
  secret): the callback's install, status, "Done", disconnect, logout, revoking
  one or all sessions, a password change and self-deletion. Operations that
  carry another proof keep that proof and run under the same user lock: a
  password reset, an operator's deletion and the epoch change inside them. Only
  expiry and shutdown end a session outside the boundary.
- Every path that changes the evidence connection or session, or tells the
  server that the account's sessions ended, keeps three rules:
  1. It checks its authority under its locks. It reads the clock once, after the
     last row lock it takes before deciding, and checks every deadline against
     that value: session expiry, the recent-proof window, enrollment and
     ceremony expiry, and reset-token expiry.
  2. It ends the evidence session only after its own authorization has succeeded
     and all its session changes, rotation partners included, are done, as the
     last step before commit.
  3. Nothing after that can roll the transaction back except a failed commit.
- A token is held for cleanup as soon as the token response yields one
  unambiguous access token, before any other validation. A response that carries
  two different access tokens is logged as a fixed category, and neither is
  revoked.
- Token cleanup is one path for every issued token that is discarded: a callback
  that fails after the code exchange (generation mismatch, rejected scope,
  failed profile fetch) and every session ending above, including process
  shutdown where shutdown still allows a request. Cleanup claims the token
  atomically, so concurrent endings send no second request, and makes exactly
  one revocation attempt under the outbound bounds, never retried. Revocation is
  always attempted and does not count toward the per-account request limit.
- Revocation is `DELETE https://api.github.com/applications/{client_id}/token`,
  authenticated with the application's client id and secret in Basic auth, with
  `{"access_token": "<token>"}` as the JSON body. The request needs the token
  itself, so a crash or a failed request leaves the token valid at GitHub, for
  at most eight hours, and held by no one. A failed revocation is logged as a
  fixed category.
- The token is never written to the database, a file, a log, a response or the
  export. A database backup contains none.
- No log, error or diagnostic dump contains the access or refresh token, the
  client secret, an `Authorization` header, a request or response body, or a raw
  GitHub error. The server container runs with core dumps off (`Ulimit=core=0:0`
  in its Quadlet). [Host hardening](host-hardening.md#database-logs-and-cores)
  proposes this limit for every Quadlet; the server's comes first, with this
  feature.
- When no session is live, the picker asks the owner to continue with GitHub;
  GitHub does not prompt again for an authorized application.

## Items and what is confirmed

| Item         | Confirmed when                                          | Label on the page               |
| ------------ | ------------------------------------------------------- | ------------------------------- |
| Pull request | It is merged and its author's id matches                | Author of a merged pull request |
| Release      | It is published, not a draft, and its author id matches | Published this release          |
| Repository   | It is public and its owner's id matches                 | Owner of this repository        |

An item that does not meet its rule is not attached. A repository owned by an
organization is never confirmed through membership. A deleted author fails the
match. Commits, contributor lists, issues, stars and contribution graphs are not
evidence.

A stored item holds: its own evidence id, type, GitHub repository id, GitHub
item id (pull request or release), canonical URL, repository full name, title or
name, the item's date, the label, the GitHub account id it was checked against,
the check time and, for a repository, the star count at that time. It holds no
body text, diff or email address. Title and repository name are cut to 200
characters and a URL is rejected over 300, so one item stays under 1 KiB.

Evidence counts toward the stored project bound: the project document plus its
evidence stays within 96 KiB. Attach, recheck and every project write enforce
it, and the account export cap is unchanged.

## Owner API

Session-authenticated, CSRF-protected cookie routes only. No bearer token and no
MCP tool reaches them.

| Method and path                                            | Purpose                                             |
| ---------------------------------------------------------- | --------------------------------------------------- |
| `GET /api/v1/github-evidence`                              | Connection, whether a session is live, its expiry   |
| `POST /api/v1/github-evidence/start`                       | Begin connect or continue; returns an authorize URL |
| `DELETE /api/v1/github-evidence?keepLinks=`                | Disconnect, with `keepLinks` `true` or `false`      |
| `POST /api/v1/github-evidence/session/end`                 | "Done"                                              |
| `GET /api/v1/github-evidence/candidates?kind=&page=`       | One page of repositories, pull requests or releases |
| `POST /api/v1/projects/{id}/evidence`                      | Attach `{kind, link}` or `{kind, candidateId}`      |
| `POST /api/v1/projects/{id}/evidence/{evidenceId}/recheck` | Check again                                         |
| `PUT /api/v1/projects/{id}/evidence/order`                 | Reorder by evidence id                              |
| `DELETE /api/v1/projects/{id}/evidence/{evidenceId}`       | Remove                                              |

- Evidence is stored beside the project, not inside the project document. The
  project `PUT` and every MCP project tool neither read nor write it from the
  request; `GET` and `get_project` return it as a read-only list.
- Attach, recheck, reorder and remove are library mutations: project revision
  precondition, idempotency key, owner lock, and the transition and revision
  rules of the
  [projects contract](projects-contract.md#generations-and-revocation).
- Attach and recheck fetch from GitHub before the transaction. Inside the
  owner-locked transaction they require the same connection generation and a
  live session of the same epoch, so a disconnect or replacement that commits
  during the fetch wins.
- A candidate is `{candidateId, kind, title, repository, date}`. `candidateId`
  is an opaque string the server derives from the kind and GitHub's repository
  and item ids; it is valid only in the session that listed it. The candidates
  response is `{items, nextPage}`.
- Attach, recheck, reorder and remove answer with the project's evidence list,
  the new project revision and each advanced resume revision. Each evidence
  entry carries the stored item fields and `confirmed`.
- Export version 2 gains `evidence` on each project, with the stored item
  fields, and `githubConnection` with the GitHub user id, login and connected
  time when a connection exists. It never contains a token.
- Errors: `evidence_unavailable` (feature off), `evidence_session_required`,
  `evidence_not_confirmed`, `evidence_limit`, `evidence_unsupported_link`,
  `evidence_upstream` (GitHub error, limit or timeout), plus the existing
  precondition and validation errors.
- With evidence off, every route answers 404 `evidence_unavailable`. Start
  answers 503 `evidence_unavailable` once shutdown has begun. "Done" without a
  live session answers 409 `evidence_session_required`. The eleventh start in an
  hour answers the API's 429 `rate_limited` with `Retry-After`;
  `evidence_upstream` is only for GitHub's own errors and timeouts and for the
  spent request budget.

| Limit                               | Value                                 |
| ----------------------------------- | ------------------------------------- |
| Evidence items per project          | 10                                    |
| Live sessions per account           | 1                                     |
| Session starts per account          | 10 per hour                           |
| GitHub requests per account         | 120 per hour, counted across sessions |
| Candidates page / pages per listing | 30 items / 10 pages                   |
| One GitHub response                 | 512 KiB, read whole, then parsed      |
| One GitHub request                  | 5 s; no retry                         |

The limits above were approved with this design. The code exchange and the
profile fetch count toward the per-account request limit; revocation does not.

## Outbound requests

A dedicated client, not the sign-in provider client.

- The only destinations are `POST https://github.com/login/oauth/access_token`
  for the code exchange and `https://api.github.com`, with certificate
  validation, no proxy from the environment and redirects refused.
- A pasted link is parsed into owner, repository and number or tag and never
  fetched as given. Recheck addresses the repository by its stored id, so a
  rename or transfer is followed without a redirect; if the repository id or
  item id GitHub returns differs from the stored one, the item is not confirmed.
- A response over the bound, or one that does not parse strictly, fails the
  action. Only the listed fields are kept. GitHub text is untrusted: stored as
  plain text and rendered as text.
- The host has no egress allowlist, so this client is the boundary; a test
  proves it refuses any other destination, scheme and redirect.

## Staleness and rechecking

Nothing reads GitHub in the background. A snapshot shows with its check date
until the owner acts. "Check again" refetches by id; an item that no longer
meets its rule or cannot be fetched loses the confirmed mark and is flagged to
the owner, who removes it or keeps it as a plain link.

## On the project page

Evidence appears in its own section, in the owner's order: the label, the title
as a link to GitHub with `rel="noopener noreferrer nofollow"`, the repository
name, the item date and the "checked on" date. An item without the confirmed
mark shows as a plain link with no label.

Evidence follows the project page in every respect: gating, indexing, the resume
revision as the only public generation, and revocation. It enters the public
snapshot with the project and adds nothing to public JSON beyond the project
page's own data.

## Privacy

- Items can reveal the owner's GitHub login through their URLs. The first time
  an owner attaches an item, the editor says that the project page will link to
  their GitHub activity and that the same evidence appears on every resume that
  shows the project.
- The connection and the evidence are account data: both are in the account
  export and deleted with the account. A GitHub id shared by several accounts
  links them for anyone holding the database.
- The operator's account deletion command runs outside the server process. The
  token of an account deleted that way is revoked at session expiry, within 30
  minutes.
- Confirmation states what GitHub records. It does not claim sole authorship.
- Nothing about a viewer is stored, and no request reaches GitHub when a page is
  viewed.

## Disconnecting

`DELETE /api/v1/github-evidence` advances the generation, clears the connection,
ends the session and asks GitHub to revoke its token. `keepLinks` is a required
query boolean; a missing or other value is a validation error. With
`keepLinks=true` the items stay as plain links; with `false` they are removed.
Either way each affected project changes through one library mutation.
Disconnecting advances the generation even when nothing is connected, so a
callback started earlier fails.

## Data

- `github_connections`: owner (unique), nullable GitHub user id, login and
  connected time, and a generation that only grows.
- `project_evidence`: project (cascade), position, the item fields above.
- The OAuth transaction purpose and user-binding constraints gain `evidence`,
  with the connection generation recorded on the transaction.
- Configuration: evidence client id and client secret; the callback URL is the
  public origin plus the fixed callback path, as for sign-in providers.

## Checks

- With evidence credentials unset, every evidence route answers 404 and sign-in
  behaves exactly as before.
- A callback under another user's session, a replayed callback, and a callback
  after a disconnect all fail without creating a connection.
- A token response with a non-empty or missing scope is refused.
- Every token discarded after the code exchange gets exactly one revocation
  attempt, also when endings race, and none of the secrets or raw responses
  above reaches a log or error.
- The server Quadlet sets `Ulimit=core=0:0`.
- The host secrets check fails on one secret of the pair before app-down.
- No route, export, log, error or database row contains a GitHub token.
- An item is attached only when its rule holds for the connected account id; a
  login match never confirms; a contributor is never confirmed.
- A project `PUT` or agent write that carries evidence fields changes no
  evidence.
- A disconnect that commits during an attach leaves nothing attached.
- Requests leave only for the two fixed destinations; another host, scheme, path
  shape or a redirect is refused without a second request.
- Recheck of a renamed repository keeps the item; a different repository under
  the old name does not confirm.
- Evidence changes reach the project page on the next request and are revoked
  with it.

## Owner action before release

The owner created the production evidence application on 2026-11-15, with token
expiration on and no device flow. Its registered callback is
`https://aboutme.vn/api/v1/github-evidence/callback`. Its client id and secret
reach the server as configuration; until both are set the feature stays off.
