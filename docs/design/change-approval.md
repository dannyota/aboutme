# Changes you approve

Status: Proposed (2026-11-21), awaiting the owner's decisions below and an
independent review. It designs roadmap version
[v0.10](../plans/roadmap.md#v010-changes-you-approve) and is recorded in
[ADR 0031](../adr/0031-approved-assistant-changes.md).

With approval on, nothing an assistant writes over MCP reaches a published
resume until the owner accepts it. Every resume keeps restorable checkpoints for
a bounded time.

## What exists

- A resume is one `resumes` row. Its `revision` is both the editor's
  compare-and-swap counter and the public generation
  ([ADR 0010](../adr/0010-public-artifact-revocation.md)). Any update of the row
  notifies open public pages and the preview-card scheduler
  ([realtime](realtime.md)).
- A public read selects the same row that saves write. An MCP write to a
  published resume is public on the next request.
- MCP has 22 tools ([owner workflow](mcp-owner-workflow.md)). Eleven write a
  resume, three write its photo, three write the account project library, and
  `delete_resume` revokes a published resume. Library text is joined into
  project pages at read time, so a library edit reaches every live resume that
  links the project ([projects contract](projects-contract.md)).
- Nothing records who made a write, and no earlier state can be restored.

## Owner decisions

Product choices proposed here. Each line is the recommendation; the owner
confirms or changes it before code starts.

| #   | Decision                   | Proposed                                                                                                                       |
| --- | -------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| D1  | What a restore brings back | Text, layout, title and resume language. Never the photo, publish or sharing settings, library projects or evidence            |
| D2  | How long history lasts     | 30 days, at most 50 checkpoints per resume, one checkpoint per editing session of up to an hour                                |
| D3  | Tools refused under review | `upload_photo` and `delete_resume`. The assistant tells the owner to do it in the editor                                       |
| D4  | Pending proposal lifetime  | 14 days. Revoking an assistant discards its pending proposals                                                                  |
| D5  | Export                     | Includes checkpoints and proposals; the export cap rises from 16 to 24 MiB                                                     |
| D6  | Release order              | History first, then review of resume writes, then review of library project edits                                              |
| D7  | Default                    | Already decided: on for every resume, effective while it is published ([roadmap](../plans/roadmap.md#owner-decisions), item 4) |

## Checkpoints

A checkpoint is a stored copy of the parts of a resume a restore brings back:
title, language, public title, favicon emoji, personal details without the
photo, content, customization, and the document schema version. It also records
when it was taken, its cause (`edit`, `agent`, `accept`, `restore`, `library`),
and who caused it: the owner, or an assistant's grant with the client's display
name copied at the time.

The editor saves on every change, so a checkpoint covers a session, not a save.
In the transaction of every write that changes restorable state:

1. If the resume has no checkpoint yet, store the state before the write first.
2. If the newest checkpoint has the same cause and actor, was updated less than
   10 minutes ago, and was created less than an hour ago, overwrite it with the
   state after the write.
3. Otherwise insert a new checkpoint with the state after the write. Accept and
   restore always insert.

The newest checkpoint therefore always equals the current state, and the one
before it is the state before the latest session. A write that leaves restorable
state unchanged, such as a publish setting or a new photo, adds nothing.

### Restore

The owner restores a checkpoint from the editor. A restore:

- replaces title, language, public title, favicon emoji, personal details except
  the photo, content and customization with the checkpoint's;
- keeps the current photo and its crop, slug, publish state, download, search
  and sign in to view settings, showcase settings, and the approval setting;
- keeps each project entry's text, and drops its library link when that library
  project no longer exists, as a library delete does;
- converts a checkpoint at an older schema version in memory, as reads do
  ([ADR 0004](../adr/0004-resume-document-contract.md)), validates the result as
  a normal write, and fails closed if it is invalid;
- is a compare-and-swap save with `If-Match` and an idempotency key. It advances
  the revision, is a non-draining edit under ADR 0010, and is public at once on
  a published resume, like any owner save.

Assistants never list, read or restore checkpoints.

### Checkpoint storage and retention

Checkpoints live in their own table, keyed by resume, never in the `resumes`
row, so taking one notifies nobody. Each row stores its byte size at insert. The
write that inserts a checkpoint deletes the oldest beyond 50 or beyond 2 MiB per
resume, and an hourly sweep deletes checkpoints older than 30 days, except the
newest of each resume. A checkpoint whose schema version is no longer accepted
is deleted and never served. Checkpoints hold no photo key, so they never keep a
photo object alive ([ADR 0009](../adr/0009-private-media-delivery.md) is
unchanged).

## Approval

Each resume has an approval setting, on by default. It is effective while the
resume is published. It lives with the publish settings, so only the owner's
signed-in session changes it; no MCP tool can.

### Which writes become proposals

While approval is effective, these tools store a proposal instead of a save:
`update_resume_metadata`, `upsert_entry`, `delete_entry`, `update_section`,
`update_structure`, `update_personal_details`, `update_customization`,
`link_project`, `refresh_project_entry`, `update_photo_crop` and `delete_photo`.

`upload_photo` and `delete_resume` are refused with the closed error
`owner_approval_required`. An upload writes the object before the database
commit, and holding an unaccepted object would need a new media reference class.
A delete revokes the page, which the owner does in the editor.

`update_project` and `delete_project` are refused with the same error when any
resume that links the project has approval effective. The third release turns
them into project proposals (below). `create_project` stays direct: a new
project is on no resume.

On an unpublished resume, or with approval off, every tool saves directly, as
today.

### What a proposal holds

A proposal is created by the same validation and the same handler logic as the
save it replaces, run against the stored resume at the revision the tool names.
A stale revision answers `revision_conflict`, as today. Instead of writing the
row, the transaction stores:

- the operation and its validated arguments;
- the part of the resume the operation reads and writes, using the partition the
  editor uses for conflicts: one entry with its section membership and position,
  one section's settings, the section structure, personal details,
  customization, metadata, or the photo;
- that part's value before (`base`) and after (`result`);
- the document schema version, the grant and the client's display name, and its
  creation and expiry times.

It commits with the tool's idempotency record
([ADR 0006](../adr/0006-transactional-idempotency.md)) and charges the same
write limits. It does not touch the `resumes` row, so the revision, public
pages, caches and preview card stay as they are.

A new proposal from the same grant for the same part replaces that grant's
pending one, which becomes `superseded`. Proposals from different assistants for
the same part both stay pending.

### Accept and reject

Accept and reject are owner-only REST writes with the signed-in session, CSRF,
`If-Match` and an idempotency key. Accepting one proposal locks the resume row,
then the proposal, and requires it to be pending, unexpired, and at the current
schema version. It then compares the part's current value:

- equal to `result`: marked `accepted`, nothing written;
- equal to `base`: the stored operation is applied to the current document
  through the normal write path, validated, saved with a new revision and a
  checkpoint (cause `accept`), and marked `accepted`;
- anything else: marked `stale` and answered `409 proposal_stale`. A stale
  proposal can only be dismissed.

Because the check is per part, accepting one proposal does not invalidate
proposals for other parts. Accept all applies pending proposals in creation
order in one transaction with one revision and one checkpoint, and reports each
outcome; stale ones are skipped. Reject marks a proposal `rejected`; reject all
does so for every pending proposal of the resume.

### Lifetime

- A pending proposal expires after 14 days.
- Revoking a grant marks its pending proposals `withdrawn` in the revoke
  transaction. Accept re-reads the proposal's status under lock, so a revoke
  racing an accept cannot apply a withdrawn proposal.
- Turning approval off or unpublishing leaves pending proposals for review.
- A decided proposal is deleted 7 days after its decision, so the assistant can
  read the outcome.
- Deleting the resume or the account deletes its proposals and checkpoints.

### What the assistant sees

[ADR 0018](../adr/0018-mcp-agent-access.md) and the [MCP guide](mcp-guide.md)
change when this design is accepted:

- A write result gains `outcome`: `saved` or `proposed`. A proposed result
  carries the proposal (id, part, status, expiry) and the stored resume, which
  the proposal did not change.
- `list_resumes` and `get_resume` say whether approval is effective and list
  that resume's pending proposals.
- A new read tool, `list_proposals`, returns a resume's proposals that are
  pending or decided in the last 7 days, with their outcome. The registry grows
  to 23 tools.
- The closed error vocabulary gains `owner_approval_required` and
  `proposal_limit`.
- The server instructions, the consent screen and the in-app guide stop saying
  that assistant edits to a published resume are public at once.

### Review screen

When a resume has pending proposals, the editor shows a Proposed changes panel.
Each item names the assistant and time and shows the part's current value beside
the proposed value. Its actions are Accept and Reject; the panel has Accept all
and Reject all. A stale item says the part changed after the proposal and offers
only Dismiss. The resume list shows a pending count per resume. Proposal text is
shown through the editor's sanitizer path, never as raw HTML.

The editor also gains a History panel: checkpoints with time, cause and actor; a
read-only preview of each through the renderer; and Restore, with a confirmation
that names the public effect on a published resume.

Exact layout, breakpoints and copy in Vietnamese and English come from a UI
design before frontend work ([workflow](../../instructions/workflow.md)).

### Library project proposals

The third release lets `update_project` and `delete_project` store a proposal
against the library project when a linking resume has approval effective. Its
part is the whole project, its staleness is the project revision, and it is
reviewed in the project editor. Accepting it applies the edit as the owner's own
library save, which reaches linking resumes as today.

## Realtime and revocation

Creating, superseding or deciding a proposal sends an owner-only stream frame
with the resume ID so an open editor refreshes its panels. Public streams get
nothing until an accept saves. Accept and restore are non-draining edits; no
part of this design changes publish state, so no revocation fence is added.

## Data lifecycle and privacy

- Export (`exportVersion` 3) adds each resume's checkpoints and proposals.
- Checkpoints and proposals never reach a public read, cache, preview card,
  showcase, sitemap or `llms.txt`.
- Backups hold them for the backup period after deletion, as they do every
  table.
- The privacy notice, in both languages, states the 30-day history, the proposal
  lifetime, and what an assistant can still change at once.

## Security

| Attacker               | Invariant                                                                                                                                                                          |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Stolen MCP token       | With approval effective, no tool changes a stored value that a public representation reads. No tool reads checkpoints, accepts, rejects, restores, or changes the approval setting |
| Stolen MCP token       | Storage it can fill is bounded by the proposal limits. Revoking the grant withdraws everything it proposed                                                                         |
| Another signed-in user | Every proposal and checkpoint query is scoped by the owner's account; ids are random                                                                                               |
| Hostile resume content | A proposal is validated when stored and again when accepted, and rendered through the editor's sanitizer                                                                           |
| Leaked backup          | It now holds up to 30 days of earlier text per resume. The privacy notice says so                                                                                                  |
| Anonymous visitor      | Nothing new is public                                                                                                                                                              |

The release test for approval: with approval effective on a published resume,
call every one of the 23 tools with valid input, then compare the public HTML,
JSON, Markdown, PDF, photo and project pages and the revision with their state
before. They must be unchanged.

## Limits

Proposed values; [budgets](budgets.md) takes them when each part ships.

| Limit                             | Value                   |
| --------------------------------- | ----------------------- |
| Checkpoints per resume            | 50, and 2 MiB stored    |
| Checkpoint age                    | 30 days, newest kept    |
| Checkpoint session                | 10 minutes idle, 1 hour |
| Pending proposals per resume      | 100                     |
| Stored proposal bytes per account | 4 MiB                   |
| Pending proposal lifetime         | 14 days                 |
| Decided proposal retention        | 7 days                  |
| Account export                    | 24 MiB                  |

## Release order

Each part is one patch release with its own migration
([ADR 0005](../adr/0005-database-migrations.md)):

1. **v0.10.0, history.** The checkpoint table and rule in every resume write
   path, the history API, the History panel and restore, export, and the privacy
   notice.
2. **v0.10.1, review of resume writes.** The approval setting, proposals, the
   MCP changes and `list_proposals`, the refusals including library edits, the
   review screen, owner frames, and the consent, guide and notice copy. It ships
   with approval on, so the first published resume an assistant touches after
   the deploy already needs review.
3. **v0.10.2, review of library project edits.** Project proposals and their
   review in the project editor.

## Left to the contracts

The OpenAPI paths and shapes, table columns and indexes, the exact part
partition per operation, the owner frame shape, and the MCP result schema are
written as contract changes with each release, from this design.
