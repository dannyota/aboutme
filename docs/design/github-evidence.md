# GitHub evidence

Status: Proposed (2026-11-11), awaiting owner approval.
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

| Configuration                          | Evidence routes | GitHub sign-in         |
| -------------------------------------- | --------------- | ---------------------- |
| Evidence application credentials unset | 404             | As ADR 0016 configures |
| Evidence application credentials set   | On              | As ADR 0016 configures |

The evidence application is a GitHub OAuth application of its own, with its own
client id, secret and callback URL. The capabilities response tells the web app
whether evidence is available. Sign-in start methods, purposes and scopes
([ADR 0016](../adr/0016-sign-in-providers.md)) are untouched.

## Connection

| Field          | Rule                                              |
| -------------- | ------------------------------------------------- |
| `githubUserId` | Numeric id from GitHub; the only matching key     |
| `login`        | As last seen; display only; never used to match   |
| `generation`   | Advanced by every connect, replace and disconnect |
| `connectedAt`  | Set when the connection is made                   |

- One connection per aboutme account. A GitHub account may be connected to
  several aboutme accounts; nothing is unique across accounts.
- `POST /api/v1/github-evidence/start` is session-authenticated and
  CSRF-protected and returns an authorize URL, as the privileged sign-in starts
  do. `GET` on it answers 405. It creates a one-use OAuth transaction with
  purpose `evidence`, state, PKCE and the exact callback URL, bound to the user
  and the connection generation at that moment.
- `GET /api/v1/github-evidence/callback` consumes the transaction once and
  requires a live session of the same user and an unchanged connection
  generation; otherwise it fails through the existing no-oracle path. It never
  resolves or links an identity, never updates reauthentication proof, and never
  starts a session of the account.
- The token response must contain a `scope` field that is present and empty. Any
  other value, or a missing field, discards the token and fails the connection.
- Connecting a different GitHub account replaces the connection and removes the
  confirmed mark from every item checked against the old account, as one
  owner-locked library mutation per affected project.

## Evidence session

The token exists only in server memory.

- A session starts at the callback, lasts at most 30 minutes, and is held in the
  memory of the server process, keyed by user. It records the user's
  authentication epoch and the connection generation. One session per account; a
  new one replaces the old.
- It ends on "Done", expiry, sign-out, an epoch change, disconnect, replacement
  and process exit. Ending revokes the token at GitHub with the application's
  credentials, best effort; a failed revocation is logged without the token.
- The token is never written to the database, a file, a log, a response or the
  export. A database backup contains none.
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
body text, diff or email address.

## Owner API

Session-authenticated, CSRF-protected cookie routes only. No bearer token and no
MCP tool reaches them.

| Method and path                                            | Purpose                                             |
| ---------------------------------------------------------- | --------------------------------------------------- |
| `GET /api/v1/github-evidence`                              | Connection, whether a session is live, its expiry   |
| `POST /api/v1/github-evidence/start`                       | Begin connect or continue; returns an authorize URL |
| `DELETE /api/v1/github-evidence`                           | Disconnect, with `keepLinks` true or false          |
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
- Errors: `evidence_unavailable` (feature off), `evidence_session_required`,
  `evidence_not_confirmed`, `evidence_limit`, `evidence_unsupported_link`,
  `evidence_upstream` (GitHub error, limit or timeout), plus the existing
  precondition and validation errors.

| Limit                               | Value                                 |
| ----------------------------------- | ------------------------------------- |
| Evidence items per project          | 10                                    |
| Live sessions per account           | 1                                     |
| Session starts per account          | 10 per hour                           |
| GitHub requests per account         | 120 per hour, counted across sessions |
| Candidates page / pages per listing | 30 items / 10 pages                   |
| One GitHub response                 | 512 KiB, read whole, then parsed      |
| One GitHub request                  | 5 s; no retry                         |

## Outbound requests

A dedicated client, not the sign-in provider client.

- The only destination is `https://api.github.com`, with certificate validation,
  no proxy from the environment and redirects refused.
- A pasted link is parsed into owner, repository and number or tag and never
  fetched as given. Recheck addresses the repository by its stored id, so a
  rename or transfer is followed without a redirect; if the repository id or
  item id GitHub returns differs from the stored one, the item is not confirmed.
- A response over the bound, or one that does not parse strictly, fails the
  action. Only the listed fields are kept. GitHub text is untrusted: stored as
  plain text and rendered as text.
- The host has no egress allowlist, so this client is the boundary; a test
  proves it refuses any other host, scheme and redirect.

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
- Confirmation states what GitHub records. It does not claim sole authorship.
- Nothing about a viewer is stored, and no request reaches GitHub when a page is
  viewed.

## Disconnecting

`DELETE /api/v1/github-evidence` advances the generation, deletes the
connection, ends the session and revokes its token. With `keepLinks` the items
stay as plain links; without it they are removed. Either way each affected
project changes through one library mutation.

## Data

- `github_connections`: owner (unique), GitHub user id, login, generation,
  connected time.
- `project_evidence`: project (cascade), position, the item fields above.
- The OAuth transaction purpose set gains `evidence`, with the connection
  generation recorded on the transaction.
- Configuration: evidence client id, client secret and callback URL.

## Checks

- With evidence credentials unset, every evidence route answers 404 and sign-in
  behaves exactly as before.
- A callback under another user's session, a replayed callback, and a callback
  after a disconnect all fail without creating a connection.
- A token response with a non-empty or missing scope is refused.
- No route, export, log, error or database row contains a GitHub token.
- An item is attached only when its rule holds for the connected account id; a
  login match never confirms; a contributor is never confirmed.
- A project `PUT` or agent write that carries evidence fields changes no
  evidence.
- A disconnect that commits during an attach leaves nothing attached.
- Requests leave only for the fixed host; another host, scheme, path shape or a
  redirect is refused without a second request.
- Recheck of a renamed repository keeps the item; a different repository under
  the old name does not confirm.
- Evidence changes reach the project page on the next request and are revoked
  with it.

## Open points for the owner

1. Limit of 10 items per project (recommended), or another value.
2. A second GitHub OAuth application for production: the owner creates it and
   supplies its credentials before the feature can be turned on.
