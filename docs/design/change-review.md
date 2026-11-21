# Changes you review

Status: Accepted by the owner (2026-11-21) after an independent review, with the
decisions below. History (release 1) shipped in v0.10.0; review of assistant
writes is next. It designs roadmap version
[v0.10](../plans/roadmap.md#v010-changes-you-approve) and is recorded in
[ADR 0031](../adr/0031-reviewed-assistant-changes.md).

With review on, nothing an assistant writes over MCP reaches a published resume
until the owner accepts it. Every resume keeps restorable checkpoints for a
bounded time. "Review" names this setting to keep it apart from the OAuth
consent the [MCP guide](mcp-guide.md) calls approval.

## What exists

- A resume is one `resumes` row. Its `revision` is both the editor's
  compare-and-swap counter and the public generation
  ([ADR 0010](../adr/0010-public-artifact-revocation.md)). Any update of the row
  notifies open public pages and the preview-card scheduler
  ([realtime](realtime.md)). A public read selects the same row that saves
  write, so an MCP write to a published resume is public on the next request.
- MCP has 22 tools ([owner workflow](mcp-owner-workflow.md)): 5 read and 17
  write. Nine change a resume's content, three its photo, and three the account
  project library; `create_resume` makes a private resume, and `delete_resume`
  revokes a published one. Library text is joined into project pages at read
  time, so a library edit reaches every live resume that links the project
  ([projects contract](projects-contract.md)).
- Nothing records who made a write, and no earlier state can be restored.

## Owner decisions

| #   | Decision                   | Decided (owner, 2026-11-21)                                                                                         |
| --- | -------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| D1  | What a restore brings back | Text, layout, title and resume language. Never the photo, publish or sharing settings, library projects or evidence |
| D2  | How long history lasts     | 30 days, at most 50 checkpoints per resume, one per editing session of up to an hour                                |
| D3  | Tools refused under review | `upload_photo` and `delete_resume`; the assistant tells the owner to use the editor                                 |
| D4  | Pending proposal lifetime  | 14 days. When the owner revokes an assistant, its pending proposals are discarded                                   |
| D5  | Export                     | Includes checkpoints and pending proposals; the export cap rises from 16 to 24 MiB                                  |
| D6  | Release order              | History, then review of resume writes, then review of library project edits                                         |
| D7  | Default                    | On for every resume, effective while it is published ([roadmap](../plans/roadmap.md#owner-decisions), item 4)       |

The public title and favicon emoji are set with the publish settings, so D1
leaves them out of checkpoints and restore.

## Checkpoints

A checkpoint is a stored copy of a resume's restorable state as it was before an
editing session: title, language, personal details without the photo, content,
customization, and the document schema version. It records when the session
started, its cause (`edit`, `assistant`, `accept`, `restore`), and its actor:
the owner, or an assistant's grant with the client's display name copied at the
time. A library rewrite counts as the actor whose project edit caused it.

The editor saves on every change, so a checkpoint covers a session, not a save.
The `resumes` row records the time and actor of its last restorable write and
when that session started. In the transaction of every write that changes
restorable state, before the write:

1. Start a new session when the resume has no session yet, the actor differs
   from the last restorable write's, that write is more than 10 minutes old, or
   the session started more than an hour ago. Accept and restore always start
   one.
2. On a new session, insert a checkpoint holding the state before this write.

Within a session nothing is copied, so an autosave costs one extra row write
only when it starts a session. The current state is the resume itself. A write
that leaves restorable state unchanged, such as a publish setting or a new
photo, neither starts a session nor adds a checkpoint. Creating a resume starts
no session; its first restorable write checkpoints the created state.

### Restore and deletion

The owner restores a checkpoint from the editor. A restore:

- first stores the current state as a checkpoint, so it can be undone;
- replaces title, language, personal details except the photo, content and
  customization with the checkpoint's;
- keeps the photo and its crop, slug, publish, download, search, sign in to view
  and showcase settings, public title, favicon emoji, and the review setting;
- keeps each project entry's text and drops its library link when that library
  project is missing from the library, as a library delete does;
- converts a checkpoint at an older schema version in memory, as reads do
  ([ADR 0004](../adr/0004-resume-document-contract.md)), validates the result as
  a normal write, and fails closed if it is invalid;
- is a compare-and-swap save with `If-Match` and an idempotency key. It advances
  the revision, is a non-draining edit under ADR 0010, and is public at once on
  a published resume, like any owner save.

The owner can delete one checkpoint or clear a resume's history. Assistants
never list, read, restore or delete checkpoints.

### Checkpoint storage and retention

Checkpoints live in their own table keyed by resume, so taking one notifies
nobody. Each row stores, at insert, the UTF-8 byte length of its JSON as
exported. The insert deletes the oldest checkpoints beyond 50 or beyond 2 MiB
for that resume, and the daily privacy retention sweep deletes each checkpoint
before it is 30 days old; reads never show an older one. A checkpoint at a
schema version the release does not accept is deleted and never served.
Checkpoints hold no photo key, so they never keep a photo object alive
([ADR 0009](../adr/0009-private-media-delivery.md)).

## Review

Each resume has a review setting, on by default. It belongs to the publish
settings, so only the owner's signed-in session changes it; no MCP tool can.
Review is effective when the server's review flag is on, the resume is
published, and its setting is on.

While the flag is off:

- every tool saves directly, the registry has 22 tools, and write results carry
  no `outcome`;
- the editor shows neither the review setting nor the Proposed changes panel;
- the consent screen, guide, privacy notice and Terms show their v0.10.0 text;
- pending proposals are kept, hidden, and still expire; turning the flag on
  shows them again, each checked for staleness as usual.

### Tools under review

While review is effective:

- these tools add to a proposal instead of saving: `update_resume_metadata`,
  `upsert_entry`, `delete_entry`, `update_section`, `update_structure`,
  `update_personal_details`, `update_customization`, `link_project`,
  `refresh_project_entry`, `update_photo_crop` and `delete_photo`;
- `upload_photo` and `delete_resume` are refused with the closed error
  `owner_review_required`. An upload writes the object before the database
  commit, and an unaccepted object would need a new media reference class. A
  delete revokes the page, which the owner does in the editor;
- `update_project` and `delete_project` are refused with the same error when the
  project is linked, or linked by a pending proposal, from any resume under
  review. The third release turns them into project proposals.

`create_project` saves directly, since a new project is on no resume until a
link is accepted. Without effective review, every tool saves directly.

### Proposals and parts

A proposal is one assistant's pending changes to one resume: at most one per
grant and resume. It is divided into parts at the granularity of the editor's
conflict keys (`targetKey` in `apps/web/app/editor/commands.ts`): each metadata
field, each personal details field, the photo crop, the photo object, the
section structure, each section setting and its entry order, each entry field,
and each customization path. An inserted or deleted entry or section is one part
holding the whole object. Each part stores its value before (`base`) and its
proposed value (`result`). A photo part stores a SHA-256 digest of the object
key, never the key.

A tool call under review runs the tool's own validation and handler logic
against the assistant's view: the stored resume with its proposal's parts
applied where the stored value still equals `base`. The `revision` the tool
names must equal the stored revision, or it answers `revision_conflict`. The
call then diffs its view before and after and merges the changed parts into the
proposal:

- a part already in the proposal whose stored value still equals its `base`
  keeps that `base` and takes the new `result`;
- a part already in the proposal whose stored value differs from its `base`
  leaves its old change, takes the stored value as its new `base`, and starts a
  new change, so an assistant that works from the owner's newer text proposes
  against it;
- a new part takes the stored value as `base`;
- a part whose `result` equals its `base` leaves the proposal.

An inserted or deleted entry and its section's entry-order part always stay in
one change, and a rebase moves them together. Each change touched by the call
gets a new sequence number. Nothing in the `resumes` row changes, so the
revision, public pages, caches and preview card stay as they are. The call
commits with its idempotency record
([ADR 0006](../adr/0006-transactional-idempotency.md)) and charges the same
write limits.

A link or refresh part also pins the library project's revision, and the review
shows that project's page content at that revision.

### Changes

The owner reviews changes, not parts. Parts that one tool call changed together
belong to one change, and changes that share a part merge, so a new section and
its entries, or a deleted section with its entries and layout, are one change. A
change is:

- **pending** while every part's stored value equals its `base`;
- **stale** when any part's stored value differs from both `base` and `result`,
  when a pinned project revision has moved, or when the document schema version
  has changed since the proposal began.

### Accept and reject

Accept and reject are owner-only REST writes with the signed-in session, CSRF,
an idempotency key, and the sequence number of each selected change as the owner
saw it. A different sequence number answers `409 proposal_changed` and the panel
reloads; changes the assistant has not touched since stay decidable. Accept also
takes `If-Match`; reject changes no resume and takes none.

Accepting one change, or all, follows the resume write lock order in
[data](data.md), then locks the proposal row. Each selected change must be
pending. For each of its parts the stored value is set to `result`; parts
already equal to `result` stay. The document is validated once and saved with a
new revision, a pre-accept checkpoint, and one notification. A stale selected
change answers `409 proposal_stale`; an invalid result answers
`422 validation_failed`; either way nothing is saved. A stale change can only be
dismissed.

Accepted and rejected parts leave the proposal. When none remain it is decided.

### Lifetime

- A pending proposal expires 14 days after its first part.
- The owner revoking an assistant, and a second-factor change that revokes every
  grant, withdraw the grant's pending proposals in the revoke transaction. A
  client's own token revocation and the idle-client cleanup keep them for
  review.
- Proposals and checkpoints outlive the grant they name: the reference is
  nullable and set to null when the grant or its client is deleted, and the
  copied display name still names the client. The owner discards such a proposal
  with Reject all.
- Turning review off or unpublishing leaves pending proposals for review.
- A decided, expired or withdrawn proposal keeps only its outcome, with no part
  values, for 7 days, so the assistant can read it.
- Deleting the resume or the account deletes its proposals and checkpoints.

### What the assistant sees

[ADR 0018](../adr/0018-mcp-agent-access.md) records these changes, and the
[MCP guide](mcp-guide.md) changes when they ship:

- A write result gains `outcome`, `saved` or `proposed`. A proposed result
  carries the stored resume and the assistant's view.
- `list_resumes` and `get_resume` say whether review is effective and return the
  calling grant's view when it has a pending proposal.
- A read tool, `list_proposals`, needs `resumes:read` and returns the calling
  grant's own proposals that are pending or decided in the last 7 days, with
  each change's outcome. No tool shows another grant's proposals. The registry
  grows to 23 tools.
- The closed error vocabulary gains `owner_review_required` and
  `proposal_limit`.
- The server instructions, the consent screen and the in-app guide say that
  assistant edits to a published resume under review wait for the owner.

### Review screen

When a resume has pending proposals, the editor shows a Proposed changes panel.
Each change names the assistant and time and shows the current value beside the
proposed value. Its actions are Accept and Reject; the panel has Accept all and
Reject all. A stale change says the resume changed after the proposal and offers
only Dismiss. The resume list shows a pending count per resume. Proposed text is
shown through the editor's sanitizer path, never as raw HTML.

The editor also gains a History panel: checkpoints with time, cause and actor; a
read-only preview of each through the renderer; Restore, with a confirmation
that names the public effect on a published resume; and Delete.

Exact layout, breakpoints and copy in Vietnamese and English come from a UI
design before frontend work ([workflow](../../instructions/workflow.md)).

### Library project proposals

The third release lets `update_project` and `delete_project` store a proposal
against the library project when a linking resume is under review. Its one part
is the whole project, it is stale when the project revision moves, and it is
reviewed in the project editor. Accepting it applies the edit as the owner's
library save, which reaches linking resumes.

## Realtime and revocation

A change to a proposal sends an owner-only stream frame with the resume ID, so
an open editor reloads its panels. Public streams get nothing until an accept
saves. Accept and restore are non-draining edits. Nothing here changes publish
state, so no revocation fence is added.

## Data lifecycle and privacy

- Export: version 3 in v0.10.0 adds checkpoints; version 4 in v0.10.1 adds
  pending proposals, including those hidden while the flag is off. The version 2
  worst case is about 12.3 MiB; adding 6 MiB of checkpoints and 4 MiB of
  proposals stays under the 24 MiB cap.
- Checkpoints and proposals never reach a public read, cache, preview card,
  showcase, sitemap or `llms.txt`.
- Backups hold them for the backup period after deletion, as they hold every
  table.
- The privacy notice and the content permission in the Terms, in both languages,
  state the 30-day history, that text removed from a resume stays in its history
  for up to 30 days unless the owner deletes the history, that deleting a resume
  or the account deletes its history, the proposal lifetime, and what an
  assistant can still change at once.

## Security

| Attacker               | Invariant                                                                                                                                                                                                                    |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Stolen MCP token       | Under review, no tool changes a stored value a public representation reads. No tool reads checkpoints or other grants' proposals, accepts, rejects, restores, or changes review                                              |
| Stolen MCP token       | It fills at most its own proposals, but can use the account's shared proposal bytes and block other assistants until the owner revokes it, which withdraws them. It cannot link a project and edit it after the owner looked |
| Stolen MCP token       | Without review it can still delete an unpublished resume with its history; that power predates this design                                                                                                                   |
| Stolen session         | It can accept, restore and delete history, the same authority it already has over the resume                                                                                                                                 |
| Another signed-in user | Every proposal and checkpoint query is scoped by the owner's account; ids are random                                                                                                                                         |
| Hostile resume content | A proposal is validated on every tool call and again on accept, and rendered through the editor's sanitizer                                                                                                                  |
| Leaked backup          | It holds up to 30 days of earlier text and pending proposal text per resume                                                                                                                                                  |
| Container, DB, network | No new secret, route class or transport; their reach is unchanged                                                                                                                                                            |
| Anonymous visitor      | Nothing new is public                                                                                                                                                                                                        |

The release test for review: with review effective on a published resume, call
every one of the 23 tools with valid input, then compare the revision, the
public HTML, JSON, Markdown, PDF, photo and project pages, the preview card, the
showcase listing, `sitemap.xml` and `llms.txt` with their state before. They
must be unchanged.

## Limits

[Budgets](budgets.md) takes these values when each part ships.

| Limit                              | Value                        |
| ---------------------------------- | ---------------------------- |
| Checkpoints per resume             | 50, and 2 MiB                |
| Checkpoint age                     | 30 days                      |
| Checkpoint session                 | 10 minutes idle, 1 hour long |
| Stored parts per proposal          | 1 MiB                        |
| Pending proposal bytes per account | 4 MiB                        |
| Pending proposal lifetime          | 14 days                      |
| Outcome retention                  | 7 days                       |
| Account export                     | 24 MiB                       |

Bytes are counted as for checkpoints. A tool call that would pass a proposal
limit answers `proposal_limit`.

## Release order

Each part is one patch release with its own migration
([ADR 0005](../adr/0005-database-migrations.md)):

1. **v0.10.0, history.** The checkpoint table, the session rule in every resume
   write path, the history API, the History panel with restore and delete,
   export, and the notice and Terms copy.
2. **v0.10.1, review of resume writes.** The review setting, proposals, the MCP
   changes with `list_proposals`, the refusals including library edits, the
   review screen, owner frames, and the consent, guide, notice and Terms copy.
   It holds roadmap features 2 to 5 together, because proposals without the
   screen, the setting or the tool cannot be used. Its migration only adds, so
   it raises no schema floor. It ships with a feature flag off, and assistant
   writes save directly until the owner turns it on. After the release is
   healthy, the owner activates it as the release floor (deploy runbook,
   activation), then turns the flag on, so no rollback reaches a release without
   review while review is on ([release floors](release-floors.md)).
3. **v0.10.2, review of library project edits.** Project proposals and their
   review in the project editor.

## Left to the contracts

The OpenAPI paths and shapes, table columns and indexes, the exact part keys per
operation, the owner frame shape, and the MCP result schema are written as
contract changes with each release, from this design: the
[resume history contract](resume-history-contract.md) for v0.10.0.
