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
([security: sign-in](../design/security-sign-in.md)). Keeping a GitHub token
would add a credential to the database and its backups for a feature that reads
only public data. GitHub grants read access to public information with no scope,
links pull requests and releases to a numeric account id, and links commits only
by email address ([sources](../plans/roadmap.md#facts-that-shape-the-order)).

## Decision

1. **A connection, not a sign-in.** An owner connects one GitHub account to
   their aboutme account for evidence. The connection stores the GitHub numeric
   id, the login as last seen and the time. It is not an identity: it never
   signs anyone in, and GitHub sign-in stays governed by ADR 0016.
2. **No scope.** The authorization request asks for no scope. Private
   repositories are never read.
3. **No stored token.** The access token lives only for one bounded evidence
   session, encrypted, and is deleted when the session ends or expires. Nothing
   reads GitHub in the background.
4. **Evidence is a checked snapshot.** The owner picks an item during a session.
   The server fetches it from GitHub, compares the item's account id with the
   connection, and stores a snapshot with the check time. The page shows the
   snapshot and its checked date. It is never refreshed without the owner.
5. **What "confirmed" means is stated per item.** A merged pull request is
   confirmed when its author id matches. A release is confirmed when its author
   id matches. A repository is confirmed as owned when its owner id matches, or
   as contributed to when GitHub lists the account as a contributor. Commits are
   not evidence.
6. **Evidence belongs to the library project** and is shown on every project
   page of that project. It is attached and removed only in the web UI, never by
   an agent tool.
7. **Disconnecting** deletes the connection. The owner chooses whether attached
   items stay as plain links without the confirmed mark or are removed.

## Alternatives rejected

- **Turn on GitHub sign-in and reuse the identity.** Couples sign-in policy to a
  portfolio feature and makes a GitHub account a way into the aboutme account.
- **Store the token and refresh evidence in the background.** A standing
  credential in the database and backups, for data the owner can recheck in one
  step.
- **A GitHub App installed on repositories.** Asks for installation on
  repositories to read data that is already public.
- **Let the owner paste links without checking.** A link proves nothing about
  who did the work.
- **Verify commits by email.** GitHub links commits by email address, which
  anyone can set.

## Consequences

- Production needs GitHub OAuth application credentials before this ships, as an
  owner action; without them the feature is off and nothing else changes.
- A snapshot can go stale: the item may be deleted, made private or renamed. The
  page says when it was checked, and the owner can check again.
- The project page may reveal the owner's GitHub login through the items
  themselves. The owner sees this before attaching the first item.
- The server makes outbound requests to one fixed host, bounded in time, size
  and count.
- ADR 0016 gains an `evidence` start purpose.

## History

Proposed 2026-11-11 from the owner's roadmap decision of 2026-11-10.
