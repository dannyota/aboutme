# Phase 4 Editor Design

Status: Approved product direction; implementation design pending review.

## Purpose

Phase 4 adds the authenticated browser editor on top of the completed resume API
and renderer. It gives an owner immediate local feedback while preserving the
server's whole-document validation, revision compare-and-swap (CAS), and
transactional idempotency rules.

The editor is client-only. It never fetches authenticated data during Nuxt
server-side rendering (SSR).

Three linked contracts complete this design:

- [Mutation contract](mutation-contract.md) owns commands, autosave,
  idempotency, retries, stale reconciliation, and conflicts.
- [Template group contract](template-group-contract.md) owns multi-request
  template completion, partial recovery, and undo admission.
- [Editor contract](editor-contract.md) owns forms, structure controls, rich
  text, customization, templates, photos, accessibility, and browser proof.

## Scope

Phase 4 includes:

- `/app/resumes`, which lists, creates, opens, renames, and deletes owned
  resumes;
- `/app/resumes/{id}`, which edits one owned current-version resume;
- personal details and all eight section types;
- section creation, deletion, placement, and order, plus entry order;
- customization controls and committed template presets;
- private owner photo upload, replacement, crop, read, and deletion;
- immediate preview through the Phase 3 pure renderer and paged wrapper;
- one-second autosave through the Phase 2B granular API; and
- authenticated browser proof through the native HTTPS harness.

Phase 4 excludes publishing, public routes, Server-Sent Events (SSE), PDF and
image export, cloud changes, and the complete Phase 9 port-443 UAT overlay.

## Authorities

This design consumes, without redefining:

- the draft-permissive aggregate and current v2 generated types;
- the Phase 2B OpenAPI, write-safety, `412`, and private-photo contracts;
- the Phase 3 sanitizer, renderer, paginator, template registry, and
  `applyTemplate` helper;
- ADR 0008 template application, ADR 0009 section-order authority, ADR 0016
  transactional idempotency, and ADR 0017 document versioning; and
- the native HTTPS authentication harness and local UAT runbook.

The current OpenAPI is the endpoint and response authority. This design does not
add or alter an API route.

## Route boundaries

### Resume list

`/app/resumes` loads only `/api/v1/resumes` summaries after authentication
resolves. A caller with no resumes sees an empty list.

Create asks for an owner-visible title and optional resume language. It omits
`document`, so the server creates its blank current-v2 document. Success
redirects to `/app/resumes/{id}`. The server remains the authority for the
three-resume cap.

A summary revision is display data, not sufficient mutation state. Rename and
delete from the list first fetch the complete resume and response `ETag` with
`GET /api/v1/resumes/{id}`. They then enter the same store and mutation queue as
editor actions. Delete requires confirmation naming the owner-visible title.

### Resume editor

`/app/resumes/{id}` fetches the complete owner response in the browser. It
validates the schema version, body revision, and response `ETag` before enabling
editing. Missing and wrong-owner IDs share one unavailable state.

The editor has four component boundaries:

- `EditorShell` owns navigation guards, save status, errors, and the responsive
  editor/preview layout.
- Editor panels translate form actions into typed local commands. They do not
  call `fetch` or mutate renderer components.
- The Pinia resume store owns accepted state, optimistic state, command queues,
  conflicts, and private photo-read state.
- The mutation coordinator is the only resume API writer. It owns CSRF,
  `If-Match`, schema headers, idempotency keys, retries, and response adoption.

The editor imports the renderer. Renderer code never imports editor, Pinia, API,
or Nuxt runtime code.

## State model

One Pinia store instance is keyed by resume ID. It separates document state from
summary metadata:

| State               | Meaning                                                                               |
| ------------------- | ------------------------------------------------------------------------------------- |
| `acceptedDocument`  | Last acknowledged current-v2 document                                                 |
| `acceptedRevision`  | Last acknowledged canonical decimal revision string                                   |
| `acceptedMetadata`  | ID, title, language, publish summary, and timestamps                                  |
| `metadataFreshness` | Whether `updatedAt` and other summary values came from a complete read/write response |
| `current`           | Accepted state with in-flight and pending commands replayed                           |
| `pending`           | Ordered unsent commands after permitted coalescing                                    |
| `inflight`          | At most one immutable request attempt                                                 |
| `conflicts`         | Local intents stopped by reconciliation                                               |
| `issues`            | Server validation issues indexed by document path                                     |
| `photoRead`         | Authorized photo data URL, object ETag, accepted-photo binding, and status            |

`acceptedRevision` is the source for the next normal `If-Match`. Its ETag form
is derived as `"r<revision>"` only after the decimal revision is validated. It
is never converted to a JavaScript number. `current` is never described as
accepted or saved.

Save state is one of `idle`, `dirty`, `saving`, `saved`, `offline`, `error`,
`conflict`, or `session-lost`. A template group adds a visible partial state; it
is not collapsed into `saved`.

All commands are pure transformations. Replaying a command does not mutate its
input or generate an ID, clock value, or network request. Entry and custom
section UUIDs are generated once at the user action boundary.

## Preview boundary

Preview receives only:

- the optimistic current-v2 document;
- the resume's total language projection;
- `mode: "paged"`; and
- an authorized normalized photo data URL when photo metadata exists.

Its page count is labelled **Estimated pages** and is never sent to the server.
The renderer's schema and photo/context checks remain active. Preview failure
does not change accepted state.

If photo metadata exists but owner photo read fails, complete preview is
suspended because the renderer requires a photo URL. Forms, save controls, and
photo replace/delete controls remain usable. The editor does not remove photo
metadata or render a photo-free fallback.

## User flow

### Initial load

1. Resolve `/api/v1/me` in the browser.
2. An initial `401` may redirect to `/login` because no local resume work exists
   yet.
3. Fetch the list or complete resume.
4. Validate its current schema version and revision/ETag agreement.
5. Enable commands only after accepted state is complete.

### Edit and save

Each form action captures target base and base-state non-target context from
optimistic `current`, derives its intended target and intended-state non-target
context, then applies locally. Preview changes immediately. The mutation
contract serializes all commands for that resume after one second of inactivity.

The server may sanitize or canonicalize a successful write. Its acknowledged
state replaces the affected accepted state, then remaining local commands replay
over it.

Missing and explicitly cleared values remain distinct:

- untouched optional fields stay absent;
- clearing a previously present field writes `""` where the generated schema
  permits an empty draft value; and
- optional objects and values use their declared unset or null operation.

Focusing and blurring an untouched field creates no command. Draft completeness
is never enforced at save time.

### Session loss

Session loss before any local work may redirect to login. Session loss while a
command is pending, in flight, failed, or conflicted keeps the editor mounted,
stops dispatch, retains the in-memory work, and shows `session-lost`.

The owner may open authentication in another tab, return, refresh `/me`, and
resume the queue. The editor does not place resume data, CSRF state, or a return
payload in the authentication URL. **Discard and sign in** explicitly drops
local work before navigation. There is no automatic discard or redirect.

### Leave and recover

Pending commands live only in memory. Resume documents, CSRF tokens, photo
bytes, and mutation payloads are not stored in local storage or IndexedDB.

Route navigation and browser unload warn while work is pending, in flight,
failed, partial, conflicted, or held by session loss. A fully accepted state
leaves without a prompt. The editor does not claim that `sendBeacon` or an
unload-time request saved data.

## Error ownership

- The mutation contract decides whether a write is accepted, retryable,
  satisfied by observed state, stale-safe, partial, or conflicted.
- The editor contract maps validation issues and product errors to controls.
- An invalid response shape, current-version mismatch, malformed revision, or
  revision/ETag disagreement stops mutation and requires a complete owner read.
- A complete owner read never silently drops local commands. Each command is
  reconciled through its captured target and non-target context.
- Renderer and photo-read failures do not corrupt accepted state.
- Server or Nuxt outages do not cause unbounded retries.

## Security and privacy

All authenticated reads and writes are browser-only, same-origin, and use
credentials. Existing `useAuth` state is the sole CSRF-token source. Mutation
code cannot accept a token from a route, query, cookie, or form field.

The editor uses generated current schema and OpenAPI types. Local validation
improves feedback but never replaces strict server decoding, document bounds,
sanitizing, origin checks, idempotency, or revision CAS.

Rich text reaches `innerHTML` only through the Phase 3 renderer and client
sanitizer. Plain fields remain Vue text bindings. Photo objects remain private;
every read passes through Go ownership checks.

## Dependencies

- Phase 2B supplies the owner resume, write-safety, schema negotiation, and
  private photo APIs.
- Phase 3 supplies current types, sanitizer policy, pure renderer, paged
  preview, templates, fonts, and `applyTemplate`.
- The native HTTPS harness supplies real cookie, OAuth, CSRF, and browser trust
  behavior.
- Authenticated API transport through Caddy preserves exact strong `"rN"` ETags
  under `Cache-Control: no-store, no-transform`; owner responses are not
  transformed or given compression suffixes.
- Phase 4 adds pinned Pinia and ProseMirror packages through the reviewed web
  dependency window.

## Design-level acceptance

Phase 4 is design-complete only when implementation evidence proves:

1. Blank current-v2 create, list open, list rename, and list delete follow the
   route boundaries above.
2. Every form and structure control updates preview immediately without
   weakening draft permissiveness.
3. The mutation contract's serialization, retry, unknown-outcome, stale-write,
   and conflict scenarios pass.
4. Accepted document/revision and summary metadata never masquerade as one
   complete response after a bodyless mutation.
5. Session loss and photo failure preserve local work without rendering an
   invalid fallback.
6. Template-group and editor-contract sanitizer, photo privacy, accessibility,
   and native HTTPS browser scenarios pass.
