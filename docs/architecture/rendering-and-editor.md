# Rendering and editor

This page describes implemented behavior. See the
[running-system overview](../architecture.md) for deployment and dependencies.

## Implemented application chrome

The application chrome uses Tailwind CSS v4 without Preflight and generated
shadcn-vue primitives backed by reka-ui. Generated primitives under
`app/components/ui/` own low-level behavior; shared composites under
`app/components/app/` own the shell, page headers, fields, status and loading
states, empty states, menus, and dialogs; pages and editor panels compose those
layers with layout utilities. `AppShell` links the public template gallery and
renders Sign in, Create account, and a theme toggle until the session read
resolves. Signed-in navigation adds Resumes, Settings, and the account menu. The
signed-in theme action moves into the account menu. `AccountMenu` and
`ThemeToggle` provide account actions and the persisted theme choice.
Capability-gated provider and connected-agent controls remain hidden when their
capability is false.

The homepage, authentication pages, legal pages, template gallery, resume
workspace, settings, and agent consent support Vietnamese and English, with
Vietnamese as the default. These surfaces reuse the `aboutme-locale` cookie.
Interface language does not change resume content or resume language. See
[localization](../design/localization.md).

[ADR 0020](../adr/0020-application-visual-identity.md) owns the chrome palette,
typeface, seal, and state marks; the
[UI specs](../design/ui/identity-and-seal.md) define their presentation. The
renderer keeps its own document background. Publish and destructive actions keep
distinct controls. The landing page renders a compiled-in sample through the
pure renderer at server render and shows the seal without a data fetch.

The chrome follows an 8 px module and keeps the preview sheet whole. At narrow
widths the editor collapses its tool rail and outline, switches between Edit and
Preview tabs, and stacks the inspector below the sections control; the sheet
scales to the available viewport down to 390 px. Dialogs, menus, focus, and
reduced-motion behavior stay on the shared accessible component boundary.

## Implemented authenticated editor

The web app carries a full optimistic-save editor behind a per-resume mutation
queue. User actions become typed commands that capture target/base/intended/
context before replay, coalesce only adjacent same-target value edits, and
dispatch after one second of inactivity. A revision CAS reconciles stale writes,
offers one safe rebase per conflict, and surfaces dedicated field, entry,
reorder, structure, crop/photo, and destructive reconfirmation actions.
Templates apply against optimistic state, preserve content, and emit a
deterministic placement delta with exact partial recovery and guarded undo. Text
fields commit on blur or Enter, unset defined values when emptied, emit nothing
when unchanged, and revert on Escape. Enumerations, checkboxes, switches,
colors, and numbers commit on change. Generated primitives, shared application
composites, and editor surfaces keep those controls and dialogs on one
accessible component boundary. Photos upload as raw bytes and are never
previewed or decoded on the client; owner reads bind an in-memory data URL to
the accepted photo key. The preview imports the pure renderer, never derives a
photo URL from an object key, and renders a photo-less projection while the
owner read is loading or unavailable. Session loss retains in-memory work for
reauthentication; no browser storage or unload beacon holds resume data.

The editor's human-only Publish dialog keeps Public resume, PDF download, and
SEO and GEO as separate choices. It flushes editor work before freezing the
publish body, owner, parent revision, and idempotency key. One CSRF refresh may
replay that same attempt; an uncertain result keeps it for explicit retry, while
a stale winner is adopted without automatic republish. A live-slug rename uses
the account's supported recent-reauthentication factor. Provider handoff is
capability-gated, stable-first, allowlisted, and opened only by a second user
action. Structured completeness issues return focus to the owning editor field.

`EditorShell` composes the editor top bar, account menu, publish action, tool
rail, collapsible resume outline, preview region, and inspector panels. The
outline and inspector use section icons and preserve the selected field when an
error summary focuses an issue. `EditorPreview` renders the pure document
projection on a white, never-cropped sheet, reports the settled estimated page
count, shows photo availability without exposing an object key, and places an
`AppSeal` at the sheet foot when the resume is public. `ResumeList` presents up
to three paper-like resume cards with title, relative update time, a public
seal/link or pencil Draft mark, empty create slots, and an overflow menu for
Rename and Delete. The sessions settings page uses shared headers, status
banners, fields, and dialogs for signed-in devices, password changes, provider
linking, and connected-agent revocation. `PublishDialog` combines the slug field
and three independent switches with human-readable explanations,
reauthentication states, the seal-colored publish action, and a successful
public link with Copy link.

## Implemented public publish and SSR

Publish state is one row per resume holding the live flag, slug, discovery
generation, and a 180-day tombstone for released slugs. Public reads project a
closed, privacy-safe document that omits every account/owner/storage/private/
hidden value and re-sanitizes retained rich text. Public JSON, photos, HTML,
Markdown, sitemap, robots, and llms.txt derive from that projection. Live pages
remain accessible by link with discovery disabled; discovery artifacts respect
the SEO/GEO flag, and PDF download respects the download flag. Private and
unpublished resumes return the uniform public `404`. Nuxt renders public HTML
through an isolated worker with a five-second deadline, exact request/origin
bounds, deterministic JSON-LD, a single matching CSP hash, and external
hydration that replaces only a mismatched revision.

The native HTTPS publish proof creates one disposable complete resume, verifies
accepted-save ordering and the full mutation-header envelope, checks discovery
off and on, then unpublishes and observes the uniform public `404` within five
seconds. Its bounded local evidence contains statuses, elapsed time, header
presence, fixed step booleans, and error counters only. The proof deletes its
exact resume and signs out on success, and its `finally` path attempts the same
exact cleanup after a failed assertion. See
[agent access](auth-and-agent-access.md#implemented-agent-access) for the agent
tool boundary.

## Resume exports

Owner PDF export flushes pending editor changes before requesting the authorized
saved snapshot. A private listener redeems one-use, attempt-bound capabilities
for an immutable, sanitized document with its normalized photo inline. Nuxt
renders the shared continuous Vue components in a bounded worker thread.

The Go render queue admits one active job and eight queued jobs. Its 20-second
deadline includes queue time. Each job uses a fresh non-root sandboxed Chromium
process, a deny-by-default proxy, and exact document, CSS, and font requests.
Its environment contains only the fixed timezone and locale values; server
credentials are never forwarded to Chromium. Cleanup joins browser processes,
callbacks, private requests, and queue work.

Public PDF requires a live resume with download enabled. The fixed 1200×630
share image requires only a live resume. Both artifacts pass the current public
generation gate before cache reuse or conditional responses and hold a lease
through body delivery. Revocation cancels and drains old work before a mutation
commits. The shared cache holds at most 128 entries and 32 MiB for 60 seconds.
See the [export runbook](../runbooks/exports.md) for checks and resource
evidence.

## Pure renderer

The web package contains the licensed font catalog, a pure Vue renderer with
continuous and deterministic paged modes, and a generated registry for all 20
validated template presets. Template and sample-resume flows start a new resume
or change its presentation. Byte-exact HTML goldens cover both starting layouts
and display modes for every preset. Renderer-boundary lint rejects runtime,
state, network, clock, random, and ambient-locale dependencies. A pinned AMD64
Chromium harness proves the named screenshot and print baselines at zero
tolerance, all bundled fonts offline, the hostile corpus in a live DOM, and the
renderer CSP against both harness and normal Nuxt output. Its reviewed manifest
builds an immutable source archive from the exact commit and excludes the
worktree, Git metadata, local state, and secret-like paths.

[Display assertions](../design/display-assertions.md) is the accepted
replacement for these pixel comparisons. Implementation and Linux/macOS
acceptance remain pending; the existing gate above remains required until
cutover. Golden HTML, renderer isolation, and production determinism stay.
Product page images remain served assets with a separate generator planned.
