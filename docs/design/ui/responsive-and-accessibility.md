# aboutme responsive behavior and accessibility

Narrow editor layouts, interaction and motion, and accessibility. Part of the
[visual design](../../../DESIGN.md).

## Responsive behavior

At widths up to 72 rem, the editor tool rail becomes a horizontal bar, the
outline and inspector share the lower workspace, the preview spans the available
editor area, and the bottom Edit/Preview tab bar switches between the editor and
preview. At widths up to 42 rem, the outline moves into a Sections sheet.

At widths up to 42 rem, the editor top bar grows to 96 px and splits into two
rows. The first holds the logo mark alone, save state, and the language toggle;
the divider, title, and public mark are hidden. The second holds Download PDF,
Publish, and the account menu. The bottom Edit and Preview tabs mark the active
tab with the secondary fill and semibold `--link` text. The preview uses a fit
zoom calculated from the available width minus 32 px on phone screens; larger
narrow layouts use 0.72 and wide layouts use 0.84 unless full zoom is requested.

## Interaction and motion

Text fields commit on blur or Enter; rich text also commits after a 400 ms pause
in typing, so the preview follows as the person writes. Empty values remove a
field, unchanged values send nothing, and Escape restores the last committed
value. Selects, checkboxes, switches, colors, and numbers commit on change. An
edit that cannot save yet, such as a date range with a start and no end, is held
in memory, shows Unsaved, and survives the field remounting.

Publishing stamps the top bar's public mark in a single 180 ms press from scale
1.12 to 1 with the ink fading in. Unpublishing lifts it in 120 ms. Primitive
controls use short color, focus, and open/close transitions. Nothing animates on
page load. Reduced motion disables the stamp and reduces application animation
and transition durations to an instant effect.

## Accessibility

The implemented surfaces use landmark labels, heading relationships, visible
focus rings, semantic buttons and links, toolbar and tab roles, and
`aria-current`/`aria-selected` state where applicable. Dialogs, sheets, menus,
and tabs are keyboard-operable. Save, photo, preview, publish, copy, and failure
messages use status or alert announcements where needed.

The renderer names the person in an h1 by default. A renderer embed inside a
chrome page whose heading owns the h1 passes `nameHeading: 'p'` in its render
context, so the name is a paragraph: the homepage sample, the sheet thumbnails
on Library cards, the new-resume confirmation, and the Create resume dialog, and
the editor's template thumbnails. The full document on a template page and the
editor preview keep the default h1.

The app reset and focus styles apply under `data-ui="app"` only and skip
`.resume-document`, `.paged-resume`, and their descendants.
