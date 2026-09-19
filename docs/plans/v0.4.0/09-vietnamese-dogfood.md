# Vietnamese workspace dogfood brief

Role: user. Model: `gpt-5.6-terra`. Start after scripted browser proof passes.
This persona report supports product review. It does not replace the required
native Vietnamese-speaking human review.

## Objective and authority

Judge the localized workspace as a Vietnamese job seeker. Read `AGENTS.md`, the
accepted localization design, `AC-EDITOR-018`, and `AC-UI-014`.

Use a fictional account on the local stack only. Do not use the owner's
production account. Do not edit files. Save bounded evidence under
`.dev/personas/v0.4.0-localization/`.

## Journey

1. At `390x844`, start signed out with no locale cookie. Register from a
   Vietnamese gallery sample, create the private copy, edit every major section,
   customize it, replace and crop a photo, publish it, copy the link, and
   download the owner PDF.
2. During creation, toggle to English and back. Check that the sample, title,
   content, and resume language do not change.
3. During editing, leave a dirty field and a visible validation error, focus a
   control, open a dialog, then toggle language. Check copy, focus, data, and
   dialog continuity.
4. Repeat the complete journey at `1440x900` in Vietnamese.
5. Switch to English, reload, and check the list, editor, publish dialog, and
   PDF action. Open an English resume under Vietnamese chrome and judge the
   language boundary.
6. Review wording, Vietnamese diacritics, natural terminology, text fit,
   keyboard use, focus order, live announcements, and error recovery.

## Report contract

Report each friction point with exact steps, viewport, severity, owning role,
and screenshot path. State whether all user-authored text and resume language
survived each toggle. List open wording choices that need owner judgment. Do not
perform Git operations. Use short plain text with no em dash.
