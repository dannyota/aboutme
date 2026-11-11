# 0030: GitHub evidence without a stored token

Status: Proposed (2026-11-11). The owner chose a separate, no-scope GitHub
connection for evidence on 2026-11-10
([roadmap](../plans/roadmap.md#owner-decisions)); this record and the
[GitHub evidence design](../design/github-evidence.md) await his approval before
code.

## Context

A project page should be able to show work a reader can check: a repository, a
merged pull request, a release. The claim only means something if the service
has checked that the item belongs to the person, and if the page says when.

GitHub sign-in is built but off in production
([ADR 0016](0016-sign-in-providers.md)). Provider tokens are discarded after the
profile fetch and no refresh token is stored
([security: sign-in](../design/security-sign-in.md)). A GitHub token written to
the database would also sit in every backup for 30 days, and GitHub OAuth
application tokens do not expire on their own. GitHub grants read access to
public information with no scope, links pull requests and releases to a durable
numeric account id, and links commits and contributor lists by email address,
which anyone can set
([sources](../plans/roadmap.md#facts-that-shape-the-order)).

## Decision

1. **A connection, not a sign-in.** An owner connects one GitHub account to
   their aboutme account for evidence. The connection stores the GitHub numeric
   id, the login as last seen and the time. It is not an identity: it never
   signs anyone in, and GitHub sign-in stays governed by ADR 0016.
2. **Its own OAuth application and routes.** Evidence uses a separate GitHub
   OAuth application and its own start and callback routes. Sign-in routes,
   their enablement and their scopes do not change, and the feature is off until
   its own credentials are configured.
3. **No scope.** The authorization request asks for none, and a token whose
   response reports any scope, or none at all, is refused. Private repositories
   are never read.
4. **The token is never written to disk.** It lives only in server memory for
   one bounded evidence session. When the session ends the server discards it
   and asks GitHub to revoke it; a crash or a failed request can leave the token
   valid at GitHub, held by no one. A restart ends every session. Nothing reads
   GitHub in the background.
5. **Evidence is a checked snapshot, owned by the server.** The owner picks an
   item during a session. The server fetches it, compares its account id with
   the connection, and stores a snapshot with GitHub's own ids and the check
   time. No client or agent can write an evidence field.
6. **What "confirmed" means is stated per item.** A merged pull request and a
   release are confirmed when their author id matches. A repository is confirmed
   when its owner id matches. Contributor lists and commits are not evidence.
7. **Evidence belongs to the library project** and is shown on every project
   page of that project.
8. **Disconnecting** deletes the connection and ends any session. The owner
   chooses whether attached items stay as plain links or are removed.

## Alternatives rejected

- **Turn on GitHub sign-in and reuse the identity.** Couples sign-in policy to a
  portfolio feature and makes a GitHub account a way into the aboutme account.
- **A purpose on the sign-in routes.** An unrecognized purpose starts login
  there, and omitting scope inherits the sign-in application's earlier grant.
- **Seal the token in the database for the session.** The ciphertext reaches
  backups and outlives the session; a later key compromise recovers a token
  GitHub still honors.
- **Store the token and refresh evidence in the background.** A standing
  credential for data the owner can recheck in one step.
- **A GitHub App installed on repositories.** Asks for installation to read data
  that is already public.
- **Let the owner paste links without checking.** A link proves nothing about
  who did the work.
- **Count contributor-list membership or commits.** Both follow commit email
  addresses, so a repository's controller can attribute work to anyone.

## Consequences

- Production needs a second GitHub OAuth application before this ships, as an
  owner action; without it the feature is off and nothing else changes.
- A session does not survive a server restart or reach another server process;
  the owner continues with GitHub again. A fleet needs a new decision.
- A snapshot can go stale. The page says when it was checked, and the owner can
  check again; a renamed or transferred repository is followed by its id.
- The project page may reveal the owner's GitHub login through the items. A
  GitHub id shared by several aboutme accounts links them for anyone holding the
  database, and confirmation does not mean exclusive authorship.
- The server makes outbound requests to one fixed host through a dedicated
  client, bounded in time, size and count.

## History

Proposed 2026-11-11 from the owner's roadmap decision of 2026-11-10. Revised the
same day after design review: separate application and routes, no token on disk,
server-owned evidence, GitHub ids for recheck, and no contributor rule.
