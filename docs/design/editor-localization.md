# Resume workspace localization

Status: Accepted (2026-09-20) under
[ADR 0047](../adr/0047-bilingual-resume-workspace.md), approved by the owner.

The resume workspace supports Vietnamese and English interface copy. Interface
language remains separate from resume language. A language toggle changes the
controls around a resume, never the resume itself.

## Decisions

The accepted product choices are:

1. **Expand the bilingual interface to the resume workspace.** The resume list,
   creation flows, editor, publish flow, and owner PDF export support Vietnamese
   and English.
2. **Keep one browser preference.** The workspace reuses the existing
   `aboutme-locale` cookie. The service does not add an account language
   preference.
3. **Keep interface and resume language independent.** Changing either language
   does not change the other. Interface changes never translate or replace
   document data.
4. **Extend the existing typed copy maps.** The release adds no localization
   framework, remote catalog, API field, or document field.
5. **Localize shared account chrome and defer its destinations.** The account
   menu shown in the application shell and editor is in scope. Settings, account
   session, password, identity, and privacy pages, connected-agent controls, and
   agent consent remain outside this release.

## Two language domains

| Domain             | Values                    | Authority                          | Effect                                                     |
| ------------------ | ------------------------- | ---------------------------------- | ---------------------------------------------------------- |
| Interface language | `vi` or `en`              | `aboutme-locale` browser cookie    | Page chrome, messages, titles, and accessible names        |
| Resume language    | Canonical BCP 47 or `und` | Resume metadata and render context | Resume `lang`, renderer defaults, dates, and public chrome |

The interface defaults to Vietnamese when the cookie is absent or invalid. The
existing cookie stays script-readable, uses path `/`, `SameSite=Lax`, and a
one-year lifetime. It contains only `vi` or `en`. Sign-in, navigation, and a
reload preserve the value. The preference remains local to a browser because
there is no account setting or server-side profile field.

The language control is available in the application shell on the resume list
and creation routes. The editor has the same control in its own top bar because
the editor does not render the application shell. The control remains reachable
and named at phone and desktop widths.

A blank flow initializes resume language from the interface language. The flow
captures that initial value once when it opens. A gallery sample's explicit
language wins. After initialization, an interface toggle does not change the
selected resume language, selected sample, suggested title, or starting
document. Closing the flow and starting a new blank flow uses the interface
language then selected.

An explicit resume-language edit may change `metadata.lng` and the renderer's
language-derived defaults. It never translates authored fields. An interface
toggle never writes `metadata.lng`.

## Localized surface

Application chrome means visible copy and non-visible assistive copy outside the
pure resume renderer. The release covers:

- Shared shell and editor chrome on resume routes: primary navigation, the
  language control, the account menu's accessible name, Settings link, theme
  action, and logout action.
- `/app/resumes`: page title, shell navigation shown on the route, loading and
  empty states, resume cards, relative times, menus, create, rename, delete,
  limits, retry states, and confirmation dialogs.
- `/app/new` and the create dialog: template and sample instructions, count and
  limit states, title and resume-language controls, validation, loading, and
  action labels.
- `/app/resumes/{id}`: the top bar, navigation rail, outline, fields, hints,
  options, rich-text toolbar, structure, templates, customization, photo and
  crop controls, preview controls, save state, conflicts, navigation guard,
  retry states, and status messages.
- Publish: every field label, explanation, warning, validation message,
  reauthentication state, uncertain outcome, success state, public-link action,
  and accessible announcement in the publish dialog.
- Owner PDF export: the action, page-size help, pending announcement, blocked
  state, and failure messages. The PDF document and public download link still
  follow resume language.
- Browser titles, document `lang`, focusable error summaries, live regions,
  accessible names, descriptions, keyboard help, and image alternative text on
  the routes above.

Brand names, template names, user text, resume titles, URLs, language tags,
error codes, test hooks, and protocol identifiers remain unchanged. A short,
reviewed allowlist owns other intentionally invariant terms.

Following the localized Settings link may open an English page. The release does
not translate settings pages, account-security dialogs, connected-agent
controls, or `/authorize`. Those destinations belong to v0.4.1. Public resume
chrome and authentication email copy retain their current language rules. The
pure renderer, public page, print route, and PDF content are outside this
interface change.

## Copy and state

The web reuses the existing `Locale` type, `useLocale()`, and
`useRouteLocale()`. Route classification includes `/app/new`, `/app/resumes`,
and the editor routes below `/app/resumes/`. It does not include settings or
`/authorize`.

Vietnamese and English catalogs have the same typed keys. Catalogs are split by
surface and expose `Record<Locale, SurfaceCopy>`, so resume-editor copy does not
enter landing, public-render, or print bundles. User-facing literals on the
localized routes come from those catalogs, apart from the reviewed invariant
allowlist.

Controllers keep semantic states, error codes, field paths, and retry data.
Components select copy for the current interface language. A toggle therefore
updates a shown error, dialog, loading state, or announcement without replaying
the action that produced it. Translated strings are not stored in a resume,
store record, pending command, URL, or API payload.

Page titles keep the existing `<page> · aboutme` form. The list, new-resume, and
generic loading titles use localized page names. A loaded editor keeps the
authored resume title and does not translate it. Scoped routes set the HTML
`lang` to the interface language. Resume preview roots keep the resume language.

Stable `data-*` hooks and action identifiers do not change. Tests may select
localized controls by role and name, but locale-neutral hooks remain available
where one test must cover both languages.

## Data, compatibility, and loss

This change has no database, schema, OpenAPI, MCP, sanitizing, renderer, or
migration change. It creates no new server state and sends no locale header or
field on resume requests.

Materialized document values are immutable under an interface toggle. This
includes authored text, a prefilled title, sample content, custom headings,
public title, slug, emoji, visibility choices, and all customization values. A
toggle also preserves unsaved field drafts, selection, focus, open dialogs,
pending commands, conflicts, and revision state. It must not remount the editor
or cause a save.

Older clients keep using English on resume routes and continue to read and edit
documents created by the new client. They already understand both allowed cookie
values. A rollback returns the resume workspace to English while leaving the
cookie and all resume data intact. There is no lossy conversion and no rollback
migration.

## Security and size

The locale cookie is untrusted input. The client accepts only exact `vi` and
`en` values and uses Vietnamese for every other value. Locale selection cannot
choose a module, URL, redirect, or HTML fragment.

Known API error codes and validation issue codes map to local safe copy. Codes
remain unchanged for control flow and logs. Unknown outcomes use a localized
generic message. Server messages and user values never become HTML; names and
other interpolated values render as text. The change does not weaken CSRF,
Origin, reauthentication, publish, export, or content-sanitizing boundaries.

Static catalogs are the only persistent size increase. The release adds no
runtime package, network request, API bytes, database bytes, or document bytes.
Route splitting keeps editor catalogs out of public-render and print workers.
The release build records the client asset delta so an accidental shared-bundle
import can be rejected before release.

## Acceptance cases

1. With no valid locale cookie, the resume list renders Vietnamese, sets
   `html lang="vi"`, and offers both language choices.
2. Selecting English on the list writes `aboutme-locale=en`; the choice survives
   sign-in, resume navigation, editor entry, and reload. Selecting Vietnamese
   provides the same persistence.
3. A user can list, create, edit, publish, copy the public link, and export an
   owner PDF in Vietnamese at phone and desktop widths. The same journey works
   in English.
4. Opening a blank create flow in Vietnamese initializes resume language to
   Vietnamese once. Switching the interface to English before submission changes
   only chrome. The selected language, title, starting document, and submitted
   payload stay byte-for-byte equal.
5. Opening a Vietnamese gallery sample and switching the interface to English
   preserves the sample, suggested title, content, and resume language.
6. An English resume opened under a Vietnamese interface keeps English preview
   labels and dates. Only the editor chrome is Vietnamese.
7. Explicitly changing resume language updates only the existing resume-language
   command and renderer-derived defaults. Authored content is unchanged.
8. Toggling the interface with a dirty field, a visible validation error, an
   open dialog, and a focused control preserves those states and focus. Shown
   chrome re-renders in the selected language. No API write or revision change
   occurs.
9. Every publish state and owner-export state has Vietnamese and English copy,
   including pending, blocked, rate-limited, session-ended, uncertain, retry,
   and success states. Publish settings and returned PDF bytes do not change
   because of interface language.
10. Known API and field issue codes produce localized safe text. An unknown code
    produces the localized generic message without displaying a server message
    or raw HTML.
11. Each scoped route has the correct interface `lang`, localized browser title,
    keyboard-reachable language control, accessible names, error focus, and live
    announcements. The preview root keeps its independent resume `lang`.
12. Catalog parity fails a test when either locale lacks a key. A source check
    fails for an unapproved user-facing literal on scoped surfaces.
13. Renderer golden output, schema fixtures, API fixtures, and stored resume
    payloads are unchanged in both interface languages.
14. A Vietnamese speaker reviews the full journey for wording, diacritics, text
    fit, keyboard use, focus order, announcements, and phone layout. Automated
    tests use fictional data.

## Release

[ADR 0047](../adr/0047-bilingual-resume-workspace.md) records the approved v4
change. The product, web, index, and decision-status designs integrate it.
Localization acceptance rows remain planned until implementation evidence proves
them.

Frontend work ships as one release because list, creation, editing, publish, and
export form one usable journey.

The implementation runs the Nuxt lint, typecheck, test, and build gates, updates
the web source manifest for new files, and runs the authenticated editor, entry,
publish, and export browser checks. The entry proof starts from a gallery sample
in each language, crosses sign-in and `/app/new`, and confirms that interface
toggles preserve the selected sample, suggested title, content, and resume
language. GitHub CI must pass on the exact release commit. Production
verification repeats the Vietnamese journey on phone and desktop, then checks
English and confirms one saved resume is unchanged after interface toggles.
