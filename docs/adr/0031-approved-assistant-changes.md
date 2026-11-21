# 0031: Approved assistant changes and resume history

Status: Proposed (2026-11-21). The
[change approval](../design/change-approval.md) design applies it; the owner's
decisions listed there are pending.

## Context

An assistant with a write grant edits a published resume, and the next public
request shows the edit with no owner review
([ADR 0018](0018-mcp-agent-access.md)). A library project edit reaches every
live resume that links it ([ADR 0029](0029-account-project-library.md)). No
earlier state of a resume can be restored, and `revision` is only a
compare-and-swap counter and the public generation
([ADR 0010](0010-public-artifact-revocation.md)).

The owner decided that reviewed changes ship before assistant-led review, and
that approval is on by default for published resumes
([roadmap](../plans/roadmap.md#owner-decisions)). The roadmap keeps a separate
draft copy of the whole resume out of scope.

## Decision

1. **Checkpoints.** Every resume keeps checkpoints of its restorable state, one
   per editing session, for a bounded time and count. A checkpoint holds no
   photo key and never protects a media object.
2. **Restore.** The owner restores text, layout, title and language. Restore
   never changes the photo, publish or sharing settings, the approval setting,
   the project library or evidence. It is an ordinary owner save.
3. **Approval setting.** Each resume has an approval setting, on by default,
   effective while the resume is published. Only the owner's signed-in session
   changes it.
4. **Proposals per tool call.** While approval is effective, each resume write
   tool stores a proposal instead of saving. A proposal names the part of the
   resume it touches and that part's value before and after. Accepting applies
   it when the part still holds the value before, so proposals for different
   parts stay valid after one is accepted. A newer proposal from the same
   assistant for the same part replaces its older one.
5. **Refusals.** While approval is effective, `upload_photo` and `delete_resume`
   are refused, and so are library edits that would reach a resume under
   approval until library proposals ship.
6. **Assistants see outcomes, not history.** Write results say whether they
   saved or proposed, and assistants can list a resume's proposals and their
   outcomes. Assistants never read checkpoints, decide proposals, restore, or
   change the approval setting.
7. **Lifetime.** Pending proposals expire, revoking a grant withdraws its
   pending proposals, decided proposals are kept briefly for the assistant to
   read, and everything is deleted with the resume or account. Export includes
   checkpoints and proposals.

## Alternatives rejected

- **A draft copy of the whole resume.** Out of scope in the roadmap, and the
  owner and the assistant would edit diverging copies that need a merge.
- **Staleness by whole revision.** Accepting one proposal would invalidate every
  other pending proposal.
- **A checkpoint per save.** The editor saves on every change, so the history
  would hold thousands of rows with no human meaning.
- **Stored diffs instead of copies.** Restoring through schema versions gets
  harder, and copies fit the size limits.
- **Approval only in the assistant's instructions.** A stolen token ignores
  instructions; the server must refuse.
- **Proposals for photo uploads.** An unaccepted object would need a new media
  reference class under [ADR 0009](0009-private-media-delivery.md).
- **Keeping proposals after a grant is revoked.** Revoking signals distrust; the
  owner can ask again.
- **Restoring publish settings.** A restore could republish a resume or reopen
  access the owner closed.

## Consequences

- MCP results, `list_resumes`, `get_resume`, the closed error vocabulary and the
  tool registry change; ADR 0018 and the MCP guide are amended when this is
  accepted.
- Two tables grow per resume within fixed limits, and backups hold up to 30 days
  of earlier text per resume. The privacy notice changes in both languages.
- The editor gains History and Proposed changes panels; the owner stream gains a
  frame for proposals.
- The export format version rises and its cap grows.

## History

- 2026-11-21: proposed as a new record for roadmap v0.10.
