# GitHub evidence

Status: Proposed (2026-11-11), awaiting owner approval.
[ADR 0030](../adr/0030-github-evidence.md) records the decision. This design
covers roadmap features "Connect GitHub", "Evidence picker" and "Evidence on the
page" ([roadmap](../plans/roadmap.md#v09-show-the-work)). It builds on the
[project library](projects.md) and changes nothing in it.

## What a person does

1. In a library project, chooses "Add evidence from GitHub".
2. The first time, connects a GitHub account: GitHub asks for no permission
   beyond public information.
3. Picks from their public repositories, their merged pull requests and their
   releases, or pastes a GitHub link.
4. Each picked item is checked and attached. The project page then shows it with
   what was confirmed and when.

## Connection

| Field          | Rule                                            |
| -------------- | ----------------------------------------------- |
| `githubUserId` | Numeric id from GitHub; the only matching key   |
| `login`        | As last seen; display only; never used to match |
| `connectedAt`  | Set when the connection is made                 |

- One connection per aboutme account. A GitHub account may be connected to
  several aboutme accounts; nothing is unique across accounts, so a connection
  cannot be used to find or block another account.
- Connecting uses the authenticated, CSRF-protected start of
  [ADR 0016](../adr/0016-sign-in-providers.md) with a new purpose, `evidence`.
  The callback must present a live session of the user who started it. It never
  creates or links an identity and never signs anyone in.
- The request asks for no scope. A token that reports any scope is discarded and
  the connection fails.
- Connecting a different GitHub account replaces the connection; items checked
  against the old account lose the confirmed mark.

## Evidence session

The token is kept only for one session.

- A session starts at the callback and lasts at most 30 minutes. It is bound to
  the user and the browser session that started it.
- The token is stored sealed under a runtime key ring, as TOTP secrets are
  ([security](security.md)), in a row deleted at expiry, at sign-out, on "Done",
  and by the privacy sweep. A key failure disables evidence sessions only. The
  token never appears in a response, log or export.
- When no session is live, the picker asks the owner to continue with GitHub
  again; GitHub does not prompt for an already authorized application.
- Every GitHub request in a session is made by the server with that token, so it
  counts against the owner's own GitHub rate limit.

## Items and what is confirmed

| Item         | Fetched from                      | Confirmed when                            | Label on the page               |
| ------------ | --------------------------------- | ----------------------------------------- | ------------------------------- |
| Pull request | The pull request resource         | It is merged and its author id matches    | Author of a merged pull request |
| Release      | The release resource              | Its author id matches                     | Published this release          |
| Repository   | The repository resource           | Its owner id matches                      | Owner of this repository        |
| Repository   | The repository's contributor list | GitHub lists the account as a contributor | Listed by GitHub as contributor |

An item that does not meet its rule is not attached. Commits, issues, stars and
contribution graphs are not evidence. Private items cannot be fetched and are
never attached.

A stored item holds: type, canonical URL, repository full name, title or name,
the item's own date, the confirmed label, the GitHub account id it was checked
against, and the check time. For a repository it also holds the star count at
check time. It holds no body text, no diff and no email address.

| Limit                       | Value         |
| --------------------------- | ------------- |
| Evidence items per project  | 10            |
| Picker list page            | 30 items      |
| GitHub requests per session | 120           |
| One GitHub response         | 512 KiB       |
| One GitHub request          | 5 s; no retry |

## Outbound requests

- The only host is `api.github.com`, over HTTPS, with certificate validation and
  redirects refused. A pasted link is parsed into owner, repository and number
  or tag, then rebuilt; no part of a pasted URL is fetched as given.
- Responses are size-bounded, parsed strictly, and only the fields above are
  kept. GitHub text is treated as untrusted: it is stored as plain text and
  rendered as text.
- A GitHub error, rate limit or timeout fails the one action and attaches
  nothing.

## Staleness and rechecking

Nothing reads GitHub in the background. A snapshot keeps showing with its check
date until the owner acts.

- "Check again", inside a session, refetches the item. If it no longer meets its
  rule or cannot be fetched, the item loses the confirmed mark and is shown to
  the owner as needing attention; the owner removes it or keeps it as a plain
  link.
- The page always shows the check date next to a confirmed item.

## On the project page

Evidence appears in its own section, in the owner's order: the label, the title
as a link to GitHub with `rel="noopener noreferrer nofollow"`, the repository
name, the item date and "checked on" date. An item without the confirmed mark
shows as a plain link with no label.

Evidence follows the project page in every respect: gating, indexing, the resume
revision as the only public generation, and revocation
([projects contract](projects-contract.md#generations-and-revocation)).
Attaching, removing, reordering or rechecking an item is a library mutation that
advances the linking live resumes.

## Privacy

- Items can reveal the owner's GitHub login through their URLs. The first time
  an owner attaches an item, the editor says that the project page will link to
  their GitHub activity, and that the same evidence appears on every resume that
  shows the project.
- The connection is account data: it is in the account export and deleted with
  the account. The evidence is part of the library project.
- Nothing about a viewer is stored, and aboutme sends no request to GitHub when
  a page is viewed.

## Agents

No MCP tool connects GitHub, starts a session, or attaches, removes or rechecks
evidence. `get_project` returns attached evidence as read-only data.

## Disconnecting

Disconnect deletes the connection and any live session. The owner chooses, in
the same step, to keep attached items as plain links or remove them; either
choice is one library mutation per project.

## Data

- `github_connections`: owner (unique), GitHub user id, login, connected time.
- `github_evidence_sessions`: owner, browser session, sealed token, expiry.
- Evidence items live in the library project document; the stored project bound
  already covers them.
- The OAuth transaction purpose set gains `evidence`.

## Checks

- A connection is made only by the user who started it; a callback under another
  user's session fails through the existing no-oracle path.
- No route, export, log or error contains a GitHub token.
- A token with any scope is refused.
- An item is attached only when its rule holds for the connected account id; a
  login match alone never confirms.
- Requests leave only for the fixed host; a pasted link to another host, or with
  a path outside the three item shapes, is rejected without a request.
- Disconnect and account deletion leave no connection, session or token.
- Evidence changes reach the project page on the next request and are revoked
  with it.

## Open points for the owner

1. Limits: 10 items per project (recommended), or another value.
2. "Listed by GitHub as contributor" for repositories the person does not own:
   include it (recommended, with that exact wording), or allow only owned
   repositories, merged pull requests and releases.
3. Production GitHub OAuth application: the owner creates it and supplies the
   credentials before the feature can be turned on.
