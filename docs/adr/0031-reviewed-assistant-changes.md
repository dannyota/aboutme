# 0031: Reviewed assistant changes and resume history

Status: Accepted (2026-11-21) by the owner after an independent review. The
[change review](../design/change-review.md) design applies it. Nothing is
implemented yet.

## Context

An assistant with a write grant edits a published resume, and the next public
request shows the edit with no owner review
([ADR 0018](0018-mcp-agent-access.md)). A library project edit reaches every
live resume that links it ([ADR 0029](0029-account-project-library.md)). No
earlier state of a resume can be restored, and `revision` is only a
compare-and-swap counter and the public generation
([ADR 0010](0010-public-artifact-revocation.md)).

The owner decided that reviewed changes ship before assistant-led review, and
that review is on by default for published resumes
([roadmap](../plans/roadmap.md#owner-decisions)). The roadmap keeps a separate
draft copy of the whole resume out of scope.

## Decision

1. **Checkpoints.** Every resume keeps a checkpoint of its restorable state
   before each editing session, for a bounded time and count. A checkpoint holds
   no photo key and never protects a media object. The owner can delete one or
   clear them.
2. **Restore.** The owner restores text, layout, title and language. Restore
   never changes the photo, publish or sharing settings, the review setting, the
   project library or evidence. It is an ordinary owner save, and it first
   checkpoints the state it replaces.
3. **Review setting.** Each resume has a review setting, on by default,
   effective while the resume is published and the server's review flag is on.
   Only the owner's signed-in session changes it. With the flag off, assistants
   and the owner see the behavior and text of the release before review.
4. **One proposal per assistant and resume.** While review is effective, each
   resume write tool adds to its grant's proposal instead of saving. The
   proposal stores fine-grained parts, each with its value before and its
   proposed value, and the assistant works on its own view of the resume with
   those parts applied. Parts one call changed together form a change that the
   owner accepts or rejects. Accepting writes the stored proposed values only
   when every part still holds its value before, so changes to other parts stay
   valid after one is accepted.
5. **Refusals.** While review is effective, `upload_photo` and `delete_resume`
   are refused, and so are library edits to a project that a resume under review
   links, until library proposals ship. A proposed link pins the project's
   revision.
6. **Assistants see their own outcomes, not history.** Write results say whether
   they saved or proposed, and an assistant can list its own proposals and their
   outcomes. Assistants never read checkpoints or other grants' proposals,
   decide proposals, restore, or change the review setting.
7. **Lifetime.** Pending proposals expire. The owner's revocation of an
   assistant withdraws its pending proposals. Outcomes are kept briefly for the
   assistant to read, and everything is deleted with the resume or account.
   Export includes checkpoints and pending proposals.

## Alternatives rejected

- **A draft copy of the whole resume.** Out of scope in the roadmap; the owner
  and the assistant would edit diverging copies that need a merge.
- **A proposal per tool call.** Calls that build on each other, such as a new
  section and its entries, could not be reviewed together, and partial updates
  would replace each other.
- **Staleness by whole revision.** Accepting one change would invalidate every
  other pending change.
- **Re-running the tool on accept.** It can produce a different result from the
  one the owner reviewed, such as a new entry id or newer library text.
- **A checkpoint per save, or a copy after every save.** The editor saves on
  every change; the first floods history, the second doubles every write.
- **Stored diffs instead of copies.** Restoring through schema versions gets
  harder, and copies fit the size limits.
- **Review only in the assistant's instructions.** A stolen token ignores
  instructions; the server must refuse.
- **Proposals for photo uploads.** An unaccepted object would need a new media
  reference class under [ADR 0009](0009-private-media-delivery.md).
- **Withdrawing proposals on every revocation.** A client's own token revocation
  at the end of a run is routine, not distrust.
- **Restoring publish settings.** A restore could republish a resume or reopen
  access the owner closed.

## Consequences

- MCP results, `list_resumes`, `get_resume`, the closed error vocabulary and the
  tool registry change; ADR 0018 records it, and the MCP guide changes when
  review ships.
- Two tables grow per resume within fixed limits, and backups hold up to 30 days
  of earlier text per resume. The privacy notice and the Terms change in both
  languages.
- The editor gains History and Proposed changes panels; the owner stream gains a
  frame for proposals.
- The export format version rises twice and its cap grows.
- The review release ships behind a flag and is activated as the release floor
  before review turns on, so a rollback below it is refused.

## History

- 2026-11-21: proposed as a new record for roadmap v0.10, and accepted by the
  owner after an independent review.
