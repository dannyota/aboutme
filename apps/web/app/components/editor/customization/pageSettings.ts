import type { Customization } from '@aboutme/schema';
import type { Locale } from '../../../i18n/locale';
import { editorControlsCopy } from '../../../i18n/editor-controls';

import type { CustomizationDelta } from '../../../editor/commands';

/**
 * Page size and margin choices for the PDF and print. The schema stores
 * margins in millimetres, 0–40 per axis, and an absent margin renders as
 * 15 mm (docs/design/templates/print.md). Letter pages show inches; the
 * stored value stays in millimetres.
 */

export type PageFormat = Customization['pageFormat'];
export type PageMargin = NonNullable<Customization['spacing']['pageMargin']>;
export type MarginChoice = 'narrow' | 'normal' | 'wide' | 'custom';
export type MarginAxis = 'x' | 'y';

export const DEFAULT_MARGIN_MM = 15;
export const MAX_MARGIN_MM = 40;
/** Consumer printers cannot print closer than this to the paper edge. */
export const PRINTABLE_EDGE_MM = 5;
const MM_PER_INCH = 25.4;

export const MARGIN_PRESETS: readonly {
  readonly choice: Exclude<MarginChoice, 'custom'>;
  readonly mm: number;
}[] = [
  { choice: 'narrow', mm: 10 },
  { choice: 'normal', mm: DEFAULT_MARGIN_MM },
  { choice: 'wide', mm: 25 },
];

export function marginLabel(
  choice: MarginChoice,
  locale: Locale = 'en',
): string {
  return editorControlsCopy[locale].page.marginLabels[choice];
}

export const PAGE_SIZE_LABELS: Readonly<Record<PageFormat, string>> = {
  a4: 'A4 · 210 × 297 mm',
  letter: 'Letter · 8.5 × 11 in',
};

export const PAGE_SIZE_SHORT: Readonly<Record<PageFormat, string>> = {
  a4: 'A4',
  letter: 'Letter',
};

export function unitFor(format: PageFormat): 'mm' | 'in' {
  return format === 'letter' ? 'in' : 'mm';
}

/** The margins the renderer uses: the stored pair, else 15 mm each. */
export function effectiveMargin(margin: PageMargin | undefined): PageMargin {
  return margin ?? { x: DEFAULT_MARGIN_MM, y: DEFAULT_MARGIN_MM };
}

/** The preset whose value both axes equal, else custom. */
export function marginChoice(margin: PageMargin | undefined): MarginChoice {
  const { x, y } = effectiveMargin(margin);
  if (x !== y) return 'custom';
  return MARGIN_PRESETS.find(({ mm }) => mm === x)?.choice ?? 'custom';
}

/**
 * The deltas that select a preset. Normal clears the stored pair, because
 * absence already renders as 15 mm. Custom changes nothing by itself; it
 * reveals the per-axis fields.
 */
export function presetDeltas(
  choice: MarginChoice,
  margin: PageMargin | undefined,
): readonly CustomizationDelta[] {
  if (choice === 'custom') return [];
  if (choice === 'normal') {
    return margin === undefined
      ? []
      : [{ op: 'unset', path: 'spacing.pageMargin' }];
  }
  const mm = MARGIN_PRESETS.find((preset) => preset.choice === choice)!.mm;
  if (margin?.x === mm && margin.y === mm) return [];
  return [
    { op: 'set', path: 'spacing.pageMargin.x', value: mm },
    { op: 'set', path: 'spacing.pageMargin.y', value: mm },
  ];
}

/**
 * The deltas for one custom axis. A document with no stored pair gets both
 * axes, because the schema requires x and y together.
 */
export function axisDeltas(
  axis: MarginAxis,
  mm: number,
  margin: PageMargin | undefined,
): readonly CustomizationDelta[] {
  if (margin === undefined) {
    const next = { ...effectiveMargin(undefined), [axis]: mm };
    return [
      { op: 'set', path: 'spacing.pageMargin.x', value: next.x },
      { op: 'set', path: 'spacing.pageMargin.y', value: next.y },
    ];
  }
  if (margin[axis] === mm) return [];
  return [{ op: 'set', path: `spacing.pageMargin.${axis}`, value: mm }];
}

function round(value: number, places: number): number {
  const factor = 10 ** places;
  return Math.round(value * factor) / factor;
}

/** A stored millimetre value in the page's unit, for display and input. */
export function toDisplay(mm: number, format: PageFormat): number {
  return format === 'letter' ? round(mm / MM_PER_INCH, 2) : round(mm, 2);
}

/**
 * A typed value in the page's unit, back in millimetres; undefined when it is
 * not a number within 0–40 mm.
 */
export function fromDisplay(
  value: number,
  format: PageFormat,
): number | undefined {
  if (!Number.isFinite(value)) return undefined;
  const mm = format === 'letter' ? round(value * MM_PER_INCH, 2) : value;
  return mm >= 0 && mm <= MAX_MARGIN_MM ? mm : undefined;
}

/** A millimetre length written in the page's unit, e.g. "15 mm", "0.6 in". */
export function formatLength(mm: number, format: PageFormat): string {
  return format === 'letter'
    ? `${round(mm / MM_PER_INCH, 1)} in`
    : `${round(mm, 1)} mm`;
}

export function presetLabel(
  choice: MarginChoice,
  format: PageFormat,
  locale: Locale = 'en',
): string {
  if (choice === 'custom') return marginLabel(choice, locale);
  const preset = MARGIN_PRESETS.find((item) => item.choice === choice)!;
  return `${marginLabel(choice, locale)} · ${formatLength(preset.mm, format)}`;
}
