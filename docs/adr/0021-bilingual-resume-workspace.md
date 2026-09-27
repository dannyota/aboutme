# 0021: Bilingual resume workspace

Status: Accepted (2026-09-20), approved by the owner.

The detailed contract is the
[resume workspace localization design](../design/localization.md).

## Context

The homepage, authentication pages, legal pages, and template gallery support
Vietnamese and English, but the resume workflow used English. The initial
community is Vietnamese, so a user could not create, edit, publish, and export a
resume in the default interface language.

Resume language is independent from interface language. The renderer uses resume
metadata for dates, labels, and public chrome. Expanding interface localization
must not turn an interface toggle into a resume write or content translation.

## Decision

1. The resume list, blank and sample creation flows, editor, publish dialog,
   owner PDF export, browser titles, errors, accessible copy, and shared account
   menu support Vietnamese and English.
2. Interface language is the `vi` or `en` value in the `aboutme-locale` browser
   cookie. Vietnamese is the default. Settings, account destination pages,
   connected-agent controls, and agent consent stay outside this decision.
3. Resume language is the canonical BCP 47 metadata value. An interface toggle
   never changes resume language, authored content, materialized defaults,
   public settings, unsaved drafts, pending commands, conflicts, or revision
   state, and never sends a resume write.
4. A blank creation flow captures the interface language as its initial resume
   language once when the flow opens. An explicit gallery sample language wins.
   Later interface toggles do not regenerate the sample, title, or starting
   document.
5. The web app extends its typed per-locale copy maps. Controller state keeps
   semantic outcomes, codes, and paths; components select copy for the current
   interface language. No remote catalog or localization framework is added.

## Compatibility, loss, and security

This decision changes no database, resume schema, OpenAPI, MCP, sanitizing,
renderer, public page, print, or PDF-content contract. It needs no data
migration and has no lossy conversion. Older clients show English
resume-workspace chrome and can read and edit the same documents. A rollback
returns that chrome to English without changing the locale cookie or resume
data.

The cookie is untrusted input. Only exact `vi` and `en` values select a locale.
Known API and validation codes map to local safe copy; unknown outcomes use a
generic localized message. Server messages and user values render as text, never
HTML. CSRF, Origin, reauthentication, publish, export, and content sanitizing
boundaries do not change.

## Consequences

- Static copy maps are the only runtime size increase. Route splitting keeps
  editor copy out of public-render and print workers.
- Automated and browser acceptance covers both languages, phone and desktop
  widths, gallery-sample entry, live toggles with dirty state, errors,
  accessibility, publish, and export. Vietnamese copy also receives human
  review.

## History

Former ADR 0047, unchanged in substance.
