# 0047: Bilingual resume workspace

Status: Accepted (2026-09-20), approved by the owner.

Amends the English-only application boundary in the
[product design](../design/product.md). The detailed contract is the
[resume workspace localization design](../design/editor-localization.md).

## Context

The homepage, authentication pages, legal pages, and template gallery support
Vietnamese and English, but the resume workflow uses English. The initial
community is Vietnamese, so a user cannot complete resume creation, editing,
publishing, and owner PDF export in the default interface language.

Resume language is already independent from interface language. The renderer
uses resume metadata for dates, labels, and public chrome. Expanding interface
localization must not turn an interface toggle into a resume write or content
translation.

## Decision

1. The resume list, blank and sample creation flows, editor, publish dialog,
   owner PDF export, browser titles, errors, accessible copy, and shared account
   menu support Vietnamese and English.
2. Interface language remains the `vi` or `en` value in the existing
   `aboutme-locale` browser cookie. Vietnamese remains the default. Settings,
   account destination pages, connected-agent controls, and agent consent stay
   outside this decision.
3. Resume language remains the existing canonical BCP 47 metadata value. An
   interface toggle never changes resume language, authored content,
   materialized defaults, public settings, unsaved drafts, pending commands,
   conflicts, or revision state, and never sends a resume write.
4. A blank creation flow captures the interface language as its initial resume
   language once when the flow opens. An explicit gallery sample language wins.
   Later interface toggles do not regenerate the sample, title, or starting
   document.
5. The web app extends its existing typed per-locale copy maps. Controller state
   keeps semantic outcomes, codes, and paths; components select copy for the
   current interface language. No remote catalog or localization framework is
   added.

## Compatibility, loss, and security

This decision changes no database, resume schema, OpenAPI, MCP, sanitizing,
renderer, public page, print, or PDF-content contract. It needs no data
migration and has no lossy conversion. Older clients continue to show English
resume-workspace chrome and can read and edit the same documents. A rollback
returns that chrome to English without changing the locale cookie or resume
data.

The cookie remains untrusted input. Only exact `vi` and `en` values select a
locale. Known API and validation codes map to local safe copy; unknown outcomes
use a generic localized message. Server messages and user values render as text,
never HTML. CSRF, Origin, reauthentication, publish, export, and content
sanitizing boundaries do not change.

## Size and release

Static copy maps are the only runtime size increase. The change adds no package,
request, API payload, database, or document bytes. Route splitting keeps editor
copy out of public-render and print workers.

The release ships list, creation, editor, publish, and owner export support
together. Automated and browser acceptance covers both languages, phone and
desktop widths, gallery-sample entry, live toggles with dirty state, errors,
accessibility, publish, and export. Vietnamese copy also receives human review.
Acceptance remains planned until that evidence is recorded.
