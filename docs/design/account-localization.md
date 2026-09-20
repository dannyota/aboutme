# Account and agent localization

Status: Approved for implementation by the manager under the owner's delegated
release scope. The owner has not reviewed this artifact. No product choice in
this design still needs owner approval.

The account settings page and agent consent page support Vietnamese and English.
They reuse the site locale without changing any account, session,
authentication, OAuth, or MCP authority.

## Decisions

1. `/app/settings/sessions` and `/authorize` use the existing interface locale.
   Vietnamese remains the default and English remains available.
2. The routes reuse the `aboutme-locale` cookie and the existing `Locale`,
   `useLocale()`, `useRouteLocale()`, and `LocaleToggle` contracts. There is no
   account language field or server locale.
3. Each settings unit and the consent route own a typed static copy catalog.
   Account and consent copy stay in their route chunks.
4. Controllers keep semantic states, codes, identifiers, field values, and
   pending operations. Components derive visible and assistive copy from the
   current locale.
5. The release translates the current UI. It does not change the actions a user
   may take, the checks before an action, or the effect of an action.

Adding a localization framework, a remote catalog, or an account language
preference would add a new runtime or data contract without improving these two
routes. Those alternatives are outside this design.

## Locale contract

The locale remains exactly `vi` or `en`. A missing or invalid cookie selects
Vietnamese. The cookie remains script-readable, uses path `/`, `SameSite=Lax`,
and a one-year lifetime. Selecting a language writes only that cookie and shared
client locale state.

Route classification adds these exact paths:

- `/app/settings/sessions`
- `/authorize`

The classification does not include `/oauth/*`, MCP routes, API routes, public
resume routes, or future settings routes. Sign-in and provider round trips
preserve the cookie under its existing path rule. The `/authorize` return path
and query remain byte-for-byte unchanged across sign-in and locale changes.

Both routes set the document `lang` and browser title from the interface locale.
The title keeps the `<page> · aboutme` form. The application shell shows the
same language control on both routes and uses localized shell and account menu
copy.

## Surface map

| Route or unit            | Localized content                                                                                                 | Preserved behavior                                                                         |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| `/app/settings/sessions` | Title, headings, loading and empty states, device details, actions, errors, success messages, and assistive text  | Session reads, logout, one-session revoke, revoke-all, recent reauthentication             |
| `PasswordSettings`       | Password status, labels, show or hide names, policy errors, pending and success states                            | Add or change only, password and provider reauthentication, session rotation               |
| `LinkedIdentities`       | Dates, availability, last-method reason, unlink flow, confirmation, errors, and success follow-up                 | Enabled-provider rules, no email display, exact identity selection, reauthentication       |
| `PrivacySettings`        | Export and deletion copy, disclosure, labels, errors, pending states, and confirmation                            | Export bytes, bodyless deletion, recent reauthentication, deletion timing                  |
| `ConnectedAgents`        | Heading, scope labels, dates, loading, empty, unavailable, retry, revoke, and confirmation                        | Capability gating, grant listing, CSRF, revoke, session-ended redirect                     |
| `/authorize`             | Title, request explanation, loading, invalid and unavailable errors, permission labels, actions, and pending copy | Query validation, sign-in return, consent read, CSRF-protected decision, external redirect |
| Shared shell             | Navigation names, account menu, theme action, language control, and accessible names                              | Session-dependent navigation and logout                                                    |

Provider brand names, browser names, operating-system names, `aboutme`, and MCP
remain invariant. A provider identifier still selects the same fixed brand name
in both locales.

Session relative times use the current locale through the existing relative time
formatter. Linked-identity and connected-agent dates use UTC with `vi-VN` for
Vietnamese and `en-US` for English. Date localization never changes stored
timestamps.

The user-agent parser keeps its current ASCII-only 4,096-byte bound, browser
recognition, operating-system recognition, and unknown fallback. Settings pass
locale-owned copy for the unknown label and the phrase joining browser and
operating system. The parser never treats a user-agent string as markup.

## Catalog and chunk boundaries

The settings route, password settings, linked identities, privacy settings,
connected agents, and consent route use separate catalogs. Each catalog reuses
the existing `WorkspaceCopy<T>` generic so Vietnamese and English have the same
typed shape. This split lets each component and its tests change without a
shared account-copy file.

Only the settings route subtree imports the five account catalogs. Only the
authorize route imports the consent catalog. Shared locale code contains locale
values, names, route classification, and cookie behavior, but no account or
consent paragraphs. Public render, print, landing, gallery, auth, and editor
workers must not import these catalogs.

Copy functions may interpolate trusted local labels and untrusted text. Vue
renders every interpolation as text. Catalog values are never HTML.

Tests compare the key shape of Vietnamese and English catalogs. A source check
or focused literal inventory covers visible strings, accessible names,
descriptions, loading labels, dialog copy, and error copy on both routes. Stable
`data-*` hooks and action identifiers do not change.

## State and payload preservation

Changing locale rerenders copy in place. It must not remount either route,
refetch data, replay a command, submit a form, navigate, or change a URL.

On settings, a locale change preserves:

- fetched sessions, identities, capabilities, and grants;
- selected identity or grant and every open confirmation dialog;
- current and new password drafts, password confirmation, and the exact typed
  account-deletion confirmation value;
- reauthentication mode, export state, errors, success state, and retry state;
- every pending read or write, including its disabled controls;
- focus, selection, and the dialog focus trap.

On consent, a locale change preserves the parsed authorization query, client
name, requested scope identifiers, load result, error kind, decision state, and
pending request. It never makes a second consent read or decision request.

Controllers store closed semantic outcomes instead of translated strings. Known
server codes map to those outcomes. The current locale maps an outcome to copy
at render time, so a visible error changes language without losing the error or
repeating the action. Unknown failures use a localized generic message and never
expose a raw server message.

Locale changes never enter these values:

- HTTP methods, paths, query keys, redirect URIs, or request bodies;
- session, identity, grant, client, or account identifiers;
- OAuth decisions `approve` and `deny`;
- scope identifiers `resumes:read` and `resumes:write`;
- provider identifiers or API error codes;
- the exact account-deletion confirmation token `DELETE`.

The label around `DELETE` is localized. The required token stays uppercase
ASCII, and comparison remains exact.

## Dialogs, focus, and announcements

The settings shell language control is outside teleported modal focus traps.
Each unlink, connected-agent revoke, and account-deletion confirmation includes
a second `LocaleToggle` in the dialog header through the existing header action
slot. This control stays inside the trap and uses the localized language-group
name. It does not close or recreate the dialog.

Pointer activation preserves the prior input focus and selection under the
existing locale-control pointer rule. Keyboard activation intentionally keeps
focus on the language button. Neither path performs a false focus restore.
Closing a dialog still restores focus to its opener. A pending destructive
action still blocks cancel, Escape, and duplicate submission under the existing
dialog rules.

The localized surface includes:

- page and section headings;
- form labels and password visibility names;
- dialog titles, descriptions, confirmation labels, and cancellation labels;
- loading text exposed to assistive technology;
- disabled-control reasons and `aria-describedby` content;
- error summaries, status messages, and retry actions;
- consent permission-list names and descriptions.

Errors keep `role="alert"` and focus behavior. Loading and success states keep
their status semantics. Changing locale updates the text of the current state
without creating a false success or failure announcement. Keyboard order and
control roles remain unchanged at phone and desktop widths.

## Consent and account security

Localization does not change the security contracts in ADR 0014, ADR 0025, or
ADR 0026.

Privileged provider starts remain authenticated POST requests with CSRF and
exact-Origin checks. Password changes, provider unlinking, session revocation,
account deletion, and agent revocation keep their recent-authentication,
capability, and session rules. No localized label becomes an action identifier.

The consent page still accepts only the closed query shape validated by Go and
checked again by the client. It posts the same query and decision through the
session, CSRF, and exact-Origin chain. Client-supplied names render as text.
Tokens, authorization codes, PKCE values, provider emails, and raw server
messages never enter copy, logs, or UI.

The connected-agent block still appears only when agent access is enabled. It
shows the fixed safe projection of client name, scopes, and timestamps. A locale
change grants no scope and cannot revive a revoked grant.

## Data, compatibility, and loss

This design changes no database, migration, resume schema, OpenAPI, MCP,
sanitizer, API payload, dependency, email payload, or document format. It adds
no server state. There is no data migration or lossy conversion.

Older clients continue to show English settings and consent copy. They ignore no
new field because there is no new field. They continue to understand both cookie
values and perform the same account and OAuth operations. Documents and account
data touched by a newer client remain readable by an older client.

A rollback returns settings and consent copy to English while retaining the
locale cookie. The rollback changes no stored account, grant, session, or resume
data and needs no rollback migration.

## Existing language surfaces

Authentication email templates already send one fixed bilingual message with
Vietnamese first and English second for verification, reset, and password-change
mail. This release verifies those templates and does not change them.

Public resume chrome already follows resume language, not interface language.
Vietnamese resumes use `Tải PDF` and `Tạo bằng aboutme.vn`; other resume
languages use `Download PDF` and `Built with aboutme.vn`. This release verifies
those labels and does not change the public renderer, public HTML validator,
print route, or PDF content.

## Size

Static route catalogs are the only persistent size increase. The release adds no
package, network request, API byte, database byte, email byte, or stored
document byte.

The release build records the settings and authorize client-chunk deltas. A
public-render, print, landing, auth, gallery, or editor chunk that gains any
account or consent catalog is a release defect.

## Acceptance

1. With no valid cookie, settings and consent render Vietnamese, set
   `html lang="vi"`, show localized page titles, and offer both languages.
2. Selecting English updates the current route in place, writes
   `aboutme-locale=en`, survives reload and sign-in round trips, and does not
   change a request payload or URL.
3. Every settings block works in Vietnamese and English, including devices,
   password, providers, privacy, connected agents, errors, retries,
   reauthentication, confirmations, and success states.
4. Locale changes preserve fields, the typed `DELETE` token, open dialogs,
   selected targets, focus, errors, pending operations, and fetched data. No
   read or write is repeated.
5. Locale controls inside all three settings confirmation dialogs are keyboard
   reachable within the focus trap. Dialog close still restores opener focus.
6. Consent works in both languages for loading, one or both scopes, approval,
   denial, invalid request, unavailable service, and sign-in return. The query,
   scope identifiers, decision values, and client name remain unchanged.
7. Known errors produce localized safe copy. Unknown errors produce the generic
   localized message. Raw codes and server messages are not shown.
8. Catalog parity and literal inventory fail when a locale key or covered string
   is missing. Existing security and request-shape tests remain green.
9. Phone and desktop browser checks cover text fit, diacritics, keyboard order,
   focus, announcements, dialogs, pending actions, and locale persistence.
10. Existing bilingual email-template tests and public resume chrome tests stay
    green without fixture or production-code changes.

## Release

Frontend implementation and tests ship as one account-localization release. The
implementation runs the Nuxt lint, typecheck, test, and build gates, updates the
web source manifest for new files, and runs the authenticated settings, MCP, and
entry browser checks. Existing behavior tests select English with the shared
test locale helper. Focused bilingual tests cover Vietnamese and live locale
changes. The exact release commit requires independent review and green GitHub
CI.

The manager may deploy that reviewed green commit under the owner's delegated
scope without waiting for an owner UI review. Production verification checks the
signed-out shell and invalid consent entry in Vietnamese and English. It may
read the owner's settings when an authorized owner account is available. Local
automated and browser evidence covers settings, consent, existing bilingual
email templates, and public resume chrome in both languages. Verification makes
no production write solely for this release. The owner may inspect the live
result, but that inspection is not a deployment gate.
