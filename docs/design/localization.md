# Interface localization

The interface supports Vietnamese and English. Interface language stays separate
from resume language: a language toggle changes the controls around a resume,
never the resume, and never an account, session, OAuth, or MCP authority.
[ADR 0047](../adr/0047-bilingual-resume-workspace.md) records the choice.

## Two language domains

| Domain             | Values                    | Authority                          | Effect                                                     |
| ------------------ | ------------------------- | ---------------------------------- | ---------------------------------------------------------- |
| Interface language | `vi` or `en`              | `aboutme-locale` browser cookie    | Page chrome, messages, titles, and accessible names        |
| Resume language    | Canonical BCP 47 or `und` | Resume metadata and render context | Resume `lang`, renderer defaults, dates, and public chrome |

The cookie is script-readable, path `/`, `SameSite=Lax`, with a one-year
lifetime. It holds only `vi` or `en`; a missing or invalid value selects
Vietnamese. Sign-in, provider round trips, navigation, and reload keep it.
Logout keeps the interface language as a device preference, so the next person
on a shared device inherits it. There is no account language field, server
locale, or locale header, so the choice is per browser.

Localized routes are the homepage, authentication and recovery pages, legal
pages, template gallery, the resume workspace (`/app/resumes`, `/app/new`, and
the editor below `/app/resumes/`), `/app/settings/sessions`, and `/authorize`.
Route classification in `useRouteLocale()` names these paths exactly. It
excludes `/oauth/*`, MCP, API, and public resume routes. A new settings route
joins only by an explicit change.

Public resume chrome follows resume language: `Tải PDF` and
`Tạo bằng aboutme.vn` for Vietnamese resumes, `Download PDF` and
`Built with aboutme.vn` otherwise. Authentication and security emails are fixed
bilingual copy, Vietnamese first. The renderer, public page, print route, and
PDF content never read the interface language.

## Resume language

A blank creation flow captures the interface language once, when it opens, as
its initial resume language; a gallery sample's explicit language wins. After
that, an interface toggle changes neither the selected resume language, sample,
suggested title, nor starting document. A new blank flow uses the interface
language current at that time.

Only an explicit resume-language edit changes `metadata.lng` and the renderer's
language-derived defaults. It never translates authored fields. An interface
toggle never writes `metadata.lng` and never sends a resume write.

## Surfaces and controls

Application chrome is every visible and assistive string outside the pure
renderer: titles, headings, labels, hints, options, errors, loading, empty,
retry, confirmation, success, and live-region text, accessible names and
descriptions, keyboard help, and image alternative text. On the resume workspace
it covers the list, creation, editor, publish dialog, and owner PDF export. On
settings it covers sessions, password, linked identities, privacy, and connected
agents. On `/authorize` it covers the request explanation, permission labels,
errors, and actions.

The application shell shows the language control on the list, creation,
settings, and consent routes; the editor shows it in its own top bar. The
control stays reachable and named at phone and desktop widths. Settings
confirmation dialogs (unlink, agent revoke, and account deletion) carry a second
`LocaleToggle` in the dialog header so it stays inside the focus trap; it does
not close or recreate the dialog. Pointer activation keeps the prior input focus
and selection; keyboard activation keeps focus on the language button.

Page titles use `<page> · aboutme`. A loaded editor keeps the authored resume
title untranslated. Localized routes set HTML `lang` to the interface language;
each resume preview root keeps the resume language.

Brand names (including provider, browser, and operating-system names), template
names, user text, resume titles, URLs, language tags, error codes, scope
identifiers, test hooks, and protocol identifiers stay unchanged. A short
reviewed allowlist names other invariant terms. Relative times follow the
interface locale. Identity and agent dates use UTC with `vi-VN` or `en-US`;
stored timestamps never change. The user-agent parser keeps its ASCII-only
4,096-byte bound and receives locale copy for its unknown label and joining
phrase.

## Catalogs and state

Each surface owns a typed static catalog, `Record<Locale, SurfaceCopy>` or the
shared `WorkspaceCopy<T>` generic, with identical Vietnamese and English keys.
Only the route that uses a catalog imports it: editor copy stays out of the
landing, public-render, and print bundles, and account and consent catalogs stay
in the settings and authorize chunks. Shared locale code holds locale values,
names, route classification, and cookie behavior, but no surface copy.

Controllers keep semantic states, closed outcomes, error codes, field paths,
identifiers, and retry data. Components choose copy for the current locale at
render time. Translated strings never enter a resume, store record, pending
command, URL, or API payload.

A locale change rerenders copy in place. It never remounts a route, refetches,
replays or submits a command, navigates, or saves. It keeps fetched data, field
drafts (including passwords and the typed `DELETE` confirmation), selection,
focus, open dialogs and their focus traps, pending operations, errors,
conflicts, and revision state. On `/authorize` it keeps the parsed query, client
name, scope identifiers, load result, and decision state, and never sends a
second consent read or decision. Errors keep `role="alert"`; status states keep
their semantics, and a locale change creates no false announcement.

Locale never changes HTTP methods, paths, query keys, redirect URIs, request
bodies, identifiers, OAuth decisions `approve` and `deny`, scopes `resumes:read`
and `resumes:write`, provider identifiers, error codes, or the exact uppercase
ASCII deletion token `DELETE`.

## Security

The cookie is untrusted input. The client accepts only exact `vi` and `en`, and
locale cannot choose a module, URL, redirect, or HTML fragment. Known API and
field issue codes map to local safe copy; an unknown outcome uses a localized
generic message and never shows a server message. Every interpolation renders as
text and catalog values are never HTML.

Localization changes no security contract in
[ADR 0014](../adr/0014-oauth-start-methods.md),
[ADR 0025](../adr/0025-password-authentication-and-identity-linking.md), or
[ADR 0026](../adr/0026-mcp-agent-access.md). CSRF, Origin, recent
reauthentication, publish, export, sanitizing, and consent checks are the same
in both languages, and no localized label becomes an action identifier.

## Compatibility and size

Localization adds no database, migration, schema, OpenAPI, MCP, sanitizer, API,
email, or document change and no server state. An older client shows English on
the routes it did not localize and still reads every document and cookie value.
Rolling back returns those routes to English and loses no data.

Static catalogs are the only size increase: no runtime package, network request,
or stored byte. A release build records the client chunk deltas. A
public-render, print, landing, auth, gallery, or editor chunk that gains another
surface's catalog is a defect.

## Acceptance

`AC-UI-014` and the editor acceptance rows hold the evidence. The durable checks
are:

1. With no valid cookie, every localized route renders Vietnamese with
   `html lang="vi"` and offers both languages.
2. Choosing a language writes the cookie, updates the route in place, and
   survives sign-in, navigation, and reload.
3. List, create, edit, publish, public-link copy, owner PDF export, every
   settings block, and consent work in both languages at phone and desktop
   widths.
4. Interface toggles leave the selected resume language, sample, title, starting
   document, submitted payloads, publish settings, PDF bytes, and stored resumes
   byte-for-byte unchanged.
5. Toggling with a dirty field, visible error, open dialog, pending action, and
   focused control keeps every one of them and sends no request.
6. An English resume under a Vietnamese interface keeps English preview labels
   and dates.
7. Known codes show localized safe text; unknown codes show the generic message.
8. Catalog parity fails when a locale lacks a key, and a source check fails on
   an unapproved literal on a localized route.
9. Renderer golden output, schema and API fixtures, bilingual email templates,
   and public resume chrome tests stay unchanged in both languages.
